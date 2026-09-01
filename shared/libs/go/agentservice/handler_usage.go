package agentservice

import (
	"encoding/json"
	"net/http"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

// applyUsageSideEffects observes stream usage, finalizes on terminal events,
// enriches EventResult.Usage for the wire, and persists turn/session aggregates.
func (s *Server) applyUsageSideEffects(sessionID string, exec *activeExecution, ev *codingagent.StreamEvent) {
	if exec == nil || ev == nil {
		return
	}
	if exec.usageAgg == nil {
		exec.usageAgg = newTurnUsageAggregator(exec.turnID)
	}
	exec.usageAgg.Observe(*ev)
	if ev.Type != codingagent.EventResult && !(ev.Type == codingagent.EventError && !ev.Retryable) {
		return
	}
	if s.usageMeter != nil {
		exec.usageAgg.MergeCalls(s.usageMeter.Take(sessionID, exec.turnID))
	}
	rec, ok := exec.usageAgg.Finalize()
	if !ok {
		if ev.Type == codingagent.EventResult && s.logger != nil {
			s.logger.Warn("token usage missing for turn", "session_id", sessionID, "turn_id", exec.turnID)
		}
		return
	}
	if ev.Type == codingagent.EventResult {
		u := rec.Usage
		ev.Usage = &u
	}
	s.persistTurnUsage(sessionID, rec)
}

func (s *Server) persistTurnUsage(sessionID string, rec codingagent.TurnUsageRecord) {
	record, err := s.sessions.Get(sessionID)
	if err != nil || record == nil {
		return
	}
	sum, err := appendTurnUsage(record.SessionDir, sessionID, rec)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("failed to persist turn usage", "session_id", sessionID, "turn_id", rec.TurnID, "error", err.Error())
		}
		return
	}
	record.Usage = sum
	if err := s.sessions.Update(record); err != nil && s.logger != nil {
		s.logger.Warn("failed to update session usage", "session_id", sessionID, "error", err.Error())
	}
}

func (s *Server) handleGetSessionUsage(w http.ResponseWriter, r *http.Request) {
	sessionID := extractPathParam(r.URL.Path, "/api/v1/sessions/")
	if sessionID == "" {
		http.Error(w, "session id required", http.StatusBadRequest)
		return
	}
	record, err := s.sessions.Get(sessionID)
	if err != nil || record == nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	rep, err := loadUsageReport(record.SessionDir, sessionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if record.Usage != nil {
		rep.Usage = *record.Usage
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(rep)
}

