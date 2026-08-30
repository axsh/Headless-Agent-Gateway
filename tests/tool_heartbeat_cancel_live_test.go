package llm_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/axsh/arctic-tern/server"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

const (
	liveHeartbeatIntervalEnv = "SSE_TOOL_HEARTBEAT_INTERVAL"
	liveHeartbeatInterval    = "1s"
	liveCodexHeartbeatModel  = "gpt-4o"
	liveToolStillRunning     = "tool_still_running"
)

func requireLiveCodexCLI(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("codex"); err != nil {
		t.Fatalf("codex CLI required on PATH for live heartbeat/cancel tests: %v", err)
	}
}

func requireLiveClaudeCLI(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("claude"); err != nil {
		t.Fatalf("claude CLI required on PATH for live heartbeat tests: %v", err)
	}
}

// startHeartbeatCancelLiveServer launches tern with a short tool heartbeat interval.
// Missing CLIs are checked by individual tests (Fatal, never Skip).
func startHeartbeatCancelLiveServer(t *testing.T) (string, func()) {
	t.Helper()
	t.Setenv(liveHeartbeatIntervalEnv, liveHeartbeatInterval)

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
  backends: [keyring]
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
	if err := srv.Launch(context.Background()); err != nil {
		t.Fatalf("Launch failed: %v", err)
	}

	baseURL := fmt.Sprintf("http://localhost:%d", srv.AgentService().Port())
	cleanup := func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutCtx)
	}
	return baseURL, cleanup
}

func sleepToolPrompt(nSeconds int) string {
	return fmt.Sprintf(`You MUST run exactly one shell/tool command that sleeps for at least %d seconds before finishing.
Do not answer with text only. Do not skip the sleep.
Unix/bash: sleep %d
Windows cmd: timeout /t %d /nobreak
After the sleep completes, reply with exactly: SLEEP_DONE`, nSeconds, nSeconds, nSeconds)
}

func shortResumePrompt() string {
	return "Reply with exactly: OK"
}

func waitSessionFollowable(t *testing.T, baseURL, sessionID string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		info := getE2ESession(t, baseURL, sessionID)
		if followable, _ := info["followable"].(bool); followable {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("session %s not followable within %s", sessionID, timeout)
}

func postCancelTurn(t *testing.T, baseURL, sessionID string) {
	t.Helper()
	resp, err := http.Post(baseURL+"/api/v1/sessions/"+sessionID+"/cancel", "application/json", nil)
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cancel status=%d body=%s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "cancelled") {
		t.Fatalf("cancel body missing cancelled: %s", body)
	}
}

func postTerminateSession(t *testing.T, baseURL, sessionID string) {
	t.Helper()
	resp, err := http.Post(baseURL+"/api/v1/sessions/"+sessionID+"/terminate", "application/json", nil)
	if err != nil {
		t.Fatalf("terminate: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("terminate status=%d body=%s", resp.StatusCode, body)
	}
}

type asyncSSEResult struct {
	resp   *http.Response
	err    error
	events []codingagent.StreamEvent
	done   bool
}

// startLongToolSSE posts /messages asynchronously and drains SSE in the background.
func startLongToolSSE(t *testing.T, baseURL, sessionID, prompt string, overallTimeout time.Duration) (headerReady <-chan *http.Response, finished <-chan asyncSSEResult) {
	t.Helper()
	headerCh := make(chan *http.Response, 1)
	finishCh := make(chan asyncSSEResult, 1)

	go func() {
		body, _ := json.Marshal(map[string]any{
			"content": []map[string]string{{"type": "text", "text": prompt}},
		})
		req, err := http.NewRequest(http.MethodPost,
			baseURL+"/api/v1/sessions/"+sessionID+"/messages",
			bytes.NewReader(body))
		if err != nil {
			finishCh <- asyncSSEResult{err: err}
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/event-stream")

		client := &http.Client{Timeout: overallTimeout}
		resp, err := client.Do(req)
		if err != nil {
			finishCh <- asyncSSEResult{err: err}
			return
		}
		headerCh <- resp

		events, gotDone := parseE2ESSEEvents(t, resp)
		_ = resp.Body.Close()
		finishCh <- asyncSSEResult{resp: resp, events: events, done: gotDone}
	}()

	return headerCh, finishCh
}

func assertTurnTerminal(t *testing.T, events []codingagent.StreamEvent, gotDone bool) {
	t.Helper()
	var terminal bool
	for _, ev := range events {
		if ev.Type == codingagent.EventResult || ev.Type == codingagent.EventError {
			terminal = true
			break
		}
	}
	if !terminal {
		t.Fatalf("missing result/error terminal event; events=%v", summarizeEvents(events))
	}
	if !gotDone {
		t.Fatal("missing SSE [DONE]")
	}
}

func summarizeEvents(events []codingagent.StreamEvent) string {
	parts := make([]string, 0, len(events))
	for _, ev := range events {
		parts = append(parts, fmt.Sprintf("%s/%s", ev.Type, truncateLiveBody(ev.Content, 40)))
	}
	return strings.Join(parts, ", ")
}

func runHeartbeatScenario(t *testing.T, baseURL, agent, model string) {
	t.Helper()
	workDir := t.TempDir()
	sessionID := createE2ESessionWithModel(t, baseURL, agent, model, workDir)

	attempt := func(prompt string) (events []codingagent.StreamEvent, gotDone bool, ok bool) {
		resp := sendE2EMessage(t, baseURL, sessionID, prompt, 120*time.Second)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("send message status=%d body=%s", resp.StatusCode, b)
		}
		events, gotDone = parseE2ESSEEvents(t, resp)
		for _, ev := range events {
			if ev.Type == codingagent.EventProgress && ev.Content == liveToolStillRunning && strings.TrimSpace(ev.ToolName) != "" {
				return events, gotDone, true
			}
		}
		return events, gotDone, false
	}

	events, gotDone, ok := attempt(sleepToolPrompt(8))
	if !ok {
		t.Logf("first attempt missing heartbeat; retrying once; events=%v", summarizeEvents(events))
		events, gotDone, ok = attempt(sleepToolPrompt(10))
	}
	if !ok {
		t.Fatalf("expected tool_still_running heartbeat after retry; events=%v", summarizeEvents(events))
	}
	assertTurnTerminal(t, events, gotDone)
}

// TestLiveToolHeartbeat_Codex verifies SSE progress heartbeats while a real Codex tool runs.
func TestLiveToolHeartbeat_Codex(t *testing.T) {
	requireLiveCodexCLI(t)
	baseURL, cleanup := startHeartbeatCancelLiveServer(t)
	defer cleanup()
	runHeartbeatScenario(t, baseURL, "codex", liveCodexHeartbeatModel)
}

// TestLiveToolHeartbeat_ClaudeCode verifies the same heartbeat contract with Claude Code.
func TestLiveToolHeartbeat_ClaudeCode(t *testing.T) {
	requireLiveClaudeCLI(t)
	baseURL, cleanup := startHeartbeatCancelLiveServer(t)
	defer cleanup()
	runHeartbeatScenario(t, baseURL, "claudecode", e2eDefaultModel)
}

// TestLiveTurnCancel_CodexResume cancels an in-flight Codex turn and resumes on the same session id.
func TestLiveTurnCancel_CodexResume(t *testing.T) {
	requireLiveCodexCLI(t)
	baseURL, cleanup := startHeartbeatCancelLiveServer(t)
	defer cleanup()

	workDir := t.TempDir()
	sessionID := createE2ESessionWithModel(t, baseURL, "codex", liveCodexHeartbeatModel, workDir)

	headerReady, finished := startLongToolSSE(t, baseURL, sessionID, sleepToolPrompt(60), 120*time.Second)

	select {
	case resp := <-headerReady:
		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			t.Fatalf("async send status=%d body=%s", resp.StatusCode, b)
		}
	case res := <-finished:
		if res.err != nil {
			t.Fatalf("async send failed before headers: %v", res.err)
		}
		t.Fatalf("SSE finished before cancel opportunity; events=%v", summarizeEvents(res.events))
	case <-time.After(60 * time.Second):
		t.Fatal("timed out waiting for SSE response headers")
	}

	waitSessionFollowable(t, baseURL, sessionID, 60*time.Second)
	postCancelTurn(t, baseURL, sessionID)

	info := getE2ESession(t, baseURL, sessionID)
	if id, _ := info["id"].(string); id != "" && id != sessionID {
		t.Fatalf("session id = %q, want %q", id, sessionID)
	}
	status, _ := info["status"].(string)
	if status == codingagent.StatusClosed {
		t.Fatal("status must not be closed after cancel")
	}
	if status != codingagent.StatusError {
		t.Fatalf("status = %q, want %q", status, codingagent.StatusError)
	}
	if errField, _ := info["error"].(string); errField != "turn cancelled" {
		t.Fatalf("error field = %q, want %q", errField, "turn cancelled")
	}

	select {
	case res := <-finished:
		if res.err != nil {
			t.Fatalf("SSE after cancel: %v", res.err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("SSE did not finish within 30s after cancel")
	}

	resp2 := sendE2EMessage(t, baseURL, sessionID, shortResumePrompt(), 90*time.Second)
	defer resp2.Body.Close()
	if resp2.StatusCode == http.StatusConflict {
		t.Fatalf("session still busy after cancel: %s", readBodyPreview(resp2))
	}
	if resp2.StatusCode == http.StatusNotFound {
		t.Fatal("session id destroyed after cancel")
	}
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("resume send status=%d body=%s", resp2.StatusCode, readBodyPreview(resp2))
	}
	events, gotDone := parseE2ESSEEvents(t, resp2)
	assertTurnTerminal(t, events, gotDone)
}

// TestLiveTurnCancel_TerminateClosesSession verifies terminate still closes the session.
func TestLiveTurnCancel_TerminateClosesSession(t *testing.T) {
	requireLiveCodexCLI(t)
	baseURL, cleanup := startHeartbeatCancelLiveServer(t)
	defer cleanup()

	workDir := t.TempDir()
	sessionID := createE2ESessionWithModel(t, baseURL, "codex", liveCodexHeartbeatModel, workDir)
	postTerminateSession(t, baseURL, sessionID)

	info := getE2ESession(t, baseURL, sessionID)
	status, _ := info["status"].(string)
	if status != codingagent.StatusClosed {
		t.Fatalf("status = %q, want %q", status, codingagent.StatusClosed)
	}
}

func readBodyPreview(resp *http.Response) string {
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}
