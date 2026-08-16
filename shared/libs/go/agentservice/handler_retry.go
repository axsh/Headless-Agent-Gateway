package agentservice

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

const defaultSSEClientDrainTimeout = 15 * time.Second

type streamTerminal struct {
	kind      codingagent.EventType
	retryable bool
	content   string
}

func (s *Server) processRetryLimits(agentName string) (maxAttempts int, interval time.Duration) {
	if agentName != "codex" {
		return 1, 0
	}
	maxAttempts = s.processRetry.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	if s.processRetry.IntervalSeconds > 0 {
		interval = time.Duration(s.processRetry.IntervalSeconds) * time.Second
	}
	return maxAttempts, interval
}

func (s *Server) clientDrainTimeout() time.Duration {
	if s.sseDrainTimeout > 0 {
		return s.sseDrainTimeout
	}
	return defaultSSEClientDrainTimeout
}

func (s *Server) classifiedTerminal(content string) (tagged string, overloaded bool) {
	overloaded = codingagent.IsRetryableUpstream(content)
	return codingagent.ClassifiedErrorContent(content, overloaded), overloaded
}

func (s *Server) stopExecOnDrainTimeout(sessionID string, exec *activeExecution) streamTerminal {
	if s.logger != nil {
		s.logger.Warn("SSE drain timed out; stopping agent process",
			"session_id", sessionID,
			"timeout", s.clientDrainTimeout().String())
	}
	if exec != nil && exec.agentSess != nil {
		_ = exec.agentSess.Close()
	}
	s.execRegistry.Unregister(sessionID)
	s.UnregisterActiveSession(sessionID)
	s.UnregisterExecCancel(sessionID)
	return streamTerminal{
		kind:      codingagent.EventError,
		retryable: false,
		content:   "client drain timeout",
	}
}

func (s *Server) sessionOptsWithResume(base []codingagent.SessionOption, sessionID, fallback string) []codingagent.SessionOption {
	opts := append([]codingagent.SessionOption{}, base...)
	resume := fallback
	if rec, err := s.sessions.Get(sessionID); err == nil && rec.AgentSessionID != "" {
		resume = rec.AgentSessionID
	}
	if resume != "" {
		opts = append(opts, codingagent.WithAgentSessionID(resume))
	}
	return opts
}

func (s *Server) closeAttempt(sessionID string, agentSess codingagent.Session) {
	if agentSess != nil {
		_ = agentSess.Close()
	}
	s.UnregisterActiveSession(sessionID)
}

func (s *Server) writeSSEDone(w http.ResponseWriter) {
	fmt.Fprintf(w, "data: [DONE]\n\n")
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (s *Server) runTurn(
	r *http.Request,
	w http.ResponseWriter,
	execCtx context.Context,
	execCancel func(),
	record *codingagent.SessionRecord,
	sessionID, turnID, correlationID, promptText, rawUserPrompt, fallbackResume string,
	baseOpts []codingagent.SessionOption,
	savedFiles []string,
) {
	agent := s.agents[record.AgentName]
	maxAttempts, interval := s.processRetryLimits(record.AgentName)
	wantSSE := strings.Contains(r.Header.Get("Accept"), "text/event-stream")

	var agentSess codingagent.Session
	var active *activeExecution
	registered := false
	healFresh := false

	finish := func() {
		s.finishActiveExecution(sessionID, agentSess, savedFiles)
	}

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 && s.logger != nil {
			s.logger.Warn("retrying coding agent process",
				"session_id", sessionID,
				"agent", record.AgentName,
				"attempt", attempt,
				"max_attempts", maxAttempts)
		}
		resumeFallback := fallbackResume
		if healFresh {
			resumeFallback = ""
		}
		opts := s.sessionOptsWithResume(baseOpts, sessionID, resumeFallback)
		attemptPrompt := promptText
		if healFresh {
			rec := record
			if latest, err := s.sessions.Get(sessionID); err == nil {
				rec = latest
			}
			wrapped, err := s.wrapPromptForSelfHeal(execCtx, rec, rawUserPrompt)
			if err != nil {
				if s.logger != nil {
					s.logger.Warn("self-heal prompt wrap failed; sending raw user prompt",
						"session_id", sessionID, "error", err.Error())
				}
				attemptPrompt = rawUserPrompt
			} else {
				attemptPrompt = wrapped
			}
			opts = append(opts, codingagent.WithPrompt(attemptPrompt))
			if s.logger != nil {
				s.logger.Debug("self-heal fresh exec without native resume",
					"session_id", sessionID, "attempt", attempt)
			}
		}
		sess, err := agent.CreateSession(execCtx, opts...)
		if err != nil {
			if s.logger != nil {
				s.logger.Error("failed to create agent session", "session_id", sessionID, "error", err.Error(), "attempt", attempt)
			}
			if !registered {
				execCancel()
				s.UnregisterExecCancel(sessionID)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if wantSSE {
				s.emitClassifiedSSE(w, active, codingagent.ClassifiedErrorContent(err.Error(), false), false)
			} else {
				writeJSONEvents(w, []codingagent.StreamEvent{{
					Type:    codingagent.EventError,
					Content: err.Error(),
				}})
			}
			finish()
			if wantSSE {
				s.writeSSEDone(w)
			}
			return
		}
		agentSess = sess
		s.RegisterActiveSession(sessionID, sess)
		ch, err := sess.Send(execCtx, attemptPrompt)
		if err != nil {
			if s.logger != nil {
				s.logger.Error("agent send failed", "session_id", sessionID, "error", err.Error(), "attempt", attempt)
			}
			if !registered {
				finish()
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			s.closeAttempt(sessionID, sess)
			if attempt < maxAttempts {
				s.clearPersistedAgentSessionID(sessionID)
				healFresh = true
				continue
			}
			finish()
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		relay := newEventRelay(ch)
		stdin, _ := sess.(codingagent.StdinWriter)
		if !registered {
			active = &activeExecution{
				sessionID:     sessionID,
				turnID:        turnID,
				correlationID: correlationID,
				agentSess:     sess,
				stdin:         stdin,
				relay:         relay,
				status:        codingagent.StatusActive,
			}
			if err := s.execRegistry.Register(sessionID, active); err != nil {
				finish()
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusConflict)
				json.NewEncoder(w).Encode(map[string]any{
					"error":  "session busy",
					"status": codingagent.StatusActive,
					"hint":   "respond or terminate",
				})
				return
			}
			registered = true
		} else {
			active.agentSess = sess
			active.stdin = stdin
			active.relay = relay
			active.streamOffset = 0
		}

		if wantSSE {
			term, suspended := s.streamSSERelay(r.Context(), w, active, true)
			if suspended {
				return
			}
			if term.kind == codingagent.EventResult {
				s.finishActiveExecution(sessionID, agentSess, savedFiles)
				if s.logger != nil {
					s.logger.Debug("unregistered before done", "session_id", sessionID, "suspended", false)
				}
				s.writeSSEDone(w)
				return
			}
			if term.retryable && attempt < maxAttempts {
				if s.logger != nil {
					s.logger.Debug("swallowing retryable process error",
						"session_id", sessionID, "attempt", attempt, "content", term.content)
				}
				s.closeAttempt(sessionID, sess)
				agentSess = nil
				s.clearPersistedAgentSessionID(sessionID)
				healFresh = true
				select {
				case <-time.After(interval):
				case <-execCtx.Done():
					s.finishActiveExecution(sessionID, nil, savedFiles)
					s.writeSSEDone(w)
					return
				}
				continue
			}
			if term.retryable {
				content, overloaded := s.classifiedTerminal(term.content)
				s.emitClassifiedSSE(w, active, content, overloaded)
			}
			s.finishActiveExecution(sessionID, agentSess, savedFiles)
			if r.Context().Err() == nil {
				s.writeSSEDone(w)
			}
			return
		}

		term, events, suspended := s.respondJSONRelay(r.Context(), w, active, true)
		if suspended {
			return
		}
		if term.kind == codingagent.EventResult {
			writeJSONEvents(w, events)
			s.finishActiveExecution(sessionID, agentSess, savedFiles)
			return
		}
		if term.retryable && attempt < maxAttempts {
			s.closeAttempt(sessionID, sess)
			agentSess = nil
			s.clearPersistedAgentSessionID(sessionID)
			healFresh = true
			select {
			case <-time.After(interval):
			case <-execCtx.Done():
				s.finishActiveExecution(sessionID, nil, savedFiles)
				return
			}
			continue
		}
		if term.retryable {
			content, _ := s.classifiedTerminal(term.content)
			events = append(events, codingagent.StreamEvent{Type: codingagent.EventError, Content: content})
			s.updateSessionStatusOnTerminal(sessionID, codingagent.StreamEvent{Type: codingagent.EventError, Content: content}, true, content)
		}
		writeJSONEvents(w, events)
		s.finishActiveExecution(sessionID, agentSess, savedFiles)
		return
	}
}

func (s *Server) emitClassifiedSSE(w http.ResponseWriter, exec *activeExecution, content string, retryable bool) {
	ev := codingagent.StreamEvent{Type: codingagent.EventError, Content: content, Retryable: retryable}
	if exec != nil {
		ev.TurnID = exec.turnID
		ev.CorrelationID = exec.correlationID
	}
	if flusher, ok := w.(http.Flusher); ok {
		_ = s.writeSSEWireEvents(w, flusher, ev)
	}
	if exec != nil {
		s.updateSessionStatusOnTerminal(exec.sessionID, ev, true, content)
	}
}

func writeJSONEvents(w http.ResponseWriter, events []codingagent.StreamEvent) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(events)
}

func (s *Server) handleRelaySideEffects(sessionID string, exec *activeExecution, ev codingagent.StreamEvent, writeSSE bool, w http.ResponseWriter, flusher http.Flusher) (suspended bool, writeErr error) {
	if ev.Type == codingagent.EventUserInputRequired {
		suspended = true
		if rec, err := s.sessions.Get(sessionID); err == nil {
			rec.Status = codingagent.StatusSuspended
			s.sessions.Update(rec)
		}
		s.execRegistry.SetStatus(sessionID, codingagent.StatusSuspended)
		exec.status = codingagent.StatusSuspended
	}
	if writeSSE && flusher != nil {
		if err := s.writeSSEWireEvents(w, flusher, ev); err != nil {
			return suspended, err
		}
	}
	if s.taskLog != nil {
		s.taskLog.Add(toAgentLogEntry(ev, sessionID, exec.turnID, exec.correlationID))
	}
	if ev.Type == codingagent.EventSystem && ev.SessionID != "" {
		if record, err := s.sessions.Get(sessionID); err == nil {
			record.AgentSessionID = ev.SessionID
			s.sessions.Update(record)
			if s.logger != nil {
				s.logger.Debug("agent session ID extracted", "session_id", sessionID, "agent_session_id", ev.SessionID)
			}
		}
	}
	if ev.Type == codingagent.EventError && ev.Retryable {
		return suspended, nil
	}
	s.updateSessionStatusOnTerminal(sessionID, ev, ev.Type == codingagent.EventError, ev.Content)
	return suspended, nil
}

// streamSSERelay streams events from a relay. Retryable errors are not written to SSE.
func (s *Server) streamSSERelay(ctx context.Context, w http.ResponseWriter, exec *activeExecution, stopOnUserInput bool) (streamTerminal, bool) {
	sessionID := exec.sessionID
	if s.logger != nil {
		s.logger.Debug("starting SSE stream", "session_id", sessionID)
	}

	if !exec.sseStarted {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		exec.sseStarted = true
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return streamTerminal{}, false
	}

	ch := exec.relay.stream(exec.streamOffset, stopOnUserInput)

	if !exec.turnMetaSent {
		meta := codingagent.StreamEvent{
			Type:          codingagent.EventSystem,
			Content:       "turn context",
			TurnID:        exec.turnID,
			CorrelationID: exec.correlationID,
		}
		if err := s.writeSSEWireEvents(w, flusher, meta); err != nil {
			if s.logger != nil {
				s.logger.Warn("failed to write turn context", "session_id", sessionID, "error", err.Error())
			}
			return streamTerminal{}, false
		}
		if s.taskLog != nil {
			s.taskLog.Add(toAgentLogEntry(meta, sessionID, exec.turnID, exec.correlationID))
		}
		exec.turnMetaSent = true
	}

	eventCount := 0
	var term streamTerminal
	suspended := false
	clientGone := false

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	var drainTimer *time.Timer
	defer func() {
		if drainTimer != nil {
			drainTimer.Stop()
		}
	}()
	drainCh := func() <-chan time.Time {
		if drainTimer == nil {
			drainTimer = time.NewTimer(s.clientDrainTimeout())
		}
		return drainTimer.C
	}

	handleEvent := func(ev codingagent.StreamEvent) bool {
		ev.TurnID = exec.turnID
		ev.CorrelationID = exec.correlationID
		eventCount++
		exec.streamOffset++
		if ev.Type == codingagent.EventError && ev.Retryable {
			term = streamTerminal{kind: codingagent.EventError, retryable: true, content: ev.Content}
			if s.logger != nil {
				s.logger.Debug("retryable process error swallowed", "session_id", sessionID)
			}
			_, _ = s.handleRelaySideEffects(sessionID, exec, ev, false, w, flusher)
			return false
		}
		if ev.Type == codingagent.EventResult {
			term = streamTerminal{kind: codingagent.EventResult}
		}
		if ev.Type == codingagent.EventError {
			term = streamTerminal{kind: codingagent.EventError, retryable: false, content: ev.Content}
		}
		write := !clientGone
		sus, err := s.handleRelaySideEffects(sessionID, exec, ev, write, w, flusher)
		if sus {
			suspended = true
		}
		if err != nil {
			clientGone = true
			if s.logger != nil {
				s.logger.Warn("failed to write SSE wire events", "session_id", sessionID, "error", err.Error())
			}
		}
		return suspended && stopOnUserInput
	}

	for {
		if clientGone {
			select {
			case ev, ok := <-ch:
				if !ok {
					goto done
				}
				if handleEvent(ev) {
					return term, true
				}
			case <-drainCh():
				term = s.stopExecOnDrainTimeout(sessionID, exec)
				goto done
			}
			continue
		}
		select {
		case <-ctx.Done():
			if !clientGone {
				clientGone = true
				if s.logger != nil {
					s.logger.Warn("client disconnected during SSE stream",
						"session_id", sessionID,
						"events_sent", eventCount)
				}
			}
		case <-ticker.C:
			if !clientGone {
				fmt.Fprintf(w, ": keepalive\n\n")
				flusher.Flush()
			}
		case ev, ok := <-ch:
			if !ok {
				goto done
			}
			if handleEvent(ev) {
				fmt.Fprintf(w, "data: [DONE]\n\n")
				flusher.Flush()
				return term, true
			}
		}
	}
done:
	if record, err := s.sessions.Get(sessionID); err == nil {
		switch {
		case term.kind == codingagent.EventError && term.retryable:
			// Intermediate retryable failure: leave status unchanged.
		case term.kind == codingagent.EventError:
			record.Status = codingagent.StatusError
			if term.content != "" {
				record.Error = term.content
			} else {
				record.Error = "unknown error occurred during execution"
			}
			s.sessions.Update(record)
		default:
			record.Status = codingagent.StatusCompleted
			record.Error = ""
			s.sessions.Update(record)
		}
		if term.kind != codingagent.EventError || !term.retryable {
			s.reconcileSessionArtifacts(context.Background(), sessionID, exec.turnID, exec.correlationID)
		}
	}
	return term, suspended
}

func (s *Server) respondJSONRelay(ctx context.Context, w http.ResponseWriter, exec *activeExecution, stopOnUserInput bool) (streamTerminal, []codingagent.StreamEvent, bool) {
	sessionID := exec.sessionID
	ch := exec.relay.stream(exec.streamOffset, stopOnUserInput)
	var events []codingagent.StreamEvent
	var term streamTerminal
	suspended := false
	clientGone := false
	var drainTimer *time.Timer
	defer func() {
		if drainTimer != nil {
			drainTimer.Stop()
		}
	}()
	drainCh := func() <-chan time.Time {
		if drainTimer == nil {
			drainTimer = time.NewTimer(s.clientDrainTimeout())
		}
		return drainTimer.C
	}

	for {
		if clientGone {
			select {
			case ev, ok := <-ch:
				if !ok {
					goto done
				}
				ev.TurnID = exec.turnID
				ev.CorrelationID = exec.correlationID
				exec.streamOffset++
				if ev.Type == codingagent.EventError && ev.Retryable {
					term = streamTerminal{kind: codingagent.EventError, retryable: true, content: ev.Content}
					continue
				}
				if ev.Type == codingagent.EventResult {
					term = streamTerminal{kind: codingagent.EventResult}
				}
				if ev.Type == codingagent.EventError {
					term = streamTerminal{kind: codingagent.EventError, content: ev.Content}
				}
				events = append(events, ev)
				s.updateSessionStatusOnTerminal(sessionID, ev, ev.Type == codingagent.EventError, ev.Content)
			case <-drainCh():
				term = s.stopExecOnDrainTimeout(sessionID, exec)
				goto done
			}
			continue
		}
		select {
		case <-ctx.Done():
			clientGone = true
			if s.logger != nil {
				s.logger.Debug("client disconnected, draining JSON relay", "session_id", sessionID)
			}
		case ev, ok := <-ch:
			if !ok {
				goto done
			}
			ev.TurnID = exec.turnID
			ev.CorrelationID = exec.correlationID
			exec.streamOffset++
			if ev.Type == codingagent.EventError && ev.Retryable {
				term = streamTerminal{kind: codingagent.EventError, retryable: true, content: ev.Content}
				continue
			}
			events = append(events, ev)
			if ev.Type == codingagent.EventResult {
				term = streamTerminal{kind: codingagent.EventResult}
			}
			if ev.Type == codingagent.EventError {
				term = streamTerminal{kind: codingagent.EventError, content: ev.Content}
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
					writeJSONEvents(w, events)
					return term, events, true
				}
			}
			s.updateSessionStatusOnTerminal(sessionID, ev, ev.Type == codingagent.EventError, ev.Content)
			if s.taskLog != nil {
				s.taskLog.Add(toAgentLogEntry(ev, sessionID, exec.turnID, exec.correlationID))
			}
			if ev.Type == codingagent.EventSystem && ev.SessionID != "" {
				if record, err := s.sessions.Get(sessionID); err == nil {
					record.AgentSessionID = ev.SessionID
					s.sessions.Update(record)
				}
			}
		}
	}
done:
	if record, err := s.sessions.Get(sessionID); err == nil {
		if term.kind == codingagent.EventError && !term.retryable {
			record.Status = codingagent.StatusError
			record.Error = term.content
			s.sessions.Update(record)
		} else if term.kind == codingagent.EventResult || term.kind == "" {
			if term.kind == codingagent.EventResult {
				record.Status = codingagent.StatusCompleted
				record.Error = ""
				s.sessions.Update(record)
			}
		}
		if term.kind != codingagent.EventError || !term.retryable {
			s.reconcileSessionArtifacts(context.Background(), sessionID, exec.turnID, exec.correlationID)
		}
	}
	return term, events, suspended
}
