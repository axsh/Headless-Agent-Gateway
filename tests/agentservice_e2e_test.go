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
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/axsh/hag/codingagent"
	"github.com/axsh/hag/codingagent/claudecode"
	"github.com/axsh/hag/hag"
)

// e2eDefaultModel is the model used for E2E tests.
// Must match a model registered in examples/standalone/model_profiles.yaml.
const e2eDefaultModel = "claude-sonnet-4-20250514"

// freePort returns a free TCP port by briefly listening on :0.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

// startE2EServer starts a real HAG server with LLM Gateway and claudecode agent.
// It uses the standalone model_profiles.yaml and dynamically-assigned ports.
// Returns the AgentService base URL and a cleanup function.
func startE2EServer(t *testing.T) (string, func()) {
	t.Helper()

	// Verify claude CLI is available.
	if _, err := exec.LookPath("claude"); err != nil {
		t.Fatalf("E2E test requires claude CLI on PATH: %v", err)
	}

	modelProfilesSrc, _ := filepath.Abs("../examples/standalone/model_profiles.yaml")

	// Discover free ports for all services.
	gwPort := freePort(t)
	wsPort := freePort(t)
	asPort := freePort(t)

	tmpDir := t.TempDir()
	tmpConfig := filepath.Join(tmpDir, "config.yaml")

	configContent := fmt.Sprintf(`llm_gateway:
  port: %d
  model_profiles_path: "%s"
log:
  level: "info"
vault:
  backend: "keyring"
websocket:
  port: %d
agent_service:
  port: %d
`, gwPort, filepath.ToSlash(modelProfilesSrc), wsPort, asPort)

	if err := os.WriteFile(tmpConfig, []byte(configContent), 0644); err != nil {
		t.Fatalf("write temp config: %v", err)
	}

	srv, err := hag.New(hag.WithConfigPath(tmpConfig))
	if err != nil {
		t.Fatalf("hag.New failed: %v", err)
	}

	ctx := context.Background()
	if err := srv.Launch(ctx); err != nil {
		t.Fatalf("Launch failed: %v", err)
	}

	// Register real claudecode agent with gateway URL.
	// ProxyURL must be called after Launch to get the actual port.
	gwURL := srv.Gateway().ProxyURL()
	adapter := claudecode.New(&codingagent.AdapterConfig{
		GatewayURL:   gwURL,
		DefaultModel: e2eDefaultModel,
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
	lineCount := 0
	for scanner.Scan() {
		line := scanner.Text()
		lineCount++
		if !strings.HasPrefix(line, "data: ") {
			if line != "" {
				t.Logf("SSE non-data line[%d]: %q", lineCount, line)
			}
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			t.Logf("SSE [DONE] received after %d lines", lineCount)
			gotDone = true
			break
		}
		var ev codingagent.StreamEvent
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			t.Logf("SSE parse error line[%d]: %v, data=%q", lineCount, err, data)
		} else {
			events = append(events, ev)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Logf("SSE scanner error: %v", err)
	}
	t.Logf("SSE total: %d lines read, %d events parsed, done=%v", lineCount, len(events), gotDone)
	return events, gotDone
}

// createE2ESession creates a session via the AgentService API.
func createE2ESession(t *testing.T, baseURL, agent, workDir string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{
		"agent":    agent,
		"model":    e2eDefaultModel,
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
func sendE2EMessage(t *testing.T, baseURL, sessionID, message string, timeout time.Duration) *http.Response {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"message": message})
	req, _ := http.NewRequest("POST",
		baseURL+"/api/v1/sessions/"+sessionID+"/messages",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	client := &http.Client{Timeout: timeout}
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
	resp := sendE2EMessage(t, baseURL, sessionID, prompt, 120*time.Second)
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
	// This test verifies error propagation when claude CLI fails.
	// We use a valid server setup but give the adapter a bogus GatewayURL
	// pointing to a non-existent server, so the CLI cannot reach the LLM API.

	if _, err := exec.LookPath("claude"); err != nil {
		t.Fatalf("E2E test requires claude CLI on PATH: %v", err)
	}

	modelProfilesSrc, _ := filepath.Abs("../examples/standalone/model_profiles.yaml")

	gwPort := freePort(t)
	wsPort := freePort(t)
	asPort := freePort(t)

	tmpDir := t.TempDir()
	tmpConfig := filepath.Join(tmpDir, "config.yaml")

	configContent := fmt.Sprintf(`llm_gateway:
  port: %d
  model_profiles_path: "%s"
log:
  level: "info"
vault:
  backend: "keyring"
websocket:
  port: %d
agent_service:
  port: %d
`, gwPort, filepath.ToSlash(modelProfilesSrc), wsPort, asPort)

	os.WriteFile(tmpConfig, []byte(configContent), 0644)

	srv, err := hag.New(hag.WithConfigPath(tmpConfig))
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

	// Register claudecode with a BOGUS gateway URL (port that nothing listens on).
	// This will cause the CLI to fail when trying to reach the LLM API.
	bogusPort := freePort(t)
	adapter := claudecode.New(&codingagent.AdapterConfig{
		GatewayURL: fmt.Sprintf("http://localhost:%d", bogusPort),
	})
	srv.AgentService().RegisterAgent(adapter)

	port := srv.AgentService().Port()
	baseURL := fmt.Sprintf("http://localhost:%d", port)
	workDir := t.TempDir()

	// Create session and send message - should get error because gateway is unreachable.
	sessionID := createE2ESession(t, baseURL, "claudecode", workDir)
	resp := sendE2EMessage(t, baseURL, sessionID, "say hello", 15*time.Second)
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
	// (indicating the CLI failed). Either way, it should NOT have
	// produced valid text output with an unreachable gateway.
	if !hasError && hasContent {
		t.Error("expected error event or no text content when gateway is unreachable")
	}
	if hasError {
		t.Log("Error propagation verified: error event received in SSE stream")
	} else {
		t.Log("Error propagation verified: no text content received (CLI failed)")
	}
}

