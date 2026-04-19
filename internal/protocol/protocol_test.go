package protocol

import (
	"testing"
)

func TestDecodeHelloEvent(t *testing.T) {
	t.Parallel()

	event, err := DecodeEvent([]byte(`{
		"type":"hello",
		"request_id":"req-001",
		"payload":{
			"device":{"device_id":"dev-001","hardware_sku":"m5cores3"},
			"state":{"config_version":2}
		}
	}`))
	if err != nil {
		t.Fatalf("DecodeEvent() error = %v", err)
	}
	var payload HelloPayload
	if err := event.DecodePayload(&payload); err != nil {
		t.Fatalf("DecodePayload() error = %v", err)
	}
	if payload.Device.DeviceID != "dev-001" {
		t.Fatalf("device id = %q, want dev-001", payload.Device.DeviceID)
	}
}

func TestHelloOKRequiresRequestAndSession(t *testing.T) {
	t.Parallel()

	_, err := NewEvent(EventHelloOK, "req-001", "sess-001", HelloOKPayload{ServerTimeMS: 1})
	if err != nil {
		t.Fatalf("NewEvent() unexpected error = %v", err)
	}

	_, err = NewEvent(EventHelloOK, "", "sess-001", HelloOKPayload{ServerTimeMS: 1})
	if err == nil {
		t.Fatal("expected error for missing request_id")
	}
}

func TestAudioStartStopValidation(t *testing.T) {
	t.Parallel()

	start, err := NewEvent(EventAudioStart, "", "", AudioStartPayload{
		StreamID:     "st-001",
		Codec:        "pcm_s16le",
		SampleRateHz: 16000,
		Channels:     1,
	})
	if err != nil {
		t.Fatalf("NewEvent(start) error = %v", err)
	}
	if _, err := MarshalEvent(start); err != nil {
		t.Fatalf("MarshalEvent(start) error = %v", err)
	}

	stop, err := NewEvent(EventAudioStop, "", "", AudioStopPayload{
		StreamID: "st-001",
		LastSeq:  2,
	})
	if err != nil {
		t.Fatalf("NewEvent(stop) error = %v", err)
	}
	if _, err := MarshalEvent(stop); err != nil {
		t.Fatalf("MarshalEvent(stop) error = %v", err)
	}
}

func TestErrorAndActionValidation(t *testing.T) {
	t.Parallel()

	if _, err := NewEvent(EventError, "", "sess-001", ErrorPayload{Code: "bad", Message: "boom"}); err != nil {
		t.Fatalf("NewEvent(error) error = %v", err)
	}
	if _, err := NewEvent(EventActionExecute, "", "sess-001", ActionExecutePayload{
		ActionID: "act-001",
		Kind:     "noop",
	}); err != nil {
		t.Fatalf("NewEvent(action.execute) error = %v", err)
	}
}

func TestStreamTracker(t *testing.T) {
	t.Parallel()

	var tracker StreamTracker
	if err := tracker.Start(AudioStartPayload{
		StreamID:     "st-001",
		Codec:        "pcm_s16le",
		SampleRateHz: 16000,
		Channels:     1,
	}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := tracker.AddFrame(); err != nil {
		t.Fatalf("AddFrame() error = %v", err)
	}
	if err := tracker.AddFrame(); err != nil {
		t.Fatalf("AddFrame() error = %v", err)
	}
	if err := tracker.Stop(AudioStopPayload{StreamID: "st-001", LastSeq: 2}); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}
