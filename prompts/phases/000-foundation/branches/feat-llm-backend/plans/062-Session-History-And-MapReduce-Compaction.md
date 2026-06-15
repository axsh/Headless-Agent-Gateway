# 062-Session-History-And-MapReduce-Compaction

> **Source Specification**: [051-Session-History-And-MapReduce-Compaction.md](file:///prompts/phases/000-foundation/branches/feat-llm-backend/ideas/051-Session-History-And-MapReduce-Compaction.md)

## Goal Description

Wayfinder のセッション管理を単一 JSON ファイル構成からフォルダベースの階層構造 (`metadata.json` + `context.json` + `history/`) に移行し、会話履歴の永続保存と Map&Reduce 方式の compaction アルゴリズムを実装する。これにより、大量メッセージ時の compaction 連鎖失敗を解消し、セッションの安定性を大幅に向上させる。

## User Review Required

> [!IMPORTANT]
> **旧フォーマットからのマイグレーション動作**: 既存の `.wayfinder/{session-id}.json` 形式のセッションファイルは自動的に新フォーマットに変換されます。マイグレーション後、旧ファイルは `.bak` 拡張子にリネームされます。

> [!WARNING]
> **History のディスク使用量**: 全ての会話ターンを個別ファイルとして保存するため、長時間のセッションではファイル数が増加します。現時点では自動クリーンアップは実装しません (将来対応)。

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: セッションフォルダ構成の階層化 | Proposed Changes > session パッケージ > session_store.go |
| R2: 会話履歴の永続記録 (History) | Proposed Changes > session パッケージ > history.go (NEW) |
| R3: Map&Reduce Compaction アルゴリズム | Proposed Changes > session パッケージ > map_reduce_summarizer.go (NEW) |
| R4: metadata.json によるコンテキスト管理 | Proposed Changes > session パッケージ > session_state.go |
| R5: 既存 API の後方互換性 | Proposed Changes > session パッケージ > session_store.go |
| R6: 履歴からの再要約 (Nice to Have) | 今回は実装しない。history/ データの永続化により将来対応の基盤は整う |
| R7: 履歴の検索・参照 (Nice to Have) | 今回は実装しない。`LoadHistory` API の提供で基盤は整う |
| R8: WBS ノードごとのメッセージスコープ分離 (Nice to Have) | 今回は実装しない。別仕様として検討 |

## Proposed Changes

### session パッケージ

---

#### [NEW] [map_reduce_summarizer_test.go](file:///shared/libs/go/wayfinder/session/map_reduce_summarizer_test.go)
*   **Description**: Map&Reduce Compaction のテスト
*   **Technical Design**:
    ```go
    // Mock LLM client for summarization tests.
    type mockSummarizerLLM struct {
        responses []string
        callCount int
        failAt    int // -1 = never fail
    }

    func (m *mockSummarizerLLM) Summarize(msgs []Message) (string, error) {
        // ...
    }
    ```
*   **テストケース**:
    - `TestSplitIntoChunks_BasicSplit` -- 20 メッセージ / maxChunkMsgs=10 で 2 チャンクに分割される
    - `TestSplitIntoChunks_ToolPairBoundary` -- チャンク境界が tool メッセージで分割されない (adjustBoundaryForToolPairs 適用)
    - `TestSplitIntoChunks_SingleChunk` -- メッセージ数が maxChunkMsgs 未満の場合、1 チャンクのまま
    - `TestSplitIntoChunks_MaxFourChunks` -- 100 メッセージでも最大 4 チャンクに制限される
    - `TestMapReduceSummarizer_Summarize_AllSuccess` -- 全チャンク要約成功 + reduce 成功
    - `TestMapReduceSummarizer_Summarize_PartialFallback` -- 一部チャンクの要約失敗 → フォールバック
    - `TestMapReduceSummarizer_Summarize_AllFallback` -- 全チャンク失敗 → 全フォールバック + reduce なし
    - `TestMapReduceSummarizer_Summarize_ReduceFallback` -- reduce 段階での LLM 失敗 → 単純連結
    - `TestReduceSummaries_PairwiseMerge` -- [A, B, C, D] -> [merge(A,B), merge(C,D)] -> [merge(AB,CD)]
    - `TestReduceSummaries_OddCount` -- [A, B, C] -> [merge(A,B), C] -> [merge(AB,C)]
    - `TestReduceSummaries_SingleInput` -- 1 要素は reduce なし

#### [NEW] [map_reduce_summarizer.go](file:///shared/libs/go/wayfinder/session/map_reduce_summarizer.go)
*   **Description**: Map&Reduce 方式の compaction アルゴリズムを実装する新規ファイル
*   **Technical Design**:
    ```go
    // SummarizeFunc is the interface for LLM-based summarization.
    // Using a function type to avoid cyclic imports with wayfinder package.
    type SummarizeFunc func(msgs []Message) (string, error)

    // MergeFunc merges two summaries into one.
    type MergeFunc func(summaryA, summaryB string) (string, error)

    // MapReduceSummarizer implements chunked summarization.
    type MapReduceSummarizer struct {
        summarize       SummarizeFunc
        merge           MergeFunc
        fallbackSummary func([]Message) string
        maxChunkMsgs    int // Max messages per chunk (default: 20)
    }

    // NewMapReduceSummarizer creates a new MapReduceSummarizer.
    func NewMapReduceSummarizer(
        summarize SummarizeFunc,
        merge MergeFunc,
        fallback func([]Message) string,
        maxChunkMsgs int,
    ) *MapReduceSummarizer

    // Summarize performs Map&Reduce summarization.
    func (s *MapReduceSummarizer) Summarize(msgs []Message) (string, error)

    // splitIntoChunks divides messages into 1-4 chunks respecting tool pair boundaries.
    func (s *MapReduceSummarizer) splitIntoChunks(msgs []Message) [][]Message

    // reduceSummaries performs pairwise reduction of chunk summaries.
    func (s *MapReduceSummarizer) reduceSummaries(summaries []string) (string, error)
    ```
*   **Logic**:
    - `splitIntoChunks`: メッセージ数を `maxChunkMsgs` で割ってチャンク数を算出 (1~4)。均等分割後、各境界を `adjustBoundaryForToolPairs` で調整する
    - `Summarize`: (1) splitIntoChunks → (2) 各チャンクを `summarize` で要約 (失敗時は `fallbackSummary`) → (3) `reduceSummaries` で統合
    - `reduceSummaries`: ペアワイズ reduce。`[A,B,C,D]` → `[merge(A,B), merge(C,D)]` → `[merge(AB,CD)]`。merge 失敗時は `summaryA + "\n---\n" + summaryB` で連結

---

#### [NEW] [history_test.go](file:///shared/libs/go/wayfinder/session/history_test.go)
*   **Description**: 会話履歴の永続記録テスト
*   **テストケース**:
    - `TestAppendHistory_WritesFiles` -- メッセージを追記すると history/ に連番ファイルが作成される
    - `TestAppendHistory_SequentialNumbering` -- startSeq から正しい連番でファイル名が付けられる
    - `TestAppendHistory_PreservesContent` -- 書き込んだ内容が正確に保持される (role, content, tool_calls, tool_call_id, timestamp)
    - `TestLoadHistory_RangeRead` -- fromSeq~toSeq 範囲の読み込みが正しく動作する
    - `TestLoadHistory_MissingFiles` -- 欠番があっても panic せず利用可能なものだけ返す
    - `TestLoadHistory_EmptyRange` -- 空範囲の場合は nil/空スライスを返す

#### [NEW] [history.go](file:///shared/libs/go/wayfinder/session/history.go)
*   **Description**: 会話履歴の永続記録 (append-only)
*   **Technical Design**:
    ```go
    // HistoryEntry represents a single conversation turn persisted in history/.
    type HistoryEntry struct {
        Seq        int              `json:"seq"`
        Role       string           `json:"role"`
        Content    string           `json:"content"`
        Timestamp  time.Time        `json:"timestamp"`
        ToolCalls  []ToolCallRecord `json:"tool_calls,omitempty"`
        ToolCallID string           `json:"tool_call_id,omitempty"`
    }

    // AppendHistory writes new messages to history/ as individual JSON files.
    // Files are named with 9-digit zero-padded sequence numbers.
    // Existing files are never modified (append-only).
    func AppendHistory(histDir string, msgs []Message, startSeq int) error

    // LoadHistory reads messages from history/ within [fromSeq, toSeq] range.
    func LoadHistory(histDir string, fromSeq, toSeq int) ([]Message, error)

    // entryToMessage converts a HistoryEntry to a session.Message.
    func entryToMessage(entry HistoryEntry) Message
    ```
*   **Logic**:
    - `AppendHistory`: `startSeq + i + 1` の連番でファイル名を生成 (`%09d.json`)。`atomicWrite` で安全に書き込む
    - `LoadHistory`: 指定範囲の連番ファイルを `os.ReadFile` で読み込み、`entryToMessage` で変換。ファイルが存在しない場合は `continue` (欠番許容)

---

#### [MODIFY] [session_state.go](file:///shared/libs/go/wayfinder/session/session_state.go)
*   **Description**: `SessionMetadata` 構造体を追加し、metadata.json に対応するデータモデルを定義する
*   **Technical Design**:
    ```go
    // SessionMetadata is persisted as metadata.json in the session folder.
    type SessionMetadata struct {
        SessionID        string           `json:"session_id"`
        Status           string           `json:"status"`
        Latest           int              `json:"latest"`             // Last history sequence number
        ContextStart     int              `json:"context_start"`      // First seq in current context (before = summarized)
        CreatedAt        time.Time        `json:"created_at"`
        UpdatedAt        time.Time        `json:"updated_at"`
        WBSTreeJSON      json.RawMessage  `json:"wbs_tree,omitempty"`
        CreatedFiles     []TrackedFile    `json:"created_files"`
        RunningProcesses []TrackedProcess `json:"running_processes"`
    }
    ```
*   **Logic**: 既存の `SessionState` はそのまま維持し、`Store` が内部で `SessionMetadata` と `context.json` のメッセージ列に分解する

---

#### [MODIFY] [session_store_test.go](file:///shared/libs/go/wayfinder/session/session_store_test.go)
*   **Description**: フォルダベース Store のテスト追加
*   **テストケース** (既存テストは維持し、以下を追加):
    - `TestStore_SaveAndLoad_FolderStructure` -- Save でフォルダ構造 (metadata.json + context.json + history/) が作成され、Load で復元できる
    - `TestStore_SaveAndLoad_RoundTrip` -- Save → Load のラウンドトリップでデータが一致
    - `TestStore_MigrateLegacy` -- 旧フォーマット `{id}.json` ファイルが存在する場合、Load で自動マイグレーションされる
    - `TestStore_MigrateLegacy_DataIntegrity` -- マイグレーション後のデータが元データと一致する
    - `TestStore_MultipleSaves_HistoryAppend` -- 複数回 Save しても history にメッセージが正しく追記される (重複なし)
    - `TestStore_Cleanup_FolderMode` -- Cleanup がフォルダベースのセッションにも対応する

#### [MODIFY] [session_store.go](file:///shared/libs/go/wayfinder/session/session_store.go)
*   **Description**: 単一ファイル保存からフォルダベース保存に移行
*   **Technical Design**:
    ```go
    // Store manages session state persistence on the filesystem.
    type Store struct {
        rootDir string // .wayfinder/ root directory
    }

    // NewStore creates a new Store for the given root directory.
    // Note: parameter semantics changed from "sessionDir" to "rootDir".
    func NewStore(rootDir string) *Store

    // sessionDir returns the directory path for a session.
    func (s *Store) sessionDir(sessionID string) string {
        return filepath.Join(s.rootDir, sessionID)
    }

    // Save writes session state to the folder structure:
    //   {rootDir}/{sessionID}/metadata.json   -- session metadata
    //   {rootDir}/{sessionID}/context.json    -- current LLM context messages
    //   {rootDir}/{sessionID}/history/NNN.json -- individual message history
    func (s *Store) Save(state *SessionState) error

    // Load reads session state from folder structure.
    // Falls back to legacy single-file format with auto-migration.
    func (s *Store) Load(sessionID string) (*SessionState, error)

    // loadFromFolder loads from the new folder structure.
    func (s *Store) loadFromFolder(sessionID string) (*SessionState, error)

    // loadLegacy loads from the old single-file format.
    func (s *Store) loadLegacy(path string) (*SessionState, error)

    // migrateToFolder converts a legacy SessionState to folder format.
    func (s *Store) migrateToFolder(state *SessionState) error

    // Cleanup removes session folders older than the threshold.
    func (s *Store) Cleanup(threshold time.Duration) (int, error)

    // LoadHistory reads messages from history/ within [fromSeq, toSeq] range.
    func (s *Store) LoadHistory(sessionID string, fromSeq, toSeq int) ([]Message, error)
    ```
*   **Logic**:
    - `Save`:
        1. `os.MkdirAll` でセッションフォルダと history/ を作成
        2. `SessionState` から `SessionMetadata` を構築し、`metadata.json` に `atomicWrite`
        3. `state.Messages` を `context.json` に `atomicWrite`
        4. 新しいメッセージを `AppendHistory` で history/ に追記。既知の `latest` (metadata から取得) と `len(state.Messages)` の差分から新規メッセージを特定する
    - `Load`:
        1. `{rootDir}/{sessionID}/metadata.json` の存在を確認 → 存在すれば `loadFromFolder`
        2. 存在しなければ `{rootDir}/{sessionID}.json` (旧フォーマット) を試行 → `migrateToFolder` で変換
        3. どちらもなければ `nil, nil` (新規セッション)
    - `loadFromFolder`: metadata.json + context.json を読み込み、`SessionState` を再構成
    - `migrateToFolder`: 旧 `SessionState` の各メッセージを history/ に書き出し、metadata.json と context.json を作成。旧ファイルを `.bak` にリネーム

---

### wayfinder パッケージ

---

#### [MODIFY] [agent_core_test.go](file:///shared/libs/go/wayfinder/agent_core_test.go)
*   **Description**: `compactionSummarizer` の Map&Reduce 統合テスト
*   **テストケース**:
    - `TestAgentCore_CompactionWithMapReduce` -- 大量メッセージを蓄積し、compaction 時に MapReduceSummarizer が使用されることを検証

#### [MODIFY] [agent_core.go](file:///shared/libs/go/wayfinder/agent_core.go)
*   **Description**: `compactionSummarizer` を MapReduceSummarizer に差し替え
*   **Technical Design**:
    ```go
    // compactionSummarizer creates a summary using Map&Reduce approach.
    func (ac *AgentCore) compactionSummarizer(msgs []session.Message) (string, error) {
        summarizer := session.NewMapReduceSummarizer(
            ac.llmSummarizeChunk,    // Map: chunk -> summary
            ac.llmMergeSummaries,    // Reduce: two summaries -> one
            ac.structuredFallbackSummary, // Fallback
            20, // maxChunkMsgs
        )
        return summarizer.Summarize(msgs)
    }

    // llmSummarizeChunk summarizes a single chunk of messages using LLM.
    func (ac *AgentCore) llmSummarizeChunk(msgs []session.Message) (string, error) {
        conversationLog := ac.buildConversationLog(msgs)
        summaryPrompt := []ChatMessage{
            {Role: "system", Content: summarizationSystemPrompt},
            {Role: "user", Content: "Summarize this conversation:\n\n" + conversationLog},
        }
        resp, err := ac.llm.GenerateMessage(
            context.Background(), ac.config.LogicalModel, summaryPrompt, nil,
        )
        if err != nil {
            return "", err
        }
        return resp.Content, nil
    }

    // llmMergeSummaries merges two summaries into one using LLM.
    func (ac *AgentCore) llmMergeSummaries(summaryA, summaryB string) (string, error) {
        mergePrompt := []ChatMessage{
            {Role: "system", Content: mergeSystemPrompt},
            {Role: "user", Content: "Summary A:\n" + summaryA + "\n\nSummary B:\n" + summaryB},
        }
        resp, err := ac.llm.GenerateMessage(
            context.Background(), ac.config.LogicalModel, mergePrompt, nil,
        )
        if err != nil {
            return "", err
        }
        return resp.Content, nil
    }
    ```
*   **Logic**:
    - 既存の `compactionSummarizer` メソッドのボディを MapReduceSummarizer に委譲する
    - 新たに `mergeSystemPrompt` 定数を追加 (2つの要約を1つにまとめるためのプロンプト)
    - `buildConversationLog` と `structuredFallbackSummary` は既存のまま活用

    ```go
    const mergeSystemPrompt = `You are a conversation summarizer.
    Merge the following two conversation summaries into a single, cohesive summary.
    Rules:
    - Preserve all tool call names and their outcomes from both summaries.
    - Maintain chronological order (Summary A happened before Summary B).
    - Preserve specific file paths, command outputs, and operation results.
    - Keep causal relationships between user requests and assistant actions.
    - Be concise but do not lose important facts.
    - Output in the same language as the summaries.`
    ```

---

#### [MODIFY] [agent_core.go](file:///shared/libs/go/wayfinder/agent_core.go) -- runWithWBSTree エラーパス修正
*   **Description**: `runWithWBSTree` のエラーパスでセッションステータスを正しく更新する
*   **Technical Design**:
    ```go
    func (ac *AgentCore) runWithWBSTree(ctx context.Context, tree *planning.WBSTree) (string, error) {
        // ... (既存のコード)

        orch := planning.NewWBSOrchestrator(nodeExec, persister, ac.logger, orchOpts...)
        if err := orch.Execute(ctx, tree); err != nil {
            ac.saveSession(session.StatusFailed)  // NEW: エラー時にステータス更新
            return "", fmt.Errorf("WBS orchestration failed: %w", err)
        }

        ac.saveSession(session.StatusCompleted)  // NEW: 正常完了時にステータス更新
        return planning.CollectResults(tree), nil
    }
    ```

## Step-by-Step Implementation Guide

### Step 1: HistoryEntry 構造体と history.go の作成 (R2)

1. `shared/libs/go/wayfinder/session/history_test.go` を作成し、以下のテストを実装:
   - `TestAppendHistory_WritesFiles`
   - `TestAppendHistory_SequentialNumbering`
   - `TestAppendHistory_PreservesContent`
   - `TestLoadHistory_RangeRead`
   - `TestLoadHistory_MissingFiles`
   - `TestLoadHistory_EmptyRange`
2. テストが失敗することを確認 (TDD: Red)
3. `shared/libs/go/wayfinder/session/history.go` を作成し、`HistoryEntry`, `AppendHistory`, `LoadHistory`, `entryToMessage` を実装
4. テストが成功することを確認 (TDD: Green)
5. `./scripts/process/build.sh` を実行して全体ビルドが通ることを確認

### Step 2: SessionMetadata 構造体の追加 (R4)

1. `shared/libs/go/wayfinder/session/session_state.go` に `SessionMetadata` 構造体を追加
2. `./scripts/process/build.sh` を実行してコンパイルが通ることを確認

### Step 3: MapReduceSummarizer の実装 (R3)

1. `shared/libs/go/wayfinder/session/map_reduce_summarizer_test.go` を作成し、以下のテストを実装:
   - `TestSplitIntoChunks_*` (4 テスト)
   - `TestMapReduceSummarizer_Summarize_*` (4 テスト)
   - `TestReduceSummaries_*` (3 テスト)
2. テストが失敗することを確認 (TDD: Red)
3. `shared/libs/go/wayfinder/session/map_reduce_summarizer.go` を作成し、`MapReduceSummarizer` を実装
4. テストが成功することを確認 (TDD: Green)
5. `./scripts/process/build.sh` を実行

### Step 4: Store のフォルダベース移行 (R1, R5)

1. `shared/libs/go/wayfinder/session/session_store_test.go` に新規テストを追加:
   - `TestStore_SaveAndLoad_FolderStructure`
   - `TestStore_SaveAndLoad_RoundTrip`
   - `TestStore_MigrateLegacy`
   - `TestStore_MigrateLegacy_DataIntegrity`
   - `TestStore_MultipleSaves_HistoryAppend`
   - `TestStore_Cleanup_FolderMode`
2. テストが失敗することを確認 (TDD: Red)
3. `shared/libs/go/wayfinder/session/session_store.go` を改修:
   - `Store` の `sessionDir` フィールドを `rootDir` にリネーム
   - `Save` メソッドをフォルダベースに変更
   - `Load` メソッドにフォルダ優先 + レガシーフォールバック + 自動マイグレーションを実装
   - `Cleanup` メソッドをフォルダ対応に更新
   - `LoadHistory` メソッドを追加
4. テストが成功することを確認 (TDD: Green)
5. `./scripts/process/build.sh` を実行

### Step 5: AgentCore の compactionSummarizer 差し替え

1. `shared/libs/go/wayfinder/agent_core_test.go` に `TestAgentCore_CompactionWithMapReduce` を追加
2. `shared/libs/go/wayfinder/agent_core.go` を改修:
   - `compactionSummarizer` メソッドを MapReduceSummarizer に委譲するよう変更
   - `llmSummarizeChunk` メソッドを追加 (既存の LLM 要約ロジックを1チャンク分に限定)
   - `llmMergeSummaries` メソッドを追加
   - `mergeSystemPrompt` 定数を追加
3. `./scripts/process/build.sh` を実行

### Step 6: runWithWBSTree のエラーパス修正

1. `shared/libs/go/wayfinder/agent_core.go` の `runWithWBSTree` メソッドに `saveSession(StatusFailed)` / `saveSession(StatusCompleted)` を追加
2. `./scripts/process/build.sh` を実行

### Step 7: 全体ビルドとテスト実行

1. `./scripts/process/build.sh` を実行して全体ビルド + 単体テストを確認
2. `./scripts/process/integration_test.sh --specify "TestE2E_Wayfinder"` を実行して wayfinder E2E テストのリグレッションを確認

## Verification Plan

### Automated Verification

1. **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```
    - 新規ファイル (`history.go`, `map_reduce_summarizer.go`) のテストが全て成功すること
    - 既存の `session_store_test.go`, `compaction_test.go` のテストがリグレッションなく成功すること

2. **Integration Tests**:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestE2E_Wayfinder"
    ```
    - Wayfinder E2E テスト (Health, GuardrailBlock, FullScenario, CompactionToolPairProtection) が全て成功すること
    - **Log Verification**: compaction 発動時に `"applying compaction"` ログが出力され、`"LLM call failed"` が出ていないことを確認

3. **E2E Tests (既存テストでの検証)**:
    本変更は session パッケージの内部リファクタリングが主であり、外部 API に変更はない。既存の E2E テスト (`TestE2E_Wayfinder_FullScenario_*`, `TestE2E_Wayfinder_CompactionToolPairProtection`) で新しい Store の動作を間接的に検証する。新規 E2E テストの追加は不要。理由: `Store` の `Save/Load` インターフェースは後方互換であり、外部から観測可能な動作に変更がないため。

### テスト項目設計のセルフレビュー (testing-rules.md 11.4)

1. **網羅性の検証**: history の読み書き、MapReduce の Map/Reduce 各段階、フォールバック各パターン、Store の新旧フォーマット互換をカバー。全テスト成功で「セッションの永続化と compaction が正しく動作している」と言える。
2. **証拠の十分性**: 各テストは値の一致検証 (ラウンドトリップ)、ファイル存在確認、チャンク数検証など具体的な assertion を含む。
3. **迂回・抜け道の排除**: Mock LLM を使用するが、フォールバックパスも明示的にテストしている。
4. **依存関係の整合性**: history.go → session_store.go → map_reduce_summarizer.go → agent_core.go の順でボトムアップにテストしている。

### 総合判定プロセス (testing-rules.md 12)

全テスト完了後、以下を確認する:
- 12.2 のチェック項目 #1-#7 を全て確認
- `./scripts/process/build.sh` のログに `WARN`, `ERROR`, `panic` が出ていないこと
- 既存の compaction_test.go テストにリグレッションがないこと

## Documentation

#### [MODIFY] [051-Session-History-And-MapReduce-Compaction.md](file:///prompts/phases/000-foundation/branches/feat-llm-backend/ideas/051-Session-History-And-MapReduce-Compaction.md)
*   **更新内容**: 実装計画作成完了のステータスを追記
