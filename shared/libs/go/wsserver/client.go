package wsserver

import (
	"github.com/gorilla/websocket"
)

const (
	// sendBufSize is the buffer size for the client send channel.
	sendBufSize = 256
)

// Client represents a single WebSocket connection.
type Client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan []byte
}

// writePump sends messages from hub to the WebSocket connection.
// Runs as a goroutine per client.
func (c *Client) writePump() {
	defer func() {
		c.conn.Close()
	}()
	for msg := range c.send {
		if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			return
		}
	}
}

// readPump reads from the WebSocket and detects client disconnect.
// Runs as a goroutine per client.
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()
	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			return // client disconnected
		}
		// Currently no client-to-server messages are processed.
	}
}
