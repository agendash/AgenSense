package service

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestLocalControlBootstrapAndAuth(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	lc, err := NewLocalControl(LocalControlConfig{
		DataDir:               filepath.Join(dir, "data"),
		PublicBaseURL:         "http://127.0.0.1:8080",
		DemoDeviceID:          "demo-device-001",
		DemoClaimToken:        "demo-claim-token",
		DemoTenantID:          "tenant-001",
		DemoInstanceID:        "inst-001",
		DemoProviderProfileID: "mock-default",
		RetryHintSec:          30,
	})
	if err != nil {
		t.Fatalf("NewLocalControl() error = %v", err)
	}

	bootstrap, err := lc.Bootstrap(context.Background(), BootstrapRequest{
		DeviceID:   "demo-device-001",
		ClaimToken: "demo-claim-token",
	})
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	if bootstrap.DeviceToken == "" {
		t.Fatal("expected device token")
	}

	deviceCtx, err := lc.AuthenticateDeviceToken(context.Background(), "demo-device-001", bootstrap.DeviceToken)
	if err != nil {
		t.Fatalf("AuthenticateDeviceToken() error = %v", err)
	}
	if deviceCtx.DeviceID != "demo-device-001" {
		t.Fatalf("device id = %q, want demo-device-001", deviceCtx.DeviceID)
	}

	if err := lc.UpdateTelemetry(context.Background(), deviceCtx.DeviceID, json.RawMessage(`{"battery":87}`)); err != nil {
		t.Fatalf("UpdateTelemetry() error = %v", err)
	}
	if err := lc.AckConfig(context.Background(), deviceCtx.DeviceID, deviceCtx.ConfigVersion); err != nil {
		t.Fatalf("AckConfig() error = %v", err)
	}
}
