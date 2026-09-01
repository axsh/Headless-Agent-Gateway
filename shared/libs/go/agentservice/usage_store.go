package agentservice

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

const usageFileName = "usage.json"

func usageFilePath(sessionDir string) string {
	return filepath.Join(sessionDir, usageFileName)
}

func loadUsageReport(sessionDir, sessionID string) (*codingagent.SessionUsageReport, error) {
	path := usageFilePath(sessionDir)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &codingagent.SessionUsageReport{
				SessionID: sessionID,
				Usage: codingagent.TokenUsage{
					Source:     codingagent.UsageSourceDerivedSessionSum,
					Confidence: codingagent.UsageConfidenceHigh,
				},
				Turns: []codingagent.TurnUsageRecord{},
			}, nil
		}
		return nil, err
	}
	var rep codingagent.SessionUsageReport
	if err := json.Unmarshal(data, &rep); err != nil {
		return nil, err
	}
	if rep.SessionID == "" {
		rep.SessionID = sessionID
	}
	if rep.Turns == nil {
		rep.Turns = []codingagent.TurnUsageRecord{}
	}
	return &rep, nil
}

func saveUsageReport(sessionDir string, rep *codingagent.SessionUsageReport) error {
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	tmp := usageFilePath(sessionDir) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, usageFilePath(sessionDir))
}

// appendTurnUsage appends a turn record, recomputes session sum, and persists usage.json.
// Returns the new session aggregate.
func appendTurnUsage(sessionDir, sessionID string, turn codingagent.TurnUsageRecord) (*codingagent.TokenUsage, error) {
	if sessionDir == "" {
		return nil, fmt.Errorf("empty session dir")
	}
	rep, err := loadUsageReport(sessionDir, sessionID)
	if err != nil {
		return nil, err
	}
	// Replace existing turn with same id if present (idempotent finalize).
	replaced := false
	for i := range rep.Turns {
		if rep.Turns[i].TurnID == turn.TurnID {
			rep.Turns[i] = turn
			replaced = true
			break
		}
	}
	if !replaced {
		rep.Turns = append(rep.Turns, turn)
	}
	sum := codingagent.TokenUsage{
		Source:     codingagent.UsageSourceDerivedSessionSum,
		Confidence: codingagent.UsageConfidenceHigh,
	}
	for _, tr := range rep.Turns {
		codingagent.AddUsage(&sum, tr.Usage)
	}
	rep.Usage = sum
	rep.SessionID = sessionID
	if err := saveUsageReport(sessionDir, rep); err != nil {
		return nil, err
	}
	return &sum, nil
}
