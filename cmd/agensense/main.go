package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"github.com/agendash/AgenSense/internal/app"
	"github.com/agendash/AgenSense/internal/debugtrace"
	"github.com/agendash/AgenSense/internal/device"
	"github.com/agendash/AgenSense/internal/gateway"
	"github.com/agendash/AgenSense/internal/httpapi"
	"github.com/agendash/AgenSense/internal/observability"
	"github.com/agendash/AgenSense/internal/provider"
	"github.com/agendash/AgenSense/internal/service"
	"github.com/agendash/AgenSense/internal/session"
	"github.com/agendash/AgenSense/internal/store"
	"github.com/agendash/AgenSense/internal/voicews"
)

func main() {
	cfg, err := app.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}

	logger, err := observability.NewLogger(cfg.LogLevel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "init logger: %v\n", err)
		os.Exit(1)
	}
	slog.SetDefault(logger)

	repo, err := store.NewFileRepository(filepath.Join(cfg.DataDir, "state.json"))
	if err != nil {
		slog.Error("failed to initialize file repository", "error", err)
		os.Exit(1)
	}
	if !cfg.DisableDemoSeed {
		if err := repo.SeedDemoData(context.Background()); err != nil {
			slog.Error("failed to seed demo data", "error", err)
			os.Exit(1)
		}
	}
	if err := app.EnsureDefaultProviderProfile(context.Background(), repo, cfg); err != nil {
		slog.Error("failed to seed default provider profile", "error", err)
		os.Exit(1)
	}
	deviceService := device.NewService(repo)
	control := service.NewDeviceControl(deviceService, repo, cfg.RetryHintSec)
	control.SetPublicBaseURL(cfg.PublicBaseURL)
	registry := service.NewRegistryService(repo)
	factory := provider.NewFactory(nil)
	var debugStore *debugtrace.Store
	if cfg.DebugEnabled {
		debugStore = debugtrace.NewStoreWithAssetDir(64, filepath.Join(cfg.DataDir, "debug-traces"))
	}
	inference := service.NewRuntimeInferenceService(registry, factory, debugStore)

	pipeline := &session.Pipeline{
		ASR:   provider.MockASR{},
		LLM:   provider.MockLLM{},
		TTS:   provider.MockTTS{},
		Debug: debugStore,
	}
	wsHandler := gateway.NewHandler(control, pipeline)
	voiceHandler := voicews.NewHandler(registry, factory, debugStore)
	httpHandler := httpapi.NewRouter(control, registry, inference, wsHandler, voiceHandler, debugStore)

	slog.Info("agensense starting",
		"addr", cfg.Addr,
		"public_base_url", cfg.PublicBaseURL,
		"state_dir", cfg.DataDir,
		"log_level", cfg.LogLevel,
		"debug_enabled", cfg.DebugEnabled,
		"demo_seed_enabled", !cfg.DisableDemoSeed,
		"default_api_key", cfg.DefaultAPIKey,
		"default_provider_id", cfg.DefaultProviderID,
	)

	if err := http.ListenAndServe(cfg.Addr, httpHandler); err != nil {
		slog.Error("agensense stopped", "error", err)
		os.Exit(1)
	}
}
