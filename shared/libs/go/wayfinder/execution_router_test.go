package wayfinder

import (
	"context"
	"errors"
	"testing"
)

func TestExecutionRouter_SimpleTask(t *testing.T) {
	mock := &MockLLMClient{
		Responses: []*LLMResponse{
			{Content: `{"route":"simple","reason":"single file read"}`},
		},
	}
	router := NewExecutionRouter(mock)

	route, reason, err := router.Route(context.Background(), "test-model", "Read the README file")
	if err != nil {
		t.Fatalf("Route failed: %v", err)
	}
	if route != RouteSimple {
		t.Errorf("route = %v, want RouteSimple", route)
	}
	if reason == "" {
		t.Error("expected non-empty reason")
	}
}

func TestExecutionRouter_ComplexTask(t *testing.T) {
	mock := &MockLLMClient{
		Responses: []*LLMResponse{
			{Content: `{"route":"planning","reason":"requires multiple files and steps"}`},
		},
	}
	router := NewExecutionRouter(mock)

	route, _, err := router.Route(context.Background(), "test-model", "Refactor the entire authentication module")
	if err != nil {
		t.Fatalf("Route failed: %v", err)
	}
	if route != RoutePlanning {
		t.Errorf("route = %v, want RoutePlanning", route)
	}
}

func TestExecutionRouter_LLMError(t *testing.T) {
	mock := &MockLLMClient{
		Errors: []error{errors.New("LLM unavailable")},
	}
	router := NewExecutionRouter(mock)

	route, _, err := router.Route(context.Background(), "test-model", "some task")
	if err != nil {
		t.Fatalf("Route should not error on LLM failure: %v", err)
	}
	if route != RouteSimple {
		t.Errorf("expected RouteSimple fallback, got %v", route)
	}
}

func TestExecutionRouter_InvalidJSON(t *testing.T) {
	mock := &MockLLMClient{
		Responses: []*LLMResponse{
			{Content: "this is not json"},
		},
	}
	router := NewExecutionRouter(mock)

	route, _, err := router.Route(context.Background(), "test-model", "some task")
	if err != nil {
		t.Fatalf("Route should not error on invalid JSON: %v", err)
	}
	if route != RouteSimple {
		t.Errorf("expected RouteSimple fallback, got %v", route)
	}
}
