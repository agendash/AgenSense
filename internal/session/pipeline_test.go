package session

import (
	"context"
	"testing"

	"github.com/agendash/agensense/internal/provider"
)

type testSink struct {
	asrText   string
	llmText   string
	ttsStream string
	ttsBytes  int
	action    Action
}

func (s *testSink) OnASRFinal(text string) error {
	s.asrText = text
	return nil
}

func (s *testSink) OnLLMDelta(text string) error {
	s.llmText += text
	return nil
}

func (s *testSink) OnLLMDone(string) error {
	return nil
}

func (s *testSink) OnTTSStart(streamID string, _ provider.AudioFormat, _ string) error {
	s.ttsStream = streamID
	return nil
}

func (s *testSink) OnTTSChunk(data []byte) error {
	s.ttsBytes += len(data)
	return nil
}

func (s *testSink) OnTTSStop(string) error {
	return nil
}

func (s *testSink) OnAction(action Action) error {
	s.action = action
	return nil
}

func TestPipelineRun(t *testing.T) {
	t.Parallel()

	p := &Pipeline{
		ASR: provider.MockASR{},
		LLM: provider.MockLLM{},
		TTS: provider.MockTTS{},
	}
	sink := &testSink{}

	err := p.Run(context.Background(), PipelineRequest{
		SessionID: "sess-001",
		DeviceID:  "dev-001",
		StreamID:  "st-001",
		Format: provider.AudioFormat{
			Codec:        "pcm_s16le",
			SampleRateHz: 16000,
			Channels:     1,
		},
		Audio: make([]byte, 16000),
	}, sink)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if sink.asrText == "" {
		t.Fatal("expected ASR output")
	}
	if sink.llmText == "" {
		t.Fatal("expected LLM output")
	}
	if sink.ttsStream == "" || sink.ttsBytes == 0 {
		t.Fatal("expected streamed TTS output")
	}
	if sink.action.Kind == "" {
		t.Fatal("expected device action")
	}
}
