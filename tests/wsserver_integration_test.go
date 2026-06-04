package llm_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/axsh/hag/config"
	"github.com/axsh/hag/hag"
	"github.com/axsh/hag/llmgateway"
	"github.com/axsh/hag/tasklog"
	"github.com/axsh/hag/wsserver"
)

// TestWebSocket_LogStreaming verifies basic log streaming:
// connect -> receive snapshot -> add log entry -> receive log message.
func TestWebSocket_LogStreaming(t *testing.T) {
	cfg := &config.AppConfig{
		WebSocket: config.WebSocketConfig{Port: 0},
	}
	stub := llmgateway.NewStubGateway()
	srv, err := hag.New(hag.WithConfig(cfg), hag.WithGateway(stub))
	if err != nil {
		t.Fatalf("hag.New: %v", err)
	}
	ctx := t.Context()
	if err := srv.Launch(ctx); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer srv.Shutdown(ctx)

	wsURL := srv.WebSocketURL()
	if wsURL == "" {
		t.Fatal("WebSocketURL() returned empty")
	}

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	// Read snapshot.
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage (snapshot): %v", err)
	}
	var snap wsserver.Message
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatalf("Unmarshal snapshot: %v", err)
	}
	if snap.Type != "snapshot" {
		t.Fatalf("expected snapshot, got %q", snap.Type)
	}

	// Add hierarchical log entries.
	tl := srv.TaskLog()
	root := tasklog.NewAgentLogEntry("agent-1", tasklog.WithKind("text"))
	root.Body = "root message"
	tl.Add(root)

	child := tasklog.NewAgentLogEntry("agent-1",
		tasklog.WithKind("thinking"),
		tasklog.WithParentLogID(root.ID),
	)
	tl.Add(child)

	// Read 2 log messages.
	for i := 0; i < 2; i++ {
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, data, err = conn.ReadMessage()
		if err != nil {
			t.Fatalf("ReadMessage[%d]: %v", i, err)
		}
		var msg wsserver.Message
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Fatalf("Unmarshal msg[%d]: %v", i, err)
		}
		if msg.Type != "log" {
			t.Errorf("msg[%d].Type = %q, want %q", i, msg.Type, "log")
		}
	}
}

// TestWebSocket_HierarchicalLogStructure verifies 3-level parentLogId chain:
// root -> tool_use -> tool_result.
func TestWebSocket_HierarchicalLogStructure(t *testing.T) {
	cfg := &config.AppConfig{
		WebSocket: config.WebSocketConfig{Port: 0},
	}
	stub := llmgateway.NewStubGateway()
	srv, err := hag.New(hag.WithConfig(cfg), hag.WithGateway(stub))
	if err != nil {
		t.Fatalf("hag.New: %v", err)
	}
	ctx := t.Context()
	if err := srv.Launch(ctx); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer srv.Shutdown(ctx)

	conn, _, err := websocket.DefaultDialer.Dial(srv.WebSocketURL(), nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	// Read snapshot.
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	conn.ReadMessage()

	// Build hierarchical logs: root -> tool_use -> tool_result.
	tl := srv.TaskLog()
	root := tasklog.NewAgentLogEntry("agent-1", tasklog.WithKind("text"))
	tl.Add(root)

	toolUse := tasklog.NewAgentLogEntry("agent-1",
		tasklog.WithKind("tool_use"),
		tasklog.WithParentLogID(root.ID),
	)
	tl.Add(toolUse)

	toolResult := tasklog.NewAgentLogEntry("agent-1",
		tasklog.WithKind("tool_result"),
		tasklog.WithParentLogID(toolUse.ID),
	)
	tl.Add(toolResult)

	// Verify hierarchy in received messages.
	received := make([]*tasklog.AgentLogEntry, 0, 3)
	for i := 0; i < 3; i++ {
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("ReadMessage[%d]: %v", i, err)
		}
		var msg wsserver.Message
		json.Unmarshal(data, &msg)
		var payload wsserver.LogPayload
		json.Unmarshal(msg.Payload, &payload)
		if payload.Entry == nil {
			t.Fatalf("msg[%d]: payload.Entry is nil", i)
		}
		received = append(received, payload.Entry)
	}

	// Verify: root has no parent.
	if received[0].ParentLogID != "" {
		t.Errorf("root.ParentLogID = %q, want empty", received[0].ParentLogID)
	}
	// Verify: tool_use's parent is root.
	if received[1].ParentLogID != received[0].ID {
		t.Errorf("toolUse.ParentLogID = %q, want %q (root.ID)",
			received[1].ParentLogID, received[0].ID)
	}
	// Verify: tool_result's parent is tool_use (3rd level).
	if received[2].ParentLogID != received[1].ID {
		t.Errorf("toolResult.ParentLogID = %q, want %q (toolUse.ID)",
			received[2].ParentLogID, received[1].ID)
	}
	// Verify kinds.
	if received[0].Kind != "text" {
		t.Errorf("received[0].Kind = %q, want %q", received[0].Kind, "text")
	}
	if received[1].Kind != "tool_use" {
		t.Errorf("received[1].Kind = %q, want %q", received[1].Kind, "tool_use")
	}
	if received[2].Kind != "tool_result" {
		t.Errorf("received[2].Kind = %q, want %q", received[2].Kind, "tool_result")
	}
}

// TestWebSocket_SnapshotContainsPreExistingLogs verifies that connecting
// after logs exist delivers a snapshot with all prior entries.
func TestWebSocket_SnapshotContainsPreExistingLogs(t *testing.T) {
	cfg := &config.AppConfig{
		WebSocket: config.WebSocketConfig{Port: 0},
	}
	stub := llmgateway.NewStubGateway()
	srv, err := hag.New(hag.WithConfig(cfg), hag.WithGateway(stub))
	if err != nil {
		t.Fatalf("hag.New: %v", err)
	}
	ctx := t.Context()
	if err := srv.Launch(ctx); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer srv.Shutdown(ctx)

	// Add entries before connecting.
	tl := srv.TaskLog()
	for i := 0; i < 5; i++ {
		e := tasklog.NewAgentLogEntry("agent-1", tasklog.WithKind("text"))
		tl.Add(e)
	}

	// Allow broadcast to process.
	time.Sleep(100 * time.Millisecond)

	// Connect client.
	conn, _, err := websocket.DefaultDialer.Dial(srv.WebSocketURL(), nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	// Read snapshot.
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}

	var msg wsserver.Message
	json.Unmarshal(data, &msg)
	if msg.Type != "snapshot" {
		t.Fatalf("Type = %q, want %q", msg.Type, "snapshot")
	}

	var snapPayload struct {
		Entries []json.RawMessage `json:"entries"`
	}
	json.Unmarshal(msg.Payload, &snapPayload)
	if len(snapPayload.Entries) != 5 {
		t.Errorf("snapshot entries = %d, want 5", len(snapPayload.Entries))
	}
}
