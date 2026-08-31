package codex

import (
	"context"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

func TestCommandExecTracker_RealtimeStderrSynthesis(t *testing.T) {
	var tracker commandExecTracker
	ch := make(chan codingagent.StreamEvent, 4)
	ctx := context.Background()

	tracker.markToolUse()
	line := "ERROR codex_core::tools::router: exec_command failed: Rejected(\"rm -f style commands are not permitted\")"
	if !tracker.trySynthesizeFromStderrLine(ch, ctx, line, 0) {
		t.Fatal("expected realtime stderr synthesis")
	}

	ev := <-ch
	if ev.Type != codingagent.EventToolResult {
		t.Fatalf("type = %v, want tool_result", ev.Type)
	}
	if !tracker.synthesizedSandboxReject() {
		t.Fatal("expected synthesizedSandboxReject flag")
	}

	// Second attempt must not duplicate.
	if tracker.trySynthesizeFromStderrLine(ch, ctx, line, 0) {
		t.Fatal("expected no duplicate synthesis")
	}
}

func TestCommandExecTracker_StderrBeforeToolUse(t *testing.T) {
	var tracker commandExecTracker
	ch := make(chan codingagent.StreamEvent, 4)
	ctx := context.Background()
	line := "ERROR codex_core::tools::router: exec_command failed: Rejected(\"rm -f style commands are not permitted\")"

	if tracker.trySynthesizeFromStderrLine(ch, ctx, line, 0) {
		t.Fatal("expected no synthesis before tool_use")
	}
	if tracker.tryFlushPendingRejection(ch, ctx, 0) {
		t.Fatal("expected no flush before tool_use")
	}

	tracker.markToolUse()
	if !tracker.tryFlushPendingRejection(ch, ctx, 0) {
		t.Fatal("expected flush after tool_use")
	}

	ev := <-ch
	if ev.Type != codingagent.EventToolResult {
		t.Fatalf("type = %v, want tool_result", ev.Type)
	}
}

func TestCommandExecTracker_HasOpenToolUse(t *testing.T) {
	var tr commandExecTracker
	if tr.hasOpenToolUse() {
		t.Fatal("empty tracker should not have open tool use")
	}
	tr.markToolUse()
	if !tr.hasOpenToolUse() {
		t.Fatal("after markToolUse want open")
	}
	tr.markToolResult()
	if tr.hasOpenToolUse() {
		t.Fatal("after markToolResult want closed")
	}
}
