package voicews

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/agendash/AgenSense/internal/debugtrace"
	"github.com/agendash/AgenSense/internal/httpapi"
	"github.com/agendash/AgenSense/internal/protocol"
	"github.com/agendash/AgenSense/internal/provider"
	"github.com/agendash/AgenSense/internal/service"
	"github.com/agendash/AgenSense/internal/store"
)

func TestVoiceWebSocketRoundTrip(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	repo, err := store.NewFileRepository(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatalf("NewFileRepository() error = %v", err)
	}

	registry := service.NewRegistryService(repo)
	if _, err := registry.UpsertProviderProfile(context.Background(), "voice-test-key", service.ProviderProfileRequest{
		ID:         "default",
		Name:       "Mock Voice",
		ASRBaseURL: "mock://asr",
		LLMBaseURL: "mock://llm",
		TTSBaseURL: "mock://tts",
		Default:    true,
	}); err != nil {
		t.Fatalf("UpsertProviderProfile() error = %v", err)
	}

	debugStore := debugtrace.NewStoreWithAssetDir(8, filepath.Join(dir, "debug-traces"))
	handler := NewHandler(registry, provider.NewFactory(nil), debugStore)
	server := httptest.NewServer(httpapi.NewRouter(nil, registry, nil, nil, handler, debugStore))
	defer server.Close()

	conn, reader := dialWebSocket(t, server.URL+"/v1/voice/ws", "voice-test-key")
	defer conn.Close()

	writeClientTextEvent(t, conn, eventSessionUpdate, map[string]any{
		"client_id":           "agendash-desktop",
		"device_label":        "MacOS",
		"session_id":          "voice-session-001",
		"provider_profile_id": "default",
		"response_language":   "zh-Hans",
		"auto_response":       true,
		"voice_assistant": map[string]any{
			"ui_context": map[string]any{
				"current_scene": "chat",
				"focused_object": map[string]any{
					"id":    "alpha",
					"kind":  "agent_session",
					"label": "alpha",
				},
			},
			"assistant_intent": map[string]any{
				"scope":                 "focused_object",
				"target_id":             "alpha",
				"action":                "local_reply",
				"requires_confirmation": false,
				"ui_surface":            "toast",
			},
			"metadata": map[string]any{
				"available_mcp_tools": []any{
					"joyce.capture_text",
					"joyce.create_reminder_candidate",
				},
			},
		},
		"format": map[string]any{
			"codec":          "pcm_s16le",
			"sample_rate_hz": 16000,
			"channels":       1,
		},
	})

	ready := readTextEvent(t, reader)
	if ready.Type != eventSessionReady {
		t.Fatalf("first event = %q, want %q", ready.Type, eventSessionReady)
	}
	var readyPayload sessionReadyPayload
	if err := ready.DecodePayload(&readyPayload); err != nil {
		t.Fatalf("DecodePayload(session.ready) error = %v", err)
	}
	if readyPayload.DeviceLabel != "MacOS" {
		t.Fatalf("session.ready device label = %q, want MacOS", readyPayload.DeviceLabel)
	}
	if readyPayload.ResponseLanguage != "zh-Hans" {
		t.Fatalf("session.ready response language = %q, want zh-Hans", readyPayload.ResponseLanguage)
	}
	if readyPayload.VoiceAssistant == nil || readyPayload.VoiceAssistant.AssistantIntent == nil {
		t.Fatal("expected session.ready to echo voice assistant metadata")
	}
	if readyPayload.VoiceAssistant.AssistantIntent.TargetID != "alpha" {
		t.Fatalf("session.ready target = %q, want alpha", readyPayload.VoiceAssistant.AssistantIntent.TargetID)
	}

	writeClientTextEvent(t, conn, protocol.EventAudioStart, map[string]any{
		"stream_id":      "in-001",
		"codec":          "pcm_s16le",
		"sample_rate_hz": 16000,
		"channels":       1,
	})
	writeClientFrame(t, conn, 0x2, bytes.Repeat([]byte{0x01, 0x02}, 8000))
	writeClientTextEvent(t, conn, protocol.EventAudioStop, map[string]any{
		"stream_id": "in-001",
		"last_seq":  1,
	})

	var final protocol.Envelope
	deadline := time.Now().Add(5 * time.Second)
	if err := conn.SetReadDeadline(deadline); err != nil {
		t.Fatalf("SetReadDeadline() error = %v", err)
	}
	for {
		event := readTextEvent(t, reader)
		if event.Type == protocol.EventASRFinal {
			final = event
			break
		}
	}

	var asr protocol.ASRFinalPayload
	if err := final.DecodePayload(&asr); err != nil {
		t.Fatalf("DecodePayload(asr.final) error = %v", err)
	}
	if asr.Text == "" {
		t.Fatal("expected asr.final text")
	}

	seen := map[string]bool{}
	binaryFrames := 0
	var mcpProposal protocol.MCPCallProposedPayload
	for !(seen[protocol.EventMCPCallProposed] && seen[protocol.EventLLMDone] && seen[protocol.EventTTSStart] && seen[protocol.EventTTSStop] && seen[eventResponseDone] && binaryFrames > 0) {
		opcode, payload := readServerFrame(t, reader)
		switch opcode {
		case 0x1:
			event, err := protocol.DecodeEvent(payload)
			if err != nil {
				t.Fatalf("DecodeEvent(server) error = %v", err)
			}
			seen[event.Type] = true
			if event.Type == protocol.EventMCPCallProposed {
				if err := event.DecodePayload(&mcpProposal); err != nil {
					t.Fatalf("DecodePayload(mcp.call.proposed) error = %v", err)
				}
			}
		case 0x2:
			binaryFrames++
		default:
			t.Fatalf("unexpected opcode %d", opcode)
		}
	}
	if mcpProposal.ProposalID == "" {
		t.Fatal("expected mcp.call.proposed proposal id")
	}
	if mcpProposal.ToolName != "joyce.capture_text" {
		t.Fatalf("mcp tool = %q, want joyce.capture_text", mcpProposal.ToolName)
	}
	if mcpProposal.Arguments["raw_text"] == "" {
		t.Fatalf("expected raw_text argument in mcp proposal: %#v", mcpProposal.Arguments)
	}

	traces := debugStore.List()
	if len(traces) != 1 {
		t.Fatalf("debug traces len = %d, want 1", len(traces))
	}
	trace := traces[0]
	if trace.Kind != debugtrace.KindVoiceTurn || trace.Source != debugtrace.SourceWS {
		t.Fatalf("trace kind/source = %s/%s, want %s/%s", trace.Kind, trace.Source, debugtrace.KindVoiceTurn, debugtrace.SourceWS)
	}
	if trace.ClientID != "agendash-desktop" || trace.HTTPPath != "/v1/voice/ws" {
		t.Fatalf("trace client/path = %q/%q", trace.ClientID, trace.HTTPPath)
	}
	if trace.DeviceLabel != "MacOS" {
		t.Fatalf("trace device label = %q, want MacOS", trace.DeviceLabel)
	}
	if trace.InputAudio == nil {
		t.Fatal("expected input audio asset")
	}
	if len(trace.Segments) != 1 {
		t.Fatalf("trace segments len = %d, want 1", len(trace.Segments))
	}
	if trace.Segments[0].Audio == nil {
		t.Fatal("expected segment audio asset")
	}
	if trace.ASR == nil || trace.ASR.Text == "" {
		t.Fatal("expected ASR stage text")
	}
	if trace.LLM == nil || trace.LLM.ResponseText == "" {
		t.Fatal("expected LLM stage response")
	}
	if trace.TTS == nil || trace.TTS.Audio == nil || trace.TTS.ChunkCount == 0 {
		t.Fatal("expected TTS stage audio")
	}
	if !traceHasEvent(trace, "voice_assistant.context") {
		t.Fatal("expected voice assistant context event in trace")
	}
	if !traceHasEvent(trace, "response.language") {
		t.Fatal("expected response language event in trace")
	}
	if _, _, ok := debugStore.ReadAsset(trace.ID, "input.wav"); !ok {
		t.Fatal("expected readable input.wav")
	}
	if _, _, ok := debugStore.ReadAsset(trace.ID, "segment-001.wav"); !ok {
		t.Fatal("expected readable segment-001.wav")
	}
}

func traceHasEvent(trace debugtrace.Trace, name string) bool {
	for _, event := range trace.Timeline {
		if event.Name == name {
			return true
		}
	}
	return false
}

func dialWebSocket(t *testing.T, rawURL, apiKey string) (net.Conn, *bufio.Reader) {
	t.Helper()

	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("Parse(rawURL) error = %v", err)
	}

	conn, err := net.Dial("tcp", parsed.Host)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}

	secKey := "dGhlIHNhbXBsZSBub25jZQ=="
	req := fmt.Sprintf(
		"GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: %s\r\nAuthorization: Bearer %s\r\n\r\n",
		parsed.RequestURI(),
		parsed.Host,
		secKey,
		apiKey,
	)
	if _, err := io.WriteString(conn, req); err != nil {
		t.Fatalf("WriteString(handshake) error = %v", err)
	}

	reader := bufio.NewReader(conn)
	status, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("ReadString(status) error = %v", err)
	}
	if !bytes.Contains([]byte(status), []byte("101")) {
		t.Fatalf("unexpected handshake status: %s", status)
	}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("ReadString(header) error = %v", err)
		}
		if line == "\r\n" {
			break
		}
	}
	return conn, reader
}

func writeClientTextEvent(t *testing.T, conn net.Conn, eventType string, payload map[string]any) {
	t.Helper()

	event, err := protocol.NewEvent(eventType, "", "", payload)
	if err != nil {
		t.Fatalf("NewEvent(%s) error = %v", eventType, err)
	}
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("Marshal(%s) error = %v", eventType, err)
	}
	writeClientFrame(t, conn, 0x1, data)
}

func writeClientFrame(t *testing.T, conn net.Conn, opcode byte, payload []byte) {
	t.Helper()

	mask := [4]byte{0x12, 0x34, 0x56, 0x78}
	header := []byte{0x80 | opcode}
	switch {
	case len(payload) < 126:
		header = append(header, 0x80|byte(len(payload)))
	case len(payload) <= 0xFFFF:
		header = append(header, 0x80|126, byte(len(payload)>>8), byte(len(payload)))
	default:
		t.Fatalf("payload too large")
	}

	masked := make([]byte, len(payload))
	for i := range payload {
		masked[i] = payload[i] ^ mask[i%4]
	}

	frame := append(header, mask[:]...)
	frame = append(frame, masked...)
	if _, err := conn.Write(frame); err != nil {
		t.Fatalf("conn.Write(frame) error = %v", err)
	}
}

func readTextEvent(t *testing.T, reader *bufio.Reader) protocol.Envelope {
	t.Helper()

	for {
		opcode, payload := readServerFrame(t, reader)
		if opcode != 0x1 {
			continue
		}
		event, err := protocol.DecodeEvent(payload)
		if err != nil {
			t.Fatalf("DecodeEvent() error = %v", err)
		}
		return event
	}
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
		b1, err := reader.ReadByte()
		if err != nil {
			t.Fatalf("ReadByte(length-high) error = %v", err)
		}
		b2, err := reader.ReadByte()
		if err != nil {
			t.Fatalf("ReadByte(length-low) error = %v", err)
		}
		length = int(b1)<<8 | int(b2)
	case 127:
		t.Fatal("unexpected 64-bit length frame")
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(reader, data); err != nil {
		t.Fatalf("ReadFull(payload) error = %v", err)
	}
	return opcode, data
}
