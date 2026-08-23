package codex_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent/codex"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent/codex/testfake"
	"github.com/axsh/arctic-tern/shared/libs/go/logger"
)

func installFakeCodexForProcessTest(t *testing.T, dir string, lines []string) {
	t.Helper()
	testfake.Install(t, dir, testfake.Options{Lines: lines})
}

func TestStartProcess_EmitsEventResultOnExitZero(t *testing.T) {
	dir := t.TempDir()
	padding := strings.Repeat("x", 65537)
	line2 := `{"type":"item.completed","item":{"type":"command_execution","aggregated_output":"` + padding + `"}}`
	testfake.Install(t, dir, testfake.Options{
		Lines: []string{
			`{"type":"item.started"}`,
			line2,
			`{"type":"item.completed"}`,
		},
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
	testfake.Install(t, dir, testfake.Options{Lines: []string{line}})

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
	testfake.Install(t, dir, testfake.Options{
		Lines: []string{
			`{"type":"turn.completed"}`,
		},
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

func TestStartProcess_InProcessRetryableThenResult(t *testing.T) {
	tests := []struct {
		name      string
		errorLine string
	}{
		{
			name:      "errorJSONL",
			errorLine: `{"type":"error","message":"Reconnecting... 1/5 (We're currently experiencing high demand, which may cause temporary errors.)"}`,
		},
		{
			name:      "turnFailed",
			errorLine: `{"type":"turn.failed","error":{"message":"We're currently experiencing high demand"}}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			launchLog := filepath.Join(dir, "launch.log")
			testfake.Install(t, dir, testfake.Options{
				Lines: []string{
					`{"type":"thread.started","thread_id":"thr-test"}`,
					tc.errorLine,
					`{"type":"turn.completed"}`,
				},
				LineDelay:     60 * time.Millisecond,
				LaunchLogPath: launchLog,
				ExitCode:      0,
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

			var errors, results int
			for ev := range ch {
				if ev.Type == codingagent.EventError {
					errors++
				}
				if ev.Type == codingagent.EventResult {
					results++
				}
			}
			if errors != 0 {
				t.Fatalf("expected 0 EventError on in-process retryable JSONL, got %d", errors)
			}
			if results != 1 {
				t.Fatalf("expected 1 EventResult, got %d", results)
			}
			if count := testfake.LaunchCount(t, launchLog); count != 1 {
				t.Fatalf("launch count = %d, want 1", count)
			}
		})
	}
}

func TestStartProcess_ReconnectStderrDoesNotEmitEventErrorOnSuccess(t *testing.T) {
	dir := t.TempDir()
	testfake.Install(t, dir, testfake.Options{
		Lines: []string{
			`{"type":"turn.completed"}`,
		},
		Stderr:   "Reconnecting... 1/5 (We're currently experiencing high demand, which may cause temporary errors.)",
		ExitCode: 0,
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

	var errors, results int
	for ev := range ch {
		if ev.Type == codingagent.EventError {
			errors++
		}
		if ev.Type == codingagent.EventResult {
			results++
		}
	}
	if errors != 0 {
		t.Fatalf("expected no EventError on successful reconnect, got %d", errors)
	}
	if results != 1 {
		t.Fatalf("expected 1 EventResult, got %d", results)
	}
}

func TestStartProcess_GenericExit1IsRetryable(t *testing.T) {
	dir := t.TempDir()
	testfake.Install(t, dir, testfake.Options{
		ExitCode: 1,
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

	var last codingagent.StreamEvent
	var saw bool
	for ev := range ch {
		if ev.Type == codingagent.EventError {
			last = ev
			saw = true
		}
	}
	if !saw {
		t.Fatal("expected EventError on generic exit 1")
	}
	if !last.Retryable {
		t.Fatal("Retryable = false, want true")
	}
	if !strings.Contains(last.Content, "exit status 1") {
		t.Errorf("Content = %q, want exit status 1", last.Content)
	}
}

func TestStartProcess_EmptyStderrUsesStdoutError(t *testing.T) {
	dir := t.TempDir()
	stdoutErr := "unexpected status 404 Not Found: model not found: gpt-4o"
	testfake.Install(t, dir, testfake.Options{
		Lines: []string{
			`{"type":"error","message":"unexpected status 404 Not Found: model not found: gpt-4o"}`,
		},
		ExitCode: 1,
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

	var last codingagent.StreamEvent
	var saw bool
	for ev := range ch {
		if ev.Type == codingagent.EventError {
			last = ev
			saw = true
		}
	}
	if !saw {
		t.Fatal("expected EventError from empty-stderr exit 1")
	}
	if last.Retryable {
		t.Fatal("Retryable = true, want false")
	}
	if !strings.Contains(last.Content, stdoutErr) {
		t.Errorf("Content = %q, want to contain %q", last.Content, stdoutErr)
	}
	if last.Content == "exit status 1" || strings.TrimSpace(last.Content) == "exit status 1" {
		t.Errorf("Content fell back to Wait() only: %q", last.Content)
	}
}

func TestStartProcess_RetryableExitSetsRetryableFlag(t *testing.T) {
	dir := t.TempDir()
	stderr := "Reconnecting... 1/5 (We're currently experiencing high demand, which may cause temporary errors.)"
	testfake.Install(t, dir, testfake.Options{
		Stderr:   stderr,
		ExitCode: 1,
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

	var last codingagent.StreamEvent
	var saw bool
	for ev := range ch {
		if ev.Type == codingagent.EventError {
			last = ev
			saw = true
		}
	}
	if !saw {
		t.Fatal("expected EventError on retryable exit")
	}
	if !last.Retryable {
		t.Fatal("Retryable = false, want true")
	}
	want := codingagent.ClassifiedErrorContent(stderr, true)
	if last.Content != want {
		t.Errorf("Content = %q, want %q", last.Content, want)
	}
}

func TestStartProcess_NonRetryableExitNoRetryableFlag(t *testing.T) {
	dir := t.TempDir()
	testfake.Install(t, dir, testfake.Options{
		Stderr:   "unauthorized",
		ExitCode: 1,
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

	var last codingagent.StreamEvent
	var saw bool
	for ev := range ch {
		if ev.Type == codingagent.EventError {
			last = ev
			saw = true
		}
	}
	if !saw {
		t.Fatal("expected EventError")
	}
	if last.Retryable {
		t.Fatal("Retryable = true, want false")
	}
	if !strings.Contains(last.Content, "unauthorized") {
		t.Errorf("Content = %q, want unauthorized", last.Content)
	}
}

func TestStartProcess_SandboxRejectionSynthesizesToolResult(t *testing.T) {
	dir := t.TempDir()
	stderr := "ERROR codex_core::tools::router: exec_command failed: Rejected(\"rm -f style commands are not permitted\")"
	testfake.Install(t, dir, testfake.Options{
		Lines: []string{
			`{"type":"item.started","item":{"type":"command_execution","command":"rm -f /tmp/check.html"}}`,
		},
		Stderr:   stderr,
		ExitCode: 1,
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

	var toolResults int
	var retryableErrors int
	for ev := range ch {
		if ev.Type == codingagent.EventToolResult {
			toolResults++
			if !strings.Contains(ev.Content, "Rejected") && !strings.Contains(ev.Content, "rm -f") {
				t.Errorf("tool result content = %q, want rejection text", ev.Content)
			}
		}
		if ev.Type == codingagent.EventError && ev.Retryable {
			retryableErrors++
		}
	}
	if toolResults != 1 {
		t.Fatalf("expected 1 EventToolResult, got %d", toolResults)
	}
	if retryableErrors != 0 {
		t.Fatalf("expected 0 retryable EventError, got %d", retryableErrors)
	}
}

func TestStartProcess_SandboxRejectionNoDuplicateToolResult(t *testing.T) {
	dir := t.TempDir()
	stderr := "ERROR Rejected(\"rm -f style commands are not permitted\")"
	testfake.Install(t, dir, testfake.Options{
		Lines: []string{
			`{"type":"item.started","item":{"type":"command_execution","command":"rm -f /tmp/x"}}`,
			`{"type":"item.completed","item":{"type":"command_execution","command":"rm -f /tmp/x","aggregated_output":"already rejected"}}`,
		},
		Stderr:   stderr,
		ExitCode: 1,
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

	var toolResults int
	for ev := range ch {
		if ev.Type == codingagent.EventToolResult {
			toolResults++
		}
	}
	if toolResults != 1 {
		t.Fatalf("expected 1 EventToolResult from stdout, got %d", toolResults)
	}
}

func TestStartProcess_PolicyTextRejectionSynthesizesToolResult(t *testing.T) {
	dir := t.TempDir()
	testfake.Install(t, dir, testfake.Options{
		Lines: []string{
			`{"type":"item.started","item":{"type":"command_execution","command":"rm -f /tmp/x"}}`,
			`{"type":"item.completed","item":{"type":"agent_message","text":"It was rejected by the environment policy (blocked by policy)."}}`,
		},
		ExitCode: 0,
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

	var toolResults int
	for ev := range ch {
		if ev.Type == codingagent.EventToolResult {
			toolResults++
			if !strings.Contains(strings.ToLower(ev.Content), "blocked by policy") {
				t.Errorf("tool result content = %q, want policy rejection text", ev.Content)
			}
		}
	}
	if toolResults != 1 {
		t.Fatalf("expected 1 EventToolResult, got %d", toolResults)
	}
}
