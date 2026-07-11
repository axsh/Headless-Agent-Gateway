package tools

import (
	"context"
	"errors"
	"fmt"
)

// ErrFeedbackRequired is returned by ask_user to signal that user feedback is needed.
// The agent loop should catch this error and suspend execution.
var ErrFeedbackRequired = errors.New("user feedback required")

// FeedbackRequest carries structured ask_user payload for suspension handling.
type FeedbackRequest struct {
	Prompt  string
	Choices []string
}

type feedbackCarrier struct {
	err     error
	request FeedbackRequest
}

func (e *feedbackCarrier) Error() string { return e.err.Error() }
func (e *feedbackCarrier) Unwrap() error { return e.err }

// FeedbackFromError extracts ask_user payload from ErrFeedbackRequired.
func FeedbackFromError(err error) (FeedbackRequest, bool) {
	var carrier *feedbackCarrier
	if errors.As(err, &carrier) {
		return carrier.request, true
	}
	return FeedbackRequest{}, false
}

func parseChoices(input map[string]any) []string {
	raw, ok := input["choices"]
	if !ok || raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func newAskUser(tc *ToolContext) ToolHandler {
	return func(ctx context.Context, input map[string]any) (string, error) {
		prompt, _ := input["prompt"].(string)
		if prompt == "" {
			return "", fmt.Errorf("ask_user: 'prompt' is required")
		}
		choices := parseChoices(input)
		return fmt.Sprintf("[WAITING FOR USER] %s", prompt), &feedbackCarrier{
			err: ErrFeedbackRequired,
			request: FeedbackRequest{
				Prompt:  prompt,
				Choices: choices,
			},
		}
	}
}
