package agentservice

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/llmgateway"
	"github.com/axsh/arctic-tern/shared/libs/go/tasklog"
	"github.com/axsh/arctic-tern/shared/libs/go/wayfinder/session"
)

// MultimodalSupporter is an optional interface that agents can implement
// to declare whether they support non-text content (e.g., images).
type MultimodalSupporter interface {
	SupportsMultimodal() bool
}

// SendMessageRequest is the request body for POST /api/v1/sessions/:id/messages.
type SendMessageRequest struct {
	Content []codingagent.ContentPart `json:"content"`
}

// handleListAgents handles GET /api/v1/agents.
func (s *Server) handleListAgents(w http.ResponseWriter, r *http.Request) {
	type agentInfo struct {
		Name string `json:"name"`
	}
	agents := make([]agentInfo, 0, len(s.agents))
	for name := range s.agents {
		agents = append(agents, agentInfo{Name: name})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(agents)
}

// handleListModels handles GET /api/v1/models.
// Returns the cached model list and default model from LLMGP.
func (s *Server) handleListModels(w http.ResponseWriter, r *http.Request) {
	models := s.gatewayModels
	if models == nil {
		models = []llmgateway.ModelInfo{}
	}
	resp := map[string]any{
		"models": models,
	}
	if s.gatewayDefault != nil {
		resp["default_model"] = s.gatewayDefault
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleCreateSession handles POST /api/v1/sessions.
func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Agent      string `json:"agent"`
		Model      string `json:"model"`
		WorkDir    string `json:"work_dir"`
		Prompt     string `json:"prompt"`
		SessionDir string `json:"session_dir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if s.logger != nil {
		s.logger.Debug("creating session", "agent", req.Agent, "model", req.Model, "work_dir", req.WorkDir)
	}

	if _, ok := s.agents[req.Agent]; !ok {
		http.Error(w, "unknown agent: "+req.Agent, http.StatusBadRequest)
		return
	}

	// Validate and resolve model (supports logical names).
	if req.Model != "" && len(s.gatewayModels) > 0 {
		resolved, ok := s.ResolveModel(req.Model)
		if ok {
			req.Model = resolved
		} else if !s.IsValidModel(req.Model) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]any{
				"error":            "unsupported model: " + req.Model,
				"available_models": s.AvailableModelNames(),
			})
			return
		}
	}

	sessionID := s.generateID()
	record := &codingagent.SessionRecord{
		ID:         sessionID,
		AgentName:  req.Agent,
		Model:      req.Model,
		Status:     codingagent.StatusActive,
		WorkDir:    req.WorkDir,
		SessionDir: req.SessionDir,
	}

	// R2, R4: Resolve WorkDir to absolute path for record consistency.
	if record.WorkDir != "" {
		if abs, err := filepath.Abs(record.WorkDir); err == nil {
			record.WorkDir = abs
		}
	}

	// SessionDir fallback: use WorkDir/.AgentName if not explicitly set.
	if record.SessionDir == "" && record.WorkDir != "" {
		if record.AgentName != "" {
			record.SessionDir = filepath.Join(record.WorkDir, "."+record.AgentName)
		} else {
			record.SessionDir = record.WorkDir
		}
	}

	// R1, R4: Resolve SessionDir to absolute path for record consistency.
	if record.SessionDir != "" {
		if abs, err := filepath.Abs(record.SessionDir); err == nil {
			record.SessionDir = abs
		}
	}

	// R5: Log resolved paths for debugging.
	if s.logger != nil {
		s.logger.Debug("session paths resolved",
			"session_id", sessionID,
			"work_dir", record.WorkDir,
			"session_dir", record.SessionDir)
	}

	s.sessions.Create(record)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{
		"session_id": sessionID,
		"status":     "created",
	})
}

// handleGetSession handles GET /api/v1/sessions/:id.
func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	id := extractPathParam(r.URL.Path, "/api/v1/sessions/")
	if s.logger != nil {
		s.logger.Debug("getting session", "session_id", id)
	}
	record, err := s.sessions.Get(id)
	if err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(record)
}

// handleDeleteSession handles DELETE /api/v1/sessions/:id.
func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	id := extractPathParam(r.URL.Path, "/api/v1/sessions/")
	if s.logger != nil {
		s.logger.Debug("deleting session", "session_id", id)
	}
	if err := s.sessions.Delete(id); err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleSendMessage handles POST /api/v1/sessions/:id/messages.
// Accepts {"content": []ContentPart} with multimodal support.
// Content Negotiation: Accept: text/event-stream -> SSE, otherwise -> JSON.
func (s *Server) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/sessions/"), "/")
	if len(parts) < 2 {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	sessionID := parts[0]

	if s.logger != nil {
		s.logger.Debug("send message", "session_id", sessionID)
	}

	record, err := s.sessions.Get(sessionID)
	if err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	var req SendMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Validate content parts.
	if err := codingagent.ValidateContentParts(req.Content); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	agent, ok := s.agents[record.AgentName]
	if !ok {
		http.Error(w, "agent not available", http.StatusInternalServerError)
		return
	}

	// Check multimodal support if non-text content is present.
	hasMultimodal := codingagent.HasNonTextContent(req.Content)
	if hasMultimodal {
		if supporter, ok := agent.(MultimodalSupporter); ok {
			if !supporter.SupportsMultimodal() {
				http.Error(w, codingagent.ErrMultimodalNotSupported.Error(), http.StatusNotImplemented)
				return
			}
		}
	}

	// Build the prompt string from content parts.
	var promptText string
	var savedFiles []string

	if hasMultimodal {
		// Use temporary multimodal prompt builder (does not persist to session dir).
		var err error
		promptText, savedFiles, err = BuildMultimodalPrompt(record.WorkDir, sessionID, req.Content)
		if err != nil {
			if s.logger != nil {
				s.logger.Error("failed to build multimodal prompt", "error", err.Error(), "session_id", sessionID)
			}
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	} else {
		promptText = codingagent.ExtractText(req.Content)
	}

	// Validate prompt size against configured limit.
	maxBytes := 1048576 // Default 1MB
	if s.profiles != nil && s.profiles.CodingAgents != nil {
		if agentCfg, ok := s.profiles.CodingAgents[record.AgentName]; ok && agentCfg.MaxPromptBytes > 0 {
			maxBytes = agentCfg.MaxPromptBytes
		}
	}
	if len(promptText) > maxBytes {
		errMsg := fmt.Sprintf("prompt size (%d bytes) exceeds the limit (%d bytes)", len(promptText), maxBytes)
		if s.logger != nil {
			s.logger.Warn("prompt too large", "session_id", sessionID, "size", len(promptText), "limit", maxBytes)
		}
		http.Error(w, errMsg, http.StatusRequestEntityTooLarge)
		return
	}

	// Append user message to persistent session history (text parts only to keep it stateless).
	var sessionParts []session.ContentPart
	for _, p := range req.Content {
		if p.Type == "text" {
			sessionParts = append(sessionParts, session.ContentPart{
				Type: "text",
				Text: p.Text,
			})
		} else if p.Type == "image" {
			// Record image presence without the binary data or path.
			sessionParts = append(sessionParts, session.ContentPart{
				Type: "image",
				Image: &session.ImageMetadata{
					MediaType: p.Source.MediaType,
					Path:      "", // No persistent path in stateless mode
				},
			})
		}
	}
	AppendSessionMessage(record.SessionDir, session.Message{
		Role:         "user",
		ContentParts: sessionParts,
		Timestamp:    time.Now(),
	})

	if s.logger != nil {
		s.logger.Debug("sending message to agent", "session_id", sessionID, "agent", record.AgentName, "model", record.Model)
		s.logger.Trace("message content", "prompt", promptText)
	}

	opts := []codingagent.SessionOption{
		codingagent.WithModel(record.Model),
		codingagent.WithPrompt(promptText),
		codingagent.WithWorkDir(record.WorkDir),
	}
	if record.AgentSessionID != "" {
		opts = append(opts, codingagent.WithAgentSessionID(record.AgentSessionID))
	}
	if record.SessionDir != "" {
		opts = append(opts, codingagent.WithSessionDir(record.SessionDir))
	}

	// Context separation: create an independent execution context
	// so agent continues running even if the HTTP client disconnects.
	execCtx, execCancel := context.WithCancel(context.Background())
	s.RegisterExecCancel(sessionID, execCancel)

	session, err := agent.CreateSession(execCtx, opts...)
	if err != nil {
		execCancel()
		s.UnregisterExecCancel(sessionID)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.RegisterActiveSession(sessionID, session)
	defer func() {
		session.Close()
		s.UnregisterActiveSession(sessionID)
		s.UnregisterExecCancel(sessionID)
		// Cleanup multimodal temp files after session completes.
		if len(savedFiles) > 0 {
			CleanupMultimodalFiles(savedFiles)
			if s.logger != nil {
				s.logger.Debug("cleaned up multimodal temp files", "session_id", sessionID, "count", len(savedFiles))
			}
		}
	}()

	ch, err := session.Send(execCtx, promptText)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		s.streamSSE(r.Context(), w, ch, sessionID)
	} else {
		s.respondJSON(r.Context(), w, ch, sessionID)
	}
}

// toAgentLogEntry converts a StreamEvent to an AgentLogEntry for TaskLog.
func toAgentLogEntry(ev codingagent.StreamEvent, sessionID string) *tasklog.AgentLogEntry {
	body, _ := json.Marshal(ev)
	logID := generateLogID()
	return tasklog.NewAgentLogSendEntry(logID, sessionID, string(body))
}

// generateLogID creates a random hex ID for log entries.
func generateLogID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// streamSSE sends streaming events in SSE format.
func (s *Server) streamSSE(ctx context.Context, w http.ResponseWriter, ch <-chan codingagent.StreamEvent, sessionID string) {
	if s.logger != nil {
		s.logger.Debug("starting SSE stream", "session_id", sessionID)
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	eventCount := 0
	var hasError bool
	var errorMsg string

	// Heartbeat ticker: send keepalive comments every 15 seconds.
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			if s.logger != nil {
				s.logger.Warn("client disconnected during SSE stream",
					"session_id", sessionID,
					"events_sent", eventCount)
			}
			return
		case <-ticker.C:
			// SSE keepalive comment to prevent intermediate proxy/OS timeouts.
			fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
		case ev, ok := <-ch:
			if !ok {
				goto done
			}
			eventCount++
			if ev.Type == codingagent.EventError {
				hasError = true
				errorMsg = ev.Content
			}
			if s.logger != nil {
				contentPreview := ""
				if ev.Content != "" {
					if len(ev.Content) > 100 {
						contentPreview = ev.Content[:100] + "..."
					} else {
						contentPreview = ev.Content
					}
				}
				s.logger.Trace("SSE stream event", "type", ev.Type, "content_preview", contentPreview)
			}

			data, _ := json.Marshal(ev)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()

			// Record event to TaskLog (C1-1)
			if s.taskLog != nil {
				s.taskLog.Add(toAgentLogEntry(ev, sessionID))
			}

			// Extract AgentSessionID from EventSystem (C2-1)
			if ev.Type == codingagent.EventSystem && ev.SessionID != "" {
				if s.logger != nil {
					s.logger.Debug("agent session ID extracted", "session_id", sessionID, "agent_session_id", ev.SessionID)
				}
				if record, err := s.sessions.Get(sessionID); err == nil {
					record.AgentSessionID = ev.SessionID
					s.sessions.Update(record)
				}
			}
		}
	}
done:
	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()

	if s.logger != nil {
		s.logger.Debug("SSE stream completed", "session_id", sessionID, "event_count", eventCount)
	}

	if record, err := s.sessions.Get(sessionID); err == nil {
		if hasError {
			record.Status = codingagent.StatusError
			if errorMsg != "" {
				record.Error = errorMsg
			} else {
				record.Error = "unknown error occurred during execution"
			}
		} else {
			record.Status = codingagent.StatusCompleted
		}
		s.sessions.Update(record)
	}
}

// respondJSON sends all events as a JSON array.
func (s *Server) respondJSON(ctx context.Context, w http.ResponseWriter, ch <-chan codingagent.StreamEvent, sessionID string) {
	var events []codingagent.StreamEvent
	var hasError bool
	var errorMsg string
	for {
		select {
		case <-ctx.Done():
			if s.logger != nil {
				s.logger.Debug("client disconnected, stopping JSON response", "session_id", sessionID)
			}
			return
		case ev, ok := <-ch:
			if !ok {
				goto done
			}
			events = append(events, ev)
			if ev.Type == codingagent.EventError {
				hasError = true
				errorMsg = ev.Content
			}

			// Record event to TaskLog (C1-4)
			if s.taskLog != nil {
				s.taskLog.Add(toAgentLogEntry(ev, sessionID))
			}

			// Extract AgentSessionID from EventSystem (C2-1)
			if ev.Type == codingagent.EventSystem && ev.SessionID != "" {
				if s.logger != nil {
					s.logger.Debug("agent session ID extracted", "session_id", sessionID, "agent_session_id", ev.SessionID)
				}
				if record, err := s.sessions.Get(sessionID); err == nil {
					record.AgentSessionID = ev.SessionID
					s.sessions.Update(record)
				}
			}
		}
	}
done:
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(events)

	if record, err := s.sessions.Get(sessionID); err == nil {
		if hasError {
			record.Status = codingagent.StatusError
			if errorMsg != "" {
				record.Error = errorMsg
			} else {
				record.Error = "unknown error occurred during execution"
			}
		} else {
			record.Status = codingagent.StatusCompleted
		}
		s.sessions.Update(record)
	}
}

// handleTerminate handles POST /api/v1/sessions/:id/terminate.
func (s *Server) handleTerminate(w http.ResponseWriter, r *http.Request) {
	// Extract session ID from /api/v1/sessions/{id}/terminate
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/sessions/"), "/")
	if len(parts) < 1 {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	sessionID := parts[0]

	record, err := s.sessions.Get(sessionID)
	if err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	if s.logger != nil {
		s.logger.Debug("terminating session", "session_id", sessionID)
	}

	// Cancel the agent execution context.
	s.CancelExecution(sessionID)

	record.Status = codingagent.StatusClosed
	s.sessions.Update(record)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "terminated"})
}

// extractPathParam extracts the first path component after the given prefix.
func extractPathParam(path, prefix string) string {
	trimmed := strings.TrimPrefix(path, prefix)
	if idx := strings.Index(trimmed, "/"); idx >= 0 {
		return trimmed[:idx]
	}
	return trimmed
}
