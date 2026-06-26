package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/axsh/arctic-tern/server"
)

// runServer is the main entry point for the tern command.
func runServer(cmd *cobra.Command, args []string) error {
	srv, err := server.New(server.WithConfigPath(cfgFile))
	if err != nil {
		return fmt.Errorf("failed to initialize tern server: %w", err)
	}

	ctx := context.Background()

	if err := srv.Launch(ctx); err != nil {
		return fmt.Errorf("failed to launch tern server: %w", err)
	}

	// Fetch and cache model list after Launch (Gateway must be serving).
	if err := srv.AgentService().FetchModelsFromGateway(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to fetch models from gateway: %v\n", err)
	}

	fmt.Println("tern server started and running...")

	// Listen for OS signals.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigChan
	fmt.Printf("Received signal %v, shutting down gracefully...\n", sig)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown failed: %w", err)
	}

	fmt.Println("tern server stopped.")
	return nil
}
