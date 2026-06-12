# 000-Foundation-Part1-Logger-Config-Vault

> **Source Specification**:
> - [000-Architecture.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/ideas/000-Architecture.md)
> - [002-ConfigAndSecrets.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/ideas/002-ConfigAndSecrets.md)

## Goal Description

HAGシステムの基盤層を構築する。具体的には:

1. **Logger interface + デフォルト実装**: 全コンポーネントが依存するロギング基盤
2. **Config loader**: `config.yaml` と `model_profiles.yaml` のロード (純粋関数)
3. **VaultStore**: APIキーのシークレット管理 (EnvVaultBackend)

この3つは他の全コンポーネント (hag.Server, LLM Gateway Proxy等) が依存する最も末端の層であり、ボトムアップ検証の起点となる。

## User Review Required

> [!IMPORTANT]
> **Go Module構成**: 現在 `features/hag/go.mod` が存在する。仕様書R6では `shared/libs/go/` 以下にパッケージを配置する設計だが、Go Module の分割方針 (モノリポ内の独立module vs ルートmodule) を決定する必要がある。本計画では `shared/libs/go/` 以下に配置し、ルートの `go.mod` で管理する方針とする。

> [!IMPORTANT]
> **VaultStore バックエンド**: 本Partでは `EnvVaultBackend` のみ実装する。`FileVaultBackend` と `KeyringVaultBackend` は将来のPartで実装する。

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| 000-R8-1: Logger interface定義 | `shared/libs/go/logger/logger.go` |
| 000-R8-2: デフォルト実装 (DefaultLogger) | `shared/libs/go/logger/default.go` |
| 000-R8-3: ログレベル debug/info/warn/error | `shared/libs/go/logger/level.go` |
| 000-R8-4: 構造化ログ | Logger.Debug(msg, fields...) |
| 000-R8-5: WithComponent | Logger.WithComponent(name) |
| 000-R8-6: WithLogger Option | Part2 (hag.Server) で実装 |
| 000-R8-7: Fatal/SetLevel不採用 | Logger interfaceに含めない |
| 000-R8-8: グローバルロガー廃止 | init() / globalLogger なし |
| 002-R1-1~R1-6: ModelProfilesConfig | `shared/libs/go/config/model_profiles.go` |
| 002-R3-1: AppConfig | `shared/libs/go/config/config.go` |
| 002-R4-1~R4-3: 設定ロード・バリデーション | `shared/libs/go/config/loader.go` |
| 002-R2-1: VaultStore interface | `shared/libs/go/vault/vault.go` |
| 002-R2-4: EnvVaultBackend | `shared/libs/go/vault/env_backend.go` |
| 002-R2-6: 環境変数名規約 | `shared/libs/go/vault/env_backend.go` |
| 002-R2-7: Resolve メソッド | `shared/libs/go/vault/vault.go` |
| 002-R2-2: マルチテナント対応 | VaultStore interfaceの設計で対応 |
| 002-R2-3: vault://参照形式 | `shared/libs/go/vault/resolve.go` |
| 002-R2-5: AES暗号化 | 将来のPart (FileVaultBackend) で実装 |
| 002-R2-4: FileVaultBackend | 将来のPartで実装 |
| 002-R2-4: KeyringVaultBackend | 将来のPartで実装 |
| 002-R4-4: 再設定API | 将来のPartで実装 (model_profiles reload) |
| 002-R5: プロバイダ固有設定 | Bifrost SDK委譲、本Partでは構造体定義のみ |

## Proposed Changes

### Logger パッケージ

#### [NEW] [level.go](file://shared/libs/go/logger/level.go)
*   **Description**: ログレベル定義
*   **Technical Design**:
    ```go
    package logger

    // Level represents log severity.
    type Level int

    const (
        LevelDebug Level = iota
        LevelInfo
        LevelWarn
        LevelError
    )

    // String returns the string representation of the level.
    func (l Level) String() string {
        switch l {
        case LevelDebug:
            return "DEBUG"
        case LevelInfo:
            return "INFO"
        case LevelWarn:
            return "WARN"
        case LevelError:
            return "ERROR"
        default:
            return "UNKNOWN"
        }
    }

    // ParseLevel parses a string into a Level.
    // Returns LevelInfo if the string is not recognized.
    func ParseLevel(s string) Level {
        switch strings.ToLower(s) {
        case "debug":
            return LevelDebug
        case "info":
            return LevelInfo
        case "warn", "warning":
            return LevelWarn
        case "error":
            return LevelError
        default:
            return LevelInfo
        }
    }
    ```

#### [NEW] [level_test.go](file://shared/libs/go/logger/level_test.go)
*   **Description**: ログレベルの単体テスト
*   **Technical Design**:
    ```go
    func TestLevel_String(t *testing.T) {
        tests := []struct {
            level Level
            want  string
        }{
            {LevelDebug, "DEBUG"},
            {LevelInfo, "INFO"},
            {LevelWarn, "WARN"},
            {LevelError, "ERROR"},
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
            {"debug", LevelDebug},
            {"DEBUG", LevelDebug},
            {"info", LevelInfo},
            {"warn", LevelWarn},
            {"warning", LevelWarn},
            {"error", LevelError},
            {"unknown", LevelInfo},  // default
            {"", LevelInfo},         // default
        }
        // ...
    }
    ```

#### [NEW] [logger.go](file://shared/libs/go/logger/logger.go)
*   **Description**: Logger interface定義 (000-R8-1)
*   **Technical Design**:
    ```go
    package logger

    // Logger defines the logging interface for HAG components.
    // In-Process users can inject their own implementation (slog, zap, syslog, etc.)
    // via hag.WithLogger(). If not provided, DefaultLogger is used.
    type Logger interface {
        // Debug logs a debug-level message with optional key-value fields.
        // fields are alternating key (string) and value (any) pairs.
        Debug(msg string, fields ...any)

        // Info logs an info-level message.
        Info(msg string, fields ...any)

        // Warn logs a warning-level message.
        Warn(msg string, fields ...any)

        // Error logs an error-level message.
        Error(msg string, fields ...any)

        // WithFields returns a child logger with additional fields.
        // The original logger is not modified (immutable).
        WithFields(fields map[string]any) Logger

        // WithComponent returns a child logger with "component" field set.
        // Called in each component's New() to tag subsequent logs.
        WithComponent(name string) Logger
    }
    ```
*   **Logic**: interfaceのみ定義。Fatal/SetLevel/SetOutputTypeは含めない (R8-7)。グローバル変数なし (R8-8)。

#### [NEW] [entry.go](file://shared/libs/go/logger/entry.go)
*   **Description**: ログエントリ構造体 (デフォルト実装内部)
*   **Technical Design**:
    ```go
    package logger

    import "time"

    // Entry represents a single log entry.
    type Entry struct {
        Timestamp time.Time      `json:"timestamp"`
        Level     Level          `json:"level"`
        Message   string         `json:"message"`
        Fields    map[string]any `json:"fields,omitempty"`
    }

    // NewEntry creates a new Entry with the current timestamp.
    func NewEntry(level Level, msg string) *Entry {
        return &Entry{
            Timestamp: time.Now(),
            Level:     level,
            Message:   msg,
            Fields:    make(map[string]any),
        }
    }
    ```

#### [NEW] [writer.go](file://shared/libs/go/logger/writer.go)
*   **Description**: LogWriter interface (デフォルト実装のプラグポイント)
*   **Technical Design**:
    ```go
    package logger

    import "io"

    // LogWriter writes formatted log output.
    type LogWriter interface {
        io.Closer
        // Write writes the payload. Level is provided for priority-aware writers (e.g. syslog).
        Write(level Level, payload []byte) (int, error)
    }
    ```

#### [NEW] [writer_stdout.go](file://shared/libs/go/logger/writer_stdout.go)
*   **Description**: 標準出力Writer
*   **Technical Design**:
    ```go
    package logger

    import "os"

    // StdoutWriter writes log output to os.Stdout (info/debug) and os.Stderr (warn/error).
    type StdoutWriter struct{}

    func NewStdoutWriter() *StdoutWriter { return &StdoutWriter{} }

    func (w *StdoutWriter) Write(level Level, payload []byte) (int, error) {
        if level >= LevelWarn {
            return os.Stderr.Write(payload)
        }
        return os.Stdout.Write(payload)
    }

    func (w *StdoutWriter) Close() error { return nil }
    ```

#### [NEW] [formatter.go](file://shared/libs/go/logger/formatter.go)
*   **Description**: Formatter interface + TextFormatter + JSONFormatter
*   **Technical Design**:
    ```go
    package logger

    // Formatter formats a log Entry into bytes.
    type Formatter interface {
        Format(*Entry) ([]byte, error)
    }

    // TextFormatter formats as "2026-01-01T00:00:00Z INFO message key=value" lines.
    type TextFormatter struct{}

    // JSONFormatter formats as JSON objects, one per line.
    type JSONFormatter struct{}
    ```
*   **Logic**:
    - `TextFormatter.Format`: `timestamp LEVEL message key1=val1 key2=val2\n`
    - `JSONFormatter.Format`: `json.Marshal(entry)` + newline
    - Fields are sorted alphabetically for deterministic output in TextFormatter.

#### [NEW] [default.go](file://shared/libs/go/logger/default.go)
*   **Description**: DefaultLogger (Logger interface実装)
*   **Technical Design**:
    ```go
    package logger

    import "sync"

    // DefaultLogger implements Logger with pluggable Formatter and LogWriter.
    type DefaultLogger struct {
        level     Level
        formatter Formatter
        writer    LogWriter
        fields    map[string]any
        mu        sync.RWMutex
    }

    // NewDefault creates a DefaultLogger with TextFormatter and StdoutWriter.
    func NewDefault(level Level) *DefaultLogger {
        return &DefaultLogger{
            level:     level,
            formatter: &TextFormatter{},
            writer:    NewStdoutWriter(),
            fields:    make(map[string]any),
        }
    }

    // NewDefaultWithOptions creates a DefaultLogger with custom formatter/writer.
    func NewDefaultWithOptions(level Level, formatter Formatter, writer LogWriter) *DefaultLogger

    func (l *DefaultLogger) Debug(msg string, fields ...any)
    func (l *DefaultLogger) Info(msg string, fields ...any)
    func (l *DefaultLogger) Warn(msg string, fields ...any)
    func (l *DefaultLogger) Error(msg string, fields ...any)

    // WithFields returns a new DefaultLogger with merged fields.
    func (l *DefaultLogger) WithFields(fields map[string]any) Logger

    // WithComponent returns a new DefaultLogger with "component" field.
    func (l *DefaultLogger) WithComponent(name string) Logger
    ```
*   **Logic**:
    - `shouldLog(level)`: `level >= l.level` の場合のみログ出力
    - `log(level, msg, fields)`: Entry生成 -> fieldsマージ (l.fields + 可変長fields) -> formatter.Format -> writer.Write
    - `WithFields`: l.fieldsのコピー + 新fieldsをマージした新しいDefaultLoggerを返す (immutable)
    - `WithComponent(name)`: `WithFields(map[string]any{"component": name})` のショートカット
    - 可変長fieldsの処理: `fields[0]=key, fields[1]=val, fields[2]=key, ...` のペアとして処理。奇数長の場合、最後のkeyは `"MISSING_VALUE"` をvalueとする

#### [NEW] [default_test.go](file://shared/libs/go/logger/default_test.go)
*   **Description**: DefaultLoggerの単体テスト
*   **Technical Design**:
    ```go
    // TestDefaultLogger_LevelFiltering: LevelInfo設定でDebugが出力されないこと
    // TestDefaultLogger_OutputFormat_Text: TextFormatterで期待形式のログが出力されること
    // TestDefaultLogger_OutputFormat_JSON: JSONFormatterで有効なJSONが出力されること
    // TestDefaultLogger_WithFields: 子ロガーにフィールドが追加されること
    // TestDefaultLogger_WithFields_Immutable: 元のロガーが変更されないこと
    // TestDefaultLogger_WithComponent: "component" フィールドが追加されること
    // TestDefaultLogger_FieldsParsing: 可変長fieldsが正しくパースされること
    // TestDefaultLogger_FieldsParsing_OddLength: 奇数長fieldsでMISSING_VALUEが設定されること
    // TestDefaultLogger_StderrForErrors: Error/WarnがStderrに出力されること
    ```
*   **Logic**: テスト用のbufferWriterを作成し、出力内容を検証する

---

### Config パッケージ

#### [NEW] [config_test.go](file://shared/libs/go/config/config_test.go)
*   **Description**: AppConfig構造体のテスト (TDD: テスト先行)
*   **Technical Design**:
    ```go
    func TestAppConfig_Defaults(t *testing.T) {
        // Zero value AppConfigのフィールドがデフォルト値であること
    }

    func TestAppConfig_YAMLUnmarshal(t *testing.T) {
        tests := []struct {
            name    string
            yaml    string
            want    AppConfig
            wantErr bool
        }{
            {
                name: "full config",
                yaml: `
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
                    LLMGateway: LLMGatewayConfig{Port: 14000, ModelProfilesPath: "./model_profiles.yaml"},
                    Vault:      VaultConfig{Backend: "env"},
                    Log:        LogConfig{Level: "info"},
                },
            },
            {
                name: "minimal config",
                yaml: `
vault:
  backend: "env"
`,
                // Port=0 (zero value), ModelProfilesPath="" ...
            },
        }
    }
    ```

#### [NEW] [config.go](file://shared/libs/go/config/config.go)
*   **Description**: AppConfig, LLMGatewayConfig, VaultConfig, LogConfig 構造体定義
*   **Technical Design**:
    ```go
    package config

    // AppConfig is the root configuration for HAG.
    type AppConfig struct {
        LLMGateway LLMGatewayConfig `yaml:"llm_gateway"`
        Vault      VaultConfig      `yaml:"vault"`
        Log        LogConfig        `yaml:"log"`
    }

    // LLMGatewayConfig holds LLM Gateway Proxy settings.
    type LLMGatewayConfig struct {
        Port              int    `yaml:"port"`
        ModelProfilesPath string `yaml:"model_profiles_path"`
        MetricsEnabled    bool   `yaml:"metrics_enabled"`
    }

    // VaultConfig holds VaultStore settings.
    type VaultConfig struct {
        Backend    string `yaml:"backend"`               // "env", "file", "keyring"
        FilePath   string `yaml:"file_path,omitempty"`
        AESEnabled bool   `yaml:"aes_enabled,omitempty"`
    }

    // LogConfig holds logging settings.
    type LogConfig struct {
        Level string `yaml:"level"` // "debug", "info", "warn", "error"
    }
    ```

#### [NEW] [model_profiles_test.go](file://shared/libs/go/config/model_profiles_test.go)
*   **Description**: ModelProfilesConfigのテスト (TDD)
*   **Technical Design**:
    ```go
    func TestModelProfilesConfig_YAMLUnmarshal(t *testing.T) {
        // Complete model_profiles.yaml parse
    }

    func TestModelProfilesConfig_Validate(t *testing.T) {
        tests := []struct {
            name    string
            config  ModelProfilesConfig
            wantErr bool
        }{
            {"valid", validConfig(), false},
            {"empty providers", emptyProviders(), true},
            {"empty model name", emptyModelName(), true},
        }
    }

    func TestModelBehavior_ToolCallFallback(t *testing.T) {
        // behavior.tool_call_fallback: true がパースされること
    }
    ```

#### [NEW] [model_profiles.go](file://shared/libs/go/config/model_profiles.go)
*   **Description**: ModelProfilesConfig 構造体群
*   **Technical Design**:
    ```go
    package config

    // ModelProfilesConfig represents model_profiles.yaml.
    type ModelProfilesConfig struct {
        DefaultProfile DefaultProfileConfig         `yaml:"default_profile"`
        Providers      map[string]ProviderConfig    `yaml:"providers"`
        Governance     GovernanceConfig             `yaml:"governance,omitempty"`
    }

    type DefaultProfileConfig struct {
        Provider string `yaml:"provider"`
        Model    string `yaml:"model"`
    }

    type ProviderConfig struct {
        Keys          []KeyConfig     `yaml:"keys"`
        NetworkConfig *NetworkConfig  `yaml:"network_config,omitempty"`
    }

    type KeyConfig struct {
        Name   string        `yaml:"name"`
        Value  string        `yaml:"value"`
        Weight float64       `yaml:"weight,omitempty"`
        Models []ModelConfig `yaml:"models"`
    }

    type ModelConfig struct {
        Name     string         `yaml:"name"`
        Behavior *ModelBehavior `yaml:"behavior,omitempty"`
    }

    type ModelBehavior struct {
        ToolCallFallback bool `yaml:"tool_call_fallback"`
    }

    type NetworkConfig struct {
        BaseURL               string `yaml:"base_url,omitempty"`
        RequestTimeoutSeconds int    `yaml:"request_timeout_seconds,omitempty"`
    }

    // GovernanceConfig holds routing rules (future implementation).
    // TODO: CEL-based routing control
    type GovernanceConfig struct {
        RoutingRules []any `yaml:"routing_rules,omitempty"`
    }

    // Validate checks the ModelProfilesConfig for correctness.
    func (c *ModelProfilesConfig) Validate() error
    ```
*   **Logic** (Validate):
    - `Providers` が空でないこと
    - 各Provider内の `Keys` が空でないこと
    - 各Key内の `Models` が空でないこと (空の場合はワイルドカード "*" 扱い)
    - 各ModelのNameが空でないこと
    - `DefaultProfile.Provider` が `Providers` に存在すること

#### [NEW] [loader_test.go](file://shared/libs/go/config/loader_test.go)
*   **Description**: 設定ロードのテスト (TDD)
*   **Technical Design**:
    ```go
    func TestLoad_ValidConfig(t *testing.T) {
        // テスト用YAMLファイルをtmp作成 -> Load -> 内容検証
    }

    func TestLoad_FileNotFound(t *testing.T) {
        _, err := Load("/nonexistent/config.yaml")
        if err == nil { t.Fatal("expected error") }
    }

    func TestLoad_InvalidYAML(t *testing.T) {
        // 不正YAMLでエラーが返ること
    }

    func TestLoadModelProfiles_Valid(t *testing.T) {
        // model_profiles.yaml ロードとバリデーション
    }

    func TestLoadModelProfiles_ValidationError(t *testing.T) {
        // バリデーションエラーが返ること
    }
    ```

#### [NEW] [loader.go](file://shared/libs/go/config/loader.go)
*   **Description**: 設定ファイルロード (純粋関数)
*   **Technical Design**:
    ```go
    package config

    import (
        "fmt"
        "os"
        "gopkg.in/yaml.v3"
    )

    // Load reads a config.yaml file and returns an AppConfig.
    // This is a pure function: it reads YAML, parses into struct, and returns.
    // It does NOT resolve vault:// references or wire dependencies.
    func Load(path string) (*AppConfig, error) {
        data, err := os.ReadFile(path)
        if err != nil {
            return nil, fmt.Errorf("failed to read config file %s: %w", path, err)
        }
        var cfg AppConfig
        if err := yaml.Unmarshal(data, &cfg); err != nil {
            return nil, fmt.Errorf("failed to parse config file %s: %w", path, err)
        }
        return &cfg, nil
    }

    // LoadModelProfiles reads a model_profiles.yaml file and validates it.
    func LoadModelProfiles(path string) (*ModelProfilesConfig, error) {
        data, err := os.ReadFile(path)
        if err != nil {
            return nil, fmt.Errorf("failed to read model profiles %s: %w", path, err)
        }
        var cfg ModelProfilesConfig
        if err := yaml.Unmarshal(data, &cfg); err != nil {
            return nil, fmt.Errorf("failed to parse model profiles %s: %w", path, err)
        }
        if err := cfg.Validate(); err != nil {
            return nil, fmt.Errorf("model profiles validation failed: %w", err)
        }
        return &cfg, nil
    }
    ```

---

### Vault パッケージ

#### [NEW] [vault_test.go](file://shared/libs/go/vault/vault_test.go)
*   **Description**: VaultStore interfaceテスト (TDD)
*   **Technical Design**:
    ```go
    // TestVaultStore_InterfaceCompliance: EnvVaultBackendがVaultStoreを満たすこと
    ```

#### [NEW] [vault.go](file://shared/libs/go/vault/vault.go)
*   **Description**: VaultStore interface定義
*   **Technical Design**:
    ```go
    package vault

    // VaultStore manages secret storage and retrieval.
    type VaultStore interface {
        // Resolve resolves a vault:// reference to the actual secret value.
        Resolve(ref string) (string, error)

        // Set stores a secret at the given path.
        Set(path string, value string) error

        // Delete removes a secret at the given path.
        Delete(path string) error

        // List returns all stored secret paths.
        List() ([]string, error)
    }
    ```

#### [NEW] [resolve_test.go](file://shared/libs/go/vault/resolve_test.go)
*   **Description**: vault://参照パースのテスト (TDD)
*   **Technical Design**:
    ```go
    func TestParseVaultRef(t *testing.T) {
        tests := []struct {
            input   string
            isVault bool
            path    string
        }{
            {"vault://providers/anthropic/primary", true, "providers/anthropic/primary"},
            {"vault://providers/openai/primary", true, "providers/openai/primary"},
            {"sk-ant-xxxxx", false, ""},
            {"", false, ""},
            {"vault://", true, ""},
        }
    }
    ```

#### [NEW] [resolve.go](file://shared/libs/go/vault/resolve.go)
*   **Description**: vault://参照パース・解決ユーティリティ
*   **Technical Design**:
    ```go
    package vault

    import "strings"

    const vaultPrefix = "vault://"

    // IsVaultRef returns true if the value is a vault:// reference.
    func IsVaultRef(value string) bool {
        return strings.HasPrefix(value, vaultPrefix)
    }

    // ParseVaultRef extracts the path from a vault:// reference.
    // Returns the path portion (e.g. "providers/anthropic/primary") and true,
    // or empty string and false if not a vault reference.
    func ParseVaultRef(value string) (string, bool) {
        if !IsVaultRef(value) {
            return "", false
        }
        return strings.TrimPrefix(value, vaultPrefix), true
    }
    ```

#### [NEW] [env_backend_test.go](file://shared/libs/go/vault/env_backend_test.go)
*   **Description**: EnvVaultBackendのテスト (TDD)
*   **Technical Design**:
    ```go
    func TestEnvVaultBackend_Resolve(t *testing.T) {
        tests := []struct {
            name    string
            ref     string
            envKey  string
            envVal  string
            want    string
            wantErr bool
        }{
            {
                name:   "anthropic key",
                ref:    "vault://providers/anthropic/primary",
                envKey: "TERN_VAULT_ANTHROPIC_PRIMARY",
                envVal: "sk-ant-test123",
                want:   "sk-ant-test123",
            },
            {
                name:    "missing env var",
                ref:     "vault://providers/openai/primary",
                envKey:  "TERN_VAULT_OPENAI_PRIMARY",
                wantErr: true,
            },
            {
                name:    "not a vault ref",
                ref:     "plaintext-key",
                wantErr: true,
            },
        }
    }

    func TestEnvVaultBackend_PathToEnvName(t *testing.T) {
        tests := []struct {
            path string
            want string
        }{
            {"providers/anthropic/primary", "TERN_VAULT_ANTHROPIC_PRIMARY"},
            {"providers/openai/team-a", "TERN_VAULT_OPENAI_TEAM_A"},
            {"providers/ollama/default", "TERN_VAULT_OLLAMA_DEFAULT"},
        }
    }
    ```

#### [NEW] [env_backend.go](file://shared/libs/go/vault/env_backend.go)
*   **Description**: 環境変数ベースのVaultStore実装
*   **Technical Design**:
    ```go
    package vault

    import (
        "fmt"
        "os"
        "strings"
    )

    // EnvVaultBackend resolves secrets from environment variables.
    // vault://providers/{provider}/{key} -> TERN_VAULT_{PROVIDER}_{KEY}
    type EnvVaultBackend struct{}

    func NewEnvVaultBackend() *EnvVaultBackend {
        return &EnvVaultBackend{}
    }

    func (b *EnvVaultBackend) Resolve(ref string) (string, error) {
        path, ok := ParseVaultRef(ref)
        if !ok {
            return "", fmt.Errorf("not a vault reference: %s", ref)
        }
        envName := pathToEnvName(path)
        val := os.Getenv(envName)
        if val == "" {
            return "", fmt.Errorf("environment variable %s not set (for vault ref %s)", envName, ref)
        }
        return val, nil
    }

    func (b *EnvVaultBackend) Set(path string, value string) error {
        return os.Setenv(pathToEnvName(path), value)
    }

    func (b *EnvVaultBackend) Delete(path string) error {
        return os.Unsetenv(pathToEnvName(path))
    }

    func (b *EnvVaultBackend) List() ([]string, error) {
        // Scan environment for TERN_VAULT_ prefix
        var paths []string
        for _, env := range os.Environ() {
            parts := strings.SplitN(env, "=", 2)
            if strings.HasPrefix(parts[0], "TERN_VAULT_") {
                paths = append(paths, envNameToPath(parts[0]))
            }
        }
        return paths, nil
    }
    ```
*   **Logic** (pathToEnvName):
    1. `vault://` prefix を除去 (ParseVaultRefで済み)
    2. pathが `providers/{provider}/{key}` 形式の場合、`providers/` prefix を除去
    3. `/` を `_` に置換
    4. 大文字化
    5. `TERN_VAULT_` prefix を付与
    6. `-` を `_` に置換 (team-a -> TEAM_A)

---

### Go Module セットアップ

#### [NEW] [go.mod](file://shared/libs/go/go.mod)
*   **Description**: shared/libs/go用のGoモジュール定義
*   **Technical Design**:
    ```
    module github.com/axsh/hag

    go 1.24.0

    require (
        gopkg.in/yaml.v3 v3.0.1
    )
    ```

## Step-by-Step Implementation Guide

> [!IMPORTANT]
> TDDサイクル: 各ステップでテストを先に書き、失敗を確認してから実装する。

1. **Step 1: Go Module初期化** [x]
    - `shared/libs/go/go.mod` を作成
    - `go mod tidy` で依存解決

2. **Step 2: Logger - Level (TDD)** [x]
    - `shared/libs/go/logger/level_test.go` を作成 (RED)
    - `shared/libs/go/logger/level.go` を実装 (GREEN)
    - ビルド確認

3. **Step 3: Logger - Entry + Writer + Formatter (TDD)** [x]
    - `shared/libs/go/logger/entry.go` を作成
    - `shared/libs/go/logger/writer.go` + `writer_stdout.go` を作成
    - `shared/libs/go/logger/formatter.go` を作成 (TextFormatter + JSONFormatter)
    - 単体テストを作成・通過確認

4. **Step 4: Logger - Interface + DefaultLogger (TDD)** [x]
    - `shared/libs/go/logger/default_test.go` を作成 (RED)
    - `shared/libs/go/logger/logger.go` (interface) を作成
    - `shared/libs/go/logger/default.go` を実装 (GREEN)
    - 全Logger テスト通過確認

5. **Step 5: Config - AppConfig (TDD)** [x]
    - `shared/libs/go/config/config_test.go` を作成 (RED)
    - `shared/libs/go/config/config.go` を実装 (GREEN)

6. **Step 6: Config - ModelProfilesConfig (TDD)** [x]
    - `shared/libs/go/config/model_profiles_test.go` を作成 (RED)
    - `shared/libs/go/config/model_profiles.go` を実装 (GREEN)

7. **Step 7: Config - Loader (TDD)** [x]
    - `shared/libs/go/config/loader_test.go` を作成 (RED)
    - `shared/libs/go/config/loader.go` を実装 (GREEN)

8. **Step 8: Vault - Interface + Resolve (TDD)** [x]
    - `shared/libs/go/vault/resolve_test.go` を作成 (RED)
    - `shared/libs/go/vault/vault.go` + `resolve.go` を実装 (GREEN)

9. **Step 9: Vault - EnvVaultBackend (TDD)** [x]
    - `shared/libs/go/vault/env_backend_test.go` を作成 (RED)
    - `shared/libs/go/vault/env_backend.go` を実装 (GREEN)

10. **Step 10: ビルド検証** [x]
    - `scripts/process/build.sh` を実行して全テスト通過を確認
    - `build.sh` に `shared/libs/go` のテスト実行サポートを追加

11. **Step 11: Gitコミット** [x]
    - logger: be8fc6c
    - config: ba55c43
    - vault: 9cd10fd
    - build.sh: d23345a

## Verification Plan

### Automated Verification

1. **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```

2. **個別パッケージテスト** (開発中の確認用):
    注: 正式検証は必ず `build.sh` を使用すること。
    開発中のフィードバック用に、以下を実行可能:
    ```bash
    cd shared/libs/go && go test ./logger/... ./config/... ./vault/...
    ```

### テスト項目のセルフレビュー (11.4)

1. **網羅性**: Logger (level, format, fields, component, immutability), Config (YAML parse, defaults, validation), Vault (resolve, env backend, path mapping) の全主要機能をカバー
2. **証拠の十分性**: 各テストは期待値との比較を行い、「エラーが出ない」だけでなく「正しい値が返る」を検証
3. **迂回排除**: buffer writerを使ってログの実出力内容を検証。環境変数の設定/未設定を明示的に制御
4. **依存関係**: Logger -> Config -> Vault のボトムアップ順でテストし、下位の動作を前提に上位を検証

## Documentation

#### [MODIFY] [design_decisions.md](file://prompts/designs/hag/design_decisions.md)
*   **更新内容**: 実装完了後、DD-040 (ロガー) の現状を更新

---

## 継続計画について

本計画はPart1 (基盤層) です。以下のPartが続きます:

- **Part2**: hag.Server Facade + LLM Gateway Proxy (000-Architecture R1-R7, 001-LLMGatewayProxy 全体)
- **Part3**: Hierarchical Agent Log + Examples (003-HierarchicalAgentLog, standalone example)
