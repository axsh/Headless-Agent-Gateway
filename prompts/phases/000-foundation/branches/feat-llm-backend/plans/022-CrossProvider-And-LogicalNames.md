# 022-CrossProvider-And-LogicalNames

> **Source Specification**: [015-Gateway-ModelDiscovery-And-LogicalNames.md](file:///prompts/phases/000-foundation/branches/feat-llm-backend/ideas/015-Gateway-ModelDiscovery-And-LogicalNames.md)

## Goal Description

クロスプロバイダ対応として、`SupportedProviders()` によるエージェント別モデルフィルタリングを撤回し、`model_profiles.yaml` に論理名 (`logical_name`) を追加して逆引き解決を実装する。また、Claude CLI がモデル名をそのまま LLMGP にパススルーすることを検証する統合テストを作成する。

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: `SupportedProviders()` 制限の撤回 | Step 1-3: `interface.go`, `adapter.go`, `service.go`, `handler.go` |
| R2: model_profiles.yaml に `logical_name` を追加 | Step 4: `config/model_profiles.go` |
| R3: 論理名からの逆引き解決 | Step 5-6: `agentservice/service.go`, `handler.go` |
| R4: モデル名パススルーの統合テスト | Step 7: `tests/agentservice_integration_test.go` |
| R5 (任意): cawa-client models で論理名表示 | 先送り: 本計画のスコープ外 |

## Proposed Changes

### codingagent パッケージ

#### [MODIFY] [interface_test.go](file:///shared/libs/go/codingagent/interface_test.go)

*   **Description**: `SupportedProviders()` の削除に伴い mock とテストを修正
*   **Technical Design**:
    ```go
    // mockAgent: SupportedProviders() 行を削除
    type mockAgent struct{}
    func (m *mockAgent) CreateSession(_ context.Context, _ ...codingagent.SessionOption) (codingagent.Session, error) {
        return nil, nil
    }
    func (m *mockAgent) Name() string { return "mock" }
    func (m *mockAgent) Close() error { return nil }
    ```

---

#### [MODIFY] [interface.go](file:///shared/libs/go/codingagent/interface.go)

*   **Description**: `CodingAgent` インターフェースから `SupportedProviders()` メソッドを削除
*   **Technical Design**:
    ```go
    type CodingAgent interface {
        CreateSession(ctx context.Context, opts ...SessionOption) (Session, error)
        Name() string
        // SupportedProviders() 削除
        Close() error
    }
    ```

---

### claudecode アダプター

#### [MODIFY] [adapter.go](file:///shared/libs/go/codingagent/claudecode/adapter.go)

*   **Description**: `SupportedProviders()` メソッド実装を削除
*   **Logic**: L29-30 の `SupportedProviders()` メソッドを削除する

---

### codex アダプター

#### [MODIFY] [adapter.go](file:///shared/libs/go/codingagent/codex/adapter.go)

*   **Description**: `SupportedProviders()` メソッド実装を削除
*   **Logic**: L29-30 の `SupportedProviders()` メソッドを削除する

---

### config パッケージ

#### [MODIFY] [model_profiles_test.go](file:///shared/libs/go/config/model_profiles_test.go)

*   **Description**: `logical_name` のパースをテスト
*   **Technical Design**:
    ```go
    func TestModelConfigLogicalName(t *testing.T) {
        tests := []struct {
            name   string
            yaml   string
            wantLN string
        }{
            {
                name:   "logical_name set",
                yaml:   "name: gpt-4o\nlogical_name: fast-coder",
                wantLN: "fast-coder",
            },
            {
                name:   "logical_name omitted",
                yaml:   "name: gpt-4o",
                wantLN: "",
            },
        }
        for _, tt := range tests {
            t.Run(tt.name, func(t *testing.T) {
                var mc ModelConfig
                if err := yaml.Unmarshal([]byte(tt.yaml), &mc); err != nil {
                    t.Fatalf("unmarshal: %v", err)
                }
                if mc.LogicalName != tt.wantLN {
                    t.Errorf("LogicalName = %q, want %q", mc.LogicalName, tt.wantLN)
                }
            })
        }
    }
    ```

---

#### [MODIFY] [model_profiles.go](file:///shared/libs/go/config/model_profiles.go)

*   **Description**: `ModelConfig` 構造体に `LogicalName` フィールドを追加、Validate に重複チェックを追加
*   **Technical Design**:
    ```go
    // ModelConfig holds per-model configuration.
    type ModelConfig struct {
        Name         string         `yaml:"name"`
        LogicalName  string         `yaml:"logical_name,omitempty"`  // 追加
        Behavior     *ModelBehavior `yaml:"behavior,omitempty"`
    }
    ```
*   **Logic**: `Validate()` に論理名の重複チェックを追加:
    ```go
    // Validate 内に追加
    logicalNames := make(map[string]string) // logical_name -> "provider/model"
    for provName, prov := range c.Providers {
        for _, key := range prov.Keys {
            for _, model := range key.Models {
                if model.LogicalName != "" {
                    key := model.LogicalName
                    ref := provName + "/" + model.Name
                    if existing, ok := logicalNames[key]; ok {
                        return fmt.Errorf("duplicate logical_name %q: %s and %s", key, existing, ref)
                    }
                    logicalNames[key] = ref
                }
            }
        }
    }
    ```

---

### agentservice パッケージ

#### [MODIFY] [handler_test.go](file:///shared/libs/go/agentservice/handler_test.go)

*   **Description**: mock の `SupportedProviders()` を削除。`IsValidModel` テストを追加。
*   **Technical Design**:
    ```go
    // mockCodingAgent: providers フィールドと SupportedProviders() を削除
    type mockCodingAgent struct {
        name string
    }
    func (m *mockCodingAgent) Name() string { return m.name }
    func (m *mockCodingAgent) Close() error { return nil }
    // SupportedProviders() 削除

    // newTestServerWithModels: providers パラメータを削除
    func newTestServerWithModels() (*agentservice.Server, http.Handler) {
        srv := agentservice.New()
        srv.RegisterAgent(&mockCodingAgent{name: "claudecode"})
        srv.SetGatewayModels(
            []llmgateway.ModelInfo{
                {Provider: "anthropic", Model: "claude-sonnet-4-20250514"},
                {Provider: "openai", Model: "gpt-4o"},
            },
            &llmgateway.ModelInfo{Provider: "anthropic", Model: "claude-sonnet-4-20250514"},
        )
        return srv, srv.HTTPHandler()
    }
    ```
*   **Logic**: 既存の `TestCreateSessionModelValidation` テストのうち、プロバイダフィルタで拒否していたケースを修正:
    - `gpt-4o` は claudecode エージェントでも valid と判定されるよう変更
    - profiles に存在しないモデルのみ invalid

---

#### [MODIFY] [service_test.go](file:///shared/libs/go/agentservice/service_test.go) (新規テスト追加)

*   **Description**: `ResolveModel()` のテストを追加
*   **Technical Design**:
    ```go
    func TestResolveModel(t *testing.T) {
        srv := agentservice.New()
        profiles := &config.ModelProfilesConfig{
            Providers: map[string]config.ProviderConfig{
                "openai": {Keys: []config.KeyConfig{{
                    Name: "default", Value: "vault://test",
                    Models: []config.ModelConfig{
                        {Name: "gpt-4o", LogicalName: "fast-coder"},
                        {Name: "gpt-4o-mini"},
                    },
                }}},
            },
        }
        srv.SetModelProfiles(profiles)

        tests := []struct {
            input     string
            wantModel string
            wantOK    bool
        }{
            {"fast-coder", "gpt-4o", true},       // 論理名マッチ
            {"gpt-4o", "gpt-4o", true},            // 実名マッチ
            {"gpt-4o-mini", "gpt-4o-mini", true},  // 実名 (論理名なし)
            {"unknown-model", "", false},           // 未定義
        }
        for _, tt := range tests {
            t.Run(tt.input, func(t *testing.T) {
                model, ok := srv.ResolveModel(tt.input)
                if ok != tt.wantOK {
                    t.Errorf("ResolveModel(%q) ok = %v, want %v", tt.input, ok, tt.wantOK)
                }
                if model != tt.wantModel {
                    t.Errorf("ResolveModel(%q) = %q, want %q", tt.input, model, tt.wantModel)
                }
            })
        }
    }
    ```

---

#### [MODIFY] [service.go](file:///shared/libs/go/agentservice/service.go)

*   **Description**: `SupportedProviders` 依存を撤回、`ResolveModel()` を追加、`SetModelProfiles()` を追加
*   **Technical Design**:

    1. `Server` 構造体に `profiles *config.ModelProfilesConfig` フィールドを追加

    2. `SetModelProfiles(p *config.ModelProfilesConfig)` メソッドを追加

    3. `IsValidModelForAgent` -> `IsValidModel` に簡素化:
    ```go
    // IsValidModel checks if a model exists in the profiles.
    func (s *Server) IsValidModel(model string) bool {
        for _, m := range s.gatewayModels {
            if m.Model == model {
                return true
            }
        }
        return false
    }
    ```

    4. `AvailableModelNamesForAgent` -> 既存の `AvailableModelNames` に統合 (ForAgent 版を削除)

    5. `ResolveModel` を追加:
    ```go
    // ResolveModel resolves a logical name or model_id to a model_id.
    // Returns (model_id, true) if found, ("", false) otherwise.
    func (s *Server) ResolveModel(input string) (string, bool) {
        if s.profiles == nil {
            return "", false
        }
        for _, prov := range s.profiles.Providers {
            for _, key := range prov.Keys {
                for _, model := range key.Models {
                    // Match by logical_name first
                    if model.LogicalName != "" && model.LogicalName == input {
                        return model.Name, true
                    }
                    // Match by model_id (name)
                    if model.Name == input {
                        return model.Name, true
                    }
                }
            }
        }
        return "", false
    }
    ```

---

#### [MODIFY] [handler.go](file:///shared/libs/go/agentservice/handler.go)

*   **Description**: `handleCreateSession` でのモデルバリデーションを修正
*   **Logic**:
    - `IsValidModelForAgent` -> `IsValidModel` に変更
    - `AvailableModelNamesForAgent` -> `AvailableModelNames` に変更
    - `ResolveModel` を使用して論理名を model_id に解決:
    ```go
    // handleCreateSession 内のモデルバリデーション部分:
    if req.Model != "" && len(s.gatewayModels) > 0 {
        resolved, ok := s.ResolveModel(req.Model)
        if !ok {
            // エラーレスポンス (既存と同様)
            w.Header().Set("Content-Type", "application/json")
            w.WriteHeader(http.StatusBadRequest)
            json.NewEncoder(w).Encode(map[string]any{
                "error":            "unsupported model: " + req.Model,
                "available_models": s.AvailableModelNames(),
            })
            return
        }
        req.Model = resolved  // 論理名を実名に解決
    }
    ```

---

### 統合テスト

#### [MODIFY] [agentservice_integration_test.go](file:///tests/agentservice_integration_test.go)

*   **Description**: mock の `SupportedProviders()` を削除。モデル名パススルー統合テストを追加。
*   **Technical Design**:

    1. `integrationMockAgent` から `providers` フィールドと `SupportedProviders()` を削除

    2. モデル名パススルー統合テストを追加:
    ```go
    // TestModelPassthroughToLLMGP verifies that Claude CLI passes
    // arbitrary model names to LLMGP without client-side validation.
    func TestModelPassthroughToLLMGP(t *testing.T) {
        // 1. Record the model field from POST /v1/messages
        var receivedModel string
        var mu sync.Mutex
        mockLLMGP := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            if r.Method == "POST" && strings.Contains(r.URL.Path, "/v1/messages") {
                var body map[string]any
                json.NewDecoder(r.Body).Decode(&body)
                mu.Lock()
                receivedModel = body["model"].(string)
                mu.Unlock()
                // Return minimal Anthropic Messages API response
                w.Header().Set("Content-Type", "application/json")
                json.NewEncoder(w).Encode(map[string]any{
                    "id":            "msg_test",
                    "type":          "message",
                    "role":          "assistant",
                    "model":         receivedModel,
                    "stop_reason":   "end_turn",
                    "content":       []map[string]any{{"type": "text", "text": "ok"}},
                    "usage":         map[string]any{"input_tokens": 1, "output_tokens": 1},
                })
            }
        }))
        defer mockLLMGP.Close()

        // 2. Check if 'claude' CLI is available
        if _, err := exec.LookPath("claude"); err != nil {
            t.Fatalf("claude CLI not found: %v (required for passthrough test)", err)
        }

        // 3. Create ClaudeCodeAdapter pointing to mock LLMGP
        adapter := claudecode.New(&codingagent.AdapterConfig{
            GatewayURL: mockLLMGP.URL,
        })

        // 4. Run with custom model name
        testModel := "gpt-4o"
        ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
        defer cancel()

        sess, err := adapter.CreateSession(ctx,
            codingagent.WithModel(testModel),
            codingagent.WithPrompt("respond with just 'ok'"),
        )
        if err != nil {
            t.Fatalf("CreateSession: %v", err)
        }
        defer sess.Close()

        ch, err := sess.Send(ctx, "respond with just 'ok'")
        if err != nil {
            t.Fatalf("Send: %v", err)
        }
        // Drain events
        for range ch {}

        // 5. Verify the model field
        mu.Lock()
        got := receivedModel
        mu.Unlock()
        if got != testModel {
            t.Errorf("LLMGP received model = %q, want %q", got, testModel)
        }
    }
    ```

    > [!NOTE]
    > テストルール S6.1 により `t.Skip()` は禁止。`claude` CLI が存在しない場合は `t.Fatalf` で失敗させる。

---

## Step-by-Step Implementation Guide

### Step 1: CodingAgent インターフェースのテスト修正

1. Edit `shared/libs/go/codingagent/interface_test.go`:
   - `mockAgent` から `SupportedProviders()` メソッドを削除

### Step 2: CodingAgent インターフェースから `SupportedProviders()` を削除

1. Edit `shared/libs/go/codingagent/interface.go`:
   - `SupportedProviders() []string` を削除 (L16-18)

### Step 3: アダプターの `SupportedProviders()` 実装を削除

1. Edit `shared/libs/go/codingagent/claudecode/adapter.go`:
   - L29-30 の `SupportedProviders()` メソッドを削除
2. Edit `shared/libs/go/codingagent/codex/adapter.go`:
   - L29-30 の `SupportedProviders()` メソッドを削除

### Step 4: config に `LogicalName` を追加

1. Edit `shared/libs/go/config/model_profiles_test.go`:
   - `TestModelConfigLogicalName` テストを追加 (YAML パースの検証)
   - `TestModelProfilesValidateDuplicateLogicalName` テストを追加 (重複チェック)
2. Edit `shared/libs/go/config/model_profiles.go`:
   - `ModelConfig` に `LogicalName string` フィールドを追加
   - `Validate()` に論理名の重複チェックロジックを追加

### Step 5: agentservice のテスト修正と `ResolveModel` テスト追加

1. Edit `shared/libs/go/agentservice/handler_test.go`:
   - `mockCodingAgent` から `providers` フィールドと `SupportedProviders()` を削除
   - `newTestServerWithModels` から providers 設定を削除
   - `TestCreateSessionModelValidation` でプロバイダフィルタ関連のテストケースを更新
2. 新規テスト `TestResolveModel` を `shared/libs/go/agentservice/service_test.go` に追加 (または handler_test.go に統合)

### Step 6: agentservice の本体を修正

1. Edit `shared/libs/go/agentservice/service.go`:
   - `Server` に `profiles` フィールドを追加
   - `SetModelProfiles()` メソッドを追加
   - `IsValidModelForAgent()` -> `IsValidModel()` に簡素化
   - `AvailableModelNamesForAgent()` を削除 (既存の `AvailableModelNames()` に統合)
   - `ResolveModel()` メソッドを追加
2. Edit `shared/libs/go/agentservice/handler.go`:
   - `handleCreateSession` 内の `IsValidModelForAgent` -> `IsValidModel` に変更
   - `ResolveModel` による論理名解決を追加

### Step 7: 統合テストの修正とパススルーテスト追加

1. Edit `tests/agentservice_integration_test.go`:
   - `integrationMockAgent` から `providers` と `SupportedProviders()` を削除
   - `TestModelPassthroughToLLMGP` 統合テストを追加

### Step 8: Verification Plan を実行

## Verification Plan

### テスト項目セルフレビュー (S11.4)

1. **網羅性**: R1-R4 の全要件がカバーされている。R5 は先送りで理由を明記済み。
2. **証拠の十分性**: `ResolveModel` は論理名マッチ/実名マッチ/未定義の 3 パターンをテスト。`IsValidModel` はプロバイダ無関係で検証。パススルーテストは実 CLI + モック LLMGP で検証。
3. **迂回排除**: パススルーテストはモック LLMGP の受信内容を直接検証するため、偽陽性の余地がない。
4. **依存関係**: config (Step 4) -> agentservice (Step 5-6) -> 統合テスト (Step 7) のボトムアップ順序で設計。

### Automated Verification

1. **Build & Unit Tests**:
   ```bash
   ./scripts/process/build.sh
   ```

2. **Integration Tests (LLM)**:
   ```bash
   ./scripts/process/integration_test.sh --categories "llm" --specify "ModelPassthrough"
   ```

3. **Integration Tests (Common)**:
   ```bash
   ./scripts/process/integration_test.sh --categories "common"
   ```

4. **総合判定**: 全テスト完了後、S12 に従い総合判定を実施。

## Documentation

#### [MODIFY] [015-Gateway-ModelDiscovery-And-LogicalNames.md](file:///prompts/phases/000-foundation/branches/feat-llm-backend/ideas/015-Gateway-ModelDiscovery-And-LogicalNames.md)
*   **更新内容**: 実装完了後、検証結果を反映
