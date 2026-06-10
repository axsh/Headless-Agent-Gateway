package wsserver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"

	"github.com/gorilla/websocket"

	"github.com/axsh/hag/logger"
	"github.com/axsh/hag/tasklog"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Server is the WebSocket server for log streaming.
type Server struct {
	port       int
	hub        *Hub
	httpServer *http.Server
	ln         net.Listener
	logger     logger.Logger
	taskLog    *tasklog.TaskLog
}

// New creates a new WebSocket Server.
// port=0 uses OS-assigned ephemeral port.
// taskLog and log may be nil.
func New(port int, tl *tasklog.TaskLog, log logger.Logger) *Server {
	if log == nil {
		log = logger.NewDefault(logger.LevelInfo)
	}
	s := &Server{
		port:    port,
		taskLog: tl,
		logger:  log.WithComponent("wsserver"),
	}
	s.logger.Debug("creating websocket server", "port", port)
	return s
}

// Launch starts the WebSocket server. Non-blocking.
func (s *Server) Launch(ctx context.Context) error {
	s.hub = newHub(s.taskLog, s.logger)
	go s.hub.run()

	// Wire TaskLog -> Hub broadcast.
	s.wireTaskLog()

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.serveWS)

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", s.port))
	if err != nil {
		return fmt.Errorf("wsserver listen: %w", err)
	}
	s.ln = ln
	s.port = ln.Addr().(*net.TCPAddr).Port

	s.httpServer = &http.Server{Handler: mux}

	go func() {
		if err := s.httpServer.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logger.Error("wsserver serve error", "error", err)
		}
	}()

	s.logger.Info("websocket server listening", "addr", fmt.Sprintf("127.0.0.1:%d", s.port))
	return nil
}

// Shutdown gracefully stops the WebSocket server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("shutting down websocket server")

	// Stop the hub (closes all client connections).
	if s.hub != nil {
		close(s.hub.stop)
	}

	// Shutdown HTTP server.
	if s.httpServer != nil {
		if err := s.httpServer.Shutdown(ctx); err != nil {
			return fmt.Errorf("wsserver shutdown: %w", err)
		}
	}
	return nil
}

// URL returns the WebSocket server URL.
func (s *Server) URL() string {
	if s.port == 0 {
		return ""
	}
	return fmt.Sprintf("ws://127.0.0.1:%d/ws", s.port)
}

// serveWS handles WebSocket upgrade requests on /ws.
func (s *Server) serveWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Error("websocket upgrade error", "error", err)
		return
	}

	remoteAddr := ""
	if conn != nil && conn.RemoteAddr() != nil {
		remoteAddr = conn.RemoteAddr().String()
	}
	s.logger.Debug("websocket client connected", "remote_addr", remoteAddr)

	client := &Client{
		hub:  s.hub,
		conn: conn,
		send: make(chan []byte, sendBufSize),
	}
	s.hub.register <- client

	go client.writePump()
	go client.readPump()
}

// wireTaskLog connects the TaskLog's onEntry callback to broadcast.
func (s *Server) wireTaskLog() {
	if s.taskLog == nil {
		return
	}
	s.taskLog.SetOnEntry(func(entry tasklog.Entry) {
		agentLog, ok := entry.(*tasklog.AgentLogEntry)
		if !ok {
			return // Only broadcast AgentLogEntry for now.
		}
		data, err := NewLogMessage(agentLog)
		if err != nil {
			s.logger.Error("log message marshal error", "error", err)
			return
		}
		s.hub.broadcast <- data
	})
}
