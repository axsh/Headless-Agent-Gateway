package wsserver

import (
	"encoding/json"

	"github.com/axsh/hag/logger"
	"github.com/axsh/hag/tasklog"
)

// Hub manages connected WebSocket clients and message broadcasting.
type Hub struct {
	clients    map[*Client]bool
	broadcast  chan []byte
	register   chan *Client
	unregister chan *Client
	taskLog    *tasklog.TaskLog
	logger     logger.Logger
	stop       chan struct{}
}

func newHub(tl *tasklog.TaskLog, log logger.Logger) *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan []byte, 256),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		taskLog:    tl,
		logger:     log,
		stop:       make(chan struct{}),
	}
}

// run is the Hub's main event loop. Runs as a goroutine.
func (h *Hub) run() {
	for {
		select {
		case client := <-h.register:
			h.clients[client] = true
			if h.logger != nil {
				remoteAddr := ""
				if client.conn != nil {
					remoteAddr = client.conn.RemoteAddr().String()
				}
				h.logger.Debug("websocket client connected", "remote_addr", remoteAddr)
				h.logger.Info("client connected", "total", len(h.clients))
			}
			// Send snapshot of existing log entries.
			h.sendSnapshot(client)

		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
				if h.logger != nil {
					remoteAddr := ""
					if client.conn != nil {
						remoteAddr = client.conn.RemoteAddr().String()
					}
					h.logger.Debug("websocket client disconnected", "remote_addr", remoteAddr)
					h.logger.Info("client disconnected", "total", len(h.clients))
				}
			}

		case msg := <-h.broadcast:
			if h.logger != nil {
				entryType := ""
				var parsed struct {
					Type string `json:"type"`
				}
				if json.Unmarshal(msg, &parsed) == nil {
					entryType = parsed.Type
				}
				h.logger.Debug("broadcasting to clients", "client_count", len(h.clients), "entry_type", entryType)
				h.logger.Trace("broadcast payload", "payload", string(msg))
			}
			for client := range h.clients {
				select {
				case client.send <- msg:
				default:
					// Client send buffer full; disconnect.
					close(client.send)
					delete(h.clients, client)
				}
			}

		case <-h.stop:
			// Close all client connections.
			for client := range h.clients {
				close(client.send)
				delete(h.clients, client)
			}
			return
		}
	}
}

// sendSnapshot sends the current TaskLog history to a newly connected client.
func (h *Hub) sendSnapshot(client *Client) {
	if h.taskLog == nil {
		return
	}
	entries := h.taskLog.Entries()
	data, err := NewSnapshotMessage(entries)
	if err != nil {
		if h.logger != nil {
			remoteAddr := ""
			if client.conn != nil {
				remoteAddr = client.conn.RemoteAddr().String()
			}
			h.logger.Error("snapshot marshal error", "error", err, "remote_addr", remoteAddr, "entry_count", len(entries))
		}
		return
	}
	select {
	case client.send <- data:
	default:
		// Buffer full; drop snapshot.
	}
}
