# 003-ConfigDir-Client-API-Example

> **Source Specification**: [ideas/003-ConfigDir-Client-API-Example.md](file://prompts/phases/000-foundation/branches/feat-profiles/ideas/003-ConfigDir-Client-API-Example.md)  
> **Related**: [plans/001-ConfigDir-Switch-Same-Session.md](file://prompts/phases/000-foundation/branches/feat-profiles/plans/001-ConfigDir-Switch-Same-Session.md) (PATCH API), [plans/002-ConfigDir-Conversation-Continuity.md](file://prompts/phases/000-foundation/branches/feat-profiles/plans/002-ConfigDir-Conversation-Continuity.md) (会話継続 LIVE)

## Goal Description

同一 `session_id` で `config_dir` を切り替えつつ会話を続けるための Go クライアント API を型付き `SessionInfo` で整理し、ルート `README.md` に短い入口を置き、詳細実装を `examples/config-dir-switch/` に公開する。サーバ側プロトコル変更は行わない。

## User Review Required

None. (仕様の User Review は承認済み: ディレクトリ名 `examples/config-dir-switch` / `SessionInfo` 追加 / デフォルトエージェント `claudecode` + `--agent`)

## Requirement Traceability

| Requirement (from Spec) | Implementation Point |
| :--- | :--- |
| R1 公開契約 (Create+ConfigDir / Send* / UpdateConfigDir / GetSession、セマンティクス文書化) | Proposed Changes > client GoDoc + example README + root README |
| R2-A 型付き GetSession → `SessionInfo` | `client/v1/session.go` `SessionInfo` + `GetSession` 戻り値変更 |
| R2-B UpdateConfigDir が `SessionInfo` を返す | `UpdateSessionConfigDir` / `Session.UpdateConfigDir` |
| R2-C 英語 GoDoc（切替セマンティクス） | 同ファイルのコメント |
| R3 `examples/config-dir-switch/` (main / README / go.mod、命題フロー順) | Proposed Changes > examples |
| R3 CLI フラグ・デフォルト claudecode | `main.go` flag 定義 |
| R4 ルート README 短い節 + リンク（全文禁止） | `README.md` Client Examples |
| R5 Example README が詳細の正（英語） | `examples/config-dir-switch/README.md` |
| R6 client httptest 単体 + example ビルド + 短い単体 | session_test + helpers_test; `build.sh` が examples/* をビルド |
| deprecated `client` 同期 | `client/session.go` (+ tests) |
| ternctl が map 戻りに依存 | `features/ternctl/main.go` を `SessionInfo` 向けに更新 |

## Proposed Changes

### client/v1 — SessionInfo と型付き API (R2)

#### [NEW] [client/v1/session_test.go](file://client/v1/session_test.go)

*   **Description**: TDD。型付き Get / PATCH を httptest で先に失敗させる。
*   **Technical Design**:
    ```go
    package client

    func TestGetSession_Typed(t *testing.T) {
        // GET /api/v1/sessions/s1 → 200 JSON SessionRecord shape
        // body includes id, agent_name, status, work_dir, session_dir,
        // config_dir, agent_session_id, error, model, created_at, updated_at
        // want: *SessionInfo with fields populated (ConfigDir == "/tmp/alpha")
    }

    func TestGetSession_HTTPError(t *testing.T) {
        // 404 → error, nil info
    }

    func TestUpdateSessionConfigDir_Typed(t *testing.T) {
        // PATCH /api/v1/sessions/s1 with {"config_dir":"/tmp/beta"}
        // assert method/path/body
        // 200 returns SessionInfo with ConfigDir "/tmp/beta", ID "s1"
    }

    func TestUpdateSessionConfigDir_ClearEmpty(t *testing.T) {
        // PATCH body {"config_dir":""} → OK, ConfigDir empty
    }

    func TestSession_UpdateConfigDir_Delegates(t *testing.T) {
        // Session{ID:"s1"}.UpdateConfigDir(ctx, "/tmp/beta") hits same PATCH
    }
    ```

#### [MODIFY] [client/v1/session.go](file://client/v1/session.go)

*   **Description**: `SessionInfo` を追加し、`GetSession` / `UpdateSessionConfigDir` / `UpdateConfigDir` の戻り値を `(*SessionInfo, error)` に変更。GoDoc にセマンティクスを英語で記載。
*   **Technical Design** (構造体・シグネチャをそのまま継承):
    ```go
    // SessionInfo is the typed session record returned by GetSession and
    // UpdateSessionConfigDir. Field names match the CAWA JSON (snake_case tags).
    type SessionInfo struct {
        ID             string    `json:"id"`
        AgentName      string    `json:"agent_name"`
        Model          string    `json:"model"`
        Status         string    `json:"status"`
        Error          string    `json:"error,omitempty"`
        WorkDir        string    `json:"work_dir"`
        AgentSessionID string    `json:"agent_session_id"`
        SessionDir     string    `json:"session_dir"`
        ConfigDir      string    `json:"config_dir,omitempty"`
        CreatedAt      time.Time `json:"created_at"`
        UpdatedAt      time.Time `json:"updated_at"`
    }

    // GetSession fetches session details.
    // Does not change work_dir, session_dir, or agent_session_id.
    func (c *Client) GetSession(ctx context.Context, sessionID string) (*SessionInfo, error)

    // UpdateSessionConfigDir sets config_dir on an existing session via PATCH.
    // Pass an empty configDir to clear (disable overlay on subsequent launches;
    // for Codex this restores --ignore-user-config on later launches).
    //
    // Semantics:
    //   - Does not change work_dir, session_dir, or agent_session_id.
    //   - Overlay applies on the next SendMessage / SendText / Send, not immediately.
    //   - Do not Terminate between turns merely to switch config_dir; terminate is
    //     only for forced teardown / cleanup after the demo.
    func (c *Client) UpdateSessionConfigDir(ctx context.Context, sessionID, configDir string) (*SessionInfo, error)

    // UpdateConfigDir updates config_dir for this session (see UpdateSessionConfigDir).
    func (s *Session) UpdateConfigDir(ctx context.Context, configDir string) (*SessionInfo, error)
    ```
*   **Logic**:
    1. 既存の HTTP 呼び出しロジックは維持。レスポンスを `json.Unmarshal` で `SessionInfo` にデコードする（`map[string]any` は使わない）。
    2. `GetSession` が非 2xx のとき現状どおりエラーにする（実装時に StatusCode チェックが無ければ追加し、PATCH と同様に本文を含める）。
    3. `import "time"` を追加。
    4. **後方互換方針（承認済みの破壊的変更）**: 仕様 R2-A/B に従い戻り値型を `*SessionInfo` に置き換える。リポジトリ内の呼び出し（`features/ternctl`）を同時更新する。外部利用者が `map[string]any` を前提にしている場合はコンパイルエラーになるが、本ブランチの意図的な DX 改善とする。`map` 版の別名メソッドは追加しない（二重 API を避ける）。

### deprecated client — 同期

#### [MODIFY] [client/session_test.go](file://client/session_test.go)

*   **Description**: `TestGetSession_Typed` / `TestUpdateSessionConfigDir_Typed` を deprecated `client` パッケージにも追加（v1 と同ケース）。

#### [MODIFY] [client/session.go](file://client/session.go)

*   **Description**: `SessionInfo` と戻り値型・GoDoc を `client/v1` と同等に同期。

### ternctl — 呼び出し側更新

#### [MODIFY] [features/ternctl/main.go](file://features/ternctl/main.go)

*   **Description**: `GetSession` / `UpdateSessionConfigDir` の戻りが `*SessionInfo` になるため、`details["status"]` 等の map アクセスをフィールドアクセスに変更。
*   **Logic**:
    ```go
    details, err := c.GetSession(ctx, session.ID)
    // ...
    out, _ := json.MarshalIndent(details, "", "  ")
    if details.Status != "completed" { ... }
    if details.Error != "" { ... }

    // cmdSession:
    if details.Status == "error" { ... use details.Error }

    // cmdSessionConfig: MarshalIndent(details) はそのまま動作
    ```

### examples/config-dir-switch (R3, R5)

#### [NEW] [examples/config-dir-switch/helpers_test.go](file://examples/config-dir-switch/helpers_test.go)

*   **Description**: 一時 config セット作成とデフォルトフラグの単体（課金なし）。
*   **Technical Design**:
    ```go
    func TestPrepareConfigDirs_WritesMarkers(t *testing.T) {
        // prepareConfigPair(t.TempDir()) → alpha, beta
        // alpha/CLAUDE.md contains "TERN_CONFIG_ALPHA" (or AGENTS.md for codex path)
        // beta marker distinct
    }

    func TestDefaultFlags(t *testing.T) {
        // parseFlags([]string{}) → Agent == "claudecode", Server == "http://localhost:3100"
    }
    ```

#### [NEW] [examples/config-dir-switch/main.go](file://examples/config-dir-switch/main.go)

*   **Description**: 命題フローの最短実装。デフォルトエージェント `claudecode`、`--agent` で `codex` 切替。
*   **Logic**（実行順・要約禁止）:
    1. `client.New(serverURL, client.WithNoTimeout())`
    2. `CreateSession` with `ConfigDir: alphaDir`、明示 `SessionDir`、`Agent` / `Model` / `WorkDir` from flags
    3. `SendText` ターン1（デフォルト短文。例: reply with exactly `turn-1`）
    4. `session.UpdateConfigDir(ctx, betaDir)` — **Terminate しない**
    5. `GetSession` で `info.ID` / `info.SessionDir` 維持と `info.ConfigDir == betaDir` をログ（不一致なら fatal）
    6. `SendText` ターン2（継続確認 + beta マーカーに触れる短いプロンプト）
    7. 終了時のみ任意で `Terminate`（`defer` でクリーンアップ可。ターン間では呼ばない）
*   **CLI フラグ**（仕様どおり）:
    ```text
    --server URL                 (default http://localhost:3100)
    --agent claudecode|codex     (default claudecode)
    --model NAME
    --work-dir DIR
    --session-dir DIR            (default: temp under os.TempDir)
    --config-dir-alpha DIR       (default: auto-created temp with marker)
    --config-dir-beta DIR        (default: auto-created temp with marker)
    --prompt1 TEXT
    --prompt2 TEXT
    ```
*   **ヘルパー**: `agent == "codex"` ならマーカーファイル `AGENTS.md`、それ以外は `CLAUDE.md`。リポジトリに大きなフィクスチャを置かない。
*   **import**: `github.com/axsh/arctic-tern/client/v1` のみ（deprecated `client` は使わない）。

#### [NEW] [examples/config-dir-switch/README.md](file://examples/config-dir-switch/README.md)

*   **Description**: 詳細の正（英語）。仕様 R5 の節をすべて含める。
*   **Sections** (必須見出し):
    1. Prerequisites — tern server, vault keys, `claude` / `codex` CLI as needed
    2. How to run — `go run .` / built binary examples for claude and `--agent codex`
    3. What this demonstrates — 命題対応（同一 session_id / config 切替 / 会話継続 / terminate 不要）
    4. API mapping 表:
       | Client method | HTTP |
       | CreateSession (+ ConfigDir) | POST /api/v1/sessions |
       | Session.SendText | POST /api/v1/sessions/:id/messages |
       | Session.UpdateConfigDir | PATCH /api/v1/sessions/:id |
       | Client.GetSession | GET /api/v1/sessions/:id |
    5. Notes — no terminate between turns; overlay on next message; empty config_dir clears
    6. LIVE billing optional — short prompts by default; real LLM cost is caller's responsibility

#### [NEW] [examples/config-dir-switch/go.mod](file://examples/config-dir-switch/go.mod)

*   **Description**: `examples/minimal-client` と同型。
    ```go
    module github.com/axsh/arctic-tern/examples/config-dir-switch

    go 1.26.4

    require github.com/axsh/arctic-tern v0.0.0

    replace github.com/axsh/arctic-tern => ../../
    ```
*   **go.sum**: `go mod tidy` 相当を example ディレクトリで実行して生成（実装時。計画ではファイル作成を必須とする）。

### README — 入口 (R4)

#### [MODIFY] [README.md](file://README.md)

*   **Description**: `### Client Examples` 直下（既存 minimal/multimodal 紹介の直後）に短い節を追加。example 全文は貼らない。
*   **Logic**（英語・必須内容）:
    1. 2–4 文: 同一 session で `config_dir` 切替可、会話継続、切替に terminate 不要
    2. 最小スニペットのみ:
       ```go
       session, _ := c.CreateSession(ctx, client.SessionRequest{
           Agent: "claudecode", WorkDir: workDir, SessionDir: sessionDir, ConfigDir: alphaDir,
       })
       stream1, _ := session.SendText(ctx, prompt1)
       // ... drain stream1 ...
       info, _ := session.UpdateConfigDir(ctx, betaDir) // PATCH; do not Terminate
       _ = info
       stream2, _ := session.SendText(ctx, prompt2)
       ```
    3. リンク: [examples/config-dir-switch/README.md](examples/config-dir-switch/README.md)、[examples/config-dir-switch/main.go](examples/config-dir-switch/main.go)
    4. Reference: [docs/ReferenceManual-WebAPIs.md](docs/ReferenceManual-WebAPIs.md) の PATCH 節へ誘導
*   **build.sh**: `examples/*/` を既にループビルドするため、新規ディレクトリ追加だけで十分。列挙変更は不要（実装時に確認のみ）。

### 任意（時間があれば）

*   `features/ternctl` ヘルプまたは README 断片から `examples/config-dir-switch` への一行リンク。

## Step-by-Step Implementation Guide

1. [x] **TDD client/v1**: `client/v1/session_test.go` を追加し、型付き Get/PATCH テストを書く → 赤確認。
2. [x] **Implement SessionInfo (v1)**: `client/v1/session.go` に `SessionInfo` と戻り値変更・GoDoc。
3. [x] **Sync deprecated client**: `client/session.go` + `client/session_test.go`。
4. [x] **Update ternctl**: map アクセスを `SessionInfo` フィールドに置換。
5. [x] **Example helpers + tests**: `examples/config-dir-switch/helpers_test.go`（および flag/parse 用の小さな関数を `main.go` から抽出するか同パッケージで export 不要な unexported テスト）。
6. [x] **Example main + go.mod + README**: 命題フロー順で実装。デフォルト `--agent=claudecode`。
7. [x] **Root README**: Client Examples に短い節 + リンク。
8. [x] **Verify**: 下記 Verification Plan。成功後コミット単位で整理。

## Verification Plan

### Automated Verification

1. **Build & Unit Tests**: `./scripts/process/build.sh`  
   - `client` / `client/v1` の新テスト通過  
   - `examples/config-dir-switch` が examples ループでビルド成功  
   - `ternctl` ビルド成功

2. **Integration Tests** (既存 ConfigDir / client 関連のリグレッション):  
   `./scripts/process/integration_test.sh --specify "ConfigDir|UpdateSessionConfigDir|session-config"`  
   （該当テストが無い場合は `--specify "AgentService_ConfigDir|ConfigDir_"` など実装時に確定した名前で再実行）

3. **E2E / Example smoke（課金なし）**:  
   - `examples/config-dir-switch` の単体（helpers / flags）は `build.sh` 内 unit で実行  
   - バイナリ `--help` または不正フラグで usage が出ること（example 内テスト、または実装時に `TestMainFlags_Help` を追加）  
   - **LIVE 課金実行は必須としない**（仕様どおり任意。ドキュメントに明記）

### Manual (補助のみ・受け入れの主手段にしない)

- サーバ起動済み環境で `go run ./examples/config-dir-switch` を一度実行し、ログに同一 `session_id` と PATCH 後 `config_dir=beta` が出ることを確認（任意）

## Documentation

| 文書 | 作業 |
| :--- | :--- |
| `README.md` | Client Examples に短い節 + example / ReferenceManual リンク |
| `examples/config-dir-switch/README.md` | NEW・詳細の正 |
| `client/v1` GoDoc | セマンティクス英語 |
| `docs/ReferenceManual-WebAPIs.md` | PATCH は既存のため必須変更なし。README からリンク |
| 仕様 `ideas/003-...` | User Review 承認済み済み |
