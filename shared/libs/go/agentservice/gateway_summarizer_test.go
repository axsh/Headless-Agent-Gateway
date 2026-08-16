package agentservice

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/wayfinder/session"
)

func TestGatewaySummarizer_ChatCompletions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("X-Gateway-Token") != "tok" {
			t.Errorf("missing token")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"content": "SUM"}},
			},
		})
	}))
	defer srv.Close()

	g := &GatewaySummarizer{GatewayURL: srv.URL, Token: "tok", DefaultModel: "m1"}
	got, err := g.Summarize(context.Background(), "", []session.Message{
		{Role: "user", Content: "hello", Origin: session.OriginClaudeCode},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "SUM" {
		t.Errorf("got %q", got)
	}
}
