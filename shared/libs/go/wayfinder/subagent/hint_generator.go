package subagent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/axsh/arctic-tern/wayfinder"
)

// ParentMessage is a simplified message from the parent session used for hint generation.
type ParentMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Hints carries meta-information for the child session.
type Hints struct {
	Objective   string `json:"objective"`
	Context     string `json:"context"`
	Constraints string `json:"constraints"`
}

// HintGenerator creates hints from parent context.
type HintGenerator struct {
	llm wayfinder.LLMClient
}

// NewHintGenerator creates a new HintGenerator.
func NewHintGenerator(llm wayfinder.LLMClient) *HintGenerator {
	return &HintGenerator{llm: llm}
}

const hintSystemPrompt = `You are analyzing a parent agent's conversation to extract context for a child agent.
Given the recent messages and a tool call, respond with a JSON object:
{"objective":"what the parent wants to know","context":"relevant conversation context","constraints":"any constraints or focus areas"}
Respond ONLY with valid JSON. No markdown, no explanation.`

// maxRecentMessages is the number of recent parent messages to include in context.
const maxRecentMessages = 5

// GenerateHints analyzes parent messages and tool params to extract hints.
func (h *HintGenerator) GenerateHints(
	ctx context.Context,
	parentMessages []ParentMessage,
	toolName string,
	toolInput map[string]any,
) (*Hints, error) {
	hintPrompt := buildHintExtractionPrompt(parentMessages, toolName, toolInput)

	messages := []wayfinder.ChatMessage{
		{Role: "system", Content: hintSystemPrompt},
		{Role: "user", Content: hintPrompt},
	}

	resp, err := h.llm.GenerateMessage(ctx, "", messages, nil)
	if err != nil {
		return nil, fmt.Errorf("hint generation failed: %w", err)
	}

	hints, err := parseHintsFromResponse(resp.Content)
	if err != nil {
		// Fallback: use the raw response as objective.
		return &Hints{
			Objective: resp.Content,
			Context:   "Failed to parse structured hints",
		}, nil
	}
	return hints, nil
}

// buildHintExtractionPrompt constructs the prompt for hint extraction.
func buildHintExtractionPrompt(messages []ParentMessage, toolName string, toolInput map[string]any) string {
	// Take only the most recent messages.
	recent := messages
	if len(recent) > maxRecentMessages {
		recent = recent[len(recent)-maxRecentMessages:]
	}

	var sb strings.Builder
	sb.WriteString("Recent conversation:\n")
	for _, m := range recent {
		content := m.Content
		if len(content) > 200 {
			content = content[:200] + "..."
		}
		sb.WriteString(fmt.Sprintf("  [%s]: %s\n", m.Role, content))
	}
	sb.WriteString(fmt.Sprintf("\nTool being called: %s\n", toolName))
	inputJSON, _ := json.Marshal(toolInput)
	sb.WriteString(fmt.Sprintf("Tool input: %s\n", string(inputJSON)))

	return sb.String()
}

// parseHintsFromResponse parses a Hints struct from LLM response text.
func parseHintsFromResponse(content string) (*Hints, error) {
	// Try to find JSON in the response.
	content = strings.TrimSpace(content)

	// Try direct parse first.
	var hints Hints
	if err := json.Unmarshal([]byte(content), &hints); err == nil {
		return &hints, nil
	}

	// Try to extract JSON from markdown code blocks.
	if idx := strings.Index(content, "{"); idx >= 0 {
		if endIdx := strings.LastIndex(content, "}"); endIdx > idx {
			jsonStr := content[idx : endIdx+1]
			if err := json.Unmarshal([]byte(jsonStr), &hints); err == nil {
				return &hints, nil
			}
		}
	}

	return nil, fmt.Errorf("failed to parse hints from response")
}
