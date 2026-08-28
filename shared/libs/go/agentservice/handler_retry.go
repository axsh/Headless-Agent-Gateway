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

const (
	defaultSSEClientDrainTimeout  = 90 * time.Second
	maxLoggedStderrBytes          = 8 * 1024
	logCodexProcessRetryExhausted = "codex process retry exhausted"
	logClientDisconnectedSSE      = "client disconnected during SSE stream"
	logSSEDrainTimedOut           = "SSE drain timed out; stopping agent process"
	drainTimeoutTerminalContent   = "client drain timeout"
	streamEndedWithoutTerminalContent = "stream ended without terminal event"
)

type streamTerminal struct {
	kind      codingagent.EventType
	retryable bool
	content   string
}

func resolveTerminalFromRelayEvents(events []codingagent.StreamEvent) streamTerminal {
	for _, ev := range events {
		if ev.Type == codingagent.EventResult {
			return streamTerminal{kind: codingagent.EventResult}
		}
	}
	for i := len(events) - 1; i >= 0; i-- {
		ev := events[i]
		if ev.Type == codingagent.EventError && !ev.Retryable {
			return streamTerminal{kind: codingagent.EventError, content: ev.Content}
		}
	}
	for i := len(events) - 1; i >= 0; i-- {
		ev := events[i]
		if ev.Type == codingagent.EventError && ev.Retryable {
			return streamTerminal{kind: codingagent.EventError, retryable: true, content: ev.Content}
		}
	}
	return streamTerminal{kind: codingagent.EventError, content: streamEndedWithoutTerminalContent}
}

func ensureRelayTerminal(term streamTerminal, relay *eventRelay) streamTerminal {
	if term.kind == codingagent.EventResult {
		return term
	}
	if term.kind == codingagent.EventError && !term.retryable {
		return term
	}
	resolved := resolveTerminalFromRelayEvents(nil)
	if relay != nil {
		resolved = resolveTerminalFromRelayEvents(relay.snapshot())
	}
	if resolved.kind == codingagent.EventResult {
		return resolved
	}
	if resolved.kind == codingagent.EventError && !resolved.retryable {
		return resolved
	}
	if term.kind == codingagent.EventError && term.retryable {
		return term
	}
	return resolved
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
		s.logger.Warn(logSSEDrainTimedOut,
			"session_id", sessionID,
			"timeout", s.clientDrainTimeout().String())
	}
	if exec != nil {
		exec.subMu.Lock()
		if exec.reattachTimer != nil {
			exec.reattachTimer.Stop()
			exec.reattachTimer = nil
		}
		exec.subMu.Unlock()
		if exec.agentSess != nil {
			_ = exec.agentSess.Close()
		}
	}
	s.execRegistry.Unregister(sessionID)
	s.UnregisterActiveSession(sessionID)
	s.UnregisterExecCancel(sessionID)
	return streamTerminal{
		kind:      codingagent.EventError,
		retryable: false,
		content:   drainTimeoutTerminalContent,
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
				writeSessionBusy(w, codingagent.StatusActive)
				return
			}
			registered = true
			if wantSSE {
				s.startSideEffectPump(active)
			}
		} else {
			active.agentSess = sess
			active.stdin = stdin
			active.relay = relay
			active.streamOffset = 0
			active.sideEffectOffset = 0
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
				s.logProcessRetryExhausted(sessionID, attempt, maxAttempts, healFresh, term)
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
			s.logProcessRetryExhausted(sessionID, attempt, maxAttempts, healFresh, term)
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
	// Skip TaskLog when an SSE subscriber is attached: attachSSE already Adds
	// synchronously. Pump still Adds after detach (drain / no subscriber).
	if s.taskLog != nil && !exec.hasSubscriber() {
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

func (s *Server) startSideEffectPump(exec *activeExecution) {
	go s.pumpExecSideEffects(exec)
}

func (s *Server) pumpExecSideEffects(exec *activeExecution) {
	sessionID := exec.sessionID
	for {
		if _, ok := s.execRegistry.Get(sessionID); !ok {
			return
		}
		relay := exec.relay
		if relay == nil {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		ch := relay.stream(exec.sideEffectOffset, false)
		for ev := range ch {
			if exec.relay != relay {
				break
			}
			ev.TurnID = exec.turnID
			ev.CorrelationID = exec.correlationID
			_, _ = s.handleRelaySideEffects(sessionID, exec, ev, false, nil, nil)
			exec.sideEffectOffset++
		}
		if exec.relay != relay {
			exec.sideEffectOffset = 0
			continue
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (s *Server) armReattachTimer(exec *activeExecution) {
	exec.subMu.Lock()
	defer exec.subMu.Unlock()
	if exec.subCancel != nil || exec.status == codingagent.StatusSuspended {
		return
	}
	if exec.reattachTimer != nil {
		return
	}
	sessionID := exec.sessionID
	d := s.clientDrainTimeout()
	if s.logger != nil {
		s.logger.Debug("arming SSE reattach timer", "session_id", sessionID, "timeout", d.String())
	}
	exec.reattachTimer = time.AfterFunc(d, func() {
		s.stopExecOnDrainTimeout(sessionID, exec)
	})
}

func (s *Server) waitDetached(ctx context.Context, exec *activeExecution) (streamTerminal, bool) {
	sessionID := exec.sessionID
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		if !exec.hasSubscriber() {
			s.armReattachTimer(exec)
		}
		if _, ok := s.execRegistry.Get(sessionID); !ok {
			return streamTerminal{kind: codingagent.EventError, content: drainTimeoutTerminalContent}, false
		}
		rec, err := s.sessions.Get(sessionID)
		if err == nil {
			switch rec.Status {
			case codingagent.StatusCompleted:
				return streamTerminal{kind: codingagent.EventResult}, false
			case codingagent.StatusError:
				return streamTerminal{kind: codingagent.EventError, content: rec.Error}, false
			case codingagent.StatusSuspended:
				return streamTerminal{}, true
			}
		}
		select {
		case <-ctx.Done():
			return streamTerminal{}, false
		case <-ticker.C:
		}
	}
}

func (s *Server) attachSSE(ctx context.Context, w http.ResponseWriter, exec *activeExecution, from int, stopOnUserInput bool) (term streamTerminal, suspended bool, detached bool) {
	sessionID := exec.sessionID
	if s.logger != nil {
		s.logger.Debug("attaching SSE subscriber", "session_id", sessionID, "from", from)
	}
	gen, subCtx := exec.stealSubscriber()
	defer func() {
		if exec.clearSubscriber(gen) {
			s.armReattachTimer(exec)
		}
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return streamTerminal{}, false, false
	}

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
		return streamTerminal{}, false, true
	}

	ch := exec.relay.stream(from, stopOnUserInput)
	idx := from
	eventCount := 0
	terminalWritten := false
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	writeEvent := func(ev codingagent.StreamEvent) bool {
		ev.TurnID = exec.turnID
		ev.CorrelationID = exec.correlationID
		logicalID := idx
		idx++
		exec.streamOffset = idx
		eventCount++
		if ev.Type == codingagent.EventError && ev.Retryable {
			term = streamTerminal{kind: codingagent.EventError, retryable: true, content: ev.Content}
			return false
		}
		if ev.Type == codingagent.EventResult {
			term = streamTerminal{kind: codingagent.EventResult}
		}
		if ev.Type == codingagent.EventError {
			term = streamTerminal{kind: codingagent.EventError, retryable: false, content: ev.Content}
		}
		if ev.Type == codingagent.EventUserInputRequired {
			suspended = true
		}
		id := logicalID
		if err := s.writeSSEWireEventsID(w, flusher, ev, &id); err != nil {
			if s.logger != nil {
				s.logger.Warn("failed to write SSE wire events", "session_id", sessionID, "error", err.Error())
			}
			detached = true
			return true
		}
		// Synchronously record TaskLog while the SSE subscriber is attached so
		// ToolCallAnalyzer runs before finishActiveExecution unregisters the pump.
		if s.taskLog != nil {
			s.taskLog.Add(toAgentLogEntry(ev, sessionID, exec.turnID, exec.correlationID))
		}
		if ev.Type == codingagent.EventResult || (ev.Type == codingagent.EventError && !ev.Retryable) {
			terminalWritten = true
		}
		return suspended && stopOnUserInput
	}

	writeSyntheticTerminal := func() {
		if terminalWritten || term.kind == "" {
			return
		}
		if term.kind == codingagent.EventError && term.retryable {
			return
		}
		ev := codingagent.StreamEvent{
			Type:    term.kind,
			Content: term.content,
			TurnID:  exec.turnID,
			CorrelationID: exec.correlationID,
		}
		if term.kind == codingagent.EventError {
			ev.Retryable = term.retryable
		}
		logicalID := idx
		idx++
		exec.streamOffset = idx
		eventCount++
		if err := s.writeSSEWireEventsID(w, flusher, ev, &logicalID); err != nil {
			if s.logger != nil {
				s.logger.Warn("failed to write synthetic terminal SSE", "session_id", sessionID, "error", err.Error())
			}
			detached = true
			return
		}
		terminalWritten = true
		s.updateSessionStatusOnTerminal(sessionID, ev, ev.Type == codingagent.EventError, ev.Content)
	}

	for {
		select {
		case <-ctx.Done():
			if s.logger != nil {
				s.logger.Warn(logClientDisconnectedSSE,
					"session_id", sessionID,
					"events_sent", eventCount)
			}
			return term, suspended, true
		case <-subCtx.Done():
			if s.logger != nil {
				s.logger.Debug("SSE subscriber stolen", "session_id", sessionID)
			}
			return term, suspended, true
		case <-ticker.C:
			fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
		case ev, ok := <-ch:
			if !ok {
				term = ensureRelayTerminal(term, exec.relay)
				writeSyntheticTerminal()
				return term, suspended, false
			}
			if writeEvent(ev) {
				if detached {
					return term, suspended, true
				}
				fmt.Fprintf(w, "data: [DONE]\n\n")
				flusher.Flush()
				return term, true, false
			}
		}
	}
}

// streamSSERelay streams events from a relay. Retryable errors are not written to SSE.
func (s *Server) streamSSERelay(ctx context.Context, w http.ResponseWriter, exec *activeExecution, stopOnUserInput bool) (streamTerminal, bool) {
	term, suspended, detached := s.attachSSE(ctx, w, exec, exec.streamOffset, stopOnUserInput)
	if detached && !suspended {
		return s.waitDetached(context.Background(), exec)
	}
	if record, err := s.sessions.Get(exec.sessionID); err == nil {
		switch {
		case term.kind == codingagent.EventError && term.retryable:
		case term.kind == codingagent.EventError:
			record.Status = codingagent.StatusError
			if term.content != "" {
				record.Error = term.content
			} else {
				record.Error = "unknown error occurred during execution"
			}
			s.sessions.Update(record)
		case term.kind == codingagent.EventResult:
			record.Status = codingagent.StatusCompleted
			record.Error = ""
			s.sessions.Update(record)
		}
		if term.kind == codingagent.EventResult || (term.kind == codingagent.EventError && !term.retryable) {
			s.reconcileSessionArtifacts(context.Background(), exec.sessionID, exec.turnID, exec.correlationID)
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

func (s *Server) logProcessRetryExhausted(sessionID string, attempt, maxAttempts int, healFresh bool, term streamTerminal) {
	if s.logger == nil {
		return
	}
	agentSessionID := ""
	if rec, err := s.sessions.Get(sessionID); err == nil {
		agentSessionID = rec.AgentSessionID
	}
	resumeMode := "resume"
	if healFresh || agentSessionID == "" {
		resumeMode = "fresh"
	}
	stderr := truncateStderrTail(term.content, maxLoggedStderrBytes)
	s.logger.Debug("codex process retries exhausted",
		"session_id", sessionID,
		"attempt", attempt,
		"max_attempts", maxAttempts,
		"resume_mode", resumeMode)
	fields := []any{
		"session_id", sessionID,
		"attempt", attempt,
		"max_attempts", maxAttempts,
		"resume_mode", resumeMode,
		"agent_session_id", agentSessionID,
		"stderr", stderr,
		"stderr_empty", stderr == "",
		"agent_session_id_empty", agentSessionID == "",
		"terminal_content", drainTimeoutTerminalContentMatches(term.content),
	}
	if st, ok := parseExitStatus(term.content); ok {
		fields = append(fields, "exit_status", st)
	} else {
		fields = append(fields, "exit_status", "")
	}
	s.logger.Error(logCodexProcessRetryExhausted, fields...)
}

func drainTimeoutTerminalContentMatches(content string) bool {
	return strings.Contains(content, drainTimeoutTerminalContent)
}

func truncateStderrTail(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

func parseExitStatus(content string) (status string, ok bool) {
	const prefix = "exit status "
	i := strings.LastIndex(strings.ToLower(content), prefix)
	if i < 0 {
		return "", false
	}
	rest := strings.TrimSpace(content[i+len(prefix):])
	if rest == "" {
		return "", false
	}
	end := 0
	for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
		end++
	}
	if end == 0 {
		return "", false
	}
	return rest[:end], true
}
