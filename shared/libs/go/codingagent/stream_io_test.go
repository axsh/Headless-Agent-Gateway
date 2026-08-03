package codingagent_test

import (
	"strings"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

func TestTruncateToolResult_UnderLimit(t *testing.T) {
	content := strings.Repeat("a", 100)
	got := codingagent.TruncateToolResult(content, codingagent.DefaultMaxToolResultBytes)
	if got != content {
		t.Fatalf("expected unchanged content, got len %d", len(got))
	}
}

func TestTruncateToolResult_OverLimit(t *testing.T) {
	content := strings.Repeat("b", 300*1024)
	max := 256 * 1024
	got := codingagent.TruncateToolResult(content, max)
	if len(got) > max {
		t.Fatalf("truncated len %d exceeds max %d", len(got), max)
	}
	if !strings.Contains(got, "[truncated,") {
		t.Fatalf("expected truncation marker, got %q", got[len(got)-60:])
	}
}

func TestTruncateToolResult_MarkerFormat(t *testing.T) {
	content := strings.Repeat("c", 300*1024)
	got := codingagent.TruncateToolResult(content, 256*1024)
	wantSuffix := "\n... [truncated, 307200 bytes total]"
	if !strings.HasSuffix(got, wantSuffix) {
		t.Fatalf("suffix = %q, want %q", got[len(got)-len(wantSuffix):], wantSuffix)
	}
}

func TestNewLargeLineScanner_ReadsOversizedLine(t *testing.T) {
	line := strings.Repeat("z", 100*1024)
	reader := strings.NewReader(line + "\n")
	scanner := codingagent.NewLargeLineScanner(reader, 0)
	if !scanner.Scan() {
		t.Fatalf("failed to scan 100KB line: %v", scanner.Err())
	}
	if len(scanner.Text()) != 100*1024 {
		t.Fatalf("line len = %d, want %d", len(scanner.Text()), 100*1024)
	}
}

func TestNewLargeLineScanner_EnforcesSmallLimit(t *testing.T) {
	line := strings.Repeat("z", 200)
	reader := strings.NewReader(line + "\n")
	scanner := codingagent.NewLargeLineScanner(reader, 64)
	if scanner.Scan() {
		t.Fatalf("expected scan to fail for 200-byte line with 64-byte limit")
	}
	if scanner.Err() == nil {
		t.Fatal("expected scanner error")
	}
}
