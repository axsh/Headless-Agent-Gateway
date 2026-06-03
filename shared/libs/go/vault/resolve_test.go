package vault

import "testing"

func TestIsVaultRef(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"vault://providers/anthropic/primary", true},
		{"vault://providers/openai/team-a", true},
		{"vault://", true},
		{"sk-ant-xxxxx", false},
		{"", false},
		{"VAULT://uppercase", false}, // case sensitive
		{"vaultx://bad", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := IsVaultRef(tt.input); got != tt.want {
				t.Errorf("IsVaultRef(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseVaultRef(t *testing.T) {
	tests := []struct {
		input   string
		isVault bool
		path    string
	}{
		{"vault://providers/anthropic/primary", true, "providers/anthropic/primary"},
		{"vault://providers/openai/team-a", true, "providers/openai/team-a"},
		{"vault://", true, ""},
		{"sk-ant-xxxxx", false, ""},
		{"", false, ""},
		{"VAULT://uppercase", false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			path, isVault := ParseVaultRef(tt.input)
			if isVault != tt.isVault {
				t.Errorf("ParseVaultRef(%q) isVault = %v, want %v", tt.input, isVault, tt.isVault)
			}
			if path != tt.path {
				t.Errorf("ParseVaultRef(%q) path = %q, want %q", tt.input, path, tt.path)
			}
		})
	}
}
