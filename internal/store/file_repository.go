package store

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/zhuzhe/agensense/internal/device"
)

const stateSchemaVersion = 1

type FileRepository struct {
	path string
	mu   sync.RWMutex
	data fileState
}

type fileState struct {
	SchemaVersion    int                                        `json:"schema_version"`
	UpdatedAt        time.Time                                  `json:"updated_at"`
	Instances        map[string]device.Instance                 `json:"instances"`
	Devices          map[string]device.Device                   `json:"devices"`
	ProviderProfiles map[string]device.ProviderProfile          `json:"provider_profiles"`
	ConfigSnapshots  map[string]map[int64]device.ConfigSnapshot `json:"config_snapshots"`
	IssuedTokens     map[string]device.IssuedDeviceToken        `json:"issued_tokens"`
	tokenIndex       map[string]string
}

func NewFileRepository(path string) (*FileRepository, error) {
	if path == "" {
		return nil, device.ErrInvalidInput
	}

	repo := &FileRepository{
		path: path,
		data: newFileState(),
	}

	if err := repo.load(); err != nil {
		return nil, err
	}

	return repo, nil
}

func (repo *FileRepository) Path() string {
	return repo.path
}

func (repo *FileRepository) SeedDemoData(ctx context.Context) error {
	_, err := repo.GetDevice(ctx, device.DemoDeviceID)
	if err == nil {
		return nil
	}

	if !errors.Is(err, device.ErrNotFound) {
		return err
	}

	return device.ApplyDemoSeed(ctx, repo, time.Now().UTC())
}

func (repo *FileRepository) SaveInstance(_ context.Context, instance device.Instance) (device.Instance, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()

	instance = cloneInstance(instance)
	repo.data.Instances[instance.ID] = instance
	if err := repo.flushLocked(); err != nil {
		return device.Instance{}, err
	}

	return cloneInstance(instance), nil
}

func (repo *FileRepository) GetInstance(_ context.Context, id string) (device.Instance, error) {
	repo.mu.RLock()
	defer repo.mu.RUnlock()

	instance, ok := repo.data.Instances[id]
	if !ok {
		return device.Instance{}, device.ErrNotFound
	}

	return cloneInstance(instance), nil
}

func (repo *FileRepository) ListInstances(_ context.Context, tenantID string) ([]device.Instance, error) {
	repo.mu.RLock()
	defer repo.mu.RUnlock()

	instances := make([]device.Instance, 0, len(repo.data.Instances))
	for _, instance := range repo.data.Instances {
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

func (repo *FileRepository) SaveDevice(_ context.Context, dev device.Device) (device.Device, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()

	dev = cloneDevice(dev)
	repo.data.Devices[dev.ID] = dev
	if err := repo.flushLocked(); err != nil {
		return device.Device{}, err
	}

	return cloneDevice(dev), nil
}

func (repo *FileRepository) GetDevice(_ context.Context, id string) (device.Device, error) {
	repo.mu.RLock()
	defer repo.mu.RUnlock()

	dev, ok := repo.data.Devices[id]
	if !ok {
		return device.Device{}, device.ErrNotFound
	}

	return cloneDevice(dev), nil
}

func (repo *FileRepository) ListDevices(_ context.Context, filter device.DeviceFilter) ([]device.Device, error) {
	repo.mu.RLock()
	defer repo.mu.RUnlock()

	devices := make([]device.Device, 0, len(repo.data.Devices))
	for _, dev := range repo.data.Devices {
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

func (repo *FileRepository) SaveProviderProfile(_ context.Context, profile device.ProviderProfile) (device.ProviderProfile, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()

	profile = cloneProviderProfile(profile)
	repo.data.ProviderProfiles[providerProfileKey(profile.TenantID, profile.ID)] = profile
	if err := repo.flushLocked(); err != nil {
		return device.ProviderProfile{}, err
	}

	return cloneProviderProfile(profile), nil
}

func (repo *FileRepository) GetProviderProfile(_ context.Context, tenantID, profileID string) (device.ProviderProfile, error) {
	repo.mu.RLock()
	defer repo.mu.RUnlock()

	profile, ok := repo.data.ProviderProfiles[providerProfileKey(tenantID, profileID)]
	if !ok {
		return device.ProviderProfile{}, device.ErrNotFound
	}

	return cloneProviderProfile(profile), nil
}

func (repo *FileRepository) ListProviderProfiles(_ context.Context, tenantID string) ([]device.ProviderProfile, error) {
	repo.mu.RLock()
	defer repo.mu.RUnlock()

	profiles := make([]device.ProviderProfile, 0, len(repo.data.ProviderProfiles))
	for _, profile := range repo.data.ProviderProfiles {
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

func (repo *FileRepository) SaveConfigSnapshot(_ context.Context, snapshot device.ConfigSnapshot) (device.ConfigSnapshot, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()

	deviceSnapshots, ok := repo.data.ConfigSnapshots[snapshot.DeviceID]
	if !ok {
		deviceSnapshots = make(map[int64]device.ConfigSnapshot)
		repo.data.ConfigSnapshots[snapshot.DeviceID] = deviceSnapshots
	}

	snapshot = cloneConfigSnapshot(snapshot)
	deviceSnapshots[snapshot.Version] = snapshot
	if err := repo.flushLocked(); err != nil {
		return device.ConfigSnapshot{}, err
	}

	return cloneConfigSnapshot(snapshot), nil
}

func (repo *FileRepository) GetConfigSnapshot(_ context.Context, deviceID string, version int64) (device.ConfigSnapshot, error) {
	repo.mu.RLock()
	defer repo.mu.RUnlock()

	deviceSnapshots, ok := repo.data.ConfigSnapshots[deviceID]
	if !ok {
		return device.ConfigSnapshot{}, device.ErrNotFound
	}

	snapshot, ok := deviceSnapshots[version]
	if !ok {
		return device.ConfigSnapshot{}, device.ErrNotFound
	}

	return cloneConfigSnapshot(snapshot), nil
}

func (repo *FileRepository) GetLatestConfigSnapshot(_ context.Context, deviceID string) (device.ConfigSnapshot, error) {
	repo.mu.RLock()
	defer repo.mu.RUnlock()

	deviceSnapshots, ok := repo.data.ConfigSnapshots[deviceID]
	if !ok || len(deviceSnapshots) == 0 {
		return device.ConfigSnapshot{}, device.ErrNotFound
	}

	var (
		found         bool
		latestVersion int64
		latest        device.ConfigSnapshot
	)

	for version, snapshot := range deviceSnapshots {
		if !found || version > latestVersion {
			found = true
			latestVersion = version
			latest = snapshot
		}
	}

	return cloneConfigSnapshot(latest), nil
}

func (repo *FileRepository) ListConfigSnapshots(_ context.Context, deviceID string) ([]device.ConfigSnapshot, error) {
	repo.mu.RLock()
	defer repo.mu.RUnlock()

	deviceSnapshots, ok := repo.data.ConfigSnapshots[deviceID]
	if !ok {
		return nil, nil
	}

	snapshots := make([]device.ConfigSnapshot, 0, len(deviceSnapshots))
	for _, snapshot := range deviceSnapshots {
		snapshots = append(snapshots, cloneConfigSnapshot(snapshot))
	}

	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].Version < snapshots[j].Version
	})

	return snapshots, nil
}

func (repo *FileRepository) SetDesiredConfigVersion(_ context.Context, deviceID string, version int64) (device.Device, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()

	dev, ok := repo.data.Devices[deviceID]
	if !ok {
		return device.Device{}, device.ErrNotFound
	}

	deviceSnapshots, ok := repo.data.ConfigSnapshots[deviceID]
	if !ok {
		return device.Device{}, device.ErrConfigMissing
	}

	if _, ok := deviceSnapshots[version]; !ok {
		return device.Device{}, device.ErrConfigMissing
	}

	dev.DesiredConfigVersion = version
	repo.data.Devices[deviceID] = dev
	if err := repo.flushLocked(); err != nil {
		return device.Device{}, err
	}

	return cloneDevice(dev), nil
}

func (repo *FileRepository) AckConfigVersion(_ context.Context, deviceID string, version int64, ackedAt time.Time) (device.Device, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()

	dev, ok := repo.data.Devices[deviceID]
	if !ok {
		return device.Device{}, device.ErrNotFound
	}

	deviceSnapshots, ok := repo.data.ConfigSnapshots[deviceID]
	if !ok {
		return device.Device{}, device.ErrConfigMissing
	}

	if _, ok := deviceSnapshots[version]; !ok {
		return device.Device{}, device.ErrConfigMissing
	}

	dev.ReportedConfigVersion = version
	dev.UpdatedAt = ackedAt
	dev.LastSeenAt = &ackedAt
	repo.data.Devices[deviceID] = dev
	if err := repo.flushLocked(); err != nil {
		return device.Device{}, err
	}

	return cloneDevice(dev), nil
}

func (repo *FileRepository) SaveIssuedDeviceToken(_ context.Context, token device.IssuedDeviceToken) (device.IssuedDeviceToken, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()

	token = cloneIssuedDeviceToken(token)
	if existingID, ok := repo.data.tokenIndex[token.TokenHash]; ok && existingID != token.ID {
		return device.IssuedDeviceToken{}, device.ErrConflict
	}

	repo.data.IssuedTokens[token.ID] = token
	repo.data.tokenIndex[token.TokenHash] = token.ID
	if err := repo.flushLocked(); err != nil {
		return device.IssuedDeviceToken{}, err
	}

	return cloneIssuedDeviceToken(token), nil
}

func (repo *FileRepository) GetIssuedDeviceToken(_ context.Context, tokenID string) (device.IssuedDeviceToken, error) {
	repo.mu.RLock()
	defer repo.mu.RUnlock()

	token, ok := repo.data.IssuedTokens[tokenID]
	if !ok {
		return device.IssuedDeviceToken{}, device.ErrNotFound
	}

	return cloneIssuedDeviceToken(token), nil
}

func (repo *FileRepository) FindIssuedDeviceTokenByHash(_ context.Context, tokenHash string) (device.IssuedDeviceToken, error) {
	repo.mu.RLock()
	defer repo.mu.RUnlock()

	tokenID, ok := repo.data.tokenIndex[tokenHash]
	if !ok {
		return device.IssuedDeviceToken{}, device.ErrNotFound
	}

	token, ok := repo.data.IssuedTokens[tokenID]
	if !ok {
		return device.IssuedDeviceToken{}, device.ErrNotFound
	}

	return cloneIssuedDeviceToken(token), nil
}

func (repo *FileRepository) TouchIssuedDeviceToken(_ context.Context, tokenID string, usedAt time.Time) (device.IssuedDeviceToken, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()

	token, ok := repo.data.IssuedTokens[tokenID]
	if !ok {
		return device.IssuedDeviceToken{}, device.ErrNotFound
	}

	token.LastUsedAt = cloneTimePtr(&usedAt)
	repo.data.IssuedTokens[tokenID] = token
	if err := repo.flushLocked(); err != nil {
		return device.IssuedDeviceToken{}, err
	}

	return cloneIssuedDeviceToken(token), nil
}

func (repo *FileRepository) RevokeIssuedDeviceToken(_ context.Context, tokenID string, revokedAt time.Time) (device.IssuedDeviceToken, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()

	token, ok := repo.data.IssuedTokens[tokenID]
	if !ok {
		return device.IssuedDeviceToken{}, device.ErrNotFound
	}

	token.RevokedAt = cloneTimePtr(&revokedAt)
	repo.data.IssuedTokens[tokenID] = token
	if err := repo.flushLocked(); err != nil {
		return device.IssuedDeviceToken{}, err
	}

	return cloneIssuedDeviceToken(token), nil
}

func (repo *FileRepository) load() error {
	if err := os.MkdirAll(filepath.Dir(repo.path), 0o755); err != nil {
		return err
	}

	payload, err := os.ReadFile(repo.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}

		return err
	}

	if len(payload) == 0 {
		return nil
	}

	var persisted fileState
	if err := json.Unmarshal(payload, &persisted); err != nil {
		return err
	}

	repo.data = normalizeState(persisted)
	return nil
}

func (repo *FileRepository) flushLocked() error {
	repo.data.SchemaVersion = stateSchemaVersion
	repo.data.UpdatedAt = time.Now().UTC()

	payload, err := json.MarshalIndent(persistedCopy(repo.data), "", "  ")
	if err != nil {
		return err
	}

	tmpPath := repo.path + ".tmp"
	if err := os.WriteFile(tmpPath, payload, 0o644); err != nil {
		return err
	}

	return os.Rename(tmpPath, repo.path)
}

func newFileState() fileState {
	return fileState{
		SchemaVersion:    stateSchemaVersion,
		Instances:        make(map[string]device.Instance),
		Devices:          make(map[string]device.Device),
		ProviderProfiles: make(map[string]device.ProviderProfile),
		ConfigSnapshots:  make(map[string]map[int64]device.ConfigSnapshot),
		IssuedTokens:     make(map[string]device.IssuedDeviceToken),
		tokenIndex:       make(map[string]string),
	}
}

func normalizeState(in fileState) fileState {
	out := newFileState()
	out.SchemaVersion = in.SchemaVersion
	out.UpdatedAt = in.UpdatedAt

	for key, instance := range in.Instances {
		out.Instances[key] = cloneInstance(instance)
	}

	for key, dev := range in.Devices {
		out.Devices[key] = cloneDevice(dev)
	}

	for key, profile := range in.ProviderProfiles {
		out.ProviderProfiles[key] = cloneProviderProfile(profile)
	}

	for deviceID, snapshots := range in.ConfigSnapshots {
		if out.ConfigSnapshots[deviceID] == nil {
			out.ConfigSnapshots[deviceID] = make(map[int64]device.ConfigSnapshot)
		}

		for version, snapshot := range snapshots {
			out.ConfigSnapshots[deviceID][version] = cloneConfigSnapshot(snapshot)
		}
	}

	for tokenID, token := range in.IssuedTokens {
		token = cloneIssuedDeviceToken(token)
		out.IssuedTokens[tokenID] = token
		if token.TokenHash != "" {
			out.tokenIndex[token.TokenHash] = tokenID
		}
	}

	return out
}

func persistedCopy(in fileState) fileState {
	out := newFileState()
	out.SchemaVersion = in.SchemaVersion
	out.UpdatedAt = in.UpdatedAt

	for key, instance := range in.Instances {
		out.Instances[key] = cloneInstance(instance)
	}

	for key, dev := range in.Devices {
		out.Devices[key] = cloneDevice(dev)
	}

	for key, profile := range in.ProviderProfiles {
		out.ProviderProfiles[key] = cloneProviderProfile(profile)
	}

	for deviceID, snapshots := range in.ConfigSnapshots {
		if out.ConfigSnapshots[deviceID] == nil {
			out.ConfigSnapshots[deviceID] = make(map[int64]device.ConfigSnapshot)
		}

		for version, snapshot := range snapshots {
			out.ConfigSnapshots[deviceID][version] = cloneConfigSnapshot(snapshot)
		}
	}

	for tokenID, token := range in.IssuedTokens {
		out.IssuedTokens[tokenID] = cloneIssuedDeviceToken(token)
	}

	return out
}

func providerProfileKey(tenantID, profileID string) string {
	return tenantID + "\x00" + profileID
}

func cloneRawMessage(in json.RawMessage) json.RawMessage {
	if len(in) == 0 {
		return nil
	}

	out := make([]byte, len(in))
	copy(out, in)
	return out
}

func cloneTimePtr(in *time.Time) *time.Time {
	if in == nil {
		return nil
	}

	out := *in
	return &out
}

func cloneInstance(in device.Instance) device.Instance {
	in.ConfigTemplate = cloneRawMessage(in.ConfigTemplate)
	return in
}

func cloneDevice(in device.Device) device.Device {
	in.Capabilities = cloneRawMessage(in.Capabilities)
	in.LastBootstrapAt = cloneTimePtr(in.LastBootstrapAt)
	in.LastSeenAt = cloneTimePtr(in.LastSeenAt)
	return in
}

func cloneProviderProfile(in device.ProviderProfile) device.ProviderProfile {
	return in
}

func cloneConfigSnapshot(in device.ConfigSnapshot) device.ConfigSnapshot {
	in.Config = cloneRawMessage(in.Config)
	return in
}

func cloneIssuedDeviceToken(in device.IssuedDeviceToken) device.IssuedDeviceToken {
	in.LastUsedAt = cloneTimePtr(in.LastUsedAt)
	in.RevokedAt = cloneTimePtr(in.RevokedAt)
	return in
}
