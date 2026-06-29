// multimodal-client demonstrates how to interact with a running tern server
// using the client library to send a multimodal message (text + image).
//
// Prerequisites:
//   - A running tern server (e.g., via minimal-server)
//   - An agent that supports multimodal (e.g., claudecode with sonnet model)
//
// Usage:
//
//	go run . [options]
//
// Examples:
//
//	# Send a query with a local image (using claudecode agent)
//	go run . -image screenshot.png -prompt "Describe this image"
//
//	# Connect to a custom server URL
//	go run . -server http://192.168.1.100:3100 -image screenshot.png
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	client "github.com/axsh/arctic-tern/client/v1"
)

// 1x1 red transparent PNG image (dummy fallback)
const dummyPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

func main() {
	serverURL := flag.String("server", "http://localhost:3100", "Tern server URL")
	imagePath := flag.String("image", "", "Path to the image file (optional, uses dummy image if empty)")
	prompt := flag.String("prompt", "Describe what you see in this image.", "Text prompt to send alongside the image")
	agent := flag.String("agent", "claudecode", "Coding agent to use")
	flag.Parse()

	ctx := context.Background()

	// Create a new client pointing to the tern server.
	c := client.New(*serverURL)

	// Create a session with the specified agent.
	session, err := c.CreateSession(ctx, client.SessionRequest{
		Agent:   *agent,
		WorkDir: ".",
	})
	if err != nil {
		log.Fatalf("failed to create session: %v", err)
	}
	defer session.Terminate(ctx)
	fmt.Printf("Session created: %s\n", session.ID)

	var stream *client.Stream
	if *imagePath != "" {
		// Use specialized helper SendImageFile
		stream, err = session.SendImageFile(ctx, *imagePath, *prompt)
	} else {
		// Use Message Builder to send a fallback query with base64 data
		parts, buildErr := client.NewMessage().
			Text(*prompt).
			ImageBase64("image/png", dummyPNG).
			Build()
		if buildErr != nil {
			log.Fatalf("failed to build message: %v", buildErr)
		}
		stream, err = session.SendMessage(ctx, parts)
	}

	if err != nil {
		log.Fatalf("failed to send message: %v", err)
	}

	// Output prints each streamed event to the provided writer.
	if err := stream.Output(os.Stdout); err != nil {
		log.Fatalf("stream output error: %v", err)
	}
}
