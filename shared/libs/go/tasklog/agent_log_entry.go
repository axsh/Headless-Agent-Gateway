package tasklog

import (
	"time"

	"github.com/google/uuid"
)

const AgentLogEntryType = "AGENT_LOG"

type AgentLogEntry struct {
	BaseEntry

	Body        string `json:"body"`
	Kind        string `json:"kind"`
	Location    string `json:"location,omitempty"`
	ParentLogID string `json:"parentLogId,omitempty"`
	TaskNodeID  string `json:"taskNodeId,omitempty"`
	AgentID     string `json:"agentId"`
	RunID       string `json:"runId,omitempty"`
	IsComplete  bool   `json:"isComplete"`
	Phase       string `json:"phase"`
}

type AgentLogOption func(*AgentLogEntry)

func WithKind(kind string) AgentLogOption {
	return func(e *AgentLogEntry) {
		e.Kind = kind
	}
}

func WithLocation(location string) AgentLogOption {
	return func(e *AgentLogEntry) {
		e.Location = location
	}
}

func WithParentLogID(parentLogID string) AgentLogOption {
	return func(e *AgentLogEntry) {
		e.ParentLogID = parentLogID
	}
}

func WithTaskNodeID(taskNodeID string) AgentLogOption {
	return func(e *AgentLogEntry) {
		e.TaskNodeID = taskNodeID
	}
}

func NewAgentLogEntry(agentID string, opts ...AgentLogOption) *AgentLogEntry {
	e := &AgentLogEntry{
		BaseEntry: BaseEntry{
			ID:        uuid.New().String(),
			Time:      time.Now(),
			EntryType: AgentLogEntryType,
		},
		AgentID: agentID,
		Kind:    "text",
		Phase:   "begin",
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

func NewAgentLogSendEntry(logID, agentID, body string) *AgentLogEntry {
	return &AgentLogEntry{
		BaseEntry: BaseEntry{
			ID:        logID,
			Time:      time.Now(),
			EntryType: AgentLogEntryType,
		},
		AgentID: agentID,
		Body:    body,
		Phase:   "send",
	}
}

func NewAgentLogEndEntry(logID, agentID string) *AgentLogEntry {
	return &AgentLogEntry{
		BaseEntry: BaseEntry{
			ID:        logID,
			Time:      time.Now(),
			EntryType: AgentLogEntryType,
		},
		AgentID:    agentID,
		IsComplete: true,
		Phase:      "end",
	}
}
