package wsserver

import "context"

type Server struct{}

func New() *Server { return &Server{} }

func (s *Server) Launch(ctx context.Context) error { return nil }

func (s *Server) Shutdown(ctx context.Context) error { return nil }
