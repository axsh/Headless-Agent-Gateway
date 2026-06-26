package codingagent

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ParseWSLPath parses a Windows path starting with a WSL prefix into (distro, linuxPath, isWSL).
// e.g. `\\wsl.localhost\Ubuntu\tmp` -> (`Ubuntu`, `/tmp`, true)
func ParseWSLPath(path string) (distro string, linuxPath string, isWSL bool) {
	if runtime.GOOS != "windows" {
		return "", "", false
	}

	if strings.HasPrefix(path, "/") {
		// Already a Unix-style slash path
		return "", filepath.ToSlash(path), true
	}

	// Clean the path to standardize separators to backslashes
	path = filepath.Clean(path)

	var prefix string
	if strings.HasPrefix(path, `\\wsl.localhost\`) {
		prefix = `\\wsl.localhost\`
	} else if strings.HasPrefix(path, `\\wsl$\`) {
		prefix = `\\wsl$\`
	} else {
		return "", "", false
	}

	rem := strings.TrimPrefix(path, prefix)
	parts := strings.SplitN(rem, `\`, 2)
	if len(parts) == 0 {
		return "", "", false
	}

	distro = parts[0]
	if len(parts) > 1 {
		linuxPath = "/" + filepath.ToSlash(parts[1])
	} else {
		linuxPath = "/"
	}
	return distro, linuxPath, true
}

// ConvertToLinuxPath converts a Windows path (which may be a WSL path) to a Linux-style path.
// If it is not a WSL path, it returns the input unchanged.
func ConvertToLinuxPath(path string) string {
	_, linuxPath, isWSL := ParseWSLPath(path)
	if isWSL {
		return linuxPath
	}
	return path
}

// VerifyWSLRuntime checks if the target CLI exists in WSL.
func VerifyWSLRuntime(ctx context.Context, distro string, cmdName string) error {
	var args []string
	if distro != "" {
		args = append(args, "-d", distro)
	}
	args = append(args, "--", "which", cmdName)

	cmd := exec.CommandContext(ctx, "wsl.exe", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("agent runtime %q not found in WSL2. Please install it in WSL2 (example: npm install -g @anthropic-ai/claude-code)", cmdName)
	}
	return nil
}

// WSLCommandBuilder builds a wsl.exe command execution structure.
type WSLCommandBuilder struct {
	Distro         string
	WorkDir        string
	Command        string
	Args           []string
	Env            []string
	DisableSandbox bool
}

// EscapeShellArg escapes a string to be safe as a shell argument inside sh -c.
func EscapeShellArg(arg string) string {
	return "'" + strings.ReplaceAll(arg, "'", "'\\''") + "'"
}

// BuildCmd returns the *exec.Cmd to execute.
func (b *WSLCommandBuilder) BuildCmd(ctx context.Context) *exec.Cmd {
	var shellCmds []string

	if b.WorkDir != "" {
		shellCmds = append(shellCmds, "cd "+EscapeShellArg(b.WorkDir))
	}

	var envStr string
	if len(b.Env) > 0 {
		var envParts []string
		for _, e := range b.Env {
			parts := strings.SplitN(e, "=", 2)
			if len(parts) == 2 {
				k, v := parts[0], parts[1]
				envParts = append(envParts, k+"="+EscapeShellArg(ConvertToLinuxPath(v)))
			} else {
				envParts = append(envParts, EscapeShellArg(e))
			}
		}
		envStr = "env " + strings.Join(envParts, " ")
	}

	var runCmdParts []string
	if !b.DisableSandbox {
		runCmdParts = append(runCmdParts, "bwrap", "--dev-bind", "/", "/")
	}
	runCmdParts = append(runCmdParts, b.Command)
	for _, arg := range b.Args {
		runCmdParts = append(runCmdParts, EscapeShellArg(arg))
	}

	runCmdStr := strings.Join(runCmdParts, " ")
	if envStr != "" {
		runCmdStr = envStr + " " + runCmdStr
	}

	shellCmds = append(shellCmds, runCmdStr)

	var wslArgs []string
	if b.Distro != "" {
		wslArgs = append(wslArgs, "-d", b.Distro)
	}
	wslArgs = append(wslArgs, "--", "sh", "-c", strings.Join(shellCmds, " && "))

	return exec.CommandContext(ctx, "wsl.exe", wslArgs...)
}
