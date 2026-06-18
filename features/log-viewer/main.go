// Package main implements a CLI log viewer and simulator for tern WebSocket
// log streaming. It supports two modes:
//
//   - Viewer mode (default): connects to a WebSocket server and displays
//     hierarchical agent logs with color coding and indentation.
//   - Simulator mode (--simulate): starts a tern server with a WebSocket
//     endpoint and generates simulated hierarchical logs for demonstration.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/websocket"

	"github.com/axsh/arctic-tern/shared/libs/go/config"
	"github.com/axsh/arctic-tern/tern"
	"github.com/axsh/arctic-tern/shared/libs/go/llmgateway"
	"github.com/axsh/arctic-tern/shared/libs/go/tasklog"
	"github.com/axsh/arctic-tern/shared/libs/go/wsserver"
)

// ANSI color codes.
const (
	colorReset   = "\033[0m"
	colorRed     = "\033[31m"
	colorGreen   = "\033[32m"
	colorYellow  = "\033[33m"
	colorCyan    = "\033[36m"
	colorGray    = "\033[90m"
	colorBoldRed = "\033[1;31m"
)

func main() {
	simulate := flag.Bool("simulate", false, "Run in simulator mode (start server + generate logs)")
	url := flag.String("url", "ws://localhost:18080/ws", "WebSocket server URL")
	port := flag.Int("port", 18080, "WebSocket server port (simulator mode)")
	flag.Parse()

	if *simulate {
		runSimulator(*port)
	} else {
		runViewer(*url)
	}
}

// runViewer connects to a WebSocket server and displays logs.
func runViewer(wsURL string) {
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		log.Fatalf("Failed to connect to %s: %v", wsURL, err)
	}
	defer conn.Close()

	fmt.Printf("%s--- Connected to %s ---%s\n", colorGreen, wsURL, colorReset)

	depthMap := make(map[string]int)

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			fmt.Printf("%s--- Disconnected: %v ---%s\n", colorRed, err, colorReset)
			return
		}

		var msg wsserver.Message
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}

		switch msg.Type {
		case "snapshot":
			var payload struct {
				Entries []json.RawMessage `json:"entries"`
			}
			json.Unmarshal(msg.Payload, &payload)
			fmt.Printf("%s--- Snapshot: %d entries ---%s\n",
				colorGray, len(payload.Entries), colorReset)
			for _, raw := range payload.Entries {
				var entry tasklog.AgentLogEntry
				if err := json.Unmarshal(raw, &entry); err == nil {
					displayEntry(&entry, depthMap)
				}
			}

		case "log":
			var payload wsserver.LogPayload
			json.Unmarshal(msg.Payload, &payload)
			if payload.Entry != nil {
				displayEntry(payload.Entry, depthMap)
			}
		}
	}
}

// displayEntry displays a log entry with indentation and colors.
func displayEntry(entry *tasklog.AgentLogEntry, depthMap map[string]int) {
	depth := 0
	if entry.ParentLogID != "" {
		if parentDepth, ok := depthMap[entry.ParentLogID]; ok {
			depth = parentDepth + 1
		}
	}
	if entry.Phase == "begin" {
		depthMap[entry.ID] = depth
	}

	indent := strings.Repeat("  ", depth)
	timestamp := entry.Time.Format("15:04:05")
	kindColor := colorForKind(entry.Kind)

	switch entry.Phase {
	case "begin":
		fmt.Printf("%s[%s] %sBEGIN %-12s%s %s\n",
			indent, timestamp, kindColor, entry.Kind, colorReset, truncate(entry.Body, 80))
	case "send":
		fmt.Printf("%s[%s]   %sSEND%s  %s\n",
			indent, timestamp, kindColor, colorReset, truncate(entry.Body, 120))
	case "end":
		fmt.Printf("%s[%s] %sEND   %-12s%s\n",
			indent, timestamp, kindColor, entry.Kind, colorReset)
	}
}

func colorForKind(kind string) string {
	switch kind {
	case "thinking":
		return colorGray
	case "tool_use":
		return colorCyan
	case "tool_result":
		return colorYellow
	case "system":
		return colorGreen
	case "error":
		return colorBoldRed
	default:
		return colorReset
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// runSimulator starts a tern server and generates simulated logs.
func runSimulator(port int) {
	cfg := &config.AppConfig{
		WebSocket: config.WebSocketConfig{Port: port},
	}
	stub := llmgateway.NewStubGateway()
	srv, err := tern.New(
		tern.WithConfig(cfg),
		tern.WithGateway(stub),
	)
	if err != nil {
		log.Fatalf("tern.New: %v", err)
	}

	ctx := context.Background()
	if err := srv.Launch(ctx); err != nil {
		log.Fatalf("Launch: %v", err)
	}
	defer srv.Shutdown(ctx)

	tl := srv.TaskLog()
	stack := &tasklog.LogStack{}

	fmt.Printf("Simulator running. WebSocket: ws://localhost:%d/ws\n", port)
	fmt.Printf("Connect with: bin/log-viewer --url ws://localhost:%d/ws\n", port)
	fmt.Println("Press Ctrl+C to stop.")

	// Wait for client connection.
	time.Sleep(2 * time.Second)

	go func() {
		generateSimulatedLogs(tl, stack)
	}()

	// Wait for signal.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan
	fmt.Println("\nShutting down...")
}

// generateSimulatedLogs generates a hierarchical log sequence
// demonstrating tool calling with nested logs.
func generateSimulatedLogs(tl *tasklog.TaskLog, stack *tasklog.LogStack) {
	// Round 1: Normal tool calling flow.
	// 1. Root log (text) begin.
	root := tasklog.NewAgentLogEntry("agent-1", tasklog.WithKind("text"))
	root.Body = "Processing user request: 'Fix the bug in main.go'"
	tl.Add(root)
	stack.Push(root.ID)
	time.Sleep(300 * time.Millisecond)

	// 2. Thinking begin -> send -> end.
	thinking := tasklog.NewAgentLogEntry("agent-1",
		tasklog.WithKind("thinking"),
		tasklog.WithParentLogID(stack.CurrentParentID()),
	)
	tl.Add(thinking)
	stack.Push(thinking.ID)
	time.Sleep(200 * time.Millisecond)

	tl.Add(tasklog.NewAgentLogSendEntry(thinking.ID, "agent-1",
		"Let me analyze the error in main.go..."))
	time.Sleep(150 * time.Millisecond)
	tl.Add(tasklog.NewAgentLogSendEntry(thinking.ID, "agent-1",
		"I should read the file first to understand the issue."))
	time.Sleep(150 * time.Millisecond)

	tl.Add(tasklog.NewAgentLogEndEntry(thinking.ID, "agent-1"))
	stack.Pop()
	time.Sleep(200 * time.Millisecond)

	// 3. tool_use begin -> send.
	toolUse := tasklog.NewAgentLogEntry("agent-1",
		tasklog.WithKind("tool_use"),
		tasklog.WithParentLogID(stack.CurrentParentID()),
	)
	toolUse.Body = "read_file"
	tl.Add(toolUse)
	stack.Push(toolUse.ID)
	time.Sleep(100 * time.Millisecond)

	tl.Add(tasklog.NewAgentLogSendEntry(toolUse.ID, "agent-1",
		`{"path": "main.go", "start_line": 1, "end_line": 50}`))
	time.Sleep(200 * time.Millisecond)

	// 4. tool_result begin -> send -> end.
	toolResult := tasklog.NewAgentLogEntry("agent-1",
		tasklog.WithKind("tool_result"),
		tasklog.WithParentLogID(stack.CurrentParentID()),
	)
	tl.Add(toolResult)
	time.Sleep(100 * time.Millisecond)

	tl.Add(tasklog.NewAgentLogSendEntry(toolResult.ID, "agent-1",
		"package main\n\nimport \"fmt\"\n\nfunc main() {\n    fmt.Println(\"Hello\")\n}"))
	time.Sleep(200 * time.Millisecond)

	tl.Add(tasklog.NewAgentLogEndEntry(toolResult.ID, "agent-1"))
	time.Sleep(100 * time.Millisecond)

	// 5. tool_use end.
	tl.Add(tasklog.NewAgentLogEndEntry(toolUse.ID, "agent-1"))
	stack.Pop()
	time.Sleep(200 * time.Millisecond)

	// 6. Second tool_use (write_file).
	toolUse2 := tasklog.NewAgentLogEntry("agent-1",
		tasklog.WithKind("tool_use"),
		tasklog.WithParentLogID(stack.CurrentParentID()),
	)
	toolUse2.Body = "write_file"
	tl.Add(toolUse2)
	stack.Push(toolUse2.ID)
	time.Sleep(100 * time.Millisecond)

	tl.Add(tasklog.NewAgentLogSendEntry(toolUse2.ID, "agent-1",
		`{"path": "main.go", "content": "package main\n\nfunc main() { ... }"}`))
	time.Sleep(300 * time.Millisecond)

	toolResult2 := tasklog.NewAgentLogEntry("agent-1",
		tasklog.WithKind("tool_result"),
		tasklog.WithParentLogID(stack.CurrentParentID()),
	)
	tl.Add(toolResult2)
	tl.Add(tasklog.NewAgentLogSendEntry(toolResult2.ID, "agent-1", "File written successfully"))
	tl.Add(tasklog.NewAgentLogEndEntry(toolResult2.ID, "agent-1"))
	time.Sleep(100 * time.Millisecond)

	tl.Add(tasklog.NewAgentLogEndEntry(toolUse2.ID, "agent-1"))
	stack.Pop()
	time.Sleep(200 * time.Millisecond)

	// 7. Root end.
	tl.Add(tasklog.NewAgentLogEndEntry(root.ID, "agent-1"))
	stack.Pop()
	time.Sleep(500 * time.Millisecond)

	// Round 2: Error scenario.
	root2 := tasklog.NewAgentLogEntry("agent-1", tasklog.WithKind("text"))
	root2.Body = "Processing next request..."
	tl.Add(root2)
	time.Sleep(300 * time.Millisecond)

	errLog := tasklog.NewAgentLogEntry("agent-1",
		tasklog.WithKind("error"),
		tasklog.WithParentLogID(root2.ID),
	)
	errLog.Body = "Connection to LLM provider failed: timeout"
	tl.Add(errLog)
	tl.Add(tasklog.NewAgentLogEndEntry(errLog.ID, "agent-1"))
	time.Sleep(200 * time.Millisecond)

	// Abnormal termination.
	tl.Add(tasklog.NewTerminatedEntry("agent-1", "provider timeout"))

	fmt.Println("\n--- Simulation complete ---")
}
