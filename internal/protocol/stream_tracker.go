package protocol

import "fmt"

// StreamTracker enforces the MVP rule that only one stream is active at a time.
type StreamTracker struct {
	active     *AudioStartPayload
	frameCount int
}

// Start opens a new tracked stream.
func (t *StreamTracker) Start(payload AudioStartPayload) error {
	if t.active != nil {
		return fmt.Errorf("%w: stream %s already active", ErrStreamState, t.active.StreamID)
	}
	if err := validateTypedPayload(EventAudioStart, payload); err != nil {
		return err
	}
	copyPayload := payload
	t.active = &copyPayload
	t.frameCount = 0
	return nil
}

// AddFrame records one binary frame for the active stream.
func (t *StreamTracker) AddFrame() error {
	if t.active == nil {
		return fmt.Errorf("%w: no active stream", ErrStreamState)
	}
	t.frameCount++
	return nil
}

// Stop closes the active stream and validates the final frame count.
func (t *StreamTracker) Stop(payload AudioStopPayload) error {
	if t.active == nil {
		return fmt.Errorf("%w: no active stream", ErrStreamState)
	}
	if payload.StreamID != t.active.StreamID {
		return fmt.Errorf("%w: stop stream %s does not match active stream %s", ErrStreamState, payload.StreamID, t.active.StreamID)
	}
	if payload.LastSeq != t.frameCount {
		return fmt.Errorf("%w: stop last_seq %d does not match frame count %d", ErrStreamState, payload.LastSeq, t.frameCount)
	}
	t.active = nil
	t.frameCount = 0
	return nil
}

// Active reports the currently tracked stream, if any.
func (t *StreamTracker) Active() (AudioStartPayload, int, bool) {
	if t.active == nil {
		return AudioStartPayload{}, 0, false
	}
	return *t.active, t.frameCount, true
}
