# 000-Wayfinder-Format-Canonical-Store-And-Ingest

> **Source Specification**: [ideas/000-Wayfinder-Format-Session-Portability.md](file://prompts/phases/001-phase02/branches/feat-session-migration/ideas/000-Wayfinder-Format-Session-Portability.md)

## Goal Description

Wayfinder 形式（`record.json` / `metadata.json` / `history/` / `context.json`）を全 Coding Agent 共通セッションの正本とし、ワークスペースの `{work_dir}/.tern/{session_id}/` にフォルダ単位で保存する。各 history エントリに `origin`（`claudecode` | `codex` | `wayfinder`）を記録する。`SendMessage` ターン完了時に `StreamEvent` を正本へ追記する。エージェント切替と補完注入は Part 2（001）で扱う。

## User Review Required

1. **`session_dir` デフォルト変更**: 未指定時を `work_dir/.{agent_name}` から `work_dir/.tern/{session_id}` へ変える（仕様 R6）。1 セッション = 1 フォルダ。CLI overlay は `{session_dir}/native`。明示 `session_dir` はそのフォルダをセッション単位として維持。
2. **ワークスペース正本（R10、必須）**: `MemorySessionStore` はキャッシュ。`record.json` を `{session_dir}` に書き、`GET /sessions?work_dir=` で `.tern/` を走査して復元する。成果物 SQLite は使わない。`.tern/` 内に session.db を置かない。
3. **任意要件 R9 / R11 / R12**: 本計画（Part 1 / Part 2）では実装しない。
4. **Wayfinder エージェント内部ストア**: `{session_dir}/{wayfinderSessionID}/` の入れ子は維持してよい（R8）が、正本の `history/` と衝突させない。agentservice が書く正本は `{session_dir}/` 直下。

上記は仕様の決定を実装へ落としたものであり、反対がなければこのまま進める。

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: Wayfinder 形式を正本（history append-only、context は派生、`.tern/{id}` 配置） | Proposed Changes > wayfinder/session canonical.go, session_store.go |
| R2: エントリ単位 origin（3 値、user も active agent、immutable） | Proposed Changes > session_state.go, history.go, canonical.go |
| R3: ターン完了時 StreamEvent 取り込み、結合、冪等 | Proposed Changes > agentservice/ingest.go, handler.go |
| R6: session_dir 未指定は `work_dir/.tern/{session_id}`、native は `{session_dir}/native`、overlay 保護 | Proposed Changes > handler.go, options.go, config_overlay.go |
| R8: 旧 history の origin 欠落は `wayfinder`、既存 Wayfinder フロー非破壊 | Proposed Changes > history.go entryToMessage, session_store.go |
| R10: record.json と work_dir List による復元。SQLite を会話正本にしない | Proposed Changes > workspace_session_store.go, handler.go |
| R4, R5, R7 | Part 2 `001-Wayfinder-Format-Agent-Switch-And-Supplement.md` |
| R9, R11, R12 | 対象外 |

## Proposed Changes

### wayfinder/session（データモデルと正本 I/O）

#### [MODIFY] [shared/libs/go/wayfinder/session/session_state_test.go](file://shared/libs/go/wayfinder/session/session_state_test.go)

*   **Description**: origin 定数と `AgentBinding` / `SessionMetadata` 新フィールドの JSON ラウンドトリップを Failed First で追加する。
*   **Technical Design**: テーブル駆動。
    - origin 付き `Message` を marshal/unmarshal し `Origin` が残る。
    - `SessionMetadata` に `active_agent` と `agent_bindings` を入れて復元する。
    - `supplement` フィールドを入れて復元する（中身の適用は Part 2。Part 1 は JSON ラウンドトリップで落とさない）。
*   **Logic**: 仕様のデータ構造をそのまま使う（後述 Technical Design の型定義）。

#### [MODIFY] [shared/libs/go/wayfinder/session/history_test.go](file://shared/libs/go/wayfinder/session/history_test.go)

*   **Description**: origin の書き込みと旧ファイル互換を Failed First で追加する。
*   **Technical Design**:
    ```go
    func TestAppendHistory_WritesOrigin(t *testing.T)
    // Append {Role:user, Origin:claudecode, Seq:1}
    // 0000001.json の origin == "claudecode"

    func TestLoadHistory_MissingOriginDefaultsWayfinder(t *testing.T)
    // origin キー無し JSON を 0000001.json として配置
    // LoadHistory の Message.Origin == "wayfinder"

    func TestAppendHistory_DoesNotOverwriteExisting(t *testing.T)
    // 既存 0000001.json の origin を claudecode にして再 Append しても変わらない
    ```
*   **Logic**: history は append-only。origin は後から変更しない。

#### [NEW] [shared/libs/go/wayfinder/session/canonical_test.go](file://shared/libs/go/wayfinder/session/canonical_test.go)

*   **Description**: Tern 正本（session_dir 直下）の Init / Append / watermark 更新を Failed First で追加する。
*   **Technical Design**:
    ```go
    func TestCanonical_InitWritesMetadata(t *testing.T)
    func TestCanonical_AppendAssignsSeqAndOrigin(t *testing.T)
    func TestCanonical_NextSeqAfterExistingFiles(t *testing.T)
    func TestCanonical_UpdateBindingWatermark(t *testing.T)
    func TestCanonical_LoadHistoryFromWatermark(t *testing.T)
    // binding.IngestedThroughSeq==2 のとき LoadHistoryFrom(3) は seq>=3 のみ
    ```
*   **Logic**: 未実行エージェントは binding 無し → fromSeq=1 で全件（Part 2 の差分抽出が使う）。

#### [MODIFY] [shared/libs/go/wayfinder/session/session_state.go](file://shared/libs/go/wayfinder/session/session_state.go)

*   **Description**: 仕様どおり origin と bindings を型に追加する。
*   **Technical Design**:
    ```go
    const (
        OriginClaudeCode = "claudecode"
        OriginCodex      = "codex"
        OriginWayfinder  = "wayfinder"
    )

    type Message struct {
        Role         string           `json:"role"`
        Content      string           `json:"content"`
        ContentParts []ContentPart    `json:"content_parts,omitempty"`
        Timestamp    time.Time        `json:"timestamp"`
        Pinned       bool             `json:"pinned"`
        Seq          int              `json:"seq"`
        ToolCalls    []ToolCallRecord `json:"tool_calls,omitempty"`
        ToolCallID   string           `json:"tool_call_id,omitempty"`
        Origin       string           `json:"origin,omitempty"`
    }

    type AgentBinding struct {
        AgentSessionID     string `json:"agent_session_id"`
        IngestedThroughSeq int    `json:"ingested_through_seq"`
    }

    type SessionState struct {
        SessionID        string                  `json:"session_id"`
        ParentID         *string                 `json:"parent_id,omitempty"`
        Status           string                  `json:"status"`
        Messages         []Message               `json:"messages"`
        CreatedFiles     []TrackedFile           `json:"created_files"`
        RunningProcesses []TrackedProcess        `json:"running_processes"`
        WBSTreeJSON      json.RawMessage         `json:"wbs_tree,omitempty"`
        CreatedAt        time.Time               `json:"created_at"`
        LastActivityAt   time.Time               `json:"last_activity_at"`
        ActiveAgent      string                  `json:"active_agent,omitempty"`
        AgentBindings    map[string]AgentBinding `json:"agent_bindings,omitempty"`
    }

    type SessionMetadata struct {
        SessionID        string                  `json:"session_id"`
        ParentID         *string                 `json:"parent_id,omitempty"`
        Status           string                  `json:"status"`
        Latest           int                     `json:"latest"`
        TotalSeq         int                     `json:"total_seq"`
        ContextStart     int                     `json:"context_start"`
        CreatedAt        time.Time               `json:"created_at"`
        UpdatedAt        time.Time               `json:"updated_at"`
        WBSTreeJSON      json.RawMessage         `json:"wbs_tree,omitempty"`
        CreatedFiles     []TrackedFile           `json:"created_files"`
        RunningProcesses []TrackedProcess        `json:"running_processes"`
        ActiveAgent      string                  `json:"active_agent,omitempty"`
        AgentBindings    map[string]AgentBinding `json:"agent_bindings,omitempty"`
        Supplement       SupplementStrategy      `json:"supplement,omitempty"`
    }

    // SupplementStrategy の適用（MapReduce 等）は Part 2。Part 1 はフィールドを落とさない。
    type SupplementStrategy struct {
        Algorithm        string `json:"algorithm,omitempty"`
        Model            string `json:"model,omitempty"`
        MaxChunkMessages int    `json:"max_chunk_messages,omitempty"`
        ThresholdBytes   int    `json:"threshold_bytes,omitempty"`
        RecentKeep       int    `json:"recent_keep,omitempty"`
    }

    func NormalizeOrigin(origin string) string {
        switch origin {
        case OriginClaudeCode, OriginCodex, OriginWayfinder:
            return origin
        default:
            return OriginWayfinder
        }
    }
    ```
*   **Logic**: origin の正規値はアダプタ `Name()` と一致。空・未知は読み込み時だけ `wayfinder` に落とす。書き込み側は active agent の `Name()` をそのまま入れる。

#### [MODIFY] [shared/libs/go/wayfinder/session/history.go](file://shared/libs/go/wayfinder/session/history.go)

*   **Description**: `HistoryEntry` に Origin を追加し、Load 時に欠落を `wayfinder` にする。
*   **Technical Design**:
    ```go
    type HistoryEntry struct {
        Seq          int              `json:"seq"`
        Role         string           `json:"role"`
        Content      string           `json:"content"`
        ContentParts []ContentPart    `json:"content_parts,omitempty"`
        Timestamp    time.Time        `json:"timestamp"`
        ToolCalls    []ToolCallRecord `json:"tool_calls,omitempty"`
        ToolCallID   string           `json:"tool_call_id,omitempty"`
        Origin       string           `json:"origin,omitempty"`
    }
    ```
    `AppendHistory` は `entry.Origin = NormalizeOrigin(msg.Origin)` を書いてから marshal する（新規ファイルのみ。既存ファイルは skip のまま）。
    `entryToMessage` は `Origin: NormalizeOrigin(entry.Origin)` をセットする。
*   **Logic**: 欠落 origin は `wayfinder`。既存ファイルは上書きしない。

#### [MODIFY] [shared/libs/go/wayfinder/session/session_store.go](file://shared/libs/go/wayfinder/session/session_store.go)

*   **Description**: `Save` / `loadFromFolder` / `migrateToFolder` で `ActiveAgent` と `AgentBindings` を落とさない。
*   **Technical Design**: `meta := SessionMetadata{ ..., ActiveAgent: state.ActiveAgent, AgentBindings: state.AgentBindings }`。load 時は `state.ActiveAgent = meta.ActiveAgent` 等。既存メタにフィールドが無い JSON はゼロ値でよい。
*   **Logic**: Wayfinder エージェントの入れ子ストアを破壊しない。正本（session_dir 直下）は `canonical.go` が担当。

#### [NEW] [shared/libs/go/wayfinder/session/canonical.go](file://shared/libs/go/wayfinder/session/canonical.go)

*   **Description**: Tern 共通正本。`session_dir` 直下を 1 セッションフォルダとして扱う。
*   **Technical Design**:
    ```go
    type Canonical struct {
        Dir string // record.SessionDir
    }

    func OpenCanonical(sessionDir string) *Canonical

    func (c *Canonical) Init(sessionID, activeAgent string) error
    // mkdir sessionDir, history/
    // metadata.json が無ければ SessionMetadata{
    //   SessionID: sessionID, Status: StatusActive,
    //   ActiveAgent: activeAgent, AgentBindings: map[string]AgentBinding{},
    //   CreatedAt/UpdatedAt: now, TotalSeq: 0, Latest: 0, ContextStart: 0,
    // } を atomicWrite
    // 既存 metadata があれば ActiveAgent だけ同期し bindings は保持

    func (c *Canonical) LoadMetadata() (*SessionMetadata, error)
    func (c *Canonical) saveMetadata(meta *SessionMetadata) error

    func (c *Canonical) NextSeq() (int, error)
    // metadata.TotalSeq+1。metadata が無い場合は history/ の %07x.json の最大 seq+1。空なら 1

    func (c *Canonical) Append(msgs []Message) error
    // Seq==0 のメッセージに NextSeq から連番を振る
    // AppendHistory(c.Dir/history, msgs)
    // metadata.TotalSeq / Latest / UpdatedAt 更新

    func (c *Canonical) LoadRange(fromSeq, toSeq int) ([]Message, error)
    // toSeq<=0 なら TotalSeq まで

    func (c *Canonical) UpdateBinding(agent, nativeSessionID string, throughSeq int) error
    // AgentBindings[agent] = {AgentSessionID: nativeSessionID, IngestedThroughSeq: throughSeq}
    // nativeSessionID が空なら既存 AgentSessionID を保持
    ```
*   **Logic**: 配置は仕様どおり。
    ```text
    {session_dir}/                      # {work_dir}/.tern/{session_id}
      record.json                      # SessionRecord（R10）
      metadata.json
      context.json                     // Part 1 では必須更新しない（派生。空でも可）
      history/0000001.json
      native/                          // アダプタの SessionDir。Part 1 で MkdirAll
    ```
    `native/projects/` / `native/sessions/` には触れない。Init で `record.json` を SessionRecord 相当の JSON で書く（ID, AgentName, WorkDir, SessionDir）。

### codingagent overlay / session_dir デフォルト

#### [MODIFY] [shared/libs/go/codingagent/config_overlay_test.go](file://shared/libs/go/codingagent/config_overlay_test.go)

*   **Description**: overlay が正本ファイルを消さないことを Failed First で追加する。
*   **Technical Design**: overlay のルートは `{session_dir}/native`。そこに `projects` を置く既存テストは維持。正本側に誤って overlay しても残るよう、`record.json` / `metadata.json` / `context.json` / `history` を保護名に入れる。canonical 直下にそれらを置いて overlay するケースを Failed First で追加する。
*   **Logic**: 仕様 R6。保護名に正本を追加する。

#### [MODIFY] [shared/libs/go/codingagent/config_overlay.go](file://shared/libs/go/codingagent/config_overlay.go)

*   **Description**: 保護 basename を追加する。
*   **Technical Design**:
    ```go
    var protectedSessionBasenames = map[string]struct{}{
        "projects": {}, "sessions": {}, "statsig": {}, "debug": {},
        "logs": {}, "tmp": {}, "cache": {},
        "history": {}, "metadata.json": {}, "context.json": {}, "record.json": {}, "native": {},
    }
    ```
*   **Logic**: allowlist に出ても削除・置換しない。既存保護は維持。

#### [MODIFY] [shared/libs/go/codingagent/options_test.go](file://shared/libs/go/codingagent/options_test.go)

*   **Description**: `ApplyDefaults` はエージェント名付き `.claudecode` フォールバックをやめる。セッション ID 無しでは `{work_dir}/.tern` を仮置きしない（衝突するため。Tern フォルダは handler が `{session_id}` 付きで決める）。
*   **Technical Design**: 既存ケース `"session dir includes agent name when set"` を削除。代わり:
    ```go
    t.Run("session dir stays empty when unset", func(t *testing.T) {
        cfg := codingagent.NewSessionConfig(codingagent.WithWorkDir("/workspace/project"))
        ac := &codingagent.AdapterConfig{AgentName: "claudecode"}
        codingagent.ApplyDefaults(cfg, ac)
        if cfg.SessionDir != "" && !strings.HasSuffix(cfg.SessionDir, "native") {
            // DefaultSessionDir 未設定なら空のまま。native 補完は agentservice 側
        }
    })
    ```
    `DefaultSessionDir` / `WithSessionDir` 明示時は従来どおり優先。
*   **Logic**: 仕様 R6。複数セッションが同一 `.tern` 直下の metadata を奪い合わない。

#### [MODIFY] [shared/libs/go/codingagent/options.go](file://shared/libs/go/codingagent/options.go)

*   **Description**: `AgentName` から `{work_dir}/.{agent}` を合成しない。空の SessionDir は空のままにする（`DefaultSessionDir` があるときだけそれを使う）。
*   **Technical Design**:
    ```go
    if cfg.SessionDir == "" && ac.DefaultSessionDir != "" {
        cfg.SessionDir = ac.DefaultSessionDir
    }
    ```
*   **Logic**: アダプタ起動時の SessionDir は agentservice が `filepath.Join(record.SessionDir, "native")` を渡す。

### agentservice（Workspace SessionStore）

#### [NEW] [shared/libs/go/agentservice/workspace_session_store_test.go](file://shared/libs/go/agentservice/workspace_session_store_test.go)

*   **Description**: `{work_dir}/.tern/{id}/record.json` の永続化と再ロードを Failed First で追加する。
*   **Technical Design**:
    ```go
    func TestWorkspaceSessionStore_CreateWritesRecordJSON(t *testing.T)
    // Create 後に workDir/.tern/{id}/record.json が存在
    // history/ はディレクトリとして存在する
    // workDir/.tern/session.db は無い

    func TestWorkspaceSessionStore_ListByWorkDirReloads(t *testing.T)
    // storeA で Create したあと storeB := NewWorkspaceSessionStore()
    // ListByWorkDir(workDir) で同じ ID / SessionDir / AgentName
    // Get(id) が storeB で成功する
    ```
*   **Logic**: 仕様 R10。メモリはキャッシュ。索引 SQLite は作らない。

#### [NEW] [shared/libs/go/agentservice/workspace_session_store.go](file://shared/libs/go/agentservice/workspace_session_store.go)

*   **Description**: `codingagent.SessionStore` をディスク正本付きで実装する。既存 `MemorySessionStore` を内包してよい。
*   **Technical Design**:
    ```go
    func NewWorkspaceSessionStore() codingagent.SessionStore

    func (s *WorkspaceSessionStore) Create(session *SessionRecord) error
    // SessionDir 未設定かつ WorkDir あり → Join(WorkDir, ".tern", ID)
    // MkdirAll(SessionDir), MkdirAll(Join(SessionDir,"native"))
    // record.json を atomic write
    // session.OpenCanonical(SessionDir).Init(...)
    // メモリへ載せる

    func (s *WorkspaceSessionStore) Update(session *SessionRecord) error
    // record.json を上書き + メモリ

    func (s *WorkspaceSessionStore) ListByWorkDir(workDir string) ([]*SessionRecord, error)
    // 絶対パス化。Join(workDir, ".tern") 配下の子ディレクトリで record.json があるものを読む
    // メモリへマージして返す

    func NativeSessionDir(sessionDir string) string { return filepath.Join(sessionDir, "native") }
    ```
    `List()` はメモリ上のみ。handler の List が `work_dir` クエリを持つなら `ListByWorkDir` を呼ぶ。
*   **Logic**: Create はディスクへ flush。既定の agentservice.New は WorkspaceSessionStore を使う。MemorySessionStore は単体のフェイクに残してよい。

### agentservice（正本初期化と ingest）

#### [NEW] [shared/libs/go/agentservice/ingest_test.go](file://shared/libs/go/agentservice/ingest_test.go)

*   **Description**: StreamEvent → history 変換の単体テスト（Failed First）。
*   **Technical Design**:
    ```go
    func TestEventsToMessages_MergesConsecutiveText(t *testing.T)
    // EventText "he" + EventText "llo" + EventResult
    // => 1 assistant Content=="hello", Origin==claudecode

    func TestEventsToMessages_ToolUseAndResult(t *testing.T)
    // text, tool_use{name:Read,input:{file_path:a.go}}, tool_result{"ok"}, result
    // => assistant text, assistant with ToolCalls, role=tool Content=ok

    func TestIngestTurn_AppendsWithoutDuplicatingUser(t *testing.T)
    // 先に user を Canonical.Append 済みの dir に events を Ingest
    // history の user は 1 件のまま、assistant が増える
    // metadata.agent_bindings[claudecode].ingested_through_seq == 最新 seq

    func TestIngestTurn_UpdatesNativeSessionID(t *testing.T)
    // EventSystem{SessionID:"abc-123"} で binding.AgentSessionID=="abc-123"
    ```
*   **Logic**: 仕様 ingest 手順。user は SendMessage 開始時に別途 Append する。Ingest はイベント由来のみ。

#### [NEW] [shared/libs/go/agentservice/ingest.go](file://shared/libs/go/agentservice/ingest.go)

*   **Description**: ターン完了時の正本追記。
*   **Technical Design**:
    ```go
    func EventsToMessages(origin string, events []codingagent.StreamEvent) []session.Message
    func IngestTurn(sessionDir, origin, nativeSessionID string, events []codingagent.StreamEvent) error
    ```
    `EventsToMessages` の走査規則（仕様の取り込み手順を具体化）:
    1. `origin = session.NormalizeOrigin(origin)`（呼び出し元は active `AgentName` を渡す）
    2. 連続する `EventText` は 1 つの `strings.Builder` に結合する
    3. `EventToolUse` が来たら、バッファ中の text を先に `Role=assistant` で flush。続けて `Role=assistant`, `ToolCalls: []ToolCallRecord{{ID:"", Name: ev.ToolName, Input: ev.ToolInput}}` を 1 メッセージにする
    4. `EventToolResult` は `Role=tool`, `Content: ev.Content`
    5. `EventResult` または列の終端で残った text を assistant として flush
    6. `EventSystem` はメッセージにしない（native id は `IngestTurn` が `ev.SessionID != ""` の最後の値を nativeSessionID より優先）
    7. `EventError` / `EventUserInputRequired` は history に書かない
    各 Message に `Origin: origin`, `Timestamp: time.Now()` を付ける。Seq は Canonical.Append が振る。

    `IngestTurn`:
    1. `c := session.OpenCanonical(sessionDir)`
    2. `msgs := EventsToMessages(origin, events)`
    3. `c.Append(msgs)`（msgs が空でも binding 更新は行う）
    4. meta を読み、`through := meta.TotalSeq`、native id は引数と EventSystem の非空を採用
    5. `c.UpdateBinding(origin, nativeID, through)`
*   **Logic**: 二重書き防止は「user は Ingest しない」「既存 seq ファイルは AppendHistory が skip」。ターン境界ごとに assistant/tool だけ足す。

#### [MODIFY] [shared/libs/go/agentservice/handler_test.go](file://shared/libs/go/agentservice/handler_test.go)

*   **Description**: CreateSession で metadata / record が初期化されること、デフォルトが `.tern/{id}` であること、SendMessage 完了後に origin 付き history があることを追加する。既存の `.claudecode` デフォルト期待があれば直す。
*   **Technical Design**:
    ```go
    func TestHandleCreateSession_InitCanonicalMetadata(t *testing.T)
    func TestHandleCreateSession_DefaultSessionDirTern(t *testing.T)
    // session_dir 省略 → GET の session_dir が filepath.Join(abs(workDir), ".tern", sessionID)

    func TestHandleListSessions_ByWorkDirReloadsFromDisk(t *testing.T)
    // Create 後、サーバの sessions を空にした相当（新 Server）で GET ?work_dir=
    // 同じ id が戻る

    func TestHandleSendMessage_IngestsAssistantWithOrigin(t *testing.T)
    // mock は EventText+"hello" + EventResult + EventSystem{SessionID:"sdk-1"}
    // history に user origin=claudecode と assistant origin=claudecode
    // metadata.agent_bindings["claudecode"].agent_session_id == "sdk-1"

    func TestHandleSendMessage_SameAgentSecondTurnResumesAndKeepsFact(t *testing.T)
    // シナリオ B1: 1 ターン目 user に CTX-TOKEN-7F3A
    // 2 ターン目 lastCfg.AgentSessionID == sdk-1
    // Prompt に TransferHeader が無い
    // history に CTX-TOKEN-7F3A が残る
    ```
    mock の `Send` に `EventSystem` を足して現行テストが壊れないことを確認する。追加イベントは無視されてきたので、既存テストは EventResult で完了していれば維持できる。
*   **Logic**: シナリオ B1 / 1 / 5 の単体相当。

#### [MODIFY] [shared/libs/go/agentservice/multimodal.go](file://shared/libs/go/agentservice/multimodal.go)

*   **Description**: `AppendSessionMessage` を Canonical 経由にし、seq 採番の `%x` と `%07x` 不整合を解消する。origin を引数または Message.Origin から渡す。
*   **Technical Design**:
    ```go
    func AppendSessionMessage(sessionDir string, msg session.Message) error {
        if sessionDir == "" {
            return nil
        }
        c := session.OpenCanonical(sessionDir)
        if msg.Origin == "" {
            msg.Origin = session.OriginWayfinder
        }
        return c.Append([]session.Message{msg})
    }
    ```
    `%x.json` の Sscanf ループは削除する。
*   **Logic**: user 追記も正本 API に統一。

#### [MODIFY] [shared/libs/go/agentservice/handler.go](file://shared/libs/go/agentservice/handler.go)

*   **Description**: CreateSession で正本 Init。session_dir デフォルトを `.tern`。user に origin。ターン完了で IngestTurn。
*   **Technical Design**:
    1. `handleCreateSession` のフォールバック（ID 採番のあと）:
       ```go
       if record.SessionDir == "" && record.WorkDir != "" {
           record.SessionDir = filepath.Join(record.WorkDir, ".tern", record.ID)
       }
       ```
       WorkspaceSessionStore.Create が record.json と Canonical.Init を行う。
       アダプタの `CreateSession` には `WithSessionDir(NativeSessionDir(record.SessionDir))` を渡す。
    2. `handleListSessions`: クエリ `work_dir` があれば `ListByWorkDir`。
    2. `handleSendMessage` の user Append:
       ```go
       AppendSessionMessage(record.SessionDir, session.Message{
           Role: "user", ContentParts: sessionParts,
           Timestamp: time.Now(), Origin: record.AgentName,
       })
       ```
    3. `finishActiveExecution` の先頭（Unregister 前）:
       ```go
       if exec, ok := s.execRegistry.Get(sessionID); ok {
           rec, _ := s.sessions.Get(sessionID)
           if rec != nil && rec.SessionDir != "" {
               native := rec.AgentSessionID
               _ = IngestTurn(rec.SessionDir, rec.AgentName, native, exec.relay.snapshot())
               if meta, err := session.OpenCanonical(rec.SessionDir).LoadMetadata(); err == nil {
                   rec.AgentSessionID = bindingNative(meta, rec.AgentName, rec.AgentSessionID)
                   _ = s.sessions.Update(rec)
               }
           }
       }
       ```
    4. `eventRelay.snapshot()` を `exec_registry.go` に追加（`events` のコピーを返す）。
*   **Logic**: クライアント切断後も `finishActiveExecution` が走る経路で取り込みする。SSE 完了時も同関数を呼ぶ現行を維持。Ingest 失敗はログしてターン成功を落とさない（正本遅延より実行結果を優先）。GET で bindings を返すのは Part 2。

#### [MODIFY] [shared/libs/go/agentservice/exec_registry.go](file://shared/libs/go/agentservice/exec_registry.go)

*   **Description**: `func (r *eventRelay) snapshot() []codingagent.StreamEvent`
*   **Technical Design**: ロックして `events` をコピー。nil relay は nil。
*   **Logic**: ingest の一次ソース。

## Step-by-Step Implementation Guide

- [x] **TDD データモデル**: `session_state_test.go` / `history_test.go` を追加し、現状 Failed を確認する。
- [x] **型拡張**: `session_state.go` / `history.go` / `session_store.go` を実装し単体を通す。
- [x] **TDD Canonical**: `canonical_test.go` を書き、`canonical.go` を実装する。
- [x] **TDD overlay / WorkspaceStore**: 保護名、空 SessionDir、record.json 再ロード。
- [x] **TDD ingest**: `ingest_test.go` を書き、`ingest.go` を実装する。
- [x] **handler 接続**: CreateSession を `.tern/{id}` + native、List work_dir、user Origin、IngestTurn。
- [x] **handler_test 更新**: デフォルト session_dir と origin 取り込みを通す。
- [x] **ビルド**: `./scripts/process/build.sh`（Linux なら `./scripts/process/build.sh --skip-etc`）。

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests**: `./scripts/process/build.sh`（Linux / Remote-SSH は `./scripts/process/build.sh --skip-etc`）
2.  **Integration Tests（本 Part では新規 E2E は必須としない。ingest は handler httptest）**: 既存が壊れないこと。
    `./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories common --specify "TestHandleCreateSession|TestHandleSendMessage"`
    Linux: `./scripts/process/build.sh --skip-etc && xvfb-run -a ./scripts/process/integration_test.sh --categories common --specify "TestHandleCreateSession|TestHandleSendMessage"`
    （`handle_*` は `go test` 対象のパッケージ単体で `build.sh` に含まれる。統合スクリプト側に同名が無ければ既存 `TestIntegration` 相当の agentservice テストでリグレッションする。）
3.  **E2E**: Part 2 の `tests/llm_session_portability_test.go` でシナリオ 1 をコード化する。Part 1 完了時点では単体 + 既存統合のパスを必須とする。

## Documentation

Part 1 単独では `docs/ReferenceManual-WebAPIs.md` の session_dir デフォルト記述（`work_dir/.{agent_name}`）を **`work_dir/.tern/{session_id}`** に更新する。`GET /api/v1/sessions?work_dir=` を追記する。切替 API の記述は Part 2。
