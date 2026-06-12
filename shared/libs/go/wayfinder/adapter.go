package wayfinder

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/axsh/arctic-tern/codingagent"
	"github.com/axsh/arctic-tern/logger"
)

const (
	// AgentName is the registered name for the Wayfinder agent.
	AgentName = "wayfinder"
)

// Adapter implements codingagent.CodingAgent for Wayfinder.
type Adapter struct {
	logger  logger.Logger
	baseURL string // Bifrost proxy URL
	token   string // Authentication token
}

// NewAdapter creates a new Wayfinder CodingAgent adapter.
func NewAdapter(baseURL, token string, log logger.Logger) *Adapter {
	if log == nil {
		log = &noopLogger{}
	}
	return &Adapter{
		logger:  log.WithComponent(AgentName),
		baseURL: baseURL,
		token:   token,
	}
}

// Name returns the agent backend name.
func (a *Adapter) Name() string {
	return AgentName
}

// CreateSession starts a new Wayfinder agent session.
func (a *Adapter) CreateSession(ctx context.Context, opts ...codingagent.SessionOption) (codingagent.Session, error) {
	cfg := codingagent.NewSessionConfig(opts...)

	// Apply defaults for wayfinder.
	if cfg.WorkDir == "" {
		cfg.WorkDir = "."
	}

	agentCfg := &AgentConfig{
		WorkDir:      cfg.WorkDir,
		SessionDir:   cfg.SessionDir,
		LogicalModel: cfg.Model,
	}
	if err := InitConfig(agentCfg); err != nil {
		return nil, fmt.Errorf("wayfinder: init config: %w", err)
	}

	llmClient := NewBifrostClient(a.baseURL, a.token)
	core := NewAgentCore(llmClient, agentCfg, a.logger)

	// If resuming, restore messages from session state.
	if cfg.AgentSessionID != "" {
		a.logger.Debug("resuming session", "session_id", cfg.AgentSessionID)
		// Session restore will be implemented in Part 2 (Session Persistence).
	}

	sessionID := cfg.AgentSessionID
	if sessionID == "" {
		sessionID = generateSessionID()
	}

	a.logger.Info("session created",
		"session_id", sessionID,
		"model", agentCfg.LogicalModel,
		"work_dir", agentCfg.WorkDir,
	)

	return &wayfinderSession{
		id:     sessionID,
		core:   core,
		config: agentCfg,
		logger: a.logger,
		prompt: cfg.Prompt,
	}, nil
}

// Close releases adapter resources.
func (a *Adapter) Close() error {
	return nil
}

// wayfinderSession implements codingagent.Session.
type wayfinderSession struct {
	id     string
	core   *AgentCore
	config *AgentConfig
	logger logger.Logger
	prompt string
	mu     sync.Mutex
}

// ID returns the session ID.
func (s *wayfinderSession) ID() string {
	return s.id
}

// Send sends a message to the agent and returns a streaming event channel.
func (s *wayfinderSession) Send(ctx context.Context, message string) (<-chan codingagent.StreamEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ch := make(chan codingagent.StreamEvent, 64)

	prompt := message
	if prompt == "" {
		prompt = s.prompt
	}

	go func() {
		defer close(ch)

		// Send initial system event.
		ch <- codingagent.StreamEvent{
			Type:      codingagent.EventSystem,
			SessionID: s.id,
		}

		result, err := s.core.Run(ctx, prompt)
		if err != nil {
			ch <- codingagent.StreamEvent{
				Type:  codingagent.EventError,
				Error: err,
			}
			return
		}

		// Send the text result.
		if result != "" {
			ch <- codingagent.StreamEvent{
				Type:    codingagent.EventText,
				Content: result,
			}
		}

		// Send completion event.
		ch <- codingagent.StreamEvent{
			Type: codingagent.EventResult,
		}
	}()

	return ch, nil
}

// Close terminates the session.
func (s *wayfinderSession) Close() error {
	s.logger.Debug("session closed", "session_id", s.id)
	return nil
}

// generateSessionID creates a simple unique session ID.
func generateSessionID() string {
	return fmt.Sprintf("wf-%d", time.Now().UnixNano())
}
