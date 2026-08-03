// Package llm_test contains E2E tests for client/v1 SSE consumption with large tool output.
package llm_test

import (
	"context"
	"strconv"
	"strings"
	"testing"

	v1 "github.com/axsh/arctic-tern/client/v1"
	"github.com/axsh/arctic-tern/shared/libs/go/agentservice"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent/codex"
	"github.com/axsh/arctic-tern/shared/libs/go/config"
	"github.com/axsh/arctic-tern/shared/libs/go/logger"
	"github.com/axsh/arctic-tern/tests/testutil"
)

func startFakeCodexE2EServer(t *testing.T) (baseURL string, cleanup func()) {
	t.Helper()
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
	return "http://localhost:" + strconv.Itoa(port), func() {
		srv.Shutdown(context.Background())
	}
}

func TestCodexE2E_ClientV1_LargeToolOutputTerminalEvent(t *testing.T) {
	baseURL, cleanup := startFakeCodexE2EServer(t)
	defer cleanup()

	ctx := context.Background()
	workDir := t.TempDir()
	client := v1.New(baseURL, v1.WithNoTimeout())

	sess, err := client.CreateSession(ctx, v1.SessionRequest{
		Agent:   "codex",
		WorkDir: workDir,
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	stream, err := sess.SendText(ctx, "trigger")
	if err != nil {
		t.Fatalf("SendText: %v", err)
	}

	wantToolContent := strings.Repeat("x", 65537)

	var gotResult bool
	var toolResults []string
	err = stream.RunWithHandlers(ctx, sess, v1.StreamHandlers{
		OnToolResult: func(content string) { toolResults = append(toolResults, content) },
		OnResult:     func() { gotResult = true },
	})
	if err != nil {
		t.Fatalf("RunWithHandlers: %v", err)
	}
	if !gotResult {
		t.Fatal("expected EventResult, got none")
	}
	if len(toolResults) != 1 {
		t.Fatalf("expected 1 tool_result callback, got %d", len(toolResults))
	}
	if toolResults[0] != wantToolContent {
		t.Fatalf("tool_result len = %d, want %d; prefix match = %v",
			len(toolResults[0]), len(wantToolContent),
			strings.HasPrefix(toolResults[0], wantToolContent[:32]))
	}

	session, err := client.GetSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	status, _ := session["status"].(string)
	if status != "completed" {
		t.Fatalf("session status = %q, want completed", status)
	}
}
