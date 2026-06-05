package agentservice

import (
	"errors"
	"sync"
	"time"

	"github.com/axsh/hag/codingagent"
)

// ErrNotFound is returned when a session is not found.
var ErrNotFound = errors.New("session not found")

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

// Create stores a new session record.
func (m *MemorySessionStore) Create(s *codingagent.SessionRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	s.CreatedAt = now
	s.UpdatedAt = now
	m.sessions[s.ID] = s
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

// Update updates an existing session record.
func (m *MemorySessionStore) Update(s *codingagent.SessionRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[s.ID]; !ok {
		return ErrNotFound
	}
	s.UpdatedAt = time.Now()
	m.sessions[s.ID] = s
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
