package codex_test

import (
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent/codex"
)

func TestDetectStdinWaitFromStderr(t *testing.T) {
	tests := []struct {
		line string
		want bool
	}{
		{"Reading additional input from stdin. Send 'EOF' to end.", true},
		{"some other log line", false},
		{"", false},
	}
	for _, tt := range tests {
		got, _ := codex.DetectStdinWaitFromStderr(tt.line)
		if got != tt.want {
			t.Errorf("DetectStdinWaitFromStderr(%q) = %v, want %v", tt.line, got, tt.want)
		}
	}
}

func TestDetectUserInputFromExecEvent(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		wantNil bool
		wantLen int
	}{
		{
			name:    "explicit choices",
			line:    `{"type":"user_input","content":"Pick one","prompt_id":"p1","choices":["yes","no"]}`,
			wantLen: 2,
		},
		{
			name:    "no choices field",
			line:    `{"type":"user_input","content":"Pick one"}`,
			wantNil: true,
		},
		{
			name:    "invalid json",
			line:    `not json`,
			wantNil: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := codex.DetectUserInputFromExecEvent(tt.line)
			if tt.wantNil {
				if ev != nil {
					t.Fatalf("expected nil, got %+v", ev)
				}
				return
			}
			if ev == nil {
				t.Fatal("expected event, got nil")
			}
			if ev.Type != codingagent.EventUserInputRequired {
				t.Errorf("type = %v", ev.Type)
			}
			if len(ev.Choices) != tt.wantLen {
				t.Errorf("choices len = %d, want %d", len(ev.Choices), tt.wantLen)
			}
		})
	}
}
