package codex_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent/codex"
)

func TestGenerateConfigTOML(t *testing.T) {
	tests := []struct {
		name       string
		model      string
		gatewayURL string
		wireAPI    string
		contains   []string
	}{
		{
			name:       "with model and gateway (default responses)",
			model:      "gpt-4o",
			gatewayURL: "http://localhost:14000",
			wireAPI:    "",
			contains: []string{
				`model = "gpt-4o"`,
				`base_url = "http://localhost:14000/v1"`,
				`wire_api = "responses"`,
				`model_provider = "gateway"`,
			},
		},
		{
			name:       "with responses wire_api",
			model:      "codex-mini-latest",
			gatewayURL: "http://localhost:14000",
			wireAPI:    "responses",
			contains: []string{
				`model = "codex-mini-latest"`,
				`wire_api = "responses"`,
			},
		},
		{
			name:       "explicit chat wire_api",
			model:      "gpt-4o",
			gatewayURL: "http://localhost:14000",
			wireAPI:    "chat",
			contains:   []string{`wire_api = "chat"`},
		},
		{
			name:       "empty model uses default",
			model:      "",
			gatewayURL: "http://localhost:14000",
			wireAPI:    "",
			contains:   []string{`model = "gpt-4o"`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := codex.GenerateConfigTOML(tt.model, tt.gatewayURL, tt.wireAPI)
			for _, want := range tt.contains {
				if !strings.Contains(result, want) {
					t.Errorf("config should contain %q, got:\n%s", want, result)
				}
			}
		})
	}
}

func TestWriteConfigTOML(t *testing.T) {
	dir, err := codex.WriteConfigTOML("gpt-4o", "http://localhost:14000", "responses")
	if err != nil {
		t.Fatalf("WriteConfigTOML error: %v", err)
	}
	defer os.RemoveAll(dir)

	configPath := filepath.Join(dir, "config.toml")
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}

	if !strings.Contains(string(content), `model = "gpt-4o"`) {
		t.Errorf("file content should contain model, got:\n%s", content)
	}
	if !strings.Contains(string(content), `wire_api = "responses"`) {
		t.Errorf("file content should contain wire_api, got:\n%s", content)
	}
}
