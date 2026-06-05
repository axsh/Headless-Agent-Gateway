package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/axsh/hag/codingagent"
)

// ProcessManager manages a Codex CLI subprocess.
type ProcessManager struct {
	cmd        *exec.Cmd
	cancel     context.CancelFunc
	configPath string // temporary config.toml path to clean up
}

// BuildArgs constructs codex CLI arguments from the config path.
func BuildArgs(configPath string) []string {
	args := []string{
		"--config", configPath,
	}
	return args
}

// BuildEnv constructs environment variables from AdapterConfig and SessionConfig.
func BuildEnv(ac *codingagent.AdapterConfig, cfg *codingagent.SessionConfig) []string {
	env := make(map[string]string)

	// Codex CLI requires OPENAI_API_KEY; gateway handles auth, so set placeholder.
	env["OPENAI_API_KEY"] = "not-needed"

	for k, v := range cfg.EnvVars {
		env[k] = v
	}

	var result []string
	for k, v := range env {
		result = append(result, k+"="+v)
	}
	return result
}

// StartProcess launches codex CLI as a subprocess.
// It sends JSON-RPC 2.0 initialize and startThread requests via stdin,
// and reads JSON-RPC 2.0 notifications from stdout.
func StartProcess(
	ctx context.Context,
	ac *codingagent.AdapterConfig,
	cfg *codingagent.SessionConfig,
	configPath string,
) (<-chan codingagent.StreamEvent, *ProcessManager, error) {
	procCtx, cancel := context.WithCancel(ctx)

	args := BuildArgs(configPath)
	cmd := exec.CommandContext(procCtx, "codex", args...)
	cmd.Dir = cfg.WorkDir
	cmd.Env = append(cmd.Environ(), BuildEnv(ac, cfg)...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, nil, fmt.Errorf("stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, nil, fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, nil, fmt.Errorf("start codex: %w", err)
	}

	ch := make(chan codingagent.StreamEvent, 64)
	pm := &ProcessManager{cmd: cmd, cancel: cancel, configPath: configPath}

	// Send initialize + startThread requests
	go func() {
		initReq, _ := BuildInitializeRequest()
		stdin.Write(initReq)
		stdin.Write([]byte("\n"))

		threadReq, _ := BuildStartThreadRequest(cfg.Prompt)
		stdin.Write(threadReq)
		stdin.Write([]byte("\n"))
	}()

	// Read JSON-RPC 2.0 notifications from stdout
	go func() {
		defer close(ch)
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}

			// Check for approval requests and auto-approve
			var msg JSONRPCMessage
			if err := json.Unmarshal([]byte(line), &msg); err == nil {
				if IsApprovalRequest(&msg) {
					resp, _ := BuildApprovalResponse(*msg.ID)
					stdin.Write(resp)
					stdin.Write([]byte("\n"))
					continue
				}
			}

			ev := ParseNotification(line)
			if ev != nil {
				select {
				case ch <- *ev:
				case <-procCtx.Done():
					return
				}
			}
		}
	}()

	return ch, pm, nil
}

// Stop terminates the subprocess and cleans up the config file.
func (pm *ProcessManager) Stop() error {
	pm.cancel()
	err := pm.cmd.Wait()
	// Clean up temporary config.toml
	if pm.configPath != "" {
		os.RemoveAll(strings.TrimSuffix(pm.configPath, "/config.toml"))
	}
	return err
}
