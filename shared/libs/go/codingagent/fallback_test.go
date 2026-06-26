package codingagent_test

import (
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

func TestParseFallbackToolCalls_SingleObject(t *testing.T) {
	input := `{"name": "Write", "arguments": {"path": "main.go", "content": "package main"}}`
	calls, ok := codingagent.ParseFallbackToolCalls(input)
	if !ok {
		t.Fatal("ParseFallbackToolCalls should return ok=true")
	}
	if len(calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(calls))
	}
	if calls[0].Name != "Write" {
		t.Errorf("Name = %v, want Write", calls[0].Name)
	}
	if calls[0].Arguments["path"] != "main.go" {
		t.Errorf("Arguments[path] = %v, want main.go", calls[0].Arguments["path"])
	}
}

func TestParseFallbackToolCalls_Array(t *testing.T) {
	input := `[{"name": "Write", "arguments": {"path": "a.go"}}, {"name": "Read", "arguments": {"path": "b.go"}}]`
	calls, ok := codingagent.ParseFallbackToolCalls(input)
	if !ok {
		t.Fatal("ParseFallbackToolCalls should return ok=true")
	}
	if len(calls) != 2 {
		t.Fatalf("len(calls) = %d, want 2", len(calls))
	}
	if calls[0].Name != "Write" {
		t.Errorf("calls[0].Name = %v, want Write", calls[0].Name)
	}
	if calls[1].Name != "Read" {
		t.Errorf("calls[1].Name = %v, want Read", calls[1].Name)
	}
}

func TestParseFallbackToolCalls_MarkdownFence(t *testing.T) {
	input := "```json\n{\"name\": \"Bash\", \"arguments\": {\"command\": \"ls\"}}\n```"
	calls, ok := codingagent.ParseFallbackToolCalls(input)
	if !ok {
		t.Fatal("ParseFallbackToolCalls should return ok=true")
	}
	if len(calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(calls))
	}
	if calls[0].Name != "Bash" {
		t.Errorf("Name = %v, want Bash", calls[0].Name)
	}
}

func TestParseFallbackToolCalls_AllToolTypes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantName string
	}{
		{
			name:     "Write",
			input:    `{"name": "Write", "arguments": {"path": "a.go", "content": "pkg"}}`,
			wantName: "Write",
		},
		{
			name:     "Read",
			input:    `{"name": "Read", "arguments": {"path": "a.go"}}`,
			wantName: "Read",
		},
		{
			name:     "Edit",
			input:    `{"name": "Edit", "arguments": {"path": "a.go", "old": "x", "new": "y"}}`,
			wantName: "Edit",
		},
		{
			name:     "Bash",
			input:    `{"name": "Bash", "arguments": {"command": "ls -la"}}`,
			wantName: "Bash",
		},
		{
			name:     "Glob",
			input:    `{"name": "Glob", "arguments": {"pattern": "*.go"}}`,
			wantName: "Glob",
		},
		{
			name:     "Grep",
			input:    `{"name": "Grep", "arguments": {"pattern": "TODO", "path": "."}}`,
			wantName: "Grep",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls, ok := codingagent.ParseFallbackToolCalls(tt.input)
			if !ok {
				t.Fatal("ParseFallbackToolCalls should return ok=true")
			}
			if calls[0].Name != tt.wantName {
				t.Errorf("Name = %v, want %v", calls[0].Name, tt.wantName)
			}
		})
	}
}

func TestParseFallbackToolCalls_Invalid(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"empty string", ""},
		{"plain text", "this is not json"},
		{"json without name", `{"arguments": {"path": "a.go"}}`},
		{"json number", "42"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls, ok := codingagent.ParseFallbackToolCalls(tt.input)
			if ok {
				t.Errorf("ParseFallbackToolCalls should return ok=false, got calls=%v", calls)
			}
		})
	}
}

func TestStripMarkdownCodeFence(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "json fence",
			input:    "```json\n{}\n```",
			expected: "{}",
		},
		{
			name:     "plain fence",
			input:    "```\n{}\n```",
			expected: "{}",
		},
		{
			name:     "fence with surrounding text",
			input:    "text before\n```json\n{}\n```\ntext after",
			expected: "{}",
		},
		{
			name:     "no fence",
			input:    "{}",
			expected: "{}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := codingagent.StripMarkdownCodeFence(tt.input)
			if got != tt.expected {
				t.Errorf("StripMarkdownCodeFence = %q, want %q", got, tt.expected)
			}
		})
	}
}
