# 001-ConfigDir-Switch-Same-Session

> **Source Specification**: [ideas/001-ConfigDir-Test-Coverage.md](file://prompts/phases/000-foundation/branches/feat-profiles/ideas/001-ConfigDir-Test-Coverage.md)  
> **Parent Specification (R8)**: [ideas/000-ConfigDir-Separate-From-SessionDir.md](file://prompts/phases/000-foundation/branches/feat-profiles/ideas/000-ConfigDir-Separate-From-SessionDir.md)  
> **Related implemented plan**: [plans/000-ConfigDir-Separate-From-SessionDir.md](file://prompts/phases/000-foundation/branches/feat-profiles/plans/000-ConfigDir-Separate-From-SessionDir.md) (Create-time `config_dir` / overlay 済み)

## Goal Description

同一 Tern `session_id` のまま `config_dir` を切り替える Web API を追加し、次の SendMessage から新しい skills/rules が overlay されつつ `session_dir` / `agent_session_id` による会話継続が保たれるようにする。Claude / Codex の両経路で integration と実 API キー LIVE E2E（課金許容）により命題を証明する。名前付き profile は非要件のまま。

## User Review Required

None. (承認済み 2026-08-05)

1. **エンドポイント形**: **Yes** — `PATCH /api/v1/sessions/:id` + body `{"config_dir":"..."}`
2. **空文字クリア**: **Yes** — overlay 無効に戻し、以降 Codex は `--ignore-user-config` 復帰
3. **名前付き profile**: **Yes** — 非要件のまま

## Step-by-Step Implementation Guide

1. [x] **Handler tests (PATCH)**: `handler_test.go` に PATCH ケース追加 → 失敗確認。
2. [x] **Extract validateAndResolveConfigDir**: CreateSession から共通化。
3. [x] **Implement handlePatchSession + route PATCH**: `handler.go` / `service.go`。
4. [x] **Client methods**: `UpdateSessionConfigDir` in `client` and `client/v1`。
5. [x] **ternctl**: `session-config` サブコマンド。
6. [x] **Docs**: ReferenceManual PATCH 節。
7. [x] **Integration Claude switch**: `TestAgentService_ConfigDir_SwitchSameSession_Claude`。
8. [x] **Integration Codex mock + switch**: Codex overlay mock + `..._Codex`。
9. [x] **Rename** `ReappliedOnResume` → `SameConfigDir_ReappliedOnSecondMessage`。
10. [x] **Fix E2E helper calls**: Shared/Lane から直接 Apply を除去。
11. [x] **LIVE E2E**: Claude + Codex switch tests with `RUN_CONFIG_DIR_LIVE=1`。
12. [x] **Verify**: build + specify mock tests + LIVE with env flag。
13. [x] **Commit / push** after green (execute phase rules).

## Requirement Traceability

| Requirement (from Spec) | Implementation Point |
| :--- | :--- |
| 001 R1 / 000 R8: 同一 session_id で config_dir 更新 API | Proposed Changes > agentservice route + handler |
| 001 R1: 更新後メッセージで新 overlay、session_dir / agent_session_id 維持 | handler Update + 既存 SendMessage の WithConfigDir |
| 001 R2: 同一 config 複ターン | tests (rename/keep Reapplied) |
| 001 R3: 差の証明 (FS + GET + 命題経路) | integration + LIVE |
| 001 R4 / R5: Claude / Codex 対称 | integration mocks + LIVE |
| 001 R6: client / ternctl / Reference Manual | client, ternctl, docs |
| 001 R7: 実 API キー LIVE | tests/agentservice_e2e_test.go |
| 001 Out of Scope: named profile | 実装しない |
| E2E helper 直接呼び出し禁止 | 既存 TestE2E_ConfigDir_* 修正 |

## Proposed Changes

### agentservice — 更新 API (000 R8 / 001 R6)

#### [MODIFY] [shared/libs/go/agentservice/handler_test.go](file://shared/libs/go/agentservice/handler_test.go)

*   **Description**: PATCH config_dir の単体テストを先に追加 (TDD)。
*   **Technical Design**:
    ```go
    func TestHandlePatchSession_ConfigDir(t *testing.T) {
        // Create with config_dir=alpha (TempDir)
        // PATCH {"config_dir": betaTempDir} → 200
        // GET: config_dir == abs(beta), session_dir / work_dir unchanged, id same
    }

    func TestHandlePatchSession_ConfigDirMissing(t *testing.T) {
        // PATCH nonexistent path → 400 "config_dir does not exist"
    }

    func TestHandlePatchSession_ConfigDirClear(t *testing.T) {
        // PATCH {"config_dir":""} → 200, GET config_dir empty/omitted
    }

    func TestHandlePatchSession_NotFound(t *testing.T) {
        // PATCH unknown id → 404
    }

    func TestHandlePatchSession_MethodOnSessionRoot(t *testing.T) {
        // Ensure PATCH is accepted on /api/v1/sessions/{id} (not only GET/DELETE)
    }
    ```

#### [MODIFY] [shared/libs/go/agentservice/service.go](file://shared/libs/go/agentservice/service.go)

*   **Description**: `routeSessionByID` でセッションルートに `PATCH` を追加。
*   **Logic**:
    ```go
    } else {
        switch r.Method {
        case http.MethodGet:
            s.handleGetSession(w, r)
        case http.MethodPatch:
            s.handlePatchSession(w, r)
        case http.MethodDelete:
            s.handleDeleteSession(w, r)
        default:
            http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
        }
    }
    ```
*   `/messages` 等のサフィックス経路は変更しない。

#### [MODIFY] [shared/libs/go/agentservice/handler.go](file://shared/libs/go/agentservice/handler.go)

*   **Description**: Create 時の config_dir 検証を共通化し、PATCH ハンドラを追加。
*   **Technical Design**:
    ```go
    // validateAndResolveConfigDir returns absolute path or "" when input is empty.
    // Non-empty invalid paths return ( "", httpStatus, errorMessage ).
    func validateAndResolveConfigDir(configDir string) (resolved string, status int, errMsg string)

    // handlePatchSession handles PATCH /api/v1/sessions/:id
    // Request body:
    //   {"config_dir": "/path/to/beta"}   // set / replace
    //   {"config_dir": ""}                // clear (overlay disabled)
    // Response 200: full SessionRecord JSON (same shape as GET)
    // Errors: 404 session not found; 400 invalid config_dir
    func (s *Server) handlePatchSession(w http.ResponseWriter, r *http.Request)
    ```
*   **Logic** (`handlePatchSession`):
    1. `id := extractPathParam(r.URL.Path, "/api/v1/sessions/")`（サフィックス無しパスのみ。`routeSessionByID` が保証）
    2. `record, err := s.sessions.Get(id)` → 無ければ 404
    3. Decode body:
       ```go
       var req struct {
           ConfigDir *string `json:"config_dir"`
       }
       ```
       - `ConfigDir == nil` → 400 `"config_dir is required"`（他フィールド更新は本計画対象外）
    4. `resolved, status, errMsg := validateAndResolveConfigDir(*req.ConfigDir)`
       - status != 0 → その status / errMsg で return
    5. `record.ConfigDir = resolved`（空文字クリア可）
    6. `record.UpdatedAt = time.Now()`（フィールドが無ければ追加しない。既存 UpdatedAt があれば更新）
    7. `s.sessions.Update(record)`
    8. Debug log: `session_id`, old/new `config_dir`, unchanged `session_dir`
    9. 200 + `json.Encode(record)`
*   **Logic** (`validateAndResolveConfigDir`): CreateSession 内の既存チェックを抽出して共有。
    ```go
    if configDir == "" {
        return "", 0, ""
    }
    abs via filepath.Abs
    os.Stat → not exist → 400 "config_dir does not exist: ..."
    !IsDir → 400 "config_dir is not a directory: ..."
    return abs, 0, ""
    ```
*   **注意**: PATCH 時点では CLI 起動・overlay は行わない。次の `handleSendMessage` が既に `record.ConfigDir` を `WithConfigDir` に渡す実装済みであることを確認し、不足があれば修正する（現状あり）。

### Client / ternctl / Docs (001 R6)

#### [MODIFY] [client/v1/session.go](file://client/v1/session.go) / [client/session.go](file://client/session.go)

*   **Technical Design**:
    ```go
    // UpdateSessionConfigDir sets config_dir on an existing session (PATCH).
    // Pass empty configDir to clear.
    func (c *Client) UpdateSessionConfigDir(ctx context.Context, sessionID, configDir string) (map[string]any, error)

    // Optional convenience on Session:
    func (s *Session) UpdateConfigDir(ctx context.Context, configDir string) (map[string]any, error)
    ```
*   **Logic**: `PATCH {base}/api/v1/sessions/{id}` with `Content-Type: application/json`, body `{"config_dir": configDir}`。200 以外は error。レスポンスを map または SessionRecord 相当で返す。

#### [MODIFY] [features/ternctl/main.go](file://features/ternctl/main.go)

*   **Description**: 既存セッションの config 更新サブコマンドまたは `run` 補助フラグ。
*   **Logic** (推奨: 専用サブコマンド):
    ```text
    ternctl session-config --id SESSION_ID --config-dir DIR
    ternctl session-config --id SESSION_ID --config-dir ""   # clear
    ```
    内部で `UpdateSessionConfigDir` を呼ぶ。ヘルプに「次のメッセージから新 config が適用される」と書く。

#### [MODIFY] [docs/ReferenceManual-WebAPIs.md](file://docs/ReferenceManual-WebAPIs.md)

*   **Description**: Create 節に加え、PATCH 節を追加。
*   **Logic** (転記する仕様文言):
    - Method `PATCH`, Path `/api/v1/sessions/:id`
    - Body: `config_dir` (string, required in body; empty string clears)
    - Does not change `work_dir`, `session_dir`, or `agent_session_id`
    - Overlay of the new config runs on the **next** message send (agent process start)
    - Same-session lane/profile switching is supported via this endpoint; named `profile` resolution is out of scope

### Integration tests — 命題経路 (001 R1–R5)

#### [MODIFY] [tests/agentservice_integration_test.go](file://tests/agentservice_integration_test.go)

*   **Description**: Claude / Codex 両 mock で切替継続を検証。既存 helper 直接呼び出し依存を避ける。
*   **Technical Design**:
    ```go
    // Codex allowlist overlay mock (do NOT reuse Claude-only allowlist helper alone)
    type configDirOverlayMockCodex struct{ name string }
    func (a *configDirOverlayMockCodex) CreateSession(..., opts...) {
        cfg := codingagent.NewSessionConfig(opts...)
        codingagent.ApplyDefaults(cfg, &codingagent.AdapterConfig{AgentName: a.name})
        if err := codingagent.OverlayConfigDir(cfg.SessionDir, cfg.ConfigDir, []string{
            "skills", "rules", "config.toml", "AGENTS.md",
        }); err != nil && cfg.ConfigDir != "" {
            return nil, err
        }
        return &integrationMockSession{}, nil
    }

    func TestAgentService_ConfigDir_SwitchSameSession_Claude(t *testing.T) {
        // server with configDirOverlayMockAgent (existing Claude allowlist)
        // alpha/beta TempDirs with skills/alpha/SKILL.md and skills/beta/SKILL.md
        // Create config_dir=alpha, session_dir=S
        // postSessionMessage → alpha skill exists under S
        // PATCH config_dir=beta
        // GET: config_dir=beta, session_dir=S, id same; capture agent_session_id if set
        // postSessionMessage → beta skill exists; alpha skill path absent or replaced
        // GET agent_session_id unchanged if previously set (mock may set via EventSystem — if mock ID stable, assert equal)
    }

    func TestAgentService_ConfigDir_SwitchSameSession_Codex(t *testing.T) {
        // same flow with configDirOverlayMockCodex registered as "codex"
        // Create agent=codex
    }

    func TestAgentService_ConfigDir_SameConfigDir_ReappliedOnSecondMessage(t *testing.T) {
        // rename from TestAgentService_ConfigDir_ReappliedOnResume
        // clarify: NOT a config switch test
    }

    func TestAgentServicePatchSession_ConfigDir(t *testing.T) {
        // API-level PATCH via httptest (optional if handler_test covers enough)
    }
    ```

### E2E — helper 除去 + LIVE (001 R7)

#### [MODIFY] [tests/agentservice_e2e_test.go](file://tests/agentservice_e2e_test.go)

*   **Description**:
    1. 既存 `TestE2E_ConfigDir_SharedAcrossSessions` / `LaneIsolation` から `claudecode.ApplyClaudeConfigDir` 直接呼び出しを削除。overlay 証明は SendMessage 後の FS、または integration に委譲して E2E は永続化のみにする。
    2. LIVE テストを追加（実キー・課金あり・skip は受け入れ未完了）。
*   **Technical Design**:
    ```go
    // Marker file content unique per lane, e.g. CLAUDE.md or skills/<lane>/SKILL.md
    // Prompt asks agent to reply with the exact marker token only.

    func TestE2E_ConfigDir_Live_Claude_SwitchSameSession(t *testing.T) {
        // Requires: claude CLI, vault API key (same as other E2E)
        // Fail (not skip) if prerequisites missing when RUN_CONFIG_DIR_LIVE=1
        // Or: if LookPath/vault missing → t.Fatal with message "acceptance incomplete"
        // Per review: skip-only is NOT acceptance. Prefer:
        //   if os.Getenv("RUN_CONFIG_DIR_LIVE") != "1" { t.Skip("set RUN_CONFIG_DIR_LIVE=1 for paid live acceptance") }
        //   else { require CLI+key or Fatal }
        // Flow LIVE-1..5 for claudecode
    }

    func TestE2E_ConfigDir_Live_Codex_SwitchSameSession(t *testing.T) {
        // Same for agent=codex; require codex CLI
    }
    ```
*   **Logic** (LIVE フロー・仕様転記):
    1. LIVE-1: alpha config_dir Create（一意マーカー `TERN_CFG_ALPHA_<rand>` を `CLAUDE.md` または Codex 側 `AGENTS.md` / skill に書く）
    2. LIVE-2: SendMessage 短いプロンプトでマーカーを答えさせる。HTTP 200、応答にマーカーまたは成功完了
    3. LIVE-3: PATCH config_dir=beta（マーカー `TERN_CFG_BETA_<rand>`）。GET で beta、`session_id`/`session_dir`/`agent_session_id` 維持
    4. LIVE-4: SendMessage で beta マーカー。`agent_session_id` が LIVE-2 後と同じ
    5. LIVE-5: `session_dir` 上に beta 由来ファイルが見える（allowlist overlay）
*   **環境変数**: `RUN_CONFIG_DIR_LIVE=1` のときのみ課金 LIVE を実行。計画の受け入れは当該フラグ付き実行の成功を要求する。

### Codex unit (維持・必要なら補強)

#### [MODIFY] [shared/libs/go/codingagent/codex/process_test.go](file://shared/libs/go/codingagent/codex/process_test.go)

*   **Description**: 既存 `TestCodexBuildArgs_WithConfigDirDisablesIgnoreUserConfig` を維持。クリア後に ignore が戻ることは、空 ConfigDir で `BuildArgs(..., true)` を呼ぶ StartProcess 分岐の単体で担保済みであることをコメントで明記。追加ケース任意:
    ```go
    // When ConfigDir == "", ignoreUserConfig == true (documented in StartProcess)
    ```

## Step-by-Step Implementation Guide

1. **Handler tests (PATCH)**: `handler_test.go` に PATCH ケース追加 → 失敗確認。
2. **Extract validateAndResolveConfigDir**: CreateSession から共通化。
3. **Implement handlePatchSession + route PATCH**: `handler.go` / `service.go`。
4. **Client methods**: `UpdateSessionConfigDir` in `client` and `client/v1`。
5. **ternctl**: `session-config` サブコマンド。
6. **Docs**: ReferenceManual PATCH 節。
7. **Integration Claude switch**: `TestAgentService_ConfigDir_SwitchSameSession_Claude`。
8. **Integration Codex mock + switch**: Codex overlay mock + `..._Codex`。
9. **Rename** `ReappliedOnResume` → `SameConfigDir_ReappliedOnSecondMessage`。
10. **Fix E2E helper calls**: Shared/Lane から直接 Apply を除去。
11. **LIVE E2E**: Claude + Codex switch tests with `RUN_CONFIG_DIR_LIVE=1`。
12. **Verify**: build + specify mock tests + LIVE with env flag。
13. **Commit / push** after green (execute phase rules).

## Verification Plan

### Automated Verification

1. **Build & Unit Tests**:
   ```bash
   ./scripts/process/build.sh
   ```

2. **Integration (mock / API / 命題)**:
   ```bash
   ./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestHandlePatchSession|TestAgentService_ConfigDir|TestAgentServiceCreateSession_ConfigDir|TestApplyClaudeConfigDir|TestApplyCodexConfigDir|TestCodexBuildArgs"
   ```

3. **E2E LIVE (課金あり・受け入れ必須)**:
   ```bash
   RUN_CONFIG_DIR_LIVE=1 ./scripts/process/integration_test.sh --specify "TestE2E_ConfigDir_Live"
   ```
   - キー / CLI 不足時は Skip ではなく、フラグ ON 時は Fatal（受け入れ未完了）。
   - Linux / Remote-SSH Linux: `build.sh --skip-etc`、integration は `xvfb-run -a` ラップ。

### Verification mapping

| Spec scenario | Test |
| :--- | :--- |
| P 命題 Claude | `TestAgentService_ConfigDir_SwitchSameSession_Claude`, `TestE2E_ConfigDir_Live_Claude_SwitchSameSession` |
| P 命題 Codex | `TestAgentService_ConfigDir_SwitchSameSession_Codex`, `TestE2E_ConfigDir_Live_Codex_SwitchSameSession` |
| A 同一 config 複ターン | `TestAgentService_ConfigDir_SameConfigDir_ReappliedOnSecondMessage` |
| B 別 session 並列 | 既存 SharedAcrossSessions / LaneIsolation（helper 除去後） |
| R8 API | `TestHandlePatchSession_*` |

## Documentation

- [MODIFY] `docs/ReferenceManual-WebAPIs.md` — PATCH `/api/v1/sessions/:id`、空文字クリア、次回メッセージで overlay、named profile 非対応を明記。
- 親仕様 / 本計画の User Review: エンドポイント形と空文字クリアは本計画の推奨で実装。異議があれば実行前に指示。
