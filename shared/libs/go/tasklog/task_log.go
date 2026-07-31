package tasklog

import "sync"

type TaskLog struct {
	mu      sync.RWMutex
	History []Entry
	onEntry func(Entry)
}

func New() *TaskLog {
	return &TaskLog{
		History: make([]Entry, 0),
	}
}

func (l *TaskLog) SetOnEntry(f func(Entry)) {
	if f == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	prev := l.onEntry
	if prev == nil {
		l.onEntry = f
		return
	}
	l.onEntry = func(e Entry) {
		prev(e)
		f(e)
	}
}

func (l *TaskLog) Add(e Entry) {
	if e == nil {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// If e is a TerminatedEntry, auto-close all incomplete AgentLogEntries in History.
	if e.Type() == TerminatedEntryType {
		for _, prev := range l.History {
			if prev.Type() == AgentLogEntryType {
				if agentLog, ok := prev.(*AgentLogEntry); ok {
					if !agentLog.IsComplete {
						agentLog.IsComplete = true
						if agentLog.Body != "" {
							agentLog.Body += "\n[auto-closed: abnormal termination]"
						} else {
							agentLog.Body = "[auto-closed: abnormal termination]"
						}
					}
				}
			}
		}
	}

	l.History = append(l.History, e)

	if l.onEntry != nil {
		l.onEntry(e)
	}
}

func (l *TaskLog) Entries() []Entry {
	l.mu.RLock()
	defer l.mu.RUnlock()

	duplicate := make([]Entry, len(l.History))
	copy(duplicate, l.History)
	return duplicate
}

func (l *TaskLog) Clone() *TaskLog {
	l.mu.RLock()
	defer l.mu.RUnlock()

	newLog := New()
	newLog.History = make([]Entry, len(l.History))
	copy(newLog.History, l.History)
	return newLog
}
