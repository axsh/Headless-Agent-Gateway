package session

import (
	"fmt"
	"strings"
	"testing"
)

// --- Mock helpers ---

// newMockSummarize returns a SummarizeFunc that echoes a summary with chunk index.
func newMockSummarize(failAt int) (SummarizeFunc, *int) {
	callCount := 0
	fn := func(msgs []Message) (string, error) {
		callCount++
		if failAt >= 0 && callCount > failAt {
			return "", fmt.Errorf("mock LLM error at call %d", callCount)
		}
		var parts []string
		for _, m := range msgs {
			parts = append(parts, m.Content)
		}
		return "Summary: " + strings.Join(parts, ","), nil
	}
	return fn, &callCount
}

// newMockMerge returns a MergeFunc that concatenates summaries.
func newMockMerge(failAt int) (MergeFunc, *int) {
	callCount := 0
	fn := func(a, b string) (string, error) {
		callCount++
		if failAt >= 0 && callCount > failAt {
			return "", fmt.Errorf("mock merge error at call %d", callCount)
		}
		return "Merged(" + a + " + " + b + ")", nil
	}
	return fn, &callCount
}

func simpleFallback(msgs []Message) string {
	var parts []string
	for _, m := range msgs {
		parts = append(parts, m.Role+":"+m.Content)
	}
	return "Fallback[" + strings.Join(parts, ";") + "]"
}

func makeMessages(n int) []Message {
	msgs := make([]Message, n)
	for i := range n {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		msgs[i] = Message{Role: role, Content: fmt.Sprintf("msg%d", i+1)}
	}
	return msgs
}

// makeMessagesWithTools creates messages including tool call pairs.
func makeMessagesWithTools(n int) []Message {
	msgs := make([]Message, 0, n)
	for i := range n {
		switch i % 4 {
		case 0:
			msgs = append(msgs, Message{Role: "user", Content: fmt.Sprintf("user-%d", i)})
		case 1:
			msgs = append(msgs, Message{
				Role:    "assistant",
				Content: fmt.Sprintf("assistant-%d", i),
				ToolCalls: []ToolCallRecord{
					{ID: fmt.Sprintf("tc-%d", i), Name: "edit_file"},
				},
			})
		case 2:
			msgs = append(msgs, Message{
				Role:       "tool",
				Content:    fmt.Sprintf("result-%d", i),
				ToolCallID: fmt.Sprintf("tc-%d", i-1),
			})
		case 3:
			msgs = append(msgs, Message{Role: "assistant", Content: fmt.Sprintf("done-%d", i)})
		}
	}
	return msgs
}

// --- splitIntoChunks tests ---

func TestSplitIntoChunks_BasicSplit(t *testing.T) {
	summarize, _ := newMockSummarize(-1)
	merge, _ := newMockMerge(-1)
	s := NewMapReduceSummarizer(summarize, merge, simpleFallback, 10)

	msgs := makeMessages(20)
	chunks := s.splitIntoChunks(msgs)

	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	total := 0
	for _, chunk := range chunks {
		total += len(chunk)
	}
	if total != 20 {
		t.Errorf("total messages: got %d, want 20", total)
	}
}

func TestSplitIntoChunks_ToolPairBoundary(t *testing.T) {
	summarize, _ := newMockSummarize(-1)
	merge, _ := newMockMerge(-1)
	s := NewMapReduceSummarizer(summarize, merge, simpleFallback, 5)

	msgs := makeMessagesWithTools(12) // Has tool pairs
	chunks := s.splitIntoChunks(msgs)

	// Verify no chunk starts with a "tool" message.
	for i, chunk := range chunks {
		if len(chunk) > 0 && chunk[0].Role == "tool" {
			t.Errorf("chunk %d starts with 'tool' role, violating tool pair boundary", i)
		}
	}

	// Verify all messages are accounted for.
	total := 0
	for _, chunk := range chunks {
		total += len(chunk)
	}
	if total != len(msgs) {
		t.Errorf("total messages: got %d, want %d", total, len(msgs))
	}
}

func TestSplitIntoChunks_SingleChunk(t *testing.T) {
	summarize, _ := newMockSummarize(-1)
	merge, _ := newMockMerge(-1)
	s := NewMapReduceSummarizer(summarize, merge, simpleFallback, 20)

	msgs := makeMessages(5)
	chunks := s.splitIntoChunks(msgs)

	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if len(chunks[0]) != 5 {
		t.Errorf("chunk size: got %d, want 5", len(chunks[0]))
	}
}

func TestSplitIntoChunks_MaxFourChunks(t *testing.T) {
	summarize, _ := newMockSummarize(-1)
	merge, _ := newMockMerge(-1)
	s := NewMapReduceSummarizer(summarize, merge, simpleFallback, 10)

	msgs := makeMessages(100) // Would be 10 chunks without cap
	chunks := s.splitIntoChunks(msgs)

	if len(chunks) > 4 {
		t.Fatalf("expected at most 4 chunks, got %d", len(chunks))
	}
	total := 0
	for _, chunk := range chunks {
		total += len(chunk)
	}
	if total != 100 {
		t.Errorf("total messages: got %d, want 100", total)
	}
}

// --- Summarize tests ---

func TestMapReduceSummarizer_Summarize_AllSuccess(t *testing.T) {
	summarize, _ := newMockSummarize(-1)
	merge, _ := newMockMerge(-1)
	s := NewMapReduceSummarizer(summarize, merge, simpleFallback, 10)

	msgs := makeMessages(20) // 2 chunks
	result, err := s.Summarize(msgs)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	// Should be "Merged(Summary:... + Summary:...)"
	if !strings.HasPrefix(result, "Merged(") {
		t.Errorf("expected merged result, got %q", result)
	}
	if !strings.Contains(result, "Summary:") {
		t.Errorf("expected chunk summaries in result, got %q", result)
	}
}

func TestMapReduceSummarizer_Summarize_PartialFallback(t *testing.T) {
	// Fail on 2nd summarize call
	summarize, _ := newMockSummarize(1)
	merge, _ := newMockMerge(-1)
	s := NewMapReduceSummarizer(summarize, merge, simpleFallback, 10)

	msgs := makeMessages(20) // 2 chunks
	result, err := s.Summarize(msgs)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	// First chunk: Summary, second chunk: Fallback
	if !strings.Contains(result, "Summary:") {
		t.Errorf("expected first chunk summary, got %q", result)
	}
	if !strings.Contains(result, "Fallback[") {
		t.Errorf("expected fallback for second chunk, got %q", result)
	}
}

func TestMapReduceSummarizer_Summarize_AllFallback(t *testing.T) {
	// Fail immediately
	summarize, _ := newMockSummarize(0)
	merge, _ := newMockMerge(-1)
	s := NewMapReduceSummarizer(summarize, merge, simpleFallback, 10)

	msgs := makeMessages(20) // 2 chunks
	result, err := s.Summarize(msgs)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	// All fallback: result is merged fallbacks
	if !strings.Contains(result, "Fallback[") {
		t.Errorf("expected fallback content, got %q", result)
	}
}

func TestMapReduceSummarizer_Summarize_ReduceFallback(t *testing.T) {
	summarize, _ := newMockSummarize(-1)
	merge, _ := newMockMerge(0) // Fail merge immediately
	s := NewMapReduceSummarizer(summarize, merge, simpleFallback, 10)

	msgs := makeMessages(20) // 2 chunks
	result, err := s.Summarize(msgs)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	// Should have plain concatenation fallback with "---" separator
	if !strings.Contains(result, "\n---\n") {
		t.Errorf("expected '---' separator in reduce fallback, got %q", result)
	}
}

// --- reduceSummaries tests ---

func TestReduceSummaries_PairwiseMerge(t *testing.T) {
	_, _ = newMockSummarize(-1) // unused
	merge, _ := newMockMerge(-1)
	s := NewMapReduceSummarizer(nil, merge, nil, 10)

	result, err := s.reduceSummaries([]string{"A", "B", "C", "D"})
	if err != nil {
		t.Fatalf("reduceSummaries: %v", err)
	}
	// [A,B,C,D] -> [Merged(A+B), Merged(C+D)] -> [Merged(Merged(A+B)+Merged(C+D))]
	expected := "Merged(Merged(A + B) + Merged(C + D))"
	if result != expected {
		t.Errorf("got %q, want %q", result, expected)
	}
}

func TestReduceSummaries_OddCount(t *testing.T) {
	merge, _ := newMockMerge(-1)
	s := NewMapReduceSummarizer(nil, merge, nil, 10)

	result, err := s.reduceSummaries([]string{"A", "B", "C"})
	if err != nil {
		t.Fatalf("reduceSummaries: %v", err)
	}
	// [A,B,C] -> [Merged(A+B), C] -> [Merged(Merged(A+B)+C)]
	expected := "Merged(Merged(A + B) + C)"
	if result != expected {
		t.Errorf("got %q, want %q", result, expected)
	}
}

func TestReduceSummaries_SingleInput(t *testing.T) {
	s := NewMapReduceSummarizer(nil, nil, nil, 10)

	result, err := s.reduceSummaries([]string{"only-one"})
	if err != nil {
		t.Fatalf("reduceSummaries: %v", err)
	}
	if result != "only-one" {
		t.Errorf("got %q, want %q", result, "only-one")
	}
}
