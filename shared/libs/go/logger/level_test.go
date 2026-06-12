package logger

import "testing"

func TestLevel_String(t *testing.T) {
	tests := []struct {
		level Level
		want  string
	}{
		{LevelTrace, "TRACE"},
		{LevelDebug, "DEBUG"},
		{LevelInfo, "INFO"},
		{LevelWarn, "WARN"},
		{LevelError, "ERROR"},
		{Level(99), "UNKNOWN"},
	}
	for _, tt := range tests {
		if got := tt.level.String(); got != tt.want {
			t.Errorf("Level(%d).String() = %q, want %q", tt.level, got, tt.want)
		}
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input string
		want  Level
	}{
		{"trace", LevelTrace},
		{"TRACE", LevelTrace},
		{"Trace", LevelTrace},
		{"debug", LevelDebug},
		{"DEBUG", LevelDebug},
		{"Debug", LevelDebug},
		{"info", LevelInfo},
		{"INFO", LevelInfo},
		{"warn", LevelWarn},
		{"WARN", LevelWarn},
		{"warning", LevelWarn},
		{"WARNING", LevelWarn},
		{"error", LevelError},
		{"ERROR", LevelError},
		{"unknown", LevelInfo},
		{"", LevelInfo},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := ParseLevel(tt.input); got != tt.want {
				t.Errorf("ParseLevel(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestLevel_Ordering(t *testing.T) {
	if !(LevelTrace < LevelDebug) {
		t.Errorf("expected LevelTrace (%d) < LevelDebug (%d)", LevelTrace, LevelDebug)
	}
	if !(LevelDebug < LevelInfo) {
		t.Errorf("expected LevelDebug (%d) < LevelInfo (%d)", LevelDebug, LevelInfo)
	}
	if !(LevelInfo < LevelWarn) {
		t.Errorf("expected LevelInfo (%d) < LevelWarn (%d)", LevelInfo, LevelWarn)
	}
	if !(LevelWarn < LevelError) {
		t.Errorf("expected LevelWarn (%d) < LevelError (%d)", LevelWarn, LevelError)
	}
}
