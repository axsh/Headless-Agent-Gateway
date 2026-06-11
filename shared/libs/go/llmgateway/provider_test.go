package llmgateway

import (
	"net/http"
	"testing"

	bifrostSchemas "github.com/maximhq/bifrost/core/schemas"
)

func TestRegisterProvider_And_GetProvider(t *testing.T) {
	// Use init()-registered providers (already registered by the time tests run).
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
	RegisterProvider(&anthropicProvider{})
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

func TestSetAuthHeaders_Anthropic(t *testing.T) {
	p, _ := GetProvider("anthropic")
	req, _ := http.NewRequest("POST", "https://example.com", nil)
	originalHeaders := http.Header{}
	originalHeaders.Set("anthropic-beta", "test-beta")
	p.SetAuthHeaders(req, "test-key", originalHeaders)

	if got := req.Header.Get("x-api-key"); got != "test-key" {
		t.Errorf("x-api-key = %q, want %q", got, "test-key")
	}
	if got := req.Header.Get("anthropic-version"); got != "2023-06-01" {
		t.Errorf("anthropic-version = %q, want %q", got, "2023-06-01")
	}
	if got := req.Header.Get("anthropic-beta"); got != "test-beta" {
		t.Errorf("anthropic-beta = %q, want %q", got, "test-beta")
	}
}

func TestSetAuthHeaders_Anthropic_NoBeta(t *testing.T) {
	p, _ := GetProvider("anthropic")
	req, _ := http.NewRequest("POST", "https://example.com", nil)
	p.SetAuthHeaders(req, "test-key", http.Header{})

	if got := req.Header.Get("anthropic-beta"); got != "" {
		t.Errorf("anthropic-beta should be empty, got %q", got)
	}
}

func TestSetAuthHeaders_OpenAI(t *testing.T) {
	p, _ := GetProvider("openai")
	req, _ := http.NewRequest("POST", "https://example.com", nil)
	p.SetAuthHeaders(req, "sk-test-key", http.Header{})

	if got := req.Header.Get("Authorization"); got != "Bearer sk-test-key" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer sk-test-key")
	}
}

func TestSetAuthHeaders_Google(t *testing.T) {
	p, _ := GetProvider("google")
	req, _ := http.NewRequest("POST", "https://example.com/v1/models", nil)
	p.SetAuthHeaders(req, "goog-key", http.Header{})

	if got := req.Header.Get("x-goog-api-key"); got != "goog-key" {
		t.Errorf("x-goog-api-key = %q, want %q", got, "goog-key")
	}
	// R8: API key must NOT appear in URL query parameters.
	if got := req.URL.RawQuery; got != "" {
		t.Errorf("query should be empty (no API key in URL), got %q", got)
	}
}

func TestSetAuthHeaders_Google_ExistingQuery(t *testing.T) {
	p, _ := GetProvider("google")
	req, _ := http.NewRequest("POST", "https://example.com/v1/models?alt=json", nil)
	p.SetAuthHeaders(req, "goog-key", http.Header{})

	// R8: Existing query params should be preserved, but API key must NOT be appended.
	if got := req.URL.RawQuery; got != "alt=json" {
		t.Errorf("query = %q, want %q (key should not be appended)", got, "alt=json")
	}
	if got := req.Header.Get("x-goog-api-key"); got != "goog-key" {
		t.Errorf("x-goog-api-key = %q, want %q", got, "goog-key")
	}
}

func TestSetAuthHeaders_Ollama_WithKey(t *testing.T) {
	p, _ := GetProvider("ollama")
	req, _ := http.NewRequest("POST", "https://example.com", nil)
	p.SetAuthHeaders(req, "some-key", http.Header{})

	if got := req.Header.Get("Authorization"); got != "Bearer some-key" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer some-key")
	}
}

func TestSetAuthHeaders_Ollama_NoKey(t *testing.T) {
	p, _ := GetProvider("ollama")
	req, _ := http.NewRequest("POST", "https://example.com", nil)
	p.SetAuthHeaders(req, "", http.Header{})

	if got := req.Header.Get("Authorization"); got != "" {
		t.Errorf("Authorization should be empty, got %q", got)
	}
}
