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

	"github.com/axsh/arctic-tern/codingagent"
	"github.com/axsh/arctic-tern/logger"
)

const gracefulShutdownTimeout = 5 * time.Second

// ProcessManager manages a Claude CLI subprocess.
type ProcessManager struct {
	cmd       *exec.Cmd
	cancel    context.CancelFunc
	logger    logger.Logger
	stderrBuf *bytes.Buffer
}

// BuildArgs constructs claude CLI arguments from SessionConfig.
func BuildArgs(cfg *codingagent.SessionConfig) []string {
	args := []string{
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
	if cfg.AgentSessionID != "" {
		args = append(args, "--resume", cfg.AgentSessionID)
	}
	// R7: Default maxTurns to 200 if not specified.
	maxTurns := cfg.MaxTurns
	if maxTurns == 0 {
		maxTurns = 200
	}
	args = append(args, "--max-turns", strconv.Itoa(maxTurns))
	return args
}

// BuildEnv constructs environment variables from AdapterConfig and SessionConfig.
func BuildEnv(ac *codingagent.AdapterConfig, cfg *codingagent.SessionConfig) []string {
	env := make(map[string]string)

	if ac.GatewayURL != "" {
		env["ANTHROPIC_BASE_URL"] = ac.GatewayURL

		// R4: Build API key with metadata for gateway.
		apiKey := "not-needed"
		fallbackStr := "false"
		if ac.ToolCallFallback {
			fallbackStr = "true"
		}
		sid := cfg.AgentSessionID
		if sid == "" {
			sid = "default"
		}
		tokenPart := ""
		if ac.GatewayToken != "" {
			tokenPart = ";token=" + ac.GatewayToken
		}
		env["ANTHROPIC_API_KEY"] = apiKey + tokenPart + ";fallback=" + fallbackStr + ";sid=" + sid
	}

	if ac.DisableSandbox {
		env["CLAUDE_CODE_SKIP_SANDBOX"] = "1"
	}

	// Session data storage directory override.
	if cfg.SessionDir != "" {
		env["CLAUDE_CONFIG_DIR"] = cfg.SessionDir
	}

	if runtime.GOOS == "windows" {
		// Suppress Git Bash path conversion behavior for subprocesses.
		env["MSYS_NO_PATHCONV"] = "1"
		// Force Claude Code to use Windows native cmd.exe instead of bash under Git Bash.
		// This resolves path mapping issues (e.g. /tmp/...) by avoiding msys-style shell outputs.
		env["SHELL"] = "C:\\Windows\\System32\\cmd.exe"
		env["COMSPEC"] = "C:\\Windows\\System32\\cmd.exe"
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

	log := ac.Logger
	if log == nil {
		log = logger.NewDefault(logger.LevelInfo)
	}
	log = log.WithComponent("claudecode")

	args := BuildArgs(cfg)
	log.Debug("building CLI arguments", "args", args)

	env := BuildEnv(ac, cfg)
	var maskedEnv []string
	for _, envVar := range env {
		if strings.HasPrefix(envVar, "ANTHROPIC_API_KEY=") {
			maskedEnv = append(maskedEnv, "ANTHROPIC_API_KEY=****")
		} else {
			maskedEnv = append(maskedEnv, envVar)
		}
	}
	log.Trace("CLI environment variables", "env", maskedEnv)

	cmd := exec.CommandContext(procCtx, "claude", args...)
	cmd.Dir = cfg.WorkDir
	cmd.Env = append(cmd.Environ(), env...)

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

	log.Info("starting claude CLI process", "work_dir", cfg.WorkDir, "model", cfg.Model)
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, nil, fmt.Errorf("start claude: %w", err)
	}

	ch := make(chan codingagent.StreamEvent, 64)
	pm := &ProcessManager{cmd: cmd, cancel: cancel, logger: log, stderrBuf: &stderrBuf}

	go func() {
		defer close(ch)
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			log.Trace("CLI stdout line", "line", line)
			ev := ParseJSONLinesEvent(line, log)
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
			log.Warn("claude CLI process exited with error", "error", err.Error(), "stderr", errMsg)
			select {
			case ch <- codingagent.StreamEvent{
				Type:    codingagent.EventError,
				Content: errMsg,
			}:
			case <-procCtx.Done():
			}
		} else {
			exitCode := cmd.ProcessState.ExitCode()
			log.Debug("claude CLI process exited", "exit_code", exitCode)
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

	pm.logger.Debug("stopping claude CLI process")

	if pm.stderrBuf != nil {
		stderrStr := strings.TrimSpace(pm.stderrBuf.String())
		if stderrStr != "" {
			fmt.Printf("[DEBUG-CLAUDE-STOP-STDERR] session_pid=%d stderr:\n%s\n", pm.cmd.Process.Pid, stderrStr)
		}
	}

	// Windows: no SIGTERM, just kill
	if runtime.GOOS == "windows" {
		pid := pm.cmd.Process.Pid
		pm.logger.Debug("killing process tree on Windows", "pid", pid)
		killCmd := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(pid))
		if killErr := killCmd.Run(); killErr != nil {
			pm.logger.Debug("taskkill failed (process may have already exited)", "error", killErr)
		}
		pm.cancel()
		return nil
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
		pm.logger.Debug("graceful shutdown timed out, killing process")
		pm.cancel()
		return <-done
	}
}

