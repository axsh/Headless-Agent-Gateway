# 027-SessionContinuation-And-Rename

> **Source Specification**: [018-SessionContinuation-And-Rename.md](file:///prompts/phases/000-foundation/branches/feat-llm-backend/ideas/018-SessionContinuation-And-Rename.md)

## Goal Description

cawa-client に `--session-id` オプションを追加し、既存セッションへのフォローアップメッセージ送信を可能にする。また、agentservice の `handleSendMessage` で保存済みの Agent Session ID を再利用してセッション継続を実現する。併せて `sdk_session_id` を `agent_session_id` にリネームする。

## User Review Required

- `agent_session_id` というフィールド名で合意されているか。`session_id` との混同がないか確認。

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: cawa-client に --session-id オプション追加 | Proposed Changes > cawa-client/main.go |
| R2: agentservice で AgentSessionID を再利用してセッション継続 | Proposed Changes > agentservice/handler.go |
| R3: sdk_session_id -> agent_session_id リネーム | Proposed Changes > codingagent パッケージ全体 |

## Proposed Changes

### codingagent パッケージ (リネーム: R3)

#### [MODIFY] [session_store.go](file:///shared/libs/go/codingagent/session_store.go)
*   **Description**: `SDKSessionID` フィールドを `AgentSessionID` にリネーム
*   **Technical Design**:
    ```go
    // SessionRecord is a persisted session record.
    type SessionRecord struct {
        ID              string    `json:"id"`
        AgentName       string    `json:"agent_name"`
        Model           string    `json:"model"`
        Status          string    `json:"status"`
        WorkDir         string    `json:"work_dir"`
        AgentSessionID  string    `json:"agent_session_id"` // Renamed from sdk_session_id
        CreatedAt       time.Time `json:"created_at"`
        UpdatedAt       time.Time `json:"updated_at"`
    }
    ```

#### [MODIFY] [options.go](file:///shared/libs/go/codingagent/options.go)
*   **Description**: `SDKSessionID` -> `AgentSessionID`、`WithSDKSessionID` -> `WithAgentSessionID`
*   **Technical Design**:
    ```go
    type SessionConfig struct {
        // ...existing fields...

        // Session resume
        AgentSessionID string // Agent-managed session ID for context resume

        // ...existing fields...
    }

    // WithAgentSessionID sets the agent session ID for context resume.
    func WithAgentSessionID(id string) SessionOption {
        return func(c *SessionConfig) { c.AgentSessionID = id }
    }
    ```
*   **Logic**: フィールド名とオプション関数名を変更するのみ。コメントも `SDK` -> `Agent` に更新。

#### [MODIFY] [options_test.go](file:///shared/libs/go/codingagent/options_test.go)
*   **Description**: テストケース名・参照フィールド名を更新
*   **Logic**:
    - `"WithSDKSessionID"` -> `"WithAgentSessionID"`
    - `codingagent.WithSDKSessionID("sdk-123")` -> `codingagent.WithAgentSessionID("sdk-123")`
    - `cfg.SDKSessionID` -> `cfg.AgentSessionID`

### claudecode アダプタ (リネーム: R3)

#### [MODIFY] [process.go](file:///shared/libs/go/codingagent/claudecode/process.go)
*   **Description**: `cfg.SDKSessionID` -> `cfg.AgentSessionID`
*   **Logic**:
    ```go
    if cfg.AgentSessionID != "" {
        args = append(args, "--session-id", cfg.AgentSessionID)
    }
    ```

#### [MODIFY] [process_test.go](file:///shared/libs/go/codingagent/claudecode/process_test.go)
*   **Description**: テストデータの `SDKSessionID` -> `AgentSessionID`
*   **Logic**: `SDKSessionID: "sdk-abc-123"` -> `AgentSessionID: "sdk-abc-123"`

### agentservice (セッション継続: R2 + リネーム: R3)

#### [MODIFY] [handler.go](file:///shared/libs/go/agentservice/handler.go)
*   **Description**:
    1. `handleSendMessage` でセッション継続ロジックを追加
    2. `SDKSessionID` -> `AgentSessionID` リネーム
*   **Technical Design (セッション継続)**:
    ```go
    // handleSendMessage 内 (L150付近):
    // 既存: agent.CreateSession() のオプション構築
    opts := []codingagent.SessionOption{
        codingagent.WithModel(record.Model),
        codingagent.WithPrompt(req.Message),
        codingagent.WithWorkDir(record.WorkDir),
    }
    // 追加: セッション継続
    if record.AgentSessionID != "" {
        opts = append(opts, codingagent.WithAgentSessionID(record.AgentSessionID))
    }
    session, err := agent.CreateSession(r.Context(), opts...)
    ```
*   **Logic (リネーム)**:
    - L210-216, L238-244: `record.SDKSessionID` -> `record.AgentSessionID`
    - コメント: `SDKSessionID` -> `AgentSessionID`

#### [MODIFY] [session_store_test.go](file:///shared/libs/go/agentservice/session_store_test.go)
*   **Description**: `SDKSessionID` -> `AgentSessionID`
*   **Logic**:
    - `record.SDKSessionID = "sdk-abc"` -> `record.AgentSessionID = "sdk-abc"`
    - `got.SDKSessionID` -> `got.AgentSessionID`

### cawa-client (セッション継続: R1)

#### [MODIFY] [main.go](file:///examples/cawa-client/main.go)
*   **Description**: `cmdRun` に `--session-id` オプションを追加し、既存セッションへのフォローアップ送信を可能にする
*   **Technical Design**:
    ```go
    func cmdRun(args []string) {
        fs := flag.NewFlagSet("run", flag.ExitOnError)
        agent := fs.String("agent", "", "Agent name (required for new session)")
        model := fs.String("model", "", "Model name")
        prompt := fs.String("prompt", "", "Prompt message (required)")
        workDir := fs.String("work-dir", ".", "Working directory")
        sessionID := fs.String("session-id", "", "Existing session ID (for continuation)")
        fs.Parse(args)

        if *prompt == "" {
            fmt.Fprintf(os.Stderr, "Error: --prompt is required\n")
            fs.Usage()
            os.Exit(1)
        }

        var sid string
        if *sessionID != "" {
            // Continuation mode: use existing session
            sid = *sessionID
            fmt.Printf("Continuing session: %s\n\n", sid)
        } else {
            // New session mode: --agent is required
            if *agent == "" {
                fmt.Fprintf(os.Stderr, "Error: --agent is required for new sessions\n")
                fs.Usage()
                os.Exit(1)
            }
            // Create session (existing logic)
            sessionBody, _ := json.Marshal(map[string]string{
                "agent": *agent, "model": *model, "work_dir": *workDir,
            })
            resp, err := http.Post(serverURL+"/api/v1/sessions",
                "application/json", bytes.NewReader(sessionBody))
            if err != nil {
                fmt.Fprintf(os.Stderr, "Error creating session: %v\n", err)
                os.Exit(1)
            }
            defer resp.Body.Close()
            respBytes, _ := io.ReadAll(resp.Body)
            if resp.StatusCode != http.StatusCreated {
                fmt.Fprintf(os.Stderr, "Error creating session (HTTP %d):\n%s\n",
                    resp.StatusCode, string(respBytes))
                os.Exit(1)
            }
            var created map[string]string
            json.Unmarshal(respBytes, &created)
            sid = created["session_id"]
            fmt.Printf("Session created: %s\n\n", sid)
        }

        // Send message with SSE (共通処理)
        msgBody, _ := json.Marshal(map[string]string{"message": *prompt})
        req, _ := http.NewRequest("POST",
            serverURL+"/api/v1/sessions/"+sid+"/messages",
            bytes.NewReader(msgBody))
        req.Header.Set("Content-Type", "application/json")
        req.Header.Set("Accept", "text/event-stream")
        // ... (existing SSE streaming logic)
    }
    ```
*   **Logic**:
    - `--session-id` が空: 現在と同じ動作 (新規セッション作成 -> メッセージ送信)
    - `--session-id` が指定: セッション作成スキップ、既存セッションにメッセージ送信
    - `--agent` は新規セッション時のみ必須に変更
*   **Usage 更新**:
    ```go
    func printUsage() {
        fmt.Println("Usage: cawa-client [--server URL] <command> [args...]")
        fmt.Println()
        fmt.Println("Commands:")
        fmt.Println("  health                                Check server health")
        fmt.Println("  agents                                List available agents")
        fmt.Println("  models                                List available models")
        fmt.Println("  run --agent NAME --prompt MSG          Create session and run")
        fmt.Println("  run --session-id ID --prompt MSG       Continue existing session")
        fmt.Println("  session --id ID                        Get session status")
        fmt.Println("  logs --id ID                           Stream session logs")
        fmt.Println("  terminate --id ID                      Terminate session")
    }
    ```

## Step-by-Step Implementation Guide

- [x] **Step 1: codingagent パッケージのリネーム (R3 - 末端)**
  - Edit `shared/libs/go/codingagent/session_store.go`: `SDKSessionID` -> `AgentSessionID`, JSON tag `sdk_session_id` -> `agent_session_id`
  - Edit `shared/libs/go/codingagent/options.go`: フィールド名・オプション関数名・コメント更新
  - Edit `shared/libs/go/codingagent/options_test.go`: テストデータ更新
  - `git add && git commit -m "refactor: rename SDKSessionID to AgentSessionID in codingagent"`

- [x] **Step 2: claudecode アダプタのリネーム (R3 - 中間)**
  - Edit `shared/libs/go/codingagent/claudecode/process.go`: `cfg.SDKSessionID` -> `cfg.AgentSessionID`
  - Edit `shared/libs/go/codingagent/claudecode/process_test.go`: テストデータ更新
  - `git add && git commit -m "refactor: rename SDKSessionID to AgentSessionID in claudecode"`

- [x] **Step 3: agentservice のリネーム + セッション継続ロジック (R2, R3)**
  - Edit `shared/libs/go/agentservice/handler.go`:
    - `record.SDKSessionID` -> `record.AgentSessionID` (4箇所)
    - `handleSendMessage` に `WithAgentSessionID` 追加 (セッション継続ロジック)
  - Edit `shared/libs/go/agentservice/session_store_test.go`: テストデータ更新
  - `git add && git commit -m "feat: add session continuation via AgentSessionID in agentservice"`

- [x] **Step 4: cawa-client に --session-id オプション追加 (R1)**
  - Edit `examples/cawa-client/main.go`:
    - `cmdRun` に `--session-id` フラグ追加
    - 分岐ロジック (新規 vs 継続)
    - `printUsage` 更新
  - `git add && git commit -m "feat: add --session-id option to cawa-client for session continuation"`

- [x] **Step 5: ビルド + 単体テスト**
  - `./scripts/process/build.sh` を実行
  - 全単体テストが PASS することを確認

- [x] **Step 6: 統合テスト**
  - `./scripts/process/integration_test.sh --specify "TestIntegration|TestWebSocket"` を実行
  - 全テストが PASS することを確認

- [x] **Step 7: E2E 動作確認**
  - standalone サーバーを起動
  - `cawa-client run --agent claudecode --prompt "..." --work-dir ./tmp/` で初回実行
  - `cawa-client session --id <ID>` で `agent_session_id` フィールドの存在を確認
  - `cawa-client run --session-id <ID> --prompt "..."` で継続実行
  - エージェントが前回のコンテキストを維持していることを確認

- [x] **Step 8: 総合判定**
  - testing-rules.md Section 12 に基づく総合判定を実施
  - git push

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```

2.  **Integration Tests** (セッション関連):
    ```bash
    ./scripts/process/integration_test.sh --specify "TestIntegration|TestWebSocket"
    ```
    *   **Log Verification**:
        - セッション作成後の JSON レスポンスに `agent_session_id` が含まれること
        - `sdk_session_id` が含まれないこと

3.  **E2E Verification** (cawa-client):
    ```bash
    # サーバー起動
    ./bin/standalone -config ./examples/standalone/config.yaml &

    # 初回実行
    ./bin/cawa-client run --agent claudecode --prompt "Create hello.py" --work-dir ./tmp/

    # セッション確認
    ./bin/cawa-client session --id <SESSION_ID>

    # 継続実行
    ./bin/cawa-client run --session-id <SESSION_ID> --prompt "Add goodbye message"
    ```

### テスト項目のセルフレビュー

1. **網羅性**: R1 (クライアントオプション), R2 (サーバー継続), R3 (リネーム) の 3 要件すべてにテスト手順がある。
2. **証拠の十分性**: リネーム確認は JSON フィールド名の検証。セッション継続は「前回のコンテキストを維持した応答」で確認。
3. **迂回排除**: `agent_session_id` が実際に CLI の `--session-id` に渡されていることをサーバーログで確認可能。
4. **依存関係の整合性**: Step 1 (末端) -> Step 2 (中間) -> Step 3 (上位) -> Step 4 (クライアント) のボトムアップ順序。

## Documentation

#### [MODIFY] [018-SessionContinuation-And-Rename.md](file:///prompts/phases/000-foundation/branches/feat-llm-backend/ideas/018-SessionContinuation-And-Rename.md)
*   **更新内容**: 実装完了後、`sdk_session_id` -> `agent_session_id` のリネーム決定を反映
