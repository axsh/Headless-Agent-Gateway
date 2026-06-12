package openai

import (
	"net/http"
	"testing"

	bifrostSchemas "github.com/maximhq/bifrost/core/schemas"
)

func TestProvider_Name(t *testing.T) {
	p := &Provider{}
	if got := p.Name(); got != "openai" {
		t.Errorf("Name() = %q, want %q", got, "openai")
	}
}

func TestProvider_BaseURL(t *testing.T) {
	p := &Provider{}
	if got := p.BaseURL(); got != "https://api.openai.com" {
		t.Errorf("BaseURL() = %q, want %q", got, "https://api.openai.com")
	}
}

func TestProvider_BifrostProvider(t *testing.T) {
	p := &Provider{}
	if got := p.BifrostProvider(); got != bifrostSchemas.OpenAI {
		t.Errorf("BifrostProvider() = %v, want %v", got, bifrostSchemas.OpenAI)
	}
}

func TestSetAuthHeaders(t *testing.T) {
	p := &Provider{}
	req, _ := http.NewRequest("POST", "https://example.com", nil)
	p.SetAuthHeaders(req, "sk-test-key", http.Header{})

	if got := req.Header.Get("Authorization"); got != "Bearer sk-test-key" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer sk-test-key")
	}
}
