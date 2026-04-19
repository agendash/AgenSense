package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zhuzhe/agensense/internal/service"
)

type fakeControl struct {
	deviceCtx service.DeviceContext
}

func (f fakeControl) Bootstrap(context.Context, service.BootstrapRequest) (service.BootstrapResponse, error) {
	return service.BootstrapResponse{
		DeviceID:      "dev-001",
		DeviceToken:   "token-001",
		WSURL:         "ws://localhost:8080/v1/session/ws",
		ConfigVersion: 1,
		Config: map[string]any{
			"voice": map[string]any{"enabled": true},
		},
		RetryHintSec: 30,
	}, nil
}

func (f fakeControl) AuthenticateDeviceToken(context.Context, string, string) (service.DeviceContext, error) {
	return f.deviceCtx, nil
}

func (fakeControl) UpdateTelemetry(context.Context, string, json.RawMessage) error {
	return nil
}

func (fakeControl) AckConfig(context.Context, string, int64) error {
	return nil
}

func TestBootstrapRoute(t *testing.T) {
	t.Parallel()

	handler := NewRouter(fakeControl{}, nil)
	req := httptest.NewRequest(http.MethodPost, "/v1/bootstrap", bytes.NewBufferString(`{"device_id":"dev-001"}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body, _ := io.ReadAll(rec.Body)
	if !bytes.Contains(body, []byte(`"device_token":"token-001"`)) {
		t.Fatalf("unexpected body: %s", body)
	}
}

func TestConfigRoute(t *testing.T) {
	t.Parallel()

	handler := NewRouter(fakeControl{
		deviceCtx: service.DeviceContext{
			DeviceID:          "dev-001",
			TenantID:          "tenant-001",
			InstanceID:        "inst-001",
			ProviderProfileID: "mock-default",
			ConfigVersion:     2,
			ReportedConfig:    1,
			Config:            map[string]any{"voice": map[string]any{"enabled": true}},
		},
	}, nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/device/config", nil)
	req.Header.Set("Authorization", "Bearer token-001")
	req.Header.Set("X-Device-Id", "dev-001")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body, _ := io.ReadAll(rec.Body)
	if !bytes.Contains(body, []byte(`"provider_profile_id":"mock-default"`)) {
		t.Fatalf("unexpected body: %s", body)
	}
}
