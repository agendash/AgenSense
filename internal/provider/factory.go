package provider

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/agendash/agensense/internal/device"
)

// Factory resolves provider clients from a stored profile.
type Factory struct {
	httpClient *http.Client
}

func NewFactory(httpClient *http.Client) *Factory {
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &Factory{httpClient: httpClient}
}

func (f *Factory) ASRClient(profile device.ProviderProfile) (ASRClient, error) {
	return resolveASRClient(profile, f.httpClient)
}

func (f *Factory) LLMClient(profile device.ProviderProfile) (LLMClient, error) {
	return resolveLLMClient(profile, f.httpClient)
}

func (f *Factory) TTSClient(profile device.ProviderProfile) (TTSClient, error) {
	return resolveTTSClient(profile, f.httpClient)
}

func resolveASRClient(profile device.ProviderProfile, httpClient *http.Client) (ASRClient, error) {
	baseURL := strings.TrimSpace(profile.ASRBaseURL)
	switch {
	case baseURL == "", strings.HasPrefix(baseURL, "mock://"):
		return MockASR{}, nil
	case strings.HasPrefix(baseURL, "http://"), strings.HasPrefix(baseURL, "https://"):
		return NewOpenAICompatibleASR(httpClient, baseURL, profile.ASRAPIKey, profile.ASRModel), nil
	default:
		return nil, fmt.Errorf("provider: unsupported ASR base url %q", baseURL)
	}
}

func resolveLLMClient(profile device.ProviderProfile, httpClient *http.Client) (LLMClient, error) {
	baseURL := strings.TrimSpace(profile.LLMBaseURL)
	switch {
	case baseURL == "", strings.HasPrefix(baseURL, "mock://"):
		return MockLLM{}, nil
	case strings.HasPrefix(baseURL, "http://"), strings.HasPrefix(baseURL, "https://"):
		return NewOpenAICompatibleLLM(httpClient, baseURL, profile.LLMAPIKey, profile.LLMModel), nil
	default:
		return nil, fmt.Errorf("provider: unsupported LLM base url %q", baseURL)
	}
}

func resolveTTSClient(profile device.ProviderProfile, httpClient *http.Client) (TTSClient, error) {
	baseURL := strings.TrimSpace(profile.TTSBaseURL)
	switch {
	case baseURL == "", strings.HasPrefix(baseURL, "mock://"):
		return MockTTS{}, nil
	case strings.HasPrefix(baseURL, "http://"), strings.HasPrefix(baseURL, "https://"):
		return NewOpenAICompatibleTTS(httpClient, baseURL, profile.TTSAPIKey, profile.TTSModel), nil
	default:
		return nil, fmt.Errorf("provider: unsupported TTS base url %q", baseURL)
	}
}
