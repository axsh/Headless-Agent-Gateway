package wayfinder

import (
	"context"
	"testing"
)

// MockLLMClient is a test double for LLMClient.
// It records calls and returns preconfigured responses.
type MockLLMClient struct {
	Responses []*LLMResponse
	Errors    []error
	CallCount int
	CallArgs  []mockCallArgs
}

type mockCallArgs struct {
	Model    string
	Messages []ChatMessage
	Tools    []ToolDefinition
}

func (m *MockLLMClient) GenerateMessage(ctx context.Context, logicalModel string, messages []ChatMessage, tools []ToolDefinition, opts ...GenerateOptions) (*LLMResponse, error) {
	idx := m.CallCount
	m.CallCount++
	m.CallArgs = append(m.CallArgs, mockCallArgs{
		Model:    logicalModel,
		Messages: messages,
		Tools:    tools,
	})
	if idx < len(m.Errors) && m.Errors[idx] != nil {
		return nil, m.Errors[idx]
	}
	if idx < len(m.Responses) {
		return m.Responses[idx], nil
	}
	return &LLMResponse{Content: "default mock response"}, nil
}

// TestMockLLMClient_ImplementsInterface verifies that MockLLMClient
// satisfies the LLMClient interface at compile time.
func TestMockLLMClient_ImplementsInterface(t *testing.T) {
	var _ LLMClient = (*MockLLMClient)(nil)
}

func TestMockLLMClient_ReturnsConfiguredResponse(t *testing.T) {
	mock := &MockLLMClient{
		Responses: []*LLMResponse{
			{Content: "hello from mock"},
		},
	}

	resp, err := mock.GenerateMessage(context.Background(), "test-model", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "hello from mock" {
		t.Errorf("Content = %q, want %q", resp.Content, "hello from mock")
	}
	if mock.CallCount != 1 {
		t.Errorf("CallCount = %d, want 1", mock.CallCount)
	}
	if mock.CallArgs[0].Model != "test-model" {
		t.Errorf("Model = %q, want %q", mock.CallArgs[0].Model, "test-model")
	}
}

func TestMockLLMClient_ReturnsError(t *testing.T) {
	mock := &MockLLMClient{
		Errors: []error{context.DeadlineExceeded},
	}

	_, err := mock.GenerateMessage(context.Background(), "model", nil, nil)
	if err != context.DeadlineExceeded {
		t.Errorf("err = %v, want context.DeadlineExceeded", err)
	}
}

func TestMockLLMClient_DefaultResponse(t *testing.T) {
	mock := &MockLLMClient{}

	resp, err := mock.GenerateMessage(context.Background(), "model", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "default mock response" {
		t.Errorf("Content = %q, want %q", resp.Content, "default mock response")
	}
}
