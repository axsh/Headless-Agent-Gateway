package agentservice

import (
	"strings"
	"testing"
	"time"
)

func TestDefaultSSEClientDrainTimeoutIs15s(t *testing.T) {
	if defaultSSEClientDrainTimeout != 15*time.Second {
		t.Fatalf("defaultSSEClientDrainTimeout = %s, want 15s", defaultSSEClientDrainTimeout)
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
