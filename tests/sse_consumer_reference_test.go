// Package llm_test contains reference SSE consumer contract tests for chunked tool results.
package llm_test

import (
	"bufio"
	"encoding/json"
	"strings"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/tests/testutil"
)

func TestSSEConsumerReference_DefaultScannerReadsChunkedStream(t *testing.T) {
	lines := testutil.BuildLargeAggregatedOutputLines(codingagent.DefaultMaxToolResultBytes)
	baseURL, cleanup := startFakeCodexE2EServerWithLines(t, testutil.FakeCodexOptions{Lines: lines})
	defer cleanup()

	workDir := t.TempDir()
	sessionID := createE2ESessionNoModel(t, baseURL, "codex", workDir)

	resp := sendE2EMessage(t, baseURL, sessionID, "trigger", 0)
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	var parts []codingagent.StreamEvent
	var gotResult bool
	var chunkID string

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, ": ") {
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}
		if len(data) >= codingagent.DefaultMaxSSEDataLineBytes {
			t.Fatalf("scanner would fail: line len %d >= max", len(data))
		}

		var raw codingagent.StreamEvent
		if err := json.Unmarshal([]byte(data), &raw); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		switch raw.Type {
		case codingagent.EventToolResultPart:
			parts = append(parts, raw)
			if chunkID == "" {
				chunkID = raw.ChunkID
			}
		case codingagent.EventToolResult:
			if raw.Content == "" && raw.ChunkID != "" {
				// completion marker
				continue
			}
		case codingagent.EventResult:
			gotResult = true
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner error: %v", err)
	}

	assembled, err := codingagent.ReassembleToolResultParts(parts)
	if err != nil {
		t.Fatalf("ReassembleToolResultParts: %v", err)
	}
	if len(assembled) != codingagent.DefaultMaxToolResultBytes {
		t.Fatalf("assembled len = %d, want %d", len(assembled), codingagent.DefaultMaxToolResultBytes)
	}
	if !gotResult {
		t.Fatal("expected EventResult in SSE stream")
	}
}
