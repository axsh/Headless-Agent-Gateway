// Package llm_test contains E2E tests for large Codex tool output SSE terminal events.
package llm_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/axsh/arctic-tern/shared/libs/go/agentservice"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent/codex"
	"github.com/axsh/arctic-tern/shared/libs/go/config"
	"github.com/axsh/arctic-tern/shared/libs/go/logger"
	"github.com/axsh/arctic-tern/tests/testutil"
)

func TestCodexE2E_LargeToolOutputTerminalEvent(t *testing.T) {
	dir := t.TempDir()
	testutil.InstallFakeCodex(t, dir, testutil.FakeCodexOptions{ExitCode: 0})
	testutil.PrependPath(t, dir)

	srv := agentservice.New(
		agentservice.WithLogger(logger.NewDefault(logger.LevelInfo)),
	)
	srv.SetModelProfiles(&config.ModelProfilesConfig{
		CodingAgents: map[string]config.AgentConfig{
			"codex": {ExecutionMode: codingagent.ExecutionModeSingleShot},
		},
	})
	adapter := codex.New(&codingagent.AdapterConfig{
		Logger: logger.NewDefault(logger.LevelInfo),
	})
	srv.RegisterAgent(adapter)

	ctx := context.Background()
	port := freePort(t)
	if err := srv.Launch(ctx, port); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer srv.Shutdown(context.Background())

	baseURL := "http://localhost:" + strconv.Itoa(port)
	workDir := t.TempDir()
	sessionID := createE2ESessionNoModel(t, baseURL, "codex", workDir)

	resp := sendE2EMessage(t, baseURL, sessionID, "trigger", 30*time.Second)
	defer resp.Body.Close()

	events, gotDone := parseE2ESSEEvents(t, resp)
	if !gotDone {
		t.Fatal("expected [DONE] sentinel in SSE stream")
	}

	resultCount := 0
	for _, ev := range events {
		if ev.Type == codingagent.EventResult {
			resultCount++
		}
	}
	if resultCount != 1 {
		t.Fatalf("expected 1 EventResult in SSE, got %d (events=%d)", resultCount, len(events))
	}

	session := getE2ESession(t, baseURL, sessionID)
	status, _ := session["status"].(string)
	if status != "completed" {
		t.Fatalf("session status = %q, want completed", status)
	}
}
