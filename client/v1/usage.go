package v1

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
	Source                   string   `json:"source"`
	Confidence               string   `json:"confidence"`
	TurnID                   string   `json:"turn_id,omitempty"`
	CallID                   string   `json:"call_id,omitempty"`
	Partial                  bool     `json:"partial,omitempty"`
	CallsSumMismatch         bool     `json:"calls_sum_mismatch,omitempty"`
}

// TurnUsageRecord is one SendMessage turn in a usage report.
type TurnUsageRecord struct {
	TurnID string       `json:"turn_id"`
	Usage  TokenUsage   `json:"usage"`
	Calls  []TokenUsage `json:"calls,omitempty"`
}

// SessionUsageReport is returned by GET /api/v1/sessions/:id/usage.
type SessionUsageReport struct {
	SessionID string            `json:"session_id"`
	Usage     TokenUsage        `json:"usage"`
	Turns     []TurnUsageRecord `json:"turns"`
}

// GetUsage fetches session / turn / call token usage for a session.
func (c *Client) GetUsage(ctx context.Context, sessionID string) (*SessionUsageReport, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/api/v1/sessions/"+sessionID+"/usage", nil)
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
func (s *Session) GetUsage(ctx context.Context) (*SessionUsageReport, error) {
	return s.client.GetUsage(ctx, s.ID)
}
