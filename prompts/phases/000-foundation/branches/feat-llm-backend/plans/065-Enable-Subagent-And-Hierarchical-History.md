# 065-Enable-Subagent-And-Hierarchical-History

> **Source Specification**: [054-Enable-Subagent-And-Hierarchical-History.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/ideas/054-Enable-Subagent-And-Hierarchical-History.md)

## Goal Description

サブエージェントモードを `config.yaml` から有効化できるようにし、WBS ノード実行時に子セッションが作成されるようにする。併せて、サブセッションの履歴格納をフラットなプレフィクスベースからディレクトリ階層ベースに変更する。

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1-1: config.yaml に enable_subagent 設定追加 | Step 1: config/config.go |
| R1-2: config.AgentServiceConfig に EnableSubagent 追加 | Step 1: config/config.go |
| R1-3: tern/server.go で EnableSubagent 伝播 | Step 2: tern/server.go |
| R1-4: codingagent.AdapterConfig に EnableSubagent 追加 | Step 1: codingagent/adapter_config.go |
| R1-5: adapter.go で AdapterConfig から AgentConfig に転写 | Step 2: wayfinder/adapter.go |
| R1-6: agentNodeExecutor が使われることを保証 | Step 2 で有効化、Step 5 E2E で検証 |
| R1-7: 子セッションディレクトリ作成 | 既存の RunChild が対応済み |
| R2: 履歴のディレクトリ階層化 | Step 3: history.go, session_store.go |
| R3: WithPrefix → WithSubDir 改名 | Step 3: session_store.go |
| R4: AppendHistory の prefix パラメータ削除 | Step 3: history.go |
| 4a: AgentRunnerConfig に HistorySubDir 追加 | Step 4: subagent_executor.go |
| 4b: agentNodeExecutor で HistorySubDir 設定 | Step 4: agent_core.go |
| 4c: RunChild で WithSubDir Store 注入 | Step 4: agent_runner.go |

## Proposed Changes

### config パッケージ

#### [MODIFY] [config.go](file://shared/libs/go/config/config.go)
*   **Description**: `AgentServiceConfig` に `EnableSubagent` フィールド追加
*   **Technical Design**:
    ```go
    type AgentServiceConfig struct {
        Port           int  `yaml:"port"`
        DisableSandbox bool `yaml:"disable_sandbox"`
        EnableSubagent bool `yaml:"enable_subagent"` // 追加
    }
    ```

---

### codingagent パッケージ

#### [MODIFY] [adapter_config.go](file://shared/libs/go/codingagent/adapter_config.go)
*   **Description**: `AdapterConfig` に `EnableSubagent` フィールド追加
*   **Technical Design**:
    ```go
    type AdapterConfig struct {
        // ... 既存フィールド ...
        EnableSubagent bool // 追加
    }
    ```

---

### tern パッケージ

#### [MODIFY] [server.go](file://shared/libs/go/tern/server.go)
*   **Description**: `resolveAgentService` の引数に `enableSubagent bool` を追加し、`AdapterConfig` に伝播
*   **Technical Design**:
    *   関数シグネチャ変更:
        ```go
        func resolveAgentService(o *options, log logger.Logger, tl *tasklog.TaskLog,
            gatewayURL string, gatewayToken string, caCertPath string,
            gw llmgateway.LLMGatewayBackend,
            disableSandbox bool, enableSubagent bool) *agentservice.Server
        ```
    *   L392-399 の `adapterCfg` に追加:
        ```go
        adapterCfg := &codingagent.AdapterConfig{
            GatewayURL:       gatewayURL,
            GatewayToken:     gatewayToken,
            Logger:           log,
            DefaultModel:     defaultModel,
            ToolCallFallback: toolCallFallback,
            DisableSandbox:   disableSandbox,
            EnableSubagent:   enableSubagent, // 追加
        }
        ```
    *   L157 の呼び出し元更新:
        ```go
        as := resolveAgentService(o, log, tl, gatewayURL, gatewayToken, caCertPath, gw,
            cfg.AgentService.DisableSandbox, cfg.AgentService.EnableSubagent)
        ```

---

### wayfinder パッケージ

#### [MODIFY] [adapter.go](file://shared/libs/go/wayfinder/adapter.go)
*   **Description**: `CreateSession` で `AdapterConfig.EnableSubagent` を `AgentConfig` に転写
*   **Technical Design**:
    *   L59-63 の変更:
        ```go
        agentCfg := &AgentConfig{
            WorkDir:        cfg.WorkDir,
            SessionDir:     cfg.SessionDir,
            LogicalModel:   cfg.Model,
            EnableSubagent: a.adapterCfg.EnableSubagent, // 追加
        }
        ```

---

### session パッケージ

#### [MODIFY] [history_test.go](file://shared/libs/go/wayfinder/session/history_test.go) (テストファースト)
*   **Description**: `AppendHistory` シグネチャ変更に伴うテスト更新と、サブディレクトリテスト追加
*   **Technical Design**:
    *   `TestAppendHistory_HexFilenames`: `AppendHistory(histDir, msgs, "")` → `AppendHistory(histDir, msgs)` に変更
    *   `TestAppendHistory_SkipExisting`: 同上
    *   `TestAppendHistory_HexFilenamePattern`: 同上
    *   `TestAppendHistory_WithPrefix` を `TestAppendHistory_SubDir` に改名:
        ```go
        func TestAppendHistory_SubDir(t *testing.T) {
            histDir := t.TempDir()
            subDir := filepath.Join(histDir, "000000a")
            os.MkdirAll(subDir, 0755)
            msgs := []Message{
                {Role: "user", Content: "sub-hello", Seq: 1, Timestamp: time.Now()},
                {Role: "assistant", Content: "sub-hi", Seq: 2, Timestamp: time.Now()},
            }
            err := AppendHistory(subDir, msgs)
            if err != nil {
                t.Fatalf("AppendHistory in subdir failed: %v", err)
            }
            // ファイルが subDir 内に作成されること
            for _, f := range []string{"0000001.json", "0000002.json"} {
                path := filepath.Join(subDir, f)
                if _, err := os.Stat(path); os.IsNotExist(err) {
                    t.Errorf("expected %s in subdir", f)
                }
            }
        }
        ```
    *   `TestStore_WithPrefix` を `TestStore_WithSubDir` に改名:
        ```go
        func TestStore_WithSubDir(t *testing.T) {
            rootDir := t.TempDir()
            store := NewStore(rootDir)
            childStore := store.WithSubDir("000000a")
            if childStore.subDir != "000000a" {
                t.Errorf("expected subDir=000000a, got %s", childStore.subDir)
            }
        }
        ```
    *   新テスト `TestStore_SaveWithSubDir` 追加:
        ```go
        func TestStore_SaveWithSubDir(t *testing.T) {
            rootDir := t.TempDir()
            store := NewStore(rootDir).WithSubDir("000000a")
            state := &SessionState{
                SessionID: "test-session",
                Status:    StatusActive,
                Messages: []Message{
                    {Role: "user", Content: "child msg", Seq: 1, Timestamp: time.Now()},
                },
                CreatedAt: time.Now(),
            }
            if err := store.Save(state); err != nil {
                t.Fatalf("save with subDir failed: %v", err)
            }
            // history/000000a/0000001.json が存在すること
            histPath := filepath.Join(rootDir, "test-session", "history", "000000a", "0000001.json")
            if _, err := os.Stat(histPath); os.IsNotExist(err) {
                t.Error("expected history file in subdirectory")
            }
        }
        ```

#### [MODIFY] [history.go](file://shared/libs/go/wayfinder/session/history.go)
*   **Description**: `AppendHistory` から `prefix` パラメータを削除
*   **Technical Design**:
    ```go
    // 変更前
    func AppendHistory(histDir string, msgs []Message, prefix string) error {
        for _, msg := range msgs {
            seqHex := fmt.Sprintf("%07x", msg.Seq)
            var filename string
            if prefix == "" {
                filename = seqHex + ".json"
            } else {
                filename = prefix + "-" + seqHex + ".json"
            }
            // ...
        }
    }

    // 変更後
    func AppendHistory(histDir string, msgs []Message) error {
        for _, msg := range msgs {
            filename := fmt.Sprintf("%07x.json", msg.Seq)
            // ... (残りのロジックは同一)
        }
    }
    ```

#### [MODIFY] [session_store.go](file://shared/libs/go/wayfinder/session/session_store.go)
*   **Description**: `WithPrefix` → `WithSubDir` 改名、`prefix` → `subDir` フィールド変更、`Save` でサブディレクトリ結合
*   **Technical Design**:
    *   フィールド変更:
        ```go
        type Store struct {
            rootDir string
            subDir  string // 旧 prefix
        }
        ```
    *   メソッド改名:
        ```go
        func (s *Store) WithSubDir(subDir string) *Store {
            return &Store{rootDir: s.rootDir, subDir: subDir}
        }
        ```
    *   `Save` メソッド L75-101 の変更:
        ```go
        // histDir を subDir 付きで構築
        histDir := filepath.Join(dir, "history", s.subDir)
        if err := os.MkdirAll(histDir, 0755); err != nil {
            return fmt.Errorf("failed to create history dir: %w", err)
        }
        // ...
        if len(newMsgs) > 0 {
            if err := AppendHistory(histDir, newMsgs); err != nil {
                return fmt.Errorf("failed to append history: %w", err)
            }
        }
        ```
    *   `migrateToFolder` L200: `AppendHistory(histDir, state.Messages, "")` → `AppendHistory(histDir, state.Messages)`

---

### subagent パッケージ

#### [MODIFY] [subagent_executor.go](file://shared/libs/go/wayfinder/subagent/subagent_executor.go)
*   **Description**: `AgentRunnerConfig` に `HistorySubDir` フィールド追加
*   **Technical Design**:
    ```go
    type AgentRunnerConfig struct {
        WorkDir             string
        SessionDir          string
        LogicalModel        string
        AllowedPathPatterns []string
        Emitter             any
        HistorySubDir       string // 追加: 子セッション履歴のサブディレクトリパス
    }
    ```

---

### wayfinder パッケージ (子セッション連携)

#### [MODIFY] [agent_core.go](file://shared/libs/go/wayfinder/agent_core.go)
*   **Description**: `agentNodeExecutor` に `parentCore` フィールド追加、`ExecuteNode` で `HistorySubDir` を設定
*   **Technical Design**:
    *   `agentNodeExecutor` 構造体に `parentCore` 追加:
        ```go
        type agentNodeExecutor struct {
            parentSessionID string
            parentCore      *AgentCore   // 追加: 親 Seq を取得するため
            childConfig     *subagent.AgentRunnerConfig
            runner          subagent.AgentRunner
            llm             subagent.LLMClient
            summarizer      subagent.SummaryStrategy
            logger          logger.Logger
        }
        ```
    *   `runWithWBSTree` L672 の初期化で `parentCore` 設定:
        ```go
        nodeExec = &agentNodeExecutor{
            parentSessionID: ac.sessionID,
            parentCore:      ac,       // 追加
            childConfig:     childCfg,
            runner:          ac.runner,
            llm:             ac.subagentLLM,
            summarizer:      subagent.NewOutcomeSummarizer(ac.subagentLLM),
            logger:          ac.logger,
        }
        ```
    *   `ExecuteNode` メソッドで `HistorySubDir` を設定:
        ```go
        func (e *agentNodeExecutor) ExecuteNode(ctx context.Context, node planning.WBSNode) (string, error) {
            prompt := fmt.Sprintf("[WBS Step %s: %s]\n%s", node.ID, node.Name, node.Description)
            childSessionID := fmt.Sprintf("%s-wbs-%s", e.parentSessionID, node.ID)

            // 親の現在 nextSeq を 7桁 hex に変換してサブディレクトリパスとする
            childCfg := *e.childConfig // コピー
            childCfg.HistorySubDir = fmt.Sprintf("%07x", e.parentCore.nextSeq)

            e.logger.Debug("executing WBS node in child session",
                "node_id", node.ID, "child_session", childSessionID,
                "history_subdir", childCfg.HistorySubDir)

            childResult, err := e.runner.RunChild(ctx, &childCfg, childSessionID, e.llm, e.logger, prompt)
            if err != nil {
                return "", err
            }

            // Summarize child result for parent.
            hints := &subagent.Hints{Objective: node.Name, Context: node.Description}
            summary, err := e.summarizer.Summarize(ctx, hints, childResult)
            if err != nil {
                e.logger.Warn("WBS node summarization failed, using raw result", "error", err.Error())
                return childResult, nil
            }
            return summary, nil
        }
        ```
    *   `AgentCore` に `SetStore` メソッド追加:
        ```go
        // SetStore sets the session store for this AgentCore.
        func (ac *AgentCore) SetStore(store *session.Store) {
            ac.store = store
        }
        ```

#### [MODIFY] [agent_runner.go](file://shared/libs/go/wayfinder/agent_runner.go)
*   **Description**: `RunChild` で `HistorySubDir` が設定されている場合に `Store.WithSubDir` を子 AgentCore に注入
*   **Technical Design**:
    ```go
    func (r *AgentRunnerImpl) RunChild(
        ctx context.Context,
        cfg *subagent.AgentRunnerConfig,
        sessionID string,
        llm subagent.LLMClient,
        log logger.Logger,
        prompt string,
    ) (string, error) {
        childCfg := &AgentConfig{
            WorkDir:             cfg.WorkDir,
            SessionDir:          cfg.SessionDir,
            LogicalModel:        cfg.LogicalModel,
            AllowedPathPatterns: cfg.AllowedPathPatterns,
            EnableSubagent:      false,
        }
        if err := InitConfig(childCfg); err != nil {
            return "", err
        }

        wrappedLLM := &subagentToWayfinderLLM{inner: llm}
        child := NewAgentCore(wrappedLLM, childCfg, log)
        child.SetSessionID(sessionID)

        // サブディレクトリ付き Store を子に注入 (追加)
        if cfg.HistorySubDir != "" {
            parentStore := session.NewStore(childCfg.SessionDir)
            child.SetStore(parentStore.WithSubDir(cfg.HistorySubDir))
        }

        if cfg.Emitter != nil {
            if emitter, ok := cfg.Emitter.(*EventEmitter); ok {
                child.SetEmitter(emitter)
            }
        }

        return child.Run(ctx, prompt)
    }
    ```

---

### 設定ファイル

#### [MODIFY] [config.yaml](file://settings/demo/config.yaml)
*   **Description**: `agent_service` セクションに `enable_subagent: true` を追加
*   **Technical Design**:
    ```yaml
    agent_service:
      port: 3100
      enable_subagent: true
    ```

## Step-by-Step Implementation Guide

### Step 1: 設定パス追加 -- config, codingagent (TDD)
- [ ] `config/config.go`: `AgentServiceConfig` に `EnableSubagent bool` フィールド追加
- [ ] `codingagent/adapter_config.go`: `AdapterConfig` に `EnableSubagent bool` フィールド追加
- [ ] ビルド確認: `./scripts/process/build.sh`
- [ ] git commit

### Step 2: 設定伝播 -- tern/server.go, wayfinder/adapter.go
- [ ] `tern/server.go`: `resolveAgentService` の引数に `enableSubagent` 追加、`adapterCfg` に反映、呼び出し元更新
- [ ] `wayfinder/adapter.go`: `agentCfg.EnableSubagent = a.adapterCfg.EnableSubagent` 追加
- [ ] `settings/demo/config.yaml`: `enable_subagent: true` 追加
- [ ] ビルド確認: `./scripts/process/build.sh`
- [ ] git commit

### Step 3: 履歴ディレクトリ階層化 -- session パッケージ (TDD)
- [ ] テストファースト: `history_test.go` のテスト更新 (prefix 引数削除、SubDir テスト追加)
- [ ] `history.go`: `AppendHistory` から `prefix` パラメータ削除
- [ ] `session_store.go`: `prefix` → `subDir` フィールド変更、`WithPrefix` → `WithSubDir` 改名、`Save` でサブディレクトリ結合
- [ ] ビルド確認: `./scripts/process/build.sh`
- [ ] git commit

### Step 4: 子セッション Store 連携 -- subagent, agent_core, agent_runner
- [ ] `subagent/subagent_executor.go`: `AgentRunnerConfig.HistorySubDir` フィールド追加
- [ ] `agent_core.go`: `agentNodeExecutor.parentCore` 追加、`ExecuteNode` で `HistorySubDir` 設定、`SetStore` メソッド追加
- [ ] `agent_runner.go`: `RunChild` で `cfg.HistorySubDir` が設定時に `Store.WithSubDir` 注入
- [ ] ビルド確認: `./scripts/process/build.sh`
- [ ] git commit

### Step 5: ビルドと検証
- [ ] 全体ビルド: `./scripts/process/build.sh`
- [ ] git push

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```
    *   全単体テスト PASS を確認
    *   特に `session` パッケージの `TestAppendHistory_SubDir`, `TestStore_WithSubDir`, `TestStore_SaveWithSubDir` が PASS すること

2.  **E2E Tests**:

    E2E テストは今回追加しない。理由:
    *   変更は内部の設定パス追加と履歴格納形式の変更であり、外部 API のインターフェースに変更はない
    *   `tests/wayfinder_e2e_test.go` は既にサブエージェント有効化後も動作する既存テストがある
    *   ディレクトリ構造の検証は単体テスト (`TestStore_SaveWithSubDir`) で十分にカバーされる

### セルフレビュー結果

1.  **要件対比チェック**: 全 R1-R4 要件と 4a-4c 実装詳細が Traceability テーブルでマッピング済み
2.  **再現性チェック**: 各 Step で変更対象ファイル、変更内容、コードスニペットが具体的に記述
3.  **データ構造チェック**: `AgentServiceConfig`, `AdapterConfig`, `AgentRunnerConfig`, `Store`, `agentNodeExecutor` の構造体変更が全て記載
4.  **テスト網羅性チェック**: history_test.go で prefix 削除テスト、SubDir テスト、Store の SubDir Save テストを TDD で計画
5.  **統合テスト実行プランチェック**: 本変更は内部リファクタリング+設定追加のため、build.sh の PASS で十分。統合テストの追加実行は不要
6.  **テスト項目設計**: ボトムアップ順序 (config → tern → session → subagent → agent_core → agent_runner)
7.  **総合判定**: 全 Step 完了後に `build.sh` PASS + git push で完了判定
8.  **E2Eテストコード化チェック**: 外部インターフェース変更なしのため E2E 追加不要、理由を明記済み
