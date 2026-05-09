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
	"unicode"

	"github.com/agendash/AgenSense/internal/debugtrace"
	"github.com/agendash/AgenSense/internal/gateway/wsconn"
	"github.com/agendash/AgenSense/internal/protocol"
	"github.com/agendash/AgenSense/internal/provider"
	"github.com/agendash/AgenSense/internal/service"
	"github.com/agendash/AgenSense/internal/voicelang"
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
	partialMinWindow        = 1500 * time.Millisecond
	partialMinGrowthFactor  = 1200 * time.Millisecond
	vadSpeechThreshold      = 0.012
	vadSilenceReleaseFactor = 220 * time.Millisecond
	realtimeTTSMaxRunes     = 48
	realtimeTTSSoftMinRunes = 36
	recentSpeechWindow      = 20 * time.Second
	recentSpeechLimit       = 4
	echoMinRunes            = 8
)

const voiceReplySystemPrompt = `You are AgenSense, a shared voice orchestration assistant for AgenDash clients.

Respond for speech playback, not a terminal or chat transcript.
- Keep the reply to one short sentence unless the user explicitly asks for detail.
- Target 28 Chinese characters or 16 English words.
- Say the outcome, next action, or blocking issue directly.
- Treat the user's message as a raw ASR transcript: it may be colloquial, fragmented, misheard, or contain filler words.
- Silently infer the user's real intent before answering; preserve names, numbers, commands, file paths, model names, and technical terms exactly when they matter.
- If the transcript appears to contain acoustic echo from recent assistant speech, ignore the echoed wording and answer only the user's new intent.
- If the transcript is mostly echo or too ambiguous to act on, give a very short clarification instead of advancing the task.
- Do not output markdown, bullet lists, JSON, XML, ANSI escapes, or tool-call notation.
- Do not narrate hidden reasoning or internal implementation details.
- If no remote code agent is focused, stay in local assistant mode and say that a focused agent is required for remote execution.
- If the request clearly sounds like an approval, scene switch, or playback command, keep the wording brief and confirmation-oriented.`

const voiceEchoContextPrompt = `Acoustic echo guard:
The latest user text is a raw ASR transcript. It may include casual speech, partial phrases, recognition errors, or acoustic echo from assistant audio that was just played.
Compare the assistant speech below with the latest ASR transcript. If the ASR mostly repeats assistant speech, treat it as echo and do not execute or advance that repeated content.
If the ASR includes both echo and a new user request, discard only the echoed portion and answer the new request.
Preserve exact names, numbers, paths, commands, and model identifiers from the user's new intent.

Assistant speech already played:
%s

Raw latest ASR transcript:
%s

Return a natural spoken reply for the inferred user intent. Keep it brief; ask one short clarification if the new intent is not stable enough.`

const mcpProposalSystemPrompt = `You are an MCP call planner for a voice gateway.
Convert the latest raw ASR transcript into exactly one proposed MCP tool call for the client to review or execute later.
Return only a single JSON object, with no markdown or prose.
The JSON shape is:
{"tool_name":"exact available tool name","arguments":{},"confidence":0.0,"requires_confirmation":true,"reason":"short reason"}
Rules:
- tool_name must exactly match one available tool.
- Preserve user names, numbers, dates, locations, code identifiers, commands, and paths.
- Put the original transcript in arguments.raw_text unless the chosen tool schema clearly has a better field.
- If the transcript lacks required scheduling details, keep the call as a candidate and include missing_required_fields in arguments.
- If no specialized tool fits, choose a capture-text or note-like tool when available; otherwise choose the safest available tool.
- Never claim execution; this is only a proposed MCP call.`

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
	partialCancel   context.CancelFunc
	partialRunID    int64
	lastPartialAt   time.Time
	lastPartialText string
	lastPartialSize int

	responseCancel context.CancelFunc
	responseStatus responseRuntime
	recentSpeech   []recentSpeechText

	idCounter int64
}

type sessionConfig struct {
	ClientID          string
	DeviceLabel       string
	SessionID         string
	ProviderProfileID string
	ResponseLanguage  string
	AutoRespond       bool
	Format            provider.AudioFormat
	VoiceAssistant    service.VoiceAssistantMetadata
}

type responseRuntime struct {
	active     bool
	text       string
	chunkCount int
	audioBytes int
}

type realtimeTTSResult struct {
	chunkCount int
	audioBytes int
	format     provider.AudioFormat
	err        error
}

type realtimeTTSSegmenter struct {
	buf       strings.Builder
	runeCount int
}

type recentSpeechText struct {
	text string
	at   time.Time
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
	AutoRespond       bool                            `json:"auto_response,omitempty"`
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
	AutoRespond       bool                            `json:"auto_response,omitempty"`
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

type mcpProposalModelOutput struct {
	ToolName             string         `json:"tool_name"`
	Tool                 string         `json:"tool"`
	Name                 string         `json:"name"`
	Arguments            map[string]any `json:"arguments"`
	Args                 map[string]any `json:"args"`
	Transcript           string         `json:"transcript"`
	Confidence           *float64       `json:"confidence"`
	RequiresConfirmation bool           `json:"requires_confirmation"`
	Reason               string         `json:"reason"`
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
			s.cancelPartialASR()
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
	cfg.AutoRespond = input.AutoRespond

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
		AutoRespond:       cfg.AutoRespond,
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
	s.cancelPartialASR()
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
	s.partialCancel = nil
	s.partialRunID++
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
	var partialCtx context.Context
	var partialRunID int64
	if canPartial {
		var partialCancel context.CancelFunc
		partialCtx, partialCancel = context.WithCancel(ctx)
		s.partialRunID++
		partialRunID = s.partialRunID
		s.partialInFlight = true
		s.partialCancel = partialCancel
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
		go s.runPartialASR(partialCtx, partialRunID, asrClient, cfg, streamID, format, audioSnapshot)
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
	partialCancel := s.cancelPartialASRLocked()
	s.inboundAudio = s.inboundAudio[:0]
	s.inboundSeq = 0
	s.mu.Unlock()
	if partialCancel != nil {
		partialCancel()
	}

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

func (s *session) runPartialASR(ctx context.Context, runID int64, asr provider.ASRClient, cfg sessionConfig, streamID string, format provider.AudioFormat, audio []byte) {
	defer func() {
		s.mu.Lock()
		if s.partialRunID == runID {
			s.partialInFlight = false
			s.partialCancel = nil
		}
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
	if ctx.Err() != nil {
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
	if !cfg.AutoRespond {
		return
	}
	responseText := text
	if isBlankASRText(text) {
		if trace != nil {
			trace.AddEvent("asr.blank_fallback", "speech detected but ASR returned no stable transcript")
		}
		responseText = "Speech-like audio was detected, but ASR returned no stable transcript. Ask the user to repeat briefly."
	}
	if err := s.handleResponseCreate(ctx, responseCreatePayload{
		Text:             responseText,
		ResponseLanguage: cfg.ResponseLanguage,
		VoiceAssistant:   cfg.VoiceAssistant,
		Metadata:         cfg.VoiceAssistant.Metadata,
		AssistantIntent:  cfg.VoiceAssistant.AssistantIntent,
		UIContext:        cfg.VoiceAssistant.UIContext,
	}); err != nil {
		if trace != nil {
			trace.Fail(err)
			s.clearTurnTrace(trace)
		}
		s.writeError("response_create_failed", err.Error())
		_ = s.writeEvent(eventResponseDone, responseDonePayload{
			Status: "failed",
			Text:   text,
		})
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
		if sharedPrompt := sharedSystemPrompt(voiceAssistant); sharedPrompt != "" {
			messages = append(messages, provider.ChatMessage{
				Role:    "system",
				Content: "Shared GUI system prompt:\n" + sharedPrompt,
			})
			if trace != nil {
				trace.AddEvent("shared_system_prompt", previewDetail(sharedPrompt, 160))
			}
		}
	}
	if echoPrompt := s.recentSpeechPrompt(text); echoPrompt != "" {
		messages = append(messages, provider.ChatMessage{
			Role:    "system",
			Content: echoPrompt,
		})
		if trace != nil {
			trace.AddEvent("echo.context", previewDetail(echoPrompt, 160))
		}
	}
	messages = append(messages, provider.ChatMessage{Role: "user", Content: text})
	if err := s.proposeMCPCall(ctx, llm, cfg, text, voiceAssistant, trace); err != nil && trace != nil {
		trace.AddEvent("mcp.call.proposed_failed", previewDetail(err.Error(), 160))
	}
	if trace != nil {
		trace.AddEvent("response.create", previewDetail(text, 96))
		trace.AddEvent("response.language", voicelang.Label(cfg.ResponseLanguage, text))
		if !voiceAssistant.Empty() {
			trace.AddEvent("voice_assistant.context", previewDetail(compactJSON(voiceAssistant), 160))
		}
		trace.StartLLM(messages)
	}

	ttsCtx, cancelTTS := context.WithCancel(ctx)
	defer cancelTTS()
	ttsSegments := make(chan string, 8)
	ttsResultCh := make(chan realtimeTTSResult, 1)
	go func() {
		ttsResultCh <- s.runRealtimeTTS(ttsCtx, tts, cfg, format, trace, ttsSegments)
	}()

	segmentsClosed := false
	closeSegments := func() {
		if !segmentsClosed {
			close(ttsSegments)
			segmentsClosed = true
		}
	}
	var earlyTTSResult *realtimeTTSResult
	waitForTTS := func() realtimeTTSResult {
		closeSegments()
		if earlyTTSResult != nil {
			return *earlyTTSResult
		}
		return <-ttsResultCh
	}
	var ttsCallbackErr error
	enqueueTTSSegment := func(segment string) error {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			return nil
		}
		if earlyTTSResult != nil {
			if earlyTTSResult.err != nil {
				return earlyTTSResult.err
			}
			return fmt.Errorf("tts pipeline stopped before accepting text")
		}
		select {
		case ttsSegments <- segment:
			return nil
		case result := <-ttsResultCh:
			earlyTTSResult = &result
			if result.err != nil {
				return result.err
			}
			return fmt.Errorf("tts pipeline stopped before accepting text")
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	var llmReply strings.Builder
	var segmenter realtimeTTSSegmenter
	err := llm.ChatStream(ctx, provider.ChatRequest{
		DeviceID:  cfg.ClientID,
		SessionID: cfg.SessionID,
		Messages:  messages,
	}, func(delta provider.ChatDelta) error {
		llmReply.WriteString(delta.Text)
		if trace != nil {
			trace.AddLLMDelta(delta.Text)
		}
		if err := s.writeEvent(protocol.EventLLMDelta, protocol.LLMDeltaPayload{
			Text: delta.Text,
		}); err != nil {
			return err
		}
		for _, segment := range segmenter.Add(delta.Text) {
			if err := enqueueTTSSegment(segment); err != nil {
				ttsCallbackErr = err
				return err
			}
		}
		return nil
	})
	if err != nil {
		cancelTTS()
		closeSegments()
		if ttsCallbackErr != nil && ctx.Err() == nil {
			if trace != nil {
				trace.Fail(ttsCallbackErr)
				s.clearTurnTrace(trace)
			}
			s.writeError("tts_failed", ttsCallbackErr.Error())
			s.writeEvent(eventResponseDone, responseDonePayload{Status: "failed"})
			return
		}
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
		if trace != nil {
			trace.AddEvent("llm.empty_speech", "provider completed without speech text")
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
		ttsResult := waitForTTS()
		if ttsResult.err != nil && ctx.Err() != context.Canceled {
			s.writeError("tts_failed", ttsResult.err.Error())
			s.writeEvent(eventResponseDone, responseDonePayload{Status: "failed", Text: replyText})
			if trace != nil {
				trace.Fail(ttsResult.err)
				s.clearTurnTrace(trace)
			}
			return
		}
		status := "completed"
		if ctx.Err() == context.Canceled {
			status = "cancelled"
		}
		s.writeEvent(eventResponseDone, responseDonePayload{Status: status, Text: replyText})
		if trace != nil {
			trace.AddEvent("response.done", status)
			trace.Complete()
			s.clearTurnTrace(trace)
		}
		return
	} else {
		for _, segment := range segmenter.Flush() {
			if err := enqueueTTSSegment(segment); err != nil {
				cancelTTS()
				if trace != nil {
					trace.Fail(err)
					s.clearTurnTrace(trace)
				}
				s.writeError("tts_failed", err.Error())
				s.writeEvent(eventResponseDone, responseDonePayload{Status: "failed", Text: replyText})
				return
			}
		}
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

	ttsResult := waitForTTS()
	if ttsResult.err == nil && ttsResult.chunkCount == 0 {
		ttsResult.err = fmt.Errorf("tts produced no audio")
	}
	ttsFormat := ttsResult.format
	if trace != nil && ttsResult.err == nil {
		trace.CompleteTTS(ttsFormat)
	}
	if ttsResult.err != nil {
		if ctx.Err() == context.Canceled {
			if trace != nil {
				trace.AddEvent("response.cancelled", "")
				trace.Complete()
				s.clearTurnTrace(trace)
			}
			s.writeEvent(eventResponseDone, responseDonePayload{Status: "cancelled", Text: replyText})
			return
		}
		s.writeError("tts_failed", ttsResult.err.Error())
		s.writeEvent(eventResponseDone, responseDonePayload{Status: "failed", Text: replyText})
		if trace != nil {
			trace.Fail(ttsResult.err)
			s.clearTurnTrace(trace)
		}
		return
	}

	status := "completed"
	if ctx.Err() == context.Canceled {
		status = "cancelled"
	}
	if trace != nil {
		trace.AddEvent("response.done", status)
		trace.Complete()
		s.clearTurnTrace(trace)
	}
	_ = s.writeEvent(eventResponseDone, responseDonePayload{
		Status:     status,
		Text:       replyText,
		ChunkCount: ttsResult.chunkCount,
		AudioBytes: ttsResult.audioBytes,
	})
}

func (s *session) proposeMCPCall(ctx context.Context, llm provider.LLMClient, cfg sessionConfig, transcript string, voiceAssistant service.VoiceAssistantMetadata, trace *debugtrace.Handle) error {
	transcript = strings.TrimSpace(transcript)
	if transcript == "" || isBlankASRText(transcript) {
		return nil
	}
	tools := mcpToolsFromVoiceAssistant(voiceAssistant)
	if len(tools) == 0 {
		return nil
	}

	proposal, err := buildMCPProposal(ctx, llm, cfg, transcript, voiceAssistant, tools)
	if err != nil {
		if trace != nil {
			trace.AddEvent("mcp.call.fallback", previewDetail(err.Error(), 160))
		}
		proposal = fallbackMCPProposal(transcript, tools, err)
	}
	if proposal.ProposalID == "" {
		proposal.ProposalID = s.nextID("mcp")
	}
	if proposal.Transcript == "" {
		proposal.Transcript = transcript
	}
	if proposal.Arguments == nil {
		proposal.Arguments = map[string]any{}
	}
	if trace != nil {
		trace.AddEvent("mcp.call.proposed", previewDetail(proposal.ToolName, 96))
	}
	return s.writeEvent(protocol.EventMCPCallProposed, proposal)
}

func buildMCPProposal(ctx context.Context, llm provider.LLMClient, cfg sessionConfig, transcript string, voiceAssistant service.VoiceAssistantMetadata, tools []string) (protocol.MCPCallProposedPayload, error) {
	if llm == nil {
		return protocol.MCPCallProposedPayload{}, fmt.Errorf("llm client is not configured")
	}
	planningCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	toolsJSON := compactJSON(tools)
	contextJSON := compactJSON(mcpPlanningContext(voiceAssistant))
	var raw strings.Builder
	err := llm.ChatStream(planningCtx, provider.ChatRequest{
		DeviceID:  cfg.ClientID,
		SessionID: cfg.SessionID,
		Messages: []provider.ChatMessage{
			{Role: "system", Content: mcpProposalSystemPrompt},
			{Role: "system", Content: "Available MCP tools JSON:\n" + toolsJSON},
			{Role: "system", Content: "Client context JSON:\n" + contextJSON},
			{Role: "user", Content: transcript},
		},
	}, func(delta provider.ChatDelta) error {
		raw.WriteString(delta.Text)
		return nil
	})
	if err != nil {
		return protocol.MCPCallProposedPayload{}, err
	}
	return parseMCPProposal(raw.String(), transcript, tools)
}

func parseMCPProposal(raw string, transcript string, tools []string) (protocol.MCPCallProposedPayload, error) {
	data, err := extractJSONObject(raw)
	if err != nil {
		return protocol.MCPCallProposedPayload{}, err
	}
	var parsed mcpProposalModelOutput
	if err := json.Unmarshal(data, &parsed); err != nil {
		return protocol.MCPCallProposedPayload{}, fmt.Errorf("invalid mcp proposal JSON: %w", err)
	}
	toolName := strings.TrimSpace(parsed.ToolName)
	if toolName == "" {
		toolName = strings.TrimSpace(parsed.Tool)
	}
	if toolName == "" {
		toolName = strings.TrimSpace(parsed.Name)
	}
	if !mcpToolAllowed(toolName, tools) {
		return protocol.MCPCallProposedPayload{}, fmt.Errorf("mcp proposal selected unavailable tool %q", toolName)
	}

	args := parsed.Arguments
	if len(args) == 0 {
		args = parsed.Args
	}
	if args == nil {
		args = map[string]any{}
	}
	if _, ok := args["raw_text"]; !ok {
		args["raw_text"] = transcript
	}

	confidence := 0.7
	if parsed.Confidence != nil {
		confidence = *parsed.Confidence
	}
	if confidence < 0 || confidence > 1 {
		return protocol.MCPCallProposedPayload{}, fmt.Errorf("mcp proposal confidence %.3f is outside 0..1", confidence)
	}
	return protocol.MCPCallProposedPayload{
		ToolName:             toolName,
		Arguments:            args,
		Transcript:           firstNonEmpty(parsed.Transcript, transcript),
		Confidence:           confidence,
		RequiresConfirmation: parsed.RequiresConfirmation,
		Reason:               strings.TrimSpace(parsed.Reason),
	}, nil
}

func fallbackMCPProposal(transcript string, tools []string, cause error) protocol.MCPCallProposedPayload {
	toolName := safestMCPTool(tools)
	reason := "LLM did not return a valid MCP proposal; falling back to raw transcript capture."
	if cause != nil && strings.TrimSpace(cause.Error()) != "" {
		reason = reason + " " + cause.Error()
	}
	return protocol.MCPCallProposedPayload{
		ToolName: toolName,
		Arguments: map[string]any{
			"raw_text": transcript,
		},
		Transcript:           transcript,
		Confidence:           0.35,
		RequiresConfirmation: true,
		Reason:               reason,
	}
}

func (s *session) runRealtimeTTS(ctx context.Context, tts provider.TTSClient, cfg sessionConfig, format provider.AudioFormat, trace *debugtrace.Handle, segments <-chan string) realtimeTTSResult {
	result := realtimeTTSResult{format: format}
	traceStarted := false

	for segment := range segments {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}
		if ctx.Err() != nil {
			result.err = ctx.Err()
			return result
		}

		if trace != nil {
			if !traceStarted {
				trace.StartTTS(segment, result.format)
				traceStarted = true
			} else {
				trace.AddEvent("tts.segment", previewDetail(segment, 96))
			}
		}
		s.rememberRecentSpeech(segment)

		streamID := s.nextID("tts")
		segmentFormat := result.format
		segmentStarted := false
		segmentChunks := 0
		segmentErr := tts.SynthesizeStream(ctx, provider.TTSRequest{
			DeviceID:  cfg.ClientID,
			SessionID: cfg.SessionID,
			Text:      segment,
			Format:    segmentFormat,
		}, func(chunk provider.AudioChunk) error {
			segmentFormat = mergeAudioFormat(segmentFormat, chunk.Format)
			result.format = mergeAudioFormat(result.format, chunk.Format)
			if !segmentStarted {
				if err := s.writeEvent(protocol.EventTTSStart, protocol.AudioStartPayload{
					StreamID:     streamID,
					Codec:        segmentFormat.Codec,
					SampleRateHz: segmentFormat.SampleRateHz,
					Channels:     segmentFormat.Channels,
				}); err != nil {
					return err
				}
				segmentStarted = true
			}
			segmentChunks++
			result.chunkCount++
			result.audioBytes += len(chunk.Data)
			if trace != nil {
				trace.AddTTSChunk(chunk.Data)
			}
			return s.conn.WriteBinary(chunk.Data)
		})
		if segmentErr != nil {
			result.err = segmentErr
			if segmentStarted {
				_ = s.writeEvent(protocol.EventTTSStop, protocol.AudioStopPayload{
					StreamID: streamID,
					LastSeq:  segmentChunks,
				})
			}
			return result
		}
		if segmentChunks == 0 {
			result.err = fmt.Errorf("tts produced no audio for segment")
			return result
		}
		if segmentStarted {
			_ = s.writeEvent(protocol.EventTTSStop, protocol.AudioStopPayload{
				StreamID: streamID,
				LastSeq:  segmentChunks,
			})
		}
	}

	return result
}

func (s *realtimeTTSSegmenter) Add(text string) []string {
	if text == "" {
		return nil
	}
	var segments []string
	for _, r := range text {
		s.buf.WriteRune(r)
		s.runeCount++
		if isRealtimeTTSSentenceBreak(r) || r == '\n' || s.runeCount >= realtimeTTSMaxRunes || (s.runeCount >= realtimeTTSSoftMinRunes && isRealtimeTTSSoftBreak(r)) {
			segments = appendFlushedRealtimeTTSSegment(segments, s)
		}
	}
	return segments
}

func (s *realtimeTTSSegmenter) Flush() []string {
	return appendFlushedRealtimeTTSSegment(nil, s)
}

func appendFlushedRealtimeTTSSegment(segments []string, s *realtimeTTSSegmenter) []string {
	segment := strings.TrimSpace(s.buf.String())
	if segment != "" {
		segments = append(segments, segment)
	}
	s.buf.Reset()
	s.runeCount = 0
	return segments
}

func isRealtimeTTSSentenceBreak(r rune) bool {
	switch r {
	case '.', '!', '?', ';', '。', '！', '？', '；':
		return true
	default:
		return false
	}
}

func isRealtimeTTSSoftBreak(r rune) bool {
	switch r {
	case ',', ':', '，', '、', '：':
		return true
	default:
		return false
	}
}

func isBlankASRText(text string) bool {
	normalized := strings.TrimSpace(strings.ToLower(text))
	if normalized == "" {
		return true
	}
	normalized = strings.Trim(normalized, " .。!！?？")
	switch normalized {
	case "[blank_audio]", "blank_audio", "[silence]", "silence":
		return true
	default:
		return false
	}
}

func (s *session) rememberRecentSpeech(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	now := s.now()
	normalized := normalizeEchoText(text)
	if len([]rune(normalized)) < echoMinRunes {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := now.Add(-recentSpeechWindow)
	next := make([]recentSpeechText, 0, len(s.recentSpeech)+1)
	for _, item := range s.recentSpeech {
		if item.at.Before(cutoff) {
			continue
		}
		next = append(next, item)
	}
	if len(next) > 0 && normalizeEchoText(next[len(next)-1].text) == normalized {
		next[len(next)-1] = recentSpeechText{text: text, at: now}
	} else {
		next = append(next, recentSpeechText{text: text, at: now})
	}
	if len(next) > recentSpeechLimit {
		next = next[len(next)-recentSpeechLimit:]
	}
	s.recentSpeech = next
}

func (s *session) recentSpeechPrompt(asrText string) string {
	items := s.recentSpeechSnapshot()
	if len(items) == 0 {
		return ""
	}
	lines := make([]string, 0, len(items))
	for _, item := range items {
		lines = append(lines, "- "+item.text)
	}
	return fmt.Sprintf(voiceEchoContextPrompt, strings.Join(lines, "\n"), strings.TrimSpace(asrText))
}

func (s *session) recentSpeechSnapshot() []recentSpeechText {
	now := s.now()
	cutoff := now.Add(-recentSpeechWindow)

	s.mu.Lock()
	defer s.mu.Unlock()

	items := make([]recentSpeechText, 0, len(s.recentSpeech))
	for _, item := range s.recentSpeech {
		if item.at.Before(cutoff) {
			continue
		}
		items = append(items, item)
	}
	if len(items) != len(s.recentSpeech) {
		s.recentSpeech = items
	}
	return append([]recentSpeechText(nil), items...)
}

func normalizeEchoText(text string) string {
	var out strings.Builder
	for _, r := range text {
		r = unicode.ToLower(r)
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
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
	cancelPartial := s.cancelPartialASRLocked()
	s.inboundTracker = protocol.StreamTracker{}
	s.inboundAudio = s.inboundAudio[:0]
	s.inboundSeq = 0
	s.speechActive = false
	s.silenceBytes = 0
	s.partialInFlight = false
	s.partialCancel = nil
	s.lastPartialAt = time.Time{}
	s.lastPartialText = ""
	s.lastPartialSize = 0
	s.mu.Unlock()
	if cancelPartial != nil {
		cancelPartial()
	}
}

func (s *session) cancelPartialASR() {
	s.mu.Lock()
	cancel := s.cancelPartialASRLocked()
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *session) cancelPartialASRLocked() context.CancelFunc {
	cancel := s.partialCancel
	s.partialRunID++
	s.partialCancel = nil
	s.partialInFlight = false
	return cancel
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
	return "AgenDash Universal Voice Layer context JSON. Use it only to resolve scope, target, UI surface, and confirmation safety. Do not read raw JSON back to the user unless asked.\n" + payload
}

func sharedSystemPrompt(metadata service.VoiceAssistantMetadata) string {
	if len(metadata.Metadata) == 0 {
		return ""
	}
	value, ok := metadata.Metadata["shared_system_prompt"]
	if !ok {
		return ""
	}
	prompt, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(prompt)
}

func mcpToolsFromVoiceAssistant(metadata service.VoiceAssistantMetadata) []string {
	seen := map[string]bool{}
	var tools []string
	for _, source := range []map[string]any{metadata.Metadata, metadata.UIContext} {
		for _, key := range []string{"mcp_tools", "available_mcp_tools", "tools"} {
			if source == nil {
				continue
			}
			tools = appendMCPTools(tools, seen, source[key])
		}
	}
	if metadata.AssistantIntent != nil {
		for _, key := range []string{"mcp_tools", "available_mcp_tools", "tools"} {
			tools = appendMCPTools(tools, seen, metadata.AssistantIntent.Args[key])
		}
	}
	return tools
}

func appendMCPTools(out []string, seen map[string]bool, value any) []string {
	switch typed := value.(type) {
	case string:
		return appendMCPTool(out, seen, typed)
	case []string:
		for _, item := range typed {
			out = appendMCPTool(out, seen, item)
		}
	case []any:
		for _, item := range typed {
			out = appendMCPTools(out, seen, item)
		}
	case map[string]any:
		for _, key := range []string{"tool_name", "name", "id"} {
			if name, ok := typed[key].(string); ok {
				out = appendMCPTool(out, seen, name)
			}
		}
	}
	return out
}

func appendMCPTool(out []string, seen map[string]bool, name string) []string {
	name = strings.TrimSpace(name)
	if name == "" || seen[name] {
		return out
	}
	seen[name] = true
	return append(out, name)
}

func mcpPlanningContext(metadata service.VoiceAssistantMetadata) map[string]any {
	out := map[string]any{
		"contract":         metadata.Contract,
		"ui_context":       metadata.UIContext,
		"assistant_intent": metadata.AssistantIntent,
	}
	if len(metadata.Metadata) > 0 {
		clean := make(map[string]any, len(metadata.Metadata))
		for key, value := range metadata.Metadata {
			if key == "shared_system_prompt" {
				continue
			}
			clean[key] = value
		}
		out["metadata"] = clean
	}
	return out
}

func extractJSONObject(raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end < start {
		return nil, fmt.Errorf("mcp proposal response did not contain a JSON object")
	}
	return []byte(raw[start : end+1]), nil
}

func mcpToolAllowed(toolName string, tools []string) bool {
	for _, tool := range tools {
		if toolName == tool {
			return true
		}
	}
	return false
}

func safestMCPTool(tools []string) string {
	if len(tools) == 0 {
		return ""
	}
	for _, preferred := range []string{"capture_text", "create_note", "note", "memory"} {
		for _, tool := range tools {
			if strings.Contains(strings.ToLower(tool), preferred) {
				return tool
			}
		}
	}
	return tools[0]
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
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

func mergeAudioFormat(current provider.AudioFormat, next provider.AudioFormat) provider.AudioFormat {
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
