# 001-Wayfinder-Session-Persistence

> **Source Specification**: [001-Wayfinder-Session-Management-and-Serialization.md](file://prompts/phases/000-foundation/branches/feat-wayfinder-agent/ideas/001-Wayfinder-Session-Management-and-Serialization.md)

## Goal Description

Wayfinder Agentのセッション管理機能を実装する。Part 1で構築したインメモリのAgentCoreに対して、ファイルベースの状態永続化を追加し、シングルショット実行間でのコンテキスト維持を実現する。

具体的には:
1. **SessionState構造体**: 会話履歴、FileCreationTracker（削除許可リスト）、CommandExecutionContext（プロセストラッカー）を含む状態構造体の定義
2. **ファイルベース永続化**: SessionDir配下にSessionID.jsonとしてアトミックに読み書き
3. **トラッカー整合性検証 (ValidateTrackerState)**: セッション復旧時にファイル存在確認・PID/コマンド名照合を行い、不整合エントリを除外
4. **コンテキストコンパクション**: ピン留め＆スライディングウィンドウ、要約コンパクション、出力トリミング
5. **AgentCoreへの統合**: Runループにセッション読み込み・即時保存・コンパクション判定を組み込み

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| ファイルベースの状態保存 | Proposed Changes > session_store.go |
| レジューム機能 | Proposed Changes > session_store.go > `Load` + agent_core.go統合 |
| 親子セッションでの設定伝播 | **Part 3で実装** (本計画ではSessionState.ParentIDフィールドのみ) |
| シリアライズ対象データの網羅 (Messages, Tracker, Processes) | Proposed Changes > session_state.go |
| 削除許可リストの永続化 (003から) | Proposed Changes > session_state.go > `CreatedFiles` |
| バックグラウンドプロセス情報の永続化 (003から) | Proposed Changes > session_state.go > `RunningProcesses` |
| セッション復旧時のトラッカー整合性検証 (003から) | Proposed Changes > tracker_validator.go > `ValidateTrackerState` |
| アトミックな書き込み | Proposed Changes > session_store.go > `atomicWrite` |
| 自動クリーンアップ | Proposed Changes > session_store.go > `Cleanup` |
| ピン留め＆スライディングウィンドウ | Proposed Changes > compaction.go |
| 要約コンパクション | Proposed Changes > compaction.go > `SummarizeOldMessages` |
| 出力・ファイルトリミング | Proposed Changes > compaction.go > `TrimLongContent` |

## Proposed Changes

### wayfinder/session パッケージ

#### [NEW] [session_state.go](file://shared/libs/go/wayfinder/session/session_state.go)
*   **Description**: セッション状態全体を表現する構造体群
*   **Technical Design**:
    ```go
    package session

    import "time"

    // Message represents a conversation message.
    type Message struct {
        Role      string    `json:"role"`      // "user", "assistant", "tool", "system"
        Content   string    `json:"content"`
        Timestamp time.Time `json:"timestamp"`
        Pinned    bool      `json:"pinned"`    // Compaction exclusion flag
        ToolCalls []ToolCallRecord `json:"tool_calls,omitempty"`
        ToolCallID string   `json:"tool_call_id,omitempty"`
    }

    // ToolCallRecord records a tool call within a message.
    type ToolCallRecord struct {
        ID    string         `json:"id"`
        Name  string         `json:"name"`
        Input map[string]any `json:"input"`
    }

    // TrackedFile represents a file created by the agent (deletion permission list entry).
    type TrackedFile struct {
        Path      string    `json:"path"`
        CreatedAt time.Time `json:"created_at"`
        IsDir     bool      `json:"is_dir"`
    }

    // TrackedProcess represents a background process launched by the agent.
    type TrackedProcess struct {
        PID       int       `json:"pid"`
        Command   string    `json:"command"`
        Args      []string  `json:"args"`
        StartedAt time.Time `json:"started_at"`
    }

    // SessionState is the full serializable session state.
    type SessionState struct {
        SessionID        string           `json:"session_id"`
        ParentID         *string          `json:"parent_id,omitempty"`
        Status           string           `json:"status"` // "active", "completed", "failed"
        Messages         []Message        `json:"messages"`
        CreatedFiles     []TrackedFile    `json:"created_files"`
        RunningProcesses []TrackedProcess `json:"running_processes"`
        CreatedAt        time.Time        `json:"created_at"`
        LastActivityAt   time.Time        `json:"last_activity_at"`
    }
    ```

#### [NEW] [session_store.go](file://shared/libs/go/wayfinder/session/session_store.go)
*   **Description**: セッション状態のファイルベース読み書き
*   **Technical Design**:
    ```go
    package session

    import (
        "encoding/json"
        "fmt"
        "os"
        "path/filepath"
        "time"
    )

    // Store manages session state persistence.
    type Store struct {
        sessionDir string
    }

    func NewStore(sessionDir string) *Store {
        return &Store{sessionDir: sessionDir}
    }

    // Load reads a session state from [sessionDir]/[sessionID].json.
    // Returns nil, nil if the file does not exist (new session).
    func (s *Store) Load(sessionID string) (*SessionState, error) {
        path := s.filePath(sessionID)
        data, err := os.ReadFile(path)
        if os.IsNotExist(err) {
            return nil, nil
        }
        if err != nil {
            return nil, fmt.Errorf("failed to read session file: %w", err)
        }
        var state SessionState
        if err := json.Unmarshal(data, &state); err != nil {
            return nil, fmt.Errorf("failed to parse session file: %w", err)
        }
        return &state, nil
    }

    // Save writes session state atomically.
    // Writes to a temp file first, then renames to avoid corruption.
    func (s *Store) Save(state *SessionState) error {
        if err := os.MkdirAll(s.sessionDir, 0755); err != nil {
            return fmt.Errorf("failed to create session dir: %w", err)
        }
        state.LastActivityAt = time.Now()
        data, err := json.MarshalIndent(state, "", "  ")
        if err != nil {
            return fmt.Errorf("failed to marshal session: %w", err)
        }
        return atomicWrite(s.filePath(state.SessionID), data)
    }

    // Cleanup removes session files older than the threshold.
    func (s *Store) Cleanup(threshold time.Duration) (int, error) {
        entries, err := os.ReadDir(s.sessionDir)
        if err != nil {
            return 0, err
        }
        removed := 0
        cutoff := time.Now().Add(-threshold)
        for _, e := range entries {
            if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
                continue
            }
            info, err := e.Info()
            if err != nil { continue }
            if info.ModTime().Before(cutoff) {
                os.Remove(filepath.Join(s.sessionDir, e.Name()))
                removed++
            }
        }
        return removed, nil
    }

    func (s *Store) filePath(sessionID string) string {
        return filepath.Join(s.sessionDir, sessionID+".json")
    }

    // atomicWrite writes data to a temp file then renames to target.
    func atomicWrite(targetPath string, data []byte) error {
        tmpPath := targetPath + ".tmp"
        if err := os.WriteFile(tmpPath, data, 0644); err != nil {
            return fmt.Errorf("failed to write temp file: %w", err)
        }
        if err := os.Rename(tmpPath, targetPath); err != nil {
            os.Remove(tmpPath) // cleanup
            return fmt.Errorf("failed to rename temp file: %w", err)
        }
        return nil
    }
    ```

#### [NEW] [tracker_validator.go](file://shared/libs/go/wayfinder/session/tracker_validator.go)
*   **Description**: セッション復旧時のトラッカー整合性検証
*   **Technical Design**:
    ```go
    package session

    import (
        "os"
        "runtime"
    )

    // ValidateTrackerState verifies integrity of deserialized tracker data.
    // Removes entries that no longer match actual system state.
    func ValidateTrackerState(state *SessionState) {
        // 1. Validate files/directories exist
        validFiles := make([]TrackedFile, 0, len(state.CreatedFiles))
        for _, f := range state.CreatedFiles {
            if _, err := os.Stat(f.Path); err == nil {
                validFiles = append(validFiles, f)
            }
            // Non-existent entries are excluded from deletion permission list
        }
        state.CreatedFiles = validFiles

        // 2. Validate process existence and name match
        validProcs := make([]TrackedProcess, 0, len(state.RunningProcesses))
        for _, p := range state.RunningProcesses {
            if verifyProcessAlive(p.PID, p.Command) {
                validProcs = append(validProcs, p)
            }
            // PID reused by different process -> excluded
        }
        state.RunningProcesses = validProcs
    }

    // verifyProcessAlive checks if a process with the given PID exists
    // and its command name matches the recorded name.
    func verifyProcessAlive(pid int, expectedCommand string) bool {
        proc, err := os.FindProcess(pid)
        if err != nil {
            return false
        }
        // On Unix, FindProcess always succeeds; need to send signal 0 to check
        if runtime.GOOS != "windows" {
            if err := proc.Signal(syscall.Signal(0)); err != nil {
                return false
            }
        }
        actualName := getProcessName(pid) // OS-specific: /proc/[pid]/comm on Linux
        if actualName == "" {
            return false
        }
        return actualName == expectedCommand
    }
    ```
*   **Logic**:
    *   `getProcessName` はOS固有の実装: Linux では `/proc/[pid]/comm` を読む。Windows では `tasklist` またはWin32 APIを使用。
    *   マッチしないエントリは削除許可リストから除外され、誤った許可判定を防止する。

#### [NEW] [compaction.go](file://shared/libs/go/wayfinder/session/compaction.go)
*   **Description**: コンテキストコンパクション機能
*   **Technical Design**:
    ```go
    package session

    // CompactionConfig holds compaction thresholds.
    type CompactionConfig struct {
        MaxTurns       int // Max conversation turns before compaction (default: 15)
        MaxContentLen  int // Max content length for single message trimming (default: 5000)
    }

    func DefaultCompactionConfig() *CompactionConfig {
        return &CompactionConfig{
            MaxTurns:      15,
            MaxContentLen: 5000,
        }
    }

    // NeedsCompaction checks if the message history exceeds the threshold.
    func NeedsCompaction(messages []Message, cfg *CompactionConfig) bool {
        turnCount := 0
        for _, m := range messages {
            if m.Role == "user" || m.Role == "assistant" {
                turnCount++
            }
        }
        return turnCount > cfg.MaxTurns
    }

    // Compact applies compaction to the message history:
    // 1. Pinned messages are always preserved
    // 2. Old unpinned messages are replaced with a summary placeholder
    // 3. Recent messages (within window) are preserved
    func Compact(messages []Message, cfg *CompactionConfig, summarizer func([]Message) (string, error)) ([]Message, error) {
        pinned := make([]Message, 0)
        unpinned := make([]Message, 0)

        for _, m := range messages {
            if m.Pinned {
                pinned = append(pinned, m)
            } else {
                unpinned = append(unpinned, m)
            }
        }

        // Keep last N messages as the sliding window
        windowSize := cfg.MaxTurns / 2
        if windowSize < 4 { windowSize = 4 }

        if len(unpinned) <= windowSize {
            return messages, nil // No compaction needed
        }

        oldMessages := unpinned[:len(unpinned)-windowSize]
        recentMessages := unpinned[len(unpinned)-windowSize:]

        // Summarize old messages
        summary, err := summarizer(oldMessages)
        if err != nil {
            return messages, fmt.Errorf("compaction summarization failed: %w", err)
        }

        summaryMsg := Message{
            Role:    "system",
            Content: "[COMPACTED CONTEXT SUMMARY]\n" + summary,
            Pinned:  true,
        }

        result := make([]Message, 0, len(pinned)+1+len(recentMessages))
        result = append(result, pinned...)
        result = append(result, summaryMsg)
        result = append(result, recentMessages...)
        return result, nil
    }

    // TrimLongContent truncates message content exceeding maxLen.
    func TrimLongContent(messages []Message, maxLen int) []Message {
        for i := range messages {
            if len(messages[i].Content) > maxLen {
                messages[i].Content = messages[i].Content[:maxLen] + "\n... [TRUNCATED]"
            }
        }
        return messages
    }
    ```

---

### wayfinder コアパッケージへの統合変更

#### [MODIFY] [agent_core.go](file://shared/libs/go/wayfinder/agent_core.go)
*   **Description**: セッション永続化とコンパクションをRunループに統合
*   **Logic**:
    *   `Run` の冒頭で `Store.Load(SessionID)` を呼び、既存セッションがあれば `SessionState.Messages` を初期メッセージとして使用。
    *   `ValidateTrackerState` を呼び出してトラッカー整合性を検証。
    *   FileTrackerを `SessionState.CreatedFiles` から復元。
    *   各ToolCall実行後およびLLM応答追加後に `Store.Save` で即時永続化。
    *   LLMリクエスト前に `NeedsCompaction` を判定し、必要ならば `Compact` を実行。

---

### テストファイル (TDD: テストを先に記述)

#### [NEW] [session_state_test.go](file://shared/libs/go/wayfinder/session/session_state_test.go)
*   **テストケース**:
    *   `TestSessionState_Serialization`: Messages, CreatedFiles, RunningProcesses を含む完全な SessionState をJSON化・復元し、全フィールドが劣化なく一致
    *   `TestSessionState_ParentID_Nil`: ParentIDがnilの場合のJSON出力で `parent_id` が省略されること
    *   `TestSessionState_ParentID_Set`: ParentIDが設定されている場合のJSON出力・復元

#### [NEW] [session_store_test.go](file://shared/libs/go/wayfinder/session/session_store_test.go)
*   **テストケース**:
    *   `TestStore_SaveAndLoad`: 保存後にロードし全フィールド一致
    *   `TestStore_Load_NotExists`: 存在しないSessionID -> nil, nil を返す
    *   `TestStore_AtomicWrite_Corruption`: 書き込み中のファイル破損がないことを検証（tmpファイルのリネーム確認）
    *   `TestStore_Cleanup`: 古いセッションファイルが削除され、新しいものは残ること
    *   `TestStore_SaveCreatesDir`: SessionDirが存在しない場合に自動作成されること

#### [NEW] [tracker_validator_test.go](file://shared/libs/go/wayfinder/session/tracker_validator_test.go)
*   **テストケース**:
    *   `TestValidateTrackerState_FileExists`: 存在するファイル -> 保持
    *   `TestValidateTrackerState_FileRemoved`: 存在しないファイル -> 除外
    *   `TestValidateTrackerState_ProcessAlive`: 存在するプロセス -> 保持
    *   `TestValidateTrackerState_ProcessDead`: 終了済みプロセス -> 除外
    *   `TestValidateTrackerState_ProcessReassigned`: PIDが別プロセスに再利用 -> 除外
    *   `TestValidateTrackerState_EmptyState`: 空リスト -> そのまま

#### [NEW] [compaction_test.go](file://shared/libs/go/wayfinder/session/compaction_test.go)
*   **テストケース**:
    *   `TestNeedsCompaction_BelowThreshold`: 閾値未満 -> false
    *   `TestNeedsCompaction_AboveThreshold`: 閾値超過 -> true
    *   `TestCompact_PinnedPreserved`: Pinned=trueのメッセージが必ず残ること
    *   `TestCompact_OldMessagesReplaced`: 古いメッセージが要約に置換されること
    *   `TestCompact_RecentWindowPreserved`: 直近のメッセージが保持されること
    *   `TestTrimLongContent`: 長い内容が切り詰められること

## Step-by-Step Implementation Guide

1.  **SessionState構造体の定義** (TDD: テスト先行):
    *   `session_state_test.go` を作成し、シリアライズ/デシリアライズテストを記述 -> 失敗確認
    *   `session_state.go` に `Message`, `TrackedFile`, `TrackedProcess`, `SessionState` を定義
    *   `git commit -m "feat(wayfinder): add SessionState struct with tracker types"`

2.  **Store (永続化) の実装** (TDD: テスト先行):
    *   `session_store_test.go` を作成し、全テストケースを記述 -> 失敗確認
    *   `session_store.go` に `Store`, `Load`, `Save`, `atomicWrite`, `Cleanup` を実装
    *   `git commit -m "feat(wayfinder): add file-based session Store with atomic write"`

3.  **トラッカー整合性検証の実装** (TDD: テスト先行):
    *   `tracker_validator_test.go` を作成し、全テストケースを記述 -> 失敗確認
    *   `tracker_validator.go` に `ValidateTrackerState`, `verifyProcessAlive` を実装
    *   OS固有の `getProcessName` を実装 (build tags使用: `_unix.go`, `_windows.go`)
    *   `git commit -m "feat(wayfinder): add tracker state validation on session restore"`

4.  **コンパクション機能の実装** (TDD: テスト先行):
    *   `compaction_test.go` を作成し、全テストケースを記述 -> 失敗確認
    *   `compaction.go` に `NeedsCompaction`, `Compact`, `TrimLongContent` を実装
    *   `git commit -m "feat(wayfinder): add context compaction with pinning and summarization"`

5.  **AgentCoreへの統合**:
    *   `agent_core.go` の `Run` にセッション読み込み・保存・コンパクションを組み込み
    *   Part 1のFileTrackerをSessionState.CreatedFilesと同期するアダプタを実装
    *   `git commit -m "feat(wayfinder): integrate session persistence into AgentCore run loop"`

6.  **ビルド・テスト実行**:
    *   Verification Planに従い全テスト実行

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```

2.  **Integration Tests**:
    ```bash
    ./scripts/process/integration_test.sh --categories taskengine --specify "TestWayfinderSession"
    ```
    *   **Log Verification**: セッションファイルが `SessionDir` 配下に正しく生成・更新されていること。

3.  **E2E Tests (新規)**:

    #### [NEW] [wayfinder_session_test.go](file://tests/wayfinder_session_test.go)
    *   **テストケース**: `TestWayfinderE2E_SessionPersistence`
        *   AgentCoreでセッション1を実行 -> ファイル生成 -> セッション終了 -> 同じSessionIDで再実行 -> 前回の会話履歴が復元されていること
    *   **テストケース**: `TestWayfinderE2E_TrackerValidation`
        *   セッション状態に存在しないファイルパスを含むJSONを作成 -> ロード -> ValidateTrackerStateにより不整合エントリが除外されていること
    *   **テストケース**: `TestWayfinderE2E_DeletionPermissionAcrossSessions`
        *   セッション1で `write_file` -> セッション終了 -> セッション2で同SessionID復旧 -> `rm` が削除許可リスト経由で許可されること
    *   **検証ポイント**: シングルショット間でのセッション状態維持、トラッカー整合性検証、削除許可リストの永続化

    E2Eテストでは既存のE2Eヘルパーは使用せず（HAGサーバー不要）、AgentCoreを直接呼び出す形式とする。理由: Wayfinder AgentはHAGとは独立したモジュールであり、agentservice_e2e_test.goのヘルパーは適用外。

### テスト項目のセルフレビュー (testing-rules 11.4)

1. **網羅性**: シリアライズ/デシリアライズ正常系、アトミック書き込み障害系、トラッカー検証(ファイル/プロセス)正常系・異常系、コンパクション閾値・ピン留め保持・要約置換をすべてカバー。
2. **証拠の十分性**: 各テストはJSONの具体的フィールド値を比較し、ファイルの存在・不在を `os.Stat` で確認。
3. **迂回排除**: summarizer はモック関数を注入し、LLM実呼び出しに依存しない。
4. **依存関係**: session_state -> session_store -> tracker_validator -> compaction -> agent_core統合 の順にボトムアップ。

### 総合判定プロセス (testing-rules 12)

全テスト完了後、testing-rules 12.2のチェック項目を確認し、総合判定を記録する。

## Documentation

本計画は新規パッケージの作成のため、既存ドキュメントへの影響はない。

---

## 継続計画について

本計画はWayfinder Agent実装の **Part 2/4** です。

- **Part 1** ([000-Wayfinder-AgentCore-Tools-LLMGP.md](file://prompts/phases/000-foundation/branches/feat-wayfinder-agent/plans/000-Wayfinder-AgentCore-Tools-LLMGP.md)): エージェントコア、ツール、ガードレール、LLMGP統合
- **Part 3** ([002-Wayfinder-Subagent-Summarization.md](file://prompts/phases/000-foundation/branches/feat-wayfinder-agent/plans/002-Wayfinder-Subagent-Summarization.md)): サブエージェント連携、要約
- **Part 4** ([003-Wayfinder-WBS-Planning-Orchestration.md](file://prompts/phases/000-foundation/branches/feat-wayfinder-agent/plans/003-Wayfinder-WBS-Planning-Orchestration.md)): WBS計画生成、オーケストレーション、実行分岐
