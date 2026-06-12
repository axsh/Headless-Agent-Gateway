package llmgateway

import "testing"

func TestMaskSecret(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "typical API key", input: "sk-ant-api03-xxxxx1234", expected: "****1234"},
		{name: "short string 2 chars", input: "ab", expected: "****"},
		{name: "empty string", input: "", expected: "****"},
		{name: "exactly 4 chars", input: "1234", expected: "****"},
		{name: "5 chars shows last 4", input: "12345", expected: "****2345"},
		{name: "single char", input: "x", expected: "****"},
		{name: "long key", input: "sk-proj-abcdefghijklmnop", expected: "****mnop"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MaskSecret(tt.input)
			if got != tt.expected {
				t.Errorf("MaskSecret(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
