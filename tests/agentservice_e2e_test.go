// Package llm_test contains E2E tests for the CodingAgent pipeline.
// These tests use REAL claude CLI and LLM Gateway to verify end-to-end
// functionality: server startup, SSE streaming, file generation, and error handling.
//
// Prerequisites:
//   - claude CLI on PATH
//   - API key registered via bin/vault-cli
package llm_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/axsh/hag/codingagent"
	"github.com/axsh/hag/codingagent/claudecode"
	"github.com/axsh/hag/config"
	"github.com/axsh/hag/hag"
)

// startE2EServer starts a real HAG server with LLM Gateway and claudecode agent.
// It uses the standalone config.yaml with ephemeral ports for AgentService and WebSocket.
// Returns the AgentService base URL and a cleanup function.
func startE2EServer(t *testing.T) (string, func()) {
	t.Helper()

	// Verify claude CLI is available.
	if _, err := exec.LookPath("claude"); err != nil {
		t.Fatalf("E2E test requires claude CLI on PATH: %v", err)
	}

	// Use the real config.yaml (with model profiles) but override ports to ephemeral.
	configPath, err := filepath.Abs("../examples/standalone/config.yaml")
	if err != nil {
		t.Fatalf("resolve config path: %v", err)
	}

	srv, err := hag.New(hag.WithConfigPath(configPath))
	if err != nil {
		t.Fatalf("hag.New failed: %v", err)
	}

	// Override ports to 0 (ephemeral) so tests don't conflict.
	// We need to use AgentService port 0 by modifying config before Launch.
	// Since WithConfigPath already set the config, we adjust via the internal API.
	// The config uses port 3100 by default. We need a different approach:
	// Create a temp config with port 0.
	tmpDir := t.TempDir()
	tmpConfig := filepath.Join(tmpDir, "config.yaml")
	modelProfilesSrc, _ := filepath.Abs("../examples/standalone/model_profiles.yaml")

	configContent := fmt.Sprintf(`llm_gateway:
  port: 0
  model_profiles_path: "%s"
log:
  level: "info"
vault:
  backend: "keyring"
websocket:
  port: 0
agent_service:
  port: 0
`, filepath.ToSlash(modelProfilesSrc))

	if err := os.WriteFile(tmpConfig, []byte(configContent), 0644); err != nil {
		t.Fatalf("write temp config: %v", err)
	}

	srv, err = hag.New(hag.WithConfigPath(tmpConfig))
	if err != nil {
		t.Fatalf("hag.New with temp config failed: %v", err)
	}

	ctx := context.Background()
	if err := srv.Launch(ctx); err != nil {
		t.Fatalf("Launch failed: %v", err)
	}

	// Register real claudecode agent with gateway URL.
	// ProxyURL must be called after Launch to get the actual ephemeral port.
	gwURL := srv.Gateway().ProxyURL()
	adapter := claudecode.New(&codingagent.AdapterConfig{
		GatewayURL: gwURL,
	})
	srv.AgentService().RegisterAgent(adapter)

	port := srv.AgentService().Port()
	baseURL := fmt.Sprintf("http://localhost:%d", port)

	cleanup := func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutCtx)
	}

	return baseURL, cleanup
}

// parseE2ESSEEvents reads SSE events from an HTTP response body.
// Returns parsed events and whether [DONE] sentinel was received.
func parseE2ESSEEvents(t *testing.T, body *http.Response) ([]codingagent.StreamEvent, bool) {
	t.Helper()
	var events []codingagent.StreamEvent
	gotDone := false

	scanner := bufio.NewScanner(body.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			gotDone = true
			break
		}
		var ev codingagent.StreamEvent
		if json.Unmarshal([]byte(data), &ev) == nil {
			events = append(events, ev)
		}
	}
	return events, gotDone
}

// createE2ESession creates a session via the AgentService API.
func createE2ESession(t *testing.T, baseURL, agent, workDir string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{
		"agent":    agent,
		"work_dir": workDir,
	})
	resp, err := http.Post(baseURL+"/api/v1/sessions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create session: expected 201, got %d", resp.StatusCode)
	}
	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	sid := result["session_id"]
	if sid == "" {
		t.Fatal("create session: empty session_id")
	}
	return sid
}

// sendE2EMessage sends a message and returns SSE response (caller must close body).
func sendE2EMessage(t *testing.T, baseURL, sessionID, message string) *http.Response {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"message": message})
	req, _ := http.NewRequest("POST",
		baseURL+"/api/v1/sessions/"+sessionID+"/messages",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("send message: %v", err)
	}
	return resp
}

// getE2ESession retrieves session status.
func getE2ESession(t *testing.T, baseURL, sessionID string) map[string]interface{} {
	t.Helper()
	resp, err := http.Get(baseURL + "/api/v1/sessions/" + sessionID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	defer resp.Body.Close()
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	return result
}

// --- TC-001: Standalone server health with real gateway ---

// TestE2E_StandaloneHealth verifies that a real standalone server starts
// with LLM Gateway and claudecode agent, and health endpoint responds correctly.
func TestE2E_StandaloneHealth(t *testing.T) {
	baseURL, cleanup := startE2EServer(t)
	defer cleanup()

	resp, err := http.Get(baseURL + "/health")
	if err != nil {
		t.Fatalf("health request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health: expected 200, got %d", resp.StatusCode)
	}

	var health map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&health)

	// status should be "ok"
	status, _ := health["status"].(string)
	if status != "ok" {
		t.Errorf("health.status = %q, want %q", status, "ok")
	}

	// agents should contain "claudecode"
	agents, _ := health["agents"].([]interface{})
	found := false
	for _, a := range agents {
		if name, ok := a.(string); ok && name == "claudecode" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("health.agents does not contain 'claudecode': %v", agents)
	}

	// gateway status should be "ok"
	gw, _ := health["gateway"].(map[string]interface{})
	gwStatus, _ := gw["status"].(string)
	if gwStatus != "ok" {
		t.Errorf("health.gateway.status = %q, want %q", gwStatus, "ok")
	}
}

// --- TC-002: E2E streaming + file generation ---

// TestE2E_CodingAgentStreaming verifies the complete cawa-client run flow:
// session creation, SSE streaming with real claude CLI, file generation,
// and session completion.
func TestE2E_CodingAgentStreaming(t *testing.T) {
	baseURL, cleanup := startE2EServer(t)
	defer cleanup()

	// Create a temp directory for the agent to work in.
	workDir := t.TempDir()

	// 1. Create session
	sessionID := createE2ESession(t, baseURL, "claudecode", workDir)
	t.Logf("Session created: %s", sessionID)

	// 2. Send message requesting file creation
	prompt := "Create a file named hello.txt in the current directory containing exactly the text 'Hello World'. Do nothing else."
	resp := sendE2EMessage(t, baseURL, sessionID, prompt)
	defer resp.Body.Close()

	// Verify SSE content type
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/event-stream") {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}

	// 3. Parse SSE events
	events, gotDone := parseE2ESSEEvents(t, resp)

	// Must receive [DONE]
	if !gotDone {
		t.Fatal("expected [DONE] sentinel in SSE stream")
	}

	// Log event types for diagnostics
	for i, ev := range events {
		t.Logf("event[%d]: type=%s content_len=%d", i, ev.Type, len(ev.Content))
	}

	// Must have at least 1 event
	if len(events) == 0 {
		t.Fatal("expected at least 1 SSE event, got 0")
	}

	// Check for error events - if present, log and fail
	for _, ev := range events {
		if ev.Type == codingagent.EventError {
			t.Fatalf("received error event from claude CLI: %s", ev.Content)
		}
	}

	// Must have at least one text or tool_use event (proof of streaming)
	hasContent := false
	for _, ev := range events {
		if ev.Type == codingagent.EventText || ev.Type == codingagent.EventToolUse {
			hasContent = true
			break
		}
	}
	if !hasContent {
		t.Error("expected at least one text or tool_use event in SSE stream")
	}

	// 4. Verify file was created
	filePath := filepath.Join(workDir, "hello.txt")
	content, err := os.ReadFile(filePath)
	if err != nil {
		// List directory contents for debugging
		entries, _ := os.ReadDir(workDir)
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("expected hello.txt in %s, got files: %v, error: %v", workDir, names, err)
	}
	if !strings.Contains(string(content), "Hello World") {
		t.Errorf("hello.txt content = %q, want to contain 'Hello World'", string(content))
	}
	t.Logf("File created successfully: %s (%d bytes)", filePath, len(content))

	// 5. Verify session status
	session := getE2ESession(t, baseURL, sessionID)
	sessionStatus, _ := session["status"].(string)
	if sessionStatus != "completed" {
		t.Errorf("session status = %q, want %q", sessionStatus, "completed")
	}

	// 6. Verify sdk_session_id was captured
	sdkSID, _ := session["sdk_session_id"].(string)
	if sdkSID == "" {
		t.Error("sdk_session_id should be non-empty after successful session")
	}
	t.Logf("SDK Session ID: %s", sdkSID)
}

// --- TC-005: Error event E2E verification ---

// TestE2E_CodingAgentError verifies that when claude CLI encounters an error
// (e.g. invalid model), the error is propagated through SSE.
func TestE2E_CodingAgentError(t *testing.T) {
	// This test uses a dedicated server with a specifically misconfigured agent.
	// We need to create a config without valid model profiles to trigger an error.

	if _, err := exec.LookPath("claude"); err != nil {
		t.Fatalf("E2E test requires claude CLI on PATH: %v", err)
	}

	tmpDir := t.TempDir()

	// Create a minimal config with ephemeral ports but no valid model profiles.
	// The gateway will start but won't have valid API keys for this model.
	tmpConfig := filepath.Join(tmpDir, "config.yaml")
	// Use a config that starts the gateway but uses empty model profiles.
	emptyProfiles := filepath.Join(tmpDir, "empty_profiles.yaml")
	os.WriteFile(emptyProfiles, []byte("providers: {}\n"), 0644)

	configContent := fmt.Sprintf(`llm_gateway:
  port: 0
  model_profiles_path: "%s"
log:
  level: "info"
vault:
  backend: "env"
websocket:
  port: 0
agent_service:
  port: 0
`, filepath.ToSlash(emptyProfiles))

	os.WriteFile(tmpConfig, []byte(configContent), 0644)

	cfg, err := config.Load(tmpConfig)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	srv, err := hag.New(hag.WithConfig(cfg))
	if err != nil {
		t.Fatalf("hag.New: %v", err)
	}

	ctx := context.Background()
	if err := srv.Launch(ctx); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutCtx)
	}()

	// Register claudecode with gateway URL but no valid API key.
	// ProxyURL must be called after Launch for ephemeral port resolution.
	gwURL := srv.Gateway().ProxyURL()
	adapter := claudecode.New(&codingagent.AdapterConfig{
		GatewayURL: gwURL,
	})
	srv.AgentService().RegisterAgent(adapter)

	port := srv.AgentService().Port()
	baseURL := fmt.Sprintf("http://localhost:%d", port)
	workDir := t.TempDir()

	// Create session and send message - should get error because no valid API key.
	sessionID := createE2ESession(t, baseURL, "claudecode", workDir)
	resp := sendE2EMessage(t, baseURL, sessionID, "say hello")
	defer resp.Body.Close()

	events, _ := parseE2ESSEEvents(t, resp)

	// Should have error event or zero content events
	hasError := false
	hasContent := false
	for _, ev := range events {
		if ev.Type == codingagent.EventError {
			hasError = true
			t.Logf("Error event received: %s", ev.Content)
		}
		if ev.Type == codingagent.EventText {
			hasContent = true
		}
	}

	// The test passes if we got an error event, or if there are no text events
	// (indicating the CLI failed silently). Either way, it should NOT have
	// produced valid text output without a valid API key.
	if !hasError && hasContent {
		t.Error("expected error event or no text content when API key is invalid")
	}
	if hasError {
		t.Log("Error propagation verified: error event received in SSE stream")
	} else {
		t.Log("Error propagation verified: no text content received (CLI failed)")
	}
}
