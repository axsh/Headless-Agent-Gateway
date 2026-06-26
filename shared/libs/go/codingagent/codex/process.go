package codex

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/logger"
)

const gracefulShutdownTimeout = 5 * time.Second

// ProcessManager manages a Codex CLI subprocess.
type ProcessManager struct {
	cmd       *exec.Cmd
	cancel    context.CancelFunc
	codexHome string // temporary CODEX_HOME directory to clean up
	logger    logger.Logger
}

// BuildArgs constructs codex CLI arguments for non-interactive execution.
// Uses "codex exec --json" with config overrides via -c flags.
func BuildArgs(prompt string, configOverrides []string) []string {
	args := []string{
		"exec",
		"--json",
		"--dangerously-bypass-approvals-and-sandbox",
		"--ignore-user-config",
	}
	args = append(args, configOverrides...)
	args = append(args, prompt)
	return args
}

// BuildEnv constructs environment variables from AdapterConfig and SessionConfig.
func BuildEnv(ac *codingagent.AdapterConfig, cfg *codingagent.SessionConfig) []string {
	env := make(map[string]string)

	// R4: Build OPENAI_API_KEY with metadata for gateway session tracking.
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
		env["TERN_GATEWAY_TOKEN"] = ac.GatewayToken
	}
	env["OPENAI_API_KEY"] = apiKey + tokenPart + ";fallback=" + fallbackStr + ";sid=" + sid

	// Session data storage directory override.
	if cfg.SessionDir != "" {
		env["CODEX_HOME"] = cfg.SessionDir
	}

	if runtime.GOOS == "windows" {
		// Suppress Git Bash path conversion behavior for subprocesses.
		env["MSYS_NO_PATHCONV"] = "1"
		// Force Codex to use Windows native cmd.exe instead of bash under Git Bash.
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

// StartProcess launches codex CLI as a subprocess using "codex exec --json".
// It reads JSONL events from stdout and converts them to StreamEvents.
func StartProcess(
	ctx context.Context,
	ac *codingagent.AdapterConfig,
	cfg *codingagent.SessionConfig,
	configOverrides []string,
	codexHome string,
) (<-chan codingagent.StreamEvent, *ProcessManager, error) {
	procCtx, cancel := context.WithCancel(ctx)

	// R6: Initialize logger.
	log := ac.Logger
	if log == nil {
		log = logger.NewDefault(logger.LevelInfo)
	}
	log = log.WithComponent("codex")

	args := BuildArgs(cfg.Prompt, configOverrides)
	log.Debug("building CLI arguments", "args", args)

	env := BuildEnv(ac, cfg)

	// Set CODEX_HOME for session/auth data (not config - config is via -c flags).
	codexHomeSet := false
	for _, e := range env {
		if strings.HasPrefix(e, "CODEX_HOME=") {
			codexHomeSet = true
			break
		}
	}
	if !codexHomeSet && codexHome != "" {
		env = append(env, "CODEX_HOME="+codexHome)
	}

	// Ensure CODEX_HOME directory exists (codex CLI requires it to be pre-created).
	for i, e := range env {
		if strings.HasPrefix(e, "CODEX_HOME=") {
			home := strings.TrimPrefix(e, "CODEX_HOME=")
			// Resolve relative path to absolute to avoid confusion with cmd.Dir.
			if !filepath.IsAbs(home) {
				if abs, err := filepath.Abs(home); err == nil {
					home = abs
					env[i] = "CODEX_HOME=" + home
				}
			}
			if err := os.MkdirAll(home, 0755); err != nil {
				log.Warn("failed to create CODEX_HOME directory", "path", home, "error", err)
			}
			break
		}
	}

	// R6: Log masked environment variables.
	var maskedEnv []string
	for _, envVar := range env {
		if strings.HasPrefix(envVar, "OPENAI_API_KEY=") {
			maskedEnv = append(maskedEnv, "OPENAI_API_KEY=****")
		} else {
			maskedEnv = append(maskedEnv, envVar)
		}
	}
	log.Trace("CLI environment variables", "env", maskedEnv)

	distro, linuxWorkDir, isWSL := codingagent.ParseWSLPath(cfg.WorkDir)
	var cmd *exec.Cmd
	if isWSL {
		if err := codingagent.VerifyWSLRuntime(procCtx, distro, "codex"); err != nil {
			cancel()
			return nil, nil, err
		}

		builder := &codingagent.WSLCommandBuilder{
			Distro:         distro,
			WorkDir:        linuxWorkDir,
			Command:        "codex",
			Args:           args,
			Env:            env,
			DisableSandbox: ac.DisableSandbox,
		}
		cmd = builder.BuildCmd(procCtx)
	} else {
		cmd = exec.CommandContext(procCtx, "codex", args...)
		cmd.Dir = cfg.WorkDir
		cmd.Env = append(cmd.Environ(), env...)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, nil, fmt.Errorf("stdout pipe: %w", err)
	}

	// Suppress stdin warning/blocking by providing an empty reader that returns EOF immediately.
	cmd.Stdin = bytes.NewReader(nil)

	// R3: Capture stderr for diagnostics.
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	log.Info("starting codex CLI process", "work_dir", cfg.WorkDir, "model", cfg.Model)
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, nil, fmt.Errorf("start codex: %w", err)
	}

	ch := make(chan codingagent.StreamEvent, 64)
	pm := &ProcessManager{cmd: cmd, cancel: cancel, codexHome: codexHome, logger: log}

	// Read JSONL events from stdout (codex exec --json outputs JSONL).
	go func() {
		defer close(ch)
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}
			log.Trace("CLI stdout line", "line", line)

			ev := ParseExecEvent(line)
			if ev != nil {
				select {
				case ch <- *ev:
				case <-procCtx.Done():
					return
				}
			} else {
				log.Trace("unhandled codex event type (ignored)", "line", line)
			}
		}

		// R3: Check exit code and report stderr on failure.
		if err := cmd.Wait(); err != nil {
			errMsg := strings.TrimSpace(stderrBuf.String())
			if errMsg == "" {
				errMsg = err.Error()
			}
			log.Warn("codex CLI process exited with error", "error", err.Error(), "stderr", errMsg)
			select {
			case ch <- codingagent.StreamEvent{
				Type:    codingagent.EventError,
				Content: errMsg,
			}:
			case <-procCtx.Done():
			}
		} else {
			exitCode := cmd.ProcessState.ExitCode()
			log.Debug("codex CLI process exited", "exit_code", exitCode)
		}
	}()

	return ch, pm, nil
}

// Stop gracefully terminates the subprocess and cleans up the codex home.
// 1. Send SIGTERM (Unix) or Kill (Windows)
// 2. Wait up to 5 seconds for exit
// 3. Force kill if timeout
// 4. Clean up temporary CODEX_HOME directory
func (pm *ProcessManager) Stop() error {
	if pm.cmd.Process == nil {
		return nil
	}

	pm.logger.Debug("stopping codex CLI process")

	var err error
	if runtime.GOOS == "windows" {
		pid := pm.cmd.Process.Pid
		pm.logger.Debug("killing process tree on Windows", "pid", pid)
		killCmd := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(pid))
		if killErr := killCmd.Run(); killErr != nil {
			pm.logger.Debug("taskkill failed (process may have already exited)", "error", killErr)
		}
		pm.cancel()
		err = nil
	} else {
		pm.cmd.Process.Signal(syscall.SIGTERM)
		done := make(chan error, 1)
		go func() { done <- pm.cmd.Wait() }()
		select {
		case err = <-done:
		case <-time.After(gracefulShutdownTimeout):
			pm.logger.Debug("graceful shutdown timed out, killing process")
			pm.cancel()
			err = <-done
		}
	}

	// Clean up temporary CODEX_HOME directory.
	if pm.codexHome != "" {
		os.RemoveAll(pm.codexHome)
	}
	return err
}
