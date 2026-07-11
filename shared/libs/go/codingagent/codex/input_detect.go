package codex

import (
	"encoding/json"
	"strings"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

var stdinWaitPatterns = []string{
	"Reading additional input from stdin",
}

// DetectStdinWaitFromStderr reports whether a stderr line indicates stdin wait.
func DetectStdinWaitFromStderr(line string) (bool, string) {
	for _, p := range stdinWaitPatterns {
		if strings.Contains(line, p) {
			return true, line
		}
	}
	return false, ""
}

// DetectUserInputFromExecEvent parses a JSONL line for explicit user-input events.
// Returns nil unless the event has a structured choices field (R12: no heuristics).
func DetectUserInputFromExecEvent(line string) *codingagent.StreamEvent {
	var raw struct {
		Type     string   `json:"type"`
		Content  string   `json:"content,omitempty"`
		Message  string   `json:"message,omitempty"`
		PromptID string   `json:"prompt_id,omitempty"`
		Choices  []string `json:"choices"`
	}
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return nil
	}
	if raw.Choices == nil {
		return nil
	}
	content := raw.Content
	if content == "" {
		content = raw.Message
	}
	if content == "" {
		content = "Agent is waiting for user input"
	}
	return &codingagent.StreamEvent{
		Type:     codingagent.EventUserInputRequired,
		Content:  content,
		PromptID: raw.PromptID,
		Choices:  raw.Choices,
	}
}
