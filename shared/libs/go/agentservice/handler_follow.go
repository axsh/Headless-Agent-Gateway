package agentservice

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

func parseFollowFrom(r *http.Request, bufLen int) (start int, err error) {
	raw := r.URL.Query().Get("from")
	if raw == "" {
		raw = r.Header.Get("Last-Event-ID")
	}
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("invalid from")
	}
	start = n + 1
	if start > bufLen {
		return 0, fmt.Errorf("from exceeds buffer")
	}
	return start, nil
}

func (s *Server) handleFollow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sessionID := extractPathParam(r.URL.Path, "/api/v1/sessions/")
	if s.logger != nil {
		s.logger.Debug("follow session events", "session_id", sessionID)
	}
	if _, err := s.sessions.Get(sessionID); err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	if !strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		http.Error(w, "Accept: text/event-stream required", http.StatusNotAcceptable)
		return
	}
	exec, ok := s.execRegistry.Get(sessionID)
	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]any{"error": "no active turn"})
		return
	}
	bufLen := 0
	if exec.relay != nil {
		bufLen = exec.relay.eventCount()
	}
	from, err := parseFollowFrom(r, bufLen)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	term, _, _ := s.attachSSE(r.Context(), w, exec, from, true)
	if term.kind == codingagent.EventResult || term.kind == codingagent.EventError {
		s.writeSSEDone(w)
	}
}
