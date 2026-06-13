package subagent

import (
	"context"
	"errors"
	"testing"
)

func TestSummarizeForParent_Success(t *testing.T) {
	mock := &mockLLM{
		responses: []*LLMResponse{
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
		responses: []*LLMResponse{
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
		responses: []*LLMResponse{
			{Content: "Status: SUCCESS\nSummary: Processed large output."},
		},
	}
	s := NewSummarizer(mock)

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
	if mock.callCount != 1 {
		t.Errorf("callCount = %d, want 1", mock.callCount)
	}
}

// ---- DetailedSummarizer.Summarize tests ----

func TestDetailedSummarizer_Summarize(t *testing.T) {
	mock := &mockLLM{
		responses: []*LLMResponse{
			{Content: "Status: SUCCESS\nSummary: Build OK.\nKey Findings: None"},
		},
	}
	s := NewDetailedSummarizer(mock)

	hints := &Hints{Objective: "Build project", Context: "User wants to build"}
	result, err := s.Summarize(context.Background(), hints, "Build passed with 0 errors.")
	if err != nil {
		t.Fatalf("Summarize failed: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty result")
	}
}

func TestDetailedSummarizer_ImplementsStrategy(t *testing.T) {
	var _ SummaryStrategy = (*DetailedSummarizer)(nil)
}

// ---- OutcomeSummarizer tests ----

func TestOutcomeSummarizer_CompactOutput(t *testing.T) {
	mock := &mockLLM{
		responses: []*LLMResponse{
			{Content: "The build step completed successfully with no errors."},
		},
	}
	s := NewOutcomeSummarizer(mock)

	hints := &Hints{Objective: "Build project", Context: "CI build step"}
	result, err := s.Summarize(context.Background(), hints, "Build passed. 0 errors, 0 warnings.")
	if err != nil {
		t.Fatalf("Summarize failed: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty result")
	}
	// Verify LLM was called once.
	if mock.callCount != 1 {
		t.Errorf("callCount = %d, want 1", mock.callCount)
	}
}

func TestOutcomeSummarizer_LLMError(t *testing.T) {
	mock := &mockLLM{
		err: errors.New("LLM unavailable"),
	}
	s := NewOutcomeSummarizer(mock)

	hints := &Hints{Objective: "test"}
	_, err := s.Summarize(context.Background(), hints, "some output")
	if err == nil {
		t.Fatal("expected error when LLM fails")
	}
}

func TestOutcomeSummarizer_ImplementsStrategy(t *testing.T) {
	var _ SummaryStrategy = (*OutcomeSummarizer)(nil)
}
