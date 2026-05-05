package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/agendash/AgenSense/internal/protocol"
)

const (
	eventSessionUpdate  = "session.update"
	eventSessionReady   = "session.ready"
	eventResponseCreate = "response.create"
	eventResponseDone   = "response.done"
	eventASRPartial     = "asr.partial"
	eventVADState       = "vad.state"

	defaultBaseURL           = "http://127.0.0.1:8080"
	defaultAPIKey            = "demo-user-key"
	defaultProviderProfileID = "smoke-mock"
	defaultSampleRateHz      = 16000
	defaultChannels          = 1
)

type config struct {
	BaseURL              string
	APIKey               string
	ProviderProfileID    string
	EnsureMockProfile    bool
	InputSource          string
	SeedText             string
	ClientID             string
	DeviceLabel          string
	SessionID            string
	OutputDir            string
	Timeout              time.Duration
	SampleRateHz         int
	Channels             int
	LeadingSilenceMS     int
	SpeechMS             int
	TrailingSilenceMS    int
	ChunkMS              int
	Realtime             bool
	ExpectDebug          bool
	PrintEvents          bool
	AgenleashBaseURL     string
	AgenleashToken       string
	AgenleashAdapter     string
	AgenleashWorkspace   string
	AgenleashMessage     string
	AgenleashMessageMode string
	AgenleashWait        time.Duration
}

type smokeReport struct {
	BaseURL            string             `json:"base_url"`
	WSURL              string             `json:"ws_url"`
	ProviderProfileID  string             `json:"provider_profile_id"`
	MockProfileEnsured bool               `json:"mock_profile_ensured"`
	InputSource        string             `json:"input_source"`
	SeedText           string             `json:"seed_text,omitempty"`
	SeedTTS            *audioSummary      `json:"seed_tts,omitempty"`
	ClientID           string             `json:"client_id"`
	DeviceLabel        string             `json:"device_label"`
	SessionID          string             `json:"session_id"`
	StartedAt          time.Time          `json:"started_at"`
	CompletedAt        time.Time          `json:"completed_at,omitempty"`
	DurationMs         int64              `json:"duration_ms,omitempty"`
	Input              audioSummary       `json:"input"`
	ASRText            string             `json:"asr_text,omitempty"`
	ASRPartialCount    int                `json:"asr_partial_count,omitempty"`
	LLMDeltaCount      int                `json:"llm_delta_count,omitempty"`
	LLMText            string             `json:"llm_text,omitempty"`
	TTS                audioSummary       `json:"tts"`
	ResponseStatus     string             `json:"response_status,omitempty"`
	Events             []eventRecord      `json:"events"`
	DebugTrace         *debugTraceSummary `json:"debug_trace,omitempty"`
	Agenleash          *agenleashSummary  `json:"agenleash,omitempty"`
	OutputFiles        map[string]string  `json:"output_files,omitempty"`
	PrintEvents        bool               `json:"-"`
}

type audioSummary struct {
	Codec        string `json:"codec"`
	SampleRateHz int    `json:"sample_rate_hz"`
	Channels     int    `json:"channels"`
	Bytes        int    `json:"bytes"`
	ChunkCount   int    `json:"chunk_count,omitempty"`
	DurationMS   int    `json:"duration_ms,omitempty"`
}

type eventRecord struct {
	At        time.Time       `json:"at"`
	Direction string          `json:"direction"`
	Type      string          `json:"type"`
	Bytes     int             `json:"bytes,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

type debugTraceSummary struct {
	ID                string `json:"id"`
	Status            string `json:"status"`
	Kind              string `json:"kind"`
	Source            string `json:"source"`
	InputAudioBytes   int    `json:"input_audio_bytes,omitempty"`
	SegmentCount      int    `json:"segment_count"`
	SegmentAudioBytes int    `json:"segment_audio_bytes,omitempty"`
	ASRText           string `json:"asr_text,omitempty"`
	LLMDeltaCount     int    `json:"llm_delta_count,omitempty"`
	TTSChunkCount     int    `json:"tts_chunk_count,omitempty"`
	TTSAudioBytes     int    `json:"tts_audio_bytes,omitempty"`
	InputAssetBytes   int    `json:"input_asset_bytes,omitempty"`
	SegmentAssetBytes int    `json:"segment_asset_bytes,omitempty"`
	TTSAssetBytes     int    `json:"tts_asset_bytes,omitempty"`
}

type agenleashSummary struct {
	BaseURL           string    `json:"base_url"`
	Adapter           string    `json:"adapter"`
	Workspace         string    `json:"workspace"`
	SessionID         string    `json:"session_id,omitempty"`
	MessageID         string    `json:"message_id,omitempty"`
	State             string    `json:"state,omitempty"`
	LastOutputPreview string    `json:"last_output_preview,omitempty"`
	StartedAt         time.Time `json:"started_at,omitempty"`
	CompletedAt       time.Time `json:"completed_at,omitempty"`
	DurationMs        int64     `json:"duration_ms,omitempty"`
}

type wsClient struct {
	conn   net.Conn
	reader *bufio.Reader
}

type traceListResponse struct {
	Items []traceItem `json:"items"`
}

type traceItem struct {
	ID         string    `json:"id"`
	Kind       string    `json:"kind"`
	Source     string    `json:"source"`
	Status     string    `json:"status"`
	ClientID   string    `json:"client_id"`
	SessionID  string    `json:"session_id"`
	StartedAt  time.Time `json:"started_at"`
	InputAudio *struct {
		Bytes int `json:"bytes"`
	} `json:"input_audio"`
	InputAudioURL string `json:"input_audio_url"`
	ASR           *struct {
		Text string `json:"text"`
	} `json:"asr"`
	LLM *struct {
		DeltaCount   int    `json:"delta_count"`
		ResponseText string `json:"response_text"`
	} `json:"llm"`
	TTS *struct {
		ChunkCount int `json:"chunk_count"`
		Audio      *struct {
			Bytes int `json:"bytes"`
		} `json:"audio"`
	} `json:"tts"`
	TTSAudioURL string `json:"tts_audio_url"`
	Segments    []struct {
		ID         string `json:"id"`
		AudioBytes int    `json:"audio_bytes"`
		AudioURL   string `json:"audio_url"`
	} `json:"segments"`
}

func main() {
	cfg := parseFlags()
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()

	report, err := run(ctx, cfg)
	if report != nil && cfg.OutputDir != "" {
		if writeErr := writeReport(cfg.OutputDir, report); writeErr != nil {
			fmt.Fprintf(os.Stderr, "write report: %v\n", writeErr)
		}
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "AgenSense smoke failed: %v\n", err)
		os.Exit(1)
	}
	printSummary(report)
}

func parseFlags() config {
	now := time.Now().Format("20060102-150405")
	cfg := config{}
	flag.StringVar(&cfg.BaseURL, "base-url", envOrDefault("AGENSENSE_BASE_URL", defaultBaseURL), "AgenSense HTTP base URL.")
	flag.StringVar(&cfg.APIKey, "api-key", envOrDefault("AGENSENSE_API_KEY", defaultAPIKey), "AgenSense API key for provider and voice websocket calls.")
	flag.StringVar(&cfg.ProviderProfileID, "provider-profile-id", envOrDefault("AGENSENSE_PROVIDER_PROFILE", defaultProviderProfileID), "Provider profile used by the simulated AgenDash voice session.")
	flag.BoolVar(&cfg.EnsureMockProfile, "ensure-mock-provider", envOrDefaultBool("AGENSENSE_SMOKE_ENSURE_MOCK_PROVIDER", true), "Create or update provider_profile_id as a mock:// ASR/LLM/TTS profile before the run.")
	flag.StringVar(&cfg.InputSource, "input-source", envOrDefault("AGENSENSE_SMOKE_INPUT_SOURCE", "tts"), "Input audio source: tts or tone.")
	flag.StringVar(&cfg.SeedText, "seed-text", envOrDefault("AGENSENSE_SMOKE_SEED_TEXT", "Please summarize the active workspace and say AgenSense smoke ok."), "Text synthesized first when input-source=tts.")
	flag.StringVar(&cfg.ClientID, "client-id", envOrDefault("AGENSENSE_SMOKE_CLIENT_ID", "agendash-smoke"), "Client ID sent in session.update.")
	flag.StringVar(&cfg.DeviceLabel, "device-label", envOrDefault("AGENSENSE_SMOKE_DEVICE_LABEL", "AgenDash Smoke"), "Device/client label sent in session.update.")
	flag.StringVar(&cfg.SessionID, "session-id", envOrDefault("AGENSENSE_SMOKE_SESSION_ID", "smoke-"+now), "Session ID sent in session.update.")
	flag.StringVar(&cfg.OutputDir, "out", "", "Artifact directory. Defaults to tmp/smoke/<session-id>.")
	flag.DurationVar(&cfg.Timeout, "timeout", envOrDefaultDuration("AGENSENSE_SMOKE_TIMEOUT", 60*time.Second), "End-to-end timeout.")
	flag.IntVar(&cfg.SampleRateHz, "sample-rate", defaultSampleRateHz, "Input/output PCM sample rate.")
	flag.IntVar(&cfg.Channels, "channels", defaultChannels, "Input/output PCM channel count.")
	flag.IntVar(&cfg.LeadingSilenceMS, "leading-silence-ms", 240, "Leading silence duration in synthetic input audio.")
	flag.IntVar(&cfg.SpeechMS, "speech-ms", 900, "Synthetic speech tone duration.")
	flag.IntVar(&cfg.TrailingSilenceMS, "trailing-silence-ms", 320, "Trailing silence duration in synthetic input audio.")
	flag.IntVar(&cfg.ChunkMS, "chunk-ms", 40, "Client audio chunk duration.")
	flag.BoolVar(&cfg.Realtime, "realtime", true, "Sleep between audio chunks to mimic live microphone streaming.")
	flag.BoolVar(&cfg.ExpectDebug, "expect-debug", envOrDefaultBool("AGENSENSE_DEBUG", false), "Verify /debug/api/traces and audio assets after the voice turn. Defaults to AGENSENSE_DEBUG.")
	flag.BoolVar(&cfg.PrintEvents, "print-events", true, "Print the event sequence in the final summary.")
	flag.StringVar(&cfg.AgenleashBaseURL, "agenleash-base-url", envOrDefault("AGENLEASH_BASE_URL", ""), "Optional AgenLeash base URL. When set, the smoke starts a code-agent session and posts a workspace prompt.")
	flag.StringVar(&cfg.AgenleashToken, "agenleash-token", envOrDefault("AGENLEASH_TOKEN", ""), "Optional AgenLeash token.")
	flag.StringVar(&cfg.AgenleashAdapter, "agenleash-adapter", envOrDefault("AGENLEASH_ADAPTER", "codex"), "AgenLeash adapter to start.")
	flag.StringVar(&cfg.AgenleashWorkspace, "agenleash-workspace", envOrDefault("AGENLEASH_WORKSPACE", ""), "Workspace cwd for the optional AgenLeash session. Defaults to the current directory.")
	flag.StringVar(&cfg.AgenleashMessage, "agenleash-message", envOrDefault("AGENLEASH_MESSAGE", ""), "Message sent to the optional AgenLeash session. Defaults to a safe workspace smoke prompt using the ASR transcript.")
	flag.StringVar(&cfg.AgenleashMessageMode, "agenleash-message-mode", envOrDefault("AGENLEASH_MESSAGE_MODE", "start-arg"), "How to send the optional AgenLeash prompt: start-arg or post.")
	flag.DurationVar(&cfg.AgenleashWait, "agenleash-wait", envOrDefaultDuration("AGENLEASH_WAIT", 25*time.Second), "How long to poll the optional AgenLeash session after posting the message.")
	flag.Parse()

	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	cfg.APIKey = strings.TrimSpace(cfg.APIKey)
	cfg.ProviderProfileID = strings.TrimSpace(cfg.ProviderProfileID)
	cfg.InputSource = strings.ToLower(strings.TrimSpace(cfg.InputSource))
	cfg.SeedText = strings.TrimSpace(cfg.SeedText)
	cfg.ClientID = strings.TrimSpace(cfg.ClientID)
	cfg.DeviceLabel = strings.TrimSpace(cfg.DeviceLabel)
	cfg.SessionID = strings.TrimSpace(cfg.SessionID)
	cfg.AgenleashBaseURL = strings.TrimRight(strings.TrimSpace(cfg.AgenleashBaseURL), "/")
	cfg.AgenleashToken = strings.TrimSpace(cfg.AgenleashToken)
	cfg.AgenleashAdapter = strings.TrimSpace(cfg.AgenleashAdapter)
	cfg.AgenleashWorkspace = strings.TrimSpace(cfg.AgenleashWorkspace)
	cfg.AgenleashMessage = strings.TrimSpace(cfg.AgenleashMessage)
	cfg.AgenleashMessageMode = strings.ToLower(strings.TrimSpace(cfg.AgenleashMessageMode))
	if cfg.OutputDir == "" {
		cfg.OutputDir = filepath.Join("tmp", "smoke", cfg.SessionID)
	}
	if cfg.AgenleashWorkspace == "" {
		if cwd, err := os.Getwd(); err == nil {
			cfg.AgenleashWorkspace = cwd
		}
	}
	return cfg
}

func run(ctx context.Context, cfg config) (*smokeReport, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	started := time.Now()
	wsURL, err := voiceWSURL(cfg.BaseURL)
	if err != nil {
		return nil, err
	}
	report := &smokeReport{
		BaseURL:           cfg.BaseURL,
		WSURL:             wsURL,
		ProviderProfileID: cfg.ProviderProfileID,
		InputSource:       cfg.InputSource,
		SeedText:          cfg.SeedText,
		ClientID:          cfg.ClientID,
		DeviceLabel:       cfg.DeviceLabel,
		SessionID:         cfg.SessionID,
		StartedAt:         started,
		OutputFiles:       map[string]string{},
		PrintEvents:       cfg.PrintEvents,
	}

	client := &http.Client{Timeout: cfg.Timeout}
	if err := checkHealth(ctx, client, cfg.BaseURL); err != nil {
		return report, err
	}
	if cfg.EnsureMockProfile {
		if err := ensureMockProvider(ctx, client, cfg); err != nil {
			return report, err
		}
		report.MockProfileEnsured = true
	}
	inputAudio, inputSummary, seedSummary, seedAudio, err := buildInputAudio(ctx, client, cfg)
	if err != nil {
		return report, err
	}
	inputChunks := splitChunks(inputAudio, bytesPerChunk(inputSummary.SampleRateHz, inputSummary.Channels, cfg.ChunkMS))
	inputSummary.Bytes = len(inputAudio)
	inputSummary.ChunkCount = len(inputChunks)
	report.Input = inputSummary
	report.SeedTTS = seedSummary

	conn, err := dialWebSocket(ctx, wsURL, cfg.APIKey)
	if err != nil {
		return report, err
	}
	defer conn.close()

	if err := writeEvent(conn, cfg.SessionID, eventSessionUpdate, map[string]any{
		"client_id":           cfg.ClientID,
		"device_label":        cfg.DeviceLabel,
		"session_id":          cfg.SessionID,
		"provider_profile_id": cfg.ProviderProfileID,
		"format": map[string]any{
			"codec":          "pcm_s16le",
			"sample_rate_hz": inputSummary.SampleRateHz,
			"channels":       inputSummary.Channels,
		},
	}); err != nil {
		return report, err
	}
	report.Events = append(report.Events, eventRecord{At: time.Now(), Direction: "client", Type: eventSessionUpdate})

	if err := waitForEvent(ctx, conn, report, eventSessionReady); err != nil {
		return report, err
	}

	streamID := "input-001"
	if err := writeEvent(conn, cfg.SessionID, protocol.EventAudioStart, map[string]any{
		"stream_id":      streamID,
		"codec":          "pcm_s16le",
		"sample_rate_hz": inputSummary.SampleRateHz,
		"channels":       inputSummary.Channels,
	}); err != nil {
		return report, err
	}
	report.Events = append(report.Events, eventRecord{At: time.Now(), Direction: "client", Type: protocol.EventAudioStart})

	for _, chunk := range inputChunks {
		if err := writeClientFrame(conn.conn, 0x2, chunk); err != nil {
			return report, err
		}
		if cfg.Realtime {
			if err := sleepContext(ctx, time.Duration(cfg.ChunkMS)*time.Millisecond); err != nil {
				return report, err
			}
		}
	}

	if err := writeEvent(conn, cfg.SessionID, protocol.EventAudioStop, map[string]any{
		"stream_id": streamID,
		"last_seq":  len(inputChunks),
	}); err != nil {
		return report, err
	}
	report.Events = append(report.Events, eventRecord{At: time.Now(), Direction: "client", Type: protocol.EventAudioStop})

	asrText, err := waitForASRFinal(ctx, conn, report)
	if err != nil {
		return report, err
	}
	report.ASRText = asrText

	if err := writeEvent(conn, cfg.SessionID, eventResponseCreate, map[string]any{"text": asrText}); err != nil {
		return report, err
	}
	report.Events = append(report.Events, eventRecord{At: time.Now(), Direction: "client", Type: eventResponseCreate})

	ttsAudio, err := waitForResponse(ctx, conn, report)
	if err != nil {
		return report, err
	}
	report.TTS.Codec = firstNonEmpty(report.TTS.Codec, "pcm_s16le")
	if report.TTS.SampleRateHz == 0 {
		report.TTS.SampleRateHz = inputSummary.SampleRateHz
	}
	if report.TTS.Channels == 0 {
		report.TTS.Channels = inputSummary.Channels
	}
	report.TTS.Bytes = len(ttsAudio)

	if err := writeArtifacts(cfg.OutputDir, report, inputAudio, seedAudio, ttsAudio); err != nil {
		return report, err
	}

	if cfg.AgenleashBaseURL != "" {
		leash, err := runAgenleashSmoke(ctx, client, cfg, report.ASRText)
		if err != nil {
			return report, err
		}
		report.Agenleash = leash
	}

	if cfg.ExpectDebug {
		trace, err := waitForDebugTrace(ctx, client, cfg.BaseURL, cfg.SessionID, cfg.ClientID)
		if err != nil {
			return report, err
		}
		summary, err := verifyDebugAssets(ctx, client, cfg.BaseURL, trace)
		if err != nil {
			return report, err
		}
		report.DebugTrace = summary
	}

	report.CompletedAt = time.Now()
	report.DurationMs = report.CompletedAt.Sub(started).Milliseconds()
	return report, validateReport(report)
}

func (cfg config) validate() error {
	if cfg.BaseURL == "" {
		return errors.New("base-url is required")
	}
	if cfg.APIKey == "" {
		return errors.New("api-key is required")
	}
	if cfg.ProviderProfileID == "" {
		return errors.New("provider-profile-id is required")
	}
	if cfg.InputSource != "tone" && cfg.InputSource != "tts" {
		return errors.New("input-source must be tone or tts")
	}
	if cfg.InputSource == "tts" && cfg.SeedText == "" {
		return errors.New("seed-text is required when input-source=tts")
	}
	if cfg.ClientID == "" {
		return errors.New("client-id is required")
	}
	if cfg.SessionID == "" {
		return errors.New("session-id is required")
	}
	if cfg.SampleRateHz <= 0 || cfg.Channels <= 0 {
		return errors.New("sample-rate and channels must be > 0")
	}
	if cfg.ChunkMS <= 0 {
		return errors.New("chunk-ms must be > 0")
	}
	if cfg.SpeechMS <= 0 {
		return errors.New("speech-ms must be > 0")
	}
	if cfg.AgenleashBaseURL != "" {
		if cfg.AgenleashAdapter == "" {
			return errors.New("agenleash-adapter is required when agenleash-base-url is set")
		}
		if cfg.AgenleashWorkspace == "" {
			return errors.New("agenleash-workspace is required when agenleash-base-url is set")
		}
		if cfg.AgenleashWait < 0 {
			return errors.New("agenleash-wait must be >= 0")
		}
		if cfg.AgenleashMessageMode != "start-arg" && cfg.AgenleashMessageMode != "post" {
			return errors.New("agenleash-message-mode must be start-arg or post")
		}
	}
	return nil
}

func checkHealth(ctx context.Context, client *http.Client, baseURL string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/healthz", nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("healthz: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("healthz status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func ensureMockProvider(ctx context.Context, client *http.Client, cfg config) error {
	body := map[string]any{
		"id":           cfg.ProviderProfileID,
		"name":         "AgenDash Voice Smoke Mock",
		"asr_base_url": "mock://asr",
		"llm_base_url": "mock://llm",
		"tts_base_url": "mock://tts",
		"asr_model":    "mock-asr",
		"llm_model":    "mock-llm",
		"tts_model":    "mock-tts",
		"default":      false,
	}
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.BaseURL+"/v1/providers", bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("ensure mock provider: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("ensure mock provider status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func buildInputAudio(ctx context.Context, client *http.Client, cfg config) ([]byte, audioSummary, *audioSummary, []byte, error) {
	switch cfg.InputSource {
	case "tone":
		audio := synthesizeInputPCM(cfg.SampleRateHz, cfg.Channels, cfg.LeadingSilenceMS, cfg.SpeechMS, cfg.TrailingSilenceMS)
		return audio, audioSummary{
			Codec:        "pcm_s16le",
			SampleRateHz: cfg.SampleRateHz,
			Channels:     cfg.Channels,
			Bytes:        len(audio),
			DurationMS:   cfg.LeadingSilenceMS + cfg.SpeechMS + cfg.TrailingSilenceMS,
		}, nil, nil, nil
	case "tts":
		ttsAudio, ttsSummary, err := synthesizeSeedTTS(ctx, client, cfg)
		if err != nil {
			return nil, audioSummary{}, nil, nil, err
		}
		pcm, pcmSummary, err := normalizeInputPCM(ttsAudio, ttsSummary)
		if err != nil {
			return nil, audioSummary{}, &ttsSummary, nil, err
		}
		audio := make([]byte, 0, len(pcm)+bytesPerDuration(pcmSummary.SampleRateHz, pcmSummary.Channels, cfg.LeadingSilenceMS+cfg.TrailingSilenceMS))
		audio = append(audio, silencePCM(pcmSummary.SampleRateHz, pcmSummary.Channels, cfg.LeadingSilenceMS)...)
		audio = append(audio, pcm...)
		audio = append(audio, silencePCM(pcmSummary.SampleRateHz, pcmSummary.Channels, cfg.TrailingSilenceMS)...)
		pcmSummary.Bytes = len(audio)
		pcmSummary.DurationMS = durationMSForBytes(pcmSummary.SampleRateHz, pcmSummary.Channels, len(audio))
		return audio, pcmSummary, &ttsSummary, pcm, nil
	default:
		return nil, audioSummary{}, nil, nil, fmt.Errorf("unsupported input-source %q", cfg.InputSource)
	}
}

func synthesizeSeedTTS(ctx context.Context, client *http.Client, cfg config) ([]byte, audioSummary, error) {
	body := map[string]any{
		"provider_profile_id": cfg.ProviderProfileID,
		"client_id":           cfg.ClientID,
		"device_label":        cfg.DeviceLabel,
		"session_id":          cfg.SessionID + "-seed-tts",
		"text":                cfg.SeedText,
		"format": map[string]any{
			"codec":          "pcm_s16le",
			"sample_rate_hz": cfg.SampleRateHz,
			"channels":       cfg.Channels,
		},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, audioSummary{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.BaseURL+"/v1/tts/synthesize", bytes.NewReader(raw))
	if err != nil {
		return nil, audioSummary{}, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, audioSummary{}, fmt.Errorf("seed tts: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, audioSummary{}, fmt.Errorf("seed tts status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var decoded struct {
		ProviderProfileID string       `json:"provider_profile_id"`
		Format            audioSummary `json:"format"`
		AudioBase64       string       `json:"audio_base64"`
		ChunkCount        int          `json:"chunk_count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, audioSummary{}, err
	}
	audio, err := base64.StdEncoding.DecodeString(strings.TrimSpace(decoded.AudioBase64))
	if err != nil {
		return nil, audioSummary{}, fmt.Errorf("seed tts audio_base64: %w", err)
	}
	summary := decoded.Format
	if summary.Codec == "" {
		summary.Codec = "pcm_s16le"
	}
	if summary.SampleRateHz <= 0 {
		summary.SampleRateHz = cfg.SampleRateHz
	}
	if summary.Channels <= 0 {
		summary.Channels = cfg.Channels
	}
	summary.Bytes = len(audio)
	summary.ChunkCount = decoded.ChunkCount
	summary.DurationMS = durationMSForBytes(summary.SampleRateHz, summary.Channels, len(audio))
	return audio, summary, nil
}

func normalizeInputPCM(audio []byte, summary audioSummary) ([]byte, audioSummary, error) {
	if len(audio) == 0 {
		return nil, audioSummary{}, errors.New("seed tts returned empty audio")
	}
	if looksLikeWAV(audio) || strings.EqualFold(summary.Codec, "wav") {
		pcm, sampleRate, channels, err := decodePCMFromWAV(audio)
		if err != nil {
			return nil, audioSummary{}, err
		}
		return pcm, audioSummary{
			Codec:        "pcm_s16le",
			SampleRateHz: sampleRate,
			Channels:     channels,
			Bytes:        len(pcm),
			DurationMS:   durationMSForBytes(sampleRate, channels, len(pcm)),
		}, nil
	}
	if summary.Codec != "" && summary.Codec != "pcm_s16le" {
		return nil, audioSummary{}, fmt.Errorf("seed tts codec %q cannot be streamed to /v1/voice/ws", summary.Codec)
	}
	if summary.SampleRateHz <= 0 {
		summary.SampleRateHz = defaultSampleRateHz
	}
	if summary.Channels <= 0 {
		summary.Channels = defaultChannels
	}
	summary.Codec = "pcm_s16le"
	summary.Bytes = len(audio)
	summary.DurationMS = durationMSForBytes(summary.SampleRateHz, summary.Channels, len(audio))
	return audio, summary, nil
}

func dialWebSocket(ctx context.Context, rawURL, apiKey string) (*wsClient, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	host := parsed.Host
	dialer := net.Dialer{}
	var conn net.Conn
	switch parsed.Scheme {
	case "ws":
		conn, err = dialer.DialContext(ctx, "tcp", host)
	case "wss":
		tlsDialer := tls.Dialer{NetDialer: &dialer, Config: &tls.Config{ServerName: parsed.Hostname(), MinVersion: tls.VersionTLS12}}
		conn, err = tlsDialer.DialContext(ctx, "tcp", host)
	default:
		return nil, fmt.Errorf("unsupported websocket scheme %q", parsed.Scheme)
	}
	if err != nil {
		return nil, fmt.Errorf("websocket dial: %w", err)
	}

	secKey, err := websocketKey()
	if err != nil {
		conn.Close()
		return nil, err
	}
	request := fmt.Sprintf(
		"GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: %s\r\nAuthorization: Bearer %s\r\n\r\n",
		parsed.RequestURI(),
		parsed.Host,
		secKey,
		apiKey,
	)
	if _, err := io.WriteString(conn, request); err != nil {
		conn.Close()
		return nil, err
	}
	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, &http.Request{Method: http.MethodGet})
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("websocket handshake: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		conn.Close()
		return nil, fmt.Errorf("websocket handshake status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if got := strings.TrimSpace(resp.Header.Get("Sec-WebSocket-Accept")); got != computeAcceptKey(secKey) {
		conn.Close()
		return nil, fmt.Errorf("websocket handshake accept mismatch")
	}
	return &wsClient{conn: conn, reader: reader}, nil
}

func (c *wsClient) close() {
	_ = writeClientFrame(c.conn, 0x8, nil)
	_ = c.conn.Close()
}

func waitForEvent(ctx context.Context, conn *wsClient, report *smokeReport, want string) error {
	for {
		opcode, payload, err := readFrameContext(ctx, conn)
		if err != nil {
			return err
		}
		if opcode != 0x1 {
			report.Events = append(report.Events, eventRecord{At: time.Now(), Direction: "server", Type: "binary", Bytes: len(payload)})
			continue
		}
		event, err := decodeServerEvent(payload, report)
		if err != nil {
			return err
		}
		if event.Type == want {
			return nil
		}
	}
}

func waitForASRFinal(ctx context.Context, conn *wsClient, report *smokeReport) (string, error) {
	for {
		opcode, payload, err := readFrameContext(ctx, conn)
		if err != nil {
			return "", err
		}
		if opcode != 0x1 {
			report.Events = append(report.Events, eventRecord{At: time.Now(), Direction: "server", Type: "binary", Bytes: len(payload)})
			continue
		}
		event, err := decodeServerEvent(payload, report)
		if err != nil {
			return "", err
		}
		switch event.Type {
		case eventASRPartial:
			report.ASRPartialCount++
		case protocol.EventASRFinal:
			var asr protocol.ASRFinalPayload
			if err := event.DecodePayload(&asr); err != nil {
				return "", err
			}
			return strings.TrimSpace(asr.Text), nil
		}
	}
}

func waitForResponse(ctx context.Context, conn *wsClient, report *smokeReport) ([]byte, error) {
	var audio []byte
	for {
		opcode, payload, err := readFrameContext(ctx, conn)
		if err != nil {
			return audio, err
		}
		switch opcode {
		case 0x1:
			event, err := decodeServerEvent(payload, report)
			if err != nil {
				return audio, err
			}
			switch event.Type {
			case protocol.EventLLMDelta:
				var delta protocol.LLMDeltaPayload
				if err := event.DecodePayload(&delta); err != nil {
					return audio, err
				}
				report.LLMDeltaCount++
				report.LLMText += delta.Text
			case protocol.EventLLMDone:
				var done protocol.LLMDonePayload
				if err := event.DecodePayload(&done); err != nil {
					return audio, err
				}
				report.LLMText = strings.TrimSpace(done.Text)
			case protocol.EventTTSStart:
				var start protocol.AudioStartPayload
				if err := event.DecodePayload(&start); err != nil {
					return audio, err
				}
				report.TTS.Codec = start.Codec
				report.TTS.SampleRateHz = start.SampleRateHz
				report.TTS.Channels = start.Channels
			case protocol.EventTTSStop:
				var stop protocol.AudioStopPayload
				if err := event.DecodePayload(&stop); err != nil {
					return audio, err
				}
				report.TTS.ChunkCount = stop.LastSeq
			case eventResponseDone:
				var done struct {
					Status     string `json:"status"`
					Text       string `json:"text"`
					ChunkCount int    `json:"chunk_count"`
					AudioBytes int    `json:"audio_bytes"`
				}
				if err := event.DecodePayload(&done); err != nil {
					return audio, err
				}
				report.ResponseStatus = done.Status
				if done.Text != "" {
					report.LLMText = done.Text
				}
				if report.TTS.ChunkCount == 0 {
					report.TTS.ChunkCount = done.ChunkCount
				}
				return audio, nil
			}
		case 0x2:
			audio = append(audio, payload...)
			report.Events = append(report.Events, eventRecord{At: time.Now(), Direction: "server", Type: "tts.binary", Bytes: len(payload)})
		default:
			return audio, fmt.Errorf("unexpected websocket opcode %d", opcode)
		}
	}
}

func decodeServerEvent(payload []byte, report *smokeReport) (protocol.Envelope, error) {
	event, err := protocol.DecodeEvent(payload)
	if err != nil {
		return protocol.Envelope{}, err
	}
	report.Events = append(report.Events, eventRecord{
		At:        time.Now(),
		Direction: "server",
		Type:      event.Type,
		Payload:   append(json.RawMessage(nil), event.Payload...),
	})
	if event.Type == protocol.EventError {
		var serverError protocol.ErrorPayload
		if err := event.DecodePayload(&serverError); err != nil {
			return event, err
		}
		return event, fmt.Errorf("server error %s: %s", serverError.Code, serverError.Message)
	}
	return event, nil
}

func writeEvent(conn *wsClient, sessionID, eventType string, payload any) error {
	event, err := protocol.NewEvent(eventType, "", sessionID, payload)
	if err != nil {
		return err
	}
	data, err := protocol.MarshalEvent(event)
	if err != nil {
		return err
	}
	return writeClientFrame(conn.conn, 0x1, data)
}

func readFrameContext(ctx context.Context, conn *wsClient) (byte, []byte, error) {
	for {
		deadline := time.Now().Add(2 * time.Second)
		if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
			deadline = ctxDeadline
		}
		_ = conn.conn.SetReadDeadline(deadline)
		opcode, payload, err := readServerFrame(conn.reader)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() && ctx.Err() == nil {
				continue
			}
			if ctx.Err() != nil {
				return 0, nil, ctx.Err()
			}
			return 0, nil, err
		}
		switch opcode {
		case 0x9:
			if err := writeClientFrame(conn.conn, 0xA, payload); err != nil {
				return 0, nil, err
			}
			continue
		case 0x8:
			return opcode, payload, errors.New("websocket closed by server")
		default:
			return opcode, payload, nil
		}
	}
}

func writeClientFrame(conn net.Conn, opcode byte, payload []byte) error {
	mask := [4]byte{}
	if _, err := rand.Read(mask[:]); err != nil {
		return err
	}
	header := []byte{0x80 | opcode}
	switch {
	case len(payload) < 126:
		header = append(header, 0x80|byte(len(payload)))
	case len(payload) <= 0xFFFF:
		header = append(header, 0x80|126, byte(len(payload)>>8), byte(len(payload)))
	default:
		header = append(header, 0x80|127)
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(payload)))
		header = append(header, length[:]...)
	}

	masked := make([]byte, len(payload))
	for i := range payload {
		masked[i] = payload[i] ^ mask[i%4]
	}
	frame := append(header, mask[:]...)
	frame = append(frame, masked...)
	_, err := conn.Write(frame)
	return err
}

func readServerFrame(reader *bufio.Reader) (byte, []byte, error) {
	first, err := reader.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	if first&0x80 == 0 {
		return 0, nil, errors.New("fragmented websocket frames are not supported")
	}
	second, err := reader.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	opcode := first & 0x0F
	masked := second&0x80 != 0
	length := int64(second & 0x7F)
	switch length {
	case 126:
		var buf [2]byte
		if _, err := io.ReadFull(reader, buf[:]); err != nil {
			return 0, nil, err
		}
		length = int64(binary.BigEndian.Uint16(buf[:]))
	case 127:
		var buf [8]byte
		if _, err := io.ReadFull(reader, buf[:]); err != nil {
			return 0, nil, err
		}
		length = int64(binary.BigEndian.Uint64(buf[:]))
	}
	if length < 0 || length > 16<<20 {
		return 0, nil, fmt.Errorf("websocket frame too large: %d", length)
	}
	var mask [4]byte
	if masked {
		if _, err := io.ReadFull(reader, mask[:]); err != nil {
			return 0, nil, err
		}
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return 0, nil, err
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	return opcode, payload, nil
}

func waitForDebugTrace(ctx context.Context, client *http.Client, baseURL, sessionID, clientID string) (traceItem, error) {
	var lastErr error
	deadline := time.Now().Add(4 * time.Second)
	for {
		trace, err := fetchDebugTrace(ctx, client, baseURL, sessionID, clientID)
		if err == nil {
			return trace, nil
		}
		lastErr = err
		if time.Now().After(deadline) {
			break
		}
		if err := sleepContext(ctx, 200*time.Millisecond); err != nil {
			return traceItem{}, err
		}
	}
	return traceItem{}, lastErr
}

func fetchDebugTrace(ctx context.Context, client *http.Client, baseURL, sessionID, clientID string) (traceItem, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/debug/api/traces", nil)
	if err != nil {
		return traceItem{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return traceItem{}, fmt.Errorf("debug traces: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return traceItem{}, fmt.Errorf("debug traces status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var list traceListResponse
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return traceItem{}, err
	}
	sort.SliceStable(list.Items, func(i, j int) bool {
		return list.Items[i].StartedAt.After(list.Items[j].StartedAt)
	})
	for _, item := range list.Items {
		if item.Kind == "voice_turn" && item.Source == "ws" && item.SessionID == sessionID && item.ClientID == clientID {
			return item, nil
		}
	}
	return traceItem{}, fmt.Errorf("debug trace not found for session_id=%s client_id=%s", sessionID, clientID)
}

func verifyDebugAssets(ctx context.Context, client *http.Client, baseURL string, trace traceItem) (*debugTraceSummary, error) {
	summary := &debugTraceSummary{
		ID:            trace.ID,
		Status:        trace.Status,
		Kind:          trace.Kind,
		Source:        trace.Source,
		SegmentCount:  len(trace.Segments),
		LLMDeltaCount: 0,
	}
	if trace.InputAudio != nil {
		summary.InputAudioBytes = trace.InputAudio.Bytes
	}
	if trace.ASR != nil {
		summary.ASRText = trace.ASR.Text
	}
	if trace.LLM != nil {
		summary.LLMDeltaCount = trace.LLM.DeltaCount
	}
	if trace.TTS != nil {
		summary.TTSChunkCount = trace.TTS.ChunkCount
		if trace.TTS.Audio != nil {
			summary.TTSAudioBytes = trace.TTS.Audio.Bytes
		}
	}
	for _, segment := range trace.Segments {
		summary.SegmentAudioBytes += segment.AudioBytes
	}
	if trace.InputAudioURL != "" {
		size, err := fetchAssetSize(ctx, client, baseURL, trace.InputAudioURL)
		if err != nil {
			return summary, err
		}
		summary.InputAssetBytes = size
	}
	if trace.TTSAudioURL != "" {
		size, err := fetchAssetSize(ctx, client, baseURL, trace.TTSAudioURL)
		if err != nil {
			return summary, err
		}
		summary.TTSAssetBytes = size
	}
	for _, segment := range trace.Segments {
		if segment.AudioURL == "" {
			continue
		}
		size, err := fetchAssetSize(ctx, client, baseURL, segment.AudioURL)
		if err != nil {
			return summary, err
		}
		summary.SegmentAssetBytes += size
	}
	return summary, validateDebugTrace(summary)
}

func fetchAssetSize(ctx context.Context, client *http.Client, baseURL, assetPath string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+assetPath, nil)
	if err != nil {
		return 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("debug asset %s: %w", assetPath, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return 0, fmt.Errorf("debug asset %s status %d: %s", assetPath, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return 0, err
	}
	if len(data) <= 44 {
		return 0, fmt.Errorf("debug asset %s is too small: %d bytes", assetPath, len(data))
	}
	return len(data), nil
}

func runAgenleashSmoke(ctx context.Context, client *http.Client, cfg config, asrText string) (*agenleashSummary, error) {
	started := time.Now()
	summary := &agenleashSummary{
		BaseURL:   cfg.AgenleashBaseURL,
		Adapter:   cfg.AgenleashAdapter,
		Workspace: cfg.AgenleashWorkspace,
		StartedAt: started,
	}
	if err := checkAgenleashHealth(ctx, client, cfg); err != nil {
		return summary, err
	}
	message := agenleashPrompt(cfg, asrText)
	sessionID, state, err := startAgenleashSession(ctx, client, cfg, message)
	if err != nil {
		return summary, err
	}
	summary.SessionID = sessionID
	summary.State = state

	if cfg.AgenleashMessageMode == "post" {
		messageID, err := postAgenleashMessage(ctx, client, cfg, sessionID, message)
		if err != nil {
			return summary, err
		}
		summary.MessageID = messageID
	} else {
		summary.MessageID = "start-arg"
	}

	if cfg.AgenleashWait > 0 {
		state, preview, err := waitForAgenleashSession(ctx, client, cfg, sessionID, cfg.AgenleashWait)
		if err != nil {
			return summary, err
		}
		summary.State = state
		summary.LastOutputPreview = preview
	}
	summary.CompletedAt = time.Now()
	summary.DurationMs = summary.CompletedAt.Sub(started).Milliseconds()
	return summary, nil
}

func agenleashPrompt(cfg config, asrText string) string {
	if strings.TrimSpace(cfg.AgenleashMessage) != "" {
		return strings.TrimSpace(cfg.AgenleashMessage)
	}
	return fmt.Sprintf("Voice smoke ASR transcript: %q\nInspect the current workspace enough to confirm access, then reply with exactly: AgenSense workspace smoke ok", asrText)
}

func checkAgenleashHealth(ctx context.Context, client *http.Client, cfg config) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.AgenleashBaseURL+"/healthz", nil)
	if err != nil {
		return err
	}
	setAgenleashAuth(req, cfg.AgenleashToken)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("agenleash healthz: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("agenleash healthz status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func startAgenleashSession(ctx context.Context, client *http.Client, cfg config, prompt string) (string, string, error) {
	body := map[string]any{
		"adapter":    cfg.AgenleashAdapter,
		"cwd":        cfg.AgenleashWorkspace,
		"start_mode": "new",
	}
	if cfg.AgenleashMessageMode == "start-arg" && strings.TrimSpace(prompt) != "" {
		body["args"] = []string{strings.TrimSpace(prompt)}
	}
	data, err := json.Marshal(body)
	if err != nil {
		return "", "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.AgenleashBaseURL+"/api/v1/agent/start", bytes.NewReader(data))
	if err != nil {
		return "", "", err
	}
	setAgenleashAuth(req, cfg.AgenleashToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("agenleash start session: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", "", fmt.Errorf("agenleash start session status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var decoded struct {
		SessionID string `json:"session_id"`
		Session   struct {
			ID    string `json:"id"`
			State string `json:"state"`
		} `json:"session"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return "", "", err
	}
	sessionID := firstNonEmpty(decoded.SessionID, decoded.Session.ID)
	if sessionID == "" {
		return "", "", errors.New("agenleash start session response missing session_id")
	}
	return sessionID, decoded.Session.State, nil
}

func postAgenleashMessage(ctx context.Context, client *http.Client, cfg config, sessionID, message string) (string, error) {
	body := map[string]any{"content": message}
	data, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.AgenleashBaseURL+"/api/v1/sessions/"+url.PathEscape(sessionID)+"/messages", bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	setAgenleashAuth(req, cfg.AgenleashToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("agenleash post message: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("agenleash post message status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var decoded struct {
		MessageID string `json:"message_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return "", err
	}
	return decoded.MessageID, nil
}

func waitForAgenleashSession(ctx context.Context, client *http.Client, cfg config, sessionID string, wait time.Duration) (string, string, error) {
	deadline := time.Now().Add(wait)
	var lastState string
	var lastPreview string
	for {
		state, preview, err := getAgenleashSession(ctx, client, cfg, sessionID)
		if err != nil {
			return lastState, lastPreview, err
		}
		lastState, lastPreview = state, preview
		if state == "stopped" || state == "errored" || (preview != "" && state != "running" && state != "pending") {
			return lastState, lastPreview, nil
		}
		if time.Now().After(deadline) {
			return lastState, lastPreview, nil
		}
		if err := sleepContext(ctx, 750*time.Millisecond); err != nil {
			return lastState, lastPreview, err
		}
	}
}

func getAgenleashSession(ctx context.Context, client *http.Client, cfg config, sessionID string) (string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.AgenleashBaseURL+"/api/v1/sessions/"+url.PathEscape(sessionID), nil)
	if err != nil {
		return "", "", err
	}
	setAgenleashAuth(req, cfg.AgenleashToken)
	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("agenleash get session: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", "", fmt.Errorf("agenleash get session status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var decoded struct {
		Session struct {
			State             string `json:"state"`
			LastOutputPreview string `json:"last_output_preview"`
		} `json:"session"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return "", "", err
	}
	return decoded.Session.State, decoded.Session.LastOutputPreview, nil
}

func setAgenleashAuth(req *http.Request, token string) {
	if strings.TrimSpace(token) == "" {
		return
	}
	req.Header.Set("X-Agenleash-Token", strings.TrimSpace(token))
}

func validateReport(report *smokeReport) error {
	if report.ASRText == "" {
		return errors.New("missing asr.final text")
	}
	if report.LLMDeltaCount == 0 {
		return errors.New("missing llm.delta events")
	}
	if report.LLMText == "" {
		return errors.New("missing llm.done text")
	}
	if report.TTS.ChunkCount == 0 {
		return errors.New("missing tts.stop chunk count")
	}
	if report.TTS.Bytes == 0 {
		return errors.New("missing streamed TTS audio bytes")
	}
	if report.ResponseStatus != "completed" {
		return fmt.Errorf("response.done status = %q, want completed", report.ResponseStatus)
	}
	if report.Agenleash != nil {
		if report.Agenleash.SessionID == "" {
			return errors.New("agenleash smoke missing session_id")
		}
		if strings.EqualFold(report.Agenleash.State, "errored") {
			return errors.New("agenleash smoke session entered errored state")
		}
	}
	if !hasEvent(report.Events, eventVADState) {
		return errors.New("missing vad.state event")
	}
	if !hasVADState(report.Events, "speech_started") {
		return errors.New("missing vad.state speech_started event")
	}
	if !hasVADState(report.Events, "speech_stopped") {
		return errors.New("missing vad.state speech_stopped event")
	}
	return nil
}

func validateDebugTrace(summary *debugTraceSummary) error {
	if summary == nil {
		return errors.New("missing debug trace")
	}
	if summary.Status != "completed" {
		return fmt.Errorf("debug trace status = %q, want completed", summary.Status)
	}
	if summary.InputAudioBytes == 0 || summary.InputAssetBytes == 0 {
		return errors.New("debug trace missing input audio asset")
	}
	if summary.SegmentCount == 0 || summary.SegmentAudioBytes == 0 || summary.SegmentAssetBytes == 0 {
		return errors.New("debug trace missing VAD segment audio")
	}
	if summary.ASRText == "" {
		return errors.New("debug trace missing ASR text")
	}
	if summary.LLMDeltaCount == 0 {
		return errors.New("debug trace missing LLM deltas")
	}
	if summary.TTSChunkCount == 0 || summary.TTSAudioBytes == 0 || summary.TTSAssetBytes == 0 {
		return errors.New("debug trace missing TTS audio asset")
	}
	return nil
}

func writeArtifacts(outDir string, report *smokeReport, inputAudio, seedAudio, ttsAudio []byte) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	files := map[string][]byte{
		"input.pcm": inputAudio,
		"input.wav": wavBytes(inputAudio, report.Input.SampleRateHz, report.Input.Channels),
	}
	if len(seedAudio) > 0 && report.SeedTTS != nil {
		files["seed_tts.pcm"] = seedAudio
		files["seed_tts.wav"] = wavBytes(seedAudio, report.SeedTTS.SampleRateHz, report.SeedTTS.Channels)
	}
	if len(ttsAudio) > 0 {
		files["tts.pcm"] = ttsAudio
		files["tts.wav"] = wavBytes(ttsAudio, report.TTS.SampleRateHz, report.TTS.Channels)
	}
	for name, data := range files {
		path := filepath.Join(outDir, name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return err
		}
		report.OutputFiles[name] = path
	}
	return nil
}

func writeReport(outDir string, report *smokeReport) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	if report.OutputFiles == nil {
		report.OutputFiles = map[string]string{}
	}
	report.OutputFiles["report.json"] = filepath.Join(outDir, "report.json")
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	path := report.OutputFiles["report.json"]
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return nil
}

func printSummary(report *smokeReport) {
	if report == nil {
		return
	}
	fmt.Println("AgenSense voice smoke OK")
	fmt.Printf("base_url: %s\n", report.BaseURL)
	fmt.Printf("provider_profile_id: %s\n", report.ProviderProfileID)
	fmt.Printf("session_id: %s\n", report.SessionID)
	fmt.Printf("input_source: %s\n", report.InputSource)
	if report.SeedTTS != nil {
		fmt.Printf("seed_tts: %d chunks, %d bytes, %d ms\n", report.SeedTTS.ChunkCount, report.SeedTTS.Bytes, report.SeedTTS.DurationMS)
	}
	fmt.Printf("input: %d chunks, %d bytes, %d ms\n", report.Input.ChunkCount, report.Input.Bytes, report.Input.DurationMS)
	fmt.Printf("asr: %s\n", report.ASRText)
	fmt.Printf("llm: %d deltas, %d chars\n", report.LLMDeltaCount, len([]rune(report.LLMText)))
	fmt.Printf("tts: %d chunks, %d bytes\n", report.TTS.ChunkCount, report.TTS.Bytes)
	if report.Agenleash != nil {
		fmt.Printf("agenleash: session=%s state=%s workspace=%s\n", report.Agenleash.SessionID, report.Agenleash.State, report.Agenleash.Workspace)
		if report.Agenleash.LastOutputPreview != "" {
			fmt.Printf("agenleash_preview: %s\n", report.Agenleash.LastOutputPreview)
		}
	}
	if report.DebugTrace != nil {
		fmt.Printf("debug_trace: %s, segments=%d, input_asset=%d bytes, tts_asset=%d bytes\n",
			report.DebugTrace.ID,
			report.DebugTrace.SegmentCount,
			report.DebugTrace.InputAssetBytes,
			report.DebugTrace.TTSAssetBytes,
		)
	}
	if len(report.OutputFiles) > 0 {
		keys := make([]string, 0, len(report.OutputFiles))
		for key := range report.OutputFiles {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			fmt.Printf("%s: %s\n", key, report.OutputFiles[key])
		}
	}
	if report.PrintEvents && len(report.Events) > 0 {
		types := compactServerEventTypes(report.Events)
		fmt.Printf("server_events: %s\n", strings.Join(types, " -> "))
	}
}

func synthesizeInputPCM(sampleRateHz, channels, leadingSilenceMS, speechMS, trailingSilenceMS int) []byte {
	var out []byte
	out = append(out, silencePCM(sampleRateHz, channels, leadingSilenceMS)...)
	out = append(out, tonePCM(sampleRateHz, channels, speechMS, 220, 0.24)...)
	out = append(out, silencePCM(sampleRateHz, channels, trailingSilenceMS)...)
	return out
}

func silencePCM(sampleRateHz, channels, durationMS int) []byte {
	samples := sampleRateHz * durationMS / 1000
	return make([]byte, samples*channels*2)
}

func tonePCM(sampleRateHz, channels, durationMS int, frequency, amplitude float64) []byte {
	samples := sampleRateHz * durationMS / 1000
	data := make([]byte, samples*channels*2)
	for i := 0; i < samples; i++ {
		phase := 2 * math.Pi * frequency * float64(i) / float64(sampleRateHz)
		value := int16(math.Sin(phase) * math.MaxInt16 * amplitude)
		for ch := 0; ch < channels; ch++ {
			offset := (i*channels + ch) * 2
			binary.LittleEndian.PutUint16(data[offset:offset+2], uint16(value))
		}
	}
	return data
}

func splitChunks(data []byte, chunkBytes int) [][]byte {
	if chunkBytes <= 0 {
		return [][]byte{data}
	}
	chunks := make([][]byte, 0, (len(data)+chunkBytes-1)/chunkBytes)
	for start := 0; start < len(data); start += chunkBytes {
		end := start + chunkBytes
		if end > len(data) {
			end = len(data)
		}
		chunks = append(chunks, data[start:end])
	}
	return chunks
}

func bytesPerChunk(sampleRateHz, channels, chunkMS int) int {
	bytes := sampleRateHz * channels * 2 * chunkMS / 1000
	if bytes <= 0 {
		return sampleRateHz * channels * 2 / 10
	}
	return bytes
}

func bytesPerDuration(sampleRateHz, channels, durationMS int) int {
	if sampleRateHz <= 0 {
		sampleRateHz = defaultSampleRateHz
	}
	if channels <= 0 {
		channels = defaultChannels
	}
	if durationMS <= 0 {
		return 0
	}
	return sampleRateHz * channels * 2 * durationMS / 1000
}

func durationMSForBytes(sampleRateHz, channels, bytes int) int {
	if sampleRateHz <= 0 {
		sampleRateHz = defaultSampleRateHz
	}
	if channels <= 0 {
		channels = defaultChannels
	}
	if bytes <= 0 {
		return 0
	}
	return bytes * 1000 / (sampleRateHz * channels * 2)
}

func looksLikeWAV(audio []byte) bool {
	return len(audio) >= 12 &&
		bytes.Equal(audio[:4], []byte("RIFF")) &&
		bytes.Equal(audio[8:12], []byte("WAVE"))
}

func decodePCMFromWAV(audio []byte) ([]byte, int, int, error) {
	if len(audio) < 44 || !looksLikeWAV(audio) {
		return nil, 0, 0, errors.New("invalid wav audio")
	}
	var sampleRate int
	var channels int
	var bitsPerSample int
	var pcm []byte
	for offset := 12; offset+8 <= len(audio); {
		chunkID := string(audio[offset : offset+4])
		chunkSize := int(binary.LittleEndian.Uint32(audio[offset+4 : offset+8]))
		chunkStart := offset + 8
		chunkEnd := chunkStart + chunkSize
		if chunkSize < 0 || chunkEnd > len(audio) {
			return nil, 0, 0, errors.New("invalid wav chunk size")
		}
		switch chunkID {
		case "fmt ":
			if chunkSize < 16 {
				return nil, 0, 0, errors.New("invalid wav fmt chunk")
			}
			audioFormat := binary.LittleEndian.Uint16(audio[chunkStart : chunkStart+2])
			if audioFormat != 1 {
				return nil, 0, 0, fmt.Errorf("unsupported wav format %d", audioFormat)
			}
			channels = int(binary.LittleEndian.Uint16(audio[chunkStart+2 : chunkStart+4]))
			sampleRate = int(binary.LittleEndian.Uint32(audio[chunkStart+4 : chunkStart+8]))
			bitsPerSample = int(binary.LittleEndian.Uint16(audio[chunkStart+14 : chunkStart+16]))
		case "data":
			pcm = append([]byte(nil), audio[chunkStart:chunkEnd]...)
		}
		offset = chunkEnd
		if offset%2 != 0 {
			offset++
		}
	}
	if sampleRate <= 0 || channels <= 0 {
		return nil, 0, 0, errors.New("wav fmt metadata is missing")
	}
	if bitsPerSample != 16 {
		return nil, 0, 0, fmt.Errorf("unsupported wav bits per sample %d", bitsPerSample)
	}
	if len(pcm) == 0 {
		return nil, 0, 0, errors.New("wav data chunk is missing")
	}
	return pcm, sampleRate, channels, nil
}

func wavBytes(pcm []byte, sampleRateHz, channels int) []byte {
	if sampleRateHz <= 0 {
		sampleRateHz = defaultSampleRateHz
	}
	if channels <= 0 {
		channels = defaultChannels
	}
	byteRate := sampleRateHz * channels * 2
	blockAlign := channels * 2
	dataSize := len(pcm)
	riffSize := 36 + dataSize
	var buf bytes.Buffer
	buf.WriteString("RIFF")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(riffSize))
	buf.WriteString("WAVE")
	buf.WriteString("fmt ")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(16))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(channels))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(sampleRateHz))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(byteRate))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(blockAlign))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(16))
	buf.WriteString("data")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(dataSize))
	buf.Write(pcm)
	return buf.Bytes()
}

func voiceWSURL(baseURL string) (string, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	switch parsed.Scheme {
	case "http":
		parsed.Scheme = "ws"
	case "https":
		parsed.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", fmt.Errorf("unsupported base-url scheme %q", parsed.Scheme)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/v1/voice/ws"
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func websocketKey() (string, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(nonce[:]), nil
}

func computeAcceptKey(key string) string {
	sum := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	return base64.StdEncoding.EncodeToString(sum[:])
}

func hasEvent(events []eventRecord, eventType string) bool {
	for _, event := range events {
		if event.Type == eventType {
			return true
		}
	}
	return false
}

func hasVADState(events []eventRecord, state string) bool {
	for _, event := range events {
		if event.Type != eventVADState || len(event.Payload) == 0 {
			continue
		}
		var payload struct {
			State string `json:"state"`
		}
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			continue
		}
		if payload.State == state {
			return true
		}
	}
	return false
}

func compactServerEventTypes(events []eventRecord) []string {
	var out []string
	var last string
	count := 0
	flush := func() {
		if last == "" {
			return
		}
		if count > 1 {
			out = append(out, fmt.Sprintf("%s x%d", last, count))
		} else {
			out = append(out, last)
		}
	}
	for _, event := range events {
		if event.Direction != "server" {
			continue
		}
		if event.Type == last {
			count++
			continue
		}
		flush()
		last = event.Type
		count = 1
	}
	flush()
	return out
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envOrDefaultBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	switch strings.ToLower(value) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func envOrDefaultDuration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
