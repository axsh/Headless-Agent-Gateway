package config

import (
	"reflect"
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
  file_path: "/etc/tern/vault.json"
  aes_enabled: true
`,
			want: AppConfig{
				Vault: VaultConfig{
					Backend:    "file",
					FilePath:   "/etc/tern/vault.json",
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
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestLogConfig_Outputs(t *testing.T) {
	input := `
log:
  level: "debug"
  outputs:
    - type: "stdout"
    - type: "syslog"
      network: "udp"
      address: "localhost:514"
      tag: "tern"
`
	var cfg AppConfig
	err := yaml.Unmarshal([]byte(input), &cfg)
	if err != nil {
		t.Fatalf("unexpected error unmarshaling log config: %v", err)
	}

	if cfg.Log.Level != "debug" {
		t.Errorf("expected level debug, got %q", cfg.Log.Level)
	}
	if len(cfg.Log.Outputs) != 2 {
		t.Fatalf("expected 2 outputs, got %d", len(cfg.Log.Outputs))
	}
	if cfg.Log.Outputs[0].Type != "stdout" {
		t.Errorf("expected output 0 to be stdout, got %q", cfg.Log.Outputs[0].Type)
	}
	if cfg.Log.Outputs[1].Type != "syslog" {
		t.Errorf("expected output 1 to be syslog, got %q", cfg.Log.Outputs[1].Type)
	}
	if cfg.Log.Outputs[1].Network != "udp" || cfg.Log.Outputs[1].Address != "localhost:514" || cfg.Log.Outputs[1].Tag != "tern" {
		t.Errorf("syslog output mismatch: %+v", cfg.Log.Outputs[1])
	}
}

func TestLLMGatewayConfig_SecurityFields(t *testing.T) {
	input := `
llm_gateway:
  port: 14000
  tls:
    enabled: true
    mode: "auto"
    cert_file: "/path/to/cert.pem"
    key_file: "/path/to/key.pem"
    extra_sans:
      - "gateway"
      - "proxy"
  auth_token: "static-token-123"
  max_request_body_bytes: 5242880
  session:
    max_sessions: 500
    ttl_seconds: 3600
  server:
    read_timeout_seconds: 15
    write_timeout_seconds: 120
    idle_timeout_seconds: 30
    max_header_bytes: 524288
`
	var cfg AppConfig
	err := yaml.Unmarshal([]byte(input), &cfg)
	if err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}

	gw := cfg.LLMGateway
	if gw.Port != 14000 {
		t.Errorf("Port: got %d, want 14000", gw.Port)
	}

	// TLS
	if !gw.TLS.Enabled {
		t.Error("TLS.Enabled: got false, want true")
	}
	if gw.TLS.Mode != "auto" {
		t.Errorf("TLS.Mode: got %q, want %q", gw.TLS.Mode, "auto")
	}
	if gw.TLS.CertFile != "/path/to/cert.pem" {
		t.Errorf("TLS.CertFile: got %q, want %q", gw.TLS.CertFile, "/path/to/cert.pem")
	}
	if gw.TLS.KeyFile != "/path/to/key.pem" {
		t.Errorf("TLS.KeyFile: got %q, want %q", gw.TLS.KeyFile, "/path/to/key.pem")
	}
	if len(gw.TLS.ExtraSANs) != 2 || gw.TLS.ExtraSANs[0] != "gateway" || gw.TLS.ExtraSANs[1] != "proxy" {
		t.Errorf("TLS.ExtraSANs: got %v, want [gateway proxy]", gw.TLS.ExtraSANs)
	}

	// AuthToken
	if gw.AuthToken != "static-token-123" {
		t.Errorf("AuthToken: got %q, want %q", gw.AuthToken, "static-token-123")
	}

	// MaxRequestBodyBytes
	if gw.MaxRequestBodyBytes != 5242880 {
		t.Errorf("MaxRequestBodyBytes: got %d, want 5242880", gw.MaxRequestBodyBytes)
	}

	// Session
	if gw.Session.MaxSessions != 500 {
		t.Errorf("Session.MaxSessions: got %d, want 500", gw.Session.MaxSessions)
	}
	if gw.Session.TTLSeconds != 3600 {
		t.Errorf("Session.TTLSeconds: got %d, want 3600", gw.Session.TTLSeconds)
	}

	// Server
	if gw.Server.ReadTimeoutSeconds != 15 {
		t.Errorf("Server.ReadTimeoutSeconds: got %d, want 15", gw.Server.ReadTimeoutSeconds)
	}
	if gw.Server.WriteTimeoutSeconds != 120 {
		t.Errorf("Server.WriteTimeoutSeconds: got %d, want 120", gw.Server.WriteTimeoutSeconds)
	}
	if gw.Server.IdleTimeoutSeconds != 30 {
		t.Errorf("Server.IdleTimeoutSeconds: got %d, want 30", gw.Server.IdleTimeoutSeconds)
	}
	if gw.Server.MaxHeaderBytes != 524288 {
		t.Errorf("Server.MaxHeaderBytes: got %d, want 524288", gw.Server.MaxHeaderBytes)
	}
}

func TestLLMGatewayConfig_ApplyDefaults(t *testing.T) {
	cfg := &AppConfig{}
	cfg.LLMGateway.ApplyDefaults()

	gw := cfg.LLMGateway
	if gw.MaxRequestBodyBytes != 10*1024*1024 {
		t.Errorf("MaxRequestBodyBytes default: got %d, want %d", gw.MaxRequestBodyBytes, 10*1024*1024)
	}
	if gw.Session.MaxSessions != 1000 {
		t.Errorf("Session.MaxSessions default: got %d, want 1000", gw.Session.MaxSessions)
	}
	if gw.Session.TTLSeconds != 86400 {
		t.Errorf("Session.TTLSeconds default: got %d, want 86400", gw.Session.TTLSeconds)
	}
	if gw.Server.ReadTimeoutSeconds != 30 {
		t.Errorf("Server.ReadTimeoutSeconds default: got %d, want 30", gw.Server.ReadTimeoutSeconds)
	}
	if gw.Server.WriteTimeoutSeconds != 300 {
		t.Errorf("Server.WriteTimeoutSeconds default: got %d, want 300", gw.Server.WriteTimeoutSeconds)
	}
	if gw.Server.IdleTimeoutSeconds != 60 {
		t.Errorf("Server.IdleTimeoutSeconds default: got %d, want 60", gw.Server.IdleTimeoutSeconds)
	}
	if gw.Server.MaxHeaderBytes != 1<<20 {
		t.Errorf("Server.MaxHeaderBytes default: got %d, want %d", gw.Server.MaxHeaderBytes, 1<<20)
	}
}

func TestLLMGatewayConfig_ApplyDefaults_NoOverwrite(t *testing.T) {
	cfg := &AppConfig{}
	cfg.LLMGateway.MaxRequestBodyBytes = 1024
	cfg.LLMGateway.Session.MaxSessions = 50
	cfg.LLMGateway.Server.WriteTimeoutSeconds = 600
	cfg.LLMGateway.ApplyDefaults()

	if cfg.LLMGateway.MaxRequestBodyBytes != 1024 {
		t.Errorf("MaxRequestBodyBytes: got %d, want 1024 (should not overwrite)", cfg.LLMGateway.MaxRequestBodyBytes)
	}
	if cfg.LLMGateway.Session.MaxSessions != 50 {
		t.Errorf("Session.MaxSessions: got %d, want 50 (should not overwrite)", cfg.LLMGateway.Session.MaxSessions)
	}
	if cfg.LLMGateway.Server.WriteTimeoutSeconds != 600 {
		t.Errorf("Server.WriteTimeoutSeconds: got %d, want 600 (should not overwrite)", cfg.LLMGateway.Server.WriteTimeoutSeconds)
	}
}

