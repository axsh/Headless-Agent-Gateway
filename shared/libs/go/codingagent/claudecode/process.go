package claudecode

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/axsh/hag/codingagent"
)

const gracefulShutdownTimeout = 5 * time.Second

// ProcessManager manages a Claude CLI subprocess.
type ProcessManager struct {
	cmd    *exec.Cmd
	cancel context.CancelFunc
}

// BuildArgs constructs claude CLI arguments from SessionConfig.
func BuildArgs(cfg *codingagent.SessionConfig) []string {
	args := []string{
		"--bare",
		"--output-format", "stream-json",
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
	if cfg.MaxTurns > 0 {
		args = append(args, "--max-turns", strconv.Itoa(cfg.MaxTurns))
	}
	return args
}

// BuildEnv constructs environment variables from AdapterConfig and SessionConfig.
func BuildEnv(ac *codingagent.AdapterConfig, cfg *codingagent.SessionConfig) []string {
	env := make(map[string]string)

	if ac.GatewayURL != "" {
		env["ANTHROPIC_BASE_URL"] = ac.GatewayURL
		// Gateway handles auth; CLI needs a non-empty key to proceed.
		env["ANTHROPIC_API_KEY"] = "not-needed"
	}

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

	// R3: Capture stderr for diagnostics.
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	// R7: Suppress stdin warning by providing an empty reader that returns EOF immediately.
	cmd.Stdin = bytes.NewReader(nil)

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
		// R4: Check exit code and report stderr on failure.
		if err := cmd.Wait(); err != nil {
			errMsg := strings.TrimSpace(stderrBuf.String())
			if errMsg == "" {
				errMsg = err.Error()
			}
			select {
			case ch <- codingagent.StreamEvent{
				Type:    codingagent.EventError,
				Content: errMsg,
			}:
			case <-procCtx.Done():
			}
		}
	}()

	return ch, pm, nil
}

// Stop gracefully terminates the subprocess.
// 1. Send SIGTERM (Unix) or Kill (Windows)
// 2. Wait up to 5 seconds for exit
// 3. Force kill if timeout
func (pm *ProcessManager) Stop() error {
	if pm.cmd.Process == nil {
		return nil
	}

	// Windows: no SIGTERM, just kill
	if runtime.GOOS == "windows" {
		pm.cancel()
		return pm.cmd.Wait()
	}

	// Unix: send SIGTERM first
	pm.cmd.Process.Signal(syscall.SIGTERM)

	done := make(chan error, 1)
	go func() { done <- pm.cmd.Wait() }()

	select {
	case err := <-done:
		return err
	case <-time.After(gracefulShutdownTimeout):
		// Force kill
		pm.cancel()
		return <-done
	}
}

