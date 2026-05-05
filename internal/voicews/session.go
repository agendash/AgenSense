package voicews

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/agendash/agensense/internal/debugtrace"
	"github.com/agendash/agensense/internal/gateway/wsconn"
	"github.com/agendash/agensense/internal/protocol"
	"github.com/agendash/agensense/internal/provider"
	"github.com/agendash/agensense/internal/service"
	"github.com/agendash/agensense/internal/voicelang"
)

const (
	eventSessionUpdate  = "session.update"
	eventSessionReady   = "session.ready"
	eventInputCancel    = "input.cancel"
	eventResponseCreate = "response.create"
	eventResponseCancel = "response.cancel"
	eventResponseDone   = "response.done"
	eventASRPartial     = "asr.partial"
	eventVADState       = "vad.state"

	defaultClientID         = "agendash-voice"
	defaultSampleRateHz     = 16000
	defaultChannels         = 1
	partialMinWindow        = 600 * time.Millisecond
	partialMinGrowthFactor  = 350 * time.Millisecond
	vadSpeechThreshold      = 0.012
	vadSilenceReleaseFactor = 220 * time.Millisecond
)

const voiceReplySystemPrompt = `You are Agensense, a shared voice orchestration assistant for Agendash clients.

Respond for speech playback, not a terminal or chat transcript.
- Keep the reply to one or two short sentences.
- Say the outcome, next action, or blocking issue directly.
- Do not output markdown, bullet lists, JSON, XML, ANSI escapes, or tool-call notation.
- Do not narrate hidden reasoning or internal implementation details.
- If no remote code agent is focused, stay in local assistant mode and say that a focused agent is required for remote execution.
- If the request clearly sounds like an approval, scene switch, or playback command, keep the wording brief and confirmation-oriented.`

type sessionDeps struct {
	conn     *wsconn.Conn
	apiKey   string
	registry *service.RegistryService
	factory  *provider.Factory
	debug    *debugtrace.Store
	now      func() time.Time
}

type session struct {
	conn     *wsconn.Conn
	apiKey   string
	registry *service.RegistryService
	factory  *provider.Factory
	debug    *debugtrace.Store
	now      func() time.Time

	mu sync.Mutex

	cfg sessionConfig

	asr provider.ASRClient
	llm provider.LLMClient
	tts provider.TTSClient

	inboundTracker protocol.StreamTracker
	inboundFormat  provider.AudioFormat
	inboundAudio   []byte
	inboundSeq     int
	turnTrace      *debugtrace.Handle

	speechActive     bool
	silenceBytes     int
	segmentSeq       int
	activeSegmentID  string
	lastSegmentID    string
	segmentStartByte int
	segmentStartMs   int64
	segmentPeakLevel float64

	partialInFlight bool
	lastPartialAt   time.Time
	lastPartialText string
	lastPartialSize int

	responseCancel context.CancelFunc
	responseStatus responseRuntime

	idCounter int64
}

type sessionConfig struct {
	ClientID          string
	DeviceLabel       string
	SessionID         string
	ProviderProfileID string
	ResponseLanguage  string
	Format            provider.AudioFormat
	VoiceAssistant    service.VoiceAssistantMetadata
}

type responseRuntime struct {
	active     bool
	text       string
	chunkCount int
	audioBytes int
}

type completedSegment struct {
	id            string
	streamID      string
	format        provider.AudioFormat
	audio         []byte
	startOffsetMs int64
	endOffsetMs   int64
	endLevel      float64
	peakLevel     float64
}

type sessionUpdatePayload struct {
	ClientID          string                          `json:"client_id,omitempty"`
	DeviceLabel       string                          `json:"device_label,omitempty"`
	SessionID         string                          `json:"session_id,omitempty"`
	ProviderProfileID string                          `json:"provider_profile_id,omitempty"`
	ResponseLanguage  string                          `json:"response_language,omitempty"`
	Format            map[string]any                  `json:"format,omitempty"`
	VoiceAssistant    *service.VoiceAssistantMetadata `json:"voice_assistant,omitempty"`
	UIContext         map[string]any                  `json:"ui_context,omitempty"`
	AssistantIntent   *service.VoiceAssistantIntent   `json:"assistant_intent,omitempty"`
	Metadata          map[string]any                  `json:"metadata,omitempty"`
}

type sessionReadyPayload struct {
	ClientID          string                          `json:"client_id"`
	DeviceLabel       string                          `json:"device_label,omitempty"`
	SessionID         string                          `json:"session_id"`
	ProviderProfileID string                          `json:"provider_profile_id"`
	ResponseLanguage  string                          `json:"response_language,omitempty"`
	Format            protocol.AudioStartPayload      `json:"format"`
	VoiceAssistant    *service.VoiceAssistantMetadata `json:"voice_assistant,omitempty"`
}

type responseCreatePayload struct {
	Text             string                         `json:"text"`
	ResponseLanguage string                         `json:"response_language,omitempty"`
	VoiceAssistant   service.VoiceAssistantMetadata `json:"voice_assistant,omitempty"`
	UIContext        map[string]any                 `json:"ui_context,omitempty"`
	AssistantIntent  *service.VoiceAssistantIntent  `json:"assistant_intent,omitempty"`
	Metadata         map[string]any                 `json:"metadata,omitempty"`
}

type responseDonePayload struct {
	Status     string `json:"status"`
	Text       string `json:"text,omitempty"`
	ChunkCount int    `json:"chunk_count,omitempty"`
	AudioBytes int    `json:"audio_bytes,omitempty"`
}

type vadStatePayload struct {
	State string  `json:"state"`
	Level float64 `json:"level"`
}

func newSession(deps sessionDeps) *session {
	now := deps.now
	if now == nil {
		now = time.Now
	}
	return &session{
		conn:     deps.conn,
		apiKey:   deps.apiKey,
		registry: deps.registry,
		factory:  deps.factory,
		debug:    deps.debug,
		now:      now,
		cfg: sessionConfig{
			ClientID:  defaultClientID,
			SessionID: fmt.Sprintf("voice-%d", now().UnixNano()),
			Format: provider.AudioFormat{
				Codec:        "pcm_s16le",
				SampleRateHz: defaultSampleRateHz,
				Channels:     defaultChannels,
			},
		},
	}
}

func (s *session) run(ctx context.Context) {
	for {
		opcode, payload, err := s.conn.ReadFrame()
		if err != nil {
			s.cancelResponse()
			s.completeOpenTrace()
			return
		}

		switch opcode {
		case wsconn.OpText:
			event, err := protocol.DecodeEvent(payload)
			if err != nil {
				s.writeError("invalid_event", err.Error())
				continue
			}
			if err := s.handleEvent(ctx, event); err != nil {
				slog.WarnContext(ctx, "voice websocket event failed",
					"type", event.Type,
					"error", err,
				)
				s.writeError("event_rejected", err.Error())
			}
		case wsconn.OpBinary:
			if err := s.handleAudioChunk(ctx, payload); err != nil {
				s.writeError("audio_rejected", err.Error())
			}
		case wsconn.OpClose:
			s.cancelResponse()
			s.completeOpenTrace()
			return
		default:
			s.writeError("unsupported_frame", "unsupported websocket frame")
		}
	}
}

func (s *session) handleEvent(ctx context.Context, event protocol.Envelope) error {
	switch event.Type {
	case eventSessionUpdate:
		var payload sessionUpdatePayload
		if err := event.DecodePayload(&payload); err != nil {
			return err
		}
		return s.handleSessionUpdate(ctx, payload)
	case protocol.EventAudioStart:
		var payload protocol.AudioStartPayload
		if err := event.DecodePayload(&payload); err != nil {
			return err
		}
		return s.handleAudioStart(payload)
	case protocol.EventAudioStop:
		var payload protocol.AudioStopPayload
		if err := event.DecodePayload(&payload); err != nil {
			return err
		}
		return s.handleAudioStop(ctx, payload)
	case eventInputCancel:
		s.cancelInput()
		return nil
	case eventResponseCreate:
		var payload responseCreatePayload
		if err := event.DecodePayload(&payload); err != nil {
			return err
		}
		return s.handleResponseCreate(ctx, payload)
	case eventResponseCancel:
		active := s.cancelResponse()
		if !active {
			s.completeOpenTraceWithResponseDone("cancelled")
		}
		return s.writeEvent(eventResponseDone, responseDonePayload{
			Status: "cancelled",
		})
	default:
		return fmt.Errorf("unsupported event type %q", event.Type)
	}
}

func (s *session) handleSessionUpdate(ctx context.Context, input sessionUpdatePayload) error {
	cfg := s.cfg
	if strings.TrimSpace(input.ClientID) != "" {
		cfg.ClientID = strings.TrimSpace(input.ClientID)
	}
	if strings.TrimSpace(input.DeviceLabel) != "" {
		cfg.DeviceLabel = strings.TrimSpace(input.DeviceLabel)
	}
	if strings.TrimSpace(input.SessionID) != "" {
		cfg.SessionID = strings.TrimSpace(input.SessionID)
	}
	if cfg.ClientID == "" {
		cfg.ClientID = defaultClientID
	}
	if cfg.SessionID == "" {
		cfg.SessionID = fmt.Sprintf("voice-%d", s.now().UnixNano())
	}
	if input.Format != nil {
		cfg.Format = parseFormat(input.Format, cfg.Format)
	}
	nextVoiceAssistant := cfg.VoiceAssistant
	if input.VoiceAssistant != nil && !input.VoiceAssistant.Empty() {
		nextVoiceAssistant = *input.VoiceAssistant
	}
	cfg.VoiceAssistant = service.MergeVoiceAssistantMetadata(nextVoiceAssistant, input.UIContext, input.AssistantIntent, input.Metadata)
	cfg.ResponseLanguage = resolveResponseLanguage(
		cfg.ResponseLanguage,
		input.ResponseLanguage,
		input.Metadata,
		cfg.VoiceAssistant,
	)

	profile, err := s.registry.ResolveProviderProfile(ctx, s.apiKey, strings.TrimSpace(input.ProviderProfileID))
	if err != nil {
		return err
	}

	asr, err := s.factory.ASRClient(profile)
	if err != nil {
		return err
	}
	llm, err := s.factory.LLMClient(profile)
	if err != nil {
		return err
	}
	tts, err := s.factory.TTSClient(profile)
	if err != nil {
		return err
	}

	cfg.ProviderProfileID = profile.ID

	s.mu.Lock()
	s.cfg = cfg
	s.asr = asr
	s.llm = llm
	s.tts = tts
	s.mu.Unlock()

	var readyVoiceAssistant *service.VoiceAssistantMetadata
	if !cfg.VoiceAssistant.Empty() {
		readyVoiceAssistant = &cfg.VoiceAssistant
	}
	return s.writeEvent(eventSessionReady, sessionReadyPayload{
		ClientID:          cfg.ClientID,
		DeviceLabel:       cfg.DeviceLabel,
		SessionID:         cfg.SessionID,
		ProviderProfileID: cfg.ProviderProfileID,
		ResponseLanguage:  cfg.ResponseLanguage,
		Format: protocol.AudioStartPayload{
			StreamID:     "input",
			Codec:        cfg.Format.Codec,
			SampleRateHz: cfg.Format.SampleRateHz,
			Channels:     cfg.Format.Channels,
		},
		VoiceAssistant: readyVoiceAssistant,
	})
}

func (s *session) handleAudioStart(payload protocol.AudioStartPayload) error {
	if payload.StreamID == "" {
		return fmt.Errorf("audio.start.payload.stream_id is required")
	}
	if payload.Codec != "pcm_s16le" {
		return fmt.Errorf("audio.start.payload.codec must be pcm_s16le")
	}
	if payload.SampleRateHz <= 0 || payload.Channels <= 0 {
		return fmt.Errorf("audio.start payload sample rate and channels must be > 0")
	}

	s.cancelResponse()
	s.completeOpenTrace()

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.inboundTracker.Start(payload); err != nil {
		return err
	}
	s.inboundFormat = provider.AudioFormat{
		Codec:        payload.Codec,
		SampleRateHz: payload.SampleRateHz,
		Channels:     payload.Channels,
	}
	s.inboundAudio = s.inboundAudio[:0]
	s.inboundSeq = 0
	s.speechActive = false
	s.silenceBytes = 0
	s.segmentSeq = 0
	s.activeSegmentID = ""
	s.lastSegmentID = ""
	s.segmentStartByte = 0
	s.segmentStartMs = 0
	s.segmentPeakLevel = 0
	s.partialInFlight = false
	s.lastPartialAt = time.Time{}
	s.lastPartialText = ""
	s.lastPartialSize = 0
	s.turnTrace = s.startTraceLocked(payload.StreamID)
	if s.turnTrace != nil {
		s.turnTrace.AddEvent("ws.audio.start", fmt.Sprintf("%s · %s · %d Hz · %d ch", payload.StreamID, payload.Codec, payload.SampleRateHz, payload.Channels))
	}
	return nil
}

func (s *session) handleAudioChunk(ctx context.Context, payload []byte) error {
	s.mu.Lock()
	if _, _, ok := s.inboundTracker.Active(); !ok {
		s.mu.Unlock()
		return fmt.Errorf("binary audio received without audio.start")
	}
	if err := s.inboundTracker.AddFrame(); err != nil {
		s.mu.Unlock()
		return err
	}

	chunkStartByte := len(s.inboundAudio)
	s.inboundAudio = append(s.inboundAudio, payload...)
	s.inboundSeq++
	format := s.inboundFormat
	stream, _, _ := s.inboundTracker.Active()
	audioSnapshot := append([]byte(nil), s.inboundAudio...)
	streamID := stream.StreamID
	now := s.now()
	level := pcmLevel(payload)
	speaking := level >= vadSpeechThreshold
	stateChanged := false
	vadState := ""
	var completed *completedSegment
	if speaking {
		s.silenceBytes = 0
		if !s.speechActive {
			s.speechActive = true
			stateChanged = true
			vadState = "speech_started"
			s.segmentSeq++
			s.activeSegmentID = fmt.Sprintf("segment-%03d", s.segmentSeq)
			s.segmentStartByte = chunkStartByte
			s.segmentStartMs = durationMs(formatByteDuration(format, chunkStartByte))
			s.segmentPeakLevel = level
			if s.turnTrace != nil {
				s.turnTrace.StartSegment(s.activeSegmentID, streamID, format, s.segmentStartMs, level)
			}
		}
	} else if s.speechActive {
		s.silenceBytes += len(payload)
		if formatByteDuration(format, s.silenceBytes) >= vadSilenceReleaseFactor {
			s.speechActive = false
			s.silenceBytes = 0
			stateChanged = true
			vadState = "speech_stopped"
			completed = s.completeActiveSegmentLocked(streamID, format, len(s.inboundAudio), level)
		}
	}
	if s.speechActive && level > s.segmentPeakLevel {
		s.segmentPeakLevel = level
	}

	canPartial := s.asr != nil &&
		len(audioSnapshot) > 0 &&
		!s.partialInFlight &&
		(now.Sub(s.lastPartialAt) >= partialMinWindow || s.lastPartialAt.IsZero()) &&
		formatByteDuration(format, len(audioSnapshot)-s.lastPartialSize) >= partialMinGrowthFactor
	if canPartial {
		s.partialInFlight = true
		s.lastPartialAt = now
		s.lastPartialSize = len(audioSnapshot)
	}
	asrClient := s.asr
	cfg := s.cfg
	s.mu.Unlock()

	if stateChanged {
		s.writeEvent(eventVADState, vadStatePayload{
			State: vadState,
			Level: level,
		})
	}
	if completed != nil {
		s.recordCompletedSegment(completed)
	}

	if canPartial {
		go s.runPartialASR(ctx, asrClient, cfg, streamID, format, audioSnapshot)
	}
	return nil
}

func (s *session) handleAudioStop(ctx context.Context, payload protocol.AudioStopPayload) error {
	s.mu.Lock()
	active, frames, ok := s.inboundTracker.Active()
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("audio.stop received without audio.start")
	}
	if payload.StreamID != active.StreamID {
		s.mu.Unlock()
		return fmt.Errorf("audio.stop stream_id mismatch")
	}
	if payload.LastSeq != frames {
		s.mu.Unlock()
		return fmt.Errorf("audio.stop last_seq=%d does not match frame count %d", payload.LastSeq, frames)
	}
	if err := s.inboundTracker.Stop(payload); err != nil {
		s.mu.Unlock()
		return err
	}

	audio := append([]byte(nil), s.inboundAudio...)
	format := s.inboundFormat
	asrClient := s.asr
	cfg := s.cfg
	trace := s.turnTrace
	completed := s.completeActiveSegmentLocked(payload.StreamID, format, len(s.inboundAudio), 0)
	s.inboundAudio = s.inboundAudio[:0]
	s.inboundSeq = 0
	s.partialInFlight = false
	s.mu.Unlock()

	if trace != nil {
		trace.SetInputAudio(format, audio)
		trace.AddEvent("ws.audio.stop", fmt.Sprintf("%s · %d frames · %d bytes", payload.StreamID, payload.LastSeq, len(audio)))
		trace.StartASR(format, len(audio))
	}
	if completed != nil {
		s.recordCompletedSegment(completed)
	}
	if asrClient == nil {
		if trace != nil {
			trace.Fail(fmt.Errorf("voice session is not configured"))
		}
		return fmt.Errorf("voice session is not configured")
	}
	go s.runFinalASR(ctx, asrClient, cfg, payload.StreamID, format, audio, trace)
	return nil
}

func (s *session) handleResponseCreate(ctx context.Context, payload responseCreatePayload) error {
	text := strings.TrimSpace(payload.Text)
	if text == "" {
		return fmt.Errorf("response.create.payload.text is required")
	}

	s.cancelResponse()

	s.mu.Lock()
	llmClient := s.llm
	ttsClient := s.tts
	cfg := s.cfg
	trace := s.turnTrace
	format := cfg.Format
	if format.Codec == "" {
		format.Codec = "pcm_s16le"
	}
	if format.SampleRateHz <= 0 {
		format.SampleRateHz = defaultSampleRateHz
	}
	if format.Channels <= 0 {
		format.Channels = defaultChannels
	}
	s.mu.Unlock()

	if llmClient == nil || ttsClient == nil {
		return fmt.Errorf("voice session is not configured")
	}

	runCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.responseCancel = cancel
	s.responseStatus = responseRuntime{
		active: true,
	}
	s.mu.Unlock()

	voiceAssistant := cfg.VoiceAssistant
	if !payload.VoiceAssistant.Empty() {
		voiceAssistant = payload.VoiceAssistant
	}
	voiceAssistant = service.MergeVoiceAssistantMetadata(voiceAssistant, payload.UIContext, payload.AssistantIntent, payload.Metadata)
	cfg.ResponseLanguage = resolveResponseLanguage(
		cfg.ResponseLanguage,
		payload.ResponseLanguage,
		payload.Metadata,
		voiceAssistant,
	)
	go s.runResponse(runCtx, llmClient, ttsClient, cfg, format, text, voiceAssistant, trace)
	return nil
}

func (s *session) runPartialASR(ctx context.Context, asr provider.ASRClient, cfg sessionConfig, streamID string, format provider.AudioFormat, audio []byte) {
	defer func() {
		s.mu.Lock()
		s.partialInFlight = false
		s.mu.Unlock()
	}()
	if asr == nil || len(audio) == 0 {
		return
	}

	resp, err := asr.Transcribe(ctx, provider.TranscribeRequest{
		DeviceID:  cfg.ClientID,
		SessionID: cfg.SessionID,
		Format:    format,
		Audio:     audio,
	})
	if err != nil {
		return
	}
	text := strings.TrimSpace(resp.Text)
	if text == "" {
		return
	}

	s.mu.Lock()
	active, _, ok := s.inboundTracker.Active()
	if !ok || active.StreamID != streamID || text == s.lastPartialText {
		s.mu.Unlock()
		return
	}
	s.lastPartialText = text
	trace := s.turnTrace
	s.mu.Unlock()
	if trace != nil {
		trace.AddEvent("asr.partial", previewDetail(text, 96))
	}
	_ = s.writeEvent(eventASRPartial, protocol.ASRFinalPayload{
		StreamID: streamID,
		Text:     text,
	})
}

func (s *session) runFinalASR(ctx context.Context, asr provider.ASRClient, cfg sessionConfig, streamID string, format provider.AudioFormat, audio []byte, trace *debugtrace.Handle) {
	if len(audio) == 0 {
		s.writeError("empty_audio", "audio buffer is empty")
		if trace != nil {
			trace.Fail(fmt.Errorf("audio buffer is empty"))
			s.clearTurnTrace(trace)
		}
		return
	}

	resp, err := asr.Transcribe(ctx, provider.TranscribeRequest{
		DeviceID:  cfg.ClientID,
		SessionID: cfg.SessionID,
		Format:    format,
		Audio:     audio,
	})
	if err != nil {
		s.writeError("asr_failed", err.Error())
		if trace != nil {
			trace.Fail(err)
			s.clearTurnTrace(trace)
		}
		return
	}
	text := strings.TrimSpace(resp.Text)
	if trace != nil {
		trace.CompleteASR(text)
		if lastSegmentID := s.getLastSegmentID(); lastSegmentID != "" {
			trace.SetSegmentText(lastSegmentID, text)
		}
	}
	if err := s.writeEvent(protocol.EventASRFinal, protocol.ASRFinalPayload{
		StreamID: streamID,
		Text:     text,
	}); err != nil {
		slog.WarnContext(ctx, "voice websocket asr.final write failed", "error", err)
	}
}

func (s *session) runResponse(ctx context.Context, llm provider.LLMClient, tts provider.TTSClient, cfg sessionConfig, format provider.AudioFormat, text string, voiceAssistant service.VoiceAssistantMetadata, trace *debugtrace.Handle) {
	defer s.clearResponse()

	languageInstruction := voicelang.Instruction(cfg.ResponseLanguage, text)
	messages := []provider.ChatMessage{
		{Role: "system", Content: voiceReplySystemPrompt},
		{Role: "system", Content: languageInstruction},
	}
	if !voiceAssistant.Empty() {
		contextPrompt := voiceAssistantPrompt(voiceAssistant)
		if contextPrompt != "" {
			messages = append(messages, provider.ChatMessage{
				Role:    "system",
				Content: contextPrompt,
			})
		}
	}
	messages = append(messages, provider.ChatMessage{Role: "user", Content: text})
	if trace != nil {
		trace.AddEvent("response.create", previewDetail(text, 96))
		trace.AddEvent("response.language", voicelang.Label(cfg.ResponseLanguage, text))
		if !voiceAssistant.Empty() {
			trace.AddEvent("voice_assistant.context", previewDetail(compactJSON(voiceAssistant), 160))
		}
		trace.StartLLM(messages)
	}

	var llmReply strings.Builder
	err := llm.ChatStream(ctx, provider.ChatRequest{
		DeviceID:  cfg.ClientID,
		SessionID: cfg.SessionID,
		Messages:  messages,
	}, func(delta provider.ChatDelta) error {
		llmReply.WriteString(delta.Text)
		if trace != nil {
			trace.AddLLMDelta(delta.Text)
		}
		return s.writeEvent(protocol.EventLLMDelta, protocol.LLMDeltaPayload{
			Text: delta.Text,
		})
	})
	if err != nil {
		if ctx.Err() == context.Canceled {
			if trace != nil {
				trace.AddEvent("response.cancelled", "")
				trace.Complete()
				s.clearTurnTrace(trace)
			}
			s.writeEvent(eventResponseDone, responseDonePayload{Status: "cancelled"})
			return
		}
		if trace != nil {
			trace.Fail(err)
			s.clearTurnTrace(trace)
		}
		s.writeError("llm_failed", err.Error())
		s.writeEvent(eventResponseDone, responseDonePayload{Status: "failed"})
		return
	}

	replyText := strings.TrimSpace(llmReply.String())
	if replyText == "" {
		replyText = "I did not catch a stable request."
	}
	if trace != nil {
		trace.CompleteLLM(replyText)
	}
	if err := s.writeEvent(protocol.EventLLMDone, protocol.LLMDonePayload{Text: replyText}); err != nil {
		s.writeError("llm_done_failed", err.Error())
		if trace != nil {
			trace.Fail(err)
			s.clearTurnTrace(trace)
		}
		return
	}

	streamID := s.nextID("tts")
	if trace != nil {
		trace.StartTTS(replyText, format)
	}
	if err := s.writeEvent(protocol.EventTTSStart, protocol.AudioStartPayload{
		StreamID:     streamID,
		Codec:        format.Codec,
		SampleRateHz: format.SampleRateHz,
		Channels:     format.Channels,
	}); err != nil {
		s.writeError("tts_start_failed", err.Error())
		if trace != nil {
			trace.Fail(err)
			s.clearTurnTrace(trace)
		}
		return
	}

	chunkCount := 0
	audioBytes := 0
	err = tts.SynthesizeStream(ctx, provider.TTSRequest{
		DeviceID:  cfg.ClientID,
		SessionID: cfg.SessionID,
		Text:      replyText,
		Format:    format,
	}, func(chunk provider.AudioChunk) error {
		chunkCount++
		audioBytes += len(chunk.Data)
		if trace != nil {
			trace.AddTTSChunk(chunk.Data)
		}
		return s.conn.WriteBinary(chunk.Data)
	})
	if err != nil && ctx.Err() != context.Canceled {
		s.writeError("tts_failed", err.Error())
		s.writeEvent(eventResponseDone, responseDonePayload{Status: "failed", Text: replyText})
		if trace != nil {
			trace.Fail(err)
			s.clearTurnTrace(trace)
		}
		return
	}

	_ = s.writeEvent(protocol.EventTTSStop, protocol.AudioStopPayload{
		StreamID: streamID,
		LastSeq:  chunkCount,
	})
	status := "completed"
	if ctx.Err() == context.Canceled {
		status = "cancelled"
	}
	if trace != nil {
		trace.CompleteTTS(format)
		trace.AddEvent("response.done", status)
		trace.Complete()
		s.clearTurnTrace(trace)
	}
	_ = s.writeEvent(eventResponseDone, responseDonePayload{
		Status:     status,
		Text:       replyText,
		ChunkCount: chunkCount,
		AudioBytes: audioBytes,
	})
}

func (s *session) clearResponse() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.responseCancel = nil
	s.responseStatus = responseRuntime{}
}

func (s *session) cancelResponse() bool {
	s.mu.Lock()
	cancel := s.responseCancel
	s.responseCancel = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
		return true
	}
	return false
}

func (s *session) cancelInput() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inboundTracker = protocol.StreamTracker{}
	s.inboundAudio = s.inboundAudio[:0]
	s.inboundSeq = 0
	s.speechActive = false
	s.silenceBytes = 0
	s.partialInFlight = false
	s.lastPartialAt = time.Time{}
	s.lastPartialText = ""
	s.lastPartialSize = 0
}

func (s *session) nextID(prefix string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.idCounter++
	return fmt.Sprintf("%s-%06d", prefix, s.idCounter)
}

func (s *session) writeEvent(eventType string, payload any) error {
	s.mu.Lock()
	sessionID := s.cfg.SessionID
	s.mu.Unlock()
	event, err := protocol.NewEvent(eventType, "", sessionID, payload)
	if err != nil {
		return err
	}
	data, err := protocol.MarshalEvent(event)
	if err != nil {
		return err
	}
	return s.conn.WriteText(data)
}

func (s *session) writeError(code, message string) {
	_ = s.writeEvent(protocol.EventError, protocol.ErrorPayload{
		Code:    code,
		Message: message,
	})
}

func (s *session) startTraceLocked(streamID string) *debugtrace.Handle {
	if s.debug == nil {
		return nil
	}
	trace := s.debug.StartTrace(debugtrace.KindVoiceTurn, debugtrace.SourceWS, debugtrace.TraceMeta{
		ClientID:          s.cfg.ClientID,
		DeviceLabel:       s.cfg.DeviceLabel,
		SessionID:         s.cfg.SessionID,
		ProviderProfileID: s.cfg.ProviderProfileID,
		HTTPPath:          "/v1/voice/ws",
	})
	if trace != nil {
		trace.AddEvent("ws.stream.bound", streamID)
	}
	return trace
}

func (s *session) completeOpenTrace() {
	s.mu.Lock()
	trace := s.turnTrace
	s.turnTrace = nil
	s.mu.Unlock()
	if trace != nil {
		trace.Complete()
	}
}

func (s *session) completeOpenTraceWithResponseDone(status string) {
	s.mu.Lock()
	trace := s.turnTrace
	s.turnTrace = nil
	s.mu.Unlock()
	if trace != nil {
		trace.AddEvent("response.done", status)
		trace.Complete()
	}
}

func (s *session) clearTurnTrace(trace *debugtrace.Handle) {
	if trace == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.turnTrace == trace {
		s.turnTrace = nil
	}
}

func (s *session) completeActiveSegmentLocked(streamID string, format provider.AudioFormat, endByte int, endLevel float64) *completedSegment {
	if s.activeSegmentID == "" {
		return nil
	}
	startByte := s.segmentStartByte
	if startByte < 0 {
		startByte = 0
	}
	if endByte > len(s.inboundAudio) {
		endByte = len(s.inboundAudio)
	}
	if endByte < startByte {
		endByte = startByte
	}
	segmentAudio := append([]byte(nil), s.inboundAudio[startByte:endByte]...)
	segment := &completedSegment{
		id:            s.activeSegmentID,
		streamID:      streamID,
		format:        format,
		audio:         segmentAudio,
		startOffsetMs: s.segmentStartMs,
		endOffsetMs:   durationMs(formatByteDuration(format, endByte)),
		endLevel:      endLevel,
		peakLevel:     s.segmentPeakLevel,
	}
	s.lastSegmentID = s.activeSegmentID
	s.activeSegmentID = ""
	s.segmentStartByte = 0
	s.segmentStartMs = 0
	s.segmentPeakLevel = 0
	return segment
}

func (s *session) recordCompletedSegment(segment *completedSegment) {
	if segment == nil {
		return
	}
	s.mu.Lock()
	trace := s.turnTrace
	s.mu.Unlock()
	if trace == nil {
		return
	}
	trace.CompleteSegment(
		segment.id,
		segment.format,
		segment.audio,
		segment.startOffsetMs,
		segment.endOffsetMs,
		segment.endLevel,
		segment.peakLevel,
	)
}

func (s *session) getLastSegmentID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastSegmentID
}

func durationMs(d time.Duration) int64 {
	return d.Milliseconds()
}

func previewDetail(text string, limit int) string {
	if limit <= 0 || len(text) <= limit {
		return text
	}
	return text[:limit] + "..."
}

func voiceAssistantPrompt(metadata service.VoiceAssistantMetadata) string {
	if metadata.Empty() {
		return ""
	}
	payload := compactJSON(metadata)
	if payload == "" {
		return ""
	}
	return "Agendash Universal Voice Layer context JSON. Use it only to resolve scope, target, UI surface, and confirmation safety. Do not read raw JSON back to the user unless asked.\n" + payload
}

func resolveResponseLanguage(current string, explicit string, metadata map[string]any, voiceAssistant service.VoiceAssistantMetadata) string {
	if strings.TrimSpace(explicit) != "" {
		return voicelang.Normalize(explicit)
	}
	if normalized := voicelang.FromMetadata(metadata); normalized != voicelang.Auto {
		return normalized
	}
	if normalized := voicelang.FromMetadata(voiceAssistant.Metadata); normalized != voicelang.Auto {
		return normalized
	}
	if strings.TrimSpace(current) != "" {
		return voicelang.Normalize(current)
	}
	return voicelang.Auto
}

func compactJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(data)
}

func parseFormat(input map[string]any, fallback provider.AudioFormat) provider.AudioFormat {
	codec, _ := input["codec"].(string)
	if strings.TrimSpace(codec) == "" {
		codec = fallback.Codec
	}
	if codec == "" {
		codec = "pcm_s16le"
	}
	sampleRate := intValue(input["sample_rate_hz"], fallback.SampleRateHz)
	if sampleRate <= 0 {
		sampleRate = defaultSampleRateHz
	}
	channels := intValue(input["channels"], fallback.Channels)
	if channels <= 0 {
		channels = defaultChannels
	}
	return provider.AudioFormat{
		Codec:        codec,
		SampleRateHz: sampleRate,
		Channels:     channels,
	}
}

func intValue(value any, fallback int) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case float32:
		return int(typed)
	default:
		return fallback
	}
}

func formatByteDuration(format provider.AudioFormat, bytes int) time.Duration {
	sampleRate := format.SampleRateHz
	if sampleRate <= 0 {
		sampleRate = defaultSampleRateHz
	}
	channels := format.Channels
	if channels <= 0 {
		channels = defaultChannels
	}
	if bytes <= 0 {
		return 0
	}
	seconds := float64(bytes) / float64(sampleRate*channels*2)
	return time.Duration(seconds * float64(time.Second))
}

func pcmLevel(payload []byte) float64 {
	if len(payload) < 2 {
		return 0
	}
	samples := len(payload) / 2
	var sum float64
	for offset := 0; offset+1 < len(payload); offset += 2 {
		sample := int16(binary.LittleEndian.Uint16(payload[offset : offset+2]))
		value := float64(sample) / float64(math.MaxInt16)
		sum += value * value
	}
	return math.Sqrt(sum / float64(samples))
}
