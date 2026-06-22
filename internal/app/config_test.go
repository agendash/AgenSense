package app

import "testing"

func TestLoadConfigDefaultsToOMLXProvider(t *testing.T) {
	t.Setenv("AGENSENSE_DEMO_PROVIDER_PROFILE_ID", "")
	t.Setenv("AGENSENSE_DEFAULT_PROVIDER_ID", "")
	t.Setenv("AGENSENSE_DEFAULT_PROVIDER_NAME", "")
	t.Setenv("AGENSENSE_DEFAULT_PROVIDER_BASE_URL", "")
	t.Setenv("AGENSENSE_DEFAULT_ASR_MODEL", "")
	t.Setenv("AGENSENSE_DEFAULT_LLM_MODEL", "")
	t.Setenv("AGENSENSE_DEFAULT_MULTIMODAL_MODEL", "")
	t.Setenv("AGENSENSE_DEFAULT_TTS_MODEL", "")
	t.Setenv("AGENSENSE_DEFAULT_VAD_MODEL", "")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if cfg.DemoProviderProfileID != defaultOMLXProviderID {
		t.Fatalf("DemoProviderProfileID = %q, want %q", cfg.DemoProviderProfileID, defaultOMLXProviderID)
	}
	if cfg.DefaultProviderID != defaultOMLXProviderID {
		t.Fatalf("DefaultProviderID = %q, want %q", cfg.DefaultProviderID, defaultOMLXProviderID)
	}
	if cfg.DefaultProviderBaseURL != defaultOMLXProviderBaseURL {
		t.Fatalf("DefaultProviderBaseURL = %q, want %q", cfg.DefaultProviderBaseURL, defaultOMLXProviderBaseURL)
	}
	if cfg.DefaultASRModel != defaultOMLXASRModel {
		t.Fatalf("DefaultASRModel = %q, want %q", cfg.DefaultASRModel, defaultOMLXASRModel)
	}
	if cfg.DefaultLLMModel != defaultOMLXLLMModel {
		t.Fatalf("DefaultLLMModel = %q, want %q", cfg.DefaultLLMModel, defaultOMLXLLMModel)
	}
	if cfg.DefaultMultimodalModel != defaultOMLXMultimodalModel {
		t.Fatalf("DefaultMultimodalModel = %q, want %q", cfg.DefaultMultimodalModel, defaultOMLXMultimodalModel)
	}
	if cfg.DefaultTTSModel != defaultOMLXTTSModel {
		t.Fatalf("DefaultTTSModel = %q, want %q", cfg.DefaultTTSModel, defaultOMLXTTSModel)
	}
	if cfg.DefaultVADModel != defaultOMLXVADModel {
		t.Fatalf("DefaultVADModel = %q, want %q", cfg.DefaultVADModel, defaultOMLXVADModel)
	}
}

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
