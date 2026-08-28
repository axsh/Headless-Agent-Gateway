package llm_test

// Shared Claude / Codex E2E parity helpers.
// Used by agent-specific suites and by TestAgentParityMatrix_* (agent-swap matrix).

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

func assertParitySSEDone(t *testing.T, gotDone bool) {
	t.Helper()
	if !gotDone {
		t.Fatal("expected [DONE] sentinel in SSE stream")
	}
}

func assertParitySessionCompleted(t *testing.T, baseURL, sessionID string) {
	t.Helper()
	resp, err := http.Get(baseURL + "/api/v1/sessions/" + sessionID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get session status = %d", resp.StatusCode)
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	if body.Status != "completed" {
		t.Fatalf("session status = %q, want completed", body.Status)
	}
}

func fileCreatePrompt(workDir, fileName, contents string) string {
	abs, err := filepath.Abs(workDir)
	if err != nil {
		abs = workDir
	}
	target := filepath.Join(abs, fileName)
	return fmt.Sprintf(
		"Create a file at the absolute path %q containing exactly the text %q. "+
			"You must actually create the file on disk (apply_patch, shell redirect, or file write tools). "+
			"Do not write under /tmp. Do nothing else.",
		target, contents,
	)
}

func assertParityWorkFileExists(t *testing.T, workDir, fileName string, events []codingagent.StreamEvent) {
	t.Helper()
	primary := filepath.Join(workDir, fileName)
	if content, err := os.ReadFile(primary); err == nil {
		t.Logf("found work file %s (%d bytes)", primary, len(content))
		return
	}

	var candidates []string
	for _, ev := range events {
		if ev.Type == codingagent.EventToolUse {
			for _, key := range []string{"file_path", "path", "notebook_path"} {
				if v, ok := ev.ToolInput[key].(string); ok && v != "" {
					candidates = append(candidates, v)
				}
			}
		}
		if ev.Type == codingagent.EventToolResult && ev.Content != "" {
			candidates = append(candidates, ev.Content)
		}
	}

	for _, c := range candidates {
		if filepath.Base(c) != fileName && !strings.Contains(c, fileName) {
			continue
		}
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			t.Logf("found file via tool path %s", c)
			return
		}
		// Also accept same basename under workDir (model may report unix-style path).
		under := filepath.Join(workDir, filepath.Base(c))
		if st, err := os.Stat(under); err == nil && !st.IsDir() {
			t.Logf("found file under workDir %s (reported %s)", under, c)
			return
		}
	}

	entries, _ := os.ReadDir(workDir)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	t.Fatalf("expected %s under %s; workdir=%v tool_paths=%v", fileName, workDir, names, candidates)
}
