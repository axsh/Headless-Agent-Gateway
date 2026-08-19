package v1_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	v1 "github.com/axsh/arctic-tern/client/v1"
)

func TestFollow_UsesGETEvents(t *testing.T) {
	var gotPath, gotQuery, gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotAccept = r.Header.Get("Accept")
		if r.Method != http.MethodGet {
			t.Errorf("method = %s", r.Method)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"type\":\"result\"}\n\n")
		fmt.Fprintf(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	sess := v1.ResumeSession(v1.New(srv.URL, v1.WithNoTimeout()), "sess-1")
	stream, err := sess.Follow(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Output(io.Discard); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v1/sessions/sess-1/events" {
		t.Fatalf("path = %s", gotPath)
	}
	if gotQuery != "" {
		t.Fatalf("query = %s, want empty", gotQuery)
	}
	if !strings.Contains(gotAccept, "text/event-stream") {
		t.Fatalf("accept = %s", gotAccept)
	}

	stream2, err := sess.FollowFrom(context.Background(), "4")
	if err != nil {
		t.Fatal(err)
	}
	_ = stream2.Output(io.Discard)
	if gotQuery != "from=4" {
		t.Fatalf("follow from query = %s", gotQuery)
	}
}

func TestStream_LastEventID_SkipsEventsWithoutID(t *testing.T) {
	payload := "data: {\"type\":\"system\",\"content\":\"turn context\"}\n\n" +
		"id: 0\ndata: {\"type\":\"text\",\"content\":\"a\"}\n\n" +
		"data: [DONE]\n\n"
	stream := v1.NewStreamFromReader(strings.NewReader(payload))
	for range stream.Events() {
	}
	if stream.LastEventID() != "0" {
		t.Fatalf("LastEventID = %q", stream.LastEventID())
	}
}

func TestStream_LastEventID_IgnoresPartsUntilComplete(t *testing.T) {
	payload := "id: 2\ndata: {\"type\":\"tool_result_part\",\"content\":\"hel\",\"chunk_id\":\"c\",\"index\":0,\"total\":2}\n\n" +
		"id: 2\ndata: {\"type\":\"tool_result_part\",\"content\":\"lo\",\"chunk_id\":\"c\",\"index\":1,\"total\":2}\n\n" +
		"id: 2\ndata: {\"type\":\"tool_result\",\"tool_name\":\"shell\",\"chunk_id\":\"c\",\"content\":\"\"}\n\n" +
		"data: [DONE]\n\n"
	stream := v1.NewStreamFromReader(strings.NewReader(payload))
	var n int
	for ev := range stream.Events() {
		if ev.Type == v1.EventToolResult {
			n++
			if ev.ID != "2" {
				t.Fatalf("id = %q", ev.ID)
			}
		}
	}
	if n != 1 {
		t.Fatalf("tool_result count = %d", n)
	}
	if stream.LastEventID() != "2" {
		t.Fatalf("LastEventID = %q", stream.LastEventID())
	}
}
