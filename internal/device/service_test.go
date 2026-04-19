package device

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
)

type mutableClock struct {
	now time.Time
}

func (clock *mutableClock) Now() time.Time {
	return clock.now
}

func seedMemoryRepository(t *testing.T, repo Repository, now time.Time) {
	t.Helper()

	if err := ApplyDemoSeed(context.Background(), repo, now); err != nil {
		t.Fatalf("seed demo data: %v", err)
	}
}

func TestServiceBootstrapAndAuthenticate(t *testing.T) {
	repo := NewMemoryRepository()
	seedAt := time.Date(2026, 4, 17, 15, 0, 0, 0, time.UTC)
	seedMemoryRepository(t, repo, seedAt)

	clock := &mutableClock{now: seedAt.Add(5 * time.Minute)}
	random := bytes.NewReader(bytes.Repeat([]byte{0x11}, 64))
	svc := NewService(repo, WithClock(clock), WithRandomReader(random), WithTokenTTL(2*time.Hour))

	result, err := svc.Bootstrap(context.Background(), BootstrapRequest{
		DeviceID:        DemoDeviceID,
		ChipID:          DemoFirmwareChipID,
		HardwareSKU:     DemoFirmwareSKU,
		FirmwareVersion: "1.3.0",
		FirmwareChannel: "beta",
		Capabilities:    []byte(`{"usb_hid":true,"display":"lcd"}`),
		ClaimToken:      DemoClaimToken,
	})
	if err != nil {
		t.Fatalf("bootstrap device: %v", err)
	}

	if result.Device.FirmwareVersion != "1.3.0" {
		t.Fatalf("firmware version = %q, want %q", result.Device.FirmwareVersion, "1.3.0")
	}

	if result.Instance.ID != DemoInstanceID {
		t.Fatalf("instance id = %q, want %q", result.Instance.ID, DemoInstanceID)
	}

	if result.ProviderProfile.ID != DemoProviderID {
		t.Fatalf("provider profile id = %q, want %q", result.ProviderProfile.ID, DemoProviderID)
	}

	if result.ConfigSnapshot.Version != DemoConfigVersion {
		t.Fatalf("config version = %d, want %d", result.ConfigSnapshot.Version, DemoConfigVersion)
	}

	if result.TokenValue == "" {
		t.Fatal("token value should not be empty")
	}

	clock.now = clock.now.Add(30 * time.Second)
	auth, err := svc.AuthenticateDeviceToken(context.Background(), DemoDeviceID, result.TokenValue)
	if err != nil {
		t.Fatalf("authenticate device token: %v", err)
	}

	if auth.Token.ID != result.IssuedToken.ID {
		t.Fatalf("token id = %q, want %q", auth.Token.ID, result.IssuedToken.ID)
	}

	if auth.Token.LastUsedAt == nil || !auth.Token.LastUsedAt.Equal(clock.now) {
		t.Fatalf("last used at = %v, want %v", auth.Token.LastUsedAt, clock.now)
	}
}

func TestServiceRejectsExpiredDeviceToken(t *testing.T) {
	repo := NewMemoryRepository()
	seedAt := time.Date(2026, 4, 17, 15, 0, 0, 0, time.UTC)
	seedMemoryRepository(t, repo, seedAt)

	clock := &mutableClock{now: seedAt}
	random := bytes.NewReader(bytes.Repeat([]byte{0x22}, 64))
	svc := NewService(repo, WithClock(clock), WithRandomReader(random), WithTokenTTL(time.Minute))

	result, err := svc.Bootstrap(context.Background(), BootstrapRequest{
		DeviceID:    DemoDeviceID,
		ClaimToken:  DemoClaimToken,
		ChipID:      DemoFirmwareChipID,
		HardwareSKU: DemoFirmwareSKU,
	})
	if err != nil {
		t.Fatalf("bootstrap device: %v", err)
	}

	clock.now = clock.now.Add(2 * time.Minute)
	_, err = svc.AuthenticateDeviceToken(context.Background(), DemoDeviceID, result.TokenValue)
	if !errors.Is(err, ErrExpired) {
		t.Fatalf("authenticate expired token error = %v, want %v", err, ErrExpired)
	}
}

func TestServiceAckConfigVersion(t *testing.T) {
	repo := NewMemoryRepository()
	seedAt := time.Date(2026, 4, 17, 15, 0, 0, 0, time.UTC)
	seedMemoryRepository(t, repo, seedAt)

	secondSnapshot := ConfigSnapshot{
		DeviceID:          DemoDeviceID,
		TenantID:          DemoTenantID,
		InstanceID:        DemoInstanceID,
		Version:           2,
		ProviderProfileID: DemoProviderID,
		Source:            "unit-test",
		Config:            []byte(`{"voice":{"enabled":true},"providers":{"profile":"mock"},"debug":{"log_level":"debug"}}`),
		CreatedAt:         seedAt.Add(time.Minute),
	}

	if _, err := repo.SaveConfigSnapshot(context.Background(), secondSnapshot); err != nil {
		t.Fatalf("save second snapshot: %v", err)
	}

	if _, err := repo.SetDesiredConfigVersion(context.Background(), DemoDeviceID, 2); err != nil {
		t.Fatalf("set desired config version: %v", err)
	}

	clock := &mutableClock{now: seedAt.Add(2 * time.Minute)}
	svc := NewService(repo, WithClock(clock))

	dev, err := svc.AckConfigVersion(context.Background(), DemoDeviceID, 2)
	if err != nil {
		t.Fatalf("ack config version: %v", err)
	}

	if dev.ReportedConfigVersion != 2 {
		t.Fatalf("reported config version = %d, want %d", dev.ReportedConfigVersion, 2)
	}

	if dev.LastSeenAt == nil || !dev.LastSeenAt.Equal(clock.now) {
		t.Fatalf("last seen at = %v, want %v", dev.LastSeenAt, clock.now)
	}
}
