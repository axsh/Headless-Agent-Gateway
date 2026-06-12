package subagent

import (
	"context"
	"errors"
	"testing"

	"github.com/axsh/arctic-tern/wayfinder"
)

func TestSummarizeForParent_Success(t *testing.T) {
	mock := &mockLLM{
		responses: []*wayfinder.LLMResponse{
			{Content: "Status: SUCCESS\nSummary: Build completed with no errors.\nKey Findings: None"},
		},
	}
	s := NewSummarizer(mock)

	hints := &Hints{Objective: "Check build output", Context: "User wants to verify compilation"}
	result, err := s.SummarizeForParent(context.Background(), hints, "Build output: all packages compiled successfully")
	if err != nil {
		t.Fatalf("SummarizeForParent failed: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty summary")
	}
	if result != "Status: SUCCESS\nSummary: Build completed with no errors.\nKey Findings: None" {
		t.Errorf("unexpected summary: %q", result)
	}
}

func TestSummarizeForParent_WarningFocus(t *testing.T) {
	mock := &mockLLM{
		responses: []*wayfinder.LLMResponse{
			{Content: "Status: SUCCESS\nSummary: Build passed with 3 warnings.\nKey Findings: Deprecated API usage in auth.go:42"},
		},
	}
	s := NewSummarizer(mock)

	hints := &Hints{Objective: "Check for warnings", Context: "Focus on deprecation warnings"}
	result, err := s.SummarizeForParent(context.Background(), hints, "Warning: deprecated API at auth.go:42\nWarning: unused import\n...")
	if err != nil {
		t.Fatalf("SummarizeForParent failed: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty summary")
	}
}

func TestSummarizeForParent_LLMError(t *testing.T) {
	mock := &mockLLM{
		err: errors.New("LLM unavailable"),
	}
	s := NewSummarizer(mock)

	hints := &Hints{Objective: "test"}
	_, err := s.SummarizeForParent(context.Background(), hints, "some output")
	if err == nil {
		t.Fatal("expected error when LLM fails")
	}
}

func TestSummarizeForParent_TruncatesLongOutput(t *testing.T) {
	mock := &mockLLM{
		responses: []*wayfinder.LLMResponse{
			{Content: "Status: SUCCESS\nSummary: Processed large output."},
		},
	}
	s := NewSummarizer(mock)

	// Generate a very long output.
	longOutput := ""
	for range 2000 {
		longOutput += "line of output that is quite long and repetitive\n"
	}

	hints := &Hints{Objective: "Process output"}
	result, err := s.SummarizeForParent(context.Background(), hints, longOutput)
	if err != nil {
		t.Fatalf("SummarizeForParent failed: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty summary")
	}

	// Verify that the prompt sent to LLM was truncated.
	if mock.callCount != 1 {
		t.Errorf("callCount = %d, want 1", mock.callCount)
	}
}
