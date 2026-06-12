# 002-Wayfinder-Subagent-Summarization

> **Source Specification**: [002-Wayfinder-Subagent-Hierarchy-and-Summarization.md](file://prompts/phases/000-foundation/branches/feat-wayfinder-agent/ideas/002-Wayfinder-Subagent-Hierarchy-and-Summarization.md)

## Goal Description

Wayfinder Agentのサブエージェント連携機能を実装する。重いツール実行（特にコマンド実行）時に、親セッションのコンテキストウィンドウを圧迫しないよう、子セッションを動的に生成してツールを実行させ、結果をLLMで要約・加工して親セッションに返却する。

具体的には:
1. **SubagentExecutor**: 親セッションからのツール実行要求を受け、子セッションを生成してAgentCore.Runを再帰的に呼び出す
2. **ヒント生成 (HintGenerator)**: 親セッションの会話履歴とツールパラメータから、子セッションへのメタ情報（目的、文脈）を生成
3. **子セッション内の要約処理**: 生の出力結果をLLMで加工・要約し、親セッションが必要とする情報に絞り込む
4. **階層セッションログ**: 子セッションの完全な会話履歴を独立したセッションファイルとして永続化

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| 子セッションの動的生成 | Proposed Changes > subagent_executor.go > `Execute` |
| 実行ディレクトリの引き継ぎ | Proposed Changes > subagent_executor.go > 子AgentConfigでWorkDir/SessionDirを継承 |
| インテリジェントなヒント生成 | Proposed Changes > hint_generator.go > `GenerateHints` |
| 子セッション内でのツール実行 | Proposed Changes > subagent_executor.go > 子AgentCore.Run |
| LLMによる出力の加工・要約 | Proposed Changes > summarizer.go > `SummarizeForParent` |
| 親セッションへの要約結果返却 | Proposed Changes > subagent_executor.go > 要約をツール結果として返す |
| 階層セッションログの保存 | Proposed Changes > subagent_executor.go > 子セッションは独自SessionIDでSessionDirに保存 |
| 要約プロンプト設計方針 | Proposed Changes > summarizer.go > summarySystemPrompt |
| 親子セッションでの設定伝播 (001) | Proposed Changes > subagent_executor.go > AgentConfig継承 |
| AgentCoreの再帰呼び出し構造 | Proposed Changes > subagent_executor.go > NewAgentCore + Run |

## Proposed Changes

### wayfinder/subagent パッケージ

#### [NEW] [subagent_executor.go](file://shared/libs/go/wayfinder/subagent/subagent_executor.go)
*   **Description**: サブエージェント実行器。親からのツール実行要求を受け、子セッションでAgentCoreを駆動する。
*   **Technical Design**:
    ```go
    package subagent

    import (
        "context"
        "fmt"

        "github.com/google/uuid"
    )

    // SubagentExecutor manages child session lifecycle.
    type SubagentExecutor struct {
        parentConfig *wayfinder.AgentConfig
        llm          wayfinder.LLMClient
        toolFactory  ToolFactoryFunc
        hints        *HintGenerator
        summarizer   *Summarizer
        logger       wayfinder.Logger
    }

    // ToolFactoryFunc creates a ToolRegistry for a child session.
    type ToolFactoryFunc func(cfg *wayfinder.AgentConfig) *wayfinder.ToolRegistry

    func NewSubagentExecutor(
        parentCfg *wayfinder.AgentConfig,
        llm wayfinder.LLMClient,
        toolFactory ToolFactoryFunc,
        logger wayfinder.Logger,
    ) *SubagentExecutor {
        return &SubagentExecutor{
            parentConfig: parentCfg,
            llm:          llm,
            toolFactory:  toolFactory,
            hints:        NewHintGenerator(llm),
            summarizer:   NewSummarizer(llm),
            logger:       logger,
        }
    }

    // Execute runs a tool in a child session and returns summarized result.
    func (e *SubagentExecutor) Execute(
        ctx context.Context,
        parentMessages []session.Message,
        toolName string,
        toolInput map[string]any,
    ) (string, error) {
        // 1. Generate hints from parent context
        hints, err := e.hints.GenerateHints(ctx, parentMessages, toolName, toolInput)
        if err != nil {
            e.logger.Warn("hint generation failed, proceeding without hints: %v", err)
            hints = &Hints{Objective: "Execute the requested tool and report results."}
        }

        // 2. Create child session config (inherit WorkDir and SessionDir)
        childSessionID := uuid.New().String()
        childConfig := &wayfinder.AgentConfig{
            WorkDir:             e.parentConfig.WorkDir,
            SessionDir:          e.parentConfig.SessionDir,
            SessionID:           childSessionID,
            Model:               e.parentConfig.Model,
            AllowedPathPatterns: e.parentConfig.AllowedPathPatterns,
        }

        // 3. Create child AgentCore with tool registry
        childTools := e.toolFactory(childConfig)
        childCore := wayfinder.NewAgentCore(childConfig, e.llm, childTools, e.logger)

        // 4. Build child prompt with hints and tool execution instruction
        childPrompt := fmt.Sprintf(
            "[SUBAGENT TASK]\nObjective: %s\nContext: %s\n\n"+
            "Execute the following tool and provide a summary of the results:\n"+
            "Tool: %s\nInput: %v",
            hints.Objective, hints.Context, toolName, toolInput,
        )

        // 5. Run child AgentCore (recursive call)
        childResult, err := childCore.Run(ctx, childPrompt)
        if err != nil {
            return "", fmt.Errorf("child session %s failed: %w", childSessionID, err)
        }

        // 6. Summarize child result for parent consumption
        summary, err := e.summarizer.SummarizeForParent(ctx, hints, childResult)
        if err != nil {
            // Fallback: return raw result if summarization fails
            return childResult, nil
        }

        return summary, nil
    }
    ```
*   **Logic**:
    *   子セッションは親と同じ `WorkDir`, `SessionDir`, `AllowedPathPatterns` を引き継ぐ
    *   子セッションのIDは新規UUIDを生成し、`SessionDir` に独立して保存される
    *   ヒント生成の失敗はフォールバックし、ツール実行自体は継続する
    *   要約の失敗もフォールバックし、生の結果を返す

#### [NEW] [hint_generator.go](file://shared/libs/go/wayfinder/subagent/hint_generator.go)
*   **Description**: 親の会話履歴とツール呼び出しパラメータからヒントを生成する
*   **Technical Design**:
    ```go
    package subagent

    import "context"

    // Hints carries meta-information for the child session.
    type Hints struct {
        Objective   string `json:"objective"`   // What the parent wants to know
        Context     string `json:"context"`     // Relevant context from parent conversation
        Constraints string `json:"constraints"` // Any constraints or focus areas
    }

    // HintGenerator creates hints from parent context.
    type HintGenerator struct {
        llm wayfinder.LLMClient
    }

    func NewHintGenerator(llm wayfinder.LLMClient) *HintGenerator {
        return &HintGenerator{llm: llm}
    }

    // GenerateHints analyzes parent messages and tool params to extract hints.
    func (h *HintGenerator) GenerateHints(
        ctx context.Context,
        parentMessages []session.Message,
        toolName string,
        toolInput map[string]any,
    ) (*Hints, error) {
        // Build prompt asking LLM to analyze parent context and extract:
        // - What does the parent agent want to know from this tool execution?
        // - What context is relevant?
        // - What constraints should the child session follow?
        hintPrompt := buildHintExtractionPrompt(parentMessages, toolName, toolInput)

        messages := []wayfinder.ChatMessage{
            {Role: "system", Content: hintSystemPrompt},
            {Role: "user", Content: hintPrompt},
        }

        resp, err := h.llm.GenerateMessage(ctx, "claude", messages, nil)
        if err != nil {
            return nil, err
        }

        return parseHintsFromResponse(resp.Content)
    }
    ```
*   **Logic**:
    *   `buildHintExtractionPrompt` は親メッセージの直近5件とツールパラメータを組み合わせてプロンプトを構築
    *   `hintSystemPrompt` は「親エージェントの目的を推定し、JSON形式の `Hints` を返せ」という指示
    *   `parseHintsFromResponse` はLLMの応答からJSON部分を抽出して `Hints` にデシリアライズ

#### [NEW] [summarizer.go](file://shared/libs/go/wayfinder/subagent/summarizer.go)
*   **Description**: 子セッションの出力を親セッション向けに要約・加工する
*   **Technical Design**:
    ```go
    package subagent

    import (
        "context"
        "fmt"
    )

    // Summarizer produces concise summaries for parent consumption.
    type Summarizer struct {
        llm wayfinder.LLMClient
    }

    func NewSummarizer(llm wayfinder.LLMClient) *Summarizer {
        return &Summarizer{llm: llm}
    }

    const summarySystemPrompt = `You are a result summarizer for a coding agent.
Given the raw output from a tool execution and the parent agent's objective,
produce a concise summary in the following format:

Status: [SUCCESS/FAILURE]
Summary: [3-5 line summary of results]
Key Findings / Errors: [Important errors, stack traces, or key data points if any]

Focus only on information relevant to the parent's objective.
Do NOT include progress logs, timestamps, or verbose output.`

    // SummarizeForParent takes child session output and produces a summary
    // tailored to the parent's needs based on hints.
    func (s *Summarizer) SummarizeForParent(ctx context.Context, hints *Hints, rawOutput string) (string, error) {
        prompt := fmt.Sprintf(
            "Parent's Objective: %s\nParent's Context: %s\n\nRaw Output:\n%s",
            hints.Objective, hints.Context, rawOutput,
        )

        messages := []wayfinder.ChatMessage{
            {Role: "system", Content: summarySystemPrompt},
            {Role: "user", Content: prompt},
        }

        resp, err := s.llm.GenerateMessage(ctx, "claude", messages, nil)
        if err != nil {
            return "", fmt.Errorf("summarization failed: %w", err)
        }

        return resp.Content, nil
    }
    ```
*   **Logic**:
    *   `summarySystemPrompt` の出力フォーマット:
        - `Status`: SUCCESS / FAILURE
        - `Summary`: 簡潔な要約 (3-5行)
        - `Key Findings / Errors`: 重要なエラーやスタックトレース (ない場合は "None")
    *   親の `Objective` に沿った情報のみを抽出し、進捗ログやタイムスタンプは排除

---

### wayfinder コアパッケージへの統合変更

#### [MODIFY] [agent_core.go](file://shared/libs/go/wayfinder/agent_core.go)
*   **Description**: ToolCallの実行時にSubagentExecutorを使用するオプションを追加
*   **Technical Design**:
    ```go
    // AgentCore に subagent フィールドを追加
    type AgentCore struct {
        config    *AgentConfig
        llm       LLMClient
        tools     *ToolRegistry
        tracker   *FileTracker
        subagent  *subagent.SubagentExecutor // nil if subagent is disabled
        logger    Logger
    }

    // SetSubagentExecutor configures the subagent executor.
    func (a *AgentCore) SetSubagentExecutor(exec *subagent.SubagentExecutor) {
        a.subagent = exec
    }
    ```
*   **Logic**:
    *   `executeTool` メソッド内で、`execute_command` ツールかつ `a.subagent != nil` の場合、直接実行ではなく `SubagentExecutor.Execute` に委譲する
    *   他のツール（`read_file`, `write_file` 等の軽量ツール）はこれまで通り直接実行

#### [MODIFY] [tool_execute_command.go](file://shared/libs/go/wayfinder/tools/tool_execute_command.go)
*   **Description**: バックグラウンド実行時のPID/コマンド名をTrackedProcessとして記録する機能を追加
*   **Logic**:
    *   バックグラウンドモード (`background: true`) で起動した場合、`ProcessTracker.Track(pid, command, args)` で記録
    *   この記録はSessionState.RunningProcessesとしてシリアライズされ、セッション復旧時の検証対象となる

---

### テストファイル (TDD: テストを先に記述)

#### [NEW] [subagent_executor_test.go](file://shared/libs/go/wayfinder/subagent/subagent_executor_test.go)
*   **テストケース**:
    *   `TestSubagentExecutor_Execute_Success`: MockLLMでヒント生成 -> 子AgentCore実行 -> 要約生成が正常に動作
    *   `TestSubagentExecutor_InheritConfig`: 子セッションが親のWorkDir/SessionDir/AllowedPathPatternsを引き継いでいること
    *   `TestSubagentExecutor_HintFallback`: ヒント生成に失敗してもツール実行が継続されること
    *   `TestSubagentExecutor_SummarizationFallback`: 要約に失敗しても生の結果が返ること
    *   `TestSubagentExecutor_ChildSessionFileCreated`: 子セッションファイルが SessionDir に保存されること

#### [NEW] [hint_generator_test.go](file://shared/libs/go/wayfinder/subagent/hint_generator_test.go)
*   **テストケース**:
    *   `TestGenerateHints_ExtractsObjective`: 親のメッセージからObjectiveが正しく抽出されること (MockLLM)
    *   `TestGenerateHints_ContextFromRecentMessages`: 直近5件のメッセージからcontextが生成されること
    *   `TestGenerateHints_LLMError`: LLMエラー時にerrorが返ること

#### [NEW] [summarizer_test.go](file://shared/libs/go/wayfinder/subagent/summarizer_test.go)
*   **テストケース**:
    *   `TestSummarizeForParent_BuildTest`: "ビルドが通るか確認" Objective -> Status/Summaryフォーマットの要約が返ること
    *   `TestSummarizeForParent_WarningFocus`: "警告を確認" Objective -> 警告にフォーカスした要約が返ること
    *   `TestSummarizeForParent_LLMError`: LLMエラー時にerrorが返ること

## Step-by-Step Implementation Guide

1.  **Hints構造体とHintGeneratorの実装** (TDD: テスト先行):
    *   `hint_generator_test.go` を作成し、全テストケースを記述 -> 失敗確認
    *   `hint_generator.go` に `Hints` 構造体、`HintGenerator`、`GenerateHints` を実装
    *   `git commit -m "feat(wayfinder): add HintGenerator for subagent context extraction"`

2.  **Summarizerの実装** (TDD: テスト先行):
    *   `summarizer_test.go` を作成し、全テストケースを記述 -> 失敗確認
    *   `summarizer.go` に `Summarizer`、`SummarizeForParent`、`summarySystemPrompt` を実装
    *   `git commit -m "feat(wayfinder): add Summarizer for child session output processing"`

3.  **SubagentExecutorの実装** (TDD: テスト先行):
    *   `subagent_executor_test.go` を作成し、全テストケースを記述 -> 失敗確認
    *   `subagent_executor.go` に `SubagentExecutor`、`Execute` を実装
    *   `git commit -m "feat(wayfinder): add SubagentExecutor with recursive AgentCore invocation"`

4.  **AgentCoreへの統合**:
    *   `agent_core.go` に `subagent` フィールドと `SetSubagentExecutor` を追加
    *   `executeTool` に `execute_command` のサブエージェント委譲ロジックを追加
    *   `tool_execute_command.go` にProcessTracker記録を追加
    *   `git commit -m "feat(wayfinder): integrate SubagentExecutor into AgentCore tool dispatch"`

5.  **ビルド・テスト実行**:
    *   Verification Planに従い全テスト実行

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```

2.  **Integration Tests**:
    ```bash
    ./scripts/process/integration_test.sh --categories llm,taskengine --specify "TestWayfinderSubagent"
    ```
    *   **Log Verification**: 子セッションファイルが `SessionDir` に作成されていること、親セッションの Messages にToolCallの結果として要約テキストのみが含まれていること。

3.  **E2E Tests (新規)**:

    #### [NEW] [wayfinder_subagent_test.go](file://tests/wayfinder_subagent_test.go)
    *   **テストケース**: `TestWayfinderE2E_SubagentCommandExecution`
        *   MockLLMClientを使用し、`execute_command` ツールでコマンドを実行するシナリオ
        *   子セッションが生成され、結果がLLMで要約されて親セッションに返却されること
        *   子セッションファイルが `SessionDir/[ChildID].json` に保存されていること
    *   **テストケース**: `TestWayfinderE2E_SubagentContextPreservation`
        *   大量出力(1000行以上)を生成するコマンドを実行
        *   親セッションのMessages長が大量出力そのものより大幅に短いこと(要約効果の確認)
    *   **テストケース**: `TestWayfinderE2E_SubagentHintsDrivenSummary`
        *   親のObjectiveを変えて同じコマンドを実行した場合に、要約内容が変化すること
    *   **検証ポイント**: 親子セッション間の情報フロー、コンテキスト消費の節約効果、ヒントによる要約の動的変化

### テスト項目のセルフレビュー (testing-rules 11.4)

1. **網羅性**: ヒント生成(成功/失敗)、要約(成功/失敗/Objective変化)、サブエージェント全体フロー(成功/フォールバック/設定継承)を網羅。
2. **証拠の十分性**: MockLLMClientで応答を制御し、出力フォーマット(Status/Summary/Key Findings)の構造を文字列マッチで検証。
3. **迂回排除**: 全テストでMockLLMClientを使用し、外部LLM依存なし。
4. **依存関係**: HintGenerator -> Summarizer -> SubagentExecutor -> AgentCore統合 の順にボトムアップ。

### 総合判定プロセス (testing-rules 12)

全テスト完了後、testing-rules 12.2のチェック項目を確認し、総合判定を記録する。

## Documentation

本計画は新規パッケージの作成のため、既存ドキュメントへの影響はない。

---

## 継続計画について

本計画はWayfinder Agent実装の **Part 3/4** です。

- **Part 1** ([000-Wayfinder-AgentCore-Tools-LLMGP.md](file://prompts/phases/000-foundation/branches/feat-wayfinder-agent/plans/000-Wayfinder-AgentCore-Tools-LLMGP.md)): エージェントコア、ツール、ガードレール、LLMGP統合
- **Part 2** ([001-Wayfinder-Session-Persistence.md](file://prompts/phases/000-foundation/branches/feat-wayfinder-agent/plans/001-Wayfinder-Session-Persistence.md)): セッション管理、永続化、コンパクション
- **Part 4** ([003-Wayfinder-WBS-Planning-Orchestration.md](file://prompts/phases/000-foundation/branches/feat-wayfinder-agent/plans/003-Wayfinder-WBS-Planning-Orchestration.md)): WBS計画生成、オーケストレーション、実行分岐
