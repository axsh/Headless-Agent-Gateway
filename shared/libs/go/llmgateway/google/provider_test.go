package google

import (
	"net/http"
	"testing"

	bifrostSchemas "github.com/maximhq/bifrost/core/schemas"
)

func TestProvider_Name(t *testing.T) {
	p := &Provider{}
	if got := p.Name(); got != "google" {
		t.Errorf("Name() = %q, want %q", got, "google")
	}
}

func TestProvider_BaseURL(t *testing.T) {
	p := &Provider{}
	if got := p.BaseURL(); got != "https://generativelanguage.googleapis.com" {
		t.Errorf("BaseURL() = %q, want %q", got, "https://generativelanguage.googleapis.com")
	}
}

func TestProvider_BifrostProvider(t *testing.T) {
	p := &Provider{}
	if got := p.BifrostProvider(); got != bifrostSchemas.Gemini {
		t.Errorf("BifrostProvider() = %v, want %v", got, bifrostSchemas.Gemini)
	}
}

func TestSetAuthHeaders(t *testing.T) {
	p := &Provider{}
	req, _ := http.NewRequest("POST", "https://example.com/v1/models", nil)
	p.SetAuthHeaders(req, "goog-key", http.Header{})

	if got := req.Header.Get("x-goog-api-key"); got != "goog-key" {
		t.Errorf("x-goog-api-key = %q, want %q", got, "goog-key")
	}
	// API key must NOT appear in URL query parameters.
	if got := req.URL.RawQuery; got != "" {
		t.Errorf("query should be empty (no API key in URL), got %q", got)
	}
}

func TestSetAuthHeaders_ExistingQuery(t *testing.T) {
	p := &Provider{}
	req, _ := http.NewRequest("POST", "https://example.com/v1/models?alt=json", nil)
	p.SetAuthHeaders(req, "goog-key", http.Header{})

	if got := req.URL.RawQuery; got != "alt=json" {
		t.Errorf("query = %q, want %q (key should not be appended)", got, "alt=json")
	}
	if got := req.Header.Get("x-goog-api-key"); got != "goog-key" {
		t.Errorf("x-goog-api-key = %q, want %q", got, "goog-key")
	}
}
