package agentservice

import (
	"strings"
	"testing"
	"time"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

func TestResolveTerminalFromRelayEvents(t *testing.T) {
	result := resolveTerminalFromRelayEvents([]codingagent.StreamEvent{
		{Type: codingagent.EventToolResult, Content: "rejected"},
	})
	if result.kind != codingagent.EventError {
		t.Fatalf("kind = %v, want error", result.kind)
	}
	if result.content != streamEndedWithoutTerminalContent {
		t.Fatalf("content = %q", result.content)
	}

	result = resolveTerminalFromRelayEvents([]codingagent.StreamEvent{
		{Type: codingagent.EventToolUse},
		{Type: codingagent.EventToolResult, Content: "x"},
		{Type: codingagent.EventResult},
	})
	if result.kind != codingagent.EventResult {
		t.Fatalf("kind = %v, want result", result.kind)
	}

	result = resolveTerminalFromRelayEvents([]codingagent.StreamEvent{
		{Type: codingagent.EventError, Content: "fatal", Retryable: false},
	})
	if result.kind != codingagent.EventError || result.retryable {
		t.Fatalf("got %+v", result)
	}
}

func TestEnsureRelayTerminal_PrefersExistingNonRetryable(t *testing.T) {
	term := ensureRelayTerminal(streamTerminal{
		kind:    codingagent.EventError,
		content: "already",
	}, nil)
	if term.content != "already" {
		t.Fatalf("content = %q", term.content)
	}
}

func TestEnsureRelayTerminal_FromRelaySnapshot(t *testing.T) {
	ch := make(chan codingagent.StreamEvent, 2)
	ch <- codingagent.StreamEvent{Type: codingagent.EventToolResult, Content: "Rejected"}
	close(ch)
	relay := newEventRelay(ch)
	time.Sleep(50 * time.Millisecond)

	term := ensureRelayTerminal(streamTerminal{}, relay)
	if term.kind != codingagent.EventError {
		t.Fatalf("kind = %v, want error", term.kind)
	}
	if term.content != streamEndedWithoutTerminalContent {
		t.Fatalf("content = %q", term.content)
	}
}

func TestDefaultSSEClientDrainTimeoutIs90s(t *testing.T) {
	if defaultSSEClientDrainTimeout != 90*time.Second {
		t.Fatalf("defaultSSEClientDrainTimeout = %s, want 90s", defaultSSEClientDrainTimeout)
	}
}

func TestTruncateStderrTail(t *testing.T) {
	if got := truncateStderrTail("abc", 8*1024); got != "abc" {
		t.Fatalf("short = %q", got)
	}
	long := strings.Repeat("x", 8*1024+10)
	got := truncateStderrTail(long, 8*1024)
	if len(got) != 8*1024 {
		t.Fatalf("len(got) = %d, want %d", len(got), 8*1024)
	}
	if got != long[10:] {
		t.Fatal("did not keep the tail")
	}
}

func TestParseExitStatus(t *testing.T) {
	st, ok := parseExitStatus("codex CLI process exited with error (exit status 1)")
	if !ok || st != "1" {
		t.Fatalf("got %q ok=%v, want 1", st, ok)
	}
	if _, ok := parseExitStatus(""); ok {
		t.Fatal("empty should not parse")
	}
}
