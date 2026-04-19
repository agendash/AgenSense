package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/zhuzhe/agensense/internal/service"
)

// Router exposes the control-plane HTTP endpoints for the MVP.
type Router struct {
	control   service.ControlPlane
	sessionWS http.Handler
}

// NewRouter builds an HTTP router for bootstrap/config/telemetry/session traffic.
func NewRouter(control service.ControlPlane, sessionWS http.Handler) http.Handler {
	r := &Router{
		control:   control,
		sessionWS: sessionWS,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", r.handleHealthz)
	mux.HandleFunc("/v1/bootstrap", r.handleBootstrap)
	mux.HandleFunc("/v1/device/config", r.handleDeviceConfig)
	mux.HandleFunc("/v1/device/telemetry", r.handleDeviceTelemetry)
	if sessionWS != nil {
		mux.Handle("/v1/session/ws", sessionWS)
	}
	return loggingMiddleware(mux)
}

func (r *Router) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true,
	})
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
