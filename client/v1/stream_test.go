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
