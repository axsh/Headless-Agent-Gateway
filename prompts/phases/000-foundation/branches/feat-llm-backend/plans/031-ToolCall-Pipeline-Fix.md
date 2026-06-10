# 031-ToolCall-Pipeline-Fix

> **Source Specification**: [022-ToolCall-Pipeline-Fix.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/ideas/022-ToolCall-Pipeline-Fix.md)

## Goal Description

LLM Gatewayの変換層に存在する不備を修正し、Claude Code CLI経由でOpenAIモデル (gpt-5.3-codex等) を使用した際にTool Call (ファイル作成、編集など) が正常に動作するようにする。主な修正は: (1) Responses APIストリーム変換のstop_reason判定実装、(2) Chat Completions変換のstop_reason防御、(3) API Keyメタデータ伝達、(4) x-api-keyからのfallbackフラグ読み取り。

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: Responses APIストリーム変換 stop_reason | Proposed Changes > convert_a2r.go (ConvertResponsesStreamToAnthropic) |
| R2: Responses API非ストリーム変換 stop_reason | Proposed Changes > convert_a2r.go (ConvertResponsesResponseToAnthropic) - 既存実装の検証 |
| R3: Chat Completions stop_reason防御 | Proposed Changes > stream_converter.go (ConvertOpenAIStreamToAnthropic) |
| R4: API Keyメタデータ追加 | Proposed Changes > process.go (BuildEnv) |
| R5: ToolCallFallbackパラメータ伝達 | Proposed Changes > adapter_config.go + main.go |
| R7: maxTurnsデフォルト設定 | Proposed Changes > process.go (BuildArgs) |
| R8: x-api-keyからfallback読み取り | Proposed Changes > fallback.go + proxy_anthropic.go + proxy_openai.go |
| R6: ストリーミングFallback (任意) | 先送り: VV4でも未対応。本計画のスコープ外。 |

## Proposed Changes

### llmgateway パッケージ (変換層修正)

---

#### [MODIFY] [convert_a2r_test.go](file://shared/libs/go/llmgateway/convert_a2r_test.go)
*   **Description**: Responses API変換のstop_reason判定に関する単体テストを追加
*   **Technical Design**:
    ```go
    func TestConvertResponsesStreamToAnthropic_StopReasonToolUse(t *testing.T)
    func TestConvertResponsesStreamToAnthropic_StopReasonEndTurn(t *testing.T)
    func TestConvertResponsesResponseToAnthropic_StopReasonToolUse(t *testing.T)
    func TestConvertResponsesResponseToAnthropic_StopReasonEndTurn(t *testing.T)
    ```
*   **Logic (T1, T2)**:
    *   `TestConvertResponsesStreamToAnthropic_StopReasonToolUse`:
        - `response.output_item.added` (type=function_call) + `response.function_call_arguments.delta` + `response.function_call_arguments.done` + `response.completed` のSSEストリームを構築
        - `httptest.NewRecorder` に書き込んだ結果を解析
        - `message_delta` イベントの `stop_reason` が `"tool_use"` であることを検証
    *   `TestConvertResponsesStreamToAnthropic_StopReasonEndTurn`:
        - `response.content_part.added` + `response.output_text.delta` + `response.output_text.done` + `response.completed` のSSEストリーム (テキストのみ) を構築
        - `message_delta` イベントの `stop_reason` が `"end_turn"` であることを検証
    *   `TestConvertResponsesResponseToAnthropic_StopReasonToolUse`:
        - function_call出力を含むResponsesResponse JSONを構築
        - `ConvertResponsesResponseToAnthropic()` を呼び出し
        - 結果のstop_reasonが `"tool_use"` であることを検証
    *   `TestConvertResponsesResponseToAnthropic_StopReasonEndTurn`:
        - テキストのみのResponsesResponse JSONを構築
        - 結果のstop_reasonが `"end_turn"` であることを検証

---

#### [MODIFY] [convert_a2r.go](file://shared/libs/go/llmgateway/convert_a2r.go)
*   **Description**: R1 - `ConvertResponsesStreamToAnthropic` にfunction_callフラグ追跡を追加してstop_reasonを正しく判定
*   **Technical Design**:
    - 関数内のローカル変数 `hadFunctionCall bool` を追加
    - `response.output_item.added` ケースで `item.Type == "function_call"` の時に `hadFunctionCall = true` を設定
    - `response.completed` ケースで `hadFunctionCall` を参照してstop_reasonを決定
*   **Logic**:
    ```go
    // 関数冒頭に追加 (L307付近, messageSentやcontentBlockIndexと同列)
    hadFunctionCall := false

    // response.output_item.added ケース内 (L364付近)
    if item.Type == "function_call" {
        hadFunctionCall = true
        // 既存: sendMessageStart("") + content_block_start 送信
    }

    // response.completed ケース (L403-L412を修正)
    case "response.completed":
        stopReason := "end_turn"
        if hadFunctionCall {
            stopReason = "tool_use"
        }
        data := fmt.Sprintf(`{"type":"message_delta","delta":{"stop_reason":"%s"},"usage":{"output_tokens":0}}`, stopReason)
        writeSSE("message_delta", data)
        writeSSE("message_stop", `{"type":"message_stop"}`)
    ```
*   **R2検証**: `ConvertResponsesResponseToAnthropic()` (L244-L298) は既に `hasToolUse` フラグで正しく判定している。追加テストで動作を確認するのみ。

---

#### [MODIFY] [stream_converter_test.go](file://shared/libs/go/llmgateway/stream_converter_test.go)
*   **Description**: Chat Completions変換のstop_reason防御に関する単体テストを追加
*   **Technical Design**:
    ```go
    func TestConvertOpenAIStreamToAnthropic_StopReasonDefense_ToolUse(t *testing.T)
    func TestConvertOpenAIStreamToAnthropic_StopReasonDefense_NoToolUse(t *testing.T)
    ```
*   **Logic (T3)**:
    *   `TestConvertOpenAIStreamToAnthropic_StopReasonDefense_ToolUse`:
        - tool_callsを含むストリームで、finishReasonが`"stop"`のOpenAIチャンクを構築
        - `ConvertOpenAIStreamToAnthropic()` を呼び出し
        - 出力SSEのmessage_deltaでstop_reasonが`"tool_use"`になることを検証 (防御ロジック発動)
    *   `TestConvertOpenAIStreamToAnthropic_StopReasonDefense_NoToolUse`:
        - テキストのみのストリームで、finishReasonが`"stop"`のチャンクを構築
        - stop_reasonが`"end_turn"` (通常のmapFinishReason結果) であることを検証

---

#### [MODIFY] [stream_converter.go](file://shared/libs/go/llmgateway/stream_converter.go)
*   **Description**: R3 - `ConvertOpenAIStreamToAnthropic` にtool_calls検出時のstop_reason強制上書きロジックを追加
*   **Technical Design**:
    - [DONE]処理内 (L119-L137) で、`toolBlockStarted` フラグを参照してstop_reasonを防御的に上書き
*   **Logic**:
    ```go
    // L119-L123 を修正 (既存コードの直後に防御ロジック追加)
    // Send message_delta with stop_reason
    stopReason := "end_turn"
    if finishReason != "" {
        stopReason = mapFinishReason(finishReason)
    }
    // R3: Defense - force tool_use if tool blocks were detected
    if toolBlockStarted && stopReason != "tool_use" {
        stopReason = "tool_use"
    }
    ```

---

#### [MODIFY] [fallback_test.go](file://shared/libs/go/llmgateway/fallback_test.go)
*   **Description**: `ExtractFallbackFlag` 関数の単体テストを追加
*   **Technical Design**:
    ```go
    func TestExtractFallbackFlag(t *testing.T)
    ```
*   **Logic (T5)**:
    - テーブル駆動テスト:
    ```go
    tests := []struct {
        name   string
        header string
        want   bool
    }{
        {"with_fallback_true", "not-needed;fallback=true;sid=abc", true},
        {"with_fallback_false", "not-needed;fallback=false;sid=abc", false},
        {"no_fallback_part", "not-needed", false},
        {"empty_string", "", false},
        {"only_sid", "not-needed;sid=abc", false},
        {"bearer_prefix", "Bearer not-needed;fallback=true;sid=abc", true},
    }
    ```

---

#### [MODIFY] [fallback.go](file://shared/libs/go/llmgateway/fallback.go)
*   **Description**: R8 - `ExtractFallbackFlag` 関数を追加。x-api-keyヘッダーからfallbackフラグを抽出する。
*   **Technical Design**:
    ```go
    // ExtractFallbackFlag extracts the fallback flag from an x-api-key or Authorization header value.
    // The format is: "key;fallback=true;sid=SESSION_ID".
    // Returns true if fallback=true is found.
    func ExtractFallbackFlag(authHeader string) bool
    ```
*   **Logic**:
    ```go
    func ExtractFallbackFlag(authHeader string) bool {
        if strings.HasPrefix(authHeader, "Bearer ") {
            authHeader = strings.TrimPrefix(authHeader, "Bearer ")
        }
        for _, part := range strings.Split(authHeader, ";") {
            part = strings.TrimSpace(part)
            if part == "fallback=true" {
                return true
            }
        }
        return false
    }
    ```

---

#### [MODIFY] [proxy_anthropic.go](file://shared/libs/go/llmgateway/proxy_anthropic.go)
*   **Description**: R8 - `handleAnthropicMessages` でx-api-keyからfallbackフラグを読み取り、RoutedModelのToolCallFallbackにOR条件で適用
*   **Technical Design**:
    - L57 (`sessionID := ExtractSessionID(...)`) の直後にfallbackフラグ抽出を追加
*   **Logic**:
    ```go
    sessionID := ExtractSessionID(r.Header.Get("x-api-key"))

    // R8: Extract fallback flag from x-api-key header
    headerFallback := ExtractFallbackFlag(r.Header.Get("x-api-key"))

    routed, err := p.driver.router.ResolveModel(req.Model, sessionID)
    if err != nil {
        // ... 既存エラー処理
    }

    // R8: OR condition - enable fallback if either profile or header says so
    if headerFallback {
        routed.ToolCallFallback = true
    }
    ```

---

#### [MODIFY] [proxy_openai.go](file://shared/libs/go/llmgateway/proxy_openai.go)
*   **Description**: R8 - `handleOpenAIChatCompletions` でも同様にx-api-keyからfallbackフラグを適用
*   **Technical Design**:
    - sessionID抽出の直後にfallbackフラグ抽出を追加
*   **Logic**: proxy_anthropic.goと同等のパターン

---

### codingagent パッケージ (メタデータ伝達)

---

#### [MODIFY] [process_test.go](file://shared/libs/go/codingagent/claudecode/process_test.go)
*   **Description**: `BuildEnv` のAPI Keyメタデータと `BuildArgs` のmaxTurnsデフォルトに関する単体テストを追加
*   **Technical Design**:
    ```go
    func TestBuildEnv_APIKeyMetadata(t *testing.T)
    func TestBuildArgs_MaxTurnsDefault(t *testing.T)
    ```
*   **Logic (T4)**:
    *   `TestBuildEnv_APIKeyMetadata`:
        - テーブル駆動テスト:
        ```go
        tests := []struct {
            name             string
            toolCallFallback bool
            sessionID        string
            wantContains     string
        }{
            {"fallback_true_with_sid", true, "sess-123", ";fallback=true;sid=sess-123"},
            {"fallback_false_with_sid", false, "sess-456", ";fallback=false;sid=sess-456"},
            {"fallback_true_no_sid", true, "", ";fallback=true;sid=default"},
        }
        ```
        - BuildEnv結果からANTHROPIC_API_KEY=... の行を探し、期待文字列が含まれることを検証
    *   `TestBuildArgs_MaxTurnsDefault`:
        - MaxTurns=0のSessionConfigでBuildArgs()を呼び出し
        - `--max-turns 200` が引数に含まれることを検証
        - MaxTurns=50のSessionConfigで呼び出し、`--max-turns 50` が含まれることを検証

---

#### [MODIFY] [adapter_config.go](file://shared/libs/go/codingagent/adapter_config.go)
*   **Description**: R5 - AdapterConfigに`ToolCallFallback`フィールドを追加
*   **Technical Design**:
    ```go
    type AdapterConfig struct {
        // ... 既存フィールド

        // ToolCallFallback enables text-to-tool-call conversion in the Gateway.
        // When true, the ANTHROPIC_API_KEY includes ";fallback=true" metadata
        // so the gateway proxy can apply fallback logic for models that
        // sometimes emit tool calls as text instead of proper function_call.
        ToolCallFallback bool
    }
    ```

---

#### [MODIFY] [process.go](file://shared/libs/go/codingagent/claudecode/process.go)
*   **Description**: R4 - `BuildEnv`でAPI Keyにsid/fallbackメタデータを追加。R7 - `BuildArgs`でmaxTurnsデフォルト設定。
*   **Technical Design**:
    - `BuildEnv`: ANTHROPIC_API_KEYの値にメタデータを付加
    - `BuildArgs`: MaxTurns==0の場合にデフォルト200を設定
*   **Logic (BuildEnv)**:
    ```go
    func BuildEnv(ac *codingagent.AdapterConfig, cfg *codingagent.SessionConfig) []string {
        env := make(map[string]string)

        if ac.GatewayURL != "" {
            env["ANTHROPIC_BASE_URL"] = ac.GatewayURL

            // R4: Build API key with metadata for gateway
            apiKey := "not-needed"
            fallbackStr := "false"
            if ac.ToolCallFallback {
                fallbackStr = "true"
            }
            sid := cfg.SessionID
            if sid == "" {
                sid = "default"
            }
            env["ANTHROPIC_API_KEY"] = apiKey + ";fallback=" + fallbackStr + ";sid=" + sid
        }

        // ... 残りは既存ロジックをそのまま維持
    }
    ```
*   **Logic (BuildArgs)**:
    ```go
    // R7: maxTurns のデフォルト設定 (既存 L45-L47 を修正)
    maxTurns := cfg.MaxTurns
    if maxTurns == 0 {
        maxTurns = 200 // VV4 equivalent default
    }
    args = append(args, "--max-turns", strconv.Itoa(maxTurns))
    ```
    既存のL45-L47:
    ```go
    if cfg.MaxTurns > 0 {
        args = append(args, "--max-turns", strconv.Itoa(cfg.MaxTurns))
    }
    ```
    を上記に置き換える。

---

### examples/standalone (設定注入)

---

#### [MODIFY] [main.go](file://examples/standalone/main.go)
*   **Description**: R5 - registerCodingAgentsでToolCallFallback設定を注入
*   **Technical Design**:
    - DefaultModel取得後、model_profilesからそのモデルのbehavior.tool_call_fallback設定を取得してAdapterConfigに反映
*   **Logic**:
    ```go
    func registerCodingAgents(srv *hag.Server) {
        if _, err := exec.LookPath("claude"); err == nil {
            gwURL := srv.Gateway().ProxyURL()

            defaultModel := ""
            toolCallFallback := false
            if dm := srv.Gateway().DefaultModel(); dm != nil {
                defaultModel = dm.Model
                toolCallFallback = dm.ToolCallFallback  // from model profile behavior
            }

            adapter := claudecode.New(&codingagent.AdapterConfig{
                GatewayURL:       gwURL,
                DefaultModel:     defaultModel,
                ToolCallFallback: toolCallFallback,
            })
            srv.AgentService().RegisterAgent(adapter)

            fmt.Printf("Registered coding agent: claudecode (gateway=%s, default_model=%s, fallback=%v)\n",
                gwURL, defaultModel, toolCallFallback)
        } else {
            fmt.Println("Warning: claude CLI not found, claudecode agent not registered")
        }
    }
    ```

> [!NOTE]
> `srv.Gateway().DefaultModel()` が返す型に `ToolCallFallback` フィールドが必要。現在の `DefaultModel()` が `*RoutedModel` を返す場合は対応済み。`*config.DefaultProfileConfig` を返す場合は、RoutedModelを返すメソッドを追加するか、直接profilesから設定を取得する必要がある。実装時にインターフェースを確認して適切に対応する。

---

## Step-by-Step Implementation Guide

### Step 1: 単体テスト作成 (TDD - テスト先行)

1. `shared/libs/go/llmgateway/convert_a2r_test.go` に `TestConvertResponsesStreamToAnthropic_StopReasonToolUse` と `TestConvertResponsesStreamToAnthropic_StopReasonEndTurn` を追加
2. `shared/libs/go/llmgateway/convert_a2r_test.go` に `TestConvertResponsesResponseToAnthropic_StopReasonToolUse` と `TestConvertResponsesResponseToAnthropic_StopReasonEndTurn` を追加
3. `shared/libs/go/llmgateway/stream_converter_test.go` に `TestConvertOpenAIStreamToAnthropic_StopReasonDefense_ToolUse` と `NoToolUse` を追加
4. `shared/libs/go/llmgateway/fallback_test.go` に `TestExtractFallbackFlag` を追加
5. `shared/libs/go/codingagent/claudecode/process_test.go` に `TestBuildEnv_APIKeyMetadata` と `TestBuildArgs_MaxTurnsDefault` を追加
6. ビルドしてテストがFAIL (未実装の関数・ロジック不足) であることを確認

### Step 2: convert_a2r.go 修正 (R1)

1. `ConvertResponsesStreamToAnthropic()` に `hadFunctionCall` フラグ変数を追加
2. `response.output_item.added` ケースで `hadFunctionCall = true` を設定
3. `response.completed` ケースでstop_reason判定ロジックを実装
4. テスト実行: R1関連テストがPASSすることを確認

### Step 3: stream_converter.go 修正 (R3)

1. `ConvertOpenAIStreamToAnthropic()` の [DONE] 処理に防御ロジックを追加
2. テスト実行: R3関連テストがPASSすることを確認

### Step 4: fallback.go 修正 (R8)

1. `ExtractFallbackFlag()` 関数を追加
2. テスト実行: R8関連テストがPASSすることを確認

### Step 5: proxy_anthropic.go / proxy_openai.go 修正 (R8)

1. 両ハンドラにfallbackフラグ抽出ロジックを追加
2. RoutedModelのToolCallFallbackにOR条件で適用

### Step 6: adapter_config.go 修正 (R5)

1. `ToolCallFallback bool` フィールドをAdapterConfigに追加

### Step 7: process.go 修正 (R4, R7)

1. `BuildEnv()` でAPI Keyにメタデータを付加
2. `BuildArgs()` でmaxTurnsデフォルト200を設定
3. テスト実行: R4, R7関連テストがPASSすることを確認

### Step 8: main.go 修正 (R5)

1. `registerCodingAgents()` でToolCallFallback設定をAdapterConfigに注入

### Step 9: ビルドと全テスト実行

1. `./scripts/process/build.sh` で全体ビルド
2. 全単体テストがPASSすることを確認
3. 統合テスト実行

---

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```

2.  **Integration Tests**:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories llm
    ```
    *   **Log Verification**:
        - Gateway変換ログでstop_reasonが正しく設定されているか確認
        - API Keyのメタデータ (fallback, sid) がログに記録されているか確認

3.  **Regression Tests**:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories common,llm
    ```

4.  **E2E Tests**:

    本変更はLLM Gatewayの変換層とCLIの環境変数設定という内部処理の修正であり、E2Eテストの追加は以下の理由により省略する:

    - 既存E2Eテスト `TestE2E_CodingAgentStreaming` (TC-002) がファイル作成のE2E検証を行っており、本修正が正しく動作すればこのテストが引き続きPASSする
    - 本修正の核心はGatewayのSSE変換ロジック (純粋な入出力変換) であり、単体テストで十分に検証可能
    - OpenAIモデル固有のE2Eテスト (TC-006相当) は実APIキーが必要であり、CI環境での自動実行に制約がある

### テスト項目設計のセルフレビュー (S11)

#### 11.3 観点チェックリスト

| # | 観点 | 対応状況 |
|---|------|----------|
| 1 | 正常系の動作確認 | T1 (ストリームtool_use), T2 (非ストリームtool_use), T3 (CC tool_use防御) |
| 2 | 異常系・境界値 | T1 (end_turn), T3 (ツールなし), T5 (空文字列、パートなし) |
| 3 | 外部連携の実動作 | 統合テスト (llm) で実LLMとの連携を確認 |
| 4 | データの一貫性 | T1/T2: 入力SSE→変換→出力SSEの双方向確認 |
| 5 | 状態遷移の検証 | T1: hadFunctionCallフラグの状態追跡 |
| 6 | 設定・構成の反映 | T4: API Keyメタデータ, T5: fallbackフラグ抽出 |
| 7 | 副作用の確認 | T4: 既存環境変数への影響がないこと |

#### 11.4 セルフレビュー

1. **網羅性**: R1-R5, R7-R8の全要件に対応するテストが存在する。R6 (ストリーミングFallback) はスコープ外として明示的に除外。
2. **証拠の十分性**: 各テストは「stop_reasonが正しい値である」「API Keyに正しいメタデータが含まれる」など、具体的な値の検証を行っている。
3. **迂回排除**: ストリーム変換テストではSSEの実際のテキスト出力を解析しており、別経路での処理は不可能。
4. **依存関係**: ボトムアップ順序: ExtractFallbackFlag (末端) → ConvertResponsesStreamToAnthropic (中間) → BuildEnv (アダプター) → proxy_anthropic.go (上位)

### 総合判定プロセス (S12)

全テスト完了後、S12.2のチェック項目7点を確認し、総合判定結果をウォークスルーに記載する。
