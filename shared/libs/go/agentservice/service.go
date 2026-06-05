package agentservice

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/axsh/hag/codingagent"
	"github.com/axsh/hag/logger"
	"github.com/axsh/hag/tasklog"
)

// AgentService is the interface for the Coding Agent API service.
type AgentService interface {
	HTTPHandler() http.Handler
}

// Server is the Coding Agent API service layer.
type Server struct {
	agents     map[string]codingagent.CodingAgent
	sessions   codingagent.SessionStore
	logger     logger.Logger
	taskLog    *tasklog.TaskLog
	gatewayURL string
}

// ServerOption configures a Server.
type ServerOption func(*Server)

// WithLogger sets the logger for the server.
func WithLogger(log logger.Logger) ServerOption {
	return func(s *Server) { s.logger = log }
}

// WithTaskLog sets the TaskLog for the server.
func WithTaskLog(tl *tasklog.TaskLog) ServerOption {
	return func(s *Server) { s.taskLog = tl }
}

// WithGatewayURL sets the LLM Gateway Proxy URL for health checks.
func WithGatewayURL(url string) ServerOption {
	return func(s *Server) { s.gatewayURL = url }
}

// New creates a new AgentService Server.
func New(opts ...ServerOption) *Server {
	s := &Server{
		agents:   make(map[string]codingagent.CodingAgent),
		sessions: NewMemorySessionStore(),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// RegisterAgent registers a CodingAgent with the server.
func (s *Server) RegisterAgent(agent codingagent.CodingAgent) {
	s.agents[agent.Name()] = agent
}

// HTTPHandler returns the HTTP handler with all endpoint routes.
func (s *Server) HTTPHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/api/v1/agents", s.routeAgents)
	mux.HandleFunc("/api/v1/sessions", s.routeSessions)
	mux.HandleFunc("/api/v1/sessions/", s.routeSessionByID)
	return mux
}

func (s *Server) routeAgents(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListAgents(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) routeSessions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.handleCreateSession(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) routeSessionByID(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if strings.HasSuffix(path, "/messages") {
		s.handleSendMessage(w, r)
	} else if strings.HasSuffix(path, "/terminate") {
		s.handleTerminate(w, r)
	} else {
		switch r.Method {
		case http.MethodGet:
			s.handleGetSession(w, r)
		case http.MethodDelete:
			s.handleDeleteSession(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

// generateID generates a unique session ID.
func (s *Server) generateID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
