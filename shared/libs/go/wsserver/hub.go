package wsserver

import (
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
				h.logger.Info("client connected", "total", len(h.clients))
			}
			// Send snapshot of existing log entries.
			h.sendSnapshot(client)

		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
				if h.logger != nil {
					h.logger.Info("client disconnected", "total", len(h.clients))
				}
			}

		case msg := <-h.broadcast:
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
			h.logger.Error("snapshot marshal error", "error", err)
		}
		return
	}
	select {
	case client.send <- data:
	default:
		// Buffer full; drop snapshot.
	}
}
