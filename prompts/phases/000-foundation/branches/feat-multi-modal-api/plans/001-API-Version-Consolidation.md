# 001-API-Version-Consolidation

> **Source Specification**: [001-API-Version-Consolidation.md](file://prompts/phases/000-foundation/branches/feat-multi-modal-api/ideas/001-API-Version-Consolidation.md)

## Goal Description

旧 v2 API (マルチモーダル対応コンテンツブロック配列形式) を新 v1 API として統一し、旧 v1 API (`{"message": "..."}` 形式) を廃止する。`server.WithEnableVersion()` オプションを追加し、`client/v1` パッケージを新規作成する。

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1-1: `WithEnableVersion()` オプション関数 | Step 1 > server/options.go |
| R1-2: 未指定時は全バージョン有効 | Step 1 > server/server.go |
| R1-3: HTTPHandler でバージョン別ルート | Step 3 > agentservice/service.go |
| R2-1: 旧v2 -> 新v1 エンドポイント移行 | Step 3 > agentservice/handler.go |
| R2-2: 旧v1 メッセージ送信削除 | Step 3 > agentservice/handler.go |
| R2-3: セッション管理系もv1に統一 | Step 3 > agentservice/handler.go, service.go |
| R3-1: `client/v1/` パッケージ新規作成 | Step 4 > client/v1/*.go |
| R3-2: 旧 `client/` Deprecated化 | Step 5 > client/*.go |
| R3-3: minimal-client 更新 | Step 6 > examples/minimal-client/main.go |
| R4-1: minimal-server コメント追加 | Step 6 > examples/minimal-server/main.go |
| R4-2: minimal-client マルチモーダル例 | Step 6 > examples/minimal-client/main.go |
| R5-1: README 更新 | Step 8 > README.md |
| R5-2: v1 がコンテンツブロック形式であることを明記 | Step 8 > README.md |

---

## Proposed Changes

### server (バージョン制御オプション)

#### [MODIFY] [options_test.go](file://server/server_test.go)
*   **Description**: `WithEnableVersion` のバリデーションテストを追加。
*   **テストケース**:
    *   `TestWithEnableVersion_Valid`: `WithEnableVersion(1)` -> エラーなし。
    *   `TestWithEnableVersion_InvalidVersion`: `WithEnableVersion(99)` -> `"unsupported API version: 99"` エラー。
    *   `TestWithEnableVersion_Multiple`: `WithEnableVersion(1, 99)` -> エラー (99が不正)。
    *   `TestWithEnableVersion_Default`: 未指定 -> 全ルート登録。

#### [MODIFY] [options.go](file://server/options.go)
*   **Description**: `WithEnableVersion` Option 関数と `enableVersions` フィールドを追加。
*   **Technical Design**:
    ```go
    type options struct {
        // ...existing fields...
        enableVersions []int
    }

    // WithEnableVersion specifies which API versions to enable.
    // Supported versions: 1.
    // If not called, all supported versions are enabled by default.
    func WithEnableVersion(versions ...int) Option {
        return func(o *options) {
            o.enableVersions = versions
        }
    }
    ```

#### [MODIFY] [server.go](file://server/server.go)
*   **Description**: `New()` でバージョンバリデーションを実施し、`agentservice.Server` にバージョン情報を渡す。
*   **Logic**:
    1. `o.enableVersions` が空の場合、デフォルトで `[]int{1}` を設定。
    2. `supportedVersions := map[int]bool{1: true}` でサポート済みバージョンを定義。
    3. 各バージョンを検証し、未サポートなら `fmt.Errorf("tern: unsupported API version: %d", v)` を返す。
    4. `resolveAgentService()` に `enableVersions` を渡す。

---

### agentservice (ハンドラ統合・ルート再番号付け)

#### [MODIFY] [handler_test.go](file://shared/libs/go/agentservice/handler_test.go)
*   **Description**: 既存のv1テストのパスとリクエスト形式を新v1 (content block) 形式に更新。
*   **Logic**: テスト内の `{"message": "..."}` を `{"content": [{"type": "text", "text": "..."}]}` に変換。URLパスは `/api/v1/` のまま。

#### [MODIFY] [handler_v2_test.go](file://shared/libs/go/agentservice/handler_v2_test.go)
*   **Description**: 全テストのURLパスを `/api/v2/sessions/` -> `/api/v1/sessions/` に変更。ファイルの内容は handler_test.go にマージ後、削除する。

#### [MODIFY] [handler.go](file://shared/libs/go/agentservice/handler.go)
*   **Description**: `handleSendMessage` を旧v2の content block 形式 (`handleSendMessageV2` のロジック) に書き換え。旧 `{"message": "..."}` 形式を完全に削除。
*   **Technical Design**:
    ```go
    // SendMessageRequest is the request body for POST /api/v1/sessions/:id/messages.
    type SendMessageRequest struct {
        Content []codingagent.ContentPart `json:"content"`
    }

    // handleSendMessage handles POST /api/v1/sessions/:id/messages.
    // Accepts {"content": []ContentPart} with multimodal support.
    func (s *Server) handleSendMessage(w http.ResponseWriter, r *http.Request) {
        // handler_v2.go の handleSendMessageV2 のロジックをそのまま移行。
        // パスプレフィックスを /api/v1/sessions/ に変更。
    }
    ```
*   **Logic**:
    1. `strings.TrimPrefix(r.URL.Path, "/api/v1/sessions/")` でセッションID抽出。
    2. `SendMessageRequest` で JSON デコード。
    3. `codingagent.ValidateContentParts()` でバリデーション。
    4. `MultimodalSupporter` チェック。
    5. `BuildMultimodalPrompt` / `ExtractText` でプロンプト構築。
    6. セッション作成、Send、SSE/JSON レスポンス。
    7. `CleanupMultimodalFiles` で一時ファイル削除。
    *   `handleTerminate`, `handleGetSession`, `handleDeleteSession` 等はパス参照を更新不要 (既に `/api/v1/` を使用)。

#### [DELETE] [handler_v2.go](file://shared/libs/go/agentservice/handler_v2.go)
*   **Description**: handler.go に統合完了後、このファイルを削除する。
*   **Logic**: `MultimodalSupporter` インターフェースと `SendMessageRequest` (旧 `SendMessageV2Request`) は handler.go に移動済み。

#### [MODIFY] [service.go](file://shared/libs/go/agentservice/service.go)
*   **Description**: `enabledVersions` フィールド追加、`HTTPHandler` でバージョン別ルート登録。
*   **Technical Design**:
    ```go
    type Server struct {
        // ...existing fields...
        enabledVersions map[int]bool
    }

    // SetEnabledVersions configures which API versions are active.
    func (s *Server) SetEnabledVersions(versions []int) {
        s.enabledVersions = make(map[int]bool)
        for _, v := range versions {
            s.enabledVersions[v] = true
        }
    }

    func (s *Server) isVersionEnabled(v int) bool {
        if len(s.enabledVersions) == 0 {
            return true // default: all enabled
        }
        return s.enabledVersions[v]
    }

    func (s *Server) HTTPHandler() http.Handler {
        if s.cliVersions == nil {
            s.cliVersions = detectCLIVersions(s.agents, s.logger)
        }
        mux := http.NewServeMux()
        mux.HandleFunc("/health", s.handleHealth)

        if s.isVersionEnabled(1) {
            mux.HandleFunc("/api/v1/agents", s.routeAgents)
            mux.HandleFunc("/api/v1/models", s.routeModels)
            mux.HandleFunc("/api/v1/sessions", s.routeSessions)
            mux.HandleFunc("/api/v1/sessions/", s.routeSessionByID)
        }
        return mux
    }
    ```
*   **Logic**: `/api/v2/sessions/` のルートは完全に削除。`routeSessionByIDV2` は不要。

---

### client/v1 (新クライアントパッケージ)

#### [NEW] [client_test.go](file://client/v1/client_test.go)
*   **Description**: `New()` と `WithHTTPClient`, `WithNoTimeout` のテスト。
*   **テストケース**: 旧 `client/client_test.go` と同様の構造。

#### [NEW] [session_test.go](file://client/v1/session_test.go)
*   **Description**: `SendMessage()` のテスト。content block 形式の検証。
*   **テストケース**:
    *   `TestSendMessage_TextOnly`: テキストのみ `[]ContentPart` -> 200 OK。
    *   `TestSendMessage_WithImage`: 画像付き `[]ContentPart` -> 200 OK。
    *   `TestSendMessage_Error`: サーバーエラー時の処理。

#### [NEW] [client.go](file://client/v1/client.go)
*   **Description**: 旧 `client/client.go` をベースに `package v1` として再構成。
*   **Technical Design**:
    ```go
    package v1

    import (
        "net/http"
        "time"
    )

    type Client struct {
        baseURL    string
        httpClient *http.Client
    }

    func New(baseURL string, opts ...ClientOption) *Client {
        c := &Client{
            baseURL: baseURL,
            httpClient: &http.Client{Timeout: 30 * time.Second},
        }
        for _, opt := range opts { opt(c) }
        return c
    }

    type ClientOption func(*Client)

    func WithHTTPClient(hc *http.Client) ClientOption {
        return func(c *Client) { c.httpClient = hc }
    }

    func WithNoTimeout() ClientOption {
        return func(c *Client) { c.httpClient.Timeout = 0 }
    }
    ```

#### [NEW] [content.go](file://client/v1/content.go)
*   **Description**: `ContentPart` と `ImageSource` 型を定義。
*   **Technical Design**:
    ```go
    package v1

    // ContentPart represents a single content block in a v1 message.
    type ContentPart struct {
        Type   string       `json:"type"`
        Text   string       `json:"text,omitempty"`
        Source *ImageSource `json:"source,omitempty"`
    }

    // ImageSource contains base64-encoded image data.
    type ImageSource struct {
        Type      string `json:"type"`
        MediaType string `json:"media_type"`
        Data      string `json:"data"`
    }
    ```

#### [NEW] [session.go](file://client/v1/session.go)
*   **Description**: セッション管理。`SendMessage` は `[]ContentPart` を受け取る。
*   **Technical Design**:
    ```go
    package v1

    // Session represents an active coding agent session.
    type Session struct {
        ID     string
        client *Client
    }

    // SessionRequest is the request to create a session.
    type SessionRequest struct {
        Agent      string `json:"agent"`
        Model      string `json:"model,omitempty"`
        WorkDir    string `json:"work_dir"`
        SessionDir string `json:"session_dir,omitempty"`
    }

    func (c *Client) CreateSession(ctx context.Context, req SessionRequest) (*Session, error) {
        // POST /api/v1/sessions (旧clientと同一ロジック)
    }

    // SendMessage sends a multimodal message to the session and returns a Stream.
    func (s *Session) SendMessage(ctx context.Context, content []ContentPart) (*Stream, error) {
        body, err := json.Marshal(map[string]any{"content": content})
        // POST /api/v1/sessions/{id}/messages
    }

    // SendText is a convenience method for sending text-only messages.
    func (s *Session) SendText(ctx context.Context, message string) (*Stream, error) {
        return s.SendMessage(ctx, []ContentPart{{Type: "text", Text: message}})
    }

    func ResumeSession(c *Client, sessionID string) *Session { ... }
    func (s *Session) Terminate(ctx context.Context) error { ... }
    func (c *Client) GetSession(ctx context.Context, sessionID string) (map[string]any, error) { ... }
    ```

#### [NEW] [agents.go](file://client/v1/agents.go)
*   **Description**: `ListAgents` を移植。パスは `/api/v1/agents`。

#### [NEW] [models.go](file://client/v1/models.go)
*   **Description**: `ListModels` を移植。パスは `/api/v1/models`。

#### [NEW] [health.go](file://client/v1/health.go)
*   **Description**: `Health` を移植。パスは `/health`。

#### [NEW] [stream.go](file://client/v1/stream.go)
*   **Description**: 旧 `client/stream.go` をそのままコピー。`package v1` に変更。

#### [NEW] [stream_test.go](file://client/v1/stream_test.go)
*   **Description**: 旧 `client/stream_test.go` をコピー。`package v1_test` に変更。

---

### 旧 client パッケージ (Deprecated)

#### [MODIFY] [client/client.go](file://client/client.go)
*   **Description**: ファイル冒頭に Deprecated コメントを追加。
*   **Logic**: `// Deprecated: Use github.com/axsh/arctic-tern/client/v1 instead.`

#### [MODIFY] [client/session.go](file://client/session.go)
*   **Description**: ファイル冒頭に Deprecated コメントを追加。

---

### features/ternctl

#### [MODIFY] [features/ternctl/main.go](file://features/ternctl/main.go)
*   **Description**: import を `client/v1` に変更し、`SendMessage` を content block 形式に更新。
*   **Logic**:
    1. import: `"github.com/axsh/arctic-tern/client"` -> `client "github.com/axsh/arctic-tern/client/v1"`
    2. `session.SendMessage(ctx, *prompt)` -> `session.SendText(ctx, *prompt)`

---

### examples

#### [MODIFY] [examples/minimal-server/main.go](file://examples/minimal-server/main.go)
*   **Description**: `server.WithEnableVersion(1)` のコメント例を追加。
*   **Logic**: `server.New()` の呼び出し付近にコメントを追加:
    ```go
    // Optional: specify which API versions to enable.
    // srv, err := server.New(server.WithConfigPath(configPath), server.WithEnableVersion(1))
    ```

#### [MODIFY] [examples/minimal-client/main.go](file://examples/minimal-client/main.go)
*   **Description**: `client/v1` を使用するように変更。`SendText` とマルチモーダルのコメント例を追加。
*   **Logic**:
    1. import: `"github.com/axsh/arctic-tern/client"` -> `client "github.com/axsh/arctic-tern/client/v1"`
    2. `session.SendMessage(ctx, "...")` -> `session.SendText(ctx, "...")`
    3. コメントでマルチモーダル送信例を追加。

---

### tests (結合テスト)

#### [MODIFY] [tests/multimodal_integration_test.go](file://tests/multimodal_integration_test.go)
*   **Description**: 全テストのURLパスを `/api/v2/` -> `/api/v1/` に変更。旧v1テストは content block 形式に更新。

#### [MODIFY] [tests/agentservice_integration_test.go](file://tests/agentservice_integration_test.go)
*   **Description**: テスト内の `/api/v1/sessions/:id/messages` へのリクエストを `{"content": [...]}` 形式に変更。

---

## Step-by-Step Implementation Guide

### Step 1: server WithEnableVersion オプション (TDD)
1. `server/server_test.go` に `TestWithEnableVersion_*` テストケースを追加。
2. `server/options.go` に `enableVersions` フィールドと `WithEnableVersion()` 関数を追加。
3. `server/server.go` の `New()` にバージョンバリデーションを追加。
4. `./scripts/process/build.sh` でビルド・テスト。
5. `git add && git commit -F tmp/commit_msg.txt`

### Step 2: agentservice バージョン制御 (TDD)
1. `service.go` に `enabledVersions` フィールド、`SetEnabledVersions()`, `isVersionEnabled()` を追加。
2. `HTTPHandler()` を更新: `isVersionEnabled(1)` で v1 ルートを条件登録。
3. v2 ルート (`/api/v2/sessions/`) を削除。
4. `./scripts/process/build.sh` でビルド・テスト。
5. `git add && git commit -F tmp/commit_msg.txt`

### Step 3: ハンドラ統合 (旧v2 -> 新v1)
1. `handler.go` の `handleSendMessage` を `handler_v2.go` の `handleSendMessageV2` のロジックで書き換え。
   - リクエスト型を `SendMessageRequest` (`{"content": [...]}`) に変更。
   - `MultimodalSupporter` チェック、`BuildMultimodalPrompt`、`CleanupMultimodalFiles` を含む。
   - パスプレフィックスを `/api/v1/sessions/` に統一。
2. `handler_v2.go` を削除 (`MultimodalSupporter`, `SendMessageRequest` は handler.go に移動済み)。
3. `handler_test.go` と `handler_v2_test.go` を統合。全テストのURLを `/api/v1/` に統一。
4. `./scripts/process/build.sh` でビルド・テスト。
5. `git add && git commit -F tmp/commit_msg.txt`

### Step 4: client/v1 パッケージ作成 (TDD)
1. `client/v1/content.go` を作成 (`ContentPart`, `ImageSource`)。
2. `client/v1/client.go` を作成。
3. `client/v1/stream.go`, `client/v1/stream_test.go` を旧 client からコピー。
4. `client/v1/session.go` を作成 (`SendMessage(ctx, []ContentPart)`, `SendText(ctx, string)`)。
5. `client/v1/session_test.go` を作成。
6. `client/v1/agents.go`, `client/v1/models.go`, `client/v1/health.go` を作成。
7. `client/v1/client_test.go` を作成。
8. `./scripts/process/build.sh` でビルド・テスト。
9. `git add && git commit -F tmp/commit_msg.txt`

### Step 5: 旧 client Deprecated 化
1. `client/client.go` の冒頭に `// Deprecated` コメント追加。
2. `client/session.go`, `client/agents.go`, `client/models.go`, `client/health.go`, `client/stream.go` にも同様。
3. `git add && git commit -F tmp/commit_msg.txt`

### Step 6: examples と ternctl の更新
1. `examples/minimal-client/main.go`: import を `client/v1` に変更、`SendText` 使用。
2. `examples/minimal-server/main.go`: `WithEnableVersion` コメント追加。
3. `features/ternctl/main.go`: import を `client/v1` に変更、`SendText` 使用。
4. `./scripts/process/build.sh` でビルド・テスト。
5. `git add && git commit -F tmp/commit_msg.txt`

### Step 7: 結合テスト更新
1. `tests/multimodal_integration_test.go`: 全 URL パスを `/api/v1/` に変更。
2. `tests/agentservice_integration_test.go`: メッセージ送信リクエストを content block 形式に変更。
3. `./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestMultimodal"` で結合テスト実行。
4. `./scripts/process/integration_test.sh --specify "TestAgentService"` でリグレッション確認。
5. `git add && git commit -F tmp/commit_msg.txt`

### Step 8: README 更新
1. README.md のサンプルコードを全て新しい構造に合わせて更新。
2. `git add && git commit -F tmp/commit_msg.txt`

### Step 9: 検証・プッシュ
1. Verification Plan の全ステップを実行。
2. `git push`

---

## Verification Plan

### Automated Verification

1. **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```
    *   `server/`: `TestWithEnableVersion_*` (4件)
    *   `agentservice/`: ハンドラ統合テスト、バージョン制御テスト
    *   `client/v1/`: `TestSendMessage_*`, `TestSendText_*`, ストリームテスト

2. **Integration Tests**:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestMultimodal"
    ```
    *   全テストが `/api/v1/` パスで動作することを確認。

    ```bash
    ./scripts/process/integration_test.sh --specify "TestAgentService"
    ```
    *   既存テストがリグレッションなく動作することを確認。
    *   **Log Verification**: テスト出力に `FAIL` や `panic` がないこと。全テスト PASS を確認。

3. **E2E Tests**:
    本変更はAPIパスとリクエスト形式の変更であり、既存の結合テスト (`tests/multimodal_integration_test.go`, `tests/agentservice_integration_test.go`) がE2Eテストに相当する。これらを更新することで新API構造の動作を検証する。追加のE2Eテストファイルは不要。

### テスト項目設計のセルフレビュー (11.4)

1. **網羅性**: server バージョン制御 (有効/不正/デフォルト)、ハンドラ統合 (テキスト/画像/エラー/非対応エージェント)、client/v1 (テキスト/画像/ストリーム)、結合テスト (10件) で主要パスをカバー。
2. **証拠の十分性**: HTTPステータスコード、レスポンスボディの内容、エラーメッセージを検証。
3. **迂回排除**: URLパスの変更テスト、旧v2パスへのアクセスが404になることを確認。
4. **依存関係**: codingagent.ContentPart (Step 0で確認済み) -> handler (Step 3) -> client/v1 (Step 4) -> 結合テスト (Step 7) のボトムアップ順序。

### 総合判定プロセス (12)

全テスト完了後に testing-rules 12.2 のチェック項目7件を実施し、walkthrough.md に結果を記録する。

## Documentation

#### [MODIFY] [README.md](file://README.md)
*   **更新内容**:
    *   Server サンプル: `WithEnableVersion` コメント追加。
    *   Client サンプル: import を `client/v1` に変更、`SendText` / `SendMessage` 両方の例。
    *   Multimodal セクション: `/api/v1/` パスに更新。
    *   Architecture セクション: 「v2 API」の記述を「v1 API (コンテンツブロック形式)」に修正。
    *   Roadmap: 「CAWA API v2」を「CAWA API v1 (multimodal)」に修正。
