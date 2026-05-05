package service

import (
	"context"

	"github.com/agendash/AgenSense/internal/provider"
)

// ProviderProfileRequest is the user-facing provider profile payload.
type ProviderProfileRequest struct {
	ID         string `json:"id"`
	Name       string `json:"name,omitempty"`
	ASRBaseURL string `json:"asr_base_url,omitempty"`
	ASRAPIKey  string `json:"asr_api_key,omitempty"`
	ASRModel   string `json:"asr_model,omitempty"`
	LLMBaseURL string `json:"llm_base_url,omitempty"`
	LLMAPIKey  string `json:"llm_api_key,omitempty"`
	LLMModel   string `json:"llm_model,omitempty"`
	TTSBaseURL string `json:"tts_base_url,omitempty"`
	TTSAPIKey  string `json:"tts_api_key,omitempty"`
	TTSModel   string `json:"tts_model,omitempty"`
	VADBaseURL string `json:"vad_base_url,omitempty"`
	VADAPIKey  string `json:"vad_api_key,omitempty"`
	VADModel   string `json:"vad_model,omitempty"`
	Default    bool   `json:"default,omitempty"`
}

// ProviderProfileResponse is the stored provider profile returned to clients.
type ProviderProfileResponse struct {
	ID         string `json:"id"`
	Name       string `json:"name,omitempty"`
	Namespace  string `json:"namespace"`
	ASRBaseURL string `json:"asr_base_url,omitempty"`
	ASRModel   string `json:"asr_model,omitempty"`
	LLMBaseURL string `json:"llm_base_url,omitempty"`
	LLMModel   string `json:"llm_model,omitempty"`
	TTSBaseURL string `json:"tts_base_url,omitempty"`
	TTSModel   string `json:"tts_model,omitempty"`
	VADBaseURL string `json:"vad_base_url,omitempty"`
	VADModel   string `json:"vad_model,omitempty"`
	Default    bool   `json:"default,omitempty"`
}

type AudioFormatInput struct {
	Codec        string `json:"codec"`
	SampleRateHz int    `json:"sample_rate_hz"`
	Channels     int    `json:"channels"`
}

// VoiceAssistantIntent mirrors the Agendash Universal Voice Layer action shape.
// It is optional metadata for clients that want Agensense to preserve UI scope.
type VoiceAssistantIntent struct {
	Scope                string         `json:"scope,omitempty"`
	TargetID             string         `json:"target_id,omitempty"`
	Action               string         `json:"action,omitempty"`
	Args                 map[string]any `json:"args,omitempty"`
	RequiresConfirmation bool           `json:"requires_confirmation,omitempty"`
	UISurface            string         `json:"ui_surface,omitempty"`
	Label                string         `json:"label,omitempty"`
	Reason               string         `json:"reason,omitempty"`
	Confidence           float64        `json:"confidence,omitempty"`
}

// VoiceAssistantMetadata carries UI context without forcing provider-specific prompt behavior.
type VoiceAssistantMetadata struct {
	Contract        string                `json:"contract,omitempty"`
	UIContext       map[string]any        `json:"ui_context,omitempty"`
	AssistantIntent *VoiceAssistantIntent `json:"assistant_intent,omitempty"`
	Metadata        map[string]any        `json:"metadata,omitempty"`
}

// MergeVoiceAssistantMetadata folds legacy top-level fields into the nested
// voice_assistant envelope while keeping every field optional for old clients.
func MergeVoiceAssistantMetadata(base VoiceAssistantMetadata, uiContext map[string]any, intent *VoiceAssistantIntent, metadata map[string]any) VoiceAssistantMetadata {
	out := base
	if len(uiContext) > 0 {
		out.UIContext = uiContext
	}
	if intent != nil {
		out.AssistantIntent = intent
	}
	if len(metadata) > 0 {
		out.Metadata = metadata
	}
	if out.Contract == "" && !out.Empty() {
		out.Contract = "universal_voice_layer_v1"
	}
	return out
}

// Empty reports whether no Voice Assistant metadata was provided.
func (m VoiceAssistantMetadata) Empty() bool {
	return m.Contract == "" && len(m.UIContext) == 0 && m.AssistantIntent == nil && len(m.Metadata) == 0
}

func (f AudioFormatInput) toProviderFormat() provider.AudioFormat {
	return provider.AudioFormat{
		Codec:        f.Codec,
		SampleRateHz: f.SampleRateHz,
		Channels:     f.Channels,
	}
}

// ASRInferenceRequest runs one audio-to-text request.
type ASRInferenceRequest struct {
	ProviderProfileID string           `json:"provider_profile_id,omitempty"`
	ClientID          string           `json:"client_id,omitempty"`
	DeviceLabel       string           `json:"device_label,omitempty"`
	HardwareSKU       string           `json:"hardware_sku,omitempty"`
	FirmwareVersion   string           `json:"firmware_version,omitempty"`
	FirmwareChannel   string           `json:"firmware_channel,omitempty"`
	SessionID         string           `json:"session_id,omitempty"`
	Format            AudioFormatInput `json:"format"`
	AudioBase64       string           `json:"audio_base64"`
}

type ASRInferenceResponse struct {
	ProviderProfileID string `json:"provider_profile_id"`
	Text              string `json:"text"`
}

// ChatInferenceRequest runs one text generation request.
type ChatInferenceRequest struct {
	ProviderProfileID string                 `json:"provider_profile_id,omitempty"`
	ClientID          string                 `json:"client_id,omitempty"`
	DeviceLabel       string                 `json:"device_label,omitempty"`
	HardwareSKU       string                 `json:"hardware_sku,omitempty"`
	FirmwareVersion   string                 `json:"firmware_version,omitempty"`
	FirmwareChannel   string                 `json:"firmware_channel,omitempty"`
	SessionID         string                 `json:"session_id,omitempty"`
	Messages          []provider.ChatMessage `json:"messages"`
	VoiceAssistant    VoiceAssistantMetadata `json:"voice_assistant,omitempty"`
	UIContext         map[string]any         `json:"ui_context,omitempty"`
	AssistantIntent   *VoiceAssistantIntent  `json:"assistant_intent,omitempty"`
	Metadata          map[string]any         `json:"metadata,omitempty"`
}

type ChatInferenceResponse struct {
	ProviderProfileID string                `json:"provider_profile_id"`
	Text              string                `json:"text"`
	Deltas            []string              `json:"deltas,omitempty"`
	AssistantIntent   *VoiceAssistantIntent `json:"assistant_intent,omitempty"`
}

// TTSInferenceRequest runs one text-to-speech request.
type TTSInferenceRequest struct {
	ProviderProfileID string           `json:"provider_profile_id,omitempty"`
	ClientID          string           `json:"client_id,omitempty"`
	DeviceLabel       string           `json:"device_label,omitempty"`
	HardwareSKU       string           `json:"hardware_sku,omitempty"`
	FirmwareVersion   string           `json:"firmware_version,omitempty"`
	FirmwareChannel   string           `json:"firmware_channel,omitempty"`
	SessionID         string           `json:"session_id,omitempty"`
	Text              string           `json:"text"`
	Format            AudioFormatInput `json:"format"`
}

type TTSInferenceResponse struct {
	ProviderProfileID string           `json:"provider_profile_id"`
	Format            AudioFormatInput `json:"format"`
	AudioBase64       string           `json:"audio_base64"`
	ChunkCount        int              `json:"chunk_count"`
}

// ProviderRegistry exposes provider profile management through API-key namespaces.
type ProviderRegistry interface {
	UpsertProviderProfile(ctx context.Context, apiKey string, req ProviderProfileRequest) (ProviderProfileResponse, error)
	ListProviderProfiles(ctx context.Context, apiKey string) ([]ProviderProfileResponse, error)
	GetProviderProfile(ctx context.Context, apiKey, profileID string) (ProviderProfileResponse, error)
}

// InferenceService exposes direct-use ASR/LLM/TTS APIs for non-device clients.
type InferenceService interface {
	Transcribe(ctx context.Context, apiKey string, req ASRInferenceRequest) (ASRInferenceResponse, error)
	Chat(ctx context.Context, apiKey string, req ChatInferenceRequest) (ChatInferenceResponse, error)
	Synthesize(ctx context.Context, apiKey string, req TTSInferenceRequest) (TTSInferenceResponse, error)
}
