# 066-Subagent-History-Integration-And-Command-Timeout

> **Source Specification**: [055-Subagent-History-Integration-And-Command-Timeout.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/ideas/055-Subagent-History-Integration-And-Command-Timeout.md)

## Goal Description

SubagentExecutor 経路で生成される子セッション履歴を親セッションの `history/` 配下にサブディレクトリとして格納する。併せて `execute_command` ツールにタイムアウト機構を追加し、長時間実行プロセスによるエージェントブロックを防止する。

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1-1: Execute に parentSeqFunc コールバックを渡す | Step 2: subagent_executor.go |
| R1-2: childConfig.HistorySubDir に親 Seq を設定 | Step 2: subagent_executor.go Execute() |
| R1-3: 子セッション履歴が history/{seqHex}/ に格納される | Step 2 + 既存 RunChild の WithSubDir ロジック |
| R1-4: UUID セッションIDは維持 | 変更なし (既存 uuid.New() をそのまま使用) |
| R2-1: デフォルトタイムアウト 120秒 | Step 3: tool_command.go |
| R2-2: timeout_seconds パラメータでカスタム指定 | Step 3: tool_command.go |
| R2-3: タイムアウト時のプロセス強制終了+メッセージ | Step 3: tool_command.go |
| R2-4: ツール定義に timeout_seconds 追加 | Step 3: register.go |

## Proposed Changes

### subagent パッケージ

#### [MODIFY] [subagent_executor_test.go](file://shared/libs/go/wayfinder/subagent/subagent_executor_test.go) (テストファースト)
*   **Description**: `Execute` で `HistorySubDir` が設定されることを検証するテスト追加
*   **Technical Design**:
    ```go
    func TestSubagentExecutor_Execute_SetsHistorySubDir(t *testing.T) {
        // Arrange: parentSeqFunc returns 10 (= "000000a" in hex)
        mockRunner := &mockAgentRunner{}
        mockLLM := &mockLLMClient{}
        parentCfg := &AgentRunnerConfig{
            WorkDir:    "/tmp/work",
            SessionDir: "/tmp/sessions",
        }
        exec := NewSubagentExecutor(parentCfg, mockLLM, mockRunner, nil,
            WithParentSeqFunc(func() int { return 10 }),
        )

        // Act
        exec.Execute(context.Background(), nil, "execute_command", map[string]any{"command": "echo hi"})

        // Assert: mockRunner should have received HistorySubDir = "000000a"
        if mockRunner.lastConfig == nil {
            t.Fatal("expected RunChild to be called")
        }
        if mockRunner.lastConfig.HistorySubDir != "000000a" {
            t.Errorf("HistorySubDir = %q, want %q", mockRunner.lastConfig.HistorySubDir, "000000a")
        }
    }

    func TestSubagentExecutor_Execute_NoParentSeqFunc(t *testing.T) {
        // Without WithParentSeqFunc, HistorySubDir should be empty.
        mockRunner := &mockAgentRunner{}
        mockLLM := &mockLLMClient{}
        parentCfg := &AgentRunnerConfig{
            WorkDir:    "/tmp/work",
            SessionDir: "/tmp/sessions",
        }
        exec := NewSubagentExecutor(parentCfg, mockLLM, mockRunner, nil)

        exec.Execute(context.Background(), nil, "execute_command", map[string]any{"command": "echo hi"})

        if mockRunner.lastConfig != nil && mockRunner.lastConfig.HistorySubDir != "" {
            t.Errorf("HistorySubDir = %q, want empty", mockRunner.lastConfig.HistorySubDir)
        }
    }

    // mockAgentRunner captures the last RunChild call.
    type mockAgentRunner struct {
        lastConfig    *AgentRunnerConfig
        lastSessionID string
    }

    func (m *mockAgentRunner) RunChild(ctx context.Context, cfg *AgentRunnerConfig, sessionID string, llm LLMClient, log logger.Logger, prompt string) (string, error) {
        m.lastConfig = cfg
        m.lastSessionID = sessionID
        return "child result", nil
    }
    ```

#### [MODIFY] [subagent_executor.go](file://shared/libs/go/wayfinder/subagent/subagent_executor.go)
*   **Description**: `SubagentExecutor` に `parentSeqFunc` フィールド追加、`WithParentSeqFunc` オプション追加、`Execute` で `HistorySubDir` 設定
*   **Technical Design**:
    *   構造体変更:
        ```go
        type SubagentExecutor struct {
            parentConfig   *AgentRunnerConfig
            llm            LLMClient
            runner         AgentRunner
            hints          *HintGenerator
            summarizer     SummaryStrategy
            logger         logger.Logger
            parentSeqFunc  func() int // Returns parent's current nextSeq for history subdirectory naming.
        }
        ```
    *   オプション型追加:
        ```go
        // SubagentOption configures optional SubagentExecutor behavior.
        type SubagentOption func(*SubagentExecutor)

        // WithParentSeqFunc sets the callback to retrieve parent's current sequence number.
        func WithParentSeqFunc(f func() int) SubagentOption {
            return func(e *SubagentExecutor) {
                e.parentSeqFunc = f
            }
        }
        ```
    *   `NewSubagentExecutor` シグネチャ変更:
        ```go
        func NewSubagentExecutor(
            parentCfg *AgentRunnerConfig,
            llm LLMClient,
            runner AgentRunner,
            log logger.Logger,
            opts ...SubagentOption,
        ) *SubagentExecutor {
            if log == nil {
                log = &noopLog{}
            }
            e := &SubagentExecutor{
                parentConfig: parentCfg,
                llm:          llm,
                runner:       runner,
                hints:        NewHintGenerator(llm),
                summarizer:   NewSummarizer(llm),
                logger:       log,
            }
            for _, opt := range opts {
                opt(e)
            }
            return e
        }
        ```
    *   `Execute` メソッド L120-128 変更:
        ```go
        // 2. Create child session config (inherit WorkDir and SessionDir).
        childSessionID := uuid.New().String()
        childConfig := &AgentRunnerConfig{
            WorkDir:             e.parentConfig.WorkDir,
            SessionDir:          e.parentConfig.SessionDir,
            LogicalModel:        e.parentConfig.LogicalModel,
            AllowedPathPatterns: e.parentConfig.AllowedPathPatterns,
            Emitter:             e.parentConfig.Emitter,
        }

        // Set HistorySubDir from parent's current sequence number.
        if e.parentSeqFunc != nil {
            childConfig.HistorySubDir = fmt.Sprintf("%07x", e.parentSeqFunc())
        }
        ```

---

### wayfinder パッケージ (呼び出し元)

#### [MODIFY] [adapter.go](file://shared/libs/go/wayfinder/adapter.go)
*   **Description**: `NewSubagentExecutor` 呼び出しに `WithParentSeqFunc` を渡す
*   **Technical Design**:
    *   L108 変更:
        ```go
        subExec := subagent.NewSubagentExecutor(parentCfg, subLLM, runner, a.logger,
            subagent.WithParentSeqFunc(func() int { return core.NextSeq() }),
        )
        ```

#### [MODIFY] [agent_core.go](file://shared/libs/go/wayfinder/agent_core.go)
*   **Description**: `NextSeq` メソッドを追加 (parentSeqFunc 用の公開アクセサ)
*   **Technical Design**:
    ```go
    // NextSeq returns the current next sequence number.
    // Used by SubagentExecutor to determine history subdirectory naming.
    func (ac *AgentCore) NextSeq() int {
        return ac.nextSeq
    }
    ```

---

### tools パッケージ

#### [MODIFY] [tools_test.go](file://shared/libs/go/wayfinder/tools/tools_test.go) (テストファースト)
*   **Description**: execute_command タイムアウトのテスト追加
*   **Technical Design**:
    ```go
    func TestExecuteCommand_Timeout(t *testing.T) {
        tc := newTestToolContext(t)
        handler := newExecuteCommand(tc)

        // Use a short timeout (2 seconds) with a command that would run longer.
        result, err := handler(context.Background(), map[string]any{
            "command":         "sleep 30",
            "timeout_seconds": float64(2),
        })
        if err != nil {
            t.Fatalf("unexpected error: %v", err)
        }
        if !strings.Contains(result, "Command timed out after 2 seconds") {
            t.Errorf("expected timeout message in result, got: %s", result)
        }
    }

    func TestExecuteCommand_NoTimeout(t *testing.T) {
        tc := newTestToolContext(t)
        handler := newExecuteCommand(tc)

        result, err := handler(context.Background(), map[string]any{
            "command": "echo hello",
        })
        if err != nil {
            t.Fatalf("unexpected error: %v", err)
        }
        if strings.Contains(result, "timed out") {
            t.Errorf("unexpected timeout message in result: %s", result)
        }
        if !strings.Contains(result, "hello") {
            t.Errorf("expected 'hello' in result, got: %s", result)
        }
    }
    ```

#### [MODIFY] [tool_command.go](file://shared/libs/go/wayfinder/tools/tool_command.go)
*   **Description**: `newExecuteCommand` にタイムアウト機構を追加
*   **Technical Design**:
    *   import に `"time"` を追加
    *   `newExecuteCommand` 関数を以下に変更:
        ```go
        func newExecuteCommand(tc *ToolContext) ToolHandler {
            return func(ctx context.Context, input map[string]any) (string, error) {
                commandLine, _ := input["command"].(string)
                if commandLine == "" {
                    return "", fmt.Errorf("execute_command: command is required")
                }

                // Check blocked commands.
                if tc.IsBlockedCommand(commandLine) {
                    return "", fmt.Errorf("execute_command: command is blocked for safety: %s", commandLine)
                }

                // Determine timeout (default: 120 seconds).
                timeout := 120 * time.Second
                if t, ok := input["timeout_seconds"].(float64); ok && t > 0 {
                    timeout = time.Duration(t) * time.Second
                }

                // Create timeout context.
                execCtx, cancel := context.WithTimeout(ctx, timeout)
                defer cancel()

                // Foreground execution with combined output.
                cmd := exec.CommandContext(execCtx, "sh", "-c", commandLine)
                cmd.Dir = tc.WorkDir
                var stdout, stderr bytes.Buffer
                cmd.Stdout = &stdout
                cmd.Stderr = &stderr

                err := cmd.Run()
                result := stdout.String()
                if stderr.Len() > 0 {
                    result += "\nSTDERR:\n" + stderr.String()
                }
                if execCtx.Err() == context.DeadlineExceeded {
                    result += fmt.Sprintf("\nCommand timed out after %d seconds", int(timeout.Seconds()))
                } else if err != nil {
                    result += fmt.Sprintf("\nExit error: %v", err)
                }

                // Truncate output to prevent context overflow.
                const maxOutputLen = 100000
                if len(result) > maxOutputLen {
                    result = result[:maxOutputLen] + "\n... (output truncated)"
                }

                return result, nil
            }
        }
        ```

#### [MODIFY] [register.go](file://shared/libs/go/wayfinder/tools/register.go)
*   **Description**: execute_command のツール定義に `timeout_seconds` パラメータ追加
*   **Technical Design**:
    *   L72-79 変更:
        ```go
        reg.Register("execute_command", "Execute a shell command (foreground only)",
            map[string]any{
                "type": "object",
                "properties": map[string]any{
                    "command":         map[string]any{"type": "string", "description": "Shell command to execute"},
                    "timeout_seconds": map[string]any{"type": "integer", "description": "Maximum execution time in seconds (default: 120)"},
                },
                "required": []string{"command"},
            }, newExecuteCommand(tc))
        ```

## Step-by-Step Implementation Guide

### Step 1: AgentCore.NextSeq アクセサ追加
- [ ] `agent_core.go`: `NextSeq()` メソッド追加
- [ ] ビルド確認: `./scripts/process/build.sh`
- [ ] git commit

### Step 2: SubagentExecutor に parentSeqFunc 注入 (TDD)
- [ ] テストファースト: `subagent_executor_test.go` にテスト追加
- [ ] `subagent_executor.go`: `parentSeqFunc` フィールド、`SubagentOption` 型、`WithParentSeqFunc`、`NewSubagentExecutor` 変更、`Execute` で `HistorySubDir` 設定
- [ ] `adapter.go`: `NewSubagentExecutor` 呼び出しに `WithParentSeqFunc` 追加
- [ ] ビルド確認: `./scripts/process/build.sh`
- [ ] git commit

### Step 3: execute_command タイムアウト (TDD)
- [ ] テストファースト: `tools_test.go` にタイムアウトテスト追加
- [ ] `tool_command.go`: タイムアウトロジック追加
- [ ] `register.go`: `timeout_seconds` パラメータ追加
- [ ] ビルド確認: `./scripts/process/build.sh`
- [ ] git commit

### Step 4: ビルドと検証
- [ ] 全体ビルド: `./scripts/process/build.sh`
- [ ] git push

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```
    *   全単体テスト PASS を確認
    *   特に以下のテストが PASS すること:
        *   `TestSubagentExecutor_Execute_SetsHistorySubDir`
        *   `TestSubagentExecutor_Execute_NoParentSeqFunc`
        *   `TestExecuteCommand_Timeout`
        *   `TestExecuteCommand_NoTimeout`

2.  **E2E Tests**:

    E2E テストは今回追加しない。理由:
    *   R1 は内部の設定パス追加であり外部 API に変更なし。SubagentExecutor のテストは単体テスト (`mockAgentRunner`) で十分にカバーされる
    *   R2 はツール内部のタイムアウト追加であり、ツールの入出力インターフェースに構造的変更なし。単体テスト (`TestExecuteCommand_Timeout`) でタイムアウト動作を検証済み

### セルフレビュー結果

1.  **要件対比チェック**: 全 R1-1〜R1-4、R2-1〜R2-4 が Traceability テーブルでマッピング済み
2.  **再現性チェック**: 各 Step で変更対象ファイル、変更内容、コードスニペットが具体的に記述
3.  **データ構造チェック**: `SubagentExecutor` の構造体変更、`SubagentOption` 型の追加が全て記載
4.  **テスト網羅性チェック**: SubagentExecutor テスト (parentSeqFunc あり/なし)、execute_command テスト (タイムアウトあり/正常完了) の4ケース計画済み
5.  **統合テスト実行プランチェック**: 内部リファクタリング+ツール改善のため build.sh PASS で十分。統合テストの追加は不要
6.  **テスト項目設計**: ボトムアップ順序 (agent_core -> subagent_executor -> tools)
7.  **総合判定**: 全 Step 完了後に `build.sh` PASS + git push で完了判定
8.  **E2Eテストコード化チェック**: 外部インターフェース変更なしのため E2E 追加不要、理由を明記済み

## Documentation

#### [MODIFY] [055-Subagent-History-Integration-And-Command-Timeout.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/ideas/055-Subagent-History-Integration-And-Command-Timeout.md)
*   **更新内容**: 実装計画作成済みのステータスを反映 (必要に応じて)
