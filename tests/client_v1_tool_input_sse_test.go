package llm_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	v1 "github.com/axsh/arctic-tern/client/v1"
)

func TestClientV1_SSE_ToolUseExposesToolInput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/messages") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"type\":\"tool_use\",\"tool_name\":\"command_execution\",\"tool_input\":{\"command\":\"ls -la\"}}\n\n")
		fmt.Fprintf(w, "data: {\"type\":\"result\"}\n\n")
		fmt.Fprintf(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	client := v1.New(srv.URL, v1.WithNoTimeout())
	sess := v1.ResumeSession(client, "sess-tool-input")
	stream, err := sess.SendText(context.Background(), "ignored")
	if err != nil {
		t.Fatalf("SendText: %v", err)
	}

	var toolUse *v1.Event
	for ev := range stream.Events() {
		if ev.Type == v1.EventError {
			t.Fatalf("unexpected error: %s", ev.Error)
		}
		if ev.Type == v1.EventToolUse {
			copied := ev
			toolUse = &copied
		}
	}
	if toolUse == nil {
		t.Fatal("expected tool_use event")
	}
	if toolUse.ToolName != "command_execution" {
		t.Fatalf("ToolName = %q", toolUse.ToolName)
	}
	if toolUse.ToolInput["command"] != "ls -la" {
		t.Fatalf("ToolInput = %#v", toolUse.ToolInput)
	}
}

func TestClientV1_SSE_OnToolUseReceivesToolInput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/messages") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"type\":\"tool_use\",\"tool_name\":\"command_execution\",\"tool_input\":{\"command\":\"echo hi\"}}\n\n")
		fmt.Fprintf(w, "data: {\"type\":\"result\"}\n\n")
		fmt.Fprintf(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	client := v1.New(srv.URL, v1.WithNoTimeout())
	sess := v1.ResumeSession(client, "sess-tool-input-cb")
	stream, err := sess.SendText(context.Background(), "ignored")
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
