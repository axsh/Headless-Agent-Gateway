package v1

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// SessionInfo is the typed session record returned by GetSession and
// UpdateSessionConfigDir. Field names match the CAWA JSON (snake_case tags).
type SessionInfo struct {
	ID             string    `json:"id"`
	AgentName      string    `json:"agent_name"`
	Model          string    `json:"model"`
	Status         string    `json:"status"`
	Error          string    `json:"error,omitempty"`
	WorkDir        string    `json:"work_dir"`
	AgentSessionID string    `json:"agent_session_id"`
	SessionDir     string    `json:"session_dir"`
	ConfigDir      string    `json:"config_dir,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

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

// SendMessage sends a multimodal message to the session and returns a Stream.
// The content parameter accepts a slice of ContentPart for text, images, etc.
func (s *Session) SendMessage(ctx context.Context, content []ContentPart) (*Stream, error) {
	return s.SendMessageWithCorrelation(ctx, content, "")
}

// SendMessageWithCorrelation sends a multimodal message with an optional correlation ID.
func (s *Session) SendMessageWithCorrelation(ctx context.Context, content []ContentPart, correlationID string) (*Stream, error) {
	body, err := json.Marshal(map[string]any{
		"content":        content,
		"correlation_id": correlationID,
	})
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

// SendText is a convenience method for sending text-only messages.
func (s *Session) SendText(ctx context.Context, message string) (*Stream, error) {
	return s.SendMessage(ctx, []ContentPart{{Type: "text", Text: message}})
}

// SendImageFile is a convenience method that reads an image file from path,
// automatically detects its media type, and sends it alongside a text prompt.
func (s *Session) SendImageFile(ctx context.Context, path string, prompt string) (*Stream, error) {
	parts, err := NewMessage().
		Text(prompt).
		ImageFile(path).
		Build()
	if err != nil {
		return nil, err
	}
	return s.SendMessage(ctx, parts)
}

// Respond sends user input to a suspended session and returns a continuation stream.
func (s *Session) Respond(ctx context.Context, content string) (*Stream, error) {
	body, err := json.Marshal(map[string]string{"content": content})
	if err != nil {
		return nil, fmt.Errorf("marshal respond request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		s.client.baseURL+"/api/v1/sessions/"+s.ID+"/respond",
		bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create respond request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := s.client.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send respond request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("respond failed (HTTP %d): %s", resp.StatusCode, string(respBody))
	}

	return newStream(resp.Body), nil
}

// SendTextWithHandlers sends a text message and runs the interactive handler loop.
func (s *Session) SendTextWithHandlers(ctx context.Context, message string, h StreamHandlers) error {
	stream, err := s.SendText(ctx, message)
	if err != nil {
		return err
	}
	if h.OnText == nil {
		h.OnText = func(text string) { fmt.Print(text) }
	}
	return stream.RunWithHandlers(ctx, s, h)
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

// GetSession fetches session details by ID.
// Does not change work_dir, session_dir, or agent_session_id.
func (c *Client) GetSession(ctx context.Context, sessionID string) (*SessionInfo, error) {
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
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get session failed (HTTP %d): %s", resp.StatusCode, string(respBody))
	}

	var result SessionInfo
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("decode session response: %w", err)
	}
	return &result, nil
}

// UpdateSessionConfigDir sets config_dir on an existing session via PATCH.
// Pass an empty configDir to clear (disable overlay on subsequent launches;
// for Codex this restores --ignore-user-config on later launches).
//
// Semantics:
//   - Does not change work_dir, session_dir, or agent_session_id.
//   - Overlay applies on the next SendMessage / SendText / Send, not immediately.
//   - Do not Terminate between turns merely to switch config_dir; terminate is
//     only for forced teardown / cleanup after the demo.
func (c *Client) UpdateSessionConfigDir(ctx context.Context, sessionID, configDir string) (*SessionInfo, error) {
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
	var result SessionInfo
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("decode patch response: %w", err)
	}
	return &result, nil
}

// UpdateConfigDir updates config_dir for this session (see UpdateSessionConfigDir).
func (s *Session) UpdateConfigDir(ctx context.Context, configDir string) (*SessionInfo, error) {
	return s.client.UpdateSessionConfigDir(ctx, s.ID, configDir)
}
