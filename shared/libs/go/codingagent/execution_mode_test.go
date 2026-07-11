package codingagent_test

import (
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

func TestNormalizeExecutionMode(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"interactive", codingagent.ExecutionModeInteractive},
		{"single_shot", codingagent.ExecutionModeSingleShot},
		{"", codingagent.ExecutionModeInteractive},
		{"invalid", codingagent.ExecutionModeInteractive},
	}
	for _, tt := range tests {
		if got := codingagent.NormalizeExecutionMode(tt.input); got != tt.want {
			t.Errorf("NormalizeExecutionMode(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
