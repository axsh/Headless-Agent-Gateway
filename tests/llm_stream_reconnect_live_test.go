package llm_test

import (
	"strings"
	"testing"
	"time"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

func TestStreamReconnectLiveResumeSend(t *testing.T) {
	requireCLI(t, "codex")
	baseURL, cleanup := startCodexE2EServer(t)
	defer cleanup()

	workDir := t.TempDir()
	sessionID := createE2ESessionWithModel(t, baseURL, "codex", "gpt-4o", workDir)
	liveReconnectTurn(t, baseURL, sessionID, "Reply with exactly: reconnect-live-ok")
	liveReconnectTurn(t, baseURL, sessionID, "Reply with a short ack that this is still the same session.")
}

func liveReconnectTurn(t *testing.T, baseURL, sessionID, prompt string) {
	t.Helper()
	resp := sendE2EMessage(t, baseURL, sessionID, prompt, 4*time.Minute)
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
