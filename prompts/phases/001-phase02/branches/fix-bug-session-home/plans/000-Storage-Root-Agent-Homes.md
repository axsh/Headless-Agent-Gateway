# 000-Storage-Root-Agent-Homes

> **Source Specification**: `prompts/phases/001-phase02/branches/fix-bug-session-home/ideas/000-Storage-Root-Agent-Homes.md`

## Goal Description

CreateSession API に任意フィールド `storage_root`（候補 B）を追加し、Coding Agent Home 群（`.tern` / `.codex` / `.claude`）の親を `work_dir` 固定から切り離す。`session_dir` は正本リーフ上書きとして現行互換を維持し、省略時は `{storage_root}/.tern/{session_id}` とする。`VendorHomeDir` は `storage_root` から vendor home を導出する。一覧 API は任意クエリ `storage_root` でスキャン基点を切り替えられるようにする。

## User Review Required

None.（仕様レビュー時の推奨「候補 B」を本計画で確定して実装する）

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1 用語・概念モデル | `storage_paths.go` ヘルパ + docs |
| R2 目標レイアウト | CreateSession フォールバック + VendorHomeDir |
| R3 既定値 | `ResolveStorageRoot` / `CanonicalSessionDir` / `VendorHomeDir` |
| R4 分離と共有 | VendorHomeDir（リーフ非混在）+ 同一 storage_root 共有 |
| R5 環境変数マッピング | SendMessage → VendorHomeDir → adapter SessionDir |
| R6 config_dir 現行維持 | overlay 先は VendorHomeDir 結果（自動追従） |
| R7 API 候補 B | CreateSession `storage_root` + `session_dir` 互換 |
| R8 一覧 API 拡張 | `?storage_root=` + record に `storage_root` 永続 |
| R9 ドキュメント用語区別 | ReferenceManual-WebAPIs.md |
| R10 Issue #48 非再発 | 既存 llm_vendor_home_layout 回帰 + storage_root 共有ケース |
| R11 Agent 個別上書き (May) | 本計画では実装しない（内部識別ヘルパのみ） |
| R12 native/ (May) | 変更なし（ドキュメント維持） |

## Proposed Changes

### Path helpers (agentservice)

#### [NEW] [shared/libs/go/agentservice/storage_paths_test.go](file://shared/libs/go/agentservice/storage_paths_test.go)

*   **Description**: TDD — `ResolveStorageRoot` / `WayfinderDir` / `CanonicalSessionDir` / `EffectiveStorageRoot` のテーブルテスト。
*   **Technical Design**:
    *   Cases:
        *   `storage_root=""` + `work_dir="/ws"` → `"/ws"`
        *   `storage_root="/data"` + `work_dir="/ws"` → `"/data"`
        *   `WayfinderDir("/data")` → `filepath.Join("/data", ".tern")`（`session_id` を含まない）
        *   `CanonicalSessionDir("/data", "abc")` → `filepath.Join("/data", ".tern", "abc")`
        *   `EffectiveStorageRoot(record)`: `StorageRoot` 優先、空なら `WorkDir`（旧レコード互換）
*   **Logic**: 仕様 R3 / R1 の既定値をそのまま検証する。

#### [NEW] [shared/libs/go/agentservice/storage_paths.go](file://shared/libs/go/agentservice/storage_paths.go)

*   **Description**: Home / 正本パス導出ヘルパ。
*   **Technical Design**:
    ```go
    // ResolveStorageRoot returns storage_root if non-empty, otherwise workDir.
    func ResolveStorageRoot(storageRoot, workDir string) string

    // WayfinderDir returns the Wayfinder/Tern Home: {storageRoot}/.tern (no session_id).
    func WayfinderDir(storageRoot string) string

    // CanonicalSessionDir returns the Tern canonical leaf: {storageRoot}/.tern/{sessionID}.
    func CanonicalSessionDir(storageRoot, sessionID string) string

    // EffectiveStorageRoot returns record.StorageRoot, or WorkDir when empty (legacy records).
    func EffectiveStorageRoot(rec *codingagent.SessionRecord) string
    ```
*   **Logic**:
    *   `ResolveStorageRoot`: `if storageRoot != "" { return storageRoot }; return workDir`
    *   `WayfinderDir`: `filepath.Join(storageRoot, ".tern")`
    *   `CanonicalSessionDir`: `filepath.Join(WayfinderDir(storageRoot), sessionID)`
    *   `EffectiveStorageRoot`: `if rec.StorageRoot != "" { return rec.StorageRoot }; return rec.WorkDir`

### Vendor home

#### [MODIFY] [shared/libs/go/agentservice/vendor_home_test.go](file://shared/libs/go/agentservice/vendor_home_test.go)

*   **Description**: 第 1 引数を `storageRoot` 意味に更新。`storage_root ≠ work_dir` ケースを追加。
*   **Technical Design**: 既存テーブルの第 1 列名を `storageRoot` に。追加ケース:
    *   `{"codex_other_root", "/data", "codex", "/ws/.tern/s1", filepath.Join("/data", ".codex")}`
    *   `{"claude_other_root", "/data", "claudecode", "/ws/.tern/s1", filepath.Join("/data", ".claude")}`
*   **Logic**: vendor home 親は常に第 1 引数（storage_root）。wayfinder は引き続き `sessionDir`（正本リーフ）。

#### [MODIFY] [shared/libs/go/agentservice/vendor_home.go](file://shared/libs/go/agentservice/vendor_home.go)

*   **Description**: パラメータ名とコメントを `storageRoot` に合わせる（シグネチャ位置は維持して呼び出し側の破壊を最小化）。
*   **Technical Design**:
    ```go
    // VendorHomeDir returns the Coding Agent home for launch/overlay.
    // Mapping:
    //   codex       → {storageRoot}/.codex
    //   claudecode  → {storageRoot}/.claude   // never .claudecode
    //   wayfinder   → sessionDir              // Tern canonical leaf (.tern/{id})
    func VendorHomeDir(storageRoot, agentName, sessionDir string) string
    ```
*   **Logic**: 既存 switch の `workDir` 参照を `storageRoot` に置換。挙動は第 1 引数が親ルートであること以外同じ。

### SessionRecord

#### [MODIFY] [shared/libs/go/codingagent/session_store.go](file://shared/libs/go/codingagent/session_store.go)

*   **Description**: `StorageRoot` フィールド追加。
*   **Technical Design**:
    ```go
    type SessionRecord struct {
        ID             string    `json:"id"`
        AgentName      string    `json:"agent_name"`
        Model          string    `json:"model"`
        Status         string    `json:"status"`
        Error          string    `json:"error,omitempty"`
        WorkDir        string    `json:"work_dir"`
        StorageRoot    string    `json:"storage_root,omitempty"`
        AgentSessionID string    `json:"agent_session_id"`
        SessionDir     string    `json:"session_dir"`
        ConfigDir      string    `json:"config_dir,omitempty"`
        CreatedAt      time.Time `json:"created_at"`
        UpdatedAt      time.Time `json:"updated_at"`
    }
    ```
*   **Logic**: `storage_root` を `record.json` に永続。省略時（旧レコード）は空でよく、実行時は `EffectiveStorageRoot` で `work_dir` にフォールバック。

### CreateSession / SendMessage

#### [MODIFY] [shared/libs/go/agentservice/handler_session_test.go](file://shared/libs/go/agentservice/handler_session_test.go)

*   **Description**: CreateSession の `storage_root` 既定・明示・`session_dir` 互換、List の `storage_root` クエリを追加。
*   **Technical Design**:
    *   `TestHandleCreateSession_DefaultSessionDirTern`: 省略時 `session_dir = {work_dir}/.tern/{id}` かつ `storage_root` が work_dir（Abs）と一致することを確認。
    *   NEW `TestHandleCreateSession_StorageRootOverridesParent`:
        *   `work_dir=work`, `storage_root=root`（別 TempDir）
        *   正本 = `{root}/.tern/{id}/record.json` が存在
        *   GET session の `storage_root` / `session_dir` が期待どおり
        *   `work/.tern` は作られない（または空）
    *   NEW `TestHandleCreateSession_ExplicitSessionDirKeepsLeaf`:
        *   明示 `session_dir=leaf` + `storage_root=root` → 正本は leaf、`storage_root` は root
    *   NEW / 更新 List: `TestHandleListSessions_ByStorageRoot`:
        *   `storage_root` にセッションを作り、`GET ?work_dir=work&storage_root=root` で見つかる
*   **Logic**: 仕様 VS1 / VS2 / VS5 / R7 / R8。

#### [MODIFY] [shared/libs/go/agentservice/handler.go](file://shared/libs/go/agentservice/handler.go)

*   **Description**: CreateSession に `storage_root` を追加し、パス解決順を更新。SendMessage の VendorHomeDir 呼び出しを `EffectiveStorageRoot` 経由に。
*   **Technical Design**:
    ```go
    var req struct {
        Agent       string `json:"agent"`
        Model       string `json:"model"`
        WorkDir     string `json:"work_dir"`
        Prompt      string `json:"prompt"`
        SessionDir  string `json:"session_dir"`
        ConfigDir   string `json:"config_dir"`
        StorageRoot string `json:"storage_root"`
    }
    ```
*   **Logic**（CreateSession）:
    1. `WorkDir` を Abs
    2. `StorageRoot = ResolveStorageRoot(req.StorageRoot, record.WorkDir)` を Abs して `record.StorageRoot` に保存
    3. `SessionDir` 空かつ StorageRoot 非空 → `CanonicalSessionDir(record.StorageRoot, record.ID)`
    4. `SessionDir` を Abs
    5. Debug ログに `storage_root` を追加
*   **Logic**（SendMessage 付近）:
    ```go
    root := EffectiveStorageRoot(record)
    if vh := VendorHomeDir(root, record.AgentName, record.SessionDir); vh != "" {
        opts = append(opts, codingagent.WithSessionDir(vh))
    }
    ```

### WorkspaceSessionStore / List

#### [MODIFY] [shared/libs/go/agentservice/workspace_session_store_test.go](file://shared/libs/go/agentservice/workspace_session_store_test.go)

*   **Description**: Create フォールバックが `storage_root` 基準であること、`ListByStorageRoot` のスキャンを検証。
*   **Technical Design**: Create で `StorageRoot` のみ設定・`SessionDir` 空 → `{storage_root}/.tern/{id}`。List は `storage_root` 引数で `.tern` をスキャン。
*   **Logic**: R3 / R8。

#### [MODIFY] [shared/libs/go/agentservice/workspace_session_store.go](file://shared/libs/go/agentservice/workspace_session_store.go)

*   **Description**: Create フォールバックと List スキャン基点を `storage_root` 対応に。
*   **Technical Design**:
    ```go
    type WorkDirSessionLister interface {
        ListByWorkDir(workDir string) ([]*codingagent.SessionRecord, error)
        ListByStorageRoot(storageRoot string) ([]*codingagent.SessionRecord, error)
    }
    ```
*   **Logic**:
    *   Create: `if SessionDir=="" { root := EffectiveStorageRoot(rec); if root!="" && ID!="" { SessionDir = CanonicalSessionDir(root, ID) } }`
    *   List: `ternDir := filepath.Join(absRoot, ".tern")` をスキャン（現行と同じ読込ロジック）

#### [MODIFY] [shared/libs/go/agentservice/handler_session.go](file://shared/libs/go/agentservice/handler_session.go)

*   **Description**: List に任意クエリ `storage_root` を追加。
*   **Technical Design**:
    *   `work_dir` は現行どおり必須（クライアント互換）
    *   `storage_root` 任意。指定時は `{storage_root}/.tern` をスキャン、未指定時は `{work_dir}/.tern`（現行）
*   **Logic**:
    ```go
    workDir := r.URL.Query().Get("work_dir")
    storageRoot := r.URL.Query().Get("storage_root")
    scanRoot := ResolveStorageRoot(storageRoot, workDir)
    // call ListByStorageRoot(scanRoot)
    ```

### Client / ternctl

#### [MODIFY] [client/v1/session.go](file://client/v1/session.go)

*   **Description**: `SessionRequest` / `SessionInfo` / `ListSessions` に `storage_root` を追加。
*   **Technical Design**:
    ```go
    type SessionRequest struct {
        Agent       string `json:"agent"`
        Model       string `json:"model,omitempty"`
        WorkDir     string `json:"work_dir"`
        StorageRoot string `json:"storage_root,omitempty"`
        SessionDir  string `json:"session_dir,omitempty"`
        ConfigDir   string `json:"config_dir,omitempty"`
    }
    // SessionInfo に StorageRoot string `json:"storage_root,omitempty"`
    // ListSessionsWithRoot(ctx, workDir, storageRoot string)
    ```
*   **Logic**: List は `storage_root` 非空時に `&storage_root=` を付与。

#### [MODIFY] [features/ternctl/main.go](file://features/ternctl/main.go)

*   **Description**: `--storage-root` フラグを追加し CreateSession に渡す。
*   **Technical Design**: `fs.String("storage-root", "", "Parent directory for .tern/.codex/.claude (default: work-dir)")`
*   **Logic**: 空なら JSON に含めない（サーバ側既定）。ヘルプ文言を更新。

### Integration / E2E tests

#### [MODIFY] [tests/llm_vendor_home_layout_test.go](file://tests/llm_vendor_home_layout_test.go)

*   **Description**: 既存テストは `storage_root` 省略時に現行どおり通ることを確認。NEW: `storage_root ≠ work_dir` で vendor/正本が同じ親に載るケース。
*   **Technical Design**:
    *   NEW `TestStorageRoot_SharedParentForHomes`:
        *   workDir / storageRoot を別 TempDir
        *   CreateSession JSON に `storage_root`
        *   SendMessage 後、adapter SessionDir = `{storageRoot}/.codex`
        *   正本 = `{storageRoot}/.tern/{id}`、`workDir/.tern` に record が無い
    *   NEW `TestStorageRoot_ExplicitSessionDir_VendorFromStorageRoot`:
        *   明示 leaf + storage_root → vendor は storage_root、正本は leaf
*   **Logic**: VS2 / VS5 / R10。

#### [MODIFY] [tests/llm_session_portability_test.go](file://tests/llm_session_portability_test.go)

*   **Description**: `portCreate` に任意 `storageRoot` を渡せるよう拡張（後方互換: 空文字で省略）。
*   **Technical Design**: JSON body に `storage_root` を条件付き追加。
*   **Logic**: 既存呼び出しは壊れないこと。

#### [NEW] [tests/common_storage_root_test.go](file://tests/common_storage_root_test.go)

*   **Description**: E2E — Create → List(`storage_root`) → Get でパス一貫性を検証（カテゴリ common）。
*   **Technical Design**:
    *   HTTP で CreateSession（`storage_root` 明示）
    *   GET session で `storage_root` / `session_dir` 確認
    *   GET `/api/v1/sessions?work_dir=...&storage_root=...` で一覧に含まれること
*   **Logic**: VS1 / VS2 / R8。

### Documentation

#### [MODIFY] [docs/ReferenceManual-WebAPIs.md](file://docs/ReferenceManual-WebAPIs.md)

*   **Description**: Create / List / Get に `storage_root` と用語区別を追記。
*   **Technical Design**（文言を計画に固定）:
    *   **Terms**:
        *   `storage_root`: parent of Agent Homes (default = `work_dir`)
        *   Agent Homes: `{storage_root}/.tern` (Wayfinder Home), `{storage_root}/.codex`, `{storage_root}/.claude`
        *   Canonical session folder: `{storage_root}/.tern/{session_id}` (= API `session_dir` when defaulted)
        *   `session_id`: HTTP session id; not part of Home path definition
    *   Create: `storage_root` Optional; default `work_dir`. `session_dir` Optional canonical leaf override; default `{storage_root}/.tern/{session_id}`.
    *   Env: `CODEX_HOME={storage_root}/.codex`, `CLAUDE_CONFIG_DIR={storage_root}/.claude`
    *   `config_dir` overlay target remains the Agent Home (derived from `storage_root`)
    *   List: `work_dir` required; optional `storage_root` selects scan root (`{storage_root}/.tern/*/record.json`)
*   **Logic**: R9 / VS6。

## Step-by-Step Implementation Guide

1. **[x] Path helpers (TDD)**: Add `storage_paths_test.go` (fail) then `storage_paths.go` (pass). Commit.
2. **[x] VendorHomeDir (TDD)**: Update `vendor_home_test.go` then `vendor_home.go`. Commit.
3. **[x] SessionRecord field**: Add `StorageRoot` to `session_store.go`. Commit.
4. **[x] Workspace store (TDD)**: Update store tests + `workspace_session_store.go` Create/List. Commit.
5. **[x] CreateSession / List / SendMessage (TDD)**: Update handler tests then `handler.go` + `handler_session.go`. Commit.
6. **[x] Client + ternctl**: Update `client/v1/session.go` and `features/ternctl/main.go`. Commit.
7. **[x] Integration/E2E**: Extend `portCreate` / vendor layout tests; add `tests/common_storage_root_test.go`. Commit.
8. **[x] Docs**: Update `docs/ReferenceManual-WebAPIs.md`. Commit.
9. **[x] Verify**: `./scripts/process/build.sh` then integration tests (see Verification Plan). Push on success.

## Verification Plan

### Automated Verification

1. **Build & Unit Tests**: `./scripts/process/build.sh`
2. **Integration Tests (targeted)**:
   ```bash
   ./scripts/process/integration_test.sh --categories common,llm --specify "StorageRoot|VendorHome|SessionDir|storage_root|TernSessionDir|CodexUses|ClaudeUses|WayfinderUses|RepeatedCodex"
   ```
3. **Integration Tests (broader session regression)**:
   ```bash
   ./scripts/process/integration_test.sh --categories common,llm --specify "session"
   ```
4. **E2E Tests**: `tests/common_storage_root_test.go`（上記 `--categories common` に含まれる）

> Windows (Git Bash): `xvfb-run` / `--skip-etc` は不要。Linux / Remote-SSH の場合のみ `build.sh --skip-etc` と `xvfb-run -a` ラップを適用する。

## Documentation

| 文書 | 更新内容 |
| :--- | :--- |
| `docs/ReferenceManual-WebAPIs.md` | Create/List/Get の `storage_root`、Home vs 正本 vs `session_id`、env マッピングを `storage_root` 基準に更新 |
| ternctl `--help` | `--storage-root` 説明 |
| 本ブランチ ideas 仕様 | 変更なし（既に候補 B 推奨） |
