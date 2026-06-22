package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
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
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		var body struct {
			Messages []ChatMessage `json:"messages"`
		}
		if err := json.NewDecoder(bytes.NewReader(raw)).Decode(&body); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		var rawBody struct {
			Messages []map[string]any `json:"messages"`
		}
		if err := json.NewDecoder(bytes.NewReader(raw)).Decode(&rawBody); err != nil {
			t.Fatalf("Decode raw body error = %v", err)
		}
		if _, ok := rawBody.Messages[0]["role"]; !ok {
			t.Fatalf("message json key role missing: %#v", rawBody.Messages[0])
		}
		if _, ok := rawBody.Messages[0]["Role"]; ok {
			t.Fatalf("message json has uppercase Role key: %#v", rawBody.Messages[0])
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

func TestOpenAICompatibleLLMSendsReasoningEffort(t *testing.T) {
	t.Setenv("AGENSENSE_OPENAI_REASONING_EFFORT", "none")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ReasoningEffort string `json:"reasoning_effort"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		if body.ReasoningEffort != "none" {
			t.Fatalf("reasoning_effort = %q, want none", body.ReasoningEffort)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n"))
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

func TestOpenAICompatibleMultimodalBuildsImageRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("path = %q, want /chat/completions", r.URL.Path)
		}
		var body struct {
			Model    string `json:"model"`
			Stream   bool   `json:"stream"`
			Messages []struct {
				Role    string `json:"role"`
				Content []struct {
					Type     string `json:"type"`
					Text     string `json:"text"`
					ImageURL struct {
						URL string `json:"url"`
					} `json:"image_url"`
				} `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		if body.Model != "test-vision" || body.Stream {
			t.Fatalf("model/stream = %q/%v, want test-vision/false", body.Model, body.Stream)
		}
		if len(body.Messages) != 1 || len(body.Messages[0].Content) != 2 {
			t.Fatalf("messages = %#v", body.Messages)
		}
		if body.Messages[0].Content[0].Type != "text" || body.Messages[0].Content[0].Text != "what is this" {
			t.Fatalf("text part = %#v", body.Messages[0].Content[0])
		}
		imageURL := body.Messages[0].Content[1].ImageURL.URL
		if !strings.HasPrefix(imageURL, "data:image/png;base64,") {
			t.Fatalf("image url = %q, want image data url", imageURL)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"it is a test image"}}]}`))
	}))
	defer server.Close()

	client := NewOpenAICompatibleMultimodal(server.Client(), server.URL, "", "test-vision")
	got, err := client.Complete(context.Background(), MultimodalRequest{
		Messages: []MultimodalMessage{{
			Role: "user",
			Content: []MultimodalContent{
				{Type: "text", Text: "what is this"},
				{Type: "image", Data: []byte{1, 2, 3}, MIMEType: "image/png"},
			},
		}},
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if got.Text != "it is a test image" {
		t.Fatalf("text = %q, want test reply", got.Text)
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

func TestOpenAICompatibleTTSStreamAllowlist(t *testing.T) {
	t.Setenv("AGENSENSE_OPENAI_TTS_SENTENCE_STREAM", "0")
	t.Setenv("AGENSENSE_OPENAI_TTS_STREAM", "1")
	t.Setenv("AGENSENSE_OPENAI_TTS_STREAM_BASE_URLS", "http://127.0.0.1:18082/v1")

	pcm := bytes.Repeat([]byte{0x01, 0x02}, 16)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		if _, ok := body["stream"]; ok {
			t.Fatal("request unexpectedly included stream for non-allowlisted base URL")
		}
		_, _ = w.Write(pcm)
	}))
	defer server.Close()

	client := NewOpenAICompatibleTTS(server.Client(), server.URL, "", "test-tts")
	err := client.SynthesizeStream(context.Background(), TTSRequest{
		Text:   "hello",
		Format: AudioFormat{Codec: "pcm_s16le", SampleRateHz: 16000, Channels: 1},
	}, func(AudioChunk) error {
		return nil
	})
	if err != nil {
		t.Fatalf("SynthesizeStream() error = %v", err)
	}
}

func TestOpenAICompatibleTTSStreamAllowlistMatch(t *testing.T) {
	t.Setenv("AGENSENSE_OPENAI_TTS_SENTENCE_STREAM", "0")
	t.Setenv("AGENSENSE_OPENAI_TTS_STREAM", "1")

	pcm := bytes.Repeat([]byte{0x01, 0x02}, 16)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		if body["stream"] != true {
			t.Fatalf("stream = %#v, want true", body["stream"])
		}
		_, _ = w.Write(pcm)
	}))
	defer server.Close()
	t.Setenv("AGENSENSE_OPENAI_TTS_STREAM_BASE_URLS", server.URL)

	client := NewOpenAICompatibleTTS(server.Client(), server.URL, "", "test-tts")
	err := client.SynthesizeStream(context.Background(), TTSRequest{
		Text:   "hello",
		Format: AudioFormat{Codec: "pcm_s16le", SampleRateHz: 16000, Channels: 1},
	}, func(AudioChunk) error {
		return nil
	})
	if err != nil {
		t.Fatalf("SynthesizeStream() error = %v", err)
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
