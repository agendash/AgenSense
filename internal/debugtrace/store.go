package debugtrace

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/agendash/AgenSense/internal/provider"
)

const (
	KindVoiceTurn = "voice_turn"
	KindASR       = "asr"
	KindLLM       = "llm"
	KindTTS       = "tts"

	SourceHTTP = "http"
	SourceWS   = "ws"

	defaultTraceLimit   = 64
	defaultMaxAssetSize = 8 << 20
	maxStoredDeltas     = 2048
)

type Store struct {
	limit        int
	maxAssetSize int
	assetDir     string

	seq atomic.Uint64

	mu     sync.RWMutex
	order  []string
	traces map[string]*traceRecord
}

type TraceMeta struct {
	DeviceID          string
	DeviceLabel       string
	HardwareSKU       string
	FirmwareVersion   string
	FirmwareChannel   string
	ClientID          string
	SessionID         string
	ProviderProfileID string
	HTTPPath          string
}

type Trace struct {
	ID                string       `json:"id"`
	Kind              string       `json:"kind"`
	Source            string       `json:"source"`
	Status            string       `json:"status"`
	HTTPPath          string       `json:"http_path,omitempty"`
	DeviceID          string       `json:"device_id,omitempty"`
	DeviceLabel       string       `json:"device_label,omitempty"`
	HardwareSKU       string       `json:"hardware_sku,omitempty"`
	FirmwareVersion   string       `json:"firmware_version,omitempty"`
	FirmwareChannel   string       `json:"firmware_channel,omitempty"`
	ClientID          string       `json:"client_id,omitempty"`
	SessionID         string       `json:"session_id,omitempty"`
	ProviderProfileID string       `json:"provider_profile_id,omitempty"`
	Error             string       `json:"error,omitempty"`
	StartedAt         time.Time    `json:"started_at"`
	UpdatedAt         time.Time    `json:"updated_at"`
	CompletedAt       time.Time    `json:"completed_at,omitempty"`
	TotalDurationMs   int64        `json:"total_duration_ms,omitempty"`
	InputAudioFormat  AudioFormat  `json:"input_audio_format,omitempty"`
	InputAudio        *AudioAsset  `json:"input_audio,omitempty"`
	ASR               *ASRStage    `json:"asr,omitempty"`
	LLM               *LLMStage    `json:"llm,omitempty"`
	TTS               *TTSStage    `json:"tts,omitempty"`
	Segments          []Segment    `json:"segments,omitempty"`
	Timeline          []TraceEvent `json:"timeline,omitempty"`
}

type AudioFormat struct {
	Codec        string `json:"codec,omitempty"`
	SampleRateHz int    `json:"sample_rate_hz,omitempty"`
	Channels     int    `json:"channels,omitempty"`
}

type AudioAsset struct {
	MIME      string `json:"mime"`
	Bytes     int    `json:"bytes"`
	Truncated bool   `json:"truncated,omitempty"`
}

type Segment struct {
	ID            string      `json:"id"`
	StreamID      string      `json:"stream_id,omitempty"`
	StartedAt     time.Time   `json:"started_at,omitempty"`
	CompletedAt   time.Time   `json:"completed_at,omitempty"`
	StartOffsetMs int64       `json:"start_offset_ms,omitempty"`
	EndOffsetMs   int64       `json:"end_offset_ms,omitempty"`
	DurationMs    int64       `json:"duration_ms,omitempty"`
	AudioBytes    int         `json:"audio_bytes,omitempty"`
	Format        AudioFormat `json:"format,omitempty"`
	StartLevel    float64     `json:"start_level,omitempty"`
	EndLevel      float64     `json:"end_level,omitempty"`
	PeakLevel     float64     `json:"peak_level,omitempty"`
	Text          string      `json:"text,omitempty"`
	Audio         *AudioAsset `json:"audio,omitempty"`
	AudioURL      string      `json:"audio_url,omitempty"`
}

type ASRStage struct {
	StartedAt   time.Time   `json:"started_at,omitempty"`
	CompletedAt time.Time   `json:"completed_at,omitempty"`
	DurationMs  int64       `json:"duration_ms,omitempty"`
	Text        string      `json:"text,omitempty"`
	AudioBytes  int         `json:"audio_bytes,omitempty"`
	Format      AudioFormat `json:"format,omitempty"`
}

type LLMStage struct {
	StartedAt       time.Time `json:"started_at,omitempty"`
	CompletedAt     time.Time `json:"completed_at,omitempty"`
	DurationMs      int64     `json:"duration_ms,omitempty"`
	FirstDeltaMs    int64     `json:"first_delta_ms,omitempty"`
	MessageCount    int       `json:"message_count,omitempty"`
	Messages        []Message `json:"messages,omitempty"`
	Deltas          []string  `json:"deltas,omitempty"`
	DeltasTruncated bool      `json:"deltas_truncated,omitempty"`
	DeltaCount      int       `json:"delta_count,omitempty"`
	DeltaChars      int       `json:"delta_chars,omitempty"`
	ResponseText    string    `json:"response_text,omitempty"`
}

type TTSStage struct {
	StartedAt    time.Time   `json:"started_at,omitempty"`
	CompletedAt  time.Time   `json:"completed_at,omitempty"`
	DurationMs   int64       `json:"duration_ms,omitempty"`
	FirstChunkMs int64       `json:"first_chunk_ms,omitempty"`
	Text         string      `json:"text,omitempty"`
	ChunkCount   int         `json:"chunk_count,omitempty"`
	AudioBytes   int         `json:"audio_bytes,omitempty"`
	Format       AudioFormat `json:"format,omitempty"`
	Audio        *AudioAsset `json:"audio,omitempty"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type TraceEvent struct {
	At     time.Time `json:"at"`
	Name   string    `json:"name"`
	Detail string    `json:"detail,omitempty"`
}

type Handle struct {
	store *Store
	id    string
}

type traceRecord struct {
	trace           Trace
	inputAudioData  []byte
	inputAudioMIME  string
	ttsAudioData    []byte
	ttsAudioMIME    string
	ttsRaw          []byte
	ttsRawTruncated bool
	segmentAudio    map[string]storedAsset
}

type storedAsset struct {
	data []byte
	mime string
}

func NewStore(limit int) *Store {
	return NewStoreWithAssetDir(limit, "")
}

func NewStoreWithAssetDir(limit int, assetDir string) *Store {
	if limit <= 0 {
		limit = defaultTraceLimit
	}
	return &Store{
		limit:        limit,
		maxAssetSize: defaultMaxAssetSize,
		assetDir:     assetDir,
		traces:       make(map[string]*traceRecord),
	}
}

func (s *Store) StartTrace(kind, source string, meta TraceMeta) *Handle {
	if s == nil {
		return nil
	}

	now := time.Now()
	id := fmt.Sprintf("trace_%d_%06d", now.UnixMilli(), s.seq.Add(1))
	record := &traceRecord{
		segmentAudio: make(map[string]storedAsset),
		trace: Trace{
			ID:                id,
			Kind:              kind,
			Source:            source,
			Status:            "running",
			HTTPPath:          meta.HTTPPath,
			DeviceID:          meta.DeviceID,
			DeviceLabel:       meta.DeviceLabel,
			HardwareSKU:       meta.HardwareSKU,
			FirmwareVersion:   meta.FirmwareVersion,
			FirmwareChannel:   meta.FirmwareChannel,
			ClientID:          meta.ClientID,
			SessionID:         meta.SessionID,
			ProviderProfileID: meta.ProviderProfileID,
			StartedAt:         now,
			UpdatedAt:         now,
		},
	}
	record.trace.Timeline = append(record.trace.Timeline, TraceEvent{
		At:   now,
		Name: "trace.started",
	})

	s.mu.Lock()
	defer s.mu.Unlock()

	s.order = append(s.order, id)
	s.traces[id] = record
	for len(s.order) > s.limit {
		evictID := s.order[0]
		s.order = s.order[1:]
		delete(s.traces, evictID)
		s.removeTraceAssets(evictID)
	}
	return &Handle{store: s, id: id}
}

func (s *Store) List() []Trace {
	if s == nil {
		return nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]Trace, 0, len(s.order))
	for i := len(s.order) - 1; i >= 0; i-- {
		if record := s.traces[s.order[i]]; record != nil {
			out = append(out, cloneTrace(record.trace))
		}
	}
	return out
}

func (s *Store) Get(id string) (Trace, bool) {
	if s == nil {
		return Trace{}, false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	record, ok := s.traces[id]
	if !ok {
		return Trace{}, false
	}
	return cloneTrace(record.trace), true
}

func (s *Store) ReadAsset(id, name string) ([]byte, string, bool) {
	if s == nil {
		return nil, "", false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	record, ok := s.traces[id]
	if !ok {
		return nil, "", false
	}

	switch name {
	case "input.wav":
		if len(record.inputAudioData) == 0 {
			return nil, "", false
		}
		return append([]byte(nil), record.inputAudioData...), record.inputAudioMIME, true
	case "tts.wav":
		if len(record.ttsAudioData) == 0 {
			return nil, "", false
		}
		return append([]byte(nil), record.ttsAudioData...), record.ttsAudioMIME, true
	default:
		asset, ok := record.segmentAudio[name]
		if !ok || len(asset.data) == 0 {
			return nil, "", false
		}
		return append([]byte(nil), asset.data...), asset.mime, true
	}
}

func (h *Handle) SetHTTPPath(path string) {
	h.mutate(func(record *traceRecord, now time.Time) {
		record.trace.HTTPPath = path
	})
}

func (h *Handle) SetProviderProfileID(profileID string) {
	h.mutate(func(record *traceRecord, now time.Time) {
		record.trace.ProviderProfileID = profileID
	})
}

func (h *Handle) AddEvent(name, detail string) {
	h.mutate(func(record *traceRecord, now time.Time) {
		record.trace.Timeline = append(record.trace.Timeline, TraceEvent{
			At:     now,
			Name:   name,
			Detail: previewText(detail, 160),
		})
	})
}

func (h *Handle) SetInputAudio(format provider.AudioFormat, audio []byte) {
	h.mutate(func(record *traceRecord, now time.Time) {
		record.trace.InputAudioFormat = fromProviderFormat(format)
		record.trace.InputAudio = nil
		record.inputAudioData = nil
		record.inputAudioMIME = ""

		data, mime, truncated := encodeAudioAsset(format, audio, h.store.maxAssetSize)
		record.trace.InputAudio = &AudioAsset{
			MIME:      mime,
			Bytes:     len(data),
			Truncated: truncated,
		}
		record.inputAudioData = data
		record.inputAudioMIME = mime
		if err := h.store.persistAsset(record, "input.wav", data); err != nil {
			record.trace.Timeline = append(record.trace.Timeline, TraceEvent{
				At:     now,
				Name:   "asset.persist_failed",
				Detail: err.Error(),
			})
		}
		record.trace.Timeline = append(record.trace.Timeline, TraceEvent{
			At:     now,
			Name:   "input.audio.received",
			Detail: fmt.Sprintf("%d bytes", len(audio)),
		})
	})
}

func (h *Handle) StartSegment(id, streamID string, format provider.AudioFormat, startOffsetMs int64, level float64) {
	h.mutate(func(record *traceRecord, now time.Time) {
		if id == "" {
			return
		}
		index := findSegment(record.trace.Segments, id)
		if index == -1 {
			record.trace.Segments = append(record.trace.Segments, Segment{ID: id})
			index = len(record.trace.Segments) - 1
		}
		segment := &record.trace.Segments[index]
		segment.StreamID = streamID
		segment.StartedAt = now
		segment.StartOffsetMs = startOffsetMs
		segment.Format = fromProviderFormat(format)
		segment.StartLevel = level
		segment.PeakLevel = maxFloat(segment.PeakLevel, level)
		record.trace.Timeline = append(record.trace.Timeline, TraceEvent{
			At:     now,
			Name:   "vad.segment.started",
			Detail: id,
		})
	})
}

func (h *Handle) CompleteSegment(id string, format provider.AudioFormat, audio []byte, startOffsetMs, endOffsetMs int64, endLevel, peakLevel float64) {
	h.mutate(func(record *traceRecord, now time.Time) {
		if id == "" {
			return
		}
		index := findSegment(record.trace.Segments, id)
		if index == -1 {
			record.trace.Segments = append(record.trace.Segments, Segment{
				ID:            id,
				StartOffsetMs: startOffsetMs,
			})
			index = len(record.trace.Segments) - 1
		}
		segment := &record.trace.Segments[index]
		segment.CompletedAt = now
		segment.EndOffsetMs = endOffsetMs
		segment.DurationMs = endOffsetMs - segment.StartOffsetMs
		if segment.DurationMs < 0 {
			segment.DurationMs = 0
		}
		segment.AudioBytes = len(audio)
		segment.Format = fromProviderFormat(format)
		segment.EndLevel = endLevel
		segment.PeakLevel = maxFloat(segment.PeakLevel, peakLevel)

		data, mime, truncated := encodeAudioAsset(format, audio, h.store.maxAssetSize)
		segment.Audio = &AudioAsset{
			MIME:      mime,
			Bytes:     len(data),
			Truncated: truncated,
		}
		name := segmentAssetName(id)
		if record.segmentAudio == nil {
			record.segmentAudio = make(map[string]storedAsset)
		}
		record.segmentAudio[name] = storedAsset{
			data: data,
			mime: mime,
		}
		if err := h.store.persistAsset(record, name, data); err != nil {
			record.trace.Timeline = append(record.trace.Timeline, TraceEvent{
				At:     now,
				Name:   "asset.persist_failed",
				Detail: err.Error(),
			})
		}
		record.trace.Timeline = append(record.trace.Timeline, TraceEvent{
			At:     now,
			Name:   "vad.segment.completed",
			Detail: fmt.Sprintf("%s · %d bytes · %d ms", id, len(audio), segment.DurationMs),
		})
	})
}

func (h *Handle) SetSegmentText(id, text string) {
	h.mutate(func(record *traceRecord, now time.Time) {
		index := findSegment(record.trace.Segments, id)
		if index == -1 {
			return
		}
		record.trace.Segments[index].Text = text
		record.trace.Timeline = append(record.trace.Timeline, TraceEvent{
			At:     now,
			Name:   "vad.segment.transcribed",
			Detail: previewText(text, 96),
		})
	})
}

func (h *Handle) StartASR(format provider.AudioFormat, audioBytes int) {
	h.mutate(func(record *traceRecord, now time.Time) {
		stage := ensureASR(record)
		stage.StartedAt = now
		stage.AudioBytes = audioBytes
		stage.Format = fromProviderFormat(format)
		record.trace.Timeline = append(record.trace.Timeline, TraceEvent{
			At:   now,
			Name: "asr.started",
		})
	})
}

func (h *Handle) CompleteASR(text string) {
	h.mutate(func(record *traceRecord, now time.Time) {
		stage := ensureASR(record)
		stage.CompletedAt = now
		if !stage.StartedAt.IsZero() {
			stage.DurationMs = now.Sub(stage.StartedAt).Milliseconds()
		}
		stage.Text = text
		record.trace.Timeline = append(record.trace.Timeline, TraceEvent{
			At:     now,
			Name:   "asr.completed",
			Detail: previewText(text, 96),
		})
	})
}

func (h *Handle) StartLLM(messages []provider.ChatMessage) {
	h.mutate(func(record *traceRecord, now time.Time) {
		stage := ensureLLM(record)
		stage.StartedAt = now
		stage.Messages = cloneMessages(messages)
		stage.MessageCount = len(messages)
		stage.Deltas = nil
		stage.DeltasTruncated = false
		stage.DeltaCount = 0
		stage.DeltaChars = 0
		stage.FirstDeltaMs = 0
		stage.ResponseText = ""
		record.trace.Timeline = append(record.trace.Timeline, TraceEvent{
			At:   now,
			Name: "llm.started",
		})
	})
}

func (h *Handle) AddLLMDelta(text string) {
	h.mutate(func(record *traceRecord, now time.Time) {
		stage := ensureLLM(record)
		if stage.DeltaCount == 0 && !stage.StartedAt.IsZero() {
			stage.FirstDeltaMs = now.Sub(stage.StartedAt).Milliseconds()
			record.trace.Timeline = append(record.trace.Timeline, TraceEvent{
				At:     now,
				Name:   "llm.first_delta",
				Detail: previewText(text, 64),
			})
		}
		stage.DeltaCount++
		stage.DeltaChars += len(text)
		if len(stage.Deltas) < maxStoredDeltas {
			stage.Deltas = append(stage.Deltas, text)
		} else {
			stage.DeltasTruncated = true
		}
		stage.ResponseText += text
	})
}

func (h *Handle) CompleteLLM(text string) {
	h.mutate(func(record *traceRecord, now time.Time) {
		stage := ensureLLM(record)
		stage.CompletedAt = now
		if !stage.StartedAt.IsZero() {
			stage.DurationMs = now.Sub(stage.StartedAt).Milliseconds()
		}
		if text != "" {
			stage.ResponseText = text
		}
		record.trace.Timeline = append(record.trace.Timeline, TraceEvent{
			At:     now,
			Name:   "llm.completed",
			Detail: previewText(stage.ResponseText, 96),
		})
	})
}

func (h *Handle) StartTTS(text string, format provider.AudioFormat) {
	h.mutate(func(record *traceRecord, now time.Time) {
		stage := ensureTTS(record)
		stage.StartedAt = now
		stage.Text = text
		stage.Format = fromProviderFormat(format)
		stage.ChunkCount = 0
		stage.AudioBytes = 0
		stage.FirstChunkMs = 0
		stage.Audio = nil
		record.ttsRaw = nil
		record.ttsRawTruncated = false
		record.ttsAudioData = nil
		record.ttsAudioMIME = ""
		record.trace.Timeline = append(record.trace.Timeline, TraceEvent{
			At:     now,
			Name:   "tts.started",
			Detail: previewText(text, 96),
		})
	})
}

func (h *Handle) AddTTSChunk(data []byte) {
	h.mutate(func(record *traceRecord, now time.Time) {
		stage := ensureTTS(record)
		if stage.ChunkCount == 0 && !stage.StartedAt.IsZero() {
			stage.FirstChunkMs = now.Sub(stage.StartedAt).Milliseconds()
			record.trace.Timeline = append(record.trace.Timeline, TraceEvent{
				At:   now,
				Name: "tts.first_chunk",
			})
		}
		stage.ChunkCount++
		stage.AudioBytes += len(data)
		record.ttsRaw, record.ttsRawTruncated = appendLimited(record.ttsRaw, data, h.store.maxAssetSize)
	})
}

func (h *Handle) CompleteTTS(format provider.AudioFormat) {
	h.mutate(func(record *traceRecord, now time.Time) {
		stage := ensureTTS(record)
		stage.CompletedAt = now
		if !stage.StartedAt.IsZero() {
			stage.DurationMs = now.Sub(stage.StartedAt).Milliseconds()
		}
		stage.Format = fromProviderFormat(format)
		data, mime, extraTruncated := encodeAudioAsset(format, record.ttsRaw, h.store.maxAssetSize)
		stage.Audio = &AudioAsset{
			MIME:      mime,
			Bytes:     len(data),
			Truncated: record.ttsRawTruncated || extraTruncated,
		}
		record.ttsAudioData = data
		record.ttsAudioMIME = mime
		if err := h.store.persistAsset(record, "tts.wav", data); err != nil {
			record.trace.Timeline = append(record.trace.Timeline, TraceEvent{
				At:     now,
				Name:   "asset.persist_failed",
				Detail: err.Error(),
			})
		}
		record.trace.Timeline = append(record.trace.Timeline, TraceEvent{
			At:   now,
			Name: "tts.completed",
		})
	})
}

func (h *Handle) Complete() {
	h.finish("", "completed")
}

func (h *Handle) Fail(err error) {
	if err == nil {
		h.finish("", "error")
		return
	}
	h.finish(err.Error(), "error")
}

func (h *Handle) finish(message, status string) {
	h.mutate(func(record *traceRecord, now time.Time) {
		record.trace.Status = status
		record.trace.Error = message
		record.trace.CompletedAt = now
		record.trace.TotalDurationMs = now.Sub(record.trace.StartedAt).Milliseconds()
		eventName := "trace.completed"
		if status == "error" {
			eventName = "trace.failed"
		}
		record.trace.Timeline = append(record.trace.Timeline, TraceEvent{
			At:     now,
			Name:   eventName,
			Detail: previewText(message, 96),
		})
	})
}

func (h *Handle) mutate(fn func(*traceRecord, time.Time)) {
	if h == nil || h.store == nil {
		return
	}

	h.store.mu.Lock()
	defer h.store.mu.Unlock()

	record, ok := h.store.traces[h.id]
	if !ok {
		return
	}

	now := time.Now()
	fn(record, now)
	record.trace.UpdatedAt = now
}

func ensureASR(record *traceRecord) *ASRStage {
	if record.trace.ASR == nil {
		record.trace.ASR = &ASRStage{}
	}
	return record.trace.ASR
}

func ensureLLM(record *traceRecord) *LLMStage {
	if record.trace.LLM == nil {
		record.trace.LLM = &LLMStage{}
	}
	return record.trace.LLM
}

func ensureTTS(record *traceRecord) *TTSStage {
	if record.trace.TTS == nil {
		record.trace.TTS = &TTSStage{}
	}
	return record.trace.TTS
}

func findSegment(segments []Segment, id string) int {
	for index, segment := range segments {
		if segment.ID == id {
			return index
		}
	}
	return -1
}

func cloneTrace(in Trace) Trace {
	out := in
	if in.InputAudio != nil {
		copy := *in.InputAudio
		out.InputAudio = &copy
	}
	if in.ASR != nil {
		copy := *in.ASR
		out.ASR = &copy
	}
	if in.LLM != nil {
		copy := *in.LLM
		copy.Messages = append([]Message(nil), in.LLM.Messages...)
		copy.Deltas = append([]string(nil), in.LLM.Deltas...)
		out.LLM = &copy
	}
	if in.TTS != nil {
		copy := *in.TTS
		if in.TTS.Audio != nil {
			audioCopy := *in.TTS.Audio
			copy.Audio = &audioCopy
		}
		out.TTS = &copy
	}
	out.Segments = append([]Segment(nil), in.Segments...)
	for index := range out.Segments {
		if out.Segments[index].Audio != nil {
			audioCopy := *out.Segments[index].Audio
			out.Segments[index].Audio = &audioCopy
		}
	}
	out.Timeline = append([]TraceEvent(nil), in.Timeline...)
	return out
}

func (s *Store) persistAsset(record *traceRecord, name string, data []byte) error {
	if s.assetDir == "" || len(data) == 0 {
		return nil
	}
	dir := filepath.Join(s.assetDir, record.trace.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, filepath.Base(name)), data, 0o644)
}

func (s *Store) removeTraceAssets(traceID string) {
	if s.assetDir == "" || traceID == "" {
		return
	}
	_ = os.RemoveAll(filepath.Join(s.assetDir, filepath.Base(traceID)))
}

func segmentAssetName(id string) string {
	return filepath.Base(id) + ".wav"
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func cloneMessages(messages []provider.ChatMessage) []Message {
	if len(messages) == 0 {
		return nil
	}
	out := make([]Message, 0, len(messages))
	for _, message := range messages {
		out = append(out, Message{
			Role:    message.Role,
			Content: message.Content,
		})
	}
	return out
}

func fromProviderFormat(format provider.AudioFormat) AudioFormat {
	return AudioFormat{
		Codec:        format.Codec,
		SampleRateHz: format.SampleRateHz,
		Channels:     format.Channels,
	}
}

func previewText(text string, limit int) string {
	if limit <= 0 || len(text) <= limit {
		return text
	}
	return text[:limit] + "..."
}

func appendLimited(dst, src []byte, max int) ([]byte, bool) {
	if max <= 0 {
		return append(dst, src...), false
	}
	if len(dst) >= max {
		return dst, true
	}
	remaining := max - len(dst)
	if len(src) > remaining {
		dst = append(dst, src[:remaining]...)
		return dst, true
	}
	return append(dst, src...), false
}

func encodeAudioAsset(format provider.AudioFormat, audio []byte, max int) ([]byte, string, bool) {
	if len(audio) == 0 {
		return nil, "audio/wav", false
	}
	if looksLikeWAV(audio) {
		data, truncated := appendLimited(nil, audio, max)
		return data, "audio/wav", truncated
	}
	if format.Codec == "" || format.Codec == "pcm_s16le" {
		data, truncated := pcmToWAV(format, audio, max)
		return data, "audio/wav", truncated
	}
	data, truncated := appendLimited(nil, audio, max)
	return data, "application/octet-stream", truncated
}

func looksLikeWAV(audio []byte) bool {
	return len(audio) >= 12 &&
		bytes.Equal(audio[:4], []byte("RIFF")) &&
		bytes.Equal(audio[8:12], []byte("WAVE"))
}

func pcmToWAV(format provider.AudioFormat, pcm []byte, max int) ([]byte, bool) {
	sampleRate := format.SampleRateHz
	if sampleRate <= 0 {
		sampleRate = 16000
	}
	channels := format.Channels
	if channels <= 0 {
		channels = 1
	}

	byteRate := sampleRate * channels * 2
	blockAlign := channels * 2
	dataSize := len(pcm)
	riffSize := 36 + dataSize

	header := bytes.NewBuffer(make([]byte, 0, 44))
	header.WriteString("RIFF")
	_ = binary.Write(header, binary.LittleEndian, uint32(riffSize))
	header.WriteString("WAVE")
	header.WriteString("fmt ")
	_ = binary.Write(header, binary.LittleEndian, uint32(16))
	_ = binary.Write(header, binary.LittleEndian, uint16(1))
	_ = binary.Write(header, binary.LittleEndian, uint16(channels))
	_ = binary.Write(header, binary.LittleEndian, uint32(sampleRate))
	_ = binary.Write(header, binary.LittleEndian, uint32(byteRate))
	_ = binary.Write(header, binary.LittleEndian, uint16(blockAlign))
	_ = binary.Write(header, binary.LittleEndian, uint16(16))
	header.WriteString("data")
	_ = binary.Write(header, binary.LittleEndian, uint32(dataSize))

	data, headerTruncated := appendLimited(nil, header.Bytes(), max)
	data, dataTruncated := appendLimited(data, pcm, max)
	return data, headerTruncated || dataTruncated
}
