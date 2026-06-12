# 053-Fix-SessionDir-RelativePath-Bug

> **Source Specification**: prompts/phases/000-foundation/branches/feat-llm-backend/ideas/042-Fix-SessionDir-RelativePath-Bug.md

## Goal Description

`session_dir` および `work_dir` が相対パスのままCLIプロセスに渡されることで、`CLAUDE_CONFIG_DIR`/`CODEX_HOME` がCWDからの相対解決となり、ディレクトリが二重化（例: `tmp/tmp/.claudecode/`）するバグを修正する。全てのパスを絶対パスに正規化する。

## User Review Required

> [!IMPORTANT]
> **handler.go のフォールバック処理の重複について**: 現在 `SessionDir` のフォールバックロジックが `handler.go` と `options.go (ApplyDefaults)` の2箇所に存在する。本計画では **両方で絶対パス化を適用**するが、フォールバックロジック自体の統一は行わない（本バグ修正のスコープ外）。将来的なリファクタリング対象として残す。

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: session_dir の絶対パス化 | Proposed Changes > options.go, handler.go |
| R2: work_dir の絶対パス化 | Proposed Changes > options.go, handler.go |
| R3: 既存の明示指定への影響なし | Proposed Changes > options.go (絶対パスはそのまま) + テストで検証 |
| R4: session_dir ストアの整合性 | Proposed Changes > handler.go (レコード保存時に絶対パス化) |
| R5: ログの改善 | Proposed Changes > handler.go (絶対パス解決後のログ出力) |

## Proposed Changes

### codingagent パッケージ (コア変更)

#### [MODIFY] [options_test.go](file://shared/libs/go/codingagent/options_test.go)
*   **Description**: `ApplyDefaults` の絶対パス解決に関するテストケースを追加
*   **Technical Design**:
    *   既存の `TestApplyDefaults` 関数に新しいサブテスト4件を追加
*   **Logic**:
    *   テストケース1: 「relative WorkDir is resolved to absolute path」
        - `WithWorkDir("relative/path")` を設定
        - `ApplyDefaults` 後に `cfg.WorkDir` が `filepath.IsAbs()` で true を返すことを検証
        - `filepath.Abs("relative/path")` の結果と一致することを検証
    *   テストケース2: 「relative SessionDir is resolved to absolute path」
        - `WithSessionDir("rel/session")` を設定
        - `ApplyDefaults` 後に `cfg.SessionDir` が `filepath.IsAbs()` で true を返すことを検証
    *   テストケース3: 「SessionDir fallback with relative WorkDir produces absolute path」
        - `WithWorkDir("tmp")` を設定、AgentName="claudecode"
        - `ApplyDefaults` 後に `cfg.SessionDir` が絶対パスであること
        - `cfg.SessionDir` が `filepath.Join(absWorkDir, ".claudecode")` と一致すること
    *   テストケース4: 「absolute WorkDir and SessionDir are not modified」
        - `WithWorkDir("/absolute/work")` と `WithSessionDir("/absolute/session")` を設定
        - `ApplyDefaults` 後に値がそのまま保持されることを検証

---

#### [MODIFY] [options.go](file://shared/libs/go/codingagent/options.go)
*   **Description**: `ApplyDefaults` 関数に `WorkDir` と `SessionDir` の絶対パス解決を追加
*   **Technical Design**:
    ```go
    func ApplyDefaults(cfg *SessionConfig, ac *AdapterConfig) {
        if cfg.WorkDir == "" {
            cfg.WorkDir = ac.DefaultWorkDir
        }
        // R2: Resolve WorkDir to absolute path.
        // Relative paths cause issues when used as base for SessionDir
        // or as cmd.Dir for subprocess execution.
        if cfg.WorkDir != "" {
            if abs, err := filepath.Abs(cfg.WorkDir); err == nil {
                cfg.WorkDir = abs
            }
        }

        if cfg.Model == "" {
            cfg.Model = ac.DefaultModel
        }
        if cfg.EnvVars == nil && ac.DefaultEnvVars != nil {
            cfg.EnvVars = make(map[string]string)
            for k, v := range ac.DefaultEnvVars {
                cfg.EnvVars[k] = v
            }
        }
        // SessionDir fallback: explicit > AdapterConfig > WorkDir/.AgentName > WorkDir
        if cfg.SessionDir == "" {
            if ac.DefaultSessionDir != "" {
                cfg.SessionDir = ac.DefaultSessionDir
            } else if cfg.WorkDir != "" && ac.AgentName != "" {
                cfg.SessionDir = filepath.Join(cfg.WorkDir, "."+ac.AgentName)
            } else if cfg.WorkDir != "" {
                cfg.SessionDir = cfg.WorkDir
            }
        }
        // R1: Resolve SessionDir to absolute path.
        // CLI tools (claude, codex) resolve CLAUDE_CONFIG_DIR / CODEX_HOME
        // relative to their CWD, not the caller's CWD, causing path duplication.
        if cfg.SessionDir != "" {
            if abs, err := filepath.Abs(cfg.SessionDir); err == nil {
                cfg.SessionDir = abs
            }
        }
    }
    ```
*   **Logic**:
    - `WorkDir` が空でない場合: `filepath.Abs()` で絶対パスに変換。`Abs` はすでに絶対パスの場合そのまま返すため、R3（既存の絶対パス指定への影響なし）を満たす
    - WorkDir の絶対パス化を **SessionDir フォールバック計算の前** に行うことで、フォールバック結果（`filepath.Join(cfg.WorkDir, "."+ac.AgentName)`）も自然に絶対パスになる
    - 明示指定の `SessionDir` が相対パスの場合も、末尾で `filepath.Abs()` により絶対パス化される

---

### agentservice パッケージ (API層の変更)

#### [MODIFY] [handler.go](file://shared/libs/go/agentservice/handler.go)
*   **Description**: `handleCreateSession` で `SessionRecord` に保存する `WorkDir`/`SessionDir` を絶対パスに解決する (R4)。デバッグログも追加 (R5)。
*   **Technical Design**:
    `handleCreateSession` の `record` 作成後、`s.sessions.Create(record)` の前に絶対パス変換を挿入する。
    ```go
    // handleCreateSession handles POST /api/v1/sessions.
    func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
        // ... (既存の req decode, agent validation, model validation は変更なし) ...

        sessionID := s.generateID()
        record := &codingagent.SessionRecord{
            ID:         sessionID,
            AgentName:  req.Agent,
            Model:      req.Model,
            Status:     codingagent.StatusActive,
            WorkDir:    req.WorkDir,
            SessionDir: req.SessionDir,
        }

        // R2, R4: Resolve WorkDir to absolute path for record consistency.
        if record.WorkDir != "" {
            if abs, err := filepath.Abs(record.WorkDir); err == nil {
                record.WorkDir = abs
            }
        }

        // SessionDir fallback: use WorkDir/.AgentName if not explicitly set.
        if record.SessionDir == "" && record.WorkDir != "" {
            if record.AgentName != "" {
                record.SessionDir = filepath.Join(record.WorkDir, "."+record.AgentName)
            } else {
                record.SessionDir = record.WorkDir
            }
        }

        // R1, R4: Resolve SessionDir to absolute path for record consistency.
        if record.SessionDir != "" {
            if abs, err := filepath.Abs(record.SessionDir); err == nil {
                record.SessionDir = abs
            }
        }

        // R5: Log resolved paths for debugging.
        if s.logger != nil {
            s.logger.Debug("session paths resolved",
                "session_id", sessionID,
                "work_dir", record.WorkDir,
                "session_dir", record.SessionDir)
        }

        s.sessions.Create(record)
        // ... (既存のレスポンス送信は変更なし) ...
    }
    ```
*   **Logic**:
    - `WorkDir` の絶対パス化を **SessionDir フォールバック計算の前** に配置（options.go と同じ順序）
    - フォールバック後の `SessionDir` を `filepath.Abs()` で絶対パスに変換
    - ログを追加して解決後のパスを出力（R5）

---

### 既存テストの更新

#### [MODIFY] [options_test.go](file://shared/libs/go/codingagent/options_test.go)
*   **Description**: 既存テストの期待値を絶対パス化に合わせて更新
*   **Technical Design**:
    - 「session dir falls back to work dir when no agent name」: 既存テストは絶対パスの WorkDir を使用しているため変更不要
    - 「session dir includes agent name when set」: 同上、絶対パスを使用しているため変更不要
    - 「explicit session dir takes priority」: 絶対パスを使用しているため変更不要
    - 「default session dir from adapter config」: 絶対パスを使用しているため変更不要
    - 「defaults applied when fields are zero」: `DefaultWorkDir: "/default/work"` は絶対パスのため変更不要
    - 「session options take priority over defaults」: `WithWorkDir("/explicit/dir")` は絶対パスのため変更不要
*   **Logic**: 既存テストは全て絶対パスを入力値として使用しているため、テスト結果への影響なし。新テストのみ追加。

#### [MODIFY] [agentservice_e2e_test.go](file://tests/agentservice_e2e_test.go)
*   **Description**: `TestE2E_SessionDirFallback` を修正。修正後は `session_dir` が `filepath.Join(workDir, ".claudecode")` になるため、テストの期待値を更新する
*   **Technical Design**:
    ```go
    func TestE2E_SessionDirFallback(t *testing.T) {
        baseURL, cleanup := startE2EServer(t)
        defer cleanup()
        workDir := t.TempDir()
        initGitRepo(t, workDir)

        // Create session WITHOUT session_dir
        body, _ := json.Marshal(map[string]string{
            "agent":    "claudecode",
            "model":    e2eDefaultModel,
            "work_dir": workDir,
        })
        resp, err := http.Post(baseURL+"/api/v1/sessions", "application/json", bytes.NewReader(body))
        if err != nil {
            t.Fatalf("create session: %v", err)
        }
        defer resp.Body.Close()
        var result map[string]string
        json.NewDecoder(resp.Body).Decode(&result)
        sessionID := result["session_id"]

        // Get session and verify session_dir == work_dir/.claudecode (absolute path)
        session := getE2ESession(t, baseURL, sessionID)
        sessionDir, _ := session["session_dir"].(string)
        sessionWorkDir, _ := session["work_dir"].(string)

        // After fix: session_dir should be work_dir/.claudecode (absolute)
        wantSessionDir := filepath.Join(sessionWorkDir, ".claudecode")
        if sessionDir != wantSessionDir {
            t.Errorf("session_dir = %q, want %q", sessionDir, wantSessionDir)
        }

        // Both should be absolute paths
        if !filepath.IsAbs(sessionDir) {
            t.Errorf("session_dir should be absolute, got %q", sessionDir)
        }
        if !filepath.IsAbs(sessionWorkDir) {
            t.Errorf("work_dir should be absolute, got %q", sessionWorkDir)
        }
        t.Logf("session_dir fallback verified: %s", sessionDir)
    }
    ```
*   **Logic**:
    - `t.TempDir()` は絶対パスを返すため、`workDir` は元々絶対パス
    - 修正前: `session_dir == work_dir` を期待していた（AgentNameがあるのに `.claudecode` がつかないテスト）
    - 修正後: `session_dir == work_dir + "/.claudecode"` を期待する（実際の handler.go のフォールバックロジックと一致）
    - 絶対パスであることの検証を追加

## Step-by-Step Implementation Guide

1.  **テストを先に書く (TDD - Red Phase)**:
    *   `shared/libs/go/codingagent/options_test.go` に4件の新テストケースを追加
    *   テストケース:
        - `"relative WorkDir is resolved to absolute path"`
        - `"relative SessionDir is resolved to absolute path"`
        - `"SessionDir fallback with relative WorkDir produces absolute path"`
        - `"absolute WorkDir and SessionDir are not modified"`
    *   ビルドスクリプトで失敗することを確認: `./scripts/process/build.sh`
    *   コミット: `test: add unit tests for relative path resolution in ApplyDefaults`

2.  **options.go を修正 (TDD - Green Phase)**:
    *   `shared/libs/go/codingagent/options.go` の `ApplyDefaults` 関数を修正
    *   WorkDir の絶対パス化（SessionDir フォールバック計算の前に配置）
    *   SessionDir の絶対パス化（フォールバック計算の後に配置）
    *   ビルドスクリプトでテスト成功を確認: `./scripts/process/build.sh`
    *   コミット: `fix: resolve WorkDir and SessionDir to absolute paths in ApplyDefaults`

3.  **handler.go を修正**:
    *   `shared/libs/go/agentservice/handler.go` の `handleCreateSession` を修正
    *   WorkDir の絶対パス化をフォールバック処理の前に追加
    *   SessionDir の絶対パス化をフォールバック処理の後に追加
    *   パス解決後のデバッグログを追加
    *   ビルドスクリプトでテスト成功を確認: `./scripts/process/build.sh`
    *   コミット: `fix: resolve session record paths to absolute in handleCreateSession`

4.  **E2Eテストを更新**:
    *   `tests/agentservice_e2e_test.go` の `TestE2E_SessionDirFallback` を更新
    *   期待値を `session_dir == work_dir` から `session_dir == work_dir/.claudecode` に変更
    *   絶対パスの検証を追加
    *   コミット: `test: update SessionDirFallback E2E test for absolute path resolution`

5.  **ビルド & テスト**:
    *   Verification Plan の手順に従ってビルドとテストを実行

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests**:
    ビルドスクリプトを実行し、全ての単体テストが通過することを確認:
    ```bash
    ./scripts/process/build.sh
    ```

2.  **Integration Tests**:
    セッション管理に関連する統合テストを実行:
    ```bash
    ./scripts/process/integration_test.sh --categories common --specify "SessionDir"
    ```
    *   **Log Verification**: サーバーログに `session paths resolved` メッセージが出力され、`work_dir` と `session_dir` が絶対パスであることを確認

3.  **E2E Tests (既存テストの更新)**:

    #### [MODIFY] [agentservice_e2e_test.go](file://tests/agentservice_e2e_test.go)
    *   **テストケース**: `TestE2E_SessionDirFallback` - `session_dir` 未指定時のフォールバックと絶対パス解決の検証
    *   **検証ポイント**:
        - `session_dir` が `work_dir/.claudecode` （絶対パス）であること
        - `work_dir` が絶対パスであること
    *   **E2Eテスト新規追加不要の理由**: 本修正は純粋なパス正規化の内部修正であり、外部から観測可能な新しい機能の追加ではない。既存の `TestE2E_SessionDirFallback` を更新することで十分にカバーされる。また、`TestE2E_CodingAgentStreaming` 等の既存E2Eテストが `createE2ESession` ヘルパー経由で絶対パスの `session_dir` を明示指定しているため、既存テストのリグレッション確認も自動的に行われる。

### テスト項目設計のセルフレビュー (testing-rules.md 11.4)

1.  **網羅性の検証**: 本修正の対象は2つの関数（`ApplyDefaults`, `handleCreateSession`）のパス正規化ロジックのみ。単体テスト4件で相対/絶対の両パターンとフォールバックの組み合わせをカバーし、E2Eテスト1件で実際のAPI経由での動作を確認する。パス正規化の動作を確認するにはこのテスト群で十分と判断。
2.  **証拠の十分性**: 各テストケースで `filepath.IsAbs()` による絶対パス判定と、`filepath.Abs()` の期待値との一致を検証しており、「パスが正しく解決された」ことの証拠として十分。
3.  **迂回・抜け道の排除**: `options.go` と `handler.go` の両方に修正を加え、それぞれの経路でテストが存在する。`BuildEnv` は入力をそのまま使用するため、入力が正しければ出力も正しい（上流での正規化に依存）。
4.  **依存関係の整合性**: `ApplyDefaults` (末端) -> `handler.go` (中間) -> E2E (全体) の順でテストが設計されており、ボトムアップの検証順序を満たしている。

### 総合判定プロセス (testing-rules.md 12)

全テスト完了後、以下の観点で総合判定を実施:
- テストログに `SKIP`/`WARN` マーカーがないか
- `session paths resolved` ログが正しく出力されているか
- 既存の E2E テスト (`TestE2E_CodingAgentStreaming` 等) がリグレッションなく通過しているか

## Documentation

#### [MODIFY] [042-Fix-SessionDir-RelativePath-Bug.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/ideas/042-Fix-SessionDir-RelativePath-Bug.md)
*   **更新内容**: 変更不要。仕様書は修正方針を記述しており、実装後も有効な情報のまま。
