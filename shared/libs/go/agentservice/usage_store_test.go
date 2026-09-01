package agentservice

import (
	"path/filepath"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

func TestAppendTurnUsage_SumsSession(t *testing.T) {
	dir := t.TempDir()
	sessionDir := filepath.Join(dir, "sess")
	sum1, err := appendTurnUsage(sessionDir, "s1", codingagent.TurnUsageRecord{
		TurnID: "t1",
		Usage: codingagent.TokenUsage{
			InputTokens: 10, OutputTokens: 2,
			Source: codingagent.UsageSourceClaudeResult, Confidence: codingagent.UsageConfidenceHigh,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if sum1.InputTokens != 10 || sum1.OutputTokens != 2 {
		t.Fatalf("sum1 = %+v", sum1)
	}
	sum2, err := appendTurnUsage(sessionDir, "s1", codingagent.TurnUsageRecord{
		TurnID: "t2",
		Usage: codingagent.TokenUsage{
			InputTokens: 5, OutputTokens: 3,
			Source: codingagent.UsageSourceClaudeResult, Confidence: codingagent.UsageConfidenceHigh,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if sum2.InputTokens != 15 || sum2.OutputTokens != 5 {
		t.Fatalf("sum2 = %+v", sum2)
	}
	rep, err := loadUsageReport(sessionDir, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Turns) != 2 {
		t.Fatalf("turns = %d", len(rep.Turns))
	}
	if rep.Usage.Source != codingagent.UsageSourceDerivedSessionSum {
		t.Errorf("source = %q", rep.Usage.Source)
	}
}
