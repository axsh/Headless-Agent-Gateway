# 058-Compaction-ToolPair-Protection-And-Streaming

> **Source Specification**: [047-Compaction-ToolPair-Protection-And-Streaming.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/ideas/047-Compaction-ToolPair-Protection-And-Streaming.md)

## Goal Description

wayfinder エージェントの3つの問題を修正する:
1. context compaction がツール呼び出しペアを分断し、LLM API 400 エラーを引き起こすバグを修正
2. defaultSummarizer を文字列クリッピングから LLM ベースの真の要約に置換
3. BifrostClient にストリーミング対応を追加し、テキストデルタのリアルタイム配信を実現

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: Compaction スライディングウィンドウのツールペア保護 | Proposed Changes > session > compaction.go |
| R2: defaultSummarizer の LLM ベース要約への置換 | Proposed Changes > wayfinder > agent_core.go |
| R3: BifrostClient のストリーミング対応 | Proposed Changes > wayfinder > bifrost_client.go, llm_client.go, agent_core.go |
| R4: ストリーミングの設定可能性 (Nice to Have) | 本計画では先送り。R3 の基盤が確立された後に設定化を検討する |

---

## Proposed Changes

### session パッケージ (Compaction ツールペア保護)

#### [MODIFY] [compaction_test.go](file://shared/libs/go/wayfinder/session/compaction_test.go)
*   **Description**: ツールペア保護のテストケースを追加（TDD: テストを先に書く）
*   **Technical Design**:
    ```go
    func TestCompact_ToolPairNotSplit(t *testing.T)
    func TestCompact_MultipleToolResultsNotSplit(t *testing.T)
    func TestCompact_BoundaryAdjustmentWithConsecutiveToolMessages(t *testing.T)
    func TestCompact_NoToolMessages_NoAdjustment(t *testing.T)
    func TestAdjustBoundaryForToolPairs(t *testing.T)
    func TestValidateToolPairIntegrity(t *testing.T)
    ```
*   **Logic**:
    *   `TestCompact_ToolPairNotSplit`: MaxTurns=4 で以下のメッセージ列を構築:
        ```
        [0] user: "prompt1"
        [1] assistant: "response" + ToolCalls: [{ID:"tc1", Name:"edit_file"}]
        [2] tool: "File edited" (ToolCallID:"tc1")
        [3] assistant: "done"
        [4] user: "prompt2"
        [5] assistant: "response2"
        [6] user: "prompt3"
        [7] assistant: "response3"
        ```
        windowSize = max(4/2, 4) = 4 なので初期境界 = 8-4 = 4。recentMessages = [4,5,6,7]。
        ツールペア分断なし。compaction 後、`tool` メッセージの直前に assistant(ToolCalls非空) が存在しないことを確認。
    *   `TestCompact_MultipleToolResultsNotSplit`: MaxTurns=4 で以下のメッセージ列を構築:
        ```
        [0] user: "prompt1"
        [1] assistant: "response" + ToolCalls: [{ID:"tc1", Name:"edit_file"}, {ID:"tc2", Name:"execute_command"}]
        [2] tool: "File edited" (ToolCallID:"tc1")
        [3] tool: "Command executed" (ToolCallID:"tc2")
        [4] assistant: "done"
        [5] user: "prompt2"
        [6] assistant: "response2"
        [7] user: "prompt3"
        [8] assistant: "response3"
        ```
        windowSize=4、初期境界=9-4=5。recentMessages=[5,6,7,8]。分断なし。
        境界を3にずらすケース: windowSize を調整して境界が [2]tool に当たる状態を作り、[1]assistant まで含まれることを検証。
    *   `TestCompact_BoundaryAdjustmentWithConsecutiveToolMessages`: 境界が tool メッセージの中間に当たった場合、連続する全 tool メッセージと対応する assistant まで含めて境界がずれることを検証
    *   `TestCompact_NoToolMessages_NoAdjustment`: ツールメッセージがない場合は境界調整が発生しないことを検証
    *   `TestAdjustBoundaryForToolPairs`: ヘルパー関数 `adjustBoundaryForToolPairs` の単体テスト。各種境界位置でのずれ量を検証
    *   `TestValidateToolPairIntegrity`: `validateToolPairIntegrity` 関数の単体テスト。正常ケースと異常ケース（孤立した tool メッセージ）を検証

---

#### [MODIFY] [compaction.go](file://shared/libs/go/wayfinder/session/compaction.go)
*   **Description**: `Compact()` にツールペア保護の境界調整ロジックを追加
*   **Technical Design**:
    ```go
    // adjustBoundaryForToolPairs adjusts the sliding window boundary
    // to avoid splitting tool call pairs (assistant+tool_calls -> tool results).
    // If the boundary falls on a tool message, it shifts backward to include
    // the corresponding assistant message with tool calls.
    func adjustBoundaryForToolPairs(unpinned []Message, boundary int) int

    // validateToolPairIntegrity checks that every tool message has
    // a preceding assistant message with matching tool calls.
    // Returns true if the message list is valid.
    func validateToolPairIntegrity(messages []Message) bool
    ```
*   **Logic**:
    *   `adjustBoundaryForToolPairs`:
        1. `boundary` が 0 以下の場合はそのまま返す
        2. `unpinned[boundary]` のロールを確認
        3. `unpinned[boundary].Role == "tool"` の場合:
            a. `boundary` を1つ前にデクリメント
            b. `boundary >= 0` かつ `unpinned[boundary].Role == "tool"` の間、デクリメントを繰り返す（連続 tool 対応）
            c. `boundary >= 0` かつ `unpinned[boundary].Role == "assistant"` かつ `len(unpinned[boundary].ToolCalls) > 0` の場合、そこが正しい境界
            d. 上記条件に合わない場合（データ不整合）、元の boundary をそのまま返す（安全側に倒す）
        4. `boundary < 0` の場合は 0 を返す（全メッセージを recent に含める）
    *   `validateToolPairIntegrity`:
        1. メッセージ列を走査
        2. `Role == "tool"` のメッセージを見つけた場合、その直前のメッセージが `Role == "assistant"` かつ `ToolCalls` が非空であることを確認
        3. 直前が別の `tool` メッセージの場合、さらに遡って assistant を探す
        4. 不正なペアが見つかった場合は `false` を返す
    *   `Compact()` の変更:
        1. 既存の境界計算後に `adjustBoundaryForToolPairs` を呼び出す
        2. compaction 適用後の結果に `validateToolPairIntegrity` を実行
        3. 検証失敗の場合はログ警告を出し、元のメッセージ列をそのまま返す

---

### wayfinder パッケージ (LLM ベース要約 + ストリーミング)

#### [MODIFY] [agent_core_test.go](file://shared/libs/go/wayfinder/agent_core_test.go)
*   **Description**: defaultSummarizer と structuredFallbackSummary のテストを追加（TDD）
*   **Technical Design**:
    ```go
    func TestDefaultSummarizer_CallsLLM(t *testing.T)
    func TestDefaultSummarizer_FallbackOnLLMError(t *testing.T)
    func TestStructuredFallbackSummary_IncludesToolInfo(t *testing.T)
    func TestStructuredFallbackSummary_IncludesToolResults(t *testing.T)
    func TestBuildConversationLog_StructuredFormat(t *testing.T)
    ```
*   **Logic**:
    *   `TestDefaultSummarizer_CallsLLM`: モック LLMClient を使い、summarizer が LLM を呼び出すことを検証。プロンプトに "conversation summarizer" が含まれることを確認
    *   `TestDefaultSummarizer_FallbackOnLLMError`: LLM がエラーを返した場合、`structuredFallbackSummary` の結果が返ることを検証
    *   `TestStructuredFallbackSummary_IncludesToolInfo`: ToolCalls 付き assistant メッセージのツール名がフォールバック出力に含まれることを検証
    *   `TestStructuredFallbackSummary_IncludesToolResults`: tool ロールメッセージの結果がフォールバック出力に含まれることを検証
    *   `TestBuildConversationLog_StructuredFormat`: `buildConversationLog` ヘルパーの出力フォーマットが正しいことを検証

---

#### [MODIFY] [agent_core.go](file://shared/libs/go/wayfinder/agent_core.go)
*   **Description**: `defaultSummarizer` を LLM ベースの要約に置換、`structuredFallbackSummary` を追加、`runSimple` にストリーミング分岐を追加
*   **Technical Design**:
    *   新規関数:
        ```go
        func (ac *AgentCore) buildConversationLog(msgs []session.Message) string
        func (ac *AgentCore) structuredFallbackSummary(msgs []session.Message) string
        func truncateWithEllipsis(s string, maxLen int) string
        ```
    *   変更関数:
        ```go
        func (ac *AgentCore) defaultSummarizer(msgs []session.Message) (string, error)  // LLM ベースに置換
        func (ac *AgentCore) runSimple(ctx context.Context, prompt string) (string, error)  // ストリーミング分岐追加
        ```
*   **Logic**:
    *   `buildConversationLog`: メッセージ列を構造化テキストに変換する。仕様書のロジックをそのまま実装:
        ```go
        func (ac *AgentCore) buildConversationLog(msgs []session.Message) string {
            var b strings.Builder
            for _, m := range msgs {
                switch m.Role {
                case "user":
                    b.WriteString(fmt.Sprintf("USER: %s\n", m.Content))
                case "assistant":
                    b.WriteString(fmt.Sprintf("ASSISTANT: %s\n", m.Content))
                    for _, tc := range m.ToolCalls {
                        b.WriteString(fmt.Sprintf("  [TOOL CALL: %s (id=%s)]\n", tc.Name, tc.ID))
                    }
                case "tool":
                    b.WriteString(fmt.Sprintf("  [TOOL RESULT (id=%s): %s]\n", m.ToolCallID, m.Content))
                }
            }
            return b.String()
        }
        ```
    *   `defaultSummarizer`: LLM に要約を依頼する。仕様書のプロンプトをそのまま使用:
        ```go
        func (ac *AgentCore) defaultSummarizer(msgs []session.Message) (string, error) {
            conversationLog := ac.buildConversationLog(msgs)
            summaryPrompt := []ChatMessage{
                {Role: "system", Content: summarizationSystemPrompt},
                {Role: "user", Content: "Summarize this conversation:\n\n" + conversationLog},
            }
            resp, err := ac.llm.GenerateMessage(context.Background(), ac.config.LogicalModel, summaryPrompt, nil)
            if err != nil {
                ac.logger.Warn("LLM summarization failed, using structured fallback", "error", err.Error())
                return ac.structuredFallbackSummary(msgs), nil
            }
            return resp.Content, nil
        }
        ```
        `summarizationSystemPrompt` はパッケージレベル定数:
        ```go
        const summarizationSystemPrompt = `You are a conversation summarizer. Summarize the following conversation concisely.
        Rules:
        - Preserve the meaning and intent of user requests and assistant responses.
        - MUST preserve all tool call names and their outcomes (success/failure/key results).
        - MUST preserve specific file paths, command outputs, and operation results.
        - Keep causal relationships between user requests and assistant actions.
        - Output in the same language as the conversation.
        - Be concise but do not lose important facts.`
        ```
    *   `structuredFallbackSummary`: 仕様書のフォールバックロジックをそのまま実装
    *   `truncateWithEllipsis`:
        ```go
        func truncateWithEllipsis(s string, maxLen int) string {
            if len(s) <= maxLen { return s }
            return s[:maxLen] + "..."
        }
        ```
    *   `runSimple` の変更: LLM 呼び出し部分でストリーミング分岐を追加:
        ```go
        var resp *LLMResponse
        var err error
        if streamClient, ok := ac.llm.(StreamingLLMClient); ok && ac.emitter != nil {
            onDelta := func(delta string) {
                ac.emitter.Emit(codingagent.StreamEvent{
                    Type:    codingagent.EventText,
                    Content: delta,
                })
            }
            resp, err = streamClient.GenerateMessageStream(ctx, ac.config.LogicalModel, ac.messages, toolDefs, onDelta)
        } else {
            resp, err = ac.llm.GenerateMessage(ctx, ac.config.LogicalModel, ac.messages, toolDefs)
        }
        ```

---

#### [MODIFY] [llm_client.go](file://shared/libs/go/wayfinder/llm_client.go)
*   **Description**: `StreamingLLMClient` インターフェースを追加
*   **Technical Design**:
    ```go
    // StreamingLLMClient extends LLMClient with streaming support.
    type StreamingLLMClient interface {
        LLMClient
        // GenerateMessageStream sends a streaming request and calls onDelta
        // for each text delta chunk. Returns the final complete response
        // (including any tool calls) after the stream ends.
        GenerateMessageStream(
            ctx context.Context,
            logicalModel string,
            messages []ChatMessage,
            tools []ToolDefinition,
            onDelta func(textDelta string),
        ) (*LLMResponse, error)
    }
    ```
*   **Logic**: インターフェース定義のみ。実装は `BifrostClient` で行う。

---

#### [MODIFY] [bifrost_client_test.go](file://shared/libs/go/wayfinder/bifrost_client_test.go)
*   **Description**: ストリーミングパースのテストを追加（TDD）
*   **Technical Design**:
    ```go
    func TestBifrostClient_GenerateMessageStream_TextOnly(t *testing.T)
    func TestBifrostClient_GenerateMessageStream_WithToolCalls(t *testing.T)
    func TestBifrostClient_GenerateMessageStream_ServerError(t *testing.T)
    func TestBifrostClient_BuildRequestBody_WithStream(t *testing.T)
    ```
*   **Logic**:
    *   `TestBifrostClient_GenerateMessageStream_TextOnly`: httptest.Server で Anthropic SSE レスポンスをモック。`event: content_block_delta` + `data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"Hello"}}` のイベント列を返す。`onDelta` が "Hello" で呼ばれること、最終 `LLMResponse.Content` が "Hello" であることを検証
    *   `TestBifrostClient_GenerateMessageStream_WithToolCalls`: SSE で `content_block_start` (tool_use) + `content_block_delta` (input_json_delta) + `content_block_stop` を返す。`LLMResponse.ToolCalls` にツール情報が含まれることを検証
    *   `TestBifrostClient_GenerateMessageStream_ServerError`: サーバーが 500 を返した場合のエラーハンドリングを検証
    *   `TestBifrostClient_BuildRequestBody_WithStream`: `buildStreamRequestBody` が `"stream": true` を含むことを検証

---

#### [MODIFY] [bifrost_client.go](file://shared/libs/go/wayfinder/bifrost_client.go)
*   **Description**: `GenerateMessageStream` メソッドを追加し、`StreamingLLMClient` インターフェースを実装
*   **Technical Design**:
    ```go
    func (bc *BifrostClient) GenerateMessageStream(
        ctx context.Context,
        logicalModel string,
        messages []ChatMessage,
        tools []ToolDefinition,
        onDelta func(textDelta string),
    ) (*LLMResponse, error)

    func (bc *BifrostClient) buildStreamRequestBody(
        model string,
        messages []ChatMessage,
        toolDefs []ToolDefinition,
    ) map[string]any

    func (bc *BifrostClient) parseSSEStream(
        body io.Reader,
        onDelta func(textDelta string),
    ) (*LLMResponse, error)
    ```
*   **Logic**:
    *   `buildStreamRequestBody`: 既存の `buildRequestBody` と同じロジックだが `"stream": true` を追加
    *   `GenerateMessageStream`:
        1. `buildStreamRequestBody` でリクエストボディを構築
        2. `POST /v1/messages` に送信（`Accept: text/event-stream` ヘッダー付き）
        3. レスポンスステータスが 200 以外の場合はエラーを返す
        4. `parseSSEStream` でレスポンスボディをパース
    *   `parseSSEStream`:
        1. `bufio.Scanner` で行ごとに読み取り
        2. `event: ` 行でイベントタイプを取得
        3. `data: ` 行で JSON ペイロードを取得
        4. イベントタイプ別の処理:
            - `content_block_delta` + `delta.type == "text_delta"`: `onDelta(delta.text)` を呼び出し、テキストを蓄積
            - `content_block_start` + `content_block.type == "tool_use"`: tool_use ブロックの開始を記録（id, name）
            - `content_block_delta` + `delta.type == "input_json_delta"`: ツール引数 JSON を蓄積
            - `content_block_stop`: ツール引数の蓄積を完了
            - `message_stop` / `[DONE]`: ストリーム終了
        5. 蓄積したテキストとツールコールから `LLMResponse` を構築して返す

---

### E2E テスト

#### [MODIFY] [wayfinder_e2e_test.go](file://tests/wayfinder_e2e_test.go)
*   **Description**: compaction ツールペア保護の E2E テストを追加
*   **Technical Design**:
    ```go
    func TestE2E_Wayfinder_CompactionToolPairProtection(t *testing.T)
    ```
*   **Logic**:
    *   E2E テストサーバーを起動
    *   セッションを作成し、ツール呼び出しを誘発する複数のプロンプトを連続送信:
        1. `"Create a file named test1.txt with content 'hello'"` -- ファイル作成でツール呼び出し発生
        2. `"Read the file test1.txt and tell me its content"` -- 再度ツール呼び出し
        3. `"Create a file named test2.txt with content 'world'"` -- 3回目
        4. `"Create a file named test3.txt with content 'foo'"` -- 4回目
        5. `"What files exist in the directory? List them."` -- 5回目（ここで compaction が発動する可能性あり）
    *   最終プロンプトが 400 エラーなしで応答を返すことを検証
    *   作成されたファイルが実際に存在することを副作用として検証
    *   **E2Eテストが不要でない理由**: compaction のバグは unit test だけでは完全に再現できず、実際の LLM レスポンスによるツール呼び出しパターンでの検証が必要

---

## Step-by-Step Implementation Guide

### Phase A: Compaction ツールペア保護

- [ ] **Step 1: compaction テストを追加** (TDD - Failed First)
    *   Edit `shared/libs/go/wayfinder/session/compaction_test.go`
    *   `TestAdjustBoundaryForToolPairs` を追加: 各種境界位置での調整結果を検証するテーブル駆動テスト
    *   `TestValidateToolPairIntegrity` を追加: 正常ケースと異常ケース
    *   `TestCompact_ToolPairNotSplit` を追加: 単一ツールペアの保護
    *   `TestCompact_MultipleToolResultsNotSplit` を追加: 複数ツール結果の保護
    *   `TestCompact_BoundaryAdjustmentWithConsecutiveToolMessages` を追加
    *   `TestCompact_NoToolMessages_NoAdjustment` を追加
    *   ビルド実行して全テストが FAIL することを確認

- [ ] **Step 2: compaction.go にツールペア保護を実装**
    *   Edit `shared/libs/go/wayfinder/session/compaction.go`
    *   `adjustBoundaryForToolPairs` 関数を追加
    *   `validateToolPairIntegrity` 関数を追加
    *   `Compact()` 関数内で `adjustBoundaryForToolPairs` を呼び出す（境界計算後）
    *   `Compact()` 関数内で `validateToolPairIntegrity` を compaction 結果に適用
    *   ビルド実行して Step 1 のテストが PASS することを確認
    *   **Git commit**: `feat: add tool pair boundary protection to compaction`

### Phase B: LLM ベース要約

- [ ] **Step 3: summarizer テストを追加** (TDD - Failed First)
    *   Edit `shared/libs/go/wayfinder/agent_core_test.go`
    *   `TestDefaultSummarizer_CallsLLM` を追加
    *   `TestDefaultSummarizer_FallbackOnLLMError` を追加
    *   `TestStructuredFallbackSummary_IncludesToolInfo` を追加
    *   `TestStructuredFallbackSummary_IncludesToolResults` を追加
    *   `TestBuildConversationLog_StructuredFormat` を追加
    *   ビルド実行して全テストが FAIL することを確認

- [ ] **Step 4: agent_core.go に LLM ベース要約を実装**
    *   Edit `shared/libs/go/wayfinder/agent_core.go`
    *   `import` に `"context"`, `"strings"` を追加
    *   `summarizationSystemPrompt` 定数を追加
    *   `buildConversationLog` メソッドを追加
    *   `truncateWithEllipsis` 関数を追加
    *   `structuredFallbackSummary` メソッドを追加
    *   `defaultSummarizer` を LLM 呼び出し版に置き換え
    *   ビルド実行して Step 3 のテストが PASS することを確認
    *   **Git commit**: `feat: replace string-clipping summarizer with LLM-based summarization`

### Phase C: BifrostClient ストリーミング対応

- [ ] **Step 5: StreamingLLMClient インターフェースを追加**
    *   Edit `shared/libs/go/wayfinder/llm_client.go`
    *   `StreamingLLMClient` インターフェースを追加
    *   **Git commit**: `feat: add StreamingLLMClient interface`

- [ ] **Step 6: BifrostClient ストリーミングテストを追加** (TDD - Failed First)
    *   Edit `shared/libs/go/wayfinder/bifrost_client_test.go`
    *   `TestBifrostClient_BuildRequestBody_WithStream` を追加
    *   `TestBifrostClient_GenerateMessageStream_TextOnly` を追加
    *   `TestBifrostClient_GenerateMessageStream_WithToolCalls` を追加
    *   `TestBifrostClient_GenerateMessageStream_ServerError` を追加
    *   ビルド実行して全テストが FAIL することを確認

- [ ] **Step 7: BifrostClient にストリーミングを実装**
    *   Edit `shared/libs/go/wayfinder/bifrost_client.go`
    *   `buildStreamRequestBody` メソッドを追加
    *   `parseSSEStream` メソッドを追加
    *   `GenerateMessageStream` メソッドを追加
    *   ビルド実行して Step 6 のテストが PASS することを確認
    *   **Git commit**: `feat: add streaming support to BifrostClient`

- [ ] **Step 8: AgentCore にストリーミング分岐を追加**
    *   Edit `shared/libs/go/wayfinder/agent_core.go`
    *   `runSimple` の LLM 呼び出し部分にストリーミング分岐を追加
    *   ビルド実行して既存テストが PASS することを確認
    *   **Git commit**: `feat: use streaming LLM calls in AgentCore when available`

### Phase D: E2E テスト + 検証

- [ ] **Step 9: E2E テストを追加**
    *   Edit `tests/wayfinder_e2e_test.go`
    *   `TestE2E_Wayfinder_CompactionToolPairProtection` を追加
    *   **Git commit**: `test: add E2E test for compaction tool pair protection`

- [ ] **Step 10: 全体ビルド + テスト実行**
    *   Verification Plan の全コマンドを実行

---

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```

2.  **Integration Tests** (wayfinder 関連):
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories taskengine --specify "Wayfinder"
    ```
    *   **Log Verification**:
        *   `compaction` 関連のログに `WARN` が出力されていないこと
        *   `bifrost non-stream anthropic request` ではなく `bifrost stream anthropic request` のログが出力されること（ストリーミング有効時）

3.  **E2E Tests (新規/追加)**:

    #### [NEW] TestE2E_Wayfinder_CompactionToolPairProtection
    *   **テストケース**: ツール呼び出しを伴う複数のプロンプトを連続送信し、compaction が発動してもエラーが発生しないことを検証
    *   **検証ポイント**:
        *   全プロンプトが HTTP 200 で応答を返すこと
        *   ツール呼び出しで作成されたファイルが実際に存在すること
        *   400 エラー（`function response turn` エラー）が発生しないこと

    **E2Eテストが不要でない理由**: compaction のバグは unit test で境界ロジックを検証できるが、実際の LLM レスポンスによるツール呼び出しパターンとメッセージ蓄積による compaction 発動は E2E でのみ完全に検証できる。

### テスト項目設計のセルフレビュー (testing-rules.md 11)

#### ボトムアップ順序の確認

```
依存関係:
  AgentCore.runSimple -> AgentCore.applyCompaction -> session.Compact -> adjustBoundaryForToolPairs
  AgentCore.runSimple -> AgentCore.defaultSummarizer -> ac.llm.GenerateMessage
  AgentCore.runSimple -> StreamingLLMClient.GenerateMessageStream -> BifrostClient.parseSSEStream

テスト順序:
  Step 1: adjustBoundaryForToolPairs, validateToolPairIntegrity (末端)
  Step 1: Compact with tool pairs (Compact が adjustBoundary を使用)
  Step 3: buildConversationLog, structuredFallbackSummary (末端)
  Step 3: defaultSummarizer (LLM 呼び出しを含む)
  Step 6: parseSSEStream, buildStreamRequestBody (末端)
  Step 6: GenerateMessageStream (SSE パース全体)
  Step 9: E2E (全体統合)
```

#### 観点チェックリスト (testing-rules.md 11.3)

| # | 観点 | 対応テスト |
|---|------|-----------|
| 1 | 正常系 | TestCompact_ToolPairNotSplit, TestDefaultSummarizer_CallsLLM, TestBifrostClient_GenerateMessageStream_TextOnly |
| 2 | 異常系・境界値 | TestCompact_NoToolMessages_NoAdjustment, TestDefaultSummarizer_FallbackOnLLMError, TestBifrostClient_GenerateMessageStream_ServerError |
| 3 | 外部連携 | TestE2E_Wayfinder_CompactionToolPairProtection (実 LLM 使用) |
| 4 | データ一貫性 | TestValidateToolPairIntegrity (compaction 後のメッセージ構造) |
| 5 | 状態遷移 | TestCompact_BoundaryAdjustmentWithConsecutiveToolMessages |
| 6 | 設定反映 | ストリーミング分岐（StreamingLLMClient 実装の有無で分岐） |
| 7 | 副作用 | E2E テストでファイル作成の副作用を検証 |

#### セルフレビュー結果 (testing-rules.md 11.4)

1. **網羅性**: 3つの要件（R1, R2, R3）全てにテストが設計されている。compaction の境界調整、LLM 要約のフォールバック、SSE パースの正常系/異常系をカバー。十分と判断。
2. **証拠の十分性**: 各テストは「エラーが出ない」だけでなく、「境界が正しくずれること」「ツール名が要約に含まれること」「onDelta が正しく呼ばれること」等、期待する具体的な値を検証。十分と判断。
3. **迂回排除**: ストリーミング分岐テストで `StreamingLLMClient` が使用されることを型アサーションで確認。compaction テストで `adjustBoundaryForToolPairs` の結果を直接検証。十分と判断。
4. **依存関係**: ボトムアップ順序に従い、末端関数 -> 統合関数 -> E2E の順でテスト。依存関係の整合性は保たれている。

### 総合判定プロセス (testing-rules.md 12)

全テスト完了後、testing-rules.md 12.2 のチェック項目 7 点を確認し、総合判定を walkthrough.md に記載する。

## Documentation

#### [MODIFY] [README.md](file://README.md)
*   **更新内容**: wayfinder エージェントの Streaming 対応について記述を追加（該当する機能説明セクションがあれば）
