package llm_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/axsh/arctic-tern/server"
	"github.com/axsh/arctic-tern/shared/libs/go/artifact/store"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// parityAgentRow is one Coding Agent under the transparent E2E matrix.
// Only Agent/Model/CLIName differ; case bodies must stay agent-agnostic.
type parityAgentRow struct {
	Agent   string
	Model   string
	CLIName string
}

var parityAgents = []parityAgentRow{
	{Agent: "claudecode", Model: e2eDefaultModel, CLIName: "claude"},
	{Agent: "codex", Model: "gpt-4o", CLIName: "codex"},
}

func requireParityCLI(t *testing.T, cliName string) {
	t.Helper()
	if _, err := exec.LookPath(cliName); err != nil {
		t.Skipf("%s CLI not on PATH: %v", cliName, err)
	}
}

// startParityMatrixServer starts tern with CreateAll agents.
// Per-row CLI presence is checked by requireParityCLI (no Fatal here).
func startParityMatrixServer(t *testing.T) (string, func()) {
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
  disable_sandbox: true
`, gwPort, filepath.ToSlash(modelProfilesSrc), wsPort, asPort)
	require.NoError(t, os.WriteFile(tmpConfig, []byte(configContent), 0644))

	srv, err := server.New(server.WithConfigPath(tmpConfig))
	require.NoError(t, err)
	require.NoError(t, srv.Launch(context.Background()))

	baseURL := fmt.Sprintf("http://localhost:%d", srv.AgentService().Port())
	cleanup := func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}
	return baseURL, cleanup
}

func resolveTernctlBin(t *testing.T) string {
	t.Helper()
	ternctlName := "../bin/ternctl"
	if runtime.GOOS == "windows" {
		absBase, err := filepath.Abs(ternctlName)
		require.NoError(t, err)
		if _, err := os.Stat(absBase + ".exe"); err == nil {
			return absBase + ".exe"
		}
		if _, err := os.Stat(absBase); err == nil {
			exePath := filepath.Join(t.TempDir(), "ternctl.exe")
			data, readErr := os.ReadFile(absBase)
			require.NoError(t, readErr)
			require.NoError(t, os.WriteFile(exePath, data, 0755))
			return exePath
		}
		t.Fatalf("ternctl binary not found (looked for %s[.exe])", absBase)
	}
	abs, err := filepath.Abs(ternctlName)
	require.NoError(t, err)
	if _, err := os.Stat(abs); err != nil {
		t.Fatalf("ternctl binary not found at %s: %v", abs, err)
	}
	return abs
}

func runParityFileCreate(t *testing.T, row parityAgentRow) {
	t.Helper()
	requireParityCLI(t, row.CLIName)

	baseURL, cleanup := startParityMatrixServer(t)
	defer cleanup()

	workDir := t.TempDir()
	sessionID := createE2ESessionWithModel(t, baseURL, row.Agent, row.Model, workDir)
	t.Logf("parity FileCreate agent=%s session=%s", row.Agent, sessionID)

	// Identical prompt for every agent row.
	prompt := fileCreatePrompt(workDir, "parity_hello.txt", "Parity Hello")
	resp := sendE2EMessage(t, baseURL, sessionID, prompt, 120*time.Second)
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/event-stream") {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}

	events, gotDone := parseE2ESSEEvents(t, resp)
	assertParitySSEDone(t, gotDone)

	for _, ev := range events {
		if ev.Type == codingagent.EventError {
			// Symmetric skip only: upstream/API issues may hit either agent.
			if strings.Contains(ev.Content, "404") ||
				strings.Contains(ev.Content, "upstream_error") ||
				strings.Contains(ev.Content, "authentication") {
				t.Skipf("Skipping parity FileCreate for %s due to upstream/auth: %s", row.Agent, ev.Content)
			}
			t.Fatalf("error event from %s: %s", row.Agent, ev.Content)
		}
	}

	hasContent := false
	for _, ev := range events {
		if ev.Type == codingagent.EventText || ev.Type == codingagent.EventToolUse || ev.Type == codingagent.EventResult {
			hasContent = true
			break
		}
	}
	if !hasContent {
		t.Fatal("expected at least one text, tool_use, or result event")
	}

	assertParityWorkFileExists(t, workDir, "parity_hello.txt", events)
	assertParitySessionCompleted(t, baseURL, sessionID)
}

func runParityTernctl(t *testing.T, row parityAgentRow) {
	t.Helper()
	requireParityCLI(t, row.CLIName)
	ternctlBin := resolveTernctlBin(t)

	baseURL, cleanup := startParityMatrixServer(t)
	defer cleanup()

	workDir := filepath.Join(t.TempDir(), "work")
	require.NoError(t, os.MkdirAll(workDir, 0755))
	_ = os.MkdirAll(filepath.Join(workDir, ".codex"), 0755)
	_ = os.MkdirAll(filepath.Join(workDir, ".claude"), 0755)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, ternctlBin,
		"--server", baseURL,
		"run",
		"--agent", row.Agent,
		"--prompt", "please run 'echo hello' command and report the result.",
		"--work-dir", workDir,
	)
	output, err := cmd.CombinedOutput()
	outputStr := string(output)
	t.Logf("parity Ternctl agent=%s output:\n%s", row.Agent, outputStr)

	if err != nil {
		if strings.Contains(outputStr, "Refusing to create helper binaries") ||
			strings.Contains(outputStr, "404") ||
			strings.Contains(outputStr, "upstream_error") ||
			strings.Contains(outputStr, "authentication") {
			t.Skipf("Skipping parity Ternctl for %s: %v", row.Agent, err)
		}
		t.Fatalf("ternctl exited with error for %s: %v\n%s", row.Agent, err, outputStr)
	}

	if !strings.Contains(outputStr, "Session created:") {
		t.Error("expected 'Session created:' in output")
	}
	if !strings.Contains(outputStr, "[Tool:") {
		t.Error("expected '[Tool: ...]' in output")
	}
	hasToolResult := strings.Contains(outputStr, "[Tool Result]")
	hasEchoEvidence := strings.Contains(outputStr, "hello") || strings.Contains(outputStr, "Hello")
	if !hasToolResult && !hasEchoEvidence {
		t.Error("expected '[Tool Result]' or echo evidence in ternctl stdout")
	}
	if !strings.Contains(outputStr, `"status": "completed"`) && !strings.Contains(outputStr, `"status": "active"`) {
		t.Error("expected session status 'completed' or 'active'")
	}
}

// TestAgentParityMatrix_FileCreate runs the same FileCreate case for each Coding Agent.
func TestAgentParityMatrix_FileCreate(t *testing.T) {
	for _, row := range parityAgents {
		row := row
		t.Run(row.Agent, func(t *testing.T) {
			runParityFileCreate(t, row)
		})
	}
}

// TestAgentParityMatrix_Ternctl runs the same ternctl case for each Coding Agent.
func TestAgentParityMatrix_Ternctl(t *testing.T) {
	for _, row := range parityAgents {
		row := row
		t.Run(row.Agent, func(t *testing.T) {
			runParityTernctl(t, row)
		})
	}
}

// TestAgentParityMatrix_FixtureWriteList swaps only fake agent Name() and asserts List.
func TestAgentParityMatrix_FixtureWriteList(t *testing.T) {
	writeEv := codingagent.StreamEvent{
		Type:     codingagent.EventToolUse,
		ToolName: "Write",
		ToolInput: map[string]any{
			"file_path": "hello.txt",
			"content":   "parity",
		},
	}
	for _, name := range []string{"claudecode", "codex"} {
		name := name
		t.Run(name, func(t *testing.T) {
			agent := &turnDiffFakeAgent{name: name, events: []codingagent.StreamEvent{writeEv}}
			baseURL, sessionID, st := startTurnDiffArtifactServer(t, agent, nil)
			postSSEMessage(t, baseURL, sessionID, "create hello")

			page, err := st.ListSystemArtifacts(context.Background(), store.SystemArtifactFilter{
				SessionIDs: []string{sessionID},
			})
			require.NoError(t, err)
			AssertSystemArtifactPathsContain(t, page.Items, "hello.txt")
			found := false
			for _, item := range page.Items {
				if filepath.Base(item.Key) == "hello.txt" {
					found = true
					assert.Equal(t, "Write", item.ToolName)
				}
			}
			require.True(t, found, "expected hello.txt for agent %s", name)
		})
	}
}
