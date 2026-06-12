package agentservice

import "testing"

func TestParseCLIVersion(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantMajor int
		wantMinor int
		wantPatch int
		wantErr   bool
	}{
		{
			name:      "v2.1.169 with suffix",
			input:     "2.1.169 (Claude Code)",
			wantMajor: 2, wantMinor: 1, wantPatch: 169,
		},
		{
			name:      "v2.0.14 old version",
			input:     "2.0.14 (Claude Code)",
			wantMajor: 2, wantMinor: 0, wantPatch: 14,
		},
		{
			name:      "version only",
			input:     "2.1.169",
			wantMajor: 2, wantMinor: 1, wantPatch: 169,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			major, minor, patch, err := parseCLIVersion(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if major != tt.wantMajor || minor != tt.wantMinor || patch != tt.wantPatch {
				t.Errorf("got %d.%d.%d, want %d.%d.%d",
					major, minor, patch,
					tt.wantMajor, tt.wantMinor, tt.wantPatch)
			}
		})
	}
}

func TestCheckCLIVersion(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{name: "v2.1.169 OK", raw: "2.1.169 (Claude Code)", wantErr: false},
		{name: "v2.1.0 exact minimum", raw: "2.1.0", wantErr: false},
		{name: "v2.0.14 too old", raw: "2.0.14 (Claude Code)", wantErr: true},
		{name: "v1.99.0 too old", raw: "1.99.0", wantErr: true},
		{name: "v3.0.0 future OK", raw: "3.0.0", wantErr: false},
		{name: "unavailable skipped", raw: "unavailable", wantErr: false},
		{name: "empty skipped", raw: "", wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkCLIVersion(tt.raw, minClaudeCLIVersion)
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
