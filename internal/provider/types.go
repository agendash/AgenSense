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
	Role    string `json:"role"`
	Content string `json:"content"`
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

// MultimodalContent is one provider-agnostic multimodal content part.
type MultimodalContent struct {
	Type     string
	Text     string
	ImageURL string
	Data     []byte
	MIMEType string
	Name     string
}

// MultimodalMessage is a chat-style message with text and image content parts.
type MultimodalMessage struct {
	Role    string
	Content []MultimodalContent
}

// MultimodalRequest is the provider-agnostic multimodal generation input.
type MultimodalRequest struct {
	DeviceID  string
	SessionID string
	Messages  []MultimodalMessage
}

// MultimodalResponse is the provider-agnostic multimodal generation output.
type MultimodalResponse struct {
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

// MultimodalClient completes chat-style requests that include images.
type MultimodalClient interface {
	Complete(ctx context.Context, req MultimodalRequest) (MultimodalResponse, error)
}

// TTSClient streams synthesized audio chunks.
type TTSClient interface {
	SynthesizeStream(ctx context.Context, req TTSRequest, cb func(AudioChunk) error) error
}
