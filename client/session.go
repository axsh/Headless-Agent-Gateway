// Deprecated: Use github.com/axsh/arctic-tern/client/v1 instead.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Session represents an active coding agent session.
type Session struct {
	ID     string
	client *Client
}

// ResumeSession creates a Session handle for an existing session ID.
// No server call is made; this merely wraps the ID for subsequent operations.
func ResumeSession(c *Client, sessionID string) *Session {
	return &Session{ID: sessionID, client: c}
}

// SessionRequest is the request to create a session.
type SessionRequest struct {
	Agent      string `json:"agent"`
	Model      string `json:"model,omitempty"`
	WorkDir    string `json:"work_dir"`
	SessionDir string `json:"session_dir,omitempty"`
	ConfigDir  string `json:"config_dir,omitempty"`
}

// CreateSession creates a new session and returns a Session object.
func (c *Client) CreateSession(ctx context.Context, req SessionRequest) (*Session, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal session request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/api/v1/sessions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create session request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send session request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read session response: %w", err)
	}

	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("create session failed (HTTP %d): %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("decode session response: %w", err)
	}

	return &Session{
		ID:     result.SessionID,
		client: c,
	}, nil
}

// SendMessage sends a message to the session and returns a Stream.
func (s *Session) SendMessage(ctx context.Context, message string) (*Stream, error) {
	body, err := json.Marshal(map[string]string{"message": message})
	if err != nil {
		return nil, fmt.Errorf("marshal message: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		s.client.baseURL+"/api/v1/sessions/"+s.ID+"/messages",
		bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create message request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := s.client.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send message: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("send message failed (HTTP %d): %s", resp.StatusCode, string(respBody))
	}

	return newStream(resp.Body), nil
}

// Terminate terminates the session.
func (s *Session) Terminate(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		s.client.baseURL+"/api/v1/sessions/"+s.ID+"/terminate", nil)
	if err != nil {
		return fmt.Errorf("create terminate request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send terminate request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("terminate failed (HTTP %d): %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// GetSession retrieves session details by ID.
func (c *Client) GetSession(ctx context.Context, sessionID string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/api/v1/sessions/"+sessionID, nil)
	if err != nil {
		return nil, fmt.Errorf("create get session request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read session response: %w", err)
	}

	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("decode session response: %w", err)
	}
	return result, nil
}

// UpdateSessionConfigDir sets config_dir on an existing session via PATCH.
// Pass an empty configDir to clear (disable overlay on subsequent launches).
func (c *Client) UpdateSessionConfigDir(ctx context.Context, sessionID, configDir string) (map[string]any, error) {
	body, err := json.Marshal(map[string]string{"config_dir": configDir})
	if err != nil {
		return nil, fmt.Errorf("marshal patch request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch,
		c.baseURL+"/api/v1/sessions/"+sessionID, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create patch session request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("patch session: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read patch response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("patch session failed (HTTP %d): %s", resp.StatusCode, string(respBody))
	}
	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("decode patch response: %w", err)
	}
	return result, nil
}

// UpdateConfigDir updates config_dir for this session.
func (s *Session) UpdateConfigDir(ctx context.Context, configDir string) (map[string]any, error) {
	return s.client.UpdateSessionConfigDir(ctx, s.ID, configDir)
}
