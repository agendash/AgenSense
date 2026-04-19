package device

import (
	"encoding/json"
	"time"
)

type Instance struct {
	ID                string          `json:"id"`
	TenantID          string          `json:"tenant_id"`
	Name              string          `json:"name,omitempty"`
	Region            string          `json:"region,omitempty"`
	GatewayWSURL      string          `json:"gateway_ws_url"`
	ProviderProfileID string          `json:"provider_profile_id,omitempty"`
	ConfigTemplate    json.RawMessage `json:"config_template,omitempty"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
}

type Device struct {
	ID                    string          `json:"id"`
	TenantID              string          `json:"tenant_id"`
	InstanceID            string          `json:"instance_id"`
	HardwareSKU           string          `json:"hardware_sku"`
	ChipID                string          `json:"chip_id"`
	MACAddr               string          `json:"mac_addr,omitempty"`
	FirmwareVersion       string          `json:"firmware_version,omitempty"`
	FirmwareChannel       string          `json:"firmware_channel,omitempty"`
	Capabilities          json.RawMessage `json:"capabilities,omitempty"`
	FactoryClaimTokenHash string          `json:"factory_claim_token_hash,omitempty"`
	DesiredConfigVersion  int64           `json:"desired_config_version"`
	ReportedConfigVersion int64           `json:"reported_config_version"`
	LastBootstrapAt       *time.Time      `json:"last_bootstrap_at,omitempty"`
	LastSeenAt            *time.Time      `json:"last_seen_at,omitempty"`
	CreatedAt             time.Time       `json:"created_at"`
	UpdatedAt             time.Time       `json:"updated_at"`
}

type ProviderProfile struct {
	ID         string    `json:"id"`
	TenantID   string    `json:"tenant_id"`
	Name       string    `json:"name,omitempty"`
	ASRBaseURL string    `json:"asr_base_url,omitempty"`
	ASRAPIKey  string    `json:"asr_api_key,omitempty"`
	ASRModel   string    `json:"asr_model,omitempty"`
	LLMBaseURL string    `json:"llm_base_url,omitempty"`
	LLMAPIKey  string    `json:"llm_api_key,omitempty"`
	LLMModel   string    `json:"llm_model,omitempty"`
	TTSBaseURL string    `json:"tts_base_url,omitempty"`
	TTSAPIKey  string    `json:"tts_api_key,omitempty"`
	TTSModel   string    `json:"tts_model,omitempty"`
	VADBaseURL string    `json:"vad_base_url,omitempty"`
	VADAPIKey  string    `json:"vad_api_key,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type ConfigSnapshot struct {
	DeviceID          string          `json:"device_id"`
	TenantID          string          `json:"tenant_id"`
	InstanceID        string          `json:"instance_id"`
	Version           int64           `json:"version"`
	ProviderProfileID string          `json:"provider_profile_id,omitempty"`
	Source            string          `json:"source,omitempty"`
	Config            json.RawMessage `json:"config"`
	Signature         string          `json:"signature,omitempty"`
	CreatedAt         time.Time       `json:"created_at"`
}

type IssuedDeviceToken struct {
	ID         string     `json:"id"`
	DeviceID   string     `json:"device_id"`
	TenantID   string     `json:"tenant_id"`
	InstanceID string     `json:"instance_id"`
	TokenHash  string     `json:"token_hash"`
	IssuedAt   time.Time  `json:"issued_at"`
	ExpiresAt  time.Time  `json:"expires_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
}

type BootstrapRequest struct {
	DeviceID        string          `json:"device_id"`
	ChipID          string          `json:"chip_id"`
	HardwareSKU     string          `json:"hardware_sku"`
	FirmwareVersion string          `json:"firmware_version,omitempty"`
	FirmwareChannel string          `json:"firmware_channel,omitempty"`
	Capabilities    json.RawMessage `json:"capabilities,omitempty"`
	ClaimToken      string          `json:"claim_token"`
}

type BootstrapResult struct {
	Device          Device            `json:"device"`
	Instance        Instance          `json:"instance"`
	ProviderProfile ProviderProfile   `json:"provider_profile"`
	ConfigSnapshot  ConfigSnapshot    `json:"config_snapshot"`
	IssuedToken     IssuedDeviceToken `json:"issued_token"`
	TokenValue      string            `json:"token_value"`
}

type DeviceFilter struct {
	TenantID   string
	InstanceID string
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

func cloneInstance(in Instance) Instance {
	in.ConfigTemplate = cloneRawMessage(in.ConfigTemplate)
	return in
}

func cloneDevice(in Device) Device {
	in.Capabilities = cloneRawMessage(in.Capabilities)
	in.LastBootstrapAt = cloneTimePtr(in.LastBootstrapAt)
	in.LastSeenAt = cloneTimePtr(in.LastSeenAt)
	return in
}

func cloneProviderProfile(in ProviderProfile) ProviderProfile {
	return in
}

func cloneConfigSnapshot(in ConfigSnapshot) ConfigSnapshot {
	in.Config = cloneRawMessage(in.Config)
	return in
}

func cloneIssuedDeviceToken(in IssuedDeviceToken) IssuedDeviceToken {
	in.LastUsedAt = cloneTimePtr(in.LastUsedAt)
	in.RevokedAt = cloneTimePtr(in.RevokedAt)
	return in
}
