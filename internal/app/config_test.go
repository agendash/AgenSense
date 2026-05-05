package app

import "testing"

func TestLoadConfigDebugDisabledByDefault(t *testing.T) {
	t.Setenv("AGENSENSE_DEBUG", "")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.DebugEnabled {
		t.Fatal("DebugEnabled = true, want false")
	}
}

func TestLoadConfigDebugEnabledFromEnv(t *testing.T) {
	t.Setenv("AGENSENSE_DEBUG", "true")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if !cfg.DebugEnabled {
		t.Fatal("DebugEnabled = false, want true")
	}
}
