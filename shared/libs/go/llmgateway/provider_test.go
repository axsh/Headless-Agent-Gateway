package llmgateway

import (
	"net/http"
	"testing"

	bifrostSchemas "github.com/maximhq/bifrost/core/schemas"
)

func TestRegisterProvider_And_GetProvider(t *testing.T) {
	// Providers are registered by TestMain via registerTestProviders().
	tests := []struct {
		name         string
		providerName string
		wantOK       bool
	}{
		{"anthropic is registered", "anthropic", true},
		{"openai is registered", "openai", true},
		{"google is registered", "google", true},
		{"ollama is registered", "ollama", true},
		{"unknown is not registered", "unknown", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := GetProvider(tt.providerName)
			if ok != tt.wantOK {
				t.Errorf("GetProvider(%q) ok = %v, want %v", tt.providerName, ok, tt.wantOK)
			}
		})
	}
}

func TestRegisterProvider_DuplicatePanics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for duplicate registration, got none")
		}
	}()

	// Attempt to register a provider with a name that's already taken.
	dummy := &dummyProvider{name: "anthropic"}
	RegisterProvider(dummy)
}

func TestAllProviders_HaveRequiredFields(t *testing.T) {
	expected := map[string]struct {
		baseURL        string
		bifrostProvider bifrostSchemas.ModelProvider
	}{
		"anthropic": {"https://api.anthropic.com", bifrostSchemas.Anthropic},
		"openai":    {"https://api.openai.com", bifrostSchemas.OpenAI},
		"google":    {"https://generativelanguage.googleapis.com", bifrostSchemas.Gemini},
		"ollama":    {"http://localhost:11434", bifrostSchemas.Ollama},
	}

	for name, want := range expected {
		p, ok := GetProvider(name)
		if !ok {
			t.Fatalf("provider %q not registered", name)
		}
		if got := p.BaseURL(); got != want.baseURL {
			t.Errorf("provider %q BaseURL = %q, want %q", name, got, want.baseURL)
		}
		if got := p.BifrostProvider(); got != want.bifrostProvider {
			t.Errorf("provider %q BifrostProvider = %v, want %v", name, got, want.bifrostProvider)
		}
	}
}

// dummyProvider is a test-only Provider implementation.
type dummyProvider struct {
	name string
}

func (d *dummyProvider) Name() string    { return d.name }
func (d *dummyProvider) BaseURL() string { return "http://example.com" }
func (d *dummyProvider) SetAuthHeaders(_ *http.Request, _ string, _ http.Header) {}
func (d *dummyProvider) BifrostProvider() bifrostSchemas.ModelProvider {
	return bifrostSchemas.Anthropic
}
