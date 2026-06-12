package anthropic

import (
	"net/http"
	"testing"

	bifrostSchemas "github.com/maximhq/bifrost/core/schemas"
)

func TestProvider_Name(t *testing.T) {
	p := &Provider{}
	if got := p.Name(); got != "anthropic" {
		t.Errorf("Name() = %q, want %q", got, "anthropic")
	}
}

func TestProvider_BaseURL(t *testing.T) {
	p := &Provider{}
	if got := p.BaseURL(); got != "https://api.anthropic.com" {
		t.Errorf("BaseURL() = %q, want %q", got, "https://api.anthropic.com")
	}
}

func TestProvider_BifrostProvider(t *testing.T) {
	p := &Provider{}
	if got := p.BifrostProvider(); got != bifrostSchemas.Anthropic {
		t.Errorf("BifrostProvider() = %v, want %v", got, bifrostSchemas.Anthropic)
	}
}

func TestSetAuthHeaders(t *testing.T) {
	p := &Provider{}
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

func TestSetAuthHeaders_NoBeta(t *testing.T) {
	p := &Provider{}
	req, _ := http.NewRequest("POST", "https://example.com", nil)
	p.SetAuthHeaders(req, "test-key", http.Header{})

	if got := req.Header.Get("anthropic-beta"); got != "" {
		t.Errorf("anthropic-beta should be empty, got %q", got)
	}
}
