package codex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/axsh/hag/codingagent"
	"github.com/axsh/hag/logger"
)

const gracefulShutdownTimeout = 5 * time.Second

// ProcessManager manages a Codex CLI subprocess.
type ProcessManager struct {
	cmd        *exec.Cmd
	cancel     context.CancelFunc
	configPath string // temporary config.toml path to clean up
	logger     logger.Logger
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
	env["OPENAI_API_KEY"] = apiKey + ";fallback=" + fallbackStr + ";sid=" + sid

	// Session data storage directory override.
	if cfg.SessionDir != "" {
		env["CODEX_HOME"] = cfg.SessionDir
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

	// R6: Initialize logger.
	log := ac.Logger
	if log == nil {
		log = logger.NewDefault(logger.LevelInfo)
	}
	log = log.WithComponent("codex")

	args := BuildArgs(configPath)
	log.Debug("building CLI arguments", "args", args)

	env := BuildEnv(ac, cfg)
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

	cmd := exec.CommandContext(procCtx, "codex", args...)
	cmd.Dir = cfg.WorkDir
	cmd.Env = append(cmd.Environ(), env...)

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

	// R3: Capture stderr for diagnostics.
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	log.Info("starting codex CLI process", "work_dir", cfg.WorkDir, "model", cfg.Model)
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, nil, fmt.Errorf("start codex: %w", err)
	}

	ch := make(chan codingagent.StreamEvent, 64)
	pm := &ProcessManager{cmd: cmd, cancel: cancel, configPath: configPath, logger: log}

	// Send initialize + startThread requests via stdin.
	go func() {
		initReq, _ := BuildInitializeRequest()
		stdin.Write(initReq)
		stdin.Write([]byte("\n"))

		threadReq, _ := BuildStartThreadRequest(cfg.Prompt)
		stdin.Write(threadReq)
		stdin.Write([]byte("\n"))

		// R7: Close stdin to signal EOF to Codex CLI.
		stdin.Close()
	}()

	// Read JSON-RPC 2.0 notifications from stdout.
	go func() {
		defer close(ch)
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}
			log.Trace("CLI stdout line", "line", line)

			// Check for approval requests and auto-approve.
			var msg JSONRPCMessage
			if err := json.Unmarshal([]byte(line), &msg); err == nil {
				if IsApprovalRequest(&msg) {
					resp, _ := BuildApprovalResponse(*msg.ID)
					// Re-open stdin is not possible after Close,
					// but approval_request requires stdin write.
					// Note: stdin is already closed via R7.
					// In practice, Codex CLI auto-approves in full-auto mode
					// when using the config.toml approach. This path is kept
					// for compatibility but may not be reached.
					log.Debug("approval request received (stdin closed, may not respond)", "id", *msg.ID)
					_ = resp
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

// Stop gracefully terminates the subprocess and cleans up the config file.
// 1. Send SIGTERM (Unix) or Kill (Windows)
// 2. Wait up to 5 seconds for exit
// 3. Force kill if timeout
// 4. Clean up temporary config.toml
func (pm *ProcessManager) Stop() error {
	if pm.cmd.Process == nil {
		return nil
	}

	pm.logger.Debug("stopping codex CLI process")

	var err error
	if runtime.GOOS == "windows" {
		pm.cancel()
		err = pm.cmd.Wait()
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

	// Clean up temporary config.toml
	if pm.configPath != "" {
		os.RemoveAll(strings.TrimSuffix(pm.configPath, "/config.toml"))
	}
	return err
}
