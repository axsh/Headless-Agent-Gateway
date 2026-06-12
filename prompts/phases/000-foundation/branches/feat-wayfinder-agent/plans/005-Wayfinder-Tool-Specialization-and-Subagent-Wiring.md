# 005-Wayfinder-Tool-Specialization-and-Subagent-Wiring

> **Source Specification**: [005-Wayfinder-Tool-Specialization-and-Subagent-Wiring.md](file:///prompts/phases/000-foundation/branches/feat-wayfinder-agent/ideas/005-Wayfinder-Tool-Specialization-and-Subagent-Wiring.md)

## Goal Description

Wayfinder エージェントの3つの課題を解決する:
1. `execute_command` のバックグラウンド実行を専用ツール `run_background_process` に分離し、構造化レスポンスを返す
2. 実装済みだがAdapter層で未接続の全コンポーネント (SessionID, SubagentExecutor, Router, Planner) を接続する
3. WBSノード実行を子セッション(サブエージェント)で行うよう修正する

## User Review Required

- `execute_command` のサブエージェント委任について、常に委任するか、出力サイズ閾値ベースで判定するか。本計画では**常に委任**方式を採用する (サブエージェントが有効な場合)。
- `EnableSubagent` のデフォルト値を `false` にする。接続はするが、テストで動作確認するまでデフォルトではオフにしておく。

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R-001: execute_command から background を分離 | Proposed Changes > tool_command.go, tool_command_bg.go |
| R-002: execute_command はフォアグラウンド専用 | Proposed Changes > tool_command.go |
| R-003: kill_process の構造化 | Proposed Changes > tool_command.go |
| R-004: executeTool のルーティングロジック | Proposed Changes > agent_core.go |
| R-005: Adapter層で全コンポーネント接続 | Proposed Changes > adapter.go |
| R-006: AgentRunner の実装 | Proposed Changes > agent_runner.go |
| R-007: EnableSubagent フラグ | Proposed Changes > config.go |
| R-008: WBSノードの子セッション実行 | Proposed Changes > agent_core.go (agentNodeExecutor) |

---

## Proposed Changes

### Tools (shared/libs/go/wayfinder/tools)

#### [MODIFY] [tool_command_test.go](file:///shared/libs/go/wayfinder/tools/tool_command_test.go)
*   **Description**: テスト更新 - `execute_command` のbackgroundパラメータ削除と `kill_process` 構造化レスポンスに対応
*   **Technical Design**:
    - `TestExecuteCommand_Background` テストを削除 (backgroundは `run_background_process` に移行)
    - `TestKillProcess_StructuredResponse`: JSON形式の応答を検証
    - `TestKillProcess_AlreadyTerminated`: `already_terminated` ステータスを検証
    - `TestKillProcess_AccessDenied_ProcessGone`: Windowsでの `Access is denied` + プロセス不在を検証
*   **Logic**:
    - kill_processの応答を `{"status": "killed", "pid": 12345}` フォーマットで検証
    - `already_terminated` ケースは `os.ErrProcessDone` や `process already finished` エラーメッセージで判定

#### [MODIFY] [tool_command.go](file:///shared/libs/go/wayfinder/tools/tool_command.go)
*   **Description**: `execute_command` から `background` パラメータを削除、`kill_process` を構造化レスポンスに変更
*   **Technical Design**:
    ```go
    // newExecuteCommand - background分岐を完全に削除
    func newExecuteCommand(tc *ToolContext) ToolHandler {
        return func(ctx context.Context, input map[string]any) (string, error) {
            commandLine, _ := input["command"].(string)
            if commandLine == "" {
                return "", fmt.Errorf("execute_command: command is required")
            }
            if tc.IsBlockedCommand(commandLine) {
                return "", fmt.Errorf("execute_command: command is blocked: %s", commandLine)
            }
            // フォアグラウンド実行のみ (background分岐なし)
            cmd := exec.CommandContext(ctx, "sh", "-c", commandLine)
            // ... 既存のフォアグラウンド処理
        }
    }
    ```
    ```go
    // newKillProcess - 構造化レスポンスに変更
    func newKillProcess(tc *ToolContext) ToolHandler {
        return func(ctx context.Context, input map[string]any) (string, error) {
            // ... PIDパース (既存ロジック維持)
            proc, err := os.FindProcess(pid)
            if err != nil {
                return "", fmt.Errorf("kill_process: process %d not found: %w", pid, err)
            }
            if err := proc.Kill(); err != nil {
                // プロセスが既に終了しているケース
                if strings.Contains(err.Error(), "process already finished") ||
                    strings.Contains(err.Error(), "no such process") ||
                    strings.Contains(err.Error(), "Access is denied") {
                    tc.Tracker.UntrackProcess(pid)
                    result, _ := json.Marshal(map[string]any{
                        "status": "already_terminated", "pid": pid,
                    })
                    return string(result), nil
                }
                return "", fmt.Errorf("kill_process: failed to kill: %w", err)
            }
            tc.Tracker.UntrackProcess(pid)
            result, _ := json.Marshal(map[string]any{
                "status": "killed", "pid": pid,
            })
            return string(result), nil
        }
    }
    ```

#### [NEW] [tool_command_bg_test.go](file:///shared/libs/go/wayfinder/tools/tool_command_bg_test.go)
*   **Description**: `run_background_process` ツールのテスト
*   **Technical Design**:
    - `TestRunBackgroundProcess_Success`: 正常起動でJSON `{"status":"started","pid":XXXX,"command":"..."}` が返ること
    - `TestRunBackgroundProcess_BlockedCommand`: ブロックコマンドでエラー
    - `TestRunBackgroundProcess_EmptyCommand`: 空文字列でエラー
*   **Logic**:
    - `sleep 30` 等の軽量コマンドで起動テスト
    - 返却値を `json.Unmarshal` してフィールドを検証
    - テスト終了時にプロセスをクリーンアップ

#### [NEW] [tool_command_bg.go](file:///shared/libs/go/wayfinder/tools/tool_command_bg.go)
*   **Description**: `run_background_process` ツールハンドラ (構造化JSON返却)
*   **Technical Design**:
    ```go
    package tools

    import (
        "context"
        "encoding/json"
        "fmt"
        "os/exec"
    )

    // newRunBackgroundProcess creates the run_background_process tool handler.
    func newRunBackgroundProcess(tc *ToolContext) ToolHandler {
        return func(ctx context.Context, input map[string]any) (string, error) {
            command, _ := input["command"].(string)
            if command == "" {
                return "", fmt.Errorf("run_background_process: command is required")
            }
            if tc.IsBlockedCommand(command) {
                return "", fmt.Errorf("run_background_process: command blocked: %s", command)
            }

            cmd := exec.CommandContext(ctx, "sh", "-c", command)
            cmd.Dir = tc.WorkDir
            if err := cmd.Start(); err != nil {
                return "", fmt.Errorf("run_background_process: start failed: %w", err)
            }
            pid := cmd.Process.Pid
            tc.Tracker.TrackProcess(pid, command)
            go func() { _ = cmd.Wait() }()

            result, _ := json.Marshal(map[string]any{
                "status":  "started",
                "pid":     pid,
                "command": command,
            })
            return string(result), nil
        }
    }
    ```

#### [MODIFY] [register.go](file:///shared/libs/go/wayfinder/tools/register.go)
*   **Description**: `run_background_process` を登録、`execute_command` から `background` パラメータを削除
*   **Technical Design**:
    ```go
    // execute_command - background プロパティを削除
    reg.Register("execute_command", "Execute a shell command (foreground only)",
        map[string]any{
            "type": "object",
            "properties": map[string]any{
                "command": map[string]any{"type": "string", "description": "Shell command to execute"},
            },
            "required": []string{"command"},
        }, newExecuteCommand(tc))

    // run_background_process - 新規追加
    reg.Register("run_background_process", "Start a command as a background process and return its PID",
        map[string]any{
            "type": "object",
            "properties": map[string]any{
                "command": map[string]any{"type": "string", "description": "Shell command to run in background"},
            },
            "required": []string{"command"},
        }, newRunBackgroundProcess(tc))
    ```

---

### AgentCore (shared/libs/go/wayfinder)

#### [MODIFY] [config.go](file:///shared/libs/go/wayfinder/config.go)
*   **Description**: `EnableSubagent` フラグを追加
*   **Technical Design**:
    ```go
    type AgentConfig struct {
        WorkDir             string
        SessionDir          string
        LogicalModel        string
        AllowedPathPatterns []string
        SystemPrompt        string
        EnableSubagent      bool   // サブエージェント委任を有効化 (デフォルト: false)
    }
    ```

#### [NEW] [agent_runner.go](file:///shared/libs/go/wayfinder/agent_runner.go)
*   **Description**: `subagent.AgentRunner` インターフェースの具象実装。子AgentCoreを生成して実行する。
*   **Technical Design**:
    ```go
    package wayfinder

    import (
        "context"
        "github.com/axsh/arctic-tern/logger"
        "github.com/axsh/arctic-tern/wayfinder/subagent"
    )

    // AgentRunnerImpl implements subagent.AgentRunner.
    // It creates a child AgentCore instance and runs it with the given prompt.
    type AgentRunnerImpl struct {
        baseURL string
        token   string
    }

    // NewAgentRunnerImpl creates a new AgentRunnerImpl.
    func NewAgentRunnerImpl(baseURL, token string) *AgentRunnerImpl {
        return &AgentRunnerImpl{baseURL: baseURL, token: token}
    }

    // RunChild creates a child AgentCore and runs it with the prompt.
    func (r *AgentRunnerImpl) RunChild(
        ctx context.Context,
        cfg *subagent.AgentRunnerConfig,
        sessionID string,
        llm subagent.LLMClient,
        log logger.Logger,
        prompt string,
    ) (string, error) {
        // Create child config.
        childCfg := &AgentConfig{
            WorkDir:             cfg.WorkDir,
            SessionDir:          cfg.SessionDir,
            LogicalModel:        cfg.LogicalModel,
            AllowedPathPatterns: cfg.AllowedPathPatterns,
            EnableSubagent:      false, // 子セッションではサブエージェントを無効化 (無限再帰防止)
        }
        if err := InitConfig(childCfg); err != nil {
            return "", err
        }

        // Wrap subagent.LLMClient to wayfinder.LLMClient.
        wrappedLLM := &llmClientAdapter{inner: llm}

        // Create child AgentCore.
        child := NewAgentCore(wrappedLLM, childCfg, log)
        child.SetSessionID(sessionID)

        // Run child with the prompt.
        return child.Run(ctx, prompt)
    }

    // llmClientAdapter wraps subagent.LLMClient to satisfy wayfinder.LLMClient.
    type llmClientAdapter struct {
        inner subagent.LLMClient
    }

    func (a *llmClientAdapter) GenerateMessage(
        ctx context.Context, model string, msgs []ChatMessage, tools []ToolDefinition,
    ) (*LLMResponse, error) {
        // Convert wayfinder types to subagent types.
        subMsgs := make([]subagent.ChatMessage, len(msgs))
        for i, m := range msgs {
            subMsgs[i] = subagent.ChatMessage{
                Role:    m.Role,
                Content: m.Content,
            }
            for _, tc := range m.ToolCalls {
                subMsgs[i].ToolCalls = append(subMsgs[i].ToolCalls, subagent.ToolCall{
                    ID: tc.ID, Name: tc.Name, Input: tc.Input,
                })
            }
            subMsgs[i].ToolCallID = m.ToolCallID
        }
        subTools := make([]subagent.ToolDefinition, len(tools))
        for i, t := range tools {
            subTools[i] = subagent.ToolDefinition{
                Name: t.Name, Description: t.Description, InputSchema: t.InputSchema,
            }
        }
        resp, err := a.inner.GenerateMessage(ctx, model, subMsgs, subTools)
        if err != nil {
            return nil, err
        }
        result := &LLMResponse{Content: resp.Content}
        for _, tc := range resp.ToolCalls {
            result.ToolCalls = append(result.ToolCalls, ToolCall{
                ID: tc.ID, Name: tc.Name, Input: tc.Input,
            })
        }
        return result, nil
    }
    ```

#### [MODIFY] [agent_core.go](file:///shared/libs/go/wayfinder/agent_core.go)
*   **Description**: executeTool のルーティングロジック変更、agentNodeExecutor を子セッション方式に変更
*   **Technical Design** (executeTool):
    ```go
    func (ac *AgentCore) executeTool(ctx context.Context, tc ToolCall) string {
        // サブエージェント対象ツールのリスト (execute_command のみ)
        shouldDelegate := ac.subagent != nil && ac.config.EnableSubagent &&
            tc.Name == "execute_command"

        if shouldDelegate {
            ac.logger.Debug("delegating to subagent", "tool", tc.Name)
            // ParentMessage への変換
            parentMsgs := make([]subagent.ParentMessage, 0, len(ac.messages))
            for _, m := range ac.messages {
                parentMsgs = append(parentMsgs, subagent.ParentMessage{
                    Role: m.Role, Content: m.Content,
                })
            }
            result, err := ac.subagent.Execute(ctx, parentMsgs, tc.Name, tc.Input)
            if err != nil {
                ac.logger.Debug("subagent failed", "error", err.Error())
                return fmt.Sprintf("Error: %v", err)
            }
            return result
        }

        // 直接実行
        tool, ok := ac.registry.Get(tc.Name)
        if !ok {
            return fmt.Sprintf("Error: unknown tool %q", tc.Name)
        }
        result, err := tool.Handler(ctx, tc.Input)
        if err != nil {
            return fmt.Sprintf("Error: %v", err)
        }
        return result
    }
    ```
*   **Technical Design** (agentNodeExecutor):
    ```go
    // agentNodeExecutor - 子セッション方式に変更
    type agentNodeExecutor struct {
        parentSessionID string
        childConfig     *subagent.AgentRunnerConfig
        runner          subagent.AgentRunner
        llm             subagent.LLMClient  // Summarizer用
        summarizer      *subagent.Summarizer
        logger          logger.Logger
    }

    func (e *agentNodeExecutor) ExecuteNode(ctx context.Context, node planning.WBSNode) (string, error) {
        prompt := fmt.Sprintf("[WBS Step %s: %s]\n%s", node.ID, node.Name, node.Description)
        childSessionID := fmt.Sprintf("%s-wbs-%s", e.parentSessionID, node.ID)

        childResult, err := e.runner.RunChild(ctx, e.childConfig, childSessionID, e.llm, e.logger, prompt)
        if err != nil {
            return "", err
        }

        // 結果を要約
        hints := &subagent.Hints{Objective: node.Name, Context: node.Description}
        summary, err := e.summarizer.SummarizeForParent(ctx, hints, childResult)
        if err != nil {
            e.logger.Warn("summarization failed, using raw result", "error", err.Error())
            return childResult, nil
        }
        return summary, nil
    }
    ```
*   **Technical Design** (runWithWBSTree):
    ```go
    func (ac *AgentCore) runWithWBSTree(ctx context.Context, tree *planning.WBSTree) (string, error) {
        // 子セッション実行用の agentNodeExecutor を構築
        var nodeExec planning.NodeExecutor

        if ac.subagent != nil && ac.config.EnableSubagent {
            // サブエージェント有効: 子セッションで実行
            childCfg := &subagent.AgentRunnerConfig{
                WorkDir:      ac.config.WorkDir,
                SessionDir:   ac.config.SessionDir,
                LogicalModel: ac.config.LogicalModel,
            }
            // runner は ac から取得する必要がある → AgentCore に runner フィールド追加
            nodeExec = &agentNodeExecutor{
                parentSessionID: ac.sessionID,
                childConfig:     childCfg,
                runner:          ac.runner,
                llm:             ac.subagentLLM,
                summarizer:      subagent.NewSummarizer(ac.subagentLLM),
                logger:          ac.logger,
            }
        } else {
            // サブエージェント無効: 従来通り親で実行 (フォールバック)
            nodeExec = &agentNodeExecutorSimple{core: ac}
        }

        persister := &agentWBSPersister{core: ac}
        orch := planning.NewWBSOrchestrator(nodeExec, persister, ac.logger)
        if err := orch.Execute(ctx, tree); err != nil {
            return "", fmt.Errorf("WBS orchestration failed: %w", err)
        }
        return planning.CollectResults(tree), nil
    }

    // agentNodeExecutorSimple is the fallback (existing behavior).
    type agentNodeExecutorSimple struct {
        core *AgentCore
    }

    func (e *agentNodeExecutorSimple) ExecuteNode(ctx context.Context, node planning.WBSNode) (string, error) {
        prompt := fmt.Sprintf("[WBS Step %s: %s]\n%s", node.ID, node.Name, node.Description)
        return e.core.runSimple(ctx, prompt)
    }
    ```
*   **AgentCore struct に追加フィールド**:
    ```go
    type AgentCore struct {
        // ... existing fields ...
        runner      subagent.AgentRunner  // AgentRunner for child sessions
        subagentLLM subagent.LLMClient    // LLMClient for subagent (summarizer etc.)
    }
    ```
*   **Setter追加**:
    ```go
    func (ac *AgentCore) SetRunner(runner subagent.AgentRunner) {
        ac.runner = runner
    }
    func (ac *AgentCore) SetSubagentLLM(llm subagent.LLMClient) {
        ac.subagentLLM = llm
    }
    ```

#### [MODIFY] [adapter.go](file:///shared/libs/go/wayfinder/adapter.go)
*   **Description**: 全コンポーネントを AgentCore に接続
*   **Technical Design**:
    ```go
    func (a *Adapter) CreateSession(ctx context.Context, opts ...codingagent.SessionOption) (codingagent.Session, error) {
        cfg := codingagent.NewSessionConfig(opts...)

        if cfg.WorkDir == "" {
            cfg.WorkDir = "."
        }

        agentCfg := &AgentConfig{
            WorkDir:        cfg.WorkDir,
            SessionDir:     cfg.SessionDir,
            LogicalModel:   cfg.Model,
            EnableSubagent: false, // デフォルト: オフ (将来的にtrue)
        }
        if err := InitConfig(agentCfg); err != nil {
            return nil, fmt.Errorf("wayfinder: init config: %w", err)
        }

        llmClient := NewBifrostClient(a.baseURL, a.token)
        core := NewAgentCore(llmClient, agentCfg, a.logger)

        // === 全コンポーネント接続 ===

        // 1. セッションIDを設定
        sessionID := cfg.AgentSessionID
        if sessionID == "" {
            sessionID = generateSessionID()
        }
        core.SetSessionID(sessionID)

        // 2. セッション復元 (既存セッションの再開時)
        if cfg.AgentSessionID != "" {
            a.logger.Debug("resuming session", "session_id", cfg.AgentSessionID)
            // restoreSession は Run() 内で自動実行される
        }

        // 3. ExecutionRouter を接続
        router := NewExecutionRouter(llmClient)
        core.SetRouter(router)

        // 4. WBSPlanner を接続
        planner := planning.NewWBSPlanner(llmClient)
        core.SetPlanner(planner)

        // 5. AgentRunner + SubagentExecutor を接続
        runner := NewAgentRunnerImpl(a.baseURL, a.token)
        core.SetRunner(runner)

        // subagent の LLMClient をセット (Summarizer, HintGenerator用)
        subLLM := newSubagentLLMAdapter(llmClient)
        core.SetSubagentLLM(subLLM)

        parentCfg := &subagent.AgentRunnerConfig{
            WorkDir:      agentCfg.WorkDir,
            SessionDir:   agentCfg.SessionDir,
            LogicalModel: agentCfg.LogicalModel,
        }
        subExec := subagent.NewSubagentExecutor(parentCfg, subLLM, runner, a.logger)
        core.SetSubagentExecutor(subExec)

        a.logger.Info("session created",
            "session_id", sessionID,
            "model", agentCfg.LogicalModel,
            "work_dir", agentCfg.WorkDir,
        )

        return &wayfinderSession{
            id:     sessionID,
            core:   core,
            config: agentCfg,
            logger: a.logger,
            prompt: cfg.Prompt,
        }, nil
    }

    // newSubagentLLMAdapter wraps BifrostClient to subagent.LLMClient.
    func newSubagentLLMAdapter(bc *BifrostClient) subagent.LLMClient {
        return &subagentLLMAdapter{bc: bc}
    }

    type subagentLLMAdapter struct {
        bc *BifrostClient
    }

    func (a *subagentLLMAdapter) GenerateMessage(
        ctx context.Context, model string, msgs []subagent.ChatMessage, tools []subagent.ToolDefinition,
    ) (*subagent.LLMResponse, error) {
        // Convert subagent types to wayfinder types, call BifrostClient, convert back.
        wfMsgs := make([]ChatMessage, len(msgs))
        for i, m := range msgs {
            wfMsgs[i] = ChatMessage{Role: m.Role, Content: m.Content, ToolCallID: m.ToolCallID}
            for _, tc := range m.ToolCalls {
                wfMsgs[i].ToolCalls = append(wfMsgs[i].ToolCalls, ToolCall{
                    ID: tc.ID, Name: tc.Name, Input: tc.Input,
                })
            }
        }
        wfTools := make([]ToolDefinition, len(tools))
        for i, t := range tools {
            wfTools[i] = ToolDefinition{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema}
        }
        resp, err := a.bc.GenerateMessage(ctx, model, wfMsgs, wfTools)
        if err != nil {
            return nil, err
        }
        result := &subagent.LLMResponse{Content: resp.Content}
        for _, tc := range resp.ToolCalls {
            result.ToolCalls = append(result.ToolCalls, subagent.ToolCall{
                ID: tc.ID, Name: tc.Name, Input: tc.Input,
            })
        }
        return result, nil
    }
    ```

---

### E2E Tests (tests/)

#### [MODIFY] [wayfinder_e2e_test.go](file:///tests/wayfinder_e2e_test.go)
*   **Description**: E2Eテストを新ツール構成 (`run_background_process`, 構造化 `kill_process`) に対応
*   **Technical Design**:
    - Step 4 のプロンプト変更: 「Start a background process...」→ LLMが `run_background_process` を使うことを期待
    - PID抽出ロジックを構造化JSON対応に変更:
      ```go
      // 構造化JSON対応のPID抽出
      func extractPIDFromOutput(output string) (int, error) {
          // まずJSONからの抽出を試みる
          var result map[string]any
          if err := json.Unmarshal([]byte(output), &result); err == nil {
              if pid, ok := result["pid"].(float64); ok {
                  return int(pid), nil
              }
          }
          // フォールバック: テキスト応答からの正規表現抽出 (既存ロジック)
          // ...
      }
      ```
    - Step 5 の kill 結果検証を構造化JSON対応に変更

---

## Step-by-Step Implementation Guide

### Phase 1: ツール層の変更 (TDD)

1. **テスト先行: run_background_process テスト作成**:
    - `tool_command_bg_test.go` を新規作成
    - `TestRunBackgroundProcess_Success`, `TestRunBackgroundProcess_BlockedCommand`, `TestRunBackgroundProcess_EmptyCommand` を実装
    - テスト失敗を確認

2. **run_background_process ハンドラ実装**:
    - `tool_command_bg.go` を新規作成
    - テスト成功を確認

3. **テスト先行: kill_process 構造化テスト追加**:
    - `tool_command_test.go` に `TestKillProcess_StructuredResponse`, `TestKillProcess_AlreadyTerminated` を追加
    - テスト失敗を確認

4. **kill_process の構造化レスポンス実装**:
    - `tool_command.go` の `newKillProcess` を修正
    - テスト成功を確認

5. **execute_command から background 削除**:
    - `tool_command.go` の `newExecuteCommand` から background 分岐を削除
    - `tool_command_test.go` の background テストを削除
    - テスト成功を確認

6. **register.go 更新**:
    - `run_background_process` を登録
    - `execute_command` のスキーマから `background` を削除
    - コミット: `feat(wayfinder): add run_background_process, structurize kill_process`

### Phase 2: AgentCore の変更

7. **config.go に EnableSubagent 追加**:
    - `AgentConfig` に `EnableSubagent bool` を追加
    - コミット: `feat(wayfinder): add EnableSubagent config flag`

8. **agent_runner.go 新規作成**:
    - `AgentRunnerImpl` と `llmClientAdapter` を実装
    - コミット: `feat(wayfinder): implement AgentRunnerImpl for child sessions`

9. **agent_core.go の executeTool ルーティング変更**:
    - `shouldDelegate` 判定ロジックを追加
    - `ParentMessage` 変換を修正
    - コミット: `feat(wayfinder): update executeTool subagent routing`

10. **agent_core.go の agentNodeExecutor を子セッション方式に変更**:
    - `agentNodeExecutor` 構造体にフィールド追加
    - `agentNodeExecutorSimple` をフォールバック用に追加
    - `runWithWBSTree` を更新
    - AgentCore に `runner`, `subagentLLM` フィールドと Setter を追加
    - コミット: `feat(wayfinder): WBS nodes execute in child sessions`

### Phase 3: Adapter接続

11. **adapter.go の全コンポーネント接続**:
    - `CreateSession()` 内で5つのSetterを全て呼び出す
    - `subagentLLMAdapter` の型変換ヘルパーを追加
    - コミット: `feat(wayfinder): wire all components in adapter`

### Phase 4: E2Eテスト更新

12. **wayfinder_e2e_test.go の更新**:
    - PID抽出をJSON対応に変更
    - Step 4/5 のプロンプトとアサーションを調整
    - コミット: `test(wayfinder): update E2E for new tool structure`

### Phase 5: ビルドと検証

13. **Verification Plan の実行** (下記参照)

---

## Verification Plan

### Automated Verification

1. **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```

2. **Integration Tests (E2E)**:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify 'TestE2E_Wayfinder_FullScenario_Claude'
    ```
    ```bash
    ./scripts/process/integration_test.sh --specify 'TestE2E_Wayfinder_FullScenario_GPTCodex'
    ```
    ```bash
    ./scripts/process/integration_test.sh --specify 'TestE2E_Wayfinder_FullScenario_Gemini'
    ```
    *   **Log Verification**:
        - `run_background_process` ツールが使用されていること (ログに `tool_use: run_background_process`)
        - `kill_process` のレスポンスがJSON形式であること
        - PID抽出がJSON解析で成功すること
        - セッションIDがログに記録されていること (`session_id` フィールド)

3. **E2E Tests**:

    E2Eテストは既存の `tests/wayfinder_e2e_test.go` を更新して対応する。新規E2Eテストの追加は不要 (既存テストが全ステップをカバー)。

    #### [MODIFY] [wayfinder_e2e_test.go](file:///tests/wayfinder_e2e_test.go)
    *   **テストケース**: `TestE2E_Wayfinder_FullScenario_Claude/GPTCodex/Gemini`
    *   **検証ポイント**:
        - Step 4: LLMが `run_background_process` を選択し、JSON構造化レスポンスが返ること
        - Step 5: `kill_process` がJSON構造化レスポンスを返すこと
        - PID抽出がJSONパースで成功すること

### セルフレビュー結果

1. **要件対比チェック**: R-001〜R-008 全て Implementation Point にマッピング済み
2. **再現性チェック**: 全コードスニペットは実装可能な詳細度
3. **データ構造チェック**: `AgentConfig`, `agentNodeExecutor`, `AgentRunnerImpl`, `llmClientAdapter`, `subagentLLMAdapter` 全て記載
4. **テスト網羅性**: ツールのUnit Test (TDD), Agent CoreのルーティングTest, E2Eテスト全てカバー
5. **統合テスト実行プラン**: `--specify` で個別シナリオ指定済み

---

## Documentation

#### [MODIFY] [005-Wayfinder-Tool-Specialization-and-Subagent-Wiring.md](file:///prompts/phases/000-foundation/branches/feat-wayfinder-agent/ideas/005-Wayfinder-Tool-Specialization-and-Subagent-Wiring.md)
*   **更新内容**: 実装完了後、仕様書の各要件にステータス (完了/未完了) を追記
