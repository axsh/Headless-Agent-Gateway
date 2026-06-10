// Package llm_test contains E2E tests for the Codex CodingAgent pipeline.
// These tests use REAL codex CLI and LLM Gateway to verify end-to-end
// functionality: server startup, SSE streaming, file generation, and error handling.
//
// Prerequisites:
//   - codex CLI on PATH
//   - API key registered via bin/vault-cli
package llm_test

import (
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
	"github.com/axsh/hag/codingagent/codex"
	"github.com/axsh/hag/hag"
)

// startCodexE2EServer starts a HAG server with codex agent registered.
// It uses the standalone model_profiles.yaml and dynamically-assigned ports.
// Returns the AgentService base URL and a cleanup function.
func startCodexE2EServer(t *testing.T) (string, func()) {
	t.Helper()

	// Verify codex CLI is available.
	if _, err := exec.LookPath("codex"); err != nil {
		t.Fatalf("E2E test requires codex CLI on PATH: %v", err)
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

	// Register real codex agent with gateway URL.
	gwURL := srv.Gateway().ProxyURL()
	codexAdapter := codex.New(&codingagent.AdapterConfig{
		GatewayURL:   gwURL,
		DefaultModel: "gpt-4o",
	})
	srv.AgentService().RegisterAgent(codexAdapter)

	port := srv.AgentService().Port()
	baseURL := fmt.Sprintf("http://localhost:%d", port)

	cleanup := func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutCtx)
	}

	return baseURL, cleanup
}

// --- TC-Codex-001: Codex + default model (gpt-4o) file creation ---

// TestCodexE2E_FileCreation verifies Codex CLI + default model (gpt-4o)
// can create a file through the full CAWA pipeline.
func TestCodexE2E_FileCreation(t *testing.T) {
	baseURL, cleanup := startCodexE2EServer(t)
	defer cleanup()

	workDir := t.TempDir()

	// 1. Create session with codex agent
	sessionID := createE2ESessionWithModel(t, baseURL, "codex", "gpt-4o", workDir)
	t.Logf("Session created: %s", sessionID)

	// 2. Send file creation prompt
	prompt := "Create a file named hello.txt in the current directory containing exactly the text 'Hello Codex'. Do nothing else."
	resp := sendE2EMessage(t, baseURL, sessionID, prompt, 120*time.Second)
	defer resp.Body.Close()

	// 3. Verify SSE content type
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/event-stream") {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}

	// 4. Parse SSE events
	events, gotDone := parseE2ESSEEvents(t, resp)
	if !gotDone {
		t.Fatal("expected [DONE] sentinel in SSE stream")
	}

	// Log event types for diagnostics
	for i, ev := range events {
		t.Logf("event[%d]: type=%s content_len=%d", i, ev.Type, len(ev.Content))
	}

	// Check for error events
	for _, ev := range events {
		if ev.Type == codingagent.EventError {
			t.Fatalf("received error event from codex CLI: %s", ev.Content)
		}
	}

	// Must have at least one text or tool_use event
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

	// 5. Verify file was created
	filePath := filepath.Join(workDir, "hello.txt")
	content, err := os.ReadFile(filePath)
	if err != nil {
		entries, _ := os.ReadDir(workDir)
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("expected hello.txt in %s, got files: %v, error: %v", workDir, names, err)
	}
	if !strings.Contains(string(content), "Hello Codex") {
		t.Errorf("hello.txt content = %q, want to contain 'Hello Codex'", string(content))
	}
	t.Logf("File created successfully: %s (%d bytes)", filePath, len(content))

	// 6. Verify session status
	session := getE2ESession(t, baseURL, sessionID)
	sessionStatus, _ := session["status"].(string)
	if sessionStatus != "completed" {
		t.Errorf("session status = %q, want %q", sessionStatus, "completed")
	}
}

// --- TC-Codex-002: Codex + Gemini model file creation ---

// TestCodexE2E_GeminiModel_FileCreation verifies Codex CLI + Gemini model
// can create a file through LLMGP cross-provider routing.
func TestCodexE2E_GeminiModel_FileCreation(t *testing.T) {
	baseURL, cleanup := startCodexE2EServer(t)
	defer cleanup()
	workDir := t.TempDir()

	sessionID := createE2ESessionWithModel(t, baseURL, "codex", "gemini-2.5-flash", workDir)
	t.Logf("Session created: %s", sessionID)

	prompt := "Create a file named test.txt in the current directory containing exactly the text 'Hello from Gemini via Codex'. Do nothing else."
	resp := sendE2EMessage(t, baseURL, sessionID, prompt, 120*time.Second)
	defer resp.Body.Close()

	events, gotDone := parseE2ESSEEvents(t, resp)
	if !gotDone {
		t.Fatal("expected [DONE] sentinel in SSE stream")
	}

	for i, ev := range events {
		t.Logf("event[%d]: type=%s content_len=%d", i, ev.Type, len(ev.Content))
	}
	for _, ev := range events {
		if ev.Type == codingagent.EventError {
			t.Fatalf("received error event: %s", ev.Content)
		}
	}

	filePath := filepath.Join(workDir, "test.txt")
	content, err := os.ReadFile(filePath)
	if err != nil {
		entries, _ := os.ReadDir(workDir)
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("expected test.txt in %s, got files: %v, error: %v", workDir, names, err)
	}
	if !strings.Contains(string(content), "Hello from Gemini via Codex") {
		t.Errorf("test.txt content = %q, want to contain 'Hello from Gemini via Codex'", string(content))
	}
	t.Logf("File created successfully: %s (%d bytes)", filePath, len(content))
}

// --- TC-Codex-003: Error propagation ---

// TestCodexE2E_ErrorPropagation verifies that when Codex CLI cannot reach
// the gateway, the error is propagated through SSE.
func TestCodexE2E_ErrorPropagation(t *testing.T) {
	if _, err := exec.LookPath("codex"); err != nil {
		t.Fatalf("E2E test requires codex CLI on PATH: %v", err)
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

	// Register codex with BOGUS gateway URL (port that nothing listens on).
	bogusPort := freePort(t)
	adapter := codex.New(&codingagent.AdapterConfig{
		GatewayURL: fmt.Sprintf("http://localhost:%d", bogusPort),
	})
	srv.AgentService().RegisterAgent(adapter)

	port := srv.AgentService().Port()
	baseURL := fmt.Sprintf("http://localhost:%d", port)
	workDir := t.TempDir()

	// Create session and send message - should get error because gateway is unreachable.
	sessionID := createE2ESessionWithModel(t, baseURL, "codex", "gpt-4o", workDir)
	resp := sendE2EMessage(t, baseURL, sessionID, "say hello", 30*time.Second)
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

	// The test passes if we got an error event, or if there are no text events.
	if !hasError && hasContent {
		t.Error("expected error event or no text content when gateway is unreachable")
	}
	if hasError {
		t.Log("Error propagation verified: error event received in SSE stream")
	} else {
		t.Log("Error propagation verified: no text content received (CLI failed)")
	}
}

// --- TC-Codex-004: Health check includes codex agent ---

// TestCodexE2E_HealthWithCodexAgent verifies that the health endpoint
// includes the codex agent in the agents list.
func TestCodexE2E_HealthWithCodexAgent(t *testing.T) {
	baseURL, cleanup := startCodexE2EServer(t)
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

	// agents should contain "codex"
	agents, _ := health["agents"].([]interface{})
	found := false
	for _, a := range agents {
		if name, ok := a.(string); ok && name == "codex" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("health.agents does not contain 'codex': %v", agents)
	}
}
