package claudecode

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/axsh/hag/codingagent"
)

// ProcessManager manages a Claude CLI subprocess.
type ProcessManager struct {
	cmd    *exec.Cmd
	cancel context.CancelFunc
}

// BuildArgs constructs claude CLI arguments from SessionConfig.
func BuildArgs(cfg *codingagent.SessionConfig) []string {
	args := []string{
		"--output-format", "stream-json",
		"--input-format", "stream-json",
		"--verbose",
		"--permission-mode", "bypassPermissions",
	}
	if cfg.Prompt != "" {
		args = append(args, "-p", cfg.Prompt)
	}
	if cfg.Model != "" {
		args = append(args, "--model", cfg.Model)
	}
	if len(cfg.AllowedTools) > 0 {
		args = append(args, "--allowedTools", strings.Join(cfg.AllowedTools, ","))
	}
	if cfg.SDKSessionID != "" {
		args = append(args, "--session-id", cfg.SDKSessionID)
	}
	return args
}

// BuildEnv constructs environment variables from AdapterConfig and SessionConfig.
func BuildEnv(ac *codingagent.AdapterConfig, cfg *codingagent.SessionConfig) []string {
	env := make(map[string]string)

	if ac.GatewayURL != "" {
		env["ANTHROPIC_BASE_URL"] = ac.GatewayURL
	}

	// CLI requires API key but gateway handles auth, so set a placeholder.
	env["ANTHROPIC_API_KEY"] = "not-needed"

	if ac.DisableSandbox {
		env["CLAUDE_CODE_SKIP_SANDBOX"] = "1"
	}

	for k, v := range cfg.EnvVars {
		env[k] = v
	}

	var result []string
	for k, v := range env {
		result = append(result, k+"="+v)
	}
	return result
}

// StartProcess launches claude CLI as a subprocess and streams JSON Lines
// events through the returned channel.
func StartProcess(
	ctx context.Context,
	ac *codingagent.AdapterConfig,
	cfg *codingagent.SessionConfig,
) (<-chan codingagent.StreamEvent, *ProcessManager, error) {
	procCtx, cancel := context.WithCancel(ctx)

	args := BuildArgs(cfg)
	cmd := exec.CommandContext(procCtx, "claude", args...)
	cmd.Dir = cfg.WorkDir
	cmd.Env = append(cmd.Environ(), BuildEnv(ac, cfg)...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, nil, fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, nil, fmt.Errorf("start claude: %w", err)
	}

	ch := make(chan codingagent.StreamEvent, 64)
	pm := &ProcessManager{cmd: cmd, cancel: cancel}

	go func() {
		defer close(ch)
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			ev := ParseJSONLinesEvent(line)
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

// Stop terminates the subprocess.
func (pm *ProcessManager) Stop() error {
	pm.cancel()
	return pm.cmd.Wait()
}
