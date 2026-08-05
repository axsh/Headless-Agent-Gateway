// Package llm_test contains real Codex CLI E2E tests for large search output.
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

	v1 "github.com/axsh/arctic-tern/client/v1"
)

func TestCodexE2E_RealCLI_ClientV1_LargeSearchOutput(t *testing.T) {
	if _, err := exec.LookPath("codex"); err != nil {
		t.Skipf("codex CLI not found on PATH, skipping: %v", err)
	}

	baseURL, cleanup := startCodexE2EServer(t)
	defer cleanup()

	workDir := t.TempDir()
	for i := 0; i < 500; i++ {
		path := filepath.Join(workDir, fmt.Sprintf("file_%04d.txt", i))
		if err := os.WriteFile(path, []byte(strings.Repeat("SEARCHABLE_TOKEN\n", 200)), 0644); err != nil {
			t.Fatalf("write file: %v", err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	client := v1.New(baseURL, v1.WithNoTimeout())
	sess, err := client.CreateSession(ctx, v1.SessionRequest{
		Agent:   "codex",
		WorkDir: workDir,
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	prompt := "Search the workspace for all occurrences of SEARCHABLE_TOKEN using ripgrep or grep. Report the count, then finish."
	stream, err := sess.SendText(ctx, prompt)
	if err != nil {
		t.Fatalf("SendText: %v", err)
	}

	var gotResult bool
	err = stream.RunWithHandlers(ctx, sess, v1.StreamHandlers{
		OnResult: func() { gotResult = true },
	})
	if err != nil {
		if strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "upstream") {
			t.Skipf("Skipping: API unavailable: %v", err)
		}
		t.Fatalf("RunWithHandlers: %v", err)
	}
	if !gotResult {
		t.Fatal("expected EventResult")
	}

	session, err := client.GetSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	status := session.Status
	if status != "completed" {
		t.Fatalf("session status = %q, want completed", status)
	}
}
