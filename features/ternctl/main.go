package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	client "github.com/axsh/arctic-tern/client/v1"
	"github.com/axsh/arctic-tern/shared/libs/go/logger"
)

var (
	serverURL string
	log       logger.Logger
)

func main() {
	logLevel := flag.String("log-level", "info", "ternctl log level (trace, debug, info, warn, error)")
	flag.StringVar(&serverURL, "server", "http://localhost:3100", "CAWA server URL")
	flag.Parse()

	lvl := logger.ParseLevel(*logLevel)
	log = logger.NewDefault(lvl).WithComponent("ternctl")

	args := flag.Args()
	if len(args) == 0 {
		printUsage()
		os.Exit(1)
	}

	c := client.New(serverURL, client.WithNoTimeout())

	switch args[0] {
	case "health":
		cmdHealth(c)
	case "agents":
		cmdAgents(c)
	case "models":
		cmdModels(c)
	case "run":
		cmdRun(c, args[1:])
	case "session":
		cmdSession(c, args[1:])
	case "terminate":
		cmdTerminate(c, args[1:])
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", args[0])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("Usage: ternctl [--server URL] [--log-level LEVEL] <command> [args...]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  health                                Check server health")
	fmt.Println("  agents                                List available agents")
	fmt.Println("  models                                List available models")
	fmt.Println("  run --agent NAME --prompt MSG          Create session and run")
	fmt.Println("      [--session-dir DIR]                Session storage directory")
	fmt.Println("      [--config-dir DIR]                 Agent config set directory (skills/rules)")
	fmt.Println("  run --resume ID --prompt MSG           Continue existing session")
	fmt.Println("  session --id ID                        Get session status")
	fmt.Println("  terminate --id ID                      Terminate session")
}

func cmdHealth(c *client.Client) {
	ctx := context.Background()
	health, err := c.Health(ctx)
	if err != nil {
		log.Error("health check failed", "error", err.Error())
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	out, _ := json.MarshalIndent(health, "", "  ")
	fmt.Println(string(out))
}

func cmdAgents(c *client.Client) {
	ctx := context.Background()
	agents, err := c.ListAgents(ctx)
	if err != nil {
		log.Error("failed to list agents", "error", err.Error())
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	for _, a := range agents {
		fmt.Println(a.Name)
	}
}

func cmdModels(c *client.Client) {
	ctx := context.Background()
	models, err := c.ListModels(ctx)
	if err != nil {
		log.Error("failed to list models", "error", err.Error())
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	defaultModelName := ""
	if models.DefaultModel != nil {
		defaultModelName = models.DefaultModel.Model
	}

	// Group by provider.
	byProvider := make(map[string][]string)
	var providerOrder []string
	for _, m := range models.Models {
		if _, exists := byProvider[m.Provider]; !exists {
			providerOrder = append(providerOrder, m.Provider)
		}
		byProvider[m.Provider] = append(byProvider[m.Provider], m.Model)
	}

	fmt.Println("Available models:")
	for _, provider := range providerOrder {
		pmodels := byProvider[provider]
		fmt.Printf("  %s:\n", provider)
		for _, model := range pmodels {
			if model == defaultModelName {
				fmt.Printf("    * %s (default)\n", model)
			} else {
				fmt.Printf("    - %s\n", model)
			}
		}
	}
}

func cmdRun(c *client.Client, args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	agent := fs.String("agent", "", "Agent name (required for new session)")
	model := fs.String("model", "", "Model name")
	prompt := fs.String("prompt", "", "Prompt message (required)")
	workDir := fs.String("work-dir", ".", "Working directory")
	sessionDir := fs.String("session-dir", "", "Session data storage directory (default: work-dir)")
	configDir := fs.String("config-dir", "", "Agent config set directory (skills/rules); overlaid into session-dir")
	resumeSessionID := fs.String("resume", "", "Existing session ID (for continuation)")
	fs.Parse(args)

	if *prompt == "" {
		fmt.Fprintf(os.Stderr, "Error: --prompt is required\n")
		fs.Usage()
		os.Exit(1)
	}

	ctx := context.Background()
	var session *client.Session
	var err error

	if *resumeSessionID != "" {
		// Continuation mode: wrap existing session ID.
		session = client.ResumeSession(c, *resumeSessionID)
		log.Debug("continuing session", "session_id", session.ID)
		fmt.Printf("Continuing session: %s\n\n", session.ID)
	} else {
		if *agent == "" {
			fmt.Fprintf(os.Stderr, "Error: --agent is required for new sessions\n")
			fs.Usage()
			os.Exit(1)
		}
		session, err = c.CreateSession(ctx, client.SessionRequest{
			Agent:      *agent,
			Model:      *model,
			WorkDir:    *workDir,
			SessionDir: *sessionDir,
			ConfigDir:  *configDir,
		})
		if err != nil {
			log.Error("error creating session", "error", err.Error())
			fmt.Fprintf(os.Stderr, "Error creating session: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Session created: %s\n\n", session.ID)
	}

	// Send message and stream output.
	stream, err := session.SendText(ctx, *prompt)
	if err != nil {
		log.Error("error sending message", "error", err.Error(), "session_id", session.ID)
		fmt.Fprintf(os.Stderr, "Error sending message: %v\n", err)
		os.Exit(1)
	}

	streamErr := stream.Output(os.Stdout)
	fmt.Println()

	// Show final session status.
	details, err := c.GetSession(ctx, session.ID)
	if err == nil {
		out, _ := json.MarshalIndent(details, "", "  ")
		fmt.Println(string(out))

		// Warn if session did not complete successfully.
		if status, ok := details["status"].(string); ok && status != "completed" {
			fmt.Fprintf(os.Stderr, "\nWarning: session ended with status %q (expected \"completed\")\n", status)
			if errMsg, ok := details["error"].(string); ok && errMsg != "" {
				fmt.Fprintf(os.Stderr, "Error details: %s\n", errMsg)
			}
		}
	}

	if streamErr != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", streamErr)
		os.Exit(1)
	}
}

func cmdSession(c *client.Client, args []string) {
	fs := flag.NewFlagSet("session", flag.ExitOnError)
	id := fs.String("id", "", "Session ID (required)")
	fs.Parse(args)
	if *id == "" {
		fmt.Fprintf(os.Stderr, "Error: --id is required\n")
		os.Exit(1)
	}

	ctx := context.Background()
	details, err := c.GetSession(ctx, *id)
	if err != nil {
		log.Error("failed to get session", "session_id", *id, "error", err.Error())
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	out, _ := json.MarshalIndent(details, "", "  ")
	fmt.Println(string(out))

	if status, ok := details["status"].(string); ok && status == "error" {
		errMsg := "unknown error"
		if msg, ok := details["error"].(string); ok && msg != "" {
			errMsg = msg
		}
		fmt.Fprintf(os.Stderr, "Session failed with error: %s\n", errMsg)
		os.Exit(1)
	}
}

func cmdTerminate(c *client.Client, args []string) {
	fs := flag.NewFlagSet("terminate", flag.ExitOnError)
	id := fs.String("id", "", "Session ID (required)")
	fs.Parse(args)
	if *id == "" {
		fmt.Fprintf(os.Stderr, "Error: --id is required\n")
		os.Exit(1)
	}

	ctx := context.Background()
	session := client.ResumeSession(c, *id)
	if err := session.Terminate(ctx); err != nil {
		log.Error("failed to terminate session", "session_id", *id, "error", err.Error())
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Session terminated")
}
