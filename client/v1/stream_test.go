package v1

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

