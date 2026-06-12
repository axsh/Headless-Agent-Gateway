# 038: llmgateway パッケージのプロバイダ別サブパッケージ化

## 背景 (Background)

現在の `shared/libs/go/llmgateway/` パッケージは35ファイルがフラットに配置されており、プロバイダ固有の実装（anthropic, google, openai, ollama）がコアロジックと混在している。

### 現在のファイル構成と問題

```
llmgateway/
  provider.go                      # Provider インターフェース + レジストリ (コア)
  provider_anthropic.go             # Anthropic Provider (init登録済み)
  provider_google.go                # Google Provider (init登録済み)
  provider_openai.go                # OpenAI Provider (init登録済み)
  provider_ollama.go                # Ollama Provider (init登録済み)
  proxy.go                         # ProxyServer コア (コア)
  proxy_anthropic.go               # Anthropic HTTPハンドラ + SSEストリーム変換 (456行)
  proxy_openai.go                  # OpenAI HTTPハンドラ + SSEストリーム変換 (300行)
  convert_anthropic_bifrost.go     # Anthropic <-> Bifrost 変換 (347行)
  types_anthropic.go               # Anthropic 型定義
  fallback.go                      # Anthropic ToolCall フォールバック
  tool_sanitize.go                 # クロスプロバイダ ツール名サニタイズ
  ... (その他コアファイル)
```

問題:
1. **ファイルが多すぎてナビゲーションが困難**: 35ファイルがフラットに存在
2. **プロバイダ固有コードが目視で判別しにくい**: `proxy_anthropic.go` と `proxy.go` が同階層
3. **新プロバイダ追加時に影響範囲が不明確**: どのファイルがプロバイダ固有かわからない

`codingagent` パッケージでは既に `claudecode/`, `codex/` とサブパッケージに分けて factory/self-register パターンを採用しており、`llmgateway` もこれに合わせるべきである。

## 要件 (Requirements)

### 必須要件

1. **プロバイダ別サブパッケージの作成**: プロバイダ固有の実装を以下のサブパッケージに分離する:
   - `llmgateway/anthropic/` -- Anthropic固有コード
   - `llmgateway/openai/` -- OpenAI固有コード
   - `llmgateway/google/` -- Google固有コード (現在は provider 登録のみ)
   - `llmgateway/ollama/` -- Ollama固有コード (現在は provider 登録のみ)

2. **Factory/Self-Register パターンの適用**: 各プロバイダサブパッケージの `init()` 関数で以下を登録する:
   - `Provider` インターフェース実装 (既存の `RegisterProvider` を活用)
   - **HTTPハンドラファクトリ** (新規): プロバイダ固有のHTTPハンドラを `ProxyServer` に登録する仕組み

3. **コアパッケージのスリム化**: `llmgateway/` パッケージ直下にはコアロジックのみ残す:
   - `backend.go` -- インターフェース定義
   - `provider.go` -- Provider インターフェース + レジストリ
   - `proxy.go` -- ProxyServer コア (setupRoutes はレジストリからハンドラを取得)
   - `routing.go` -- モデルルーティング
   - `bifrost_driver.go` / `bifrost_account.go` -- Bifrost SDK連携
   - `errors.go` -- エラーハンドリング
   - `masking.go` -- シークレットマスキング
   - `stub.go` -- テスト用スタブ

4. **各プロバイダサブパッケージに移動するファイル**:

   | 移動先 | ファイル | 内容 |
   | :--- | :--- | :--- |
   | `anthropic/` | `provider_anthropic.go` | Provider 実装 |
   | `anthropic/` | `proxy_anthropic.go` | HTTPハンドラ + SSEストリーム変換 |
   | `anthropic/` | `convert_anthropic_bifrost.go` | Anthropic <-> Bifrost 変換 |
   | `anthropic/` | `types_anthropic.go` | Anthropic 型定義 |
   | `anthropic/` | `fallback.go` | ToolCall フォールバック |
   | `openai/` | `provider_openai.go` | Provider 実装 |
   | `openai/` | `proxy_openai.go` | HTTPハンドラ + SSEストリーム変換 |
   | `google/` | `provider_google.go` | Provider 実装 |
   | `ollama/` | `provider_ollama.go` | Provider 実装 |

5. **ハンドラレジストリの導入**: `setupRoutes` がプロバイダを静的に参照する現在の方式から、レジストリベースの動的ルート登録に変更する。

### 任意要件

- `passthrough.go` はプロバイダ横断的な機能なのでコアに残す
- `tool_sanitize.go` はクロスプロバイダ機能なのでコアに残す
- `proxy_tls.go` はインフラ機能なのでコアに残す
- テストファイルも対応するサブパッケージに移動する

## 実現方針 (Implementation Approach)

### 移動後のディレクトリ構造

```
llmgateway/
  # コア
  backend.go                     # LLMGatewayBackend インターフェース
  provider.go                    # Provider インターフェース + レジストリ
  handler.go                     # [NEW] ハンドラレジストリ (RegisterHandler/GetHandlers)
  proxy.go                       # ProxyServer コア (setupRoutes -> レジストリ参照)
  routing.go                     # ModelRouter
  bifrost_driver.go              # Bifrost SDK ドライバ
  bifrost_account.go             # Bifrost アカウント管理
  errors.go                      # GatewayError
  masking.go                     # MaskSecret
  passthrough.go                 # PassthroughDriver
  tool_sanitize.go               # sanitizeToolsForProvider
  proxy_tls.go                   # TLSCertManager
  stub.go                        # StubGateway

  # プロバイダ別サブパッケージ
  anthropic/
    provider.go                  # init() で RegisterProvider
    handler.go                   # init() で RegisterHandler (POST /v1/messages)
    convert.go                   # Anthropic <-> Bifrost 変換
    types.go                     # Anthropic 型定義
    fallback.go                  # ToolCall フォールバック
    handler_test.go              # テスト
    convert_test.go              # テスト

  openai/
    provider.go                  # init() で RegisterProvider
    handler.go                   # init() で RegisterHandler (POST /v1/responses)
    handler_test.go              # テスト

  google/
    provider.go                  # init() で RegisterProvider

  ollama/
    provider.go                  # init() で RegisterProvider
```

### ハンドラレジストリの設計

```go
// llmgateway/handler.go (新規)

// HandlerFactory creates an HTTP handler bound to a ProxyServer.
type HandlerFactory func(p *ProxyServer) http.HandlerFunc

// RegisterHandler registers a provider-specific HTTP handler.
func RegisterHandler(method, path string, factory HandlerFactory)

// AllHandlers returns all registered handler factories.
func AllHandlers() []RegisteredHandler
```

各プロバイダの `init()` で:

```go
// anthropic/handler.go
func init() {
    llmgateway.RegisterHandler("POST", "/v1/messages", NewAnthropicHandler)
}

// openai/handler.go
func init() {
    llmgateway.RegisterHandler("POST", "/v1/responses", NewOpenAIHandler)
}
```

### 循環参照の回避

サブパッケージが親パッケージの `ProxyServer` を参照する必要があるため、インターフェースを使って循環参照を回避する:

```go
// llmgateway/handler.go
type HandlerContext interface {
    Logger() logger.Logger
    Config() *config.AppConfig
    Vault() vault.VaultStore
    BifrostSDK() ...
    Router() *ModelRouter
}
```

## 検証シナリオ (Verification Scenarios)

1. ビルドが通ることを確認（循環参照がないこと）
2. 全ての Provider が `init()` で自動登録されることを確認
3. HTTPハンドラが正しくルーティングされることを確認
4. 既存の単体テストが全て通ることを確認
5. 既存の E2E テストが全て通ることを確認

## テスト項目 (Testing for the Requirements)

```bash
# ビルドと単体テスト
./scripts/process/build.sh

# 統合テスト（LLMカテゴリ）
./scripts/process/integration_test.sh --specify "TestE2E_"
```
