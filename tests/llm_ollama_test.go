// Package llm_test contains Ollama integration tests for the LLM gateway.
// These tests require a running Ollama server at localhost:11434.
//
// Prerequisites:
//
//	ollama serve
//	ollama pull llama3.2:1b
package llm_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// checkOllamaAvailable verifies that the Ollama server is running.
func checkOllamaAvailable(t *testing.T) {
	t.Helper()
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://localhost:11434")
	if err != nil {
		t.Fatalf("Ollama server not available at localhost:11434: %v (run: ollama serve)", err)
	}
	resp.Body.Close()
}

func TestOllama_NonStream(t *testing.T) {
	checkOllamaAvailable(t)

	baseURL, cleanup := testServer(t)
	defer cleanup()

	body := map[string]any{
		"model":      "llama3.2:1b",
		"max_tokens": 50,
		"messages": []map[string]string{
			{"role": "user", "content": "Say exactly: hello ollama test"},
		},
	}
	bodyBytes, _ := json.Marshal(body)

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Post(baseURL+"/v1/messages", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("POST /v1/messages (Ollama) failed: %v", err)
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

	// Anthropic format verification
	if result["type"] != "message" {
		t.Errorf("expected type=message, got %v", result["type"])
	}
	content, ok := result["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("expected non-empty content array, got: %s", string(respBody))
	}

	t.Logf("Ollama (non-stream) response: %s", string(respBody))
}

func TestOllama_Stream(t *testing.T) {
	checkOllamaAvailable(t)

	baseURL, cleanup := testServer(t)
	defer cleanup()

	body := map[string]any{
		"model":      "llama3.2:1b",
		"max_tokens": 50,
		"stream":     true,
		"messages": []map[string]string{
			{"role": "user", "content": "Say exactly: hello ollama streaming"},
		},
	}
	bodyBytes, _ := json.Marshal(body)

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Post(baseURL+"/v1/messages", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("POST /v1/messages (Ollama stream) failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(respBody))
	}

	respBody, _ := io.ReadAll(resp.Body)
	events := string(respBody)

	if !strings.Contains(events, "event: message_start") {
		t.Error("missing message_start event")
	}
	if !strings.Contains(events, "event: content_block_delta") {
		t.Error("missing content_block_delta event")
	}
	if !strings.Contains(events, "event: message_stop") {
		t.Error("missing message_stop event")
	}

	t.Logf("Ollama (stream) response length: %d bytes", len(events))
}
