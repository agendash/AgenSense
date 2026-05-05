package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"testing"
)

func TestVoiceWSURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		base string
		want string
	}{
		{name: "http", base: "http://127.0.0.1:8080", want: "ws://127.0.0.1:8080/v1/voice/ws"},
		{name: "https", base: "https://sense.example.test/root/", want: "wss://sense.example.test/root/v1/voice/ws"},
		{name: "ws", base: "ws://localhost:8080", want: "ws://localhost:8080/v1/voice/ws"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := voiceWSURL(tt.base)
			if err != nil {
				t.Fatalf("voiceWSURL() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("voiceWSURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSyntheticAudioContainsSilenceAndSpeech(t *testing.T) {
	t.Parallel()

	audio := synthesizeInputPCM(16000, 1, 200, 500, 200)
	wantBytes := 16000 * 1 * 2 * 900 / 1000
	if len(audio) != wantBytes {
		t.Fatalf("audio bytes = %d, want %d", len(audio), wantBytes)
	}

	leading := audio[:16000*2*200/1000]
	if !bytes.Equal(leading, make([]byte, len(leading))) {
		t.Fatal("leading silence contains non-zero samples")
	}

	speech := audio[len(leading) : len(leading)+16000*2*500/1000]
	if bytes.Equal(speech, make([]byte, len(speech))) {
		t.Fatal("speech section is all zero")
	}
}

func TestWAVBytesWrapsPCM(t *testing.T) {
	t.Parallel()

	pcm := []byte{1, 0, 2, 0, 3, 0, 4, 0}
	wav := wavBytes(pcm, 16000, 1)
	if len(wav) != 44+len(pcm) {
		t.Fatalf("wav len = %d, want %d", len(wav), 44+len(pcm))
	}
	if string(wav[:4]) != "RIFF" || string(wav[8:12]) != "WAVE" || string(wav[36:40]) != "data" {
		t.Fatalf("invalid wav header: %q %q %q", wav[:4], wav[8:12], wav[36:40])
	}
	if got := int(binary.LittleEndian.Uint32(wav[40:44])); got != len(pcm) {
		t.Fatalf("data size = %d, want %d", got, len(pcm))
	}
	if !bytes.Equal(wav[44:], pcm) {
		t.Fatal("wav payload does not match pcm")
	}

	decoded, sampleRate, channels, err := decodePCMFromWAV(wav)
	if err != nil {
		t.Fatalf("decodePCMFromWAV() error = %v", err)
	}
	if sampleRate != 16000 || channels != 1 {
		t.Fatalf("metadata = %d/%d, want 16000/1", sampleRate, channels)
	}
	if !bytes.Equal(decoded, pcm) {
		t.Fatal("decoded pcm does not match input")
	}
}

func TestSplitChunks(t *testing.T) {
	t.Parallel()

	chunks := splitChunks([]byte{1, 2, 3, 4, 5}, 2)
	if len(chunks) != 3 {
		t.Fatalf("chunks len = %d, want 3", len(chunks))
	}
	if !bytes.Equal(chunks[0], []byte{1, 2}) || !bytes.Equal(chunks[1], []byte{3, 4}) || !bytes.Equal(chunks[2], []byte{5}) {
		t.Fatalf("unexpected chunks: %#v", chunks)
	}
}

func TestAgenleashPrompt(t *testing.T) {
	t.Parallel()

	got := agenleashPrompt(config{}, "please inspect the workspace")
	if !bytes.Contains([]byte(got), []byte("please inspect the workspace")) {
		t.Fatalf("prompt did not include ASR text: %q", got)
	}
	custom := agenleashPrompt(config{AgenleashMessage: "custom"}, "ignored")
	if custom != "custom" {
		t.Fatalf("custom prompt = %q, want custom", custom)
	}
}

func TestVADStateAndCompactEvents(t *testing.T) {
	t.Parallel()

	started, _ := json.Marshal(map[string]any{"state": "speech_started"})
	events := []eventRecord{
		{Direction: "server", Type: eventSessionReady},
		{Direction: "server", Type: eventVADState, Payload: started},
		{Direction: "server", Type: "tts.binary"},
		{Direction: "server", Type: "tts.binary"},
		{Direction: "client", Type: eventResponseCreate},
		{Direction: "server", Type: eventResponseDone},
	}
	if !hasVADState(events, "speech_started") {
		t.Fatal("expected speech_started VAD state")
	}
	got := compactServerEventTypes(events)
	want := []string{eventSessionReady, eventVADState, "tts.binary x2", eventResponseDone}
	if len(got) != len(want) {
		t.Fatalf("compact len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("compact[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
