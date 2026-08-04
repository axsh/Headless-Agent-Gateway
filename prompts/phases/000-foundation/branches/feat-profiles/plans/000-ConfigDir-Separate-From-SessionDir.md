# 000-ConfigDir-Separate-From-SessionDir

> **Source Specification**: [ideas/000-ConfigDir-Separate-From-SessionDir.md](file://prompts/phases/000-foundation/branches/feat-profiles/ideas/000-ConfigDir-Separate-From-SessionDir.md)

## Goal Description

`POST /api/v1/sessions` に任意フィールド `config_dir` を追加し、会話永続化ルート (`session_dir` → `CLAUDE_CONFIG_DIR` / `CODEX_HOME`) と設定ソース (rules / skills) を直交させる。CLI は両者を単一ルートに持つため、`config_dir` 指定時のみ Tern が許可リスト方式で `config_dir` → `session_dir` へオーバーレイする。省略時は現行互換 (オーバーレイなし、Codex は `--ignore-user-config` 維持)。

## User Review Required

None. (以下はレビューで承認済み)

1. **コンテナ運用**: **Yes** — `config_dir` / `session_dir` / `work_dir` は Tern プロセスから到達可能なパスであること、およびコンテナ利用時の注意をドキュメントに明記する。
2. **任意要件 O1 / O3**: **Yes** — 本計画では実装しない (後続 Issue)。
3. **Claude skills 配置規約**: **Yes** — `config_dir` は完全に別フォルダに設定が置かれる前提。オーバーレイ先は `$CLAUDE_CONFIG_DIR/skills/` および `$CLAUDE_CONFIG_DIR/settings.json` (CLAUDE_CONFIG_DIR = `~/.claude` 置換)。既存のプロジェクト `.claude/` 配下へのネスト収容や、そのための機能的サポートは行わない。

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: CreateSession に任意 `config_dir`、省略時互換、絶対パス化、不正パスは 400 | Proposed Changes > agentservice/handler.go |
| R2: SessionRecord / GET に `config_dir`、再開時再利用 | Proposed Changes > codingagent/session_store.go, agentservice/handler.go |
| R3: SessionConfig.ConfigDir / WithConfigDir / ApplyDefaults 絶対パス化のみ | Proposed Changes > codingagent/options.go |
| R4: Claude は CLAUDE_CONFIG_DIR=session_dir 維持、指定時のみ overlay、work_dir 非注入 | Proposed Changes > codingagent/config_overlay.go, claudecode/* |
| R5: Codex は CODEX_HOME=session_dir 維持、指定時 overlay と ignore-user-config 見直し、`-c` 維持 | Proposed Changes > codex/process.go, codex/adapter.go, config_overlay |
| R6: client / client/v1 / ternctl / ReferenceManual | Proposed Changes > client/*, features/ternctl, docs |
| R7: 同一 config_dir + 別 session_dir、起動ごとの再 overlay | Proposed Changes > overlay + E2E |
| O2: Wayfinder はレコード保存のみ (適用 no-op) | Proposed Changes > agentservice (保存のみ)、wayfinder 変更なし |
| O1 / O3 | 本計画対象外 |

## Proposed Changes

### codingagent 共通 (R3, R4/R5 overlay 基盤)

#### [MODIFY] [shared/libs/go/codingagent/options_test.go](file://shared/libs/go/codingagent/options_test.go)

*   **Description**: `WithConfigDir` と `ApplyDefaults` の ConfigDir 絶対パス化テストを追加 (TDD Failed First)。
*   **Technical Design**:
    ```go
    // TestSessionOptionFunctions に追加
    {
        name: "WithConfigDir",
        opt:  codingagent.WithConfigDir("/mnt/config-sets/alpha"),
        check: func(t *testing.T, cfg *codingagent.SessionConfig) {
            if cfg.ConfigDir != "/mnt/config-sets/alpha" {
                t.Errorf("ConfigDir = %v, want /mnt/config-sets/alpha", cfg.ConfigDir)
            }
        },
    },

    // TestApplyDefaults に追加
    t.Run("empty ConfigDir stays empty (no fallback)", func(t *testing.T) {
        cfg := codingagent.NewSessionConfig(codingagent.WithWorkDir(t.TempDir()))
        codingagent.ApplyDefaults(cfg, &codingagent.AdapterConfig{AgentName: "claudecode"})
        if cfg.ConfigDir != "" {
            t.Errorf("ConfigDir = %q, want empty", cfg.ConfigDir)
        }
    })

    t.Run("relative ConfigDir resolved to absolute", func(t *testing.T) {
        cfg := codingagent.NewSessionConfig(codingagent.WithConfigDir("rel-config"))
        codingagent.ApplyDefaults(cfg, &codingagent.AdapterConfig{})
        if !filepath.IsAbs(cfg.ConfigDir) {
            t.Errorf("ConfigDir should be absolute, got %q", cfg.ConfigDir)
        }
    })

    t.Run("absolute ConfigDir unchanged in value after Abs", func(t *testing.T) {
        abs := filepath.Join(t.TempDir(), "alpha")
        cfg := codingagent.NewSessionConfig(codingagent.WithConfigDir(abs))
        codingagent.ApplyDefaults(cfg, &codingagent.AdapterConfig{})
        if cfg.ConfigDir != abs {
            // Abs may clean separators; compare with filepath.Clean
            if filepath.Clean(cfg.ConfigDir) != filepath.Clean(abs) {
                t.Errorf("ConfigDir = %q, want %q", cfg.ConfigDir, abs)
            }
        }
    })
    ```

#### [MODIFY] [shared/libs/go/codingagent/options.go](file://shared/libs/go/codingagent/options.go)

*   **Description**: `SessionConfig` に `ConfigDir` を追加し、オプションと ApplyDefaults を拡張する。
*   **Technical Design**:
    ```go
    type SessionConfig struct {
        // ... existing fields ...
        SessionDir string

        // ConfigDir is an optional source directory for agent config assets
        // (skills, rules, settings). When set, adapters overlay allowlisted
        // entries into SessionDir before launch. Empty means disabled
        // (backward compatible: no overlay).
        ConfigDir string

        VFSMounts []VFSMount
        // ...
    }

    func WithConfigDir(dir string) SessionOption {
        return func(c *SessionConfig) { c.ConfigDir = dir }
    }
    ```
*   **Logic** (`ApplyDefaults` 末尾付近、SessionDir 絶対パス化の直後):
    ```go
    // ConfigDir: absolute path only when non-empty. No fallback when empty.
    if cfg.ConfigDir != "" {
        if abs, err := filepath.Abs(cfg.ConfigDir); err == nil {
            cfg.ConfigDir = abs
        }
    }
    ```

#### [NEW] [shared/libs/go/codingagent/config_overlay_test.go](file://shared/libs/go/codingagent/config_overlay_test.go)

*   **Description**: 許可リスト overlay の単体テスト (symlink 成功 / copy フォールバック / 保護エントリ非破壊 / 空 allowlist / 再適用)。
*   **Technical Design** (テーブル駆動):
    ```go
    func TestOverlayConfigDir(t *testing.T) {
        // Case: overlay skills/ and settings.json; leave projects/ untouched
        // Case: destination projects/ with marker file survives
        // Case: missing allowlisted source entries are skipped (not error)
        // Case: re-apply updates symlink/copy for allowlisted names
        // Case: empty configDir or empty sessionDir returns error
    }

    func TestLinkOrCopy(t *testing.T) {
        // Prefer symlink; if symlink fails (simulate or OS-specific), copy works
        // Directory recursive copy preserves nested SKILL.md
    }
    ```

#### [NEW] [shared/libs/go/codingagent/config_overlay.go](file://shared/libs/go/codingagent/config_overlay.go)

*   **Description**: アダプタ共通の overlay 実装。
*   **Technical Design**:
    ```go
    package codingagent

    // OverlayConfigDir copies or symlinks allowlisted names from configDir
    // into sessionDir. Only entries that exist under configDir are applied.
    // Existing session-only data outside the allowlist is never removed.
    // Protected names under sessionDir (e.g. "projects", "sessions") are
    // never deleted by this function even if they appear in allowlist by mistake.
    func OverlayConfigDir(sessionDir, configDir string, allowlist []string) error

    // linkOrCopy creates dst as a symlink to src when possible; otherwise
    // recursively copies src to dst. If dst exists, it is removed first
    // only when it is itself a symlink or a previous overlay target for
    // that allowlisted name (os.RemoveAll on that single basename).
    func linkOrCopy(src, dst string) error

    // Default protected basenames (never removed as side effect):
    // "projects", "sessions", "statsig", "debug", "logs", "tmp", "cache"
    ```
*   **Logic**:
    1. `sessionDir` / `configDir` が空なら `fmt.Errorf`
    2. `os.Stat(configDir)` がディレクトリでないなら error
    3. `os.MkdirAll(sessionDir, 0755)`
    4. 各 `name` in allowlist:
       - `src := filepath.Join(configDir, name)` が存在しなければ continue
       - `dst := filepath.Join(sessionDir, name)`
       - basename が protected 集合に含まれかつ dst が既にディレクトリで中身がある場合は error ではなく skip + 警告は呼び出し側ログ (関数は protected 名を allowlist から無視する安全策でも可)
       - `linkOrCopy(src, dst)`
    5. 起動のたびに再適用 (R7): 同一 allowlisted basename は毎回差し替え

### Claude Code アダプタ (R4)

#### [NEW] [shared/libs/go/codingagent/claudecode/config_overlay_test.go](file://shared/libs/go/codingagent/claudecode/config_overlay_test.go)

*   **Description**: Claude 用 allowlist 定数と `ApplyClaudeConfigDir` のテスト。
*   **Logic**:
    - allowlist = `[]string{"skills", "rules", "CLAUDE.md", "settings.json"}`
    - fixture: `configDir/skills/demo/SKILL.md`, `configDir/settings.json`
    - overlay 後 `sessionDir/skills/demo/SKILL.md` が読める
    - 事前に置いた `sessionDir/projects/marker` が残る
    - `ConfigDir==""` のとき Apply は no-op (nil)

#### [NEW] [shared/libs/go/codingagent/claudecode/config_overlay.go](file://shared/libs/go/codingagent/claudecode/config_overlay.go)

*   **Description**: Claude 向けラッパ。
*   **Technical Design**:
    ```go
    var claudeConfigAllowlist = []string{
        "skills", "rules", "CLAUDE.md", "settings.json",
    }

    // ApplyClaudeConfigDir overlays configDir into sessionDir when configDir != "".
    // Does not write into work_dir.
    func ApplyClaudeConfigDir(sessionDir, configDir string) error {
        if configDir == "" {
            return nil
        }
        return codingagent.OverlayConfigDir(sessionDir, configDir, claudeConfigAllowlist)
    }
    ```

#### [MODIFY] [shared/libs/go/codingagent/claudecode/adapter.go](file://shared/libs/go/codingagent/claudecode/adapter.go)

*   **Description**: CreateSession で StartProcess 前に overlay を実行。
*   **Logic**:
    ```go
    a.logger.Debug("creating claude code session",
        "model", cfg.Model, "work_dir", cfg.WorkDir,
        "session_dir", cfg.SessionDir, "config_dir", cfg.ConfigDir)

    if err := ApplyClaudeConfigDir(cfg.SessionDir, cfg.ConfigDir); err != nil {
        return nil, fmt.Errorf("claudecode: apply config_dir: %w", err)
    }

    ch, pm, err := StartProcess(ctx, a.config, cfg)
    ```
*   **注意**: `BuildEnv` の `CLAUDE_CONFIG_DIR = SessionDir` は変更しない。

### Codex アダプタ (R5)

#### [MODIFY] [shared/libs/go/codingagent/codex/process_test.go](file://shared/libs/go/codingagent/codex/process_test.go)

*   **Description**: `BuildArgs` の ignore-user-config 条件テストを追加・既存テストをシグネチャ変更に追従。
*   **Technical Design**:
    ```go
    // 既存 TestCodexBuildArgs: BuildArgs(prompt, overrides, true /* ignoreUserConfig */)
    // が "--ignore-user-config" を含むことを維持

    func TestCodexBuildArgs_WithConfigDirDisablesIgnoreUserConfig(t *testing.T) {
        args := codex.BuildArgs("hi", nil, false)
        for _, a := range args {
            if a == "--ignore-user-config" {
                t.Fatal("ignore-user-config must be omitted when config_dir is active")
            }
        }
    }

    func TestCodexBuildEnv_SessionDirStillSetsCODEX_HOME(t *testing.T) {
        // ConfigDir の有無に関わらず CODEX_HOME=SessionDir
    }
    ```

#### [NEW] [shared/libs/go/codingagent/codex/config_overlay_test.go](file://shared/libs/go/codingagent/codex/config_overlay_test.go)

*   **Description**: Codex allowlist overlay テスト。
*   **Logic**:
    - allowlist = `[]string{"skills", "rules", "config.toml", "AGENTS.md"}`
    - `sessionDir/sessions/keep` が残ること
    - overlay 後 `sessionDir/skills/...` と任意の `config.toml` が存在

#### [NEW] [shared/libs/go/codingagent/codex/config_overlay.go](file://shared/libs/go/codingagent/codex/config_overlay.go)

*   **Technical Design**:
    ```go
    var codexConfigAllowlist = []string{
        "skills", "rules", "config.toml", "AGENTS.md",
    }

    func ApplyCodexConfigDir(sessionDir, configDir string) error {
        if configDir == "" {
            return nil
        }
        return codingagent.OverlayConfigDir(sessionDir, configDir, codexConfigAllowlist)
    }
    ```

#### [MODIFY] [shared/libs/go/codingagent/codex/process.go](file://shared/libs/go/codingagent/codex/process.go)

*   **Description**: `BuildArgs` に ignore フラグを追加。`StartProcess` は `cfg.ConfigDir != ""` のとき false。
*   **Technical Design**:
    ```go
    // BuildArgs constructs codex CLI arguments.
    // When ignoreUserConfig is true, appends --ignore-user-config (legacy default).
    // When false (config_dir active), omits it so overlayed $CODEX_HOME/config.toml
    // and skills under CODEX_HOME can load; -c overrides still win for Tern keys.
    func BuildArgs(prompt string, configOverrides []string, ignoreUserConfig bool) []string {
        args := []string{
            "exec",
            "--json",
            "--dangerously-bypass-approvals-and-sandbox",
        }
        if ignoreUserConfig {
            args = append(args, "--ignore-user-config")
        }
        args = append(args, configOverrides...)
        if prompt != "" {
            args = append(args, "-")
        }
        return args
    }
    ```
*   **Logic** (`StartProcess` 内):
    ```go
    ignoreUserConfig := cfg.ConfigDir == ""
    args := BuildArgs(cfg.Prompt, configOverrides, ignoreUserConfig)
    // BuildEnv unchanged: CODEX_HOME = cfg.SessionDir when SessionDir != ""
    ```

#### [MODIFY] [shared/libs/go/codingagent/codex/adapter.go](file://shared/libs/go/codingagent/codex/adapter.go)

*   **Description**: StartProcess 前に `ApplyCodexConfigDir`。
*   **Logic**:
    ```go
    if err := ApplyCodexConfigDir(cfg.SessionDir, cfg.ConfigDir); err != nil {
        return nil, fmt.Errorf("codex: apply config_dir: %w", err)
    }
    // existing WriteConfigTOML temp home remains fallback only when
    // SessionDir/CODEX_HOME not set via BuildEnv; SessionDir 指定時は
    // 現行どおり env の CODEX_HOME が優先される (変更なし)。
    ch, pm, err := StartProcess(...)
    ```

### agentservice (R1, R2, O2)

#### [MODIFY] [shared/libs/go/agentservice/handler_test.go](file://shared/libs/go/agentservice/handler_test.go)

*   **Description**: CreateSession / GetSession の `config_dir` テスト (TDD)。
*   **Technical Design**:
    ```go
    func TestHandleCreateSession_WithConfigDir(t *testing.T) {
        // config_dir = 実在する TempDir
        // POST 201, GET で config_dir が絶対パスで返る
    }

    func TestHandleCreateSession_ConfigDirMissing(t *testing.T) {
        // config_dir = 存在しないパス → 400, body に判別可能な文言
        // 例: "config_dir does not exist" / "config_dir is not a directory"
    }

    func TestHandleCreateSession_ConfigDirOmitted(t *testing.T) {
        // 省略時 201、GET の config_dir は "" または欠落 (omitempty)
        // session_dir フォールバックは現行どおり
    }

    func TestHandleCreateSession_ConfigDirRelativeResolved(t *testing.T) {
        // 相対パスを渡し、GET で絶対パスになっていること
    }
    ```

#### [MODIFY] [shared/libs/go/codingagent/session_store.go](file://shared/libs/go/codingagent/session_store.go)

*   **Description**: `SessionRecord` にフィールド追加。
*   **Technical Design**:
    ```go
    type SessionRecord struct {
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
    ```

#### [MODIFY] [shared/libs/go/agentservice/handler.go](file://shared/libs/go/agentservice/handler.go)

*   **Description**: リクエスト受理・検証・永続化・実行時 `WithConfigDir`。
*   **Technical Design** (CreateSession リクエスト):
    ```go
    var req struct {
        Agent      string `json:"agent"`
        Model      string `json:"model"`
        WorkDir    string `json:"work_dir"`
        Prompt     string `json:"prompt"`
        SessionDir string `json:"session_dir"`
        ConfigDir  string `json:"config_dir"`
    }
    ```
*   **Logic** (SessionDir 絶対パス化の後):
    ```go
    record.ConfigDir = req.ConfigDir
    if record.ConfigDir != "" {
        if abs, err := filepath.Abs(record.ConfigDir); err == nil {
            record.ConfigDir = abs
        }
        fi, err := os.Stat(record.ConfigDir)
        if err != nil {
            if os.IsNotExist(err) {
                http.Error(w, "config_dir does not exist: "+record.ConfigDir, http.StatusBadRequest)
                return
            }
            http.Error(w, "config_dir stat failed: "+err.Error(), http.StatusBadRequest)
            return
        }
        if !fi.IsDir() {
            http.Error(w, "config_dir is not a directory: "+record.ConfigDir, http.StatusBadRequest)
            return
        }
    }
    // log config_dir alongside work_dir / session_dir
    ```
*   **Logic** (handleSendMessage の opts 組み立て、SessionDir の直後):
    ```go
    if record.ConfigDir != "" {
        opts = append(opts, codingagent.WithConfigDir(record.ConfigDir))
    }
    ```
*   **O2**: Wayfinder も同じレコード / opts 経由で `ConfigDir` が渡るが、Wayfinder アダプタは未使用でよい (フィールド保存のみで要件充足)。必要なら wayfinder CreateSession 先頭で `if cfg.ConfigDir != "" { log.Info("config_dir ignored for wayfinder") }` を1行追加してもよいが必須ではない。

### Client / ternctl (R6)

#### [MODIFY] [client/v1/session.go](file://client/v1/session.go) / [client/session.go](file://client/session.go)

*   **Technical Design**:
    ```go
    type SessionRequest struct {
        Agent      string `json:"agent"`
        Model      string `json:"model,omitempty"`
        WorkDir    string `json:"work_dir"`
        SessionDir string `json:"session_dir,omitempty"`
        ConfigDir  string `json:"config_dir,omitempty"`
    }
    ```

#### [MODIFY] [features/ternctl/main.go](file://features/ternctl/main.go)

*   **Description**: `run` に `--config-dir` を追加し `SessionRequest.ConfigDir` へ渡す。ヘルプ文も更新。
*   **Logic**:
    ```go
    configDir := fs.String("config-dir", "", "Agent config set directory (skills/rules); overlaid into session-dir")
    // ...
    session, err = c.CreateSession(ctx, client.SessionRequest{
        Agent:      *agent,
        Model:      *model,
        WorkDir:    *workDir,
        SessionDir: *sessionDir,
        ConfigDir:  *configDir,
    })
    ```

### Integration / E2E (R1, R2, R7, シナリオ1–4)

#### [MODIFY] [tests/agentservice_integration_test.go](file://tests/agentservice_integration_test.go)

*   **Description**: モックエージェントでの API レベル検証。
*   **Technical Design**:
    ```go
    func TestAgentServiceCreateSession_ConfigDirPersisted(t *testing.T) { ... }
    func TestAgentServiceCreateSession_ConfigDirInvalid(t *testing.T) { ... }
    ```

#### [MODIFY] [tests/agentservice_e2e_test.go](file://tests/agentservice_e2e_test.go)

*   **Description**: 実サーバ向け E2E (CLI 実起動が重い場合はレコード + overlay 副作用のファイルシステム検証までを必須とし、実 CLI skill 解決は環境依存で skip 可)。
*   **Technical Design**:
    ```go
    // シナリオ1
    func TestE2E_ConfigDirOmitted_Compatible(t *testing.T) {
        // config_dir なし Create → GET で config_dir 空
        // SessionDirFallback と矛盾しないこと
    }

    // シナリオ2 / R7
    func TestE2E_ConfigDir_SharedAcrossSessions(t *testing.T) {
        // 同一 configDir (skills/marker) + 別 sessionDir で 2 Create
        // 各 sessionDir に skills が overlay されていること (ファイル存在)
        // GET の session_dir が互いに異なること、config_dir が同一であること
    }

    // シナリオ3
    func TestE2E_ConfigDir_LaneIsolation(t *testing.T) {
        // alpha / beta で異なる skill 名を配置し、各 sessionDir に対応 skill のみ見えること
    }

    // シナリオ4
    func TestE2E_ConfigDir_ReappliedOnResume(t *testing.T) {
        // Create with config_dir → sessionDir の overlay を一度削除または差し替え
        // 同一 session にメッセージ送信 (SendMessage → CreateSession 再実行パス)
        // overlay が再適用されること
        // (実 LLM が不要な場合は mock agent 登録の integration 側で代替可)
    }
    ```

## Step-by-Step Implementation Guide

1. [x] **Unit tests (options)**: `options_test.go` に ConfigDir ケースを追加し、実装前に失敗することを確認する (`./scripts/process/build.sh`)。
2. [x] **Implement SessionConfig**: `options.go` に `ConfigDir` / `WithConfigDir` / ApplyDefaults 絶対パス化を実装。
3. [x] **Unit tests (overlay)**: `config_overlay_test.go` を追加し失敗確認。
4. [x] **Implement overlay**: `config_overlay.go` (`OverlayConfigDir`, `linkOrCopy`, protected names)。
5. [x] **SessionRecord**: `session_store.go` に `ConfigDir` を追加。
6. [x] **Handler tests**: `handler_test.go` に Create/GET/400 ケースを追加し失敗確認。
7. [x] **Handler impl**: `handler.go` で受理・検証・記録・`WithConfigDir` 伝播。
8. [x] **Claude overlay**: `claudecode/config_overlay*.go` + `adapter.go` 呼び出し。BuildEnv は変更しない。
9. [x] **Codex tests + BuildArgs**: `process_test.go` 更新 → `BuildArgs(..., ignoreUserConfig bool)` → `StartProcess` 分岐 → `ApplyCodexConfigDir` in adapter。
10. [x] **Clients**: `client/session.go`, `client/v1/session.go` にフィールド追加。
11. [x] **ternctl**: `--config-dir` 追加。
12. [x] **Integration/E2E**: `tests/agentservice_integration_test.go`, `tests/agentservice_e2e_test.go` にシナリオ実装。
13. [x] **Docs**: `ReferenceManual-WebAPIs.md` 更新 (下記 Documentation)。
14. [x] **Full verify**: build + 指定 integration。

各ステップ完了ごとに意味のある単位でコミットする (実装フェーズの Git ルールに従う)。

## Verification Plan

### Automated Verification

1. **Build & Unit Tests**:
   ```bash
   ./scripts/process/build.sh
   ```
   (Linux / Remote-SSH Linux の場合は `./scripts/process/build.sh --skip-etc`)

2. **Integration Tests (API / overlay / 互換)**:
   ```bash
   ./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestHandleCreateSession_WithConfigDir|TestHandleCreateSession_ConfigDirMissing|TestHandleCreateSession_ConfigDirOmitted|TestAgentServiceCreateSession_ConfigDir|TestE2E_ConfigDir|TestE2E_SessionDirFallback|TestCodexBuildArgs|TestOverlayConfigDir|TestApplyClaudeConfigDir|TestApplyCodexConfigDir"
   ```

3. **E2E Tests**: `tests/agentservice_e2e_test.go` に追加した `TestE2E_ConfigDir*` を上記 `--specify` に含める。実 Claude/Codex CLI が無い環境ではファイルシステム上の overlay 検証までを必須パスとし、CLI 依存アサーションは既存 E2E と同様のスキップ方針に合わせる。

### Verification mapping to spec scenarios

| Spec scenario | Test |
| :--- | :--- |
| 1 省略時互換 | `TestE2E_ConfigDirOmitted_Compatible`, `TestE2E_SessionDirFallback`, `TestCodexBuildArgs` (ignore 維持) |
| 2 共有 config + 別 session | `TestE2E_ConfigDir_SharedAcrossSessions` |
| 3 レーン別 config | `TestE2E_ConfigDir_LaneIsolation` |
| 4 再開時再適用 | `TestE2E_ConfigDir_ReappliedOnResume` |
| R1 不正パス 400 | `TestHandleCreateSession_ConfigDirMissing` |

## Documentation

#### [MODIFY] [docs/ReferenceManual-WebAPIs.md](file://docs/ReferenceManual-WebAPIs.md)

Create Session 節に追記:

- `config_dir` (string, Optional): Agent config set directory (skills / rules / settings). When set, Tern overlays allowlisted entries into `session_dir` before launching the agent. When omitted, behavior is unchanged from previous versions (no overlay).
- Paths (`work_dir`, `session_dir`, `config_dir`) must be visible to the Tern process (e.g. mounted into the container when Tern runs in Docker).
- Persistence env mapping remains: Claude `CLAUDE_CONFIG_DIR=session_dir`, Codex `CODEX_HOME=session_dir`.
- Precedence notes (spec 記載どおり):
  - Claude: CLI flags > project `.claude` under work_dir > user config under `CLAUDE_CONFIG_DIR` (after overlay)
  - Codex: `-c` > (when `config_dir` set) `$CODEX_HOME` user config/skills > project `.codex`; when `config_dir` omitted, `--ignore-user-config` + `-c` as today
- Overlay re-applied on each agent process start; session-only data (`projects/`, `sessions/`, …) is preserved.
- Get Session response includes `config_dir` when set.
