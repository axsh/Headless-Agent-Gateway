package client

import (
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestStream_Output(t *testing.T) {
	sseData := "data: {\"type\":\"text\",\"content\":\"Hello \"}\n\ndata: {\"type\":\"text\",\"content\":\"World\"}\n\ndata: [DONE]\n\n"
	body := io.NopCloser(strings.NewReader(sseData))
	stream := newStream(body)

	var buf strings.Builder
	err := stream.Output(&buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := buf.String(); got != "Hello World" {
		t.Errorf("output = %q, want %q", got, "Hello World")
	}
}

func TestStream_Output_Error(t *testing.T) {
	sseData := "data: {\"type\":\"text\",\"content\":\"partial\"}\n\ndata: {\"type\":\"error\",\"content\":\"something went wrong\"}\n\ndata: [DONE]\n\n"
	body := io.NopCloser(strings.NewReader(sseData))
	stream := newStream(body)

	var buf strings.Builder
	err := stream.Output(&buf)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "something went wrong") {
		t.Errorf("error = %v, want containing 'something went wrong'", err)
	}
	if got := buf.String(); got != "partial" {
		t.Errorf("output = %q, want %q", got, "partial")
	}
}

func TestStream_Output_ToolUse(t *testing.T) {
	sseData := "data: {\"type\":\"tool_use\",\"tool_name\":\"write_file\"}\n\ndata: {\"type\":\"tool_result\",\"content\":\"file created\"}\n\ndata: [DONE]\n\n"
	body := io.NopCloser(strings.NewReader(sseData))
	stream := newStream(body)

	var buf strings.Builder
	err := stream.Output(&buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "[Tool: write_file]") {
		t.Errorf("output missing tool use, got %q", got)
	}
	if !strings.Contains(got, "[Tool Result] file created") {
		t.Errorf("output missing tool result, got %q", got)
	}
}

func TestStream_Run_WithHandlers(t *testing.T) {
	sseData := "data: {\"type\":\"text\",\"content\":\"hello\"}\n\ndata: {\"type\":\"result\",\"content\":\"done\"}\n\ndata: [DONE]\n\n"
	body := io.NopCloser(strings.NewReader(sseData))
	stream := newStream(body)

	var texts []string
	var gotResult bool
	stream.OnText(func(text string) {
		texts = append(texts, text)
	}).OnResult(func(ev Event) {
		gotResult = true
	})

	err := stream.Run()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(texts) != 1 || texts[0] != "hello" {
		t.Errorf("texts = %v, want [\"hello\"]", texts)
	}
	if !gotResult {
		t.Error("result handler was not called")
	}
}

func TestStream_Events_Channel(t *testing.T) {
	sseData := "data: {\"type\":\"text\",\"content\":\"a\"}\n\ndata: {\"type\":\"text\",\"content\":\"b\"}\n\ndata: [DONE]\n\n"
	body := io.NopCloser(strings.NewReader(sseData))
	stream := newStream(body)

	var events []Event
	for ev := range stream.Events() {
		events = append(events, ev)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	if events[0].Text != "a" || events[1].Text != "b" {
		t.Errorf("events = %v, want a, b", events)
	}
}

func TestStream_Output_NodeEvents(t *testing.T) {
	sseData := `data: {"type":"node_start","content":"1: Setup"}
data: {"type":"node_complete","content":"1: Setup"}
data: {"type":"progress","content":"1/3"}
data: {"type":"node_failed","content":"2: Build - compile error"}
data: [DONE]
`
	body := io.NopCloser(strings.NewReader(sseData))
	stream := newStream(body)

	var buf strings.Builder
	err := stream.Output(&buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "[Node Start: 1: Setup]") {
		t.Errorf("missing node_start, got %q", got)
	}
	if !strings.Contains(got, "[Node Complete: 1: Setup]") {
		t.Errorf("missing node_complete, got %q", got)
	}
	if !strings.Contains(got, "[WBS 1/3]") {
		t.Errorf("missing progress, got %q", got)
	}
	if !strings.Contains(got, "[Node Failed: 2: Build - compile error]") {
		t.Errorf("missing node_failed, got %q", got)
	}
}

func TestStream_Output_KeepAliveIgnored(t *testing.T) {
	// Keepalive lines are SSE comments (starting with ':') and should be ignored.
	sseData := ": keepalive\n\ndata: {\"type\":\"text\",\"content\":\"hello\"}\n\n: keepalive\n\ndata: [DONE]\n\n"
	body := io.NopCloser(strings.NewReader(sseData))
	stream := newStream(body)

	var buf strings.Builder
	err := stream.Output(&buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := buf.String(); got != "hello" {
		t.Errorf("output = %q, want %q (keepalive should be ignored)", got, "hello")
	}
}

func TestStream_Output_IncompleteStream(t *testing.T) {
	// SSE stream that ends without [DONE] marker (simulates connection drop).
	sseData := "data: {\"type\":\"text\",\"content\":\"partial\"}\n\n"
	body := io.NopCloser(strings.NewReader(sseData))
	stream := newStream(body)

	var buf strings.Builder
	err := stream.Output(&buf)
	if err == nil {
		t.Fatal("expected error for incomplete stream, got nil")
	}
	if !strings.Contains(err.Error(), "stream terminated unexpectedly") {
		t.Errorf("error = %v, want containing 'stream terminated unexpectedly'", err)
	}
	// Partial text should still be written before the error.
	if got := buf.String(); got != "partial" {
		t.Errorf("output = %q, want %q", got, "partial")
	}
}

func TestStream_Output_ScannerError(t *testing.T) {
	// Simulate a read error from the response body.
	errReader := &errorReader{err: fmt.Errorf("connection reset")}
	body := io.NopCloser(errReader)
	stream := newStream(body)

	var buf strings.Builder
	err := stream.Output(&buf)
	if err == nil {
		t.Fatal("expected error for scanner failure, got nil")
	}
	if !strings.Contains(err.Error(), "stream read error") {
		t.Errorf("error = %v, want containing 'stream read error'", err)
	}
}

// errorReader is a test helper that always returns an error on Read.
type errorReader struct {
	err error
}

func (r *errorReader) Read(p []byte) (int, error) {
	return 0, r.err
}

func TestStream_Events_ToolUseIncludesToolInput(t *testing.T) {
	tests := []struct {
		name     string
		dataJSON string
		check    func(t *testing.T, evs []Event)
	}{
		{
			name:     "with_command",
			dataJSON: `{"type":"tool_use","tool_name":"command_execution","tool_input":{"command":"ls -la"}}`,
			check: func(t *testing.T, evs []Event) {
				t.Helper()
				ev := findLegacyEvent(t, evs, EventToolUse)
				if ev.ToolName != "command_execution" {
					t.Fatalf("ToolName = %q", ev.ToolName)
				}
				if ev.ToolInput["command"] != "ls -la" {
					t.Fatalf("ToolInput[command] = %v", ev.ToolInput["command"])
				}
			},
		},
		{
			name:     "missing_tool_input",
			dataJSON: `{"type":"tool_use","tool_name":"Bash"}`,
			check: func(t *testing.T, evs []Event) {
				t.Helper()
				ev := findLegacyEvent(t, evs, EventToolUse)
				if ev.ToolInput != nil {
					t.Fatalf("ToolInput = %#v, want nil", ev.ToolInput)
				}
			},
		},
		{
			name:     "empty_tool_input",
			dataJSON: `{"type":"tool_use","tool_name":"Bash","tool_input":{}}`,
			check: func(t *testing.T, evs []Event) {
				t.Helper()
				ev := findLegacyEvent(t, evs, EventToolUse)
				if ev.ToolInput == nil || len(ev.ToolInput) != 0 {
					t.Fatalf("ToolInput = %#v", ev.ToolInput)
				}
			},
		},
		{
			name:     "nested",
			dataJSON: `{"type":"tool_use","tool_name":"apply_patch","tool_input":{"changes":[{"path":"a.go"}]}}`,
			check: func(t *testing.T, evs []Event) {
				t.Helper()
				ev := findLegacyEvent(t, evs, EventToolUse)
				changes, ok := ev.ToolInput["changes"].([]any)
				if !ok || len(changes) != 1 {
					t.Fatalf("changes = %#v", ev.ToolInput["changes"])
				}
				item, ok := changes[0].(map[string]any)
				if !ok || item["path"] != "a.go" {
					t.Fatalf("changes[0] = %#v", changes[0])
				}
			},
		},
		{
			name:     "non_tool_use",
			dataJSON: `{"type":"text","content":"hi"}`,
			check: func(t *testing.T, evs []Event) {
				t.Helper()
				ev := findLegacyEvent(t, evs, EventText)
				if ev.ToolInput != nil {
					t.Fatalf("ToolInput = %#v, want nil", ev.ToolInput)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			payload := "data: " + tc.dataJSON + "\n\n" +
				"data: {\"type\":\"result\"}\n\n" +
				"data: [DONE]\n\n"
			stream := newStream(io.NopCloser(strings.NewReader(payload)))
			var evs []Event
			for ev := range stream.Events() {
				if ev.Type == EventError {
					t.Fatalf("unexpected error: %s", ev.Error)
				}
				evs = append(evs, ev)
			}
			tc.check(t, evs)
		})
	}
}

func TestStream_OnToolUse_RunReceivesToolInput(t *testing.T) {
	payload := "data: {\"type\":\"tool_use\",\"tool_name\":\"command_execution\",\"tool_input\":{\"command\":\"echo hi\"}}\n\n" +
		"data: {\"type\":\"result\"}\n\n" +
		"data: [DONE]\n\n"
	stream := newStream(io.NopCloser(strings.NewReader(payload)))

	var gotName string
	var gotInput map[string]any
	err := stream.OnToolUse(func(toolName string, toolInput map[string]any) {
		gotName = toolName
		gotInput = toolInput
	}).Run()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if gotName != "command_execution" {
		t.Fatalf("toolName = %q", gotName)
	}
	if gotInput["command"] != "echo hi" {
		t.Fatalf("toolInput = %#v", gotInput)
	}
}

func TestStream_Output_ToolUseSummaryCommandAndPath(t *testing.T) {
	tests := []struct {
		name       string
		dataJSON   string
		wantSubstr []string
		denySubstr []string
	}{
		{
			name:       "command",
			dataJSON:   `{"type":"tool_use","tool_name":"command_execution","tool_input":{"command":"ls -la"}}`,
			wantSubstr: []string{"[Tool: command_execution]", "command=ls -la"},
			denySubstr: []string{`"tool_input"`},
		},
		{
			name:       "path",
			dataJSON:   `{"type":"tool_use","tool_name":"Write","tool_input":{"path":"main.go"}}`,
			wantSubstr: []string{"[Tool: Write]", "path=main.go"},
			denySubstr: []string{"command="},
		},
		{
			name:       "command_over_path",
			dataJSON:   `{"type":"tool_use","tool_name":"X","tool_input":{"command":"x","path":"y"}}`,
			wantSubstr: []string{"command=x"},
			denySubstr: []string{"path=y"},
		},
		{
			name:       "no_summary_keys",
			dataJSON:   `{"type":"tool_use","tool_name":"SomeTool","tool_input":{"changes":[]}}`,
			wantSubstr: []string{"\n[Tool: SomeTool]\n"},
			denySubstr: []string{"command=", "path="},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			payload := "data: " + tc.dataJSON + "\n\ndata: [DONE]\n\n"
			stream := newStream(io.NopCloser(strings.NewReader(payload)))
			var buf strings.Builder
			if err := stream.Output(&buf); err != nil {
				t.Fatalf("Output: %v", err)
			}
			got := buf.String()
			for _, s := range tc.wantSubstr {
				if !strings.Contains(got, s) {
					t.Fatalf("output %q missing %q", got, s)
				}
			}
			for _, s := range tc.denySubstr {
				if strings.Contains(got, s) {
					t.Fatalf("output %q unexpectedly contains %q", got, s)
				}
			}
		})
	}
}

func findLegacyEvent(t *testing.T, evs []Event, typ EventType) Event {
	t.Helper()
	for _, ev := range evs {
		if ev.Type == typ {
			return ev
		}
	}
	t.Fatalf("event type %q not found in %#v", typ, evs)
	return Event{}
}

