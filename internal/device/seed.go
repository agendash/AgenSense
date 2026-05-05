package device

import (
	"context"
	"encoding/json"
	"time"
)

const (
	DemoTenantID        = "home-lab"
	DemoInstanceID      = "cn-shanghai-main"
	DemoProviderID      = "default"
	DemoDeviceID        = "vdk-coreS3-001"
	DemoClaimToken      = "factory-claim-token"
	DemoGatewayWSURL    = "ws://127.0.0.1:8080/v1/session/ws"
	DemoConfigSource    = "demo-seed"
	DemoConfigVersion   = int64(1)
	DemoFirmwareSKU     = "m5cores3-facekit-audio"
	DemoFirmwareChipID  = "esp32s3-abcdef"
	DemoProviderName    = "Mock Default"
	DemoProviderBaseURL = "mock://default"
	DemoProviderAPIKey  = ""
	DemoASRModel        = "mock-asr"
	DemoLLMModel        = "mock-llm"
	DemoTTSModel        = "mock-tts"
)

type DemoSeed struct {
	Instance        Instance
	Device          Device
	ProviderProfile ProviderProfile
	ConfigSnapshot  ConfigSnapshot
}

func BuildDemoSeed(now time.Time) DemoSeed {
	config := map[string]any{
		"voice": map[string]any{
			"enabled": true,
		},
		"providers": map[string]any{
			"profile": DemoProviderID,
		},
		"features": map[string]any{
			"hid": true,
		},
	}

	configJSON, _ := json.Marshal(config)

	instanceTemplate := map[string]any{
		"voice": map[string]any{
			"enabled": true,
		},
		"providers": map[string]any{
			"profile": DemoProviderID,
		},
	}

	templateJSON, _ := json.Marshal(instanceTemplate)

	capabilities := map[string]any{
		"display":    "lcd",
		"touch":      true,
		"usb_hid":    true,
		"usb_mic":    true,
		"cellular":   false,
		"microphone": true,
	}

	capabilitiesJSON, _ := json.Marshal(capabilities)

	return DemoSeed{
		Instance: Instance{
			ID:                DemoInstanceID,
			TenantID:          DemoTenantID,
			Name:              "Demo Shanghai Lab",
			Region:            "cn-shanghai",
			GatewayWSURL:      DemoGatewayWSURL,
			ProviderProfileID: DemoProviderID,
			ConfigTemplate:    templateJSON,
			CreatedAt:         now,
			UpdatedAt:         now,
		},
		Device: Device{
			ID:                    DemoDeviceID,
			TenantID:              DemoTenantID,
			InstanceID:            DemoInstanceID,
			HardwareSKU:           DemoFirmwareSKU,
			ChipID:                DemoFirmwareChipID,
			FirmwareVersion:       "1.2.0",
			FirmwareChannel:       "stable",
			Capabilities:          capabilitiesJSON,
			FactoryClaimTokenHash: HashCredential(DemoClaimToken),
			DesiredConfigVersion:  DemoConfigVersion,
			ReportedConfigVersion: 0,
			CreatedAt:             now,
			UpdatedAt:             now,
		},
		ProviderProfile: ProviderProfile{
			ID:         DemoProviderID,
			TenantID:   DemoTenantID,
			Name:       DemoProviderName,
			ASRBaseURL: DemoProviderBaseURL,
			ASRAPIKey:  DemoProviderAPIKey,
			ASRModel:   DemoASRModel,
			LLMBaseURL: DemoProviderBaseURL,
			LLMAPIKey:  DemoProviderAPIKey,
			LLMModel:   DemoLLMModel,
			TTSBaseURL: DemoProviderBaseURL,
			TTSAPIKey:  DemoProviderAPIKey,
			TTSModel:   DemoTTSModel,
			CreatedAt:  now,
			UpdatedAt:  now,
		},
		ConfigSnapshot: ConfigSnapshot{
			DeviceID:          DemoDeviceID,
			TenantID:          DemoTenantID,
			InstanceID:        DemoInstanceID,
			Version:           DemoConfigVersion,
			ProviderProfileID: DemoProviderID,
			Source:            DemoConfigSource,
			Config:            configJSON,
			CreatedAt:         now,
		},
	}
}

func ApplyDemoSeed(ctx context.Context, repo Repository, now time.Time) error {
	seed := BuildDemoSeed(now)

	if _, err := repo.SaveInstance(ctx, seed.Instance); err != nil {
		return err
	}

	if _, err := repo.SaveProviderProfile(ctx, seed.ProviderProfile); err != nil {
		return err
	}

	if _, err := repo.SaveDevice(ctx, seed.Device); err != nil {
		return err
	}

	if _, err := repo.SaveConfigSnapshot(ctx, seed.ConfigSnapshot); err != nil {
		return err
	}

	_, err := repo.SetDesiredConfigVersion(ctx, seed.Device.ID, seed.ConfigSnapshot.Version)
	return err
}
