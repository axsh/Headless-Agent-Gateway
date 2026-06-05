package agentservice

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// handleLogStream handles GET /api/v1/sessions/:id/logs.
// Streams session logs via SSE (Server-Sent Events).
func (s *Server) handleLogStream(w http.ResponseWriter, r *http.Request) {
	// Extract session ID from /api/v1/sessions/{id}/logs
	sessionID := extractPathParam(r.URL.Path, "/api/v1/sessions/")

	record, err := s.sessions.Get(sessionID)
	if err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Send snapshot of existing logs
	if s.taskLog != nil {
		entries := s.taskLog.Entries()
		for _, entry := range entries {
			data, _ := json.Marshal(entry)
			fmt.Fprintf(w, "event: log\ndata: %s\n\n", data)
		}
		flusher.Flush()
	}

	// If session is already terminal, emit status and finish
	if isTerminalStatus(record.Status) {
		emitTerminationStatus(w, flusher, record.Status)
		return
	}

	// Poll for new logs (500ms interval)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	lastIndex := 0
	if s.taskLog != nil {
		lastIndex = len(s.taskLog.Entries())
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if s.taskLog != nil {
				entries := s.taskLog.Entries()
				if len(entries) > lastIndex {
					for _, entry := range entries[lastIndex:] {
						data, _ := json.Marshal(entry)
						fmt.Fprintf(w, "event: log\ndata: %s\n\n", data)
					}
					lastIndex = len(entries)
					flusher.Flush()
				}
			}

			// Check session state for termination
			rec, err := s.sessions.Get(sessionID)
			if err == nil && isTerminalStatus(rec.Status) {
				emitTerminationStatus(w, flusher, rec.Status)
				return
			}
		}
	}
}

// emitTerminationStatus sends the final status event and [DONE] marker.
func emitTerminationStatus(w http.ResponseWriter, flusher http.Flusher, status string) {
	sseStatus := "terminated"
	if status == "error" {
		sseStatus = "failed"
	}
	fmt.Fprintf(w, "event: status\ndata: {\"status\":\"%s\"}\n\n", sseStatus)
	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}
