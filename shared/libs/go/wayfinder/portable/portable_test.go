package portable

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/wayfinder/session"
)

func TestDelta_ForeignOriginOnly(t *testing.T) {
	msgs := []session.Message{
		{Seq: 1, Origin: session.OriginClaudeCode, Role: "user", Content: "a"},
		{Seq: 2, Origin: session.OriginClaudeCode, Role: "assistant", Content: "b"},
	}
	got := Delta(msgs, session.OriginCodex, 0)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
}

func TestDelta_ExcludesSameOrigin(t *testing.T) {
	msgs := []session.Message{
		{Seq: 1, Origin: session.OriginClaudeCode, Content: "c"},
		{Seq: 2, Origin: session.OriginCodex, Content: "x"},
		{Seq: 3, Origin: session.OriginWayfinder, Content: "w"},
	}
	got := Delta(msgs, session.OriginCodex, 0)
	if len(got) != 2 {
		t.Fatalf("len = %d, %+v", len(got), got)
	}
	for _, m := range got {
		if session.NormalizeOrigin(m.Origin) == session.OriginCodex {
			t.Errorf("codex origin leaked: %+v", m)
		}
	}
}

func TestDelta_AfterWatermarkKeepsOnlyNewForeign(t *testing.T) {
	msgs := []session.Message{
		{Seq: 1, Origin: session.OriginClaudeCode, Content: "old"},
		{Seq: 2, Origin: session.OriginClaudeCode, Content: "old2"},
		{Seq: 3, Origin: session.OriginCodex, Content: "new"},
	}
	got := Delta(msgs, session.OriginClaudeCode, 2)
	if len(got) != 1 || got[0].Seq != 3 {
		t.Fatalf("got %+v, want seq 3", got)
	}
	msgs[2].Origin = session.OriginClaudeCode
	got = Delta(msgs, session.OriginClaudeCode, 2)
	if len(got) != 0 {
		t.Fatalf("same origin after watermark should be empty, got %+v", got)
	}
}

func TestRenderSupplement_LabelsOrigin(t *testing.T) {
	text := RenderSupplement(session.OriginCodex, []session.Message{
		{Origin: session.OriginClaudeCode, Role: "assistant", ToolCalls: []session.ToolCallRecord{{Name: "Read"}}},
		{Origin: session.OriginWayfinder, Role: "assistant", Content: "plan"},
	})
	if !strings.Contains(text, "[origin=claudecode]") || !strings.Contains(text, "[origin=wayfinder]") {
		t.Errorf("missing origin labels: %s", text)
	}
	if strings.Contains(text, "--resume") || strings.Contains(text, "agent_session_id") {
		t.Errorf("leaked resume strings: %s", text)
	}
}

func TestRenderSupplement_NeutralizesForeignTools(t *testing.T) {
	text := RenderSupplement(session.OriginCodex, []session.Message{
		{Origin: session.OriginClaudeCode, Role: "assistant", ToolCalls: []session.ToolCallRecord{{Name: "Read"}}},
	})
	if !strings.Contains(text, "tool(claudecode:Read)") {
		t.Errorf("want foreign tool label, got %s", text)
	}
}

func TestRenderSupplement_SameOriginKeepsName(t *testing.T) {
	text := RenderSupplement(session.OriginClaudeCode, []session.Message{
		{Origin: session.OriginClaudeCode, Role: "assistant", ToolCalls: []session.ToolCallRecord{{Name: "Read"}}},
	})
	if !strings.Contains(text, "tool(Read)") {
		t.Errorf("want same-origin tool name, got %s", text)
	}
	if strings.Contains(text, "tool(claudecode:Read)") {
		t.Errorf("same origin should not prefix agent: %s", text)
	}
}

func TestWrapPrompt_PutsSupplementBeforeUser(t *testing.T) {
	got := WrapPrompt("SUP", "USER")
	if !strings.HasPrefix(got, TransferHeader) {
		t.Errorf("missing header: %s", got)
	}
	if !strings.Contains(got, "supplementary history, not your own previous turn") {
		t.Errorf("missing notice: %s", got)
	}
	idxSup := strings.Index(got, "SUP")
	idxUser := strings.Index(got, "USER")
	if idxSup < 0 || idxUser < 0 || idxSup > idxUser {
		t.Errorf("SUP should precede USER: %s", got)
	}
}

func TestBuildSupplement_FullRendersAll(t *testing.T) {
	called := false
	llm := &stubSummarizer{summarize: func(msgs []session.Message) (string, error) {
		called = true
		return "NO", nil
	}}
	got, err := BuildSupplement(context.Background(), session.OriginCodex, []session.Message{
		{Seq: 1, Origin: session.OriginClaudeCode, Role: "user", Content: "CTX-TOKEN-7F3A"},
	}, Strategy{Algorithm: AlgorithmFull}, llm)
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("full must not call summarizer")
	}
	if !strings.Contains(got, "CTX-TOKEN-7F3A") {
		t.Errorf("missing token: %s", got)
	}
}

func TestBuildSupplement_StructuredDoesNotCallLLM(t *testing.T) {
	llm := &stubSummarizer{summarize: func(msgs []session.Message) (string, error) {
		t.Fatal("structured must not call summarizer")
		return "", nil
	}}
	got, err := BuildSupplement(context.Background(), session.OriginCodex, []session.Message{
		{Origin: session.OriginClaudeCode, Role: "user", Content: "hello"},
	}, Strategy{Algorithm: AlgorithmStructured}, llm)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "claudecode") {
		t.Errorf("missing origin: %s", got)
	}
}

func TestBuildSupplement_MapReduceWhenOverThreshold(t *testing.T) {
	const token = "CTX-TOKEN-7F3A"
	var seen []session.Message
	llm := &stubSummarizer{
		summarize: func(msgs []session.Message) (string, error) {
			seen = append(seen, msgs...)
			return "MR-SUMMARY", nil
		},
		merge: func(a, b string) (string, error) {
			return a + " " + b, nil
		},
	}
	msgs := make([]session.Message, 0, 12)
	msgs = append(msgs, session.Message{Seq: 1, Origin: session.OriginClaudeCode, Role: "user", Content: token + strings.Repeat("x", 4000)})
	for i := 2; i <= 12; i++ {
		msgs = append(msgs, session.Message{
			Seq:     i,
			Origin:  session.OriginClaudeCode,
			Role:    "assistant",
			Content: strings.Repeat("y", 4000),
		})
	}
	orig := msgs[0].Content
	got, err := BuildSupplement(context.Background(), session.OriginCodex, msgs, Strategy{
		Algorithm:        AlgorithmMapReduce,
		ThresholdBytes:   100,
		RecentKeep:       2,
		MaxChunkMessages: 4,
	}, llm)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range seen {
		if strings.Contains(m.Content, token) {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("summarize input must contain token from old chunk")
	}
	if !strings.Contains(got, "MR-SUMMARY") && !strings.Contains(got, token) {
		t.Errorf("inject missing summary or token: %s", got)
	}
	if !strings.Contains(got, "[COMPACTED CONTEXT SUMMARY]") {
		t.Errorf("missing compacted header: %s", got)
	}
	if msgs[0].Content != orig {
		t.Fatal("history content must not be mutated")
	}
}

func TestBuildSupplement_MapReduceChunkFallback(t *testing.T) {
	calls := 0
	llm := &stubSummarizer{
		summarize: func(msgs []session.Message) (string, error) {
			calls++
			if calls == 1 {
				return "", errors.New("chunk fail")
			}
			return "OK-CHUNK", nil
		},
		merge: func(a, b string) (string, error) {
			return a + "|" + b, nil
		},
	}
	msgs := []session.Message{
		{Seq: 1, Origin: session.OriginClaudeCode, Role: "user", Content: strings.Repeat("a", 200)},
		{Seq: 2, Origin: session.OriginClaudeCode, Role: "assistant", Content: strings.Repeat("b", 200)},
		{Seq: 3, Origin: session.OriginClaudeCode, Role: "user", Content: strings.Repeat("c", 200)},
		{Seq: 4, Origin: session.OriginClaudeCode, Role: "assistant", Content: strings.Repeat("d", 200)},
	}
	got, err := BuildSupplement(context.Background(), session.OriginCodex, msgs, Strategy{
		Algorithm:        AlgorithmMapReduce,
		ThresholdBytes:   10,
		RecentKeep:       1,
		MaxChunkMessages: 1,
	}, llm)
	if err != nil {
		t.Fatalf("BuildSupplement error: %v", err)
	}
	if !strings.Contains(got, "OK-CHUNK") && !strings.Contains(got, "claudecode") {
		t.Errorf("expected LLM result or structured fallback, got %s", got)
	}
}

func TestBuildSupplement_MapReduceNilLLMErrors(t *testing.T) {
	msgs := []session.Message{
		{Seq: 1, Origin: session.OriginClaudeCode, Role: "user", Content: strings.Repeat("z", 100)},
	}
	_, err := BuildSupplement(context.Background(), session.OriginCodex, msgs, Strategy{
		Algorithm:      AlgorithmMapReduce,
		ThresholdBytes: 10,
		RecentKeep:     1,
	}, nil)
	if err == nil {
		t.Fatal("expected error when llm is nil over threshold")
	}
}

func TestMergeStrategy_Precedence(t *testing.T) {
	got, err := MergeStrategy(
		Strategy{Algorithm: AlgorithmMapReduce, Model: "server-model"},
		Strategy{Algorithm: AlgorithmStructured},
		Strategy{Algorithm: AlgorithmFull},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got.Algorithm != AlgorithmFull {
		t.Errorf("algorithm = %q, want full", got.Algorithm)
	}
	partial, err := MergeStrategy(
		Strategy{Algorithm: AlgorithmMapReduce, Model: "base"},
		Strategy{Model: "x"},
		Strategy{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if partial.Algorithm != AlgorithmMapReduce {
		t.Errorf("partial should keep algorithm, got %q", partial.Algorithm)
	}
	if partial.Model != "x" {
		t.Errorf("model = %q, want x", partial.Model)
	}
}

type stubSummarizer struct {
	summarize func([]session.Message) (string, error)
	merge     func(string, string) (string, error)
}

func (s *stubSummarizer) Summarize(_ context.Context, _ string, msgs []session.Message) (string, error) {
	return s.summarize(msgs)
}

func (s *stubSummarizer) Merge(_ context.Context, _ string, a, b string) (string, error) {
	if s.merge != nil {
		return s.merge(a, b)
	}
	return a + "\n" + b, nil
}
