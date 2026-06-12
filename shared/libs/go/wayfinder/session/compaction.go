package session

import "fmt"

// CompactionConfig holds compaction thresholds.
type CompactionConfig struct {
	MaxTurns      int // Max conversation turns before compaction (default: 15)
	MaxContentLen int // Max content length for single message trimming (default: 5000)
}

// DefaultCompactionConfig returns the default compaction configuration.
func DefaultCompactionConfig() *CompactionConfig {
	return &CompactionConfig{
		MaxTurns:      15,
		MaxContentLen: 5000,
	}
}

// NeedsCompaction checks if the message history exceeds the threshold.
func NeedsCompaction(messages []Message, cfg *CompactionConfig) bool {
	turnCount := 0
	for _, m := range messages {
		if m.Role == "user" || m.Role == "assistant" {
			turnCount++
		}
	}
	return turnCount > cfg.MaxTurns
}

// Compact applies compaction to the message history:
//  1. Pinned messages are always preserved
//  2. Old unpinned messages are replaced with a summary placeholder
//  3. Recent messages (within window) are preserved
func Compact(messages []Message, cfg *CompactionConfig, summarizer func([]Message) (string, error)) ([]Message, error) {
	var pinned []Message
	var unpinned []Message

	for _, m := range messages {
		if m.Pinned {
			pinned = append(pinned, m)
		} else {
			unpinned = append(unpinned, m)
		}
	}

	// Keep last N messages as the sliding window.
	windowSize := cfg.MaxTurns / 2
	if windowSize < 4 {
		windowSize = 4
	}

	if len(unpinned) <= windowSize {
		return messages, nil // No compaction needed.
	}

	oldMessages := unpinned[:len(unpinned)-windowSize]
	recentMessages := unpinned[len(unpinned)-windowSize:]

	// Summarize old messages.
	summary, err := summarizer(oldMessages)
	if err != nil {
		return messages, fmt.Errorf("compaction summarization failed: %w", err)
	}

	summaryMsg := Message{
		Role:    "system",
		Content: "[COMPACTED CONTEXT SUMMARY]\n" + summary,
		Pinned:  true,
	}

	result := make([]Message, 0, len(pinned)+1+len(recentMessages))
	result = append(result, pinned...)
	result = append(result, summaryMsg)
	result = append(result, recentMessages...)
	return result, nil
}

// TrimLongContent truncates message content exceeding maxLen.
func TrimLongContent(messages []Message, maxLen int) []Message {
	for i := range messages {
		if len(messages[i].Content) > maxLen {
			messages[i].Content = messages[i].Content[:maxLen] + "\n... [TRUNCATED]"
		}
	}
	return messages
}
