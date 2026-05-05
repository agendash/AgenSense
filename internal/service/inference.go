package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"strings"

	"github.com/agendash/agensense/internal/debugtrace"
	"github.com/agendash/agensense/internal/provider"
)

// RuntimeInferenceService exposes direct-use ASR / LLM / TTS APIs.
type RuntimeInferenceService struct {
	registry *RegistryService
	factory  *provider.Factory
	debug    *debugtrace.Store
}

func NewRuntimeInferenceService(registry *RegistryService, factory *provider.Factory, debug *debugtrace.Store) *RuntimeInferenceService {
	return &RuntimeInferenceService{
		registry: registry,
		factory:  factory,
		debug:    debug,
	}
}

func (s *RuntimeInferenceService) Transcribe(ctx context.Context, apiKey string, req ASRInferenceRequest) (ASRInferenceResponse, error) {
	if s.registry == nil || s.factory == nil {
		return ASRInferenceResponse{}, ErrInvalidInput
	}
	audio, err := base64.StdEncoding.DecodeString(strings.TrimSpace(req.AudioBase64))
	if err != nil {
		return ASRInferenceResponse{}, ErrInvalidInput
	}
	handle := s.startTrace(debugtrace.KindASR, req.ProviderProfileID, strings.TrimSpace(req.ClientID), strings.TrimSpace(req.DeviceLabel), strings.TrimSpace(req.HardwareSKU), strings.TrimSpace(req.FirmwareVersion), strings.TrimSpace(req.FirmwareChannel), strings.TrimSpace(req.SessionID), "/v1/asr/transcribe")
	handle.SetInputAudio(req.Format.toProviderFormat(), audio)
	handle.StartASR(req.Format.toProviderFormat(), len(audio))
	profile, err := s.registry.ResolveProviderProfile(ctx, apiKey, req.ProviderProfileID)
	if err != nil {
		handle.Fail(err)
		return ASRInferenceResponse{}, err
	}
	handle.SetProviderProfileID(profile.ID)
	client, err := s.factory.ASRClient(profile)
	if err != nil {
		handle.Fail(err)
		return ASRInferenceResponse{}, err
	}
	resp, err := client.Transcribe(ctx, provider.TranscribeRequest{
		DeviceID:  firstNonEmpty(strings.TrimSpace(req.ClientID), NamespaceFromAPIKey(apiKey)),
		SessionID: strings.TrimSpace(req.SessionID),
		Format:    req.Format.toProviderFormat(),
		Audio:     audio,
	})
	if err != nil {
		handle.Fail(err)
		return ASRInferenceResponse{}, err
	}
	handle.CompleteASR(resp.Text)
	handle.Complete()
	return ASRInferenceResponse{
		ProviderProfileID: profile.ID,
		Text:              resp.Text,
	}, nil
}

func (s *RuntimeInferenceService) Chat(ctx context.Context, apiKey string, req ChatInferenceRequest) (ChatInferenceResponse, error) {
	if s.registry == nil || s.factory == nil {
		return ChatInferenceResponse{}, ErrInvalidInput
	}
	if len(req.Messages) == 0 {
		return ChatInferenceResponse{}, ErrInvalidInput
	}
	voiceAssistant := MergeVoiceAssistantMetadata(req.VoiceAssistant, req.UIContext, req.AssistantIntent, req.Metadata)
	handle := s.startTrace(debugtrace.KindLLM, req.ProviderProfileID, strings.TrimSpace(req.ClientID), strings.TrimSpace(req.DeviceLabel), strings.TrimSpace(req.HardwareSKU), strings.TrimSpace(req.FirmwareVersion), strings.TrimSpace(req.FirmwareChannel), strings.TrimSpace(req.SessionID), "/v1/llm/chat")
	if !voiceAssistant.Empty() {
		handle.AddEvent("voice_assistant.context", compactJSON(voiceAssistant))
	}
	profile, err := s.registry.ResolveProviderProfile(ctx, apiKey, req.ProviderProfileID)
	if err != nil {
		handle.Fail(err)
		return ChatInferenceResponse{}, err
	}
	handle.SetProviderProfileID(profile.ID)
	client, err := s.factory.LLMClient(profile)
	if err != nil {
		handle.Fail(err)
		return ChatInferenceResponse{}, err
	}
	var deltas []string
	var builder strings.Builder
	handle.StartLLM(req.Messages)
	err = client.ChatStream(ctx, provider.ChatRequest{
		DeviceID:  firstNonEmpty(strings.TrimSpace(req.ClientID), NamespaceFromAPIKey(apiKey)),
		SessionID: strings.TrimSpace(req.SessionID),
		Messages:  req.Messages,
	}, func(delta provider.ChatDelta) error {
		deltas = append(deltas, delta.Text)
		builder.WriteString(delta.Text)
		handle.AddLLMDelta(delta.Text)
		return nil
	})
	if err != nil {
		handle.Fail(err)
		return ChatInferenceResponse{}, err
	}
	replyText := strings.TrimSpace(builder.String())
	handle.CompleteLLM(replyText)
	handle.Complete()
	return ChatInferenceResponse{
		ProviderProfileID: profile.ID,
		Text:              replyText,
		Deltas:            deltas,
		AssistantIntent:   voiceAssistant.AssistantIntent,
	}, nil
}

func compactJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(data)
}

func (s *RuntimeInferenceService) Synthesize(ctx context.Context, apiKey string, req TTSInferenceRequest) (TTSInferenceResponse, error) {
	if s.registry == nil || s.factory == nil {
		return TTSInferenceResponse{}, ErrInvalidInput
	}
	if strings.TrimSpace(req.Text) == "" {
		return TTSInferenceResponse{}, ErrInvalidInput
	}
	handle := s.startTrace(debugtrace.KindTTS, req.ProviderProfileID, strings.TrimSpace(req.ClientID), strings.TrimSpace(req.DeviceLabel), strings.TrimSpace(req.HardwareSKU), strings.TrimSpace(req.FirmwareVersion), strings.TrimSpace(req.FirmwareChannel), strings.TrimSpace(req.SessionID), "/v1/tts/synthesize")
	profile, err := s.registry.ResolveProviderProfile(ctx, apiKey, req.ProviderProfileID)
	if err != nil {
		handle.Fail(err)
		return TTSInferenceResponse{}, err
	}
	handle.SetProviderProfileID(profile.ID)
	client, err := s.factory.TTSClient(profile)
	if err != nil {
		handle.Fail(err)
		return TTSInferenceResponse{}, err
	}
	var audio []byte
	chunkCount := 0
	format := req.Format
	if format.Codec == "" {
		format.Codec = "pcm_s16le"
	}
	if format.SampleRateHz <= 0 {
		format.SampleRateHz = 16000
	}
	if format.Channels <= 0 {
		format.Channels = 1
	}
	handle.StartTTS(req.Text, format.toProviderFormat())
	err = client.SynthesizeStream(ctx, provider.TTSRequest{
		DeviceID:  firstNonEmpty(strings.TrimSpace(req.ClientID), NamespaceFromAPIKey(apiKey)),
		SessionID: strings.TrimSpace(req.SessionID),
		Text:      req.Text,
		Format:    format.toProviderFormat(),
	}, func(chunk provider.AudioChunk) error {
		audio = append(audio, chunk.Data...)
		chunkCount++
		handle.AddTTSChunk(chunk.Data)
		return nil
	})
	if err != nil {
		handle.Fail(err)
		return TTSInferenceResponse{}, err
	}
	format = normalizeSynthesisFormat(format, audio)
	handle.CompleteTTS(format.toProviderFormat())
	handle.Complete()
	return TTSInferenceResponse{
		ProviderProfileID: profile.ID,
		Format:            format,
		AudioBase64:       base64.StdEncoding.EncodeToString(audio),
		ChunkCount:        chunkCount,
	}, nil
}

func normalizeSynthesisFormat(format AudioFormatInput, audio []byte) AudioFormatInput {
	if !looksLikeWAV(audio) {
		return format
	}

	format.Codec = "wav"
	if meta, ok := readWAVMetadata(audio); ok {
		if meta.SampleRateHz > 0 {
			format.SampleRateHz = meta.SampleRateHz
		}
		if meta.Channels > 0 {
			format.Channels = meta.Channels
		}
	}
	return format
}

func (s *RuntimeInferenceService) startTrace(kind, providerProfileID, clientID, deviceLabel, hardwareSKU, firmwareVersion, firmwareChannel, sessionID, path string) *debugtrace.Handle {
	if s == nil || s.debug == nil {
		return nil
	}
	return s.debug.StartTrace(kind, debugtrace.SourceHTTP, debugtrace.TraceMeta{
		ClientID:          clientID,
		DeviceLabel:       deviceLabel,
		HardwareSKU:       hardwareSKU,
		FirmwareVersion:   firmwareVersion,
		FirmwareChannel:   firmwareChannel,
		SessionID:         sessionID,
		ProviderProfileID: providerProfileID,
		HTTPPath:          path,
	})
}

type wavMetadata struct {
	SampleRateHz int
	Channels     int
}

func looksLikeWAV(audio []byte) bool {
	return len(audio) >= 12 &&
		bytes.Equal(audio[:4], []byte("RIFF")) &&
		bytes.Equal(audio[8:12], []byte("WAVE"))
}

func readWAVMetadata(audio []byte) (wavMetadata, bool) {
	if len(audio) < 44 || !looksLikeWAV(audio) {
		return wavMetadata{}, false
	}

	for offset := 12; offset+8 <= len(audio); {
		chunkID := audio[offset : offset+4]
		chunkSize := int(binary.LittleEndian.Uint32(audio[offset+4 : offset+8]))
		chunkStart := offset + 8
		chunkEnd := chunkStart + chunkSize
		if chunkEnd > len(audio) {
			return wavMetadata{}, false
		}

		if bytes.Equal(chunkID, []byte("fmt ")) && chunkSize >= 16 {
			return wavMetadata{
				Channels:     int(binary.LittleEndian.Uint16(audio[chunkStart+2 : chunkStart+4])),
				SampleRateHz: int(binary.LittleEndian.Uint32(audio[chunkStart+4 : chunkStart+8])),
			}, true
		}

		offset = chunkEnd
		if offset%2 != 0 {
			offset++
		}
	}

	return wavMetadata{}, false
}
