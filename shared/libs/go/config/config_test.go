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
  backends: [env]
log:
  level: "info"
`,
			want: AppConfig{
				LLMGateway: LLMGatewayConfig{
					Port:              14000,
					ModelProfilesPath: "./model_profiles.yaml",
					MetricsEnabled:    false,
				},
				Vault: VaultConfig{Backends: []string{"env"}},
				Log:   LogConfig{Level: "info"},
			},
		},
		{
			name: "minimal config",
			input: `
vault:
  backends: [env]
`,
			want: AppConfig{
				Vault: VaultConfig{Backends: []string{"env"}},
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
  backends: [file]
  file_path: "/etc/tern/vault.json"
  aes_enabled: true
`,
			want: AppConfig{
				Vault: VaultConfig{
					Backends:   []string{"file"},
					FilePath:   "/etc/tern/vault.json",
					AESEnabled: true,
				},
			},
		},
		{
			name: "backends list order",
			input: `
vault:
  backends: [keyring, env]
`,
			want: AppConfig{
				Vault: VaultConfig{Backends: []string{"keyring", "env"}},
			},
		},
		{
			name: "old backend field preserved",
			input: `
vault:
  backend: "keyring"
`,
			want: AppConfig{
				Vault: VaultConfig{Backend: "keyring"},
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

func TestLLMGatewayRetry_ZeroBecomesBoundedDefault(t *testing.T) {
	cfg := &AppConfig{}
	cfg.LLMGateway.ApplyDefaults()
	if cfg.LLMGateway.Retry.MaxRetries != 2 {
		t.Errorf("Retry.MaxRetries = %d, want 2", cfg.LLMGateway.Retry.MaxRetries)
	}
	if cfg.LLMGateway.Retry.InitialDelaySeconds != 1 {
		t.Errorf("Retry.InitialDelaySeconds = %d, want 1", cfg.LLMGateway.Retry.InitialDelaySeconds)
	}
	if cfg.LLMGateway.Retry.MaxDelaySeconds != 8 {
		t.Errorf("Retry.MaxDelaySeconds = %d, want 8", cfg.LLMGateway.Retry.MaxDelaySeconds)
	}
}

func TestAgentServiceProcessRetry_ZeroBecomesThree(t *testing.T) {
	cfg := &AppConfig{}
	cfg.AgentService.ApplyDefaults()
	if cfg.AgentService.ProcessRetry.MaxAttempts != 3 {
		t.Errorf("ProcessRetry.MaxAttempts = %d, want 3", cfg.AgentService.ProcessRetry.MaxAttempts)
	}
	if cfg.AgentService.ProcessRetry.IntervalSeconds != 3 {
		t.Errorf("ProcessRetry.IntervalSeconds = %d, want 3", cfg.AgentService.ProcessRetry.IntervalSeconds)
	}
}

func TestAgentServiceSSEReattachTimeout_ZeroBecomesNinety(t *testing.T) {
	cfg := &AppConfig{}
	cfg.AgentService.ApplyDefaults()
	if cfg.AgentService.SSEReattachTimeoutSeconds != 90 {
		t.Fatalf("SSEReattachTimeoutSeconds = %d, want 90", cfg.AgentService.SSEReattachTimeoutSeconds)
	}
}

func TestAgentServiceSSEReattachTimeout_NoOverwrite(t *testing.T) {
	cfg := &AppConfig{}
	cfg.AgentService.SSEReattachTimeoutSeconds = 30
	cfg.AgentService.ApplyDefaults()
	if cfg.AgentService.SSEReattachTimeoutSeconds != 30 {
		t.Fatalf("SSEReattachTimeoutSeconds = %d, want 30", cfg.AgentService.SSEReattachTimeoutSeconds)
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

func TestAgentServiceSupplement_YAMLLoad(t *testing.T) {
	input := `
agent_service:
  port: 3100
  supplement:
    algorithm: map_reduce
    model: ""
    max_chunk_messages: 20
    threshold_bytes: 32768
    recent_keep: 8
`
	var cfg AppConfig
	if err := yaml.Unmarshal([]byte(input), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.AgentService.Supplement.Algorithm != "map_reduce" {
		t.Errorf("algorithm = %q", cfg.AgentService.Supplement.Algorithm)
	}
	if cfg.AgentService.Supplement.MaxChunkMessages != 20 {
		t.Errorf("max_chunk_messages = %d", cfg.AgentService.Supplement.MaxChunkMessages)
	}
	if cfg.AgentService.Supplement.ThresholdBytes != 32768 {
		t.Errorf("threshold_bytes = %d", cfg.AgentService.Supplement.ThresholdBytes)
	}
	if cfg.AgentService.Supplement.RecentKeep != 8 {
		t.Errorf("recent_keep = %d", cfg.AgentService.Supplement.RecentKeep)
	}
}

func TestAgentServiceSupplement_UnspecifiedZero(t *testing.T) {
	var cfg AppConfig
	if err := yaml.Unmarshal([]byte("agent_service:\n  port: 1\n"), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.AgentService.Supplement.Algorithm != "" {
		t.Errorf("unspecified algorithm should be empty, got %q", cfg.AgentService.Supplement.Algorithm)
	}
}
