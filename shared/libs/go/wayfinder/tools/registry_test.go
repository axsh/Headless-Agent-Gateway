package tools

import (
	"context"
	"testing"
)

func TestRegistry_RegisterAndGet(t *testing.T) {
	reg := NewRegistry()
	handler := func(ctx context.Context, input map[string]any) (string, error) {
		return "result", nil
	}
	reg.Register("test_tool", "A test tool", nil, handler)

	tool, ok := reg.Get("test_tool")
	if !ok {
		t.Fatal("expected tool to be registered")
	}
	if tool.Name != "test_tool" {
		t.Errorf("Name = %q, want %q", tool.Name, "test_tool")
	}
	if tool.Description != "A test tool" {
		t.Errorf("Description = %q, want %q", tool.Description, "A test tool")
	}
	result, err := tool.Handler(context.Background(), nil)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result != "result" {
		t.Errorf("handler result = %q, want %q", result, "result")
	}
}

func TestRegistry_GetMissing(t *testing.T) {
	reg := NewRegistry()
	_, ok := reg.Get("nonexistent")
	if ok {
		t.Error("expected Get to return false for nonexistent tool")
	}
}

func TestRegistry_Definitions(t *testing.T) {
	reg := NewRegistry()
	reg.Register("tool_a", "Tool A", map[string]any{"type": "object"}, nil)
	reg.Register("tool_b", "Tool B", map[string]any{"type": "object"}, nil)

	defs := reg.Definitions()
	if len(defs) != 2 {
		t.Fatalf("len(Definitions) = %d, want 2", len(defs))
	}
	names := map[string]bool{}
	for _, d := range defs {
		names[d.Name] = true
	}
	if !names["tool_a"] || !names["tool_b"] {
		t.Errorf("Definitions missing expected tools: %v", defs)
	}
}
