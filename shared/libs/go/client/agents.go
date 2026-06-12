package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Agent describes an available coding agent.
type Agent struct {
	Name string `json:"name"`
}

// ListAgents returns the available coding agents.
func (c *Client) ListAgents(ctx context.Context) ([]Agent, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/agents", nil)
	if err != nil {
		return nil, fmt.Errorf("create agents request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read agents response: %w", err)
	}

	var agents []Agent
	if err := json.Unmarshal(body, &agents); err != nil {
		return nil, fmt.Errorf("decode agents response: %w", err)
	}

	return agents, nil
}
