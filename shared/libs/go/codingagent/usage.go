package codingagent

import "time"

// Usage source identifiers (stable API IDs).
const (
	UsageSourceClaudeResult       = "claude_result"
	UsageSourceClaudeAssistant    = "claude_assistant"
	UsageSourceCodexTurnCompleted = "codex_turn_completed"
	UsageSourceCodexTokenCount    = "codex_token_count"
	UsageSourceLLMGateway         = "llmgateway"
	UsageSourceDerivedSessionSum  = "derived_session_sum"
)

// Usage confidence levels.
const (
	UsageConfidenceHigh = "high"
	UsageConfidenceLow  = "low"
)

// Model source identifiers (where TokenUsage.Model came from).
const (
	ModelSourceAgent       = "agent"
	ModelSourceTernSession = "tern_session"
)

// TokenUsage is token accounting for a turn, call, or session aggregate.
type TokenUsage struct {
	InputTokens              int      `json:"input_tokens"`
	OutputTokens             int      `json:"output_tokens"`
	CachedInputTokens        int      `json:"cached_input_tokens,omitempty"`
	CacheCreationInputTokens int      `json:"cache_creation_input_tokens,omitempty"`
	ReasoningOutputTokens    int      `json:"reasoning_output_tokens,omitempty"`
	TotalTokens              int      `json:"total_tokens,omitempty"` // provider-supplied only; never synthesize
	TotalCostUSD             *float64 `json:"total_cost_usd,omitempty"`
	Model                    string   `json:"model,omitempty"`
	ModelSource              string   `json:"model_source,omitempty"`
	Source                   string   `json:"source"`
	Confidence               string   `json:"confidence"`
	TurnID                   string   `json:"turn_id,omitempty"`
	CallID                   string   `json:"call_id,omitempty"`
	Partial                  bool     `json:"partial,omitempty"`
	CallsSumMismatch         bool     `json:"calls_sum_mismatch,omitempty"`
}

// TurnUsageRecord is one SendMessage turn persisted under usage.json.
type TurnUsageRecord struct {
	TurnID  string       `json:"turn_id"`
	EndedAt time.Time    `json:"ended_at,omitempty"`
	Usage   TokenUsage   `json:"usage"`
	Calls   []TokenUsage `json:"calls,omitempty"`
}

// SessionUsageReport is the GET /api/v1/sessions/:id/usage response body.
type SessionUsageReport struct {
	SessionID string            `json:"session_id"`
	Usage     TokenUsage        `json:"usage"`
	Turns     []TurnUsageRecord `json:"turns"`
}

// UsageQuery filters turns returned by GetUsage / GET .../usage.
// Zero value means no filter (all turns).
type UsageQuery struct {
	LastN       int
	AfterTurnID string
	FromTurnID  string
	ToTurnID    string
	Since       time.Time
	Until       time.Time
}

// Empty reports whether q requests no filtering.
func (q UsageQuery) Empty() bool {
	return q.LastN <= 0 && q.AfterTurnID == "" && q.FromTurnID == "" && q.ToTurnID == "" &&
		q.Since.IsZero() && q.Until.IsZero()
}

// AddUsage sums numeric token fields from src into dst.
// total_tokens is never invented: it is summed only when both sides have non-zero totals,
// otherwise left as dst's value plus src when dst was zero.
// TotalCostUSD is summed only when both pointers are non-nil.
func AddUsage(dst *TokenUsage, src TokenUsage) {
	if dst == nil {
		return
	}
	dst.InputTokens += src.InputTokens
	dst.OutputTokens += src.OutputTokens
	dst.CachedInputTokens += src.CachedInputTokens
	dst.CacheCreationInputTokens += src.CacheCreationInputTokens
	dst.ReasoningOutputTokens += src.ReasoningOutputTokens
	if src.TotalTokens > 0 {
		dst.TotalTokens += src.TotalTokens
	}
	if src.TotalCostUSD != nil {
		if dst.TotalCostUSD == nil {
			v := *src.TotalCostUSD
			dst.TotalCostUSD = &v
		} else {
			*dst.TotalCostUSD += *src.TotalCostUSD
		}
	}
}

// SumTurnUsage sums turn usages into a session-style aggregate.
func SumTurnUsage(turns []TurnUsageRecord) TokenUsage {
	sum := TokenUsage{
		Source:     UsageSourceDerivedSessionSum,
		Confidence: UsageConfidenceHigh,
	}
	for _, tr := range turns {
		AddUsage(&sum, tr.Usage)
	}
	return sum
}

// FilterTurnUsage applies q to turns (append order preserved). LastN is applied last.
func FilterTurnUsage(turns []TurnUsageRecord, q UsageQuery) []TurnUsageRecord {
	if q.Empty() || len(turns) == 0 {
		return turns
	}
	out := turns
	if q.AfterTurnID != "" {
		idx := indexOfTurn(out, q.AfterTurnID)
		if idx < 0 {
			out = nil
		} else {
			out = out[idx+1:]
		}
	}
	if q.FromTurnID != "" || q.ToTurnID != "" {
		out = filterTurnIDRange(out, q.FromTurnID, q.ToTurnID)
	}
	if !q.Since.IsZero() || !q.Until.IsZero() {
		filtered := make([]TurnUsageRecord, 0, len(out))
		for _, tr := range out {
			if tr.EndedAt.IsZero() {
				continue
			}
			if !q.Since.IsZero() && tr.EndedAt.Before(q.Since) {
				continue
			}
			if !q.Until.IsZero() && tr.EndedAt.After(q.Until) {
				continue
			}
			filtered = append(filtered, tr)
		}
		out = filtered
	}
	if q.LastN > 0 && len(out) > q.LastN {
		out = out[len(out)-q.LastN:]
	}
	return out
}

func indexOfTurn(turns []TurnUsageRecord, id string) int {
	for i, tr := range turns {
		if tr.TurnID == id {
			return i
		}
	}
	return -1
}

func filterTurnIDRange(turns []TurnUsageRecord, fromID, toID string) []TurnUsageRecord {
	if fromID == "" && toID == "" {
		return turns
	}
	start := 0
	end := len(turns) - 1
	if fromID != "" {
		start = indexOfTurn(turns, fromID)
		if start < 0 {
			return nil
		}
	}
	if toID != "" {
		end = indexOfTurn(turns, toID)
		if end < 0 {
			return nil
		}
	}
	if start > end {
		return nil
	}
	return turns[start : end+1]
}
