# 063-LLM-Response-Stability-MaxTokens-Logging

> **Source Specification**: [052-LLM-Response-Stability-MaxTokens-Logging.md](file:///prompts/phases/000-foundation/branches/feat-llm-backend/ideas/052-LLM-Response-Stability-MaxTokens-Logging.md)

## Goal Description

LLM の応答安定性を改善する。`max_output_tokens` を `model_profiles.yaml` で設定可能にし、空レスポンスの検出・リトライ機構を追加し、ログの情報量と粒度 (Debug/Trace 分離) を最適化する。

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: max_output_tokens の設定化 | Step 1-3: config, handlerctx, routing, anthropic/handler, bifrost_client |
| R2: LLMResponse に StopReason を追加 | Step 4: llm_client.go, bifrost_client.go |
| R3: 空レスポンスの検出とリトライ | Step 5: agent_core.go |
| R4: ログレベルの適正化 (Debug -> Trace) | Step 6: anthropic/handler.go |
| R5: LLM リクエスト/レスポンスの概要ログ追加 | Step 5: agent_core.go |
| R6: LLM Gateway ストリーム完了ログの改善 | Step 6: anthropic/handler.go |

---

## Proposed Changes

### config パッケージ

#### [MODIFY] [model_profiles_test.go](file:///shared/libs/go/config/model_profiles_test.go)
*   **Description**: `MaxOutputTokens` のパース・バリデーションテストを追加
*   **Technical Design**:
    ```go
    func TestModelProfilesConfig_MaxOutputTokens(t *testing.T) {
        tests := []struct {
            name     string
            yaml     string
            expected int
        }{
            {
                name: "max_output_tokens set",
                yaml: `providers:
  google:
    api_keys:
      - name: default
        models:
          - name: gemini-2.5-flash
            behavior:
              max_output_tokens: 65536`,
                expected: 65536,
            },
            {
                name: "max_output_tokens not set (zero value)",
                yaml: `providers:
  google:
    api_keys:
      - name: default
        models:
          - name: gemini-2.5-flash`,
                expected: 0,
            },
        }
        // パース後に Providers["google"].ApiKeys[0].Models[0].Behavior.MaxOutputTokens を検証
    }
    ```

#### [MODIFY] [model_profiles.go](file:///shared/libs/go/config/model_profiles.go)
*   **Description**: `ModelBehavior` に `MaxOutputTokens` フィールドを追加
*   **Technical Design**:
    ```go
    type ModelBehavior struct {
        ToolCallFallback bool `yaml:"tool_call_fallback"`
        StructuredOutput bool `yaml:"structured_output"`
        MaxOutputTokens  int  `yaml:"max_output_tokens,omitempty"`
    }
    ```
*   **Logic**: YAML デシリアライズのみ。未設定時はゼロ値 (`0`)。

---

### handlerctx パッケージ

#### [MODIFY] [context.go](file:///shared/libs/go/llmgateway/handlerctx/context.go)
*   **Description**: `RoutedModel` に `MaxOutputTokens` フィールドを追加
*   **Technical Design**:
    ```go
    type RoutedModel struct {
        Provider         string `json:"provider"`
        KeyName          string `json:"key_name,omitempty"`
        KeyValue         string `json:"-"`
        Model            string `json:"model"`
        Mode             string `json:"mode,omitempty"`
        ToolCallFallback bool   `json:"tool_call_fallback"`
        MaxOutputTokens  int    `json:"max_output_tokens,omitempty"` // 新規追加
    }
    ```

---

### llmgateway パッケージ

#### [MODIFY] [routing_test.go](file:///shared/libs/go/llmgateway/routing_test.go)
*   **Description**: `ResolveModel` が `MaxOutputTokens` を伝達するテストを追加
*   **Technical Design**:
    ```go
    func TestResolveModel_MaxOutputTokens(t *testing.T) {
        profiles := &config.ModelProfilesConfig{
            Providers: map[string]config.ProviderConfig{
                "google": {
                    ApiKeys: []config.KeyConfig{{
                        Name: "default",
                        Models: []config.ModelConfig{{
                            Name: "gemini-2.5-flash",
                            Behavior: &config.ModelBehavior{
                                MaxOutputTokens: 65536,
                            },
                        }},
                    }},
                },
            },
        }
        router := NewModelRouter(profiles, nil, nil)
        routed, err := router.ResolveModel("gemini-2.5-flash", "")
        assert.NoError(t, err)
        assert.Equal(t, 65536, routed.MaxOutputTokens)
    }

    func TestResolveModel_MaxOutputTokens_Unset(t *testing.T) {
        // Behavior == nil のケース -> MaxOutputTokens == 0
    }
    ```

#### [MODIFY] [routing.go](file:///shared/libs/go/llmgateway/routing.go)
*   **Description**: `ResolveModel` で `MaxOutputTokens` を `RoutedModel` に伝達
*   **Technical Design**: L66-76 の `resolved` 構築部分を修正
*   **Logic**:
    ```go
    // 既存の RoutedModel 構築箇所 (L69-76)
    var fallback bool
    var maxOutputTokens int
    if model.Behavior != nil {
        fallback = model.Behavior.ToolCallFallback
        maxOutputTokens = model.Behavior.MaxOutputTokens
    }
    resolved = &RoutedModel{
        Provider:         providerName,
        KeyName:          key.Name,
        KeyValue:         key.Secret,
        Model:            modelName,
        Mode:             model.Mode,
        ToolCallFallback: fallback,
        MaxOutputTokens:  maxOutputTokens,
    }
    ```

---

### llmgateway/anthropic パッケージ

#### [MODIFY] [handler.go](file:///shared/libs/go/llmgateway/anthropic/handler.go)
*   **Description**: 4つの変更: (1) MaxTokens オーバーライド (R1), (2) Debug->Trace 降格 (R4), (3) ストリーム完了ログ改善 (R6)
*   **Technical Design**:

    **(1) MaxTokens オーバーライド** (L142 付近、`handleMessagesViaBifrost` 呼び出し前):
    ```go
    // 現在:
    handleMessagesViaBifrost(ctx, w, r, body, routed)

    // 変更後: routed.MaxOutputTokens がセットされていれば fullReq.MaxTokens をオーバーライド
    // handleMessagesViaBifrost 内部の fullReq 解析直後に追加:
    if routed.MaxOutputTokens > 0 {
        fullReq.MaxTokens = routed.MaxOutputTokens
        log.Debug("max_tokens overridden from model profile",
            "original", origMaxTokens, "overridden", routed.MaxOutputTokens)
    }
    ```

    **(2) Debug -> Trace 降格** (L170, L190):
    ```go
    // L170: log.Debug -> log.Trace
    log.Trace("raw anthropic request messages", "json", string(reqMessagesJSON))
    // L190: log.Debug -> log.Trace
    log.Trace("converted bifrost request", "json", string(bReqJSON))
    ```

    **(3) ストリーム完了ログ改善** (L279 `handleMessagesBifrostStream` 内):
    ```go
    func handleMessagesBifrostStream(...) {
        // 冒頭に追加:
        startTime := time.Now()

        // L462: 完了ログを改善
        elapsed := time.Since(startTime)
        log.Info("bifrost anthropic stream completed",
            "model", model,
            "chunks", chunkCount,
            "duration_ms", elapsed.Milliseconds(),
            "output_tokens", totalOutputTokens)
    }
    ```

---

### wayfinder パッケージ

#### [MODIFY] [llm_client.go](file:///shared/libs/go/wayfinder/llm_client.go)
*   **Description**: `LLMResponse` に `StopReason` フィールドを追加 (R2)
*   **Technical Design**:
    ```go
    type LLMResponse struct {
        Content    string     `json:"content"`
        ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
        StopReason string     `json:"stop_reason,omitempty"` // 新規追加
    }
    ```

#### [MODIFY] [bifrost_client_test.go](file:///shared/libs/go/wayfinder/bifrost_client_test.go)
*   **Description**: StopReason パース、デフォルト max_tokens の変更に対応するテスト
*   **Technical Design**:
    ```go
    func TestBifrostClient_ParseResponse_StopReason(t *testing.T) {
        tests := []struct {
            name       string
            response   map[string]any
            wantReason string
        }{
            {
                name: "end_turn",
                response: map[string]any{
                    "stop_reason": "end_turn",
                    "content":     []any{map[string]any{"type": "text", "text": "hello"}},
                },
                wantReason: "end_turn",
            },
            {
                name: "max_tokens",
                response: map[string]any{
                    "stop_reason": "max_tokens",
                    "content":     []any{map[string]any{"type": "text", "text": "truncated"}},
                },
                wantReason: "max_tokens",
            },
            {
                name: "no stop_reason",
                response: map[string]any{
                    "content": []any{map[string]any{"type": "text", "text": "hi"}},
                },
                wantReason: "",
            },
        }
        // 各ケースで bc.parseResponse(tt.response) -> result.StopReason を検証
    }

    func TestBifrostClient_DefaultMaxTokens(t *testing.T) {
        // buildRequestBody で max_tokens が 16384 になることを検証
        // 既存テスト (L43-44) の期待値を 4096 -> 16384 に変更
    }

    func TestBifrostClient_ParseSSEStream_StopReason(t *testing.T) {
        // message_delta イベントで stop_reason を取得できることを検証
    }
    ```

#### [MODIFY] [bifrost_client.go](file:///shared/libs/go/wayfinder/bifrost_client.go)
*   **Description**: (1) デフォルト max_tokens を 16384 に変更 (R1), (2) parseResponse/parseSSEStream で StopReason を取得 (R2)
*   **Technical Design**:

    **(1) デフォルト max_tokens 変更** (L77):
    ```go
    // 変更前:
    "max_tokens": 4096,
    // 変更後:
    "max_tokens": 16384,
    ```

    **(2) parseResponse で StopReason を取得** (L165-198):
    ```go
    func (bc *BifrostClient) parseResponse(respBody map[string]any) (*LLMResponse, error) {
        result := &LLMResponse{}

        // stop_reason を取得
        if sr, ok := respBody["stop_reason"].(string); ok {
            result.StopReason = sr
        }

        // ...existing content parsing...
        return result, nil
    }
    ```

    **(3) parseSSEStream で StopReason を取得** (L248-344):
    ```go
    func (bc *BifrostClient) parseSSEStream(body io.Reader, onDelta func(textDelta string)) (*LLMResponse, error) {
        // 既存変数に追加:
        var stopReason string

        // message_delta イベント処理を追加:
        case "message_delta":
            delta, _ := event["delta"].(map[string]any)
            if delta != nil {
                if sr, ok := delta["stop_reason"].(string); ok {
                    stopReason = sr
                }
            }

        // 最終 return:
        return &LLMResponse{
            Content:    strings.Join(textParts, ""),
            ToolCalls:  toolCalls,
            StopReason: stopReason,
        }, nil
    }
    ```

#### [MODIFY] [agent_core_test.go](file:///shared/libs/go/wayfinder/agent_core_test.go)
*   **Description**: 空レスポンスのリトライ/失敗テスト (R3), ログ出力テスト (R5)
*   **Technical Design**:
    ```go
    func TestAgentCore_EmptyResponse_Retry(t *testing.T) {
        // MockLLM が1回目は空レスポンス、2回目は正常レスポンスを返す
        // -> リトライにより正常完了すること
        callCount := 0
        mockLLM := &MockLLM{
            GenerateFunc: func(...) (*LLMResponse, error) {
                callCount++
                if callCount == 1 {
                    return &LLMResponse{Content: "", StopReason: "end_turn"}, nil
                }
                return &LLMResponse{Content: "hello"}, nil
            },
        }
        // Run して結果が "hello" であることを検証
        // callCount == 2 であることを検証
    }

    func TestAgentCore_EmptyResponse_MaxRetry_Fails(t *testing.T) {
        // MockLLM が常に空レスポンスを返す
        // -> リトライ後にエラーを返し、セッションが StatusFailed になること
        mockLLM := &MockLLM{
            GenerateFunc: func(...) (*LLMResponse, error) {
                return &LLMResponse{Content: "", StopReason: "max_tokens"}, nil
            },
        }
        // Run してエラーが返ること、エラーメッセージに "empty response" を含むことを検証
    }
    ```

#### [MODIFY] [agent_core.go](file:///shared/libs/go/wayfinder/agent_core.go)
*   **Description**: (1) 空レスポンスのリトライ (R3), (2) LLM リクエスト/レスポンスの概要ログ (R5)
*   **Technical Design**:

    **(1) runSimple ループの変更** (L146-221):
    ```go
    func (ac *AgentCore) runSimple(ctx context.Context, toolDefs []ToolDefinition) (string, error) {
        emptyRetried := false // 空レスポンスのリトライフラグ (ループ外)

        for iteration := range maxIterations {
            // R5: LLM リクエストログ (Info)
            ac.logger.Info("LLM request",
                "iteration", iteration,
                "model", ac.config.LogicalModel,
                "messages_count", len(ac.messages))

            ac.applyCompaction()

            // LLM 呼び出し (既存ロジック)
            startTime := time.Now()
            var resp *LLMResponse
            var err error
            // ... streaming/non-streaming logic ...

            if err != nil {
                // ... existing error handling ...
            }

            // R5: LLM レスポンスログ (Info)
            elapsed := time.Since(startTime)
            ac.logger.Info("LLM response",
                "iteration", iteration,
                "duration_ms", elapsed.Milliseconds(),
                "stop_reason", resp.StopReason,
                "content_len", len(resp.Content),
                "tool_calls_count", len(resp.ToolCalls))

            // R3: 空レスポンスのリトライ
            if len(resp.ToolCalls) == 0 {
                if resp.Content == "" {
                    ac.logger.Warn("LLM returned empty response",
                        "iteration", iteration,
                        "stop_reason", resp.StopReason,
                        "model", ac.config.LogicalModel)
                    if resp.StopReason == "max_tokens" {
                        ac.logger.Warn("output may have been truncated; consider increasing max_output_tokens in model_profiles.yaml")
                    }
                    if !emptyRetried {
                        emptyRetried = true
                        continue
                    }
                    ac.saveSession(session.StatusFailed)
                    return "", fmt.Errorf("agent core: LLM returned empty response at iteration %d (stop_reason=%s)", iteration, resp.StopReason)
                }
                // ... existing completion logic (L176-189) ...
            }

            // ツール呼び出しが成功したらリトライフラグをリセット
            emptyRetried = false

            // ... existing tool call processing (L192-220) ...
        }
        // ... existing max iterations handling ...
    }
    ```
*   **Logic**:
    - `emptyRetried` はループの**外**で宣言。空レスポンスでリトライするのは最大1回。
    - ツール呼び出しが成功した場合はリトライフラグをリセットする (正常なイテレーションが挟まった場合、再度1回リトライ可能にする)。
    - `time.Now()` を LLM 呼び出し前に記録し、レスポンス受信後に `time.Since` で所要時間を計算。

---

### settings / testdata

#### [MODIFY] [demo/model_profiles.yaml](file:///settings/demo/model_profiles.yaml)
*   **Description**: `max_output_tokens` 設定例を追加
*   **Logic**: google プロバイダの gemini-2.5-flash に `max_output_tokens: 65536` を追加

#### [MODIFY] [example/model_profiles.yaml](file:///settings/example/model_profiles.yaml)
*   **Description**: スキーマコメントに `max_output_tokens` を追加

#### [MODIFY] [testdata/model_profiles.yaml](file:///tests/testdata/model_profiles.yaml)
*   **Description**: google プロバイダに `behavior.max_output_tokens: 65536` を追加 (E2E テストでの検証用)

---

## Step-by-Step Implementation Guide

### Step 1: config パッケージ -- MaxOutputTokens 追加

1. Edit [model_profiles_test.go](file:///shared/libs/go/config/model_profiles_test.go): `TestModelProfilesConfig_MaxOutputTokens` テストを追加。`MaxOutputTokens` の YAML パースを検証。
2. Edit [model_profiles.go](file:///shared/libs/go/config/model_profiles.go): `ModelBehavior` に `MaxOutputTokens int` フィールドを追加。
3. `git commit -m "feat(config): add MaxOutputTokens to ModelBehavior"`

- [x] テスト先行: テストを書いて fail を確認
- [x] 実装して pass を確認

### Step 2: handlerctx/routing -- MaxOutputTokens 伝達

1. Edit [routing_test.go](file:///shared/libs/go/llmgateway/routing_test.go): `TestResolveModel_MaxOutputTokens` テストを追加。
2. Edit [context.go](file:///shared/libs/go/llmgateway/handlerctx/context.go): `RoutedModel` に `MaxOutputTokens int` フィールドを追加。
3. Edit [routing.go](file:///shared/libs/go/llmgateway/routing.go): `ResolveModel` で `model.Behavior.MaxOutputTokens` を `RoutedModel.MaxOutputTokens` にコピー。
4. `git commit -m "feat(llmgateway): propagate MaxOutputTokens through routing"`

- [x] テスト先行: テストを書いて fail を確認
- [x] 実装して pass を確認

### Step 3: anthropic/handler -- MaxTokens オーバーライド

1. Edit [handler.go](file:///shared/libs/go/llmgateway/anthropic/handler.go): `handleMessagesViaBifrost` 内で `routed.MaxOutputTokens > 0` の場合に `fullReq.MaxTokens` をオーバーライド。
2. `git commit -m "feat(anthropic): override max_tokens from model profile"`

- [x] 実装後にビルド確認

### Step 4: wayfinder -- StopReason 伝達

1. Edit [bifrost_client_test.go](file:///shared/libs/go/wayfinder/bifrost_client_test.go): `TestBifrostClient_ParseResponse_StopReason`, `TestBifrostClient_ParseSSEStream_StopReason`, `TestBifrostClient_DefaultMaxTokens` テストを追加/更新。
2. Edit [llm_client.go](file:///shared/libs/go/wayfinder/llm_client.go): `LLMResponse` に `StopReason string` フィールドを追加。
3. Edit [bifrost_client.go](file:///shared/libs/go/wayfinder/bifrost_client.go): (a) デフォルト `max_tokens` を `16384` に変更、(b) `parseResponse` で `stop_reason` を取得、(c) `parseSSEStream` で `message_delta` の `stop_reason` を取得。
4. `git commit -m "feat(wayfinder): add StopReason to LLMResponse, update default max_tokens"`

- [x] テスト先行: テストを書いて fail を確認
- [x] 実装して pass を確認

### Step 5: wayfinder -- 空レスポンス対策とログ追加

1. Edit [agent_core_test.go](file:///shared/libs/go/wayfinder/agent_core_test.go): `TestAgentCore_EmptyResponse_Retry`, `TestAgentCore_EmptyResponse_MaxRetry_Fails` テストを追加。
2. Edit [agent_core.go](file:///shared/libs/go/wayfinder/agent_core.go): (a) `emptyRetried` フラグとリトライロジック、(b) LLM request/response の Info ログ、(c) 空レスポンス時の Warn ログ。
3. `git commit -m "feat(wayfinder): add empty response retry and LLM request/response logging"`

- [x] テスト先行: テストを書いて fail を確認
- [x] 実装して pass を確認

### Step 6: ログレベル改善

1. Edit [handler.go](file:///shared/libs/go/llmgateway/anthropic/handler.go): (a) L170, L190 の `log.Debug` を `log.Trace` に変更、(b) `handleMessagesBifrostStream` 冒頭に `startTime := time.Now()` を追加し完了ログを `log.Info` に昇格。
2. `git commit -m "refactor(llmgateway): improve log levels (Debug->Trace) and add stream duration"`

- [x] 実装後にビルド確認

### Step 7: Settings / Testdata 更新

1. Edit [demo/model_profiles.yaml](file:///settings/demo/model_profiles.yaml): gemini-2.5-flash に `max_output_tokens: 65536` を追加。
2. Edit [example/model_profiles.yaml](file:///settings/example/model_profiles.yaml): スキーマコメントに `max_output_tokens` を追記。
3. Edit [testdata/model_profiles.yaml](file:///tests/testdata/model_profiles.yaml): google プロバイダの gemini-2.5-flash に `behavior.max_output_tokens: 65536` を追加。
4. `git commit -m "docs: add max_output_tokens to model_profiles examples and testdata"`

- [x] 設定ファイル更新

### Step 8: ビルドとテスト

1. 全体ビルド + 単体テスト:
    ```bash
    ./scripts/process/build.sh
    ```
2. 統合テスト (Wayfinder E2E):
    ```bash
    ./scripts/process/integration_test.sh --specify "TestE2E_Wayfinder"
    ```
3. 総合判定の実施 (testing-rules.md 12)
4. `git push`

---

## Verification Plan

### Automated Verification

1. **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```
    - `config/model_profiles_test.go`: `TestModelProfilesConfig_MaxOutputTokens` -- MaxOutputTokens パース
    - `llmgateway/routing_test.go`: `TestResolveModel_MaxOutputTokens` -- routing 経路の伝達
    - `wayfinder/bifrost_client_test.go`: `TestBifrostClient_ParseResponse_StopReason`, `TestBifrostClient_ParseSSEStream_StopReason`, `TestBifrostClient_DefaultMaxTokens` -- StopReason パースとデフォルト値
    - `wayfinder/agent_core_test.go`: `TestAgentCore_EmptyResponse_Retry`, `TestAgentCore_EmptyResponse_MaxRetry_Fails` -- 空レスポンスリトライ

2. **Integration Tests**:
    ```bash
    ./scripts/process/integration_test.sh --specify "TestE2E_Wayfinder"
    ```
    - `TestE2E_Wayfinder_Health`: 正常系リグレッション
    - `TestE2E_Wayfinder_FullScenario_Gemini`: 正常系リグレッション (max_output_tokens 65536 が testdata に反映済み)
    - `TestE2E_Wayfinder_CompactionToolPairProtection`: compaction リグレッション
    - **Log Verification**: サーバーログに `"LLM request"` と `"LLM response"` の Info ログが出力されること。`duration_ms`, `stop_reason` が含まれること。

3. **E2E Tests (新規/追加)**:

    **E2E テストのコード追加は不要**。理由:
    - R1 (max_output_tokens): LLM Gateway 内部のパラメータ伝達であり、外部から観測可能な動作変更ではない。既存 E2E (`TestE2E_Wayfinder_FullScenario_Gemini`) で testdata の max_output_tokens が使われる経路が検証される。
    - R2 (StopReason): 内部構造体の拡張。単体テストで十分に検証可能。
    - R3 (空レスポンスリトライ): MockLLM を使った単体テストで検証する。実際の LLM で空レスポンスを再現することは不安定。
    - R4-R6 (ログ改善): ログレベル変更は機能変更ではない。

### テスト項目設計のセルフレビュー (11.4)

1. **網羅性の検証**: R1-R6 の全要件が単体テストまたは既存 E2E テストでカバーされている。
2. **証拠の十分性**: StopReason のパーステストは具体的な値 (`end_turn`, `max_tokens`, 空) を検証。空レスポンスリトライは callCount で実際にリトライされたことを確認。max_tokens のデフォルト値変更はリクエストボディの数値を直接検証。
3. **迂回・抜け道の排除**: routing テストで MaxOutputTokens がゼロの場合 (Behavior==nil) も検証し、フォールバックパスをカバー。
4. **依存関係の整合性**: config -> routing -> handler の順に依存するため、ボトムアップ順序で Step 1 -> 2 -> 3 と検証。

### 総合判定プロセス (12)

全テスト完了後、testing-rules.md 12.2 のチェック項目に従い総合判定を実施する。

## Documentation

#### [MODIFY] [052-LLM-Response-Stability-MaxTokens-Logging.md](file:///prompts/phases/000-foundation/branches/feat-llm-backend/ideas/052-LLM-Response-Stability-MaxTokens-Logging.md)
*   **更新内容**: 実装完了後、変更なし (仕様書は現状のまま)。
