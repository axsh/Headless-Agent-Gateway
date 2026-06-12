package planning

import (
	"context"
	"fmt"
	"testing"
)

// mockNodeExecutor implements NodeExecutor for testing.
type mockNodeExecutor struct {
	results     map[string]string // nodeID -> result
	errors      map[string]error  // nodeID -> error
	executedIDs []string          // tracks execution order
}

func (m *mockNodeExecutor) ExecuteNode(ctx context.Context, node WBSNode) (string, error) {
	m.executedIDs = append(m.executedIDs, node.ID)
	if err, ok := m.errors[node.ID]; ok {
		return "", err
	}
	if result, ok := m.results[node.ID]; ok {
		return result, nil
	}
	return "done", nil
}

// mockPersister implements StatePersister for testing.
type mockPersister struct {
	saveCount int
}

func (m *mockPersister) PersistWBS(tree *WBSTree) {
	m.saveCount++
}

func TestWBSOrchestrator_Execute_AllComplete(t *testing.T) {
	tree := &WBSTree{
		RootNodes: []WBSNode{
			{ID: "1", Name: "Step 1", Status: StatusPending},
			{ID: "2", Name: "Step 2", Status: StatusPending},
		},
	}

	executor := &mockNodeExecutor{
		results: map[string]string{"1": "step 1 done", "2": "step 2 done"},
	}
	persister := &mockPersister{}

	orch := NewWBSOrchestrator(executor, persister, nil)
	err := orch.Execute(context.Background(), tree)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !tree.IsComplete() {
		t.Error("expected all nodes to be completed")
	}
}

func TestWBSOrchestrator_Execute_NodeFailure(t *testing.T) {
	tree := &WBSTree{
		RootNodes: []WBSNode{
			{ID: "1", Name: "Step 1", Status: StatusPending},
			{ID: "2", Name: "Step 2", Status: StatusPending, Dependencies: []string{"1"}},
		},
	}

	executor := &mockNodeExecutor{
		errors: map[string]error{"1": fmt.Errorf("build failed")},
	}
	persister := &mockPersister{}

	orch := NewWBSOrchestrator(executor, persister, nil)
	err := orch.Execute(context.Background(), tree)
	if err == nil {
		t.Fatal("expected error on node failure")
	}

	// Node 1 should be failed.
	statusMap := tree.buildStatusMap()
	if statusMap["1"] != StatusFailed {
		t.Errorf("node 1 status = %q, want %q", statusMap["1"], StatusFailed)
	}
	// Node 2 should still be pending (blocked).
	if statusMap["2"] != StatusPending {
		t.Errorf("node 2 status = %q, want %q", statusMap["2"], StatusPending)
	}
}

func TestWBSOrchestrator_Execute_Deadlock(t *testing.T) {
	tree := &WBSTree{
		RootNodes: []WBSNode{
			{ID: "1", Status: StatusPending, Dependencies: []string{"2"}},
			{ID: "2", Status: StatusPending, Dependencies: []string{"1"}},
		},
	}

	executor := &mockNodeExecutor{}
	persister := &mockPersister{}

	orch := NewWBSOrchestrator(executor, persister, nil)
	err := orch.Execute(context.Background(), tree)
	if err == nil {
		t.Fatal("expected deadlock error")
	}
}

func TestWBSOrchestrator_Execute_DependencyOrder(t *testing.T) {
	tree := &WBSTree{
		RootNodes: []WBSNode{
			{ID: "1", Name: "First", Status: StatusPending},
			{ID: "2", Name: "Second", Status: StatusPending, Dependencies: []string{"1"}},
			{ID: "3", Name: "Third", Status: StatusPending, Dependencies: []string{"2"}},
		},
	}

	executor := &mockNodeExecutor{
		results: map[string]string{
			"1": "first done",
			"2": "second done",
			"3": "third done",
		},
	}
	persister := &mockPersister{}

	orch := NewWBSOrchestrator(executor, persister, nil)
	err := orch.Execute(context.Background(), tree)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Verify execution order.
	if len(executor.executedIDs) != 3 {
		t.Fatalf("expected 3 executions, got %d", len(executor.executedIDs))
	}
	expected := []string{"1", "2", "3"}
	for i, id := range expected {
		if executor.executedIDs[i] != id {
			t.Errorf("execution[%d] = %q, want %q", i, executor.executedIDs[i], id)
		}
	}
}

func TestWBSOrchestrator_Resume_SkipCompleted(t *testing.T) {
	// Simulate a resume scenario: node 1 already completed.
	tree := &WBSTree{
		RootNodes: []WBSNode{
			{ID: "1", Name: "Step 1", Status: StatusCompleted, ResultSummary: "already done"},
			{ID: "2", Name: "Step 2", Status: StatusPending, Dependencies: []string{"1"}},
		},
	}

	executor := &mockNodeExecutor{
		results: map[string]string{"2": "step 2 done"},
	}
	persister := &mockPersister{}

	orch := NewWBSOrchestrator(executor, persister, nil)
	err := orch.Execute(context.Background(), tree)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Only node 2 should have been executed.
	if len(executor.executedIDs) != 1 {
		t.Fatalf("expected 1 execution (skip completed), got %d", len(executor.executedIDs))
	}
	if executor.executedIDs[0] != "2" {
		t.Errorf("executed[0] = %q, want %q", executor.executedIDs[0], "2")
	}
}

func TestWBSOrchestrator_PersistAfterEachNode(t *testing.T) {
	tree := &WBSTree{
		RootNodes: []WBSNode{
			{ID: "1", Status: StatusPending},
			{ID: "2", Status: StatusPending},
		},
	}

	executor := &mockNodeExecutor{
		results: map[string]string{"1": "done", "2": "done"},
	}
	persister := &mockPersister{}

	orch := NewWBSOrchestrator(executor, persister, nil)
	err := orch.Execute(context.Background(), tree)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Each node gets 2 persist calls: running + completed.
	// 2 nodes * 2 = 4 persist calls.
	if persister.saveCount != 4 {
		t.Errorf("persist count = %d, want 4 (running+completed per node)", persister.saveCount)
	}
}
