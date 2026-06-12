package planning

import (
	"context"
	"fmt"
	"strings"

	"github.com/axsh/arctic-tern/logger"
)

// NodeExecutor executes a single WBS node.
// This interface decouples the orchestrator from the subagent implementation,
// avoiding cyclic imports.
type NodeExecutor interface {
	ExecuteNode(ctx context.Context, node WBSNode) (string, error)
}

// StatePersister saves WBS tree state for resume support.
type StatePersister interface {
	PersistWBS(tree *WBSTree)
}

// WBSOrchestrator drives WBS execution with node-level delegation.
type WBSOrchestrator struct {
	executor  NodeExecutor
	persister StatePersister
	logger    logger.Logger
}

// NewWBSOrchestrator creates a new WBSOrchestrator.
func NewWBSOrchestrator(
	executor NodeExecutor,
	persister StatePersister,
	log logger.Logger,
) *WBSOrchestrator {
	if log == nil {
		log = &noopLogger{}
	}
	return &WBSOrchestrator{
		executor:  executor,
		persister: persister,
		logger:    log,
	}
}

// Execute drives the WBS execution loop.
// Returns nil when all nodes complete, or error on failure/deadlock.
func (o *WBSOrchestrator) Execute(ctx context.Context, tree *WBSTree) error {
	for {
		// 1. Check termination conditions.
		if tree.IsComplete() {
			o.logger.Info("WBS execution completed successfully")
			return nil
		}
		if tree.HasFailed() {
			return o.handleFailure(tree)
		}
		if tree.IsDeadlocked() {
			return fmt.Errorf("WBS execution deadlocked: pending nodes exist but none are executable")
		}

		// 2. Get next executable nodes.
		executableNodes := tree.NextExecutableNodes()
		if len(executableNodes) == 0 {
			return fmt.Errorf("no executable nodes found")
		}

		// 3. Execute each node sequentially.
		for _, node := range executableNodes {
			// Check context cancellation.
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("WBS execution cancelled: %w", err)
			}

			// Mark as running.
			tree.UpdateNodeStatus(node.ID, StatusRunning, "")
			o.persist(tree)
			o.logger.Debug("executing WBS node", "node_id", node.ID, "node_name", node.Name)

			// Execute via injected executor.
			result, err := o.executor.ExecuteNode(ctx, node)
			if err != nil {
				// Mark as failed.
				tree.UpdateNodeStatus(node.ID, StatusFailed, fmt.Sprintf("Error: %v", err))
				o.persist(tree)
				o.logger.Error("WBS node failed", "node_id", node.ID, "error", err.Error())
				return o.handleFailure(tree)
			}

			// Mark as completed.
			tree.UpdateNodeStatus(node.ID, StatusCompleted, result)
			o.persist(tree)
			o.logger.Debug("WBS node completed", "node_id", node.ID, "result_len", len(result))
		}
	}
}

// CollectResults gathers result summaries from all completed nodes.
func CollectResults(tree *WBSTree) string {
	var parts []string
	tree.walkNodes(func(node *WBSNode) {
		if node.Status == StatusCompleted && node.ResultSummary != "" {
			parts = append(parts, fmt.Sprintf("[%s] %s: %s", node.ID, node.Name, node.ResultSummary))
		}
	})
	return strings.Join(parts, "\n")
}

// handleFailure blocks dependent nodes and returns error with status report.
func (o *WBSOrchestrator) handleFailure(tree *WBSTree) error {
	var failedNodes []string
	tree.walkNodes(func(node *WBSNode) {
		if node.Status == StatusFailed {
			failedNodes = append(failedNodes, fmt.Sprintf("%s (%s): %s", node.ID, node.Name, node.ResultSummary))
		}
	})
	return fmt.Errorf("WBS execution paused due to failed nodes:\n%s\nDependent nodes are blocked.", strings.Join(failedNodes, "\n"))
}

// persist saves WBS state if persister is configured.
func (o *WBSOrchestrator) persist(tree *WBSTree) {
	if o.persister != nil {
		o.persister.PersistWBS(tree)
	}
}

// noopLogger is a no-op logger for when none is provided.
type noopLogger struct{}

func (n *noopLogger) Trace(msg string, fields ...any) {}
func (n *noopLogger) Debug(msg string, fields ...any) {}
func (n *noopLogger) Info(msg string, fields ...any)  {}
func (n *noopLogger) Warn(msg string, fields ...any)  {}
func (n *noopLogger) Error(msg string, fields ...any) {}
func (n *noopLogger) WithFields(fields map[string]any) logger.Logger {
	return n
}
func (n *noopLogger) WithComponent(name string) logger.Logger { return n }
