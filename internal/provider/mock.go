package provider

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
)

const (
	defaultSampleRate = 16000
	defaultChannels   = 1
	bytesPerSample    = 2
)

// MockASR produces deterministic transcripts from raw audio length.
type MockASR struct{}

// Transcribe implements ASRClient.
func (MockASR) Transcribe(_ context.Context, req TranscribeRequest) (TranscribeResponse, error) {
	if len(req.Audio) == 0 {
		return TranscribeResponse{}, errors.New("mock asr: empty audio")
	}

	rate := req.Format.SampleRateHz
	if rate <= 0 {
		rate = defaultSampleRate
	}

	channels := req.Format.Channels
	if channels <= 0 {
		channels = defaultChannels
	}

	seconds := float64(len(req.Audio)) / float64(rate*channels*bytesPerSample)
	text := fmt.Sprintf(
		"mock transcript from %.2f seconds of audio (%d bytes)",
		seconds,
		len(req.Audio),
	)
	slog.Debug("mock asr response prepared",
		"device_id", req.DeviceID,
		"session_id", req.SessionID,
		"text_chars", len(text),
	)
	return TranscribeResponse{Text: text}, nil
}

// MockLLM produces a stable reply derived from the last user message.
type MockLLM struct{}

// ChatStream implements LLMClient.
func (MockLLM) ChatStream(ctx context.Context, req ChatRequest, cb func(ChatDelta) error) error {
	var userText string
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == "user" {
			userText = req.Messages[i].Content
			break
		}
	}
	if userText == "" {
		userText = "hello from the mock gateway"
	}

	reply := fmt.Sprintf(
		"Mock agent reply: I heard %q. The gateway voice pipeline is working and ready for real providers later.",
		userText,
	)
	slog.Debug("mock llm response prepared",
		"device_id", req.DeviceID,
		"session_id", req.SessionID,
		"reply_chars", len(reply),
	)
	for _, chunk := range chunkSentence(reply, 24) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err := cb(ChatDelta{Text: chunk}); err != nil {
			return err
		}
	}
	return nil
}

// MockMultimodal produces a stable text reply from text/image content parts.
type MockMultimodal struct{}

// Complete implements MultimodalClient.
func (MockMultimodal) Complete(_ context.Context, req MultimodalRequest) (MultimodalResponse, error) {
	textParts := 0
	imageParts := 0
	var lastText string
	for _, message := range req.Messages {
		for _, part := range message.Content {
			if strings.TrimSpace(part.Text) != "" {
				textParts++
				lastText = strings.TrimSpace(part.Text)
			}
			if strings.TrimSpace(part.ImageURL) != "" || len(part.Data) > 0 {
				imageParts++
			}
		}
	}
	if textParts == 0 && imageParts == 0 {
		return MultimodalResponse{}, errors.New("mock multimodal: empty content")
	}
	reply := fmt.Sprintf(
		"Mock multimodal reply: analyzed %d image(s) with %d text part(s). Last prompt: %q.",
		imageParts,
		textParts,
		lastText,
	)
	slog.Debug("mock multimodal response prepared",
		"device_id", req.DeviceID,
		"session_id", req.SessionID,
		"image_parts", imageParts,
		"text_parts", textParts,
		"reply_chars", len(reply),
	)
	return MultimodalResponse{Text: reply}, nil
}

// MockTTS synthesizes a deterministic sine-wave PCM stream.
type MockTTS struct{}

// SynthesizeStream implements TTSClient.
func (MockTTS) SynthesizeStream(ctx context.Context, req TTSRequest, cb func(AudioChunk) error) error {
	format := req.Format
	if format.Codec == "" {
		format.Codec = "pcm_s16le"
	}
	if format.SampleRateHz <= 0 {
		format.SampleRateHz = defaultSampleRate
	}
	if format.Channels <= 0 {
		format.Channels = defaultChannels
	}
	if format.Codec != "pcm_s16le" {
		return fmt.Errorf("mock tts: unsupported codec %q", format.Codec)
	}

	data := synthesizeTonePCM(format, req.Text)
	slog.Debug("mock tts audio prepared",
		"device_id", req.DeviceID,
		"session_id", req.SessionID,
		"audio_bytes", len(data),
		"text_chars", len(req.Text),
	)
	const chunkSize = 640
	for start := 0; start < len(data); start += chunkSize {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		end := start + chunkSize
		if end > len(data) {
			end = len(data)
		}
		if err := cb(AudioChunk{Data: data[start:end]}); err != nil {
			return err
		}
	}
	return nil
}

func chunkSentence(text string, target int) []string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{text}
	}

	chunks := make([]string, 0, len(words))
	var current strings.Builder
	for _, word := range words {
		if current.Len() == 0 {
			current.WriteString(word)
			continue
		}
		if current.Len()+1+len(word) > target {
			chunks = append(chunks, current.String()+" ")
			current.Reset()
			current.WriteString(word)
			continue
		}
		current.WriteByte(' ')
		current.WriteString(word)
	}
	if current.Len() > 0 {
		chunks = append(chunks, current.String())
	}
	return chunks
}

func synthesizeTonePCM(format AudioFormat, text string) []byte {
	chars := len([]rune(strings.TrimSpace(text)))
	if chars == 0 {
		chars = 8
	}

	durationMS := 350 + chars*20
	if durationMS > 2400 {
		durationMS = 2400
	}
	samples := format.SampleRateHz * durationMS / 1000
	data := make([]byte, samples*format.Channels*bytesPerSample)

	const frequency = 440.0
	const amplitude = 0.22
	const fadeSamples = 400

	for i := 0; i < samples; i++ {
		phase := 2 * math.Pi * frequency * float64(i) / float64(format.SampleRateHz)
		envelope := 1.0
		if i < fadeSamples {
			envelope = float64(i) / float64(fadeSamples)
		}
		if tail := samples - i; tail < fadeSamples {
			envelope = float64(tail) / float64(fadeSamples)
		}
		value := int16(math.Sin(phase) * math.MaxInt16 * amplitude * envelope)
		for ch := 0; ch < format.Channels; ch++ {
			offset := (i*format.Channels + ch) * bytesPerSample
			data[offset] = byte(value)
			data[offset+1] = byte(value >> 8)
		}
	}
	return data
}
