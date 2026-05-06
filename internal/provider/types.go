package provider

import "context"

// AudioFormat describes raw streamed audio exchanged with devices or providers.
type AudioFormat struct {
	Codec        string
	SampleRateHz int
	Channels     int
}

// TranscribeRequest is the provider-agnostic ASR input.
type TranscribeRequest struct {
	DeviceID  string
	SessionID string
	Format    AudioFormat
	Audio     []byte
}

// TranscribeResponse is the provider-agnostic ASR output.
type TranscribeResponse struct {
	Text string
}

// ChatMessage is the provider-agnostic chat message shape.
type ChatMessage struct {
	Role    string
	Content string
}

// ChatRequest is the provider-agnostic LLM input.
type ChatRequest struct {
	DeviceID  string
	SessionID string
	Messages  []ChatMessage
}

// ChatDelta is one streamed text chunk emitted by an LLM provider.
type ChatDelta struct {
	Text string
}

// TTSRequest is the provider-agnostic speech synthesis input.
type TTSRequest struct {
	DeviceID  string
	SessionID string
	Text      string
	Format    AudioFormat
}

// AudioChunk is one streamed audio chunk emitted by a TTS provider.
type AudioChunk struct {
	Data   []byte
	Format AudioFormat
}

// ASRClient transcribes audio into text.
type ASRClient interface {
	Transcribe(ctx context.Context, req TranscribeRequest) (TranscribeResponse, error)
}

// LLMClient streams text deltas for chat output.
type LLMClient interface {
	ChatStream(ctx context.Context, req ChatRequest, cb func(ChatDelta) error) error
}

// TTSClient streams synthesized audio chunks.
type TTSClient interface {
	SynthesizeStream(ctx context.Context, req TTSRequest, cb func(AudioChunk) error) error
}
