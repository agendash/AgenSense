package device

import (
	"context"
	"sort"
	"sync"
	"time"
)

type MemoryRepository struct {
	mu               sync.RWMutex
	instances        map[string]Instance
	devices          map[string]Device
	providerProfiles map[string]ProviderProfile
	configSnapshots  map[string]map[int64]ConfigSnapshot
	issuedTokens     map[string]IssuedDeviceToken
	tokenIndex       map[string]string
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		instances:        make(map[string]Instance),
		devices:          make(map[string]Device),
		providerProfiles: make(map[string]ProviderProfile),
		configSnapshots:  make(map[string]map[int64]ConfigSnapshot),
		issuedTokens:     make(map[string]IssuedDeviceToken),
		tokenIndex:       make(map[string]string),
	}
}

func (repo *MemoryRepository) SaveInstance(_ context.Context, instance Instance) (Instance, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()

	instance = cloneInstance(instance)
	repo.instances[instance.ID] = instance
	return cloneInstance(instance), nil
}

func (repo *MemoryRepository) GetInstance(_ context.Context, id string) (Instance, error) {
	repo.mu.RLock()
	defer repo.mu.RUnlock()

	instance, ok := repo.instances[id]
	if !ok {
		return Instance{}, ErrNotFound
	}

	return cloneInstance(instance), nil
}

func (repo *MemoryRepository) ListInstances(_ context.Context, tenantID string) ([]Instance, error) {
	repo.mu.RLock()
	defer repo.mu.RUnlock()

	instances := make([]Instance, 0, len(repo.instances))
	for _, instance := range repo.instances {
		if tenantID != "" && instance.TenantID != tenantID {
			continue
		}

		instances = append(instances, cloneInstance(instance))
	}

	sort.Slice(instances, func(i, j int) bool {
		return instances[i].ID < instances[j].ID
	})

	return instances, nil
}

func (repo *MemoryRepository) SaveDevice(_ context.Context, dev Device) (Device, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()

	dev = cloneDevice(dev)
	repo.devices[dev.ID] = dev
	return cloneDevice(dev), nil
}

func (repo *MemoryRepository) GetDevice(_ context.Context, id string) (Device, error) {
	repo.mu.RLock()
	defer repo.mu.RUnlock()

	dev, ok := repo.devices[id]
	if !ok {
		return Device{}, ErrNotFound
	}

	return cloneDevice(dev), nil
}

func (repo *MemoryRepository) ListDevices(_ context.Context, filter DeviceFilter) ([]Device, error) {
	repo.mu.RLock()
	defer repo.mu.RUnlock()

	devices := make([]Device, 0, len(repo.devices))
	for _, dev := range repo.devices {
		if filter.TenantID != "" && dev.TenantID != filter.TenantID {
			continue
		}

		if filter.InstanceID != "" && dev.InstanceID != filter.InstanceID {
			continue
		}

		devices = append(devices, cloneDevice(dev))
	}

	sort.Slice(devices, func(i, j int) bool {
		return devices[i].ID < devices[j].ID
	})

	return devices, nil
}

func (repo *MemoryRepository) SaveProviderProfile(_ context.Context, profile ProviderProfile) (ProviderProfile, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()

	profile = cloneProviderProfile(profile)
	repo.providerProfiles[providerProfileKey(profile.TenantID, profile.ID)] = profile
	return cloneProviderProfile(profile), nil
}

func (repo *MemoryRepository) GetProviderProfile(_ context.Context, tenantID, profileID string) (ProviderProfile, error) {
	repo.mu.RLock()
	defer repo.mu.RUnlock()

	profile, ok := repo.providerProfiles[providerProfileKey(tenantID, profileID)]
	if !ok {
		return ProviderProfile{}, ErrNotFound
	}

	return cloneProviderProfile(profile), nil
}

func (repo *MemoryRepository) ListProviderProfiles(_ context.Context, tenantID string) ([]ProviderProfile, error) {
	repo.mu.RLock()
	defer repo.mu.RUnlock()

	profiles := make([]ProviderProfile, 0, len(repo.providerProfiles))
	for _, profile := range repo.providerProfiles {
		if tenantID != "" && profile.TenantID != tenantID {
			continue
		}

		profiles = append(profiles, cloneProviderProfile(profile))
	}

	sort.Slice(profiles, func(i, j int) bool {
		return profiles[i].ID < profiles[j].ID
	})

	return profiles, nil
}

func (repo *MemoryRepository) SaveConfigSnapshot(_ context.Context, snapshot ConfigSnapshot) (ConfigSnapshot, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()

	deviceSnapshots, ok := repo.configSnapshots[snapshot.DeviceID]
	if !ok {
		deviceSnapshots = make(map[int64]ConfigSnapshot)
		repo.configSnapshots[snapshot.DeviceID] = deviceSnapshots
	}

	snapshot = cloneConfigSnapshot(snapshot)
	deviceSnapshots[snapshot.Version] = snapshot
	return cloneConfigSnapshot(snapshot), nil
}

func (repo *MemoryRepository) GetConfigSnapshot(_ context.Context, deviceID string, version int64) (ConfigSnapshot, error) {
	repo.mu.RLock()
	defer repo.mu.RUnlock()

	deviceSnapshots, ok := repo.configSnapshots[deviceID]
	if !ok {
		return ConfigSnapshot{}, ErrNotFound
	}

	snapshot, ok := deviceSnapshots[version]
	if !ok {
		return ConfigSnapshot{}, ErrNotFound
	}

	return cloneConfigSnapshot(snapshot), nil
}

func (repo *MemoryRepository) GetLatestConfigSnapshot(_ context.Context, deviceID string) (ConfigSnapshot, error) {
	repo.mu.RLock()
	defer repo.mu.RUnlock()

	deviceSnapshots, ok := repo.configSnapshots[deviceID]
	if !ok || len(deviceSnapshots) == 0 {
		return ConfigSnapshot{}, ErrNotFound
	}

	var (
		latestVersion int64
		latest        ConfigSnapshot
		found         bool
	)

	for version, snapshot := range deviceSnapshots {
		if !found || version > latestVersion {
			latestVersion = version
			latest = snapshot
			found = true
		}
	}

	return cloneConfigSnapshot(latest), nil
}

func (repo *MemoryRepository) ListConfigSnapshots(_ context.Context, deviceID string) ([]ConfigSnapshot, error) {
	repo.mu.RLock()
	defer repo.mu.RUnlock()

	deviceSnapshots, ok := repo.configSnapshots[deviceID]
	if !ok {
		return nil, nil
	}

	snapshots := make([]ConfigSnapshot, 0, len(deviceSnapshots))
	for _, snapshot := range deviceSnapshots {
		snapshots = append(snapshots, cloneConfigSnapshot(snapshot))
	}

	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].Version < snapshots[j].Version
	})

	return snapshots, nil
}

func (repo *MemoryRepository) SetDesiredConfigVersion(_ context.Context, deviceID string, version int64) (Device, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()

	dev, ok := repo.devices[deviceID]
	if !ok {
		return Device{}, ErrNotFound
	}

	if _, ok := repo.configSnapshots[deviceID][version]; !ok {
		return Device{}, ErrConfigMissing
	}

	dev.DesiredConfigVersion = version
	repo.devices[deviceID] = dev
	return cloneDevice(dev), nil
}

func (repo *MemoryRepository) AckConfigVersion(_ context.Context, deviceID string, version int64, ackedAt time.Time) (Device, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()

	dev, ok := repo.devices[deviceID]
	if !ok {
		return Device{}, ErrNotFound
	}

	if _, ok := repo.configSnapshots[deviceID][version]; !ok {
		return Device{}, ErrConfigMissing
	}

	dev.ReportedConfigVersion = version
	dev.UpdatedAt = ackedAt
	dev.LastSeenAt = &ackedAt
	repo.devices[deviceID] = dev
	return cloneDevice(dev), nil
}

func (repo *MemoryRepository) SaveIssuedDeviceToken(_ context.Context, token IssuedDeviceToken) (IssuedDeviceToken, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()

	token = cloneIssuedDeviceToken(token)
	if existingID, ok := repo.tokenIndex[token.TokenHash]; ok && existingID != token.ID {
		return IssuedDeviceToken{}, ErrConflict
	}

	repo.issuedTokens[token.ID] = token
	repo.tokenIndex[token.TokenHash] = token.ID
	return cloneIssuedDeviceToken(token), nil
}

func (repo *MemoryRepository) GetIssuedDeviceToken(_ context.Context, tokenID string) (IssuedDeviceToken, error) {
	repo.mu.RLock()
	defer repo.mu.RUnlock()

	token, ok := repo.issuedTokens[tokenID]
	if !ok {
		return IssuedDeviceToken{}, ErrNotFound
	}

	return cloneIssuedDeviceToken(token), nil
}

func (repo *MemoryRepository) FindIssuedDeviceTokenByHash(_ context.Context, tokenHash string) (IssuedDeviceToken, error) {
	repo.mu.RLock()
	defer repo.mu.RUnlock()

	tokenID, ok := repo.tokenIndex[tokenHash]
	if !ok {
		return IssuedDeviceToken{}, ErrNotFound
	}

	token, ok := repo.issuedTokens[tokenID]
	if !ok {
		return IssuedDeviceToken{}, ErrNotFound
	}

	return cloneIssuedDeviceToken(token), nil
}

func (repo *MemoryRepository) TouchIssuedDeviceToken(_ context.Context, tokenID string, usedAt time.Time) (IssuedDeviceToken, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()

	token, ok := repo.issuedTokens[tokenID]
	if !ok {
		return IssuedDeviceToken{}, ErrNotFound
	}

	token.LastUsedAt = &usedAt
	repo.issuedTokens[tokenID] = token
	return cloneIssuedDeviceToken(token), nil
}

func (repo *MemoryRepository) RevokeIssuedDeviceToken(_ context.Context, tokenID string, revokedAt time.Time) (IssuedDeviceToken, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()

	token, ok := repo.issuedTokens[tokenID]
	if !ok {
		return IssuedDeviceToken{}, ErrNotFound
	}

	token.RevokedAt = &revokedAt
	repo.issuedTokens[tokenID] = token
	return cloneIssuedDeviceToken(token), nil
}

func providerProfileKey(tenantID, profileID string) string {
	return tenantID + "\x00" + profileID
}
