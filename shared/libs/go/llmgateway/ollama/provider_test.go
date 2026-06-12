package ollama

import (
	"net/http"
	"testing"

	bifrostSchemas "github.com/maximhq/bifrost/core/schemas"
)

func TestProvider_Name(t *testing.T) {
	p := &Provider{}
	if got := p.Name(); got != "ollama" {
		t.Errorf("Name() = %q, want %q", got, "ollama")
	}
}

func TestProvider_BaseURL(t *testing.T) {
	p := &Provider{}
	if got := p.BaseURL(); got != "http://localhost:11434" {
		t.Errorf("BaseURL() = %q, want %q", got, "http://localhost:11434")
	}
}

func TestProvider_BifrostProvider(t *testing.T) {
	p := &Provider{}
	if got := p.BifrostProvider(); got != bifrostSchemas.Ollama {
		t.Errorf("BifrostProvider() = %v, want %v", got, bifrostSchemas.Ollama)
	}
}

func TestSetAuthHeaders_WithKey(t *testing.T) {
	p := &Provider{}
	req, _ := http.NewRequest("POST", "https://example.com", nil)
	p.SetAuthHeaders(req, "some-key", http.Header{})

	if got := req.Header.Get("Authorization"); got != "Bearer some-key" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer some-key")
	}
}

func TestSetAuthHeaders_NoKey(t *testing.T) {
	p := &Provider{}
	req, _ := http.NewRequest("POST", "https://example.com", nil)
	p.SetAuthHeaders(req, "", http.Header{})

	if got := req.Header.Get("Authorization"); got != "" {
		t.Errorf("Authorization should be empty, got %q", got)
	}
}
