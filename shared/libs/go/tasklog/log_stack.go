package tasklog

import "sync"

type LogStack struct {
	mu    sync.Mutex
	stack []string
}

func (s *LogStack) CurrentParentID() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.stack) == 0 {
		return ""
	}
	return s.stack[len(s.stack)-1]
}

func (s *LogStack) Push(logID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.stack = append(s.stack, logID)
}

func (s *LogStack) Pop() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.stack) == 0 {
		return ""
	}
	idx := len(s.stack) - 1
	popped := s.stack[idx]
	s.stack = s.stack[:idx]
	return popped
}
