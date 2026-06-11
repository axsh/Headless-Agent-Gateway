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

	"github.com/axsh/arctic-tern/config"
	"github.com/axsh/arctic-tern/tern"
	"github.com/axsh/arctic-tern/vault"
)

// testServer starts a tern.Server with KeyringVaultBackend and returns
// the base URL and a cleanup function.
func testServer(t *testing.T) (string, string, func()) {
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

	srv, err := tern.New(
		tern.WithConfig(cfg),
		tern.WithKeyringVault(),
	)
	if err != nil {
		t.Fatalf("tern.New: %v", err)
	}

	if err := srv.Launch(t.Context()); err != nil {
		t.Fatalf("Launch: %v", err)
	}

	gw := srv.Gateway()
	baseURL := gw.ProxyURL()
	token := srv.GatewayToken()

	return baseURL, token, func() {
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

	baseURL, token, cleanup := testServer(t)
	defer cleanup()

	body := map[string]any{
		"model":      "claude-sonnet-4-20250514",
		"max_tokens": 50,
		"messages": []map[string]string{
			{"role": "user", "content": "Say exactly: hello integration test"},
		},
	}

	resp := postWithAuth(t, token, nil, baseURL+"/v1/messages", body)
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

	baseURL, token, cleanup := testServer(t)
	defer cleanup()

	body := map[string]any{
		"model": "gpt-4.1-mini",
		"messages": []map[string]string{
			{"role": "user", "content": "Say exactly: hello integration test"},
		},
		"max_tokens": 50,
	}

	resp := postWithAuth(t, token, nil, baseURL+"/v1/chat/completions", body)
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

	baseURL, token, cleanup := testServer(t)
	defer cleanup()

	body := map[string]any{
		"model":      "claude-sonnet-4-20250514",
		"max_tokens": 50,
		"stream":     true,
		"messages": []map[string]string{
			{"role": "user", "content": "Say exactly: hello streaming test"},
		},
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp := postWithAuth(t, token, client, baseURL+"/v1/messages", body)
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

	baseURL, _, cleanup := testServer(t)

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

func TestCrossProvider_OpenAI_via_AnthropicEndpoint_NonStream(t *testing.T) {
	checkKeyringAvailable(t, "openai")

	baseURL, token, cleanup := testServer(t)
	defer cleanup()

	// Send request to /v1/messages (Anthropic endpoint) with OpenAI model.
	// The LLMGP should convert Anthropic -> OpenAI, forward to api.openai.com,
	// and convert the response back to Anthropic format.
	body := map[string]any{
		"model":      "gpt-4.1-mini",
		"max_tokens": 50,
		"messages": []map[string]string{
			{"role": "user", "content": "Say exactly: cross-provider test ok"},
		},
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp := postWithAuth(t, token, client, baseURL+"/v1/messages", body)
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(respBody))
	}

	// Response should be in Anthropic format (converted from OpenAI)
	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("JSON decode failed: %v\nbody: %s", err, string(respBody))
	}

	// Must have Anthropic-style "content" array
	content, ok := result["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("expected non-empty content array (Anthropic format), got: %s", string(respBody))
	}

	// Must have Anthropic-style "type": "message"
	if result["type"] != "message" {
		t.Errorf("expected type=message, got %v", result["type"])
	}

	// Must have stop_reason (Anthropic field, not finish_reason)
	if _, ok := result["stop_reason"]; !ok {
		t.Errorf("expected stop_reason field in response, got: %s", string(respBody))
	}

	t.Logf("Cross-provider (non-stream) response: %s", string(respBody))
}

func TestCrossProvider_OpenAI_via_AnthropicEndpoint_Stream(t *testing.T) {
	checkKeyringAvailable(t, "openai")

	baseURL, token, cleanup := testServer(t)
	defer cleanup()

	// Send streaming request to /v1/messages with OpenAI model.
	// The LLMGP should convert the request, forward to OpenAI with stream:true,
	// and convert the OpenAI SSE stream to Anthropic SSE stream format.
	body := map[string]any{
		"model":      "gpt-4.1-mini",
		"max_tokens": 50,
		"stream":     true,
		"messages": []map[string]string{
			{"role": "user", "content": "Say exactly: cross-provider streaming test ok"},
		},
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp := postWithAuth(t, token, client, baseURL+"/v1/messages", body)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(respBody))
	}

	// Read the full SSE stream
	respBody, _ := io.ReadAll(resp.Body)
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
	// Must contain text_delta (Anthropic format, not OpenAI delta.content)
	if !strings.Contains(events, "text_delta") {
		t.Error("missing text_delta in stream events")
	}

	t.Logf("Cross-provider (stream) response length: %d bytes", len(events))
}

func TestResponsesAPI_Codex_via_AnthropicEndpoint_NonStream(t *testing.T) {
	checkKeyringAvailable(t, "openai")

	baseURL, token, cleanup := testServer(t)
	defer cleanup()

	// Send request to /v1/messages (Anthropic endpoint) with Codex model.
	// The LLMGP should convert Anthropic -> Responses API, forward to api.openai.com/v1/responses,
	// and convert the response back to Anthropic format.
	body := map[string]any{
		"model":      "gpt-5.3-codex",
		"max_tokens": 128,
		"messages": []map[string]string{
			{"role": "user", "content": "Say exactly: responses api e2e test ok"},
		},
	}

	client := &http.Client{Timeout: 60 * time.Second}
	resp := postWithAuth(t, token, client, baseURL+"/v1/messages", body)
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(respBody))
	}

	// Response must be in Anthropic format (converted from Responses API).
	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("JSON decode failed: %v\nbody: %s", err, string(respBody))
	}

	// Must have Anthropic-style "type": "message"
	if result["type"] != "message" {
		t.Errorf("expected type=message, got %v", result["type"])
	}

	// Must have "role": "assistant"
	if result["role"] != "assistant" {
		t.Errorf("expected role=assistant, got %v", result["role"])
	}

	// Must have Anthropic-style "content" array with non-empty text
	content, ok := result["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("expected non-empty content array (Anthropic format), got: %s", string(respBody))
	}
	block, ok := content[0].(map[string]any)
	if !ok {
		t.Fatalf("expected content[0] to be object, got: %v", content[0])
	}
	if block["type"] != "text" {
		t.Errorf("expected content[0].type=text, got %v", block["type"])
	}
	text, _ := block["text"].(string)
	if text == "" {
		t.Errorf("expected non-empty content[0].text, got empty")
	}

	// Must have stop_reason (Anthropic field, not finish_reason)
	if _, ok := result["stop_reason"]; !ok {
		t.Errorf("expected stop_reason field in response, got: %s", string(respBody))
	}

	// Must have usage with input_tokens > 0
	usage, ok := result["usage"].(map[string]any)
	if !ok {
		t.Errorf("expected usage object in response, got: %s", string(respBody))
	} else {
		inputTokens, _ := usage["input_tokens"].(float64)
		if inputTokens <= 0 {
			t.Errorf("expected input_tokens > 0, got %v", inputTokens)
		}
	}

	t.Logf("Codex Responses API (non-stream) response: %s", string(respBody))
}

func TestResponsesAPI_Codex_via_AnthropicEndpoint_Stream(t *testing.T) {
	checkKeyringAvailable(t, "openai")

	baseURL, token, cleanup := testServer(t)
	defer cleanup()

	// Send streaming request to /v1/messages with Codex model.
	// The LLMGP should convert the request, forward to OpenAI /v1/responses with stream:true,
	// and convert the Responses API SSE stream to Anthropic SSE stream format.
	body := map[string]any{
		"model":      "gpt-5.3-codex",
		"max_tokens": 128,
		"stream":     true,
		"messages": []map[string]string{
			{"role": "user", "content": "Say exactly: responses api streaming test ok"},
		},
	}

	client := &http.Client{Timeout: 60 * time.Second}
	resp := postWithAuth(t, token, client, baseURL+"/v1/messages", body)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(respBody))
	}

	// Read the full SSE stream.
	respBody, _ := io.ReadAll(resp.Body)
	events := string(respBody)

	// Verify Anthropic SSE event format (converted from Responses API events).
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
	// Must contain text_delta (Anthropic format, not Responses API delta format).
	if !strings.Contains(events, "text_delta") {
		t.Error("missing text_delta in stream events")
	}

	// Verify event ordering: message_start before text_delta before message_stop.
	startIdx := strings.Index(events, "message_start")
	deltaIdx := strings.Index(events, "text_delta")
	stopIdx := strings.Index(events, "message_stop")
	if startIdx >= deltaIdx {
		t.Errorf("message_start (pos %d) should come before text_delta (pos %d)", startIdx, deltaIdx)
	}
	if deltaIdx >= stopIdx {
		t.Errorf("text_delta (pos %d) should come before message_stop (pos %d)", deltaIdx, stopIdx)
	}

	t.Logf("Codex Responses API (stream) response length: %d bytes", len(events))
}

func TestMain(m *testing.M) {
	// Integration tests use the real OS Keyring.
	// Tests will skip if the required API keys are not registered.
	fmt.Println("LLM Integration Tests")
	fmt.Println("Prerequisites: API keys registered via bin/vault-cli")
	os.Exit(m.Run())
}

func postWithAuth(t *testing.T, token string, client *http.Client, url string, body any) *http.Response {
	t.Helper()
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req, err := http.NewRequest("POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gateway-Token", token)
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	return resp
}
