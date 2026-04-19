package protocol

import (
	"encoding/json"
	"fmt"
	"time"
)

// Envelope is the canonical JSON control-message shape for the MVP.
type Envelope struct {
	Type      string          `json:"type"`
	RequestID string          `json:"request_id,omitempty"`
	SessionID string          `json:"session_id,omitempty"`
	TSMS      int64           `json:"ts_ms"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

// NewEvent builds a validated envelope from the provided payload value.
func NewEvent(eventType, requestID, sessionID string, payload any) (Envelope, error) {
	raw, err := marshalPayload(payload)
	if err != nil {
		return Envelope{}, err
	}
	event := Envelope{
		Type:      eventType,
		RequestID: requestID,
		SessionID: sessionID,
		TSMS:      time.Now().UnixMilli(),
		Payload:   raw,
	}
	if err := validateEnvelope(event); err != nil {
		return Envelope{}, err
	}
	return event, nil
}

// MarshalEvent validates and marshals an envelope to JSON.
func MarshalEvent(event Envelope) ([]byte, error) {
	if err := validateEnvelope(event); err != nil {
		return nil, err
	}
	return json.Marshal(event)
}

// DecodeEvent unmarshals and validates an envelope from JSON.
func DecodeEvent(data []byte) (Envelope, error) {
	var event Envelope
	if err := json.Unmarshal(data, &event); err != nil {
		return Envelope{}, fmt.Errorf("%w: %v", ErrInvalidEnvelope, err)
	}
	if err := validateEnvelope(event); err != nil {
		return Envelope{}, err
	}
	return event, nil
}

// DecodePayload unmarshals the envelope payload into a typed destination.
func (e Envelope) DecodePayload(dst any) error {
	if len(e.Payload) == 0 {
		return fmt.Errorf("%w: missing payload", ErrInvalidPayload)
	}
	if err := json.Unmarshal(e.Payload, dst); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidPayload, err)
	}
	return nil
}

func marshalPayload(payload any) (json.RawMessage, error) {
	if payload == nil {
		return nil, nil
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}
