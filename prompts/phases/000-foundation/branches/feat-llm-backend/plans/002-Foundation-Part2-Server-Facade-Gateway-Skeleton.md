# 002-Foundation-Part2-Server-Facade-Gateway-Skeleton

> **Source Specification**:
> - [000-Architecture.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/ideas/000-Architecture.md) (R1-R7: Server Facade, Option, Lifecycle)
> - [001-LLMGatewayProxy.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/ideas/001-LLMGatewayProxy.md) (R1, R2: Interface, HTTP Proxy skeleton)
> - [002-ConfigAndSecrets.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/ideas/002-ConfigAndSecrets.md) (Config/Vault integration)

## Goal Description

Part1 (Logger / Config / Vault) の基盤層の上に、HAGの中核である `hag.Server` ファサードと `llmgateway` パッケージの骨格を構築する。

本計画では以下を実装する:
1. **hag.Server Facade**: Option DI、ライフサイクル (New/Launch/Shutdown)、コンポーネントワイヤリング
2. **llmgateway パッケージの骨格**: `LLMGatewayBackend` interface、データ型定義、HTTP Proxy (スタブハンドラ)
3. **Stub Gateway**: テスト用スタブ実装 (`StubGateway`)

BifrostDriver の実連携、Anthropic/OpenAI ハンドラの実装、SSEストリーミングは Part3 で実装する。

## User Review Required

> [!IMPORTANT]
> **スコープの分割**: 001-LLMGatewayProxy の全要件を一度に実装するには量が多いため、Part2 (骨格) と Part3 (Bifrost実連携) に分割しています。Part2ではスタブハンドラで全エンドポイントの構造を確立し、Part3でBifrost SDKとの統合を行います。

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| 000-R1-1: コア機能は shared/libs/go/ のGoパッケージ | Proposed Changes > hag/ |
| 000-R1-2: hag.Server ファサード型 | hag/server.go |
| 000-R1-3: In-Process API (New/Launch/Shutdown/Gateway) | hag/server.go |
| 000-R1-4: Config nil -> default, Option > WithConfig > default | hag/server.go (resolveDefaults) |
| 000-R1-6: features/hag/main.go -> examples/ | 本計画ではスキップ (Part4: Examples) |
| 000-R3-3: New() での初期化順序 | hag/server.go (New) |
| 000-R3-4: Launch() での起動順序 | hag/server.go (Launch) |
| 000-R3-5: Shutdown逆順 | hag/server.go (Shutdown) |
| 000-R3-6: Optionで外部注入可能 | hag/options.go |
| 000-R4-1 ~ R4-4: ライフサイクルパターン | hag/server.go |
| 000-R5-1 ~ R5-6: DI方針 | hag/server.go, options.go |
| 000-R6-1: ディレクトリ構造 | shared/libs/go/hag/, shared/libs/go/llmgateway/ |
| 000-R9-1 ~ R9-3: エラーハンドリング | hag/server.go |
| 001-R1-1: LLMGatewayBackend interface | llmgateway/backend.go |
| 001-R1-2: New + Launch + Shutdown パターン | llmgateway/backend.go |
| 001-R1-6: メソッド定義 | llmgateway/backend.go |
| 001-R2-1: HTTP Proxy エンドポイント | llmgateway/proxy.go |
| 001-R2-2: 並行リクエスト処理 | llmgateway/proxy.go (http.Server) |
| 001-R2-3: リッスンポート設定 | llmgateway/proxy.go |
| 001-R7-1: エラーレスポンス形式 | llmgateway/errors.go |

**Part3以降に先送り**:
- 001-R1-3, R1-4: BifrostDriver / PassthroughDriver の実実装
- 001-R3: Anthropic Messages API ハンドラ実装
- 001-R4: OpenAI Chat Completions ハンドラ実装
- 001-R5: モデルルーティング実装
- 001-R6: ToolCall フォールバック
- 001-R8: Rate Limiting (Bifrost SDK委譲)
- 001-R9: 可観測性
- 001-R10: Gateway URL注入 (ProxyURL は本計画で実装するが、Agent Driver連携は将来)

---

## Proposed Changes

### hag パッケージ (Facade)

#### [NEW] [server_test.go](file://shared/libs/go/hag/server_test.go)
*   **Description**: hag.Server の単体テスト (TDD RED先行)
*   **Technical Design**:
    ```go
    package hag

    func TestNew_DefaultConfig(t *testing.T)
    // New() を引数なしで呼び出し、デフォルトConfigでServerが生成されること
    // cfg, logger, vault, gateway が全てデフォルト値であること

    func TestNew_WithConfig(t *testing.T)
    // WithConfig でAppConfigを渡し、Serverにcfgが設定されること

    func TestNew_WithConfigPath(t *testing.T)
    // WithConfigPath で一時ファイルのYAMLパスを渡し、ロードされること

    func TestNew_WithLogger(t *testing.T)
    // WithLogger でカスタムロガーを渡し、Server内で使用されること

    func TestNew_WithVaultStore(t *testing.T)
    // WithVaultStore でカスタムVaultStoreを渡すこと

    func TestNew_WithGateway(t *testing.T)
    // WithGateway でスタブGatewayを渡し、Gateway()で取得できること

    func TestNew_OptionPriority(t *testing.T)
    // Option > WithConfig > Default の優先順位を検証

    func TestServer_LaunchShutdown(t *testing.T)
    // Launch -> Shutdown のライフサイクルが正常に動作すること
    // StubGatewayを注入し、Launch/Shutdownが呼ばれることを確認

    func TestServer_Gateway_ReturnsInjected(t *testing.T)
    // WithGatewayで注入したインスタンスがGateway()で返ること

    func TestServer_Shutdown_ReverseOrder(t *testing.T)
    // 複数コンポーネントのShutdownが起動の逆順であること (記録用stubで確認)

    func TestNew_InvalidConfigPath(t *testing.T)
    // 不正なConfigPathでNew()がエラーを返すこと
    ```

#### [NEW] [server.go](file://shared/libs/go/hag/server.go)
*   **Description**: `hag.Server` ファサード (New / Launch / Shutdown / Gateway)
*   **Technical Design**:

    ```go
    package hag

    import (
        "context"
        "fmt"
        "github.com/axsh/hag/config"
        "github.com/axsh/hag/logger"
        "github.com/axsh/hag/llmgateway"
        "github.com/axsh/hag/vault"
    )

    // Server is the HAG core facade that orchestrates all components.
    // Users interact with HAG through this type.
    type Server struct {
        cfg     *config.AppConfig
        logger  logger.Logger
        vault   vault.VaultStore
        gateway llmgateway.LLMGatewayBackend
    }

    // New creates a new HAG Server with the given options.
    // No goroutines are started; no network listeners are opened.
    // Options are applied in order: individual Option > WithConfig > default.
    func New(opts ...Option) (*Server, error)

    // Launch starts all components. Non-blocking.
    // Calls gateway.Launch(ctx) internally.
    func (s *Server) Launch(ctx context.Context) error

    // Shutdown gracefully stops all components in reverse launch order.
    func (s *Server) Shutdown(ctx context.Context) error

    // Gateway returns the LLM Gateway Proxy backend.
    func (s *Server) Gateway() llmgateway.LLMGatewayBackend
    ```

*   **Logic**:
    *   `New()` 内部の初期化順序 (000-R3-3準拠):
        1. `options` 構造体を生成し、全 `Option` を適用
        2. Config解決: `WithConfigPath` -> `config.Load(path)` / `WithConfig` -> そのまま / なし -> `&config.AppConfig{}`
        3. Logger解決: `WithLogger` が未指定なら `logger.NewDefault(logger.ParseLevel(cfg.Log.Level))`
        4. VaultStore解決: `WithVaultStore` が未指定なら `vault.NewEnvVaultBackend()`
        5. Gateway解決: `WithGateway` が未指定なら `llmgateway.NewProxyServer(cfg, vaultStore, log)`
    *   `Launch()`: `s.gateway.Launch(ctx)` を呼ぶ
    *   `Shutdown()`: `s.gateway.Shutdown(ctx)` を呼ぶ (将来コンポーネント追加時は逆順)

#### [NEW] [options.go](file://shared/libs/go/hag/options.go)
*   **Description**: Functional Options パターン
*   **Technical Design**:

    ```go
    package hag

    // Option configures a Server.
    type Option func(*options)

    type options struct {
        cfg        *config.AppConfig
        configPath string
        logger     logger.Logger
        vault      vault.VaultStore
        gateway    llmgateway.LLMGatewayBackend
    }

    // WithConfig sets the configuration directly.
    func WithConfig(cfg *config.AppConfig) Option

    // WithConfigPath sets the configuration file path.
    // Internally calls config.Load(path).
    func WithConfigPath(path string) Option

    // WithLogger injects a custom Logger implementation.
    func WithLogger(log logger.Logger) Option

    // WithVaultStore injects a custom VaultStore implementation.
    func WithVaultStore(vs vault.VaultStore) Option

    // WithGateway injects a custom LLMGatewayBackend implementation.
    func WithGateway(gw llmgateway.LLMGatewayBackend) Option
    ```

---

### llmgateway パッケージ (Gateway骨格)

#### [NEW] [backend.go](file://shared/libs/go/llmgateway/backend.go)
*   **Description**: `LLMGatewayBackend` interface + 関連データ型
*   **Technical Design**:

    ```go
    package llmgateway

    import "context"

    // LLMGatewayBackend is the interface for LLM proxy backends.
    type LLMGatewayBackend interface {
        // Launch starts the HTTP proxy server. Non-blocking.
        Launch(ctx context.Context) error

        // Shutdown gracefully stops the HTTP proxy server.
        Shutdown(ctx context.Context) error

        // ListModels returns the list of configured models.
        ListModels() []ModelInfo

        // Health returns the backend health status.
        Health() HealthStatus

        // ProxyURL returns the HTTP proxy URL for agent CLI injection.
        ProxyURL() string
    }

    // ModelInfo describes a configured model.
    type ModelInfo struct {
        Provider string `json:"provider"`
        Model    string `json:"model"`
    }

    // HealthStatus describes the backend health.
    type HealthStatus struct {
        Status    string `json:"status"`    // "ok", "degraded", "down"
        Message   string `json:"message,omitempty"`
        Models    int    `json:"models"`    // number of configured models
    }
    ```

#### [NEW] [backend_test.go](file://shared/libs/go/llmgateway/backend_test.go)
*   **Description**: interface準拠チェック + StubGatewayテスト
*   **Technical Design**:
    ```go
    package llmgateway

    // Compile-time check: ProxyServer implements LLMGatewayBackend.
    var _ LLMGatewayBackend = (*ProxyServer)(nil)

    // Compile-time check: StubGateway implements LLMGatewayBackend.
    var _ LLMGatewayBackend = (*StubGateway)(nil)

    func TestStubGateway_Lifecycle(t *testing.T)
    // Launch/Shutdown が正常に動作すること
    // ProxyURL() が空文字列を返すこと
    // ListModels() が空スライスを返すこと
    // Health() がstatus "stub" を返すこと
    ```

#### [NEW] [stub.go](file://shared/libs/go/llmgateway/stub.go)
*   **Description**: テスト用スタブ実装
*   **Technical Design**:

    ```go
    package llmgateway

    // StubGateway is a no-op implementation of LLMGatewayBackend for testing.
    type StubGateway struct {
        Launched  bool
        ShutDown  bool
    }

    func NewStubGateway() *StubGateway
    func (s *StubGateway) Launch(ctx context.Context) error   // sets Launched=true
    func (s *StubGateway) Shutdown(ctx context.Context) error  // sets ShutDown=true
    func (s *StubGateway) ListModels() []ModelInfo              // returns nil
    func (s *StubGateway) Health() HealthStatus                 // returns {Status:"stub"}
    func (s *StubGateway) ProxyURL() string                     // returns ""
    ```

#### [NEW] [errors.go](file://shared/libs/go/llmgateway/errors.go)
*   **Description**: エラーコード定義とJSON形式のエラーレスポンス
*   **Technical Design**:

    ```go
    package llmgateway

    // GatewayError represents a structured error response.
    type GatewayError struct {
        Type    string `json:"type"`
        Message string `json:"message"`
        Code    string `json:"code,omitempty"`
        Status  int    `json:"-"` // HTTP status code (not serialized)
    }

    func (e *GatewayError) Error() string

    // Pre-defined errors:
    var (
        ErrModelNotFound    = &GatewayError{Type: "invalid_request_error", Message: "model not found", Code: "model_not_found", Status: 404}
        ErrProviderError    = &GatewayError{Type: "api_error", Message: "provider error", Code: "provider_error", Status: 502}
        ErrInternalError    = &GatewayError{Type: "api_error", Message: "internal server error", Code: "internal_error", Status: 500}
    )

    // WriteErrorResponse writes a JSON error response to the http.ResponseWriter.
    func WriteErrorResponse(w http.ResponseWriter, err *GatewayError)
    ```

#### [NEW] [errors_test.go](file://shared/libs/go/llmgateway/errors_test.go)
*   **Description**: エラー型テスト
*   **Technical Design**:
    ```go
    func TestGatewayError_Error(t *testing.T)
    // Error() が "type: message" 形式の文字列を返すこと

    func TestWriteErrorResponse(t *testing.T)
    // httptest.ResponseRecorder でJSONレスポンスを検証
    // Content-Type: application/json
    // ステータスコード一致
    // ボディがJSON形式で type, message, code を含むこと
    ```

#### [NEW] [proxy.go](file://shared/libs/go/llmgateway/proxy.go)
*   **Description**: HTTP Proxy Server (ProxyServer)
*   **Technical Design**:

    ```go
    package llmgateway

    import (
        "context"
        "fmt"
        "net"
        "net/http"
        "github.com/axsh/hag/config"
        "github.com/axsh/hag/logger"
        "github.com/axsh/hag/vault"
    )

    // ProxyServer implements LLMGatewayBackend with an HTTP proxy server.
    type ProxyServer struct {
        cfg      *config.AppConfig
        profiles *config.ModelProfilesConfig
        vault    vault.VaultStore
        logger   logger.Logger
        server   *http.Server
        listener net.Listener
        port     int
    }

    // NewProxyServer creates a ProxyServer.
    // It loads model_profiles.yaml if cfg.LLMGateway.ModelProfilesPath is set.
    func NewProxyServer(cfg *config.AppConfig, vs vault.VaultStore, log logger.Logger) (*ProxyServer, error)

    // Launch starts the HTTP server on the configured port.
    // If port is 0, an ephemeral port is used (useful for testing).
    func (p *ProxyServer) Launch(ctx context.Context) error

    // Shutdown gracefully stops the HTTP server.
    func (p *ProxyServer) Shutdown(ctx context.Context) error

    // ProxyURL returns "http://localhost:{port}".
    func (p *ProxyServer) ProxyURL() string

    // ListModels returns model info from loaded profiles.
    func (p *ProxyServer) ListModels() []ModelInfo

    // Health returns the proxy server health status.
    func (p *ProxyServer) Health() HealthStatus

    // setupRoutes registers HTTP handlers on the given mux.
    func (p *ProxyServer) setupRoutes(mux *http.ServeMux)
    ```

*   **Logic**:
    *   `setupRoutes` でのエンドポイント登録 (001-R2-1):
        *   `GET /` -> `handleIndex` (200 OK + エンドポイント一覧JSON)
        *   `GET /health` -> `handleHealth` (HealthStatus JSON)
        *   `POST /v1/messages` -> `handleAnthropicMessages` (Part2ではスタブ: 501 Not Implemented)
        *   `POST /v1/chat/completions` -> `handleOpenAIChatCompletions` (Part2ではスタブ: 501 Not Implemented)
        *   `GET /v1/models` -> `handleModels` (ModelProfilesからモデル一覧JSON)
    *   `Launch`: `net.Listen("tcp", ":port")` -> `go http.Serve(listener, mux)`
    *   `Shutdown`: `server.Shutdown(ctx)`
    *   Port=0 の場合、`listener.Addr()` からアクティブポートを取得 (テスト用)

#### [NEW] [proxy_test.go](file://shared/libs/go/llmgateway/proxy_test.go)
*   **Description**: ProxyServer のHTTPエンドポイントテスト
*   **Technical Design**:
    ```go
    func TestProxyServer_Launch_Shutdown(t *testing.T)
    // Port=0 でLaunchし、ProxyURL()が空でないこと
    // Shutdown後にHTTPリクエストが失敗すること

    func TestProxyServer_Index(t *testing.T)
    // GET / -> 200 OK, JSON body にendpointsリスト

    func TestProxyServer_Health(t *testing.T)
    // GET /health -> 200 OK, JSON body に status, models

    func TestProxyServer_Models(t *testing.T)
    // GET /v1/models -> 200 OK, JSON body にモデル一覧
    // model_profiles.yaml に定義されたモデルが返ること

    func TestProxyServer_AnthropicStub(t *testing.T)
    // POST /v1/messages -> 501 Not Implemented (Part2スタブ)

    func TestProxyServer_OpenAIStub(t *testing.T)
    // POST /v1/chat/completions -> 501 Not Implemented (Part2スタブ)

    func TestProxyServer_ProxyURL(t *testing.T)
    // ProxyURL() が "http://localhost:{port}" 形式であること

    func TestProxyServer_ListModels(t *testing.T)
    // ListModels() がprofiles内のモデルを返すこと
    ```

---

### go.mod 更新

#### [MODIFY] [go.mod](file://shared/libs/go/go.mod)
*   **Description**: hag パッケージと llmgateway パッケージの依存関係追加
*   **Logic**: 新パッケージは同一モジュール内のため go.mod 変更は不要。`go mod tidy` のみ実行。

---

## Step-by-Step Implementation Guide

> [!IMPORTANT]
> TDDサイクル: 各ステップでテストを先に書き、失敗を確認してから実装する。

1. **Step 1: llmgateway - Interface + DataTypes (TDD)**
    - [x] `shared/libs/go/llmgateway/backend_test.go` を作成 (compile-time checks)
    - [x] `shared/libs/go/llmgateway/backend.go` を作成 (LLMGatewayBackend interface, ModelInfo, HealthStatus)
    - [x] ビルド確認

2. **Step 2: llmgateway - StubGateway (TDD)** [x]
    - [x] `shared/libs/go/llmgateway/backend_test.go` にStubGatewayテストを追加 (RED)
    - [x] `shared/libs/go/llmgateway/stub.go` を実装 (GREEN)
    - [x] テスト通過確認

3. **Step 3: llmgateway - Errors (TDD)** [x]
    - [x] `shared/libs/go/llmgateway/errors_test.go` を作成 (RED)
    - [x] `shared/libs/go/llmgateway/errors.go` を実装 (GREEN)
    - [x] テスト通過確認

4. **Step 4: llmgateway - ProxyServer Core (TDD)** [x]
    - [x] `shared/libs/go/llmgateway/proxy_test.go` を作成 (Launch/Shutdown/ProxyURL テスト)
    - [x] `shared/libs/go/llmgateway/proxy.go` を実装 (NewProxyServer, Launch, Shutdown, ProxyURL)
    - [x] テスト通過確認

5. **Step 5: llmgateway - HTTP Endpoints (TDD)** [x]
    - [x] `shared/libs/go/llmgateway/proxy_test.go` にエンドポイントテスト追加 (Index, Health, Models, Stubs)
    - [x] `shared/libs/go/llmgateway/proxy.go` にハンドラ実装
    - [x] テスト通過確認
    - [x] Git commit: 3a46a18

6. **Step 6: hag - Options (TDD)** [x]
    - [x] `shared/libs/go/hag/server_test.go` を作成 (Option注入テスト: RED)
    - [x] `shared/libs/go/hag/options.go` を実装
    - [x] `shared/libs/go/hag/server.go` のNew()をスケルトン実装

7. **Step 7: hag - Server New + Defaults (TDD)** [x]
    - [x] `shared/libs/go/hag/server_test.go` にNew()テストを追加 (WithConfig, WithConfigPath, default, error)
    - [x] `shared/libs/go/hag/server.go` のNew()を完全実装 (resolveDefaults含む)
    - [x] テスト通過確認

8. **Step 8: hag - Launch / Shutdown (TDD)** [x]
    - [x] `shared/libs/go/hag/server_test.go` にLaunch/Shutdownテスト追加
    - [x] `shared/libs/go/hag/server.go` のLaunch/Shutdownを実装
    - [x] テスト通過確認
    - [x] Git commit: 65f4ece

9. **Step 9: ビルド検証** [x]
    - [x] `./scripts/process/build.sh` を実行して全テスト通過を確認 (全69テスト PASS)

10. **Step 10: Git push**
    - [x] こまめにコミット (Step 1-5: llmgateway 3a46a18, Step 6-8: hag 65f4ece)
    - [ ] 全テスト通過後に push

---

## Verification Plan

### Automated Verification

1. **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```

2. **個別パッケージテスト** (開発中フィードバック用):
    注: 正式検証は必ず `build.sh` を使用すること。
    ```bash
    cd shared/libs/go && go test ./llmgateway/... ./hag/... -v
    ```

### テスト項目のセルフレビュー (11.4)

1. **網羅性**: hag.Server (Option, New, Launch, Shutdown, Gateway), llmgateway (interface, StubGateway, ProxyServer, HTTP endpoints, Errors) の全主要機能をカバー
2. **証拠の十分性**: 各テストはHTTPレスポンスの具体的なステータスコード・JSONボディを検証。「エラーが出ない」だけでなく「正しい値が返る」を確認
3. **迂回排除**: StubGatewayの状態フラグ (Launched, ShutDown) で正しいコンポーネントが呼ばれていることを確認。httptest.NewRequest/ResponseRecorderで実際のHTTPハンドラをテスト
4. **依存関係**: Part1 (Logger, Config, Vault) -> llmgateway (interface, stub, errors) -> ProxyServer -> hag.Server のボトムアップ順

---

## Documentation

#### [MODIFY] [design_decisions.md](file://prompts/designs/hag/design_decisions.md)
*   **更新内容**: DD-002 (LLMGatewayBackend), DD-003 (ライフサイクル) の現状を更新

---

## 継続計画について

本計画は Part2 (Server Facade + Gateway Skeleton) です。以下の Part が続きます:

- **Part3**: BifrostDriver 実連携 + Anthropic Messages API ハンドラ + OpenAI ハンドラ + モデルルーティング + SSEストリーミング
- **Part4**: Hierarchical Agent Log + Examples (003-HierarchicalAgentLog, standalone example, Docker)
