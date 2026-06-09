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
)

var serverURL string

func main() {
	flag.StringVar(&serverURL, "server", "http://localhost:3100", "CAWA server URL")
	flag.Parse()

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
	fmt.Println("Usage: cawa-client [--server URL] <command> [args...]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  health                        Check server health")
	fmt.Println("  agents                        List available agents")
	fmt.Println("  models                        List available models")
	fmt.Println("  run --agent NAME --prompt MSG  Create session and run")
	fmt.Println("  session --id ID               Get session status")
	fmt.Println("  logs --id ID                  Stream session logs")
	fmt.Println("  terminate --id ID             Terminate session")
}

// cmdHealth calls GET /health and displays the result.
func cmdHealth() {
	resp, err := http.Get(serverURL + "/health")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	fmt.Printf("Status: %d\n", resp.StatusCode)

	var health map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&health); err == nil {
		out, _ := json.MarshalIndent(health, "", "  ")
		fmt.Println(string(out))
	}
}

// cmdAgents calls GET /api/v1/agents and displays the result.
func cmdAgents() {
	resp, err := http.Get(serverURL + "/api/v1/agents")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	var agents []struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&agents); err != nil {
		fmt.Fprintf(os.Stderr, "Error decoding agents: %v\n", err)
		os.Exit(1)
	}
	for _, a := range agents {
		fmt.Println(a.Name)
	}
}

// cmdModels calls GET /api/v1/models and displays available models.
func cmdModels() {
	resp, err := http.Get(serverURL + "/api/v1/models")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

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
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
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

// cmdRun creates a session, sends a message via SSE, and shows the result.
func cmdRun(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	agent := fs.String("agent", "", "Agent name (required)")
	model := fs.String("model", "", "Model name")
	prompt := fs.String("prompt", "", "Prompt message (required)")
	workDir := fs.String("work-dir", ".", "Working directory")
	fs.Parse(args)

	if *agent == "" || *prompt == "" {
		fmt.Fprintf(os.Stderr, "Error: --agent and --prompt are required\n")
		fs.Usage()
		os.Exit(1)
	}

	// 1. Create session
	sessionBody, _ := json.Marshal(map[string]string{
		"agent": *agent, "model": *model, "work_dir": *workDir,
	})
	resp, err := http.Post(serverURL+"/api/v1/sessions",
		"application/json", bytes.NewReader(sessionBody))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating session: %v\n", err)
		os.Exit(1)
	}
	var created map[string]string
	json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()
	sessionID := created["session_id"]
	fmt.Printf("Session created: %s\n\n", sessionID)

	// 2. Send message with SSE
	msgBody, _ := json.Marshal(map[string]string{"message": *prompt})
	req, _ := http.NewRequest("POST",
		serverURL+"/api/v1/sessions/"+sessionID+"/messages",
		bytes.NewReader(msgBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error sending message: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	// 3. Stream SSE events
	streamSSE(resp.Body)

	// 4. Show final session status
	fmt.Println()
	cmdSessionByID(sessionID)
}

// streamSSE reads SSE data lines and prints events to stdout.
func streamSSE(body io.Reader) {
	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			fmt.Println("\n--- Stream completed ---")
			return
		}
		var ev struct {
			Type     string `json:"type"`
			Content  string `json:"content"`
			ToolName string `json:"tool_name,omitempty"`
		}
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
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
	resp, err := http.Get(serverURL + "/api/v1/sessions/" + id)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	var session map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&session); err == nil {
		out, _ := json.MarshalIndent(session, "", "  ")
		fmt.Println(string(out))
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

	req, _ := http.NewRequest("GET",
		serverURL+"/api/v1/sessions/"+*id+"/logs", nil)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
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

	resp, err := http.Post(
		serverURL+"/api/v1/sessions/"+*id+"/terminate",
		"application/json", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	io.Copy(os.Stdout, resp.Body)
	fmt.Println()
}
