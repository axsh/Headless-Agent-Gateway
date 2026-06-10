package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/axsh/hag/codingagent"
	"github.com/axsh/hag/codingagent/claudecode"
	"github.com/axsh/hag/hag"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to HAG configuration file")
	flag.Parse()

	// Initialize HAG Server using WithConfigPath
	srv, err := hag.New(hag.WithConfigPath(*configPath))
	if err != nil {
		log.Fatalf("failed to initialize HAG server: %v", err)
	}

	// Register coding agents before Launch (so HTTPHandler has agents available).
	registerCodingAgents(srv)

	ctx := context.Background()

	// Launch server (starts Gateway HTTP, AgentService, WebSocket).
	if err := srv.Launch(ctx); err != nil {
		log.Fatalf("failed to launch HAG server: %v", err)
	}

	// Fetch and cache model list AFTER Launch (Gateway must be serving).
	if err := srv.AgentService().FetchModelsFromGateway(); err != nil {
		fmt.Printf("Warning: failed to fetch models from gateway: %v\n", err)
	}

	fmt.Println("HAG server started and running...")

	// Listen for OS signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigChan
	fmt.Printf("Received signal %v, shutting down gracefully...\n", sig)

	// Create a timeout context for shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("graceful shutdown failed: %v", err)
	}

	fmt.Println("HAG server stopped.")
}

// registerCodingAgents registers coding agent adapters with the AgentService.
// Agents are only registered if their CLI tool is available on PATH.
func registerCodingAgents(srv *hag.Server) {
	if _, err := exec.LookPath("claude"); err == nil {
		gwURL := srv.Gateway().ProxyURL()

		// Resolve default model and behavior from Gateway (model_profiles.yaml).
		defaultModel := ""
		toolCallFallback := false
		if dm := srv.Gateway().DefaultModel(); dm != nil {
			defaultModel = dm.Model
			toolCallFallback = dm.ToolCallFallback
		}

		adapter := claudecode.New(&codingagent.AdapterConfig{
			GatewayURL:       gwURL,
			DefaultModel:     defaultModel,
			ToolCallFallback: toolCallFallback,
		})
		srv.AgentService().RegisterAgent(adapter)

		fmt.Printf("Registered coding agent: claudecode (gateway=%s, default_model=%s, fallback=%v)\n",
			gwURL, defaultModel, toolCallFallback)
	} else {
		fmt.Println("Warning: claude CLI not found, claudecode agent not registered")
	}
}


