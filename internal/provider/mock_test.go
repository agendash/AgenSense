package provider

import (
	"context"
	"testing"
)

func TestMockASRTranscribe(t *testing.T) {
	t.Parallel()

	got, err := MockASR{}.Transcribe(context.Background(), TranscribeRequest{
		Format: AudioFormat{Codec: "pcm_s16le", SampleRateHz: 16000, Channels: 1},
		Audio:  make([]byte, 32000),
	})
	if err != nil {
		t.Fatalf("Transcribe() error = %v", err)
	}
	if got.Text == "" {
		t.Fatal("Transcribe() returned empty text")
	}
}

func TestMockLLMChatStream(t *testing.T) {
	t.Parallel()

	var chunks []string
	err := MockLLM{}.ChatStream(context.Background(), ChatRequest{
		Messages: []ChatMessage{{Role: "user", Content: "test transcript"}},
	}, func(delta ChatDelta) error {
		chunks = append(chunks, delta.Text)
		return nil
	})
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("ChatStream() produced no chunks")
	}
}

func TestMockMultimodalComplete(t *testing.T) {
	t.Parallel()

	got, err := MockMultimodal{}.Complete(context.Background(), MultimodalRequest{
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
	if got.Text == "" {
		t.Fatal("Complete() returned empty text")
	}
}

func TestMockTTSSynthesizeStream(t *testing.T) {
	t.Parallel()

	var total int
	err := MockTTS{}.SynthesizeStream(context.Background(), TTSRequest{
		Text:   "hello mock gateway",
		Format: AudioFormat{Codec: "pcm_s16le", SampleRateHz: 16000, Channels: 1},
	}, func(chunk AudioChunk) error {
		total += len(chunk.Data)
		return nil
	})
	if err != nil {
		t.Fatalf("SynthesizeStream() error = %v", err)
	}
	if total == 0 {
		t.Fatal("SynthesizeStream() produced no audio")
	}
}
