package voicews

import (
	"net/http"
	"strings"
	"time"

	"github.com/agendash/AgenSense/internal/debugtrace"
	"github.com/agendash/AgenSense/internal/gateway/wsconn"
	"github.com/agendash/AgenSense/internal/provider"
	"github.com/agendash/AgenSense/internal/service"
)

// Handler serves the direct-use full-duplex voice websocket session.
type Handler struct {
	registry *service.RegistryService
	factory  *provider.Factory
	debug    *debugtrace.Store
	now      func() time.Time
}

func NewHandler(registry *service.RegistryService, factory *provider.Factory, debug *debugtrace.Store) *Handler {
	return &Handler{
		registry: registry,
		factory:  factory,
		debug:    debug,
		now:      time.Now,
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.registry == nil || h.factory == nil {
		http.Error(w, "voice websocket unavailable", http.StatusServiceUnavailable)
		return
	}

	apiKey := bearerToken(r.Header.Get("Authorization"))
	if apiKey == "" {
		http.Error(w, "missing api key", http.StatusUnauthorized)
		return
	}

	conn, err := wsconn.Upgrade(w, r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer conn.Close()

	session := newSession(sessionDeps{
		conn:     conn,
		apiKey:   apiKey,
		registry: h.registry,
		factory:  h.factory,
		debug:    h.debug,
		now:      h.now,
	})
	session.run(r.Context())
}

func bearerToken(header string) string {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(header, prefix))
}
