# 028-SessionPersistence-DirectoryConfig

> **Source Specification**: [019-SessionPersistence-DirectoryConfig.md](file:///prompts/phases/000-foundation/branches/feat-llm-backend/ideas/019-SessionPersistence-DirectoryConfig.md)

## Goal Description

エージェントのセッションデータ保存先を制御する `SessionDir` フィールドを追加する。`--session-dir` 未指定時は `WorkDir` をフォールバック値として適用する。Claude Code では `CLAUDE_CONFIG_DIR`、Codex CLI では `CODEX_HOME` 環境変数に変換してセッション保存先を制御する。

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: SessionConfig に SessionDir フィールド追加 | Proposed Changes > codingagent/options.go |
| R1: デフォルト値として WorkDir を適用 | Proposed Changes > codingagent/options.go (ApplyDefaults) |
| R1: Claude Code で CLAUDE_CONFIG_DIR に変換 | Proposed Changes > claudecode/process.go |
| R1: Codex CLI で CODEX_HOME に変換 | Proposed Changes > codex/process.go |
| R2: cawa-client に --session-dir オプション追加 | Proposed Changes > cawa-client/main.go |
| R3: SessionRecord に SessionDir フィールド追加 | Proposed Changes > codingagent/session_store.go |
| R4: ApplyDefaults での SessionDir フォールバック | Proposed Changes > codingagent/options.go (ApplyDefaults) |
| R5: Antigravity CLI 対応 | 先送り (保存先制御方法が不明確なため) |

## Proposed Changes

### codingagent パッケージ (R1, R3, R4)

#### [MODIFY] [options_test.go](file:///shared/libs/go/codingagent/options_test.go)
*   **Description**: `WithSessionDir` オプション関数のテスト追加、`ApplyDefaults` の SessionDir フォールバックテスト追加
*   **Technical Design**:
    ```go
    // TestSessionOptionFunctions の tests に追加
    {
        name: "WithSessionDir",
        opt:  codingagent.WithSessionDir("/data/sessions"),
        check: func(t *testing.T, cfg *codingagent.SessionConfig) {
            if cfg.SessionDir != "/data/sessions" {
                t.Errorf("SessionDir = %v, want /data/sessions", cfg.SessionDir)
            }
        },
    },

    // TestApplyDefaults に追加
    t.Run("session dir falls back to work dir", func(t *testing.T) {
        cfg := codingagent.NewSessionConfig(
            codingagent.WithWorkDir("/workspace/project"),
        )
        ac := &codingagent.AdapterConfig{}
        codingagent.ApplyDefaults(cfg, ac)
        if cfg.SessionDir != "/workspace/project" {
            t.Errorf("SessionDir = %v, want /workspace/project", cfg.SessionDir)
        }
    })

    t.Run("explicit session dir takes priority", func(t *testing.T) {
        cfg := codingagent.NewSessionConfig(
            codingagent.WithWorkDir("/workspace/project"),
            codingagent.WithSessionDir("/data/sessions"),
        )
        ac := &codingagent.AdapterConfig{}
        codingagent.ApplyDefaults(cfg, ac)
        if cfg.SessionDir != "/data/sessions" {
            t.Errorf("SessionDir = %v, want /data/sessions", cfg.SessionDir)
        }
    })

    t.Run("default session dir from adapter config", func(t *testing.T) {
        cfg := codingagent.NewSessionConfig(
            codingagent.WithWorkDir("/workspace/project"),
        )
        ac := &codingagent.AdapterConfig{
            DefaultSessionDir: "/default/sessions",
        }
        codingagent.ApplyDefaults(cfg, ac)
        if cfg.SessionDir != "/default/sessions" {
            t.Errorf("SessionDir = %v, want /default/sessions", cfg.SessionDir)
        }
    })
    ```

#### [MODIFY] [options.go](file:///shared/libs/go/codingagent/options.go)
*   **Description**: `SessionConfig` に `SessionDir` フィールド追加、`WithSessionDir` オプション関数追加、`ApplyDefaults` に SessionDir フォールバックロジック追加
*   **Technical Design**:
    ```go
    type SessionConfig struct {
        // ...existing fields...

        // Session data storage directory.
        // When set, the adapter maps this to the agent-specific env var
        // (e.g., CLAUDE_CONFIG_DIR, CODEX_HOME).
        // Falls back to WorkDir if not explicitly set.
        SessionDir string
    }

    // WithSessionDir sets the session data storage directory.
    func WithSessionDir(dir string) SessionOption {
        return func(c *SessionConfig) { c.SessionDir = dir }
    }
    ```
*   **Logic (ApplyDefaults)**:
    ```go
    func ApplyDefaults(cfg *SessionConfig, ac *AdapterConfig) {
        // ...existing defaults (WorkDir, Model, EnvVars)...

        // SessionDir fallback: explicit > AdapterConfig > WorkDir
        if cfg.SessionDir == "" {
            if ac.DefaultSessionDir != "" {
                cfg.SessionDir = ac.DefaultSessionDir
            } else if cfg.WorkDir != "" {
                cfg.SessionDir = cfg.WorkDir
            }
        }
    }
    ```
    - 優先順序: `WithSessionDir()` (明示指定) > `AdapterConfig.DefaultSessionDir` > `WorkDir` (フォールバック)

#### [MODIFY] [session_store.go](file:///shared/libs/go/codingagent/session_store.go)
*   **Description**: `SessionRecord` に `SessionDir` フィールド追加
*   **Technical Design**:
    ```go
    type SessionRecord struct {
        ID             string    `json:"id"`
        AgentName      string    `json:"agent_name"`
        Model          string    `json:"model"`
        Status         string    `json:"status"`
        WorkDir        string    `json:"work_dir"`
        AgentSessionID string    `json:"agent_session_id"`
        SessionDir     string    `json:"session_dir"`
        CreatedAt      time.Time `json:"created_at"`
        UpdatedAt      time.Time `json:"updated_at"`
    }
    ```

#### [MODIFY] [adapter_config.go](file:///shared/libs/go/codingagent/adapter_config.go)
*   **Description**: `AdapterConfig` に `DefaultSessionDir` フィールド追加
*   **Technical Design**:
    ```go
    type AdapterConfig struct {
        // ...existing fields...

        // DefaultSessionDir is the default session data storage directory.
        // Can be overridden per-session via WithSessionDir.
        // Falls back to WorkDir if not set.
        DefaultSessionDir string
    }
    ```

### claudecode アダプタ (R1)

#### [MODIFY] [process_test.go](file:///shared/libs/go/codingagent/claudecode/process_test.go)
*   **Description**: `BuildEnv` に `SessionDir` -> `CLAUDE_CONFIG_DIR` 変換のテスト追加
*   **Technical Design**:
    ```go
    // TestBuildEnv の tests に追加
    {
        name:    "session dir sets CLAUDE_CONFIG_DIR",
        ac:      &codingagent.AdapterConfig{},
        cfg:     &codingagent.SessionConfig{SessionDir: "/data/sessions"},
        wantKey: "CLAUDE_CONFIG_DIR",
        wantVal: "/data/sessions",
    },
    ```

#### [MODIFY] [process.go](file:///shared/libs/go/codingagent/claudecode/process.go)
*   **Description**: `BuildEnv` で `SessionDir` -> `CLAUDE_CONFIG_DIR` 環境変数変換追加
*   **Logic**:
    ```go
    func BuildEnv(ac *codingagent.AdapterConfig, cfg *codingagent.SessionConfig) []string {
        // ...existing logic...

        // Session data storage directory override.
        if cfg.SessionDir != "" {
            env["CLAUDE_CONFIG_DIR"] = cfg.SessionDir
        }

        // ...existing EnvVars override...
    }
    ```
    - `cfg.EnvVars` によるオーバーライドの前に配置。ユーザーが `EnvVars` で直接 `CLAUDE_CONFIG_DIR` を指定した場合はそちらが優先される。

### codex アダプタ (R1)

#### [MODIFY] [process_test.go](file:///shared/libs/go/codingagent/codex/process_test.go)
*   **Description**: `BuildEnv` に `SessionDir` -> `CODEX_HOME` 変換のテスト追加
*   **Technical Design**:
    ```go
    // TestBuildEnv の tests に追加
    {
        name:    "session dir sets CODEX_HOME",
        ac:      &codingagent.AdapterConfig{},
        cfg:     &codingagent.SessionConfig{SessionDir: "/data/sessions"},
        wantKey: "CODEX_HOME",
        wantVal: "/data/sessions",
    },
    ```

#### [MODIFY] [process.go](file:///shared/libs/go/codingagent/codex/process.go)
*   **Description**: `BuildEnv` で `SessionDir` -> `CODEX_HOME` 環境変数変換追加
*   **Logic**:
    ```go
    func BuildEnv(ac *codingagent.AdapterConfig, cfg *codingagent.SessionConfig) []string {
        // ...existing logic...

        // Session data storage directory override.
        if cfg.SessionDir != "" {
            env["CODEX_HOME"] = cfg.SessionDir
        }

        // ...existing EnvVars override...
    }
    ```

### agentservice (R2, R3)

#### [MODIFY] [handler.go](file:///shared/libs/go/agentservice/handler.go)
*   **Description**: セッション作成リクエストに `session_dir` フィールド追加、`SessionRecord` に保存、`CreateSession` 時に `WithSessionDir()` を渡す
*   **Technical Design (handleCreateSession)**:
    ```go
    var req struct {
        Agent      string `json:"agent"`
        Model      string `json:"model"`
        WorkDir    string `json:"work_dir"`
        Prompt     string `json:"prompt"`
        SessionDir string `json:"session_dir"`
    }
    // ...
    record := &codingagent.SessionRecord{
        ID:         sessionID,
        AgentName:  req.Agent,
        Model:      req.Model,
        Status:     codingagent.StatusActive,
        WorkDir:    req.WorkDir,
        SessionDir: req.SessionDir,
    }
    ```
*   **Technical Design (handleSendMessage)** - 既存の opts 構築に `WithSessionDir` 追加:
    ```go
    opts := []codingagent.SessionOption{
        codingagent.WithModel(record.Model),
        codingagent.WithPrompt(req.Message),
        codingagent.WithWorkDir(record.WorkDir),
    }
    if record.AgentSessionID != "" {
        opts = append(opts, codingagent.WithAgentSessionID(record.AgentSessionID))
    }
    if record.SessionDir != "" {
        opts = append(opts, codingagent.WithSessionDir(record.SessionDir))
    }
    ```

### cawa-client (R2)

#### [MODIFY] [main.go](file:///examples/cawa-client/main.go)
*   **Description**: `cmdRun` に `--session-dir` オプション追加、セッション作成 API のボディに `session_dir` を含める
*   **Technical Design**:
    ```go
    func cmdRun(args []string) {
        fs := flag.NewFlagSet("run", flag.ExitOnError)
        // ...existing flags...
        sessionDir := fs.String("session-dir", "", "Session data storage directory (default: work-dir)")
        // ...
        // New session mode:
        sessionBody, _ := json.Marshal(map[string]string{
            "agent": *agent, "model": *model,
            "work_dir": *workDir, "session_dir": *sessionDir,
        })
    }
    ```
*   **Usage 更新**:
    ```go
    fmt.Println("  run --agent NAME --prompt MSG          Create session and run")
    fmt.Println("      [--session-dir DIR]                Session storage directory")
    fmt.Println("  run --session-id ID --prompt MSG       Continue existing session")
    ```

## Step-by-Step Implementation Guide

- [ ] **Step 1: codingagent パッケージ - テスト追加 (TDD Red)**
  - Edit `shared/libs/go/codingagent/options_test.go`:
    - `WithSessionDir` のテストケース追加
    - `ApplyDefaults` の SessionDir フォールバックテスト 3件追加
  - この時点ではコンパイルエラー (Red)

- [ ] **Step 2: codingagent パッケージ - 実装 (TDD Green)**
  - Edit `shared/libs/go/codingagent/options.go`: `SessionDir` フィールド、`WithSessionDir()` 関数、`ApplyDefaults` ロジック追加
  - Edit `shared/libs/go/codingagent/session_store.go`: `SessionRecord.SessionDir` フィールド追加
  - Edit `shared/libs/go/codingagent/adapter_config.go`: `AdapterConfig.DefaultSessionDir` フィールド追加
  - `git add && git commit -m "feat: add SessionDir field and WithSessionDir option to codingagent"`

- [ ] **Step 3: claudecode アダプタ - テスト追加 (TDD Red)**
  - Edit `shared/libs/go/codingagent/claudecode/process_test.go`: `CLAUDE_CONFIG_DIR` 変換テスト追加

- [ ] **Step 4: claudecode アダプタ - 実装 (TDD Green)**
  - Edit `shared/libs/go/codingagent/claudecode/process.go`: `BuildEnv` に `CLAUDE_CONFIG_DIR` 設定追加
  - `git add && git commit -m "feat: map SessionDir to CLAUDE_CONFIG_DIR in claudecode"`

- [ ] **Step 5: codex アダプタ - テスト追加 + 実装**
  - Edit `shared/libs/go/codingagent/codex/process_test.go`: `CODEX_HOME` 変換テスト追加
  - Edit `shared/libs/go/codingagent/codex/process.go`: `BuildEnv` に `CODEX_HOME` 設定追加
  - `git add && git commit -m "feat: map SessionDir to CODEX_HOME in codex"`

- [ ] **Step 6: agentservice - セッション作成と継続に SessionDir 追加**
  - Edit `shared/libs/go/agentservice/handler.go`:
    - `handleCreateSession`: リクエスト struct に `SessionDir` 追加、`SessionRecord` に保存
    - `handleSendMessage`: opts に `WithSessionDir` 追加
  - `git add && git commit -m "feat: add session_dir to session creation and message sending"`

- [ ] **Step 7: cawa-client - --session-dir オプション追加**
  - Edit `examples/cawa-client/main.go`:
    - `cmdRun` に `--session-dir` フラグ追加
    - セッション作成リクエストに `session_dir` 含める
    - `printUsage` 更新
  - `git add && git commit -m "feat: add --session-dir option to cawa-client"`

- [ ] **Step 8: ビルド + 単体テスト**
  - `./scripts/process/build.sh` を実行
  - 全単体テストが PASS することを確認

- [ ] **Step 9: 統合テスト**
  - `./scripts/process/integration_test.sh --specify "TestIntegration|TestWebSocket"` を実行

- [ ] **Step 10: E2E 動作確認**
  - standalone サーバー起動
  - `cawa-client run --agent claudecode --prompt "hello" --work-dir ./tmp/` で初回実行 (session_dir = work_dir フォールバック確認)
  - `cawa-client session --id <ID>` で `session_dir` フィールド確認
  - `cawa-client run --agent claudecode --prompt "hello" --work-dir ./tmp/ --session-dir /tmp/test-sessions` でカスタム保存先確認

- [ ] **Step 11: 総合判定 + git push**
  - testing-rules.md Section 12 に基づく総合判定
  - git push

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```

2.  **Integration Tests**:
    ```bash
    ./scripts/process/integration_test.sh --specify "TestIntegration|TestWebSocket"
    ```
    *   **Log Verification**:
        - セッション作成レスポンスに `session_dir` フィールドが含まれること
        - フォールバックテストで `SessionDir == WorkDir` になること

3.  **E2E Verification**:
    ```bash
    ./bin/standalone -config ./examples/standalone/config.yaml &
    # デフォルト (work_dir フォールバック)
    ./bin/cawa-client run --agent claudecode --prompt "hello" --work-dir ./tmp/
    ./bin/cawa-client session --id <ID>  # session_dir == ./tmp/ を確認
    # カスタム保存先
    ./bin/cawa-client run --agent claudecode --prompt "hello" --work-dir ./tmp/ --session-dir /tmp/test-sessions
    ./bin/cawa-client session --id <ID>  # session_dir == /tmp/test-sessions を確認
    ```

### テスト項目のセルフレビュー

1. **網羅性**: R1 (フィールド追加 + 環境変数変換), R2 (クライアントオプション), R3 (レコード保存), R4 (フォールバック) の 4 要件すべてにテスト手順がある。R5 は先送り (明記済み)。
2. **証拠の十分性**: `ApplyDefaults` の 3 パターン (フォールバック / 明示指定 / デフォルト設定) で優先順序を検証。`BuildEnv` で環境変数名と値の一致を確認。
3. **迂回排除**: `CLAUDE_CONFIG_DIR` / `CODEX_HOME` が実際に設定されていることを `BuildEnv` の戻り値で直接検証。
4. **依存関係の整合性**: Step 1-2 (codingagent 末端) -> Step 3-5 (アダプタ中間) -> Step 6 (agentservice 上位) -> Step 7 (クライアント) のボトムアップ順序。

## Documentation

#### [MODIFY] [019-SessionPersistence-DirectoryConfig.md](file:///prompts/phases/000-foundation/branches/feat-llm-backend/ideas/019-SessionPersistence-DirectoryConfig.md)
*   **更新内容**: 実装完了後、実装結果を反映
