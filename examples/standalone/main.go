package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

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

	ctx := context.Background()

	// Launch server
	if err := srv.Launch(ctx); err != nil {
		log.Fatalf("failed to launch HAG server: %v", err)
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
