package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/agendash/AgenSense/internal/saytts"
)

const (
	defaultAddr       = "127.0.0.1:18082"
	defaultChunkBytes = 1024
)

type speechInput string

func (s *speechInput) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		*s = speechInput(text)
		return nil
	}
	var parts []string
	if err := json.Unmarshal(data, &parts); err != nil {
		return err
	}
	*s = speechInput(strings.Join(parts, " "))
	return nil
}

type speechRequest struct {
	Model             string      `json:"model"`
	Input             speechInput `json:"input"`
	Voice             string      `json:"voice,omitempty"`
	ResponseFormat    string      `json:"response_format,omitempty"`
	Stream            bool        `json:"stream,omitempty"`
	StreamingInterval int         `json:"streaming_interval,omitempty"`
}

func main() {
	addr := envString("AGENSENSE_TTS_SAY_ADDR", defaultAddr)
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", handleHealthz)
	mux.HandleFunc("/v1/models", handleModels)
	mux.HandleFunc("/audio/speech", handleSpeech)
	mux.HandleFunc("/v1/audio/speech", handleSpeech)

	slog.Info("agensense tts say starting",
		"addr", addr,
		"voice", defaultVoice(),
		"sample_rate_hz", envInt("AGENSENSE_TTS_SAY_SAMPLE_RATE", saytts.DefaultSampleRateHz),
		"channels", envInt("AGENSENSE_TTS_SAY_CHANNELS", saytts.DefaultChannels),
	)
	if err := http.ListenAndServe(addr, mux); err != nil {
		slog.Error("agensense tts say stopped", "error", err)
		os.Exit(1)
	}
}

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"service": "agensense-tts-say",
	})
}

func handleModels(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
		return
	}
	voice := defaultVoice()
	writeJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data": []map[string]any{{
			"id":       voice,
			"object":   "model",
			"owned_by": "local",
		}},
	})
}

func handleSpeech(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
		return
	}

	var input speechRequest
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json"})
		return
	}
	text := strings.TrimSpace(string(input.Input))
	if text == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "empty_input"})
		return
	}
	responseFormat := strings.ToLower(strings.TrimSpace(input.ResponseFormat))
	if responseFormat == "" {
		responseFormat = "pcm"
	}
	if responseFormat != "pcm" && responseFormat != "wav" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "unsupported_response_format"})
		return
	}

	start := time.Now()
	wav, format, err := saytts.SynthesizeWAV(req.Context(), text, saytts.Options{
		Voice:        chooseVoice(input),
		SampleRateHz: envInt("AGENSENSE_TTS_SAY_SAMPLE_RATE", saytts.DefaultSampleRateHz),
		Channels:     envInt("AGENSENSE_TTS_SAY_CHANNELS", saytts.DefaultChannels),
		Rate:         envString("AGENSENSE_TTS_SAY_RATE", ""),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}

	audio := wav
	contentType := "audio/wav"
	codec := "wav"
	if responseFormat == "pcm" {
		pcm, pcmFormat, err := saytts.PCMFromWAV(wav)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		audio = pcm
		format = pcmFormat
		contentType = "application/octet-stream"
		codec = "pcm_s16le"
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("X-AgenSense-Audio-Codec", codec)
	w.Header().Set("X-AgenSense-Sample-Rate", strconv.Itoa(format.SampleRateHz))
	w.Header().Set("X-AgenSense-Channels", strconv.Itoa(format.Channels))
	if !input.Stream {
		w.Header().Set("Content-Length", strconv.Itoa(len(audio)))
	}
	w.WriteHeader(http.StatusOK)

	interval := streamInterval(input.StreamingInterval)
	if err := writeAudio(req.Context(), w, audio, envInt("AGENSENSE_TTS_SAY_CHUNK_BYTES", defaultChunkBytes), interval); err != nil {
		slog.Warn("tts response write failed", "error", err)
		return
	}
	slog.Info("tts speech completed",
		"model", input.Model,
		"voice", chooseVoice(input),
		"stream", input.Stream,
		"response_format", responseFormat,
		"total_ms", time.Since(start).Milliseconds(),
		"audio_bytes", len(audio),
		"text_chars", len([]rune(text)),
	)
}

func chooseVoice(input speechRequest) string {
	if voice := cleanVoice(input.Voice); voice != "" {
		return voice
	}
	model := cleanVoice(input.Model)
	if model != "" && !strings.Contains(strings.ToLower(model), "tts") {
		return model
	}
	return defaultVoice()
}

func cleanVoice(value string) string {
	value = strings.TrimSpace(value)
	switch strings.ToLower(value) {
	case "", "none", "-", "alloy":
		return ""
	default:
		return value
	}
}

func defaultVoice() string {
	return envString("AGENSENSE_TTS_SAY_VOICE", saytts.DefaultVoice)
}

func streamInterval(requested int) time.Duration {
	if requested > 0 {
		return time.Duration(requested) * time.Millisecond
	}
	ms := envInt("AGENSENSE_TTS_SAY_STREAM_INTERVAL_MS", 0)
	if ms <= 0 {
		return 0
	}
	return time.Duration(ms) * time.Millisecond
}

func writeAudio(ctx context.Context, w http.ResponseWriter, audio []byte, chunkBytes int, interval time.Duration) error {
	if chunkBytes <= 0 {
		chunkBytes = defaultChunkBytes
	}
	flusher, _ := w.(http.Flusher)
	if flusher != nil {
		flusher.Flush()
	}
	for start := 0; start < len(audio); start += chunkBytes {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		end := start + chunkBytes
		if end > len(audio) {
			end = len(audio)
		}
		if _, err := w.Write(audio[start:end]); err != nil {
			return err
		}
		if flusher != nil {
			flusher.Flush()
		}
		if interval > 0 && end < len(audio) {
			timer := time.NewTimer(interval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func envString(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func init() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))
}
