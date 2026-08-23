package llm_test

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/axsh/arctic-tern/shared/libs/go/agentservice"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent/codex"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent/codex/testfake"
	"github.com/axsh/arctic-tern/shared/libs/go/config"
	"github.com/axsh/arctic-tern/shared/libs/go/logger"
)

func TestSessionRecover_CodexTestfakeSandboxReject(t *testing.T) {
	dir := t.TempDir()
	stderr := "ERROR codex_core::tools::router: exec_command failed: Rejected(\"rm -f style commands are not permitted\")"
	testfake.Install(t, dir, testfake.Options{
		Lines: []string{
			`{"type":"item.started","item":{"type":"command_execution","command":"rm -f /tmp/x"}}`,
		},
		Stderr:   stderr,
		ExitCode: 1,
	})

	log := logger.NewDefault(logger.LevelInfo)
	srv := agentservice.New(
		agentservice.WithLogger(log),
		agentservice.WithProcessRetry(config.ProcessRetryConfig{MaxAttempts: 1, IntervalSeconds: 0}),
		agentservice.WithSandboxDisabled(true),
	)
	srv.SetModelProfiles(&config.ModelProfilesConfig{
		CodingAgents: map[string]config.AgentConfig{
			"codex": {ExecutionMode: codingagent.ExecutionModeSingleShot},
		},
	})
	srv.RegisterAgent(codex.New(&codingagent.AdapterConfig{Logger: log}))

	ctx := context.Background()
	port := freePort(t)
	if err := srv.Launch(ctx, port); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer srv.Shutdown(context.Background())

	baseURL := "http://localhost:" + strconv.Itoa(port)
	workDir := t.TempDir()
	sessionID := createE2ESessionNoModel(t, baseURL, "codex", workDir)

	resp := sendE2EMessage(t, baseURL, sessionID, "run rm", 90*time.Second)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("send message: %d %s", resp.StatusCode, b)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read SSE: %v", err)
	}

	var sawToolResult bool
	var sawError bool
	scanner := bufio.NewScanner(strings.NewReader(string(body)))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}
		if strings.Contains(data, "tool_result") {
			sawToolResult = true
		}
		if strings.Contains(data, `"type":"error"`) {
			sawError = true
		}
	}
	if !sawToolResult {
		t.Fatalf("expected tool_result on SSE, body: %s", body)
	}
	if !sawError && !strings.Contains(string(body), `"type":"result"`) {
		t.Fatalf("expected terminal error or result on SSE, body: %s", body)
	}
}
