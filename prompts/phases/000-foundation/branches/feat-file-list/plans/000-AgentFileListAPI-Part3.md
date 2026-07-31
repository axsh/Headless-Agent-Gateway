# 000-AgentFileListAPI-Part3

> **Source Specification**: prompts/phases/000-foundation/branches/feat-file-list/ideas/000-AgentFileListAPI.md

## Goal Description

アーティファクト管理 Web API の Part 3 として、以下を実装する。

1. **Go クライアントライブラリ** — `client/v1/artifacts.go`（`SystemArtifactClient` / `UserArtifactClient`）
2. **README.md 更新** — Artifact API Examples セクションの追加
3. **統合テスト完成** — Part 1/2 のテストと合わせた全体 E2E シナリオ

Part 1/2 が完了していることを前提とする。

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| Go クライアントライブラリ設計（仕様書）| Proposed Changes > client/v1/artifacts.go |
| README サンプル 1-6（仕様書）| Proposed Changes > README.md |
| 検証シナリオ 1-16（仕様書）| Proposed Changes > tests/ |

## Proposed Changes

### [NEW] client/v1/artifacts_test.go
*   **Description**: Go クライアントのテスト（先に作成）。`httptest.NewServer` でモック API サーバーを立て、クライアントの HTTP 呼び出しを検証する。
*   **Test Cases**:

```go
// TestSystemArtifactClient_List
tests := []struct {
    name        string
    opts        ArtifactListOptions
    serverResp  string   // JSON
    wantLen     int
}{
    {
        name: "returns items",
        serverResp: `{"total_count":2,"page":1,"per_page":30,"items":[
            {"key":"a.go","operation":"create","agent_id":"cursor"},
            {"key":"b.go","operation":"update","agent_id":"cursor"}
        ]}`,
        wantLen: 2,
    },
    {
        name: "glob filter passed as query param",
        opts: ArtifactListOptions{Q: "**/*.go"},
        // verify URL contains q=%2A%2A%2F*.go
    },
    {
        name: "session_id filter passed (multiple)",
        opts: ArtifactListOptions{SessionID: []string{"s1", "s2"}},
        // verify URL contains session_id=s1&session_id=s2
    },
}

// TestSystemArtifactClient_Download
// TestSystemArtifactClient_ArchiveTo
// TestUserArtifactClient_PutFile
// TestUserArtifactClient_List
// TestUserArtifactClient_DownloadTo
// TestUserArtifactClient_Delete
// TestUserArtifactClient_ArchiveTo
```

### [NEW] client/v1/artifacts.go
*   **Description**: `SystemArtifactClient` / `UserArtifactClient` の実装
*   **Technical Design**（仕様書 "Go クライアントライブラリ設計" より全量継承）:

```go
package v1

import (
    "archive/zip"
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "io"
    "mime"
    "mime/multipart"
    "net/http"
    "net/url"
    "os"
    "path/filepath"
    "strconv"
    "strings"
    "time"
)

// ---- Shared types ----

// ArtifactListOptions holds filters and pagination for artifact list requests.
type ArtifactListOptions struct {
    Q              string
    AgentID        []string
    SessionID      []string
    Operation      string
    Since          time.Time
    Until          time.Time
    IncludeDeleted bool
    Page           int
    PerPage        int
    Sort           string
    Order          string
}

// SystemArtifactItem is one entry in a system artifact list response.
type SystemArtifactItem struct {
    Key        string    `json:"key"`
    Operation  string    `json:"operation"`
    AgentID    string    `json:"agent_id"`
    SessionID  string    `json:"session_id"`
    OccurredAt time.Time `json:"occurred_at"`
    ToolName   string    `json:"tool_name"`
    SHA        string    `json:"sha"`
    Size       int64     `json:"size"`
}

// SystemArtifactList is the paginated list response for system artifacts.
type SystemArtifactList struct {
    TotalCount int                  `json:"total_count"`
    Page       int                  `json:"page"`
    PerPage    int                  `json:"per_page"`
    Items      []SystemArtifactItem `json:"items"`
}

// UserArtifactItem is one entry in a user artifact list response.
type UserArtifactItem struct {
    Key       string    `json:"key"`
    Filename  string    `json:"filename"`
    Size      int64     `json:"size"`
    MIMEType  string    `json:"mime_type"`
    SHA       string    `json:"sha"`
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
}

// UserArtifactList is the paginated list response for user artifacts.
type UserArtifactList struct {
    TotalCount int                `json:"total_count"`
    Page       int                `json:"page"`
    PerPage    int                `json:"per_page"`
    Items      []UserArtifactItem `json:"items"`
}

// ArchiveRequest specifies which files to include in a ZIP archive.
type ArchiveRequest struct {
    Keys      []string `json:"keys,omitempty"`
    Q         string   `json:"q,omitempty"`
    SessionID []string `json:"session_id,omitempty"`
    Format    string   `json:"format,omitempty"`
}

// ---- SystemArtifactClient ----

// SystemArtifactClient operates on /api/v1/artifacts/system endpoints.
type SystemArtifactClient struct {
    c *Client
}

// SystemArtifacts returns a SystemArtifactClient.
func (c *Client) SystemArtifacts() *SystemArtifactClient {
    return &SystemArtifactClient{c: c}
}

// List retrieves a paginated list of system artifacts.
func (s *SystemArtifactClient) List(ctx context.Context, opts ArtifactListOptions) (*SystemArtifactList, error) {
    u := s.c.baseURL + "/api/v1/artifacts/system"
    u += buildSystemQuery(opts)
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
    if err != nil { return nil, fmt.Errorf("list system artifacts: %w", err) }
    resp, err := s.c.httpClient.Do(req)
    if err != nil { return nil, fmt.Errorf("list system artifacts: %w", err) }
    defer resp.Body.Close()
    if resp.StatusCode != http.StatusOK {
        b, _ := io.ReadAll(resp.Body)
        return nil, fmt.Errorf("list system artifacts (HTTP %d): %s", resp.StatusCode, b)
    }
    var result SystemArtifactList
    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        return nil, fmt.Errorf("decode system artifact list: %w", err)
    }
    return &result, nil
}

// Get retrieves the metadata and operation history for a single system artifact key.
func (s *SystemArtifactClient) Get(ctx context.Context, key string) (*SystemArtifactItem, error) { ... }

// Download returns the file content as an io.ReadCloser. Caller must Close().
func (s *SystemArtifactClient) Download(ctx context.Context, key string) (io.ReadCloser, error) {
    u := s.c.baseURL + "/api/v1/artifacts/system/" + url.PathEscape(key) + "/content"
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
    if err != nil { return nil, fmt.Errorf("download system artifact: %w", err) }
    resp, err := s.c.httpClient.Do(req)
    if err != nil { return nil, fmt.Errorf("download system artifact: %w", err) }
    if resp.StatusCode != http.StatusOK {
        resp.Body.Close()
        return nil, fmt.Errorf("download system artifact (HTTP %d)", resp.StatusCode)
    }
    return resp.Body, nil
}

// DownloadTo downloads a system artifact and saves it to dst path.
func (s *SystemArtifactClient) DownloadTo(ctx context.Context, key, dst string) error {
    rc, err := s.Download(ctx, key)
    if err != nil { return err }
    defer rc.Close()
    f, err := os.Create(dst)
    if err != nil { return err }
    defer f.Close()
    _, err = io.Copy(f, rc)
    return err
}

// Archive returns a ZIP archive of the requested files as an io.ReadCloser. Caller must Close().
func (s *SystemArtifactClient) Archive(ctx context.Context, req ArchiveRequest) (io.ReadCloser, error) {
    body, _ := json.Marshal(req)
    httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
        s.c.baseURL+"/api/v1/artifacts/system/archive", bytes.NewReader(body))
    if err != nil { return nil, err }
    httpReq.Header.Set("Content-Type", "application/json")
    resp, err := s.c.httpClient.Do(httpReq)
    if err != nil { return nil, err }
    if resp.StatusCode != http.StatusOK {
        resp.Body.Close()
        return nil, fmt.Errorf("archive system artifacts (HTTP %d)", resp.StatusCode)
    }
    return resp.Body, nil
}

// ArchiveTo downloads a ZIP archive and saves it to dst path.
func (s *SystemArtifactClient) ArchiveTo(ctx context.Context, req ArchiveRequest, dst string) error {
    rc, err := s.Archive(ctx, req)
    if err != nil { return err }
    defer rc.Close()
    f, err := os.Create(dst)
    if err != nil { return err }
    defer f.Close()
    _, err = io.Copy(f, rc)
    return err
}

// ---- UserArtifactClient ----

// UserArtifactClient operates on /api/v1/artifacts/user endpoints.
type UserArtifactClient struct {
    c *Client
}

// UserArtifacts returns a UserArtifactClient.
func (c *Client) UserArtifacts() *UserArtifactClient {
    return &UserArtifactClient{c: c}
}

// List retrieves a paginated list of user artifacts.
func (u *UserArtifactClient) List(ctx context.Context, opts ArtifactListOptions) (*UserArtifactList, error) { ... }

// Put uploads content from r, associating it with key.
// mimeType may be empty (auto-detected server-side).
func (u *UserArtifactClient) Put(ctx context.Context, key string, r io.Reader, mimeType string) (*UserArtifactItem, error) {
    // Multipart body: field name "file", filename = last path segment of key
    var buf bytes.Buffer
    mw := multipart.NewWriter(&buf)
    filename := filepath.Base(key)
    fw, err := mw.CreateFormFile("file", filename)
    if err != nil { return nil, err }
    if _, err := io.Copy(fw, r); err != nil { return nil, err }
    mw.Close()

    urlPath := u.c.baseURL + "/api/v1/artifacts/user/" + url.PathEscape(key)
    req, err := http.NewRequestWithContext(ctx, http.MethodPut, urlPath, &buf)
    if err != nil { return nil, err }
    req.Header.Set("Content-Type", mw.FormDataContentType())

    resp, err := u.c.httpClient.Do(req)
    if err != nil { return nil, err }
    defer resp.Body.Close()
    if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
        b, _ := io.ReadAll(resp.Body)
        return nil, fmt.Errorf("put user artifact (HTTP %d): %s", resp.StatusCode, b)
    }
    var item UserArtifactItem
    if err := json.NewDecoder(resp.Body).Decode(&item); err != nil { return nil, err }
    return &item, nil
}

// PutFile opens a local file and uploads it with the given key.
func (u *UserArtifactClient) PutFile(ctx context.Context, key, localPath string) (*UserArtifactItem, error) {
    f, err := os.Open(localPath)
    if err != nil { return nil, err }
    defer f.Close()
    mimeType := mime.TypeByExtension(filepath.Ext(localPath))
    return u.Put(ctx, key, f, mimeType)
}

// Get retrieves metadata for a user artifact by key.
func (u *UserArtifactClient) Get(ctx context.Context, key string) (*UserArtifactItem, error) { ... }

// Download returns the file content as an io.ReadCloser. Caller must Close().
func (u *UserArtifactClient) Download(ctx context.Context, key string) (io.ReadCloser, error) { ... }

// DownloadTo downloads a user artifact and saves it to dst path.
func (u *UserArtifactClient) DownloadTo(ctx context.Context, key, dst string) error { ... }

// Delete removes a user artifact by key.
func (u *UserArtifactClient) Delete(ctx context.Context, key string) error { ... }

// Archive returns a ZIP archive of the requested user artifacts.
func (u *UserArtifactClient) Archive(ctx context.Context, req ArchiveRequest) (io.ReadCloser, error) { ... }

// ArchiveTo downloads a ZIP archive and saves it to dst path.
func (u *UserArtifactClient) ArchiveTo(ctx context.Context, req ArchiveRequest, dst string) error { ... }

// ---- Helpers ----

// buildSystemQuery converts ArtifactListOptions into a URL query string.
func buildSystemQuery(opts ArtifactListOptions) string {
    v := url.Values{}
    if opts.Q != "" { v.Set("q", opts.Q) }
    for _, id := range opts.AgentID   { v.Add("agent_id", id) }
    for _, id := range opts.SessionID { v.Add("session_id", id) }
    if opts.Operation != "" { v.Set("operation", opts.Operation) }
    if !opts.Since.IsZero() { v.Set("since", opts.Since.Format(time.RFC3339)) }
    if !opts.Until.IsZero() { v.Set("until", opts.Until.Format(time.RFC3339)) }
    if opts.IncludeDeleted { v.Set("include_deleted", "true") }
    if opts.Page > 0    { v.Set("page",     strconv.Itoa(opts.Page)) }
    if opts.PerPage > 0 { v.Set("per_page", strconv.Itoa(opts.PerPage)) }
    if opts.Sort  != "" { v.Set("sort",  opts.Sort) }
    if opts.Order != "" { v.Set("order", opts.Order) }
    if len(v) == 0 { return "" }
    return "?" + v.Encode()
}
```

### [MODIFY] README.md
*   **Description**: `### Artifact API Examples` セクションを追加する
*   **挿入位置**: `### Vault API Examples` セクションの直前
*   **内容**: 仕様書「README サンプルコード」の 6 サンプルをそのまま掲載する（仕様書 "README サンプルコード" セクション参照）

---

## Step-by-Step Implementation Guide

1. **[Client テスト作成]**: `client/v1/artifacts_test.go` を新規作成する（Red）。
2. **[型定義]**: `client/v1/artifacts.go` に `ArtifactListOptions`・`SystemArtifactItem`・`SystemArtifactList`・`UserArtifactItem`・`UserArtifactList`・`ArchiveRequest` を記述する。
3. **[SystemArtifactClient 実装]**: `SystemArtifacts()`・`List`・`Get`・`Download`・`DownloadTo`・`Archive`・`ArchiveTo` を実装する（Green）。
4. **[UserArtifactClient 実装]**: `UserArtifacts()`・`List`・`Put`・`PutFile`・`Get`・`Download`・`DownloadTo`・`Delete`・`Archive`・`ArchiveTo` を実装する（Green）。
5. **[README 更新]**: `README.md` に `### Artifact API Examples` セクションを挿入し、6 つのサンプルコードを追加する。
6. **[ビルド確認]**: `./scripts/process/build.sh --skip-frontend --skip-etc`
7. **[統合テスト完走確認]**: 下記コマンドで Part 1/2/3 の全統合テストを実行する。

---

## Verification Plan

### Automated Verification

1. **Build & Unit Tests**:
   ```
   ./scripts/process/build.sh --skip-frontend --skip-etc
   ```
2. **All Artifact Integration Tests**:
   ```
   ./scripts/process/build.sh --skip-frontend --skip-etc && ./scripts/process/integration_test.sh --categories taskengine --specify "artifact"
   ```

### E2E シナリオ（`tests/artifact_e2e_test.go`）

仕様書「検証シナリオ」1-16 に対応する完全な E2E テストを作成する。

```go
// Scenario 1-7: System Artifact の収集と取得
// TestArtifact_E2E_SystemCollection:
//   1. tern サーバーを起動（in-process）
//   2. Cursor セッションを作成し、Write・StrReplace の StreamEvent を注入
//   3. GET /api/v1/artifacts/system?session_id=X → create + update の 2 イベントが返る
//   4. q=**/*.go フィルタ → .go ファイルのみ
//   5. POST /api/v1/artifacts/system/archive (q=**/*.go) → ZIP にファイル含まれる
//   6. セッション横断 ?q=**/user.go → 複数セッションのイベント返る

// Scenario 7-10: User Artifact CRUD
// TestArtifact_E2E_UserArtifactCRUD:
//   7. PUT /api/v1/artifacts/user/datasets/a.csv → 201
//   8. GET /api/v1/artifacts/user/datasets/a.csv/content → バイナリ一致
//   9. DELETE → 204; GET → 404
//   10. POST /api/v1/artifacts/user/archive → ZIP

// Scenario 11-16: MCP アクセス
// TestArtifact_E2E_MCPAccess:
//   11. MCP list_user_artifacts → 全件返る
//   12. list_user_artifacts(q="datasets/**") → glob フィルタ動作
//   13. get_user_artifact(key, encoding="text") → テキスト一致
//   14. get_user_artifact(key, encoding="base64") → base64 エンコード済みバイナリ一致
//   15. get_user_artifact("missing") → エラーメッセージ、セッション継続
```

## Documentation

- `README.md` の `### Artifact API Examples` セクションが追加されていること。
- `client/v1/README.md`（存在する場合）に `SystemArtifacts()` / `UserArtifacts()` メソッドへの言及を追加する。
