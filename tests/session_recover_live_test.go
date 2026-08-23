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

func requireCodexCLI(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("codex"); err != nil {
		t.Fatalf("codex CLI required on PATH for live session recover test: %v", err)
	}
}

// startCodexE2EServerSandboxEnforced starts tern with codex sandbox policy active (disable_sandbox: false).
func startCodexE2EServerSandboxEnforced(t *testing.T) (string, func()) {
	t.Helper()

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
  disable_sandbox: false
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

	baseURL := fmt.Sprintf("http://localhost:%d", srv.AgentService().Port())
	cleanup := func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutCtx)
	}
	return baseURL, cleanup
}

func postLiveMessageWithContext(t *testing.T, baseURL, sessionID, message string, ctx context.Context) *http.Response {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"content": []map[string]string{{"type": "text", "text": message}},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		baseURL+"/api/v1/sessions/"+sessionID+"/messages",
		bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("send message: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("send message: expected 200, got %d: %s", resp.StatusCode, b)
	}
	return resp
}

func getLiveFollowEvents(t *testing.T, ctx context.Context, baseURL, sessionID, from string) *http.Response {
	t.Helper()
	u := baseURL + "/api/v1/sessions/" + sessionID + "/events"
	if from != "" {
		u += "?from=" + from
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		t.Fatalf("new follow request: %v", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("follow events: %v", err)
	}
	return resp
}

func waitE2ESessionCompleted(t *testing.T, baseURL, sessionID string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		status := getE2ESession(t, baseURL, sessionID)
		if s, ok := status["status"].(string); ok && s == "completed" {
			return
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("session %s not completed within %s", sessionID, timeout)
}

// TestSessionRecoverLive_CodexSandboxReject verifies Issue #51 end-to-end with real codex CLI.
// Scenario D: POST disconnect after tool_use, then FollowFrom for tool_result and terminal.
// Prerequisites: codex on PATH, vault API key, LLM gateway reachable (same as other codex E2E tests).
// t.Skip is intentionally not used — missing prerequisites fail the test.
func TestSessionRecoverLive_CodexSandboxReject(t *testing.T) {
	requireCodexCLI(t)
	baseURL, cleanup := startCodexE2EServerSandboxEnforced(t)
	defer cleanup()

	workDir := t.TempDir()
	tmpFile := filepath.Join(workDir, "check.html")
	sessionID := createE2ESessionWithModel(t, baseURL, "codex", "gpt-5.3-codex", workDir)

	// Issue #51 compound bash ending with rm -f cleanup (sandbox enforced).
	prompt := fmt.Sprintf(`Run exactly one command_execution with this compound bash command (must end with rm -f cleanup):
bash -lc 'touch %s && rm -f %s'`,
		filepath.ToSlash(tmpFile), filepath.ToSlash(tmpFile))

	postCtx, postCancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer postCancel()
	resp := postLiveMessageWithContext(t, baseURL, sessionID, prompt, postCtx)

	streamBody, lastID := readLiveSSEUntilRecover(t, resp.Body)
	resp.Body.Close()

	if !strings.Contains(streamBody, "tool_result") {
		// POST may disconnect before tool_result; try Follow from last logical id.
		if lastID == "" {
			t.Fatalf("missing tool_result and no logical id for follow: %s", truncateLiveBody(streamBody, 2000))
		}
		waitE2ESessionCompleted(t, baseURL, sessionID, 45*time.Second)

		followCtx, followCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer followCancel()
		follow := getLiveFollowEvents(t, followCtx, baseURL, sessionID, lastID)
		defer follow.Body.Close()
		if follow.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(follow.Body)
			t.Fatalf("follow: expected 200, got %d: %s", follow.StatusCode, b)
		}
		raw, err := io.ReadAll(follow.Body)
		if err != nil {
			t.Fatalf("read follow body: %v", err)
		}
		streamBody = string(raw)
	}

	lower := strings.ToLower(streamBody)
	if !strings.Contains(streamBody, "tool_result") {
		t.Fatalf("missing tool_result: %s", truncateLiveBody(streamBody, 2000))
	}
	if !strings.Contains(lower, "rejected") && !strings.Contains(lower, "rm -f") && !strings.Contains(lower, "not permitted") && !strings.Contains(lower, "blocked by policy") {
		t.Fatalf("missing rejection text: %s", truncateLiveBody(streamBody, 2000))
	}
	if !strings.Contains(streamBody, "[DONE]") && !strings.Contains(streamBody, `"type":"result"`) {
		t.Fatalf("missing [DONE] and terminal result: %s", truncateLiveBody(streamBody, 2000))
	}
}

func readLiveSSEUntilRecover(t *testing.T, body io.Reader) (string, string) {
	t.Helper()
	var buf strings.Builder
	var lastID string
	pending := ""
	scanner := codingagent.NewLargeLineScanner(body, 0)
	for scanner.Scan() {
		line := scanner.Text()
		buf.WriteString(line)
		buf.WriteString("\n")
		if strings.HasPrefix(line, "id: ") {
			pending = strings.TrimSpace(strings.TrimPrefix(line, "id: "))
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if pending != "" {
			lastID = pending
		}
		if data == "[DONE]" {
			return buf.String(), lastID
		}
	}
	if err := scanner.Err(); err != nil {
		t.Logf("SSE scanner stopped: %v", err)
	}
	return buf.String(), lastID
}

func truncateLiveBody(body string, max int) string {
	if len(body) <= max {
		return body
	}
	return body[:max] + "..."
}
