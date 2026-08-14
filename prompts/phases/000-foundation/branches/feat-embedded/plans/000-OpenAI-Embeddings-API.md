# 000-OpenAI-Embeddings-API

> **Source Specification**: prompts/phases/000-foundation/branches/feat-embedded/ideas/000-OpenAI-Embeddings-API.md

## Goal Description

OpenAI 互換 Embeddings API を Tern に追加する。

- LLMGP に `POST /v1/embeddings` を追加し、Bifrost `EmbeddingRequest` 経由で OpenAI / Ollama / Google へ転送する
- AgentService に Coding Agent をバイパスする `POST /api/v1/embeddings` と `GET /api/v1/embeddings/models` を追加する
- `client/v1` に `CreateEmbedding` / `ListEmbeddingModels` を追加する
- `model_profiles.yaml` の `mode: embedding` で chat モデルと分離する
- `examples/embeddings-client` を追加する

## User Review Required

1. **mode 不一致は 400 に固定**: Embeddings エンドポイントに `mode` が `embedding` でないモデルを指定した場合、上流へ送らず `400` (`code: "invalid_model_mode"`) を返す。
2. **ハンドラ配置**: OpenAI 互換面のため `shared/libs/go/llmgateway/openai/embeddings.go` に `POST /v1/embeddings` を登録する（マルチプロバイダでも外面は OpenAI JSON）。
3. **一覧の dual source**:
   - エージェント向け `GET /api/v1/models` / LLMGP `GET /v1/models` は embedding モデルを **除外**
   - Embeddings 向け `GET /api/v1/embeddings/models` は AgentService が `profiles` から `mode: embedding` のみを返す（LLMGP に同名一覧は必須としない）

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: LLMGP `POST /v1/embeddings` | Proposed Changes > llmgateway/openai/embeddings.go |
| R2: Client `CreateEmbedding` | Proposed Changes > client/v1/embeddings.go |
| R3: Agent バイパスで LLMGP 転送 | Proposed Changes > agentservice/embeddings.go |
| R4: `mode: embedding` + ListModels 除外 | Proposed Changes > config/model_profiles.go, llmgateway/proxy.go |
| R5: エラーハンドリング | Proposed Changes > openai/embeddings.go, agentservice/embeddings.go |
| R6: OpenAI / Ollama / Google | Proposed Changes > openai/embeddings.go（Bifrost ルーティング）+ settings example |
| R7: Embeddings モデル一覧 API | Proposed Changes > agentservice/embeddings.go, client/v1/embeddings.go |
| R8: examples | Proposed Changes > examples/embeddings-client |
| O3: 入力サイズ制限（任意） | 第1弾は既存 `MaxRequestBodyBytes` のみ適用。専用ガードは実装しない |

## Proposed Changes

### Config: mode embedding とフィルタ

#### [NEW] shared/libs/go/config/model_mode_test.go
*   **Description**: `mode` 定数とヘルパのテーブル駆動テスト（先に作成）
*   **Test Cases**:
    *   `TestIsEmbeddingMode`: `""` / `"chat"` / `"responses"` → false、`"embedding"` → true（大小無視しない。完全一致 `"embedding"` のみ）
    *   `TestListModelsByMode_EmbeddingOnly`: providers に chat + embedding を混ぜ、embedding のみ返す
    *   `TestListModelsByMode_ExcludesEmbedding`: chat/responses のみ返す（embedding 除外）

#### [MODIFY] [shared/libs/go/config/model_profiles.go](file://shared/libs/go/config/model_profiles.go)
*   **Description**: embedding mode 定数と一覧ヘルパを追加する
*   **Technical Design**:

```go
const (
    ModelModeChat      = "chat"      // default when Mode == ""
    ModelModeResponses = "responses"
    ModelModeEmbedding = "embedding"
)

// EffectiveMode returns the model mode; empty Mode is treated as chat.
func EffectiveMode(mode string) string {
    if mode == "" {
        return ModelModeChat
    }
    return mode
}

// IsEmbeddingMode reports whether mode is embedding.
func IsEmbeddingMode(mode string) bool {
    return mode == ModelModeEmbedding
}

// ModelRef is a provider/model pair used by list helpers.
type ModelRef struct {
    Provider string
    Model    string
    Mode     string
}

// ListModelRefs returns models matching wantMode.
// wantMode == ModelModeEmbedding: only embedding
// wantMode == "" (agent list): all non-embedding (chat/responses/empty)
func (c *ModelProfilesConfig) ListModelRefs(wantMode string) []ModelRef
```

*   **Logic**:
    *   `ModelConfig.Mode` コメントを `"chat" (default), "responses", or "embedding"` に更新
    *   `Validate()` で未知 mode（上記以外）をエラーにする
    *   `ListModelRefs(ModelModeEmbedding)` は R7 用
    *   `ListModelRefs("")` はエージェント向け一覧用（embedding 除外）

---

### LLMGP: Embeddings エンドポイント

#### [NEW] shared/libs/go/llmgateway/openai/embeddings_test.go
*   **Description**: `/v1/embeddings` ハンドラの単体テスト（httptest + stub Bifrost / stub router）
*   **Test Cases**:
    *   `TestHandleEmbeddings_SingleText`: input string → 200、`data[0].embedding` 非空
    *   `TestHandleEmbeddings_BatchTexts`: input []string 長さ2 → data 長さ2、index 対応
    *   `TestHandleEmbeddings_MissingInput`: 400 `invalid_request_error`
    *   `TestHandleEmbeddings_InvalidJSON`: 400
    *   `TestHandleEmbeddings_ModelNotFound`: 404 `model_not_found`
    *   `TestHandleEmbeddings_WrongMode`: routed.Mode が chat → 400 `invalid_model_mode`
    *   `TestHandleEmbeddings_UpstreamError`: Bifrost エラー → 502（または StatusCode 透過）
    *   `TestHandleEmbeddings_Providers`: provider 名 `openai` / `ollama` / `google` それぞれで `ToBifrostProvider` 経由の呼び出しが行われること（テーブル駆動）

#### [NEW] shared/libs/go/llmgateway/openai/embeddings.go
*   **Description**: OpenAI 互換 Embeddings ハンドラ
*   **Technical Design**:

```go
func init() {
    handlerctx.RegisterHandler("POST /v1/embeddings", HandleEmbeddings)
}

// HandleEmbeddings returns http.HandlerFunc for POST /v1/embeddings.
func HandleEmbeddings(ctx handlerctx.HandlerContext) http.HandlerFunc

// openaiEmbeddingRequest is the OpenAI-compatible request body.
type openaiEmbeddingRequest struct {
    Model          string          `json:"model"`
    Input          json.RawMessage `json:"input"` // string or []string
    EncodingFormat *string         `json:"encoding_format,omitempty"`
    Dimensions     *int            `json:"dimensions,omitempty"`
}

// openaiEmbeddingResponse mirrors OpenAI / Bifrost embedding response JSON.
type openaiEmbeddingResponse struct {
    Object string                   `json:"object"` // "list"
    Data   []openaiEmbeddingData    `json:"data"`
    Model  string                   `json:"model"`
    Usage  *openaiEmbeddingUsage    `json:"usage,omitempty"`
}

type openaiEmbeddingData struct {
    Object    string          `json:"object"` // "embedding"
    Embedding json.RawMessage `json:"embedding"`
    Index     int             `json:"index"`
}

type openaiEmbeddingUsage struct {
    PromptTokens int `json:"prompt_tokens"`
    TotalTokens  int `json:"total_tokens"`
}
```

*   **Logic**:
    1. `MaxRequestBodyBytes` を適用して body を読む
    2. JSON パース。`model` 空または `input` 欠落/空/`null` → 400
    3. `input` を string または []string にデコード（それ以外 → 400）
    4. `router.ResolveModel(model, sessionID)`。失敗 → 404
    5. `!config.IsEmbeddingMode(routed.Mode)` → 400 `invalid_model_mode`
    6. Vault 参照を解決（既存 responses ハンドラと同様）
    7. `schemas.BifrostEmbeddingRequest` を組み立て:
       * `Provider = ctx.ToBifrostProvider(routed.Provider)`
       * `Model = routed.Model`
       * `Input`: 単一なら `EmbeddingInput{Text: &s}`、複数なら `EmbeddingInput{Texts: ss}`
       * `Params`: `EncodingFormat` / `Dimensions` を転送
    8. `ctx.BifrostSDK().EmbeddingRequest(bifrostCtx, req)` を呼ぶ
    9. 成功時は Bifrost レスポンスを OpenAI 互換 JSON として 200 で返す（`json.NewEncoder(w).Encode(resp)` で足りる場合はそのまま）
    10. Bifrost エラーは StatusCode があればそれを、なければ 502 で `upstream_error`

#### [MODIFY] [shared/libs/go/llmgateway/proxy.go](file://shared/libs/go/llmgateway/proxy.go)
*   **Description**: エージェント向けモデル一覧から embedding を除外し、index に embeddings を載せる
*   **Technical Design / Logic**:
    *   `ListModels()`: `model.Mode == "embedding"` のエントリをスキップ
    *   `DefaultModel()`: default が embedding なら nil（または非 embedding の default のみ返す）。default が embedding の場合は `nil` を返す
    *   `handleIndex` の endpoints 配列に `"POST /v1/embeddings"` を追加

#### [MODIFY] [shared/libs/go/llmgateway/proxy_test.go](file://shared/libs/go/llmgateway/proxy_test.go)
*   **Description**: ListModels が embedding を除外することを検証するテストを追加
*   **Test Cases**:
    *   profiles に chat + embedding を混ぜ、`ListModels` / `GET /v1/models` に embedding 名が出ないこと

#### [MODIFY] [shared/libs/go/llmgateway/handlerctx/context.go](file://shared/libs/go/llmgateway/handlerctx/context.go)
*   **Description**: `RoutedModel.Mode` コメントに `"embedding"` を追記（型変更なし）

---

### AgentService: バイパス API

#### [NEW] shared/libs/go/agentservice/embeddings_test.go
*   **Description**: Embeddings 経路の単体テスト
*   **Test Cases**:
    *   `TestHandleCreateEmbedding_ProxiesToGateway`: httptest で偽 LLMGP を立て、AgentService が `POST {gatewayURL}/v1/embeddings` に転送し、Coding Agent の `CreateSession` が呼ばれないこと
    *   `TestHandleCreateEmbedding_ForwardsAuthToken`: `X-Gateway-Token` が付与されること（token 設定時）
    *   `TestHandleCreateEmbedding_GatewayUnreachable`: 5xx / クライアントエラー
    *   `TestHandleCreateEmbedding_MissingInput`: 400（Gateway 到達前に検証してもよい。Gateway に委譲する場合は Gateway の 400 を透過）
    *   `TestHandleListEmbeddingModels`: profiles に chat + embedding を入れ、embedding のみ返す
    *   `TestHandleListModels_ExcludesEmbedding`: 既存 `SetGatewayModels` 経路で embedding が混ざらないこと（Fetch 後の想定は LLMGP 除外に依存。直接 Set した場合のテストは「キャッシュをそのまま返す」現状維持でも可。FetchModelsFromGateway の結合は統合テストで担保）

#### [NEW] shared/libs/go/agentservice/embeddings.go
*   **Description**: Agent バイパスの Embeddings HTTP ハンドラ
*   **Technical Design**:

```go
// handleCreateEmbedding handles POST /api/v1/embeddings.
// Proxies the OpenAI-compatible JSON body to LLMGP POST /v1/embeddings.
// Does not create sessions or start Coding Agents.
func (s *Server) handleCreateEmbedding(w http.ResponseWriter, r *http.Request)

// handleListEmbeddingModels handles GET /api/v1/embeddings/models.
func (s *Server) handleListEmbeddingModels(w http.ResponseWriter, r *http.Request)

type embeddingModelsResponse struct {
    Models []llmgateway.ModelInfo `json:"models"`
}
```

*   **Logic (`handleCreateEmbedding`)**:
    1. Method は POST のみ
    2. `s.gatewayURL == ""` → 503 `gateway_not_configured`
    3. body を読み、`http.NewRequestWithContext` で `POST s.gatewayURL+"/v1/embeddings"` を作成
    4. `Content-Type: application/json` をコピー。`s.gatewayToken != ""` なら `X-Gateway-Token` を付与
    5. レスポンス status / body をクライアントへ透過（ストリームではない）
    6. 転送失敗（dial error）→ 502
*   **Logic (`handleListEmbeddingModels`)**:
    1. `s.profiles == nil` → `{"models":[]}`
    2. `s.profiles.ListModelRefs(config.ModelModeEmbedding)` を `[]llmgateway.ModelInfo` に変換して返す

#### [MODIFY] [shared/libs/go/agentservice/service.go](file://shared/libs/go/agentservice/service.go)
*   **Description**: ルート登録
*   **Logic**: `HTTPHandler()` の v1 ブロックに追加:

```go
mux.HandleFunc("/api/v1/embeddings", s.routeEmbeddings)
mux.HandleFunc("/api/v1/embeddings/models", s.routeEmbeddingModels)
```

```go
func (s *Server) routeEmbeddings(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
        return
    }
    s.handleCreateEmbedding(w, r)
}

func (s *Server) routeEmbeddingModels(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodGet {
        http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
        return
    }
    s.handleListEmbeddingModels(w, r)
}
```

---

### Client API

#### [NEW] client/v1/embeddings_test.go
*   **Description**: Client Embeddings API の単体テスト（httptest 偽サーバ）
*   **Test Cases**:
    *   `TestCreateEmbedding_Single`: POST `/api/v1/embeddings`、レスポンスデコード
    *   `TestCreateEmbedding_Batch`: Input が配列でエンコードされること
    *   `TestCreateEmbedding_HTTPError`: 非 2xx で error
    *   `TestListEmbeddingModels`: GET `/api/v1/embeddings/models`

#### [NEW] client/v1/embeddings.go
*   **Description**: 公開 Client API
*   **Technical Design**（仕様書のシグネチャを継承）:

```go
package v1

// EmbeddingRequest is the request for CreateEmbedding.
type EmbeddingRequest struct {
    Model          string   `json:"model"`
    Input          any      `json:"input"` // string or []string
    EncodingFormat string   `json:"encoding_format,omitempty"`
    Dimensions     int      `json:"dimensions,omitempty"`
}

// EmbeddingResponse is the OpenAI-compatible embeddings response.
type EmbeddingResponse struct {
    Object string          `json:"object"`
    Data   []EmbeddingData `json:"data"`
    Model  string          `json:"model"`
    Usage  *EmbeddingUsage `json:"usage,omitempty"`
}

type EmbeddingData struct {
    Object    string    `json:"object"`
    Embedding []float64 `json:"embedding"`
    Index     int       `json:"index"`
}

type EmbeddingUsage struct {
    PromptTokens int `json:"prompt_tokens"`
    TotalTokens  int `json:"total_tokens"`
}

// EmbeddingModelsResponse is the response from ListEmbeddingModels.
type EmbeddingModelsResponse struct {
    Models []ModelInfo `json:"models"`
}

// CreateEmbedding calls POST /api/v1/embeddings (Coding Agent bypass).
func (c *Client) CreateEmbedding(ctx context.Context, req EmbeddingRequest) (*EmbeddingResponse, error)

// ListEmbeddingModels calls GET /api/v1/embeddings/models.
func (c *Client) ListEmbeddingModels(ctx context.Context) (*EmbeddingModelsResponse, error)
```

*   **Logic**:
    *   `CreateEmbedding`: `encoding_format` / `dimensions` はゼロ値なら JSON から省略（`omitempty`）。`Input` が string / []string 以外ならクライアント側で error を返してよい
    *   非 2xx 時は body を含めた `fmt.Errorf`（既存 client パターンに合わせる）
    *   `EmbeddingData.Embedding` は第1弾で `[]float64` を前提とする（`encoding_format=base64` は任意。使う場合は後続で型拡張。第1弾テストは float のみ）

---

### 設定例・ドキュメント・Examples

#### [MODIFY] [settings/example/model_profiles.yaml](file://settings/example/model_profiles.yaml)
*   **Description**: embedding モデル例を追加
*   **Logic**（追記例）:

```yaml
  openai:
    api_keys:
      - name: default
        secret: vault://providers/openai/default
        models:
          - name: gpt-4o-mini
          # ... existing ...
          - name: text-embedding-3-small
            mode: embedding
          - name: text-embedding-3-large
            mode: embedding

  google:
    api_keys:
      - name: default
        secret: vault://providers/google/default
        models:
          - name: gemini-2.5-flash
            # ...
          - name: text-embedding-004
            mode: embedding

  ollama:
    api_keys:
      - name: default
        models:
          - name: qwen2.5-coder:7b
            behavior:
              tool_call_fallback: true
          - name: nomic-embed-text
            mode: embedding
```

#### [MODIFY] [tests/testdata/model_profiles.yaml](file://tests/testdata/model_profiles.yaml)
*   **Description**: 統合テスト用に embedding モデルを追加（OpenAI / 可能なら google/ollama）
*   **Logic**: 既存 chat モデルを壊さず `mode: embedding` エントリを追加

#### [NEW] examples/embeddings-client/main.go
*   **Description**: R8 最小サンプル
*   **Logic**:
    1. `client.New(serverURL)`
    2. `ListEmbeddingModels`
    3. 先頭モデル（または引数）で `CreateEmbedding`（input: `"hello embeddings"`）
    4. embedding 次元数をログ出力
    5. セッション API は呼ばない

#### [NEW] examples/embeddings-client/go.mod
*   **Description**: `minimal-client` と同様に `replace github.com/axsh/arctic-tern => ../../`

#### [NEW] examples/embeddings-client/README.md
*   **Description**: 起動前提（Tern 起動、embedding モデル登録、必要なら API キー）を英語で簡潔に記載

#### [MODIFY] [tests/examples_build_test.go](file://tests/examples_build_test.go)
*   **Description**: `TestExamples_EmbeddingsClient_Builds` を追加

#### [MODIFY] [docs/ReferenceManual-WebAPIs.md](file://docs/ReferenceManual-WebAPIs.md)
*   **Description**: エンドポイント表と詳細に Embeddings API を追加（英語ドキュメント）
*   **Logic**:
    *   `POST /api/v1/embeddings`
    *   `GET /api/v1/embeddings/models`
    *   Coding Agent を経由しない旨を明記

---

### 統合 / E2E テスト

#### [NEW] tests/embeddings_api_test.go
*   **Description**: stub / ローカルサーバによる統合テスト（実 API キー不要を優先）
*   **Test Cases**（`//go:build integration`、package `llm_test`）:
    *   `TestEmbedding_CreateViaClient_StubGateway`: AgentService + 偽 LLMGP（固定ベクトル返却）で `client.CreateEmbedding` が成功し、Agent 未起動
    *   `TestEmbedding_ListModels_SeparatesChatAndEmbedding`: chat は `ListModels`、embedding は `ListEmbeddingModels` のみ
    *   `TestEmbedding_BatchInputs`: 入力2件 → data 2件
    *   `TestEmbedding_InvalidModelMode`: chat モデル名で CreateEmbedding → 400
    *   `TestEmbedding_UnknownModel`: 404/4xx
    *   `TestEmbedding_MultiProviderRouting`: 偽 LLMGP が受け取った Bifrost provider 相当（または転送先）を openai/ollama/google の3ケースで検証（AgentService プロキシ経由で model 名だけ変えても同一契約で 200）

#### [NEW] tests/embeddings_e2e_test.go
*   **Description**: 実キーがある場合のみ実行する E2E（スキップ可能）
*   **Test Cases**:
    *   `TestEmbeddingE2E_OpenAI`: vault に openai キーが無ければ `t.Skip`。`text-embedding-3-small` で実ベクトル取得
    *   可能なら Google / Ollama も個別テスト + Skip

## Step-by-Step Implementation Guide

1. [x] **RED/GREEN config helpers**: `shared/libs/go/config/model_mode_test.go` と `model_profiles.go` の mode 定数・`ListModelRefs`・Validate 更新。
2. [x] **RED/GREEN ListModels filter**: `proxy_test.go` / `proxy.go` の embedding 除外と index endpoints。
3. [x] **RED/GREEN embeddings handler**: `openai/embeddings_test.go` / `openai/embeddings.go`。
4. [x] **RED/GREEN agentservice**: `agentservice/embeddings_test.go` / `embeddings.go` + ルート登録。
5. [x] **RED/GREEN client**: `client/v1/embeddings_test.go` / `embeddings.go`。
6. [x] **Fixtures**: `settings/example/model_profiles.yaml` と `tests/testdata/model_profiles.yaml`。
7. [x] **Integration tests**: `tests/embeddings_api_test.go` / `embeddings_e2e_test.go`。
8. [x] **Example**: `examples/embeddings-client` と `examples_build_test.go`、README。
9. [x] **Docs**: `docs/ReferenceManual-WebAPIs.md`。
10. [x] **Verify**: Verification Plan のコマンドを実行し、失敗があれば修正する。
## Verification Plan

### Automated Verification

1. **Build & Unit Tests**:

```bash
./scripts/process/build.sh
```

2. **Integration Tests（Embeddings 一式）**:

```bash
./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "Embedding"
```

3. **Examples build を含む確認**:

```bash
./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "Examples_EmbeddingsClient"
```

4. **E2E（キーがある環境）**:

```bash
./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "EmbeddingE2E"
```

5. **非回帰（既存セッション経路）**:

```bash
./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "E2E"
```

（カテゴリ機構が有効な環境では次も可）

```bash
./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories llm --specify "Embedding"
```

### Scenario Mapping

| Spec Scenario | Automated Test |
| :--- | :--- |
| シナリオ1 単一テキスト | `TestEmbedding_CreateViaClient_StubGateway` / `TestEmbeddingE2E_OpenAI` |
| シナリオ2 バッチ | `TestEmbedding_BatchInputs` |
| シナリオ3 セッション非回帰 | 既存 E2E + `--specify "E2E"` |
| シナリオ4 不正リクエスト | `TestHandleEmbeddings_MissingInput` / `TestEmbedding_UnknownModel` |
| シナリオ5 モデル一覧分離 | `TestEmbedding_ListModels_SeparatesChatAndEmbedding` |
| シナリオ6 複数プロバイダ | `TestEmbedding_MultiProviderRouting` (+ E2E Skip 可) |
| シナリオ7 examples | `TestExamples_EmbeddingsClient_Builds` |

## Documentation

*   `docs/ReferenceManual-WebAPIs.md`: Embeddings エンドポイントを追記
*   `examples/embeddings-client/README.md`: 新規
*   `settings/example/model_profiles.yaml`: embedding モデル例
*   仕様書自体の更新は不要（本計画が実装の正）
