package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"github.com/agendash/AgenSense/internal/textnorm"
)

const (
	defaultOpenAIASRPrompt = "Chinese transcripts must use Simplified Chinese. Preserve English words, product names, code identifiers, commands, and paths."
	fallbackOpenAITTSVoice = "alloy"
)

type OpenAICompatibleASR struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	model      string
}

func NewOpenAICompatibleASR(httpClient *http.Client, baseURL, apiKey, model string) *OpenAICompatibleASR {
	return &OpenAICompatibleASR{
		httpClient: ensureHTTPClient(httpClient),
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     strings.TrimSpace(apiKey),
		model:      defaultString(strings.TrimSpace(model), "gpt-4o-mini-transcribe"),
	}
}

func (c *OpenAICompatibleASR) Transcribe(ctx context.Context, req TranscribeRequest) (TranscribeResponse, error) {
	start := time.Now()
	audioData, err := pcmToWAV(req.Format, req.Audio)
	if err != nil {
		return TranscribeResponse{}, err
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("model", c.model); err != nil {
		return TranscribeResponse{}, err
	}
	if language := openAIASRLanguage(); language != "" {
		if err := writer.WriteField("language", language); err != nil {
			return TranscribeResponse{}, err
		}
	}
	if prompt := openAIASRPrompt(); prompt != "" {
		if err := writer.WriteField("prompt", prompt); err != nil {
			return TranscribeResponse{}, err
		}
	}
	fileWriter, err := writer.CreateFormFile("file", "audio.wav")
	if err != nil {
		return TranscribeResponse{}, err
	}
	if _, err := fileWriter.Write(audioData); err != nil {
		return TranscribeResponse{}, err
	}
	if err := writer.Close(); err != nil {
		return TranscribeResponse{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, joinOpenAIPath(c.baseURL, "/audio/transcriptions"), &body)
	if err != nil {
		return TranscribeResponse{}, err
	}
	httpReq.Header.Set("Content-Type", writer.FormDataContentType())
	setBearerIfPresent(httpReq, c.apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return TranscribeResponse{}, err
	}
	defer resp.Body.Close()
	headersAt := time.Now()
	if resp.StatusCode/100 != 2 {
		return TranscribeResponse{}, decodeHTTPError(resp)
	}

	var out struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return TranscribeResponse{}, err
	}
	text := textnorm.NormalizeChineseScript(strings.TrimSpace(out.Text), os.Getenv("AGENSENSE_ASR_CHINESE_SCRIPT"))
	if text == "" {
		return TranscribeResponse{}, fmt.Errorf("provider: empty ASR response")
	}
	slog.InfoContext(ctx, "provider asr completed",
		"provider", "openai-compatible",
		"model", c.model,
		"ttfb_ms", headersAt.Sub(start).Milliseconds(),
		"total_ms", time.Since(start).Milliseconds(),
		"audio_bytes", len(req.Audio),
		"text_chars", len(text),
	)
	return TranscribeResponse{Text: text}, nil
}

func openAIASRLanguage() string {
	return strings.TrimSpace(os.Getenv("AGENSENSE_OPENAI_ASR_LANGUAGE"))
}

func openAIASRPrompt() string {
	if prompt, ok := os.LookupEnv("AGENSENSE_OPENAI_ASR_PROMPT"); ok {
		return strings.TrimSpace(prompt)
	}
	return defaultOpenAIASRPrompt
}

func openAIReasoningEffort() string {
	return strings.TrimSpace(os.Getenv("AGENSENSE_OPENAI_REASONING_EFFORT"))
}

type OpenAICompatibleLLM struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	model      string
}

func NewOpenAICompatibleLLM(httpClient *http.Client, baseURL, apiKey, model string) *OpenAICompatibleLLM {
	return &OpenAICompatibleLLM{
		httpClient: ensureHTTPClient(httpClient),
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     strings.TrimSpace(apiKey),
		model:      defaultString(strings.TrimSpace(model), "gpt-4.1-mini"),
	}
}

func (c *OpenAICompatibleLLM) ChatStream(ctx context.Context, req ChatRequest, cb func(ChatDelta) error) error {
	start := time.Now()
	messages := normalizeChatMessagesForOpenAI(req.Messages)
	body := map[string]any{
		"model":    c.model,
		"messages": messages,
		"stream":   true,
	}
	if effort := openAIReasoningEffort(); effort != "" {
		body["reasoning_effort"] = effort
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, joinOpenAIPath(c.baseURL, "/chat/completions"), bytes.NewReader(raw))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	setBearerIfPresent(httpReq, c.apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	headersAt := time.Now()
	if resp.StatusCode/100 != 2 {
		return decodeHTTPError(resp)
	}

	scanner := bufio.NewScanner(resp.Body)
	firstDeltaAt := time.Time{}
	deltaCount := 0
	deltaChars := 0
	firstDeltaText := ""
	droppedOpeningDuplicate := false
	logCompletion := func() {
		firstDeltaMS := int64(-1)
		if !firstDeltaAt.IsZero() {
			firstDeltaMS = firstDeltaAt.Sub(start).Milliseconds()
		}
		slog.InfoContext(ctx, "provider llm completed",
			"provider", "openai-compatible",
			"model", c.model,
			"headers_ms", headersAt.Sub(start).Milliseconds(),
			"first_delta_ms", firstDeltaMS,
			"total_ms", time.Since(start).Milliseconds(),
			"delta_count", deltaCount,
			"delta_chars", deltaChars,
			"message_count", len(messages),
		)
	}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			logCompletion()
			return nil
		}

		var chunk struct {
			Error *struct {
				Message string `json:"message"`
				Type    string `json:"type"`
				Code    any    `json:"code"`
			} `json:"error"`
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return err
		}
		if chunk.Error != nil {
			message := strings.TrimSpace(chunk.Error.Message)
			if message == "" {
				message = "provider stream error"
			}
			return fmt.Errorf("provider: stream error: %s", message)
		}
		for _, choice := range chunk.Choices {
			text := choice.Delta.Content
			if text == "" {
				text = choice.Message.Content
			}
			if strings.TrimSpace(text) == "" {
				continue
			}
			if deltaCount == 1 && !droppedOpeningDuplicate && text == firstDeltaText {
				droppedOpeningDuplicate = true
				continue
			}
			if firstDeltaAt.IsZero() {
				firstDeltaAt = time.Now()
				firstDeltaText = text
			}
			deltaCount++
			deltaChars += len(text)
			if err := cb(ChatDelta{Text: text}); err != nil {
				return err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	logCompletion()
	return nil
}

type OpenAICompatibleMultimodal struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	model      string
}

func NewOpenAICompatibleMultimodal(httpClient *http.Client, baseURL, apiKey, model string) *OpenAICompatibleMultimodal {
	return &OpenAICompatibleMultimodal{
		httpClient: ensureHTTPClient(httpClient),
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     strings.TrimSpace(apiKey),
		model:      defaultString(strings.TrimSpace(model), "gpt-4.1-mini"),
	}
}

func (c *OpenAICompatibleMultimodal) Complete(ctx context.Context, req MultimodalRequest) (MultimodalResponse, error) {
	start := time.Now()
	messages, imageCount, err := normalizeMultimodalMessagesForOpenAI(req.Messages)
	if err != nil {
		return MultimodalResponse{}, err
	}
	body := map[string]any{
		"model":    c.model,
		"messages": messages,
		"stream":   false,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return MultimodalResponse{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, joinOpenAIPath(c.baseURL, "/chat/completions"), bytes.NewReader(raw))
	if err != nil {
		return MultimodalResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	setBearerIfPresent(httpReq, c.apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return MultimodalResponse{}, err
	}
	defer resp.Body.Close()
	headersAt := time.Now()
	if resp.StatusCode/100 != 2 {
		return MultimodalResponse{}, decodeHTTPError(resp)
	}

	var out struct {
		Error *struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    any    `json:"code"`
		} `json:"error"`
		Choices []struct {
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return MultimodalResponse{}, err
	}
	if out.Error != nil {
		message := strings.TrimSpace(out.Error.Message)
		if message == "" {
			message = "provider multimodal error"
		}
		return MultimodalResponse{}, fmt.Errorf("provider: multimodal error: %s", message)
	}
	if len(out.Choices) == 0 {
		return MultimodalResponse{}, fmt.Errorf("provider: empty multimodal response")
	}
	text := strings.TrimSpace(openAIMessageContentText(out.Choices[0].Message.Content))
	if text == "" {
		return MultimodalResponse{}, fmt.Errorf("provider: empty multimodal response")
	}
	slog.InfoContext(ctx, "provider multimodal completed",
		"provider", "openai-compatible",
		"model", c.model,
		"headers_ms", headersAt.Sub(start).Milliseconds(),
		"total_ms", time.Since(start).Milliseconds(),
		"message_count", len(messages),
		"image_count", imageCount,
		"text_chars", len(text),
	)
	return MultimodalResponse{Text: text}, nil
}

type OpenAICompatibleTTS struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	model      string
}

func NewOpenAICompatibleTTS(httpClient *http.Client, baseURL, apiKey, model string) *OpenAICompatibleTTS {
	return &OpenAICompatibleTTS{
		httpClient: ensureHTTPClient(httpClient),
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     strings.TrimSpace(apiKey),
		model:      defaultString(strings.TrimSpace(model), "gpt-4o-mini-tts"),
	}
}

func (c *OpenAICompatibleTTS) SynthesizeStream(ctx context.Context, req TTSRequest, cb func(AudioChunk) error) error {
	start := time.Now()
	voice, includeVoice := c.ttsVoice()
	segments := []string{strings.TrimSpace(req.Text)}
	if openAITTSSentenceStreamEnabled() {
		segments = splitTTSSegments(req.Text, openAITTSSegmentMaxRunes())
	}
	if len(segments) == 0 {
		return fmt.Errorf("provider: empty TTS input")
	}

	firstChunkAt := time.Time{}
	totalChunks := 0
	totalBytes := 0
	lastFormat := req.Format
	for i, segment := range segments {
		err := c.synthesizeOnce(ctx, segment, voice, includeVoice, req.Format, func(chunk AudioChunk) error {
			if firstChunkAt.IsZero() {
				firstChunkAt = time.Now()
			}
			lastFormat = mergeTTSChunkFormat(lastFormat, chunk.Format)
			totalChunks++
			totalBytes += len(chunk.Data)
			return cb(chunk)
		})
		if err != nil {
			return err
		}
		if i < len(segments)-1 {
			silence := ttsSegmentSilence(lastFormat, openAITTSSegmentSilenceMS())
			if len(silence) > 0 {
				totalChunks++
				totalBytes += len(silence)
				if err := cb(AudioChunk{Data: silence, Format: lastFormat}); err != nil {
					return err
				}
			}
		}
	}

	firstChunkMS := int64(-1)
	if !firstChunkAt.IsZero() {
		firstChunkMS = firstChunkAt.Sub(start).Milliseconds()
	}
	slog.InfoContext(ctx, "provider tts completed",
		"provider", "openai-compatible",
		"model", c.model,
		"first_chunk_ms", firstChunkMS,
		"total_ms", time.Since(start).Milliseconds(),
		"chunk_count", totalChunks,
		"audio_bytes", totalBytes,
		"text_chars", len(req.Text),
		"segments", len(segments),
	)
	return nil
}

func (c *OpenAICompatibleTTS) synthesizeOnce(ctx context.Context, text string, voice string, includeVoice bool, format AudioFormat, cb func(AudioChunk) error) error {
	body := map[string]any{
		"model":           c.model,
		"input":           text,
		"response_format": openAITTSResponseFormat(),
	}
	if openAITTSStreamEnabled(c.baseURL) {
		body["stream"] = true
		if interval := openAITTSStreamingInterval(); interval > 0 {
			body["streaming_interval"] = interval
		}
	}
	if includeVoice {
		body["voice"] = voice
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}

	start := time.Now()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, joinOpenAIPath(c.baseURL, "/audio/speech"), bytes.NewReader(raw))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	setBearerIfPresent(httpReq, c.apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	headersAt := time.Now()
	if resp.StatusCode/100 != 2 {
		return decodeHTTPError(resp)
	}

	stats, err := streamTTSAudio(resp.Body, format, cb)
	if err != nil {
		return err
	}
	slog.DebugContext(ctx, "provider tts segment completed",
		"provider", "openai-compatible",
		"model", c.model,
		"headers_ms", headersAt.Sub(start).Milliseconds(),
		"total_ms", time.Since(start).Milliseconds(),
		"chunk_count", stats.chunkCount,
		"audio_bytes", stats.audioBytes,
		"text_chars", len(text),
		"wav_unwrapped", stats.wavUnwrapped,
	)
	return nil
}

func (c *OpenAICompatibleTTS) ttsVoice() (string, bool) {
	if voice, ok := os.LookupEnv("AGENSENSE_OPENAI_TTS_VOICE"); ok {
		voice = strings.TrimSpace(voice)
		if voice == "" || strings.EqualFold(voice, "none") || voice == "-" {
			return "", false
		}
		return voice, true
	}
	if strings.Contains(strings.ToLower(c.model), "qwen3-tts") {
		return "", false
	}
	return fallbackOpenAITTSVoice, true
}

func openAITTSResponseFormat() string {
	format := strings.TrimSpace(os.Getenv("AGENSENSE_OPENAI_TTS_RESPONSE_FORMAT"))
	if format == "" {
		return "pcm"
	}
	return format
}

func openAITTSStreamEnabled(baseURL string) bool {
	value, ok := os.LookupEnv("AGENSENSE_OPENAI_TTS_STREAM")
	if !ok {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "1", "true", "yes", "on":
		return openAITTSStreamBaseURLAllowed(baseURL)
	default:
		return false
	}
}

func openAITTSStreamBaseURLAllowed(baseURL string) bool {
	allowlist := strings.TrimSpace(os.Getenv("AGENSENSE_OPENAI_TTS_STREAM_BASE_URLS"))
	if allowlist == "" {
		return true
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	for _, item := range strings.Split(allowlist, ",") {
		allowed := strings.TrimRight(strings.TrimSpace(item), "/")
		if allowed == "" {
			continue
		}
		if baseURL == allowed || strings.HasPrefix(baseURL+"/", allowed+"/") {
			return true
		}
	}
	return false
}

func openAITTSStreamingInterval() int {
	value := strings.TrimSpace(os.Getenv("AGENSENSE_OPENAI_TTS_STREAMING_INTERVAL"))
	if value == "" {
		return 0
	}
	var interval int
	if _, err := fmt.Sscanf(value, "%d", &interval); err != nil || interval <= 0 {
		return 0
	}
	return interval
}

func openAITTSSentenceStreamEnabled() bool {
	value, ok := os.LookupEnv("AGENSENSE_OPENAI_TTS_SENTENCE_STREAM")
	if !ok {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func openAITTSSegmentMaxRunes() int {
	value := strings.TrimSpace(os.Getenv("AGENSENSE_OPENAI_TTS_SEGMENT_MAX_RUNES"))
	if value == "" {
		return 120
	}
	var max int
	if _, err := fmt.Sscanf(value, "%d", &max); err != nil || max <= 0 {
		return 120
	}
	return max
}

func openAITTSSegmentSilenceMS() int {
	value := strings.TrimSpace(os.Getenv("AGENSENSE_OPENAI_TTS_SEGMENT_SILENCE_MS"))
	if value == "" {
		return 180
	}
	var ms int
	if _, err := fmt.Sscanf(value, "%d", &ms); err != nil || ms < 0 {
		return 180
	}
	return ms
}

func splitTTSSegments(text string, maxRunes int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if maxRunes <= 0 {
		maxRunes = 120
	}

	var segments []string
	var builder strings.Builder
	runeCount := 0
	flush := func() {
		segment := strings.TrimSpace(builder.String())
		if segment != "" {
			segments = append(segments, segment)
		}
		builder.Reset()
		runeCount = 0
	}

	for _, r := range text {
		builder.WriteRune(r)
		runeCount++
		if isTTSSentenceBreak(r) || r == '\n' || runeCount >= maxRunes {
			flush()
		}
	}
	flush()
	return segments
}

func isTTSSentenceBreak(r rune) bool {
	switch r {
	case '.', '!', '?', ';', '。', '！', '？', '；':
		return true
	default:
		return false
	}
}

func mergeTTSChunkFormat(current AudioFormat, next AudioFormat) AudioFormat {
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

func ttsSegmentSilence(format AudioFormat, silenceMS int) []byte {
	if silenceMS <= 0 {
		return nil
	}
	if format.SampleRateHz <= 0 || format.Channels <= 0 {
		return nil
	}
	if format.Codec != "" && format.Codec != "pcm_s16le" {
		return nil
	}
	samples := format.SampleRateHz * silenceMS / 1000
	if samples <= 0 {
		return nil
	}
	return make([]byte, samples*format.Channels*bytesPerSample)
}

type ttsStreamStats struct {
	chunkCount   int
	audioBytes   int
	wavUnwrapped bool
}

func streamTTSAudio(body io.Reader, requested AudioFormat, cb func(AudioChunk) error) (ttsStreamStats, error) {
	reader := bufio.NewReader(body)
	header, err := reader.Peek(12)
	if err != nil {
		if err == io.EOF {
			return streamRawTTSAudio(reader, requested, cb)
		}
		return ttsStreamStats{}, err
	}
	if !bytes.Equal(header[:4], []byte("RIFF")) || !bytes.Equal(header[8:12], []byte("WAVE")) {
		return streamRawTTSAudio(reader, requested, cb)
	}
	if _, err := reader.Discard(12); err != nil {
		return ttsStreamStats{}, err
	}
	return streamWAVAsPCM(reader, requested, cb)
}

func streamRawTTSAudio(reader io.Reader, format AudioFormat, cb func(AudioChunk) error) (ttsStreamStats, error) {
	var stats ttsStreamStats
	const chunkSize = 1024
	buf := make([]byte, chunkSize)
	for {
		n, readErr := reader.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			stats.chunkCount++
			stats.audioBytes += n
			if err := cb(AudioChunk{Data: chunk, Format: format}); err != nil {
				return stats, err
			}
		}
		if readErr == nil {
			continue
		}
		if readErr == io.EOF {
			return stats, nil
		}
		return stats, readErr
	}
}

func streamWAVAsPCM(reader io.Reader, requested AudioFormat, cb func(AudioChunk) error) (ttsStreamStats, error) {
	stats := ttsStreamStats{wavUnwrapped: true}
	format := requested
	dataSeen := false
	for {
		chunkHeader := make([]byte, 8)
		if _, err := io.ReadFull(reader, chunkHeader); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				if dataSeen {
					return stats, nil
				}
			}
			return stats, err
		}
		chunkID := string(chunkHeader[:4])
		chunkSize := int64(binary.LittleEndian.Uint32(chunkHeader[4:8]))
		switch chunkID {
		case "fmt ":
			payload := make([]byte, chunkSize)
			if _, err := io.ReadFull(reader, payload); err != nil {
				return stats, err
			}
			if len(payload) >= 16 {
				audioFormat := binary.LittleEndian.Uint16(payload[0:2])
				channels := int(binary.LittleEndian.Uint16(payload[2:4]))
				sampleRate := int(binary.LittleEndian.Uint32(payload[4:8]))
				bitsPerSample := int(binary.LittleEndian.Uint16(payload[14:16]))
				if audioFormat != 1 || bitsPerSample != 16 {
					return stats, fmt.Errorf("provider: unsupported WAV TTS format tag=%d bits=%d", audioFormat, bitsPerSample)
				}
				format = AudioFormat{
					Codec:        "pcm_s16le",
					SampleRateHz: sampleRate,
					Channels:     channels,
				}
			}
		case "data":
			dataSeen = true
			if err := streamWAVData(reader, chunkSize, format, &stats, cb); err != nil {
				return stats, err
			}
		default:
			if err := discardExactly(reader, chunkSize); err != nil {
				return stats, err
			}
		}
		if chunkSize%2 != 0 {
			if err := discardExactly(reader, 1); err != nil {
				return stats, err
			}
		}
	}
}

func streamWAVData(reader io.Reader, size int64, format AudioFormat, stats *ttsStreamStats, cb func(AudioChunk) error) error {
	const chunkSize = 1024
	remaining := size
	buf := make([]byte, chunkSize)
	for remaining > 0 {
		toRead := int64(len(buf))
		if remaining < toRead {
			toRead = remaining
		}
		n, err := io.ReadFull(reader, buf[:toRead])
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			stats.chunkCount++
			stats.audioBytes += n
			if err := cb(AudioChunk{Data: chunk, Format: format}); err != nil {
				return err
			}
			remaining -= int64(n)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func discardExactly(reader io.Reader, size int64) error {
	if size <= 0 {
		return nil
	}
	_, err := io.CopyN(io.Discard, reader, size)
	return err
}

func ensureHTTPClient(client *http.Client) *http.Client {
	if client != nil {
		return client
	}
	return &http.Client{}
}

func setBearerIfPresent(req *http.Request, apiKey string) {
	if strings.TrimSpace(apiKey) == "" {
		return
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))
}

func joinOpenAIPath(baseURL, suffix string) string {
	if strings.HasSuffix(baseURL, suffix) {
		return baseURL
	}
	return strings.TrimRight(baseURL, "/") + path.Clean("/"+suffix)
}

func defaultString(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func normalizeChatMessagesForOpenAI(messages []ChatMessage) []ChatMessage {
	if len(messages) == 0 {
		return nil
	}

	systemParts := make([]string, 0, len(messages))
	nonSystem := make([]ChatMessage, 0, len(messages))
	for _, message := range messages {
		if strings.EqualFold(strings.TrimSpace(message.Role), "system") {
			content := strings.TrimSpace(message.Content)
			if content != "" {
				systemParts = append(systemParts, content)
			}
			continue
		}
		nonSystem = append(nonSystem, message)
	}

	out := make([]ChatMessage, 0, len(nonSystem)+1)
	if len(systemParts) > 0 {
		out = append(out, ChatMessage{
			Role:    "system",
			Content: strings.Join(systemParts, "\n\n"),
		})
	}
	out = append(out, nonSystem...)
	return out
}

func normalizeMultimodalMessagesForOpenAI(messages []MultimodalMessage) ([]map[string]any, int, error) {
	if len(messages) == 0 {
		return nil, 0, fmt.Errorf("provider: empty multimodal messages")
	}

	out := make([]map[string]any, 0, len(messages))
	imageCount := 0
	for _, message := range messages {
		role := strings.TrimSpace(message.Role)
		if role == "" {
			role = "user"
		}
		parts := make([]map[string]any, 0, len(message.Content))
		for _, content := range message.Content {
			part, isImage, ok := openAIContentPart(content)
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
		out = append(out, map[string]any{
			"role":    role,
			"content": parts,
		})
	}
	if len(out) == 0 {
		return nil, 0, fmt.Errorf("provider: empty multimodal content")
	}
	return out, imageCount, nil
}

func openAIContentPart(content MultimodalContent) (map[string]any, bool, bool) {
	contentType := strings.ToLower(strings.TrimSpace(content.Type))
	text := strings.TrimSpace(content.Text)
	if text != "" && (contentType == "" || contentType == "text" || contentType == "input_text") {
		return map[string]any{
			"type": "text",
			"text": text,
		}, false, true
	}

	imageURL := strings.TrimSpace(content.ImageURL)
	if imageURL == "" && len(content.Data) > 0 {
		mimeType := strings.TrimSpace(content.MIMEType)
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		imageURL = "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(content.Data)
	}
	if imageURL != "" && (contentType == "" || contentType == "image" || contentType == "image_url" || contentType == "input_image") {
		return map[string]any{
			"type": "image_url",
			"image_url": map[string]any{
				"url": imageURL,
			},
		}, true, true
	}
	return nil, false, false
}

func openAIMessageContentText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err != nil {
		return ""
	}
	var builder strings.Builder
	for _, part := range parts {
		if strings.TrimSpace(part.Text) == "" {
			continue
		}
		if builder.Len() > 0 {
			builder.WriteString("\n")
		}
		builder.WriteString(strings.TrimSpace(part.Text))
	}
	return builder.String()
}

func decodeHTTPError(resp *http.Response) error {
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return fmt.Errorf("provider: upstream status %d", resp.StatusCode)
	}
	return fmt.Errorf("provider: upstream status %d: %s", resp.StatusCode, trimmed)
}

func pcmToWAV(format AudioFormat, pcm []byte) ([]byte, error) {
	if len(pcm) == 0 {
		return nil, fmt.Errorf("provider: empty audio payload")
	}
	if format.Codec != "" && format.Codec != "pcm_s16le" {
		return nil, fmt.Errorf("provider: unsupported input codec %q", format.Codec)
	}

	sampleRate := format.SampleRateHz
	if sampleRate <= 0 {
		sampleRate = defaultSampleRate
	}
	channels := format.Channels
	if channels <= 0 {
		channels = defaultChannels
	}

	byteRate := sampleRate * channels * bytesPerSample
	blockAlign := channels * bytesPerSample
	dataSize := len(pcm)
	riffSize := 36 + dataSize

	header := bytes.NewBuffer(make([]byte, 0, 44+len(pcm)))
	header.WriteString("RIFF")
	_ = binaryWriteLE(header, uint32(riffSize))
	header.WriteString("WAVE")
	header.WriteString("fmt ")
	_ = binaryWriteLE(header, uint32(16))
	_ = binaryWriteLE(header, uint16(1))
	_ = binaryWriteLE(header, uint16(channels))
	_ = binaryWriteLE(header, uint32(sampleRate))
	_ = binaryWriteLE(header, uint32(byteRate))
	_ = binaryWriteLE(header, uint16(blockAlign))
	_ = binaryWriteLE(header, uint16(bytesPerSample*8))
	header.WriteString("data")
	_ = binaryWriteLE(header, uint32(dataSize))
	header.Write(pcm)
	return header.Bytes(), nil
}

func binaryWriteLE(buf *bytes.Buffer, value any) error {
	return binary.Write(buf, binary.LittleEndian, value)
}
