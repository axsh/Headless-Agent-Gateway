# 000-Reasoning-Model-Configuration

> **Source Specification**: `prompts/phases/001-phase02/branches/feat-reasoning/ideas/000-Reasoning-Model-Configuration.md`

## Goal Description

OpenAI Responses API において推論パラメータが必須化され `effort: "none"` が非サポートとなった `gpt-6-astra` を受け、以下の改修を行う：
1. `model_profiles.yaml` の各モデル設定に reasoning の必須フラグ（`required`）、選択可能 effort（`supported_efforts`）、デフォルト effort（`default_effort`）を定義可能にする。
2. `ModelProfilesConfig.Validate()` による設定読み込み時の厳格な整合性検証を提供する。
3. ルーティング結果（`RoutedModel`）およびモデル情報取得 API（`GET /v1/models`, `GET /api/v1/models`, `client/v1`）へ reasoning メタデータを露出する。
4. LLM Gateway（`POST /v1/responses`）において、クライアントが effort を省略した際の既定値自動補完と、不正な effort（`gpt-6-astra` への `none` 等）に対する早期 HTTP 400 バリデーションを実装する。
5. Bifrost が `gpt-6-astra` を非推論モデルと誤認して `reasoning` パラメータを `nil` 消去してしまう不具合に対応するため、フォーク先リポジトリ（`axsh/bifrost` コミット `e05e7f8ca72a8471c7591063c104f350a94f76f2`）のパッチを `go.mod` の `replace` ディレクティブにより統合・検証する。

## User Review Required

- **Bifrost の `replace` ディレクティブ導入**:
  - `go.mod` および `tests/go.mod` において `replace github.com/maximhq/bifrost/core => github.com/axsh/bifrost/core e05e7f8ca72a8471c7591063c104f350a94f76f2` を適用します。これにより、ローカル・CI環境共にフォーク側の修正コミットを参照してビルド・テストが実行されます。
- **既存モデルへの影響**:
  - `reasoning` 設定を持たない既存モデル（`gpt-4o`, `claude-sonnet-4-6` 等）は従来通りの動作を維持します（完全な後方互換性）。

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| **R1**: モデル設定への Reasoning 設定項目の追加 | Proposed Changes > `shared/libs/go/config/model_profiles.go` |
| **R2**: 設定ファイルの整合性バリデーション (`Validate()`) | Proposed Changes > `shared/libs/go/config/model_profiles.go`, `model_profiles_test.go` |
| **R3**: ルーティングレイヤへのメタデータ伝達 (`RoutedModel`) | Proposed Changes > `shared/libs/go/llmgateway/handlerctx/context.go`, `routing.go`, `routing_test.go` |
| **R4**: モデル情報公開 API（`GET /v1/models`, `GET /api/v1/models`）への露出 | Proposed Changes > `backend.go`, `proxy.go`, `proxy_test.go`, `client/v1/models.go` |
| **R5**: Bifrost パッチ適用と統合検証 | Proposed Changes > `go.mod`, `tests/go.mod`, `tests/testdata/model_profiles.yaml`, `tests/llm_gateway_reasoning_test.go` |
| **R6**: LLM Gateway（`POST /v1/responses`）での早期バリデーションと既定値補完 | Proposed Changes > `shared/libs/go/llmgateway/openai/handler.go`, `responses_reasoning_test.go` |

---

## Proposed Changes

### Configuration Layer (`shared/libs/go/config`)

#### [MODIFY] [model_profiles_test.go](file://shared/libs/go/config/model_profiles_test.go)
*   **Description**: `ModelReasoning` の YAML アンマーシャルおよび `ModelProfilesConfig.Validate()` の推論設定バリデーションテストを追加する（TDD Red 先行実装）。
*   **Technical Design**:
    - `TestModelBehavior_Reasoning_YAMLParse`: `required`, `supported_efforts`, `default_effort` のパース、および省略時のデフォルト動作を検証。
    - `TestModelProfilesConfig_Validate_Reasoning`:
      - 正常系: 有効な effort 設定、`reasoning` 未指定モデルの混在。
      - 異常系 1 (未知の effort): `supported_efforts` または `default_effort` に `"super-fast"` などの未知文字列。
      - 異常系 2 (required かつ空): `required: true` なのに `supported_efforts` が空。
      - 異常系 3 (required かつ none 含有): `required: true` なのに `supported_efforts` に `"none"` が含まれる。
      - 異常系 4 (default_effort 不整合): `default_effort: "high"` が `supported_efforts: ["low", "medium"]` に含まれない。
      - 異常系 5 (required かつ default 欠落): `required: true` なのに `default_effort` が空。
      - 異常系 6 (重複 effort): `supported_efforts: ["low", "medium", "low"]` の重複検出。
*   **Logic**: 仕様書 R2 の全検証マトリクスをテーブル駆動テスト（`tests := []struct{...}`）で実装。

#### [MODIFY] [model_profiles.go](file://shared/libs/go/config/model_profiles.go)
*   **Description**: `ModelReasoning` 構造体の新設、`ModelBehavior` への追加、Reasoning effort 定数・セットの定義、`Validate()` での検証ロジックを追加する。
*   **Technical Design**:
    ```go
    // Supported OpenAI reasoning effort levels.
    const (
        ReasoningEffortNone    = "none"
        ReasoningEffortMinimal = "minimal"
        ReasoningEffortLow     = "low"
        ReasoningEffortMedium  = "medium"
        ReasoningEffortHigh    = "high"
        ReasoningEffortXHigh   = "xhigh"
        ReasoningEffortMax     = "max"
    )

    // ValidReasoningEfforts is the set of all recognized reasoning effort levels.
    var ValidReasoningEfforts = map[string]struct{}{
        ReasoningEffortNone:    {},
        ReasoningEffortMinimal: {},
        ReasoningEffortLow:     {},
        ReasoningEffortMedium:  {},
        ReasoningEffortHigh:    {},
        ReasoningEffortXHigh:   {},
        ReasoningEffortMax:     {},
    }

    // ModelReasoning defines reasoning capability constraints and defaults for a model.
    type ModelReasoning struct {
        Required         bool     `yaml:"required" json:"required"`
        SupportedEfforts []string `yaml:"supported_efforts,omitempty" json:"supported_efforts,omitempty"`
        DefaultEffort    string   `yaml:"default_effort,omitempty" json:"default_effort,omitempty"`
    }

    type ModelBehavior struct {
        ToolCallFallback bool            `yaml:"tool_call_fallback"`
        StructuredOutput bool            `yaml:"structured_output"`
        MaxOutputTokens  int             `yaml:"max_output_tokens,omitempty"`
        Reasoning        *ModelReasoning `yaml:"reasoning,omitempty"` // Added
    }
    ```
*   **Logic**:
    - `Validate()` 内のモデルループにおいて、`model.Behavior != nil && model.Behavior.Reasoning != nil` の場合、ヘルパー関数 `validateReasoningConfig(provName, key.Name, model.Name, model.Behavior.Reasoning)` を呼び出す。
    - バリデーション詳細ロジック：
      1. `supported_efforts` の各要素が `ValidReasoningEfforts` に存在するか検証。未知値ならエラー。
      2. 重複チェック用 `seen := make(map[string]bool)` で重複を検出したらエラー。
      3. `r.Required` が true の場合：
         - `len(r.SupportedEfforts) == 0` ならエラー (`reasoning.required is true but supported_efforts is empty`)。
         - `seen[ReasoningEffortNone]` が true ならエラー (`reasoning.required is true but supported_efforts contains "none"`)。
         - `r.DefaultEffort == ""` ならエラー (`reasoning.required is true but default_effort is empty`)。
      4. `r.DefaultEffort != ""` の場合：
         - `ValidReasoningEfforts` に存在するか検証。
         - `seen[r.DefaultEffort]` が false（`supported_efforts` に含まれない）ならエラー (`default_effort %q is not in supported_efforts`)。

---

### Routing & Context Layer (`shared/libs/go/llmgateway`)

#### [MODIFY] [routing_test.go](file://shared/libs/go/llmgateway/routing_test.go)
*   **Description**: `ModelRouter.ResolveModel` が `Behavior.Reasoning` のメタデータを正確に `RoutedModel` にコピー・保持することを検証する。
*   **Technical Design**:
    - `TestModelRouter_ResolveModel_Reasoning`:
      - `ModelConfig` に `Reasoning: &config.ModelReasoning{Required: true, SupportedEfforts: []string{"low", "medium"}, DefaultEffort: "medium"}` を設定。
      - `ResolveModel` を呼び出し、返却された `RoutedModel.Reasoning` が一致することをアサート。
      - 2回目の解決時（セッションキャッシュ経由）でも同じ `Reasoning` メタデータが保持されていることを確認。

#### [MODIFY] [handlerctx/context.go](file://shared/libs/go/llmgateway/handlerctx/context.go)
*   **Description**: `RoutedModel` 構造体に `Reasoning *config.ModelReasoning` フィールドを追加する。
*   **Technical Design**:
    ```go
    type RoutedModel struct {
        Provider         string                 `json:"provider"`
        KeyName          string                 `json:"key_name,omitempty"`
        KeyValue         string                 `json:"-"`
        Model            string                 `json:"model"`
        Mode             string                 `json:"mode,omitempty"`
        ToolCallFallback bool                   `json:"tool_call_fallback"`
        MaxOutputTokens  int                    `json:"max_output_tokens,omitempty"`
        Reasoning        *config.ModelReasoning `json:"reasoning,omitempty"` // Added
    }
    ```

#### [MODIFY] [routing.go](file://shared/libs/go/llmgateway/routing.go)
*   **Description**: `ModelRouter.ResolveModel` で `model.Behavior.Reasoning` を `RoutedModel` にコピーする。
*   **Technical Design**:
    ```go
    var fallback bool
    var maxOutputTokens int
    var reasoning *config.ModelReasoning
    if model.Behavior != nil {
        fallback = model.Behavior.ToolCallFallback
        maxOutputTokens = model.Behavior.MaxOutputTokens
        reasoning = model.Behavior.Reasoning
    }
    resolved = &RoutedModel{
        Provider:         providerName,
        KeyName:          key.Name,
        KeyValue:         key.Secret,
        Model:            modelName,
        Mode:             model.Mode,
        ToolCallFallback: fallback,
        MaxOutputTokens:  maxOutputTokens,
        Reasoning:        reasoning,
    }
    ```

---

### Model Info & API Layer (`shared/libs/go/llmgateway`, `client/v1`)

#### [MODIFY] [proxy_test.go](file://shared/libs/go/llmgateway/proxy_test.go)
*   **Description**: `GET /v1/models` で `reasoning` 設定を持つモデルが正しくシリアライズされ、未指定モデルではキーが省略されることを検証する。
*   **Technical Design**:
    - `TestProxyServer_ListModels_Reasoning`:
      - `model_profiles.yaml` に `gpt-6-astra`（reasoning 設定あり）と `gpt-4o`（reasoning 設定なし）を登録。
      - `p.ListModels()` および `GET /v1/models` レスポンスを検証。
      - `gpt-6-astra` の要素に `reasoning.required == true`, `supported_efforts`, `default_effort == "medium"` が存在することを確認。
      - `gpt-4o` の要素には `reasoning` フィールドが存在しない（nil/omitempty）ことを確認。

#### [MODIFY] [backend.go](file://shared/libs/go/llmgateway/backend.go)
*   **Description**: `ModelInfo` 構造体に `Reasoning *config.ModelReasoning` フィールドを追加する。
*   **Technical Design**:
    ```go
    type ModelInfo struct {
        Provider         string                 `json:"provider"`
        Model            string                 `json:"model"`
        ToolCallFallback bool                   `json:"tool_call_fallback,omitempty"`
        Reasoning        *config.ModelReasoning `json:"reasoning,omitempty"` // Added
    }
    ```

#### [MODIFY] [proxy.go](file://shared/libs/go/llmgateway/proxy.go)
*   **Description**: `ListModels()` および `DefaultModel()` において、`ModelInfo` 構築時に `model.Behavior.Reasoning` を設定する。
*   **Technical Design**:
    - `ListModels()` 内で `info := ModelInfo{ Provider: providerName, Model: model.Name }` に加え、`if model.Behavior != nil { info.Reasoning = model.Behavior.Reasoning }` を追加。
    - `DefaultModel()` 内でも同様に `if m.Behavior != nil { info.Reasoning = m.Behavior.Reasoning }` を設定。

#### [MODIFY] [client/v1/models.go](file://client/v1/models.go)
*   **Description**: クライアント SDK の `ModelInfo` に `Reasoning *ModelReasoning` を追加し、API 経由で取得できるようにする。
*   **Technical Design**:
    ```go
    // ModelReasoning describes reasoning constraints and defaults for a model.
    type ModelReasoning struct {
        Required         bool     `json:"required"`
        SupportedEfforts []string `json:"supported_efforts,omitempty"`
        DefaultEffort    string   `json:"default_effort,omitempty"`
    }

    type ModelInfo struct {
        Provider  string          `json:"provider"`
        Model     string          `json:"model"`
        Reasoning *ModelReasoning `json:"reasoning,omitempty"` // Added
    }
    ```

---

### Gateway Request Validation & Backfill (`shared/libs/go/llmgateway/openai`)

#### [NEW] [responses_reasoning_test.go](file://shared/libs/go/llmgateway/openai/responses_reasoning_test.go)
*   **Description**: `POST /v1/responses` における reasoning パラメータのバリデーションと `default_effort` 自動補完の単体テスト。
*   **Technical Design**:
    - `TestHandleResponses_ReasoningBackfill`:
      - クライアントが `reasoning` パラメータを指定せずに `gpt-6-astra`（`default_effort: "medium"` 設定あり）へリクエストした場合、リクエスト内部で `reasoning.effort` が `"medium"` に補完されること。
    - `TestHandleResponses_ReasoningMissingRequired`:
      - `required: true` かつ `default_effort` が未設定のモデルに対し effort 未指定でリクエストした場合、HTTP 400（`code: "missing_reasoning_effort"`）が返却されること。
    - `TestHandleResponses_ReasoningUnsupportedEffort`:
      - `gpt-6-astra`（`supported_efforts: ["low", "medium", "high", "xhigh", "max"]`）に対し `reasoning: {"effort": "none"}` を送信した場合、Upstream に行かず Gateway 段階で HTTP 400（`code: "unsupported_reasoning_effort"`）が返却されること。

#### [MODIFY] [handler.go](file://shared/libs/go/llmgateway/openai/handler.go)
*   **Description**: `handleResponses` 内でモデル解決直後に Reasoning バリデーションおよび既定値補完ロジックを実行する。
*   **Technical Design**:
    ```go
    // Route the model
    routed, err := router.ResolveModel(req.Model, sessionID)
    if err != nil { ... }

    // R6: Validate and backfill reasoning effort based on routed model profile
    if routed.Reasoning != nil {
        var effort string
        if oaiReq.Reasoning != nil && oaiReq.Reasoning.Effort != nil {
            effort = *oaiReq.Reasoning.Effort
        }

        if effort == "" {
            if routed.Reasoning.DefaultEffort != "" {
                if oaiReq.Reasoning == nil {
                    oaiReq.Reasoning = &bifrostSchemas.ResponsesParametersReasoning{}
                }
                oaiReq.Reasoning.Effort = &routed.Reasoning.DefaultEffort
            } else if routed.Reasoning.Required {
                writeGWError(ctx, w, r, modelName, &handlerctx.GatewayError{
                    Type:    "invalid_request_error",
                    Message: fmt.Sprintf("reasoning effort is required for model %q", routed.Model),
                    Code:    "missing_reasoning_effort",
                    Status:  http.StatusBadRequest,
                })
                return
            }
        } else if len(routed.Reasoning.SupportedEfforts) > 0 {
            supported := false
            for _, se := range routed.Reasoning.SupportedEfforts {
                if se == effort {
                    supported = true
                    break
                }
            }
            if !supported {
                writeGWError(ctx, w, r, modelName, &handlerctx.GatewayError{
                    Type:    "invalid_request_error",
                    Message: fmt.Sprintf("unsupported reasoning effort %q for model %q", effort, routed.Model),
                    Code:    "unsupported_reasoning_effort",
                    Status:  http.StatusBadRequest,
                })
                return
            }
        }
    }
    ```

---

### Dependency & Integration (`go.mod`, `tests/`)

#### [MODIFY] [go.mod](file://go.mod) & [tests/go.mod](file://tests/go.mod)
*   **Description**: `axsh/bifrost` の `fix/gpt-6-astra-reasoning-support` コミット（`e05e7f8ca72a8471c7591063c104f350a94f76f2`）を `replace` ディレクティブに追加する。
*   **Technical Design**:
    ```text
    replace github.com/maximhq/bifrost/core => github.com/axsh/bifrost/core e05e7f8ca72a8471c7591063c104f350a94f76f2
    ```

#### [MODIFY] [tests/testdata/model_profiles.yaml](file://tests/testdata/model_profiles.yaml)
*   **Description**: テスト用プロファイルに `gpt-6-astra` モデル定義を追加する。
*   **Technical Design**:
    ```yaml
    providers:
      openai:
        api_keys:
          - name: default
            secret: "vault://providers/openai/default"
            models:
              - name: gpt-6-astra
                mode: responses
                behavior:
                  reasoning:
                    required: true
                    supported_efforts:
                      - low
                      - medium
                      - high
                      - xhigh
                      - max
                    default_effort: medium
    ```

#### [NEW] [tests/llm_gateway_reasoning_test.go](file://tests/llm_gateway_reasoning_test.go)
*   **Description**: LLM Gateway および AgentService を通じた `gpt-6-astra` の Reasoning 統合 E2E テスト。
*   **Technical Design**:
    - `TestLLMGateway_GPT6Astra_ModelsAPI`:
      - Gateway を起動し、`GET /v1/models` で `gpt-6-astra` の `reasoning` メタデータが取得できることを検証。
      - AgentService を起動し、`GET /api/v1/models` で同様に中継取得できることを検証。
    - `TestLLMGateway_GPT6Astra_ResponsesWire_PreservesReasoning`:
      - パッチ適用済み Bifrost とローカル HTTP テストサーバー（モック Upstream）を連携させ、`POST /v1/responses` に `model: "gpt-6-astra"`, `reasoning: {"effort": "low"}` を送信。
      - モック Upstream が受信した HTTP ペイロードにおいて、`reasoning.effort` が `"low"` として保持されていること、および `effort: "max"` がダウングレードされずに到達することを検証。

---

## Step-by-Step Implementation Guide

- [x] **Step 1: Bifrost replace 設定**:
    - `go.mod` および `tests/go.mod` に `replace github.com/maximhq/bifrost/core => github.com/axsh/bifrost/core e05e7f8ca72a8471c7591063c104f350a94f76f2` を追記。
    - `go mod tidy` を実行し、依存関係を解決。
    - コミット: `chore(deps): replace bifrost/core with axsh/bifrost commit e05e7f8`

- [x] **Step 2: Config Layer テスト作成 (TDD Red)**:
    - `shared/libs/go/config/model_profiles_test.go` に `TestModelBehavior_Reasoning_YAMLParse` および `TestModelProfilesConfig_Validate_Reasoning` を追加。
    - コミット: `test(config): add unit tests for model reasoning config and validation`

- [x] **Step 3: Config Layer 実装 (TDD Green)**:
    - `shared/libs/go/config/model_profiles.go` に `ModelReasoning`, `ModelBehavior.Reasoning`, `ReasoningEffort*` 定数、`validateReasoningConfig` を実装。
    - 単体テストを実行して Green を確認。
    - コミット: `feat(config): add ModelReasoning to ModelBehavior and validate reasoning rules`

- [x] **Step 4: Routing & HandlerContext レイヤ更新**:
    - `shared/libs/go/llmgateway/handlerctx/context.go` の `RoutedModel` に `Reasoning` フィールドを追加。
    - `shared/libs/go/llmgateway/routing_test.go` に `TestModelRouter_ResolveModel_Reasoning` を追加。
    - `shared/libs/go/llmgateway/routing.go` で `ResolveModel` コピーロジックを実装。
    - コミット: `feat(llmgateway): propagate ModelReasoning to RoutedModel during routing`

- [x] **Step 5: Model Info & Discovery API 更新**:
    - `shared/libs/go/llmgateway/backend.go` の `ModelInfo` に `Reasoning` を追加。
    - `client/v1/models.go` の `ModelInfo` に `Reasoning` を追加。
    - `shared/libs/go/llmgateway/proxy_test.go` に `TestProxyServer_ListModels_Reasoning` を追加。
    - `shared/libs/go/llmgateway/proxy.go` の `ListModels` / `DefaultModel` で `Reasoning` を設定。
    - コミット: `feat(llmgateway): expose model reasoning metadata in ListModels and models API`

- [x] **Step 6: Gateway Early Validation & Backfill 実装**:
    - `shared/libs/go/llmgateway/openai/responses_reasoning_test.go` を新規作成（Red）。
    - `shared/libs/go/llmgateway/openai/handler.go` で `handleResponses` にバリデーション & 補完ロジックを実装（Green）。
    - コミット: `feat(llmgateway): implement reasoning effort backfill and early validation on /v1/responses`

- [x] **Step 7: 統合テスト用データおよび E2E テスト追加**:
    - `tests/testdata/model_profiles.yaml` に `gpt-6-astra` を追加。
    - `tests/llm_gateway_reasoning_test.go` を新規作成し、API 露出および Upstream 電文の推論パラメータ透過性を検証。
    - コミット: `test(e2e): add end-to-end integration tests for gpt-6-astra reasoning support`

- [/] **Step 8: 全体验証パイプライン実行**:
    - `./scripts/process/build.sh --skip-frontend --skip-etc`
    - `./scripts/process/integration_test.sh --specify "TestLLMGateway_GPT6Astra"`
    - 全テスト通過を確認。

---

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh --skip-frontend --skip-etc
    ```
    - `config`, `llmgateway`, `openai`, `client/v1` の単体テストがすべて合格することを確認。

2.  **Integration Tests**:
    ```bash
    ./scripts/process/integration_test.sh --specify "TestLLMGateway_GPT6Astra"
    ```
    - `GET /v1/models` および `GET /api/v1/models` での `reasoning` メタデータ取得。
    - モック Upstream への `POST /v1/responses` 電文で `reasoning.effort` が `low`, `max` ともに欠落せず届くことの検証。
    - 不正な effort（`none`）で Gateway が早期に HTTP 400 を返すことの検証。

3.  **Regression Check**:
    ```bash
    ./scripts/process/integration_test.sh --specify "TestLLMGateway"
    ```
    - 既存のモデルルーティングや既存機能にリグレッションが発生していないことを確認。

---

## Documentation

- `prompts/phases/001-phase02/branches/feat-reasoning/ideas/000-Reasoning-Model-Configuration.md`: 実装完了後に進捗と突合。
- `settings/example/model_profiles.yaml`: `gpt-6-astra` の設定例コメントを追加。
