package session

import (
	"testing"
)

func TestNeedsCompaction_BelowThreshold(t *testing.T) {
	cfg := DefaultCompactionConfig()
	messages := make([]Message, 0)
	for range 10 {
		messages = append(messages, Message{Role: "user", Content: "hi"})
		messages = append(messages, Message{Role: "assistant", Content: "hello"})
	}
	// 20 messages = 20 turns, but cfg.MaxTurns is 15, so it should need compaction.
	// Let's test below threshold first.
	fewMessages := messages[:6] // 6 messages = 6 turns
	if NeedsCompaction(fewMessages, cfg) {
		t.Error("NeedsCompaction should be false for 6 turns (threshold=15)")
	}
}

func TestNeedsCompaction_AboveThreshold(t *testing.T) {
	cfg := DefaultCompactionConfig()
	var messages []Message
	for range 20 {
		messages = append(messages, Message{Role: "user", Content: "hi"})
	}
	if !NeedsCompaction(messages, cfg) {
		t.Error("NeedsCompaction should be true for 20 turns (threshold=15)")
	}
}

func TestCompact_PinnedPreserved(t *testing.T) {
	cfg := &CompactionConfig{MaxTurns: 4, MaxContentLen: 5000}
	messages := []Message{
		{Role: "system", Content: "System prompt", Pinned: true},
		{Role: "user", Content: "old message 1"},
		{Role: "assistant", Content: "old response 1"},
		{Role: "user", Content: "old message 2"},
		{Role: "assistant", Content: "old response 2"},
		{Role: "user", Content: "old message 3"},
		{Role: "assistant", Content: "old response 3"},
		{Role: "user", Content: "recent message 1"},
		{Role: "assistant", Content: "recent response 1"},
		{Role: "user", Content: "recent message 2"},
		{Role: "assistant", Content: "recent response 2"},
	}

	summarizer := func(msgs []Message) (string, error) {
		return "Summary of old messages", nil
	}

	result, err := Compact(messages, cfg, summarizer)
	if err != nil {
		t.Fatalf("Compact failed: %v", err)
	}

	// Check that pinned message is preserved.
	foundPinned := false
	for _, m := range result {
		if m.Content == "System prompt" && m.Pinned {
			foundPinned = true
		}
	}
	if !foundPinned {
		t.Error("pinned message should be preserved after compaction")
	}
}

func TestCompact_OldMessagesReplaced(t *testing.T) {
	cfg := &CompactionConfig{MaxTurns: 4, MaxContentLen: 5000}
	messages := []Message{
		{Role: "user", Content: "old 1"},
		{Role: "assistant", Content: "old 2"},
		{Role: "user", Content: "old 3"},
		{Role: "assistant", Content: "old 4"},
		{Role: "user", Content: "old 5"},
		{Role: "assistant", Content: "old 6"},
		{Role: "user", Content: "recent 1"},
		{Role: "assistant", Content: "recent 2"},
		{Role: "user", Content: "recent 3"},
		{Role: "assistant", Content: "recent 4"},
	}

	summarizer := func(msgs []Message) (string, error) {
		return "Summarized old conversation", nil
	}

	result, err := Compact(messages, cfg, summarizer)
	if err != nil {
		t.Fatalf("Compact failed: %v", err)
	}

	// Should have summary message + recent window.
	foundSummary := false
	for _, m := range result {
		if m.Role == "system" && m.Pinned {
			foundSummary = true
		}
	}
	if !foundSummary {
		t.Error("summary message should be present after compaction")
	}

	// Old messages should not be present.
	for _, m := range result {
		if m.Content == "old 1" || m.Content == "old 2" {
			t.Error("old messages should be replaced by summary")
		}
	}
}

func TestCompact_RecentWindowPreserved(t *testing.T) {
	cfg := &CompactionConfig{MaxTurns: 4, MaxContentLen: 5000}
	messages := []Message{
		{Role: "user", Content: "old 1"},
		{Role: "assistant", Content: "old 2"},
		{Role: "user", Content: "old 3"},
		{Role: "assistant", Content: "old 4"},
		{Role: "user", Content: "old 5"},
		{Role: "assistant", Content: "old 6"},
		{Role: "user", Content: "recent 1"},
		{Role: "assistant", Content: "recent 2"},
		{Role: "user", Content: "recent 3"},
		{Role: "assistant", Content: "recent 4"},
	}

	summarizer := func(msgs []Message) (string, error) {
		return "Summary", nil
	}

	result, err := Compact(messages, cfg, summarizer)
	if err != nil {
		t.Fatalf("Compact failed: %v", err)
	}

	// Recent messages should be preserved.
	foundRecent := false
	for _, m := range result {
		if m.Content == "recent 4" {
			foundRecent = true
		}
	}
	if !foundRecent {
		t.Error("recent messages should be preserved after compaction")
	}
}

func TestTrimLongContent(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "short"},
		{Role: "tool", Content: "x" + string(make([]byte, 6000))},
	}

	result := TrimLongContent(messages, 5000)
	if len(result[0].Content) != 5 {
		t.Errorf("short content should not be trimmed, got len=%d", len(result[0].Content))
	}
	if len(result[1].Content) > 5020 {
		t.Errorf("long content should be trimmed, got len=%d", len(result[1].Content))
	}
	if result[1].Content[len(result[1].Content)-len("... [TRUNCATED]"):] != "... [TRUNCATED]" {
		t.Error("trimmed content should end with truncation marker")
	}
}
