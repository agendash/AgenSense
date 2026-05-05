package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/agendash/agensense/internal/device"
	"github.com/agendash/agensense/internal/service"
)

// EnsureDefaultProviderProfile makes the shared-service default namespace usable
// without a manual registration step. It only promotes the built-in profile when
// the namespace has no default yet or is still pinned to the old mock default.
func EnsureDefaultProviderProfile(ctx context.Context, repo device.Repository, cfg Config) error {
	if repo == nil {
		return nil
	}

	apiKey := strings.TrimSpace(cfg.DefaultAPIKey)
	profileID := strings.TrimSpace(cfg.DefaultProviderID)
	if apiKey == "" || profileID == "" {
		return nil
	}

	namespace := service.NamespaceFromAPIKey(apiKey)
	if namespace == "" {
		return nil
	}

	now := time.Now().UTC()
	profile := device.ProviderProfile{
		ID:         profileID,
		TenantID:   namespace,
		Name:       strings.TrimSpace(cfg.DefaultProviderName),
		ASRBaseURL: strings.TrimSpace(cfg.DefaultProviderBaseURL),
		ASRAPIKey:  strings.TrimSpace(cfg.DefaultProviderAPIKey),
		ASRModel:   strings.TrimSpace(cfg.DefaultASRModel),
		LLMBaseURL: strings.TrimSpace(cfg.DefaultProviderBaseURL),
		LLMAPIKey:  strings.TrimSpace(cfg.DefaultProviderAPIKey),
		LLMModel:   strings.TrimSpace(cfg.DefaultLLMModel),
		TTSBaseURL: strings.TrimSpace(cfg.DefaultProviderBaseURL),
		TTSAPIKey:  strings.TrimSpace(cfg.DefaultProviderAPIKey),
		TTSModel:   strings.TrimSpace(cfg.DefaultTTSModel),
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	existing, err := repo.GetProviderProfile(ctx, namespace, profileID)
	switch {
	case err == nil:
		profile.CreatedAt = existing.CreatedAt
	case !errors.Is(err, device.ErrNotFound):
		return fmt.Errorf("ensure default provider: get provider profile: %w", err)
	}

	if _, err := repo.SaveProviderProfile(ctx, profile); err != nil {
		return fmt.Errorf("ensure default provider: save provider profile: %w", err)
	}

	promote, nextVersion, err := shouldPromoteDefaultProvider(ctx, repo, namespace, profileID)
	if err != nil {
		return fmt.Errorf("ensure default provider: inspect current default: %w", err)
	}
	if !promote {
		return nil
	}

	config := map[string]any{
		"default_provider_profile_id": profileID,
	}
	raw, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("ensure default provider: marshal config snapshot: %w", err)
	}

	if _, err := repo.SaveConfigSnapshot(ctx, device.ConfigSnapshot{
		DeviceID:          namespace,
		TenantID:          namespace,
		InstanceID:        namespace,
		Version:           nextVersion,
		ProviderProfileID: profileID,
		Source:            "builtin-default",
		Config:            raw,
		CreatedAt:         now,
	}); err != nil {
		return fmt.Errorf("ensure default provider: save config snapshot: %w", err)
	}

	return nil
}

func shouldPromoteDefaultProvider(ctx context.Context, repo device.Repository, namespace, targetProfileID string) (bool, int64, error) {
	snapshot, err := repo.GetLatestConfigSnapshot(ctx, namespace)
	if errors.Is(err, device.ErrNotFound) {
		return true, 1, nil
	}
	if err != nil {
		return false, 0, err
	}

	currentDefault := defaultProviderProfileID(snapshot.Config)
	nextVersion := snapshot.Version + 1
	if currentDefault == "" || currentDefault == "mock-default" {
		return true, nextVersion, nil
	}
	if currentDefault == targetProfileID {
		return false, nextVersion, nil
	}
	return false, nextVersion, nil
}

func defaultProviderProfileID(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}

	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		return ""
	}

	value, _ := config["default_provider_profile_id"].(string)
	return strings.TrimSpace(value)
}
