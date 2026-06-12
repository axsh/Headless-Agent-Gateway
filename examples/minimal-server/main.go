// minimal-server demonstrates the simplest way to start a tern server.
// It loads configuration from a YAML file and starts the server with
// automatic coding agent registration via init() imports.
//
// Usage:
//
//	go run . -config config.yaml
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/axsh/arctic-tern/tern"

	// Auto-register all built-in coding agents.
	_ "github.com/axsh/arctic-tern/codingagent/all"
)

func main() {
	configPath := "config.yaml"
	if len(os.Args) > 2 && os.Args[1] == "-config" {
		configPath = os.Args[2]
	}

	srv, err := tern.New(tern.WithConfigPath(configPath))
	if err != nil {
		log.Fatalf("failed to initialize: %v", err)
	}

	ctx := context.Background()
	if err := srv.Launch(ctx); err != nil {
		log.Fatalf("failed to launch: %v", err)
	}
	defer srv.Shutdown(ctx)

	fmt.Printf("tern server running on http://localhost:%d\n", srv.AgentService().Port())

	// Wait for interrupt signal.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	fmt.Println("shutting down...")
}
