package agentservice

import (
	"context"

	"github.com/axsh/arctic-tern/shared/libs/go/artifact/analyzer"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

func snapshotKey(sessionID, turnID string) string {
	return sessionID + ":" + turnID
}

func (s *Server) captureTurnSnapshot(sessionID, turnID, workDir string) {
	if s.artifactStore == nil || workDir == "" {
		return
	}
	if turnID == "" {
		return
	}
	if rec, err := s.sessions.Get(sessionID); err == nil {
		if !codingagent.EffectiveFileChangeCollectors(rec.FileChangeCollectors).WorkdirReconcile {
			return
		}
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
	s.sessionSnapshots[snapshotKey(sessionID, turnID)] = snap
	s.sessionSnapshotsMu.Unlock()
	if s.logger != nil {
		s.logger.Debug("turn snapshot captured", "session_id", sessionID, "turn_id", turnID, "file_count", len(snap.Files))
	}
}

func (s *Server) reconcileSessionArtifacts(ctx context.Context, sessionID, turnID, correlationID string) {
	if s.artifactStore == nil {
		return
	}
	record, err := s.sessions.Get(sessionID)
	if err != nil {
		return
	}
	cfg := codingagent.EffectiveFileChangeCollectors(record.FileChangeCollectors)

	// Flush Claude Tier1 aggregate (turn_files) before the Tier3 workdir gate.
	// Codex file_change/turn_diff are not collected here (Write-family tools only).
	if s.toolAnalyzer != nil && cfg.StructuredTool {
		var events []codingagent.StreamEvent
		if exec, ok := s.execRegistry.Get(sessionID); ok && exec != nil && exec.relay != nil {
			events = exec.relay.EventsSnapshot()
		}
		ops := analyzer.CollectClaudeTier1OpsFromStream(events)
		if len(ops) > 0 {
			if err := s.toolAnalyzer.FlushTurnFiles(ctx, sessionID, turnID, correlationID, ops); err != nil {
				if s.logger != nil {
					s.logger.Warn("turn_files flush failed", "session_id", sessionID, "turn_id", turnID, "error", err.Error())
				}
			} else if s.logger != nil {
				s.logger.Debug("turn_files flushed", "session_id", sessionID, "turn_id", turnID, "op_count", len(ops))
			}
		}
	}

	if !cfg.WorkdirReconcile {
		return
	}

	var startSnap analyzer.DirSnapshot
	hasSnap := false
	key := snapshotKey(sessionID, turnID)
	s.sessionSnapshotsMu.Lock()
	if snap, ok := s.sessionSnapshots[key]; ok {
		startSnap = snap
		hasSnap = analyzer.HasSnapshotData(snap)
		delete(s.sessionSnapshots, key)
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
		turnID,
		correlationID,
		resolver,
		startSnap,
		hasSnap,
	); err != nil && s.logger != nil {
		s.logger.Debug("session artifact reconciliation failed", "session_id", sessionID, "turn_id", turnID, "error", err.Error())
	}
}
