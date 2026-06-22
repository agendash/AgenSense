package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/agendash/AgenSense/internal/debugtrace"
	"github.com/agendash/AgenSense/internal/service"
)

type fakeControl struct {
	deviceCtx service.DeviceContext
}

type fakeRegistry struct {
	profiles []service.ProviderProfileResponse
}

func (f fakeRegistry) UpsertProviderProfile(context.Context, string, service.ProviderProfileRequest) (service.ProviderProfileResponse, error) {
	return service.ProviderProfileResponse{
		ID:        "mock-default",
		Namespace: "apikey_deadbeef",
		Default:   true,
		LLMModel:  "mock-llm",
	}, nil
}

func (f fakeRegistry) ListProviderProfiles(context.Context, string) ([]service.ProviderProfileResponse, error) {
	if f.profiles != nil {
		return f.profiles, nil
	}
	return []service.ProviderProfileResponse{{
		ID:        "mock-default",
		Namespace: "apikey_deadbeef",
		Default:   true,
	}}, nil
}

func (f fakeRegistry) GetProviderProfile(context.Context, string, string) (service.ProviderProfileResponse, error) {
	return service.ProviderProfileResponse{
		ID:        "mock-default",
		Namespace: "apikey_deadbeef",
		Default:   true,
	}, nil
}

type fakeInference struct{}

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

func (fakeInference) Transcribe(context.Context, string, service.ASRInferenceRequest) (service.ASRInferenceResponse, error) {
	return service.ASRInferenceResponse{
		ProviderProfileID: "mock-default",
		Text:              "mock transcript",
	}, nil
}

func (fakeInference) Chat(context.Context, string, service.ChatInferenceRequest) (service.ChatInferenceResponse, error) {
	return service.ChatInferenceResponse{
		ProviderProfileID: "mock-default",
		Text:              "mock reply",
		Deltas:            []string{"mock ", "reply"},
	}, nil
}

func (fakeInference) ChatStream(_ context.Context, _ string, _ service.ChatInferenceRequest, cb service.ChatDeltaCallback) (service.ChatInferenceResponse, error) {
	deltas := []string{"mock ", "reply"}
	for _, delta := range deltas {
		if cb != nil {
			if err := cb(delta); err != nil {
				return service.ChatInferenceResponse{}, err
			}
		}
	}
	return service.ChatInferenceResponse{
		ProviderProfileID: "mock-default",
		Text:              "mock reply",
		Deltas:            deltas,
	}, nil
}

func (fakeInference) CompleteMultimodal(context.Context, string, service.MultimodalInferenceRequest) (service.MultimodalInferenceResponse, error) {
	return service.MultimodalInferenceResponse{
		ProviderProfileID: "mock-default",
		Text:              "mock multimodal reply",
	}, nil
}

func (fakeInference) AnalyzeVision(context.Context, string, service.VisionInferenceRequest) (service.VisionInferenceResponse, error) {
	return service.VisionInferenceResponse{
		ProviderProfileID: "mock-default",
		Text:              "mock vision reply",
		ImageCount:        1,
	}, nil
}

func (fakeInference) Synthesize(context.Context, string, service.TTSInferenceRequest) (service.TTSInferenceResponse, error) {
	return service.TTSInferenceResponse{
		ProviderProfileID: "mock-default",
		Format:            service.AudioFormatInput{Codec: "pcm_s16le", SampleRateHz: 16000, Channels: 1},
		AudioBase64:       "AQID",
		ChunkCount:        1,
	}, nil
}

func TestBootstrapRoute(t *testing.T) {
	t.Parallel()

	handler := NewRouter(fakeControl{}, fakeRegistry{}, fakeInference{}, nil, nil, nil)
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
	}, fakeRegistry{}, fakeInference{}, nil, nil, nil)
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

func TestProviderRegistryRoute(t *testing.T) {
	t.Parallel()

	handler := NewRouter(fakeControl{}, fakeRegistry{}, fakeInference{}, nil, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/v1/providers", bytes.NewBufferString(`{"id":"mock-default","llm_model":"mock-llm","default":true}`))
	req.Header.Set("Authorization", "Bearer test-api-key")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body, _ := io.ReadAll(rec.Body)
	if !bytes.Contains(body, []byte(`"id":"mock-default"`)) {
		t.Fatalf("unexpected body: %s", body)
	}
}

func TestDirectLLMRoute(t *testing.T) {
	t.Parallel()

	handler := NewRouter(fakeControl{}, fakeRegistry{}, fakeInference{}, nil, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/v1/llm/chat", bytes.NewBufferString(`{"provider_profile_id":"mock-default","messages":[{"role":"user","content":"hello"}]}`))
	req.Header.Set("Authorization", "Bearer test-api-key")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body, _ := io.ReadAll(rec.Body)
	if !bytes.Contains(body, []byte(`"text":"mock reply"`)) {
		t.Fatalf("unexpected body: %s", body)
	}
}

func TestDirectLLMStreamRoute(t *testing.T) {
	t.Parallel()

	handler := NewRouter(fakeControl{}, fakeRegistry{}, fakeInference{}, nil, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/v1/llm/chat/stream", bytes.NewBufferString(`{"provider_profile_id":"mock-default","messages":[{"role":"user","content":"hello"}]}`))
	req.Header.Set("Authorization", "Bearer test-api-key")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "text/event-stream") {
		t.Fatalf("content-type = %q, want text/event-stream", got)
	}
	body, _ := io.ReadAll(rec.Body)
	if !bytes.Contains(body, []byte("event: delta")) || !bytes.Contains(body, []byte(`"text":"mock "`)) {
		t.Fatalf("missing delta event: %s", body)
	}
	if !bytes.Contains(body, []byte("event: done")) || !bytes.Contains(body, []byte(`"text":"mock reply"`)) {
		t.Fatalf("missing done event: %s", body)
	}
}

func TestDirectMultimodalRoute(t *testing.T) {
	t.Parallel()

	handler := NewRouter(fakeControl{}, fakeRegistry{}, fakeInference{}, nil, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/v1/multimodal/chat", bytes.NewBufferString(`{"provider_profile_id":"mock-default","messages":[{"role":"user","content":[{"type":"text","text":"what is this"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AQID"}}]}]}`))
	req.Header.Set("Authorization", "Bearer test-api-key")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body, _ := io.ReadAll(rec.Body)
	if !bytes.Contains(body, []byte(`"text":"mock multimodal reply"`)) {
		t.Fatalf("unexpected body: %s", body)
	}
}

func TestDirectVisionRoute(t *testing.T) {
	t.Parallel()

	handler := NewRouter(fakeControl{}, fakeRegistry{}, fakeInference{}, nil, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/v1/vision/analyze", bytes.NewBufferString(`{"provider_profile_id":"mock-default","prompt":"what is this","images":[{"image_base64":"AQID","mime_type":"image/png"}]}`))
	req.Header.Set("Authorization", "Bearer test-api-key")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body, _ := io.ReadAll(rec.Body)
	if !bytes.Contains(body, []byte(`"text":"mock vision reply"`)) || !bytes.Contains(body, []byte(`"image_count":1`)) {
		t.Fatalf("unexpected body: %s", body)
	}
}

func TestDebugTraceRoutes(t *testing.T) {
	t.Parallel()

	debugStore := debugtrace.NewStore(8)
	handle := debugStore.StartTrace(debugtrace.KindLLM, debugtrace.SourceHTTP, debugtrace.TraceMeta{
		ClientID:  "client-001",
		SessionID: "sess-001",
		HTTPPath:  "/v1/llm/chat",
	})
	handle.StartLLM(nil)
	handle.AddLLMDelta("hello")
	handle.CompleteLLM("hello")
	handle.Complete()

	handler := NewRouter(fakeControl{}, fakeRegistry{}, fakeInference{}, nil, nil, debugStore)

	listReq := httptest.NewRequest(http.MethodGet, "/debug/api/traces", nil)
	listRec := httptest.NewRecorder()
	handler.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d", listRec.Code, http.StatusOK)
	}
	listBody, _ := io.ReadAll(listRec.Body)
	if !bytes.Contains(listBody, []byte(`"kind":"llm"`)) {
		t.Fatalf("unexpected list body: %s", listBody)
	}

	pageReq := httptest.NewRequest(http.MethodGet, "/debug/traces", nil)
	pageRec := httptest.NewRecorder()
	handler.ServeHTTP(pageRec, pageReq)
	if pageRec.Code != http.StatusOK {
		t.Fatalf("page status = %d, want %d", pageRec.Code, http.StatusOK)
	}
	if !bytes.Contains(pageRec.Body.Bytes(), []byte("Debug Traces")) {
		t.Fatalf("unexpected page body: %s", pageRec.Body.Bytes())
	}
}

func TestDebugTraceRoutesDisabled(t *testing.T) {
	t.Parallel()

	handler := NewRouter(fakeControl{}, fakeRegistry{}, fakeInference{}, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/debug/api/traces", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}
