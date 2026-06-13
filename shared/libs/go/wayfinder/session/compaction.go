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

	// Calculate initial boundary and adjust for tool pair protection.
	boundary := len(unpinned) - windowSize
	boundary = adjustBoundaryForToolPairs(unpinned, boundary)
	boundary = adjustBoundaryForUserStart(unpinned, boundary)

	// If boundary reached 0, all messages would be in recentMessages.
	// This means no compaction is possible, skip.
	if boundary == 0 {
		return messages, nil
	}

	oldMessages := unpinned[:boundary]
	recentMessages := unpinned[boundary:]

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

	// Post-compaction validation: ensure tool pairs are intact and message ordering is valid.
	if !validateToolPairIntegrity(result) || !validateMessageOrdering(result) {
		// Safety fallback: return original messages to avoid API errors.
		return messages, nil
	}

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

// adjustBoundaryForToolPairs adjusts the sliding window boundary
// to avoid splitting tool call pairs (assistant+tool_calls -> tool results).
// If the boundary falls on a tool message, it shifts backward to include
// the corresponding assistant message with tool calls.
func adjustBoundaryForToolPairs(unpinned []Message, boundary int) int {
	if boundary <= 0 {
		return 0
	}
	if boundary >= len(unpinned) {
		return boundary
	}

	// If the boundary message is not a tool message, no adjustment needed.
	if unpinned[boundary].Role != "tool" {
		return boundary
	}

	// Shift backward past all consecutive tool messages.
	originalBoundary := boundary
	for boundary > 0 && unpinned[boundary].Role == "tool" {
		boundary--
	}

	// Check if we landed on an assistant with tool calls.
	if boundary >= 0 && unpinned[boundary].Role == "assistant" && len(unpinned[boundary].ToolCalls) > 0 {
		return boundary
	}

	// Data inconsistency: return original boundary (safe fallback).
	return originalBoundary
}

// adjustBoundaryForUserStart adjusts the boundary so that
// recentMessages starts with a "user" role message.
// This prevents system(summary) -> assistant(tool_calls) sequences
// that violate Gemini's function call ordering constraint.
func adjustBoundaryForUserStart(unpinned []Message, boundary int) int {
	if boundary <= 0 {
		return 0
	}
	if boundary >= len(unpinned) {
		return boundary
	}

	// If the boundary message is already "user", no adjustment needed.
	if unpinned[boundary].Role == "user" {
		return boundary
	}

	// Shift backward until we find a "user" message.
	for boundary > 0 && unpinned[boundary].Role != "user" {
		boundary--
	}

	// If we reached index 0 and it's not "user", compaction should be skipped.
	if boundary == 0 && unpinned[0].Role != "user" {
		return 0
	}

	return boundary
}

// validateToolPairIntegrity checks that every tool message has
// a preceding assistant message with matching tool calls.
// Returns true if the message list is valid.
func validateToolPairIntegrity(messages []Message) bool {
	for i, m := range messages {
		if m.Role != "tool" {
			continue
		}
		// Walk backward to find the originating assistant message.
		foundAssistant := false
		for j := i - 1; j >= 0; j-- {
			if messages[j].Role == "tool" {
				// Another tool message -- continue walking back.
				continue
			}
			if messages[j].Role == "assistant" && len(messages[j].ToolCalls) > 0 {
				foundAssistant = true
			}
			break
		}
		if !foundAssistant {
			return false
		}
	}
	return true
}

// validateMessageOrdering checks that the first non-pinned, non-system
// message after compaction summary is a "user" role message.
// This prevents Gemini API errors where function_call follows system message.
func validateMessageOrdering(messages []Message) bool {
	for _, m := range messages {
		if m.Pinned || m.Role == "system" {
			continue
		}
		// First non-pinned, non-system message must be "user".
		return m.Role == "user"
	}
	return true // No non-pinned messages (edge case).
}
