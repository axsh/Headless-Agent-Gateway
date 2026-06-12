package subagent

import (
	"context"
	"errors"
	"testing"

	"github.com/axsh/arctic-tern/wayfinder"
)

// mockLLM is a test helper LLM client for the subagent package.
type mockLLM struct {
	responses []*wayfinder.LLMResponse
	err       error
	callCount int
}

func (m *mockLLM) GenerateMessage(_ context.Context, _ string, _ []wayfinder.ChatMessage, _ []wayfinder.ToolDefinition) (*wayfinder.LLMResponse, error) {
	m.callCount++
	if m.err != nil {
		return nil, m.err
	}
	if m.callCount <= len(m.responses) {
		return m.responses[m.callCount-1], nil
	}
	return &wayfinder.LLMResponse{Content: ""}, nil
}

func TestGenerateHints_ExtractsObjective(t *testing.T) {
	mock := &mockLLM{
		responses: []*wayfinder.LLMResponse{
			{Content: `{"objective":"Check build output","context":"User asked to verify compilation","constraints":"Focus on errors"}`},
		},
	}
	gen := NewHintGenerator(mock)

	hints, err := gen.GenerateHints(context.Background(), []ParentMessage{
		{Role: "user", Content: "Please build the project and check for errors"},
	}, "execute_command", map[string]any{"command": "go build ./..."})
	if err != nil {
		t.Fatalf("GenerateHints failed: %v", err)
	}
	if hints.Objective != "Check build output" {
		t.Errorf("Objective = %q, want %q", hints.Objective, "Check build output")
	}
	if hints.Context == "" {
		t.Error("Context should not be empty")
	}
}

func TestGenerateHints_ContextFromRecentMessages(t *testing.T) {
	mock := &mockLLM{
		responses: []*wayfinder.LLMResponse{
			{Content: `{"objective":"Run tests","context":"Recent conversation about testing","constraints":""}`},
		},
	}
	gen := NewHintGenerator(mock)

	// Create 10 messages; only last 5 should be used.
	messages := make([]ParentMessage, 10)
	for i := range 10 {
		messages[i] = ParentMessage{Role: "user", Content: "msg"}
	}

	hints, err := gen.GenerateHints(context.Background(), messages, "execute_command", map[string]any{"command": "go test"})
	if err != nil {
		t.Fatalf("GenerateHints failed: %v", err)
	}
	if hints == nil {
		t.Fatal("hints should not be nil")
	}
	if mock.callCount != 1 {
		t.Errorf("callCount = %d, want 1", mock.callCount)
	}
}

func TestGenerateHints_LLMError(t *testing.T) {
	mock := &mockLLM{
		err: errors.New("LLM unavailable"),
	}
	gen := NewHintGenerator(mock)

	_, err := gen.GenerateHints(context.Background(), []ParentMessage{
		{Role: "user", Content: "test"},
	}, "execute_command", map[string]any{})
	if err == nil {
		t.Fatal("expected error when LLM fails")
	}
}

func TestGenerateHints_InvalidJSON(t *testing.T) {
	mock := &mockLLM{
		responses: []*wayfinder.LLMResponse{
			{Content: "This is not valid JSON at all"},
		},
	}
	gen := NewHintGenerator(mock)

	hints, err := gen.GenerateHints(context.Background(), []ParentMessage{
		{Role: "user", Content: "test"},
	}, "execute_command", map[string]any{})
	if err != nil {
		t.Fatalf("should not error on invalid JSON, got: %v", err)
	}
	if hints.Objective == "" {
		t.Error("Objective should have a fallback value")
	}
}

