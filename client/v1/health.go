package v1

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// HealthResponse is the response from the health endpoint.
type HealthResponse struct {
	Status string `json:"status"`
}

// Health checks if the server is healthy.
func (c *Client) Health(ctx context.Context) (*HealthResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/health", nil)
	if err != nil {
		return nil, fmt.Errorf("create health request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("health check: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read health response: %w", err)
	}

	var health HealthResponse
	if err := json.Unmarshal(body, &health); err != nil {
		return nil, fmt.Errorf("decode health response: %w", err)
	}

	return &health, nil
}
