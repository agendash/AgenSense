package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/agendash/agensense/internal/device"
	"github.com/agendash/agensense/internal/service"
)

func TestEnsureDefaultProviderProfilePromotesMockDefault(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo := device.NewMemoryRepository()
	cfg := LoadTestConfig()
	namespace := service.NamespaceFromAPIKey(cfg.DefaultAPIKey)
	now := time.Date(2026, 4, 20, 5, 0, 0, 0, time.UTC)

	if _, err := repo.SaveProviderProfile(ctx, device.ProviderProfile{
		ID:         "mock-default",
		TenantID:   namespace,
		Name:       "Mock Default",
		ASRBaseURL: "mock://asr",
		LLMBaseURL: "mock://llm",
		TTSBaseURL: "mock://tts",
		CreatedAt:  now,
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("save mock provider: %v", err)
	}

	mockDefaultConfig, err := json.Marshal(map[string]any{
		"default_provider_profile_id": "mock-default",
	})
	if err != nil {
		t.Fatalf("marshal mock config: %v", err)
	}
	if _, err := repo.SaveConfigSnapshot(ctx, device.ConfigSnapshot{
		DeviceID:          namespace,
		TenantID:          namespace,
		InstanceID:        namespace,
		Version:           2,
		ProviderProfileID: "mock-default",
		Source:            "test",
		Config:            mockDefaultConfig,
		CreatedAt:         now,
	}); err != nil {
		t.Fatalf("save mock snapshot: %v", err)
	}

	if err := EnsureDefaultProviderProfile(ctx, repo, cfg); err != nil {
		t.Fatalf("EnsureDefaultProviderProfile() error = %v", err)
	}

	profile, err := repo.GetProviderProfile(ctx, namespace, cfg.DefaultProviderID)
	if err != nil {
		t.Fatalf("get built-in provider: %v", err)
	}
	if profile.ASRBaseURL != cfg.DefaultProviderBaseURL {
		t.Fatalf("ASRBaseURL = %q, want %q", profile.ASRBaseURL, cfg.DefaultProviderBaseURL)
	}

	latest, err := repo.GetLatestConfigSnapshot(ctx, namespace)
	if err != nil {
		t.Fatalf("get latest snapshot: %v", err)
	}
	if latest.Version != 3 {
		t.Fatalf("latest version = %d, want 3", latest.Version)
	}
	if defaultProviderProfileID(latest.Config) != cfg.DefaultProviderID {
		t.Fatalf("default provider = %q, want %q", defaultProviderProfileID(latest.Config), cfg.DefaultProviderID)
	}
}

func TestEnsureDefaultProviderProfileKeepsCustomDefault(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo := device.NewMemoryRepository()
	cfg := LoadTestConfig()
	namespace := service.NamespaceFromAPIKey(cfg.DefaultAPIKey)
	now := time.Date(2026, 4, 20, 5, 10, 0, 0, time.UTC)

	customDefaultConfig, err := json.Marshal(map[string]any{
		"default_provider_profile_id": "team-localai",
	})
	if err != nil {
		t.Fatalf("marshal custom config: %v", err)
	}
	if _, err := repo.SaveConfigSnapshot(ctx, device.ConfigSnapshot{
		DeviceID:          namespace,
		TenantID:          namespace,
		InstanceID:        namespace,
		Version:           5,
		ProviderProfileID: "team-localai",
		Source:            "test",
		Config:            customDefaultConfig,
		CreatedAt:         now,
	}); err != nil {
		t.Fatalf("save custom snapshot: %v", err)
	}

	if err := EnsureDefaultProviderProfile(ctx, repo, cfg); err != nil {
		t.Fatalf("EnsureDefaultProviderProfile() error = %v", err)
	}

	latest, err := repo.GetLatestConfigSnapshot(ctx, namespace)
	if err != nil {
		t.Fatalf("get latest snapshot: %v", err)
	}
	if latest.Version != 5 {
		t.Fatalf("latest version = %d, want 5", latest.Version)
	}
	if defaultProviderProfileID(latest.Config) != "team-localai" {
		t.Fatalf("default provider = %q, want team-localai", defaultProviderProfileID(latest.Config))
	}

	if _, err := repo.GetProviderProfile(ctx, namespace, cfg.DefaultProviderID); err != nil {
		t.Fatalf("get built-in provider: %v", err)
	}
}

func LoadTestConfig() Config {
	return Config{
		DefaultAPIKey:          "demo-user-key",
		DefaultProviderID:      "default",
		DefaultProviderName:    "Mock Default",
		DefaultProviderBaseURL: "mock://default",
		DefaultProviderAPIKey:  "",
		DefaultASRModel:        "mock-asr",
		DefaultLLMModel:        "mock-llm",
		DefaultTTSModel:        "mock-tts",
	}
}
