package gateway

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agendash/AgenSense/internal/device"
	"github.com/agendash/AgenSense/internal/httpapi"
	"github.com/agendash/AgenSense/internal/protocol"
	"github.com/agendash/AgenSense/internal/provider"
	"github.com/agendash/AgenSense/internal/service"
	"github.com/agendash/AgenSense/internal/session"
	"github.com/agendash/AgenSense/internal/store"
)

func TestGatewayRoundTrip(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	repo, err := store.NewFileRepository(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatalf("NewFileRepository() error = %v", err)
	}
	if err := repo.SeedDemoData(context.Background()); err != nil {
		t.Fatalf("SeedDemoData() error = %v", err)
	}

	control := service.NewDeviceControl(device.NewService(repo), repo, 30)
	registry := service.NewRegistryService(repo)
	inference := service.NewRuntimeInferenceService(registry, provider.NewFactory(nil), nil)
	pipeline := &session.Pipeline{
		ASR: provider.MockASR{},
		LLM: provider.MockLLM{},
		TTS: provider.MockTTS{},
	}
	server := httptest.NewServer(httpapi.NewRouter(control, registry, inference, NewHandler(control, pipeline), nil, nil))
	defer server.Close()
	control.SetPublicBaseURL(server.URL)

	bootstrapResp := bootstrapDevice(t, server.URL)
	conn, reader := dialWebSocket(t, bootstrapResp.WSURL, bootstrapResp.DeviceToken, bootstrapResp.DeviceID)
	defer conn.Close()

	helloEvent, err := protocol.NewEvent(protocol.EventHello, "req-001", "", protocol.HelloPayload{
		Device: protocol.HelloDevice{
			DeviceID:        bootstrapResp.DeviceID,
			HardwareSKU:     device.DemoFirmwareSKU,
			FirmwareVersion: "1.2.0",
			Capabilities: map[string]any{
				"display": "lcd",
				"touch":   true,
				"usb_hid": true,
				"usb_mic": true,
			},
		},
		State: protocol.HelloState{ConfigVersion: bootstrapResp.ConfigVersion},
	})
	if err != nil {
		t.Fatalf("NewEvent(hello) error = %v", err)
	}
	helloPayload, err := protocol.MarshalEvent(helloEvent)
	if err != nil {
		t.Fatalf("MarshalEvent(hello) error = %v", err)
	}
	writeClientFrame(t, conn, 0x1, helloPayload)

	event1 := readTextEvent(t, reader)
	if event1.Type != protocol.EventHelloOK {
		t.Fatalf("first event = %q, want %q", event1.Type, protocol.EventHelloOK)
	}
	event2 := readTextEvent(t, reader)
	if event2.Type != protocol.EventConfigSnapshot {
		t.Fatalf("second event = %q, want %q", event2.Type, protocol.EventConfigSnapshot)
	}

	audioStart, err := protocol.NewEvent(protocol.EventAudioStart, "", "", protocol.AudioStartPayload{
		StreamID:     "st-001",
		Codec:        "pcm_s16le",
		SampleRateHz: 16000,
		Channels:     1,
	})
	if err != nil {
		t.Fatalf("NewEvent(audio.start) error = %v", err)
	}
	audioStartBytes, _ := protocol.MarshalEvent(audioStart)
	writeClientFrame(t, conn, 0x1, audioStartBytes)
	writeClientFrame(t, conn, 0x2, []byte{0x01, 0x02, 0x03, 0x04})
	audioStop, err := protocol.NewEvent(protocol.EventAudioStop, "", "", protocol.AudioStopPayload{
		StreamID: "st-001",
		LastSeq:  1,
	})
	if err != nil {
		t.Fatalf("NewEvent(audio.stop) error = %v", err)
	}
	audioStopBytes, _ := protocol.MarshalEvent(audioStop)
	writeClientFrame(t, conn, 0x1, audioStopBytes)

	seen := map[string]bool{}
	binaryFrames := 0
	deadline := time.Now().Add(3 * time.Second)
	if err := conn.SetReadDeadline(deadline); err != nil {
		t.Fatalf("SetReadDeadline() error = %v", err)
	}
	for !(seen[protocol.EventASRFinal] && seen[protocol.EventLLMDone] && seen[protocol.EventTTSStart] && seen[protocol.EventTTSStop] && seen[protocol.EventActionExecute] && binaryFrames > 0) {
		opcode, payload := readServerFrame(t, reader)
		switch opcode {
		case 0x1:
			event, err := protocol.DecodeEvent(payload)
			if err != nil {
				t.Fatalf("DecodeEvent(server) error = %v", err)
			}
			seen[event.Type] = true
		case 0x2:
			binaryFrames++
		default:
			t.Fatalf("unexpected opcode %d", opcode)
		}
	}
}

func bootstrapDevice(t *testing.T, baseURL string) service.BootstrapResponse {
	t.Helper()

	body := bytes.NewBufferString(`{
		"device_id":"` + device.DemoDeviceID + `",
		"chip_id":"` + device.DemoFirmwareChipID + `",
		"hardware_sku":"` + device.DemoFirmwareSKU + `",
		"firmware_version":"1.2.0",
		"firmware_channel":"stable",
		"capabilities":{
			"display":"lcd",
			"touch":true,
			"usb_hid":true,
			"usb_mic":true
		},
		"claim_token":"` + device.DemoClaimToken + `"
	}`)
	resp, err := http.Post(baseURL+"/v1/bootstrap", "application/json", body)
	if err != nil {
		t.Fatalf("POST /v1/bootstrap error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		t.Fatalf("bootstrap status = %d body=%s", resp.StatusCode, data)
	}
	var out service.BootstrapResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode bootstrap response: %v", err)
	}
	if out.DeviceToken == "" || out.WSURL == "" {
		t.Fatalf("unexpected bootstrap response: %+v", out)
	}
	return out
}

func dialWebSocket(t *testing.T, wsURL, token, deviceID string) (net.Conn, *bufio.Reader) {
	t.Helper()

	parsed, err := url.Parse(wsURL)
	if err != nil {
		t.Fatalf("Parse(wsURL) error = %v", err)
	}

	conn, err := net.Dial("tcp", parsed.Host)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}

	reader := bufio.NewReader(conn)
	keyBytes := make([]byte, 16)
	if _, err := rand.Read(keyBytes); err != nil {
		t.Fatalf("rand.Read() error = %v", err)
	}
	key := base64.StdEncoding.EncodeToString(keyBytes)
	req := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: %s\r\nAuthorization: Bearer %s\r\nX-Device-Id: %s\r\nX-Protocol-Version: v1\r\n\r\n",
		parsed.RequestURI(), parsed.Host, key, token, deviceID)
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatalf("write handshake: %v", err)
	}

	resp, err := http.ReadResponse(reader, &http.Request{Method: http.MethodGet})
	if err != nil {
		t.Fatalf("ReadResponse() error = %v", err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		data, _ := io.ReadAll(resp.Body)
		t.Fatalf("handshake status = %d body=%s", resp.StatusCode, data)
	}
	return conn, reader
}

func readTextEvent(t *testing.T, reader *bufio.Reader) protocol.Envelope {
	t.Helper()
	opcode, payload := readServerFrame(t, reader)
	if opcode != 0x1 {
		t.Fatalf("opcode = %d, want text", opcode)
	}
	event, err := protocol.DecodeEvent(payload)
	if err != nil {
		t.Fatalf("DecodeEvent() error = %v", err)
	}
	return event
}

func readServerFrame(t *testing.T, reader *bufio.Reader) (byte, []byte) {
	t.Helper()

	first, err := reader.ReadByte()
	if err != nil {
		t.Fatalf("ReadByte(first) error = %v", err)
	}
	second, err := reader.ReadByte()
	if err != nil {
		t.Fatalf("ReadByte(second) error = %v", err)
	}
	opcode := first & 0x0F
	length := int(second & 0x7F)
	switch length {
	case 126:
		var v uint16
		if err := binary.Read(reader, binary.BigEndian, &v); err != nil {
			t.Fatalf("binary.Read(uint16) error = %v", err)
		}
		length = int(v)
	case 127:
		var v uint64
		if err := binary.Read(reader, binary.BigEndian, &v); err != nil {
			t.Fatalf("binary.Read(uint64) error = %v", err)
		}
		length = int(v)
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(reader, payload); err != nil {
		t.Fatalf("ReadFull(payload) error = %v", err)
	}
	return opcode, payload
}

func writeClientFrame(t *testing.T, conn net.Conn, opcode byte, payload []byte) {
	t.Helper()

	var frame bytes.Buffer
	frame.WriteByte(0x80 | opcode)

	length := len(payload)
	switch {
	case length < 126:
		frame.WriteByte(0x80 | byte(length))
	case length <= 0xFFFF:
		frame.WriteByte(0x80 | 126)
		if err := binary.Write(&frame, binary.BigEndian, uint16(length)); err != nil {
			t.Fatalf("binary.Write(uint16) error = %v", err)
		}
	default:
		frame.WriteByte(0x80 | 127)
		if err := binary.Write(&frame, binary.BigEndian, uint64(length)); err != nil {
			t.Fatalf("binary.Write(uint64) error = %v", err)
		}
	}

	mask := [4]byte{0x11, 0x22, 0x33, 0x44}
	frame.Write(mask[:])
	masked := append([]byte(nil), payload...)
	for i := range masked {
		masked[i] ^= mask[i%4]
	}
	frame.Write(masked)

	if _, err := conn.Write(frame.Bytes()); err != nil {
		t.Fatalf("Write(frame) error = %v", err)
	}
}

func TestDialWebSocketRejectsInvalidStatusLine(t *testing.T) {
	t.Parallel()

	reader := bufio.NewReader(strings.NewReader("HTTP/1.1 400 Bad Request\r\n\r\n"))
	if _, err := http.ReadResponse(reader, &http.Request{Method: http.MethodGet}); err != nil {
		t.Fatalf("ReadResponse() should parse simple response, got %v", err)
	}
}
