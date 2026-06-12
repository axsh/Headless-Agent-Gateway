package wayfinder

import (
	"context"
	"encoding/json"
	"strings"
)

// ExecutionRoute represents the determined execution path.
type ExecutionRoute int

const (
	// RouteSimple is direct tool execution without planning.
	RouteSimple ExecutionRoute = iota
	// RoutePlanning is WBS planning + orchestrated execution.
	RoutePlanning
)

// ExecutionRouter determines the execution path based on task complexity.
type ExecutionRouter struct {
	llm LLMClient
}

// NewExecutionRouter creates a new ExecutionRouter.
func NewExecutionRouter(llm LLMClient) *ExecutionRouter {
	return &ExecutionRouter{llm: llm}
}

const routerSystemPrompt = `You are a task complexity analyzer.
Given a user's request, determine if it requires planning or can be executed directly.

Respond with a JSON object:
{"route": "simple" or "planning", "reason": "brief explanation"}

Guidelines for "planning" route:
- Multiple files need to be created or modified
- Multiple sequential steps with dependencies
- Complex refactoring or architectural changes
- Tasks requiring investigation followed by implementation

Guidelines for "simple" route:
- Single file read/write
- Simple question answering
- Single command execution
- Minor edits or fixes

Respond ONLY with valid JSON.`

// Route analyzes the user prompt and returns the execution route.
func (r *ExecutionRouter) Route(ctx context.Context, model string, prompt string) (ExecutionRoute, string, error) {
	messages := []ChatMessage{
		{Role: "system", Content: routerSystemPrompt},
		{Role: "user", Content: prompt},
	}

	resp, err := r.llm.GenerateMessage(ctx, model, messages, nil)
	if err != nil {
		// Default to simple on error.
		return RouteSimple, "routing failed, defaulting to simple", nil
	}

	jsonStr := extractRouterJSON(resp.Content)
	var result struct {
		Route  string `json:"route"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return RouteSimple, "failed to parse routing response", nil
	}

	if result.Route == "planning" {
		return RoutePlanning, result.Reason, nil
	}
	return RouteSimple, result.Reason, nil
}

// extractRouterJSON extracts JSON from LLM response.
func extractRouterJSON(content string) string {
	content = strings.TrimSpace(content)

	// Try to find JSON by locating { ... }.
	if idx := strings.Index(content, "{"); idx >= 0 {
		if endIdx := strings.LastIndex(content, "}"); endIdx > idx {
			return content[idx : endIdx+1]
		}
	}

	return content
}
