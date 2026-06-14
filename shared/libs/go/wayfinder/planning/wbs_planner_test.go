package planning

import (
	"context"
	"testing"
)

// mockLLM is a test helper for the planning package.
type mockLLM struct {
	responses []*LLMResponse
	err       error
	callCount int
}

func (m *mockLLM) GenerateMessage(_ context.Context, _ string, _ []ChatMessage, _ []ToolDefinition, _ ...GenerateOptions) (*LLMResponse, error) {
	m.callCount++
	if m.err != nil {
		return nil, m.err
	}
	if m.callCount <= len(m.responses) {
		return m.responses[m.callCount-1], nil
	}
	return &LLMResponse{Content: ""}, nil
}

func TestWBSPlanner_GenerateWBS_Success(t *testing.T) {
	wbsJSON := `{
		"root_nodes": [
			{"id": "1", "name": "Setup", "description": "Setup env", "status": "pending", "dependencies": []},
			{"id": "2", "name": "Build", "description": "Build project", "status": "pending", "dependencies": ["1"]}
		]
	}`
	mock := &mockLLM{
		responses: []*LLMResponse{{Content: wbsJSON}},
	}
	planner := NewWBSPlanner(mock)

	tree, err := planner.GenerateWBS(context.Background(), "test-model", "Build a web app")
	if err != nil {
		t.Fatalf("GenerateWBS failed: %v", err)
	}
	if len(tree.RootNodes) != 2 {
		t.Fatalf("expected 2 root nodes, got %d", len(tree.RootNodes))
	}
	if tree.RootNodes[0].ID != "1" {
		t.Errorf("root[0].ID = %q, want %q", tree.RootNodes[0].ID, "1")
	}
	if tree.RootNodes[1].Dependencies[0] != "1" {
		t.Errorf("root[1] should depend on 1")
	}
}

func TestWBSPlanner_GenerateWBS_InvalidJSON(t *testing.T) {
	mock := &mockLLM{
		responses: []*LLMResponse{{Content: "this is not json"}},
	}
	planner := NewWBSPlanner(mock)

	_, err := planner.GenerateWBS(context.Background(), "test-model", "task")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestWBSPlanner_GenerateWBS_AllStatusesPending(t *testing.T) {
	// LLM might return statuses other than "pending"; planner should normalize.
	wbsJSON := `{
		"root_nodes": [
			{"id": "1", "name": "Step", "description": "desc", "status": "completed", "dependencies": []},
			{"id": "2", "name": "Step2", "description": "desc2", "status": "running", "dependencies": []}
		]
	}`
	mock := &mockLLM{
		responses: []*LLMResponse{{Content: wbsJSON}},
	}
	planner := NewWBSPlanner(mock)

	tree, err := planner.GenerateWBS(context.Background(), "test-model", "task")
	if err != nil {
		t.Fatalf("GenerateWBS failed: %v", err)
	}
	for _, n := range tree.RootNodes {
		if n.Status != StatusPending {
			t.Errorf("node %q status = %q, want %q", n.ID, n.Status, StatusPending)
		}
	}
}

func TestWBSPlanner_ExtractJSON_CodeBlock(t *testing.T) {
	wrapped := "Here is the plan:\n```json\n{\"root_nodes\":[{\"id\":\"1\",\"name\":\"Test\",\"description\":\"d\",\"status\":\"pending\",\"dependencies\":[]}]}\n```\nDone."
	mock := &mockLLM{
		responses: []*LLMResponse{{Content: wrapped}},
	}
	planner := NewWBSPlanner(mock)

	tree, err := planner.GenerateWBS(context.Background(), "test-model", "task")
	if err != nil {
		t.Fatalf("GenerateWBS failed: %v", err)
	}
	if len(tree.RootNodes) != 1 {
		t.Fatalf("expected 1 root node, got %d", len(tree.RootNodes))
	}
}

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "plain JSON",
			input: `{"root_nodes":[]}`,
			want:  `{"root_nodes":[]}`,
		},
		{
			name:  "code block",
			input: "```json\n{\"root_nodes\":[]}\n```",
			want:  `{"root_nodes":[]}`,
		},
		{
			name:  "surrounded text",
			input: "Here is the plan:\n{\"root_nodes\":[]}\nDone.",
			want:  `{"root_nodes":[]}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractJSON(tt.input)
			if got != tt.want {
				t.Errorf("extractJSON() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGenerateWBS_WithStructuredOutput(t *testing.T) {
	// When structured output is enabled, the planner should use the response directly
	// without extractJSON processing.
	wbsJSON := `{"root_nodes":[{"id":"1","name":"Deploy","description":"Deploy app","status":"pending","dependencies":[]}]}`
	mock := &mockLLM{
		responses: []*LLMResponse{{Content: wbsJSON}},
	}
	planner := NewWBSPlanner(mock)
	planner.SetStructuredOutput(true)

	tree, err := planner.GenerateWBS(context.Background(), "test-model", "Deploy application")
	if err != nil {
		t.Fatalf("GenerateWBS failed: %v", err)
	}
	if len(tree.RootNodes) != 1 {
		t.Fatalf("expected 1 root node, got %d", len(tree.RootNodes))
	}
	if tree.RootNodes[0].Name != "Deploy" {
		t.Errorf("node name = %q, want %q", tree.RootNodes[0].Name, "Deploy")
	}
}

func TestGenerateWBS_FallbackWithoutStructuredOutput(t *testing.T) {
	// Without structured output, the planner should use extractJSON to unwrap
	// markdown code blocks.
	wrapped := "```json\n{\"root_nodes\":[{\"id\":\"1\",\"name\":\"Test\",\"description\":\"d\",\"status\":\"pending\",\"dependencies\":[]}]}\n```"
	mock := &mockLLM{
		responses: []*LLMResponse{{Content: wrapped}},
	}
	planner := NewWBSPlanner(mock)
	// useStructuredOutput defaults to false

	tree, err := planner.GenerateWBS(context.Background(), "test-model", "task")
	if err != nil {
		t.Fatalf("GenerateWBS failed: %v", err)
	}
	if len(tree.RootNodes) != 1 {
		t.Fatalf("expected 1 root node, got %d", len(tree.RootNodes))
	}
}
