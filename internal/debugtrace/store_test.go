package debugtrace

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/agendash/agensense/internal/provider"
)

func TestStoreSegmentsAndDiskAssets(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewStoreWithAssetDir(8, filepath.Join(dir, "debug-traces"))
	handle := store.StartTrace(KindVoiceTurn, SourceWS, TraceMeta{
		ClientID:          "agendash-client",
		SessionID:         "voice-session-001",
		ProviderProfileID: "default",
		HTTPPath:          "/v1/voice/ws",
	})

	format := provider.AudioFormat{
		Codec:        "pcm_s16le",
		SampleRateHz: 16000,
		Channels:     1,
	}
	audio := bytes.Repeat([]byte{0x01, 0x02}, 320)
	handle.SetInputAudio(format, audio)
	handle.StartSegment("segment-001", "input-001", format, 0, 0.2)
	handle.CompleteSegment("segment-001", format, audio[:160], 0, 5, 0.1, 0.3)
	handle.SetSegmentText("segment-001", "hello from segment")
	handle.Complete()

	traces := store.List()
	if len(traces) != 1 {
		t.Fatalf("List() len = %d, want 1", len(traces))
	}
	trace := traces[0]
	if trace.InputAudio == nil {
		t.Fatal("expected input audio metadata")
	}
	if len(trace.Segments) != 1 {
		t.Fatalf("segments len = %d, want 1", len(trace.Segments))
	}
	if trace.Segments[0].Text != "hello from segment" {
		t.Fatalf("segment text = %q", trace.Segments[0].Text)
	}
	if trace.Segments[0].Audio == nil {
		t.Fatal("expected segment audio metadata")
	}

	input, mime, ok := store.ReadAsset(trace.ID, "input.wav")
	if !ok {
		t.Fatal("ReadAsset(input.wav) missing")
	}
	if mime != "audio/wav" || !bytes.HasPrefix(input, []byte("RIFF")) {
		t.Fatalf("input asset mime/prefix = %q/%q", mime, input[:4])
	}

	segment, mime, ok := store.ReadAsset(trace.ID, "segment-001.wav")
	if !ok {
		t.Fatal("ReadAsset(segment-001.wav) missing")
	}
	if mime != "audio/wav" || !bytes.HasPrefix(segment, []byte("RIFF")) {
		t.Fatalf("segment asset mime/prefix = %q/%q", mime, segment[:4])
	}

	if _, err := os.Stat(filepath.Join(dir, "debug-traces", trace.ID, "input.wav")); err != nil {
		t.Fatalf("expected input asset on disk: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "debug-traces", trace.ID, "segment-001.wav")); err != nil {
		t.Fatalf("expected segment asset on disk: %v", err)
	}
}
