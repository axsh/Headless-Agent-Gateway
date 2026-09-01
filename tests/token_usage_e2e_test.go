package llm_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	v1 "github.com/axsh/arctic-tern/client/v1"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

// TestClaudeCodeE2E_TokenUsage_TurnAndSession verifies result.usage and GetUsage
// after a short live SendMessage (V1 / V3 / V7).
func TestClaudeCodeE2E_TokenUsage_TurnAndSession(t *testing.T) {
	baseURL, cleanup := startE2EServer(t)
	defer cleanup()

	workDir := t.TempDir()
	sessionID := createE2ESessionWithModel(t, baseURL, "claudecode", e2eDefaultModel, workDir)

	body := sendE2EMessage(t, baseURL, sessionID, "Reply with exactly: pong", 3*time.Minute)
	defer body.Body.Close()
	events, _ := parseE2ESSEEvents(t, body)
	var resultUsage *codingagent.TokenUsage
	for _, ev := range events {
		if ev.Type == codingagent.EventResult {
			resultUsage = ev.Usage
		}
		if ev.Type == codingagent.EventError {
			t.Skipf("Skipping: agent error: %s", ev.Content)
		}
	}
	if resultUsage == nil {
		t.Fatal("expected result.usage")
	}
	// Claude may report input_tokens=0 on some gateway/cache paths; require any
	// positive accounting signal (output, cache, or cost).
	if resultUsage.OutputTokens <= 0 && resultUsage.InputTokens <= 0 &&
		resultUsage.CachedInputTokens <= 0 && resultUsage.TotalCostUSD == nil {
		t.Fatalf("result usage empty = %+v", resultUsage)
	}

	req, err := http.NewRequest(http.MethodGet, baseURL+"/api/v1/sessions/"+sessionID+"/usage", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET usage status = %d", resp.StatusCode)
	}
	var rep codingagent.SessionUsageReport
	if err := json.NewDecoder(resp.Body).Decode(&rep); err != nil {
		t.Fatal(err)
	}
	if rep.Usage.OutputTokens < resultUsage.OutputTokens || len(rep.Turns) < 1 {
		t.Fatalf("usage report = %+v", rep)
	}

	cli := v1.New(baseURL)
	info, err := cli.GetSession(context.Background(), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if info.Usage == nil || (info.Usage.OutputTokens <= 0 && info.Usage.InputTokens <= 0 && info.Usage.TotalCostUSD == nil) {
		t.Fatalf("GetSession usage = %+v", info.Usage)
	}
	clientRep, err := cli.GetUsage(context.Background(), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if clientRep.Usage.OutputTokens != rep.Usage.OutputTokens {
		t.Fatalf("client usage mismatch: %+v vs %+v", clientRep.Usage, rep.Usage)
	}
}
