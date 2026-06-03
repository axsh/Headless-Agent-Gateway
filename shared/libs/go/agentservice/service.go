package agentservice

import "net/http"

type AgentService interface {
	HTTPHandler() http.Handler
}

type Server struct{}

func New() *Server { return &Server{} }

func (s *Server) HTTPHandler() http.Handler { return http.NewServeMux() }
