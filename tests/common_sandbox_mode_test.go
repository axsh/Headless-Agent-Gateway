package llm_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
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

func TestSandboxMode_OmitDefaultsReadOnly(t *testing.T) {
	launchLog, baseURL, cleanup := startSandboxModeCodexServer(t, false)
	defer cleanup()

	workDir := t.TempDir()
	sessionID := createSandboxModeSession(t, baseURL, workDir, "")
	assertSessionSandboxMode(t, baseURL, sessionID, codingagent.SandboxModeReadOnly)

	sendE2EMessage(t, baseURL, sessionID, "hello", 30*time.Second).Body.Close()
	assertLaunchArgs(t, launchLog, false, codingagent.SandboxModeReadOnly)
}

func TestSandboxMode_ExplicitReadOnly(t *testing.T) {
	launchLog, baseURL, cleanup := startSandboxModeCodexServer(t, false)
	defer cleanup()

	workDir := t.TempDir()
	sessionID := createSandboxModeSession(t, baseURL, workDir, codingagent.SandboxModeReadOnly)
	assertSessionSandboxMode(t, baseURL, sessionID, codingagent.SandboxModeReadOnly)

	sendE2EMessage(t, baseURL, sessionID, "hello", 30*time.Second).Body.Close()
	assertLaunchArgs(t, launchLog, false, codingagent.SandboxModeReadOnly)
}

func TestSandboxMode_WorkspaceWrite(t *testing.T) {
	launchLog, baseURL, cleanup := startSandboxModeCodexServer(t, false)
	defer cleanup()

	workDir := t.TempDir()
	sessionID := createSandboxModeSession(t, baseURL, workDir, codingagent.SandboxModeWorkspaceWrite)
	assertSessionSandboxMode(t, baseURL, sessionID, codingagent.SandboxModeWorkspaceWrite)

	sendE2EMessage(t, baseURL, sessionID, "hello", 30*time.Second).Body.Close()
	assertLaunchArgs(t, launchLog, false, codingagent.SandboxModeWorkspaceWrite)
}

func TestSandboxMode_DangerFullAccess(t *testing.T) {
	launchLog, baseURL, cleanup := startSandboxModeCodexServer(t, false)
	defer cleanup()

	workDir := t.TempDir()
	sessionID := createSandboxModeSession(t, baseURL, workDir, codingagent.SandboxModeDangerFullAccess)
	assertSessionSandboxMode(t, baseURL, sessionID, codingagent.SandboxModeDangerFullAccess)

	sendE2EMessage(t, baseURL, sessionID, "hello", 30*time.Second).Body.Close()
	assertLaunchArgs(t, launchLog, true, "")
}

func TestSandboxMode_ServerDisableSandboxFallback(t *testing.T) {
	launchLog, baseURL, cleanup := startSandboxModeCodexServer(t, true)
	defer cleanup()

	workDir := t.TempDir()
	sessionID := createSandboxModeSession(t, baseURL, workDir, "")
	assertSessionSandboxMode(t, baseURL, sessionID, codingagent.SandboxModeDangerFullAccess)

	sendE2EMessage(t, baseURL, sessionID, "hello", 30*time.Second).Body.Close()
	assertLaunchArgs(t, launchLog, true, "")
}

func TestSandboxMode_ExplicitReadOnlyBeatsServerDisable(t *testing.T) {
	launchLog, baseURL, cleanup := startSandboxModeCodexServer(t, true)
	defer cleanup()

	workDir := t.TempDir()
	sessionID := createSandboxModeSession(t, baseURL, workDir, codingagent.SandboxModeReadOnly)
	assertSessionSandboxMode(t, baseURL, sessionID, codingagent.SandboxModeReadOnly)

	sendE2EMessage(t, baseURL, sessionID, "hello", 30*time.Second).Body.Close()
	assertLaunchArgs(t, launchLog, false, codingagent.SandboxModeReadOnly)
}

func TestSandboxMode_InvalidValue(t *testing.T) {
	_, baseURL, cleanup := startSandboxModeCodexServer(t, false)
	defer cleanup()

	workDir := t.TempDir()
	body, _ := json.Marshal(map[string]string{
		"agent":        "codex",
		"work_dir":     workDir,
		"sandbox_mode": "full-auto",
	})
	resp, err := http.Post(baseURL+"/api/v1/sessions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400, got %d: %s", resp.StatusCode, b)
	}
}

func startSandboxModeCodexServer(t *testing.T, disableSandbox bool) (launchLog, baseURL string, cleanup func()) {
	t.Helper()
	dir := t.TempDir()
	launchLog = filepath.Join(dir, "launch.log")
	testfake.Install(t, dir, testfake.Options{
		Lines: []string{
			`{"type":"item.completed","item":{"type":"agent_message","text":"ok"}}`,
			`{"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":1}}`,
		},
		LaunchLogPath: launchLog,
	})

	log := logger.NewDefault(logger.LevelInfo)
	opts := []agentservice.ServerOption{
		agentservice.WithLogger(log),
		agentservice.WithProcessRetry(config.ProcessRetryConfig{MaxAttempts: 1, IntervalSeconds: 0}),
		agentservice.WithSandboxDisabled(disableSandbox),
	}
	srv := agentservice.New(opts...)
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
	baseURL = "http://localhost:" + strconv.Itoa(port)
	cleanup = func() { _ = srv.Shutdown(context.Background()) }
	return launchLog, baseURL, cleanup
}

func createSandboxModeSession(t *testing.T, baseURL, workDir, sandboxMode string) string {
	t.Helper()
	payload := map[string]string{
		"agent":    "codex",
		"work_dir": workDir,
	}
	if sandboxMode != "" {
		payload["sandbox_mode"] = sandboxMode
	}
	body, _ := json.Marshal(payload)
	resp, err := http.Post(baseURL+"/api/v1/sessions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create session: expected 201, got %d: %s", resp.StatusCode, b)
	}
	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result["session_id"] == "" {
		t.Fatal("empty session_id")
	}
	return result["session_id"]
}

func assertSessionSandboxMode(t *testing.T, baseURL, sessionID, want string) {
	t.Helper()
	info := getE2ESession(t, baseURL, sessionID)
	got, _ := info["sandbox_mode"].(string)
	if got != want {
		t.Fatalf("sandbox_mode = %q, want %q", got, want)
	}
}

func assertLaunchArgs(t *testing.T, launchLog string, wantBypass bool, wantSandboxFlag string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var data []byte
	var err error
	for time.Now().Before(deadline) {
		data, err = os.ReadFile(launchLog)
		if err == nil && len(bytes.TrimSpace(data)) > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil || len(bytes.TrimSpace(data)) == 0 {
		t.Fatalf("launch log empty or unreadable: %v", err)
	}
	text := string(data)
	hasBypass := strings.Contains(text, "dangerously-bypass-approvals-and-sandbox")
	if hasBypass != wantBypass {
		t.Fatalf("bypass present=%v want=%v; log=%s", hasBypass, wantBypass, text)
	}
	if wantSandboxFlag != "" {
		needle := "-s " + wantSandboxFlag
		if !strings.Contains(text, needle) {
			t.Fatalf("expected %q in launch log: %s", needle, text)
		}
	}
}
