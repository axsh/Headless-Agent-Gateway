package codingagent

import (
	"bufio"
	"fmt"
	"io"
)

const (
	// DefaultScannerMaxTokenSize is the default max JSONL line size for agent stdout scanners.
	DefaultScannerMaxTokenSize = 4 * 1024 * 1024 // 4MB
	// DefaultMaxToolResultBytes is the default max EventToolResult content size for SSE relay.
	DefaultMaxToolResultBytes = 256 * 1024 // 256KB
)

// NewLargeLineScanner returns a Scanner with configurable max token size.
// maxTokenSize <= 0 uses DefaultScannerMaxTokenSize.
func NewLargeLineScanner(r io.Reader, maxTokenSize int) *bufio.Scanner {
	s := bufio.NewScanner(r)
	limit := maxTokenSize
	if limit <= 0 {
		limit = DefaultScannerMaxTokenSize
	}
	// bufio.Scanner grows the token buffer up to cap(buf); keep cap aligned with limit
	// so small configured limits are enforced (see bufio.Scanner.Buffer).
	initialCap := limit
	const defaultInitialCap = 64 * 1024
	if initialCap > defaultInitialCap {
		initialCap = defaultInitialCap
	}
	buf := make([]byte, 0, initialCap)
	s.Buffer(buf, limit)
	return s
}

// TruncateToolResult truncates content to maxBytes for SSE/TaskLog relay.
// Appends "\n... [truncated, N bytes total]" when truncated.
// maxBytes <= 0 uses DefaultMaxToolResultBytes.
func TruncateToolResult(content string, maxBytes int) string {
	limit := maxBytes
	if limit <= 0 {
		limit = DefaultMaxToolResultBytes
	}
	if len(content) <= limit {
		return content
	}
	marker := fmt.Sprintf("\n... [truncated, %d bytes total]", len(content))
	markerLen := len(marker)
	if limit <= markerLen {
		return content[:limit]
	}
	return content[:limit-markerLen] + marker
}
