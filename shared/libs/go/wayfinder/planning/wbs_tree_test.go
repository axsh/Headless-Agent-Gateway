package planning

import (
	"encoding/json"
	"testing"
)

func TestWBSTree_NextExecutableNodes_NoDeps(t *testing.T) {
	tree := &WBSTree{
		RootNodes: []WBSNode{
			{ID: "1", Name: "Step 1", Status: StatusPending},
			{ID: "2", Name: "Step 2", Status: StatusPending},
		},
	}
	nodes := tree.NextExecutableNodes()
	if len(nodes) != 2 {
		t.Errorf("expected 2 executable nodes, got %d", len(nodes))
	}
}

func TestWBSTree_NextExecutableNodes_WithDeps(t *testing.T) {
	tree := &WBSTree{
		RootNodes: []WBSNode{
			{ID: "1", Name: "Step 1", Status: StatusCompleted},
			{ID: "2", Name: "Step 2", Status: StatusPending, Dependencies: []string{"1"}},
			{ID: "3", Name: "Step 3", Status: StatusPending, Dependencies: []string{"2"}},
		},
	}
	nodes := tree.NextExecutableNodes()
	if len(nodes) != 1 {
		t.Fatalf("expected 1 executable node, got %d", len(nodes))
	}
	if nodes[0].ID != "2" {
		t.Errorf("expected node ID %q, got %q", "2", nodes[0].ID)
	}
}

func TestWBSTree_NextExecutableNodes_BlockedByFailed(t *testing.T) {
	tree := &WBSTree{
		RootNodes: []WBSNode{
			{ID: "1", Name: "Step 1", Status: StatusFailed},
			{ID: "2", Name: "Step 2", Status: StatusPending, Dependencies: []string{"1"}},
		},
	}
	nodes := tree.NextExecutableNodes()
	if len(nodes) != 0 {
		t.Errorf("expected 0 executable nodes (blocked by failed dep), got %d", len(nodes))
	}
}

func TestWBSTree_IsComplete(t *testing.T) {
	tree := &WBSTree{
		RootNodes: []WBSNode{
			{ID: "1", Status: StatusCompleted},
			{ID: "2", Status: StatusCompleted},
		},
	}
	if !tree.IsComplete() {
		t.Error("expected IsComplete=true when all nodes completed")
	}
}

func TestWBSTree_IsComplete_Mixed(t *testing.T) {
	tree := &WBSTree{
		RootNodes: []WBSNode{
			{ID: "1", Status: StatusCompleted},
			{ID: "2", Status: StatusPending},
		},
	}
	if tree.IsComplete() {
		t.Error("expected IsComplete=false when pending nodes remain")
	}
}

func TestWBSTree_HasFailed(t *testing.T) {
	tree := &WBSTree{
		RootNodes: []WBSNode{
			{ID: "1", Status: StatusCompleted},
			{ID: "2", Status: StatusFailed, ResultSummary: "build error"},
		},
	}
	if !tree.HasFailed() {
		t.Error("expected HasFailed=true")
	}
}

func TestWBSTree_IsDeadlocked(t *testing.T) {
	// Circular dependency: 1 depends on 2, 2 depends on 1.
	tree := &WBSTree{
		RootNodes: []WBSNode{
			{ID: "1", Status: StatusPending, Dependencies: []string{"2"}},
			{ID: "2", Status: StatusPending, Dependencies: []string{"1"}},
		},
	}
	if !tree.IsDeadlocked() {
		t.Error("expected IsDeadlocked=true for circular dependencies")
	}
}

func TestWBSTree_UpdateNodeStatus(t *testing.T) {
	tree := &WBSTree{
		RootNodes: []WBSNode{
			{ID: "1", Status: StatusPending},
			{ID: "2", Status: StatusPending},
		},
	}
	found := tree.UpdateNodeStatus("1", StatusRunning, "")
	if !found {
		t.Error("expected UpdateNodeStatus to find node 1")
	}

	found = tree.UpdateNodeStatus("1", StatusCompleted, "done successfully")
	if !found {
		t.Error("expected UpdateNodeStatus to find node 1")
	}

	// Verify the update took effect.
	statusMap := tree.buildStatusMap()
	if statusMap["1"] != StatusCompleted {
		t.Errorf("node 1 status = %q, want %q", statusMap["1"], StatusCompleted)
	}

	// Non-existent node.
	found = tree.UpdateNodeStatus("99", StatusCompleted, "")
	if found {
		t.Error("expected UpdateNodeStatus to not find node 99")
	}
}

func TestWBSTree_Serialization(t *testing.T) {
	tree := &WBSTree{
		RootNodes: []WBSNode{
			{
				ID:           "1",
				Name:         "Setup",
				Description:  "Initialize project",
				Status:       StatusCompleted,
				Dependencies: nil,
				SubSteps: []WBSNode{
					{ID: "1.1", Name: "Create dirs", Status: StatusCompleted},
				},
				ResultSummary: "all dirs created",
			},
			{
				ID:           "2",
				Name:         "Build",
				Description:  "Build the project",
				Status:       StatusPending,
				Dependencies: []string{"1"},
			},
		},
	}

	data, err := json.Marshal(tree)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var restored WBSTree
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if len(restored.RootNodes) != 2 {
		t.Fatalf("expected 2 root nodes, got %d", len(restored.RootNodes))
	}
	if restored.RootNodes[0].ID != "1" {
		t.Errorf("root[0].ID = %q, want %q", restored.RootNodes[0].ID, "1")
	}
	if len(restored.RootNodes[0].SubSteps) != 1 {
		t.Errorf("root[0].SubSteps len = %d, want 1", len(restored.RootNodes[0].SubSteps))
	}
	if restored.RootNodes[1].Dependencies[0] != "1" {
		t.Errorf("root[1].Dependencies[0] = %q, want %q", restored.RootNodes[1].Dependencies[0], "1")
	}
}

func TestWBSTree_NestedSubSteps(t *testing.T) {
	tree := &WBSTree{
		RootNodes: []WBSNode{
			{
				ID:     "1",
				Status: StatusCompleted,
				SubSteps: []WBSNode{
					{
						ID:     "1.1",
						Status: StatusCompleted,
						SubSteps: []WBSNode{
							{ID: "1.1.1", Status: StatusPending},
						},
					},
				},
			},
		},
	}

	// Walk should find all 3 nodes.
	count := 0
	tree.walkNodes(func(node *WBSNode) {
		count++
	})
	if count != 3 {
		t.Errorf("expected 3 nodes in traversal, got %d", count)
	}

	// IsComplete should be false (1.1.1 is pending).
	if tree.IsComplete() {
		t.Error("expected IsComplete=false when nested substep is pending")
	}

	// NextExecutableNodes should return 1.1.1 (no dependencies).
	nodes := tree.NextExecutableNodes()
	if len(nodes) != 1 || nodes[0].ID != "1.1.1" {
		t.Errorf("expected next executable = [1.1.1], got %v", nodes)
	}
}
