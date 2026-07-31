# 000-AgentFileListAPI-Part2

> **Source Specification**: prompts/phases/000-foundation/branches/feat-file-list/ideas/000-AgentFileListAPI.md

## Goal Description

アーティファクト管理 Web API の Part 2 として、以下を実装する。

1. **User Artifact File Storage** — Tern 管理ストレージへのファイル書き込み/読み込み
2. **User Artifact Store** — SQLite への `user_artifacts` テーブル追加
3. **User Artifact API** — `PUT/GET/DELETE /api/v1/artifacts/user` 系エンドポイント
4. **MCP Server** — Coding Agent が `key` で User Artifact を参照できる MCP ツール提供

Part 1 (System Artifact) は前提。Part 3 (Go Client + README) は別ファイル参照。

## User Review Required

- `put_user_artifact` MCP ツール（O5 任意）はデフォルト有効とし、設定ファイルで無効化できるようにする予定。
- User Artifact の実ファイル配置先はデフォルト `{configDir}/user-artifacts/{uuid}` とし、`config.yaml` で上書きできる。

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R8: User Artifact CRUD | Proposed Changes > artifact/api/user.go |
| R9: key でアクセス | Proposed Changes > artifact/store/store.go (UserArtifact メソッド) |
| R10: MCP ツール経由アクセス | Proposed Changes > artifact/mcp/server.go |
| R11: source 区別 | Proposed Changes > artifact/store/models.go (UserArtifact 型) |
| 検証シナリオ 11-16: MCP アクセス | Proposed Changes > artifact/mcp/server.go |

## Proposed Changes

### [MODIFY] shared/libs/go/artifact/store/models.go
*   **Description**: `UserArtifact` 型・`UserArtifactFilter` を追加
*   **Technical Design**:

```go
// UserArtifact is a user-uploaded artifact stored in Tern-managed storage.
type UserArtifact struct {
    ID         string    `db:"id"`          // UUID
    Key        string    `db:"key"`         // user-defined logical path
    ActualPath string    `db:"actual_path"` // path in Tern-managed storage
    Filename   string    `db:"filename"`    // original filename
    Size       int64     `db:"size"`
    MIMEType   string    `db:"mime_type"`
    ContentSHA string    `db:"content_sha"`
    CreatedAt  time.Time `db:"created_at"`
    UpdatedAt  time.Time `db:"updated_at"`
}

// UserArtifactFilter holds query filters for ListUserArtifacts.
type UserArtifactFilter struct {
    Q       string // doublestar glob applied to Key
    Page    int
    PerPage int
    Sort    string // "key" | "created_at" | "updated_at" | "size"
    Order   string // "asc" | "desc"
}

// UserArtifactPage is the paginated result.
type UserArtifactPage struct {
    TotalCount int
    Page       int
    PerPage    int
    Items      []UserArtifact
}
```

### [MODIFY] shared/libs/go/artifact/store/store_test.go
*   **Description**: User Artifact の CRUD テストケースを追加
*   **Test Cases**:

```go
// TestSaveAndGetUserArtifact
tests := []struct {
    name      string
    artifact  UserArtifact
    wantFound bool
}{
    {name: "save and get by key", artifact: UserArtifact{Key: "datasets/a.csv", ...}, wantFound: true},
    {name: "get nonexistent key", wantFound: false},
}

// TestListUserArtifacts_GlobFilter
tests := []struct {
    name      string
    keys      []string
    q         string
    wantCount int
}{
    {name: "datasets/** returns 2", keys: []string{"datasets/a.csv", "datasets/b.csv", "configs/x.yaml"}, q: "datasets/**", wantCount: 2},
    {name: "no filter returns all", keys: []string{"a", "b", "c"}, q: "", wantCount: 3},
}

// TestDeleteUserArtifact
// → Delete after Save → Get returns not found
```

### [MODIFY] shared/libs/go/artifact/store/store.go
*   **Description**: `ArtifactStore` インターフェースに User Artifact メソッドを追加し、SQLite マイグレーションに `user_artifacts` テーブルを追加
*   **Technical Design**:

```go
// ArtifactStore (追加メソッド)
interface {
    // User artifacts
    SaveUserArtifact(ctx context.Context, a UserArtifact) error   // INSERT OR REPLACE
    GetUserArtifactByKey(ctx context.Context, key string) (*UserArtifact, error)
    ListUserArtifacts(ctx context.Context, f UserArtifactFilter) (*UserArtifactPage, error)
    DeleteUserArtifact(ctx context.Context, key string) error
}
```

*   **Migration 追加 DDL**:

```sql
CREATE TABLE IF NOT EXISTS user_artifacts (
    id           TEXT PRIMARY KEY,
    key          TEXT NOT NULL UNIQUE,
    actual_path  TEXT NOT NULL,
    filename     TEXT NOT NULL,
    size         INTEGER NOT NULL DEFAULT 0,
    mime_type    TEXT NOT NULL DEFAULT '',
    content_sha  TEXT NOT NULL DEFAULT '',
    created_at   DATETIME NOT NULL,
    updated_at   DATETIME NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_ua_key        ON user_artifacts(key);
CREATE INDEX IF NOT EXISTS idx_ua_created_at ON user_artifacts(created_at);
```

### [NEW] shared/libs/go/artifact/storage/storage_test.go
*   **Description**: UserArtifactStorage のテスト（先に作成）
*   **Test Cases**:

```go
// TestLocalStorage_WriteAndRead
// TestLocalStorage_Delete
// TestLocalStorage_SHACalculation: コンテンツ書き込み後 SHA256 が返る
// TestLocalStorage_SizeCalculation
```

### [NEW] shared/libs/go/artifact/storage/storage.go
*   **Description**: User Artifact の実ファイル管理
*   **Technical Design**:

```go
package storage

import (
    "crypto/sha256"
    "encoding/hex"
    "io"
    "mime"
    "net/http"
    "os"
    "path/filepath"

    "github.com/google/uuid"
)

// UserArtifactStorage manages the physical files for user artifacts.
type UserArtifactStorage interface {
    // Write stores the content and returns (actualPath, size, sha256, mimeType, error).
    Write(filename string, r io.Reader) (actualPath string, size int64, sha string, mimeType string, err error)
    // Read returns a ReadCloser for the file at actualPath.
    Read(actualPath string) (io.ReadCloser, error)
    // Delete removes the file at actualPath.
    Delete(actualPath string) error
}

// LocalStorage implements UserArtifactStorage using the local file system.
type LocalStorage struct {
    baseDir string // e.g. /var/tern/user-artifacts
}

func NewLocalStorage(baseDir string) (*LocalStorage, error) {
    if err := os.MkdirAll(baseDir, 0o750); err != nil {
        return nil, err
    }
    return &LocalStorage{baseDir: baseDir}, nil
}

func (s *LocalStorage) Write(filename string, r io.Reader) (string, int64, string, string, error) {
    id := uuid.New().String()
    dst := filepath.Join(s.baseDir, id)
    f, err := os.Create(dst)
    if err != nil { return "", 0, "", "", err }
    defer f.Close()

    h := sha256.New()
    // Sniff MIME type from the first 512 bytes.
    buf := make([]byte, 512)
    n, _ := r.Read(buf)
    buf = buf[:n]
    mimeType := http.DetectContentType(buf)

    mw := io.MultiWriter(f, h)
    if _, err := mw.Write(buf); err != nil { return "", 0, "", "", err }
    size, err := io.Copy(mw, r)
    if err != nil { return "", 0, "", "", err }
    size += int64(n)

    // Fallback MIME detection from filename extension.
    if extMime := mime.TypeByExtension(filepath.Ext(filename)); extMime != "" {
        mimeType = extMime
    }

    return dst, size, hex.EncodeToString(h.Sum(nil)), mimeType, nil
}
```

### [NEW] shared/libs/go/artifact/api/user_test.go
*   **Description**: User Artifact HTTP ハンドラのテスト（先に作成）
*   **Test Cases**:
    - `PUT /api/v1/artifacts/user/datasets/a.csv` multipart/form-data → 201 + JSON
    - `PUT /api/v1/artifacts/user/datasets/a.csv` 同 key で再 PUT → 200（上書き）
    - `GET /api/v1/artifacts/user` → 200 + items 一覧
    - `GET /api/v1/artifacts/user?q=datasets/**` → glob フィルタ
    - `GET /api/v1/artifacts/user/datasets/a.csv` → 200 + メタデータ JSON
    - `GET /api/v1/artifacts/user/datasets/a.csv/content` → 200 + バイナリ
    - `GET /api/v1/artifacts/user/nonexistent` → 404
    - `DELETE /api/v1/artifacts/user/datasets/a.csv` → 204
    - `GET` 後 `DELETE` 後 `GET` → 404
    - `POST /api/v1/artifacts/user/archive` with `{"keys":["datasets/a.csv"]}` → 200 + zip

### [NEW] shared/libs/go/artifact/api/user.go
*   **Description**: User Artifact の HTTP ハンドラ群
*   **Technical Design**:

```go
package api

// UserArtifactHandler handles /api/v1/artifacts/user routes.
type UserArtifactHandler struct {
    store   store.ArtifactStore
    storage storage.UserArtifactStorage
}

func NewUserArtifactHandler(s store.ArtifactStore, st storage.UserArtifactStorage) *UserArtifactHandler {
    return &UserArtifactHandler{store: s, storage: st}
}

func (h *UserArtifactHandler) RegisterRoutes(mux *http.ServeMux, prefix string) {
    mux.HandleFunc(prefix,     h.routeRoot)   // list
    mux.HandleFunc(prefix+"/", h.routeByKey)  // CRUD + archive
}
```

*   **Logic（各ルート）**:
    - `routeRoot`: `GET` → `handleList`
    - `routeByKey`:
        - `POST` かつパスが `…/archive` → `handleArchive`
        - `PUT` → `handlePut`
        - `GET` かつパスが `…/{key}/content` → `handleContent`
        - `GET` → `handleGet`
        - `DELETE` → `handleDelete`
    - `handlePut`:
        1. `key` を URL パスから抽出（`/api/v1/artifacts/user/{key}` の `{key}` 部分）
        2. `Content-Type` が `multipart/form-data` → `r.FormFile("file")` で取得、それ以外 → `r.Body` を直接使用
        3. `storage.Write(filename, reader)` を呼び出し `(actualPath, size, sha, mimeType)` を取得
        4. 既存レコードがあれば古い `actualPath` のファイルを `storage.Delete` で削除
        5. `store.SaveUserArtifact(UserArtifact{...})` で DB 保存
        6. 新規作成なら 201、上書きなら 200 を返す
    - `handleContent`: `store.GetUserArtifactByKey` → `storage.Read(actualPath)` → `io.Copy(w, rc)`
    - `handleArchive`: System と同様に ZIP ストリームを返す。User Artifact の場合は `store.ListUserArtifacts` + `storage.Read` で各ファイルを ZIP に追加

### [NEW] shared/libs/go/artifact/mcp/server_test.go
*   **Description**: MCP Server のテスト（先に作成）
*   **Test Cases**:

```go
// TestListUserArtifacts_MCPTool
tests := []struct {
    name       string
    artifacts  []store.UserArtifact  // 事前に store に保存
    params     map[string]string     // MCP ツール引数
    wantCount  int
    wantKeys   []string
}{
    {
        name: "list all",
        artifacts: []store.UserArtifact{
            {Key: "a.csv", Filename: "a.csv"},
            {Key: "b.csv", Filename: "b.csv"},
        },
        wantCount: 2,
    },
    {
        name: "glob filter",
        artifacts: []store.UserArtifact{
            {Key: "datasets/a.csv"}, {Key: "configs/b.yaml"},
        },
        params: map[string]string{"q": "datasets/**"},
        wantCount: 1,
        wantKeys: []string{"datasets/a.csv"},
    },
}

// TestGetUserArtifact_MCPTool
tests := []struct {
    name      string
    key       string
    content   string   // 実ファイルに書き込む内容
    encoding  string   // "text" or "base64"
    wantOK    bool
}{
    {name: "text content", key: "note.txt", content: "hello", encoding: "text", wantOK: true},
    {name: "base64 binary", key: "img.png", content: "PNG...", encoding: "base64", wantOK: true},
    {name: "nonexistent key", key: "missing.txt", wantOK: false},
}

// TestMCPServer_AuthIsolation
// セッション外からの呼び出しが拒否されること（認証トークン不一致 → エラー）
```

### [NEW] shared/libs/go/artifact/mcp/server.go
*   **Description**: `mark3labs/mcp-go` を使った MCP Server 実装
*   **Technical Design**:

```go
package mcp

import (
    "context"
    "encoding/base64"
    "io"

    mcpgo "github.com/mark3labs/mcp-go/mcp"
    "github.com/mark3labs/mcp-go/server"

    "github.com/axsh/arctic-tern/shared/libs/go/artifact/store"
    "github.com/axsh/arctic-tern/shared/libs/go/artifact/storage"
)

// ArtifactMCPServer exposes User Artifact operations to Coding Agents via MCP.
type ArtifactMCPServer struct {
    mcpServer *server.MCPServer
    store     store.ArtifactStore
    storage   storage.UserArtifactStorage
}

// New creates and registers all MCP tools.
func New(s store.ArtifactStore, st storage.UserArtifactStorage) *ArtifactMCPServer {
    ms := server.NewMCPServer("tern-artifact", "1.0.0",
        server.WithToolCapabilities(true),
    )
    a := &ArtifactMCPServer{mcpServer: ms, store: s, storage: st}
    a.registerTools()
    return a
}

func (a *ArtifactMCPServer) registerTools() {
    // All three tools are registered by default.
    // put_user_artifact can be disabled via config (putArtifactDisabled flag).

    // list_user_artifacts
    a.mcpServer.AddTool(mcpgo.NewTool("list_user_artifacts",
        mcpgo.WithDescription("List user-uploaded artifacts. Supports glob filter via 'q' parameter."),
        mcpgo.WithString("q", mcpgo.Description("Glob pattern to filter artifact keys, e.g. 'datasets/**'")),
        mcpgo.WithNumber("page",     mcpgo.Description("Page number, 1-indexed")),
        mcpgo.WithNumber("per_page", mcpgo.Description("Items per page (max 100, default 30)")),
    ), a.handleListUserArtifacts)

    // get_user_artifact
    a.mcpServer.AddTool(mcpgo.NewTool("get_user_artifact",
        mcpgo.WithDescription("Get the content of a user artifact by its logical key."),
        mcpgo.WithString("key",      mcpgo.Required(), mcpgo.Description("Logical key of the artifact")),
        mcpgo.WithString("encoding", mcpgo.Description("'text' (default) or 'base64' for binary files")),
    ), a.handleGetUserArtifact)
}

// handleListUserArtifacts implements the list_user_artifacts MCP tool.
func (a *ArtifactMCPServer) handleListUserArtifacts(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
    q, _ := req.Params.Arguments["q"].(string)
    page, _ := req.Params.Arguments["page"].(float64)
    perPage, _ := req.Params.Arguments["per_page"].(float64)

    result, err := a.store.ListUserArtifacts(ctx, store.UserArtifactFilter{
        Q:       q,
        Page:    int(page),
        PerPage: int(perPage),
    })
    if err != nil {
        return mcpgo.NewToolResultError("failed to list artifacts: " + err.Error()), nil
    }

    items := make([]map[string]any, len(result.Items))
    for i, item := range result.Items {
        items[i] = map[string]any{
            "key":      item.Key,
            "filename": item.Filename,
            "size":     item.Size,
            "mime_type":item.MIMEType,
        }
    }
    out, _ := json.Marshal(map[string]any{"total_count": result.TotalCount, "items": items})
    return mcpgo.NewToolResultText(string(out)), nil
}

// handleGetUserArtifact implements the get_user_artifact MCP tool.
func (a *ArtifactMCPServer) handleGetUserArtifact(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
    key, _ := req.Params.Arguments["key"].(string)
    encoding, _ := req.Params.Arguments["encoding"].(string)
    if encoding == "" { encoding = "text" }

    artifact, err := a.store.GetUserArtifactByKey(ctx, key)
    if err != nil || artifact == nil {
        return mcpgo.NewToolResultError("artifact not found: " + key), nil
    }

    rc, err := a.storage.Read(artifact.ActualPath)
    if err != nil {
        return mcpgo.NewToolResultError("failed to read artifact: " + err.Error()), nil
    }
    defer rc.Close()

    data, err := io.ReadAll(rc)
    if err != nil {
        return mcpgo.NewToolResultError("failed to read content: " + err.Error()), nil
    }

    if encoding == "base64" {
        return mcpgo.NewToolResultText(base64.StdEncoding.EncodeToString(data)), nil
    }
    return mcpgo.NewToolResultText(string(data)), nil
}

// ServeSSE starts the MCP server on an SSE transport at the given address.
func (a *ArtifactMCPServer) ServeSSE(addr string) error {
    s := server.NewSSEServer(a.mcpServer, server.WithBaseURL("http://"+addr))
    return s.Start(addr)
}

// Handler returns an http.Handler for embedding in an existing HTTP server.
func (a *ArtifactMCPServer) Handler() http.Handler {
    return server.NewSSEServer(a.mcpServer, server.WithBaseURL("")).Handler()
}
```

### [MODIFY] server/server.go
*   **Description**: User Artifact Storage の初期化、User Artifact API ルート登録、MCP Server の起動
*   **Technical Design**:
    - `LocalStorage` を `{configDir}/user-artifacts` で初期化
    - `UserArtifactHandler.RegisterRoutes(mux, "/api/v1/artifacts/user")` を呼び出す
    - `ArtifactMCPServer` を起動し、`/mcp/artifacts` パスに Handler をマウントする

---

## Step-by-Step Implementation Guide

1. **[UserArtifact モデル追加]**: `shared/libs/go/artifact/store/models.go` に `UserArtifact`・`UserArtifactFilter`・`UserArtifactPage` を追記する。
2. **[Store テスト追加]**: `shared/libs/go/artifact/store/store_test.go` に User Artifact テストケースを追記する（Red）。
3. **[Store 実装追加]**: `store.go` に `user_artifacts` テーブル DDL と User Artifact CRUD メソッドを追記する（Green）。
4. **[Storage テスト作成]**: `shared/libs/go/artifact/storage/storage_test.go` を新規作成する（Red）。
5. **[Storage 実装]**: `shared/libs/go/artifact/storage/storage.go` を新規作成する（Green）。
6. **[User API テスト作成]**: `shared/libs/go/artifact/api/user_test.go` を新規作成する（Red）。
7. **[User API 実装]**: `shared/libs/go/artifact/api/user.go` を新規作成する（Green）。
8. **[MCP Server テスト作成]**: `shared/libs/go/artifact/mcp/server_test.go` を新規作成する（Red）。
9. **[MCP Server 実装]**: `shared/libs/go/artifact/mcp/server.go` を新規作成する（Green）。
10. **[server 統合]**: `server/server.go` に User Artifact Storage・User API・MCP Server を組み込む。
11. **[ビルド確認]**: `./scripts/process/build.sh --skip-frontend --skip-etc`

---

## Verification Plan

### Automated Verification

1. **Build & Unit Tests**:
   ```
   ./scripts/process/build.sh --skip-frontend --skip-etc
   ```
2. **Integration Tests（User Artifact API）**:
   ```
   ./scripts/process/build.sh --skip-frontend --skip-etc && ./scripts/process/integration_test.sh --categories taskengine --specify "user-artifact"
   ```
3. **Integration Tests（MCP Server）**:
   ```
   ./scripts/process/build.sh --skip-frontend --skip-etc && ./scripts/process/integration_test.sh --categories taskengine --specify "mcp-user-artifact"
   ```

### Integration Test Files

`tests/artifact_user_integration_test.go`:
```go
// TestUserArtifactEndToEnd:
// 1. PUT /api/v1/artifacts/user/datasets/a.csv
// 2. GET /api/v1/artifacts/user → a.csv が含まれる
// 3. GET /api/v1/artifacts/user/datasets/a.csv/content → バイナリ一致
// 4. DELETE /api/v1/artifacts/user/datasets/a.csv → 204
// 5. GET /api/v1/artifacts/user/datasets/a.csv → 404
```

`tests/artifact_mcp_integration_test.go`:
```go
// TestMCPUserArtifactEndToEnd:
// 1. Web API で User Artifact をアップロード
// 2. MCP Client で list_user_artifacts を呼び出す → 一覧に含まれる
// 3. MCP Client で get_user_artifact(key) を呼び出す → コンテンツ一致
// 4. 存在しない key で get_user_artifact → エラーメッセージが返る
// 5. glob フィルタ付き list_user_artifacts → 対象 key のみ返る
```

## Documentation

- `README.md` への記載は Part 3 で実施する。
