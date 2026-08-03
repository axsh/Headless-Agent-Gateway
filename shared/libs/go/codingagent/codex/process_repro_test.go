package codex_test

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
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent/codex"
	"github.com/axsh/arctic-tern/shared/libs/go/logger"
)

func installFakeCodexForProcessTest(t *testing.T, dir string, lines []string) {
	t.Helper()
	linesFile := filepath.Join(dir, "lines.jsonl")
	if err := os.WriteFile(linesFile, []byte(strings.Join(lines, "\n")), 0644); err != nil {
		t.Fatalf("write lines: %v", err)
	}
	mainSrc := `package main
import ("fmt"; "os"; "strings")
func main() {
 for _, arg := range os.Args[1:] {
  if arg == "--version" || arg == "-V" { fmt.Println("fake-codex 0.0.0"); os.Exit(0) }
 }
 hasExec := false
 for _, arg := range os.Args[1:] { if arg == "exec" { hasExec = true; break } }
 if !hasExec { os.Exit(0) }
 data, err := os.ReadFile(os.Getenv("FAKE_CODEX_LINES_FILE"))
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
	binName := "codex"
	if runtime.GOOS == "windows" {
		binName = "codex.exe"
	}
	cmd := exec.Command("go", "build", "-o", filepath.Join(dir, binName), mainPath)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fake codex: %v\n%s", err, out)
	}
	sep := string(os.PathListSeparator)
	t.Setenv("PATH", dir+sep+os.Getenv("PATH"))
	t.Setenv("FAKE_CODEX_LINES_FILE", linesFile)
}

func TestStartProcess_EmitsEventResultOnExitZero(t *testing.T) {
	dir := t.TempDir()
	padding := strings.Repeat("x", 65537)
	line2 := `{"type":"item.completed","item":{"type":"command_execution","aggregated_output":"` + padding + `"}}`
	installFakeCodexForProcessTest(t, dir, []string{
		`{"type":"item.started"}`,
		line2,
		`{"type":"item.completed"}`,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ch, pm, err := codex.StartProcess(ctx, &codingagent.AdapterConfig{
		Logger: logger.NewDefault(logger.LevelInfo),
	}, &codingagent.SessionConfig{
		WorkDir:       t.TempDir(),
		ExecutionMode: codingagent.ExecutionModeSingleShot,
	}, nil, "")
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

func TestStartProcess_ScannerErrorEmitsEventError(t *testing.T) {
	dir := t.TempDir()
	padding := strings.Repeat("x", 200)
	line := `{"type":"item.completed","aggregated_output":"` + padding + `"}`
	installFakeCodexForProcessTest(t, dir, []string{line})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ch, pm, err := codex.StartProcess(ctx, &codingagent.AdapterConfig{
		Logger: logger.NewDefault(logger.LevelInfo),
	}, &codingagent.SessionConfig{
		WorkDir:              t.TempDir(),
		ExecutionMode:        codingagent.ExecutionModeSingleShot,
		ScannerMaxTokenBytes: 64,
	}, nil, "")
	if err != nil {
		t.Fatalf("StartProcess: %v", err)
	}
	defer pm.Stop()

	var errorCount int
	for ev := range ch {
		if ev.Type == codingagent.EventError && strings.Contains(ev.Content, "stdout read error") {
			errorCount++
		}
	}
	if errorCount == 0 {
		t.Fatal("expected EventError with stdout read error, got none")
	}
}

func TestStartProcess_NoDuplicateEventResult(t *testing.T) {
	dir := t.TempDir()
	installFakeCodexForProcessTest(t, dir, []string{
		`{"type":"turn.completed"}`,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ch, pm, err := codex.StartProcess(ctx, &codingagent.AdapterConfig{
		Logger: logger.NewDefault(logger.LevelInfo),
	}, &codingagent.SessionConfig{
		WorkDir:       t.TempDir(),
		ExecutionMode: codingagent.ExecutionModeSingleShot,
	}, nil, "")
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
