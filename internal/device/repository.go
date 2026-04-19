package device

import (
	"context"
	"time"
)

type Repository interface {
	SaveInstance(ctx context.Context, instance Instance) (Instance, error)
	GetInstance(ctx context.Context, id string) (Instance, error)
	ListInstances(ctx context.Context, tenantID string) ([]Instance, error)

	SaveDevice(ctx context.Context, device Device) (Device, error)
	GetDevice(ctx context.Context, id string) (Device, error)
	ListDevices(ctx context.Context, filter DeviceFilter) ([]Device, error)

	SaveProviderProfile(ctx context.Context, profile ProviderProfile) (ProviderProfile, error)
	GetProviderProfile(ctx context.Context, tenantID, profileID string) (ProviderProfile, error)
	ListProviderProfiles(ctx context.Context, tenantID string) ([]ProviderProfile, error)

	SaveConfigSnapshot(ctx context.Context, snapshot ConfigSnapshot) (ConfigSnapshot, error)
	GetConfigSnapshot(ctx context.Context, deviceID string, version int64) (ConfigSnapshot, error)
	GetLatestConfigSnapshot(ctx context.Context, deviceID string) (ConfigSnapshot, error)
	ListConfigSnapshots(ctx context.Context, deviceID string) ([]ConfigSnapshot, error)
	SetDesiredConfigVersion(ctx context.Context, deviceID string, version int64) (Device, error)
	AckConfigVersion(ctx context.Context, deviceID string, version int64, ackedAt time.Time) (Device, error)

	SaveIssuedDeviceToken(ctx context.Context, token IssuedDeviceToken) (IssuedDeviceToken, error)
	GetIssuedDeviceToken(ctx context.Context, tokenID string) (IssuedDeviceToken, error)
	FindIssuedDeviceTokenByHash(ctx context.Context, tokenHash string) (IssuedDeviceToken, error)
	TouchIssuedDeviceToken(ctx context.Context, tokenID string, usedAt time.Time) (IssuedDeviceToken, error)
	RevokeIssuedDeviceToken(ctx context.Context, tokenID string, revokedAt time.Time) (IssuedDeviceToken, error)
}

type DemoSeeder interface {
	SeedDemoData(ctx context.Context) error
}
