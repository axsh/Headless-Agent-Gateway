# 004-Foundation-Part3-BifrostDriver-Routing-Handlers

> **Source Specification**:
> - [001-LLMGatewayProxy.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/ideas/001-LLMGatewayProxy.md)
> - [000-Architecture.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/ideas/000-Architecture.md)
> - [investigation_summary.md](file://prompts/designs/hag/investigation_summary.md)

## Goal Description

Part2 で構築した ProxyServer (HTTP Proxy スケルトン) 上のスタブハンドラを実装に置き換え、実際の LLM プロバイダとの通信を行えるようにする。具体的には:

1. **モデルルーティング** (`routing.go`): リクエストの `model` フィールドから `model_profiles.yaml` を参照し、プロバイダ・キー・モデルを特定するルーティングロジック
2. **BifrostDriver** (`bifrost_driver.go`): Bifrost SDK (`github.com/maximhq/bifrost/core`) をラップし、`LLMGatewayBackend` インターフェースを実装する Driver
3. **Anthropic Messages API ハンドラ** (`proxy_anthropic.go`): `POST /v1/messages` を受け取り、Bifrost SDK 経由で Anthropic プロバイダに中継。SSE ストリーミング対応
4. **OpenAI Chat Completions ハンドラ** (`proxy_openai.go`): `POST /v1/chat/completions` を受け取り、Bifrost SDK 経由で OpenAI プロバイダに中継。非ストリーミングのみ (ストリーミングは TODO)
5. **Secret マスキング** (`masking.go`): API キーの下4桁マスクユーティリティ

## User Review Required

> [!IMPORTANT]
> **Bifrost SDK 依存の追加**: `go.mod` に `github.com/maximhq/bifrost` を追加します。Bifrost SDK は fasthttp ベースの大量の依存を持つため、`go.sum` のサイズが大幅に増加します。

> [!IMPORTANT]
> **BifrostDriver vs ProxyServer の位置付け**: Part2 では `ProxyServer` が `LLMGatewayBackend` を直接実装していましたが、Part3 では `BifrostDriver` が新しい `LLMGatewayBackend` 実装となります。`ProxyServer` は引き続き HTTP サーバを担当しますが、LLM リクエストの処理は `BifrostDriver` に委譲する形になります。`hag.Server` の `WithGateway` で注入するのは `BifrostDriver` になります。
>
> - `BifrostDriver`: `LLMGatewayBackend` 実装。内部で `ProxyServer` を HTTP フロントエンドとして使い、Bifrost SDK をバックエンドとして使う
> - `ProxyServer`: HTTP サーバのみ。`BifrostDriver` への参照を持つ

> [!WARNING]
> **Account インターフェースの実装**: Bifrost SDK は `schemas.Account` インターフェースを要求します。`model_profiles.yaml` から Bifrost の Account 形式に変換するアダプタ (`BifrostAccount`) を実装する必要があります。

---

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1-3: BifrostDriver を定義 | `bifrost_driver.go` |
| R1-4: Bifrost SDK ラッパー | `bifrost_driver.go` > BifrostDriver struct |
| R2-1: HTTP エンドポイント | `proxy.go` (既存、ハンドラ委譲に変更) |
| R3-1: Anthropic Messages API 準拠 | `proxy_anthropic.go` |
| R3-2: SSE独自拡張なし | `proxy_anthropic.go` > SSE 透過転送 |
| R3-3: model から provider ルックアップ | `routing.go` > ResolveModel() |
| R3-4: provider/model 別フィールド | `routing.go` > RoutedModel struct |
| R4-1: OpenAI Chat Completions 非ストリーミング | `proxy_openai.go` |
| R4-2: stream:true は TODO | `proxy_openai.go` > TODO コメント |
| R5-1: model_profiles.yaml 照合 | `routing.go` > ResolveModel() |
| R5-2: 未定義モデルのエラー | `routing.go` > ErrModelNotFound |
| R5-3: Claude Code サブセッションフォールバック | Part4 に先送り (セッション管理が前提) |
| R7-1: エラーレスポンス互換 JSON | `errors.go` (既存) |
| R7-2: プロバイダエラー変換 | `proxy_anthropic.go`, `proxy_openai.go` |
| R7-3: HTTP ステータス開示 | `errors.go` (既存の GatewayError.Message に含める) |
| R8-1: Rate Limiting は Bifrost 委譲 | `bifrost_driver.go` (Bifrost SDK 内蔵) |
| R9-1: logger パッケージ使用 | 全ファイルで logger.Logger を使用 |
| R9-2: API キーマスク | `masking.go` |
| R10-1: ProxyURL() | `bifrost_driver.go` > ProxyURL() |

**先送り事項**:
- R1-5 (PassthroughDriver): Part4
- R5-3 (サブセッションフォールバック): Part4 (セッション管理が必要)
- R6 (テキスト→ToolCall 変換): Part4 (モデル動作設定と連動)
- R9-3 (Trace ログレベル): Part4 (ログ階層化と連動)
- R9-4 (メトリクス収集): Part4

---

## Proposed Changes

### llmgateway パッケージ

テスト記述はボトムアップ順で先に記載する。

---

#### [NEW] [masking.go](file://shared/libs/go/llmgateway/masking.go)
*   **Description**: API キーのマスキングユーティリティ
*   **Technical Design**:
    ```go
    // MaskSecret masks a secret string, showing only the last 4 characters.
    // If the string is 4 characters or shorter, it returns "****".
    func MaskSecret(s string) string
    ```
*   **Logic**:
    1. `len(s) <= 4` の場合は `"****"` を返す
    2. それ以外は `"****" + s[len(s)-4:]` を返す

---

#### [NEW] [masking_test.go](file://shared/libs/go/llmgateway/masking_test.go)
*   **Description**: MaskSecret のテーブル駆動テスト
*   **テストケース**:
    | input | expected |
    |---|---|
    | `"sk-ant-api03-xxxxx1234"` | `"****1234"` |
    | `"ab"` | `"****"` |
    | `""` | `"****"` |
    | `"1234"` | `"****"` |
    | `"12345"` | `"****2345"` |

---

#### [NEW] [routing.go](file://shared/libs/go/llmgateway/routing.go)
*   **Description**: model_profiles.yaml からモデルをルックアップするルーティングロジック
*   **Technical Design**:
    ```go
    // RoutedModel holds the resolved provider, key value, and model name.
    type RoutedModel struct {
        Provider string // e.g. "anthropic"
        KeyName  string // e.g. "primary"
        KeyValue string // actual API key (or vault:// ref)
        Model    string // e.g. "claude-sonnet-4-20250514"
    }

    // ModelRouter resolves model names to provider/key/model using profiles.
    type ModelRouter struct {
        profiles *config.ModelProfilesConfig
        logger   logger.Logger
    }

    // NewModelRouter creates a ModelRouter from model profiles config.
    func NewModelRouter(profiles *config.ModelProfilesConfig, log logger.Logger) *ModelRouter

    // ResolveModel resolves a model name to a RoutedModel.
    // Returns ErrModelNotFound if the model is not defined in profiles.
    func (r *ModelRouter) ResolveModel(modelName string) (*RoutedModel, error)
    ```
*   **Logic**:
    1. `profiles.Providers` を走査し、各 provider の各 key の各 model を検索
    2. `model.Name == modelName` にマッチしたら `RoutedModel` を返す
    3. マッチしなければ `ErrModelNotFound` を返す
    4. `profiles` が nil の場合は常に `ErrModelNotFound`

---

#### [NEW] [routing_test.go](file://shared/libs/go/llmgateway/routing_test.go)
*   **Description**: ModelRouter のテーブル駆動テスト
*   **テストケース**:
    | シナリオ | 入力 model | 期待結果 |
    |---|---|---|
    | 正常: anthropic モデル | `claude-sonnet-4-20250514` | RoutedModel{Provider: "anthropic", ...} |
    | 正常: openai モデル | `gpt-4o` | RoutedModel{Provider: "openai", ...} |
    | 異常: 未定義モデル | `nonexistent-model` | ErrModelNotFound |
    | 異常: nil profiles | any | ErrModelNotFound |
    | 境界: 空文字列 | `""` | ErrModelNotFound |

---

#### [NEW] [bifrost_account.go](file://shared/libs/go/llmgateway/bifrost_account.go)
*   **Description**: `model_profiles.yaml` を Bifrost SDK の `schemas.Account` に変換するアダプタ
*   **Technical Design**:
    ```go
    // BifrostAccount implements bifrost schemas.Account interface
    // by adapting model_profiles.yaml configuration.
    type BifrostAccount struct {
        profiles *config.ModelProfilesConfig
        vault    vault.VaultStore
        logger   logger.Logger
    }

    // NewBifrostAccount creates a BifrostAccount from model profiles.
    func NewBifrostAccount(
        profiles *config.ModelProfilesConfig,
        vs vault.VaultStore,
        log logger.Logger,
    ) *BifrostAccount

    // GetConfiguredProviders returns the list of configured provider keys.
    func (a *BifrostAccount) GetConfiguredProviders() ([]schemas.ModelProvider, error)

    // GetConfigForProvider returns the Bifrost provider config for a given provider.
    func (a *BifrostAccount) GetConfigForProvider(provider schemas.ModelProvider) (*schemas.ProviderConfig, error)
    ```
*   **Logic**:
    - `GetConfiguredProviders()`: `profiles.Providers` の key 一覧を `schemas.ModelProvider` に変換
    - `GetConfigForProvider()`: プロバイダの keys を Bifrost の `schemas.KeyConfig` に変換。`vault://` プレフィックス付きの値は VaultStore で解決してから設定
    - プロバイダ名の HAG -> Bifrost マッピング:
      - `"anthropic"` -> `schemas.Anthropic`
      - `"openai"` -> `schemas.OpenAI`
      - `"ollama"` -> `schemas.Ollama`
      - 他のプロバイダは Bifrost SDK がサポートする名前にそのまま変換

---

#### [NEW] [bifrost_driver.go](file://shared/libs/go/llmgateway/bifrost_driver.go)
*   **Description**: Bifrost SDK をラップする `LLMGatewayBackend` 実装
*   **Technical Design**:
    ```go
    // BifrostDriver implements LLMGatewayBackend using Bifrost SDK.
    type BifrostDriver struct {
        cfg      *config.AppConfig
        profiles *config.ModelProfilesConfig
        vault    vault.VaultStore
        logger   logger.Logger
        bifrost  *bifrost.Bifrost // Bifrost SDK instance
        proxy    *ProxyServer    // HTTP frontend
        router   *ModelRouter    // model routing
    }

    // NewBifrostDriver creates a BifrostDriver.
    func NewBifrostDriver(
        cfg *config.AppConfig,
        vs vault.VaultStore,
        log logger.Logger,
    ) (*BifrostDriver, error)
    ```
*   **Logic**:
    1. `NewBifrostDriver`:
       - model_profiles.yaml をロード
       - `NewModelRouter` でルーターを生成
       - `NewBifrostAccount` でアカウントアダプタを生成
       - `bifrost.Init()` で Bifrost SDK を初期化
       - `NewProxyServer` で HTTP フロントエンドを生成。ProxyServer に BifrostDriver への参照を設定
    2. `Launch(ctx)`:
       - `proxy.Launch(ctx)` で HTTP サーバを起動
    3. `Shutdown(ctx)`:
       - `proxy.Shutdown(ctx)` で HTTP サーバを停止
    4. `ListModels()`: `proxy.ListModels()` に委譲
    5. `Health()`: `proxy.Health()` に委譲 (将来は Bifrost SDK の状態も反映)
    6. `ProxyURL()`: `proxy.ProxyURL()` に委譲

---

#### [MODIFY] [proxy.go](file://shared/libs/go/llmgateway/proxy.go)
*   **Description**: ProxyServer に BifrostDriver への逆参照を追加し、ハンドラからルーティングと Bifrost SDK を利用可能にする
*   **Technical Design**:
    ```go
    type ProxyServer struct {
        // ... existing fields ...
        driver *BifrostDriver // back-reference for handlers (nil when standalone)
    }

    // SetDriver sets the BifrostDriver back-reference.
    // Called by BifrostDriver.NewBifrostDriver() after proxy creation.
    func (p *ProxyServer) SetDriver(d *BifrostDriver)
    ```
*   **Logic**: ハンドラ関数 (handleAnthropicMessages, handleOpenAIChatCompletions) は `p.driver` が nil でなければ BifrostDriver 経由で処理し、nil の場合は既存のスタブ応答を返す

---

#### [NEW] [proxy_anthropic.go](file://shared/libs/go/llmgateway/proxy_anthropic.go)
*   **Description**: Anthropic Messages API (`POST /v1/messages`) の実ハンドラ
*   **Technical Design**:
    ```go
    // handleAnthropicMessages handles POST /v1/messages.
    // Reads the request body, resolves the model, and forwards to Bifrost SDK.
    // Supports both non-streaming and SSE streaming responses.
    func (p *ProxyServer) handleAnthropicMessages(w http.ResponseWriter, r *http.Request)
    ```
*   **Logic**:
    1. リクエストボディを読み取り、JSON パース (model フィールドを抽出)
    2. `p.driver.router.ResolveModel(model)` でプロバイダを特定
    3. 解決できない場合は `WriteErrorResponse(w, ErrModelNotFound)` を返す
    4. `stream` フィールドを確認:
       - **非ストリーミング**: Bifrost SDK の `ChatCompletionRequest` を呼び出し、レスポンスを Anthropic 形式に変換して返す
       - **SSE ストリーミング**: Bifrost SDK の `ChatCompletionStreamRequest` を呼び出し、チャンクを SSE 形式で逐次書き出す。`Content-Type: text/event-stream` ヘッダを設定。Flusher を使用
    5. Bifrost SDK エラーは `WriteErrorResponse` で JSON エラーとして返す
    6. API キーのログ出力は `MaskSecret()` でマスク

---

#### [NEW] [proxy_openai.go](file://shared/libs/go/llmgateway/proxy_openai.go)
*   **Description**: OpenAI Chat Completions API (`POST /v1/chat/completions`) の実ハンドラ
*   **Technical Design**:
    ```go
    // handleOpenAIChatCompletions handles POST /v1/chat/completions.
    // Non-streaming only. stream:true returns 501.
    func (p *ProxyServer) handleOpenAIChatCompletions(w http.ResponseWriter, r *http.Request)
    ```
*   **Logic**:
    1. リクエストボディを読み取り、JSON パース (model, stream フィールドを抽出)
    2. `stream: true` の場合は 501 Not Implemented を返す (TODO コメント付き)
    3. `p.driver.router.ResolveModel(model)` でプロバイダを特定
    4. 解決できない場合は `WriteErrorResponse(w, ErrModelNotFound)` を返す
    5. Bifrost SDK の `ChatCompletionRequest` を呼び出し、レスポンスを OpenAI 形式で返す
    6. Bifrost SDK エラーは `WriteErrorResponse` で JSON エラーとして返す

---

### hag パッケージ

#### [MODIFY] [server.go](file://shared/libs/go/hag/server.go)
*   **Description**: `resolveDefaults` で BifrostDriver を既定の Gateway にする
*   **Technical Design**:
    ```go
    // resolveDefaults: gateway が nil の場合
    // Before: gateway = NewProxyServer(cfg, vs, log)
    // After:  gateway = NewBifrostDriver(cfg, vs, log)
    // Fallback: model_profiles.yaml がない場合は引き続き NewProxyServer
    ```
*   **Logic**:
    1. `cfg.LLMGateway.ModelProfilesPath` が設定されている場合は `NewBifrostDriver` を使用
    2. 設定されていない (空文字列) 場合は `NewProxyServer` にフォールバック (テスト用途)
    3. `NewBifrostDriver` が失敗した場合は `NewProxyServer` にフォールバックしログ出力

---

## Step-by-Step Implementation Guide

1. **Step 1: Bifrost SDK 依存の追加**
    - [x] `shared/libs/go/go.mod` に `github.com/maximhq/bifrost` を追加
    - [x] `go mod tidy` で依存関係を解決
    - [x] `git add && git commit -m "deps: add bifrost SDK dependency"`

2. **Step 2: MaskSecret ユーティリティ (TDD)**
    - [x] `masking_test.go` を作成 (テーブル駆動テスト)
    - [x] テスト実行 -> 失敗確認
    - [x] `masking.go` を実装
    - [x] テスト実行 -> 全PASS確認
    - [x] `git add && git commit -m "feat: add MaskSecret utility for API key masking"`

3. **Step 3: ModelRouter (TDD)**
    - [x] `routing_test.go` を作成 (テーブル駆動テスト)
    - [x] テスト実行 -> 失敗確認
    - [x] `routing.go` を実装
    - [x] テスト実行 -> 全PASS確認
    - [x] `git add && git commit -m "feat: add ModelRouter for model_profiles.yaml routing"`

4. **Step 4: BifrostAccount アダプタ (TDD)**
    - [/] `bifrost_account_test.go` を作成
    - [ ] テスト実行 -> 失敗確認
    - [ ] `bifrost_account.go` を実装
    - [ ] テスト実行 -> 全PASS確認
    - [ ] `git add && git commit -m "feat: add BifrostAccount adapter for profiles-to-SDK conversion"`

5. **Step 5: BifrostDriver (TDD)**
    - [x] `bifrost_driver_test.go` を作成 (New, ListModels, Health, ProxyURL)
    - [x] テスト実行 -> 失敗確認
    - [x] `bifrost_driver.go` を実装
    - [x] ProxyServer に `SetDriver()` メソッドと `driver` フィールドを追加
    - [x] テスト実行 -> 全PASS確認
    - [x] `git add && git commit -m "feat: add BifrostDriver LLMGatewayBackend implementation"`

6. **Step 6: Anthropic ハンドラ (TDD)**
    - [x] `proxy_anthropic_test.go` を作成 (正常系: non-stream, stream。異常系: 未定義モデル)
    - [x] テスト実行 -> 失敗確認
    - [x] `proxy_anthropic.go` のスタブハンドラを実装に置換
    - [x] `proxy.go` から旧スタブを削除
    - [x] テスト実行 -> 全PASS確認
    - [x] `git add && git commit -m "feat: implement Anthropic Messages API handler with SSE"`

7. **Step 7: OpenAI ハンドラ (TDD)**
    - [x] `proxy_openai_test.go` を作成 (正常系: non-stream。異常系: stream:true, 未定義モデル)
    - [x] テスト実行 -> 失敗確認
    - [x] `proxy_openai.go` のスタブハンドラを実装に置換
    - [x] テスト実行 -> 全PASS確認
    - [x] `git add && git commit -m "feat: implement OpenAI Chat Completions handler"`

8. **Step 8: hag.Server のデフォルト Gateway 更新**
    - [x] `server.go` の `resolveDefaults` を更新
    - [x] 既存テストが引き続き PASS することを確認
    - [x] `git add && git commit -m "feat: use BifrostDriver as default gateway when profiles configured"`

9. **Step 9: ビルド検証**
    - [x] `./scripts/process/build.sh` で全テスト通過を確認
    - [ ] `git push`

10. **Step 10: 統合テスト (実プロバイダ接続)** -- Part 3.5 完了後に実施
    - Part 3.5 (KeyringVaultBackend + vault-cli) で OS Keyring に API キーを登録してから実施する
    - [ ] `vault-cli set --provider anthropic --stdin` で API キーを OS Keyring に登録
    - [ ] `./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories "llm"`
    - [ ] 総合判定プロセス (testing-rules.md 12) を実施

---

## Verification Plan

### Automated Verification

1. **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```

2. **Integration Tests** (実 LLM プロバイダとの接続確認):
    > Part 3.5 (KeyringVaultBackend + vault-cli) 完了後に実施。
    > OS Keyring に API キーが登録済みであること。
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories "llm"
    ```

3. **個別パッケージテスト** (開発中フィードバック用):
    注: 正式検証は必ず `build.sh` を使用すること。
    ```bash
    cd shared/libs/go && go test ./llmgateway/... -v -race -count=1
    ```

### テスト項目のセルフレビュー (11.4)

1. **網羅性**: MaskSecret, ModelRouter, BifrostAccount, BifrostDriver, Anthropic handler (non-stream/SSE), OpenAI handler (non-stream/stream 拒否) の全主要機能をカバー。エラーケース (未定義モデル、nil profiles、Bifrost SDK エラー) も検証
2. **証拠の十分性**: 各テストは具体的な値 (マスク文字列、RoutedModel のフィールド値、HTTP レスポンスの JSON body) を検証。「エラーが出ない」だけでなく「正しい値が返る」を確認
3. **迂回排除**: ModelRouter テストは実際の profiles config を使い、BifrostAccount テストは変換結果の Bifrost 形式を検証。ハンドラテストは HTTP リクエスト/レスポンスの全ボディを検証
4. **依存関係**: masking (末端) -> routing -> bifrost_account -> bifrost_driver -> handlers -> hag.Server のボトムアップ順

### 総合判定プロセス (12)

全テスト完了後、testing-rules.md 12 に従い総合判定を実施する。

---

## Documentation

#### [MODIFY] [design_decisions.md](file://prompts/designs/hag/design_decisions.md)
*   **更新内容**: DD-002 (BifrostDriver 実装完了)、DD-003 (ライフサイクルの BifrostDriver 統合) を更新

---

## 継続計画について

本計画は Part3 (BifrostDriver + Routing + Handlers) です。以下の Part が続きます:

- **Part3.5**: KeyringVaultBackend + vault-cli (OS Keyring ベースの VaultStore 実装と CLI ツール)。Part3 の Step 10 (統合テスト) はこの完了後に実施する
- **Part4**: PassthroughDriver + テキスト→ToolCall 変換 + サブセッションフォールバック + Hierarchical Agent Log + Examples (standalone example, Docker)
