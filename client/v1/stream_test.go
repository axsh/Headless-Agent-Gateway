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
