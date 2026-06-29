// minimal-client demonstrates the simplest way to interact with a running
// tern server using the client library. It creates a session, sends a message,
// and streams the response to stdout.
//
// Prerequisites:
//   - A running tern server (e.g., via minimal-server)
//   - For the wayfinder agent: Ollama running with qwen2.5-coder:7b model
//
// Usage:
//
//	go run . [server-url]
//
// Examples:
//
//	# Connect to default server (localhost:3100)
//	go run .
//
//	# Connect to a remote server
//	go run . http://192.168.1.100:3100
//
//	# Run the built binary
//	./bin/minimal-client http://localhost:3100
package main

import (
	"context"
	"log"
	"os"

	client "github.com/axsh/arctic-tern/client/v1"
)

func main() {
	// Default server URL; override with first argument.
	serverURL := "http://localhost:3100"
	if len(os.Args) > 1 {
		serverURL = os.Args[1]
	}

	ctx := context.Background()

	// Create a new client pointing to the tern server.
	c := client.New(serverURL)

	// Create a session with the wayfinder agent and a local Ollama model.
	// Agent: "wayfinder" - Tern's built-in coding agent
	// Model: "qwen2.5-coder:7b" - A local model running on Ollama
	session, err := c.CreateSession(ctx, client.SessionRequest{
		Agent:   "wayfinder",
		Model:   "qwen2.5-coder:7b",
		WorkDir: ".",
	})
	if err != nil {
		log.Fatalf("create session: %v", err)
	}
	defer session.Terminate(ctx)
	log.Printf("Session: %s", session.ID)

	// Send a text message and stream the response to stdout.
	stream, err := session.SendText(ctx, "Create a file called hello.txt with the content 'Hello, World!'")
	if err != nil {
		log.Fatalf("send message: %v", err)
	}

	// Output prints each streamed event to the provided writer.
	if err := stream.Output(os.Stdout); err != nil {
		log.Fatalf("stream output: %v", err)
	}
}
