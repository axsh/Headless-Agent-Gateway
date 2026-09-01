package v1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetUsage_Decode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/sessions/s1/usage" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(SessionUsageReport{
			SessionID: "s1",
			Usage:     TokenUsage{InputTokens: 15, OutputTokens: 5, Source: "derived_session_sum", Confidence: "high"},
			Turns: []TurnUsageRecord{{
				TurnID: "t1",
				Usage:  TokenUsage{InputTokens: 15, OutputTokens: 5, Source: "claude_result", Confidence: "high"},
			}},
		})
	}))
	defer srv.Close()
	c := New(srv.URL)
	rep, err := c.GetUsage(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Usage.InputTokens != 15 || len(rep.Turns) != 1 {
		t.Fatalf("report = %+v", rep)
	}
}

func TestGetUsage_DecodeModelSource(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(SessionUsageReport{
			SessionID: "s1",
			Usage:     TokenUsage{InputTokens: 10, OutputTokens: 2, Source: "derived_session_sum", Confidence: "high"},
			Turns: []TurnUsageRecord{{
				TurnID: "t1",
				Usage: TokenUsage{
					InputTokens: 10, OutputTokens: 2,
					Model: "claude-sonnet", ModelSource: ModelSourceAgent,
					Source: "claude_result", Confidence: "high",
				},
			}},
		})
	}))
	defer srv.Close()
	c := New(srv.URL)
	rep, err := c.GetUsage(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	u := rep.Turns[0].Usage
	if u.Model != "claude-sonnet" || u.ModelSource != ModelSourceAgent {
		t.Fatalf("usage = %+v", u)
	}
}

func TestGetUsage_LastNQuery(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(SessionUsageReport{
			SessionID: "s1",
			Usage:     TokenUsage{InputTokens: 3, OutputTokens: 3, Source: "derived_session_sum", Confidence: "high"},
			Turns: []TurnUsageRecord{{
				TurnID: "t2",
				Usage:  TokenUsage{InputTokens: 3, OutputTokens: 3, Source: "claude_result", Confidence: "high"},
			}},
		})
	}))
	defer srv.Close()
	c := New(srv.URL)
	rep, err := c.GetUsage(context.Background(), "s1", UsageQuery{LastN: 1})
	if err != nil {
		t.Fatal(err)
	}
	if gotQuery != "last_n=1" {
		t.Fatalf("query = %q", gotQuery)
	}
	if len(rep.Turns) != 1 || rep.Turns[0].TurnID != "t2" {
		t.Fatalf("rep = %+v", rep)
	}
}

func TestGetSession_UsageField(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "s1", "agent_name": "claudecode", "model": "m", "status": "completed",
			"work_dir": "/w", "session_dir": "/s", "agent_session_id": "",
			"usage": map[string]any{"input_tokens": 9, "output_tokens": 2, "source": "derived_session_sum", "confidence": "high"},
		})
	}))
	defer srv.Close()
	c := New(srv.URL)
	info, err := c.GetSession(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	if info.Usage == nil || info.Usage.InputTokens != 9 {
		t.Fatalf("usage = %+v", info.Usage)
	}
}
