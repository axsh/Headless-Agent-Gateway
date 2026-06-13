package subagent

import (
	"context"
	"fmt"
)

// SummaryStrategy defines the summarization approach.
// Different implementations optimize for different consumers.
type SummaryStrategy interface {
	Summarize(ctx context.Context, hints *Hints, rawOutput string) (string, error)
}

// --- DetailedSummarizer ---

const detailedSummarySystemPrompt = `You are a result summarizer for a coding agent.
Given the raw output from a tool execution and the parent agent's objective,
produce a concise summary in the following format:

Status: [SUCCESS/FAILURE]
Summary: [3-5 line summary of results]
Key Findings / Errors: [Important errors, stack traces, or key data points if any]

Focus only on information relevant to the parent's objective.
Do NOT include progress logs, timestamps, or verbose output.`

// maxRawOutputLen is the maximum length of raw output to include in the summary prompt.
const maxRawOutputLen = 50000

// DetailedSummarizer preserves tool call structure and key data points.
// Used by: Tool Calling subagent path (SubagentExecutor).
type DetailedSummarizer struct {
	llm LLMClient
}

// NewDetailedSummarizer creates a new DetailedSummarizer.
func NewDetailedSummarizer(llm LLMClient) *DetailedSummarizer {
	return &DetailedSummarizer{llm: llm}
}

// Summarize produces a structured summary preserving key data points.
func (s *DetailedSummarizer) Summarize(ctx context.Context, hints *Hints, rawOutput string) (string, error) {
	return summarizeWithPrompt(ctx, s.llm, detailedSummarySystemPrompt, hints, rawOutput)
}

// SummarizeForParent is a backward-compatible alias for Summarize.
func (s *DetailedSummarizer) SummarizeForParent(ctx context.Context, hints *Hints, rawOutput string) (string, error) {
	return s.Summarize(ctx, hints, rawOutput)
}

// --- OutcomeSummarizer ---

const outcomeSummarySystemPrompt = `You are summarizing the result of a subtask execution.
Describe what was done and whether the objective was achieved in 1-3 sentences.
Do not include tool call details, file listings, or raw command output.
Focus only on the outcome: what happened and whether it succeeded or failed.`

// OutcomeSummarizer focuses on success/failure and high-level outcome.
// Used by: WBS Planning node executor (agentNodeExecutor).
type OutcomeSummarizer struct {
	llm LLMClient
}

// NewOutcomeSummarizer creates a new OutcomeSummarizer.
func NewOutcomeSummarizer(llm LLMClient) *OutcomeSummarizer {
	return &OutcomeSummarizer{llm: llm}
}

// Summarize produces a compact outcome-focused summary.
func (s *OutcomeSummarizer) Summarize(ctx context.Context, hints *Hints, rawOutput string) (string, error) {
	return summarizeWithPrompt(ctx, s.llm, outcomeSummarySystemPrompt, hints, rawOutput)
}

// --- Backward-compatible aliases ---

// Summarizer is an alias for DetailedSummarizer (backward compatibility).
type Summarizer = DetailedSummarizer

// NewSummarizer creates a new DetailedSummarizer (backward compatibility).
func NewSummarizer(llm LLMClient) *Summarizer {
	return NewDetailedSummarizer(llm)
}

// --- Shared helper ---

// summarizeWithPrompt is the common summarization logic shared by all strategies.
func summarizeWithPrompt(ctx context.Context, llm LLMClient, systemPrompt string, hints *Hints, rawOutput string) (string, error) {
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
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: prompt},
	}

	resp, err := llm.GenerateMessage(ctx, "", messages, nil)
	if err != nil {
		return "", fmt.Errorf("summarization failed: %w", err)
	}

	return resp.Content, nil
}
