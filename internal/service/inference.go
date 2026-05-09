package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"strings"

	"github.com/agendash/AgenSense/internal/debugtrace"
	"github.com/agendash/AgenSense/internal/provider"
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
	return s.ChatStream(ctx, apiKey, req, nil)
}

func (s *RuntimeInferenceService) ChatStream(ctx context.Context, apiKey string, req ChatInferenceRequest, cb ChatDeltaCallback) (ChatInferenceResponse, error) {
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
		if cb != nil {
			return cb(delta.Text)
		}
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

func (s *RuntimeInferenceService) CompleteMultimodal(ctx context.Context, apiKey string, req MultimodalInferenceRequest) (MultimodalInferenceResponse, error) {
	resp, _, err := s.completeMultimodal(ctx, apiKey, req, "/v1/multimodal/chat")
	return resp, err
}

func (s *RuntimeInferenceService) AnalyzeVision(ctx context.Context, apiKey string, req VisionInferenceRequest) (VisionInferenceResponse, error) {
	if len(req.Images) == 0 {
		return VisionInferenceResponse{}, ErrInvalidInput
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		prompt = "Describe the image."
	}

	content := []MultimodalContentInput{{
		Type: "text",
		Text: prompt,
	}}
	for _, image := range req.Images {
		content = append(content, MultimodalContentInput{
			Type:        "image_url",
			ImageBase64: image.ImageBase64,
			URL:         firstNonEmpty(strings.TrimSpace(image.ImageURL), strings.TrimSpace(image.URL)),
			MIMEType:    image.MIMEType,
			Name:        image.Name,
		})
	}
	multimodalResp, imageCount, err := s.completeMultimodal(ctx, apiKey, MultimodalInferenceRequest{
		ProviderProfileID: req.ProviderProfileID,
		ClientID:          req.ClientID,
		DeviceLabel:       req.DeviceLabel,
		HardwareSKU:       req.HardwareSKU,
		FirmwareVersion:   req.FirmwareVersion,
		FirmwareChannel:   req.FirmwareChannel,
		SessionID:         req.SessionID,
		Messages: []MultimodalMessageInput{{
			Role:    "user",
			Content: content,
		}},
		VoiceAssistant:  req.VoiceAssistant,
		UIContext:       req.UIContext,
		AssistantIntent: req.AssistantIntent,
		Metadata:        req.Metadata,
	}, "/v1/vision/analyze")
	if err != nil {
		return VisionInferenceResponse{}, err
	}
	return VisionInferenceResponse{
		ProviderProfileID: multimodalResp.ProviderProfileID,
		Text:              multimodalResp.Text,
		ImageCount:        imageCount,
		AssistantIntent:   multimodalResp.AssistantIntent,
	}, nil
}

func (s *RuntimeInferenceService) completeMultimodal(ctx context.Context, apiKey string, req MultimodalInferenceRequest, path string) (MultimodalInferenceResponse, int, error) {
	if s.registry == nil || s.factory == nil {
		return MultimodalInferenceResponse{}, 0, ErrInvalidInput
	}
	messages, imageCount, err := providerMultimodalMessages(req.Messages)
	if err != nil {
		return MultimodalInferenceResponse{}, 0, err
	}
	voiceAssistant := MergeVoiceAssistantMetadata(req.VoiceAssistant, req.UIContext, req.AssistantIntent, req.Metadata)
	handle := s.startTrace(debugtrace.KindMultimodal, req.ProviderProfileID, strings.TrimSpace(req.ClientID), strings.TrimSpace(req.DeviceLabel), strings.TrimSpace(req.HardwareSKU), strings.TrimSpace(req.FirmwareVersion), strings.TrimSpace(req.FirmwareChannel), strings.TrimSpace(req.SessionID), path)
	if !voiceAssistant.Empty() {
		handle.AddEvent("voice_assistant.context", compactJSON(voiceAssistant))
	}
	handle.AddEvent("multimodal.started", compactJSON(map[string]any{
		"message_count": len(messages),
		"image_count":   imageCount,
	}))
	profile, err := s.registry.ResolveProviderProfile(ctx, apiKey, req.ProviderProfileID)
	if err != nil {
		handle.Fail(err)
		return MultimodalInferenceResponse{}, 0, err
	}
	handle.SetProviderProfileID(profile.ID)
	client, err := s.factory.MultimodalClient(profile)
	if err != nil {
		handle.Fail(err)
		return MultimodalInferenceResponse{}, 0, err
	}
	providerResp, err := client.Complete(ctx, provider.MultimodalRequest{
		DeviceID:  firstNonEmpty(strings.TrimSpace(req.ClientID), NamespaceFromAPIKey(apiKey)),
		SessionID: strings.TrimSpace(req.SessionID),
		Messages:  messages,
	})
	if err != nil {
		handle.Fail(err)
		return MultimodalInferenceResponse{}, 0, err
	}
	replyText := strings.TrimSpace(providerResp.Text)
	handle.AddEvent("multimodal.completed", replyText)
	handle.Complete()
	return MultimodalInferenceResponse{
		ProviderProfileID: profile.ID,
		Text:              replyText,
		AssistantIntent:   voiceAssistant.AssistantIntent,
	}, imageCount, nil
}

func providerMultimodalMessages(inputs []MultimodalMessageInput) ([]provider.MultimodalMessage, int, error) {
	if len(inputs) == 0 {
		return nil, 0, ErrInvalidInput
	}
	out := make([]provider.MultimodalMessage, 0, len(inputs))
	imageCount := 0
	for _, input := range inputs {
		role := strings.TrimSpace(input.Role)
		if role == "" {
			role = "user"
		}
		parts := make([]provider.MultimodalContent, 0, len(input.Content))
		for _, content := range input.Content {
			part, isImage, ok, err := providerMultimodalContent(content)
			if err != nil {
				return nil, 0, err
			}
			if !ok {
				continue
			}
			parts = append(parts, part)
			if isImage {
				imageCount++
			}
		}
		if len(parts) == 0 {
			continue
		}
		out = append(out, provider.MultimodalMessage{
			Role:    role,
			Content: parts,
		})
	}
	if len(out) == 0 {
		return nil, 0, ErrInvalidInput
	}
	return out, imageCount, nil
}

func providerMultimodalContent(input MultimodalContentInput) (provider.MultimodalContent, bool, bool, error) {
	contentType := strings.ToLower(strings.TrimSpace(input.Type))
	text := strings.TrimSpace(input.Text)
	if text != "" && (contentType == "" || contentType == "text" || contentType == "input_text") {
		return provider.MultimodalContent{
			Type: "text",
			Text: text,
			Name: strings.TrimSpace(input.Name),
		}, false, true, nil
	}

	url := strings.TrimSpace(input.URL)
	if input.ImageURL != nil && strings.TrimSpace(input.ImageURL.URL) != "" {
		url = strings.TrimSpace(input.ImageURL.URL)
	}
	mimeType := strings.TrimSpace(input.MIMEType)
	base64Input := strings.TrimSpace(input.ImageBase64)
	if base64Input != "" && (contentType == "" || contentType == "image" || contentType == "image_url" || contentType == "input_image") {
		data, detectedMIME, err := decodeImageBase64(base64Input)
		if err != nil {
			return provider.MultimodalContent{}, false, false, err
		}
		if mimeType == "" {
			mimeType = detectedMIME
		}
		if mimeType == "" {
			mimeType = "image/png"
		}
		return provider.MultimodalContent{
			Type:     "image",
			Data:     data,
			MIMEType: mimeType,
			Name:     strings.TrimSpace(input.Name),
		}, true, true, nil
	}
	if url != "" && (contentType == "" || contentType == "image" || contentType == "image_url" || contentType == "input_image") {
		return provider.MultimodalContent{
			Type:     "image_url",
			ImageURL: url,
			MIMEType: mimeType,
			Name:     strings.TrimSpace(input.Name),
		}, true, true, nil
	}
	return provider.MultimodalContent{}, false, false, nil
}

func decodeImageBase64(input string) ([]byte, string, error) {
	if strings.HasPrefix(input, "data:") {
		header, payload, ok := strings.Cut(input, ",")
		if !ok {
			return nil, "", ErrInvalidInput
		}
		if !strings.Contains(strings.ToLower(header), ";base64") {
			return nil, "", ErrInvalidInput
		}
		mimeType := strings.TrimPrefix(strings.Split(header, ";")[0], "data:")
		data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(payload))
		if err != nil {
			return nil, "", ErrInvalidInput
		}
		return data, mimeType, nil
	}
	data, err := base64.StdEncoding.DecodeString(input)
	if err != nil {
		return nil, "", ErrInvalidInput
	}
	return data, "", nil
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
	actualFormat := format
	handle.StartTTS(req.Text, format.toProviderFormat())
	err = client.SynthesizeStream(ctx, provider.TTSRequest{
		DeviceID:  firstNonEmpty(strings.TrimSpace(req.ClientID), NamespaceFromAPIKey(apiKey)),
		SessionID: strings.TrimSpace(req.SessionID),
		Text:      req.Text,
		Format:    format.toProviderFormat(),
	}, func(chunk provider.AudioChunk) error {
		actualFormat = mergeProviderAudioFormat(actualFormat, chunk.Format)
		audio = append(audio, chunk.Data...)
		chunkCount++
		handle.AddTTSChunk(chunk.Data)
		return nil
	})
	if err != nil {
		handle.Fail(err)
		return TTSInferenceResponse{}, err
	}
	format = normalizeSynthesisFormat(actualFormat, audio)
	handle.CompleteTTS(format.toProviderFormat())
	handle.Complete()
	return TTSInferenceResponse{
		ProviderProfileID: profile.ID,
		Format:            format,
		AudioBase64:       base64.StdEncoding.EncodeToString(audio),
		ChunkCount:        chunkCount,
	}, nil
}

func mergeProviderAudioFormat(current AudioFormatInput, next provider.AudioFormat) AudioFormatInput {
	if strings.TrimSpace(next.Codec) != "" {
		current.Codec = next.Codec
	}
	if next.SampleRateHz > 0 {
		current.SampleRateHz = next.SampleRateHz
	}
	if next.Channels > 0 {
		current.Channels = next.Channels
	}
	return current
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
