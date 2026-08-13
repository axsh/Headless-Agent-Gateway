package analyzer

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/axsh/arctic-tern/shared/libs/go/artifact/store"
)

// ReconcileSource identifies where a file operation came from.
type ReconcileSource int

const (
	SourceStructuredTool ReconcileSource = 1
	SourceShellParser    ReconcileSource = 2
	SourceGitDiff        ReconcileSource = 3
	SourceSnapshot       ReconcileSource = 4
	SourceStructuredOut  ReconcileSource = 5
)

// ReconcileInput aggregates all sources for one session.
type ReconcileInput struct {
	SessionID       string
	ExistingEvents  []store.SystemArtifactEvent
	GitChanges      []GitDiffResult
	SnapshotChanges []ParsedFileOp
	StructuredPaths []ParsedFileOp
}

type reconcileCandidate struct {
	key       string
	operation string
	toolName  string
	absPath   string
	source    ReconcileSource
	at        time.Time
}

// Reconcile merges inputs, deduplicates by key, and returns supplemental events to persist.
func Reconcile(in ReconcileInput, workDirResolver WorkDirResolver, projectRoot string) []store.SystemArtifactEvent {
	normalizer := &ToolCallAnalyzer{
		projectRoot:     filepath.ToSlash(filepath.Clean(projectRoot)),
		workDirResolver: workDirResolver,
	}

	byKey := make(map[string]reconcileCandidate)

	add := func(path, operation, toolName string, source ReconcileSource, at time.Time) {
		if path == "" || operation == "" {
			return
		}
		key, abs := normalizer.ResolvePathForSession(path, in.SessionID)
		if key == "" {
			key = filepath.ToSlash(path)
		}
		existing, ok := byKey[key]
		if ok {
			if source > existing.source {
				return
			}
			if source == existing.source && !at.After(existing.at) {
				return
			}
		}
		byKey[key] = reconcileCandidate{
			key:       key,
			operation: operation,
			toolName:  toolName,
			absPath:   abs,
			source:    source,
			at:        at,
		}
	}

	realtimeKeys := make(map[string]struct{})
	for _, e := range in.ExistingEvents {
		src := sourceForToolName(e.ToolName)
		add(e.Key, e.Operation, e.ToolName, src, e.OccurredAt)
		if src <= SourceShellParser {
			realtimeKeys[e.Key] = struct{}{}
		}
	}

	for _, g := range in.GitChanges {
		add(g.Path, g.Operation, "reconcile:git", SourceGitDiff, time.Now())
	}
	for _, s := range in.SnapshotChanges {
		add(s.Path, s.Operation, "reconcile:snapshot", SourceSnapshot, time.Now())
	}
	for _, s := range in.StructuredPaths {
		add(s.Path, s.Operation, "reconcile:structured", SourceStructuredOut, time.Now())
	}

	var out []store.SystemArtifactEvent
	for key, cand := range byKey {
		if cand.source <= SourceShellParser {
			continue
		}
		if _, ok := realtimeKeys[key]; ok {
			continue
		}
		out = append(out, store.SystemArtifactEvent{
			SessionID:  in.SessionID,
			AgentID:    in.SessionID,
			Key:        cand.key,
			ActualPath: cand.absPath,
			Operation:  cand.operation,
			OccurredAt: cand.at,
			ToolName:   cand.toolName,
		})
	}
	return dedupEvents(out)
}

func sourceForToolName(toolName string) ReconcileSource {
	switch toolName {
	case "file_change", "Write", "Edit", "MultiEdit", "NotebookEdit", "StrReplace", "Delete":
		return SourceStructuredTool
	case "command_execution", "Bash", "shell", "shell_command":
		return SourceShellParser
	case "reconcile:git":
		return SourceGitDiff
	case "reconcile:snapshot":
		return SourceSnapshot
	case "reconcile:structured":
		return SourceStructuredOut
	default:
		if strings.HasPrefix(toolName, "reconcile:") {
			return SourceStructuredOut
		}
		return SourceStructuredTool
	}
}

func dedupEvents(events []store.SystemArtifactEvent) []store.SystemArtifactEvent {
	type dedupKey struct {
		sessionID string
		key       string
		operation string
	}
	latest := make(map[dedupKey]store.SystemArtifactEvent)
	for _, e := range events {
		k := dedupKey{e.SessionID, e.Key, e.Operation}
		if prev, ok := latest[k]; !ok || e.OccurredAt.After(prev.OccurredAt) {
			latest[k] = e
		}
	}
	out := make([]store.SystemArtifactEvent, 0, len(latest))
	for _, e := range latest {
		out = append(out, e)
	}
	return out
}

// RunSessionReconciliation loads existing events, applies git/snapshot supplements, and saves new events.
func RunSessionReconciliation(
	st store.ArtifactStore,
	sessionID, workDir, projectRoot string,
	workDirResolver WorkDirResolver,
	startSnapshot DirSnapshot,
	hasStartSnapshot bool,
) error {
	if st == nil || sessionID == "" || workDir == "" {
		return nil
	}

	existing, err := st.ListAllSystemArtifacts(context.Background(), store.SystemArtifactFilter{
		SessionIDs:     []string{sessionID},
		IncludeDeleted: true,
	})
	if err != nil {
		return err
	}

	input := ReconcileInput{
		SessionID:      sessionID,
		ExistingEvents: existing,
	}

	if IsGitRepo(workDir) {
		changes, err := DetectGitChanges(workDir)
		if err != nil {
			return err
		}
		input.GitChanges = changes
	} else if hasStartSnapshot {
		endSnap, err := TakeSnapshot(workDir)
		if err != nil {
			return err
		}
		input.SnapshotChanges = DiffSnapshots(startSnapshot, endSnap)
	}

	supplements := Reconcile(input, workDirResolver, projectRoot)
	for _, e := range supplements {
		if err := st.SaveSystemArtifactEvent(context.Background(), e); err != nil {
			return err
		}
	}
	return nil
}
