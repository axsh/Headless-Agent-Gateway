package codingagent_test

import (
	"strings"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

func TestResolveSandboxMode(t *testing.T) {
	tests := []struct {
		name                 string
		explicit             string
		serverDisableSandbox bool
		want                 string
		wantErr              bool
	}{
		{name: "omit server false", explicit: "", serverDisableSandbox: false, want: codingagent.SandboxModeReadOnly},
		{name: "omit server true", explicit: "", serverDisableSandbox: true, want: codingagent.SandboxModeDangerFullAccess},
		{name: "explicit read-only wins over server true", explicit: codingagent.SandboxModeReadOnly, serverDisableSandbox: true, want: codingagent.SandboxModeReadOnly},
		{name: "explicit workspace-write", explicit: codingagent.SandboxModeWorkspaceWrite, serverDisableSandbox: false, want: codingagent.SandboxModeWorkspaceWrite},
		{name: "explicit danger with server false", explicit: codingagent.SandboxModeDangerFullAccess, serverDisableSandbox: false, want: codingagent.SandboxModeDangerFullAccess},
		{name: "unknown", explicit: "full-auto", serverDisableSandbox: false, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := codingagent.ResolveSandboxMode(tt.explicit, tt.serverDisableSandbox)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				if !strings.Contains(err.Error(), "unsupported sandbox_mode") {
					t.Fatalf("error = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveSandboxMode: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSandboxModeDisablesSandbox(t *testing.T) {
	if codingagent.SandboxModeDisablesSandbox(codingagent.SandboxModeReadOnly) {
		t.Fatal("read-only must not disable sandbox")
	}
	if !codingagent.SandboxModeDisablesSandbox(codingagent.SandboxModeDangerFullAccess) {
		t.Fatal("danger-full-access must disable sandbox")
	}
	if codingagent.SandboxModeDisablesSandbox("") {
		t.Fatal("empty must not disable sandbox")
	}
}

func TestEffectiveSandboxMode(t *testing.T) {
	if got := codingagent.EffectiveSandboxMode(""); got != codingagent.SandboxModeReadOnly {
		t.Fatalf("got %q", got)
	}
	if got := codingagent.EffectiveSandboxMode(codingagent.SandboxModeDangerFullAccess); got != codingagent.SandboxModeDangerFullAccess {
		t.Fatalf("got %q", got)
	}
}
