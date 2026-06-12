package planning

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// LLMClient is the interface for LLM communication.
// Defined locally to avoid cyclic import with the wayfinder root package.
type LLMClient interface {
	GenerateMessage(ctx context.Context, model string, messages []ChatMessage, tools []ToolDefinition) (*LLMResponse, error)
}

// ChatMessage is a message in the LLM conversation.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ToolDefinition describes a tool available to the LLM.
type ToolDefinition struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"input_schema"`
}

// LLMResponse is the response from an LLM call.
type LLMResponse struct {
	Content string `json:"content"`
}

// WBSPlanner generates WBS plans using LLM Structured Output.
type WBSPlanner struct {
	llm LLMClient
}

// NewWBSPlanner creates a new WBSPlanner.
func NewWBSPlanner(llm LLMClient) *WBSPlanner {
	return &WBSPlanner{llm: llm}
}

const wbsPlannerSystemPrompt = `You are a task planning agent.
Given a user's request, break it down into a hierarchical Work Breakdown Structure (WBS).
Output a JSON object with the following schema:
{
  "root_nodes": [
    {
      "id": "1",
      "name": "Step name",
      "description": "Detailed instruction for this step",
      "status": "pending",
      "dependencies": [],
      "sub_steps": []
    }
  ]
}

Rules:
- Use hierarchical IDs: "1", "1.1", "1.2", "2", etc.
- All statuses must be "pending"
- Set dependencies to IDs of steps that must complete first
- Keep each step atomic and actionable
- Sub-steps represent breakdown of a parent step
- Respond ONLY with valid JSON. No markdown wrappers, no explanation.`

// GenerateWBS creates a WBS tree from the user's request.
func (p *WBSPlanner) GenerateWBS(ctx context.Context, model string, userRequest string) (*WBSTree, error) {
	messages := []ChatMessage{
		{Role: "system", Content: wbsPlannerSystemPrompt},
		{Role: "user", Content: userRequest},
	}

	resp, err := p.llm.GenerateMessage(ctx, model, messages, nil)
	if err != nil {
		return nil, fmt.Errorf("WBS generation failed: %w", err)
	}

	jsonStr := extractJSON(resp.Content)
	var tree WBSTree
	if err := json.Unmarshal([]byte(jsonStr), &tree); err != nil {
		return nil, fmt.Errorf("failed to parse WBS JSON: %w", err)
	}

	// Normalize: all statuses should be "pending".
	tree.walkNodesMut(func(node *WBSNode) {
		node.Status = StatusPending
	})

	return &tree, nil
}

// extractJSON extracts JSON content from LLM response.
// Handles cases where JSON is wrapped in markdown code blocks or surrounded by text.
func extractJSON(content string) string {
	content = strings.TrimSpace(content)

	// Try to extract from ```json ... ``` code block.
	if idx := strings.Index(content, "```json"); idx >= 0 {
		start := idx + len("```json")
		if endIdx := strings.Index(content[start:], "```"); endIdx >= 0 {
			return strings.TrimSpace(content[start : start+endIdx])
		}
	}

	// Try to extract from ``` ... ``` code block.
	if idx := strings.Index(content, "```"); idx >= 0 {
		start := idx + len("```")
		if endIdx := strings.Index(content[start:], "```"); endIdx >= 0 {
			return strings.TrimSpace(content[start : start+endIdx])
		}
	}

	// Try to find JSON by locating outermost { ... }.
	if idx := strings.Index(content, "{"); idx >= 0 {
		if endIdx := strings.LastIndex(content, "}"); endIdx > idx {
			return content[idx : endIdx+1]
		}
	}

	return content
}
