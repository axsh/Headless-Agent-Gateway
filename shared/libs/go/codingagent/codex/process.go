package codex

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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

// ProcessManager manages a Codex CLI subprocess.
type ProcessManager struct {
	cmd         *exec.Cmd
	cancel      context.CancelFunc
	codexHome   string // temporary CODEX_HOME directory to clean up
	logger      logger.Logger
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

// BuildArgs constructs codex CLI arguments for non-interactive execution.
// Uses "codex exec --json" with config overrides via -c flags.
// When ignoreUserConfig is true, appends --ignore-user-config (legacy default).
// When false (config_dir active), omits it so overlayed $CODEX_HOME/config.toml
// and skills under CODEX_HOME can load; -c overrides still win for Tern keys.
// When resumeSessionID != "", uses "codex exec resume <id>" to continue a thread.
// When prompt is non-empty, "-" is appended to instruct codex to read from stdin.
func BuildArgs(prompt string, configOverrides []string, ignoreUserConfig bool, resumeSessionID string, disableSandbox bool) []string {
	common := []string{
		"--json",
	}
	if disableSandbox {
		common = append(common, "--dangerously-bypass-approvals-and-sandbox")
	}
	if ignoreUserConfig {
		common = append(common, "--ignore-user-config")
	}
	common = append(common, configOverrides...)

	var args []string
	if resumeSessionID != "" {
		args = append([]string{"exec", "resume"}, common...)
		args = append(args, resumeSessionID)
	} else {
		args = append([]string{"exec"}, common...)
	}
	if prompt != "" {
		args = append(args, "-")
	}
	return args
}

// BuildEnv constructs environment variables from AdapterConfig and SessionConfig.
func BuildEnv(ac *codingagent.AdapterConfig, cfg *codingagent.SessionConfig) []string {
	env := make(map[string]string)

	// R4: Build OPENAI_API_KEY with metadata for gateway session tracking.
	apiKey := "tern-internal-key-placeholder"
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

	ignoreUserConfig := cfg.ConfigDir == ""
	args := BuildArgs(cfg.Prompt, configOverrides, ignoreUserConfig, cfg.AgentSessionID, ac.DisableSandbox)
	log.Debug("building CLI arguments", "args", args, "ignore_user_config", ignoreUserConfig, "config_dir", cfg.ConfigDir, "resume_session_id", cfg.AgentSessionID)

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
			val := strings.TrimPrefix(envVar, "OPENAI_API_KEY=")
			display := val
			if len(val) > 10 {
				display = val[:10] + "..."
			}
			maskedEnv = append(maskedEnv, "OPENAI_API_KEY="+display)
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

	// Deliver prompt via stdin pipe. The "-" arg in BuildArgs tells codex
	// to read instructions from stdin, avoiding Windows command-line length limits.
	stdinReader, stdinWriter := io.Pipe()
	cmd.Stdin = stdinReader

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
	now := time.Now().UnixNano()
	lastActivity.Store(now)
	touchActivity := func() { lastActivity.Store(time.Now().UnixNano()) }

	var tracker commandExecTracker
	var stopAfterSandbox func()
	var stopAfterSandboxMu sync.Mutex

	go func() {
		defer close(stderrDone)
		scanner := codingagent.NewLargeLineScanner(stderrPipe, cfg.ScannerMaxTokenBytes)
		for scanner.Scan() {
			line := scanner.Text()
			touchActivity()
			stderrBuf.WriteString(line)
			stderrBuf.WriteString("\n")
			log.Debug("CLI stderr line", "line", line)
			if matched, content := DetectStdinWaitFromStderr(line); matched {
				msg := content
				if msg == "" {
					msg = "Agent is waiting for user input"
				}
				select {
				case ch <- codingagent.StreamEvent{
					Type:    codingagent.EventUserInputRequired,
					Content: msg,
				}:
				case <-procCtx.Done():
					return
				}
			}
			if tracker.trySynthesizeFromStderrLine(ch, procCtx, line, cfg.MaxToolResultBytes) {
				log.Debug("synthesized sandbox tool_result from stderr line")
				if cfg.ExecutionMode != codingagent.ExecutionModeSingleShot {
					stopAfterSandboxMu.Lock()
					stop := stopAfterSandbox
					stopAfterSandboxMu.Unlock()
					if stop != nil {
						go stop()
					}
				}
			}
		}
		if err := scanner.Err(); err != nil {
			log.Warn("codex CLI stderr scanner error", "error", err.Error())
		}
	}()

	log.Info("starting codex CLI process", "work_dir", cfg.WorkDir, "model", cfg.Model, "cmd", cmd.String())
	if err := cmd.Start(); err != nil {
		stdinWriter.Close()
		cancel()
		return nil, nil, fmt.Errorf("start codex: %w", err)
	}

	// Write prompt to stdin; close after write only in single_shot mode.
	interactive := cfg.ExecutionMode != codingagent.ExecutionModeSingleShot
	go func() {
		if !interactive {
			defer stdinWriter.Close()
		}
		if cfg.Prompt != "" {
			if _, err := io.WriteString(stdinWriter, cfg.Prompt); err != nil {
				log.Warn("failed to write prompt to stdin", "error", err)
			}
		}
	}()

	pm := &ProcessManager{
		cmd:         cmd,
		cancel:      cancel,
		codexHome:   codexHome,
		logger:      log,
		stdinWriter: stdinWriter,
	}
	stopAfterSandboxMu.Lock()
	stopAfterSandbox = func() { pm.Stop() }
	stopAfterSandboxMu.Unlock()

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
					pm.emitTimeout(ch, procCtx, fmt.Sprintf("agent max execution timeout after %ds", maxSec))
					pm.Stop()
					return
				}
				last := time.Unix(0, lastActivity.Load())
				if time.Since(last) > time.Duration(idleSec)*time.Second {
					pm.emitTimeout(ch, procCtx, fmt.Sprintf("agent idle timeout after %ds", idleSec))
					pm.Stop()
					return
				}
			}
		}
	}()

	// Read JSONL events from stdout (codex exec --json outputs JSONL).
	go func() {
		defer close(ch)
		defer cancel() // Option A: cancel procCtx when stdout closes so the timeout goroutine exits cleanly
		scanner := codingagent.NewLargeLineScanner(stdout, cfg.ScannerMaxTokenBytes)
		resultEmitted := false
		lastErrContent := ""
		for scanner.Scan() {
			line := scanner.Text()
			touchActivity()
			if line == "" {
				continue
			}
			log.Trace("CLI stdout line", "line", line)

			if inputEv := DetectUserInputFromExecEvent(line); inputEv != nil {
				select {
				case ch <- *inputEv:
				case <-procCtx.Done():
					return
				}
			}

			ev := ParseExecEvent(line)
			if ev != nil {
				if ev.Type == codingagent.EventToolUse && ev.ToolName == "command_execution" {
					tracker.markToolUse()
					if tracker.tryFlushPendingRejection(ch, procCtx, cfg.MaxToolResultBytes) {
						log.Debug("synthesized sandbox tool_result from buffered stderr rejection")
						if cfg.ExecutionMode != codingagent.ExecutionModeSingleShot {
							stopAfterSandboxMu.Lock()
							stop := stopAfterSandbox
							stopAfterSandboxMu.Unlock()
							if stop != nil {
								go stop()
							}
						}
					}
				}
				if ev.Type == codingagent.EventToolResult {
					if tracker.synthesizedSandboxReject() {
						tracker.markToolResult()
						continue
					}
					ev.Content = codingagent.TruncateToolResult(ev.Content, cfg.MaxToolResultBytes)
					tracker.markToolResult()
				}
				if ev.Type == codingagent.EventResult {
					resultEmitted = true
				}
				if ev.Type == codingagent.EventError && ev.Content != "" {
					lastErrContent = ev.Content
				}
				if ev.Type == codingagent.EventText {
					tracker.trySynthesizeFromRejectionText(ch, procCtx, ev.Content, cfg.MaxToolResultBytes)
				}
				select {
				case ch <- *ev:
				case <-procCtx.Done():
					return
				}
			} else {
				log.Debug("unhandled codex event type (ignored)", "line", line)
			}
		}

		if err := scanner.Err(); err != nil {
			log.Warn("codex stdout scanner error", "error", err.Error())
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

		// Close stdin before cmd.Wait() so the stdin copy goroutine can exit.
		pm.closeStdin()

		tracker.synthesizeIfNeeded(ch, procCtx, stderrBuf.String(), cfg.MaxToolResultBytes)

		// R3: Check exit code and report stderr on failure.
		if err := cmd.Wait(); err != nil {
			if tracker.synthesizedSandboxReject() {
				log.Debug("skipping process exit EventError after synthesized sandbox tool_result")
			} else {
			errMsg := strings.TrimSpace(stderrBuf.String())
			if errMsg == "" {
				errMsg = lastErrContent
				if errMsg != "" {
					log.Debug("using last stdout EventError as process exit message", "content", errMsg)
				}
			}
			if errMsg == "" {
				errMsg = err.Error()
			}
			log.Warn("codex CLI process exited with error", "error", err.Error(), "stderr", errMsg)
			retryable := !codingagent.IsNonRetryableError(errMsg)
			content := errMsg
			if retryable {
				overloaded := codingagent.IsRetryableUpstream(errMsg)
				content = codingagent.ClassifiedErrorContent(errMsg, overloaded)
			}
			log.Debug("classified process exit error", "retryable", retryable)
			select {
			case ch <- codingagent.StreamEvent{
				Type:      codingagent.EventError,
				Content:   content,
				Retryable: retryable,
			}:
			case <-procCtx.Done():
			}
			}
		} else {
			exitCode := cmd.ProcessState.ExitCode()
			log.Debug("codex CLI process exited", "exit_code", exitCode)
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

// commandExecTracker tracks pending command_execution tool calls for sandbox synthesis.
type commandExecTracker struct {
	mu                sync.Mutex
	pending           bool
	toolResultSent    bool
	synthesizedReject bool
	pendingRejection  string // stderr rejection seen before matching tool_use
}

func (t *commandExecTracker) markToolUse() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.synthesizedReject {
		return
	}
	t.pending = true
	t.toolResultSent = false
}

func (t *commandExecTracker) markToolResult() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pending = false
	t.toolResultSent = true
	t.pendingRejection = ""
}

func (t *commandExecTracker) trySynthesizeFromStderrLine(
	ch chan<- codingagent.StreamEvent,
	ctx context.Context,
	line string,
	maxBytes int,
) bool {
	return t.trySynthesizeFromRejectionText(ch, ctx, line, maxBytes)
}

func (t *commandExecTracker) trySynthesizeFromRejectionText(
	ch chan<- codingagent.StreamEvent,
	ctx context.Context,
	text string,
	maxBytes int,
) bool {
	text = strings.TrimSpace(text)
	if text == "" || !codingagent.IsSandboxRejection(text) {
		return false
	}
	content := ExtractSandboxRejectionContent(text)
	if content == "" {
		content = text
	}

	t.mu.Lock()
	pending := t.pending
	already := t.toolResultSent
	t.mu.Unlock()

	if !pending || already {
		t.mu.Lock()
		if !already && content != "" {
			t.pendingRejection = content
		}
		t.mu.Unlock()
		return false
	}
	return t.emitSandboxToolResult(ch, ctx, content, maxBytes)
}

func (t *commandExecTracker) tryFlushPendingRejection(
	ch chan<- codingagent.StreamEvent,
	ctx context.Context,
	maxBytes int,
) bool {
	t.mu.Lock()
	content := t.pendingRejection
	pending := t.pending
	already := t.toolResultSent
	t.mu.Unlock()

	if content == "" || !pending || already {
		return false
	}

	if !t.emitSandboxToolResult(ch, ctx, content, maxBytes) {
		return false
	}

	t.mu.Lock()
	t.pendingRejection = ""
	t.mu.Unlock()
	return true
}

func (t *commandExecTracker) synthesizeIfNeeded(
	ch chan<- codingagent.StreamEvent,
	ctx context.Context,
	stderr string,
	maxBytes int,
) {
	if t.tryFlushPendingRejection(ch, ctx, maxBytes) {
		return
	}
	stderr = strings.TrimSpace(stderr)
	if stderr == "" || !codingagent.IsSandboxRejection(stderr) {
		return
	}
	t.emitSandboxToolResult(ch, ctx, ExtractSandboxRejectionContent(stderr), maxBytes)
}

func (t *commandExecTracker) emitSandboxToolResult(
	ch chan<- codingagent.StreamEvent,
	ctx context.Context,
	content string,
	maxBytes int,
) bool {
	t.mu.Lock()
	pending := t.pending
	already := t.toolResultSent
	t.mu.Unlock()

	if !pending || already {
		return false
	}

	content = codingagent.TruncateToolResult(content, maxBytes)
	select {
	case ch <- codingagent.StreamEvent{
		Type:    codingagent.EventToolResult,
		Content: content,
	}:
	case <-ctx.Done():
		return false
	}

	t.mu.Lock()
	t.pending = false
	t.toolResultSent = true
	t.synthesizedReject = true
	t.mu.Unlock()
	return true
}

func (t *commandExecTracker) synthesizedSandboxReject() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.synthesizedReject
}

func (pm *ProcessManager) emitTimeout(ch chan<- codingagent.StreamEvent, ctx context.Context, msg string) {
	defer func() { recover() }() // Option B: absorb panic if ch is already closed
	select {
	case ch <- codingagent.StreamEvent{Type: codingagent.EventError, Content: msg}:
	case <-ctx.Done():
	}
}

func (pm *ProcessManager) closeStdin() {
	pm.stdinMu.Lock()
	defer pm.stdinMu.Unlock()
	if pm.stdinWriter != nil {
		pm.stdinWriter.Close()
		pm.stdinWriter = nil
	}
}

// Stop gracefully terminates the subprocess and cleans up the codex home.
// 1. Send SIGTERM (Unix) or Kill (Windows)
// 2. Wait up to 5 seconds for exit
// 3. Force kill if timeout
// 4. Clean up temporary CODEX_HOME directory
func (pm *ProcessManager) Stop() error {
	pm.closeStdin()
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
