// config-dir-switch demonstrates switching config_dir on the same Tern session
// without terminate, then continuing the conversation on the next SendText.
//
// Prerequisites:
//   - A running tern server
//   - Vault keys and claude/codex CLI for the chosen agent
//
// Usage:
//
//	go run . [flags]
//
// Examples:
//
//	go run .
//	go run . --agent codex
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	client "github.com/axsh/arctic-tern/client/v1"
)

const (
	markerAlpha = "TERN_CONFIG_ALPHA"
	markerBeta  = "TERN_CONFIG_BETA"
)

type runFlags struct {
	Server         string
	Agent          string
	Model          string
	WorkDir        string
	SessionDir     string
	ConfigDirAlpha string
	ConfigDirBeta  string
	Prompt1        string
	Prompt2        string
}

func parseFlags(args []string) (*runFlags, error) {
	fs := flag.NewFlagSet("config-dir-switch", flag.ContinueOnError)
	f := &runFlags{}
	fs.StringVar(&f.Server, "server", "http://localhost:3100", "Tern server URL")
	fs.StringVar(&f.Agent, "agent", "claudecode", "Agent name (claudecode|codex)")
	fs.StringVar(&f.Model, "model", "", "Optional model name")
	fs.StringVar(&f.WorkDir, "work-dir", ".", "Work directory")
	fs.StringVar(&f.SessionDir, "session-dir", "", "Session directory (default: temp)")
	fs.StringVar(&f.ConfigDirAlpha, "config-dir-alpha", "", "Alpha config_dir (default: temp with marker)")
	fs.StringVar(&f.ConfigDirBeta, "config-dir-beta", "", "Beta config_dir (default: temp with marker)")
	fs.StringVar(&f.Prompt1, "prompt1", "Reply with exactly the word turn-1 and nothing else.", "First turn prompt")
	fs.StringVar(&f.Prompt2, "prompt2", "", "Second turn prompt (default depends on agent markers)")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	return f, nil
}

func markerFileName(agent string) string {
	if strings.EqualFold(agent, "codex") {
		return "AGENTS.md"
	}
	return "CLAUDE.md"
}

// prepareConfigPair creates alpha/beta config directories with distinct markers.
// When alphaPath or betaPath is non-empty, that path is used instead of a temp dir.
func prepareConfigPair(baseDir, agent, alphaPath, betaPath string) (alpha, beta string, err error) {
	name := markerFileName(agent)
	write := func(dir, marker string) error {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, name), []byte(marker+"\n"), 0o644)
	}

	alpha = alphaPath
	if alpha == "" {
		alpha = filepath.Join(baseDir, "config-alpha")
	}
	beta = betaPath
	if beta == "" {
		beta = filepath.Join(baseDir, "config-beta")
	}
	if err := write(alpha, markerAlpha); err != nil {
		return "", "", err
	}
	if err := write(beta, markerBeta); err != nil {
		return "", "", err
	}
	return alpha, beta, nil
}

func defaultPrompt2(agent string) string {
	name := markerFileName(agent)
	return fmt.Sprintf(
		"What single word did you reply with in the previous turn? Also quote the marker string from %s if present.",
		name,
	)
}

func main() {
	f, err := parseFlags(os.Args[1:])
	if err != nil {
		os.Exit(2)
	}
	if err := run(f); err != nil {
		log.Fatalf("%v", err)
	}
}

func run(f *runFlags) error {
	ctx := context.Background()

	base := filepath.Join(os.TempDir(), fmt.Sprintf("tern-config-dir-switch-%d", os.Getpid()))
	if err := os.MkdirAll(base, 0o755); err != nil {
		return fmt.Errorf("create temp base: %w", err)
	}

	sessionDir := f.SessionDir
	if sessionDir == "" {
		sessionDir = filepath.Join(base, "session")
		if err := os.MkdirAll(sessionDir, 0o755); err != nil {
			return fmt.Errorf("create session dir: %w", err)
		}
	}

	alphaDir, betaDir, err := prepareConfigPair(base, f.Agent, f.ConfigDirAlpha, f.ConfigDirBeta)
	if err != nil {
		return fmt.Errorf("prepare config dirs: %w", err)
	}

	prompt2 := f.Prompt2
	if prompt2 == "" {
		prompt2 = defaultPrompt2(f.Agent)
	}

	c := client.New(f.Server, client.WithNoTimeout())

	session, err := c.CreateSession(ctx, client.SessionRequest{
		Agent:      f.Agent,
		Model:      f.Model,
		WorkDir:    f.WorkDir,
		SessionDir: sessionDir,
		ConfigDir:  alphaDir,
	})
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	sessionID := session.ID
	log.Printf("session_id=%s config_dir=alpha (%s)", sessionID, alphaDir)
	defer func() {
		if err := session.Terminate(ctx); err != nil {
			log.Printf("terminate cleanup: %v", err)
		}
	}()

	stream1, err := session.SendText(ctx, f.Prompt1)
	if err != nil {
		return fmt.Errorf("send turn1: %w", err)
	}
	log.Printf("--- turn 1 ---")
	if err := stream1.Output(os.Stdout); err != nil {
		return fmt.Errorf("stream turn1: %w", err)
	}
	fmt.Println()

	// Switch config_dir on the same session; do not Terminate between turns.
	info, err := session.UpdateConfigDir(ctx, betaDir)
	if err != nil {
		return fmt.Errorf("update config_dir: %w", err)
	}
	if info.ID != sessionID {
		return fmt.Errorf("session id changed after PATCH: got %q want %q", info.ID, sessionID)
	}
	if info.SessionDir != "" && info.SessionDir != sessionDir {
		return fmt.Errorf("session_dir changed after PATCH: got %q want %q", info.SessionDir, sessionDir)
	}
	if info.ConfigDir != betaDir {
		return fmt.Errorf("config_dir after PATCH = %q, want %q", info.ConfigDir, betaDir)
	}
	log.Printf("patched config_dir=%s session_id=%s session_dir=%s (unchanged id)", info.ConfigDir, info.ID, info.SessionDir)

	got, err := c.GetSession(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("get session: %w", err)
	}
	if got.ConfigDir != betaDir || got.ID != sessionID {
		return fmt.Errorf("GetSession mismatch: id=%q config_dir=%q", got.ID, got.ConfigDir)
	}
	log.Printf("GetSession ok: id=%s config_dir=%s", got.ID, got.ConfigDir)

	stream2, err := session.SendText(ctx, prompt2)
	if err != nil {
		return fmt.Errorf("send turn2: %w", err)
	}
	log.Printf("--- turn 2 ---")
	if err := stream2.Output(os.Stdout); err != nil {
		return fmt.Errorf("stream turn2: %w", err)
	}
	fmt.Println()
	log.Printf("done: same session_id=%s continued after config_dir switch", sessionID)
	return nil
}
