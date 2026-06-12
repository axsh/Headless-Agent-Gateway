package wayfinder

import (
	"sync"
	"time"

	"github.com/axsh/arctic-tern/wayfinder/session"
)

// TrackedFile records metadata about a file created by the agent.
type TrackedFile struct {
	Path      string `json:"path"`
	CreatedBy string `json:"created_by,omitempty"` // session ID that created the file
}

// TrackedProcess records metadata about a process started by the agent.
type TrackedProcess struct {
	PID         int    `json:"pid"`
	CommandLine string `json:"command_line"`
	SessionID   string `json:"session_id,omitempty"`
}

// FileTracker tracks files and processes created by the agent.
// It is safe for concurrent use.
type FileTracker struct {
	mu        sync.RWMutex
	files     map[string]*TrackedFile
	processes map[int]*TrackedProcess
}

// NewFileTracker creates a new empty FileTracker.
func NewFileTracker() *FileTracker {
	return &FileTracker{
		files:     make(map[string]*TrackedFile),
		processes: make(map[int]*TrackedProcess),
	}
}

// TrackFile records a file path as agent-managed.
func (ft *FileTracker) TrackFile(path string) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.files[path] = &TrackedFile{Path: path}
}

// TrackFileWithSession records a file with session context.
func (ft *FileTracker) TrackFileWithSession(path, sessionID string) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.files[path] = &TrackedFile{Path: path, CreatedBy: sessionID}
}

// IsTracked returns true if the file path is tracked by the agent.
func (ft *FileTracker) IsTracked(path string) bool {
	ft.mu.RLock()
	defer ft.mu.RUnlock()
	_, ok := ft.files[path]
	return ok
}

// UntrackFile removes a file from tracking.
func (ft *FileTracker) UntrackFile(path string) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	delete(ft.files, path)
}

// TrackProcess records a process PID as agent-managed.
func (ft *FileTracker) TrackProcess(pid int, commandLine string) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.processes[pid] = &TrackedProcess{PID: pid, CommandLine: commandLine}
}

// UntrackProcess removes a process from tracking.
func (ft *FileTracker) UntrackProcess(pid int) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	delete(ft.processes, pid)
}

// IsProcessTracked returns true if the PID is tracked.
func (ft *FileTracker) IsProcessTracked(pid int) bool {
	ft.mu.RLock()
	defer ft.mu.RUnlock()
	_, ok := ft.processes[pid]
	return ok
}

// TrackedFiles returns a copy of all tracked files.
func (ft *FileTracker) TrackedFiles() []*TrackedFile {
	ft.mu.RLock()
	defer ft.mu.RUnlock()
	result := make([]*TrackedFile, 0, len(ft.files))
	for _, f := range ft.files {
		result = append(result, f)
	}
	return result
}

// TrackedProcesses returns a copy of all tracked processes.
func (ft *FileTracker) TrackedProcesses() []*TrackedProcess {
	ft.mu.RLock()
	defer ft.mu.RUnlock()
	result := make([]*TrackedProcess, 0, len(ft.processes))
	for _, p := range ft.processes {
		result = append(result, p)
	}
	return result
}

// TrackedFilesSnapshot returns tracked files as session.TrackedFile slice for serialization.
func (ft *FileTracker) TrackedFilesSnapshot() []session.TrackedFile {
	ft.mu.RLock()
	defer ft.mu.RUnlock()
	result := make([]session.TrackedFile, 0, len(ft.files))
	for _, f := range ft.files {
		result = append(result, session.TrackedFile{
			Path:      f.Path,
			CreatedAt: time.Now(),
		})
	}
	return result
}

// TrackedProcessesSnapshot returns tracked processes as session.TrackedProcess slice for serialization.
func (ft *FileTracker) TrackedProcessesSnapshot() []session.TrackedProcess {
	ft.mu.RLock()
	defer ft.mu.RUnlock()
	result := make([]session.TrackedProcess, 0, len(ft.processes))
	for _, p := range ft.processes {
		result = append(result, session.TrackedProcess{
			PID:       p.PID,
			Command:   p.CommandLine,
			StartedAt: time.Now(),
		})
	}
	return result
}

