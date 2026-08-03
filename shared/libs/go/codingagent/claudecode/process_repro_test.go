package claudecode_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent/claudecode"
	"github.com/axsh/arctic-tern/shared/libs/go/logger"
)

func installFakeClaudeForProcessTest(t *testing.T, dir string, lines []string) {
	t.Helper()
	linesFile := filepath.Join(dir, "lines.jsonl")
	if err := os.WriteFile(linesFile, []byte(strings.Join(lines, "\n")), 0644); err != nil {
		t.Fatalf("write lines: %v", err)
	}
	mainSrc := `package main
import ("fmt"; "os"; "strings")
func main() {
 data, err := os.ReadFile(os.Getenv("FAKE_CLAUDE_LINES_FILE"))
 if err != nil { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
 for _, line := range strings.Split(string(data), "\n") {
  if line != "" { fmt.Println(line) }
 }
 os.Exit(0)
}`
	mainPath := filepath.Join(dir, "main.go")
	if err := os.WriteFile(mainPath, []byte(mainSrc), 0644); err != nil {
		t.Fatalf("write main: %v", err)
	}
	binName := "claude"
	if runtime.GOOS == "windows" {
		binName = "claude.exe"
	}
	cmd := exec.Command("go", "build", "-o", filepath.Join(dir, binName), mainPath)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fake claude: %v\n%s", err, out)
	}
	sep := string(os.PathListSeparator)
	t.Setenv("PATH", dir+sep+os.Getenv("PATH"))
	t.Setenv("FAKE_CLAUDE_LINES_FILE", linesFile)
}

func TestClaudeStartProcess_EmitsEventResultOnExitZero(t *testing.T) {
	dir := t.TempDir()
	padding := strings.Repeat("x", 65537)
	line2 := `{"type":"user","message":{"content":[{"type":"tool_result","content":"` + padding + `"}]}}`
	installFakeClaudeForProcessTest(t, dir, []string{
		`{"type":"system","subtype":"init","session_id":"s1"}`,
		line2,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"done"}]}}`,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ch, pm, err := claudecode.StartProcess(ctx, &codingagent.AdapterConfig{
		Logger: logger.NewDefault(logger.LevelInfo),
	}, &codingagent.SessionConfig{
		WorkDir:       t.TempDir(),
		ExecutionMode: codingagent.ExecutionModeSingleShot,
	})
	if err != nil {
		t.Fatalf("StartProcess: %v", err)
	}
	defer pm.Stop()

	resultCount := 0
	for ev := range ch {
		if ev.Type == codingagent.EventResult {
			resultCount++
		}
	}
	if resultCount != 1 {
		t.Fatalf("expected 1 EventResult, got %d", resultCount)
	}
}

func TestClaudeStartProcess_NoDuplicateEventResult(t *testing.T) {
	dir := t.TempDir()
	installFakeClaudeForProcessTest(t, dir, []string{
		`{"type":"result"}`,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ch, pm, err := claudecode.StartProcess(ctx, &codingagent.AdapterConfig{
		Logger: logger.NewDefault(logger.LevelInfo),
	}, &codingagent.SessionConfig{
		WorkDir:       t.TempDir(),
		ExecutionMode: codingagent.ExecutionModeSingleShot,
	})
	if err != nil {
		t.Fatalf("StartProcess: %v", err)
	}
	defer pm.Stop()

	resultCount := 0
	for ev := range ch {
		if ev.Type == codingagent.EventResult {
			resultCount++
		}
	}
	if resultCount != 1 {
		t.Fatalf("expected 1 EventResult, got %d", resultCount)
	}
}
