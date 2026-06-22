package saytts

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

const (
	DefaultVoice        = "Tingting"
	DefaultSampleRateHz = 16000
	DefaultChannels     = 1
)

type Format struct {
	Codec        string
	SampleRateHz int
	Channels     int
}

type Options struct {
	Voice        string
	SampleRateHz int
	Channels     int
	Rate         string
}

func SynthesizeWAV(ctx context.Context, text string, opts Options) ([]byte, Format, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, Format{}, fmt.Errorf("say tts: empty input")
	}

	voice := strings.TrimSpace(opts.Voice)
	if voice == "" {
		voice = DefaultVoice
	}
	sampleRate := opts.SampleRateHz
	if sampleRate <= 0 {
		sampleRate = DefaultSampleRateHz
	}
	channels := opts.Channels
	if channels <= 0 {
		channels = DefaultChannels
	}

	file, err := os.CreateTemp("", "agensense-say-*.wav")
	if err != nil {
		return nil, Format{}, err
	}
	path := file.Name()
	_ = file.Close()
	defer os.Remove(path)

	args := []string{
		"-v", voice,
		"-o", path,
		"--file-format=WAVE",
		"--data-format=LEI16@" + strconv.Itoa(sampleRate),
		"--channels=" + strconv.Itoa(channels),
	}
	if rate := strings.TrimSpace(opts.Rate); rate != "" {
		args = append(args, "-r", rate)
	}
	args = append(args, text)

	cmd := exec.CommandContext(ctx, "say", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		detail := strings.TrimSpace(string(output))
		if detail == "" {
			detail = err.Error()
		}
		return nil, Format{}, fmt.Errorf("say tts: %s", detail)
	}

	wav, err := os.ReadFile(path)
	if err != nil {
		return nil, Format{}, err
	}
	format, err := WAVFormat(wav)
	if err != nil {
		return nil, Format{}, err
	}
	return wav, format, nil
}

func PCMFromWAV(wav []byte) ([]byte, Format, error) {
	if len(wav) < 12 || !bytes.Equal(wav[:4], []byte("RIFF")) || !bytes.Equal(wav[8:12], []byte("WAVE")) {
		return nil, Format{}, fmt.Errorf("say tts: invalid WAV data")
	}

	var format Format
	var pcm []byte
	for offset := 12; offset+8 <= len(wav); {
		chunkID := string(wav[offset : offset+4])
		chunkSize := int(binary.LittleEndian.Uint32(wav[offset+4 : offset+8]))
		chunkStart := offset + 8
		chunkEnd := chunkStart + chunkSize
		if chunkEnd > len(wav) {
			return nil, Format{}, fmt.Errorf("say tts: truncated WAV chunk %q", chunkID)
		}

		switch chunkID {
		case "fmt ":
			if chunkSize < 16 {
				return nil, Format{}, fmt.Errorf("say tts: invalid WAV fmt chunk")
			}
			audioFormat := binary.LittleEndian.Uint16(wav[chunkStart : chunkStart+2])
			channels := int(binary.LittleEndian.Uint16(wav[chunkStart+2 : chunkStart+4]))
			sampleRate := int(binary.LittleEndian.Uint32(wav[chunkStart+4 : chunkStart+8]))
			bitsPerSample := int(binary.LittleEndian.Uint16(wav[chunkStart+14 : chunkStart+16]))
			if audioFormat != 1 || bitsPerSample != 16 {
				return nil, Format{}, fmt.Errorf("say tts: unsupported WAV format tag=%d bits=%d", audioFormat, bitsPerSample)
			}
			format = Format{
				Codec:        "pcm_s16le",
				SampleRateHz: sampleRate,
				Channels:     channels,
			}
		case "data":
			pcm = append(pcm[:0], wav[chunkStart:chunkEnd]...)
		}

		offset = chunkEnd
		if offset%2 != 0 {
			offset++
		}
	}

	if format.Codec == "" {
		return nil, Format{}, fmt.Errorf("say tts: missing WAV fmt chunk")
	}
	if len(pcm) == 0 {
		return nil, Format{}, fmt.Errorf("say tts: missing WAV audio data")
	}
	return pcm, format, nil
}

func WAVFormat(wav []byte) (Format, error) {
	_, format, err := PCMFromWAV(wav)
	return format, err
}
