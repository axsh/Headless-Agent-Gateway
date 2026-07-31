# 000-AgentFileListAPI-Part1

> **Source Specification**: prompts/phases/000-foundation/branches/feat-file-list/ideas/000-AgentFileListAPI.md

## Goal Description

アーティファクト管理 Web API の Part 1 として、以下を実装する。

1. **Artifact Store** — SQLite を使った永続化層（`system_artifact_events` / `sessions` テーブル）
2. **Tool Call Analyzer** — `TaskLog` の `StreamEvent` から System Artifact イベントを抽出
3. **System Artifact API** — `GET /api/v1/artifacts/system` 系エンドポイントおよび agentservice への統合

Part 2 (User Artifact + MCP) および Part 3 (Go Client + README) は別ファイル参照。

## User Review Required

- SQLite の WAL モード採用（並列読み取り性能向上）は採用したが、他の DB を考慮する場合は interface 設計を変更する必要があります。
- `agentservice.Server` に `ArtifactStore` を注入する方法（`WithArtifactStore` Option）を採用予定。既存 Option パターンに倣います。

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: Tool call から create/update/delete イベントを記録 | Proposed Changes > artifact/analyzer |
| R2: セッション・Agent フィルタ付き System Artifact 一覧 | Proposed Changes > artifact/api/system.go |
| R3: セッション横断検索 | Proposed Changes > artifact/api/system.go (query params) |
| R4: glob フィルタリング | Proposed Changes > artifact/store/store.go (doublestar) |
| R5: ページネーション | Proposed Changes > artifact/store/store.go + api/system.go |
| R6: 単一ファイルダウンロード | Proposed Changes > artifact/api/system.go (content endpoint) |
| R7: 複数ファイル ZIP ダウンロード | Proposed Changes > artifact/api/system.go (archive endpoint) |
| R11: source 区別 (system/user) | Proposed Changes > artifact/store/models.go |
| R12: Cursor / Claude Code 対応 | Proposed Changes > artifact/analyzer/analyzer.go |

## Proposed Changes

### [NEW] shared/libs/go/artifact/store/models.go
*   **Description**: DB モデル型定義
*   **Technical Design**:

```go
package store

import "time"

// Session represents a Coding Agent session record.
type Session struct {
    ID        string    `db:"id"`
    AgentID   string    `db:"agent_id"`
    AgentName string    `db:"agent_name"`
    StartedAt time.Time `db:"started_at"`
    EndedAt   *time.Time `db:"ended_at"`
}

// SystemArtifactEvent represents one file operation event by a Coding Agent.
type SystemArtifactEvent struct {
    ID          int64     `db:"id"`
    SessionID   string    `db:"session_id"`
    AgentID     string    `db:"agent_id"`
    Key         string    `db:"key"`         // project-root-relative path
    ActualPath  string    `db:"actual_path"` // absolute FS path
    Operation   string    `db:"operation"`   // "create" | "update" | "delete"
    OccurredAt  time.Time `db:"occurred_at"`
    ToolName    string    `db:"tool_name"`
    ContentSHA  string    `db:"content_sha"` // SHA256 hex, may be empty
}

// Operation constants.
const (
    OperationCreate = "create"
    OperationUpdate = "update"
    OperationDelete = "delete"
)

// SystemArtifactFilter holds query filters for ListSystemArtifacts.
type SystemArtifactFilter struct {
    Q              string     // doublestar glob applied to Key
    AgentIDs       []string
    SessionIDs     []string
    Operation      string
    Since          *time.Time
    Until          *time.Time
    IncludeDeleted bool       // if false, exclude keys whose latest op is "delete"
    Page           int        // 1-indexed
    PerPage        int        // default 30, max 100
    Sort           string     // "key" | "occurred_at" | "operation"
    Order          string     // "asc" | "desc"
}

// SystemArtifactPage is the paginated result.
type SystemArtifactPage struct {
    TotalCount int
    Page       int
    PerPage    int
    Items      []SystemArtifactEvent
}
```

### [NEW] shared/libs/go/artifact/store/store_test.go
*   **Description**: ArtifactStore のテーブル駆動テスト（先に作成）
*   **Test Cases**:

```go
// TestSaveAndListSystemArtifacts
tests := []struct {
    name           string
    events         []SystemArtifactEvent
    filter         SystemArtifactFilter
    wantCount      int
    wantKeys       []string
}{
    {
        name: "no filter returns all",
        events: []SystemArtifactEvent{
            {SessionID: "s1", AgentID: "cursor", Key: "a/b.go", Operation: "create"},
            {SessionID: "s1", AgentID: "cursor", Key: "a/c.go", Operation: "create"},
        },
        filter:    SystemArtifactFilter{PerPage: 10},
        wantCount: 2,
    },
    {
        name: "glob filter **/*.go",
        events: []SystemArtifactEvent{
            {Key: "a/b.go", Operation: "create"},
            {Key: "a/b.txt", Operation: "create"},
        },
        filter:    SystemArtifactFilter{Q: "**/*.go", PerPage: 10},
        wantCount: 1,
        wantKeys:  []string{"a/b.go"},
    },
    {
        name: "session_id filter",
        // ...
    },
    {
        name: "include_deleted=false excludes deleted key",
        events: []SystemArtifactEvent{
            {Key: "x.go", Operation: "create"},
            {Key: "x.go", Operation: "delete"},
        },
        filter:    SystemArtifactFilter{IncludeDeleted: false, PerPage: 10},
        wantCount: 0,
    },
    {
        name: "pagination page=2",
        // 5 events, perPage=2 → page=2 returns items 3-4
    },
}
```

### [NEW] shared/libs/go/artifact/store/store.go
*   **Description**: `ArtifactStore` インターフェースと SQLite 実装
*   **Technical Design**:

```go
package store

import (
    "context"
    "database/sql"
    _ "modernc.org/sqlite"
    "github.com/bmatcuk/doublestar/v4"
)

// ArtifactStore is the persistence interface for artifact events.
type ArtifactStore interface {
    // Session operations
    UpsertSession(ctx context.Context, s Session) error
    CloseSession(ctx context.Context, sessionID string) error

    // System artifacts
    SaveSystemArtifactEvent(ctx context.Context, e SystemArtifactEvent) error
    ListSystemArtifacts(ctx context.Context, f SystemArtifactFilter) (*SystemArtifactPage, error)
    GetSystemArtifactByKey(ctx context.Context, key string) ([]SystemArtifactEvent, error)

    // Close releases DB resources.
    Close() error
}

// SQLiteStore implements ArtifactStore using SQLite.
type SQLiteStore struct {
    db *sql.DB
}

// NewSQLiteStore opens (or creates) the SQLite DB at dbPath and runs migrations.
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) { ... }

func (s *SQLiteStore) migrate() error { ... }
```

*   **Logic**:
    - `migrate()` は以下の DDL を `database/sql` で直接実行する（migration ファイルは不使用、インラインで管理）:

```sql
PRAGMA journal_mode=WAL;

CREATE TABLE IF NOT EXISTS sessions (
    id          TEXT PRIMARY KEY,
    agent_id    TEXT NOT NULL,
    agent_name  TEXT NOT NULL DEFAULT '',
    started_at  DATETIME NOT NULL,
    ended_at    DATETIME
);

CREATE TABLE IF NOT EXISTS system_artifact_events (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id   TEXT NOT NULL REFERENCES sessions(id),
    agent_id     TEXT NOT NULL,
    key          TEXT NOT NULL,
    actual_path  TEXT NOT NULL DEFAULT '',
    operation    TEXT NOT NULL,
    occurred_at  DATETIME NOT NULL,
    tool_name    TEXT NOT NULL DEFAULT '',
    content_sha  TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_sae_session   ON system_artifact_events(session_id);
CREATE INDEX IF NOT EXISTS idx_sae_agent     ON system_artifact_events(agent_id);
CREATE INDEX IF NOT EXISTS idx_sae_key       ON system_artifact_events(key);
CREATE INDEX IF NOT EXISTS idx_sae_occurred  ON system_artifact_events(occurred_at);
```

    - `ListSystemArtifacts` のフィルタリング:
        1. SQL で `session_id`、`agent_id`、`operation`、`since`/`until` を WHERE 句で絞り込む
        2. `Q`（glob）は SQL の LIKE では不十分なため、Go 側で `doublestar.Match(q, key)` を使い in-memory フィルタリングする（全件 SELECT 後フィルタ）
        3. `IncludeDeleted=false` の場合: サブクエリ `SELECT key FROM system_artifact_events GROUP BY key HAVING last(operation) != 'delete'` で生存キーを取得し、`key IN (...)` で絞る
        4. ページネーション: `LIMIT` + `OFFSET` を使用。`TotalCount` は glob フィルタ後の件数
        5. `Sort`: `ORDER BY key|occurred_at|operation {ASC|DESC}`

### [NEW] shared/libs/go/artifact/analyzer/analyzer_test.go
*   **Description**: ToolCallAnalyzer のテーブル駆動テスト（先に作成）
*   **Test Cases**:

```go
// TestAnalyzeStreamEvent
tests := []struct {
    name      string
    event     codingagent.StreamEvent
    wantNil   bool
    wantKey   string
    wantOp    string
}{
    {
        name: "Write tool → create",
        event: codingagent.StreamEvent{
            Type:     codingagent.EventToolUse,
            ToolName: "Write",
            ToolInput: map[string]interface{}{
                "path":     "/project/internal/user.go",
                "contents": "package ...",
            },
        },
        wantKey: "internal/user.go",
        wantOp:  "create",
    },
    {
        name: "StrReplace tool → update",
        event: codingagent.StreamEvent{
            Type: codingagent.EventToolUse, ToolName: "StrReplace",
            ToolInput: map[string]interface{}{"path": "/project/a.go"},
        },
        wantKey: "a.go", wantOp: "update",
    },
    {
        name: "Delete tool → delete",
        event: codingagent.StreamEvent{
            Type: codingagent.EventToolUse, ToolName: "Delete",
            ToolInput: map[string]interface{}{"path": "/project/a.go"},
        },
        wantKey: "a.go", wantOp: "delete",
    },
    {
        name:    "text event → nil (ignored)",
        event:   codingagent.StreamEvent{Type: codingagent.EventText, Content: "hello"},
        wantNil: true,
    },
    {
        name: "EditFile tool (Claude Code) → update",
        event: codingagent.StreamEvent{
            Type: codingagent.EventToolUse, ToolName: "Edit",
            ToolInput: map[string]interface{}{"file_path": "/project/b.go"},
        },
        wantKey: "b.go", wantOp: "update",
    },
}
```

### [NEW] shared/libs/go/artifact/analyzer/analyzer.go
*   **Description**: `StreamEvent` からファイル操作を抽出する `ToolCallAnalyzer`
*   **Technical Design**:

```go
package analyzer

import (
    "time"
    "github.com/axsh/arctic-tern/shared/libs/go/artifact/store"
    "github.com/axsh/arctic-tern/shared/libs/go/codingagent"
    "github.com/axsh/arctic-tern/shared/libs/go/tasklog"
)

// ToolMapping maps a tool name to its operation and the ToolInput field containing the path.
type ToolMapping struct {
    Operation string // "create" | "update" | "delete"
    PathField string // key in ToolInput that holds the file path, e.g. "path" or "file_path"
}

// defaultToolMappings covers Cursor Agent and Claude Code tools.
// Key: tool name (case-sensitive).
var defaultToolMappings = map[string]ToolMapping{
    // Cursor Agent
    "Write":     {Operation: store.OperationCreate, PathField: "path"},
    "StrReplace":{Operation: store.OperationUpdate, PathField: "path"},
    "Delete":    {Operation: store.OperationDelete, PathField: "path"},
    // Claude Code
    "Write":     {Operation: store.OperationCreate, PathField: "file_path"},
    "Edit":      {Operation: store.OperationUpdate, PathField: "file_path"},
    "MultiEdit": {Operation: store.OperationUpdate, PathField: "file_path"},
    // NOTE: "Write" conflicts; Cursor uses "path", Claude Code uses "file_path".
    // Resolved by trying "path" first, then "file_path".
}

// ToolCallAnalyzer extracts SystemArtifactEvent from TaskLog entries.
type ToolCallAnalyzer struct {
    store      store.ArtifactStore
    projectRoot string
    toolMaps   map[string][]ToolMapping // key → list (one per path-field variant)
}

// New creates a ToolCallAnalyzer and attaches it to the given TaskLog.
func New(tl *tasklog.TaskLog, s store.ArtifactStore, projectRoot string) *ToolCallAnalyzer {
    a := &ToolCallAnalyzer{store: s, projectRoot: projectRoot}
    a.toolMaps = buildDefaultToolMaps()
    tl.SetOnEntry(a.onEntry)
    return a
}

// onEntry is called for each new TaskLog entry.
func (a *ToolCallAnalyzer) onEntry(e tasklog.Entry) {
    agentLog, ok := e.(*tasklog.AgentLogEntry)
    if !ok || agentLog.Phase != "send" { return }

    var ev codingagent.StreamEvent
    if err := json.Unmarshal([]byte(agentLog.Body), &ev); err != nil { return }

    event := a.analyzeEvent(ev, agentLog.AgentID, agentLog.ID)
    if event == nil { return }

    ctx := context.Background()
    _ = a.store.SaveSystemArtifactEvent(ctx, *event)
}

// analyzeEvent returns a SystemArtifactEvent if ev is a recognized file-write tool_use, else nil.
func (a *ToolCallAnalyzer) analyzeEvent(ev codingagent.StreamEvent, sessionID, agentID string) *store.SystemArtifactEvent {
    if ev.Type != codingagent.EventToolUse { return nil }

    mappings, ok := a.toolMaps[ev.ToolName]
    if !ok { return nil }

    var filePath string
    var op string
    for _, m := range mappings {
        if p, ok := ev.ToolInput[m.PathField].(string); ok && p != "" {
            filePath = p
            op = m.Operation
            break
        }
    }
    if filePath == "" { return nil }

    key := a.toRelativePath(filePath)
    return &store.SystemArtifactEvent{
        SessionID:  sessionID,
        AgentID:    agentID,
        Key:        key,
        ActualPath: filePath,
        Operation:  op,
        OccurredAt: time.Now(),
        ToolName:   ev.ToolName,
    }
}

// toRelativePath converts an absolute path to a project-root-relative path.
func (a *ToolCallAnalyzer) toRelativePath(absPath string) string {
    rel, err := filepath.Rel(a.projectRoot, absPath)
    if err != nil { return absPath }
    return filepath.ToSlash(rel)
}
```

*   **Logic（ToolMapping の Write 衝突解決）**:
    - `Write` には 2 種類の path フィールドがある。`ToolMapping` を `map[string][]ToolMapping`（スライス）にして両方を登録し、`for _, m := range mappings` で `path` → `file_path` の順にフォールバック。

### [NEW] shared/libs/go/artifact/api/system_test.go
*   **Description**: System Artifact HTTP ハンドラのユニットテスト（先に作成）
*   **Test Cases**:
    - `GET /api/v1/artifacts/system` → 200 + JSON `{"total_count":N, "items":[...]}`
    - `GET /api/v1/artifacts/system?q=**/*.go` → glob フィルタが機能する
    - `GET /api/v1/artifacts/system?session_id=X&session_id=Y` → 複数セッション
    - `GET /api/v1/artifacts/system?page=2&per_page=2` → ページネーション
    - `GET /api/v1/artifacts/system/{key}` → 200 + operations 配列
    - `GET /api/v1/artifacts/system/{key}/content` → 200 + file bytes（テスト用一時ファイルを作成）
    - `GET /api/v1/artifacts/system/{key}/content` で存在しないファイル → 404
    - `POST /api/v1/artifacts/system/archive` with `{"q":"**/*.go"}` → 200 + zip バイナリ
    - `POST /api/v1/artifacts/system/archive` with `{"keys":["a.go"]}` → 200 + zip
    - METHOD 以外 → 405

### [NEW] shared/libs/go/artifact/api/system.go
*   **Description**: System Artifact の HTTP ハンドラ群
*   **Technical Design**:

```go
package api

import (
    "archive/zip"
    "encoding/json"
    "io"
    "net/http"
    "os"
    "strings"

    "github.com/axsh/arctic-tern/shared/libs/go/artifact/store"
)

// SystemArtifactHandler handles /api/v1/artifacts/system routes.
type SystemArtifactHandler struct {
    store store.ArtifactStore
}

func NewSystemArtifactHandler(s store.ArtifactStore) *SystemArtifactHandler {
    return &SystemArtifactHandler{store: s}
}

// RegisterRoutes registers routes on mux.
// Caller passes the prefix "/api/v1/artifacts/system".
func (h *SystemArtifactHandler) RegisterRoutes(mux *http.ServeMux, prefix string) {
    mux.HandleFunc(prefix,          h.routeRoot)     // list + archive
    mux.HandleFunc(prefix+"/",      h.routeByKey)    // /{key} + /{key}/content
}
```

*   **Logic（各ルート）**:
    - `routeRoot`: `POST` かつパスが `…/archive` → `handleArchive`、`GET` → `handleList`
    - `routeByKey`:
        - パスが `…/{key}/content` → `handleContent`
        - それ以外 → `handleGetByKey`
    - `handleList`: クエリパラメータを `store.SystemArtifactFilter` にマッピングして `ListSystemArtifacts` を呼び出し JSON 返却
    - `handleGetByKey`: `GetSystemArtifactByKey` でイベント履歴を取得し JSON 返却
    - `handleContent`: `GetSystemArtifactByKey` で最新 `ActualPath` を取得し `os.Open` → `io.Copy(w, f)`。`Content-Disposition: attachment; filename="{basename}"` を付与。ファイル不在 → 404
    - `handleArchive`:
        1. リクエスト JSON をパースし `keys` + `q` を取得
        2. `q` が指定された場合は `ListSystemArtifacts(filter{Q:q})` で key 一覧を取得
        3. `archive/zip` で ZIP を生成し `w` に直接ストリーム書き込み
        4. `Content-Type: application/zip`、`Content-Disposition: attachment; filename="artifacts.zip"` を設定

### [MODIFY] shared/libs/go/agentservice/service.go
*   **Description**: `ArtifactStore` と `ToolCallAnalyzer` を `agentservice.Server` に統合
*   **Technical Design**:
    - `agentservice.Server` に `artifactStore store.ArtifactStore` フィールドを追加
    - `WithArtifactStore(s store.ArtifactStore) Option` を追加
    - `Launch()` 内でストアが設定されている場合、`analyzer.New(s.taskLog, s.artifactStore, s.workDir)` を呼び出す
    - セッション作成時に `store.UpsertSession(Session{ID: sessionID, AgentID: agentType, StartedAt: time.Now()})` を呼び出す
    - セッション終了時（Terminate）に `store.CloseSession(sessionID)` を呼び出す

### [MODIFY] server/server.go
*   **Description**: Artifact Store を初期化して agentservice に渡し、System Artifact API ルートを登録する
*   **Technical Design**:
    - `Server` 構造体に `artifactStore *store.SQLiteStore` を追加
    - `New()` 内で `store.NewSQLiteStore(filepath.Join(configDir, "artifacts.db"))` を呼び出す
    - `agentservice.WithArtifactStore(s.artifactStore)` を渡す
    - `agentservice.Server` の mux に `SystemArtifactHandler.RegisterRoutes` を呼び出す（`agentservice.Server` に `RegisterExtraRoutes(fn func(*http.ServeMux))` メソッドを追加する）

### [MODIFY] go.mod（ルート）
*   新規依存を追加:
    - `modernc.org/sqlite` — CGO フリー SQLite ドライバ
    - `github.com/bmatcuk/doublestar/v4` — glob フィルタ

---

## Step-by-Step Implementation Guide

1. **[DB モデル定義]**: `shared/libs/go/artifact/store/models.go` を新規作成する。型定義・定数をすべて記述。
2. **[Store テスト作成]**: `shared/libs/go/artifact/store/store_test.go` を新規作成する。上記 Test Cases を含むテーブル駆動テスト。コンパイルエラーが出ることを確認（Red）。
3. **[Store 実装]**: `shared/libs/go/artifact/store/store.go` を新規作成する。SQLite マイグレーション、`SaveSystemArtifactEvent`、`ListSystemArtifacts`、`GetSystemArtifactByKey` を実装。テストが通ること（Green）を確認。
4. **[依存追加]**: `go get modernc.org/sqlite github.com/bmatcuk/doublestar/v4` を実行して `go.mod`/`go.sum` を更新。
5. **[Analyzer テスト作成]**: `shared/libs/go/artifact/analyzer/analyzer_test.go` を新規作成する。上記 Test Cases。
6. **[Analyzer 実装]**: `shared/libs/go/artifact/analyzer/analyzer.go` を新規作成する。テストが通ること（Green）を確認。
7. **[System API テスト作成]**: `shared/libs/go/artifact/api/system_test.go` を新規作成する。`httptest.NewRecorder` を使ったハンドラテスト。
8. **[System API 実装]**: `shared/libs/go/artifact/api/system.go` を新規作成する。テストが通ること（Green）を確認。
9. **[agentservice 統合]**: `shared/libs/go/agentservice/service.go` に `WithArtifactStore` Option と `RegisterExtraRoutes` を追加。セッション作成/終了時に store を呼び出す。
10. **[server 統合]**: `server/server.go` で SQLiteStore を初期化し agentservice に渡す。System Artifact API ルートを登録する。
11. **[ビルド確認]**: `./scripts/process/build.sh --skip-frontend --skip-etc` を実行して全体がコンパイルできることを確認。

---

## Verification Plan

### Automated Verification

1. **Build & Unit Tests**:
   ```
   ./scripts/process/build.sh --skip-frontend --skip-etc
   ```
2. **Integration Tests（System Artifact API）**:
   ```
   ./scripts/process/build.sh --skip-frontend --skip-etc && ./scripts/process/integration_test.sh --categories taskengine --specify "system-artifact"
   ```

### Integration Test File

`tests/artifact_system_integration_test.go` を作成する（Step 10 と並行）:

```go
// TestSystemArtifactEndToEnd:
// 1. tern サーバーを起動（in-process）
// 2. Claude Code セッションをモックして Write StreamEvent を注入
// 3. GET /api/v1/artifacts/system?session_id=X が該当ファイルを返すことを確認
// 4. GET /api/v1/artifacts/system/{key}/content でファイルバイナリが取得できることを確認
// 5. POST /api/v1/artifacts/system/archive?q=**/*.go で ZIP が返ることを確認
```

## Documentation

- `README.md` への記載は Part 3 で実施する。
- `server/` パッケージの godoc を更新（`artifactStore` フィールドへの言及）。
