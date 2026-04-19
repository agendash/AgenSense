package device

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"time"
)

const DefaultDeviceTokenTTL = 24 * time.Hour

type Clock interface {
	Now() time.Time
}

type ServiceOption func(*Service)

type Service struct {
	repo     Repository
	clock    Clock
	random   io.Reader
	tokenTTL time.Duration
}

type AuthenticateResult struct {
	Device Device
	Token  IssuedDeviceToken
}

type systemClock struct{}

func (systemClock) Now() time.Time {
	return time.Now().UTC()
}

func NewService(repo Repository, opts ...ServiceOption) *Service {
	svc := &Service{
		repo:     repo,
		clock:    systemClock{},
		random:   rand.Reader,
		tokenTTL: DefaultDeviceTokenTTL,
	}

	for _, opt := range opts {
		if opt != nil {
			opt(svc)
		}
	}

	return svc
}

func WithClock(clock Clock) ServiceOption {
	return func(svc *Service) {
		if clock != nil {
			svc.clock = clock
		}
	}
}

func WithRandomReader(random io.Reader) ServiceOption {
	return func(svc *Service) {
		if random != nil {
			svc.random = random
		}
	}
}

func WithTokenTTL(ttl time.Duration) ServiceOption {
	return func(svc *Service) {
		if ttl > 0 {
			svc.tokenTTL = ttl
		}
	}
}

func (svc *Service) Bootstrap(ctx context.Context, req BootstrapRequest) (BootstrapResult, error) {
	if strings.TrimSpace(req.DeviceID) == "" || strings.TrimSpace(req.ClaimToken) == "" {
		return BootstrapResult{}, ErrInvalidInput
	}

	dev, err := svc.repo.GetDevice(ctx, req.DeviceID)
	if err != nil {
		return BootstrapResult{}, err
	}

	if !matchesCredential(dev.FactoryClaimTokenHash, req.ClaimToken) {
		return BootstrapResult{}, ErrUnauthorized
	}

	if dev.ChipID != "" && req.ChipID != "" && dev.ChipID != req.ChipID {
		return BootstrapResult{}, ErrUnauthorized
	}

	if dev.HardwareSKU != "" && req.HardwareSKU != "" && dev.HardwareSKU != req.HardwareSKU {
		return BootstrapResult{}, ErrUnauthorized
	}

	now := svc.clock.Now()
	dev.FirmwareVersion = req.FirmwareVersion
	dev.FirmwareChannel = req.FirmwareChannel
	dev.Capabilities = cloneRawMessage(req.Capabilities)
	dev.UpdatedAt = now
	dev.LastBootstrapAt = &now

	dev, err = svc.repo.SaveDevice(ctx, dev)
	if err != nil {
		return BootstrapResult{}, err
	}

	instance, err := svc.repo.GetInstance(ctx, dev.InstanceID)
	if err != nil {
		return BootstrapResult{}, err
	}

	snapshot, err := svc.CurrentConfig(ctx, dev.ID)
	if err != nil {
		return BootstrapResult{}, err
	}

	var profile ProviderProfile
	if instance.ProviderProfileID != "" {
		profile, err = svc.repo.GetProviderProfile(ctx, instance.TenantID, instance.ProviderProfileID)
		if err != nil {
			return BootstrapResult{}, err
		}
	}

	issued, tokenValue, err := svc.IssueDeviceToken(ctx, dev)
	if err != nil {
		return BootstrapResult{}, err
	}

	return BootstrapResult{
		Device:          dev,
		Instance:        instance,
		ProviderProfile: profile,
		ConfigSnapshot:  snapshot,
		IssuedToken:     issued,
		TokenValue:      tokenValue,
	}, nil
}

func (svc *Service) CurrentConfig(ctx context.Context, deviceID string) (ConfigSnapshot, error) {
	dev, err := svc.repo.GetDevice(ctx, deviceID)
	if err != nil {
		return ConfigSnapshot{}, err
	}

	if dev.DesiredConfigVersion > 0 {
		snapshot, err := svc.repo.GetConfigSnapshot(ctx, deviceID, dev.DesiredConfigVersion)
		if err == nil {
			return snapshot, nil
		}

		if err != ErrNotFound {
			return ConfigSnapshot{}, err
		}
	}

	snapshot, err := svc.repo.GetLatestConfigSnapshot(ctx, deviceID)
	if err != nil {
		if err == ErrNotFound {
			return ConfigSnapshot{}, ErrConfigMissing
		}

		return ConfigSnapshot{}, err
	}

	return snapshot, nil
}

func (svc *Service) AckConfigVersion(ctx context.Context, deviceID string, version int64) (Device, error) {
	if strings.TrimSpace(deviceID) == "" || version <= 0 {
		return Device{}, ErrInvalidInput
	}

	return svc.repo.AckConfigVersion(ctx, deviceID, version, svc.clock.Now())
}

func (svc *Service) IssueDeviceToken(ctx context.Context, dev Device) (IssuedDeviceToken, string, error) {
	if strings.TrimSpace(dev.ID) == "" {
		return IssuedDeviceToken{}, "", ErrInvalidInput
	}

	tokenID, err := svc.randomTokenComponent(9)
	if err != nil {
		return IssuedDeviceToken{}, "", err
	}

	secret, err := svc.randomTokenComponent(24)
	if err != nil {
		return IssuedDeviceToken{}, "", err
	}

	tokenValue := fmt.Sprintf("dtk_%s.%s", tokenID, secret)
	now := svc.clock.Now()
	record := IssuedDeviceToken{
		ID:         "dtk_" + tokenID,
		DeviceID:   dev.ID,
		TenantID:   dev.TenantID,
		InstanceID: dev.InstanceID,
		TokenHash:  HashCredential(tokenValue),
		IssuedAt:   now,
		ExpiresAt:  now.Add(svc.tokenTTL),
	}

	record, err = svc.repo.SaveIssuedDeviceToken(ctx, record)
	if err != nil {
		return IssuedDeviceToken{}, "", err
	}

	return record, tokenValue, nil
}

func (svc *Service) AuthenticateDeviceToken(ctx context.Context, deviceID, tokenValue string) (AuthenticateResult, error) {
	if strings.TrimSpace(deviceID) == "" || strings.TrimSpace(tokenValue) == "" {
		return AuthenticateResult{}, ErrInvalidInput
	}

	record, err := svc.repo.FindIssuedDeviceTokenByHash(ctx, HashCredential(tokenValue))
	if err != nil {
		return AuthenticateResult{}, err
	}

	if record.DeviceID != deviceID {
		return AuthenticateResult{}, ErrUnauthorized
	}

	now := svc.clock.Now()
	if record.RevokedAt != nil {
		return AuthenticateResult{}, ErrUnauthorized
	}

	if !record.ExpiresAt.IsZero() && now.After(record.ExpiresAt) {
		return AuthenticateResult{}, ErrExpired
	}

	dev, err := svc.repo.GetDevice(ctx, record.DeviceID)
	if err != nil {
		return AuthenticateResult{}, err
	}

	record, err = svc.repo.TouchIssuedDeviceToken(ctx, record.ID, now)
	if err != nil {
		return AuthenticateResult{}, err
	}

	return AuthenticateResult{
		Device: dev,
		Token:  record,
	}, nil
}

func HashCredential(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func matchesCredential(expectedHash, credential string) bool {
	if expectedHash == "" {
		return false
	}

	actualHash := HashCredential(credential)
	return subtle.ConstantTimeCompare([]byte(expectedHash), []byte(actualHash)) == 1
}

func (svc *Service) randomTokenComponent(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := io.ReadFull(svc.random, buf); err != nil {
		return "", err
	}

	return strings.TrimRight(base64.RawURLEncoding.EncodeToString(buf), "="), nil
}
