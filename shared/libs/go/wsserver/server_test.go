package wsserver

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/axsh/hag/logger"
	"github.com/axsh/hag/tasklog"
)

func TestServer_LaunchShutdown(t *testing.T) {
	log := logger.NewDefault(logger.LevelDebug)
	tl := tasklog.New()
	srv := New(0, tl, log)

	if err := srv.Launch(t.Context()); err != nil {
		t.Fatalf("Launch: %v", err)
	}

	url := srv.URL()
	if url == "" {
		t.Fatal("URL() returned empty string after Launch")
	}

	if err := srv.Shutdown(t.Context()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}

func TestServer_WebSocketConnection(t *testing.T) {
	log := logger.NewDefault(logger.LevelDebug)
	tl := tasklog.New()
	srv := New(0, tl, log)

	if err := srv.Launch(t.Context()); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer srv.Shutdown(t.Context())

	conn, _, err := websocket.DefaultDialer.Dial(srv.URL(), nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	// Should receive snapshot message.
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}

	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if msg.Type != "snapshot" {
		t.Errorf("Type = %q, want %q", msg.Type, "snapshot")
	}
}

func TestServer_LogBroadcast(t *testing.T) {
	log := logger.NewDefault(logger.LevelDebug)
	tl := tasklog.New()
	srv := New(0, tl, log)

	if err := srv.Launch(t.Context()); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer srv.Shutdown(t.Context())

	conn, _, err := websocket.DefaultDialer.Dial(srv.URL(), nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	// Read snapshot (should be empty).
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage (snapshot): %v", err)
	}
	var snapMsg Message
	json.Unmarshal(data, &snapMsg)
	if snapMsg.Type != "snapshot" {
		t.Errorf("expected snapshot, got %q", snapMsg.Type)
	}

	// Add an entry to TaskLog.
	entry := tasklog.NewAgentLogEntry("agent-1",
		tasklog.WithKind("thinking"),
		tasklog.WithParentLogID("root-id"),
	)
	tl.Add(entry)

	// Read the broadcast log message.
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err = conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage (log): %v", err)
	}
	var logMsg Message
	json.Unmarshal(data, &logMsg)
	if logMsg.Type != "log" {
		t.Errorf("expected log, got %q", logMsg.Type)
	}
	var payload LogPayload
	json.Unmarshal(logMsg.Payload, &payload)
	if payload.Entry.Kind != "thinking" {
		t.Errorf("Kind = %q, want %q", payload.Entry.Kind, "thinking")
	}
	if payload.Entry.ParentLogID != "root-id" {
		t.Errorf("ParentLogID = %q, want %q", payload.Entry.ParentLogID, "root-id")
	}
}

func TestServer_SnapshotOnConnect(t *testing.T) {
	log := logger.NewDefault(logger.LevelDebug)
	tl := tasklog.New()
	srv := New(0, tl, log)

	// Add entries before any client connects.
	e1 := tasklog.NewAgentLogEntry("agent-1", tasklog.WithKind("text"))
	e1.Body = "first entry"
	tl.Add(e1)
	e2 := tasklog.NewAgentLogEntry("agent-1", tasklog.WithKind("thinking"))
	e2.Body = "second entry"
	tl.Add(e2)

	if err := srv.Launch(t.Context()); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer srv.Shutdown(t.Context())

	conn, _, err := websocket.DefaultDialer.Dial(srv.URL(), nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	// Read snapshot with pre-existing entries.
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}

	var msg Message
	json.Unmarshal(data, &msg)
	if msg.Type != "snapshot" {
		t.Fatalf("Type = %q, want %q", msg.Type, "snapshot")
	}

	// Decode snapshot entries as raw JSON array.
	var snapPayload struct {
		Entries []json.RawMessage `json:"entries"`
	}
	json.Unmarshal(msg.Payload, &snapPayload)
	if len(snapPayload.Entries) != 2 {
		t.Errorf("snapshot entries = %d, want 2", len(snapPayload.Entries))
	}
}

func TestServer_MultipleClients(t *testing.T) {
	log := logger.NewDefault(logger.LevelDebug)
	tl := tasklog.New()
	srv := New(0, tl, log)

	if err := srv.Launch(t.Context()); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer srv.Shutdown(t.Context())

	// Connect 2 clients.
	conn1, _, err := websocket.DefaultDialer.Dial(srv.URL(), nil)
	if err != nil {
		t.Fatalf("Dial client1: %v", err)
	}
	defer conn1.Close()

	conn2, _, err := websocket.DefaultDialer.Dial(srv.URL(), nil)
	if err != nil {
		t.Fatalf("Dial client2: %v", err)
	}
	defer conn2.Close()

	// Read snapshots from both.
	for _, conn := range []*websocket.Conn{conn1, conn2} {
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		conn.ReadMessage()
	}

	// Add an entry.
	entry := tasklog.NewAgentLogEntry("agent-1", tasklog.WithKind("text"))
	entry.Body = "broadcast test"
	tl.Add(entry)

	// Both clients should receive the log message.
	for i, conn := range []*websocket.Conn{conn1, conn2} {
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("client%d ReadMessage: %v", i+1, err)
		}
		var msg Message
		json.Unmarshal(data, &msg)
		if msg.Type != "log" {
			t.Errorf("client%d: Type = %q, want %q", i+1, msg.Type, "log")
		}
	}
}

func TestServer_ClientDisconnect(t *testing.T) {
	log := logger.NewDefault(logger.LevelDebug)
	tl := tasklog.New()
	srv := New(0, tl, log)

	if err := srv.Launch(t.Context()); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer srv.Shutdown(t.Context())

	conn, _, err := websocket.DefaultDialer.Dial(srv.URL(), nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}

	// Read snapshot.
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	conn.ReadMessage()

	// Close client connection.
	conn.Close()

	// Allow time for readPump to detect disconnect and Hub to process unregister.
	time.Sleep(100 * time.Millisecond)

	// Add entry; should not panic even with no clients.
	entry := tasklog.NewAgentLogEntry("agent-1", tasklog.WithKind("text"))
	tl.Add(entry)

	// Allow broadcast to process.
	time.Sleep(50 * time.Millisecond)
}
