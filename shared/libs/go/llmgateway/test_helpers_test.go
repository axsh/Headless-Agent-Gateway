package llmgateway

import (
	"net/http"

	bifrostSchemas "github.com/maximhq/bifrost/core/schemas"
)

// registerTestProviders registers all standard providers for testing.
// This is needed because provider subpackages (anthropic, openai, etc.)
// cannot be imported from the parent package due to import cycles.
// In production, the consuming binary imports these subpackages.
func registerTestProviders() {
	// Only register if not already registered (avoids panic on duplicate).
	providerMu.RLock()
	_, hasAnthropic := providerRegistry["anthropic"]
	providerMu.RUnlock()

	if hasAnthropic {
		return // Already registered.
	}

	RegisterProvider(&testAnthropicProvider{})
	RegisterProvider(&testOpenAIProvider{})
	RegisterProvider(&testGoogleProvider{})
	RegisterProvider(&testOllamaProvider{})
}

// --- Test provider implementations (mirror subpackage behavior) ---

type testAnthropicProvider struct{}

func (p *testAnthropicProvider) Name() string    { return "anthropic" }
func (p *testAnthropicProvider) BaseURL() string { return "https://api.anthropic.com" }
func (p *testAnthropicProvider) SetAuthHeaders(req *http.Request, apiKey string, originalHeaders http.Header) {
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	if beta := originalHeaders.Get("anthropic-beta"); beta != "" {
		req.Header.Set("anthropic-beta", beta)
	}
}
func (p *testAnthropicProvider) BifrostProvider() bifrostSchemas.ModelProvider {
	return bifrostSchemas.Anthropic
}

type testOpenAIProvider struct{}

func (p *testOpenAIProvider) Name() string    { return "openai" }
func (p *testOpenAIProvider) BaseURL() string { return "https://api.openai.com" }
func (p *testOpenAIProvider) SetAuthHeaders(req *http.Request, apiKey string, _ http.Header) {
	req.Header.Set("Authorization", "Bearer "+apiKey)
}
func (p *testOpenAIProvider) BifrostProvider() bifrostSchemas.ModelProvider {
	return bifrostSchemas.OpenAI
}

type testGoogleProvider struct{}

func (p *testGoogleProvider) Name() string    { return "google" }
func (p *testGoogleProvider) BaseURL() string { return "https://generativelanguage.googleapis.com" }
func (p *testGoogleProvider) SetAuthHeaders(req *http.Request, apiKey string, _ http.Header) {
	req.Header.Set("x-goog-api-key", apiKey)
	req.Header.Del("Authorization")
}
func (p *testGoogleProvider) BifrostProvider() bifrostSchemas.ModelProvider {
	return bifrostSchemas.Gemini
}

type testOllamaProvider struct{}

func (p *testOllamaProvider) Name() string    { return "ollama" }
func (p *testOllamaProvider) BaseURL() string { return "http://localhost:11434" }
func (p *testOllamaProvider) SetAuthHeaders(req *http.Request, apiKey string, _ http.Header) {
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
}
func (p *testOllamaProvider) BifrostProvider() bifrostSchemas.ModelProvider {
	return bifrostSchemas.Ollama
}
