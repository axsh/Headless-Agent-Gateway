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

	"github.com/axsh/arctic-tern/shared/libs/go/artifact/store"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/config"
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

// RespondRequest is the request body for POST /api/v1/sessions/:id/respond.
type RespondRequest struct {
	Content string `json:"content"`
}

func (s *Server) resolveAgentConfig(agentName string) config.AgentConfig {
	return config.ResolveAgentConfig(s.profiles, agentName)
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

	// Register the session in the artifact store for file-operation tracking.
	if s.artifactStore != nil {
		_ = s.artifactStore.UpsertSession(r.Context(), store.Session{
			ID:        sessionID,
			AgentID:   req.Agent,
			AgentName: req.Agent,
			StartedAt: time.Now(),
		})
	}

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

	if exec, ok := s.execRegistry.Get(sessionID); ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]any{
			"error":  "session busy",
			"status": exec.status,
			"hint":   "respond or terminate",
		})
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
		promptText, savedFiles, err = BuildMultimodalPrompt(sessionID, req.Content)
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
	agentCfg := s.resolveAgentConfig(record.AgentName)
	maxBytes := agentCfg.MaxPromptBytes
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
		codingagent.WithExecutionMode(agentCfg.ExecutionMode),
		codingagent.WithIdleTimeout(agentCfg.IdleTimeoutSeconds),
		codingagent.WithMaxExecution(agentCfg.MaxExecutionSeconds),
	}
	if record.AgentSessionID != "" {
		opts = append(opts, codingagent.WithAgentSessionID(record.AgentSessionID))
	}
	if record.SessionDir != "" {
		opts = append(opts, codingagent.WithSessionDir(record.SessionDir))
	}
	if agentCfg.ScannerMaxTokenBytes > 0 {
		opts = append(opts, codingagent.WithScannerMaxTokenBytes(agentCfg.ScannerMaxTokenBytes))
	}
	if agentCfg.MaxToolResultBytes > 0 {
		opts = append(opts, codingagent.WithMaxToolResultBytes(agentCfg.MaxToolResultBytes))
	}

	// Context separation: create an independent execution context
	// so agent continues running even if the HTTP client disconnects.
	execCtx, execCancel := context.WithCancel(context.Background())
	s.RegisterExecCancel(sessionID, execCancel)

	agentSess, err := agent.CreateSession(execCtx, opts...)
	if err != nil {
		execCancel()
		s.UnregisterExecCancel(sessionID)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.RegisterActiveSession(sessionID, agentSess)

	finishExecution := false
	defer func() {
		if !finishExecution {
			return
		}
		agentSess.Close()
		s.UnregisterActiveSession(sessionID)
		s.UnregisterExecCancel(sessionID)
		s.execRegistry.Unregister(sessionID)
		if len(savedFiles) > 0 {
			CleanupMultimodalFiles(savedFiles)
			if s.logger != nil {
				s.logger.Debug("cleaned up multimodal temp files", "session_id", sessionID, "count", len(savedFiles))
			}
		}
	}()

	ch, err := agentSess.Send(execCtx, promptText)
	if err != nil {
		finishExecution = true
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	relay := newEventRelay(ch)
	stdin, _ := agentSess.(codingagent.StdinWriter)
	active := &activeExecution{
		sessionID: sessionID,
		agentSess: agentSess,
		stdin:     stdin,
		relay:     relay,
		status:    codingagent.StatusActive,
	}
	if err := s.execRegistry.Register(sessionID, active); err != nil {
		finishExecution = true
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]any{
			"error":  "session busy",
			"status": codingagent.StatusActive,
			"hint":   "respond or terminate",
		})
		return
	}

	if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		_, suspended := s.streamSSERelay(r.Context(), w, active, true)
		if suspended {
			return
		}
	} else {
		suspended := s.respondJSONRelay(r.Context(), w, active, true)
		if suspended {
			return
		}
	}
	finishExecution = true
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

func (s *Server) writeSSEWireEvents(w http.ResponseWriter, flusher http.Flusher, ev codingagent.StreamEvent) error {
	wireEvents, err := codingagent.SplitStreamEventForSSE(ev, codingagent.DefaultMaxSSEDataLineBytes)
	if err != nil {
		return err
	}
	for _, wireEv := range wireEvents {
		data, _ := json.Marshal(wireEv)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}
	return nil
}

func (s *Server) updateSessionStatusOnTerminal(sessionID string, ev codingagent.StreamEvent, hasError bool, errorMsg string) {
	if ev.Type != codingagent.EventResult && ev.Type != codingagent.EventError {
		return
	}
	record, err := s.sessions.Get(sessionID)
	if err != nil {
		return
	}
	if hasError || ev.Type == codingagent.EventError {
		record.Status = codingagent.StatusError
		if errorMsg != "" {
			record.Error = errorMsg
		} else if ev.Content != "" {
			record.Error = ev.Content
		} else {
			record.Error = "unknown error occurred during execution"
		}
	} else {
		record.Status = codingagent.StatusCompleted
		record.Error = ""
	}
	s.sessions.Update(record)
}

func (s *Server) finalizeSessionStatusOnDisconnect(sessionID string, exec *activeExecution) {
	if exec == nil || exec.relay == nil {
		return
	}
	record, err := s.sessions.Get(sessionID)
	if err != nil {
		return
	}
	if record.Status == codingagent.StatusCompleted || record.Status == codingagent.StatusError {
		return
	}

	var hasError bool
	var errorMsg string
	for _, ev := range exec.relay.EventsSnapshot() {
		switch ev.Type {
		case codingagent.EventResult:
			s.updateSessionStatusOnTerminal(sessionID, ev, hasError, errorMsg)
			return
		case codingagent.EventError:
			hasError = true
			errorMsg = ev.Content
			s.updateSessionStatusOnTerminal(sessionID, ev, true, errorMsg)
			return
		}
	}

	record.Status = codingagent.StatusError
	record.Error = "client disconnected before completion"
	s.sessions.Update(record)
}

// handleRespond handles POST /api/v1/sessions/:id/respond.
func (s *Server) handleRespond(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/sessions/"), "/")
	if len(parts) < 2 || parts[1] != "respond" {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	sessionID := parts[0]

	exec, ok := s.execRegistry.Get(sessionID)
	if !ok {
		http.Error(w, "no active execution", http.StatusConflict)
		return
	}

	record, err := s.sessions.Get(sessionID)
	if err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	if record.Status != codingagent.StatusSuspended && exec.status != codingagent.StatusSuspended {
		http.Error(w, "session is not suspended", http.StatusConflict)
		return
	}

	var req RespondRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Content == "" {
		http.Error(w, "content is required", http.StatusBadRequest)
		return
	}

	if exec.stdin == nil {
		http.Error(w, "stdin not available for this agent", http.StatusInternalServerError)
		return
	}
	if err := exec.stdin.WriteStdin(req.Content); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	record.Status = codingagent.StatusActive
	s.sessions.Update(record)
	s.execRegistry.SetStatus(sessionID, codingagent.StatusActive)
	exec.status = codingagent.StatusActive

	if !strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		http.Error(w, "Accept: text/event-stream required", http.StatusNotAcceptable)
		return
	}

	_, _ = s.streamSSERelay(r.Context(), w, exec, false)

	exec.agentSess.Close()
	s.UnregisterActiveSession(sessionID)
	s.UnregisterExecCancel(sessionID)
	s.execRegistry.Unregister(sessionID)
}

// streamSSERelay streams events from a relay. Returns next offset and whether execution suspended.
func (s *Server) streamSSERelay(ctx context.Context, w http.ResponseWriter, exec *activeExecution, stopOnUserInput bool) (int, bool) {
	sessionID := exec.sessionID
	if s.logger != nil {
		s.logger.Debug("starting SSE stream", "session_id", sessionID)
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return exec.streamOffset, false
	}

	ch := exec.relay.stream(exec.streamOffset, stopOnUserInput)
	eventCount := 0
	var hasError bool
	var errorMsg string
	suspended := false

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
			s.finalizeSessionStatusOnDisconnect(sessionID, exec)
			return exec.streamOffset, suspended
		case <-ticker.C:
			fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
		case ev, ok := <-ch:
			if !ok {
				goto done
			}
			eventCount++
			exec.streamOffset++
			if ev.Type == codingagent.EventError {
				hasError = true
				errorMsg = ev.Content
			}
			if ev.Type == codingagent.EventUserInputRequired {
				suspended = true
				if rec, err := s.sessions.Get(sessionID); err == nil {
					rec.Status = codingagent.StatusSuspended
					s.sessions.Update(rec)
				}
				s.execRegistry.SetStatus(sessionID, codingagent.StatusSuspended)
				exec.status = codingagent.StatusSuspended
			}

			if err := s.writeSSEWireEvents(w, flusher, ev); err != nil {
				if s.logger != nil {
					s.logger.Warn("failed to write SSE wire events", "session_id", sessionID, "error", err.Error())
				}
				return exec.streamOffset, suspended
			}

			if s.taskLog != nil {
				s.taskLog.Add(toAgentLogEntry(ev, sessionID))
			}

			if ev.Type == codingagent.EventSystem && ev.SessionID != "" {
				if record, err := s.sessions.Get(sessionID); err == nil {
					record.AgentSessionID = ev.SessionID
					s.sessions.Update(record)
				}
			}

			s.updateSessionStatusOnTerminal(sessionID, ev, hasError, errorMsg)

			if suspended && stopOnUserInput {
				fmt.Fprintf(w, "data: [DONE]\n\n")
				flusher.Flush()
				return exec.streamOffset, true
			}
		}
	}
done:
	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()

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
	return exec.streamOffset, suspended
}

func (s *Server) respondJSONRelay(ctx context.Context, w http.ResponseWriter, exec *activeExecution, stopOnUserInput bool) bool {
	sessionID := exec.sessionID
	ch := exec.relay.stream(exec.streamOffset, stopOnUserInput)
	var events []codingagent.StreamEvent
	var hasError bool
	var errorMsg string
	suspended := false

	for {
		select {
		case <-ctx.Done():
			return suspended
		case ev, ok := <-ch:
			if !ok {
				goto done
			}
			exec.streamOffset++
			events = append(events, ev)
			if ev.Type == codingagent.EventError {
				hasError = true
				errorMsg = ev.Content
			}
			if ev.Type == codingagent.EventUserInputRequired {
				suspended = true
				if rec, err := s.sessions.Get(sessionID); err == nil {
					rec.Status = codingagent.StatusSuspended
					s.sessions.Update(rec)
				}
				s.execRegistry.SetStatus(sessionID, codingagent.StatusSuspended)
				exec.status = codingagent.StatusSuspended
				if stopOnUserInput {
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(events)
					return true
				}
			}
			s.updateSessionStatusOnTerminal(sessionID, ev, hasError, errorMsg)
			if s.taskLog != nil {
				s.taskLog.Add(toAgentLogEntry(ev, sessionID))
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
			}
		} else {
			record.Status = codingagent.StatusCompleted
		}
		s.sessions.Update(record)
	}
	return suspended
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

			if err := s.writeSSEWireEvents(w, flusher, ev); err != nil {
				if s.logger != nil {
					s.logger.Warn("failed to write SSE wire events", "session_id", sessionID, "error", err.Error())
				}
				return
			}

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

			s.updateSessionStatusOnTerminal(sessionID, ev, hasError, errorMsg)
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

	// Mark session as closed in the artifact store.
	if s.artifactStore != nil {
		_ = s.artifactStore.CloseSession(r.Context(), sessionID)
	}

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
