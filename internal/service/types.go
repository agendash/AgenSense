package service

import (
	"context"
	"encoding/json"
	"errors"
)

var (
	// ErrUnauthorized reports invalid or missing device authentication.
	ErrUnauthorized = errors.New("unauthorized")
	// ErrNotFound reports a missing device or related object.
	ErrNotFound = errors.New("not found")
	// ErrInvalidInput reports a request validation failure.
	ErrInvalidInput = errors.New("invalid input")
)

// BootstrapRequest is the control-plane bootstrap input.
type BootstrapRequest struct {
	DeviceID        string         `json:"device_id"`
	ChipID          string         `json:"chip_id"`
	HardwareSKU     string         `json:"hardware_sku"`
	FirmwareVersion string         `json:"firmware_version"`
	FirmwareChannel string         `json:"firmware_channel"`
	Capabilities    map[string]any `json:"capabilities"`
	ClaimToken      string         `json:"claim_token"`
}

// BootstrapResponse is the control-plane bootstrap output.
type BootstrapResponse struct {
	TenantID      string         `json:"tenant_id"`
	InstanceID    string         `json:"instance_id"`
	DeviceID      string         `json:"device_id"`
	DeviceToken   string         `json:"device_token"`
	WSURL         string         `json:"ws_url"`
	ConfigVersion int64          `json:"config_version"`
	Config        map[string]any `json:"config"`
	RetryHintSec  int            `json:"retry_hint_sec"`
}

// DeviceContext is the authenticated device view used by HTTP and WebSocket flows.
type DeviceContext struct {
	DeviceID            string
	TenantID            string
	InstanceID          string
	ProviderProfileID   string
	ConfigVersion       int64
	ReportedConfig      int64
	Config              map[string]any
	Capabilities        map[string]any
	FirmwareVersion     string
	FirmwareChannel     string
	DefaultAudioCodec   string
	DefaultSampleRateHz int
}

// ControlPlane manages HTTP bootstrap/config/telemetry flows.
type ControlPlane interface {
	Bootstrap(ctx context.Context, req BootstrapRequest) (BootstrapResponse, error)
	AuthenticateDeviceToken(ctx context.Context, deviceID, token string) (DeviceContext, error)
	UpdateTelemetry(ctx context.Context, deviceID string, telemetry json.RawMessage) error
	AckConfig(ctx context.Context, deviceID string, version int64) error
}
