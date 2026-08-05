package agentservice

import (
	"context"

	"github.com/axsh/arctic-tern/shared/libs/go/artifact/analyzer"
)

func (s *Server) captureSessionSnapshot(sessionID, workDir string) {
	if s.artifactStore == nil || workDir == "" {
		return
	}
	if analyzer.IsGitRepo(workDir) {
		return
	}
	snap, err := analyzer.TakeSnapshot(workDir)
	if err != nil {
		if s.logger != nil {
			s.logger.Debug("session snapshot capture failed", "session_id", sessionID, "error", err.Error())
		}
		return
	}
	s.sessionSnapshotsMu.Lock()
	if s.sessionSnapshots == nil {
		s.sessionSnapshots = make(map[string]analyzer.DirSnapshot)
	}
	s.sessionSnapshots[sessionID] = snap
	s.sessionSnapshotsMu.Unlock()
	if s.logger != nil {
		s.logger.Debug("session snapshot captured", "session_id", sessionID, "file_count", len(snap.Files))
	}
}

func (s *Server) reconcileSessionArtifacts(ctx context.Context, sessionID string) {
	if s.artifactStore == nil {
		return
	}
	record, err := s.sessions.Get(sessionID)
	if err != nil {
		return
	}

	var startSnap analyzer.DirSnapshot
	hasSnap := false
	s.sessionSnapshotsMu.Lock()
	if snap, ok := s.sessionSnapshots[sessionID]; ok {
		startSnap = snap
		hasSnap = analyzer.HasSnapshotData(snap)
		delete(s.sessionSnapshots, sessionID)
	}
	s.sessionSnapshotsMu.Unlock()

	resolver := func(sid string) string {
		if rec, err := s.sessions.Get(sid); err == nil {
			return rec.WorkDir
		}
		return ""
	}

	if err := analyzer.RunSessionReconciliation(
		s.artifactStore,
		sessionID,
		record.WorkDir,
		s.artifactWorkDir,
		resolver,
		startSnap,
		hasSnap,
	); err != nil && s.logger != nil {
		s.logger.Debug("session artifact reconciliation failed", "session_id", sessionID, "error", err.Error())
	}
}