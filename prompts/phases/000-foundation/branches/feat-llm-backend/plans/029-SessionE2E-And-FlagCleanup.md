# 029-SessionE2E-And-FlagCleanup

> **Source Specification**: [020-SessionE2E-And-FlagCleanup.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/ideas/020-SessionE2E-And-FlagCleanup.md)

## Goal Description

セッション継続と SessionDir フォールバックの E2E テストを追加し、`cawa-client` の `--session-id` フラグを `--resume` にリネームする。

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: セッション継続の E2E テスト追加 | Proposed Changes > tests/agentservice_e2e_test.go (TestE2E_SessionContinuation) |
| R1: SessionDir フォールバックの E2E テスト追加 | Proposed Changes > tests/agentservice_e2e_test.go (TestE2E_SessionDirFallback) |
| R2: --session-id から --resume へのリネーム | Proposed Changes > examples/cawa-client/main.go |

## Proposed Changes

### E2E テスト (R1)

#### [MODIFY] [agentservice_e2e_test.go](file://tests/agentservice_e2e_test.go)
*   **Description**: セッション継続と SessionDir フォールバックの E2E テスト関数を追加。セッション作成時に `session_dir` を指定するヘルパーも追加。
*   **Technical Design**:

    **ヘルパー関数の追加**:
    ```go
    // createE2ESessionWithSessionDir creates a session with session_dir specified.
    func createE2ESessionWithSessionDir(t *testing.T, baseURL, agent, workDir, sessionDir string) string {
        t.Helper()
        body, _ := json.Marshal(map[string]string{
            "agent":       agent,
            "model":       e2eDefaultModel,
            "work_dir":    workDir,
            "session_dir": sessionDir,
        })
        resp, err := http.Post(baseURL+"/api/v1/sessions", "application/json", bytes.NewReader(body))
        if err != nil {
            t.Fatalf("create session: %v", err)
        }
        defer resp.Body.Close()
        if resp.StatusCode != http.StatusCreated {
            t.Fatalf("create session: expected 201, got %d", resp.StatusCode)
        }
        var result map[string]string
        json.NewDecoder(resp.Body).Decode(&result)
        sid := result["session_id"]
        if sid == "" {
            t.Fatal("create session: empty session_id")
        }
        return sid
    }
    ```

    **TestE2E_SessionContinuation**:
    ```go
    // TestE2E_SessionContinuation verifies that a second message to the same
    // session reuses the agent_session_id (Claude Code SDK session),
    // proving conversation context is maintained.
    func TestE2E_SessionContinuation(t *testing.T) {
        baseURL, cleanup := startE2EServer(t)
        defer cleanup()
        workDir := t.TempDir()

        // 1. Create session
        sessionID := createE2ESession(t, baseURL, "claudecode", workDir)
        t.Logf("Session created: %s", sessionID)

        // 2. First message
        prompt1 := "Create a file named msg1.txt in the current directory containing exactly 'first message'. Do nothing else."
        resp1 := sendE2EMessage(t, baseURL, sessionID, prompt1, 120*time.Second)
        events1, gotDone1 := parseE2ESSEEvents(t, resp1)
        resp1.Body.Close()
        if !gotDone1 {
            t.Fatal("expected [DONE] for first message")
        }
        for _, ev := range events1 {
            if ev.Type == codingagent.EventError {
                t.Fatalf("first message error: %s", ev.Content)
            }
        }

        // 3. Verify agent_session_id was captured
        session1 := getE2ESession(t, baseURL, sessionID)
        agentSID1, _ := session1["agent_session_id"].(string)
        if agentSID1 == "" {
            t.Fatal("agent_session_id should be non-empty after first message")
        }
        t.Logf("Agent Session ID after msg1: %s", agentSID1)

        // 4. Second message (continuation)
        prompt2 := "List all files in the current directory. Do nothing else."
        resp2 := sendE2EMessage(t, baseURL, sessionID, prompt2, 120*time.Second)
        events2, gotDone2 := parseE2ESSEEvents(t, resp2)
        resp2.Body.Close()
        if !gotDone2 {
            t.Fatal("expected [DONE] for second message")
        }
        for _, ev := range events2 {
            if ev.Type == codingagent.EventError {
                t.Fatalf("second message error: %s", ev.Content)
            }
        }

        // 5. Verify agent_session_id is preserved (same SDK session)
        session2 := getE2ESession(t, baseURL, sessionID)
        agentSID2, _ := session2["agent_session_id"].(string)
        if agentSID2 == "" {
            t.Fatal("agent_session_id should be non-empty after second message")
        }
        if agentSID1 != agentSID2 {
            t.Errorf("agent_session_id changed: %s -> %s (expected same session)", agentSID1, agentSID2)
        }
        t.Logf("Agent Session ID after msg2: %s (preserved=%v)", agentSID2, agentSID1 == agentSID2)
    }
    ```

    **TestE2E_SessionDirFallback**:
    ```go
    // TestE2E_SessionDirFallback verifies that when session_dir is not specified,
    // it falls back to work_dir in the session record.
    func TestE2E_SessionDirFallback(t *testing.T) {
        baseURL, cleanup := startE2EServer(t)
        defer cleanup()
        workDir := t.TempDir()

        // Create session WITHOUT session_dir
        sessionID := createE2ESession(t, baseURL, "claudecode", workDir)

        // Get session and verify session_dir == work_dir
        session := getE2ESession(t, baseURL, sessionID)
        sessionDir, _ := session["session_dir"].(string)
        sessionWorkDir, _ := session["work_dir"].(string)

        if sessionDir != sessionWorkDir {
            t.Errorf("session_dir = %q, want %q (same as work_dir)", sessionDir, sessionWorkDir)
        }
        t.Logf("session_dir fallback verified: %s", sessionDir)
    }
    ```

*   **Logic**:
    - `TestE2E_SessionContinuation`: 同一 HAG セッションに2回メッセージを送り、`agent_session_id` が維持されることで Claude Code SDK のセッション継続を証明する。
    - `TestE2E_SessionDirFallback`: `session_dir` 未指定時に `work_dir` がフォールバックされることを API レスポンスで検証する。ただし、フォールバックは `ApplyDefaults` (codingagent パッケージ) で行われるため、セッション作成時点では空の可能性がある。`handleCreateSession` で `ApplyDefaults` 相当の処理が行われているか、または GET レスポンスに `session_dir` が含まれるかを確認する必要がある。

> [!IMPORTANT]
> **SessionDir フォールバックの実装位置の確認**:
> 現在の `handleCreateSession` ([handler.go:82-90](file://shared/libs/go/agentservice/handler.go#L82-L90)) では、リクエストの `session_dir` をそのまま `SessionRecord` に保存している。`ApplyDefaults` によるフォールバック (`SessionDir = WorkDir`) は `handleSendMessage` 内の `agent.CreateSession()` 時に適用される。
> つまり、セッション作成直後の GET では `session_dir` が空になる可能性がある。
> テストを正しく書くためには、`handleCreateSession` 内でもフォールバックを適用するか、テストの期待値を調整する必要がある。
> **対策**: `handleCreateSession` で `req.SessionDir == ""` かつ `req.WorkDir != ""` の場合に `record.SessionDir = req.WorkDir` とするフォールバックを追加する。

---

### agentservice ハンドラ (R1 の前提修正)

#### [MODIFY] [handler.go](file://shared/libs/go/agentservice/handler.go)
*   **Description**: `handleCreateSession` で `SessionDir` のフォールバックを適用。セッション作成時点で `session_dir` が空なら `work_dir` を設定する。
*   **Technical Design**:
    ```go
    // handleCreateSession - 既存コードの record 作成後に追加:
    record := &codingagent.SessionRecord{
        ID:         sessionID,
        AgentName:  req.Agent,
        Model:      req.Model,
        Status:     codingagent.StatusActive,
        WorkDir:    req.WorkDir,
        SessionDir: req.SessionDir,
    }
    // SessionDir fallback: use WorkDir if not explicitly set.
    if record.SessionDir == "" && record.WorkDir != "" {
        record.SessionDir = record.WorkDir
    }
    s.sessions.Create(record)
    ```

---

### cawa-client (R2)

#### [MODIFY] [main.go](file://examples/cawa-client/main.go)
*   **Description**: `--session-id` フラグを `--resume` にリネーム
*   **Technical Design**:

    **printUsage の変更**:
    ```go
    // Before:
    fmt.Println("  run --session-id ID --prompt MSG       Continue existing session")
    // After:
    fmt.Println("  run --resume ID --prompt MSG           Continue existing session")
    ```

    **cmdRun の変更**:
    ```go
    // Before:
    existingSessionID := fs.String("session-id", "", "Existing session ID (for continuation)")
    // After:
    resumeSessionID := fs.String("resume", "", "Existing session ID (for continuation)")
    ```

    **条件分岐の変更**:
    ```go
    // Before:
    if *existingSessionID != "" {
        sid = *existingSessionID
        fmt.Printf("Continuing session: %s\n\n", sid)
    // After:
    if *resumeSessionID != "" {
        sid = *resumeSessionID
        fmt.Printf("Continuing session: %s\n\n", sid)
    ```

## Step-by-Step Implementation Guide

- [x] **Step 1: handler.go の SessionDir フォールバック追加**
  - Edit `shared/libs/go/agentservice/handler.go`:
    - `handleCreateSession` の `record` 作成後、`s.sessions.Create(record)` の前に、`SessionDir` が空なら `WorkDir` を設定するフォールバックを追加
  - `git add && git commit -m "fix: apply SessionDir fallback in handleCreateSession"`

- [x] **Step 2: E2E テストの追加**
  - Edit `tests/agentservice_e2e_test.go`:
    - `createE2ESessionWithSessionDir` ヘルパー関数を追加
    - `TestE2E_SessionContinuation` テスト関数を追加
    - `TestE2E_SessionDirFallback` テスト関数を追加
  - `git add && git commit -m "test: add E2E tests for session continuation and SessionDir fallback"`

- [x] **Step 3: cawa-client の --session-id -> --resume リネーム**
  - Edit `examples/cawa-client/main.go`:
    - `printUsage` の `--session-id` を `--resume` に変更
    - `cmdRun` の `existingSessionID` を `resumeSessionID` に変更、フラグ名を `"resume"` に変更
    - 条件分岐の変数参照を更新
  - `git add && git commit -m "refactor: rename --session-id to --resume in cawa-client"`

- [x] **Step 4: ビルド + 単体テスト**
  - `./scripts/process/build.sh` を実行

- [x] **Step 5: E2E テスト実行**
  - `./scripts/process/integration_test.sh --specify "TestE2E_Session"` を実行

- [x] **Step 6: 既存 E2E テストのリグレッション確認**
  - `./scripts/process/integration_test.sh --specify "TestE2E"` を実行
  - 既存の `TestE2E_CodingAgentStreaming` で `sdk_session_id` -> `agent_session_id` の陳腐化を発見・修正

- [x] **Step 7: 総合判定 + git push**
  - testing-rules.md Section 12 に基づく総合判定
  - git push

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```

2.  **E2E Tests (新規追加)**:
    ```bash
    ./scripts/process/integration_test.sh --categories "llm" --specify "TestE2E_Session"
    ```
    *   **Log Verification**:
        - `TestE2E_SessionContinuation`: `agent_session_id` が2回目のメッセージ後も変わらないこと
        - `TestE2E_SessionDirFallback`: `session_dir` が `work_dir` と一致すること

3.  **既存 E2E テストのリグレッション確認**:
    ```bash
    ./scripts/process/integration_test.sh --categories "llm" --specify "TestE2E"
    ```

#### [MODIFY] [agentservice_e2e_test.go](file://tests/agentservice_e2e_test.go)
*   **テストケース**:
    - `TestE2E_SessionContinuation`: 同一セッションへの2回のメッセージ送信でセッション継続を検証
    - `TestE2E_SessionDirFallback`: SessionDir 未指定時の WorkDir フォールバックを検証
*   **検証ポイント**:
    - `agent_session_id` が2回目のメッセージ後も同じ値を保持 (セッション継続の証拠)
    - `session_dir` フィールドが `work_dir` と一致 (フォールバックの証拠)

### テスト項目のセルフレビュー

1. **網羅性**: R1 (E2Eテスト2件) と R2 (フラグリネーム) の全要件にテスト/検証手順がある。
2. **証拠の十分性**: セッション継続は `agent_session_id` の一致で検証。SessionDir は API レスポンスの値比較で検証。フラグリネームはビルド成功 + 既存テスト通過で検証。
3. **迂回排除**: `agent_session_id` の値を直接比較することで、偶然別のセッションが作成されていないことを証明。
4. **依存関係**: Step 1 (handler修正) -> Step 2 (E2Eテスト) -> Step 3 (フラグリネーム) のボトムアップ順序。

## Documentation

#### [MODIFY] [020-SessionE2E-And-FlagCleanup.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/ideas/020-SessionE2E-And-FlagCleanup.md)
*   **更新内容**: 実装完了後、実装結果を反映
