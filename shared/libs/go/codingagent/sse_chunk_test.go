package codingagent_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

func TestSplitStreamEventForSSE_SmallPayloadNoSplit(t *testing.T) {
	ev := codingagent.StreamEvent{
		Type:    codingagent.EventToolResult,
		Content: strings.Repeat("a", 100),
	}
	events, err := codingagent.SplitStreamEventForSSE(ev, 0)
	if err != nil {
		t.Fatalf("SplitStreamEventForSSE: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(events))
	}
	if events[0].Type != codingagent.EventToolResult {
		t.Fatalf("type = %q, want tool_result", events[0].Type)
	}
	if events[0].Content != ev.Content {
		t.Fatalf("content changed after no-split path")
	}
}

func TestSplitStreamEventForSSE_LargePayloadChunksUnder64KB(t *testing.T) {
	ev := codingagent.StreamEvent{
		Type:    codingagent.EventToolResult,
		Content: strings.Repeat("b", codingagent.DefaultMaxToolResultBytes),
	}
	events, err := codingagent.SplitStreamEventForSSE(ev, 0)
	if err != nil {
		t.Fatalf("SplitStreamEventForSSE: %v", err)
	}
	if len(events) < 2 {
		t.Fatalf("len(events) = %d, want at least 2", len(events))
	}

	partCount := 0
	for _, e := range events {
		if e.Type == codingagent.EventToolResultPart {
			partCount++
		}
		data, err := json.Marshal(e)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if len(data) >= codingagent.DefaultMaxSSEDataLineBytes {
			t.Fatalf("SSE wire line len %d >= max %d", len(data), codingagent.DefaultMaxSSEDataLineBytes)
		}
	}

	last := events[len(events)-1]
	if last.Type != codingagent.EventToolResult || last.Content != "" {
		t.Fatalf("last event = %+v, want empty tool_result completion marker", last)
	}
	if partCount == 0 {
		t.Fatal("expected at least one tool_result_part")
	}
}

func TestSplitStreamEventForSSE_ReassemblyRoundTrip(t *testing.T) {
	content := strings.Repeat("c", codingagent.DefaultMaxToolResultBytes)
	ev := codingagent.StreamEvent{
		Type:    codingagent.EventToolResult,
		Content: content,
	}
	events, err := codingagent.SplitStreamEventForSSE(ev, 0)
	if err != nil {
		t.Fatalf("SplitStreamEventForSSE: %v", err)
	}

	var parts []codingagent.StreamEvent
	var completion codingagent.StreamEvent
	for _, e := range events {
		switch e.Type {
		case codingagent.EventToolResultPart:
			parts = append(parts, e)
		case codingagent.EventToolResult:
			completion = e
		}
	}

	got, err := codingagent.ReassembleToolResultParts(parts)
	if err != nil {
		t.Fatalf("ReassembleToolResultParts: %v", err)
	}
	if got != content {
		t.Fatalf("reassembled len = %d, want %d", len(got), len(content))
	}
	if completion.ChunkID != parts[0].ChunkID {
		t.Fatalf("completion chunk_id = %q, want %q", completion.ChunkID, parts[0].ChunkID)
	}
}

func TestSplitStreamEventForSSE_NonToolResultPassthrough(t *testing.T) {
	cases := []codingagent.StreamEvent{
		{Type: codingagent.EventText, Content: "hello"},
		{Type: codingagent.EventResult},
	}
	for _, ev := range cases {
		events, err := codingagent.SplitStreamEventForSSE(ev, 0)
		if err != nil {
			t.Fatalf("SplitStreamEventForSSE(%q): %v", ev.Type, err)
		}
		if len(events) != 1 {
			t.Fatalf("len(events) = %d, want 1 for %q", len(events), ev.Type)
		}
		if events[0].Type != ev.Type || events[0].Content != ev.Content {
			t.Fatalf("passthrough mismatch for %q", ev.Type)
		}
	}
}
