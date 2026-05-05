package service

import "testing"

func TestMergeVoiceAssistantMetadataUsesTopLevelCompatibilityFields(t *testing.T) {
	intent := &VoiceAssistantIntent{
		Scope:                "focused_object",
		TargetID:             "session-alpha",
		Action:               "set_composer",
		RequiresConfirmation: false,
		UISurface:            "anchored_input",
	}

	metadata := MergeVoiceAssistantMetadata(
		VoiceAssistantMetadata{},
		map[string]any{"current_scene": "chat"},
		intent,
		map[string]any{"client": "agendash"},
	)

	if metadata.Empty() {
		t.Fatal("expected metadata to be non-empty")
	}
	if metadata.Contract != "universal_voice_layer_v1" {
		t.Fatalf("Contract = %q, want universal_voice_layer_v1", metadata.Contract)
	}
	if metadata.AssistantIntent == nil || metadata.AssistantIntent.TargetID != "session-alpha" {
		t.Fatalf("AssistantIntent = %#v, want target session-alpha", metadata.AssistantIntent)
	}
	if metadata.UIContext["current_scene"] != "chat" {
		t.Fatalf("UIContext current_scene = %#v, want chat", metadata.UIContext["current_scene"])
	}
}

func TestNormalizeSynthesisFormatDetectsWAV(t *testing.T) {
	audio := []byte{
		'R', 'I', 'F', 'F', 0x24, 0x11, 0x01, 0x00,
		'W', 'A', 'V', 'E',
		'f', 'm', 't', ' ', 0x10, 0x00, 0x00, 0x00,
		0x01, 0x00, 0x01, 0x00,
		0x80, 0x3e, 0x00, 0x00,
		0x00, 0x7d, 0x00, 0x00,
		0x02, 0x00, 0x10, 0x00,
		'd', 'a', 't', 'a', 0x00, 0x11, 0x01, 0x00,
	}
	format := normalizeSynthesisFormat(AudioFormatInput{
		Codec:        "pcm_s16le",
		SampleRateHz: 8000,
		Channels:     2,
	}, audio)

	if format.Codec != "wav" {
		t.Fatalf("Codec = %q, want wav", format.Codec)
	}
	if format.SampleRateHz != 16000 {
		t.Fatalf("SampleRateHz = %d, want 16000", format.SampleRateHz)
	}
	if format.Channels != 1 {
		t.Fatalf("Channels = %d, want 1", format.Channels)
	}
}

func TestNormalizeSynthesisFormatLeavesPCMUntouched(t *testing.T) {
	format := normalizeSynthesisFormat(AudioFormatInput{
		Codec:        "pcm_s16le",
		SampleRateHz: 16000,
		Channels:     1,
	}, []byte{0x00, 0x01, 0x02, 0x03})

	if format.Codec != "pcm_s16le" {
		t.Fatalf("Codec = %q, want pcm_s16le", format.Codec)
	}
}
