package codex_test

import (
	"bufio"
	"fmt"
	"strings"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

func buildThreeLineReproReader() *strings.Reader {
	padding := strings.Repeat("x", 65537)
	line2 := fmt.Sprintf(
		`{"type":"item.completed","item":{"type":"command_execution","aggregated_output":"%s"}}`,
		padding,
	)
	content := strings.Join([]string{
		`{"type":"item.started"}`,
		line2,
		`{"type":"item.completed"}`,
	}, "\n") + "\n"
	return strings.NewReader(content)
}

func TestScanner_DefaultLimitStopsAt64KB(t *testing.T) {
	reader := buildThreeLineReproReader()
	var lines []string
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if len(lines) != 1 {
		t.Fatalf("expected 1 line before ErrTooLong, got %d lines", len(lines))
	}
	if err := scanner.Err(); err == nil {
		t.Fatal("expected scanner.Err() to be non-nil (bufio.ErrTooLong)")
	}
}

func TestScanner_LargeLineReadsAllThreeLines(t *testing.T) {
	reader := buildThreeLineReproReader()
	var lines []string
	scanner := codingagent.NewLargeLineScanner(reader, 0)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner error: %v", err)
	}
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
}
