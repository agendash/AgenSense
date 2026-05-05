package service

import (
	"context"
	"strings"
	"time"

	"github.com/agendash/AgenSense/internal/device"
)

type timeNowFunc func() time.Time

// RegistryService stores provider profiles in API-key-scoped namespaces.
type RegistryService struct {
	repo device.Repository
	now  timeNowFunc
}

func NewRegistryService(repo device.Repository) *RegistryService {
	return &RegistryService{
		repo: repo,
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

func (s *RegistryService) UpsertProviderProfile(ctx context.Context, apiKey string, req ProviderProfileRequest) (ProviderProfileResponse, error) {
	if s.repo == nil {
		return ProviderProfileResponse{}, ErrInvalidInput
	}

	namespace := NamespaceFromAPIKey(apiKey)
	if namespace == "" {
		return ProviderProfileResponse{}, ErrUnauthorized
	}
	if strings.TrimSpace(req.ID) == "" {
		return ProviderProfileResponse{}, ErrInvalidInput
	}

	now := s.now()
	profile := device.ProviderProfile{
		ID:         strings.TrimSpace(req.ID),
		TenantID:   namespace,
		Name:       strings.TrimSpace(req.Name),
		ASRBaseURL: strings.TrimSpace(req.ASRBaseURL),
		ASRAPIKey:  strings.TrimSpace(req.ASRAPIKey),
		ASRModel:   strings.TrimSpace(req.ASRModel),
		LLMBaseURL: strings.TrimSpace(req.LLMBaseURL),
		LLMAPIKey:  strings.TrimSpace(req.LLMAPIKey),
		LLMModel:   strings.TrimSpace(req.LLMModel),
		TTSBaseURL: strings.TrimSpace(req.TTSBaseURL),
		TTSAPIKey:  strings.TrimSpace(req.TTSAPIKey),
		TTSModel:   strings.TrimSpace(req.TTSModel),
		VADBaseURL: strings.TrimSpace(req.VADBaseURL),
		VADAPIKey:  strings.TrimSpace(req.VADAPIKey),
		VADModel:   strings.TrimSpace(req.VADModel),
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if existing, err := s.repo.GetProviderProfile(ctx, namespace, profile.ID); err == nil {
		profile.CreatedAt = existing.CreatedAt
	}

	saved, err := s.repo.SaveProviderProfile(ctx, profile)
	if err != nil {
		return ProviderProfileResponse{}, err
	}

	if req.Default {
		if err := s.setDefaultProfileConfig(ctx, namespace, saved.ID); err != nil {
			return ProviderProfileResponse{}, err
		}
	}

	currentDefault, _ := s.getDefaultProfileID(ctx, namespace)
	return providerProfileResponse(saved, currentDefault == saved.ID), nil
}

func (s *RegistryService) ListProviderProfiles(ctx context.Context, apiKey string) ([]ProviderProfileResponse, error) {
	if s.repo == nil {
		return nil, ErrInvalidInput
	}

	namespace := NamespaceFromAPIKey(apiKey)
	if namespace == "" {
		return nil, ErrUnauthorized
	}

	profiles, err := s.repo.ListProviderProfiles(ctx, namespace)
	if err != nil {
		return nil, err
	}
	defaultID, _ := s.getDefaultProfileID(ctx, namespace)
	out := make([]ProviderProfileResponse, 0, len(profiles))
	for _, profile := range profiles {
		out = append(out, providerProfileResponse(profile, profile.ID == defaultID))
	}
	return out, nil
}

func (s *RegistryService) GetProviderProfile(ctx context.Context, apiKey, profileID string) (ProviderProfileResponse, error) {
	if s.repo == nil {
		return ProviderProfileResponse{}, ErrInvalidInput
	}

	namespace := NamespaceFromAPIKey(apiKey)
	if namespace == "" {
		return ProviderProfileResponse{}, ErrUnauthorized
	}
	if strings.TrimSpace(profileID) == "" {
		return ProviderProfileResponse{}, ErrInvalidInput
	}

	profile, err := s.repo.GetProviderProfile(ctx, namespace, strings.TrimSpace(profileID))
	if err != nil {
		return ProviderProfileResponse{}, err
	}
	defaultID, _ := s.getDefaultProfileID(ctx, namespace)
	return providerProfileResponse(profile, profile.ID == defaultID), nil
}

func (s *RegistryService) ResolveProviderProfile(ctx context.Context, apiKey, profileID string) (device.ProviderProfile, error) {
	if s.repo == nil {
		return device.ProviderProfile{}, ErrInvalidInput
	}

	namespace := NamespaceFromAPIKey(apiKey)
	if namespace == "" {
		return device.ProviderProfile{}, ErrUnauthorized
	}

	if strings.TrimSpace(profileID) != "" {
		profile, err := s.repo.GetProviderProfile(ctx, namespace, strings.TrimSpace(profileID))
		if err != nil {
			return device.ProviderProfile{}, mapDeviceError(err)
		}
		return profile, nil
	}

	defaultID, err := s.getDefaultProfileID(ctx, namespace)
	if err == nil && defaultID != "" {
		profile, getErr := s.repo.GetProviderProfile(ctx, namespace, defaultID)
		if getErr != nil {
			return device.ProviderProfile{}, mapDeviceError(getErr)
		}
		return profile, nil
	}

	profiles, err := s.repo.ListProviderProfiles(ctx, namespace)
	if err != nil {
		return device.ProviderProfile{}, mapDeviceError(err)
	}
	if len(profiles) == 1 {
		return profiles[0], nil
	}
	if len(profiles) == 0 {
		return device.ProviderProfile{}, ErrNotFound
	}
	return device.ProviderProfile{}, ErrInvalidInput
}

func (s *RegistryService) setDefaultProfileConfig(ctx context.Context, namespace, profileID string) error {
	snapshot, err := s.repo.GetLatestConfigSnapshot(ctx, namespace)
	if err != nil && err != device.ErrNotFound {
		return err
	}
	version := int64(1)
	if err == nil && snapshot.Version >= version {
		version = snapshot.Version + 1
	}

	config := map[string]any{
		"default_provider_profile_id": profileID,
	}

	raw, marshalErr := jsonMarshal(config)
	if marshalErr != nil {
		return marshalErr
	}

	_, err = s.repo.SaveConfigSnapshot(ctx, device.ConfigSnapshot{
		DeviceID:          namespace,
		TenantID:          namespace,
		InstanceID:        namespace,
		Version:           version,
		ProviderProfileID: profileID,
		Source:            "provider-registry",
		Config:            raw,
		CreatedAt:         s.now(),
	})
	return err
}

func (s *RegistryService) getDefaultProfileID(ctx context.Context, namespace string) (string, error) {
	snapshot, err := s.repo.GetLatestConfigSnapshot(ctx, namespace)
	if err != nil {
		return "", err
	}

	var config map[string]any
	if err := jsonUnmarshal(snapshot.Config, &config); err != nil {
		return "", err
	}
	if value, ok := config["default_provider_profile_id"].(string); ok {
		return value, nil
	}
	return "", ErrNotFound
}

func providerProfileResponse(profile device.ProviderProfile, isDefault bool) ProviderProfileResponse {
	return ProviderProfileResponse{
		ID:         profile.ID,
		Name:       profile.Name,
		Namespace:  profile.TenantID,
		ASRBaseURL: profile.ASRBaseURL,
		ASRModel:   profile.ASRModel,
		LLMBaseURL: profile.LLMBaseURL,
		LLMModel:   profile.LLMModel,
		TTSBaseURL: profile.TTSBaseURL,
		TTSModel:   profile.TTSModel,
		VADBaseURL: profile.VADBaseURL,
		VADModel:   profile.VADModel,
		Default:    isDefault,
	}
}
