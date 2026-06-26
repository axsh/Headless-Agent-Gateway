# 057-ModelProfiles-Restructure

> **Source Specification**: [046-ModelProfiles-Restructure.md](../ideas/046-ModelProfiles-Restructure.md)

## Goal Description

`model_profiles.yaml` のフィールド名変更 (`keys` -> `api_keys`, `value` -> `secret`)、`secret` フィールドの nil 許可、Ollama 接続先設定の明示化、スキーマ説明コメント追加、examples の整備、`settings/` フォルダへの設定ファイル移動、README 更新を行う。

## User Review Required

> [!IMPORTANT]
> `keys` -> `api_keys`、`value` -> `secret` は破壊的変更です。既存の `model_profiles.yaml` を使用している環境では、フィールド名の更新が必要になります。

None. (全て仕様書レビューで確認済み)

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: `keys` -> `api_keys`, `value` -> `secret` | Proposed Changes > config/model_profiles.go, 全 YAML ファイル |
| R2: `secret` フィールドの nil 許可 | Proposed Changes > config/model_profiles.go (omitempty), bifrost_account.go |
| R3: Ollama 接続先設定の明示化 | Proposed Changes > features/tern/model_profiles.yaml, settings/ YAML |
| R4: スキーマ説明コメント追加 | Proposed Changes > features/tern/model_profiles.yaml |
| R5-1: minimal-server に model_profiles.yaml 追加 | Proposed Changes > examples/minimal-server/model_profiles.yaml |
| R5-2: minimal-server/main.go 修正 | Proposed Changes > examples/minimal-server/main.go |
| R5-3: minimal-client/main.go 修正 | Proposed Changes > examples/minimal-client/main.go |
| R6: settings/ フォルダ新設 | Proposed Changes > settings/ |
| R7: README.md 更新 | Proposed Changes > README.md |

## Proposed Changes

### config パッケージ (Go構造体定義)

#### [MODIFY] [model_profiles.go](file://shared/libs/go/config/model_profiles.go)

*   **Description**: `ProviderConfig.Keys` -> `ApiKeys` (yaml: `api_keys`)、`KeyConfig.Value` -> `Secret` (yaml: `secret,omitempty`) に変更
*   **Technical Design**:
    ```go
    // ProviderConfig holds per-provider configuration.
    type ProviderConfig struct {
        ApiKeys       []KeyConfig    `yaml:"api_keys"`
        NetworkConfig *NetworkConfig `yaml:"network_config,omitempty"`
    }

    // KeyConfig holds an API key configuration.
    type KeyConfig struct {
        Name   string        `yaml:"name"`
        Secret string        `yaml:"secret,omitempty"`
        Weight float64       `yaml:"weight,omitempty"`
        Models []ModelConfig `yaml:"models"`
    }
    ```
*   **Logic (Validate)**:
    *   L70: `prov.Keys` -> `prov.ApiKeys` に変更
    *   L73: `prov.Keys` -> `prov.ApiKeys` に変更 (バリデーションループ)
    *   L92: `prov.Keys` -> `prov.ApiKeys` に変更 (logical_name ループ)
    *   バリデーション: `len(prov.ApiKeys) == 0` のチェックは維持 (少なくとも1つの api_key エントリは必要)
    *   `key.Secret` の空チェックは追加しない (nil 許可)

---

#### [MODIFY] [model_profiles_test.go](file://shared/libs/go/config/model_profiles_test.go)

*   **Description**: テスト内の YAML リテラルと Go 構造体リテラルを新フィールド名に更新
*   **Technical Design**:
    *   `testModelProfilesYAML` (L9-L46): `keys:` -> `api_keys:`, `value:` -> `secret:`
    *   `TestModelProfilesConfig_YAMLUnmarshal` (L48-L105): `.Keys` -> `.ApiKeys`, `.Value` -> `.Secret`
    *   `TestModelProfilesConfig_Validate` (L107-L196): `Keys:` -> `ApiKeys:`, `Value:` -> `Secret:`
    *   **新規テストケース追加**: `secret` が空/省略されたプロバイダーでもバリデーションが通ることを確認

    ```go
    // 既存テストの validConfig 関数を更新:
    func() ModelProfilesConfig {
        return ModelProfilesConfig{
            DefaultProfile: DefaultProfileConfig{Provider: "anthropic", Model: "claude-sonnet-4-20250514"},
            Providers: map[string]ProviderConfig{
                "anthropic": {
                    ApiKeys: []KeyConfig{
                        {Name: "primary", Secret: "vault://x", Models: []ModelConfig{{Name: "claude-sonnet-4-20250514"}}},
                    },
                },
            },
        }
    }

    // 新規テストケース:
    {
        name:    "empty secret is valid",
        modify:  func(c *ModelProfilesConfig) {
            c.Providers["anthropic"] = ProviderConfig{
                ApiKeys: []KeyConfig{
                    {Name: "default", Secret: "", Models: []ModelConfig{{Name: "claude-sonnet-4-20250514"}}},
                },
            }
        },
        wantErr: false,
    },
    ```

---

#### [MODIFY] [loader_test.go](file://shared/libs/go/config/loader_test.go)

*   **Description**: `TestLoadModelProfiles_Valid` のYAMLリテラルとアサーションを更新
*   **Logic**:
    *   L69: `keys:` -> `api_keys:`
    *   L71: `value:` -> `secret:`
    *   L88: `.Keys[0].Value` -> `.ApiKeys[0].Secret`
    *   L89: 同上

---

### llmgateway パッケージ (ルーティング・Bifrost連携)

#### [MODIFY] [routing.go](file://shared/libs/go/llmgateway/routing.go)

*   **Description**: `provider.Keys` -> `provider.ApiKeys`, `key.Value` -> `key.Secret` に変更
*   **Logic**:
    *   L62: `for _, key := range provider.Keys {` -> `for _, key := range provider.ApiKeys {`
    *   L72: `KeyValue: key.Value,` -> `KeyValue: key.Secret,`

---

#### [MODIFY] [routing_test.go](file://shared/libs/go/llmgateway/routing_test.go)

*   **Description**: `testProfiles()` ヘルパーと全テストの構造体リテラルを更新
*   **Logic**:
    *   `testProfiles()` 関数内の全ての `Keys:` -> `ApiKeys:`, `Value:` -> `Secret:`
    *   L16-L25 (anthropic): `Keys: []config.KeyConfig{{..., Value: "sk-ant-test-key", ...}}` -> `ApiKeys: []config.KeyConfig{{..., Secret: "sk-ant-test-key", ...}}`
    *   L28-L38 (openai): 同上
    *   L41-L49 (google): 同上

---

#### [MODIFY] [bifrost_account.go](file://shared/libs/go/llmgateway/bifrost_account.go)

*   **Description**: `.Keys` -> `.ApiKeys`, `.Value` -> `.Secret`、`Secret` が空の場合の処理追加
*   **Logic**:
    *   L98: `len(provCfg.Keys)` -> `len(provCfg.ApiKeys)`
    *   L99: `provCfg.Keys` -> `provCfg.ApiKeys`
    *   L101: `keyCfg.Value` -> `keyCfg.Secret`
    *   L102: `vault.IsVaultRef(keyValue)` の前に `keyValue != ""` チェックを追加
    *   修正後のロジック:
    ```go
    keyValue := keyCfg.Secret
    if keyValue != "" && vault.IsVaultRef(keyValue) && a.vault != nil {
        resolved, err := a.vault.Resolve(keyValue)
        if err != nil {
            if a.logger != nil {
                a.logger.Warn("failed to resolve vault ref for key %q: %v", keyCfg.Name, err)
            }
            resolved = keyValue
        }
        keyValue = resolved
    }
    ```

---

#### [MODIFY] [bifrost_account_test.go](file://shared/libs/go/llmgateway/bifrost_account_test.go)

*   **Description**: テスト内の `Keys:` -> `ApiKeys:`, `Value:` -> `Secret:` を更新
*   **Logic**:
    *   L104: `Keys:` -> `ApiKeys:`
    *   L107: `Value:` -> `Secret:`
    *   `testProfiles()` ヘルパーは `routing_test.go` で更新済みなので、このファイルの直接リテラルのみ更新

---

#### [MODIFY] [proxy.go](file://shared/libs/go/llmgateway/proxy.go)

*   **Description**: `provider.Keys` -> `provider.ApiKeys` の3箇所を更新
*   **Logic**:
    *   L155: `for _, key := range provider.Keys {` -> `for _, key := range provider.ApiKeys {`
    *   L171: 同上
    *   L200: `for _, key := range prov.Keys {` -> `for _, key := range prov.ApiKeys {`

---

### agentservice パッケージ

#### [MODIFY] [service.go](file://shared/libs/go/agentservice/service.go)

*   **Description**: `prov.Keys` -> `prov.ApiKeys` を更新
*   **Logic**:
    *   L401: `for _, key := range prov.Keys {` -> `for _, key := range prov.ApiKeys {`

---

### YAML ファイル (設定ファイル群)

#### [MODIFY] [model_profiles.yaml](file://features/tern/model_profiles.yaml)

*   **Description**: フィールド名変更 + スキーマコメント追加 + Ollama `network_config` 追加
*   **Technical Design**: 以下の内容に完全置換
    ```yaml
    # ============================================================
    # model_profiles.yaml - モデルプロファイル設定
    # ============================================================
    #
    # スキーマ:
    #   default_profile:
    #     provider: <string>         # デフォルトで使用するプロバイダー名
    #     model: <string>            # デフォルトで使用するモデル名
    #
    #   providers:
    #     <provider_name>:           # プロバイダー識別子 (openai, anthropic, google, ollama)
    #       api_keys:                # APIキー設定のリスト
    #         - name: <string>       # キーの識別名
    #           secret: <string>     # APIキーの値 (省略可)
    #                                #   - vault://... : Vault参照URI
    #                                #   - sk-xxx...   : APIキー直接指定
    #                                #   - (省略)      : 認証不要のプロバイダー (例: Ollama)
    #           weight: <float>      # ロードバランシング重み (省略時: 1.0)
    #           models:              # このキーで利用可能なモデルのリスト
    #             - name: <string>   # モデル識別子
    #               logical_name: <string>  # 論理名エイリアス (省略可)
    #               mode: <string>          # APIモード: "chat" (既定) / "responses"
    #               behavior:               # モデル固有の動作設定 (省略可)
    #                 tool_call_fallback: <bool>  # テキストからツールコール変換 (ローカルLLM用)
    #       network_config:          # ネットワーク設定 (省略可)
    #         base_url: <string>     # APIベースURL (プロバイダーデフォルトを上書き)
    #         request_timeout_seconds: <int>  # リクエストタイムアウト秒数
    #
    # ============================================================

    # デフォルトプロファイル: 未指定時に使用されるプロバイダーとモデル
    default_profile:
      provider: google
      model: gemini-2.5-flash

    providers:
      # --- OpenAI ---
      openai:
        api_keys:
          - name: default
            secret: vault://providers/openai/default
            models:
              - name: gpt-4o
              - name: gpt-4o-mini
              - name: gpt-4.1-mini
              - name: gpt-5.4-mini
              - name: gpt-5.5
              - name: gpt-5.5-pro
              - name: gpt-5.3-codex
                mode: responses    # Codex CLI 用 Responses API モード

      # --- Anthropic ---
      anthropic:
        api_keys:
          - name: default
            secret: vault://providers/anthropic/default
            models:
              - name: claude-sonnet-4-20250514
              - name: claude-opus-4-20250514
              - name: claude-sonnet-4-6
              - name: claude-opus-4-8
              - name: claude-haiku-4-5

      # --- Google ---
      google:
        api_keys:
          - name: default
            secret: vault://providers/google/default
            models:
              - name: gemini-2.5-flash
              - name: gemini-2.5-pro
              - name: gemini-3.5-flash

      # --- Ollama (ローカル推論) ---
      # secret は不要 (認証なし)
      # network_config.base_url でOllamaサーバーのアドレスを指定
      ollama:
        api_keys:
          - name: default
            # secret を省略: Ollama は認証キー不要
            models:
              - name: qwen2.5-coder:7b
                behavior:
                  tool_call_fallback: true  # ローカルLLM用テキスト→ツールコール変換
        network_config:
          base_url: "http://localhost:11434"  # リモートの場合: "http://remote-host:11434"
    ```

---

#### [MODIFY] [model_profiles.yaml (testdata)](file://tests/testdata/model_profiles.yaml)

*   **Description**: テストデータのフィールド名を更新
*   **Logic**: `keys:` -> `api_keys:`, `value:` -> `secret:` (4箇所)
    *   Ollama の `secret` は空文字列 `""` を省略 (フィールドごと削除)

---

#### [NEW] [model_profiles.yaml (examples/minimal-server)](file://examples/minimal-server/model_profiles.yaml)

*   **Description**: ミニマルな model_profiles.yaml の例を新規作成
*   **Technical Design**: 全プロバイダーの最安モデルを1つずつ含む
    ```yaml
    # ミニマル model_profiles.yaml の例
    # 各プロバイダーの最もコスト効率の高いモデルを1つずつ設定

    # デフォルトプロファイル
    default_profile:
      provider: ollama
      model: qwen2.5-coder:7b

    providers:
      # OpenAI (最安: gpt-4o-mini)
      openai:
        api_keys:
          - name: default
            secret: vault://providers/openai/default
            models:
              - name: gpt-4o-mini

      # Anthropic (最安: claude-haiku-4-5)
      anthropic:
        api_keys:
          - name: default
            secret: vault://providers/anthropic/default
            models:
              - name: claude-haiku-4-5

      # Google (最安: gemini-2.5-flash)
      google:
        api_keys:
          - name: default
            secret: vault://providers/google/default
            models:
              - name: gemini-2.5-flash

      # Ollama (ローカル推論、APIキー不要)
      ollama:
        api_keys:
          - name: default
            models:
              - name: qwen2.5-coder:7b
                behavior:
                  tool_call_fallback: true
        network_config:
          base_url: "http://localhost:11434"
    ```

---

### examples の修正

#### [MODIFY] [main.go (examples/minimal-server)](file://examples/minimal-server/main.go)

*   **Description**: コマンドライン引数説明の修正、具体的なコマンドライン例追加、コメント充実
*   **Technical Design**: 以下に完全置換
    ```go
    // minimal-server demonstrates the simplest way to start a tern server.
    // It loads configuration from a YAML file and starts the server with
    // automatic coding agent registration via init() imports.
    //
    // Usage:
    //
    //   go run . -config config.yaml
    //
    // Examples:
    //
    //   # Run with default config.yaml in current directory
    //   go run .
    //
    //   # Run with a specific config file
    //   go run . -config ./settings/example/config.yaml
    //
    //   # Run the built binary
    //   ./bin/minimal-server -config ./settings/example/config.yaml
    package main

    import (
        "context"
        "fmt"
        "log"
        "os"
        "os/signal"
        "syscall"

        "github.com/axsh/arctic-tern/tern"
    )

    func main() {
        // Default config path; override with -config flag.
        configPath := "config.yaml"
        if len(os.Args) > 2 && os.Args[1] == "-config" {
            configPath = os.Args[2]
        }

        // Initialize the tern server with the given config.
        // tern.New automatically registers all built-in coding agents
        // (Claude Code, Codex, Wayfinder, etc.) and LLM providers
        // (OpenAI, Anthropic, Google, Ollama) via init() imports.
        srv, err := tern.New(tern.WithConfigPath(configPath))
        if err != nil {
            log.Fatalf("failed to initialize: %v", err)
        }

        // Launch starts the CAWA Agent Service and LLM Gateway.
        ctx := context.Background()
        if err := srv.Launch(ctx); err != nil {
            log.Fatalf("failed to launch: %v", err)
        }
        defer srv.Shutdown(ctx)

        fmt.Printf("tern server running on http://localhost:%d\n", srv.AgentService().Port())

        // Wait for interrupt signal to gracefully shut down.
        sigChan := make(chan os.Signal, 1)
        signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
        <-sigChan

        fmt.Println("shutting down...")
    }
    ```

---

#### [MODIFY] [main.go (examples/minimal-client)](file://examples/minimal-client/main.go)

*   **Description**: デフォルト Agent/Model を wayfinder/qwen2.5-coder:7b に変更、コメント追加、コマンドライン例追記
*   **Technical Design**: 以下に完全置換
    ```go
    // minimal-client demonstrates the simplest way to interact with a running
    // tern server using the client library. It creates a session, sends a message,
    // and streams the response to stdout.
    //
    // Prerequisites:
    //   - A running tern server (e.g., via minimal-server)
    //   - For the wayfinder agent: Ollama running with qwen2.5-coder:7b model
    //
    // Usage:
    //
    //   go run . [server-url]
    //
    // Examples:
    //
    //   # Connect to default server (localhost:3100)
    //   go run .
    //
    //   # Connect to a remote server
    //   go run . http://192.168.1.100:3100
    //
    //   # Run the built binary
    //   ./bin/minimal-client http://localhost:3100
    package main

    import (
        "context"
        "log"
        "os"

        "github.com/axsh/arctic-tern/client"
    )

    func main() {
        // Default server URL; override with first argument.
        serverURL := "http://localhost:3100"
        if len(os.Args) > 1 {
            serverURL = os.Args[1]
        }

        ctx := context.Background()

        // Create a new client pointing to the tern server.
        c := client.New(serverURL)

        // Create a session with the wayfinder agent and a local Ollama model.
        // Agent: "wayfinder" - Tern's built-in coding agent
        // Model: "qwen2.5-coder:7b" - A local model running on Ollama
        session, err := c.CreateSession(ctx, client.SessionRequest{
            Agent:   "wayfinder",
            Model:   "qwen2.5-coder:7b",
            WorkDir: ".",
        })
        if err != nil {
            log.Fatalf("create session: %v", err)
        }
        defer session.Terminate(ctx)
        log.Printf("Session: %s", session.ID)

        // Send a message and stream the response to stdout.
        stream, err := session.SendMessage(ctx, "Create a file called hello.txt with the content 'Hello, World!'")
        if err != nil {
            log.Fatalf("send message: %v", err)
        }

        // Output prints each streamed event to the provided writer.
        if err := stream.Output(os.Stdout); err != nil {
            log.Fatalf("stream output: %v", err)
        }
    }
    ```

---

### settings/ フォルダ (新設)

#### [NEW] [config.yaml (settings/example)](file://settings/example/config.yaml)

*   **Description**: examples 用のシンプルな設定ファイル
    ```yaml
    # Tern サーバー設定 (example用)
    # examples/minimal-server で使用するシンプルな設定

    llm_gateway:
      port: 14000
      model_profiles_path: "model_profiles.yaml"

    vault:
      backend: "keyring"

    agent_service:
      port: 3100

    log:
      level: "info"
      outputs:
        - type: "stdout"
    ```

---

#### [NEW] [model_profiles.yaml (settings/example)](file://settings/example/model_profiles.yaml)

*   **Description**: examples 用のモデルプロファイル
*   **Logic**: `examples/minimal-server/model_profiles.yaml` と同内容

---

#### [NEW] [config.yaml (settings/demo)](file://settings/demo/config.yaml)

*   **Description**: 本番寄りの設定 (`features/tern/config.yaml` を移動)
*   **Logic**: 既存の `features/tern/config.yaml` の内容をそのまま移動 (コメント追加)

---

#### [NEW] [model_profiles.yaml (settings/demo)](file://settings/demo/model_profiles.yaml)

*   **Description**: 本番寄りのモデルプロファイル (`features/tern/model_profiles.yaml` を移動)
*   **Logic**: 更新後の `features/tern/model_profiles.yaml` の内容をそのまま移動

---

#### [MODIFY] [config.yaml (features/tern)](file://features/tern/config.yaml)

*   **Description**: `model_profiles_path` を `settings/demo/model_profiles.yaml` への相対パスに変更
*   **Logic**: `features/tern/` から `settings/demo/` への相対パスを設定。あるいは、`config.yaml` 自体を `settings/demo/` に移動し、`features/tern/config.yaml` を削除する。
*   **実装方針**: `features/tern/config.yaml` と `features/tern/model_profiles.yaml` を完全に `settings/demo/` へ移動。`features/tern/` で起動する際は `--config ../../settings/demo/config.yaml` を指定する運用に変更。

> [!IMPORTANT]
> `features/tern/config.yaml` の `model_profiles_path` は相対パスで解決されるため、`settings/demo/config.yaml` に移動した場合は `model_profiles_path: "model_profiles.yaml"` のままで同ディレクトリの `model_profiles.yaml` を参照する。

---

#### [DELETE] [model_profiles.yaml (features/tern)](file://features/tern/model_profiles.yaml)

*   **Description**: `settings/demo/` に移動済みのため削除

---

#### [DELETE] [config.yaml (features/tern)](file://features/tern/config.yaml)

*   **Description**: `settings/demo/` に移動済みのため削除

---

### ドキュメント

#### [MODIFY] [README.md](file://README.md)

*   **Description**: 新フィールド名への更新、`settings/` ディレクトリの説明追加
*   **Logic**:
    *   L301: `value` -> `secret` (vault 環境変数の説明)
    *   L334-L354: Quick Start の `model_profiles.yaml` サンプルを `api_keys` / `secret` に更新
    *   L341-L343: `keys:` -> `api_keys:`, `value:` -> `secret:`
    *   L347-L350: 同上
    *   L361: `--config` パスを `./settings/demo/config.yaml` に変更
    *   L429-L447: Project Structure に `settings/` ディレクトリを追加
    *   READMEの Client セクション (L86-L88): Agent/Model 例を `wayfinder` / `qwen2.5-coder:7b` に更新

## Step-by-Step Implementation Guide

### Phase 1: Go構造体の変更 (TDD)

1.  **テスト更新 (config パッケージ)**:
    *   `shared/libs/go/config/model_profiles_test.go` を編集: YAML リテラル内の `keys:` -> `api_keys:`, `value:` -> `secret:` に変更。Go構造体リテラルの `.Keys` -> `.ApiKeys`, `.Value` -> `.Secret` に変更。`secret` 空文字列のバリデーションテストケースを追加。
    *   `shared/libs/go/config/loader_test.go` を編集: YAML リテラル内の `keys:` -> `api_keys:`, `value:` -> `secret:` に変更。アサーションの `.Keys[0].Value` -> `.ApiKeys[0].Secret` に変更。
    *   この時点でテストは失敗する (構造体フィールドがまだ変更されていないため)。

2.  **構造体変更 (config パッケージ)**:
    *   `shared/libs/go/config/model_profiles.go` を編集: `ProviderConfig.Keys` -> `.ApiKeys` (yaml: `api_keys`)、`KeyConfig.Value` -> `.Secret` (yaml: `secret,omitempty`)。`Validate()` 内の参照も更新。
    *   `git commit`

3.  **テスト更新 (llmgateway パッケージ)**:
    *   `shared/libs/go/llmgateway/routing_test.go` を編集: `testProfiles()` ヘルパーの `Keys:` -> `ApiKeys:`, `Value:` -> `Secret:` を更新。
    *   `shared/libs/go/llmgateway/bifrost_account_test.go` を編集: 構造体リテラルの `Keys:` -> `ApiKeys:`, `Value:` -> `Secret:` を更新。

4.  **ロジック変更 (llmgateway パッケージ)**:
    *   `shared/libs/go/llmgateway/routing.go` を編集: `.Keys` -> `.ApiKeys`, `.Value` -> `.Secret`。
    *   `shared/libs/go/llmgateway/bifrost_account.go` を編集: `.Keys` -> `.ApiKeys`, `.Value` -> `.Secret`。`Secret` 空文字列時の vault 解決スキップ追加。
    *   `shared/libs/go/llmgateway/proxy.go` を編集: `.Keys` -> `.ApiKeys` (3箇所)。
    *   `git commit`

5.  **ロジック変更 (agentservice パッケージ)**:
    *   `shared/libs/go/agentservice/service.go` を編集: `.Keys` -> `.ApiKeys`。
    *   `git commit`

### Phase 2: YAML ファイルの更新

6.  **テストデータ更新**:
    *   `tests/testdata/model_profiles.yaml` を編集: `keys:` -> `api_keys:`, `value:` -> `secret:`。Ollama の `secret: ""` を削除。
    *   `git commit`

7.  **settings/ フォルダの作成と設定ファイル移動**:
    *   `settings/example/config.yaml` を新規作成。
    *   `settings/example/model_profiles.yaml` を新規作成。
    *   `settings/demo/config.yaml` を新規作成 (既存の `features/tern/config.yaml` をベースにコメント追加)。
    *   `settings/demo/model_profiles.yaml` を新規作成 (スキーマコメント付きの完全版)。
    *   `features/tern/config.yaml` を削除。
    *   `features/tern/model_profiles.yaml` を削除。
    *   `git commit`

### Phase 3: examples の整備

8.  **examples/minimal-server の更新**:
    *   `examples/minimal-server/model_profiles.yaml` を新規作成。
    *   `examples/minimal-server/main.go` を更新 (コメント修正)。
    *   `git commit`

9.  **examples/minimal-client の更新**:
    *   `examples/minimal-client/main.go` を更新 (デフォルト値変更、コメント追加)。
    *   `git commit`

### Phase 4: ドキュメント更新

10. **README.md の更新**:
    *   `keys`/`value` -> `api_keys`/`secret` の全箇所を更新。
    *   Project Structure に `settings/` を追加。
    *   Client コード例の Agent/Model を更新。
    *   `--config` パスを `settings/demo/config.yaml` に変更。
    *   `git commit`

### Phase 5: ビルド検証

11. **ビルドと全テスト実行** (Verification Plan 参照)

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```

2.  **Integration Tests** (LLMカテゴリ):
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories llm
    ```

### E2E テストの判断

E2E テストの追加は不要です。理由:
*   本変更は YAML フィールド名のリネームと設定ファイルの再配置が主な内容
*   Go 構造体のフィールドリネームは、既存のユニットテストで十分にカバーされる
*   外部から観測可能な API の動作に変更はない (フィールドのリネームは内部設定のみ)
*   設定ファイルの配置変更は、ビルドスクリプトが正常に通ることで検証される

### テスト項目設計セルフレビュー (11.4)

1.  **網羅性の検証**: YAML パース (unmarshal)、バリデーション (空 secret 許可)、ルーティング解決、Bifrost キー変換、vault 解決 (空 secret スキップ)。全てのレイヤーで新フィールド名が正しく動作することを確認するテストが存在する。十分である。
2.  **証拠の十分性**: 各テストは具体的な値のアサーションを行っている (フィールド値が期待通りか、nil でないか、エラーが返るか)。単に「エラーが出ない」だけではない。
3.  **迂回・抜け道の排除**: YAML パーステストでは `api_keys` / `secret` というタグ名でのみパースが成功する。旧フィールド名ではパースされないため、古いフィールド名の残存は検知される。
4.  **依存関係の整合性**: config -> llmgateway -> agentservice の依存順で、ボトムアップにテストが構成されている。

## Documentation

#### [MODIFY] [README.md](file://README.md)
*   **更新内容**: 上記 R7 の通り。`keys`/`value` -> `api_keys`/`secret`、Project Structure に `settings/` 追加、コード例更新。
