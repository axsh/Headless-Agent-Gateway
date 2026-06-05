package codex_test

import (
	"os"
	"strings"
	"testing"

	"github.com/axsh/hag/codingagent/codex"
)

func TestGenerateConfigTOML(t *testing.T) {
	tests := []struct {
		name       string
		model      string
		gatewayURL string
		contains   []string
	}{
		{
			name:       "with model and gateway",
			model:      "gpt-4o",
			gatewayURL: "http://localhost:14000",
			contains: []string{
				`model = "gpt-4o"`,
				`base_url = "http://localhost:14000"`,
				`wire_api = "chat"`,
				`model_provider = "gateway"`,
			},
		},
		{
			name:       "empty model uses default",
			model:      "",
			gatewayURL: "http://localhost:14000",
			contains:   []string{`model = "gpt-4o"`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := codex.GenerateConfigTOML(tt.model, tt.gatewayURL)
			for _, want := range tt.contains {
				if !strings.Contains(result, want) {
					t.Errorf("config should contain %q, got:\n%s", want, result)
				}
			}
		})
	}
}

func TestWriteConfigTOML(t *testing.T) {
	path, err := codex.WriteConfigTOML("gpt-4o", "http://localhost:14000")
	if err != nil {
		t.Fatalf("WriteConfigTOML error: %v", err)
	}
	defer os.RemoveAll(path)

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}

	if !strings.Contains(string(content), `model = "gpt-4o"`) {
		t.Errorf("file content should contain model, got:\n%s", content)
	}
}
