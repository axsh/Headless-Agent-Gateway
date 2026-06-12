# 051-LLMGateway-Handler-Subpackages

> **Source Specification**: [040-LLMGateway-Handler-Subpackages.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/ideas/040-LLMGateway-Handler-Subpackages.md)

## Goal Description

`llmgateway` パッケージの HTTP ハンドラ (`proxy_anthropic.go`, `proxy_openai.go`) とそれに付随する型・変換ロジックを、対応するサブパッケージ (`anthropic/`, `openai/`) に移設する。`HandlerContext` インターフェースを導入し、ハンドラがサブパッケージから親パッケージの機能にアクセスできるようにする。

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| HandlerContext インターフェースの定義 | Proposed Changes > handler_context.go |
| Anthropic ハンドラの移設 | Proposed Changes > anthropic/handler.go, types.go, convert.go |
| OpenAI ハンドラの移設 | Proposed Changes > openai/handler.go |
| 共有ユーティリティの整理 (ToBifrostProvider, SanitizeToolsForProvider) | Proposed Changes > proxy_openai.go (削除), tool_sanitize.go |
| setupRoutes の修正 | Proposed Changes > proxy.go |
| 既存テストの維持 | Verification Plan |

## Proposed Changes

### llmgateway (親パッケージ)

---

#### [NEW] [handler_context.go](file://shared/libs/go/llmgateway/handler_context.go)
*   **Description**: ハンドラがサブパッケージから親パッケージの機能にアクセスするための公開インターフェースと、`ProxyServer` による実装
*   **Technical Design**:
    ```go
    package llmgateway

    import (
        bifrost "github.com/maximhq/bifrost/core"
        "github.com/axsh/arctic-tern/config"
        "github.com/axsh/arctic-tern/logger"
        "github.com/axsh/arctic-tern/vault"
    )

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

    // Compile-time check: ProxyServer implements HandlerContext.
    var _ HandlerContext = (*ProxyServer)(nil)

    func (p *ProxyServer) Config() *config.AppConfig { return p.cfg }
    func (p *ProxyServer) Logger() logger.Logger     { return p.logger }
    func (p *ProxyServer) Vault() vault.VaultStore    { return p.vault }

    func (p *ProxyServer) Router() *ModelRouter {
        if p.driver == nil {
            return nil
        }
        return p.driver.router
    }

    func (p *ProxyServer) BifrostSDK() *bifrost.Bifrost {
        if p.driver == nil {
            return nil
        }
        return p.driver.bifrostSDK
    }
    ```

#### [MODIFY] [tool_sanitize.go](file://shared/libs/go/llmgateway/tool_sanitize.go)
*   **Description**: `sanitizeToolsForProvider` をエクスポート化 (`SanitizeToolsForProvider`)
*   **Technical Design**:
    - 関数名を `sanitizeToolsForProvider` -> `SanitizeToolsForProvider` に変更
    - シグネチャ: `func SanitizeToolsForProvider(req *bifrostSchemas.BifrostResponsesRequest, provider bifrostSchemas.ModelProvider, log logger.Logger)`

#### [MODIFY] [proxy.go](file://shared/libs/go/llmgateway/proxy.go)
*   **Description**: `setupRoutes` がサブパッケージのハンドラ関数を呼び出す形に変更
*   **Technical Design**:
    ```go
    import (
        anthropicHandler "github.com/axsh/arctic-tern/llmgateway/anthropic"
        openaiHandler "github.com/axsh/arctic-tern/llmgateway/openai"
    )

    func (p *ProxyServer) setupRoutes(mux *http.ServeMux) {
        mux.HandleFunc("GET /{$}", p.handleIndex)
        mux.HandleFunc("GET /health", p.handleHealth)
        mux.HandleFunc("GET /v1/models", p.handleModels)
        mux.HandleFunc("POST /v1/messages", p.authMiddleware(anthropicHandler.HandleMessages(p)))
        mux.HandleFunc("POST /v1/responses", p.authMiddleware(openaiHandler.HandleResponses(p)))
    }
    ```
*   **Logic**: `p.handleAnthropicMessages` -> `anthropicHandler.HandleMessages(p)` に変更。`p` は `HandlerContext` インターフェースとして渡される。

#### [DELETE] [proxy_anthropic.go](file://shared/libs/go/llmgateway/proxy_anthropic.go)
*   **Description**: `anthropic/handler.go` に移設後、削除

#### [DELETE] [types_anthropic.go](file://shared/libs/go/llmgateway/types_anthropic.go)
*   **Description**: `anthropic/types.go` に移設後、削除

#### [DELETE] [convert_anthropic_bifrost.go](file://shared/libs/go/llmgateway/convert_anthropic_bifrost.go)
*   **Description**: `anthropic/convert.go` に移設後、削除

#### [DELETE] [proxy_anthropic_test.go](file://shared/libs/go/llmgateway/proxy_anthropic_test.go)
*   **Description**: `anthropic/handler_test.go` に移設後、削除

#### [DELETE] [convert_anthropic_bifrost_test.go](file://shared/libs/go/llmgateway/convert_anthropic_bifrost_test.go)
*   **Description**: `anthropic/convert_test.go` に移設後、削除

#### [DELETE] [proxy_openai.go](file://shared/libs/go/llmgateway/proxy_openai.go)
*   **Description**: `openai/handler.go` に移設後、削除。`toBifrostProvider` は `ToBifrostProvider` として親パッケージに残す（新ファイルまたは既存ファイルに配置）

#### [DELETE] [proxy_openai_test.go](file://shared/libs/go/llmgateway/proxy_openai_test.go)
*   **Description**: `openai/handler_test.go` に移設後、削除

---

### llmgateway/anthropic (サブパッケージ)

---

#### [NEW] [handler.go](file://shared/libs/go/llmgateway/anthropic/handler.go)
*   **Description**: `proxy_anthropic.go` から移設。`ProxyServer` のメソッドをパッケージレベル関数に変換
*   **Technical Design**:
    ```go
    package anthropic

    import (
        "encoding/json"
        "errors"
        "fmt"
        "io"
        "net/http"

        bifrostSchemas "github.com/maximhq/bifrost/core/schemas"

        "github.com/axsh/arctic-tern/llmgateway"
        "github.com/axsh/arctic-tern/vault"
    )

    // HandleMessages returns an http.HandlerFunc that handles POST /v1/messages.
    func HandleMessages(ctx llmgateway.HandlerContext) http.HandlerFunc {
        return func(w http.ResponseWriter, r *http.Request) {
            handleAnthropicMessages(ctx, w, r)
        }
    }
    ```
*   **Logic**:
    - `p.cfg` -> `ctx.Config()`
    - `p.logger` -> `ctx.Logger()`
    - `p.vault` -> `ctx.Vault()`
    - `p.driver.router` -> `ctx.Router()`
    - `p.driver.bifrostSDK` -> `ctx.BifrostSDK()`
    - `p.handleAnthropicMessagesViaBifrost(...)` -> `handleAnthropicMessagesViaBifrost(ctx, ...)`
    - 全てのメソッドは `func handleXxx(ctx llmgateway.HandlerContext, ...)` のパッケージレベル関数に変換
    - `toBifrostProvider` -> `llmgateway.ToBifrostProvider`
    - `sanitizeToolsForProvider` -> `llmgateway.SanitizeToolsForProvider`
    - `WriteErrorResponse` -> `llmgateway.WriteErrorResponse`
    - `MaskSecret` -> `llmgateway.MaskSecret`
    - `ExtractSessionID` -> `llmgateway.ExtractSessionID`
    - `ExtractFallbackFlag` -> `llmgateway.ExtractFallbackFlag`
    - `TryFallbackAnthropicResponse` -> `llmgateway.TryFallbackAnthropicResponse`
    - `emitSSEJSON`, `generateAnthropicID` はこのパッケージ内の非公開関数として移動
    - `anthropicRequest` 型もこのパッケージ内の非公開型として移動

#### [NEW] [types.go](file://shared/libs/go/llmgateway/anthropic/types.go)
*   **Description**: `types_anthropic.go` から移設。Anthropic API の型定義
*   **Technical Design**:
    ```go
    package anthropic

    // AnthropicFullRequest, AnthropicTool, AnthropicMsg,
    // ContentBlock, AnthropicResponse, AnthropicUsage
    // をそのまま移動。パッケージ名が anthropic になるため、
    // 外部からは anthropic.FullRequest のように参照可能。
    // ただし親パッケージ (llmgateway) 内の fallback.go が
    // AnthropicResponse を参照しているため、型名はそのまま維持する。
    ```
*   **Logic**: 型名は全てエクスポートのまま維持。`json` タグは変更なし。

#### [NEW] [convert.go](file://shared/libs/go/llmgateway/anthropic/convert.go)
*   **Description**: `convert_anthropic_bifrost.go` から移設。Anthropic <-> Bifrost 変換ロジック
*   **Technical Design**:
    - `ConvertAnthropicToBifrost` -> そのまま移動
    - `ConvertBifrostToAnthropic` -> そのまま移動
    - 内部ヘルパー関数 (`extractSystemInstructions`, `convertAnthropicMessage`, etc.) もそのまま移動
    - `generateAnthropicID`, `randomHexString` もこのファイルまたは `handler.go` に移動

#### [NEW] [convert_test.go](file://shared/libs/go/llmgateway/anthropic/convert_test.go)
*   **Description**: `convert_anthropic_bifrost_test.go` から移設
*   **Logic**: パッケージ名を `anthropic` に変更。内部型への参照を調整。

#### [NEW] [handler_test.go](file://shared/libs/go/llmgateway/anthropic/handler_test.go)
*   **Description**: `proxy_anthropic_test.go` から移設
*   **Logic**: パッケージ名を `anthropic` に変更。

---

### llmgateway/openai (サブパッケージ)

---

#### [NEW] [handler.go](file://shared/libs/go/llmgateway/openai/handler.go)
*   **Description**: `proxy_openai.go` から移設。`ProxyServer` のメソッドをパッケージレベル関数に変換
*   **Technical Design**:
    ```go
    package openai

    import (
        "github.com/axsh/arctic-tern/llmgateway"
    )

    // HandleResponses returns an http.HandlerFunc that handles POST /v1/responses.
    func HandleResponses(ctx llmgateway.HandlerContext) http.HandlerFunc {
        return func(w http.ResponseWriter, r *http.Request) {
            handleOpenAIResponses(ctx, w, r)
        }
    }
    ```
*   **Logic**:
    - Anthropic と同様のパターンで `p.*` -> `ctx.*()` に変換
    - `isStreamRequest` はこのパッケージ内の非公開関数として移動 (OpenAI 専用)
    - `toBifrostProvider` -> `llmgateway.ToBifrostProvider`
    - `sanitizeToolsForProvider` -> `llmgateway.SanitizeToolsForProvider`
    - `openaiRequest` 型もこのパッケージ内に移動

#### [NEW] [handler_test.go](file://shared/libs/go/llmgateway/openai/handler_test.go)
*   **Description**: `proxy_openai_test.go` から移設
*   **Logic**: パッケージ名を `openai` に変更。

---

### llmgateway (残す共有関数)

---

#### [NEW] [bifrost_helpers.go](file://shared/libs/go/llmgateway/bifrost_helpers.go)
*   **Description**: `toBifrostProvider` をエクスポートして親パッケージに残す
*   **Technical Design**:
    ```go
    package llmgateway

    import bifrostSchemas "github.com/maximhq/bifrost/core/schemas"

    // ToBifrostProvider converts tern provider name to Bifrost ModelProvider.
    // Uses the Provider Registry first, then falls back to static mapping.
    func ToBifrostProvider(provider string) bifrostSchemas.ModelProvider {
        if mp, ok := resolveProviderName(provider); ok {
            return mp
        }
        return bifrostSchemas.ModelProvider(provider)
    }
    ```

---

### fallback.go の対応

---

#### [MODIFY] [fallback.go](file://shared/libs/go/llmgateway/fallback.go)
*   **Description**: `TryFallbackAnthropicResponse` は親パッケージに残す。ただし Anthropic 型への依存がある場合はサブパッケージをインポートする。
*   **Logic**:
    - `TryFallbackAnthropicResponse` は `AnthropicResponse` 型を使用するが、この型は `anthropic/types.go` に移動する。
    - `fallback.go` から `anthropic.AnthropicResponse` をインポートすることになるが、これは循環参照にならない (親 -> 子方向のインポートではなく、子パッケージを利用する方向)。
    - **注意**: 親パッケージ (`llmgateway`) がサブパッケージ (`llmgateway/anthropic`) をインポートすると循環参照の可能性がある。`anthropic` パッケージは `llmgateway` パッケージの `HandlerContext` を使うため。
    - **対応**: `TryFallbackAnthropicResponse` は `[]byte` を受け取り `[]byte` を返すため、内部で `json.Unmarshal` を使って匿名構造体にデシリアライズしている。`AnthropicResponse` 型は直接使っていない。したがって `fallback.go` は変更不要。

## Step-by-Step Implementation Guide

1. **HandlerContext インターフェース作成**:
   - `shared/libs/go/llmgateway/handler_context.go` を新規作成
   - `ProxyServer` による実装メソッド (Config, Logger, Vault, Router, BifrostSDK) を定義
   - コミット

2. **共有ユーティリティのエクスポート化**:
   - `tool_sanitize.go`: `sanitizeToolsForProvider` -> `SanitizeToolsForProvider` にリネーム
   - `bifrost_helpers.go` を新規作成: `toBifrostProvider` -> `ToBifrostProvider` をエクスポート
   - 親パッケージ内で旧名を使っている箇所は一旦このステップでは残す（後で削除ファイルと共に消える）
   - コミット

3. **Anthropic ハンドラのサブパッケージ移設**:
   - `anthropic/types.go`: `types_anthropic.go` の内容を移動（パッケージ名変更）
   - `anthropic/convert.go`: `convert_anthropic_bifrost.go` の内容を移動（パッケージ名変更、親パッケージ関数参照を `llmgateway.` プレフィックスに）
   - `anthropic/convert_test.go`: `convert_anthropic_bifrost_test.go` の内容を移動
   - `anthropic/handler.go`: `proxy_anthropic.go` の内容を移動（`ProxyServer` メソッド -> パッケージレベル関数に変換、`ctx.Config()` 等の呼び出しに変換）
   - `anthropic/handler_test.go`: `proxy_anthropic_test.go` の内容を移動
   - 旧ファイル (`proxy_anthropic.go`, `types_anthropic.go`, `convert_anthropic_bifrost.go`, テスト) を削除
   - コミット

4. **OpenAI ハンドラのサブパッケージ移設**:
   - `openai/handler.go`: `proxy_openai.go` の内容を移動（`isStreamRequest` も含む）
   - `openai/handler_test.go`: `proxy_openai_test.go` の内容を移動
   - `proxy_openai.go` 内の `toBifrostProvider` は Step 2 で既にエクスポート済み
   - 旧ファイル (`proxy_openai.go`, テスト) を削除
   - コミット

5. **setupRoutes 修正**:
   - `proxy.go` のインポートに `anthropicHandler`, `openaiHandler` を追加
   - `setupRoutes` を `anthropicHandler.HandleMessages(p)`, `openaiHandler.HandleResponses(p)` に変更
   - コミット

6. **ビルドとテスト実行** (Verification Plan 参照)

## Verification Plan

### Automated Verification

1. **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```
    - `shared/libs/go/llmgateway/` 直下にプロバイダ固有ファイルが残っていないこと
    - 全テスト PASS

2. **Integration Tests**:
    ```bash
    ./scripts/process/integration_test.sh --categories llm
    ```
    - LLM 関連の統合テストが全て PASS すること

3. **E2E テスト**:
    E2E テストの追加は不要。理由: 純粋な内部リファクタリングであり、外部から観測可能な動作に変更がない。既存の統合テストと E2E テストでカバーされる。

### テスト設計セルフレビュー

- **網羅性**: ビルド + 既存単体テスト + 統合テストで、リファクタリングによるリグレッションを検出可能
- **証拠の十分性**: 統合テスト (LLM カテゴリ) はエンドツーエンドで Anthropic/OpenAI ハンドラ経由のリクエストを検証する
- **迂回排除**: setupRoutes が正しくサブパッケージのハンドラを呼び出すことはビルド成功で保証（コンパイルエラーが起きれば検出される）
- **依存関係**: HandlerContext -> Handler -> setupRoutes の順で積み上げ

### 総合判定

全テスト完了後、testing-rules.md Section 12 に従い総合判定を実施する。
