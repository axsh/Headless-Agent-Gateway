package v1_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	v1 "github.com/axsh/arctic-tern/client/v1"
)

func TestRunWithHandlers_UserInputRequiredLoop(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "text/event-stream")
		if strings.HasSuffix(r.URL.Path, "/messages") {
			fmt.Fprintf(w, "data: {\"type\":\"user_input_required\",\"content\":\"pick\",\"choices\":[\"a\"]}\n\n")
			fmt.Fprintf(w, "data: [DONE]\n\n")
			return
		}
		if strings.HasSuffix(r.URL.Path, "/respond") {
			fmt.Fprintf(w, "data: {\"type\":\"text\",\"content\":\"ok\"}\n\n")
			fmt.Fprintf(w, "data: {\"type\":\"result\"}\n\n")
			fmt.Fprintf(w, "data: [DONE]\n\n")
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := v1.New(srv.URL, v1.WithNoTimeout())
	sess := v1.ResumeSession(client, "sess-1")
	stream, err := sess.SendText(context.Background(), "hi")
	if err != nil {
		t.Fatalf("SendText: %v", err)
	}

	var texts []string
	err = stream.RunWithHandlers(context.Background(), sess, v1.StreamHandlers{
		OnUserInputRequired: func(ev v1.UserInputRequiredEvent) (string, error) {
			return "answer", nil
		},
		OnText: func(text string) { texts = append(texts, text) },
	})
	if err != nil {
		t.Fatalf("RunWithHandlers: %v", err)
	}
	if callCount < 2 {
		t.Fatalf("expected respond call, callCount=%d", callCount)
	}
	if len(texts) != 1 || texts[0] != "ok" {
		t.Fatalf("texts = %v", texts)
	}
}

func TestRunWithHandlers_MissingHandlerFailsFast(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"type\":\"user_input_required\",\"content\":\"pick\"}\n\n")
		fmt.Fprintf(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	client := v1.New(srv.URL, v1.WithNoTimeout())
	sess := v1.ResumeSession(client, "sess-2")
	stream, err := sess.SendText(context.Background(), "hi")
	if err != nil {
		t.Fatalf("SendText: %v", err)
	}

	err = stream.RunWithHandlers(context.Background(), sess, v1.StreamHandlers{})
	if err == nil || !strings.Contains(err.Error(), "no handler configured") {
		t.Fatalf("err = %v", err)
	}
}

func TestStream_Events_ReassemblesToolResultParts(t *testing.T) {
	chunkID := "test-chunk-id"
	part0 := strings.Repeat("a", 100)
	part1 := strings.Repeat("b", 100)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"type\":\"tool_result_part\",\"chunk_id\":\"%s\",\"index\":0,\"total\":2,\"content\":\"%s\"}\n\n", chunkID, part0)
		fmt.Fprintf(w, "data: {\"type\":\"tool_result_part\",\"chunk_id\":\"%s\",\"index\":1,\"total\":2,\"content\":\"%s\"}\n\n", chunkID, part1)
		fmt.Fprintf(w, "data: {\"type\":\"tool_result\",\"chunk_id\":\"%s\",\"content\":\"\"}\n\n", chunkID)
		fmt.Fprintf(w, "data: {\"type\":\"result\"}\n\n")
		fmt.Fprintf(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()

	stream := v1.NewStreamFromReader(resp.Body)
	var toolResults []string
	for ev := range stream.Events() {
		switch ev.Type {
		case v1.EventToolResult:
			toolResults = append(toolResults, ev.Text)
		case v1.EventError:
			t.Fatalf("unexpected error event: %s", ev.Error)
		}
	}
	if len(toolResults) != 1 {
		t.Fatalf("toolResults = %d, want 1", len(toolResults))
	}
	if toolResults[0] != part0+part1 {
		t.Fatalf("reassembled = %q, want %q", toolResults[0], part0+part1)
	}
}

func TestStream_Events_SingleToolResultUnchanged(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"type\":\"tool_result\",\"content\":\"small\"}\n\n")
		fmt.Fprintf(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()

	stream := v1.NewStreamFromReader(resp.Body)
	var toolResults []string
	for ev := range stream.Events() {
		if ev.Type == v1.EventToolResult {
			toolResults = append(toolResults, ev.Text)
		}
	}
	if len(toolResults) != 1 || toolResults[0] != "small" {
		t.Fatalf("toolResults = %v", toolResults)
	}
}

func TestStream_Events_IncompleteChunksError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"type\":\"tool_result_part\",\"chunk_id\":\"id\",\"index\":0,\"total\":2,\"content\":\"part\"}\n\n")
		fmt.Fprintf(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()

	stream := v1.NewStreamFromReader(resp.Body)
	for ev := range stream.Events() {
		if ev.Type == v1.EventError {
			if !strings.Contains(ev.Error, "incomplete tool_result chunks") {
				t.Fatalf("error = %q", ev.Error)
			}
			return
		}
	}
	t.Fatal("expected incomplete chunks error event")
}

func TestStream_Events_ToolUseIncludesToolInput(t *testing.T) {
	tests := []struct {
		name     string
		dataJSON string
		check    func(t *testing.T, evs []v1.Event)
	}{
		{
			name:     "with_command",
			dataJSON: `{"type":"tool_use","tool_name":"command_execution","tool_input":{"command":"ls -la"}}`,
			check: func(t *testing.T, evs []v1.Event) {
				t.Helper()
				ev := findEvent(t, evs, v1.EventToolUse)
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
			check: func(t *testing.T, evs []v1.Event) {
				t.Helper()
				ev := findEvent(t, evs, v1.EventToolUse)
				if ev.ToolName != "Bash" {
					t.Fatalf("ToolName = %q", ev.ToolName)
				}
				if ev.ToolInput != nil {
					t.Fatalf("ToolInput = %#v, want nil", ev.ToolInput)
				}
			},
		},
		{
			name:     "empty_tool_input",
			dataJSON: `{"type":"tool_use","tool_name":"Bash","tool_input":{}}`,
			check: func(t *testing.T, evs []v1.Event) {
				t.Helper()
				ev := findEvent(t, evs, v1.EventToolUse)
				if ev.ToolInput == nil {
					t.Fatal("ToolInput = nil, want empty map")
				}
				if len(ev.ToolInput) != 0 {
					t.Fatalf("len(ToolInput) = %d", len(ev.ToolInput))
				}
			},
		},
		{
			name:     "nested",
			dataJSON: `{"type":"tool_use","tool_name":"apply_patch","tool_input":{"changes":[{"path":"a.go"}]}}`,
			check: func(t *testing.T, evs []v1.Event) {
				t.Helper()
				ev := findEvent(t, evs, v1.EventToolUse)
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
			check: func(t *testing.T, evs []v1.Event) {
				t.Helper()
				ev := findEvent(t, evs, v1.EventText)
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
			stream := v1.NewStreamFromReader(strings.NewReader(payload))
			var evs []v1.Event
			for ev := range stream.Events() {
				if ev.Type == v1.EventError {
					t.Fatalf("unexpected error: %s", ev.Error)
				}
				evs = append(evs, ev)
			}
			tc.check(t, evs)
		})
	}
}

func TestStream_RunWithHandlers_OnToolUseReceivesToolInput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"type\":\"tool_use\",\"tool_name\":\"command_execution\",\"tool_input\":{\"command\":\"echo hi\"}}\n\n")
		fmt.Fprintf(w, "data: {\"type\":\"result\"}\n\n")
		fmt.Fprintf(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	client := v1.New(srv.URL, v1.WithNoTimeout())
	sess := v1.ResumeSession(client, "sess-tool-input")
	stream, err := sess.SendText(context.Background(), "hi")
	if err != nil {
		t.Fatalf("SendText: %v", err)
	}

	var gotName string
	var gotInput map[string]any
	err = stream.RunWithHandlers(context.Background(), sess, v1.StreamHandlers{
		OnToolUse: func(toolName string, toolInput map[string]any) {
			gotName = toolName
			gotInput = toolInput
		},
	})
	if err != nil {
		t.Fatalf("RunWithHandlers: %v", err)
	}
	if gotName != "command_execution" {
		t.Fatalf("toolName = %q", gotName)
	}
	if gotInput["command"] != "echo hi" {
		t.Fatalf("toolInput = %#v", gotInput)
	}
}

func TestStream_OnToolUse_RunReceivesToolInput(t *testing.T) {
	payload := "data: {\"type\":\"tool_use\",\"tool_name\":\"command_execution\",\"tool_input\":{\"command\":\"echo hi\"}}\n\n" +
		"data: {\"type\":\"result\"}\n\n" +
		"data: [DONE]\n\n"
	stream := v1.NewStreamFromReader(strings.NewReader(payload))

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
			denySubstr: []string{`"tool_input"`, "changes="},
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
			wantSubstr: []string{"[Tool: X]", "command=x"},
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
			stream := v1.NewStreamFromReader(strings.NewReader(payload))
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

func TestStream_Events_CapturesTurnID(t *testing.T) {
	payload := "data: {\"type\":\"system\",\"content\":\"turn context\",\"turn_id\":\"turn-123\",\"correlation_id\":\"corr-1\"}\n\n" +
		"data: {\"type\":\"text\",\"content\":\"hello\",\"turn_id\":\"turn-123\"}\n\n" +
		"data: [DONE]\n\n"
	stream := v1.NewStreamFromReader(strings.NewReader(payload))
	var gotSystem bool
	for ev := range stream.Events() {
		if ev.Type == v1.EventSystem {
			gotSystem = true
			if ev.TurnID != "turn-123" {
				t.Fatalf("system TurnID = %q", ev.TurnID)
			}
			if ev.CorrelationID != "corr-1" {
				t.Fatalf("system CorrelationID = %q", ev.CorrelationID)
			}
		}
	}
	if !gotSystem {
		t.Fatal("missing system event")
	}
	if stream.TurnID() != "turn-123" {
		t.Fatalf("TurnID() = %q", stream.TurnID())
	}
}

func findEvent(t *testing.T, evs []v1.Event, typ v1.EventType) v1.Event {
	t.Helper()
	for _, ev := range evs {
		if ev.Type == typ {
			return ev
		}
	}
	t.Fatalf("event type %q not found in %#v", typ, evs)
	return v1.Event{}
}
