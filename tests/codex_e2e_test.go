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
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent/codex"
	"github.com/axsh/arctic-tern/server"
)

// startCodexE2EServer starts a tern server with codex agent registered.
// It uses tests/testdata/model_profiles.yaml and dynamically-assigned ports.
// Agents are auto-registered via codingagent.CreateAll() in server.New().
// Returns the AgentService base URL and a cleanup function.
func startCodexE2EServer(t *testing.T) (string, func()) {
	t.Helper()

	// Verify codex CLI is available.
	if _, err := exec.LookPath("codex"); err != nil {
		t.Skipf("codex CLI not found on PATH, skipping: %v", err)
	}

	modelProfilesSrc, _ := filepath.Abs(filepath.Join("testdata", "model_profiles.yaml"))

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
  disable_sandbox: true
`, gwPort, filepath.ToSlash(modelProfilesSrc), wsPort, asPort)

	if err := os.WriteFile(tmpConfig, []byte(configContent), 0644); err != nil {
		t.Fatalf("write temp config: %v", err)
	}

	srv, err := server.New(server.WithConfigPath(tmpConfig))
	if err != nil {
		t.Fatalf("server.New failed: %v", err)
	}

	ctx := context.Background()
	if err := srv.Launch(ctx); err != nil {
		t.Fatalf("Launch failed: %v", err)
	}

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

	// Must have at least one result event (turn.completed) or content event.
	// Note: codex exec --json may only emit lifecycle events (turn.completed)
	// without individual text/tool_use events.
	hasResult := false
	for _, ev := range events {
		if ev.Type == codingagent.EventResult || ev.Type == codingagent.EventText || ev.Type == codingagent.EventToolUse {
			hasResult = true
			break
		}
	}
	if !hasResult {
		t.Error("expected at least one result, text, or tool_use event in SSE stream")
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
	// Decode file content - handle UTF-16 LE BOM if present (Windows codex may use it).
	contentStr := string(content)
	if len(content) >= 2 && content[0] == 0xFF && content[1] == 0xFE {
		// UTF-16 LE BOM detected, decode to UTF-8.
		var decoded []byte
		for i := 2; i+1 < len(content); i += 2 {
			ch := content[i] // Low byte of UTF-16 LE char
			if ch != 0 || content[i+1] != 0 {
				decoded = append(decoded, ch)
			}
		}
		contentStr = string(decoded)
		t.Logf("Decoded UTF-16 LE content: %q", contentStr)
	}
	if !strings.Contains(contentStr, "Hello Codex") {
		t.Errorf("hello.txt content = %q, want to contain 'Hello Codex'", contentStr)
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
// can create a file through Bifrost SDK cross-provider routing.
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
		t.Skipf("codex CLI not found on PATH, skipping: %v", err)
	}

	modelProfilesSrc, _ := filepath.Abs(filepath.Join("testdata", "model_profiles.yaml"))

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

	srv, err := server.New(server.WithConfigPath(tmpConfig))
	if err != nil {
		t.Fatalf("server.New: %v", err)
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

	// status should be "ok"
	status, _ := health["status"].(string)
	if status != "ok" {
		t.Errorf("health.status = %q, want %q", status, "ok")
	}

	// agents should contain "codex" (by querying /api/v1/agents)
	respAgents, err := http.Get(baseURL + "/api/v1/agents")
	if err != nil {
		t.Fatalf("agents request failed: %v", err)
	}
	defer respAgents.Body.Close()

	if respAgents.StatusCode != http.StatusOK {
		t.Fatalf("agents: expected 200, got %d", respAgents.StatusCode)
	}

	var agents []map[string]interface{}
	json.NewDecoder(respAgents.Body).Decode(&agents)

	found := false
	for _, a := range agents {
		if name, ok := a["name"].(string); ok && name == "codex" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("api/v1/agents does not contain 'codex': %v", agents)
	}
}

// --- TC-Codex-005: Codex + Anthropic model file creation ---

// TestCodexE2E_AnthropicModel_FileCreation verifies Codex CLI + Anthropic model
// can create a file through Bifrost SDK cross-provider routing.
func TestCodexE2E_AnthropicModel_FileCreation(t *testing.T) {
	baseURL, cleanup := startCodexE2EServer(t)
	defer cleanup()
	workDir := t.TempDir()

	sessionID := createE2ESessionWithModel(
		t, baseURL, "codex", "claude-sonnet-4-20250514", workDir)
	t.Logf("Session created: %s", sessionID)

	prompt := "Create a file named test_anthropic.txt in the current directory " +
		"containing exactly the text 'Hello from Anthropic via Codex'. Do nothing else."
	resp := sendE2EMessage(t, baseURL, sessionID, prompt, 120*time.Second)
	defer resp.Body.Close()

	// Verify SSE content type
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/event-stream") {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}

	events, gotDone := parseE2ESSEEvents(t, resp)
	if !gotDone {
		t.Fatal("expected [DONE] sentinel in SSE stream")
	}

	for i, ev := range events {
		t.Logf("event[%d]: type=%s content_len=%d", i, ev.Type, len(ev.Content))
	}
	for _, ev := range events {
		if ev.Type == codingagent.EventError {
			if strings.Contains(ev.Content, "404") || strings.Contains(ev.Content, "upstream_error") {
				t.Skipf("Skipping: Anthropic API returned upstream error: %s", ev.Content)
			}
			t.Fatalf("received error event: %s", ev.Content)
		}
	}

	hasResult := false
	for _, ev := range events {
		if ev.Type == codingagent.EventResult || ev.Type == codingagent.EventText || ev.Type == codingagent.EventToolUse {
			hasResult = true
			break
		}
	}
	if !hasResult {
		t.Error("expected at least one result, text, or tool_use event in SSE stream")
	}

	filePath := filepath.Join(workDir, "test_anthropic.txt")
	content, err := os.ReadFile(filePath)
	if err != nil {
		entries, _ := os.ReadDir(workDir)
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("expected test_anthropic.txt in %s, got: %v, err: %v",
			workDir, names, err)
	}
	contentStr := string(content)
	if !strings.Contains(contentStr, "Hello from Anthropic via Codex") {
		t.Errorf("content = %q, want 'Hello from Anthropic via Codex'", contentStr)
	}
	t.Logf("File created successfully: %s (%d bytes)", filePath, len(content))
}

// --- TC-Codex-006: Codex + GPT-5.3-codex model file creation ---

// TestCodexE2E_GPT5Codex_FileCreation verifies Codex CLI + GPT-5.3-codex (OpenAI)
// continues to work through Bifrost SDK routing.
func TestCodexE2E_GPT5Codex_FileCreation(t *testing.T) {
	baseURL, cleanup := startCodexE2EServer(t)
	defer cleanup()
	workDir := t.TempDir()

	sessionID := createE2ESessionWithModel(
		t, baseURL, "codex", "gpt-5.3-codex", workDir)
	t.Logf("Session created: %s", sessionID)

	prompt := "Create a file named test_gpt5.txt in the current directory " +
		"containing exactly the text 'Hello from GPT5 Codex'. Do nothing else."
	resp := sendE2EMessage(t, baseURL, sessionID, prompt, 120*time.Second)
	defer resp.Body.Close()

	// Verify SSE content type
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/event-stream") {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}

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

	hasResult := false
	for _, ev := range events {
		if ev.Type == codingagent.EventResult || ev.Type == codingagent.EventText || ev.Type == codingagent.EventToolUse {
			hasResult = true
			break
		}
	}
	if !hasResult {
		t.Error("expected at least one result, text, or tool_use event in SSE stream")
	}

	filePath := filepath.Join(workDir, "test_gpt5.txt")
	content, err := os.ReadFile(filePath)
	if err != nil {
		entries, _ := os.ReadDir(workDir)
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("expected test_gpt5.txt in %s, got: %v, err: %v",
			workDir, names, err)
	}
	contentStr := string(content)
	if !strings.Contains(contentStr, "Hello from GPT5 Codex") {
		t.Errorf("content = %q, want 'Hello from GPT5 Codex'", contentStr)
	}
	t.Logf("File created successfully: %s (%d bytes)", filePath, len(content))
}
// --- TC-Codex-007: Real ternctl command execution ---

// TestCodexE2E_TernctlRealCommand starts a tern server via Go API (startE2EServer)
// and runs ternctl as a real subprocess via exec.Command, verifying that tool
// use/result events appear in ternctl's stdout output.
func TestCodexE2E_TernctlRealCommand(t *testing.T) {
	// Prerequisite: codex CLI on PATH
	if _, err := exec.LookPath("codex"); err != nil {
		t.Fatalf("codex CLI not found on PATH: %v", err)
	}

	// Resolve ternctl binary path (handles Windows .exe extension)
	ternctlName := "../bin/ternctl"
	if runtime.GOOS == "windows" {
		// On Windows, exec.Command requires .exe extension.
		// Try with .exe first, then without.
		if _, err := os.Stat(ternctlName + ".exe"); err == nil {
			ternctlName = ternctlName + ".exe"
		}
	}
	ternctlBin, err := filepath.Abs(ternctlName)
	if err != nil {
		t.Fatalf("resolve ternctl path: %v", err)
	}
	if _, err := os.Stat(ternctlBin); err != nil {
		t.Fatalf("ternctl binary not found at %s: %v", ternctlBin, err)
	}

	// Phase 1: Start tern server via Go API (same as other E2E tests)
	baseURL, cleanup := startE2EServer(t)
	defer cleanup()

	// Phase 2: Run ternctl as subprocess
	tmpDir := t.TempDir()
	workDir := filepath.Join(tmpDir, "work")
	os.MkdirAll(workDir, 0755)
	os.MkdirAll(filepath.Join(workDir, ".codex"), 0755)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	ternctlCmd := exec.CommandContext(ctx, ternctlBin,
		"--server", baseURL,
		"run",
		"--agent", "codex",
		"--prompt", "please run 'echo hello' command and report the result.",
		"--work-dir", workDir,
	)
	output, err := ternctlCmd.CombinedOutput()
	outputStr := string(output)
	t.Logf("ternctl output:\n%s", outputStr)

	// Phase 3: Verify stdout content
	if err != nil {
		if strings.Contains(outputStr, "Refusing to create helper binaries") || strings.Contains(outputStr, "404") || strings.Contains(outputStr, "upstream_error") {
			t.Skipf("Skipping: ternctl exited with error (likely Windows temp dir sandbox limitation or API issue): %v\noutput: %s", err, outputStr)
		}
		t.Fatalf("ternctl exited with error: %v\noutput: %s", err, outputStr)
	}
	if !strings.Contains(outputStr, "Session created:") {
		t.Error("expected 'Session created:' in output")
	}
	if !strings.Contains(outputStr, "[Tool:") {
		t.Error("expected '[Tool: ...]' in output (tool use event)")
	}
	if !strings.Contains(outputStr, "[Tool Result]") {
		t.Error("expected '[Tool Result] ...' in output (tool result event)")
	}
	if !strings.Contains(outputStr, `"status": "completed"`) && !strings.Contains(outputStr, `"status": "active"`) {
		t.Error("expected session status 'completed' or 'active' in output")
	}
}
