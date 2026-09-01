package agentservice

import (
	"strconv"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

type turnUsageAggregator struct {
	turnID     string
	calls      []codingagent.TokenUsage
	seenCall   map[string]struct{}
	turn       *codingagent.TokenUsage
	finalized  bool
	finalRec   codingagent.TurnUsageRecord
	finalOK    bool
}

func newTurnUsageAggregator(turnID string) *turnUsageAggregator {
	return &turnUsageAggregator{
		turnID:   turnID,
		seenCall: make(map[string]struct{}),
	}
}

func (a *turnUsageAggregator) Observe(ev codingagent.StreamEvent) {
	if a == nil || ev.Usage == nil {
		return
	}
	u := *ev.Usage
	if u.CallID != "" {
		if _, ok := a.seenCall[u.CallID]; ok {
			return
		}
		a.seenCall[u.CallID] = struct{}{}
		a.calls = append(a.calls, u)
		return
	}
	if ev.Type != codingagent.EventResult && u.Source != codingagent.UsageSourceCodexTokenCount {
		return
	}
	// Prefer high-trust turn totals from result events.
	if ev.Type == codingagent.EventResult {
		switch u.Source {
		case codingagent.UsageSourceClaudeResult, codingagent.UsageSourceCodexTurnCompleted:
			cp := u
			a.turn = &cp
			return
		}
		if a.turn == nil {
			cp := u
			a.turn = &cp
		}
		return
	}
	// token_count fallback when no turn total yet
	if a.turn == nil && u.Source == codingagent.UsageSourceCodexTokenCount {
		cp := u
		a.turn = &cp
	}
}

func (a *turnUsageAggregator) MergeCalls(extra []codingagent.TokenUsage) {
	for i, u := range extra {
		id := u.CallID
		if id == "" {
			id = "gw-" + strconv.Itoa(len(a.calls)+i)
		}
		if _, ok := a.seenCall[id]; ok {
			continue
		}
		u.CallID = id
		a.seenCall[id] = struct{}{}
		a.calls = append(a.calls, u)
	}
}

func (a *turnUsageAggregator) Finalize() (codingagent.TurnUsageRecord, bool) {
	if a == nil {
		return codingagent.TurnUsageRecord{}, false
	}
	if a.finalized {
		return a.finalRec, a.finalOK
	}
	a.finalized = true
	var turn codingagent.TokenUsage
	ok := false
	if a.turn != nil {
		turn = *a.turn
		ok = true
	} else if len(a.calls) > 0 {
		turn = codingagent.TokenUsage{
			Source:     codingagent.UsageSourceLLMGateway,
			Confidence: codingagent.UsageConfidenceLow,
			Partial:    true,
		}
		for _, c := range a.calls {
			codingagent.AddUsage(&turn, c)
		}
		ok = true
	}
	if !ok {
		a.finalOK = false
		return codingagent.TurnUsageRecord{}, false
	}
	turn.TurnID = a.turnID
	if len(a.calls) > 0 {
		var callSum codingagent.TokenUsage
		for _, c := range a.calls {
			codingagent.AddUsage(&callSum, c)
		}
		if callSum.InputTokens != turn.InputTokens || callSum.OutputTokens != turn.OutputTokens {
			turn.CallsSumMismatch = true
		}
	}
	rec := codingagent.TurnUsageRecord{
		TurnID: a.turnID,
		Usage:  turn,
		Calls:  append([]codingagent.TokenUsage(nil), a.calls...),
	}
	a.finalRec = rec
	a.finalOK = true
	return rec, true
}
