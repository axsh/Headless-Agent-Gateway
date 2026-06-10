package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/axsh/hag/logger"
)

var (
	serverURL string
	log       logger.Logger
)

func main() {
	logLevel := flag.String("log-level", "info", "cawa-client log level (trace, debug, info, warn, error)")
	flag.StringVar(&serverURL, "server", "http://localhost:3100", "CAWA server URL")
	flag.Parse()

	lvl := logger.ParseLevel(*logLevel)
	log = logger.NewDefault(lvl).WithComponent("cawa-client")

	args := flag.Args()
	if len(args) == 0 {
		printUsage()
		os.Exit(1)
	}

	switch args[0] {
	case "health":
		cmdHealth()
	case "agents":
		cmdAgents()
	case "models":
		cmdModels()
	case "run":
		cmdRun(args[1:])
	case "session":
		cmdSession(args[1:])
	case "logs":
		cmdLogs(args[1:])
	case "terminate":
		cmdTerminate(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", args[0])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("Usage: cawa-client [--server URL] [--log-level LEVEL] <command> [args...]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  health                                Check server health")
	fmt.Println("  agents                                List available agents")
	fmt.Println("  models                                List available models")
	fmt.Println("  run --agent NAME --prompt MSG          Create session and run")
	fmt.Println("      [--session-dir DIR]                Session storage directory")
	fmt.Println("  run --resume ID --prompt MSG           Continue existing session")
	fmt.Println("  session --id ID                        Get session status")
	fmt.Println("  logs --id ID                           Stream session logs")
	fmt.Println("  terminate --id ID                      Terminate session")
}

// cmdHealth calls GET /health and displays the result.
func cmdHealth() {
	log.Debug("sending health check request", "url", serverURL+"/health")
	resp, err := http.Get(serverURL + "/health")
	if err != nil {
		log.Error("health check request failed", "error", err.Error())
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	log.Debug("health check response received", "status", resp.StatusCode)

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Error("failed to read health response body", "error", err.Error())
		os.Exit(1)
	}
	log.Trace("health check response body", "body", string(bodyBytes))

	var health map[string]any
	if err := json.Unmarshal(bodyBytes, &health); err == nil {
		out, _ := json.MarshalIndent(health, "", "  ")
		fmt.Println(string(out))
	} else {
		log.Error("failed to decode health response", "error", err.Error())
	}
}

// cmdAgents calls GET /api/v1/agents and displays the result.
func cmdAgents() {
	log.Debug("fetching agents list", "url", serverURL+"/api/v1/agents")
	resp, err := http.Get(serverURL + "/api/v1/agents")
	if err != nil {
		log.Error("failed to fetch agents list", "error", err.Error())
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	log.Debug("agents list response received", "status", resp.StatusCode)

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Error("failed to read agents response body", "error", err.Error())
		os.Exit(1)
	}
	log.Trace("agents list response body", "body", string(bodyBytes))

	var agents []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(bodyBytes, &agents); err != nil {
		log.Error("failed to decode agents response", "error", err.Error())
		fmt.Fprintf(os.Stderr, "Error decoding agents: %v\n", err)
		os.Exit(1)
	}
	for _, a := range agents {
		fmt.Println(a.Name)
	}
}

// cmdModels calls GET /api/v1/models and displays available models.
func cmdModels() {
	log.Debug("fetching models list", "url", serverURL+"/api/v1/models")
	resp, err := http.Get(serverURL + "/api/v1/models")
	if err != nil {
		log.Error("failed to fetch models list", "error", err.Error())
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	log.Debug("models list response received", "status", resp.StatusCode)

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Error("failed to read models response body", "error", err.Error())
		os.Exit(1)
	}
	log.Trace("models list response body", "body", string(bodyBytes))

	var body struct {
		Models []struct {
			Provider string `json:"provider"`
			Model    string `json:"model"`
		} `json:"models"`
		DefaultModel *struct {
			Provider string `json:"provider"`
			Model    string `json:"model"`
		} `json:"default_model"`
	}
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		log.Error("failed to decode models response", "error", err.Error())
		fmt.Fprintf(os.Stderr, "Error decoding models: %v\n", err)
		os.Exit(1)
	}

	defaultModelName := ""
	if body.DefaultModel != nil {
		defaultModelName = body.DefaultModel.Model
	}

	// Group by provider.
	byProvider := make(map[string][]string)
	var providerOrder []string
	for _, m := range body.Models {
		if _, exists := byProvider[m.Provider]; !exists {
			providerOrder = append(providerOrder, m.Provider)
		}
		byProvider[m.Provider] = append(byProvider[m.Provider], m.Model)
	}

	fmt.Println("Available models:")
	for _, provider := range providerOrder {
		models := byProvider[provider]
		fmt.Printf("  %s:\n", provider)
		for _, model := range models {
			if model == defaultModelName {
				fmt.Printf("    * %s (default)\n", model)
			} else {
				fmt.Printf("    - %s\n", model)
			}
		}
	}
}

// cmdRun creates a session (or continues an existing one), sends a message via SSE, and shows the result.
func cmdRun(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	agent := fs.String("agent", "", "Agent name (required for new session)")
	model := fs.String("model", "", "Model name")
	prompt := fs.String("prompt", "", "Prompt message (required)")
	workDir := fs.String("work-dir", ".", "Working directory")
	sessionDir := fs.String("session-dir", "", "Session data storage directory (default: work-dir)")
	resumeSessionID := fs.String("resume", "", "Existing session ID (for continuation)")
	fs.Parse(args)

	if *prompt == "" {
		fmt.Fprintf(os.Stderr, "Error: --prompt is required\n")
		fs.Usage()
		os.Exit(1)
	}

	var sid string
	if *resumeSessionID != "" {
		// Continuation mode: use existing session.
		sid = *resumeSessionID
		log.Debug("continuing session", "session_id", sid)
		fmt.Printf("Continuing session: %s\n\n", sid)
	} else {
		// New session mode: --agent is required.
		if *agent == "" {
			fmt.Fprintf(os.Stderr, "Error: --agent is required for new sessions\n")
			fs.Usage()
			os.Exit(1)
		}
		sessionBody, _ := json.Marshal(map[string]string{
			"agent": *agent, "model": *model,
			"work_dir": *workDir, "session_dir": *sessionDir,
		})
		log.Debug("creating new session", "agent", *agent, "model", *model, "work_dir", *workDir)
		log.Trace("session request body", "body", string(sessionBody))

		resp, err := http.Post(serverURL+"/api/v1/sessions",
			"application/json", bytes.NewReader(sessionBody))
		if err != nil {
			log.Error("error creating session", "error", err.Error())
			fmt.Fprintf(os.Stderr, "Error creating session: %v\n", err)
			os.Exit(1)
		}
		defer resp.Body.Close()
		respBytes, _ := io.ReadAll(resp.Body)

		log.Debug("session response received", "status", resp.StatusCode)
		log.Trace("session response body", "body", string(respBytes))

		if resp.StatusCode != http.StatusCreated {
			fmt.Fprintf(os.Stderr, "Error creating session (HTTP %d):\n%s\n", resp.StatusCode, string(respBytes))
			os.Exit(1)
		}

		var created map[string]string
		json.Unmarshal(respBytes, &created)
		sid = created["session_id"]
		fmt.Printf("Session created: %s\n\n", sid)
	}

	// Send message with SSE.
	msgBody, _ := json.Marshal(map[string]string{"message": *prompt})
	req, _ := http.NewRequest("POST",
		serverURL+"/api/v1/sessions/"+sid+"/messages",
		bytes.NewReader(msgBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	log.Debug("sending message to session", "session_id", sid)
	log.Trace("message payload", "body", string(msgBody))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Error("error sending message", "error", err.Error(), "session_id", sid)
		fmt.Fprintf(os.Stderr, "Error sending message: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	log.Debug("message response received", "status", resp.StatusCode)

	// Stream SSE events.
	streamSSE(resp.Body)

	// Show final session status.
	fmt.Println()
	cmdSessionByID(sid)
}

// streamSSE reads SSE data lines and prints events to stdout.
func streamSSE(body io.Reader) {
	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		line := scanner.Text()
		log.Trace("SSE raw line received", "line", line)
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			log.Debug("SSE stream completed with DONE signal")
			fmt.Println("\n--- Stream completed ---")
			return
		}
		var ev struct {
			Type     string `json:"type"`
			Content  string `json:"content"`
			ToolName string `json:"tool_name,omitempty"`
		}
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			log.Warn("failed to unmarshal SSE event", "error", err.Error(), "data", data)
			continue
		}
		switch ev.Type {
		case "text":
			fmt.Print(ev.Content)
		case "tool_use":
			fmt.Printf("\n[Tool: %s]\n", ev.ToolName)
		case "tool_result":
			fmt.Printf("[Tool Result] %s\n", ev.Content)
		case "system":
			fmt.Printf("[System] %s\n", ev.Content)
		case "error":
			log.Error("SSE error event received", "error", ev.Content)
			fmt.Fprintf(os.Stderr, "\n[Error] %s\n", ev.Content)
		default:
			fmt.Printf("[%s] %s\n", ev.Type, ev.Content)
		}
	}
}

// cmdSession handles the session subcommand.
func cmdSession(args []string) {
	fs := flag.NewFlagSet("session", flag.ExitOnError)
	id := fs.String("id", "", "Session ID (required)")
	fs.Parse(args)
	if *id == "" {
		fmt.Fprintf(os.Stderr, "Error: --id is required\n")
		os.Exit(1)
	}
	cmdSessionByID(*id)
}

// cmdSessionByID fetches and displays a session by ID.
func cmdSessionByID(id string) {
	log.Debug("fetching session details", "session_id", id, "url", serverURL+"/api/v1/sessions/"+id)
	resp, err := http.Get(serverURL + "/api/v1/sessions/" + id)
	if err != nil {
		log.Error("failed to fetch session details", "session_id", id, "error", err.Error())
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	log.Debug("session details response received", "session_id", id, "status", resp.StatusCode)

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Error("failed to read session details response body", "session_id", id, "error", err.Error())
		os.Exit(1)
	}
	log.Trace("session details response body", "session_id", id, "body", string(bodyBytes))

	var session map[string]any
	if err := json.Unmarshal(bodyBytes, &session); err == nil {
		out, _ := json.MarshalIndent(session, "", "  ")
		fmt.Println(string(out))
	} else {
		log.Error("failed to decode session details response", "session_id", id, "error", err.Error())
	}
}

// cmdLogs handles the logs subcommand (SSE log streaming).
func cmdLogs(args []string) {
	fs := flag.NewFlagSet("logs", flag.ExitOnError)
	id := fs.String("id", "", "Session ID (required)")
	fs.Parse(args)
	if *id == "" {
		fmt.Fprintf(os.Stderr, "Error: --id is required\n")
		os.Exit(1)
	}

	log.Debug("requesting session logs stream", "session_id", *id, "url", serverURL+"/api/v1/sessions/"+*id+"/logs")
	req, _ := http.NewRequest("GET",
		serverURL+"/api/v1/sessions/"+*id+"/logs", nil)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Error("failed to open session logs stream", "session_id", *id, "error", err.Error())
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	log.Debug("session logs stream connection established", "session_id", *id, "status", resp.StatusCode)
	streamSSE(resp.Body)
}

// cmdTerminate handles the terminate subcommand.
func cmdTerminate(args []string) {
	fs := flag.NewFlagSet("terminate", flag.ExitOnError)
	id := fs.String("id", "", "Session ID (required)")
	fs.Parse(args)
	if *id == "" {
		fmt.Fprintf(os.Stderr, "Error: --id is required\n")
		os.Exit(1)
	}

	log.Debug("requesting session termination", "session_id", *id, "url", serverURL+"/api/v1/sessions/"+*id+"/terminate")
	resp, err := http.Post(
		serverURL+"/api/v1/sessions/"+*id+"/terminate",
		"application/json", nil)
	if err != nil {
		log.Error("failed to request session termination", "session_id", *id, "error", err.Error())
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	log.Debug("session termination response received", "session_id", *id, "status", resp.StatusCode)

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Error("failed to read session termination response body", "session_id", *id, "error", err.Error())
		os.Exit(1)
	}
	log.Trace("session termination response body", "session_id", *id, "body", string(bodyBytes))
	fmt.Print(string(bodyBytes))
	fmt.Println()
}
