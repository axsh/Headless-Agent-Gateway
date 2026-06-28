package agentservice

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

// MultimodalSupporter is an optional interface that agents can implement
// to declare whether they support non-text content (e.g., images).
type MultimodalSupporter interface {
	SupportsMultimodal() bool
}

// SendMessageV2Request is the request body for POST /api/v2/sessions/:id/messages.
type SendMessageV2Request struct {
	Content []codingagent.ContentPart `json:"content"`
}

// handleSendMessageV2 handles POST /api/v2/sessions/:id/messages.
// Accepts {"content": []ContentPart} with multimodal support.
func (s *Server) handleSendMessageV2(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v2/sessions/"), "/")
	if len(parts) < 2 {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	sessionID := parts[0]

	if s.logger != nil {
		s.logger.Debug("v2 send message", "session_id", sessionID)
	}

	record, err := s.sessions.Get(sessionID)
	if err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	var req SendMessageV2Request
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
		// Determine base directory for temp file storage.
		baseDir := record.WorkDir
		if baseDir == "" {
			baseDir, _ = os.Getwd()
		}
		promptText, savedFiles, err = BuildMultimodalPrompt(baseDir, sessionID, req.Content)
		if err != nil {
			if s.logger != nil {
				s.logger.Error("failed to build multimodal prompt", "error", err.Error(), "session_id", sessionID)
			}
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if s.logger != nil {
			s.logger.Debug("multimodal prompt built", "session_id", sessionID, "saved_files", len(savedFiles))
		}
	} else {
		promptText = codingagent.ExtractText(req.Content)
	}

	if s.logger != nil {
		s.logger.Trace("v2 message content", "prompt", promptText)
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

// routeSessionByIDV2 routes v2 session-level requests.
// Currently only POST /api/v2/sessions/:id/messages is supported.
func (s *Server) routeSessionByIDV2(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if strings.HasSuffix(path, "/messages") {
		s.handleSendMessageV2(w, r)
	} else {
		// v2 only supports message sending; other operations use v1.
		http.Error(w, "use /api/v1/ for this operation", http.StatusNotFound)
	}
}
