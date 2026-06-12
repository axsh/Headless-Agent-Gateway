# 045-LLMGP-Security-Hardening-Part1

> **Source Specification**: [033-LLMGP-Security-Hardening.md](file:///prompts/phases/000-foundation/branches/feat-llm-backend/ideas/033-LLMGP-Security-Hardening.md)

## Goal Description

LLMGPセキュリティ強化の第1部として、Config拡張と即効性の高い対策 (R2, R3, R5, R7, R8) を実装する。
これらは既存コードの軽微な修正で完結し、他の要件 (R1 TLS, R4 認証, R6 セッション管理) への依存がない独立した変更群である。

**本Partのスコープ**:
- R2: FileVaultBackend デフォルト暗号化キー廃止
- R3: ログマスク統一
- R5: リクエストボディサイズ制限
- R7: HTTP Server タイムアウト設定
- R8: Google APIキーのURL露出排除
- Config構造体の全拡張 (R1/R4/R6用のフィールドも含む)

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R2-1: TERN_VAULT_KEY未設定でエラー | Proposed Changes > vault/file_backend.go |
| R2-2: デフォルトキー削除 | Proposed Changes > vault/file_backend.go |
| R2-3: エラーメッセージに案内 | Proposed Changes > vault/file_backend.go |
| R3-1: routing.goのTrace先頭8文字をMaskSecretに統一 | Proposed Changes > llmgateway/routing.go |
| R3-2: APIキーログ出力はMaskSecret経由 | Proposed Changes > llmgateway/routing.go |
| R5-1: MaxBytesReader適用 | Proposed Changes > llmgateway/proxy_anthropic.go, proxy_openai.go |
| R5-2: デフォルト10MB | Proposed Changes > config/config.go |
| R5-3: config.yamlで設定可能 | Proposed Changes > config/config.go |
| R5-4: 413応答 | Proposed Changes > llmgateway/proxy_anthropic.go, proxy_openai.go |
| R7-1: http.Serverタイムアウト設定 | Proposed Changes > llmgateway/proxy.go |
| R7-2: WriteTimeoutのSSE考慮 | Proposed Changes > llmgateway/proxy.go |
| R7-3: config.yamlで設定可能 | Proposed Changes > config/config.go |
| R8-1: Google URLクエリパラメータ削除 | Proposed Changes > llmgateway/provider_google.go |
| R8-2: ヘッダベース認証のみ | Proposed Changes > llmgateway/provider_google.go |
| Config拡張 (TLSConfig, SessionConfig, ServerConfig) | Proposed Changes > config/config.go |

## Proposed Changes

### config パッケージ

#### [MODIFY] [config_test.go](file:///shared/libs/go/config/config_test.go)

*   **Description**: 新規Config構造体のYAMLパース・デフォルト値のテストを追加
*   **Technical Design**: テーブル駆動テスト
*   **Logic**:
    *   `TestLLMGatewayConfig_SecurityFields`: 以下のケースを検証
        *   TLSConfig全フィールドのYAMLパース (`enabled`, `mode`, `cert_file`, `key_file`, `extra_sans`)
        *   `auth_token` フィールドのパース (空文字列、値指定)
        *   `max_request_body_bytes` のパース (0の場合はデフォルト適用前の状態)
        *   `SessionConfig` (`max_sessions`, `ttl_seconds`)
        *   `ServerConfig` (`read_timeout_seconds`, `write_timeout_seconds`, `idle_timeout_seconds`, `max_header_bytes`)
    *   YAML入力:
    ```yaml
    llm_gateway:
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
    ```
    *   検証: 各フィールドが期待値と一致すること

#### [MODIFY] [config.go](file:///shared/libs/go/config/config.go)

*   **Description**: `LLMGatewayConfig` にセキュリティ関連フィールドを追加。`TLSConfig`, `SessionConfig`, `ServerConfig` 構造体を定義。デフォルト値適用関数を追加。
*   **Technical Design**:

    ```go
    // LLMGatewayConfig に追加するフィールド
    type LLMGatewayConfig struct {
        Port              int            `yaml:"port"`
        ModelProfilesPath string         `yaml:"model_profiles_path"`
        MetricsEnabled    bool           `yaml:"metrics_enabled"`
        Retry             RetrySettings  `yaml:"retry"`
        TLS               TLSConfig      `yaml:"tls"`
        AuthToken         string         `yaml:"auth_token"`
        MaxRequestBodyBytes int64        `yaml:"max_request_body_bytes"`
        Session           SessionConfig  `yaml:"session"`
        Server            ServerConfig   `yaml:"server"`
    }

    type TLSConfig struct {
        Enabled   bool     `yaml:"enabled"`
        Mode      string   `yaml:"mode"`
        CertFile  string   `yaml:"cert_file"`
        KeyFile   string   `yaml:"key_file"`
        ExtraSANs []string `yaml:"extra_sans"`
    }

    type SessionConfig struct {
        MaxSessions int `yaml:"max_sessions"`
        TTLSeconds  int `yaml:"ttl_seconds"`
    }

    type ServerConfig struct {
        ReadTimeoutSeconds  int `yaml:"read_timeout_seconds"`
        WriteTimeoutSeconds int `yaml:"write_timeout_seconds"`
        IdleTimeoutSeconds  int `yaml:"idle_timeout_seconds"`
        MaxHeaderBytes      int `yaml:"max_header_bytes"`
    }
    ```

*   **Logic**:
    *   デフォルト値適用関数 `ApplyDefaults()`:
        *   `MaxRequestBodyBytes == 0` --> `10 * 1024 * 1024` (10MB)
        *   `Session.MaxSessions == 0` --> `1000`
        *   `Session.TTLSeconds == 0` --> `86400`
        *   `Server.ReadTimeoutSeconds == 0` --> `30`
        *   `Server.WriteTimeoutSeconds == 0` --> `300`
        *   `Server.IdleTimeoutSeconds == 0` --> `60`
        *   `Server.MaxHeaderBytes == 0` --> `1 << 20` (1MB)
    *   `ApplyDefaults()` は `config.Load()` の最後に呼び出す

---

### vault パッケージ

#### [MODIFY] [file_backend_test.go](file:///shared/libs/go/vault/file_backend_test.go)

*   **Description**: デフォルトキー廃止に伴うテスト修正
*   **Technical Design**: 既存テストの修正 + 新規テストケース追加
*   **Logic**:
    *   `TestNewFileVaultBackend_NoKey`: `TERN_VAULT_KEY` を未設定にして `NewFileVaultBackend` を呼び出す
        *   検証: エラーが返ること。エラーメッセージに `"TERN_VAULT_KEY"` と `"openssl rand"` が含まれること
    *   `TestNewFileVaultBackend_WithKey`: `TERN_VAULT_KEY` を設定して `NewFileVaultBackend` を呼び出す
        *   検証: エラーが nil であること。正常にインスタンスが作成されること
    *   既存テストで `TERN_VAULT_KEY` を設定していないものがあれば、`t.Setenv("TERN_VAULT_KEY", "test-key-for-unit-test")` を追加

#### [MODIFY] [file_backend.go](file:///shared/libs/go/vault/file_backend.go)

*   **Description**: ハードコードされたデフォルト暗号化キーを廃止
*   **Technical Design**:

    ```go
    // 変更前 (L27-29)
    rawKey := os.Getenv("TERN_VAULT_KEY")
    if rawKey == "" {
        rawKey = "default-hag-vault-key-change-me"
    }

    // 変更後
    rawKey := os.Getenv("TERN_VAULT_KEY")
    if rawKey == "" {
        return nil, fmt.Errorf(
            "TERN_VAULT_KEY environment variable is required for file vault backend; " +
            "set a strong random key (e.g. openssl rand -base64 32)")
    }
    ```

*   **Logic**: `NewFileVaultBackend` の戻り値を `(*FileVaultBackend, error)` に変更する必要がある。呼び出し側の修正も必要。

---

### llmgateway パッケージ

#### [MODIFY] [routing_test.go](file:///shared/libs/go/llmgateway/routing_test.go)

*   **Description**: ログマスク統一のテスト
*   **Technical Design**: 既存テスト内で Trace ログの出力内容を検証
*   **Logic**:
    *   `TestModelRouter_LogMasking`: モックloggerを使用して、Traceログのkey_prefixフィールドが `MaskSecret()` 形式 (例: `****abcd`) であることを検証
    *   キー値 `sk-test-1234-5678-abcd` に対して、出力が `****abcd` であることを確認
    *   先頭8文字 (`sk-test-`) が出力に含まれないことを確認

#### [MODIFY] [routing.go](file:///shared/libs/go/llmgateway/routing.go)

*   **Description**: Traceログのキー出力を `MaskSecret()` に統一
*   **Technical Design**:

    ```go
    // 変更前 (L90-95)
    keyPrefix := ""
    if len(resolved.KeyValue) > 8 {
        keyPrefix = resolved.KeyValue[:8] + "..."
    } else {
        keyPrefix = resolved.KeyValue
    }

    // 変更後
    keyPrefix := MaskSecret(resolved.KeyValue)
    ```

#### [MODIFY] [provider_test.go](file:///shared/libs/go/llmgateway/provider_test.go)

*   **Description**: Google APIキーURL露出排除のテスト
*   **Technical Design**: 既存の `TestSetAuthHeaders_Google` を修正 (または新規追加)
*   **Logic**:
    *   `TestGoogleProvider_NoURLKey`: `SetAuthHeaders` 呼び出し後に `req.URL.RawQuery` に `key=` が含まれないことを確認
    *   `TestGoogleProvider_HeaderOnly`: `req.Header.Get("x-goog-api-key")` が APIキー値と一致することを確認
    *   既存テストでURLにキーが含まれることを前提にしているものがあれば修正

#### [MODIFY] [provider_google.go](file:///shared/libs/go/llmgateway/provider_google.go)

*   **Description**: URLクエリパラメータへのAPIキー付与を削除
*   **Technical Design**:

    ```go
    // 変更前 (L19-27)
    func (p *googleProvider) SetAuthHeaders(req *http.Request, apiKey string, originalHeaders http.Header) {
        req.Header.Set("x-goog-api-key", apiKey)
        req.Header.Del("Authorization")
        if req.URL.RawQuery != "" {
            req.URL.RawQuery = req.URL.RawQuery + "&key=" + apiKey
        } else {
            req.URL.RawQuery = "key=" + apiKey
        }
    }

    // 変更後
    func (p *googleProvider) SetAuthHeaders(req *http.Request, apiKey string, originalHeaders http.Header) {
        req.Header.Set("x-goog-api-key", apiKey)
        req.Header.Del("Authorization")
        // URL query parameter is intentionally NOT set to prevent API key exposure in logs.
    }
    ```

#### [MODIFY] [proxy_anthropic_test.go](file:///shared/libs/go/llmgateway/proxy_anthropic_test.go)

*   **Description**: MaxBytesReader適用テスト
*   **Technical Design**: テーブル駆動テスト
*   **Logic**:
    *   `TestAnthropicHandler_MaxBodySize`: ProxyServerを `MaxRequestBodyBytes: 1024` で構築し、以下を検証
        *   1024バイト以下のリクエスト: 正常処理 (200 or モック応答)
        *   1025バイト以上のリクエスト: `413 Request Entity Too Large`

#### [MODIFY] [proxy_openai_test.go](file:///shared/libs/go/llmgateway/proxy_openai_test.go)

*   **Description**: MaxBytesReader適用テスト (OpenAI側)
*   **Technical Design**: Anthropic側と同様
*   **Logic**:
    *   `TestOpenAIHandler_MaxBodySize`: 同上の検証パターン

#### [MODIFY] [proxy_anthropic.go](file:///shared/libs/go/llmgateway/proxy_anthropic.go)

*   **Description**: リクエストボディサイズ制限の適用
*   **Technical Design**:

    ```go
    // handleAnthropicMessages メソッド冒頭に追加
    func (p *ProxyServer) handleAnthropicMessages(w http.ResponseWriter, r *http.Request) {
        // R5: Apply request body size limit.
        maxBody := p.cfg.LLMGateway.MaxRequestBodyBytes
        if maxBody > 0 {
            r.Body = http.MaxBytesReader(w, r.Body, maxBody)
        }
        body, err := io.ReadAll(r.Body)
        if err != nil {
            // MaxBytesError is returned when body exceeds limit.
            if err.Error() == "http: request body too large" {
                WriteErrorResponse(w, &GatewayError{
                    Type:    "invalid_request_error",
                    Message: "request body too large",
                    Code:    "request_too_large",
                    Status:  http.StatusRequestEntityTooLarge,
                })
                return
            }
            // ... existing error handling
        }
        // ... rest of handler
    }
    ```

*   **Logic**: `http.MaxBytesReader` は `io.ReadAll` でサイズ超過時に `*http.MaxBytesError` を返す。これを検知して413応答を返す。

#### [MODIFY] [proxy_openai.go](file:///shared/libs/go/llmgateway/proxy_openai.go)

*   **Description**: OpenAI側も同様にMaxBytesReaderを適用
*   **Technical Design**: Anthropic側と同じパターン

#### [MODIFY] [proxy_test.go](file:///shared/libs/go/llmgateway/proxy_test.go)

*   **Description**: HTTP Serverタイムアウト設定のテスト
*   **Technical Design**:
*   **Logic**:
    *   `TestProxyServer_TimeoutConfig`: `ServerConfig` を指定してProxyServerを構築・Launch、`http.Server` のフィールドが設定値と一致するか検証
        *   `ReadTimeout == 15 * time.Second`
        *   `WriteTimeout == 120 * time.Second`
        *   `IdleTimeout == 30 * time.Second`
        *   `MaxHeaderBytes == 524288`
    *   `TestProxyServer_DefaultTimeout`: デフォルト値で構築した場合のタイムアウト検証
        *   `ReadTimeout == 30 * time.Second`
        *   `WriteTimeout == 300 * time.Second`
        *   `IdleTimeout == 60 * time.Second`
        *   `MaxHeaderBytes == 1 << 20`

#### [MODIFY] [proxy.go](file:///shared/libs/go/llmgateway/proxy.go)

*   **Description**: HTTP Serverにタイムアウトと制限を設定
*   **Technical Design**:

    ```go
    // Launch() メソッド内 (L76付近)
    // 変更前
    p.server = &http.Server{Handler: mux}

    // 変更後
    serverCfg := p.cfg.LLMGateway.Server
    p.server = &http.Server{
        Handler:        mux,
        ReadTimeout:    time.Duration(serverCfg.ReadTimeoutSeconds) * time.Second,
        WriteTimeout:   time.Duration(serverCfg.WriteTimeoutSeconds) * time.Second,
        IdleTimeout:    time.Duration(serverCfg.IdleTimeoutSeconds) * time.Second,
        MaxHeaderBytes: serverCfg.MaxHeaderBytes,
    }
    ```

*   **Logic**: `config.ApplyDefaults()` により、設定値が0の場合でもデフォルト値が適用される

---

## Step-by-Step Implementation Guide

1. **Config構造体の拡張テスト作成**:
    - `config_test.go` に `TestLLMGatewayConfig_SecurityFields` を追加
    - テスト実行して失敗を確認

2. **Config構造体の拡張実装**:
    - `config.go` に `TLSConfig`, `SessionConfig`, `ServerConfig` を追加
    - `LLMGatewayConfig` にフィールド追加
    - `ApplyDefaults()` 関数を実装、`Load()` から呼び出し
    - テスト成功を確認
    - `git commit -m "feat: add security config structs (TLS, Session, Server)"`

3. **FileVaultBackendデフォルトキー廃止テスト作成**:
    - `file_backend_test.go` に `TestNewFileVaultBackend_NoKey`, `TestNewFileVaultBackend_WithKey` を追加
    - 既存テストに `t.Setenv` を追加

4. **FileVaultBackendデフォルトキー廃止実装**:
    - `file_backend.go` のデフォルトキーを削除、エラーを返す
    - `NewFileVaultBackend` の戻り値に `error` を追加
    - 呼び出し側を修正
    - テスト成功を確認
    - `git commit -m "feat: remove hardcoded default vault key (R2)"`

5. **ログマスク統一テスト作成**:
    - `routing_test.go` に `TestModelRouter_LogMasking` を追加

6. **ログマスク統一実装**:
    - `routing.go` の `keyPrefix` を `MaskSecret()` に変更
    - テスト成功を確認
    - `git commit -m "feat: unify API key log masking with MaskSecret (R3)"`

7. **Google APIキーURL排除テスト作成**:
    - `provider_test.go` に `TestGoogleProvider_NoURLKey` を追加

8. **Google APIキーURL排除実装**:
    - `provider_google.go` のURLクエリパラメータ付与コードを削除
    - テスト成功を確認
    - `git commit -m "feat: remove Google API key from URL query params (R8)"`

9. **リクエストボディサイズ制限テスト作成**:
    - `proxy_anthropic_test.go` に `TestAnthropicHandler_MaxBodySize` を追加
    - `proxy_openai_test.go` に `TestOpenAIHandler_MaxBodySize` を追加

10. **リクエストボディサイズ制限実装**:
    - `proxy_anthropic.go` と `proxy_openai.go` に `http.MaxBytesReader` を追加
    - テスト成功を確認
    - `git commit -m "feat: add request body size limit with MaxBytesReader (R5)"`

11. **HTTP Serverタイムアウト設定テスト作成**:
    - `proxy_test.go` に `TestProxyServer_TimeoutConfig`, `TestProxyServer_DefaultTimeout` を追加

12. **HTTP Serverタイムアウト設定実装**:
    - `proxy.go` の `http.Server` にタイムアウト設定を追加
    - テスト成功を確認
    - `git commit -m "feat: add HTTP server timeout configuration (R7)"`

13. **ビルド・テスト検証**:
    - `./scripts/process/build.sh` を実行
    - `./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories "llm"` を実行
    - `./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories "common"` を実行

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```

2.  **Integration Tests (LLM)**:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories "llm"
    ```
    *   **Log Verification**: Traceログにキー先頭8文字が含まれないこと。`MaskSecret` 形式のマスクのみが出力されること。

3.  **Integration Tests (Common)**:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories "common"
    ```
    *   **Log Verification**: サーバー起動・停止のライフサイクルが正常に動作すること。

4.  **E2Eテスト**:
    本Partは内部リファクタリング (ログマスク変更、タイムアウト設定追加、URLパラメータ削除) と既存動作の強化 (ボディサイズ制限) が主であり、外部APIの動作には影響しない。既存のE2Eテストで回帰を確認する。新規E2Eテストの追加は不要。
    - 理由: 外部から観測可能な動作変更はR2 (Vault初期化エラー) とR5 (413応答) だが、いずれも単体テストで十分に検証可能。

### テスト項目のセルフレビュー (11.4)

1.  **網羅性**: R2/R3/R5/R7/R8の全要件に対応するテストが計画されている。
2.  **証拠の十分性**: 各テストは「値が一致する」「エラーメッセージが含まれる」等の具体的なアサーションを持つ。
3.  **迂回排除**: Config経由で値が渡っていることをテスト内で検証。
4.  **依存関係**: Config --> Proxy --> Handler の順でボトムアップにテストが設計されている。

## Documentation

#### [MODIFY] [config.go](file:///shared/libs/go/config/config.go)
*   **更新内容**: 新規構造体にGoDocコメントを追加。各フィールドにデフォルト値を明記。

---

## 継続計画について

本ドキュメントは Part 1 です。Part 2 (045-LLMGP-Security-Hardening-Part2) で以下を実装します:

- **R1**: TLS対応 (自動生成/ファイル指定、証明書自動更新、期限切れ警告)
- **R4**: 内部トークン認証 (ミドルウェア、トークン生成、AdapterConfig注入)
- **R6**: セッション管理のサイズ制限とTTL (LRUキャッシュ)

Part 2 は Part 1 で追加した Config 構造体 (`TLSConfig`, `SessionConfig`, `AuthToken`) に依存します。
