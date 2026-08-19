// session-follow is a Client API example. It talks HTTP to a separate Tern
// process (default http://localhost:3100) using github.com/axsh/arctic-tern/client/v1.
// It does not embed server.New. After SendText it drops SSE and reattaches with
// Follow / FollowFrom (GET /api/v1/sessions/{id}/events), without a second message.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	client "github.com/axsh/arctic-tern/client/v1"
)

const defaultEssayPrompt = "Write a long, multi-paragraph explanation of TCP congestion control, then list three takeaways."

const holdPromptTemplate = "Run a shell command that sleeps for %d seconds (python -c with time.sleep(%d) or POSIX sleep %d). Do not answer before the sleep finishes. After it finishes, reply with exactly one short sentence. Do not ask questions. Do not write a long essay before the tool call."

const (
	followMaxAttempts = 10
	followRetryWait   = 200 * time.Millisecond
)

type runFlags struct {
	Server      string
	Agent       string
	Model       string
	WorkDir     string
	SessionDir  string
	Prompt      string
	DropAfter   int
	Respond     string
	HoldSeconds int
}

type demoOutcome struct {
	SessionID  string
	DropLastID string
	FollowMode string
	Status     string
	Followable bool
	TurnID     string
	SawResult  bool
}

func holdPrompt(n int) string {
	return fmt.Sprintf(holdPromptTemplate, n, n, n)
}

func applyPromptDefaults(f *runFlags) {
	if f.Prompt != "" {
		return
	}
	if f.HoldSeconds <= 0 {
		f.Prompt = defaultEssayPrompt
		return
	}
	f.Prompt = holdPrompt(f.HoldSeconds)
}

func parseFlags(args []string) (*runFlags, error) {
	fs := flag.NewFlagSet("session-follow", flag.ContinueOnError)
	f := &runFlags{}
	fs.StringVar(&f.Server, "server", "http://localhost:3100", "Tern server URL")
	fs.StringVar(&f.Agent, "agent", "claudecode", "Agent name (claudecode|codex)")
	fs.StringVar(&f.Model, "model", "", "Optional model name")
	fs.StringVar(&f.WorkDir, "work-dir", ".", "Work directory")
	fs.StringVar(&f.SessionDir, "session-dir", "", "Session directory (default: temp)")
	fs.StringVar(&f.Prompt, "prompt", "", "First SendText prompt (default: sleep hold-seconds then one sentence)")
	fs.IntVar(&f.HoldSeconds, "hold-seconds", 60, "Seconds to sleep in the default prompt; 0 uses the essay prompt")
	fs.IntVar(&f.DropAfter, "drop-after", 1, "Drop SSE after this many logical events (id: lines); 0 drops immediately")
	fs.StringVar(&f.Respond, "respond", "yes", "Fixed answer for user_input_required (empty: exit with error)")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	return f, nil
}

func main() {
	f, err := parseFlags(os.Args[1:])
	if err != nil {
		os.Exit(2)
	}
	applyPromptDefaults(f)
	c := client.New(f.Server, client.WithNoTimeout())
	if _, err := runFollowDemo(context.Background(), f, c, log.Printf); err != nil {
		log.Fatalf("%v", err)
	}
}

func runFollowDemo(ctx context.Context, f *runFlags, c *client.Client, logf func(string, ...any)) (demoOutcome, error) {
	var out demoOutcome

	sessionDir := f.SessionDir
	if sessionDir == "" {
		dir, err := os.MkdirTemp("", "session-follow-")
		if err != nil {
			return out, fmt.Errorf("create session dir: %w", err)
		}
		sessionDir = dir
	}

	session, err := c.CreateSession(ctx, client.SessionRequest{
		Agent:      f.Agent,
		Model:      f.Model,
		WorkDir:    f.WorkDir,
		SessionDir: sessionDir,
	})
	if err != nil {
		return out, fmt.Errorf("create session: %w", err)
	}
	out.SessionID = session.ID
	logf("session_id=%s", session.ID)
	defer func() {
		if terr := session.Terminate(ctx); terr != nil {
			logf("terminate cleanup: %v", terr)
		}
	}()

	sendCtx, cancelSend := context.WithCancel(ctx)
	defer cancelSend()

	stream, err := session.SendText(sendCtx, f.Prompt)
	if err != nil {
		return out, fmt.Errorf("send text: %w", err)
	}

	lastID, finishedBeforeDrop, err := consumeUntilDrop(cancelSend, stream, f.DropAfter)
	if err != nil {
		return out, err
	}
	if finishedBeforeDrop {
		return out, fmt.Errorf("turn finished before drop")
	}
	out.DropLastID = lastID
	logf("drop last_event_id=%q", lastID)

	info, err := c.GetSession(ctx, session.ID)
	if err != nil {
		return out, fmt.Errorf("get session: %w", err)
	}
	out.Status = info.Status
	out.Followable = info.Followable
	out.TurnID = info.TurnID
	logf("GetSession status=%s followable=%v turn_id=%s", info.Status, info.Followable, info.TurnID)

	if info.Status == "completed" {
		return out, fmt.Errorf("session completed after drop; follow not attempted")
	}
	if info.Status == "error" {
		return out, fmt.Errorf("session error, skip follow: %s", info.Error)
	}
	canFollow := info.Followable || info.Status == "active" || info.Status == "suspended"
	if !canFollow {
		return out, fmt.Errorf("session not followable: status=%s followable=%v", info.Status, info.Followable)
	}

	for attempt := 1; attempt <= followMaxAttempts; attempt++ {
		var followStream *client.Stream
		if lastID != "" {
			out.FollowMode = "FollowFrom"
			logf("follow mode=FollowFrom from=%s", lastID)
			followStream, err = session.FollowFrom(ctx, lastID)
		} else {
			out.FollowMode = "Follow"
			logf("follow mode=Follow")
			followStream, err = session.Follow(ctx)
		}
		if err != nil {
			if isFollowNoActiveTurn(err) {
				logf("follow 409 no active turn (attempt %d/%d); retry GetSession", attempt, followMaxAttempts)
				time.Sleep(followRetryWait)
				info, err = c.GetSession(ctx, session.ID)
				if err != nil {
					return out, fmt.Errorf("get session after 409: %w", err)
				}
				if info.Status == "completed" {
					return out, fmt.Errorf("session completed during follow retry without result")
				}
				if info.Followable || info.Status == "active" || info.Status == "suspended" {
					continue
				}
				return out, fmt.Errorf("session not followable after 409: status=%s", info.Status)
			}
			return out, fmt.Errorf("follow: %w", err)
		}

		saw, err := consumeThroughResult(ctx, session, followStream, f.Respond, logf)
		if err != nil {
			return out, err
		}
		if !saw {
			return out, fmt.Errorf("follow finished without result")
		}
		out.SawResult = true
		logf("follow saw result=true")
		return out, nil
	}
	return out, fmt.Errorf("follow retries exhausted")
}

func consumeUntilDrop(cancel context.CancelFunc, stream *client.Stream, dropAfter int) (lastID string, finishedBeforeDrop bool, err error) {
	dropped := false
	recordedID := ""
	if dropAfter <= 0 {
		cancel()
		dropped = true
	}
	logical := 0
	for ev := range stream.Events() {
		switch ev.Type {
		case client.EventResult:
			if !dropped {
				finishedBeforeDrop = true
				cancel()
				dropped = true
			}
		case client.EventError:
			if dropped || isIntentionalDropError(ev.Error) {
				continue
			}
			return "", false, fmt.Errorf("%s", ev.Error)
		}
		if ev.ID != "" && !dropped {
			logical++
			if logical >= dropAfter {
				recordedID = ev.ID
				cancel()
				dropped = true
			}
		}
	}
	return recordedID, finishedBeforeDrop, nil
}

func consumeThroughResult(ctx context.Context, sess *client.Session, stream *client.Stream, respond string, logf func(string, ...any)) (bool, error) {
	sawResult := false
	cur := stream
	for {
		for ev := range cur.Events() {
			switch ev.Type {
			case client.EventUserInputRequired:
				if respond == "" {
					return sawResult, fmt.Errorf("user input required; pass -respond")
				}
				next, err := sess.Respond(ctx, respond)
				if err != nil {
					return sawResult, fmt.Errorf("respond: %w", err)
				}
				cur = next
				goto nextStream
			case client.EventResult:
				sawResult = true
			case client.EventError:
				return sawResult, fmt.Errorf("%s", ev.Error)
			default:
				if ev.Type == client.EventText && ev.Text != "" {
					logf("follow text: %s", ev.Text)
				}
			}
		}
		return sawResult, nil
	nextStream:
	}
}

func isFollowNoActiveTurn(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "HTTP 409") && strings.Contains(msg, "no active turn")
}

func isIntentionalDropError(msg string) bool {
	return strings.Contains(msg, "stream terminated unexpectedly without completion marker") ||
		strings.Contains(msg, "stream read error") ||
		strings.Contains(msg, "context canceled")
}
