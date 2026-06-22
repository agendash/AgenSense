package protocol

import (
	"encoding/json"
	"fmt"
)

const (
	EventHello           = "hello"
	EventHelloOK         = "hello.ok"
	EventTelemetry       = "telemetry.update"
	EventAudioStart      = "audio.start"
	EventAudioStop       = "audio.stop"
	EventConfigSnapshot  = "config.snapshot"
	EventConfigAck       = "config.ack"
	EventASRFinal        = "asr.final"
	EventLLMDelta        = "llm.delta"
	EventLLMDone         = "llm.done"
	EventMCPCallProposed = "mcp.call.proposed"
	EventTTSStart        = "tts.start"
	EventTTSStop         = "tts.stop"
	EventActionExecute   = "action.execute"
	EventError           = "error"
)

// HelloPayload is the client hello payload.
type HelloPayload struct {
	Device HelloDevice `json:"device"`
	State  HelloState  `json:"state,omitempty"`
}

// HelloDevice describes the device identity presented in the hello event.
type HelloDevice struct {
	DeviceID        string         `json:"device_id,omitempty"`
	HardwareSKU     string         `json:"hardware_sku,omitempty"`
	FirmwareVersion string         `json:"firmware_version,omitempty"`
	Capabilities    map[string]any `json:"capabilities,omitempty"`
}

// HelloState contains the client's current config state.
type HelloState struct {
	ConfigVersion int64 `json:"config_version,omitempty"`
}

// HelloOKPayload is the server hello acknowledgement payload.
type HelloOKPayload struct {
	ServerTimeMS         int64 `json:"server_time_ms"`
	DesiredConfigVersion int64 `json:"desired_config_version,omitempty"`
}

// AudioStartPayload begins a single raw audio stream.
type AudioStartPayload struct {
	StreamID     string `json:"stream_id"`
	Codec        string `json:"codec"`
	SampleRateHz int    `json:"sample_rate_hz"`
	Channels     int    `json:"channels"`
}

// AudioStopPayload closes a single raw audio stream.
type AudioStopPayload struct {
	StreamID string `json:"stream_id"`
	LastSeq  int    `json:"last_seq"`
}

// ConfigSnapshotPayload delivers the current merged device config.
type ConfigSnapshotPayload struct {
	ConfigVersion int64          `json:"config_version"`
	Config        map[string]any `json:"config"`
}

// ConfigAckPayload acknowledges a config version.
type ConfigAckPayload struct {
	ConfigVersion int64 `json:"config_version"`
}

// ASRFinalPayload carries the final transcript text.
type ASRFinalPayload struct {
	StreamID string `json:"stream_id,omitempty"`
	Text     string `json:"text"`
}

// LLMDeltaPayload carries one streamed text delta.
type LLMDeltaPayload struct {
	Text string `json:"text"`
}

// LLMDonePayload marks the end of the LLM text stream.
type LLMDonePayload struct {
	Text string `json:"text"`
}

// MCPCallProposedPayload carries one proposed MCP tool call for a client or
// gateway to confirm, execute, or ignore. AgenSense does not execute MCP calls.
type MCPCallProposedPayload struct {
	ProposalID           string         `json:"proposal_id"`
	ToolName             string         `json:"tool_name"`
	Arguments            map[string]any `json:"arguments,omitempty"`
	Transcript           string         `json:"transcript,omitempty"`
	Confidence           float64        `json:"confidence,omitempty"`
	RequiresConfirmation bool           `json:"requires_confirmation,omitempty"`
	Reason               string         `json:"reason,omitempty"`
}

// ErrorPayload is the canonical error shape.
type ErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ActionExecutePayload is the canonical device-action shape.
type ActionExecutePayload struct {
	ActionID string         `json:"action_id"`
	Kind     string         `json:"kind"`
	Payload  map[string]any `json:"payload,omitempty"`
}

func validateEnvelope(event Envelope) error {
	if event.Type == "" {
		return fmt.Errorf("%w: missing type", ErrInvalidEnvelope)
	}

	switch event.Type {
	case EventHello:
		if event.RequestID == "" {
			return fmt.Errorf("%w: hello requires request_id", ErrInvalidEnvelope)
		}
		return validatePayload[HelloPayload](event)
	case EventHelloOK:
		if event.RequestID == "" || event.SessionID == "" {
			return fmt.Errorf("%w: hello.ok requires request_id and session_id", ErrInvalidEnvelope)
		}
		return validatePayload[HelloOKPayload](event)
	case EventAudioStart, EventTTSStart:
		return validatePayload[AudioStartPayload](event)
	case EventAudioStop, EventTTSStop:
		return validatePayload[AudioStopPayload](event)
	case EventConfigSnapshot:
		return validatePayload[ConfigSnapshotPayload](event)
	case EventConfigAck:
		return validatePayload[ConfigAckPayload](event)
	case EventASRFinal:
		return validatePayload[ASRFinalPayload](event)
	case EventLLMDelta:
		return validatePayload[LLMDeltaPayload](event)
	case EventLLMDone:
		return validatePayload[LLMDonePayload](event)
	case EventMCPCallProposed:
		return validatePayload[MCPCallProposedPayload](event)
	case EventActionExecute:
		return validatePayload[ActionExecutePayload](event)
	case EventError:
		return validatePayload[ErrorPayload](event)
	case EventTelemetry:
		// Telemetry is caller-defined JSON, so only envelope shape is validated.
		return nil
	default:
		// Unknown event names are allowed for forward compatibility.
		return nil
	}
}

func validatePayload[T any](event Envelope) error {
	if len(event.Payload) == 0 {
		return fmt.Errorf("%w: %s requires payload", ErrInvalidPayload, event.Type)
	}
	var payload T
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidPayload, err)
	}
	if err := validateTypedPayload(event.Type, payload); err != nil {
		return err
	}
	return nil
}

func validateTypedPayload(eventType string, payload any) error {
	switch value := payload.(type) {
	case HelloPayload:
		if value.Device.DeviceID == "" {
			return fmt.Errorf("%w: hello.device.device_id is required", ErrInvalidPayload)
		}
	case HelloOKPayload:
		if value.ServerTimeMS <= 0 {
			return fmt.Errorf("%w: hello.ok.payload.server_time_ms must be > 0", ErrInvalidPayload)
		}
	case AudioStartPayload:
		if value.StreamID == "" {
			return fmt.Errorf("%w: %s.payload.stream_id is required", ErrInvalidPayload, eventType)
		}
		if value.Codec != "pcm_s16le" {
			return fmt.Errorf("%w: %s.payload.codec must be pcm_s16le", ErrInvalidPayload, eventType)
		}
		if value.SampleRateHz <= 0 || value.Channels <= 0 {
			return fmt.Errorf("%w: %s.payload sample rate and channels must be > 0", ErrInvalidPayload, eventType)
		}
	case AudioStopPayload:
		if value.StreamID == "" {
			return fmt.Errorf("%w: %s.payload.stream_id is required", ErrInvalidPayload, eventType)
		}
		if value.LastSeq < 0 {
			return fmt.Errorf("%w: %s.payload.last_seq must be >= 0", ErrInvalidPayload, eventType)
		}
	case ConfigSnapshotPayload:
		if value.ConfigVersion <= 0 {
			return fmt.Errorf("%w: config.snapshot.payload.config_version must be > 0", ErrInvalidPayload)
		}
	case ConfigAckPayload:
		if value.ConfigVersion <= 0 {
			return fmt.Errorf("%w: config.ack.payload.config_version must be > 0", ErrInvalidPayload)
		}
	case ASRFinalPayload:
		if value.Text == "" {
			return fmt.Errorf("%w: asr.final.payload.text is required", ErrInvalidPayload)
		}
	case LLMDeltaPayload:
		if value.Text == "" {
			return fmt.Errorf("%w: llm.delta.payload.text is required", ErrInvalidPayload)
		}
	case LLMDonePayload:
		if value.Text == "" {
			return fmt.Errorf("%w: llm.done.payload.text is required", ErrInvalidPayload)
		}
	case MCPCallProposedPayload:
		if value.ProposalID == "" || value.ToolName == "" {
			return fmt.Errorf("%w: mcp.call.proposed.payload proposal_id and tool_name are required", ErrInvalidPayload)
		}
		if value.Confidence < 0 || value.Confidence > 1 {
			return fmt.Errorf("%w: mcp.call.proposed.payload confidence must be between 0 and 1", ErrInvalidPayload)
		}
	case ErrorPayload:
		if value.Code == "" || value.Message == "" {
			return fmt.Errorf("%w: error.payload.code and error.payload.message are required", ErrInvalidPayload)
		}
	case ActionExecutePayload:
		if value.ActionID == "" || value.Kind == "" {
			return fmt.Errorf("%w: action.execute.payload.action_id and kind are required", ErrInvalidPayload)
		}
	}
	return nil
}
