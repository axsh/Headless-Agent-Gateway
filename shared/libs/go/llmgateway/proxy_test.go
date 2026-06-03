package llmgateway

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

// Compile-time check: ProxyServer implements LLMGatewayBackend.
var _ LLMGatewayBackend = (*ProxyServer)(nil)

func newTestProxy(t *testing.T) *ProxyServer {
	t.Helper()
	// Use port 0 for ephemeral port assignment.
	p, err := NewProxyServer(nil, nil, nil)
	if err != nil {
		t.Fatalf("NewProxyServer() error = %v", err)
	}
	return p
}

func TestProxyServer_Launch_Shutdown(t *testing.T) {
	p := newTestProxy(t)

	if err := p.Launch(nil); err != nil {
		t.Fatalf("Launch() error = %v", err)
	}

	url := p.ProxyURL()
	if url == "" {
		t.Fatal("ProxyURL() returned empty string after Launch()")
	}

	// Verify the server is reachable.
	resp, err := http.Get(url + "/")
	if err != nil {
		t.Fatalf("GET / error = %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET / status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	if err := p.Shutdown(nil); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	// Verify the server is no longer reachable.
	_, err = http.Get(url + "/")
	if err == nil {
		t.Fatal("expected error after Shutdown(), got nil")
	}
}

func TestProxyServer_Index(t *testing.T) {
	p := newTestProxy(t)
	if err := p.Launch(nil); err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	defer p.Shutdown(nil)

	resp, err := http.Get(p.ProxyURL() + "/")
	if err != nil {
		t.Fatalf("GET / error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	ct := resp.Header.Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}

	var body struct {
		Endpoints []string `json:"endpoints"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("json decode error: %v", err)
	}
	if len(body.Endpoints) == 0 {
		t.Error("expected non-empty endpoints list")
	}
}

func TestProxyServer_Health(t *testing.T) {
	p := newTestProxy(t)
	if err := p.Launch(nil); err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	defer p.Shutdown(nil)

	resp, err := http.Get(p.ProxyURL() + "/health")
	if err != nil {
		t.Fatalf("GET /health error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var health HealthStatus
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		t.Fatalf("json decode error: %v", err)
	}
	if health.Status != "ok" {
		t.Errorf("status = %q, want %q", health.Status, "ok")
	}
}

func TestProxyServer_Models(t *testing.T) {
	p := newTestProxy(t)
	if err := p.Launch(nil); err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	defer p.Shutdown(nil)

	resp, err := http.Get(p.ProxyURL() + "/v1/models")
	if err != nil {
		t.Fatalf("GET /v1/models error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var body struct {
		Models []ModelInfo `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("json decode error: %v", err)
	}
	// With nil config, models list should be empty but valid JSON.
	if body.Models == nil {
		t.Error("expected non-nil models array (can be empty)")
	}
}

func TestProxyServer_AnthropicStub(t *testing.T) {
	p := newTestProxy(t)
	if err := p.Launch(nil); err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	defer p.Shutdown(nil)

	resp, err := http.Post(p.ProxyURL()+"/v1/messages", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /v1/messages error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotImplemented {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotImplemented)
	}
}

func TestProxyServer_OpenAIStub(t *testing.T) {
	p := newTestProxy(t)
	if err := p.Launch(nil); err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	defer p.Shutdown(nil)

	resp, err := http.Post(p.ProxyURL()+"/v1/chat/completions", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /v1/chat/completions error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotImplemented {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotImplemented)
	}
}

func TestProxyServer_ProxyURL(t *testing.T) {
	p := newTestProxy(t)
	if err := p.Launch(nil); err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	defer p.Shutdown(nil)

	url := p.ProxyURL()
	if url == "" {
		t.Fatal("ProxyURL() returned empty string")
	}

	// Should be http://localhost:{port} format.
	if len(url) < len("http://localhost:") {
		t.Errorf("ProxyURL() = %q, too short", url)
	}
}

func TestProxyServer_ListModels(t *testing.T) {
	p := newTestProxy(t)
	models := p.ListModels()
	// With nil config, no models are loaded.
	if len(models) != 0 {
		t.Errorf("ListModels() = %v, want empty slice", models)
	}
}

func TestProxyServer_NotFoundRoute(t *testing.T) {
	p := newTestProxy(t)
	if err := p.Launch(nil); err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	defer p.Shutdown(nil)

	resp, err := http.Get(p.ProxyURL() + "/nonexistent")
	if err != nil {
		t.Fatalf("GET /nonexistent error = %v", err)
	}
	defer resp.Body.Close()
	io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}
