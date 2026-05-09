package httpapi

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/agendash/AgenSense/internal/debugtrace"
	"github.com/agendash/AgenSense/internal/service"
)

// Router exposes the control-plane HTTP endpoints for the MVP.
type Router struct {
	control   service.ControlPlane
	registry  service.ProviderRegistry
	inference service.InferenceService
	sessionWS http.Handler
	voiceWS   http.Handler
	debug     *debugtrace.Store
}

// NewRouter builds an HTTP router for device control, provider registry, and direct inference traffic.
func NewRouter(control service.ControlPlane, registry service.ProviderRegistry, inference service.InferenceService, sessionWS http.Handler, voiceWS http.Handler, debug *debugtrace.Store) http.Handler {
	r := &Router{
		control:   control,
		registry:  registry,
		inference: inference,
		sessionWS: sessionWS,
		voiceWS:   voiceWS,
		debug:     debug,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", r.handleHealthz)
	if debug != nil {
		mux.HandleFunc("/debug/traces", r.handleDebugTracesPage)
		mux.HandleFunc("/debug/api/traces", r.handleDebugTracesList)
		mux.HandleFunc("/debug/api/traces/", r.handleDebugTraceByID)
		mux.HandleFunc("/debug/assets/", r.handleDebugTraceAsset)
	}
	mux.HandleFunc("/v1/bootstrap", r.handleBootstrap)
	mux.HandleFunc("/v1/device/config", r.handleDeviceConfig)
	mux.HandleFunc("/v1/device/telemetry", r.handleDeviceTelemetry)
	mux.HandleFunc("/v1/providers", r.handleProviders)
	mux.HandleFunc("/v1/providers/", r.handleProviderByID)
	mux.HandleFunc("/v1/asr/transcribe", r.handleASRTranscribe)
	mux.HandleFunc("/v1/llm/chat", r.handleLLMChat)
	mux.HandleFunc("/v1/llm/chat/stream", r.handleLLMChatStream)
	mux.HandleFunc("/v1/multimodal/chat", r.handleMultimodalChat)
	mux.HandleFunc("/v1/vision/analyze", r.handleVisionAnalyze)
	mux.HandleFunc("/v1/tts/synthesize", r.handleTTSSynthesize)
	if sessionWS != nil {
		mux.Handle("/v1/session/ws", sessionWS)
	}
	if voiceWS != nil {
		mux.Handle("/v1/voice/ws", voiceWS)
	}
	return loggingMiddleware(mux)
}

func (r *Router) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true,
	})
}

func (r *Router) handleDebugTracesPage(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	if r.debug == nil {
		writeError(w, http.StatusServiceUnavailable, "debug_trace_unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(debugTracePage))
}

func (r *Router) handleDebugTracesList(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	if r.debug == nil {
		writeError(w, http.StatusServiceUnavailable, "debug_trace_unavailable")
		return
	}
	traces := r.debug.List()
	items := make([]traceResponse, 0, len(traces))
	for _, trace := range traces {
		items = append(items, newTraceResponse(trace))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (r *Router) handleDebugTraceByID(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	if r.debug == nil {
		writeError(w, http.StatusServiceUnavailable, "debug_trace_unavailable")
		return
	}
	traceID := strings.TrimPrefix(req.URL.Path, "/debug/api/traces/")
	if traceID == "" {
		writeError(w, http.StatusBadRequest, "missing_trace_id")
		return
	}
	trace, ok := r.debug.Get(traceID)
	if !ok {
		writeError(w, http.StatusNotFound, "trace_not_found")
		return
	}
	writeJSON(w, http.StatusOK, newTraceResponse(trace))
}

func (r *Router) handleDebugTraceAsset(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	if r.debug == nil {
		writeError(w, http.StatusServiceUnavailable, "debug_trace_unavailable")
		return
	}

	path := strings.TrimPrefix(req.URL.Path, "/debug/assets/")
	parts := strings.Split(path, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		writeError(w, http.StatusBadRequest, "invalid_asset_path")
		return
	}

	data, mime, ok := r.debug.ReadAsset(parts[0], parts[1])
	if !ok {
		writeError(w, http.StatusNotFound, "asset_not_found")
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (r *Router) handleBootstrap(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	if r.control == nil {
		writeError(w, http.StatusServiceUnavailable, "control_plane_unavailable")
		return
	}

	var input service.BootstrapRequest
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	resp, err := r.control.Bootstrap(req.Context(), input)
	if err != nil {
		status := mapErrorStatus(err)
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (r *Router) handleDeviceConfig(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	if r.control == nil {
		writeError(w, http.StatusServiceUnavailable, "control_plane_unavailable")
		return
	}

	ctx, err := r.authenticate(req)
	if err != nil {
		writeError(w, mapErrorStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"device_id":               ctx.DeviceID,
		"tenant_id":               ctx.TenantID,
		"instance_id":             ctx.InstanceID,
		"provider_profile_id":     ctx.ProviderProfileID,
		"desired_config_version":  ctx.ConfigVersion,
		"reported_config_version": ctx.ReportedConfig,
		"config":                  ctx.Config,
	})
}

func (r *Router) handleDeviceTelemetry(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	if r.control == nil {
		writeError(w, http.StatusServiceUnavailable, "control_plane_unavailable")
		return
	}

	ctx, err := r.authenticate(req)
	if err != nil {
		writeError(w, mapErrorStatus(err), err.Error())
		return
	}

	var payload json.RawMessage
	if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	if err := r.control.UpdateTelemetry(req.Context(), ctx.DeviceID, payload); err != nil {
		writeError(w, mapErrorStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"ok": true,
	})
}

func (r *Router) handleProviders(w http.ResponseWriter, req *http.Request) {
	if r.registry == nil {
		writeError(w, http.StatusServiceUnavailable, "provider_registry_unavailable")
		return
	}

	apiKey := bearerToken(req.Header.Get("Authorization"))
	if apiKey == "" {
		writeError(w, http.StatusUnauthorized, "missing_api_key")
		return
	}

	switch req.Method {
	case http.MethodGet:
		profiles, err := r.registry.ListProviderProfiles(req.Context(), apiKey)
		if err != nil {
			writeError(w, mapErrorStatus(err), err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"items": profiles,
		})
	case http.MethodPost, http.MethodPut:
		var input service.ProviderProfileRequest
		if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json")
			return
		}
		profile, err := r.registry.UpsertProviderProfile(req.Context(), apiKey, input)
		if err != nil {
			writeError(w, mapErrorStatus(err), err.Error())
			return
		}
		writeJSON(w, http.StatusOK, profile)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
	}
}

func (r *Router) handleProviderByID(w http.ResponseWriter, req *http.Request) {
	if r.registry == nil {
		writeError(w, http.StatusServiceUnavailable, "provider_registry_unavailable")
		return
	}

	apiKey := bearerToken(req.Header.Get("Authorization"))
	if apiKey == "" {
		writeError(w, http.StatusUnauthorized, "missing_api_key")
		return
	}
	profileID := strings.TrimPrefix(req.URL.Path, "/v1/providers/")
	if strings.TrimSpace(profileID) == "" {
		writeError(w, http.StatusBadRequest, "missing_provider_profile_id")
		return
	}

	switch req.Method {
	case http.MethodGet:
		profile, err := r.registry.GetProviderProfile(req.Context(), apiKey, profileID)
		if err != nil {
			writeError(w, mapErrorStatus(err), err.Error())
			return
		}
		writeJSON(w, http.StatusOK, profile)
	case http.MethodPatch:
		var input service.ProviderProfileRequest
		if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json")
			return
		}
		if strings.TrimSpace(input.ID) == "" {
			input.ID = profileID
		}
		profile, err := r.registry.UpsertProviderProfile(req.Context(), apiKey, input)
		if err != nil {
			writeError(w, mapErrorStatus(err), err.Error())
			return
		}
		writeJSON(w, http.StatusOK, profile)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
	}
}

func (r *Router) handleASRTranscribe(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	if r.inference == nil {
		writeError(w, http.StatusServiceUnavailable, "inference_unavailable")
		return
	}
	apiKey := bearerToken(req.Header.Get("Authorization"))
	if apiKey == "" {
		writeError(w, http.StatusUnauthorized, "missing_api_key")
		return
	}
	var input service.ASRInferenceRequest
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	resp, err := r.inference.Transcribe(req.Context(), apiKey, input)
	if err != nil {
		writeError(w, mapErrorStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (r *Router) handleLLMChat(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	if r.inference == nil {
		writeError(w, http.StatusServiceUnavailable, "inference_unavailable")
		return
	}
	apiKey := bearerToken(req.Header.Get("Authorization"))
	if apiKey == "" {
		writeError(w, http.StatusUnauthorized, "missing_api_key")
		return
	}
	var input service.ChatInferenceRequest
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	resp, err := r.inference.Chat(req.Context(), apiKey, input)
	if err != nil {
		writeError(w, mapErrorStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (r *Router) handleLLMChatStream(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	if r.inference == nil {
		writeError(w, http.StatusServiceUnavailable, "inference_unavailable")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming_unavailable")
		return
	}
	apiKey := bearerToken(req.Header.Get("Authorization"))
	if apiKey == "" {
		writeError(w, http.StatusUnauthorized, "missing_api_key")
		return
	}
	var input service.ChatInferenceRequest
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	resp, err := r.inference.ChatStream(req.Context(), apiKey, input, func(delta string) error {
		if err := writeSSE(w, "delta", map[string]string{"text": delta}); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	})
	if err != nil {
		_ = writeSSE(w, "error", map[string]string{"error": err.Error()})
		flusher.Flush()
		return
	}
	_ = writeSSE(w, "done", resp)
	flusher.Flush()
}

func (r *Router) handleMultimodalChat(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	if r.inference == nil {
		writeError(w, http.StatusServiceUnavailable, "inference_unavailable")
		return
	}
	apiKey := bearerToken(req.Header.Get("Authorization"))
	if apiKey == "" {
		writeError(w, http.StatusUnauthorized, "missing_api_key")
		return
	}
	var input service.MultimodalInferenceRequest
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	resp, err := r.inference.CompleteMultimodal(req.Context(), apiKey, input)
	if err != nil {
		writeError(w, mapErrorStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (r *Router) handleVisionAnalyze(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	if r.inference == nil {
		writeError(w, http.StatusServiceUnavailable, "inference_unavailable")
		return
	}
	apiKey := bearerToken(req.Header.Get("Authorization"))
	if apiKey == "" {
		writeError(w, http.StatusUnauthorized, "missing_api_key")
		return
	}
	var input service.VisionInferenceRequest
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	resp, err := r.inference.AnalyzeVision(req.Context(), apiKey, input)
	if err != nil {
		writeError(w, mapErrorStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (r *Router) handleTTSSynthesize(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	if r.inference == nil {
		writeError(w, http.StatusServiceUnavailable, "inference_unavailable")
		return
	}
	apiKey := bearerToken(req.Header.Get("Authorization"))
	if apiKey == "" {
		writeError(w, http.StatusUnauthorized, "missing_api_key")
		return
	}
	var input service.TTSInferenceRequest
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	resp, err := r.inference.Synthesize(req.Context(), apiKey, input)
	if err != nil {
		writeError(w, mapErrorStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (r *Router) authenticate(req *http.Request) (service.DeviceContext, error) {
	token := bearerToken(req.Header.Get("Authorization"))
	if token == "" {
		return service.DeviceContext{}, service.ErrUnauthorized
	}
	deviceID := req.Header.Get("X-Device-Id")
	if deviceID == "" {
		deviceID = req.URL.Query().Get("device_id")
	}
	if deviceID == "" {
		return service.DeviceContext{}, service.ErrUnauthorized
	}
	return r.control.AuthenticateDeviceToken(req.Context(), deviceID, token)
}

func bearerToken(header string) string {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(header, prefix))
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{
		"error": message,
	})
}

func writeSSE(w http.ResponseWriter, event string, data any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if strings.TrimSpace(event) != "" {
		if _, err := fmt.Fprintf(w, "event: %s\n", event); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", raw)
	return err
}

func mapErrorStatus(err error) int {
	switch {
	case err == nil:
		return http.StatusOK
	case errors.Is(err, service.ErrUnauthorized):
		return http.StatusUnauthorized
	case errors.Is(err, service.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, service.ErrInvalidInput):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

type traceResponse struct {
	debugtrace.Trace
	InputAudioURL string `json:"input_audio_url,omitempty"`
	TTSAudioURL   string `json:"tts_audio_url,omitempty"`
}

func newTraceResponse(trace debugtrace.Trace) traceResponse {
	resp := traceResponse{Trace: trace}
	if trace.InputAudio != nil {
		resp.InputAudioURL = "/debug/assets/" + trace.ID + "/input.wav"
	}
	if trace.TTS != nil && trace.TTS.Audio != nil {
		resp.TTSAudioURL = "/debug/assets/" + trace.ID + "/tts.wav"
	}
	for index := range resp.Trace.Segments {
		if resp.Trace.Segments[index].Audio != nil {
			resp.Trace.Segments[index].AudioURL = "/debug/assets/" + trace.ID + "/" + resp.Trace.Segments[index].ID + ".wav"
		}
	}
	return resp
}

//go:embed debug_traces.html
var debugTracePage string
