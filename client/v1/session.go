package v1

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// SessionInfo is the typed session record returned by GetSession and UpdateSession.
type SessionInfo struct {
	ID             string                  `json:"id"`
	AgentName      string                  `json:"agent_name"`
	Model          string                  `json:"model"`
	Status         string                  `json:"status"`
	Error          string                  `json:"error,omitempty"`
	WorkDir        string                  `json:"work_dir"`
	StorageRoot    string                  `json:"storage_root,omitempty"`
	AgentSessionID string                  `json:"agent_session_id"`
	SessionDir     string                  `json:"session_dir"`
	ConfigDir      string                  `json:"config_dir,omitempty"`
	SandboxMode    string                  `json:"sandbox_mode,omitempty"`
	CreatedAt      time.Time               `json:"created_at"`
	UpdatedAt      time.Time               `json:"updated_at"`
	AgentBindings  map[string]AgentBinding `json:"agent_bindings,omitempty"`
	ActiveAgent    string                  `json:"active_agent,omitempty"`
	Supplement     SupplementStrategy      `json:"supplement,omitempty"`
	Followable     bool                    `json:"followable,omitempty"`
	TurnID         string                  `json:"turn_id,omitempty"`
}

// AgentBinding is a native session id and ingest watermark for one coding agent.
type AgentBinding struct {
	AgentSessionID     string `json:"agent_session_id"`
	IngestedThroughSeq int    `json:"ingested_through_seq"`
}

// SupplementStrategy selects how Tern reconstructs foreign-origin history on switch.
type SupplementStrategy struct {
	Algorithm        string `json:"algorithm,omitempty"`
	Model            string `json:"model,omitempty"`
	MaxChunkMessages int    `json:"max_chunk_messages,omitempty"`
	ThresholdBytes   int    `json:"threshold_bytes,omitempty"`
	RecentKeep       int    `json:"recent_keep,omitempty"`
}

// UpdateSessionRequest is the PATCH body. At least one field must be set.
type UpdateSessionRequest struct {
	ConfigDir  *string             `json:"config_dir,omitempty"`
	Agent      *string             `json:"agent,omitempty"`
	Model      *string             `json:"model,omitempty"`
	Supplement *SupplementStrategy `json:"supplement,omitempty"`
}

// SendMessageOpts are optional SendMessage fields.
type SendMessageOpts struct {
	CorrelationID string
	Supplement    *SupplementStrategy
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
	Agent       string `json:"agent"`
	Model       string `json:"model,omitempty"`
	WorkDir     string `json:"work_dir"`
	StorageRoot string `json:"storage_root,omitempty"`
	SessionDir  string `json:"session_dir,omitempty"`
	ConfigDir   string `json:"config_dir,omitempty"`
	// SandboxMode is optional: "read-only" (default), "workspace-write", or "danger-full-access".
	SandboxMode string `json:"sandbox_mode,omitempty"`
}

// Sandbox mode values for SessionRequest / SessionInfo (aligned with Codex CLI -s).
const (
	SandboxModeReadOnly         = "read-only"
	SandboxModeWorkspaceWrite   = "workspace-write"
	SandboxModeDangerFullAccess = "danger-full-access"
)

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
	return s.SendMessageWithOpts(ctx, content, SendMessageOpts{})
}

// SendMessageWithCorrelation sends a multimodal message with an optional correlation ID.
func (s *Session) SendMessageWithCorrelation(ctx context.Context, content []ContentPart, correlationID string) (*Stream, error) {
	return s.SendMessageWithOpts(ctx, content, SendMessageOpts{CorrelationID: correlationID})
}

// SendMessageWithOpts sends a message with optional correlation_id and supplement override.
func (s *Session) SendMessageWithOpts(ctx context.Context, content []ContentPart, opts SendMessageOpts) (*Stream, error) {
	body, err := json.Marshal(map[string]any{
		"content":        content,
		"correlation_id": opts.CorrelationID,
		"supplement":     opts.Supplement,
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

func (s *Session) follow(ctx context.Context, from string) (*Stream, error) {
	u := s.client.baseURL + "/api/v1/sessions/" + s.ID + "/events"
	if from != "" {
		u += "?from=" + url.QueryEscape(from)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("create follow request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := s.client.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("follow session: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("follow failed (HTTP %d): %s", resp.StatusCode, string(respBody))
	}
	return newStream(resp.Body), nil
}

// Follow attaches to the in-flight turn SSE from the start of the relay buffer.
func (s *Session) Follow(ctx context.Context) (*Stream, error) {
	return s.follow(ctx, "")
}

// FollowFrom attaches to the in-flight turn SSE after lastEventID (logical index).
func (s *Session) FollowFrom(ctx context.Context, lastEventID string) (*Stream, error) {
	return s.follow(ctx, lastEventID)
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
func (c *Client) UpdateSession(ctx context.Context, sessionID string, reqBody UpdateSessionRequest) (*SessionInfo, error) {
	body, err := json.Marshal(reqBody)
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

// UpdateSessionConfigDir sets config_dir on an existing session via PATCH.
func (c *Client) UpdateSessionConfigDir(ctx context.Context, sessionID, configDir string) (*SessionInfo, error) {
	return c.UpdateSession(ctx, sessionID, UpdateSessionRequest{ConfigDir: &configDir})
}

// UpdateConfigDir updates config_dir for this session (see UpdateSessionConfigDir).
func (s *Session) UpdateConfigDir(ctx context.Context, configDir string) (*SessionInfo, error) {
	return s.client.UpdateSessionConfigDir(ctx, s.ID, configDir)
}

// Update updates session fields (agent, model, config_dir, and/or supplement).
func (s *Session) Update(ctx context.Context, req UpdateSessionRequest) (*SessionInfo, error) {
	return s.client.UpdateSession(ctx, s.ID, req)
}

// UpdateAgent switches the coding agent on the same Tern session.
func (s *Session) UpdateAgent(ctx context.Context, agent string) (*SessionInfo, error) {
	return s.Update(ctx, UpdateSessionRequest{Agent: &agent})
}

// UpdateModel changes the model without switching agents.
func (s *Session) UpdateModel(ctx context.Context, model string) (*SessionInfo, error) {
	return s.Update(ctx, UpdateSessionRequest{Model: &model})
}

// ListSessions lists sessions persisted under workDir/.tern (or storageRoot/.tern when set).
func (c *Client) ListSessions(ctx context.Context, workDir string, storageRoot ...string) ([]SessionInfo, error) {
	u := c.baseURL + "/api/v1/sessions?work_dir=" + url.QueryEscape(workDir)
	if len(storageRoot) > 0 && storageRoot[0] != "" {
		u += "&storage_root=" + url.QueryEscape(storageRoot[0])
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("create list sessions request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read list sessions: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list sessions failed (HTTP %d): %s", resp.StatusCode, string(respBody))
	}
	var result []SessionInfo
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("decode list sessions: %w", err)
	}
	return result, nil
}
