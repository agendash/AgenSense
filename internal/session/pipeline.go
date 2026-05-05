package session

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/agendash/AgenSense/internal/debugtrace"
	"github.com/agendash/AgenSense/internal/provider"
	"github.com/agendash/AgenSense/internal/voicelang"
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
	ASR   provider.ASRClient
	LLM   provider.LLMClient
	TTS   provider.TTSClient
	Debug *debugtrace.Store

	idCounter atomic.Uint64
}

const voiceGatewaySystemPrompt = `You are Agensense, a shared voice orchestration assistant for Agendash-style clients.

Respond for speech playback instead of terminal output.
- Keep the reply to one or two short sentences.
- Prefer the concrete outcome, next step, or blocking issue.
- Do not output markdown, JSON, XML, ANSI escapes, tool-call notation, or hidden reasoning.
- If a request requires a focused remote code agent and none is attached, say that directly and ask the user to focus an agent first.
- If the input is an approval or control command, keep the confirmation short and explicit.`

// Run executes one voice turn against the configured providers.
func (p *Pipeline) Run(ctx context.Context, req PipelineRequest, sink Sink) error {
	if p.ASR == nil || p.LLM == nil || p.TTS == nil {
		return fmt.Errorf("session pipeline: provider dependencies are incomplete")
	}

	turnStart := time.Now()
	asrStart := turnStart
	trace := p.startTrace(req)
	trace.SetInputAudio(req.Format, req.Audio)
	trace.StartASR(req.Format, len(req.Audio))
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
		trace.Fail(err)
		return err
	}
	slog.InfoContext(ctx, "asr request completed",
		"device_id", req.DeviceID,
		"session_id", req.SessionID,
		"stream_id", req.StreamID,
		"duration_ms", time.Since(asrStart).Milliseconds(),
		"text_chars", len(asrResp.Text),
		"text", asrResp.Text,
	)
	trace.CompleteASR(asrResp.Text)
	if err := sink.OnASRFinal(asrResp.Text); err != nil {
		trace.Fail(err)
		return err
	}

	var llmReply strings.Builder
	llmStart := time.Now()
	messages := []provider.ChatMessage{
		{Role: "system", Content: voiceGatewaySystemPrompt},
		{Role: "system", Content: voicelang.Instruction(voicelang.Auto, asrResp.Text)},
		{Role: "user", Content: asrResp.Text},
	}
	trace.StartLLM(messages)
	slog.InfoContext(ctx, "llm request started",
		"device_id", req.DeviceID,
		"session_id", req.SessionID,
		"stream_id", req.StreamID,
		"message_count", len(messages),
		"user_prompt_chars", len(asrResp.Text),
	)
	err = p.LLM.ChatStream(ctx, provider.ChatRequest{
		DeviceID:  req.DeviceID,
		SessionID: req.SessionID,
		Messages:  messages,
	}, func(delta provider.ChatDelta) error {
		llmReply.WriteString(delta.Text)
		trace.AddLLMDelta(delta.Text)
		return sink.OnLLMDelta(delta.Text)
	})
	if err != nil {
		slog.ErrorContext(ctx, "llm request failed",
			"device_id", req.DeviceID,
			"session_id", req.SessionID,
			"stream_id", req.StreamID,
			"error", err,
		)
		trace.Fail(err)
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
		"duration_ms", time.Since(llmStart).Milliseconds(),
		"reply_chars", len(replyText),
		"reply_text", replyText,
	)
	trace.CompleteLLM(replyText)
	if err := sink.OnLLMDone(replyText); err != nil {
		trace.Fail(err)
		return err
	}

	ttsStreamID := p.nextID("tts")
	ttsFormat := req.Format
	if ttsFormat.Codec == "" {
		ttsFormat.Codec = "pcm_s16le"
	}
	ttsStart := time.Now()
	trace.StartTTS(replyText, ttsFormat)
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
		trace.Fail(err)
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
		trace.AddTTSChunk(chunk.Data)
		return sink.OnTTSChunk(chunk.Data)
	})
	if err != nil {
		slog.ErrorContext(ctx, "tts request failed",
			"device_id", req.DeviceID,
			"session_id", req.SessionID,
			"stream_id", ttsStreamID,
			"error", err,
		)
		trace.Fail(err)
		return err
	}
	if err := sink.OnTTSStop(ttsStreamID); err != nil {
		trace.Fail(err)
		return err
	}
	slog.InfoContext(ctx, "tts request completed",
		"device_id", req.DeviceID,
		"session_id", req.SessionID,
		"stream_id", ttsStreamID,
		"duration_ms", time.Since(ttsStart).Milliseconds(),
		"audio_bytes", ttsBytes,
	)
	trace.CompleteTTS(ttsFormat)
	slog.InfoContext(ctx, "voice turn completed",
		"device_id", req.DeviceID,
		"session_id", req.SessionID,
		"stream_id", req.StreamID,
		"total_duration_ms", time.Since(turnStart).Milliseconds(),
	)
	trace.Complete()

	return sink.OnAction(Action{
		ActionID: p.nextID("act"),
		Kind:     "noop",
		Payload: map[string]any{
			"reason": "mock pipeline complete",
		},
	})
}

func (p *Pipeline) startTrace(req PipelineRequest) *debugtrace.Handle {
	if p == nil || p.Debug == nil {
		return nil
	}
	return p.Debug.StartTrace(debugtrace.KindVoiceTurn, debugtrace.SourceWS, debugtrace.TraceMeta{
		DeviceID:  req.DeviceID,
		SessionID: req.SessionID,
	})
}

func (p *Pipeline) nextID(prefix string) string {
	return fmt.Sprintf("%s-%06d", prefix, p.idCounter.Add(1))
}
