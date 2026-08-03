package testutil

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// FakeCodexOptions configures stdout JSONL emitted by the fake codex binary.
type FakeCodexOptions struct {
	Lines    []string
	ExitCode int
	DelayMS  int
}

// BuildThreeLineReproLines returns Issue #24 minimal repro JSONL lines.
func BuildThreeLineReproLines() []string {
	padding := strings.Repeat("x", 65537)
	line2 := fmt.Sprintf(
		`{"type":"item.completed","item":{"type":"command_execution","aggregated_output":"%s"}}`,
		padding,
	)
	return []string{
		`{"type":"item.started"}`,
		line2,
		`{"type":"item.completed"}`,
	}
}

func commandExecutionLine(content string) (string, error) {
	payload, err := json.Marshal(map[string]any{
		"type": "item.completed",
		"item": map[string]any{
			"type":              "command_execution",
			"aggregated_output": content,
		},
	})
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

// BuildLargeAggregatedOutputLines returns JSONL with aggregated_output of exact byte size.
func BuildLargeAggregatedOutputLines(contentBytes int) []string {
	content := strings.Repeat("x", contentBytes)
	return BuildAggregatedOutputLinesFromContent(content)
}

// BuildAggregatedOutputLinesFromContent returns JSONL embedding arbitrary content.
func BuildAggregatedOutputLinesFromContent(content string) []string {
	line, err := commandExecutionLine(content)
	if err != nil {
		panic(err)
	}
	return []string{
		`{"type":"item.started"}`,
		line,
		`{"type":"item.completed"}`,
	}
}

// BuildDelayedLargeOutputLines returns JSONL for large output; use FakeCodexOptions.DelayMS for delay.
func BuildDelayedLargeOutputLines(contentBytes int) []string {
	return BuildLargeAggregatedOutputLines(contentBytes)
}

// BuildRipgrepLikeOutput returns multi-line search-style output of at least minBytes.
func BuildRipgrepLikeOutput(minBytes int) string {
	var b strings.Builder
	for i := 0; b.Len() < minBytes; i++ {
		if i%100 == 0 && i > 0 {
			b.WriteString("./docs/日本語/ファイル.go:1:マッチ\n")
		}
		fmt.Fprintf(&b, "./src/module_%d/foo.go:%d:match keyword\n", i/100, i%500)
	}
	return b.String()
}

// BuildMultiToolOutputLines returns JSONL with N consecutive oversized tool outputs.
func BuildMultiToolOutputLines(sizes ...int) []string {
	lines := []string{`{"type":"item.started"}`}
	runes := []byte("abcdefghijklmnopqrstuvwxyz")
	for i, size := range sizes {
		r := rune(runes[i%len(runes)])
		content := strings.Repeat(string(r), size)
		line, err := commandExecutionLine(content)
		if err != nil {
			panic(err)
		}
		lines = append(lines, line)
	}
	lines = append(lines, `{"type":"item.completed"}`)
	return lines
}

// InstallFakeCodex creates a fake "codex" executable in dir and returns its path.
func InstallFakeCodex(t *testing.T, dir string, opts FakeCodexOptions) string {
	t.Helper()

	if len(opts.Lines) == 0 {
		opts.Lines = BuildThreeLineReproLines()
	}
	exitCode := opts.ExitCode
	delayMS := opts.DelayMS

	linesFile := filepath.Join(dir, "lines.jsonl")
	var b strings.Builder
	for i, line := range opts.Lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(line)
	}
	if err := os.WriteFile(linesFile, []byte(b.String()), 0644); err != nil {
		t.Fatalf("write lines file: %v", err)
	}

	mainSrc := fmt.Sprintf(`package main

import (
	"fmt"
	"os"
	"strings"
	"time"
)

func main() {
	for _, arg := range os.Args[1:] {
		if arg == "--version" || arg == "-V" {
			fmt.Println("fake-codex 0.0.0")
			os.Exit(0)
		}
	}
	hasExec := false
	for _, arg := range os.Args[1:] {
		if arg == "exec" {
			hasExec = true
			break
		}
	}
	if !hasExec {
		os.Exit(0)
	}
	path := os.Getenv("FAKE_CODEX_LINES_FILE")
	if path == "" {
		fmt.Fprintln(os.Stderr, "FAKE_CODEX_LINES_FILE not set")
		os.Exit(1)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if line != "" {
			fmt.Println(line)
		}
	}
	if %d > 0 {
		time.Sleep(time.Duration(%d) * time.Millisecond)
	}
	os.Exit(%d)
}
`, delayMS, delayMS, exitCode)

	mainPath := filepath.Join(dir, "main.go")
	if err := os.WriteFile(mainPath, []byte(mainSrc), 0644); err != nil {
		t.Fatalf("write fake codex main: %v", err)
	}

	binName := "codex"
	if runtime.GOOS == "windows" {
		binName = "codex.exe"
	}
	binPath := filepath.Join(dir, binName)

	cmd := exec.Command("go", "build", "-o", binPath, mainPath)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fake codex: %v\n%s", err, out)
	}
	return binPath
}

// PrependPath prepends dir to PATH for subprocess lookup.
func PrependPath(t *testing.T, dir string) {
	t.Helper()
	sep := string(os.PathListSeparator)
	old := os.Getenv("PATH")
	t.Setenv("PATH", dir+sep+old)
	t.Setenv("FAKE_CODEX_LINES_FILE", filepath.Join(dir, "lines.jsonl"))
}
