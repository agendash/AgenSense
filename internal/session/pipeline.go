package session

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"

	"github.com/zhuzhe/agensense/internal/provider"
)

// Action is a provider-agnostic device action emitted by the orchestration flow.
type Action struct {
	ActionID string
	Kind     string
	Payload  map[string]any
}

// Sink receives orchestration events in transport-neutral form.
type Sink interface {
	OnASRFinal(text string) error
	OnLLMDelta(text string) error
	OnLLMDone(fullText string) error
	OnTTSStart(streamID string, format provider.AudioFormat, text string) error
	OnTTSChunk(data []byte) error
	OnTTSStop(streamID string) error
	OnAction(action Action) error
}

// PipelineRequest is the transport-neutral audio turn input.
type PipelineRequest struct {
	SessionID string
	DeviceID  string
	StreamID  string
	Format    provider.AudioFormat
	Audio     []byte
}

// Pipeline composes the provider interfaces into one speech turn.
type Pipeline struct {
	ASR provider.ASRClient
	LLM provider.LLMClient
	TTS provider.TTSClient

	idCounter atomic.Uint64
}

// Run executes one voice turn against the configured providers.
func (p *Pipeline) Run(ctx context.Context, req PipelineRequest, sink Sink) error {
	if p.ASR == nil || p.LLM == nil || p.TTS == nil {
		return fmt.Errorf("session pipeline: provider dependencies are incomplete")
	}

	slog.InfoContext(ctx, "asr request started",
		"device_id", req.DeviceID,
		"session_id", req.SessionID,
		"stream_id", req.StreamID,
		"codec", req.Format.Codec,
		"sample_rate_hz", req.Format.SampleRateHz,
		"channels", req.Format.Channels,
		"audio_bytes", len(req.Audio),
	)
	asrResp, err := p.ASR.Transcribe(ctx, provider.TranscribeRequest{
		DeviceID:  req.DeviceID,
		SessionID: req.SessionID,
		Format:    req.Format,
		Audio:     req.Audio,
	})
	if err != nil {
		slog.ErrorContext(ctx, "asr request failed",
			"device_id", req.DeviceID,
			"session_id", req.SessionID,
			"stream_id", req.StreamID,
			"error", err,
		)
		return err
	}
	slog.InfoContext(ctx, "asr request completed",
		"device_id", req.DeviceID,
		"session_id", req.SessionID,
		"stream_id", req.StreamID,
		"text_chars", len(asrResp.Text),
		"text", asrResp.Text,
	)
	if err := sink.OnASRFinal(asrResp.Text); err != nil {
		return err
	}

	var llmReply strings.Builder
	slog.InfoContext(ctx, "llm request started",
		"device_id", req.DeviceID,
		"session_id", req.SessionID,
		"stream_id", req.StreamID,
		"message_count", 2,
		"user_prompt_chars", len(asrResp.Text),
	)
	err = p.LLM.ChatStream(ctx, provider.ChatRequest{
		DeviceID:  req.DeviceID,
		SessionID: req.SessionID,
		Messages: []provider.ChatMessage{
			{Role: "system", Content: "You are the mock voice gateway assistant."},
			{Role: "user", Content: asrResp.Text},
		},
	}, func(delta provider.ChatDelta) error {
		llmReply.WriteString(delta.Text)
		return sink.OnLLMDelta(delta.Text)
	})
	if err != nil {
		slog.ErrorContext(ctx, "llm request failed",
			"device_id", req.DeviceID,
			"session_id", req.SessionID,
			"stream_id", req.StreamID,
			"error", err,
		)
		return err
	}

	replyText := strings.TrimSpace(llmReply.String())
	if replyText == "" {
		replyText = "Mock agent reply."
	}
	slog.InfoContext(ctx, "llm request completed",
		"device_id", req.DeviceID,
		"session_id", req.SessionID,
		"stream_id", req.StreamID,
		"reply_chars", len(replyText),
		"reply_text", replyText,
	)
	if err := sink.OnLLMDone(replyText); err != nil {
		return err
	}

	ttsStreamID := p.nextID("tts")
	ttsFormat := req.Format
	if ttsFormat.Codec == "" {
		ttsFormat.Codec = "pcm_s16le"
	}
	slog.InfoContext(ctx, "tts request started",
		"device_id", req.DeviceID,
		"session_id", req.SessionID,
		"stream_id", ttsStreamID,
		"text_chars", len(replyText),
		"text", replyText,
		"codec", ttsFormat.Codec,
		"sample_rate_hz", ttsFormat.SampleRateHz,
		"channels", ttsFormat.Channels,
	)
	if err := sink.OnTTSStart(ttsStreamID, ttsFormat, replyText); err != nil {
		return err
	}
	ttsBytes := 0
	err = p.TTS.SynthesizeStream(ctx, provider.TTSRequest{
		DeviceID:  req.DeviceID,
		SessionID: req.SessionID,
		Text:      replyText,
		Format:    ttsFormat,
	}, func(chunk provider.AudioChunk) error {
		ttsBytes += len(chunk.Data)
		return sink.OnTTSChunk(chunk.Data)
	})
	if err != nil {
		slog.ErrorContext(ctx, "tts request failed",
			"device_id", req.DeviceID,
			"session_id", req.SessionID,
			"stream_id", ttsStreamID,
			"error", err,
		)
		return err
	}
	if err := sink.OnTTSStop(ttsStreamID); err != nil {
		return err
	}
	slog.InfoContext(ctx, "tts request completed",
		"device_id", req.DeviceID,
		"session_id", req.SessionID,
		"stream_id", ttsStreamID,
		"audio_bytes", ttsBytes,
	)

	return sink.OnAction(Action{
		ActionID: p.nextID("act"),
		Kind:     "noop",
		Payload: map[string]any{
			"reason": "mock pipeline complete",
		},
	})
}

func (p *Pipeline) nextID(prefix string) string {
	return fmt.Sprintf("%s-%06d", prefix, p.idCounter.Add(1))
}
