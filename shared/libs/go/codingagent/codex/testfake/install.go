package testfake

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// DefaultFailStderr is the default reconnect stderr message used when FailLaunches triggers.
const DefaultFailStderr = "Reconnecting... 1/5 (We're currently experiencing high demand, which may cause temporary errors.)"

// Options configures the fake codex executable behavior.
type Options struct {
	Lines            []string       `json:"lines,omitempty"`
	Stderr           string         `json:"stderr,omitempty"`
	ExitCode         int            `json:"exit_code,omitempty"`
	LineDelay        time.Duration  `json:"line_delay,omitempty"`
	LaunchLogPath    string         `json:"launch_log_path,omitempty"`
	PIDFile          string         `json:"pid_file,omitempty"`
	HeartbeatPath    string         `json:"heartbeat_path,omitempty"`
	FailLaunches     []int          `json:"fail_launches,omitempty"` // 1-origin launch numbers that fail with exit 1
	FailStderr       string         `json:"fail_stderr,omitempty"`
	SilentFail       bool           `json:"silent_fail,omitempty"`
	FailResumeIDs    []string       `json:"fail_resume_ids,omitempty"`
	HangForever      bool           `json:"hang_forever,omitempty"`
	ThreadIDByLaunch map[int]string `json:"thread_id_by_launch,omitempty"`
}

// Install builds a fake codex executable in dir and prepends dir to PATH for the test.
func Install(t *testing.T, dir string, opts Options) {
	t.Helper()

	cfgPath := filepath.Join(dir, "fake_codex_config.json")
	cfgBytes, err := json.Marshal(opts)
	if err != nil {
		t.Fatalf("marshal fake codex config: %v", err)
	}
	if err := os.WriteFile(cfgPath, cfgBytes, 0644); err != nil {
		t.Fatalf("write fake codex config: %v", err)
	}

	mainSrc := `package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type config struct {
	Lines         []string      ` + "`json:\"lines,omitempty\"`" + `
	Stderr        string        ` + "`json:\"stderr,omitempty\"`" + `
	ExitCode      int           ` + "`json:\"exit_code,omitempty\"`" + `
	LineDelay     time.Duration ` + "`json:\"line_delay,omitempty\"`" + `
	LaunchLogPath string        ` + "`json:\"launch_log_path,omitempty\"`" + `
	PIDFile       string        ` + "`json:\"pid_file,omitempty\"`" + `
	HeartbeatPath string        ` + "`json:\"heartbeat_path,omitempty\"`" + `
	FailLaunches     []int             ` + "`json:\"fail_launches,omitempty\"`" + `
	FailStderr       string            ` + "`json:\"fail_stderr,omitempty\"`" + `
	SilentFail       bool              ` + "`json:\"silent_fail,omitempty\"`" + `
	FailResumeIDs    []string          ` + "`json:\"fail_resume_ids,omitempty\"`" + `
	HangForever      bool              ` + "`json:\"hang_forever,omitempty\"`" + `
	ThreadIDByLaunch map[string]string ` + "`json:\"thread_id_by_launch,omitempty\"`" + `
}

func main() {
	for _, arg := range os.Args[1:] {
		if arg == "--version" || arg == "-V" {
			fmt.Println("fake-codex 0.0.0")
			os.Exit(0)
		}
	}

	hasExec := false
	for _, arg := range os.Args[1:] {
		if arg == "exec" {
			hasExec = true
			break
		}
	}
	if !hasExec {
		os.Exit(0)
	}

	var cfg config
	cfgPath := os.Getenv("FAKE_CODEX_CONFIG")
	if cfgPath != "" {
		if data, err := os.ReadFile(cfgPath); err == nil {
			_ = json.Unmarshal(data, &cfg)
		}
	}

	launchNum := 1
	if cfg.LaunchLogPath != "" {
		logData, _ := os.ReadFile(cfg.LaunchLogPath)
		lines := strings.Split(strings.TrimSpace(string(logData)), "\n")
		if len(lines) > 0 && lines[0] != "" {
			launchNum = len(lines) + 1
		}
		f, err := os.OpenFile(cfg.LaunchLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err == nil {
			fmt.Fprintf(f, "launch %d args: %s\n", launchNum, strings.Join(os.Args[1:], " "))
			_ = f.Close()
		}
	}

	if cfg.PIDFile != "" {
		_ = os.WriteFile(cfg.PIDFile, []byte(strconv.Itoa(os.Getpid())), 0644)
	}

	if cfg.HeartbeatPath != "" {
		_ = os.WriteFile(cfg.HeartbeatPath, []byte(strconv.FormatInt(time.Now().UnixNano(), 10)), 0644)
		go func() {
			ticker := time.NewTicker(30 * time.Millisecond)
			defer ticker.Stop()
			for range ticker.C {
				_ = os.WriteFile(cfg.HeartbeatPath, []byte(strconv.FormatInt(time.Now().UnixNano(), 10)), 0644)
			}
		}()
	}

	shouldFail := false
	silentFail := cfg.SilentFail
	for _, failIdx := range cfg.FailLaunches {
		if failIdx == launchNum {
			shouldFail = true
			break
		}
	}
	args := os.Args[1:]
	hasResume := false
	for _, arg := range args {
		if arg == "resume" {
			hasResume = true
			break
		}
	}
	if hasResume {
		for _, arg := range args {
			for _, id := range cfg.FailResumeIDs {
				if arg == id {
					shouldFail = true
					silentFail = true
				}
			}
		}
	}

	if shouldFail {
		if !silentFail {
			failMsg := cfg.FailStderr
			if failMsg == "" {
				failMsg = "Reconnecting... 1/5 (We're currently experiencing high demand, which may cause temporary errors.)"
			}
			fmt.Fprintln(os.Stderr, failMsg)
		}
		os.Exit(1)
	}

	stderrMsg := cfg.Stderr
	if s := os.Getenv("FAKE_CODEX_STDERR"); s != "" {
		stderrMsg = s
	}
	if stderrMsg != "" {
		fmt.Fprintln(os.Stderr, stderrMsg)
	}

	lines := cfg.Lines
	if linesFile := os.Getenv("FAKE_CODEX_LINES_FILE"); linesFile != "" {
		if data, err := os.ReadFile(linesFile); err == nil {
			lines = strings.Split(string(data), "\n")
		}
	}

	overrideID := cfg.ThreadIDByLaunch[strconv.Itoa(launchNum)]
	if overrideID != "" {
		fmt.Printf("{\"type\":\"thread.started\",\"thread_id\":%q}\n", overrideID)
	}
	for i, line := range lines {
		if line == "" {
			continue
		}
		if overrideID != "" && strings.Contains(line, "thread.started") {
			continue
		}
		if i > 0 && cfg.LineDelay > 0 {
			time.Sleep(cfg.LineDelay)
		}
		fmt.Println(line)
	}

	if cfg.HangForever {
		select {}
	}

	code := cfg.ExitCode
	if v := os.Getenv("FAKE_CODEX_EXIT"); v != "" {
		if c, err := strconv.Atoi(v); err == nil {
			code = c
		}
	}
	os.Exit(code)
}
`

	mainPath := filepath.Join(dir, "main.go")
	if err := os.WriteFile(mainPath, []byte(mainSrc), 0644); err != nil {
		t.Fatalf("write fake codex main: %v", err)
	}

	binName := "codex"
	if runtime.GOOS == "windows" {
		binName = "codex.exe"
	}
	cmd := exec.Command("go", "build", "-o", filepath.Join(dir, binName), mainPath)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fake codex: %v\n%s", err, out)
	}

	sep := string(os.PathListSeparator)
	t.Setenv("PATH", dir+sep+os.Getenv("PATH"))
	t.Setenv("FAKE_CODEX_CONFIG", cfgPath)
}

// LaunchCount returns the number of times fake codex exec was launched according to launchLogPath.
func LaunchCount(t *testing.T, launchLogPath string) int {
	t.Helper()
	data, err := os.ReadFile(launchLogPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("read launch log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return 0
	}
	return len(lines)
}

// PID returns the integer PID written to pidFile.
func PID(t *testing.T, pidFile string) int {
	t.Helper()
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("read pid file: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatalf("parse pid file %q: %v", string(data), err)
	}
	return pid
}
