package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestAppConfig_YAMLUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    AppConfig
		wantErr bool
	}{
		{
			name: "full config",
			input: `
llm_gateway:
  port: 14000
  model_profiles_path: "./model_profiles.yaml"
  metrics_enabled: false
vault:
  backend: "env"
log:
  level: "info"
`,
			want: AppConfig{
				LLMGateway: LLMGatewayConfig{
					Port:              14000,
					ModelProfilesPath: "./model_profiles.yaml",
					MetricsEnabled:    false,
				},
				Vault: VaultConfig{Backend: "env"},
				Log:   LogConfig{Level: "info"},
			},
		},
		{
			name: "minimal config",
			input: `
vault:
  backend: "env"
`,
			want: AppConfig{
				Vault: VaultConfig{Backend: "env"},
			},
		},
		{
			name:  "empty config",
			input: "",
			want:  AppConfig{},
		},
		{
			name: "metrics enabled",
			input: `
llm_gateway:
  port: 15000
  metrics_enabled: true
`,
			want: AppConfig{
				LLMGateway: LLMGatewayConfig{
					Port:           15000,
					MetricsEnabled: true,
				},
			},
		},
		{
			name: "file vault backend",
			input: `
vault:
  backend: "file"
  file_path: "/etc/hag/vault.json"
  aes_enabled: true
`,
			want: AppConfig{
				Vault: VaultConfig{
					Backend:    "file",
					FilePath:   "/etc/hag/vault.json",
					AESEnabled: true,
				},
			},
		},
		{
			name: "websocket config",
			input: `
websocket:
  port: 19000
`,
			want: AppConfig{
				WebSocket: WebSocketConfig{
					Port: 19000,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got AppConfig
			err := yaml.Unmarshal([]byte(tt.input), &got)
			if (err != nil) != tt.wantErr {
				t.Fatalf("yaml.Unmarshal() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if got != tt.want {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}
