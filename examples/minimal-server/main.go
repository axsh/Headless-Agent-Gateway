// minimal-server demonstrates the simplest way to start a tern server.
// It loads configuration from a YAML file and starts the server with
// automatic coding agent registration via init() imports.
//
// Usage:
//
//	go run . -config config.yaml
//
// Examples:
//
//	# Run with default config.yaml in current directory
//	go run .
//
//	# Run with a specific config file
//	go run . -config ./settings/example/config.yaml
//
//	# Run the built binary
//	./bin/minimal-server -config ./settings/example/config.yaml
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/axsh/arctic-tern/server"
)

func main() {
	// Default config path; override with -config flag.
	configPath := "config.yaml"
	if len(os.Args) > 2 && os.Args[1] == "-config" {
		configPath = os.Args[2]
	}

	// Initialize the server with the given config.
	// server.New automatically registers all built-in coding agents
	// (Claude Code, Codex, Wayfinder, etc.) and LLM providers
	// (OpenAI, Anthropic, Google, Ollama) via init() imports.
	//
	// Optional: specify which API versions to enable:
	//   srv, err := server.New(server.WithConfigPath(configPath), server.WithEnableVersion(1))
	srv, err := server.New(server.WithConfigPath(configPath))
	if err != nil {
		log.Fatalf("failed to initialize: %v", err)
	}

	// Launch starts the CAWA Agent Service and LLM Gateway.
	ctx := context.Background()
	if err := srv.Launch(ctx); err != nil {
		log.Fatalf("failed to launch: %v", err)
	}
	defer srv.Shutdown(ctx)

	fmt.Printf("tern server running on http://localhost:%d\n", srv.AgentService().Port())

	// Wait for interrupt signal to gracefully shut down.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	fmt.Println("shutting down...")
}
