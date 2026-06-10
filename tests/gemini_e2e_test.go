package llm_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/axsh/hag/config"
	"github.com/axsh/hag/hag"
	"github.com/axsh/hag/llmgateway"
	"github.com/axsh/hag/vault"
)

// getFreePort listens on an ephemeral port and returns it.
func getFreePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to get free port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}

// setupMockGeminiServer launches a local HTTP server simulating Google Gemini API response.
func setupMockGeminiServer(t *testing.T) (*httptest.Server, func()) {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/v1beta/models/gemini-3.5-flash:generateContent", func(w http.ResponseWriter, r *http.Request) {
		// Verify Google API Key is passed via header or query param
		key := r.Header.Get("x-goog-api-key")
		if key == "" {
			key = r.URL.Query().Get("key")
		}
		if key != "AIzaSy-dummy-gemini-key-123" {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error": "invalid api key"}`))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"candidates": [{
				"content": {
					"role": "model",
					"parts": [{"text": "Hello from mock Gemini E2E!"}]
				},
				"finishReason": "STOP"
			}],
			"usageMetadata": {
				"promptTokenCount": 12,
				"candidatesTokenCount": 6,
				"totalTokenCount": 18
			}
		}`))
	})

	mux.HandleFunc("/v1beta/models/gemini-3.5-flash:streamGenerateContent", func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("x-goog-api-key")
		if key == "" {
			key = r.URL.Query().Get("key")
		}
		if key != "AIzaSy-dummy-gemini-key-123" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		if r.URL.Query().Get("alt") != "sse" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		events := []string{
			`data: {"candidates":[{"content":{"parts":[{"text":"Hi from"}]}}]}`,
			`data: {"candidates":[{"content":{"parts":[{"text":" mock streaming!"}]}}]}`,
			`data: {"candidates":[{"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5,"totalTokenCount":15}}`,
		}

		flusher, _ := w.(http.Flusher)
		for _, ev := range events {
			fmt.Fprintf(w, "%s\n\n", ev)
			if flusher != nil {
				flusher.Flush()
			}
			time.Sleep(10 * time.Millisecond)
		}
	})

	srv := httptest.NewServer(mux)

	// Save original base URL mapping and override for Gemini
	origMapping := llmgateway.GetProviderBaseURLs()
	llmgateway.SetProviderBaseURL("google", srv.URL)

	cleanup := func() {
		srv.Close()
		// Restore original URL mappings
		for k, v := range origMapping {
			llmgateway.SetProviderBaseURL(k, v)
		}
	}

	return srv, cleanup
}

func TestGeminiE2E_NonStream(t *testing.T) {
	_, serverCleanup := setupMockGeminiServer(t)
	defer serverCleanup()

	// Setup mock vault store
	vs := vault.NewEnvVaultBackend()
	_ = vs.Set("providers/google/default", "AIzaSy-dummy-gemini-key-123")
	defer func() { _ = vs.Delete("providers/google/default") }()

	profilesPath, err := filepath.Abs(filepath.Join("testdata", "model_profiles.yaml"))
	if err != nil {
		t.Fatalf("abs path: %v", err)
	}

	cfg := &config.AppConfig{
		LLMGateway: config.LLMGatewayConfig{
			Port:              0, // auto-assign
			ModelProfilesPath: profilesPath,
		},
		AgentService: config.AgentServiceConfig{
			Port: getFreePort(t), // dynamic free port
		},
		WebSocket: config.WebSocketConfig{
			Port: getFreePort(t), // dynamic free port
		},
	}

	srv, err := hag.New(
		hag.WithConfig(cfg),
		hag.WithVaultStore(vs),
	)
	if err != nil {
		t.Fatalf("hag.New: %v", err)
	}

	if err := srv.Launch(t.Context()); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer srv.Shutdown(t.Context())

	baseURL := srv.Gateway().ProxyURL()

	body := map[string]any{
		"model":      "gemini-3.5-flash",
		"max_tokens": 100,
		"messages": []map[string]string{
			{"role": "user", "content": "Hello Gemini"},
		},
	}
	bodyBytes, _ := json.Marshal(body)

	resp, err := http.Post(baseURL+"/v1/messages", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("POST to gateway failed: %v", err)
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

	// Response must be in Anthropic format
	if result["type"] != "message" {
		t.Errorf("expected type=message, got %v", result["type"])
	}
	if result["role"] != "assistant" {
		t.Errorf("expected role=assistant, got %v", result["role"])
	}

	content, ok := result["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("expected non-empty content array, got: %s", string(respBody))
	}
	block := content[0].(map[string]any)
	if block["type"] != "text" || block["text"] != "Hello from mock Gemini E2E!" {
		t.Errorf("unexpected content block: %v", block)
	}

	if result["stop_reason"] != "end_turn" {
		t.Errorf("expected stop_reason=end_turn, got %v", result["stop_reason"])
	}
}

func TestGeminiE2E_Stream(t *testing.T) {
	_, serverCleanup := setupMockGeminiServer(t)
	defer serverCleanup()

	vs := vault.NewEnvVaultBackend()
	_ = vs.Set("providers/google/default", "AIzaSy-dummy-gemini-key-123")
	defer func() { _ = vs.Delete("providers/google/default") }()

	profilesPath, err := filepath.Abs(filepath.Join("testdata", "model_profiles.yaml"))
	if err != nil {
		t.Fatalf("abs path: %v", err)
	}

	cfg := &config.AppConfig{
		LLMGateway: config.LLMGatewayConfig{
			Port:              0, // auto-assign
			ModelProfilesPath: profilesPath,
		},
		AgentService: config.AgentServiceConfig{
			Port: getFreePort(t), // dynamic free port
		},
		WebSocket: config.WebSocketConfig{
			Port: getFreePort(t), // dynamic free port
		},
	}

	srv, err := hag.New(
		hag.WithConfig(cfg),
		hag.WithVaultStore(vs),
	)
	if err != nil {
		t.Fatalf("hag.New: %v", err)
	}

	if err := srv.Launch(t.Context()); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer srv.Shutdown(t.Context())

	baseURL := srv.Gateway().ProxyURL()

	body := map[string]any{
		"model":      "gemini-3.5-flash",
		"max_tokens": 100,
		"stream":     true,
		"messages": []map[string]string{
			{"role": "user", "content": "Hello Gemini"},
		},
	}
	bodyBytes, _ := json.Marshal(body)

	resp, err := http.Post(baseURL+"/v1/messages", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("POST to gateway failed: %v", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(respBody))
	}

	events := string(respBody)

	// Verify Anthropic SSE event format
	if !strings.Contains(events, "event: message_start") {
		t.Error("missing message_start event")
	}
	if !strings.Contains(events, "event: content_block_start") {
		t.Error("missing content_block_start event")
	}
	if !strings.Contains(events, "event: content_block_delta") {
		t.Error("missing content_block_delta event")
	}
	if !strings.Contains(events, "event: message_stop") {
		t.Error("missing message_stop event")
	}

	if !strings.Contains(events, `"text":"Hi from"`) {
		t.Error("missing 'Hi from'")
	}
	if !strings.Contains(events, `"text":" mock streaming!"`) {
		t.Error("missing ' mock streaming!'")
	}
}
