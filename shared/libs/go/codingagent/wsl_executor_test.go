package codingagent

import (
	"context"
	"runtime"
	"strings"
	"testing"
)

func TestParseWSLPath(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Log("Skipping parse tests on non-Windows platform")
		return
	}

	tests := []struct {
		name      string
		input     string
		wantDist  string
		wantLinux string
		wantIsWSL bool
	}{
		{
			name:      "WSL localhost path standard",
			input:     `\\wsl.localhost\Ubuntu\tmp\vv5-stage-1\merged`,
			wantDist:  "Ubuntu",
			wantLinux: "/tmp/vv5-stage-1/merged",
			wantIsWSL: true,
		},
		{
			name:      "WSL localhost path space",
			input:     `\\wsl.localhost\Ubuntu-22.04\home\user\my project`,
			wantDist:  "Ubuntu-22.04",
			wantLinux: "/home/user/my project",
			wantIsWSL: true,
		},
		{
			name:      "WSL legacy dollar path",
			input:     `\\wsl$\Debian\var\log`,
			wantDist:  "Debian",
			wantLinux: "/var/log",
			wantIsWSL: true,
		},
		{
			name:      "Linux style slash path",
			input:     `/tmp/vv5-stage-2/merged`,
			wantDist:  "",
			wantLinux: "/tmp/vv5-stage-2/merged",
			wantIsWSL: true,
		},
		{
			name:      "Regular Windows path C drive",
			input:     `C:\Users\yamya\myprog`,
			wantDist:  "",
			wantLinux: "",
			wantIsWSL: false,
		},
		{
			name:      "Regular Windows relative path",
			input:     `.\relative\path`,
			wantDist:  "",
			wantLinux: "",
			wantIsWSL: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dist, linuxPath, isWSL := ParseWSLPath(tt.input)
			if isWSL != tt.wantIsWSL {
				t.Errorf("ParseWSLPath(%q) isWSL = %v, want %v", tt.input, isWSL, tt.wantIsWSL)
			}
			if dist != tt.wantDist {
				t.Errorf("ParseWSLPath(%q) distro = %q, want %q", tt.input, dist, tt.wantDist)
			}
			if linuxPath != tt.wantLinux {
				t.Errorf("ParseWSLPath(%q) linuxPath = %q, want %q", tt.input, linuxPath, tt.wantLinux)
			}
		})
	}
}

func TestConvertToLinuxPath(t *testing.T) {
	if runtime.GOOS != "windows" {
		return
	}

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "WSL localhost path standard",
			input: `\\wsl.localhost\Ubuntu\tmp\vv5-stage-1\merged`,
			want:  "/tmp/vv5-stage-1/merged",
		},
		{
			name:  "Regular Windows path",
			input: `C:\Users\yamya`,
			want:  `C:\Users\yamya`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ConvertToLinuxPath(tt.input)
			if got != tt.want {
				t.Errorf("ConvertToLinuxPath(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestWSLCommandBuilder(t *testing.T) {
	ctx := context.Background()

	builder := &WSLCommandBuilder{
		Distro:         "Ubuntu",
		WorkDir:        "/tmp/merged",
		Command:        "claude",
		Args:           []string{"--verbose"},
		Env:            []string{"CLAUDE_CONFIG_DIR=\\\\wsl.localhost\\Ubuntu\\tmp\\sessions", "SOME_VAR=value"},
		DisableSandbox: false,
	}

	cmd := builder.BuildCmd(ctx)

	// Verify command base
	if !strings.HasSuffix(cmd.Path, "wsl.exe") {
		t.Errorf("Expected command to be wsl.exe, got %q", cmd.Path)
	}

	// Verify generated arguments
	argsStr := strings.Join(cmd.Args, " ")
	t.Logf("Generated Args: %s", argsStr)

	// Distro check
	if !strings.Contains(argsStr, "-d Ubuntu") {
		t.Error("Expected args to contain distro '-d Ubuntu'")
	}

	// Chdir via shell check
	if !strings.Contains(argsStr, "cd '/tmp/merged'") {
		t.Error("Expected args to contain 'cd '/tmp/merged''")
	}

	// Env check (with path conversion check)
	if !strings.Contains(argsStr, "env CLAUDE_CONFIG_DIR='/tmp/sessions' SOME_VAR='value'") {
		t.Error("Expected args to contain converted env: 'env CLAUDE_CONFIG_DIR='/tmp/sessions' SOME_VAR='value''")
	}

	// Sandbox check
	if !strings.Contains(argsStr, "bwrap --dev-bind / /") {
		t.Error("Expected args to contain bwrap command")
	}

	// Target command check
	if !strings.Contains(argsStr, "claude '--verbose'") {
		t.Error("Expected args to contain target command and its arguments")
	}
}
