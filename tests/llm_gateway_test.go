// Package llm_test contains integration tests for the LLM gateway.
// These tests require real API keys registered in the OS Keyring via vault-cli.
//
// Prerequisites:
//
//	bin/vault-cli set --provider anthropic --stdin
//	bin/vault-cli set --provider openai --stdin
//	bin/vault-cli set --provider google --stdin
package llm_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/axsh/hag/config"
	"github.com/axsh/hag/hag"
	"github.com/axsh/hag/vault"
)

// testServer starts a hag.Server with KeyringVaultBackend and returns
// the base URL and a cleanup function.
func testServer(t *testing.T) (string, func()) {
	t.Helper()

	profilesPath, err := filepath.Abs(filepath.Join("testdata", "model_profiles.yaml"))
	if err != nil {
		t.Fatalf("abs path: %v", err)
	}

	cfg := &config.AppConfig{
		LLMGateway: config.LLMGatewayConfig{
			Port:              0, // auto-assign
			ModelProfilesPath: profilesPath,
		},
	}

	srv, err := hag.New(
		hag.WithConfig(cfg),
		hag.WithKeyringVault(),
	)
	if err != nil {
		t.Fatalf("hag.New: %v", err)
	}

	if err := srv.Launch(t.Context()); err != nil {
		t.Fatalf("Launch: %v", err)
	}

	gw := srv.Gateway()
	baseURL := gw.ProxyURL()

	return baseURL, func() {
		_ = srv.Shutdown(t.Context())
	}
}

// checkKeyringAvailable verifies that the required API key is registered.
func checkKeyringAvailable(t *testing.T, provider string) {
	t.Helper()
	kb := vault.NewKeyringVaultBackend()
	_, err := kb.Resolve("vault://providers/" + provider + "/default")
	if err != nil {
		t.Skipf("Skipping: %s API key not registered in OS Keyring (run: bin/vault-cli set --provider %s --stdin)", provider, provider)
	}
}

func TestAnthropicMessages_NonStream(t *testing.T) {
	checkKeyringAvailable(t, "anthropic")

	baseURL, cleanup := testServer(t)
	defer cleanup()

	body := map[string]any{
		"model":      "claude-sonnet-4-20250514",
		"max_tokens": 50,
		"messages": []map[string]string{
			{"role": "user", "content": "Say exactly: hello integration test"},
		},
	}
	bodyBytes, _ := json.Marshal(body)

	resp, err := http.Post(baseURL+"/v1/messages", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("POST /v1/messages failed: %v", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(respBody))
	}

	// Verify response has content
	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("JSON decode failed: %v\nbody: %s", err, string(respBody))
	}

	// Anthropic response should have "content" array
	content, ok := result["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("expected non-empty content array, got: %s", string(respBody))
	}

	t.Logf("Anthropic response: %s", string(respBody))
}

func TestOpenAIChatCompletions_NonStream(t *testing.T) {
	checkKeyringAvailable(t, "openai")

	baseURL, cleanup := testServer(t)
	defer cleanup()

	body := map[string]any{
		"model": "gpt-4.1-mini",
		"messages": []map[string]string{
			{"role": "user", "content": "Say exactly: hello integration test"},
		},
		"max_tokens": 50,
	}
	bodyBytes, _ := json.Marshal(body)

	resp, err := http.Post(baseURL+"/v1/chat/completions", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("POST /v1/chat/completions failed: %v", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(respBody))
	}

	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("JSON decode failed: %v\nbody: %s", err, string(respBody))
	}

	// OpenAI response should have "choices" array
	choices, ok := result["choices"].([]any)
	if !ok || len(choices) == 0 {
		t.Fatalf("expected non-empty choices array, got: %s", string(respBody))
	}

	t.Logf("OpenAI response: %s", string(respBody))
}

func TestAnthropicMessages_Stream(t *testing.T) {
	checkKeyringAvailable(t, "anthropic")

	baseURL, cleanup := testServer(t)
	defer cleanup()

	body := map[string]any{
		"model":      "claude-sonnet-4-20250514",
		"max_tokens": 50,
		"stream":     true,
		"messages": []map[string]string{
			{"role": "user", "content": "Say exactly: hello streaming test"},
		},
	}
	bodyBytes, _ := json.Marshal(body)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Post(baseURL+"/v1/messages", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("POST /v1/messages (stream) failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(respBody))
	}

	// Read SSE events
	respBody, _ := io.ReadAll(resp.Body)
	events := string(respBody)

	if !strings.Contains(events, "event:") && !strings.Contains(events, "data:") {
		t.Fatalf("expected SSE events, got: %s", events)
	}

	t.Logf("Anthropic stream response length: %d bytes", len(events))
}

func TestServerLifecycle(t *testing.T) {
	checkKeyringAvailable(t, "anthropic")

	baseURL, cleanup := testServer(t)

	// Verify server is running
	resp, err := http.Get(baseURL + "/health")
	if err != nil {
		// Health endpoint may not exist; just verify connectivity
		t.Logf("Health check: %v (expected if no health endpoint)", err)
	} else {
		resp.Body.Close()
		t.Logf("Health check status: %d", resp.StatusCode)
	}

	// Shutdown
	cleanup()

	// Wait briefly for shutdown to propagate
	time.Sleep(100 * time.Millisecond)

	// Verify server is stopped
	_, err = http.Get(baseURL + "/health")
	if err == nil {
		t.Error("expected connection error after shutdown")
	}
}

func TestMain(m *testing.M) {
	// Integration tests use the real OS Keyring.
	// Tests will skip if the required API keys are not registered.
	fmt.Println("LLM Integration Tests")
	fmt.Println("Prerequisites: API keys registered via bin/vault-cli")
	os.Exit(m.Run())
}
