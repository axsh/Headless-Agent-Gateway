package subagent

import (
	"context"
	"fmt"
)

const summarySystemPrompt = `You are a result summarizer for a coding agent.
Given the raw output from a tool execution and the parent agent's objective,
produce a concise summary in the following format:

Status: [SUCCESS/FAILURE]
Summary: [3-5 line summary of results]
Key Findings / Errors: [Important errors, stack traces, or key data points if any]

Focus only on information relevant to the parent's objective.
Do NOT include progress logs, timestamps, or verbose output.`

// maxRawOutputLen is the maximum length of raw output to include in the summary prompt.
const maxRawOutputLen = 50000

// Summarizer produces concise summaries for parent consumption.
type Summarizer struct {
	llm LLMClient
}

// NewSummarizer creates a new Summarizer.
func NewSummarizer(llm LLMClient) *Summarizer {
	return &Summarizer{llm: llm}
}

// SummarizeForParent takes child session output and produces a summary
// tailored to the parent's needs based on hints.
func (s *Summarizer) SummarizeForParent(ctx context.Context, hints *Hints, rawOutput string) (string, error) {
	// Truncate very long output to avoid overwhelming the LLM context.
	truncatedOutput := rawOutput
	if len(truncatedOutput) > maxRawOutputLen {
		truncatedOutput = truncatedOutput[:maxRawOutputLen] + "\n... [OUTPUT TRUNCATED]"
	}

	prompt := fmt.Sprintf(
		"Parent's Objective: %s\nParent's Context: %s\n\nRaw Output:\n%s",
		hints.Objective, hints.Context, truncatedOutput,
	)

	messages := []ChatMessage{
		{Role: "system", Content: summarySystemPrompt},
		{Role: "user", Content: prompt},
	}

	resp, err := s.llm.GenerateMessage(ctx, "", messages, nil)
	if err != nil {
		return "", fmt.Errorf("summarization failed: %w", err)
	}

	return resp.Content, nil
}
