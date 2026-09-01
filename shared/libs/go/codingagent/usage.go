package codingagent

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
	Source                   string   `json:"source"`
	Confidence               string   `json:"confidence"`
	TurnID                   string   `json:"turn_id,omitempty"`
	CallID                   string   `json:"call_id,omitempty"`
	Partial                  bool     `json:"partial,omitempty"`
	CallsSumMismatch         bool     `json:"calls_sum_mismatch,omitempty"`
}

// TurnUsageRecord is one SendMessage turn persisted under usage.json.
type TurnUsageRecord struct {
	TurnID string       `json:"turn_id"`
	Usage  TokenUsage   `json:"usage"`
	Calls  []TokenUsage `json:"calls,omitempty"`
}

// SessionUsageReport is the GET /api/v1/sessions/:id/usage response body.
type SessionUsageReport struct {
	SessionID string            `json:"session_id"`
	Usage     TokenUsage        `json:"usage"`
	Turns     []TurnUsageRecord `json:"turns"`
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
