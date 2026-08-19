package agentservice

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

func TestParseFollowFrom(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		query   string
		header  string
		bufLen  int
		want    int
		wantErr bool
	}{
		{name: "empty", bufLen: 5, want: 0},
		{name: "query", query: "3", bufLen: 10, want: 4},
		{name: "header", header: "1", bufLen: 10, want: 2},
		{name: "query wins", query: "3", header: "1", bufLen: 10, want: 4},
		{name: "invalid", query: "abc", bufLen: 10, wantErr: true},
		{name: "negative", query: "-1", bufLen: 10, wantErr: true},
		{name: "exceeds", query: "9", bufLen: 5, wantErr: true},
		{name: "last index ok", query: "4", bufLen: 5, want: 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/s/events", nil)
			if tt.query != "" {
				q := req.URL.Query()
				q.Set("from", tt.query)
				req.URL.RawQuery = q.Encode()
			}
			if tt.header != "" {
				req.Header.Set("Last-Event-ID", tt.header)
			}
			got, err := parseFollowFrom(req, tt.bufLen)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseFollowFrom: %v", err)
			}
			if got != tt.want {
				t.Fatalf("start = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestWriteSSEWireEvents_LogicalIDOnChunks(t *testing.T) {
	s := New()
	w := httptest.NewRecorder()
	id := 7
	ev := codingagent.StreamEvent{
		Type:     codingagent.EventToolResult,
		Content:  strings.Repeat("x", codingagent.DefaultMaxSSEDataLineBytes+64),
		ToolName: "shell",
	}
	if err := s.writeSSEWireEventsID(w, w, ev, &id); err != nil {
		t.Fatal(err)
	}
	body := w.Body.String()
	if !strings.Contains(body, "id: 7\n") {
		t.Fatalf("missing logical id: %s", body)
	}
	if strings.Count(body, "id: 7\n") < 2 {
		t.Fatalf("expected chunked ids, got %s", body)
	}
	if strings.Contains(body, "id: 8\n") {
		t.Fatal("chunk ids should share logical index")
	}
}
