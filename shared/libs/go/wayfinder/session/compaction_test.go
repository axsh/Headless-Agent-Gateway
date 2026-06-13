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

// ---- Tool Pair Protection Tests ----

func TestAdjustBoundaryForToolPairs(t *testing.T) {
	tests := []struct {
		name     string
		msgs     []Message
		boundary int
		want     int
	}{
		{
			name:     "boundary at zero returns zero",
			msgs:     []Message{{Role: "user"}},
			boundary: 0,
			want:     0,
		},
		{
			name:     "negative boundary returns zero",
			msgs:     []Message{{Role: "user"}},
			boundary: -1,
			want:     0,
		},
		{
			name: "boundary at user message no adjustment",
			msgs: []Message{
				{Role: "user", Content: "old"},
				{Role: "assistant", Content: "resp"},
				{Role: "user", Content: "new"},
			},
			boundary: 2,
			want:     2,
		},
		{
			name: "boundary at assistant message no adjustment",
			msgs: []Message{
				{Role: "user", Content: "old"},
				{Role: "assistant", Content: "resp"},
				{Role: "user", Content: "new"},
			},
			boundary: 1,
			want:     1,
		},
		{
			name: "boundary at single tool shifts to assistant",
			msgs: []Message{
				{Role: "user", Content: "prompt"},
				{Role: "assistant", Content: "resp", ToolCalls: []ToolCallRecord{{ID: "tc1", Name: "edit_file"}}},
				{Role: "tool", Content: "result", ToolCallID: "tc1"},
				{Role: "user", Content: "next"},
			},
			boundary: 2, // points at tool message
			want:     1, // should shift to assistant
		},
		{
			name: "boundary at second of two consecutive tools shifts to assistant",
			msgs: []Message{
				{Role: "user", Content: "prompt"},
				{Role: "assistant", Content: "resp", ToolCalls: []ToolCallRecord{
					{ID: "tc1", Name: "edit_file"},
					{ID: "tc2", Name: "execute_command"},
				}},
				{Role: "tool", Content: "result1", ToolCallID: "tc1"},
				{Role: "tool", Content: "result2", ToolCallID: "tc2"},
				{Role: "user", Content: "next"},
			},
			boundary: 3, // points at second tool
			want:     1, // should shift past both tools to assistant
		},
		{
			name: "boundary at first of two consecutive tools shifts to assistant",
			msgs: []Message{
				{Role: "user", Content: "prompt"},
				{Role: "assistant", Content: "resp", ToolCalls: []ToolCallRecord{
					{ID: "tc1", Name: "edit_file"},
					{ID: "tc2", Name: "execute_command"},
				}},
				{Role: "tool", Content: "result1", ToolCallID: "tc1"},
				{Role: "tool", Content: "result2", ToolCallID: "tc2"},
				{Role: "user", Content: "next"},
			},
			boundary: 2, // points at first tool
			want:     1, // should shift to assistant
		},
		{
			name: "boundary at tool with no preceding assistant returns original",
			msgs: []Message{
				{Role: "tool", Content: "orphaned", ToolCallID: "tc1"},
				{Role: "user", Content: "next"},
			},
			boundary: 0,
			want:     0, // boundary is 0, returns 0
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := adjustBoundaryForToolPairs(tt.msgs, tt.boundary)
			if got != tt.want {
				t.Errorf("adjustBoundaryForToolPairs() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestValidateToolPairIntegrity(t *testing.T) {
	tests := []struct {
		name string
		msgs []Message
		want bool
	}{
		{
			name: "no tool messages is valid",
			msgs: []Message{
				{Role: "user", Content: "hi"},
				{Role: "assistant", Content: "hello"},
			},
			want: true,
		},
		{
			name: "tool after assistant with tool calls is valid",
			msgs: []Message{
				{Role: "assistant", Content: "resp", ToolCalls: []ToolCallRecord{{ID: "tc1", Name: "edit_file"}}},
				{Role: "tool", Content: "result", ToolCallID: "tc1"},
			},
			want: true,
		},
		{
			name: "consecutive tools after assistant is valid",
			msgs: []Message{
				{Role: "assistant", Content: "resp", ToolCalls: []ToolCallRecord{
					{ID: "tc1", Name: "edit_file"},
					{ID: "tc2", Name: "run_cmd"},
				}},
				{Role: "tool", Content: "r1", ToolCallID: "tc1"},
				{Role: "tool", Content: "r2", ToolCallID: "tc2"},
			},
			want: true,
		},
		{
			name: "orphaned tool at start is invalid",
			msgs: []Message{
				{Role: "tool", Content: "result", ToolCallID: "tc1"},
				{Role: "user", Content: "hi"},
			},
			want: false,
		},
		{
			name: "tool after assistant without tool calls is invalid",
			msgs: []Message{
				{Role: "assistant", Content: "resp"},
				{Role: "tool", Content: "result", ToolCallID: "tc1"},
			},
			want: false,
		},
		{
			name: "tool after user is invalid",
			msgs: []Message{
				{Role: "user", Content: "hi"},
				{Role: "tool", Content: "result", ToolCallID: "tc1"},
			},
			want: false,
		},
		{
			name: "system summary then tool is invalid",
			msgs: []Message{
				{Role: "system", Content: "summary", Pinned: true},
				{Role: "tool", Content: "result", ToolCallID: "tc1"},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validateToolPairIntegrity(tt.msgs)
			if got != tt.want {
				t.Errorf("validateToolPairIntegrity() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCompact_ToolPairNotSplit(t *testing.T) {
	// MaxTurns=4, windowSize=max(4/2,4)=4
	// Messages: user -> assistant(tool_use) -> tool -> assistant -> user -> assistant -> user -> assistant
	// 8 unpinned, boundary=8-4=4, recent=[4,5,6,7] -- no split.
	// But with windowSize=2 boundary would be 6. Let's use MaxTurns=8, windowSize=4.
	// Actually, let's craft a scenario where the initial boundary would split.
	// Use MaxTurns=10, which gives windowSize=5.
	// Build 12 unpinned messages where tool pair is at indices 7-9 (assistant+tool+tool).
	cfg := &CompactionConfig{MaxTurns: 10, MaxContentLen: 5000}

	msgs := []Message{
		{Role: "user", Content: "prompt1"},
		{Role: "assistant", Content: "response1"},
		{Role: "user", Content: "prompt2"},
		{Role: "assistant", Content: "response2"},
		{Role: "user", Content: "prompt3"},
		{Role: "assistant", Content: "do tool", ToolCalls: []ToolCallRecord{{ID: "tc1", Name: "edit_file"}}},
		{Role: "tool", Content: "File edited", ToolCallID: "tc1"},
		{Role: "assistant", Content: "done"},
		{Role: "user", Content: "prompt4"},
		{Role: "assistant", Content: "response4"},
		{Role: "user", Content: "prompt5"},
		{Role: "assistant", Content: "response5"},
	}

	summarizer := func(oldMsgs []Message) (string, error) {
		return "Summary of old messages", nil
	}

	result, err := Compact(msgs, cfg, summarizer)
	if err != nil {
		t.Fatalf("Compact failed: %v", err)
	}

	// Verify no orphaned tool messages.
	if !validateToolPairIntegrity(result) {
		t.Error("compaction result has broken tool pair integrity")
		for i, m := range result {
			t.Logf("  [%d] role=%s tool_calls=%d tool_call_id=%q", i, m.Role, len(m.ToolCalls), m.ToolCallID)
		}
	}
}

func TestCompact_MultipleToolResultsNotSplit(t *testing.T) {
	cfg := &CompactionConfig{MaxTurns: 10, MaxContentLen: 5000}

	msgs := []Message{
		{Role: "user", Content: "prompt1"},
		{Role: "assistant", Content: "response1"},
		{Role: "user", Content: "prompt2"},
		{Role: "assistant", Content: "response2"},
		{Role: "user", Content: "prompt3"},
		{Role: "assistant", Content: "do tools", ToolCalls: []ToolCallRecord{
			{ID: "tc1", Name: "edit_file"},
			{ID: "tc2", Name: "execute_command"},
		}},
		{Role: "tool", Content: "File edited", ToolCallID: "tc1"},
		{Role: "tool", Content: "Command executed", ToolCallID: "tc2"},
		{Role: "assistant", Content: "done"},
		{Role: "user", Content: "prompt4"},
		{Role: "assistant", Content: "response4"},
		{Role: "user", Content: "prompt5"},
		{Role: "assistant", Content: "response5"},
	}

	summarizer := func(oldMsgs []Message) (string, error) {
		return "Summary", nil
	}

	result, err := Compact(msgs, cfg, summarizer)
	if err != nil {
		t.Fatalf("Compact failed: %v", err)
	}

	if !validateToolPairIntegrity(result) {
		t.Error("compaction result has broken tool pair integrity with multiple tool results")
		for i, m := range result {
			t.Logf("  [%d] role=%s tool_calls=%d tool_call_id=%q", i, m.Role, len(m.ToolCalls), m.ToolCallID)
		}
	}
}

func TestCompact_BoundaryAdjustmentWithConsecutiveToolMessages(t *testing.T) {
	// Force a small window that would normally cut into tool messages.
	cfg := &CompactionConfig{MaxTurns: 8, MaxContentLen: 5000}

	msgs := []Message{
		{Role: "user", Content: "p1"},
		{Role: "assistant", Content: "r1"},
		{Role: "user", Content: "p2"},
		{Role: "assistant", Content: "use tools", ToolCalls: []ToolCallRecord{
			{ID: "tc1", Name: "tool_a"},
			{ID: "tc2", Name: "tool_b"},
			{ID: "tc3", Name: "tool_c"},
		}},
		{Role: "tool", Content: "res_a", ToolCallID: "tc1"},
		{Role: "tool", Content: "res_b", ToolCallID: "tc2"},
		{Role: "tool", Content: "res_c", ToolCallID: "tc3"},
		{Role: "assistant", Content: "all done"},
		{Role: "user", Content: "p3"},
		{Role: "assistant", Content: "r3"},
		{Role: "user", Content: "p4"},
		{Role: "assistant", Content: "r4"},
		{Role: "user", Content: "p5"},
		{Role: "assistant", Content: "r5"},
	}

	summarizer := func(oldMsgs []Message) (string, error) {
		return "Summary", nil
	}

	result, err := Compact(msgs, cfg, summarizer)
	if err != nil {
		t.Fatalf("Compact failed: %v", err)
	}

	if !validateToolPairIntegrity(result) {
		t.Error("compaction broke tool pair integrity with consecutive tool messages")
		for i, m := range result {
			t.Logf("  [%d] role=%s tool_calls=%d tool_call_id=%q", i, m.Role, len(m.ToolCalls), m.ToolCallID)
		}
	}
}

func TestCompact_NoToolMessages_NoAdjustment(t *testing.T) {
	cfg := &CompactionConfig{MaxTurns: 4, MaxContentLen: 5000}

	msgs := []Message{
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

	summarizer := func(oldMsgs []Message) (string, error) {
		return "Summary", nil
	}

	result, err := Compact(msgs, cfg, summarizer)
	if err != nil {
		t.Fatalf("Compact failed: %v", err)
	}

	// Should behave exactly like before -- summary + recent window.
	foundSummary := false
	foundRecent := false
	for _, m := range result {
		if m.Role == "system" && m.Pinned {
			foundSummary = true
		}
		if m.Content == "recent 4" {
			foundRecent = true
		}
	}
	if !foundSummary {
		t.Error("summary message should be present")
	}
	if !foundRecent {
		t.Error("recent messages should be preserved")
	}
}

// ---- User Start Guarantee Tests ----

func TestAdjustBoundaryForUserStart(t *testing.T) {
	tests := []struct {
		name     string
		msgs     []Message
		boundary int
		want     int
	}{
		{
			name:     "boundary at zero returns zero",
			msgs:     []Message{{Role: "user"}},
			boundary: 0,
			want:     0,
		},
		{
			name:     "negative boundary returns zero",
			msgs:     []Message{{Role: "user"}},
			boundary: -1,
			want:     0,
		},
		{
			name:     "boundary beyond length unchanged",
			msgs:     []Message{{Role: "user"}, {Role: "assistant"}},
			boundary: 5,
			want:     5,
		},
		{
			name: "already user no adjustment",
			msgs: []Message{
				{Role: "assistant", Content: "old"},
				{Role: "user", Content: "new"},
				{Role: "assistant", Content: "resp"},
			},
			boundary: 1,
			want:     1,
		},
		{
			name: "assistant with tool calls shifts to user",
			msgs: []Message{
				{Role: "user", Content: "prompt"},
				{Role: "assistant", Content: "do tool", ToolCalls: []ToolCallRecord{{ID: "tc1", Name: "edit"}}},
				{Role: "tool", Content: "result", ToolCallID: "tc1"},
				{Role: "assistant", Content: "done"},
				{Role: "user", Content: "next"},
			},
			boundary: 1, // starts at assistant(tool_calls)
			want:     0, // shifts back to user at index 0
		},
		{
			name: "assistant without tool calls shifts to previous user",
			msgs: []Message{
				{Role: "user", Content: "p1"},
				{Role: "assistant", Content: "r1"},
				{Role: "user", Content: "p2"},
				{Role: "assistant", Content: "r2"},
				{Role: "assistant", Content: "r3"}, // boundary here (index 4)
				{Role: "user", Content: "p3"},
			},
			boundary: 4,
			want:     2, // shifts to user at index 2
		},
		{
			name: "no user in messages returns zero",
			msgs: []Message{
				{Role: "assistant", Content: "r1"},
				{Role: "assistant", Content: "r2"},
				{Role: "assistant", Content: "r3"},
			},
			boundary: 1,
			want:     0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := adjustBoundaryForUserStart(tt.msgs, tt.boundary)
			if got != tt.want {
				t.Errorf("adjustBoundaryForUserStart() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestValidateMessageOrdering(t *testing.T) {
	tests := []struct {
		name string
		msgs []Message
		want bool
	}{
		{
			name: "system then user is valid",
			msgs: []Message{
				{Role: "system", Content: "summary", Pinned: true},
				{Role: "user", Content: "hello"},
				{Role: "assistant", Content: "hi"},
			},
			want: true,
		},
		{
			name: "system then assistant with tool calls is invalid",
			msgs: []Message{
				{Role: "system", Content: "summary", Pinned: true},
				{Role: "assistant", Content: "do tool", ToolCalls: []ToolCallRecord{{ID: "tc1", Name: "edit"}}},
				{Role: "tool", Content: "result", ToolCallID: "tc1"},
			},
			want: false,
		},
		{
			name: "system then plain assistant is invalid",
			msgs: []Message{
				{Role: "system", Content: "summary", Pinned: true},
				{Role: "assistant", Content: "response"},
			},
			want: false,
		},
		{
			name: "pinned only is valid (edge case)",
			msgs: []Message{
				{Role: "system", Content: "prompt", Pinned: true},
			},
			want: true,
		},
		{
			name: "empty messages is valid",
			msgs: []Message{},
			want: true,
		},
		{
			name: "no pinned messages user first is valid",
			msgs: []Message{
				{Role: "user", Content: "hello"},
				{Role: "assistant", Content: "hi"},
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validateMessageOrdering(tt.msgs)
			if got != tt.want {
				t.Errorf("validateMessageOrdering() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCompact_RecentMessagesStartWithUser(t *testing.T) {
	// Scenario: boundary would naturally fall on assistant(tool_calls).
	// After adjustment, recentMessages must start with "user".
	cfg := &CompactionConfig{MaxTurns: 8, MaxContentLen: 5000}

	msgs := []Message{
		{Role: "user", Content: "p1"},
		{Role: "assistant", Content: "r1"},
		{Role: "user", Content: "p2"},
		{Role: "assistant", Content: "r2"},
		{Role: "user", Content: "p3"},
		{Role: "assistant", Content: "do tool", ToolCalls: []ToolCallRecord{{ID: "tc1", Name: "cmd"}}},
		{Role: "tool", Content: "output", ToolCallID: "tc1"},
		{Role: "assistant", Content: "done"},
		{Role: "user", Content: "p4"},
		{Role: "assistant", Content: "r4"},
		{Role: "user", Content: "p5"},
		{Role: "assistant", Content: "r5"},
	}

	summarizer := func(oldMsgs []Message) (string, error) {
		return "Summary", nil
	}

	result, err := Compact(msgs, cfg, summarizer)
	if err != nil {
		t.Fatalf("Compact failed: %v", err)
	}

	// Find first non-pinned, non-system message.
	for _, m := range result {
		if m.Pinned || m.Role == "system" {
			continue
		}
		if m.Role != "user" {
			t.Errorf("first non-system message role = %q, want %q", m.Role, "user")
			for i, msg := range result {
				contentPreview := msg.Content
				if len(contentPreview) > 30 {
					contentPreview = contentPreview[:30]
				}
				t.Logf("  [%d] role=%s pinned=%v content=%q", i, msg.Role, msg.Pinned, contentPreview)
			}
		}
		break
	}

	// Also verify tool pair integrity is maintained.
	if !validateToolPairIntegrity(result) {
		t.Error("tool pair integrity broken after compaction")
	}
}

func TestCompact_BoundaryReachesZero_SkipsCompaction(t *testing.T) {
	// All messages are assistant/tool with no user -> boundary reaches 0 -> skip compaction.
	cfg := &CompactionConfig{MaxTurns: 2, MaxContentLen: 5000}

	msgs := []Message{
		{Role: "assistant", Content: "r1", ToolCalls: []ToolCallRecord{{ID: "tc1", Name: "cmd"}}},
		{Role: "tool", Content: "output", ToolCallID: "tc1"},
		{Role: "assistant", Content: "r2", ToolCalls: []ToolCallRecord{{ID: "tc2", Name: "cmd"}}},
		{Role: "tool", Content: "output2", ToolCallID: "tc2"},
		{Role: "assistant", Content: "done"},
	}

	summarizer := func(oldMsgs []Message) (string, error) {
		return "Summary", nil
	}

	result, err := Compact(msgs, cfg, summarizer)
	if err != nil {
		t.Fatalf("Compact failed: %v", err)
	}

	// Should return original messages unchanged (compaction skipped).
	if len(result) != len(msgs) {
		t.Errorf("expected compaction to be skipped, got %d messages instead of %d", len(result), len(msgs))
	}
}

