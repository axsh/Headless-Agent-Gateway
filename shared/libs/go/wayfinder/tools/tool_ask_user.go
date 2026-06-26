package tools

import (
	"context"
	"errors"
	"fmt"
)

// ErrFeedbackRequired is returned by ask_user to signal that user feedback is needed.
// The agent loop should catch this error and suspend execution.
var ErrFeedbackRequired = errors.New("user feedback required")

func newAskUser(tc *ToolContext) ToolHandler {
	return func(ctx context.Context, input map[string]any) (string, error) {
		prompt, _ := input["prompt"].(string)
		if prompt == "" {
			return "", fmt.Errorf("ask_user: 'prompt' is required")
		}
		// Return the prompt as the result content (for context persistence),
		// and signal ErrFeedbackRequired to suspend execution.
		return fmt.Sprintf("[WAITING FOR USER] %s", prompt), ErrFeedbackRequired
	}
}
