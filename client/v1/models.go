package v1

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// ModelInfo describes an available model.
type ModelInfo struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

// ModelsResponse is the response from the models endpoint.
type ModelsResponse struct {
	Models       []ModelInfo `json:"models"`
	DefaultModel *ModelInfo  `json:"default_model"`
}

// ListModels returns the available models.
func (c *Client) ListModels(ctx context.Context) (*ModelsResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/models", nil)
	if err != nil {
		return nil, fmt.Errorf("create models request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list models: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read models response: %w", err)
	}

	var result ModelsResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode models response: %w", err)
	}

	return &result, nil
}
