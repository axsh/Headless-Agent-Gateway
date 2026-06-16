# 064-Wayfinder-Compaction-History-And-Feedback

> **Source Specification**: [053-Wayfinder-Compaction-History-And-Feedback.md](file:///prompts/phases/000-foundation/branches/feat-llm-backend/ideas/053-Wayfinder-Compaction-History-And-Feedback.md)

## Goal Description

Wayfinder エージェントの3つの主要な問題を修正する:
1. コンパクション戦略をターン数ベースの事前判定から、LLMコンテキスト長超過エラーをトリガーとするリアクティブ方式に変更し、圧縮比率を設定ファイルから制御可能にする。
2. 履歴保存ロジックを修正し、コンパクション時のファイル上書きバグを解消する。ファイル名を7桁16進数に変更し、サブセッションはハイフン連結で階層化する。
3. `ask_user` ツールを追加し、WBSオーケストレーターがユーザーフィードバックを必要とする場合にセッションを一時停止・再開できるようにする。

## User Review Required

> [!IMPORTANT]
> **コンパクション閾値の削除**: 現在の `CompactionConfig.MaxTurns` によるプロアクティブなコンパクションは完全に廃止されます。LLMからのエラーを受けたときのみコンパクションが走る設計になるため、LLM Gateway側のエラーレスポンスに `context_length_exceeded` 等の識別可能なパターンが含まれている必要があります。

> [!WARNING]
> **履歴フォーマットの互換性**: 既存の9桁10進数形式の履歴ファイル（`000000001.json`）は、新しい7桁16進数形式（`0000001.json`）とは非互換です。既存セッションの`history/`ディレクトリについてはマイグレーション対象外とし、新規セッションのみに適用します。

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: コンテキスト長超過時のリアクティブ・コンパクション | Proposed Changes > agent_core.go (runSimple) |
| R1: 超過エラーの検知ヘルパー | Proposed Changes > agent_core.go (isContextLengthExceeded) |
| R1: 圧縮比率の設定化 (config.yaml) | Proposed Changes > config.go (CompactionRatio) |
| R1: Compact関数の比率ベース分割 | Proposed Changes > compaction.go |
| R1: リトライ処理 | Proposed Changes > agent_core.go (runSimple) |
| R2: 16進数7桁ファイル名 | Proposed Changes > history.go |
| R2: サブセッションの階層化（ハイフン連結） | Proposed Changes > session_store.go, history.go |
| R2: ChatMessage への Seq フィールド追加 | Proposed Changes > session_state.go, llm_client.go |
| R2: 上書きバグ修正（Seqベース追記） | Proposed Changes > session_store.go |
| R3: ask_user ツールの定義 | Proposed Changes > tool_ask_user.go, register.go |
| R3: 実行サスペンド機能 (ErrFeedbackRequired) | Proposed Changes > agent_core.go |
| R3: StatusSuspended / StatusPendingUser | Proposed Changes > wbs_tree.go, session_state.go |
| R3: WBSオーケストレーターのサスペンド対応 | Proposed Changes > wbs_orchestrator.go |
| R3: セッションの再開（レジューム） | Proposed Changes > agent_core.go |

## Proposed Changes

### wayfinder/session パッケージ (基盤層)

---

#### [MODIFY] [session_state.go](file:///shared/libs/go/wayfinder/session/session_state.go)
*   **Description**: `Message` 構造体に `Seq` フィールドを追加。セッション・WBSノードの新ステータス定数を追加。`SessionMetadata` に `TotalSeq` フィールドを追加。
*   **Technical Design**:
    ```go
    // Message represents a conversation message with metadata for compaction.
    type Message struct {
        Role       string           `json:"role"`
        Content    string           `json:"content"`
        Timestamp  time.Time        `json:"timestamp"`
        Pinned     bool             `json:"pinned"`
        Seq        int              `json:"seq"`  // Global sequence number (immutable after assignment)
        ToolCalls  []ToolCallRecord `json:"tool_calls,omitempty"`
        ToolCallID string           `json:"tool_call_id,omitempty"`
    }
    ```
    ```go
    // Session status constants.
    const (
        StatusActive    = "active"
        StatusCompleted = "completed"
        StatusFailed    = "failed"
        StatusSuspended = "suspended" // NEW: ask_user による一時停止
    )
    ```
    ```go
    // SessionMetadata is persisted as metadata.json in the session folder.
    type SessionMetadata struct {
        // ...existing fields...
        TotalSeq         int              `json:"total_seq"`      // NEW: 全メッセージを通じた最大シーケンス番号
        // Latest は削除せず残す（後方互換性）が、新規保存時は TotalSeq を使用
    }
    ```

---

#### [MODIFY] [compaction.go](file:///shared/libs/go/wayfinder/session/compaction.go)
*   **Description**: `NeedsCompaction` を廃止。`Compact` 関数を比率ベースに変更。
*   **Technical Design**:
    ```go
    // CompactionConfig holds compaction thresholds.
    type CompactionConfig struct {
        Ratio         float64 // Compaction ratio (0.0-1.0). 0.5 = compact oldest 50%.
        MaxContentLen int     // Max content length for single message trimming.
    }

    // DefaultCompactionConfig returns the default compaction configuration.
    func DefaultCompactionConfig() *CompactionConfig {
        return &CompactionConfig{
            Ratio:         0.5,
            MaxContentLen: 5000,
        }
    }
    ```
*   **Logic**:
    *   `NeedsCompaction` 関数は削除する（呼び出し元の `applyCompaction` でも使用しなくなるため）。
    *   `Compact` 関数のロジック変更:
        1. Pinned メッセージを分離する。
        2. Unpinned メッセージの中から、`ratio` に基づいて「古い方から `ratio * len(unpinned)` 個」を圧縮対象とし、残りを保持する。
        3. `adjustBoundaryForToolPairs` と `adjustBoundaryForUserStart` は引き続き適用する。
        4. 圧縮対象メッセージを summarizer で要約し、`[COMPACTED CONTEXT SUMMARY]` システムメッセージに変換する。
        5. Pinned + Summary + Recent で再構成する。
    *   `NeedsCompaction` の代わりに、呼び出し元（`agent_core.go`）がLLMエラーを検知してから直接 `Compact` を呼ぶ。

---

#### [MODIFY] [history.go](file:///shared/libs/go/wayfinder/session/history.go)
*   **Description**: ファイル名フォーマットを7桁16進数に変更。プレフィックス連結対応。
*   **Technical Design**:
    ```go
    // AppendHistory writes new messages to history/ as individual JSON files.
    // Files are named with 7-digit zero-padded hex sequence numbers (e.g. 0000001.json).
    // prefix is the parent session's hex sequence (e.g. "000000a") for hierarchical naming.
    // If prefix is "", files are named directly as "{seq_hex}.json".
    // If prefix is "000000a", files are named as "000000a-{seq_hex}.json".
    func AppendHistory(histDir string, msgs []Message, prefix string) error {
        for _, msg := range msgs {
            seqHex := fmt.Sprintf("%07x", msg.Seq)
            var filename string
            if prefix == "" {
                filename = seqHex + ".json"
            } else {
                filename = prefix + "-" + seqHex + ".json"
            }
            entry := HistoryEntry{
                Seq:        msg.Seq,
                Role:       msg.Role,
                Content:    msg.Content,
                Timestamp:  msg.Timestamp,
                ToolCalls:  msg.ToolCalls,
                ToolCallID: msg.ToolCallID,
            }
            data, err := json.MarshalIndent(entry, "", "  ")
            if err != nil {
                return fmt.Errorf("marshal history entry %d: %w", msg.Seq, err)
            }
            targetPath := filepath.Join(histDir, filename)
            // Skip if file already exists (append-only, never overwrite).
            if _, err := os.Stat(targetPath); err == nil {
                continue
            }
            if err := atomicWrite(targetPath, data); err != nil {
                return fmt.Errorf("write history entry %d: %w", msg.Seq, err)
            }
        }
        return nil
    }
    ```
*   **Logic**: `LoadHistory` も16進数解析に対応させるが、互換性のため9桁10進数ファイルも読み込めるフォールバックを残す。

---

#### [MODIFY] [session_store.go](file:///shared/libs/go/wayfinder/session/session_store.go)
*   **Description**: `Store` にプレフィックスを追加。保存時に `Seq` ベースでフィルタリングし上書きバグを修正。
*   **Technical Design**:
    ```go
    type Store struct {
        rootDir string
        prefix  string // Hex prefix for hierarchical history naming (empty for root session)
    }

    // NewStore creates a new Store for the given root directory.
    func NewStore(rootDir string) *Store {
        return &Store{rootDir: rootDir}
    }

    // WithPrefix returns a child Store that writes history with the given prefix.
    func (s *Store) WithPrefix(prefix string) *Store {
        return &Store{rootDir: s.rootDir, prefix: prefix}
    }
    ```
*   **Logic (Save メソッドの変更)**:
    1. `metadata.json` から `prevTotalSeq` を読み取る。
    2. `state.Messages` の中で `msg.Seq > prevTotalSeq` であるもののみを `AppendHistory` に渡す。
    3. `metadata.json` の `TotalSeq` を `max(msg.Seq for all msgs)` に更新する。
    4. `context.json` は従来通り `state.Messages` 全体を書き出す（コンパクション後のコンテキスト）。

---

### wayfinder パッケージ (エージェント層)

---

#### [MODIFY] [llm_client.go](file:///shared/libs/go/wayfinder/llm_client.go)
*   **Description**: `ChatMessage` に `Seq` フィールドを追加。
*   **Technical Design**:
    ```go
    type ChatMessage struct {
        Role       string     `json:"role"`
        Content    string     `json:"content"`
        ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
        ToolCallID string     `json:"tool_call_id,omitempty"`
        Seq        int        `json:"seq,omitempty"` // Global sequence number
    }
    ```

---

#### [MODIFY] [config.go](file:///shared/libs/go/wayfinder/config.go)
*   **Description**: `AgentConfig` に `CompactionRatio` フィールドを追加。
*   **Technical Design**:
    ```go
    type AgentConfig struct {
        // ...existing fields...

        // CompactionRatio is the ratio of old messages to compact (0.0-1.0).
        // 0.5 means compact the oldest 50% of messages.
        // Used only when triggered by context length exceeded error.
        // Default: 0.5
        CompactionRatio float64
    }
    ```
*   **Logic**: `InitConfig` 内で `CompactionRatio` が 0 の場合は `0.5` をデフォルト値として設定する。

---

#### [MODIFY] [agent_core.go](file:///shared/libs/go/wayfinder/agent_core.go)
*   **Description**: リアクティブコンパクション、Seq割り当て、`ask_user` サスペンド処理を実装。
*   **Technical Design (構造体フィールド追加)**:
    ```go
    type AgentCore struct {
        // ...existing fields...
        nextSeq int // Global sequence counter for messages
    }
    ```
*   **Logic (runSimple の変更)**:
    1. **事前コンパクション削除**: `ac.applyCompaction()` の呼び出し (L156) を削除する。
    2. **Seq割り当て**: メッセージを `ac.messages` に追加するたびに `ac.nextSeq++` してその値を `ChatMessage.Seq` に設定する。
    3. **コンテキスト超過エラーのキャッチ**:
        ```go
        resp, err = ac.llm.GenerateMessage(ctx, ...)
        if err != nil {
            if isContextLengthExceeded(err) {
                ac.logger.Info("context length exceeded, applying reactive compaction",
                    "messages_before", len(ac.messages))
                if ac.applyReactiveCompaction() {
                    ac.logger.Info("reactive compaction applied, retrying",
                        "messages_after", len(ac.messages))
                    continue // retry the same iteration
                }
                // Compaction failed or not possible
            }
            // ...existing error handling...
        }
        ```
    4. **`ask_user` ツール結果の検知**:
        ```go
        result := ac.executeTool(ctx, tc)
        if errors.Is(toolErr, ErrFeedbackRequired) {
            // Save session as suspended
            ac.saveSession(session.StatusSuspended)
            return "", ErrFeedbackRequired
        }
        ```
    5. **セッション復元時の Seq 復元**: `restoreSession` で `ac.nextSeq` を復元メッセージの最大 `Seq` + 1 に設定する。

*   **新規関数**:
    ```go
    // isContextLengthExceeded checks if an LLM error indicates context length exceeded.
    func isContextLengthExceeded(err error) bool {
        msg := strings.ToLower(err.Error())
        patterns := []string{
            "context_length_exceeded",
            "max_context_length",
            "context_limit",
            "too many tokens",
            "maximum context length",
            "token limit",
            "exceeds the model's maximum context",
        }
        for _, p := range patterns {
            if strings.Contains(msg, p) {
                return true
            }
        }
        return false
    }

    // applyReactiveCompaction applies ratio-based compaction.
    // Returns true if compaction was successfully applied.
    func (ac *AgentCore) applyReactiveCompaction() bool {
        sessionMsgs := convertToSessionMessages(ac.messages)
        sessionMsgs = session.TrimLongContent(sessionMsgs, ac.compactionCfg.MaxContentLen)

        compacted, err := session.Compact(sessionMsgs, ac.compactionCfg, ac.compactionSummarizer)
        if err != nil {
            ac.logger.Warn("reactive compaction failed", "error", err.Error())
            return false
        }
        if len(compacted) >= len(sessionMsgs) {
            ac.logger.Warn("reactive compaction produced no reduction")
            return false
        }
        ac.messages = convertFromSessionMessages(compacted)
        ac.logger.Info("reactive compaction applied",
            "messages_before", len(sessionMsgs),
            "messages_after", len(compacted))
        return true
    }
    ```

*   **`applyCompaction` (旧)**: メソッドを削除する。
*   **`convertToSessionMessages` の変更**: `ChatMessage.Seq` を `session.Message.Seq` にコピーする。
*   **`convertFromSessionMessages` の変更**: `session.Message.Seq` を `ChatMessage.Seq` にコピーする。

---

#### [NEW] [tool_ask_user.go](file:///shared/libs/go/wayfinder/tools/tool_ask_user.go)
*   **Description**: `ask_user` ツールの実装。
*   **Technical Design**:
    ```go
    package tools

    import (
        "context"
        "errors"
        "fmt"
    )

    // ErrFeedbackRequired is returned by ask_user to signal that user feedback is needed.
    // The agent loop should catch this error and suspend execution.
    var ErrFeedbackRequired = errors.New("user feedback required")

    func newAskUser(tc *ToolContext) ToolHandler {
        return func(ctx context.Context, input map[string]any) (string, error) {
            prompt, _ := input["prompt"].(string)
            if prompt == "" {
                return "", fmt.Errorf("ask_user: 'prompt' is required")
            }
            // Return the prompt as the result content (for context persistence),
            // and signal ErrFeedbackRequired to suspend execution.
            return fmt.Sprintf("[WAITING FOR USER] %s", prompt), ErrFeedbackRequired
        }
    }
    ```

---

#### [MODIFY] [register.go](file:///shared/libs/go/wayfinder/tools/register.go)
*   **Description**: `ask_user` ツールを登録に追加。
*   **Technical Design**:
    ```go
    // RegisterAllTools の末尾に追加:
    reg.Register("ask_user", "Ask the user a question and wait for their response. Use this when you need user feedback, confirmation, or input before proceeding.",
        map[string]any{
            "type": "object",
            "properties": map[string]any{
                "prompt": map[string]any{"type": "string", "description": "The question or instruction to present to the user"},
            },
            "required": []string{"prompt"},
        }, newAskUser(tc))
    ```

---

### wayfinder/planning パッケージ (WBSオーケストレーション層)

---

#### [MODIFY] [wbs_tree.go](file:///shared/libs/go/wayfinder/planning/wbs_tree.go)
*   **Description**: `StatusSuspended` ノードステータスを追加。`IsComplete`, `HasFailed`, `IsDeadlocked` にサスペンド状態を考慮。
*   **Technical Design**:
    ```go
    const (
        StatusPending   = "pending"
        StatusRunning   = "running"
        StatusCompleted = "completed"
        StatusFailed    = "failed"
        StatusSuspended = "suspended" // NEW: awaiting user feedback
    )
    ```
*   **Logic**:
    *   `IsComplete()`: `StatusSuspended` は完了ではないためfalseを返す（変更不要、既存の `!= StatusCompleted` で正しく動作する）。
    *   `IsDeadlocked()`: `StatusSuspended` のノードが存在する場合はデッドロックとみなさない。`HasSuspended()` メソッドを新設する。
    ```go
    // HasSuspended returns true if any node has "suspended" status.
    func (t *WBSTree) HasSuspended() bool {
        suspended := false
        t.walkNodes(func(node *WBSNode) {
            if node.Status == StatusSuspended {
                suspended = true
            }
        })
        return suspended
    }
    ```

---

#### [MODIFY] [wbs_orchestrator.go](file:///shared/libs/go/wayfinder/planning/wbs_orchestrator.go)
*   **Description**: `Execute` ループに `ErrFeedbackRequired` / `StatusSuspended` のハンドリングを追加。
*   **Technical Design**:
    ```go
    // Execute の node 実行後:
    result, err := o.executor.ExecuteNode(ctx, node)
    if err != nil {
        // Check if this is a user feedback suspension.
        if errors.Is(err, tools.ErrFeedbackRequired) {
            tree.UpdateNodeStatus(node.ID, StatusSuspended, result)
            o.persist(tree)
            o.logger.Info("WBS node suspended awaiting user feedback", "node_id", node.ID)
            o.emit("node_suspended", fmt.Sprintf("%s: %s", node.ID, node.Name))
            return ErrWBSSuspended
        }
        // ...existing failure handling...
    }
    ```
    ```go
    // ErrWBSSuspended indicates that WBS execution was suspended awaiting user input.
    var ErrWBSSuspended = errors.New("WBS execution suspended: awaiting user feedback")
    ```
*   **Logic**: `Execute` ループの先頭の終了条件チェックに `HasSuspended()` を追加し、サスペンド中のノードがある場合は `ErrWBSSuspended` を返す。

---

## Step-by-Step Implementation Guide

### Step 1: session パッケージ -- データ構造とコンパクション (TDD)

1. **テストファースト**: `session/compaction_test.go` に比率ベースのコンパクションテストを追加する。
    - `TestCompact_RatioBased_Half`: 20メッセージに対し ratio=0.5 で古い10メッセージが要約に置き換わることを検証。
    - `TestCompact_RatioBased_ThreeQuarters`: ratio=0.75 で古い75%が対象になることを検証。
    - 既存の `TestNeedsCompaction_*` テストは削除する。
2. `session/session_state.go` を編集: `Message.Seq` フィールド追加、`StatusSuspended` 定数追加、`SessionMetadata.TotalSeq` 追加。
3. `session/compaction.go` を編集: `CompactionConfig` を `Ratio` ベースに変更、`NeedsCompaction` を削除、`Compact` を比率ベースに変更。
4. テスト実行: `./scripts/process/build.sh` でビルドと単体テストが通ることを確認。
5. **git commit**: `feat: change compaction to ratio-based strategy`

### Step 2: session パッケージ -- 履歴の16進数化とSeqベース保存 (TDD)

1. **テストファースト**: `session/history_test.go` に新しいテストを追加する。
    - `TestAppendHistory_HexFilenames`: Seq=1,2,3 のメッセージが `0000001.json`, `0000002.json`, `0000003.json` として保存されることを検証。
    - `TestAppendHistory_WithPrefix`: prefix="000000a" で `000000a-0000001.json` 形式になることを検証。
    - `TestAppendHistory_SkipExisting`: 既存ファイルが存在する場合にスキップされることを検証。
2. `session/history.go` を編集: `AppendHistory` のシグネチャ変更（`startSeq int` -> `prefix string`）、16進数ファイル名生成、既存ファイルスキップ。
3. `session/session_store.go` を編集: `Store.prefix` フィールド追加、`WithPrefix` メソッド追加、`Save` メソッドの `TotalSeq` ベースフィルタリング。
4. テスト実行: `./scripts/process/build.sh` でビルドと単体テストが通ることを確認。
5. **git commit**: `feat: hex-sequential history with prefix support`

### Step 3: wayfinder パッケージ -- リアクティブコンパクションと Seq 管理

1. `wayfinder/llm_client.go`: `ChatMessage.Seq` フィールド追加。
2. `wayfinder/config.go`: `AgentConfig.CompactionRatio` 追加、`InitConfig` でのデフォルト値設定。
3. `wayfinder/agent_core.go`:
    - `AgentCore.nextSeq` フィールド追加。
    - `applyCompaction()` を削除し、`applyReactiveCompaction()` を新設。
    - `isContextLengthExceeded()` を新設。
    - `runSimple` ループでの事前コンパクション呼び出しを削除し、LLMエラー時のリアクティブコンパクション + リトライを実装。
    - メッセージ追加時に `Seq` を割り当てるロジック追加。
    - `convertToSessionMessages` / `convertFromSessionMessages` で `Seq` をコピー。
    - `restoreSession` で `nextSeq` を復元。
4. テスト実行: `./scripts/process/build.sh`
5. **git commit**: `feat: reactive compaction on context length exceeded`

### Step 4: wayfinder/tools パッケージ -- ask_user ツール (TDD)

1. **テストファースト**: `tools/tools_test.go` にテストケースを追加。
    - `TestAskUser_ReturnsErrFeedbackRequired`: 正常呼び出しで `ErrFeedbackRequired` エラーが返ることを検証。
    - `TestAskUser_MissingPrompt`: `prompt` なしでエラーが返ることを検証。
2. `tools/tool_ask_user.go` を新規作成。
3. `tools/register.go` に `ask_user` の登録を追加。
4. テスト実行: `./scripts/process/build.sh`
5. **git commit**: `feat: add ask_user tool for user feedback`

### Step 5: wayfinder/planning パッケージ -- WBS サスペンド対応 (TDD)

1. **テストファースト**: `planning/wbs_tree_test.go` と `planning/wbs_orchestrator_test.go` にテストを追加。
    - `TestWBSTree_HasSuspended`: `StatusSuspended` ノードが正しく検出されることを検証。
    - `TestWBSOrchestrator_SuspendOnFeedback`: `ErrFeedbackRequired` を返す executor でオーケストレーターが `ErrWBSSuspended` を返し、ノードが `StatusSuspended` になることを検証。
2. `planning/wbs_tree.go`: `StatusSuspended` 定数追加、`HasSuspended()` メソッド追加。
3. `planning/wbs_orchestrator.go`: `ErrWBSSuspended` 変数追加、`Execute` ループ内に `ErrFeedbackRequired` ハンドリング追加。
4. テスト実行: `./scripts/process/build.sh`
5. **git commit**: `feat: WBS orchestrator suspend on ask_user`

### Step 6: wayfinder/agent_core.go -- ask_user サスペンド/レジューム統合

1. `agent_core.go` の `runSimple` ループ内で `executeTool` が `ErrFeedbackRequired` を返した場合のハンドリングを追加。
2. `runWithWBSTree` で `ErrWBSSuspended` が返された場合にセッションを `StatusSuspended` で保存する処理を追加。
3. `Run` メソッドで `StatusSuspended` セッションの復元時に、中断されたノードの再開処理を実装。
4. テスト実行: `./scripts/process/build.sh`
5. **git commit**: `feat: agent core suspend/resume for ask_user`

### Step 7: E2E テスト

1. `tests/wayfinder_e2e_test.go` に新しい E2E テストを追加。
2. テスト実行: `./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestE2E_Wayfinder"`
3. **git commit**: `test: add E2E tests for reactive compaction and ask_user`

### Step 8: 設定ファイルとドキュメント更新

1. `settings/demo/config.yaml` に `compaction_ratio: 0.5` の設定例を追加。
2. テスト実行: `./scripts/process/build.sh`
3. **git commit**: `docs: add compaction_ratio config example`

### Step 9: ビルドと検証

1. `./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestE2E_Wayfinder"` を実行。
2. 総合判定プロセスを実施。
3. **git push**

---

## Verification Plan

### Automated Verification

1. **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```

2. **Integration Tests** (Wayfinder 関連のみ選択実行):
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestE2E_Wayfinder"
    ```
    *   **Log Verification**:
        - `"reactive compaction applied"` の Info ログが、コンテキスト超過時にのみ出力されることを確認。
        - `"WBS node suspended"` のログが `ask_user` 呼び出し時に出力されることを確認。
        - 履歴ファイルが `0000001.json` 形式で保存されていることを確認。

3. **E2E Tests (新規追加)**:

    #### [MODIFY] [wayfinder_e2e_test.go](file:///tests/wayfinder_e2e_test.go)

    *   **テストケース 1**: `TestE2E_Wayfinder_ReactiveCompaction`
        - **検証内容**: コンテキスト超過エラーが発生した際にコンパクションが実行され、リトライが成功すること。
        - **検証ポイント**: サーバーログに `"reactive compaction applied"` が出力され、セッションが `completed` で終了すること。
        - **注記**: LLM Gateway のモックまたは極小コンテキスト制限モデルを使用して、コンテキスト超過を再現する。現実のLLMで超過を再現するのが困難な場合は、`BifrostClient` のモック差し替えによる単体テストで代替し、E2Eテストではリグレッション確認（既存テストの通過）に留める。その場合、本テストケースは省略可能とし、`agent_core_test.go` に `TestAgentCore_ReactiveCompaction_OnContextExceeded` として詳細な単体テストを実装する。

    *   **テストケース 2**: `TestE2E_Wayfinder_HexHistory`
        - **検証内容**: セッション実行後、`history/` ディレクトリに7桁16進数形式のファイルが生成されること。
        - **検証ポイント**: ファイル名のパターンマッチ (`^[0-9a-f]{7}\.json$`)、ファイル内の `seq` フィールドが単調増加であること。

    *   **テストケース 3**: `TestE2E_Wayfinder_AskUser_Suspend`
        - **検証内容**: `ask_user` ツールが呼び出された場合にセッションが `suspended` 状態で保存されること。
        - **検証ポイント**: セッション取得APIで `status: "suspended"` が返ること。
        - **注記**: このテストでは `ask_user` を呼び出すようLLMに促すプロンプトを使用する。LLMが `ask_user` を呼ぶかどうかは非決定的であるため、テスト内でモック LLM を使用して確実に `ask_user` を含むツール呼び出しを返すようにする。

### テスト項目のセルフレビュー

1. **網羅性の検証**: R1(コンパクション比率、エラー検知、リトライ)、R2(16進数、プレフィックス、上書き防止)、R3(ask_user、サスペンド、ステータス)の全要件がカバーされている。
2. **証拠の十分性**: 各テストは「エラーが出ない」だけでなく、具体的な出力値（ファイル名パターン、メッセージ数の変化、ステータス文字列）を検証している。
3. **迂回・抜け道の排除**: コンパクションテストではメッセージ数の before/after を検証し、「コンパクションが実行されなかった」場合もテスト結果でわかる。
4. **依存関係の整合性**: Step 1-2（session層）-> Step 3-6（wayfinder層）-> Step 7（E2E）のボトムアップ順序で設計されている。

---

## Documentation

#### [MODIFY] [053-Wayfinder-Compaction-History-And-Feedback.md](file:///prompts/phases/000-foundation/branches/feat-llm-backend/ideas/053-Wayfinder-Compaction-History-And-Feedback.md)
*   **更新内容**: 実装後に仕様の最終状態を反映（必要に応じて）。

#### [MODIFY] [README.md](file:///README.md)
*   **更新内容**: Wayfinder セクションに `ask_user` ツールと `compaction_ratio` 設定の説明を追加（必要に応じて）。
