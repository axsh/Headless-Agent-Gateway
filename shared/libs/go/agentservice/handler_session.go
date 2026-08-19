package agentservice

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/wayfinder/portable"
	"github.com/axsh/arctic-tern/shared/libs/go/wayfinder/session"
)

type sessionAPIResponse struct {
	codingagent.SessionRecord
	AgentBindings map[string]session.AgentBinding `json:"agent_bindings,omitempty"`
	ActiveAgent   string                          `json:"active_agent,omitempty"`
	Supplement    portable.Strategy               `json:"supplement,omitempty"`
	Followable    bool                            `json:"followable,omitempty"`
	TurnID        string                          `json:"turn_id,omitempty"`
}

type wrapResult struct {
	prompt   string
	resumeID string
	injected bool
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	workDir := r.URL.Query().Get("work_dir")
	if workDir == "" {
		http.Error(w, "work_dir is required", http.StatusBadRequest)
		return
	}
	lister, ok := s.sessions.(WorkDirSessionLister)
	if !ok {
		http.Error(w, "workspace listing is not available", http.StatusNotImplemented)
		return
	}
	recs, err := lister.ListByWorkDir(workDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := make([]sessionAPIResponse, 0, len(recs))
	for _, rec := range recs {
		out = append(out, s.sessionResponse(rec))
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

func (s *Server) handlePatchSession(w http.ResponseWriter, r *http.Request) {
	id := extractPathParam(r.URL.Path, "/api/v1/sessions/")
	if s.logger != nil {
		s.logger.Debug("patching session", "session_id", id)
	}
	record, err := s.sessions.Get(id)
	if err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	if exec, ok := s.execRegistry.Get(id); ok {
		writeSessionBusy(w, exec.status)
		return
	}
	if record.Status == codingagent.StatusSuspended {
		writeSessionBusy(w, record.Status)
		return
	}

	var req struct {
		ConfigDir  *string                     `json:"config_dir"`
		Agent      *string                     `json:"agent"`
		Model      *string                     `json:"model"`
		Supplement *session.SupplementStrategy `json:"supplement"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.ConfigDir == nil && req.Agent == nil && req.Supplement == nil && req.Model == nil {
		http.Error(w, "agent, config_dir, supplement, or model is required", http.StatusBadRequest)
		return
	}

	agentChanged := false
	if req.Agent != nil {
		name := *req.Agent
		if _, ok := s.agents[name]; !ok {
			http.Error(w, "unknown agent: "+name, http.StatusBadRequest)
			return
		}
		if name != record.AgentName {
			agentChanged = true
			record.AgentName = name
			record.AgentSessionID = ""
			if record.SessionDir != "" {
				c := session.OpenCanonical(record.SessionDir)
				if err := c.SetActiveAgent(name); err != nil && s.logger != nil {
					s.logger.Warn("failed to set active agent", "session_id", id, "error", err.Error())
				}
			}
		}
	}

	if req.Model != nil {
		model := *req.Model
		if model != "" && len(s.gatewayModels) > 0 {
			resolved, ok := s.ResolveModel(model)
			if ok {
				model = resolved
			} else if !s.IsValidModel(model) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]any{
					"error":            "unsupported model: " + model,
					"available_models": s.AvailableModelNames(),
				})
				return
			}
		}
		record.Model = model
		if s.logger != nil {
			s.logger.Debug("session model updated", "session_id", id, "model", record.Model, "agent_switch", agentChanged)
		}
	}

	if req.Supplement != nil {
		if !portable.KnownAlgorithm(req.Supplement.Algorithm) {
			http.Error(w, "unknown supplement algorithm: "+req.Supplement.Algorithm, http.StatusBadRequest)
			return
		}
		if record.SessionDir != "" {
			c := session.OpenCanonical(record.SessionDir)
			if err := c.SetSupplement(*req.Supplement); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
	}

	if req.ConfigDir != nil {
		resolved, status, errMsg := validateAndResolveConfigDir(*req.ConfigDir)
		if status != 0 {
			http.Error(w, errMsg, status)
			return
		}
		record.ConfigDir = resolved
	}

	record.UpdatedAt = time.Now()
	if err := s.sessions.Update(record); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if s.logger != nil {
		s.logger.Debug("session patched",
			"session_id", id,
			"agent", record.AgentName,
			"model", record.Model,
			"config_dir", record.ConfigDir)
	}
	w.Header().Set("Content-Type", "application/json")
	s.writeSessionJSON(w, record)
}

func (s *Server) writeSessionJSON(w http.ResponseWriter, record *codingagent.SessionRecord) {
	json.NewEncoder(w).Encode(s.sessionResponse(record))
}

func (s *Server) sessionResponse(record *codingagent.SessionRecord) sessionAPIResponse {
	resp := sessionAPIResponse{SessionRecord: *record}
	if exec, ok := s.execRegistry.Get(record.ID); ok {
		resp.Followable = true
		resp.TurnID = exec.turnID
	}
	if record.SessionDir == "" {
		resp.Supplement = portable.WithDefaults(s.serverSupplement())
		return resp
	}
	c := session.OpenCanonical(record.SessionDir)
	meta, err := c.LoadMetadata()
	if err != nil {
		resp.Supplement = portable.WithDefaults(s.serverSupplement())
		return resp
	}
	resp.ActiveAgent = meta.ActiveAgent
	resp.AgentBindings = meta.AgentBindings
	merged, mergeErr := portable.MergeStrategy(s.serverSupplement(), meta.Supplement, portable.Strategy{})
	if mergeErr != nil {
		resp.Supplement = portable.WithDefaults(s.serverSupplement())
		return resp
	}
	resp.Supplement = portable.WithDefaults(merged)
	return resp
}

func (s *Server) serverSupplement() portable.Strategy {
	return portable.Strategy{
		Algorithm:        s.supplementCfg.Algorithm,
		Model:            s.supplementCfg.Model,
		MaxChunkMessages: s.supplementCfg.MaxChunkMessages,
		ThresholdBytes:   s.supplementCfg.ThresholdBytes,
		RecentKeep:       s.supplementCfg.RecentKeep,
	}
}

func (s *Server) wrapPromptWithSupplement(ctx context.Context, record *codingagent.SessionRecord, promptText string, turn *session.SupplementStrategy) (wrapResult, error) {
	result := wrapResult{prompt: promptText, resumeID: record.AgentSessionID}
	if record.SessionDir == "" {
		return result, nil
	}
	c := session.OpenCanonical(record.SessionDir)
	meta, err := c.LoadMetadata()
	if err != nil {
		return result, nil
	}
	through := 0
	ownNative := ""
	if b, ok := meta.AgentBindings[record.AgentName]; ok {
		through = b.IngestedThroughSeq
		ownNative = b.AgentSessionID
	}
	if result.resumeID == "" {
		result.resumeID = ownNative
	}
	if meta.TotalSeq <= 0 {
		return result, nil
	}
	msgs, err := c.LoadRange(1, meta.TotalSeq)
	if err != nil || len(msgs) == 0 {
		return result, nil
	}
	delta := portable.Delta(msgs, record.AgentName, through)
	if len(delta) == 0 {
		return result, nil
	}
	turnStrat := portable.Strategy{}
	if turn != nil {
		turnStrat = *turn
	}
	strat, err := portable.MergeStrategy(s.serverSupplement(), meta.Supplement, turnStrat)
	if err != nil {
		return result, err
	}
	strat = portable.WithDefaults(strat)
	if strat.Model == "" {
		strat.Model = record.Model
	}
	sup, err := portable.BuildSupplement(ctx, record.AgentName, delta, strat, s.summarizer)
	if err != nil {
		return result, err
	}
	result.prompt = portable.WrapPrompt(sup, promptText)
	result.injected = true
	if s.logger != nil {
		s.logger.Debug("wrapped prompt with transferred context",
			"session_id", record.ID,
			"agent", record.AgentName,
			"delta", len(delta),
			"algorithm", strat.Algorithm)
	}
	return result, nil
}

func wrapHTTPStatus(err error) int {
	if err == nil {
		return http.StatusOK
	}
	if errors.Is(err, portable.ErrMapReduceRequiresLLM) {
		return http.StatusInternalServerError
	}
	if strings.Contains(err.Error(), "unknown supplement algorithm") {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

func (s *Server) wrapPromptForSelfHeal(ctx context.Context, record *codingagent.SessionRecord, userPrompt string) (string, error) {
	if record == nil || record.SessionDir == "" {
		return userPrompt, nil
	}
	c := session.OpenCanonical(record.SessionDir)
	meta, err := c.LoadMetadata()
	if err != nil {
		return userPrompt, nil
	}
	msgs, err := c.LoadRange(1, meta.TotalSeq)
	if err != nil || len(msgs) == 0 {
		return userPrompt, nil
	}
	var prior []session.Message
	for _, m := range msgs {
		if m.Seq == meta.TotalSeq && m.Role == "user" {
			continue
		}
		prior = append(prior, m)
	}
	if len(prior) == 0 {
		return userPrompt, nil
	}
	strat, err := portable.MergeStrategy(s.serverSupplement(), meta.Supplement, portable.Strategy{})
	if err != nil {
		return "", err
	}
	strat = portable.WithDefaults(strat)
	if strat.Model == "" {
		strat.Model = record.Model
	}
	sup, err := portable.BuildSupplement(ctx, record.AgentName, prior, strat, s.summarizer)
	if err != nil {
		return "", err
	}
	wrapped := portable.WrapPrompt(sup, userPrompt)
	if s.logger != nil {
		s.logger.Debug("wrapped prompt for self-heal resume fallback",
			"session_id", record.ID,
			"agent", record.AgentName,
			"prior", len(prior))
	}
	return wrapped, nil
}

func (s *Server) clearPersistedAgentSessionID(sessionID string) {
	rec, err := s.sessions.Get(sessionID)
	if err != nil || rec.AgentSessionID == "" {
		return
	}
	rec.AgentSessionID = ""
	_ = s.sessions.Update(rec)
	if s.logger != nil {
		s.logger.Warn("cleared broken native thread id for self-heal",
			"session_id", sessionID)
	}
}

func (s *Server) ingestActiveTurn(sessionID string) {
	exec, ok := s.execRegistry.Get(sessionID)
	if !ok || exec == nil || exec.relay == nil {
		return
	}
	rec, err := s.sessions.Get(sessionID)
	if err != nil || rec == nil || rec.SessionDir == "" {
		return
	}
	events := exec.relay.snapshot()
	if err := IngestTurn(rec.SessionDir, rec.AgentName, rec.AgentSessionID, events); err != nil {
		if s.logger != nil {
			s.logger.Error("failed to ingest turn into canonical history",
				"session_id", sessionID, "error", err.Error())
		}
		return
	}
	meta, err := session.OpenCanonical(rec.SessionDir).LoadMetadata()
	if err != nil {
		return
	}
	if b, ok := meta.AgentBindings[rec.AgentName]; ok && b.AgentSessionID != "" {
		rec.AgentSessionID = b.AgentSessionID
		_ = s.sessions.Update(rec)
	}
}
