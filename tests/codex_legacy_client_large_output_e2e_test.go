// Package llm_test contains E2E tests for legacy client SSE with large tool output.
package llm_test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/axsh/arctic-tern/client"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/tests/testutil"
)

func consumeLegacySSEEvents(t *testing.T, body string) (toolResults []string, gotResult bool) {
	t.Helper()
	scanner := bufio.NewScanner(strings.NewReader(body))
	var parts []codingagent.StreamEvent
	var pendingChunkID string

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}
		var raw codingagent.StreamEvent
		if err := json.Unmarshal([]byte(data), &raw); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		switch raw.Type {
		case codingagent.EventToolResultPart:
			parts = append(parts, raw)
			pendingChunkID = raw.ChunkID
		case codingagent.EventToolResult:
			if raw.Content != "" {
				toolResults = append(toolResults, raw.Content)
				continue
			}
			if raw.ChunkID != "" {
				assembled, err := codingagent.ReassembleToolResultParts(parts)
				if err != nil {
					t.Fatalf("ReassembleToolResultParts: %v", err)
				}
				toolResults = append(toolResults, assembled)
				parts = nil
				pendingChunkID = ""
			}
		case codingagent.EventResult:
			gotResult = true
		case codingagent.EventError:
			t.Fatalf("stream error: %s", raw.Content)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner: %v", err)
	}
	if pendingChunkID != "" && len(parts) > 0 {
		t.Fatal("incomplete tool_result chunks at stream end")
	}
	return toolResults, gotResult
}

func TestCodexE2E_LegacyClient_MaxTruncatedToolOutputTerminalEvent(t *testing.T) {
	lines := testutil.BuildLargeAggregatedOutputLines(codingagent.DefaultMaxToolResultBytes)
	baseURL, cleanup := startFakeCodexE2EServerWithLines(t, testutil.FakeCodexOptions{Lines: lines})
	defer cleanup()

	ctx := context.Background()
	c := client.New(baseURL, client.WithNoTimeout())
	sess, err := c.CreateSession(ctx, client.SessionRequest{
		Agent:   "codex",
		WorkDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	resp := sendE2EMessage(t, baseURL, sess.ID, "trigger", 120*time.Second)
	defer resp.Body.Close()

	bodyStr, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read SSE: %v", err)
	}

	toolResults, gotResult := consumeLegacySSEEvents(t, string(bodyStr))

	wantLen := codingagent.DefaultMaxToolResultBytes
	if !gotResult {
		t.Fatal("expected EventResult")
	}
	if len(toolResults) != 1 {
		t.Fatalf("expected 1 tool_result, got %d", len(toolResults))
	}
	if len(toolResults[0]) != wantLen {
		t.Fatalf("tool_result len = %d, want %d", len(toolResults[0]), wantLen)
	}

	session, err := c.GetSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	status, _ := session["status"].(string)
	if status != "completed" {
		t.Fatalf("session status = %q, want completed", status)
	}
}
