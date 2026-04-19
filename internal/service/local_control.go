package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// LocalControlConfig configures the file-backed MVP control plane.
type LocalControlConfig struct {
	DataDir               string
	PublicBaseURL         string
	DemoDeviceID          string
	DemoClaimToken        string
	DemoTenantID          string
	DemoInstanceID        string
	DemoProviderProfileID string
	RetryHintSec          int
}

// LocalControl implements ControlPlane with a local JSON-backed state file.
type LocalControl struct {
	mu      sync.Mutex
	cfg     LocalControlConfig
	state   localState
	dataDir string
	path    string
}

type localState struct {
	Devices map[string]*localDevice `json:"devices"`
	Tokens  map[string]string       `json:"tokens"`
}

type localDevice struct {
	DeviceID              string          `json:"device_id"`
	TenantID              string          `json:"tenant_id"`
	InstanceID            string          `json:"instance_id"`
	ProviderProfileID     string          `json:"provider_profile_id"`
	ClaimToken            string          `json:"claim_token"`
	HardwareSKU           string          `json:"hardware_sku"`
	ChipID                string          `json:"chip_id"`
	FirmwareVersion       string          `json:"firmware_version"`
	FirmwareChannel       string          `json:"firmware_channel"`
	ConfigVersion         int64           `json:"config_version"`
	ReportedConfigVersion int64           `json:"reported_config_version"`
	Config                map[string]any  `json:"config"`
	Capabilities          map[string]any  `json:"capabilities"`
	LastTelemetry         json.RawMessage `json:"last_telemetry,omitempty"`
}

// NewLocalControl creates or loads a file-backed control plane with demo seed data.
func NewLocalControl(cfg LocalControlConfig) (*LocalControl, error) {
	if cfg.DataDir == "" {
		return nil, fmt.Errorf("local control: data dir is required")
	}
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return nil, fmt.Errorf("local control: mkdir %s: %w", cfg.DataDir, err)
	}

	lc := &LocalControl{
		cfg:     cfg,
		dataDir: cfg.DataDir,
		path:    filepath.Join(cfg.DataDir, "state.json"),
		state: localState{
			Devices: make(map[string]*localDevice),
			Tokens:  make(map[string]string),
		},
	}

	if err := lc.load(); err != nil {
		return nil, err
	}
	lc.seedDemoDevice()
	if err := lc.saveLocked(); err != nil {
		return nil, err
	}
	return lc, nil
}

// Bootstrap implements ControlPlane.
func (lc *LocalControl) Bootstrap(_ context.Context, req BootstrapRequest) (BootstrapResponse, error) {
	if strings.TrimSpace(req.DeviceID) == "" || strings.TrimSpace(req.ClaimToken) == "" {
		return BootstrapResponse{}, ErrInvalidInput
	}

	lc.mu.Lock()
	defer lc.mu.Unlock()

	device, ok := lc.state.Devices[req.DeviceID]
	if !ok {
		return BootstrapResponse{}, ErrNotFound
	}
	if subtleTrim(device.ClaimToken) != subtleTrim(req.ClaimToken) {
		return BootstrapResponse{}, ErrUnauthorized
	}

	device.ChipID = req.ChipID
	device.HardwareSKU = localFirstNonEmpty(req.HardwareSKU, device.HardwareSKU)
	device.FirmwareVersion = req.FirmwareVersion
	device.FirmwareChannel = req.FirmwareChannel
	if len(req.Capabilities) > 0 {
		device.Capabilities = cloneMap(req.Capabilities)
	}

	token, err := newToken("devtok")
	if err != nil {
		return BootstrapResponse{}, fmt.Errorf("bootstrap: issue token: %w", err)
	}
	lc.state.Tokens[token] = device.DeviceID

	if err := lc.saveLocked(); err != nil {
		return BootstrapResponse{}, err
	}

	return BootstrapResponse{
		TenantID:      device.TenantID,
		InstanceID:    device.InstanceID,
		DeviceID:      device.DeviceID,
		DeviceToken:   token,
		WSURL:         wsURL(lc.cfg.PublicBaseURL),
		ConfigVersion: device.ConfigVersion,
		Config:        cloneMap(device.Config),
		RetryHintSec:  lc.cfg.RetryHintSec,
	}, nil
}

// AuthenticateDeviceToken implements ControlPlane.
func (lc *LocalControl) AuthenticateDeviceToken(_ context.Context, expectedDeviceID, token string) (DeviceContext, error) {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	deviceID, ok := lc.state.Tokens[token]
	if !ok {
		return DeviceContext{}, ErrUnauthorized
	}
	if expectedDeviceID != "" && expectedDeviceID != deviceID {
		return DeviceContext{}, ErrUnauthorized
	}
	device, ok := lc.state.Devices[deviceID]
	if !ok {
		return DeviceContext{}, ErrUnauthorized
	}
	return DeviceContext{
		DeviceID:            device.DeviceID,
		TenantID:            device.TenantID,
		InstanceID:          device.InstanceID,
		ProviderProfileID:   device.ProviderProfileID,
		ConfigVersion:       device.ConfigVersion,
		ReportedConfig:      device.ReportedConfigVersion,
		Config:              cloneMap(device.Config),
		Capabilities:        cloneMap(device.Capabilities),
		FirmwareVersion:     device.FirmwareVersion,
		FirmwareChannel:     device.FirmwareChannel,
		DefaultAudioCodec:   "pcm_s16le",
		DefaultSampleRateHz: 16000,
	}, nil
}

// UpdateTelemetry implements ControlPlane.
func (lc *LocalControl) UpdateTelemetry(_ context.Context, deviceID string, telemetry json.RawMessage) error {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	device, ok := lc.state.Devices[deviceID]
	if !ok {
		return ErrNotFound
	}
	device.LastTelemetry = append([]byte(nil), telemetry...)
	return lc.saveLocked()
}

// AckConfig implements ControlPlane.
func (lc *LocalControl) AckConfig(_ context.Context, deviceID string, version int64) error {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	device, ok := lc.state.Devices[deviceID]
	if !ok {
		return ErrNotFound
	}
	device.ReportedConfigVersion = version
	return lc.saveLocked()
}

// DemoInfo exposes the seeded demo credentials for local docs or startup logs.
func (lc *LocalControl) DemoInfo() map[string]string {
	return map[string]string{
		"device_id":   lc.cfg.DemoDeviceID,
		"claim_token": lc.cfg.DemoClaimToken,
	}
}

func (lc *LocalControl) load() error {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	data, err := os.ReadFile(lc.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("local control: read state: %w", err)
	}
	if len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, &lc.state); err != nil {
		return fmt.Errorf("local control: decode state: %w", err)
	}
	if lc.state.Devices == nil {
		lc.state.Devices = make(map[string]*localDevice)
	}
	if lc.state.Tokens == nil {
		lc.state.Tokens = make(map[string]string)
	}
	return nil
}

func (lc *LocalControl) seedDemoDevice() {
	if _, ok := lc.state.Devices[lc.cfg.DemoDeviceID]; ok {
		return
	}
	lc.state.Devices[lc.cfg.DemoDeviceID] = &localDevice{
		DeviceID:          lc.cfg.DemoDeviceID,
		TenantID:          lc.cfg.DemoTenantID,
		InstanceID:        lc.cfg.DemoInstanceID,
		ProviderProfileID: lc.cfg.DemoProviderProfileID,
		ClaimToken:        lc.cfg.DemoClaimToken,
		HardwareSKU:       "m5cores3-facekit-audio",
		ConfigVersion:     1,
		Config: map[string]any{
			"voice": map[string]any{
				"enabled":        true,
				"codec":          "pcm_s16le",
				"sample_rate_hz": 16000,
				"channels":       1,
			},
			"providers": map[string]any{
				"profile": lc.cfg.DemoProviderProfileID,
			},
			"features": map[string]any{
				"voice": true,
				"hid":   true,
			},
			"debug": map[string]any{
				"mock_mode": true,
			},
		},
		Capabilities: map[string]any{
			"display": "lcd",
			"touch":   true,
			"usb_hid": true,
			"usb_mic": true,
		},
	}
}

func (lc *LocalControl) saveLocked() error {
	data, err := json.MarshalIndent(lc.state, "", "  ")
	if err != nil {
		return fmt.Errorf("local control: encode state: %w", err)
	}
	if err := os.WriteFile(lc.path, data, 0o644); err != nil {
		return fmt.Errorf("local control: write state: %w", err)
	}
	return nil
}

func wsURL(base string) string {
	parsed, err := url.Parse(base)
	if err != nil {
		return "ws://127.0.0.1:8080/v1/session/ws"
	}
	switch parsed.Scheme {
	case "http":
		parsed.Scheme = "ws"
	case "https":
		parsed.Scheme = "wss"
	}
	parsed.Path = "/v1/session/ws"
	parsed.RawQuery = ""
	return parsed.String()
}

func newToken(prefix string) (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return prefix + "_" + hex.EncodeToString(buf[:]), nil
}

func subtleTrim(value string) string {
	return strings.TrimSpace(value)
}

func localFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func cloneMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	data, err := json.Marshal(input)
	if err != nil {
		out := make(map[string]any, len(input))
		for key, value := range input {
			out[key] = value
		}
		return out
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		out = make(map[string]any, len(input))
		for key, value := range input {
			out[key] = value
		}
	}
	return out
}
