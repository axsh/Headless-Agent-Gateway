package llmgateway

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/axsh/hag/config"
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

	// Standalone ProxyServer (no driver) - nil body produces 400 from JSON parsing.
	resp, err := http.Post(p.ProxyURL()+"/v1/messages", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /v1/messages error = %v", err)
	}
	defer resp.Body.Close()

	// Without driver, nil body -> 400 Bad Request (invalid JSON)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestProxyServer_OpenAIStub(t *testing.T) {
	p := newTestProxy(t)
	if err := p.Launch(nil); err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	defer p.Shutdown(nil)

	// Standalone ProxyServer (no driver) - nil body produces 400 from JSON parsing.
	resp, err := http.Post(p.ProxyURL()+"/v1/chat/completions", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /v1/chat/completions error = %v", err)
	}
	defer resp.Body.Close()

	// Without driver, nil body -> 400 Bad Request (invalid JSON)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
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

// writeTestModelProfiles creates a minimal model_profiles.yaml in a temp dir.
func writeTestModelProfiles(t *testing.T) (string, int) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "model_profiles.yaml")
	yaml := `
default_profile:
  provider: anthropic
  model: claude-sonnet-4-20250514
providers:
  anthropic:
    keys:
      - name: primary
        value: "vault://providers/anthropic/primary"
        models:
          - name: claude-sonnet-4-20250514
  openai:
    keys:
      - name: primary
        value: "vault://providers/openai/primary"
        models:
          - name: gpt-4o
`
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatalf("write model_profiles.yaml: %v", err)
	}
	return path, 2 // 2 models total
}

func newTestProxyWithProfiles(t *testing.T) (*ProxyServer, int) {
	t.Helper()
	path, modelCount := writeTestModelProfiles(t)
	cfg := &config.AppConfig{
		LLMGateway: config.LLMGatewayConfig{
			ModelProfilesPath: path,
		},
	}
	p, err := NewProxyServer(cfg, nil, nil)
	if err != nil {
		t.Fatalf("NewProxyServer() error = %v", err)
	}
	return p, modelCount
}

// TC-P2-03: /v1/models returns model data from profiles.
func TestProxyServer_ModelsWithProfiles(t *testing.T) {
	p, expectedCount := newTestProxyWithProfiles(t)
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
	if len(body.Models) != expectedCount {
		t.Errorf("models count = %d, want %d", len(body.Models), expectedCount)
	}
	for i, m := range body.Models {
		if m.Provider == "" {
			t.Errorf("models[%d].Provider is empty", i)
		}
		if m.Model == "" {
			t.Errorf("models[%d].Model is empty", i)
		}
	}
}

// TC-P2-04: Health.Models reflects profile model count.
func TestProxyServer_HealthWithProfiles(t *testing.T) {
	p, expectedCount := newTestProxyWithProfiles(t)
	if err := p.Launch(nil); err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	defer p.Shutdown(nil)

	resp, err := http.Get(p.ProxyURL() + "/health")
	if err != nil {
		t.Fatalf("GET /health error = %v", err)
	}
	defer resp.Body.Close()

	var health HealthStatus
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		t.Fatalf("json decode error: %v", err)
	}
	if health.Status != "ok" {
		t.Errorf("status = %q, want %q", health.Status, "ok")
	}
	if health.Models != expectedCount {
		t.Errorf("models = %d, want %d", health.Models, expectedCount)
	}
}

// TC-P2-05: Concurrent HTTP requests are handled safely.
func TestProxyServer_ConcurrentRequests(t *testing.T) {
	p := newTestProxy(t)
	if err := p.Launch(nil); err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	defer p.Shutdown(nil)

	const concurrency = 10
	var wg sync.WaitGroup
	errs := make(chan error, concurrency)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := http.Get(p.ProxyURL() + "/health")
			if err != nil {
				errs <- err
				return
			}
			defer resp.Body.Close()
			io.ReadAll(resp.Body)
			if resp.StatusCode != http.StatusOK {
				errs <- fmt.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
			}
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent request error: %v", err)
	}
}

// TC-P2-06: Error responses return JSON error body.
// Standalone ProxyServer (no driver) returns 400 for nil/empty body.
func TestProxyServer_StubErrorResponseBody(t *testing.T) {
	p := newTestProxy(t)
	if err := p.Launch(nil); err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	defer p.Shutdown(nil)

	endpoints := []string{
		"/v1/messages",
		"/v1/chat/completions",
	}

	for _, ep := range endpoints {
		t.Run(ep, func(t *testing.T) {
			resp, err := http.Post(p.ProxyURL()+ep, "application/json", nil)
			if err != nil {
				t.Fatalf("POST %s error = %v", ep, err)
			}
			defer resp.Body.Close()

			// Standalone proxy with nil body -> 400
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
			}

			ct := resp.Header.Get("Content-Type")
			if ct != "application/json" {
				t.Errorf("Content-Type = %q, want %q", ct, "application/json")
			}

			var body struct {
				Error struct {
					Type string `json:"type"`
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("json decode error: %v", err)
			}
			if body.Error.Type != "invalid_request_error" {
				t.Errorf("error.type = %q, want %q", body.Error.Type, "invalid_request_error")
			}
			if body.Error.Code != "invalid_json" {
				t.Errorf("error.code = %q, want %q", body.Error.Code, "invalid_json")
			}
		})
	}
}

// TC-P2-08: ProxyURL before Launch returns port=0 address.
func TestProxyServer_ProxyURL_BeforeLaunch(t *testing.T) {
	p := newTestProxy(t)
	url := p.ProxyURL()
	want := "http://localhost:0"
	if url != want {
		t.Errorf("ProxyURL() before Launch = %q, want %q", url, want)
	}
}
