# 012-CodingAgentAPI-Part4-QualityFixes

> **Source Specification**: [008-CodingAgentAPI-Completion.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/ideas/008-CodingAgentAPI-Completion.md)

## Goal Description

CodingAgentAPI Part1-3の実装で特定された品質上の問題を修正する。具体的には、(1) TaskLog記録の追加、(2) SDKSessionID保存ロジックの追加、(3) HealthResponseへのCLIバージョン追加、(4) Graceful Shutdownの実装、(5) 仕様書007の更新。

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| C1-1: streamSSEでTaskLog記録 | Proposed Changes > handler.go |
| C1-2: StreamEvent -> AgentLogEntry変換 | Proposed Changes > handler.go (toAgentLogEntry) |
| C1-3: WebSocket通知の自動連携 | TaskLog.onEntry callback (既存) |
| C1-4: respondJSONでもTaskLog記録 | Proposed Changes > handler.go |
| C2-1: EventSystemからSDKSessionID抽出 | Proposed Changes > handler.go (streamSSE) |
| C2-2: system.init.session_id抽出 | Proposed Changes > handler.go |
| C2-3: SessionStore.Update永続化 | Proposed Changes > handler.go |
| C2-4: GET応答にsdk_session_id | SessionRecord既存フィールド (json tagの追加) |
| C3-1: CLIVersionsフィールド追加 | Proposed Changes > health.go |
| C3-2: CLIバージョン取得 | Proposed Changes > service.go (detectCLIVersions) |
| C3-3: CLI不在時 "unavailable" | Proposed Changes > service.go |
| C3-4: 初期化時1回キャッシュ | Proposed Changes > service.go |
| C4-1: ProcessManager.Stop改善 | Proposed Changes > claudecode/process.go, codex/process.go |
| C4-2: SIGTERM -> 5秒 -> SIGKILL | Proposed Changes > 両process.go |
| C4-3: Windowsフォールバック | Proposed Changes > process.go (runtime.GOOS check) |
| C5-1: 007 Terminate APIパス修正 | Documentation > 007-CodingAgentAPI.md |
| C5-2: O7昇格 | Documentation > 007-CodingAgentAPI.md |

## Proposed Changes

### agentservice パッケージ

#### [MODIFY] [handler_test.go](file://shared/libs/go/agentservice/handler_test.go)
*   **Description**: TaskLog記録とSDKSessionID保存のテストを追加
*   **Technical Design**:

```go
// mockCodingSessionWithSystemEvent はEventSystemを返すmockセッション
type mockCodingSessionWithSystemEvent struct{}

func (s *mockCodingSessionWithSystemEvent) Send(_ context.Context, _ string) (<-chan codingagent.StreamEvent, error) {
    ch := make(chan codingagent.StreamEvent, 3)
    ch <- codingagent.StreamEvent{Type: codingagent.EventSystem, SessionID: "sdk-abc-123"}
    ch <- codingagent.StreamEvent{Type: codingagent.EventText, Content: "hello"}
    ch <- codingagent.StreamEvent{Type: codingagent.EventResult}
    close(ch)
    return ch, nil
}
func (s *mockCodingSessionWithSystemEvent) ID() string  { return "mock-session" }
func (s *mockCodingSessionWithSystemEvent) Close() error { return nil }
```

*   **Logic**:
    *   `TestHandleSendMessage_TaskLogRecording`: `agentservice.NewWithStore()` + `agentservice.WithTaskLog(tl)` でサーバ作成。メッセージ送信後、`tl.Entries()` にAgentLogEntryが記録されていることを検証
    *   `TestHandleSendMessage_SDKSessionID`: `mockCodingSessionWithSystemEvent` を使用。ストリーミング完了後、`GET /api/v1/sessions/:id` でレスポンスの `sdk_session_id` が `"sdk-abc-123"` であること検証
    *   `TestHandleSendMessage_JSON_TaskLogRecording`: Accept headerなし (JSON応答) で同様にTaskLog記録を検証

---

#### [MODIFY] [handler.go](file://shared/libs/go/agentservice/handler.go)
*   **Description**: streamSSE/respondJSONにTaskLog記録とSDKSessionID保存を追加
*   **Technical Design**:

```go
// toAgentLogEntry converts a StreamEvent to an AgentLogEntry for TaskLog.
func toAgentLogEntry(ev codingagent.StreamEvent, sessionID string) *tasklog.AgentLogEntry {
    body, _ := json.Marshal(ev)
    return tasklog.NewAgentLogSendEntry(
        uuid.New().String(),
        sessionID,
        string(body),
    )
}
```

*   **Logic** (streamSSE):
    1. イベントループ内で各`StreamEvent`を`toAgentLogEntry()`で変換し、`s.taskLog.Add()`で記録
    2. `EventSystem`イベントを検出した場合、`ev.SessionID`を`record.SDKSessionID`に設定し、`s.sessions.Update(record)`で永続化
    3. ループ完了後の`[DONE]`送信前に、最終ステータスのAgentLogEndEntryを記録
    4. `respondJSON`でも同様にイベント収集時にTaskLog記録

変更後のstreamSSEの擬似コード:
```go
func (s *Server) streamSSE(w http.ResponseWriter, ch <-chan codingagent.StreamEvent, sessionID string) {
    // ... headers ...
    for ev := range ch {
        data, _ := json.Marshal(ev)
        fmt.Fprintf(w, "data: %s\n\n", data)
        flusher.Flush()

        // TaskLog記録
        if s.taskLog != nil {
            s.taskLog.Add(toAgentLogEntry(ev, sessionID))
        }

        // SDKSessionID抽出
        if ev.Type == codingagent.EventSystem && ev.SessionID != "" {
            if record, err := s.sessions.Get(sessionID); err == nil {
                record.SDKSessionID = ev.SessionID
                s.sessions.Update(record)
            }
        }
    }
    // ... [DONE] + status update ...
}
```

---

#### [MODIFY] [health_test.go](file://shared/libs/go/agentservice/health_test.go)
*   **Description**: cli_versionsフィールドのテスト追加
*   **Logic**:
    *   `TestHealthHandler_CLIVersions`: CLIが存在しない環境でも`cli_versions`フィールドが`"unavailable"`で返ることを検証
    *   既存テスト `TestHealthHandler_AllHealthy` のレスポンス検証に `cli_versions` チェックを追加

---

#### [MODIFY] [health.go](file://shared/libs/go/agentservice/health.go)
*   **Description**: HealthResponseにCLIVersionsフィールドを追加
*   **Technical Design**:

```go
type HealthResponse struct {
    Status      string            `json:"status"`
    Agents      []string          `json:"agents"`
    CLIVersions map[string]string `json:"cli_versions"`
    Gateway     GatewayHealth     `json:"gateway"`
}
```

*   **Logic**: `handleHealth()`内で`s.cliVersions`(キャッシュ)を`resp.CLIVersions`に設定

---

#### [MODIFY] [service.go](file://shared/libs/go/agentservice/service.go)
*   **Description**: CLIバージョン検出キャッシュを追加
*   **Technical Design**:

```go
type Server struct {
    agents      map[string]codingagent.CodingAgent
    sessions    codingagent.SessionStore
    logger      logger.Logger
    taskLog     *tasklog.TaskLog
    gatewayURL  string
    cliVersions map[string]string // cached at init
}

// detectCLIVersions runs "claude --version" / "codex --version" once at init.
func detectCLIVersions(agents map[string]codingagent.CodingAgent) map[string]string {
    versions := make(map[string]string)
    cliNames := map[string]string{
        "claudecode": "claude",
        "codex":      "codex",
    }
    for agentName := range agents {
        cliName, ok := cliNames[agentName]
        if !ok {
            versions[agentName] = "unavailable"
            continue
        }
        out, err := exec.Command(cliName, "--version").Output()
        if err != nil {
            versions[agentName] = "unavailable"
            continue
        }
        versions[agentName] = strings.TrimSpace(string(out))
    }
    return versions
}
```

*   **Logic**: `New()`および`NewWithStore()`の末尾で`s.cliVersions = detectCLIVersions(s.agents)` を呼び出す。ただしagentsが空の場合(テスト時)は空mapを返す

---

### codingagent パッケージ (Graceful Shutdown)

#### [MODIFY] [claudecode/process_test.go](file://shared/libs/go/codingagent/claudecode/process_test.go)
*   **Description**: Graceful Shutdown テスト追加
*   **Logic**:
    *   `TestProcessManager_GracefulStop`: サブプロセスとして`sleep 30`を起動し、`Stop()`呼び出し後5秒以内にプロセスが終了すること検証
    *   `TestProcessManager_ForceKill`: `Stop()`呼び出し後、SIGTERMに応答しないプロセスが最終的にSIGKILLで強制終了されること検証

---

#### [MODIFY] [claudecode/process.go](file://shared/libs/go/codingagent/claudecode/process.go)
*   **Description**: Graceful Shutdown (SIGTERM -> 5秒 -> SIGKILL)
*   **Technical Design**:

```go
import (
    "runtime"
    "syscall"
    "time"
)

const gracefulShutdownTimeout = 5 * time.Second

// Stop gracefully terminates the subprocess.
// 1. Send SIGTERM (Unix) or Kill (Windows)
// 2. Wait up to 5 seconds for exit
// 3. Force kill if timeout
func (pm *ProcessManager) Stop() error {
    if pm.cmd.Process == nil {
        return nil
    }

    // Platform-specific graceful signal
    if runtime.GOOS == "windows" {
        // Windows: no SIGTERM, just kill
        pm.cancel()
        return pm.cmd.Wait()
    }

    // Unix: send SIGTERM first
    pm.cmd.Process.Signal(syscall.SIGTERM)

    // Wait with timeout
    done := make(chan error, 1)
    go func() { done <- pm.cmd.Wait() }()

    select {
    case err := <-done:
        return err
    case <-time.After(gracefulShutdownTimeout):
        // Force kill
        pm.cancel()
        return <-done
    }
}
```

---

#### [MODIFY] [codex/process_test.go](file://shared/libs/go/codingagent/codex/process_test.go)
*   **Description**: 同様のGraceful Shutdownテスト追加

---

#### [MODIFY] [codex/process.go](file://shared/libs/go/codingagent/codex/process.go)
*   **Description**: Graceful Shutdown + config.toml cleanup
*   **Technical Design**:

```go
func (pm *ProcessManager) Stop() error {
    if pm.cmd.Process == nil {
        return nil
    }

    var err error
    if runtime.GOOS == "windows" {
        pm.cancel()
        err = pm.cmd.Wait()
    } else {
        pm.cmd.Process.Signal(syscall.SIGTERM)
        done := make(chan error, 1)
        go func() { done <- pm.cmd.Wait() }()
        select {
        case err = <-done:
        case <-time.After(gracefulShutdownTimeout):
            pm.cancel()
            err = <-done
        }
    }

    // Clean up temporary config.toml
    if pm.configPath != "" {
        os.RemoveAll(strings.TrimSuffix(pm.configPath, "/config.toml"))
    }
    return err
}
```

---

### codingagent パッケージ (SessionRecord JSON tags)

#### [MODIFY] [session_store.go](file://shared/libs/go/codingagent/session_store.go)
*   **Description**: SessionRecordにJSON tagsを追加してGET応答でフィールド名が正しく出力されるようにする
*   **Technical Design**:

```go
type SessionRecord struct {
    ID           string    `json:"id"`
    AgentName    string    `json:"agent_name"`
    Model        string    `json:"model"`
    Status       string    `json:"status"`
    WorkDir      string    `json:"work_dir"`
    SDKSessionID string    `json:"sdk_session_id"`
    CreatedAt    time.Time `json:"created_at"`
    UpdatedAt    time.Time `json:"updated_at"`
}
```

---

## Step-by-Step Implementation Guide

1.  **Step 1: SessionRecord JSON tags**:
    *   Edit `shared/libs/go/codingagent/session_store.go` to add JSON struct tags
    *   テスト: 既存テストが引き続きGreen

2.  **Step 2: TaskLog記録テスト + 実装 (handler)**:
    *   Edit `shared/libs/go/agentservice/handler_test.go` to add `TestHandleSendMessage_TaskLogRecording`
    *   Edit `shared/libs/go/agentservice/handler.go` to add `toAgentLogEntry()` and TaskLog記録ロジック
    *   テスト Green を確認

3.  **Step 3: SDKSessionIDテスト + 実装 (handler)**:
    *   Edit `shared/libs/go/agentservice/handler_test.go` to add `TestHandleSendMessage_SDKSessionID`
    *   Edit `shared/libs/go/agentservice/handler.go` to add EventSystem検出ロジック
    *   テスト Green を確認

4.  **Step 4: CLIバージョン テスト + 実装 (health/service)**:
    *   Edit `shared/libs/go/agentservice/health_test.go` to add `TestHealthHandler_CLIVersions`
    *   Edit `shared/libs/go/agentservice/health.go` to add `CLIVersions` field
    *   Edit `shared/libs/go/agentservice/service.go` to add `detectCLIVersions()` and `cliVersions` cache
    *   テスト Green を確認

5.  **Step 5: Graceful Shutdown テスト + 実装 (claudecode)**:
    *   Edit `shared/libs/go/codingagent/claudecode/process_test.go` to add graceful shutdown tests
    *   Edit `shared/libs/go/codingagent/claudecode/process.go` to implement SIGTERM -> timeout -> SIGKILL
    *   テスト Green を確認

6.  **Step 6: Graceful Shutdown テスト + 実装 (codex)**:
    *   Edit `shared/libs/go/codingagent/codex/process_test.go` to add graceful shutdown tests
    *   Edit `shared/libs/go/codingagent/codex/process.go` to implement SIGTERM -> timeout -> SIGKILL + cleanup
    *   テスト Green を確認

7.  **Step 7: 仕様書007の更新**:
    *   Edit `prompts/.../ideas/007-CodingAgentAPI.md` to fix Terminate API path and promote O7

8.  **Step 8: ビルド検証**:
    *   Verification Plan を実行

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```

2.  **Integration Tests** (Part5/Part6で実行):
    ```bash
    ./scripts/process/integration_test.sh --categories "common" --specify "AgentService"
    ```

### テスト項目のセルフレビュー

| # | 観点 | 結果 |
|---|------|------|
| 1 | 正常系 | streamSSE/respondJSONでTaskLog記録、SDKSessionID保存、CLIバージョン取得 |
| 2 | 異常系 | CLI不在時のunavailable、Windows環境でのSIGTERMフォールバック |
| 3 | 外部連携 | TaskLog.onEntry -> WebSocket通知の自動連携 (既存仕組み) |
| 4 | データ一貫性 | SessionRecord.SDKSessionIDがGet()で正しく返る |
| 5 | 状態遷移 | ストリーミング完了後のstatus=completed遷移 |
| 6 | 設定反映 | WithTaskLog()オプションの反映 |
| 7 | 副作用 | Graceful Shutdown後のプロセスリソース解放 |

**セルフレビュー結果**: 全観点をカバー。ボトムアップ順序: SessionRecord JSON tags -> TaskLog変換関数 -> handler統合 -> health/service -> process shutdown。

## Documentation

#### [MODIFY] [007-CodingAgentAPI.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/ideas/007-CodingAgentAPI.md)
*   **更新内容**:
    *   R5-2のTerminate APIパスを `/api/v1/sessions/:id/terminate` に修正
    *   O7 (CLIバージョン検出) を必須要件に昇格

---

## 継続計画について

本計画書はPart4 (コード品質修正) です。以下のPartが別ファイルで続きます:

- **Part5** ([013-CodingAgentAPI-Part5-ClientContainers.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/plans/013-CodingAgentAPI-Part5-ClientContainers.md)): CAWAクライアントExample + Dockerコンテナ構成
- **Part6** ([014-CodingAgentAPI-Part6-IntegrationTests.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/plans/014-CodingAgentAPI-Part6-IntegrationTests.md)): 統合テスト
