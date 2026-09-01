package v1

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Model source values for TokenUsage.ModelSource (mirror server API).
const (
	ModelSourceAgent       = "agent"
	ModelSourceTernSession = "tern_session"
)

// TokenUsage mirrors server token accounting fields.
type TokenUsage struct {
	InputTokens              int      `json:"input_tokens"`
	OutputTokens             int      `json:"output_tokens"`
	CachedInputTokens        int      `json:"cached_input_tokens,omitempty"`
	CacheCreationInputTokens int      `json:"cache_creation_input_tokens,omitempty"`
	ReasoningOutputTokens    int      `json:"reasoning_output_tokens,omitempty"`
	TotalTokens              int      `json:"total_tokens,omitempty"`
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

// TurnUsageRecord is one SendMessage turn in a usage report.
type TurnUsageRecord struct {
	TurnID  string       `json:"turn_id"`
	EndedAt time.Time    `json:"ended_at,omitempty"`
	Usage   TokenUsage   `json:"usage"`
	Calls   []TokenUsage `json:"calls,omitempty"`
}

// SessionUsageReport is returned by GET /api/v1/sessions/:id/usage.
type SessionUsageReport struct {
	SessionID string            `json:"session_id"`
	Usage     TokenUsage        `json:"usage"`
	Turns     []TurnUsageRecord `json:"turns"`
}

// UsageQuery filters turns for GetUsage. Zero value means all turns.
type UsageQuery struct {
	LastN       int
	AfterTurnID string
	FromTurnID  string
	ToTurnID    string
	Since       time.Time
	Until       time.Time
}

// GetUsage fetches session / turn / call token usage for a session.
// Optional opts (first only) map to query parameters such as last_n.
func (c *Client) GetUsage(ctx context.Context, sessionID string, opts ...UsageQuery) (*SessionUsageReport, error) {
	u := c.baseURL + "/api/v1/sessions/" + sessionID + "/usage"
	if len(opts) > 0 {
		if q := encodeUsageQuery(opts[0]); q != "" {
			u += "?" + q
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("create get usage request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get usage: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read usage response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get usage failed (HTTP %d): %s", resp.StatusCode, string(respBody))
	}
	var result SessionUsageReport
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("decode usage response: %w", err)
	}
	return &result, nil
}

// GetUsage fetches token usage for this session.
func (s *Session) GetUsage(ctx context.Context, opts ...UsageQuery) (*SessionUsageReport, error) {
	return s.client.GetUsage(ctx, s.ID, opts...)
}

func encodeUsageQuery(q UsageQuery) string {
	v := url.Values{}
	if q.LastN > 0 {
		v.Set("last_n", strconv.Itoa(q.LastN))
	}
	if q.AfterTurnID != "" {
		v.Set("after_turn_id", q.AfterTurnID)
	}
	if q.FromTurnID != "" {
		v.Set("from_turn_id", q.FromTurnID)
	}
	if q.ToTurnID != "" {
		v.Set("to_turn_id", q.ToTurnID)
	}
	if !q.Since.IsZero() {
		v.Set("since", q.Since.UTC().Format(time.RFC3339))
	}
	if !q.Until.IsZero() {
		v.Set("until", q.Until.UTC().Format(time.RFC3339))
	}
	return v.Encode()
}
