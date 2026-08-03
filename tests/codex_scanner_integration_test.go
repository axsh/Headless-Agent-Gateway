// Package llm_test contains integration tests for Codex stdout scanner limits.
package llm_test

import (
	"context"
	"testing"
	"time"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent/codex"
	"github.com/axsh/arctic-tern/shared/libs/go/logger"
	"github.com/axsh/arctic-tern/tests/testutil"
)

func TestCodexScannerIntegration_LargeOutputMissingEventResult(t *testing.T) {
	dir := t.TempDir()
	testutil.InstallFakeCodex(t, dir, testutil.FakeCodexOptions{ExitCode: 0})
	testutil.PrependPath(t, dir)

	workDir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ac := &codingagent.AdapterConfig{
		Logger: logger.NewDefault(logger.LevelInfo),
	}
	cfg := &codingagent.SessionConfig{
		WorkDir:       workDir,
		ExecutionMode: codingagent.ExecutionModeSingleShot,
	}

	ch, pm, err := codex.StartProcess(ctx, ac, cfg, nil, "")
	if err != nil {
		t.Fatalf("StartProcess: %v", err)
	}
	defer pm.Stop()

	var events []codingagent.StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}

	resultCount := 0
	for _, ev := range events {
		if ev.Type == codingagent.EventResult {
			resultCount++
		}
	}
	if resultCount != 1 {
		t.Fatalf("expected 1 EventResult, got %d (events=%d)", resultCount, len(events))
	}
}
