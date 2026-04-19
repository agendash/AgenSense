package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/url"
	"time"

	"github.com/zhuzhe/agensense/internal/device"
)

// DeviceControl adapts the domain-level device service to the HTTP/WebSocket control-plane contract.
type DeviceControl struct {
	devices      *device.Service
	repo         device.Repository
	retryHintSec int
	publicBaseURL string
}

// NewDeviceControl constructs a control-plane adapter around the device domain service.
func NewDeviceControl(devices *device.Service, repo device.Repository, retryHintSec int) *DeviceControl {
	return &DeviceControl{
		devices:      devices,
		repo:         repo,
		retryHintSec: retryHintSec,
	}
}

// SetPublicBaseURL overrides the ws_url returned by bootstrap responses.
func (c *DeviceControl) SetPublicBaseURL(baseURL string) {
	c.publicBaseURL = baseURL
}

// Bootstrap implements ControlPlane.
func (c *DeviceControl) Bootstrap(ctx context.Context, req BootstrapRequest) (BootstrapResponse, error) {
	if c.devices == nil {
		return BootstrapResponse{}, ErrInvalidInput
	}
	slog.InfoContext(ctx, "device bootstrap requested",
		"device_id", req.DeviceID,
		"chip_id", req.ChipID,
		"hardware_sku", req.HardwareSKU,
		"firmware_version", req.FirmwareVersion,
		"firmware_channel", req.FirmwareChannel,
	)
	capabilities, err := json.Marshal(req.Capabilities)
	if err != nil {
		slog.WarnContext(ctx, "device bootstrap rejected: invalid capabilities payload",
			"device_id", req.DeviceID,
			"error", err,
		)
		return BootstrapResponse{}, ErrInvalidInput
	}

	result, err := c.devices.Bootstrap(ctx, device.BootstrapRequest{
		DeviceID:        req.DeviceID,
		ChipID:          req.ChipID,
		HardwareSKU:     req.HardwareSKU,
		FirmwareVersion: req.FirmwareVersion,
		FirmwareChannel: req.FirmwareChannel,
		Capabilities:    capabilities,
		ClaimToken:      req.ClaimToken,
	})
	if err != nil {
		slog.WarnContext(ctx, "device bootstrap failed",
			"device_id", req.DeviceID,
			"chip_id", req.ChipID,
			"hardware_sku", req.HardwareSKU,
			"error", err,
		)
		return BootstrapResponse{}, mapDeviceError(err)
	}

	resp := BootstrapResponse{
		TenantID:      result.Device.TenantID,
		InstanceID:    result.Instance.ID,
		DeviceID:      result.Device.ID,
		DeviceToken:   result.TokenValue,
		WSURL:         c.gatewayWSURL(result.Instance.GatewayWSURL),
		ConfigVersion: result.ConfigSnapshot.Version,
		Config:        rawToMap(result.ConfigSnapshot.Config),
		RetryHintSec:  c.retryHintSec,
	}

	slog.InfoContext(ctx, "device bootstrap completed",
		"device_id", resp.DeviceID,
		"tenant_id", resp.TenantID,
		"instance_id", resp.InstanceID,
		"provider_profile_id", result.ProviderProfile.ID,
		"config_version", resp.ConfigVersion,
		"ws_url", resp.WSURL,
	)

	return resp, nil
}

// AuthenticateDeviceToken implements ControlPlane.
func (c *DeviceControl) AuthenticateDeviceToken(ctx context.Context, deviceID, token string) (DeviceContext, error) {
	if c.devices == nil {
		return DeviceContext{}, ErrInvalidInput
	}

	auth, err := c.devices.AuthenticateDeviceToken(ctx, deviceID, token)
	if err != nil {
		slog.WarnContext(ctx, "device token authentication failed",
			"device_id", deviceID,
			"error", err,
		)
		return DeviceContext{}, mapDeviceError(err)
	}
	snapshot, err := c.devices.CurrentConfig(ctx, auth.Device.ID)
	if err != nil {
		slog.WarnContext(ctx, "device config lookup failed after authentication",
			"device_id", auth.Device.ID,
			"error", err,
		)
		return DeviceContext{}, mapDeviceError(err)
	}
	instance, err := c.repo.GetInstance(ctx, auth.Device.InstanceID)
	if err != nil {
		slog.WarnContext(ctx, "instance lookup failed after authentication",
			"device_id", auth.Device.ID,
			"instance_id", auth.Device.InstanceID,
			"error", err,
		)
		return DeviceContext{}, mapDeviceError(err)
	}

	deviceCtx := DeviceContext{
		DeviceID:            auth.Device.ID,
		TenantID:            auth.Device.TenantID,
		InstanceID:          auth.Device.InstanceID,
		ProviderProfileID:   firstNonEmpty(snapshot.ProviderProfileID, instance.ProviderProfileID),
		ConfigVersion:       snapshot.Version,
		ReportedConfig:      auth.Device.ReportedConfigVersion,
		Config:              rawToMap(snapshot.Config),
		Capabilities:        rawToMap(auth.Device.Capabilities),
		FirmwareVersion:     auth.Device.FirmwareVersion,
		FirmwareChannel:     auth.Device.FirmwareChannel,
		DefaultAudioCodec:   "pcm_s16le",
		DefaultSampleRateHz: 16000,
	}

	slog.DebugContext(ctx, "device token authenticated",
		"device_id", deviceCtx.DeviceID,
		"tenant_id", deviceCtx.TenantID,
		"instance_id", deviceCtx.InstanceID,
		"provider_profile_id", deviceCtx.ProviderProfileID,
		"config_version", deviceCtx.ConfigVersion,
	)

	return deviceCtx, nil
}

// UpdateTelemetry implements ControlPlane.
func (c *DeviceControl) UpdateTelemetry(ctx context.Context, deviceID string, _ json.RawMessage) error {
	if c.repo == nil {
		return nil
	}

	dev, err := c.repo.GetDevice(ctx, deviceID)
	if err != nil {
		slog.WarnContext(ctx, "telemetry update rejected",
			"device_id", deviceID,
			"error", err,
		)
		return mapDeviceError(err)
	}
	now := time.Now().UTC()
	dev.LastSeenAt = &now
	dev.UpdatedAt = now
	_, err = c.repo.SaveDevice(ctx, dev)
	if err == nil {
		slog.DebugContext(ctx, "device telemetry updated", "device_id", deviceID)
	}
	return mapDeviceError(err)
}

// AckConfig implements ControlPlane.
func (c *DeviceControl) AckConfig(ctx context.Context, deviceID string, version int64) error {
	if c.devices == nil {
		return ErrInvalidInput
	}
	_, err := c.devices.AckConfigVersion(ctx, deviceID, version)
	if err != nil {
		slog.WarnContext(ctx, "device config ack failed",
			"device_id", deviceID,
			"config_version", version,
			"error", err,
		)
	} else {
		slog.InfoContext(ctx, "device config acknowledged",
			"device_id", deviceID,
			"config_version", version,
		)
	}
	return mapDeviceError(err)
}

func mapDeviceError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, device.ErrInvalidInput):
		return ErrInvalidInput
	case errors.Is(err, device.ErrUnauthorized), errors.Is(err, device.ErrExpired):
		return ErrUnauthorized
	case errors.Is(err, device.ErrNotFound), errors.Is(err, device.ErrConfigMissing):
		return ErrNotFound
	default:
		return err
	}
}

func rawToMap(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]any{
			"_raw": string(raw),
		}
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func (c *DeviceControl) gatewayWSURL(fallback string) string {
	if c.publicBaseURL == "" {
		return fallback
	}
	parsed, err := url.Parse(c.publicBaseURL)
	if err != nil {
		return fallback
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
