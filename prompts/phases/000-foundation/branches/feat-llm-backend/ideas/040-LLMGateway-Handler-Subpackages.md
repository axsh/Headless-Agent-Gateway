# 040: LLMGateway ハンドラのサブパッケージ移設

## 背景 (Background)

先行タスク (038) で `llmgateway` パッケージのプロバイダ登録 (`provider_anthropic.go` 等) をサブパッケージ (`llmgateway/anthropic/provider.go` 等) に移設した。しかし、各プロバイダの HTTP ハンドラ実装 (`proxy_anthropic.go`, `proxy_openai.go`) やそれに付随する型定義・変換ロジック (`types_anthropic.go`, `convert_anthropic_bifrost.go`) は親パッケージ (`llmgateway`) にフラットに残っている。

これらのファイルはプロバイダ固有のロジックであり、対応するサブパッケージの下にまとめるのが自然な構造である。

### 現状の問題

- `llmgateway/` 直下に 5,355 行のコードがフラットに存在し、見通しが悪い
- プロバイダ固有のハンドラ (`proxy_anthropic.go`: 455行, `proxy_openai.go`: 299行) が汎用コード (`proxy.go`, `routing.go`) と混在
- ハンドラが `ProxyServer` のメソッドとして実装されており、`p.cfg`, `p.driver`, `p.logger`, `p.vault` 等の非公開フィールドに直接アクセスしている

## 要件 (Requirements)

### 必須要件

1. **HandlerContext インターフェースの定義**: ハンドラがサブパッケージから親パッケージの機能にアクセスするための公開インターフェースを `llmgateway` パッケージに定義する
2. **Anthropic ハンドラの移設**: 以下のファイルを `llmgateway/anthropic/` サブパッケージに移動する
   - `proxy_anthropic.go` -> `anthropic/handler.go`
   - `types_anthropic.go` -> `anthropic/types.go`
   - `convert_anthropic_bifrost.go` -> `anthropic/convert.go`
   - `proxy_anthropic_test.go` -> `anthropic/handler_test.go`
   - `convert_anthropic_bifrost_test.go` -> `anthropic/convert_test.go`
3. **OpenAI ハンドラの移設**: 以下のファイルを `llmgateway/openai/` サブパッケージに移動する
   - `proxy_openai.go` -> `openai/handler.go`
   - `proxy_openai_test.go` -> `openai/handler_test.go`
4. **共有ユーティリティの整理**: 両ハンドラから参照される関数を親パッケージに公開関数として残す
   - `toBifrostProvider` -> `ToBifrostProvider` (エクスポート)
   - `sanitizeToolsForProvider` (tool_sanitize.go) -> `SanitizeToolsForProvider` (エクスポート)
   - `isStreamRequest` -> OpenAI サブパッケージ内に移動 (OpenAI専用)
5. **setupRoutes の修正**: `ProxyServer.setupRoutes()` がサブパッケージのハンドラ関数を呼び出す形に変更する
6. **既存テストの維持**: 全テストが変更後も PASS すること

### 任意要件

- `fallback.go` 内の `TryFallbackAnthropicResponse` を `anthropic/` サブパッケージへの移動も検討する (ただし、`ExtractToolCallFromText` 等の共通関数との依存があるため、必須ではない)

## 実現方針 (Implementation Approach)

### HandlerContext インターフェース設計

`llmgateway` パッケージに以下のインターフェースを定義する:

```go
// HandlerContext provides handler-level access to ProxyServer internals.
// Subpackage handlers receive this instead of a *ProxyServer reference.
type HandlerContext interface {
    // Config returns the application config.
    Config() *config.AppConfig
    // Logger returns the logger instance.
    Logger() logger.Logger
    // Vault returns the vault store (may be nil).
    Vault() vault.VaultStore
    // Router returns the model router (may be nil).
    Router() *ModelRouter
    // BifrostSDK returns the Bifrost SDK instance (may be nil).
    BifrostSDK() *bifrost.Bifrost
}
```

`ProxyServer` がこのインターフェースを実装する。

### ファイル移動後のディレクトリ構造

```
llmgateway/
  anthropic/
    provider.go         (既存: プロバイダ登録)
    provider_test.go    (既存)
    handler.go          (新規: proxy_anthropic.go から移設)
    handler_test.go     (新規: proxy_anthropic_test.go から移設)
    types.go            (新規: types_anthropic.go から移設)
    convert.go          (新規: convert_anthropic_bifrost.go から移設)
    convert_test.go     (新規: convert_anthropic_bifrost_test.go から移設)
  openai/
    provider.go         (既存: プロバイダ登録)
    provider_test.go    (既存)
    handler.go          (新規: proxy_openai.go から移設)
    handler_test.go     (新規: proxy_openai_test.go から移設)
  google/
    provider.go         (既存: 変更なし)
    provider_test.go    (既存)
  ollama/
    provider.go         (既存: 変更なし)
    provider_test.go    (既存)
  backend.go            (既存)
  bifrost_account.go    (既存)
  bifrost_driver.go     (既存)
  errors.go             (既存)
  fallback.go           (既存)
  handler_context.go    (新規: HandlerContext インターフェース)
  masking.go            (既存)
  provider.go           (既存: レジストリ)
  proxy.go              (既存: setupRoutes 修正)
  proxy_tls.go          (既存)
  routing.go            (既存)
  stub.go               (既存)
  tool_sanitize.go      (既存: エクスポート化)
```

### setupRoutes の変更

```go
func (p *ProxyServer) setupRoutes(mux *http.ServeMux) {
    mux.HandleFunc("GET /{$}", p.handleIndex)
    mux.HandleFunc("GET /health", p.handleHealth)
    mux.HandleFunc("GET /v1/models", p.handleModels)
    mux.HandleFunc("POST /v1/messages", p.authMiddleware(anthropic.HandleMessages(p)))
    mux.HandleFunc("POST /v1/responses", p.authMiddleware(openai.HandleResponses(p)))
}
```

各サブパッケージは `func HandleMessages(ctx llmgateway.HandlerContext) http.HandlerFunc` のような関数を公開する。

### テスト戦略

- サブパッケージのテストは `HandlerContext` のモック実装を使って独立テスト可能にする
- 既存の `proxy_test.go` (統合テスト) はそのまま維持
- `test_helpers_test.go` の `TestMain` によるプロバイダ登録パターンは継続

## 検証シナリオ (Verification Scenarios)

1. `proxy_anthropic.go` のコードを `anthropic/handler.go` に移動し、`func HandleMessages(ctx HandlerContext) http.HandlerFunc` としてエクスポートする
2. `proxy_openai.go` のコードを `openai/handler.go` に移動し、`func HandleResponses(ctx HandlerContext) http.HandlerFunc` としてエクスポートする
3. `ProxyServer.setupRoutes()` がサブパッケージの関数を呼び出す形に変更される
4. `./scripts/process/build.sh` が成功する
5. `llmgateway/` 直下にプロバイダ固有ファイルが残っていないことを確認

## テスト項目 (Testing for the Requirements)

### ビルド検証

```bash
./scripts/process/build.sh
```

### 単体テスト (サブパッケージ)

```bash
cd shared/libs/go && go test -v -count=1 ./llmgateway/anthropic/...
cd shared/libs/go && go test -v -count=1 ./llmgateway/openai/...
```

### 単体テスト (親パッケージ)

```bash
cd shared/libs/go && go test -v -count=1 ./llmgateway/
```

### 統合テスト

```bash
./scripts/process/integration_test.sh --categories llm
```
