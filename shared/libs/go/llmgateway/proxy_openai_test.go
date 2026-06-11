package llmgateway

import (
	"testing"
)




func TestIsStreamRequest(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{"stream true", `{"model":"gpt-4o","stream":true}`, true},
		{"stream false", `{"model":"gpt-4o","stream":false}`, false},
		{"no stream field", `{"model":"gpt-4o"}`, false},
		{"invalid json", `bad json`, false},
		{"empty body", ``, false},
		{"stream null", `{"model":"gpt-4o","stream":null}`, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isStreamRequest([]byte(tc.body))
			if got != tc.want {
				t.Errorf("isStreamRequest(%q) = %v, want %v", tc.body, got, tc.want)
			}
		})
	}
}

func TestToBifrostProvider(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		wantStr  string
	}{
		{"openai", "openai", "openai"},
		{"anthropic", "anthropic", "anthropic"},
		{"google maps to gemini", "google", "gemini"},
		{"gemini direct", "gemini", "gemini"},
		{"unknown passthrough", "some-custom-provider", "some-custom-provider"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := toBifrostProvider(tc.provider)
			if string(got) != tc.wantStr {
				t.Errorf("toBifrostProvider(%q) = %q, want %q", tc.provider, got, tc.wantStr)
			}
		})
	}
}

