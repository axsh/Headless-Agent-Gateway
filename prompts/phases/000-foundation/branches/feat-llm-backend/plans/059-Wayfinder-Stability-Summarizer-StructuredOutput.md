# 059-Wayfinder-Stability-Summarizer-StructuredOutput

> **Source Specification**: [prompts/phases/000-foundation/branches/feat-llm-backend/ideas/048-Wayfinder-Stability-Summarizer-StructuredOutput.md](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-llm-backend/prompts/phases/000-foundation/branches/feat-llm-backend/ideas/048-Wayfinder-Stability-Summarizer-StructuredOutput.md)

## Goal Description

Wayfinder エージェントの安定性を4つの軸で強化する:

1. **R1**: 空 Content メッセージのサニタイズ -- Gemini API HTTP 400 エラーの根本原因修正
2. **R2**: WBS Planning モードでの LLM ストリーミング中継 -- 子セッションのテキストデルタを親 EventEmitter に中継
3. **R3**: Summarizer の用途別分離 -- DetailedSummarizer / OutcomeSummarizer / compactionSummarizer
4. **R4**: Structured Output による WBS Planner / ExecutionRouter の安定化

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: 空 Content サニタイズ (tool メッセージ) | Phase 1 > `bifrost_client.go` |
| R1: 空 Content サニタイズ (assistant メッセージ) | Phase 1 > `bifrost_client.go` |
| R1: `ac.messages` を変更しない送信時のみ変換 | Phase 1 > `bifrost_client.go` (buildRequestBody 内で変換) |
| R1: ストリーミング用にも同一サニタイズ適用 | Phase 1 > `buildStreamRequestBody` は `buildRequestBody` を呼ぶため自動適用 |
| R2: 子 AgentCore に親 EventEmitter を渡す | Phase 2 > `agent_runner.go` / `subagent_executor.go` |
| R2: テキストデルタ/ツールイベントの中継 | Phase 2 > `agent_runner.go` (子 AgentCore に Emitter 設定) |
| R2: node_start / node_complete イベント維持 | Phase 2 > `wbs_orchestrator.go` 変更なし |
| R3-1: SummaryStrategy インターフェース | Phase 3 > `subagent/summarizer.go` |
| R3-2: DetailedSummarizer 実装 | Phase 3 > `subagent/summarizer.go` |
| R3-3: OutcomeSummarizer 実装 | Phase 3 > `subagent/summarizer.go` |
| R3-4: defaultSummarizer リネーム | Phase 3 > `agent_core.go` |
| R4-1: ModelBehavior に StructuredOutput フラグ | Phase 4 > `config/model_profiles.go` |
| R4-2: GenerateOptions 型追加 | Phase 4 > `llm_client.go` + 各 LLMClient インターフェース |
| R4-3: WBS Planner での Structured Output | Phase 4 > `planning/wbs_planner.go` |
| R4-4: ExecutionRouter での Structured Output | Phase 4 > `execution_router.go` |

---

## Proposed Changes

### Phase 1: R1 空 Content サニタイズ

#### テスト (TDD: テストを先に記述)

#### [MODIFY] [bifrost_client_test.go](file:///shared/libs/go/wayfinder/bifrost_client_test.go)

*   **Description**: 空 Content メッセージのサニタイズを検証するテストケースを追加
*   **Technical Design**:
    ```go
    func TestBuildRequestBody_EmptyToolContent(t *testing.T)
    func TestBuildRequestBody_EmptyAssistantContent(t *testing.T)
    func TestBuildRequestBody_NonEmptyToolContentUnchanged(t *testing.T)
    ```
*   **Logic**:
    *   `TestBuildRequestBody_EmptyToolContent`: `ChatMessage{Role: "tool", Content: "", ToolCallID: "tc1"}` を含むメッセージで `buildRequestBody` を呼び出し、結果の `tool_result` content が `"(no output)"` であることを検証
    *   `TestBuildRequestBody_EmptyAssistantContent`: `ChatMessage{Role: "assistant", Content: "", ToolCalls: nil}` を含むメッセージで呼び出し、content が `"(empty)"` であることを検証
    *   `TestBuildRequestBody_NonEmptyToolContentUnchanged`: 空でない tool content がそのまま保持されることを検証

#### 実装

#### [MODIFY] [bifrost_client.go](file:///shared/libs/go/wayfinder/bifrost_client.go)

*   **Description**: `buildRequestBody` 内のメッセージ変換ループで、送信前に空 Content をサニタイズ
*   **Technical Design**:
    ```go
    // buildRequestBody 内、L87-117 の変更
    ```
*   **Logic**:
    1.  `msg.Role == "tool"` ブランチ (L87-96):
        ```go
        if msg.Role == "tool" {
            content := msg.Content
            if content == "" {
                content = "(no output)"
            }
            apiMsg["role"] = "user"
            apiMsg["content"] = []map[string]any{
                {
                    "type":        "tool_result",
                    "tool_use_id": msg.ToolCallID,
                    "content":     content,
                },
            }
        }
        ```
    2.  `else` ブランチ (L115-117、ToolCalls なしの通常メッセージ):
        ```go
        } else {
            content := msg.Content
            if content == "" {
                content = "(empty)"
            }
            apiMsg["content"] = content
        }
        ```
    3.  `buildStreamRequestBody` は `buildRequestBody` を呼び出すため、自動的にサニタイズが適用される (変更不要)

---

### Phase 2: R2 WBS ストリーミング中継

#### テスト (TDD)

#### [MODIFY] [subagent/subagent_executor_test.go](file:///shared/libs/go/wayfinder/subagent/subagent_executor_test.go)

*   **Description**: `AgentRunnerConfig.Emitter` フィールドの伝播を検証
*   **Technical Design**:
    ```go
    func TestSubagentExecutor_EmitterPropagation(t *testing.T)
    ```
*   **Logic**:
    *   `AgentRunnerConfig` に `Emitter` フィールドを設定
    *   `configCapturingRunner` で子に渡された config の `Emitter` が親と同一であることを検証

#### 実装

#### [MODIFY] [subagent/subagent_executor.go](file:///shared/libs/go/wayfinder/subagent/subagent_executor.go)

*   **Description**: `AgentRunnerConfig` に `Emitter` フィールドを追加
*   **Technical Design**:
    ```go
    // AgentRunnerConfig is a simplified config for creating child sessions.
    type AgentRunnerConfig struct {
        WorkDir             string
        SessionDir          string
        LogicalModel        string
        AllowedPathPatterns []string
        Emitter             any // codingagent.StreamEventEmitter (避 cyclic import)
    }
    ```
*   **Logic**:
    *   `Emitter` フィールドの型は `any` とし、cyclic import を回避する
    *   `AgentRunner.RunChild` のシグネチャは変更しない
    *   `SubagentExecutor.Execute` 内で childConfig に `Emitter` を設定する:
        ```go
        childConfig := &AgentRunnerConfig{
            WorkDir:             e.parentConfig.WorkDir,
            SessionDir:          e.parentConfig.SessionDir,
            LogicalModel:        e.parentConfig.LogicalModel,
            AllowedPathPatterns: e.parentConfig.AllowedPathPatterns,
            Emitter:             e.parentConfig.Emitter,
        }
        ```

#### [MODIFY] [agent_runner.go](file:///shared/libs/go/wayfinder/agent_runner.go)

*   **Description**: `RunChild` で子 AgentCore に EventEmitter を設定
*   **Technical Design**:
    ```go
    func (r *AgentRunnerImpl) RunChild(...) (string, error) {
        // 既存コード...
        child := NewAgentCore(wrappedLLM, childCfg, log)
        child.SetSessionID(sessionID)

        // Emitter の中継を追加
        if cfg.Emitter != nil {
            if emitter, ok := cfg.Emitter.(*EventEmitter); ok {
                child.SetEmitter(emitter)
            }
        }

        return child.Run(ctx, prompt)
    }
    ```
*   **Logic**:
    *   `cfg.Emitter` は `any` 型なので、`*EventEmitter` に型アサーションして設定
    *   `nil` の場合はスキップ（子セッションは no-op emitter のまま）

#### [MODIFY] [agent_core.go](file:///shared/libs/go/wayfinder/agent_core.go)

*   **Description**: `runWithWBSTree` で `agentNodeExecutor` の `childConfig.Emitter` に親 emitter を設定
*   **Technical Design** (L534-539):
    ```go
    childCfg := &subagent.AgentRunnerConfig{
        WorkDir:             ac.config.WorkDir,
        SessionDir:          ac.config.SessionDir,
        LogicalModel:        ac.config.LogicalModel,
        AllowedPathPatterns: ac.config.AllowedPathPatterns,
        Emitter:             ac.emitter, // 親 EventEmitter を中継
    }
    ```
*   **Logic**: `ac.emitter` を `childCfg.Emitter` に設定するだけ。子 AgentCore は `agent_runner.go` で `SetEmitter` される

#### [NEW] [agent_core_emitter_test.go](file:///shared/libs/go/wayfinder/agent_core_emitter_test.go)

*   **Description**: WBS 実行時に子セッションのイベントが親 EventEmitter に中継されることを検証
*   **Technical Design**:
    ```go
    func TestRunWithWBSTree_ChildEmitterRelay(t *testing.T)
    ```
*   **Logic**:
    *   モック LLM とモック AgentRunner を使用
    *   親 EventEmitter にキャプチャ機能を持たせ、子セッション実行後にイベントが記録されていることを検証
    *   **注意**: この結合テストは AgentRunner のモック化が必要なため、深い結合は避け、`Emitter` フィールドが正しく伝播されることのみを検証する

---

### Phase 3: R3 Summarizer 用途別分離

#### テスト (TDD)

#### [MODIFY] [subagent/summarizer_test.go](file:///shared/libs/go/wayfinder/subagent/summarizer_test.go)

*   **Description**: `SummaryStrategy` インターフェースの 2 実装をテスト
*   **Technical Design**:
    ```go
    func TestDetailedSummarizer_PreservesStructure(t *testing.T)
    func TestDetailedSummarizer_LLMError(t *testing.T)
    func TestOutcomeSummarizer_CompactOutput(t *testing.T)
    func TestOutcomeSummarizer_LLMError(t *testing.T)
    func TestOutcomeSummarizer_TruncatesLongOutput(t *testing.T)
    ```
*   **Logic**:
    *   `TestDetailedSummarizer_PreservesStructure`: mockLLM が `Status: SUCCESS\nSummary: ... \nKey Findings: ...` 形式の応答を返す。プロンプト内に `"key data points"` が含まれることを検証
    *   `TestOutcomeSummarizer_CompactOutput`: mockLLM が 1-3 文の自然文応答を返す。プロンプト内に `"outcome"` や `"succeeded"` が含まれることを検証
    *   LLM エラー時は両方とも `error` を返すことを検証
    *   既存の `TestSummarizeForParent_*` テストは `DetailedSummarizer` 用にリファクタリング

#### 実装

#### [MODIFY] [subagent/summarizer.go](file:///shared/libs/go/wayfinder/subagent/summarizer.go)

*   **Description**: `SummaryStrategy` インターフェースと 2 つの実装を追加
*   **Technical Design**:
    ```go
    // SummaryStrategy defines the summarization approach.
    type SummaryStrategy interface {
        Summarize(ctx context.Context, hints *Hints, rawOutput string) (string, error)
    }

    // DetailedSummarizer preserves tool call structure and data points.
    // Used by: Tool Calling subagent path.
    type DetailedSummarizer struct {
        llm LLMClient
    }

    // OutcomeSummarizer focuses on success/failure and high-level outcome.
    // Used by: WBS Planning node executor.
    type OutcomeSummarizer struct {
        llm LLMClient
    }

    // NewDetailedSummarizer creates a new DetailedSummarizer.
    func NewDetailedSummarizer(llm LLMClient) *DetailedSummarizer {
        return &DetailedSummarizer{llm: llm}
    }

    // NewOutcomeSummarizer creates a new OutcomeSummarizer.
    func NewOutcomeSummarizer(llm LLMClient) *OutcomeSummarizer {
        return &OutcomeSummarizer{llm: llm}
    }
    ```
*   **Logic**:
    *   **DetailedSummarizer のプロンプト** (既存の `summarySystemPrompt` を引き継ぐ):
        ```
        You are a result summarizer for a coding agent.
        Given the raw output from a tool execution and the parent agent's objective,
        produce a concise summary in the following format:

        Status: [SUCCESS/FAILURE]
        Summary: [3-5 line summary of results]
        Key Findings / Errors: [Important errors, stack traces, or key data points if any]

        Focus only on information relevant to the parent's objective.
        Do NOT include progress logs, timestamps, or verbose output.
        ```
    *   **OutcomeSummarizer のプロンプト** (新規):
        ```
        You are summarizing the result of a subtask execution.
        Describe what was done and whether the objective was achieved in 1-3 sentences.
        Do not include tool call details, file listings, or raw command output.
        Focus only on the outcome: what happened and whether it succeeded or failed.
        ```
    *   両方の `Summarize` メソッドは同じ構造:
        1. `rawOutput` が `maxRawOutputLen` (50000) を超える場合は truncate
        2. `fmt.Sprintf("Parent's Objective: %s\nContext: %s\n\nRaw Output:\n%s", hints.Objective, hints.Context, truncatedOutput)` でプロンプト構築
        3. `llm.GenerateMessage` を呼び出し、結果を返す
    *   既存の `Summarizer` 構造体と `NewSummarizer` は **`DetailedSummarizer` へのエイリアスとして維持** (後方互換性):
        ```go
        // Summarizer is an alias for DetailedSummarizer (backward compatibility).
        type Summarizer = DetailedSummarizer

        // NewSummarizer creates a new DetailedSummarizer (backward compatibility).
        func NewSummarizer(llm LLMClient) *Summarizer {
            return NewDetailedSummarizer(llm)
        }
        ```
    *   `SummarizeForParent` メソッドは `Summarize` メソッドに委譲:
        ```go
        func (s *DetailedSummarizer) SummarizeForParent(ctx context.Context, hints *Hints, rawOutput string) (string, error) {
            return s.Summarize(ctx, hints, rawOutput)
        }
        ```

#### [MODIFY] [subagent/subagent_executor.go](file:///shared/libs/go/wayfinder/subagent/subagent_executor.go)

*   **Description**: `SubagentExecutor.summarizer` の型を `SummaryStrategy` に変更
*   **Technical Design** (L66):
    ```go
    type SubagentExecutor struct {
        parentConfig *AgentRunnerConfig
        llm          LLMClient
        runner       AgentRunner
        hints        *HintGenerator
        summarizer   SummaryStrategy  // was: *Summarizer
        logger       logger.Logger
    }
    ```
*   **Logic**:
    *   `NewSubagentExecutor` 内で `NewDetailedSummarizer(llm)` を注入:
        ```go
        summarizer: NewDetailedSummarizer(llm),
        ```
    *   `Execute` メソッド内 L135 の呼び出しを変更:
        ```go
        // 旧: summary, err := e.summarizer.SummarizeForParent(ctx, hints, childResult)
        // 新:
        summary, err := e.summarizer.Summarize(ctx, hints, childResult)
        ```

#### [MODIFY] [agent_core.go](file:///shared/libs/go/wayfinder/agent_core.go)

*   **Description**: `agentNodeExecutor.summarizer` の型を `SummaryStrategy` に変更し、`OutcomeSummarizer` を注入。`defaultSummarizer` を `compactionSummarizer` にリネーム。
*   **Technical Design**:
    1.  `agentNodeExecutor` 構造体 (L596-603):
        ```go
        type agentNodeExecutor struct {
            parentSessionID string
            childConfig     *subagent.AgentRunnerConfig
            runner          subagent.AgentRunner
            llm             subagent.LLMClient
            summarizer      subagent.SummaryStrategy  // was: *subagent.Summarizer
            logger          logger.Logger
        }
        ```
    2.  `runWithWBSTree` (L545):
        ```go
        // 旧: summarizer: subagent.NewSummarizer(ac.subagentLLM),
        // 新:
        summarizer: subagent.NewOutcomeSummarizer(ac.subagentLLM),
        ```
    3.  `ExecuteNode` (L619):
        ```go
        // 旧: summary, err := e.summarizer.SummarizeForParent(ctx, hints, childResult)
        // 新:
        summary, err := e.summarizer.Summarize(ctx, hints, childResult)
        ```
    4.  `defaultSummarizer` -> `compactionSummarizer` (L311):
        ```go
        // 旧: func (ac *AgentCore) defaultSummarizer(msgs []session.Message) (string, error)
        // 新:
        func (ac *AgentCore) compactionSummarizer(msgs []session.Message) (string, error)
        ```
    5.  `applyCompaction` 呼び出し (L289):
        ```go
        // 旧: compacted, err := session.Compact(sessionMsgs, ac.compactionCfg, ac.defaultSummarizer)
        // 新:
        compacted, err := session.Compact(sessionMsgs, ac.compactionCfg, ac.compactionSummarizer)
        ```

---

### Phase 4: R4 Structured Output

#### テスト (TDD)

#### [MODIFY] [config/model_profiles_test.go](file:///shared/libs/go/config/model_profiles_test.go)

*   **Description**: `ModelBehavior.StructuredOutput` フラグの YAML パースを検証
*   **Technical Design**:
    ```go
    func TestModelBehavior_StructuredOutput(t *testing.T)
    ```
*   **Logic**:
    *   YAML 文字列 `behavior:\n  structured_output: true` をパースし、`StructuredOutput == true` を検証
    *   フラグ未設定時に `false` (ゼロ値) であることを検証

#### [MODIFY] [planning/wbs_planner_test.go](file:///shared/libs/go/wayfinder/planning/wbs_planner_test.go)

*   **Description**: Structured Output パスとフォールバックパスのテスト
*   **Technical Design**:
    ```go
    func TestGenerateWBS_WithStructuredOutput(t *testing.T)
    func TestGenerateWBS_FallbackWithoutStructuredOutput(t *testing.T)
    ```
*   **Logic**:
    *   `TestGenerateWBS_WithStructuredOutput`: `GenerateOptions{ResponseFormat: ...}` 付きで呼び出し、正しい WBSTree が返ることを検証
    *   `TestGenerateWBS_FallbackWithoutStructuredOutput`: `GenerateOptions` なしで呼び出し、`extractJSON` 経由で同じ結果が返ることを検証
    *   **注意**: mock LLM のシグネチャ変更 (`opts ...GenerateOptions`) が必要

#### [MODIFY] [execution_router_test.go](file:///shared/libs/go/wayfinder/execution_router_test.go)

*   **Description**: Structured Output パスのテスト
*   **Technical Design**:
    ```go
    func TestRoute_WithStructuredOutput(t *testing.T)
    ```
*   **Logic**:
    *   `structured_output: true` の場合に `GenerateOptions` が渡されることを検証

#### 実装

#### [MODIFY] [config/model_profiles.go](file:///shared/libs/go/config/model_profiles.go)

*   **Description**: `ModelBehavior` に `StructuredOutput` フラグを追加
*   **Technical Design** (L46-49):
    ```go
    // ModelBehavior holds model-specific behavior settings.
    type ModelBehavior struct {
        // ToolCallFallback enables text-to-tool-call conversion for local LLMs.
        ToolCallFallback bool `yaml:"tool_call_fallback"`
        // StructuredOutput indicates the model supports structured output (JSON schema).
        StructuredOutput bool `yaml:"structured_output"`
    }
    ```

#### [MODIFY] [llm_client.go](file:///shared/libs/go/wayfinder/llm_client.go)

*   **Description**: `GenerateOptions` / `ResponseFormat` 型を追加し、`LLMClient` インターフェースのシグネチャを拡張
*   **Technical Design**:
    ```go
    // GenerateOptions holds optional parameters for LLM generation.
    type GenerateOptions struct {
        ResponseFormat *ResponseFormat
    }

    // ResponseFormat specifies the desired response format.
    type ResponseFormat struct {
        Type       string // "json_object" or "json_schema"
        JSONSchema any    // JSON Schema definition (optional)
    }

    // LLMClient is the abstract interface for LLM communication.
    type LLMClient interface {
        GenerateMessage(ctx context.Context, logicalModel string,
            messages []ChatMessage, tools []ToolDefinition,
            opts ...GenerateOptions) (*LLMResponse, error)
    }

    // StreamingLLMClient extends LLMClient with streaming support.
    type StreamingLLMClient interface {
        LLMClient
        GenerateMessageStream(ctx context.Context, logicalModel string,
            messages []ChatMessage, tools []ToolDefinition,
            onDelta func(textDelta string)) (*LLMResponse, error)
    }
    ```
*   **Logic**:
    *   `opts ...GenerateOptions` は可変長引数なので、既存の呼び出し元はオプション引数なしで動作する (後方互換性維持)
    *   `StreamingLLMClient` のシグネチャは変更しない (ストリーミングで Structured Output は不要)

#### [MODIFY] [bifrost_client.go](file:///shared/libs/go/wayfinder/bifrost_client.go)

*   **Description**: `GenerateMessage` のシグネチャを拡張し、`response_format` をリクエストボディに追加
*   **Technical Design**:
    1.  `GenerateMessage` シグネチャ変更:
        ```go
        func (bc *BifrostClient) GenerateMessage(ctx context.Context, logicalModel string,
            messages []ChatMessage, tools []ToolDefinition,
            opts ...GenerateOptions) (*LLMResponse, error) {
        ```
    2.  `buildRequestBody` シグネチャ変更:
        ```go
        func (bc *BifrostClient) buildRequestBody(model string, messages []ChatMessage,
            toolDefs []ToolDefinition, opts ...GenerateOptions) map[string]any {
        ```
    3.  `buildRequestBody` 内で `ResponseFormat` を適用:
        ```go
        // Apply response_format if specified.
        if len(opts) > 0 && opts[0].ResponseFormat != nil {
            rf := opts[0].ResponseFormat
            if rf.Type == "json_schema" && rf.JSONSchema != nil {
                body["response_format"] = map[string]any{
                    "type":        "json_schema",
                    "json_schema": rf.JSONSchema,
                }
            } else if rf.Type == "json_object" {
                body["response_format"] = map[string]any{
                    "type": "json_object",
                }
            }
        }
        ```
    4.  `buildStreamRequestBody` も `opts` を受け取るよう変更:
        ```go
        func (bc *BifrostClient) buildStreamRequestBody(model string, messages []ChatMessage,
            toolDefs []ToolDefinition, opts ...GenerateOptions) map[string]any {
            body := bc.buildRequestBody(model, messages, toolDefs, opts...)
            body["stream"] = true
            return body
        }
        ```

#### [MODIFY] [planning/wbs_planner.go](file:///shared/libs/go/wayfinder/planning/wbs_planner.go)

*   **Description**: Structured Output 対応。LLMClient インターフェースにオプション追加。
*   **Technical Design**:
    1.  ローカル `LLMClient` インターフェースの変更 (L12-14):
        ```go
        type LLMClient interface {
            GenerateMessage(ctx context.Context, model string, messages []ChatMessage,
                tools []ToolDefinition, opts ...GenerateOptions) (*LLMResponse, error)
        }
        ```
    2.  ローカル型の追加:
        ```go
        // GenerateOptions holds optional parameters for LLM generation.
        type GenerateOptions struct {
            ResponseFormat *ResponseFormat
        }

        // ResponseFormat specifies the desired response format.
        type ResponseFormat struct {
            Type       string
            JSONSchema any
        }
        ```
    3.  `WBSPlanner` に `useStructuredOutput` フラグを追加:
        ```go
        type WBSPlanner struct {
            llm                 LLMClient
            useStructuredOutput bool
        }

        func NewWBSPlanner(llm LLMClient) *WBSPlanner {
            return &WBSPlanner{llm: llm}
        }

        // SetStructuredOutput enables/disables structured output.
        func (p *WBSPlanner) SetStructuredOutput(enabled bool) {
            p.useStructuredOutput = enabled
        }
        ```
    4.  `GenerateWBS` の変更 (L69-91):
        ```go
        func (p *WBSPlanner) GenerateWBS(ctx context.Context, model string, userRequest string) (*WBSTree, error) {
            messages := []ChatMessage{
                {Role: "system", Content: wbsPlannerSystemPrompt},
                {Role: "user", Content: userRequest},
            }

            var opts []GenerateOptions
            if p.useStructuredOutput {
                opts = append(opts, GenerateOptions{
                    ResponseFormat: &ResponseFormat{
                        Type:       "json_object",
                    },
                })
            }

            resp, err := p.llm.GenerateMessage(ctx, model, messages, nil, opts...)
            if err != nil {
                return nil, fmt.Errorf("WBS generation failed: %w", err)
            }

            // Parse JSON. extractJSON handles markdown wrappers for non-structured responses.
            jsonStr := resp.Content
            if !p.useStructuredOutput {
                jsonStr = extractJSON(jsonStr)
            }

            var tree WBSTree
            if err := json.Unmarshal([]byte(jsonStr), &tree); err != nil {
                return nil, fmt.Errorf("failed to parse WBS JSON: %w", err)
            }

            // Normalize: all statuses should be "pending".
            tree.walkNodesMut(func(node *WBSNode) {
                node.Status = StatusPending
            })

            return &tree, nil
        }
        ```

#### [MODIFY] [execution_router.go](file:///shared/libs/go/wayfinder/execution_router.go)

*   **Description**: Structured Output 対応
*   **Technical Design**:
    1.  `ExecutionRouter` に `useStructuredOutput` フラグを追加:
        ```go
        type ExecutionRouter struct {
            llm                 LLMClient
            useStructuredOutput bool
        }

        // SetStructuredOutput enables/disables structured output.
        func (r *ExecutionRouter) SetStructuredOutput(enabled bool) {
            r.useStructuredOutput = enabled
        }
        ```
    2.  `Route` メソッドの変更 (L50-74):
        ```go
        func (r *ExecutionRouter) Route(ctx context.Context, model string, prompt string) (ExecutionRoute, string, error) {
            messages := []ChatMessage{
                {Role: "system", Content: routerSystemPrompt},
                {Role: "user", Content: prompt},
            }

            var opts []GenerateOptions
            if r.useStructuredOutput {
                opts = append(opts, GenerateOptions{
                    ResponseFormat: &ResponseFormat{
                        Type: "json_object",
                    },
                })
            }

            resp, err := r.llm.GenerateMessage(ctx, model, messages, nil, opts...)
            if err != nil {
                return RouteSimple, "routing failed, defaulting to simple", nil
            }

            jsonStr := resp.Content
            if !r.useStructuredOutput {
                jsonStr = extractRouterJSON(jsonStr)
            }

            var result struct {
                Route  string `json:"route"`
                Reason string `json:"reason"`
            }
            if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
                return RouteSimple, "failed to parse routing response", nil
            }

            if result.Route == "planning" {
                return RoutePlanning, result.Reason, nil
            }
            return RouteSimple, result.Reason, nil
        }
        ```

#### [MODIFY] [agent_core.go](file:///shared/libs/go/wayfinder/agent_core.go)

*   **Description**: WBS Planner と ExecutionRouter に `StructuredOutput` フラグを伝播
*   **Technical Design**: `Run` メソッド内で `router.SetStructuredOutput` と `planner.SetStructuredOutput` を呼び出す設計も考えられるが、**設定の伝播は構築時に行う** のが適切。`AgentConfig` に `StructuredOutput` フラグを追加し、`SetRouter` / `SetPlanner` 時に適用する。
    ```go
    // agent_config.go に追加
    type AgentConfig struct {
        // 既存フィールド...
        StructuredOutput bool // model_profiles から取得
    }
    ```
*   **Logic**:
    *   `SetRouter` / `SetPlanner` 呼び出し後に `SetStructuredOutput` を呼ぶ:
        ```go
        func (ac *AgentCore) SetRouter(router *ExecutionRouter) {
            ac.router = router
            router.SetStructuredOutput(ac.config.StructuredOutput)
        }

        func (ac *AgentCore) SetPlanner(planner *planning.WBSPlanner) {
            ac.planner = planner
            planner.SetStructuredOutput(ac.config.StructuredOutput)
        }
        ```

#### [MODIFY] [settings/demo/model_profiles.yaml](file:///settings/demo/model_profiles.yaml)

*   **Description**: `structured_output` フラグを追加
*   **Logic**: 各モデルの `behavior` セクションに追加:
    ```yaml
    models:
      - name: gemini-2.5-flash
        behavior:
          structured_output: true
      - name: claude-sonnet-4-20250514
        behavior:
          structured_output: false
    ```

#### [MODIFY] [settings/example/model_profiles.yaml](file:///settings/example/model_profiles.yaml)

*   **Description**: 同上

---

### 各パッケージの LLMClient インターフェース更新

> [!IMPORTANT]
> `LLMClient` インターフェースは cyclic import 回避のため、`planning` パッケージと `subagent` パッケージにそれぞれローカル定義されている。`opts ...GenerateOptions` の追加は全ローカル定義にも反映する必要がある。

#### [MODIFY] [subagent/subagent_executor.go](file:///shared/libs/go/wayfinder/subagent/subagent_executor.go)

*   **Description**: ローカル `LLMClient` に `opts ...GenerateOptions` を追加
*   **Technical Design** (L14-16):
    ```go
    type LLMClient interface {
        GenerateMessage(ctx context.Context, model string, messages []ChatMessage,
            tools []ToolDefinition, opts ...GenerateOptions) (*LLMResponse, error)
    }
    ```
*   **Logic**: ローカル `GenerateOptions` / `ResponseFormat` 型も追加:
    ```go
    // GenerateOptions holds optional parameters for LLM generation.
    type GenerateOptions struct {
        ResponseFormat *ResponseFormat
    }

    // ResponseFormat specifies the desired response format.
    type ResponseFormat struct {
        Type       string
        JSONSchema any
    }
    ```

#### [MODIFY] [agent_runner.go](file:///shared/libs/go/wayfinder/agent_runner.go)

*   **Description**: `subagentToWayfinderLLM` の `GenerateMessage` シグネチャを更新
*   **Technical Design** (L58-63):
    ```go
    func (a *subagentToWayfinderLLM) GenerateMessage(
        ctx context.Context, model string, msgs []subagent.ChatMessage,
        tools []subagent.ToolDefinition, opts ...subagent.GenerateOptions,
    ) (*subagent.LLMResponse, error) {
    ```
*   **Logic**: `opts` は subagent -> wayfinder 変換時には使用しない（透過的に無視）

---

## Step-by-Step Implementation Guide

### Phase 1: R1 空 Content サニタイズ

- [ ] 1. `bifrost_client_test.go` に `TestBuildRequestBody_EmptyToolContent`, `TestBuildRequestBody_EmptyAssistantContent`, `TestBuildRequestBody_NonEmptyToolContentUnchanged` を追加
- [ ] 2. テスト失敗を確認
- [ ] 3. `bifrost_client.go` の `buildRequestBody` を修正 (L87-96 の tool ブランチ、L115-117 の else ブランチ)
- [ ] 4. テスト成功を確認
- [ ] 5. `git add && git commit -m "fix: sanitize empty content in bifrost request body"`

### Phase 2: R2 WBS ストリーミング中継

- [ ] 6. `subagent/subagent_executor.go` の `AgentRunnerConfig` に `Emitter any` フィールドを追加
- [ ] 7. `subagent_executor_test.go` に `TestSubagentExecutor_EmitterPropagation` を追加
- [ ] 8. テスト失敗を確認
- [ ] 9. `SubagentExecutor.Execute` で `childConfig.Emitter` を設定
- [ ] 10. テスト成功を確認
- [ ] 11. `agent_runner.go` の `RunChild` で `cfg.Emitter` を子 AgentCore に `SetEmitter` として設定
- [ ] 12. `agent_core.go` の `runWithWBSTree` で `childCfg.Emitter = ac.emitter` を設定
- [ ] 13. `git add && git commit -m "feat: relay streaming events from WBS child sessions to parent emitter"`

### Phase 3: R3 Summarizer 分離

- [ ] 14. `subagent/summarizer.go` に `SummaryStrategy` インターフェース、`DetailedSummarizer`、`OutcomeSummarizer` を追加。既存 `Summarizer` を `DetailedSummarizer` のエイリアスに変更
- [ ] 15. `subagent/summarizer_test.go` に `TestDetailedSummarizer_PreservesStructure`, `TestOutcomeSummarizer_CompactOutput` 等を追加
- [ ] 16. テスト成功を確認
- [ ] 17. `subagent/subagent_executor.go` の `summarizer` 型を `SummaryStrategy` に変更し、`Execute` 内の呼び出しを `Summarize` に変更
- [ ] 18. `agent_core.go` の `agentNodeExecutor.summarizer` を `SummaryStrategy` に変更し、`OutcomeSummarizer` を注入。`defaultSummarizer` -> `compactionSummarizer` リネーム
- [ ] 19. `git add && git commit -m "refactor: split Summarizer into DetailedSummarizer and OutcomeSummarizer"`

### Phase 4: R4 Structured Output

- [ ] 20. `llm_client.go` に `GenerateOptions` / `ResponseFormat` 型を追加し、`LLMClient` のシグネチャを `opts ...GenerateOptions` に拡張
- [ ] 21. `bifrost_client.go` の `GenerateMessage` / `buildRequestBody` / `buildStreamRequestBody` シグネチャを更新し、`response_format` を適用
- [ ] 22. `subagent/subagent_executor.go` のローカル `LLMClient` / `GenerateOptions` を更新
- [ ] 23. `planning/wbs_planner.go` のローカル型を更新し、`GenerateWBS` に Structured Output 分岐を追加
- [ ] 24. `execution_router.go` に `SetStructuredOutput` と `Route` 内の分岐を追加
- [ ] 25. `config/model_profiles.go` の `ModelBehavior` に `StructuredOutput` フラグを追加
- [ ] 26. `agent_core.go` の `SetRouter` / `SetPlanner` で `SetStructuredOutput` を適用
- [ ] 27. `agent_runner.go` の `subagentToWayfinderLLM.GenerateMessage` シグネチャを更新
- [ ] 28. 各テストファイルのモック LLM シグネチャを更新
- [ ] 29. テスト追加: `TestModelBehavior_StructuredOutput`, `TestGenerateWBS_WithStructuredOutput`, `TestGenerateWBS_FallbackWithoutStructuredOutput`, `TestRoute_WithStructuredOutput`
- [ ] 30. `settings/demo/model_profiles.yaml`, `settings/example/model_profiles.yaml` に `structured_output` フラグを追加
- [ ] 31. `git add && git commit -m "feat: add structured output support for WBS Planner and ExecutionRouter"`

### 検証

- [ ] 32. `./scripts/process/build.sh` を実行して全テスト成功を確認
- [ ] 33. `./scripts/process/integration_test.sh --categories llm` を実行して統合テスト成功を確認
- [ ] 34. 総合判定プロセスを実施

---

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```

2.  **Integration Tests**:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories llm,taskengine
    ```
    *   **Log Verification**: ログに `bifrost: HTTP 400` が出力されないことを確認。`compactionSummarizer`, `DetailedSummarizer`, `OutcomeSummarizer` のログメッセージを確認。

### E2E Tests

本計画は主に内部のインターフェースリファクタリング (R3)、送信データの修正 (R1)、設定伝播 (R2, R4) であり、外部から観測可能な API の変更は含まない。

既存の E2E テスト ([wayfinder_e2e_test.go](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-llm-backend/tests/wayfinder_e2e_test.go)) で WBS 実行パスがカバーされている場合、そのテストの成功が R1/R2/R4 の検証になる。

新規の E2E テストコードの追加は以下の理由から省略する:
- R1 (空 Content) は `buildRequestBody` の単体テストで十分に検証可能
- R2 (ストリーミング中継) は EventEmitter の内部動作であり、外部 API には影響しない
- R3 (Summarizer 分離) は純粋な内部リファクタリング
- R4 (Structured Output) は LLM リクエストの形式変更であり、応答内容は LLM 依存

既存の E2E テストでリグレッションがないことを確認する:
```bash
./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories llm --specify "TestWayfinderAgent"
```

### テスト項目のセルフレビュー

1. **網羅性の検証**: R1 は空 Content の 3 パターン (tool 空、assistant 空、非空保持) をカバー。R3 は DetailedSummarizer / OutcomeSummarizer の正常系・異常系をカバー。R4 は Structured Output 有効/無効の分岐をカバー。R2 は Emitter 伝播のみの軽量テスト。全要件が対応するテストケースを持つ。
2. **証拠の十分性**: 各テストは具体的な値の検証を含む (空文字列が `"(no output)"` に変換されることを検証、プロンプト内容の文字列検証)。
3. **迂回・抜け道の排除**: R1 は `buildRequestBody` の出力を直接検証するため、迂回は不可能。R3 は各 Summarizer のプロンプト内容を検証。
4. **依存関係の整合性**: Phase 1 -> 2 -> 3 -> 4 の順序で、末端 (BifrostClient) から上位 (AgentCore) へボトムアップで実装・テストする。

## Documentation

#### [MODIFY] [prompts/specifications/wayfinder-architecture.md](file:///prompts/specifications/wayfinder-architecture.md)

*   **更新内容**: Summarizer の分類 (DetailedSummarizer / OutcomeSummarizer / compactionSummarizer) を追記。Structured Output のフラグと適用箇所を追記。(該当ファイルが存在しない場合はスキップ)
