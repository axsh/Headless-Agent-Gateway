package agentservice

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/wayfinder/session"
)

func TestEventsToMessages_MergesConsecutiveText(t *testing.T) {
	msgs := EventsToMessages(session.OriginClaudeCode, []codingagent.StreamEvent{
		{Type: codingagent.EventText, Content: "he"},
		{Type: codingagent.EventText, Content: "llo"},
		{Type: codingagent.EventResult},
	})
	if len(msgs) != 1 {
		t.Fatalf("len = %d, want 1", len(msgs))
	}
	if msgs[0].Role != "assistant" || msgs[0].Content != "hello" {
		t.Errorf("msg = %+v", msgs[0])
	}
	if msgs[0].Origin != session.OriginClaudeCode {
		t.Errorf("Origin = %q", msgs[0].Origin)
	}
}

func TestEventsToMessages_ToolUseAndResult(t *testing.T) {
	msgs := EventsToMessages(session.OriginClaudeCode, []codingagent.StreamEvent{
		{Type: codingagent.EventText, Content: "reading"},
		{Type: codingagent.EventToolUse, ToolName: "Read", ToolInput: map[string]any{"file_path": "a.go"}},
		{Type: codingagent.EventToolResult, Content: "ok"},
		{Type: codingagent.EventResult},
	})
	if len(msgs) != 3 {
		t.Fatalf("len = %d, want 3: %+v", len(msgs), msgs)
	}
	if msgs[0].Role != "assistant" || msgs[0].Content != "reading" {
		t.Errorf("text msg = %+v", msgs[0])
	}
	if msgs[1].Role != "assistant" || len(msgs[1].ToolCalls) != 1 || msgs[1].ToolCalls[0].Name != "Read" {
		t.Errorf("tool_use msg = %+v", msgs[1])
	}
	if msgs[2].Role != "tool" || msgs[2].Content != "ok" {
		t.Errorf("tool result = %+v", msgs[2])
	}
}

func TestIngestTurn_AppendsWithoutDuplicatingUser(t *testing.T) {
	dir := t.TempDir()
	c := session.OpenCanonical(dir)
	if err := c.Init("s1", session.OriginClaudeCode); err != nil {
		t.Fatal(err)
	}
	if err := c.Append([]session.Message{{
		Role:      "user",
		Content:   "prompt",
		Origin:    session.OriginClaudeCode,
		Timestamp: time.Now(),
	}}); err != nil {
		t.Fatal(err)
	}
	if err := IngestTurn(dir, session.OriginClaudeCode, "sdk-1", []codingagent.StreamEvent{
		{Type: codingagent.EventText, Content: "answer"},
		{Type: codingagent.EventResult},
	}); err != nil {
		t.Fatal(err)
	}
	msgs, err := c.LoadRange(1, 0)
	if err != nil {
		t.Fatal(err)
	}
	users := 0
	assistants := 0
	for _, m := range msgs {
		if m.Role == "user" {
			users++
		}
		if m.Role == "assistant" {
			assistants++
		}
	}
	if users != 1 {
		t.Errorf("user count = %d, want 1", users)
	}
	if assistants < 1 {
		t.Errorf("assistant count = %d", assistants)
	}
	meta, err := c.LoadMetadata()
	if err != nil {
		t.Fatal(err)
	}
	b := meta.AgentBindings[session.OriginClaudeCode]
	if b.IngestedThroughSeq != meta.TotalSeq {
		t.Errorf("watermark = %d total=%d", b.IngestedThroughSeq, meta.TotalSeq)
	}
}

func TestIngestTurn_UpdatesNativeSessionID(t *testing.T) {
	dir := t.TempDir()
	if err := IngestTurn(dir, session.OriginClaudeCode, "", []codingagent.StreamEvent{
		{Type: codingagent.EventSystem, SessionID: "abc-123"},
		{Type: codingagent.EventText, Content: "hi"},
		{Type: codingagent.EventResult},
	}); err != nil {
		t.Fatal(err)
	}
	meta, err := session.OpenCanonical(dir).LoadMetadata()
	if err != nil {
		t.Fatal(err)
	}
	if meta.AgentBindings[session.OriginClaudeCode].AgentSessionID != "abc-123" {
		t.Errorf("bindings = %+v", meta.AgentBindings)
	}
	if _, err := os.Stat(filepath.Join(dir, "history")); err != nil {
		t.Fatal(err)
	}
}
