package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config holds the local MVP runtime configuration.
type Config struct {
	Addr                  string
	PublicBaseURL         string
	DataDir               string
	LogLevel              string
	DemoDeviceID          string
	DemoClaimToken        string
	DemoTenantID          string
	DemoInstanceID        string
	DemoProviderProfileID string
	RetryHintSec          int
}

// LoadConfig loads configuration from environment variables with safe local defaults.
func LoadConfig() (Config, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return Config{}, fmt.Errorf("load config: getwd: %w", err)
	}

	cfg := Config{
		Addr:                  envOrDefault("AGENSENSE_ADDR", ":8080"),
		PublicBaseURL:         envOrDefault("AGENSENSE_PUBLIC_BASE_URL", "http://127.0.0.1:8080"),
		DataDir:               envOrDefault("AGENSENSE_DATA_DIR", filepath.Join(cwd, "tmp", "agensense")),
		LogLevel:              envOrDefault("AGENSENSE_LOG_LEVEL", "info"),
		DemoDeviceID:          envOrDefault("AGENSENSE_DEMO_DEVICE_ID", "demo-device-001"),
		DemoClaimToken:        envOrDefault("AGENSENSE_DEMO_CLAIM_TOKEN", "demo-claim-token"),
		DemoTenantID:          envOrDefault("AGENSENSE_DEMO_TENANT_ID", "local-tenant"),
		DemoInstanceID:        envOrDefault("AGENSENSE_DEMO_INSTANCE_ID", "local-instance"),
		DemoProviderProfileID: envOrDefault("AGENSENSE_DEMO_PROVIDER_PROFILE_ID", "mock-default"),
		RetryHintSec:          envOrDefaultInt("AGENSENSE_RETRY_HINT_SEC", 30),
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
