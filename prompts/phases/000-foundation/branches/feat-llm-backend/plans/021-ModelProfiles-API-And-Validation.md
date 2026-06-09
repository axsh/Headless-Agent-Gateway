# 021-ModelProfiles-API-And-Validation

> **Source Specification**: [014-ModelProfiles-API-And-Validation.md](file:///prompts/phases/000-foundation/branches/feat-llm-backend/ideas/014-ModelProfiles-API-And-Validation.md)

## Goal Description

LLM Gateway Proxy を唯一の情報源 (Single Source of Truth) として、モデル情報 API の拡張、AgentService でのモデルバリデーション、DefaultModel のハードコード排除、cawa-client の models コマンド追加を行う。

テスト設計の方針として、**ボトムアップ順序で「実際に動作していると言い切れる」テスト群**を構築する。

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: LLM Gateway Proxy にモデル情報 API を拡張する | Proposed Changes > llmgateway > backend.go, proxy.go, bifrost_driver.go, stub.go |
| R2: AgentService にモデル一覧 API を追加する | Proposed Changes > agentservice > handler.go, service.go |
| R3: セッション作成時のモデルバリデーション | Proposed Changes > agentservice > handler.go |
| R4: DefaultModel のハードコード排除 | Proposed Changes > standalone > main.go |
| R5: model_profiles.yaml に default_profile を追加する | Proposed Changes > examples/standalone > model_profiles.yaml |
| R6: cawa-client に models サブコマンドを追加する | Proposed Changes > examples/cawa-client > main.go |

## Proposed Changes

### config パッケージ (末端: 変更なし、既存構造の確認)

`DefaultProfileConfig` は既に定義済み。`model_profiles.yaml` のパース時に `default_profile` セクションを読み込む構造は既存。変更不要。

```go
// 既存 (config/model_profiles.go L17-L21)
type DefaultProfileConfig struct {
    Provider string `yaml:"provider"`
    Model    string `yaml:"model"`
}
```

---

### llmgateway パッケージ (レイヤー 1: Gateway 側)

#### [MODIFY] [backend.go](file:///shared/libs/go/llmgateway/backend.go)
*   **Description**: `LLMGatewayBackend` インターフェースに `DefaultModel()` メソッドを追加
*   **Technical Design**:
    ```go
    type LLMGatewayBackend interface {
        Launch(ctx context.Context) error
        Shutdown(ctx context.Context) error
        ListModels() []ModelInfo
        DefaultModel() *ModelInfo  // NEW: default_profile から取得
        Health() HealthStatus
        ProxyURL() string
    }
    ```
*   **Logic**:
    *   `DefaultModel()` は `model_profiles.yaml` の `default_profile` セクションの `provider` と `model` から `*ModelInfo` を返す
    *   `default_profile` が未設定の場合は `nil` を返す

#### [MODIFY] [proxy.go](file:///shared/libs/go/llmgateway/proxy.go)
*   **Description**: `DefaultModel()` を実装し、`handleModels` レスポンスに `default_model` を追加
*   **Technical Design**:
    ```go
    // DefaultModel returns the default model from profiles.
    func (p *ProxyServer) DefaultModel() *ModelInfo {
        if p.profiles == nil {
            return nil
        }
        dp := p.profiles.DefaultProfile
        if dp.Provider == "" || dp.Model == "" {
            return nil
        }
        return &ModelInfo{Provider: dp.Provider, Model: dp.Model}
    }
    ```
*   **Logic**:
    *   `handleModels` を拡張:
    ```go
    func (p *ProxyServer) handleModels(w http.ResponseWriter, r *http.Request) {
        resp := map[string]any{
            "models": p.ListModels(),
        }
        if dm := p.DefaultModel(); dm != nil {
            resp["default_model"] = dm
        }
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(resp)
    }
    ```

#### [MODIFY] [bifrost_driver.go](file:///shared/libs/go/llmgateway/bifrost_driver.go)
*   **Description**: `DefaultModel()` を `proxy` へ委譲
*   **Technical Design**:
    ```go
    func (d *BifrostDriver) DefaultModel() *ModelInfo {
        return d.proxy.DefaultModel()
    }
    ```

#### [MODIFY] [stub.go](file:///shared/libs/go/llmgateway/stub.go)
*   **Description**: `DefaultModel()` のスタブ実装
*   **Technical Design**:
    ```go
    func (s *StubGateway) DefaultModel() *ModelInfo {
        return nil
    }
    ```

---

### agentservice パッケージ (レイヤー 2: バリデーション + API 転送)

#### [MODIFY] [service.go](file:///shared/libs/go/agentservice/service.go)
*   **Description**: `Server` に `gatewayModels` フィールドを追加し、モデル一覧をキャッシュする
*   **Technical Design**:
    ```go
    type Server struct {
        // ... existing fields ...
        gatewayModels     []llmgateway.ModelInfo  // cached at init
        gatewayDefault    *llmgateway.ModelInfo   // cached default model
    }

    // FetchModelsFromGateway calls LLMGP /v1/models and caches the result.
    func (s *Server) FetchModelsFromGateway() error {
        if s.gatewayURL == "" {
            return nil
        }
        ctx, cancel := context.WithTimeout(context.Background(), healthCheckTimeout)
        defer cancel()

        req, err := http.NewRequestWithContext(ctx, "GET", s.gatewayURL+"/v1/models", nil)
        if err != nil {
            return err
        }
        resp, err := http.DefaultClient.Do(req)
        if err != nil {
            return err
        }
        defer resp.Body.Close()

        var body struct {
            Models       []llmgateway.ModelInfo  `json:"models"`
            DefaultModel *llmgateway.ModelInfo   `json:"default_model"`
        }
        if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
            return err
        }
        s.gatewayModels = body.Models
        s.gatewayDefault = body.DefaultModel
        return nil
    }

    // IsValidModel checks if a model name exists in the cached model list.
    func (s *Server) IsValidModel(model string) bool {
        for _, m := range s.gatewayModels {
            if m.Model == model {
                return true
            }
        }
        return false
    }

    // AvailableModelNames returns a list of model name strings.
    func (s *Server) AvailableModelNames() []string {
        names := make([]string, len(s.gatewayModels))
        for i, m := range s.gatewayModels {
            names[i] = m.Model
        }
        return names
    }
    ```

#### [MODIFY] [handler.go](file:///shared/libs/go/agentservice/handler.go)
*   **Description**: `handleModels` エンドポイントの追加、`handleCreateSession` にモデルバリデーション追加
*   **Technical Design**:
    ```go
    // handleListModels handles GET /api/v1/models.
    func (s *Server) handleListModels(w http.ResponseWriter, r *http.Request) {
        resp := map[string]any{
            "models": s.gatewayModels,
        }
        if s.gatewayDefault != nil {
            resp["default_model"] = s.gatewayDefault
        }
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(resp)
    }
    ```
*   **Logic** (handleCreateSession のバリデーション追加):
    ```go
    // In handleCreateSession, after agent validation:
    if req.Model != "" && len(s.gatewayModels) > 0 {
        if !s.IsValidModel(req.Model) {
            w.Header().Set("Content-Type", "application/json")
            w.WriteHeader(http.StatusBadRequest)
            json.NewEncoder(w).Encode(map[string]any{
                "error":            "unknown model: " + req.Model,
                "available_models": s.AvailableModelNames(),
            })
            return
        }
    }
    ```
*   **Logic** (ルーティング追加):
    ```go
    // In HTTPHandler():
    mux.HandleFunc("/api/v1/models", s.routeModels)

    // In routeModels:
    func (s *Server) routeModels(w http.ResponseWriter, r *http.Request) {
        switch r.Method {
        case http.MethodGet:
            s.handleListModels(w, r)
        default:
            http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
        }
    }
    ```

---

### standalone (レイヤー 3: エントリポイント)

#### [MODIFY] [examples/standalone/main.go](file:///examples/standalone/main.go)
*   **Description**: DefaultModel のハードコードを排除し、Gateway から動的に取得
*   **Logic**:
    ```go
    func registerCodingAgents(srv *hag.Server) {
        if _, err := exec.LookPath("claude"); err == nil {
            gwURL := srv.Gateway().ProxyURL()

            // Resolve default model from Gateway (model_profiles.yaml).
            defaultModel := ""
            if dm := srv.Gateway().DefaultModel(); dm != nil {
                defaultModel = dm.Model
            }

            adapter := claudecode.New(&codingagent.AdapterConfig{
                GatewayURL:   gwURL,
                DefaultModel: defaultModel,
            })
            srv.AgentService().RegisterAgent(adapter)

            // Fetch and cache model list for AgentService validation.
            srv.AgentService().FetchModelsFromGateway()

            fmt.Printf("Registered coding agent: claudecode (gateway=%s, default_model=%s)\n", gwURL, defaultModel)
        } else {
            fmt.Println("Warning: claude CLI not found, claudecode agent not registered")
        }
    }
    ```

#### [MODIFY] [examples/standalone/model_profiles.yaml](file:///examples/standalone/model_profiles.yaml)
*   **Description**: `default_profile` セクションを追加
*   **Logic**:
    ```yaml
    default_profile:
      provider: anthropic
      model: claude-sonnet-4-20250514

    providers:
      openai:
        keys:
          - name: default
            value: vault://providers/openai/default
            models:
              - name: gpt-4o
              - name: gpt-4o-mini
              - name: gpt-4.1-mini
      anthropic:
        keys:
          - name: default
            value: vault://providers/anthropic/default
            models:
              - name: claude-sonnet-4-20250514
      google:
        keys:
          - name: default
            value: vault://providers/google/default
            models:
              - name: gemini-2.5-flash
    ```

---

### cawa-client (レイヤー 4: CLI)

#### [MODIFY] [examples/cawa-client/main.go](file:///examples/cawa-client/main.go)
*   **Description**: `models` サブコマンドの追加
*   **Technical Design**:
    ```go
    case "models":
        cmdModels()

    // cmdModels calls GET /api/v1/models and displays the result.
    func cmdModels() {
        resp, err := http.Get(serverURL + "/api/v1/models")
        if err != nil {
            fmt.Fprintf(os.Stderr, "Error: %v\n", err)
            os.Exit(1)
        }
        defer resp.Body.Close()

        var body struct {
            Models       []struct {
                Provider string `json:"provider"`
                Model    string `json:"model"`
            } `json:"models"`
            DefaultModel *struct {
                Provider string `json:"provider"`
                Model    string `json:"model"`
            } `json:"default_model"`
        }
        if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
            fmt.Fprintf(os.Stderr, "Error decoding models: %v\n", err)
            os.Exit(1)
        }

        defaultModelName := ""
        if body.DefaultModel != nil {
            defaultModelName = body.DefaultModel.Model
        }

        // Group by provider
        byProvider := make(map[string][]string)
        for _, m := range body.Models {
            byProvider[m.Provider] = append(byProvider[m.Provider], m.Model)
        }

        fmt.Println("Available models:")
        for provider, models := range byProvider {
            fmt.Printf("  %s:\n", provider)
            for _, model := range models {
                if model == defaultModelName {
                    fmt.Printf("    * %s (default)\n", model)
                } else {
                    fmt.Printf("    - %s\n", model)
                }
            }
        }
    }
    ```

---

## Step-by-Step Implementation Guide

### Phase 1: 末端テスト -- config / model_profiles (既存動作確認)

- [x] 1-1. `config/loader_test.go` に `default_profile` 読み込みテストが既存であることを確認 (追加不要の場合はスキップ)

### Phase 2: テスト先行 -- LLM Gateway (DefaultModel)

- [x] 2-1. `llmgateway/proxy_test.go` に `TestProxyServer_DefaultModel_Nil` を追加 (profiles なし -> nil)
- [x] 2-2. `llmgateway/proxy_test.go` に `TestProxyServer_DefaultModel_WithProfiles` を追加 (profiles あり -> 正しい値)
- [x] 2-3. `llmgateway/proxy_test.go` に `TestProxyServer_ModelsWithDefaultModel` を追加 (/v1/models レスポンスに default_model 含む)
- [x] 2-4. `llmgateway/backend.go` に `DefaultModel()` をインターフェースに追加
- [x] 2-5. `llmgateway/proxy.go` に `DefaultModel()` を実装、`handleModels` を拡張
- [x] 2-6. `llmgateway/bifrost_driver.go` に `DefaultModel()` を委譲実装
- [x] 2-7. `llmgateway/stub.go` に `DefaultModel()` を追加
- [x] 2-8. `llmgateway/backend_test.go` のコンパイルチェックを確認
- [x] 2-9. ビルド + 単体テスト実行 -- Phase 2 テスト全通過を確認
- [x] 2-10. コミット: `feat: add DefaultModel() to LLMGatewayBackend interface`

### Phase 3: テスト先行 -- AgentService (モデル一覧 API + バリデーション)

- [x] 3-1. `agentservice/handler_test.go` に `TestHandleListModels` を追加 (GET /api/v1/models が正しくレスポンスを返す)
- [x] 3-2. `agentservice/handler_test.go` に `TestHandleCreateSession_InvalidModel` を追加 (存在しないモデル -> 400)
- [x] 3-3. `agentservice/handler_test.go` に `TestHandleCreateSession_ValidModel` を追加 (存在するモデル -> 201)
- [x] 3-4. `agentservice/handler_test.go` に `TestHandleCreateSession_EmptyModel` を追加 (モデル未指定 -> 201、デフォルトにフォールバック)
- [x] 3-5. `agentservice/handler_test.go` に `TestHandleCreateSession_NoGatewayModels` を追加 (gatewayModels 空 -> バリデーションスキップ、201)
- [x] 3-6. `agentservice/service.go` に `FetchModelsFromGateway()`, `IsValidModel()`, `AvailableModelNames()` を実装
- [x] 3-7. `agentservice/handler.go` に `handleListModels`, `routeModels` を追加
- [x] 3-8. `agentservice/handler.go` の `handleCreateSession` にバリデーションロジックを追加
- [x] 3-9. `agentservice/service.go` の `HTTPHandler()` にルーティング追加
- [x] 3-10. ビルド + 単体テスト実行 -- Phase 3 テスト全通過を確認
- [x] 3-11. コミット: `feat: add model validation and /api/v1/models endpoint to AgentService`

### Phase 4: テスト先行 -- 統合テスト (AgentService + Gateway 連携)

- [x] 4-1. `tests/agentservice_integration_test.go` に `TestAgentServiceModelsEndpoint` を追加 (GET /api/v1/models がモデル一覧を返す)
- [x] 4-2. `tests/agentservice_integration_test.go` に `TestAgentServiceCreateSession_InvalidModel` を追加 (存在しないモデルで 400)
- [x] 4-3. `tests/agentservice_integration_test.go` に `TestAgentServiceCreateSession_ValidModel` を追加 (存在するモデルで 201)
- [x] 4-4. `tests/agentservice_integration_test.go` に `TestAgentServiceDefaultModelFromProfiles` を追加 (DefaultModel がハードコードでなく YAML から取得されることの確認)
- [x] 4-5. 統合テスト実行 -- Phase 4 テスト全通過を確認
- [x] 4-6. コミット: `test: add integration tests for model validation and profiles API`

### Phase 5: model_profiles.yaml 更新 + standalone 修正

- [x] 5-1. `examples/standalone/model_profiles.yaml` に `default_profile` セクションを追加
- [x] 5-2. `examples/standalone/main.go` の `registerCodingAgents` を修正 (DefaultModel ハードコード排除)
- [x] 5-3. ビルド + 単体テスト実行
- [x] 5-4. コミット: `feat: remove hardcoded DefaultModel, resolve from model_profiles.yaml`

### Phase 6: cawa-client 修正

- [x] 6-1. `examples/cawa-client/main.go` に `models` サブコマンドを追加
- [x] 6-2. `printUsage()` を更新
- [x] 6-3. ビルド
- [x] 6-4. コミット: `feat: add models subcommand to cawa-client`

### Phase 7: ドキュメント + 全体検証

- [x] 7-1. README.md を更新 (models API ドキュメント、models サブコマンド)
- [x] 7-2. コミット: `docs: update README with models API and validation`
- [x] 7-3. 全体ビルド + 統合テスト実行
- [x] 7-4. 総合判定プロセスの実施

## Verification Plan

### テスト項目設計

#### ボトムアップ確認順序

```
依存関係:  cawa-client -> AgentService -> LLMGatewayBackend -> config/ModelProfilesConfig

テスト順序:
  Step 1: config (末端) -- default_profile の読み込みが正しいか (既存テストで確認済み)
  Step 2: llmgateway (Gateway) -- DefaultModel() が正しく動作するか
  Step 3: agentservice (サービス) -- モデル一覧 API とバリデーションが動作するか
  Step 4: 統合テスト -- Gateway + AgentService の連携が動作するか
```

#### テスト項目一覧

| # | テスト名 | 配置 | 観点 | 検証内容 |
|---|---------|------|------|---------|
| T1 | `TestProxyServer_DefaultModel_Nil` | `llmgateway/proxy_test.go` | 正常系/境界値 | profiles なしの場合 `DefaultModel()` が nil を返す |
| T2 | `TestProxyServer_DefaultModel_WithProfiles` | `llmgateway/proxy_test.go` | 正常系 | profiles あり -> `DefaultModel()` が `{anthropic, claude-sonnet-4-20250514}` を返す |
| T3 | `TestProxyServer_ModelsWithDefaultModel` | `llmgateway/proxy_test.go` | 外部連携 | `GET /v1/models` レスポンスに `default_model` フィールドが含まれる |
| T4 | `TestHandleListModels` | `agentservice/handler_test.go` | 正常系 | `GET /api/v1/models` が models 配列と default_model を返す |
| T5 | `TestHandleCreateSession_InvalidModel` | `agentservice/handler_test.go` | 異常系 | 存在しないモデル -> 400 + エラーメッセージ + 利用可能モデル一覧 |
| T6 | `TestHandleCreateSession_ValidModel` | `agentservice/handler_test.go` | 正常系 | 存在するモデル -> 201 (既存動作の維持) |
| T7 | `TestHandleCreateSession_EmptyModel` | `agentservice/handler_test.go` | 正常系/境界値 | モデル未指定 -> 201 (バリデーションスキップ、デフォルトフォールバック) |
| T8 | `TestHandleCreateSession_NoGatewayModels` | `agentservice/handler_test.go` | 境界値/設定反映 | Gateway 未接続 (モデル一覧空) -> バリデーションスキップ、201 (フェイルオープン) |
| T9 | `TestAgentServiceModelsEndpoint` | `tests/agentservice_integration_test.go` | 外部連携 | HAG Server 経由で models API が正しく動作する |
| T10 | `TestAgentServiceCreateSession_InvalidModel` | `tests/agentservice_integration_test.go` | 異常系/外部連携 | HAG Server 経由で無効モデルが 400 で拒否される |
| T11 | `TestAgentServiceCreateSession_ValidModel` | `tests/agentservice_integration_test.go` | 正常系/外部連携 | HAG Server 経由で有効モデルが 201 で受理される |
| T12 | `TestAgentServiceDefaultModelFromProfiles` | `tests/agentservice_integration_test.go` | 設定反映/データ一貫性 | DefaultModel が model_profiles.yaml の default_profile から取得され、ハードコードされていない |

#### 観点チェックリスト結果

| # | 観点 | カバー済み |
|---|------|-----------|
| 1 | 正常系の動作確認 | T2, T3, T4, T6, T7, T9, T11 |
| 2 | 異常系・境界値 | T1, T5, T7, T8, T10 |
| 3 | 外部連携の実動作 | T3, T9, T10, T11 |
| 4 | データの一貫性 | T12 (YAML -> DefaultModel -> CLI args の一貫性) |
| 5 | 状態遷移の検証 | T6, T11 (セッション作成 -> active 状態) |
| 6 | 設定・構成の反映 | T2, T8, T12 |
| 7 | 副作用の確認 | T8 (Gateway 未接続でもサービスが正常動作) |

#### テスト項目セルフレビュー結果

1. **網羅性**: T1-T12 により、Gateway の DefaultModel 取得 -> AgentService のバリデーション -> 統合テストでの連携まで、ボトムアップで全レイヤーの動作を確認できる。全テスト成功で「モデル指定が model_profiles.yaml に基づいて正しくバリデーションされている」と言い切れる。
2. **証拠の十分性**: 各テストは「エラーが出ない」だけでなく「期待する具体的な値が返る」ことを検証している (例: T2 では ModelInfo の Provider と Model フィールドの一致を検証、T5 では 400 ステータスコードとエラーメッセージの具体的な中身を検証)。
3. **迂回・抜け道の排除**: T8 により Gateway 未接続時のフェイルオープン動作を検証。T12 により DefaultModel がハードコードでなく YAML から取得されることを検証。
4. **依存関係の整合性**: T1-T3 (Gateway 単体) が成功して初めて T4-T8 (AgentService 単体) の成功に意味がある。T9-T12 (統合テスト) は Gateway + AgentService 双方の動作を前提とする。

### Automated Verification

1.  **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```

2.  **Integration Tests** (AgentService + Gateway 連携):
    ```bash
    ./scripts/process/integration_test.sh --categories "common" --specify "TestAgentService"
    ```

3.  **LLM Integration Tests** (リグレッション):
    ```bash
    ./scripts/process/integration_test.sh --categories "llm"
    ```

*   **Log Verification**: テストログに `SKIP`, `WARN`, `TODO` マーカーが含まれていないことを確認する。全テストが実際に実行されたことを確認する。

### 総合判定プロセス

全テスト完了後、testing-rules.md section 12 に従い総合判定を実施する。

## Documentation

#### [MODIFY] [README.md](file:///README.md)
*   **更新内容**:
    *   「モデルの指定方法」セクションに `GET /api/v1/models` API の説明を追加
    *   cawa-client の使い方に `models` サブコマンドを追加
    *   `model_profiles.yaml` の書式に `default_profile` セクションの説明を追加
    *   無効なモデル指定時のエラーレスポンスの説明を追加
