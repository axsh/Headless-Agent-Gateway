package planning

// Node status constants for WBSNode.
const (
	StatusPending   = "pending"
	StatusRunning   = "running"
	StatusCompleted = "completed"
	StatusFailed    = "failed"
)

// WBSNode represents a single step in the Work Breakdown Structure.
type WBSNode struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Description   string    `json:"description"`
	Status        string    `json:"status"`
	Dependencies  []string  `json:"dependencies"`
	SubSteps      []WBSNode `json:"sub_steps,omitempty"`
	ResultSummary string    `json:"result_summary,omitempty"`
}

// WBSTree manages the entire WBS structure.
type WBSTree struct {
	RootNodes []WBSNode `json:"root_nodes"`
}

// NextExecutableNodes returns nodes that are "pending" and have all
// dependencies in "completed" status.
func (t *WBSTree) NextExecutableNodes() []WBSNode {
	statusMap := t.buildStatusMap()
	var result []WBSNode
	t.walkNodes(func(node *WBSNode) {
		if node.Status != StatusPending {
			return
		}
		allDepsCompleted := true
		for _, depID := range node.Dependencies {
			if statusMap[depID] != StatusCompleted {
				allDepsCompleted = false
				break
			}
		}
		if allDepsCompleted {
			result = append(result, *node)
		}
	})
	return result
}

// IsComplete returns true if all nodes are "completed".
func (t *WBSTree) IsComplete() bool {
	complete := true
	t.walkNodes(func(node *WBSNode) {
		if node.Status != StatusCompleted {
			complete = false
		}
	})
	return complete
}

// HasFailed returns true if any node has "failed" status.
func (t *WBSTree) HasFailed() bool {
	failed := false
	t.walkNodes(func(node *WBSNode) {
		if node.Status == StatusFailed {
			failed = true
		}
	})
	return failed
}

// IsDeadlocked returns true if there are pending nodes but none are executable.
func (t *WBSTree) IsDeadlocked() bool {
	hasPending := false
	t.walkNodes(func(node *WBSNode) {
		if node.Status == StatusPending {
			hasPending = true
		}
	})
	return hasPending && len(t.NextExecutableNodes()) == 0
}

// UpdateNodeStatus updates the status and result of a specific node by ID.
func (t *WBSTree) UpdateNodeStatus(nodeID, status, resultSummary string) bool {
	found := false
	t.walkNodesMut(func(node *WBSNode) {
		if node.ID == nodeID {
			node.Status = status
			node.ResultSummary = resultSummary
			found = true
		}
	})
	return found
}

// buildStatusMap creates a flat map of nodeID -> status for dependency checking.
func (t *WBSTree) buildStatusMap() map[string]string {
	m := make(map[string]string)
	t.walkNodes(func(node *WBSNode) {
		m[node.ID] = node.Status
	})
	return m
}

// walkNodes traverses all nodes (including nested sub_steps) in DFS order.
func (t *WBSTree) walkNodes(fn func(*WBSNode)) {
	for i := range t.RootNodes {
		walkNodeRecursive(&t.RootNodes[i], fn)
	}
}

// walkNodesMut traverses with mutable access.
func (t *WBSTree) walkNodesMut(fn func(*WBSNode)) {
	for i := range t.RootNodes {
		walkNodeRecursive(&t.RootNodes[i], fn)
	}
}

func walkNodeRecursive(node *WBSNode, fn func(*WBSNode)) {
	fn(node)
	for i := range node.SubSteps {
		walkNodeRecursive(&node.SubSteps[i], fn)
	}
}
