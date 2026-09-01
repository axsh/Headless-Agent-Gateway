package agentservice

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

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
	if rec.EndedAt.IsZero() {
		rec.EndedAt = time.Now().UTC()
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
	q, err := parseUsageQuery(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !q.Empty() {
		rep.Turns = codingagent.FilterTurnUsage(rep.Turns, q)
		rep.Usage = codingagent.SumTurnUsage(rep.Turns)
		if s.logger != nil {
			s.logger.Debug("filtered session usage",
				"session_id", sessionID,
				"last_n", q.LastN,
				"turns", len(rep.Turns),
			)
		}
	} else if record.Usage != nil {
		rep.Usage = *record.Usage
	} else {
		rep.Usage = codingagent.SumTurnUsage(rep.Turns)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(rep)
}

func parseUsageQuery(vals url.Values) (codingagent.UsageQuery, error) {
	var q codingagent.UsageQuery
	if v := vals.Get("last_n"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return q, errInvalidLastN
		}
		q.LastN = n
	}
	q.AfterTurnID = vals.Get("after_turn_id")
	q.FromTurnID = vals.Get("from_turn_id")
	q.ToTurnID = vals.Get("to_turn_id")
	if v := vals.Get("since"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return q, err
		}
		q.Since = t
	}
	if v := vals.Get("until"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return q, err
		}
		q.Until = t
	}
	return q, nil
}

var errInvalidLastN = errors.New("invalid last_n")

