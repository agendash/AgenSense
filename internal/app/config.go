package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	defaultOMLXProviderID      = "omlx-local"
	defaultOMLXProviderName    = "oMLX Local Voice Stack"
	defaultOMLXProviderBaseURL = "http://127.0.0.1:8000/v1"
	defaultOMLXASRModel        = "nemotron-3.5-asr-streaming-0.6b-8bit"
	defaultOMLXLLMModel        = "gemma-4-E4B-it-MLX-4bit"
	defaultOMLXMultimodalModel = "Qwen3.6-27B-MLX-4bit"
	defaultOMLXTTSModel        = "Qwen3-TTS-12Hz-0.6B-Base-8bit"
	defaultOMLXVADModel        = "silero-vad-v6"
)

// Config holds the local MVP runtime configuration.
type Config struct {
	Addr                   string
	PublicBaseURL          string
	DataDir                string
	LogLevel               string
	DebugEnabled           bool
	DisableDemoSeed        bool
	DemoDeviceID           string
	DemoClaimToken         string
	DemoTenantID           string
	DemoInstanceID         string
	DemoProviderProfileID  string
	DefaultAPIKey          string
	DefaultProviderID      string
	DefaultProviderName    string
	DefaultProviderBaseURL string
	DefaultProviderAPIKey  string
	DefaultASRModel        string
	DefaultLLMModel        string
	DefaultMultimodalModel string
	DefaultTTSModel        string
	DefaultVADModel        string
	RetryHintSec           int
}

// LoadConfig loads configuration from environment variables with safe local defaults.
func LoadConfig() (Config, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return Config{}, fmt.Errorf("load config: getwd: %w", err)
	}

	cfg := Config{
		Addr:                   envOrDefault("AGENSENSE_ADDR", ":8080"),
		PublicBaseURL:          envOrDefault("AGENSENSE_PUBLIC_BASE_URL", "http://127.0.0.1:8080"),
		DataDir:                envOrDefault("AGENSENSE_DATA_DIR", filepath.Join(cwd, "tmp", "agensense")),
		LogLevel:               envOrDefault("AGENSENSE_LOG_LEVEL", "info"),
		DebugEnabled:           envOrDefaultBool("AGENSENSE_DEBUG", false),
		DisableDemoSeed:        envOrDefaultBool("AGENSENSE_DISABLE_DEMO_SEED", false),
		DemoDeviceID:           envOrDefault("AGENSENSE_DEMO_DEVICE_ID", "demo-device-001"),
		DemoClaimToken:         envOrDefault("AGENSENSE_DEMO_CLAIM_TOKEN", "demo-claim-token"),
		DemoTenantID:           envOrDefault("AGENSENSE_DEMO_TENANT_ID", "local-tenant"),
		DemoInstanceID:         envOrDefault("AGENSENSE_DEMO_INSTANCE_ID", "local-instance"),
		DemoProviderProfileID:  envOrDefault("AGENSENSE_DEMO_PROVIDER_PROFILE_ID", defaultOMLXProviderID),
		DefaultAPIKey:          envOrDefault("AGENSENSE_DEFAULT_API_KEY", "demo-user-key"),
		DefaultProviderID:      envOrDefault("AGENSENSE_DEFAULT_PROVIDER_ID", defaultOMLXProviderID),
		DefaultProviderName:    envOrDefault("AGENSENSE_DEFAULT_PROVIDER_NAME", defaultOMLXProviderName),
		DefaultProviderBaseURL: envOrDefault("AGENSENSE_DEFAULT_PROVIDER_BASE_URL", defaultOMLXProviderBaseURL),
		DefaultProviderAPIKey:  envOrDefault("AGENSENSE_DEFAULT_PROVIDER_API_KEY", ""),
		DefaultASRModel:        envOrDefault("AGENSENSE_DEFAULT_ASR_MODEL", defaultOMLXASRModel),
		DefaultLLMModel:        envOrDefault("AGENSENSE_DEFAULT_LLM_MODEL", defaultOMLXLLMModel),
		DefaultMultimodalModel: envOrDefault("AGENSENSE_DEFAULT_MULTIMODAL_MODEL", defaultOMLXMultimodalModel),
		DefaultTTSModel:        envOrDefault("AGENSENSE_DEFAULT_TTS_MODEL", defaultOMLXTTSModel),
		DefaultVADModel:        envOrDefault("AGENSENSE_DEFAULT_VAD_MODEL", defaultOMLXVADModel),
		RetryHintSec:           envOrDefaultInt("AGENSENSE_RETRY_HINT_SEC", 30),
	}

	if !strings.HasPrefix(cfg.PublicBaseURL, "http://") && !strings.HasPrefix(cfg.PublicBaseURL, "https://") {
		return Config{}, fmt.Errorf("load config: AGENSENSE_PUBLIC_BASE_URL must start with http:// or https://")
	}
	return cfg, nil
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envOrDefaultInt(key string, fallback int) int {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			return parsed
		}
	}
	return fallback
}

func envOrDefaultBool(key string, fallback bool) bool {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		switch strings.ToLower(value) {
		case "1", "true", "yes", "on":
			return true
		case "0", "false", "no", "off":
			return false
		}
	}
	return fallback
}
