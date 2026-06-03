package tasklog

import (
	"sync"
	"testing"
)

func TestLogStack_BasicOperations(t *testing.T) {
	stack := &LogStack{}

	if parent := stack.CurrentParentID(); parent != "" {
		t.Errorf("expected empty parent for new stack, got %q", parent)
	}

	stack.Push("id-1")
	if parent := stack.CurrentParentID(); parent != "id-1" {
		t.Errorf("expected parent 'id-1', got %q", parent)
	}

	stack.Push("id-2")
	if parent := stack.CurrentParentID(); parent != "id-2" {
		t.Errorf("expected parent 'id-2', got %q", parent)
	}

	if popped := stack.Pop(); popped != "id-2" {
		t.Errorf("expected popped 'id-2', got %q", popped)
	}

	if parent := stack.CurrentParentID(); parent != "id-1" {
		t.Errorf("expected parent 'id-1', got %q", parent)
	}

	if popped := stack.Pop(); popped != "id-1" {
		t.Errorf("expected popped 'id-1', got %q", popped)
	}

	if parent := stack.CurrentParentID(); parent != "" {
		t.Errorf("expected empty parent after pops, got %q", parent)
	}

	if popped := stack.Pop(); popped != "" {
		t.Errorf("expected empty pop, got %q", popped)
	}
}

func TestLogStack_Concurrency(t *testing.T) {
	stack := &LogStack{}
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(val string) {
			defer wg.Done()
			stack.Push(val)
			stack.CurrentParentID()
			stack.Pop()
		}(string(rune(i)))
	}

	wg.Wait()
	if parent := stack.CurrentParentID(); parent != "" {
		t.Errorf("expected final stack to be empty, got %q", parent)
	}
}
