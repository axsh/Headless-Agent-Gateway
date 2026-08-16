package llm_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/axsh/arctic-tern/server"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

func mustStartCodexE2EServer(t *testing.T) (string, func()) {
	t.Helper()

	if _, err := exec.LookPath("codex"); err != nil {
		t.Fatalf("LIVE test requires codex CLI on PATH: %v", err)
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

func mustStartClaudeE2EServer(t *testing.T) (string, func()) {
	t.Helper()

	if _, err := exec.LookPath("claude"); err != nil {
		t.Fatalf("LIVE test requires claude CLI on PATH: %v", err)
	}

	return startE2EServer(t)
}

func liveReconnectTurnMustSucceed(t *testing.T, baseURL, sessionID, prompt, wantSubstr string) {
	t.Helper()
	resp := sendE2EMessage(t, baseURL, sessionID, prompt, 4*time.Minute)
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("send status = %d", resp.StatusCode)
	}
	events, _ := parseE2ESSEEvents(t, resp)
	sawResult := false
	var allText strings.Builder
	for _, ev := range events {
		if ev.Type == codingagent.EventError {
			t.Fatalf("received error event in live turn: %s", ev.Content)
		}
		if ev.Type == codingagent.EventResult {
			sawResult = true
		}
		if ev.Type == codingagent.EventText {
			allText.WriteString(ev.Content)
		}
	}
	if !sawResult {
		t.Fatalf("turn ended without EventResult")
	}
	if wantSubstr != "" && !strings.Contains(allText.String(), wantSubstr) {
		t.Fatalf("response text %q does not contain expected substring %q", allText.String(), wantSubstr)
	}
}

func TestStreamReconnectLiveResumeSend(t *testing.T) {
	baseURL, cleanup := mustStartCodexE2EServer(t)
	defer cleanup()

	workDir := t.TempDir()
	sessionID := createE2ESessionWithModel(t, baseURL, "codex", "gpt-4o", workDir)
	liveReconnectTurnMustSucceed(t, baseURL, sessionID, "Reply with exactly: reconnect-live-ok", "reconnect-live-ok")
	liveReconnectTurnMustSucceed(t, baseURL, sessionID, "Reply with a short ack that this is still the same session.", "")
}

func TestStreamReconnectLiveClaudeResumeSend(t *testing.T) {
	baseURL, cleanup := mustStartClaudeE2EServer(t)
	defer cleanup()

	workDir := t.TempDir()
	sessionID := createE2ESessionWithModel(t, baseURL, "claudecode", e2eDefaultModel, workDir)
	liveReconnectTurnMustSucceed(t, baseURL, sessionID, "Reply with exactly: reconnect-live-ok", "reconnect-live-ok")
	liveReconnectTurnMustSucceed(t, baseURL, sessionID, "Reply with a short ack that this is still the same session.", "")
}

func TestStreamReconnectLiveOverloadClassified(t *testing.T) {
	baseURL, cleanup := mustStartCodexE2EServer(t)
	defer cleanup()

	workDir := t.TempDir()
	sessionID := createE2ESessionWithModel(t, baseURL, "codex", "gpt-4o", workDir)
	resp := sendE2EMessage(t, baseURL, sessionID, "Reply with hello", 4*time.Minute)
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("send status = %d", resp.StatusCode)
	}
	events, _ := parseE2ESSEEvents(t, resp)
	sawResult := false
	var lastErr string
	for _, ev := range events {
		if ev.Type == codingagent.EventResult {
			sawResult = true
		}
		if ev.Type == codingagent.EventError {
			lastErr = ev.Content
		}
	}
	if sawResult {
		return
	}
	if strings.Contains(lastErr, "["+codingagent.ErrorCodeUpstreamOverloaded+"]") {
		return
	}
	t.Fatalf("turn ended without result or classified overload error=%q", lastErr)
}
