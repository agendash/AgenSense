package store

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/zhuzhe/agensense/internal/device"
)

func TestFileRepositorySeedDemoAndReload(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.json")

	repo, err := NewFileRepository(path)
	if err != nil {
		t.Fatalf("new file repository: %v", err)
	}

	if err := repo.SeedDemoData(ctx); err != nil {
		t.Fatalf("seed demo data: %v", err)
	}

	dev, err := repo.GetDevice(ctx, device.DemoDeviceID)
	if err != nil {
		t.Fatalf("get demo device: %v", err)
	}

	if dev.DesiredConfigVersion != device.DemoConfigVersion {
		t.Fatalf("desired config version = %d, want %d", dev.DesiredConfigVersion, device.DemoConfigVersion)
	}

	reloaded, err := NewFileRepository(path)
	if err != nil {
		t.Fatalf("reload repository: %v", err)
	}

	instance, err := reloaded.GetInstance(ctx, device.DemoInstanceID)
	if err != nil {
		t.Fatalf("get instance after reload: %v", err)
	}

	if instance.ProviderProfileID != device.DemoProviderID {
		t.Fatalf("provider profile id = %q, want %q", instance.ProviderProfileID, device.DemoProviderID)
	}

	snapshot, err := reloaded.GetLatestConfigSnapshot(ctx, device.DemoDeviceID)
	if err != nil {
		t.Fatalf("get latest config snapshot: %v", err)
	}

	if snapshot.Version != device.DemoConfigVersion {
		t.Fatalf("config version = %d, want %d", snapshot.Version, device.DemoConfigVersion)
	}
}

func TestFileRepositoryPersistsConfigAndTokens(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.json")

	repo, err := NewFileRepository(path)
	if err != nil {
		t.Fatalf("new file repository: %v", err)
	}

	seedAt := time.Date(2026, 4, 17, 16, 0, 0, 0, time.UTC)
	if err := device.ApplyDemoSeed(ctx, repo, seedAt); err != nil {
		t.Fatalf("apply demo seed: %v", err)
	}

	secondSnapshot := device.ConfigSnapshot{
		DeviceID:          device.DemoDeviceID,
		TenantID:          device.DemoTenantID,
		InstanceID:        device.DemoInstanceID,
		Version:           2,
		ProviderProfileID: device.DemoProviderID,
		Source:            "unit-test",
		Config:            []byte(`{"voice":{"enabled":true},"providers":{"profile":"mock"},"debug":{"echo_audio_bytes":true}}`),
		CreatedAt:         seedAt.Add(time.Minute),
	}

	if _, err := repo.SaveConfigSnapshot(ctx, secondSnapshot); err != nil {
		t.Fatalf("save second snapshot: %v", err)
	}

	if _, err := repo.SetDesiredConfigVersion(ctx, device.DemoDeviceID, 2); err != nil {
		t.Fatalf("set desired config version: %v", err)
	}

	ackedAt := seedAt.Add(2 * time.Minute)
	if _, err := repo.AckConfigVersion(ctx, device.DemoDeviceID, 2, ackedAt); err != nil {
		t.Fatalf("ack config version: %v", err)
	}

	clock := &fixedClock{now: seedAt.Add(3 * time.Minute)}
	random := bytes.NewReader(bytes.Repeat([]byte{0x33}, 64))
	svc := device.NewService(repo, device.WithClock(clock), device.WithRandomReader(random), device.WithTokenTTL(time.Hour))

	devRecord, err := repo.GetDevice(ctx, device.DemoDeviceID)
	if err != nil {
		t.Fatalf("get device before issuing token: %v", err)
	}

	issued, tokenValue, err := svc.IssueDeviceToken(ctx, devRecord)
	if err != nil {
		t.Fatalf("issue device token: %v", err)
	}

	reloaded, err := NewFileRepository(path)
	if err != nil {
		t.Fatalf("reload repository: %v", err)
	}

	reloadedDevice, err := reloaded.GetDevice(ctx, device.DemoDeviceID)
	if err != nil {
		t.Fatalf("get reloaded device: %v", err)
	}

	if reloadedDevice.DesiredConfigVersion != 2 || reloadedDevice.ReportedConfigVersion != 2 {
		t.Fatalf("device versions after reload = (%d,%d), want (2,2)", reloadedDevice.DesiredConfigVersion, reloadedDevice.ReportedConfigVersion)
	}

	tokenRecord, err := reloaded.FindIssuedDeviceTokenByHash(ctx, device.HashCredential(tokenValue))
	if err != nil {
		t.Fatalf("find issued token by hash: %v", err)
	}

	if tokenRecord.ID != issued.ID {
		t.Fatalf("token id = %q, want %q", tokenRecord.ID, issued.ID)
	}

	snapshot, err := reloaded.GetLatestConfigSnapshot(ctx, device.DemoDeviceID)
	if err != nil {
		t.Fatalf("get latest config snapshot after reload: %v", err)
	}

	if snapshot.Version != 2 {
		t.Fatalf("latest config version = %d, want %d", snapshot.Version, 2)
	}
}

type fixedClock struct {
	now time.Time
}

func (clock *fixedClock) Now() time.Time {
	return clock.now
}
