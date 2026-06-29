package agentservice

import "testing"

func TestClaudeVersionParser(t *testing.T) {
	parser := GetVersionParser("claudecode")
	if parser == nil {
		t.Fatal("expected non-nil parser for claudecode")
	}

	// Test Parse
	parseTests := []struct {
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
			name:      "with leading v",
			input:     "v2.1.169",
			wantMajor: 2, wantMinor: 1, wantPatch: 169,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
	}

	for _, tt := range parseTests {
		t.Run("Parse_"+tt.name, func(t *testing.T) {
			major, minor, patch, err := parser.Parse(tt.input)
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

	// Test Check
	checkTests := []struct {
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

	for _, tt := range checkTests {
		t.Run("Check_"+tt.name, func(t *testing.T) {
			err := parser.Check(tt.raw)
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestCodexVersionParser(t *testing.T) {
	parser := GetVersionParser("codex")
	if parser == nil {
		t.Fatal("expected non-nil parser for codex")
	}

	// Test Parse
	parseTests := []struct {
		name      string
		input     string
		wantMajor int
		wantMinor int
		wantPatch int
		wantErr   bool
	}{
		{
			name:      "codex-cli version",
			input:     "codex-cli 0.139.0",
			wantMajor: 0, wantMinor: 139, wantPatch: 0,
		},
		{
			name:      "version only",
			input:     "0.139.0",
			wantMajor: 0, wantMinor: 139, wantPatch: 0,
		},
		{
			name:      "with leading v",
			input:     "v1.2.3",
			wantMajor: 1, wantMinor: 2, wantPatch: 3,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
	}

	for _, tt := range parseTests {
		t.Run("Parse_"+tt.name, func(t *testing.T) {
			major, minor, patch, err := parser.Parse(tt.input)
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

	// Test Check
	checkTests := []struct {
		name string
		raw  string
	}{
		{name: "any version is OK", raw: "codex-cli 0.139.0"},
		{name: "even old version is OK", raw: "0.1.0"},
		{name: "empty raw is OK", raw: ""},
	}

	for _, tt := range checkTests {
		t.Run("Check_"+tt.name, func(t *testing.T) {
			err := parser.Check(tt.raw)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
