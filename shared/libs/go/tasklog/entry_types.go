package tasklog

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

const (
	MovementEntryType   = "MOVEMENT"
	TerminatedEntryType = "TERMINATED"
	ErrorEntryType      = "ERROR"
)

type MovementEntry struct {
	BaseEntry
	AgentID    string `json:"agentId"`
	NodeID     string `json:"nodeId"`
	FromNodeID string `json:"fromNodeId,omitempty"`
	Body       string `json:"body"`
}

func FormatMovementBody(nodeID string) string {
	return fmt.Sprintf("Agent moved to node '%s'", nodeID)
}

func NewMovementEntry(agentID, nodeID string) *MovementEntry {
	return &MovementEntry{
		BaseEntry: BaseEntry{
			ID:        uuid.New().String(),
			Time:      time.Now(),
			EntryType: MovementEntryType,
		},
		AgentID: agentID,
		NodeID:  nodeID,
		Body:    FormatMovementBody(nodeID),
	}
}

type TerminatedEntry struct {
	BaseEntry
	AgentID string `json:"agentId"`
	Reason  string `json:"reason"`
	Body    string `json:"body"`
}

func FormatTerminatedBody(reason string) string {
	if reason == "" {
		return "Agent terminated"
	}
	return fmt.Sprintf("Agent terminated: %s", reason)
}

func NewTerminatedEntry(agentID, reason string) *TerminatedEntry {
	return &TerminatedEntry{
		BaseEntry: BaseEntry{
			ID:        uuid.New().String(),
			Time:      time.Now(),
			EntryType: TerminatedEntryType,
		},
		AgentID: agentID,
		Reason:  reason,
		Body:    FormatTerminatedBody(reason),
	}
}

type ErrorEntry struct {
	BaseEntry
	AgentID string `json:"agentId"`
	Message string `json:"message"`
	Body    string `json:"body"`
}

func NewErrorEntry(agentID, message string) *ErrorEntry {
	return &ErrorEntry{
		BaseEntry: BaseEntry{
			ID:        uuid.New().String(),
			Time:      time.Now(),
			EntryType: ErrorEntryType,
		},
		AgentID: agentID,
		Message: message,
		Body:    message,
	}
}
