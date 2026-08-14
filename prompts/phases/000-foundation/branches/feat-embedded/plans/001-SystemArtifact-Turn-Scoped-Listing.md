# 001-SystemArtifact-Turn-Scoped-Listing

> **Source Specification**: prompts/phases/000-foundation/branches/feat-embedded/ideas/001-SystemArtifact-Turn-Scoped-Listing.md

## Goal Description

同一 `session_id` の複数 `SendMessage` 実行で混在している System Artifact イベントを、`turn_id` を一次キーとしてターン単位に相関・抽出できるようにする。

- `SendMessage` 開始時にサーバー生成 `turn_id` を採番する
- 任意 `correlation_id` を受け付け、同ターンのイベントへ引き回す
- `GET /api/v1/artifacts/system` と `client/v1` に `turn_id` フィルタを追加する
- `respond` を同一ターン継続として扱う
- Reconciliation 補完イベントもターン帰属させる

## User Review Required

1. **SSE 返却契約**: `turn_id` を SSE の先頭 `system` イベントで返す案で確定してよいか。  
   代替として HTTP Header（例: `X-Tern-Turn-ID`）返却も可能だが、SDK 側の扱いが異なる。
2. **`correlation_id` の List フィルタ**: 初回実装で `correlation_id` の List フィルタまで入れるか、まず `turn_id` のみに絞るか。
3. **Reconciliation 厳密化範囲**: 初回で git/snapshot のターン境界化を実施するか、先に realtime 経路のみターン対応して段階導入するか。

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: Tern が `SendMessage` 開始時にサーバー生成の `turn_id` を採番する | Proposed Changes > Agent Execution Context > `shared/libs/go/agentservice/handler.go`, `shared/libs/go/agentservice/exec_registry.go` |
| R2: `POST /api/v1/sessions/:id/messages` が任意の `correlation_id` を受け付ける | Proposed Changes > Agent Execution Context > `shared/libs/go/agentservice/handler.go` |
| R3: `system_artifact_events` に `turn_id` と `correlation_id` を保存できる | Proposed Changes > Store Schema and Query > `shared/libs/go/artifact/store/models.go`, `shared/libs/go/artifact/store/store.go` |
| R4: `GET /api/v1/artifacts/system` に `turn_id` フィルタを追加する | Proposed Changes > API Contract > `shared/libs/go/artifact/api/system.go` |
| R5: `client/v1` の `SystemArtifactFilter` に `TurnIDs` を追加する | Proposed Changes > Client API > `client/v1/artifacts.go` |
| R6: `SendMessage` の SSE/完了経路で `turn_id` を返却する | Proposed Changes > Stream Event Contract > `shared/libs/go/codingagent/event.go`, `shared/libs/go/agentservice/handler.go`, `client/v1/stream.go` |
| R7: `respond` は同一ターン継続として同一 `turn_id` を維持する | Proposed Changes > Agent Execution Context > `shared/libs/go/agentservice/handler.go`, `shared/libs/go/agentservice/exec_registry.go` |
| R8: Reconciliation 補完イベントもターン帰属できるようにする | Proposed Changes > Reconciliation Turn Boundary > `shared/libs/go/agentservice/artifact_reconcile.go`, `shared/libs/go/artifact/analyzer/reconcile.go`, `shared/libs/go/artifact/analyzer/git_diff.go` |
| R9: 既存データ互換（`turn_id` 空許容）を維持する | Proposed Changes > Store Schema and Query > `shared/libs/go/artifact/store/store.go` |
| R10: API リファレンスと `client/v1` ドキュメントを更新する | Proposed Changes > Documentation > `docs/ReferenceManual-WebAPIs.md`, `README.md`, `client/v1/artifacts_test.go` |
| O1: 最新完了ターン簡便取得 API | Proposed Changes > Deferred Scope（今回は未実装） |
| O2: ターン統計メタ情報 | Proposed Changes > Deferred Scope（今回は未実装） |
| O3: archive API の `turn_id` 指定 | Proposed Changes > Deferred Scope（今回は未実装） |

## Proposed Changes

### Store Schema and Query

#### [NEW] shared/libs/go/artifact/store/store_turn_test.go
*   **Description**: `turn_id` / `correlation_id` 追加後の保存・検索・後方互換を先に失敗させるテストを追加する。
*   **Technical Design**:
    *   テーブル駆動で `SystemArtifactFilter{TurnIDs: ...}` の検索結果を検証する。
    *   既存レコード（`turn_id=""`）と新規レコード（`turn_id!=""`）の混在検索を検証する。
*   **Logic**:
    *   Case1: `turn_id=t1` で `a.txt` のみ取得
    *   Case2: `turn_id=t2` で `b.txt` のみ取得
    *   Case3: `turn_id` 未指定で両方取得
    *   Case4: `correlation_id` 混在時に `turn_id` 主体で期待結果を維持

#### [MODIFY] [shared/libs/go/artifact/store/models.go](file://shared/libs/go/artifact/store/models.go)
*   **Description**: System Artifact イベントとフィルタのデータ構造を拡張する。
*   **Technical Design**:
```go
type SystemArtifactEvent struct {
    ID            int64
    SessionID     string
    AgentID       string
    TurnID        string
    CorrelationID string
    Key           string
    ActualPath    string
    Operation     string
    OccurredAt    time.Time
    ToolName      string
    ContentSHA    string
}

type SystemArtifactFilter struct {
    Q              string
    AgentIDs       []string
    SessionIDs     []string
    TurnIDs        []string
    CorrelationIDs []string
    Operation      string
    Since          *time.Time
    Until          *time.Time
    IncludeDeleted bool
    Page           int
    PerPage        int
    Sort           string
    Order          string
}
```
*   **Logic**: 仕様書で定義した `turn_id` 必須運用、`correlation_id` 任意運用を保持できる構造を追加する。

#### [MODIFY] [shared/libs/go/artifact/store/store.go](file://shared/libs/go/artifact/store/store.go)
*   **Description**: DB マイグレーション・INSERT・SELECT に `turn_id` / `correlation_id` を追加する。
*   **Technical Design**:
    *   `migrate()` に段階的マイグレーションを追加する。
      - `ALTER TABLE system_artifact_events ADD COLUMN turn_id TEXT NOT NULL DEFAULT ''`
      - `ALTER TABLE system_artifact_events ADD COLUMN correlation_id TEXT NOT NULL DEFAULT ''`
      - `CREATE INDEX IF NOT EXISTS idx_sae_turn ON system_artifact_events(turn_id);`
      - `CREATE INDEX IF NOT EXISTS idx_sae_correlation ON system_artifact_events(correlation_id);`
    *   `SaveSystemArtifactEvent` の INSERT 列を拡張する。
    *   `filterSystemArtifacts` の WHERE 条件に `turn_id IN (...)`（必要なら `correlation_id IN (...)`）を追加する。
*   **Logic**:
    *   既存行は `turn_id=""` / `correlation_id=""` のまま残り、未指定フィルタで従来挙動を維持する。
    *   `turn_id` 指定時はターン一致のみを返す。

---

### Agent Execution Context

#### [NEW] shared/libs/go/agentservice/handler_turn_test.go
*   **Description**: `SendMessage` / `respond` のターンコンテキスト継続を先に検証する。
*   **Test Cases**:
    *   `TestSendMessage_AssignsTurnID`: 新規実行で `turn_id` が採番される。
    *   `TestSendMessage_AcceptsCorrelationID`: `correlation_id` が execution に保存される。
    *   `TestRespond_KeepsSameTurnID`: `respond` 後も同一 `turn_id` を保持する。
    *   `TestConcurrentSendMessage_DifferentTurnID`: 同一 session 連続実行でターン ID が毎回変わる。

#### [MODIFY] [shared/libs/go/agentservice/handler.go](file://shared/libs/go/agentservice/handler.go)
*   **Description**: `SendMessageRequest` 拡張、ターン採番、SSE 返却、`respond` 継続を実装する。
*   **Technical Design**:
```go
type SendMessageRequest struct {
    Content       []codingagent.ContentPart `json:"content"`
    CorrelationID string                    `json:"correlation_id,omitempty"`
}

type activeExecution struct {
    sessionID      string
    turnID         string
    correlationID  string
    agentSess      codingagent.Session
    stdin          codingagent.StdinWriter
    relay          *eventRelay
    status         string
    streamOffset   int
}
```
*   **Logic**:
    *   `handleSendMessage` で `turn_id` を生成し `activeExecution` に保持する。
    *   `respond` は既存 `activeExecution` を使うため、同じ `turn_id` を継続する。
    *   SSE 経路で最初に `turn_id` を通知する（`system` イベントまたは定義済みメタ経路）。

#### [MODIFY] [shared/libs/go/agentservice/exec_registry.go](file://shared/libs/go/agentservice/exec_registry.go)
*   **Description**: 実行コンテキストに `turn_id` / `correlation_id` を保持するための構造拡張を行う。
*   **Technical Design**:
    *   `activeExecution` に新フィールドを追加
    *   既存 register/get/setstatus/unregister の API は互換維持
*   **Logic**: busy 制御ロジックは維持しつつ、ターン相関情報だけを追加する。

---

### Analyzer and Reconciliation Turn Boundary

#### [NEW] shared/libs/go/agentservice/artifact_reconcile_turn_test.go
*   **Description**: ターン開始ベースラインと終了差分の組で補完イベントが誤帰属しないことを検証する。
*   **Test Cases**:
    *   `TestReconcile_UsesTurnBaselineForGitRepo`
    *   `TestReconcile_UsesTurnBaselineForNonGitSnapshot`
    *   `TestReconcile_DoesNotLeakPreviousTurnChanges`

#### [MODIFY] [shared/libs/go/agentservice/artifact_reconcile.go](file://shared/libs/go/agentservice/artifact_reconcile.go)
*   **Description**: 現在のセッション単位 snapshot 管理をターン単位に変更する。
*   **Technical Design**:
    *   `sessionSnapshots map[string]DirSnapshot` を `turnSnapshots map[string]DirSnapshot` に変更
    *   key は `sessionID + ":" + turnID`
    *   `capture...` と `reconcile...` に `turnID` 引数を追加
*   **Logic**:
    *   各 `SendMessage` 開始時にベースラインを記録
    *   終了時に同じ `turn_id` で差分計算し、補完イベントへ `turn_id` を付与

#### [MODIFY] [shared/libs/go/artifact/analyzer/reconcile.go](file://shared/libs/go/artifact/analyzer/reconcile.go)
*   **Description**: 補完イベント生成に `TurnID` / `CorrelationID` を持ち回る。
*   **Technical Design**:
```go
type ReconcileInput struct {
    SessionID       string
    TurnID          string
    CorrelationID   string
    ExistingEvents  []store.SystemArtifactEvent
    GitChanges      []GitDiffResult
    SnapshotChanges []ParsedFileOp
    StructuredPaths []ParsedFileOp
}
```
*   **Logic**:
    *   `supplements` へ保存する `SystemArtifactEvent` に `TurnID` / `CorrelationID` を設定する。
    *   `TurnID` 空の既存イベントは読み取り専用として扱い、誤って再書き込みしない。

#### [MODIFY] [shared/libs/go/artifact/analyzer/analyzer.go](file://shared/libs/go/artifact/analyzer/analyzer.go)
*   **Description**: realtime 保存イベントに `turn_id` / `correlation_id` を付与するためのコンテキスト参照を追加する。
*   **Technical Design**:
    *   `WorkDirResolver` と同様に `TurnContextResolver` を注入
    *   `buildEvent` 時に `TurnID` / `CorrelationID` を設定
*   **Logic**: Write/Edit/Delete/Shell/file_change すべて同様に同ターン情報を付与する。

#### [MODIFY] [shared/libs/go/artifact/analyzer/git_diff.go](file://shared/libs/go/artifact/analyzer/git_diff.go)
*   **Description**: ターン開始基準から終了時点の差分を取れるよう比較基準パラメータを受けられる形へ拡張する。
*   **Logic**:
    *   現在の `git diff HEAD` 固定ではなく、ターン開始時点の基準から差分を算出する API を追加する。
    *   既存 API は後方互換ラッパーとして残す。

---

### API Contract

#### [NEW] shared/libs/go/artifact/api/system_turn_test.go
*   **Description**: `GET /api/v1/artifacts/system?turn_id=...` の動作を検証する。
*   **Test Cases**:
    *   単一 `turn_id` で絞り込める
    *   複数 `turn_id`（OR）で絞り込める
    *   `turn_id` 未指定時は従来互換
    *   レスポンス item に `turn_id` が含まれる

#### [MODIFY] [shared/libs/go/artifact/api/system.go](file://shared/libs/go/artifact/api/system.go)
*   **Description**: query 受理とレスポンス出力に `turn_id` を追加する。
*   **Technical Design**:
    *   `SystemArtifactFilter` 構築で `TurnIDs: q["turn_id"]` を設定
    *   `systemItemsJSON` に `turn_id`（必要なら `correlation_id`）を追加
*   **Logic**: `turn_id` 指定時は store フィルタに反映、未指定時は既存挙動を維持する。

---

### Stream Event Contract

#### [NEW] client/v1/session_turn_test.go
*   **Description**: `SendMessage` の返却ストリームから `turn_id` を取得できることを検証する。
*   **Test Cases**:
    *   先頭 system イベントに `turn_id` が含まれる
    *   system イベント無しでも終了しない互換動作

#### [MODIFY] [shared/libs/go/codingagent/event.go](file://shared/libs/go/codingagent/event.go)
*   **Description**: SSE/JSON イベントに `turn_id` フィールドを追加する。
*   **Technical Design**:
```go
type StreamEvent struct {
    Type          EventType              `json:"type"`
    Content       string                 `json:"content,omitempty"`
    // ...
    SessionID     string                 `json:"session_id,omitempty"`
    TurnID        string                 `json:"turn_id,omitempty"`
    CorrelationID string                 `json:"correlation_id,omitempty"`
}
```
*   **Logic**: 既存クライアントは未知フィールドを無視できるため後方互換を維持する。

#### [MODIFY] [client/v1/stream.go](file://client/v1/stream.go)
*   **Description**: `turn_id` を含む system イベントを取り出しやすくするヘルパを追加する。
*   **Technical Design**:
    *   `Stream` に `TurnID() string`（最初に観測した turn_id を返す）を追加
    *   または handlers 経路で `OnSystem` に `turn_id` を渡せるよう拡張
*   **Logic**: 既存 API を壊さず、追加 API として公開する。

---

### Client API

#### [NEW] client/v1/artifacts_turn_test.go
*   **Description**: `SystemArtifactFilter.TurnIDs` の query encode と decode を検証する。
*   **Test Cases**:
    *   `TurnIDs: []string{"t1"}` -> `?turn_id=t1`
    *   `TurnIDs: []string{"t1","t2"}` -> `?turn_id=t1&turn_id=t2`
    *   item decode で `turn_id` が保持される

#### [MODIFY] [client/v1/artifacts.go](file://client/v1/artifacts.go)
*   **Description**: `SystemArtifactItem` と `SystemArtifactFilter` を拡張し、List/ListAll の query 構築を更新する。
*   **Technical Design**:
```go
type SystemArtifactItem struct {
    Key        string    `json:"key"`
    Operation  string    `json:"operation"`
    AgentID    string    `json:"agent_id"`
    SessionID  string    `json:"session_id"`
    TurnID     string    `json:"turn_id"`
    OccurredAt time.Time `json:"occurred_at"`
    ToolName   string    `json:"tool_name"`
    SHA        string    `json:"sha"`
}

type SystemArtifactFilter struct {
    Q              string
    AgentIDs       []string
    SessionIDs     []string
    TurnIDs        []string
    CorrelationIDs []string
    Operation      string
    Since          *time.Time
    Until          *time.Time
    IncludeDeleted bool
    Page           int
    PerPage        int
    Sort           string
    Order          string
}
```
*   **Logic**: `for _, id := range f.TurnIDs { q.Add("turn_id", id) }` を追加する。

---

### Documentation

#### [MODIFY] [docs/ReferenceManual-WebAPIs.md](file://docs/ReferenceManual-WebAPIs.md)
*   **Description**: `POST /api/v1/sessions/:id/messages` の `correlation_id`、`GET /api/v1/artifacts/system` の `turn_id` フィルタ、レスポンスの `turn_id` を追記する。
*   **Logic**:
    *   request/response JSON 例を追加
    *   既存クライアント互換（未指定時動作）を明記

#### [MODIFY] [README.md](file://README.md)
*   **Description**: System Artifact のターン単位取得例を追加する。
*   **Logic**:
    *   同一 session で 2 ターン実行後に `turn_id` で絞るサンプルを追加する。

---

### Deferred Scope

#### [DEFER] latest_turn API / ターン統計 / archive turn_id
*   **Description**: O1/O2/O3 は初回リリース対象外。
*   **Reason**: まず R1-R10 の整合性と互換性を優先する。

## Step-by-Step Implementation Guide

### Implementation Progress

- [x] Store の `turn_id` / `correlation_id` モデル・マイグレーション・フィルタを実装
- [x] System Artifact API と client/v1 の `turn_id` 入出力を実装
- [x] `SendMessage` に `correlation_id` を追加し、ターン採番を実装
- [x] SSE ストリームに `turn_id` / `correlation_id` メタ情報を実装
- [x] Analyzer でターンコンテキストを保存イベントに伝搬
- [x] Reconciliation 保存イベントへの `turn_id` / `correlation_id` 反映を実装
- [/] Reconciliation の git ターン境界厳密化（開始時点基準の差分）は継続対応
- [x] 単体・統合テスト追加と指定テスト実行を完了

1. **Red: Store tests**: Add `shared/libs/go/artifact/store/store_turn_test.go` with failing cases for `turn_id` filters and legacy rows.
2. **Red: API tests**: Add `shared/libs/go/artifact/api/system_turn_test.go` for `turn_id` query and response field.
3. **Red: Agent turn tests**: Add `shared/libs/go/agentservice/handler_turn_test.go` and `artifact_reconcile_turn_test.go` for turn context and reconcile boundary.
4. **Red: Client tests**: Add `client/v1/artifacts_turn_test.go` and `client/v1/session_turn_test.go`.
5. **Green: Data model and migration**: Edit `shared/libs/go/artifact/store/models.go` and `store.go` to add columns, migration, insert/select/filter updates.
6. **Green: Agent request/execution context**: Edit `shared/libs/go/agentservice/handler.go` and `exec_registry.go` to receive `correlation_id`, generate `turn_id`, and preserve it across `respond`.
7. **Green: Analyzer turn propagation**: Edit `shared/libs/go/artifact/analyzer/analyzer.go` and `reconcile.go` to carry turn context into saved events.
8. **Green: Reconcile boundary implementation**: Edit `shared/libs/go/agentservice/artifact_reconcile.go` and `shared/libs/go/artifact/analyzer/git_diff.go` to baseline per turn.
9. **Green: HTTP API layer**: Edit `shared/libs/go/artifact/api/system.go` to parse `turn_id` and emit `turn_id` in JSON.
10. **Green: Stream event contract**: Edit `shared/libs/go/codingagent/event.go` and `client/v1/stream.go` to expose `turn_id`.
11. **Green: Client artifact filter**: Edit `client/v1/artifacts.go` to encode `turn_id` query and decode response fields.
12. **Refactor and compatibility check**: Confirm no regression for legacy events with empty `turn_id`.
13. **Docs update**: Edit `docs/ReferenceManual-WebAPIs.md` and `README.md` with new request/response examples.
14. **Final verification run**: Execute build and integration scripts, then fix failures and rerun targeted suites.

## Verification Plan

### Automated Verification

1. **Build & Unit Tests**: `./scripts/process/build.sh --skip-frontend --skip-etc`
2. **Integration Tests (targeted turn scope)**: `./scripts/process/build.sh --skip-frontend --skip-etc && ./scripts/process/integration_test.sh --categories common --specify "TestE2E_SystemArtifact_TurnScopedList|TestE2E_SystemArtifact_RespondSameTurnID|TestReconcile_TurnScopedSupplement|TestSystemArtifact_ClientFilterByTurnID"`
3. **Integration Tests (artifact compatibility)**: `./scripts/process/build.sh --skip-frontend --skip-etc && ./scripts/process/integration_test.sh --categories common --specify "TestE2E_SystemArtifact_FilterBySession|TestSystemArtifact_ListAll_Client|TestSystemArtifact_SeventyUpdatePages"`
4. **E2E Tests (tests/ 配下追加必須)**: `tests/artifact_turn_scope_e2e_test.go` を新規追加し、同一 `session_id` の T1/T2 分離と `respond` 継続を自動検証する。

## Documentation

- `docs/ReferenceManual-WebAPIs.md`
  - `POST /api/v1/sessions/:id/messages` request に `correlation_id` を追加
  - `GET /api/v1/artifacts/system` query に `turn_id`（必要に応じて `correlation_id`）を追加
  - System Artifact item response に `turn_id` を追加
- `README.md`
  - 同一セッションで取得した `turn_id` を使って `SystemArtifacts().List` を絞る使用例を追加
- `client/v1` のコメント
  - `SystemArtifactFilter.TurnIDs` と `SystemArtifactItem.TurnID` の意味を GoDoc で明示

