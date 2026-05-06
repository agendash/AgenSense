package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAICompatibleLLMMergesSystemMessages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("path = %q, want /chat/completions", r.URL.Path)
		}
		var body struct {
			Messages []ChatMessage `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		if len(body.Messages) != 2 {
			t.Fatalf("messages len = %d, want 2: %#v", len(body.Messages), body.Messages)
		}
		if body.Messages[0].Role != "system" || body.Messages[0].Content != "alpha\n\nbeta" {
			t.Fatalf("merged system message = %#v", body.Messages[0])
		}
		if body.Messages[1].Role != "user" || body.Messages[1].Content != "hello" {
			t.Fatalf("user message = %#v", body.Messages[1])
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	client := NewOpenAICompatibleLLM(server.Client(), server.URL, "", "test-llm")
	var got strings.Builder
	err := client.ChatStream(context.Background(), ChatRequest{
		Messages: []ChatMessage{
			{Role: "system", Content: "alpha"},
			{Role: "system", Content: "beta"},
			{Role: "user", Content: "hello"},
		},
	}, func(delta ChatDelta) error {
		got.WriteString(delta.Text)
		return nil
	})
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}
	if got.String() != "ok" {
		t.Fatalf("streamed text = %q, want ok", got.String())
	}
}

func TestOpenAICompatibleLLMReturnsStreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"error\":{\"message\":\"System message must be at the beginning\",\"type\":\"server_error\"}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	client := NewOpenAICompatibleLLM(server.Client(), server.URL, "", "test-llm")
	err := client.ChatStream(context.Background(), ChatRequest{
		Messages: []ChatMessage{{Role: "user", Content: "hello"}},
	}, func(ChatDelta) error {
		t.Fatal("callback should not be called")
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "System message must be at the beginning") {
		t.Fatalf("ChatStream() error = %v, want stream error message", err)
	}
}

func TestOpenAICompatibleLLMDropsDuplicatedOpeningDelta(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":null}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"你好\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"你好\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"，我是 AgenSense\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	client := NewOpenAICompatibleLLM(server.Client(), server.URL, "", "test-llm")
	var got strings.Builder
	err := client.ChatStream(context.Background(), ChatRequest{
		Messages: []ChatMessage{{Role: "user", Content: "hello"}},
	}, func(delta ChatDelta) error {
		got.WriteString(delta.Text)
		return nil
	})
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}
	if got.String() != "你好，我是 AgenSense" {
		t.Fatalf("streamed text = %q, want opening duplicate removed", got.String())
	}
}

func TestOpenAICompatibleTTSOmitsVoiceForQwen3AndUnwrapsWAV(t *testing.T) {
	t.Setenv("AGENSENSE_OPENAI_TTS_SENTENCE_STREAM", "0")

	pcm := bytes.Repeat([]byte{0x01, 0x02}, 1024)
	wav, err := pcmToWAV(AudioFormat{Codec: "pcm_s16le", SampleRateHz: 24000, Channels: 1}, pcm)
	if err != nil {
		t.Fatalf("pcmToWAV() error = %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/audio/speech" {
			t.Fatalf("path = %q, want /audio/speech", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		if _, ok := body["voice"]; ok {
			t.Fatal("qwen3-tts-cpp request unexpectedly included voice")
		}
		w.Header().Set("Content-Type", "audio/wav")
		_, _ = w.Write(wav)
	}))
	defer server.Close()

	client := NewOpenAICompatibleTTS(server.Client(), server.URL, "", "qwen3-tts-cpp")
	var got bytes.Buffer
	var gotFormat AudioFormat
	err = client.SynthesizeStream(context.Background(), TTSRequest{
		Text:   "hello",
		Format: AudioFormat{Codec: "pcm_s16le", SampleRateHz: 16000, Channels: 1},
	}, func(chunk AudioChunk) error {
		got.Write(chunk.Data)
		gotFormat = chunk.Format
		return nil
	})
	if err != nil {
		t.Fatalf("SynthesizeStream() error = %v", err)
	}
	if !bytes.Equal(got.Bytes(), pcm) {
		t.Fatalf("audio bytes were not unwrapped PCM, got %d want %d", got.Len(), len(pcm))
	}
	if gotFormat.Codec != "pcm_s16le" || gotFormat.SampleRateHz != 24000 || gotFormat.Channels != 1 {
		t.Fatalf("format = %+v, want 24k pcm_s16le mono", gotFormat)
	}
}

func TestSplitTTSSegments(t *testing.T) {
	got := splitTTSSegments("你好。Hello world! 下一句", 120)
	want := []string{"你好。", "Hello world!", "下一句"}
	if len(got) != len(want) {
		t.Fatalf("segments len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("segment[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
