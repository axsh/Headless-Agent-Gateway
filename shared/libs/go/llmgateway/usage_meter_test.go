package llmgateway

import (
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

func TestExtractTernSessionAndTurn(t *testing.T) {
	h := "not-needed;token=x;fallback=false;sid=default;tern_sid=sess-1;tid=turn-9"
	if got := ExtractTernSessionID(h); got != "sess-1" {
		t.Errorf("tern_sid = %q", got)
	}
	if got := ExtractTurnID(h); got != "turn-9" {
		t.Errorf("tid = %q", got)
	}
	if got := ExtractSessionID(h); got != "default" {
		t.Errorf("sid = %q (routing must stay default)", got)
	}
}

func TestUsageMeter_RecordTake(t *testing.T) {
	m := NewUsageMeter()
	m.Record("s", "t", codingagent.TokenUsage{InputTokens: 1, OutputTokens: 2})
	got := m.Take("s", "t")
	if len(got) != 1 || got[0].InputTokens != 1 || got[0].Source != codingagent.UsageSourceLLMGateway {
		t.Fatalf("got %+v", got)
	}
	if len(m.Take("s", "t")) != 0 {
		t.Fatal("expected empty second take")
	}
}
