package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"path"
	"strings"
	"time"
)

const defaultOpenAITTSVoice = "alloy"

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
	if strings.TrimSpace(out.Text) == "" {
		return TranscribeResponse{}, fmt.Errorf("provider: empty ASR response")
	}
	slog.InfoContext(ctx, "provider asr completed",
		"provider", "openai-compatible",
		"model", c.model,
		"ttfb_ms", headersAt.Sub(start).Milliseconds(),
		"total_ms", time.Since(start).Milliseconds(),
		"audio_bytes", len(req.Audio),
		"text_chars", len(out.Text),
	)
	return TranscribeResponse{Text: out.Text}, nil
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
	body := map[string]any{
		"model":    c.model,
		"messages": req.Messages,
		"stream":   true,
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
			"message_count", len(req.Messages),
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
		for _, choice := range chunk.Choices {
			text := choice.Delta.Content
			if text == "" {
				text = choice.Message.Content
			}
			if strings.TrimSpace(text) == "" {
				continue
			}
			if firstDeltaAt.IsZero() {
				firstDeltaAt = time.Now()
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
	body := map[string]any{
		"model":           c.model,
		"input":           req.Text,
		"voice":           defaultOpenAITTSVoice,
		"response_format": "pcm",
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}

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

	const chunkSize = 1024
	buf := make([]byte, chunkSize)
	firstChunkAt := time.Time{}
	chunkCount := 0
	audioBytes := 0
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if firstChunkAt.IsZero() {
				firstChunkAt = time.Now()
			}
			chunkCount++
			audioBytes += n
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			if err := cb(AudioChunk{Data: chunk}); err != nil {
				return err
			}
		}
		if readErr == nil {
			continue
		}
		if readErr == io.EOF {
			firstChunkMS := int64(-1)
			if !firstChunkAt.IsZero() {
				firstChunkMS = firstChunkAt.Sub(start).Milliseconds()
			}
			slog.InfoContext(ctx, "provider tts completed",
				"provider", "openai-compatible",
				"model", c.model,
				"headers_ms", headersAt.Sub(start).Milliseconds(),
				"first_chunk_ms", firstChunkMS,
				"total_ms", time.Since(start).Milliseconds(),
				"chunk_count", chunkCount,
				"audio_bytes", audioBytes,
				"text_chars", len(req.Text),
			)
			return nil
		}
		return readErr
	}
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
