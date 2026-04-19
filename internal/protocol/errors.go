package protocol

import "errors"

var (
	// ErrInvalidEnvelope reports malformed control-message envelopes.
	ErrInvalidEnvelope = errors.New("protocol: invalid envelope")
	// ErrInvalidPayload reports malformed or unsupported payload values.
	ErrInvalidPayload = errors.New("protocol: invalid payload")
	// ErrStreamState reports invalid single-stream sequencing.
	ErrStreamState = errors.New("protocol: invalid stream state")
)
