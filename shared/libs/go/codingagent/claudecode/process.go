package claudecode

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/logger"
)

const (
	gracefulShutdownTimeout = 5 * time.Second
	defaultIdleTimeoutSec   = 300
	defaultMaxExecutionSec  = 3600
)

// ProcessManager manages a Claude CLI subprocess.
type ProcessManager struct {
	cmd         *exec.Cmd
	cancel      context.CancelFunc
	logger      logger.Logger
	stderrBuf   *bytes.Buffer
	stdinWriter io.WriteCloser
	stdinMu     sync.Mutex
}

// WriteStdin writes additional input to the running CLI process.
func (pm *ProcessManager) WriteStdin(text string) error {
	pm.stdinMu.Lock()
	defer pm.stdinMu.Unlock()
	if pm.stdinWriter == nil {
		return fmt.Errorf("stdin not available (single_shot or closed)")
	}
	if _, err := io.WriteString(pm.stdinWriter, text); err != nil {
		return err
	}
	if _, err := io.WriteString(pm.stdinWriter, "\n"); err != nil {
		return err
	}
	return nil
}

func (pm *ProcessManager) closeStdin() {
	pm.stdinMu.Lock()
	defer pm.stdinMu.Unlock()
	if pm.stdinWriter != nil {
		pm.stdinWriter.Close()
		pm.stdinWriter = nil
	}
}

func resolveTimeouts(cfg *codingagent.SessionConfig) (idleSec, maxSec int) {
	idleSec = cfg.IdleTimeoutSeconds
	if idleSec == 0 {
		idleSec = defaultIdleTimeoutSec
	}
	maxSec = cfg.MaxExecutionSeconds
	if maxSec == 0 {
		maxSec = defaultMaxExecutionSec
	}
	return idleSec, maxSec
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
		env["ANTHROPIC_API_KEY"] = apiKey + tokenPart + ";fallback=" + fallbackStr + ";sid=" + sid + codingagent.MeteringMetaSuffix(cfg.TernSessionID, cfg.TurnID)
	}

	if ac.DisableSandbox {
		env["CLAUDE_CODE_SKIP_SANDBOX"] = "1"
	}
	if cfg.SandboxMode != "" {
		if codingagent.SandboxModeDisablesSandbox(cfg.SandboxMode) {
			env["CLAUDE_CODE_SKIP_SANDBOX"] = "1"
		} else {
			delete(env, "CLAUDE_CODE_SKIP_SANDBOX")
		}
	}

	// Session data storage directory override.
	if cfg.SessionDir != "" {
		env["CLAUDE_CONFIG_DIR"] = codingagent.ConvertToLinuxPath(cfg.SessionDir)
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

	distro, linuxWorkDir, isWSL := codingagent.ParseWSLPath(cfg.WorkDir)
	var cmd *exec.Cmd
	if isWSL {
		if err := codingagent.VerifyWSLRuntime(procCtx, distro, "claude"); err != nil {
			cancel()
			return nil, nil, err
		}

		disableSandbox := ac.DisableSandbox
		if cfg.SandboxMode != "" {
			disableSandbox = codingagent.SandboxModeDisablesSandbox(cfg.SandboxMode)
		}
		builder := &codingagent.WSLCommandBuilder{
			Distro:         distro,
			WorkDir:        linuxWorkDir,
			Command:        "claude",
			Args:           args,
			Env:            env,
			DisableSandbox: disableSandbox,
		}
		cmd = builder.BuildCmd(procCtx)
	} else {
		cmd = exec.CommandContext(procCtx, "claude", args...)
		cmd.Dir = cfg.WorkDir
		cmd.Env = append(cmd.Environ(), env...)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, nil, fmt.Errorf("stdout pipe: %w", err)
	}

	// Capture stderr via pipe for real-time debug logging.
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, nil, fmt.Errorf("stderr pipe: %w", err)
	}

	var stderrBuf bytes.Buffer
	stderrDone := make(chan struct{})
	ch := make(chan codingagent.StreamEvent, 64)
	var lastActivity atomic.Int64
	lastActivity.Store(time.Now().UnixNano())
	touchActivity := func() { lastActivity.Store(time.Now().UnixNano()) }

	interactive := cfg.ExecutionMode != codingagent.ExecutionModeSingleShot
	var stdinReader io.Reader
	var stdinWriter io.WriteCloser
	if interactive {
		var sw io.WriteCloser
		stdinReader, sw = io.Pipe()
		stdinWriter = sw
	} else {
		stdinReader = bytes.NewReader(nil)
	}

	go func() {
		defer close(stderrDone)
		scanner := codingagent.NewLargeLineScanner(stderrPipe, cfg.ScannerMaxTokenBytes)
		for scanner.Scan() {
			line := scanner.Text()
			touchActivity()
			stderrBuf.WriteString(line)
			stderrBuf.WriteString("\n")
			log.Debug("CLI stderr line", "line", line)
		}
		if err := scanner.Err(); err != nil {
			log.Warn("claude CLI stderr scanner error", "error", err.Error())
		}
	}()

	cmd.Stdin = stdinReader

	log.Info("starting claude CLI process", "work_dir", cfg.WorkDir, "model", cfg.Model)
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, nil, fmt.Errorf("start claude: %w", err)
	}

	pm := &ProcessManager{
		cmd:         cmd,
		cancel:      cancel,
		logger:      log,
		stderrBuf:   &stderrBuf,
		stdinWriter: stdinWriter,
	}

	idleSec, maxSec := resolveTimeouts(cfg)
	startedAt := time.Now()
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-procCtx.Done():
				return
			case <-ticker.C:
				if time.Since(startedAt) > time.Duration(maxSec)*time.Second {
					emitTimeout(ch, procCtx, fmt.Sprintf("agent max execution timeout after %ds", maxSec))
					pm.Stop()
					return
				}
				last := time.Unix(0, lastActivity.Load())
				if time.Since(last) > time.Duration(idleSec)*time.Second {
					emitTimeout(ch, procCtx, fmt.Sprintf("agent idle timeout after %ds", idleSec))
					pm.Stop()
					return
				}
			}
		}
	}()

	go func() {
		defer close(ch)
		defer cancel() // Option A: cancel procCtx when stdout closes so the timeout goroutine exits cleanly
		scanner := codingagent.NewLargeLineScanner(stdout, cfg.ScannerMaxTokenBytes)
		resultEmitted := false
		for scanner.Scan() {
			line := scanner.Text()
			touchActivity()
			log.Trace("CLI stdout line", "line", line)
			events := ParseJSONLinesEvents(line, log)
			for _, ev := range events {
				if ev == nil {
					continue
				}
				if ev.Type == codingagent.EventToolResult {
					ev.Content = codingagent.TruncateToolResult(ev.Content, cfg.MaxToolResultBytes)
				}
				if ev.Type == codingagent.EventResult {
					resultEmitted = true
				}
				select {
				case ch <- *ev:
				case <-procCtx.Done():
					return
				}
			}
		}

		if err := scanner.Err(); err != nil {
			log.Warn("claude stdout scanner error", "error", err.Error())
			select {
			case ch <- codingagent.StreamEvent{
				Type:    codingagent.EventError,
				Content: "stdout read error: " + err.Error(),
			}:
			case <-procCtx.Done():
			}
		}

		// Wait for stderr scanner to finish before calling cmd.Wait().
		<-stderrDone

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
			if !resultEmitted {
				log.Debug("emitting synthetic EventResult after successful exit")
				select {
				case ch <- codingagent.StreamEvent{Type: codingagent.EventResult}:
				case <-procCtx.Done():
				}
			}
		}
	}()

	return ch, pm, nil
}

func emitTimeout(ch chan<- codingagent.StreamEvent, ctx context.Context, msg string) {
	defer func() { recover() }() // Option B: absorb panic if ch is already closed
	select {
	case ch <- codingagent.StreamEvent{Type: codingagent.EventError, Content: msg}:
	case <-ctx.Done():
	}
}

// Stop gracefully terminates the subprocess.
// 1. Send SIGTERM (Unix) or Kill (Windows)
// 2. Wait up to 5 seconds for exit
// 3. Force kill if timeout
func (pm *ProcessManager) Stop() error {
	pm.closeStdin()
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
			pm.logger.Debug("process cleanup: attempted precautionary kill, but process had already exited", "pid", pid)
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
