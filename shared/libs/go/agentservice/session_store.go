package agentservice

import (
	"errors"
	"sync"
	"time"

	"github.com/axsh/hag/codingagent"
)

// ErrNotFound is returned when a session is not found.
var ErrNotFound = errors.New("session not found")

// ErrInvalidTransition is returned when an invalid status transition is attempted.
var ErrInvalidTransition = errors.New("invalid status transition")

// isTerminalStatus returns true if the status is a terminal state.
func isTerminalStatus(status string) bool {
	return status == codingagent.StatusCompleted ||
		status == codingagent.StatusError ||
		status == codingagent.StatusClosed
}

// MemorySessionStore is an in-memory SessionStore implementation.
type MemorySessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*codingagent.SessionRecord
}

// NewMemorySessionStore creates a new MemorySessionStore.
func NewMemorySessionStore() *MemorySessionStore {
	return &MemorySessionStore{
		sessions: make(map[string]*codingagent.SessionRecord),
	}
}

// compile-time interface compliance check
var _ codingagent.SessionStore = (*MemorySessionStore)(nil)

// Create stores a new session record (stores a copy).
func (m *MemorySessionStore) Create(s *codingagent.SessionRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	copy := *s
	copy.CreatedAt = now
	copy.UpdatedAt = now
	s.CreatedAt = now
	s.UpdatedAt = now
	m.sessions[s.ID] = &copy
	return nil
}

// Get retrieves a session record by ID.
func (m *MemorySessionStore) Get(id string) (*codingagent.SessionRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[id]
	if !ok {
		return nil, ErrNotFound
	}
	return s, nil
}

// Update updates an existing session record (stores a copy).
// Returns ErrInvalidTransition if transitioning from a terminal status to active.
func (m *MemorySessionStore) Update(s *codingagent.SessionRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	existing, ok := m.sessions[s.ID]
	if !ok {
		return ErrNotFound
	}
	// Validate status transition: terminal states cannot go back to active
	if isTerminalStatus(existing.Status) && s.Status == codingagent.StatusActive {
		return ErrInvalidTransition
	}
	copy := *s
	copy.UpdatedAt = time.Now()
	s.UpdatedAt = copy.UpdatedAt
	m.sessions[s.ID] = &copy
	return nil
}

// List returns all session records.
func (m *MemorySessionStore) List() ([]*codingagent.SessionRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*codingagent.SessionRecord, 0, len(m.sessions))
	for _, s := range m.sessions {
		result = append(result, s)
	}
	return result, nil
}

// Delete removes a session record by ID.
func (m *MemorySessionStore) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[id]; !ok {
		return ErrNotFound
	}
	delete(m.sessions, id)
	return nil
}
