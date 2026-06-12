# 014-CodingAgentAPI-Part6-IntegrationTests

> **Source Specification**: [008-CodingAgentAPI-Completion.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/ideas/008-CodingAgentAPI-Completion.md)

## Goal Description

AgentService の統合テストを作成し、Part4 で実装した品質修正 (TaskLog記録、SDKSessionID保存、CLIバージョン、ヘルスチェック) がエンドツーエンドで正しく機能することを検証する。

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| C8-1: AgentService統合テスト | Proposed Changes > tests/common/agentservice_test.go |
| C8-1-1: TestAgentServiceHealthCheck | テストシナリオ1 |
| C8-1-2: TestAgentServiceSessionLifecycle | テストシナリオ2 |
| C8-1-3: TestAgentServiceSSEStreaming | テストシナリオ3 |
| C8-1-4: TestAgentServiceTaskLogIntegration | テストシナリオ4 |
| C8-1-5: TestAgentServiceLogStreamSSE | テストシナリオ5 |
| C8-1-6: TestAgentServiceSDKSessionID | テストシナリオ6 |
| C8-3: 手動実行前提 | テスト構造: `//go:build integration` |

## Proposed Changes

### tests/common (統合テスト)

#### [NEW] [tests/common/agentservice_test.go](file://tests/common/agentservice_test.go)
*   **Description**: AgentServiceの統合テスト。mockエージェントを使用して、HTTP APIエンドポイントの一連のフローを検証する。
*   **Technical Design**:

```go
//go:build integration

package common_test

import (
    "bufio"
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "time"

    "github.com/axsh/hag/agentservice"
    "github.com/axsh/hag/codingagent"
    "github.com/axsh/hag/tasklog"
)
```

*   **Mock Definitions**:

```go
// integrationMockAgent implements codingagent.CodingAgent for integration tests.
type integrationMockAgent struct {
    name     string
    sessions []*integrationMockSession
}

func (a *integrationMockAgent) Name() string { return a.name }
func (a *integrationMockAgent) Close() error { return nil }
func (a *integrationMockAgent) CreateSession(
    _ context.Context, _ ...codingagent.SessionOption,
) (codingagent.Session, error) {
    s := &integrationMockSession{}
    a.sessions = append(a.sessions, s)
    return s, nil
}

// integrationMockSession は複数イベントタイプを返すmockセッション
type integrationMockSession struct{}

func (s *integrationMockSession) ID() string { return "sdk-integration-001" }
func (s *integrationMockSession) Close() error { return nil }
func (s *integrationMockSession) Send(
    _ context.Context, _ string,
) (<-chan codingagent.StreamEvent, error) {
    ch := make(chan codingagent.StreamEvent, 4)
    ch <- codingagent.StreamEvent{
        Type:      codingagent.EventSystem,
        SessionID: "sdk-integration-001",
    }
    ch <- codingagent.StreamEvent{
        Type:    codingagent.EventText,
        Content: "Integration test response",
    }
    ch <- codingagent.StreamEvent{
        Type:     codingagent.EventToolUse,
        ToolName: "write_file",
        Content:  `{"path":"hello.py","content":"print('hello')"}`,
    }
    ch <- codingagent.StreamEvent{Type: codingagent.EventResult}
    close(ch)
    return ch, nil
}
```

*   **Helper**:

```go
// setupTestServer creates a test server with mock agent and TaskLog.
func setupTestServer(t *testing.T) (*httptest.Server, *tasklog.TaskLog) {
    t.Helper()
    tl := tasklog.New()
    srv := agentservice.New(
        agentservice.WithTaskLog(tl),
    )
    srv.RegisterAgent(&integrationMockAgent{name: "claudecode"})
    ts := httptest.NewServer(srv.HTTPHandler())
    t.Cleanup(ts.Close)
    return ts, tl
}

// createSession は POST /api/v1/sessions でセッションを作成しIDを返す
func createSession(t *testing.T, baseURL, agent string) string {
    t.Helper()
    body, _ := json.Marshal(map[string]string{"agent": agent})
    resp, err := http.Post(baseURL+"/api/v1/sessions",
        "application/json", bytes.NewReader(body))
    if err != nil {
        t.Fatalf("create session: %v", err)
    }
    defer resp.Body.Close()
    if resp.StatusCode != http.StatusCreated {
        t.Fatalf("create session: status=%d", resp.StatusCode)
    }
    var result map[string]string
    json.NewDecoder(resp.Body).Decode(&result)
    return result["session_id"]
}
```

---

*   **テストシナリオ1: TestAgentServiceHealthCheck**

```go
func TestAgentServiceHealthCheck(t *testing.T) {
    ts, _ := setupTestServer(t)

    resp, err := http.Get(ts.URL + "/health")
    if err != nil {
        t.Fatalf("health check: %v", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        t.Errorf("status = %d, want 200", resp.StatusCode)
    }

    var health struct {
        Status      string            `json:"status"`
        Agents      []string          `json:"agents"`
        CLIVersions map[string]string `json:"cli_versions"`
        Gateway     struct {
            Status string `json:"status"`
        } `json:"gateway"`
    }
    json.NewDecoder(resp.Body).Decode(&health)

    if health.Status != "ok" {
        t.Errorf("health.status = %q, want ok", health.Status)
    }
    if len(health.Agents) != 1 || health.Agents[0] != "claudecode" {
        t.Errorf("agents = %v, want [claudecode]", health.Agents)
    }
    // cli_versions should exist (may be "unavailable" in test env)
    if health.CLIVersions == nil {
        t.Error("cli_versions should not be nil")
    }
    if _, ok := health.CLIVersions["claudecode"]; !ok {
        t.Error("cli_versions should contain claudecode entry")
    }
}
```

---

*   **テストシナリオ2: TestAgentServiceSessionLifecycle**

```go
func TestAgentServiceSessionLifecycle(t *testing.T) {
    ts, _ := setupTestServer(t)

    // 1. Create session
    sessionID := createSession(t, ts.URL, "claudecode")
    if sessionID == "" {
        t.Fatal("session_id should not be empty")
    }

    // 2. Get session
    resp, _ := http.Get(ts.URL + "/api/v1/sessions/" + sessionID)
    if resp.StatusCode != http.StatusOK {
        t.Fatalf("get session: status=%d", resp.StatusCode)
    }
    var record codingagent.SessionRecord
    json.NewDecoder(resp.Body).Decode(&record)
    resp.Body.Close()
    if record.Status != codingagent.StatusActive {
        t.Errorf("status = %q, want active", record.Status)
    }

    // 3. Terminate
    resp, _ = http.Post(ts.URL+"/api/v1/sessions/"+sessionID+"/terminate",
        "application/json", nil)
    if resp.StatusCode != http.StatusOK {
        t.Fatalf("terminate: status=%d", resp.StatusCode)
    }
    resp.Body.Close()

    // 4. Verify status = closed
    resp, _ = http.Get(ts.URL + "/api/v1/sessions/" + sessionID)
    json.NewDecoder(resp.Body).Decode(&record)
    resp.Body.Close()
    if record.Status != codingagent.StatusClosed {
        t.Errorf("after terminate: status = %q, want closed", record.Status)
    }

    // 5. Delete
    req, _ := http.NewRequest("DELETE",
        ts.URL+"/api/v1/sessions/"+sessionID, nil)
    resp, _ = http.DefaultClient.Do(req)
    if resp.StatusCode != http.StatusNoContent {
        t.Fatalf("delete: status=%d", resp.StatusCode)
    }
    resp.Body.Close()

    // 6. Verify not found
    resp, _ = http.Get(ts.URL + "/api/v1/sessions/" + sessionID)
    if resp.StatusCode != http.StatusNotFound {
        t.Errorf("after delete: status = %d, want 404", resp.StatusCode)
    }
    resp.Body.Close()
}
```

---

*   **テストシナリオ3: TestAgentServiceSSEStreaming**

```go
func TestAgentServiceSSEStreaming(t *testing.T) {
    ts, _ := setupTestServer(t)
    sessionID := createSession(t, ts.URL, "claudecode")

    // Send message with SSE
    msgBody, _ := json.Marshal(map[string]string{"message": "test prompt"})
    req, _ := http.NewRequest("POST",
        ts.URL+"/api/v1/sessions/"+sessionID+"/messages",
        bytes.NewReader(msgBody))
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Accept", "text/event-stream")

    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        t.Fatalf("send message: %v", err)
    }
    defer resp.Body.Close()

    if resp.Header.Get("Content-Type") != "text/event-stream" {
        t.Errorf("Content-Type = %q, want text/event-stream",
            resp.Header.Get("Content-Type"))
    }

    // Parse SSE events
    var events []codingagent.StreamEvent
    scanner := bufio.NewScanner(resp.Body)
    for scanner.Scan() {
        line := scanner.Text()
        if !strings.HasPrefix(line, "data: ") {
            continue
        }
        data := strings.TrimPrefix(line, "data: ")
        if data == "[DONE]" {
            break
        }
        var ev codingagent.StreamEvent
        if json.Unmarshal([]byte(data), &ev) == nil {
            events = append(events, ev)
        }
    }

    if len(events) < 3 {
        t.Fatalf("expected at least 3 events, got %d", len(events))
    }

    // Verify event types
    expectedTypes := []string{
        codingagent.EventSystem,
        codingagent.EventText,
        codingagent.EventToolUse,
    }
    for i, expected := range expectedTypes {
        if events[i].Type != expected {
            t.Errorf("event[%d].Type = %q, want %q", i, events[i].Type, expected)
        }
    }
}
```

---

*   **テストシナリオ4: TestAgentServiceTaskLogIntegration**

```go
func TestAgentServiceTaskLogIntegration(t *testing.T) {
    ts, tl := setupTestServer(t)
    sessionID := createSession(t, ts.URL, "claudecode")

    // Verify TaskLog is empty before message
    if len(tl.Entries()) != 0 {
        t.Fatalf("TaskLog should be empty before message, got %d entries",
            len(tl.Entries()))
    }

    // Send message (JSON mode to collect all at once)
    msgBody, _ := json.Marshal(map[string]string{"message": "test"})
    resp, _ := http.Post(
        ts.URL+"/api/v1/sessions/"+sessionID+"/messages",
        "application/json", bytes.NewReader(msgBody))
    io.ReadAll(resp.Body)
    resp.Body.Close()

    // Wait briefly for async processing
    time.Sleep(100 * time.Millisecond)

    // Verify TaskLog has entries
    entries := tl.Entries()
    if len(entries) == 0 {
        t.Fatal("TaskLog should have entries after message send")
    }

    // Verify entries are AgentLogEntry type
    for _, entry := range entries {
        if entry.Type() != tasklog.AgentLogEntryType {
            t.Errorf("entry.Type() = %q, want %q",
                entry.Type(), tasklog.AgentLogEntryType)
        }
    }
}
```

---

*   **テストシナリオ5: TestAgentServiceLogStreamSSE**

```go
func TestAgentServiceLogStreamSSE(t *testing.T) {
    ts, tl := setupTestServer(t)
    sessionID := createSession(t, ts.URL, "claudecode")

    // Add some TaskLog entries manually
    tl.Add(tasklog.NewAgentLogSendEntry("log-1", "claudecode", "test body"))

    // Request log stream
    req, _ := http.NewRequest("GET",
        ts.URL+"/api/v1/sessions/"+sessionID+"/logs", nil)
    req.Header.Set("Accept", "text/event-stream")

    // Use a timeout context to avoid hanging
    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
    defer cancel()
    req = req.WithContext(ctx)

    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        t.Fatalf("log stream: %v", err)
    }
    defer resp.Body.Close()

    if resp.Header.Get("Content-Type") != "text/event-stream" {
        t.Errorf("Content-Type = %q, want text/event-stream",
            resp.Header.Get("Content-Type"))
    }

    // Read at least one log event
    scanner := bufio.NewScanner(resp.Body)
    foundLog := false
    for scanner.Scan() {
        line := scanner.Text()
        if strings.HasPrefix(line, "event: log") {
            foundLog = true
            break
        }
    }

    if !foundLog {
        t.Error("expected at least one 'event: log' in SSE stream")
    }
}
```

---

*   **テストシナリオ6: TestAgentServiceSDKSessionID**

```go
func TestAgentServiceSDKSessionID(t *testing.T) {
    ts, _ := setupTestServer(t)
    sessionID := createSession(t, ts.URL, "claudecode")

    // Send message with SSE (triggers EventSystem with SessionID)
    msgBody, _ := json.Marshal(map[string]string{"message": "test"})
    req, _ := http.NewRequest("POST",
        ts.URL+"/api/v1/sessions/"+sessionID+"/messages",
        bytes.NewReader(msgBody))
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Accept", "text/event-stream")

    resp, _ := http.DefaultClient.Do(req)
    io.ReadAll(resp.Body)
    resp.Body.Close()

    // Verify SDKSessionID was saved
    resp, _ = http.Get(ts.URL + "/api/v1/sessions/" + sessionID)
    var record struct {
        SDKSessionID string `json:"sdk_session_id"`
        Status       string `json:"status"`
    }
    json.NewDecoder(resp.Body).Decode(&record)
    resp.Body.Close()

    if record.SDKSessionID != "sdk-integration-001" {
        t.Errorf("sdk_session_id = %q, want sdk-integration-001",
            record.SDKSessionID)
    }
    if record.Status != codingagent.StatusCompleted {
        t.Errorf("status = %q, want completed", record.Status)
    }
}
```

---

## Step-by-Step Implementation Guide

1.  **Step 1: テストファイルの作成**:
    *   Create `tests/common/agentservice_test.go` with `//go:build integration` tag
    *   Mock definitions, helper functions, 6つのテストシナリオを実装

2.  **Step 2: ビルド検証**:
    *   `./scripts/process/build.sh` でコンパイルエラーがないことを確認

3.  **Step 3: 統合テスト実行**:
    *   `./scripts/process/integration_test.sh --categories "common" --specify "AgentService"` で6テストが全てGreen

4.  **Step 4: 総合判定**:
    *   testing-rules.md 12 に従い、総合判定プロセスを実施

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```

2.  **Integration Tests** (AgentService統合テスト):
    ```bash
    ./scripts/process/integration_test.sh --categories "common" --specify "AgentService"
    ```
    *   **Log Verification**:
        *   6テスト全てPASS
        *   `TestAgentServiceTaskLogIntegration` でTaskLogにエントリが記録されていること
        *   `TestAgentServiceSDKSessionID` で `sdk_session_id = "sdk-integration-001"` が返ること

### テスト項目のセルフレビュー

| # | 観点 | 結果 |
|---|------|------|
| 1 | 正常系 | Health, SessionLifecycle, SSEStreaming, TaskLog, LogStream, SDKSessionID |
| 2 | 異常系 | SessionLifecycleで404確認、HealthCheckでcli_versions="unavailable"許容 |
| 3 | 外部連携 | httptest.Serverでの実HTTP通信 (mockではなく実サーバ) |
| 4 | データ一貫性 | session create -> message -> get で状態が正しく遷移 |
| 5 | 状態遷移 | active -> completed (SSE後), active -> closed (terminate後), closed -> deleted |
| 6 | 設定反映 | WithTaskLog()が正しくServer に注入され、handleSendMessage内で記録される |
| 7 | 副作用 | httptest.Server の Cleanup でリソース解放 |

**セルフレビュー結果**:
- **網羅性**: 6テストで仕様書のC1-C4の主要機能を網羅。ボトムアップ順序: HealthCheck (末端) -> SessionLifecycle -> SSEStreaming -> TaskLog -> LogStream -> SDKSessionID (依存関係順)
- **証拠の十分性**: 各テストで具体的な値 (status, event types, session_id) を検証。"エラーが出ない" だけではなく "期待値が返る" を確認
- **迂回排除**: mockエージェントが意図したイベントを返し、それがAPI応答に正しく反映されることを確認
- **依存関係**: Part4 (品質修正) が実装済みであることが前提。Part4のテストがGreenであれば本テストも意味を持つ

### 総合判定プロセス

Part4-6 の全テスト完了後、testing-rules.md 12.2 に従い以下を確認:

1. スキップされたテストの有無 -> `t.Skip` 禁止ルール遵守
2. 部分的エラーの見落とし -> ログ内の ERROR/WARN を確認
3. 迂回処理による偽成功 -> mockが意図したイベント列を返していることを確認
4. アダプタ・コンフィグの誤適用 -> RegisterAgent("claudecode") が使用されていることを検証
5. テスト間の依存 -> 各テストが独立した `setupTestServer` を使用
6. カバレッジの妥当性 -> Part4の新機能 (TaskLog, SDKSessionID, CLIVersions) を全て検証
7. 外部システムの状態 -> httptest.Server (in-process) のため外部依存なし

---

## 継続計画について

本計画書はPart6 (統合テスト) です。以下のPartが別ファイルで参照可能です:

- **Part4** ([012-CodingAgentAPI-Part4-QualityFixes.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/plans/012-CodingAgentAPI-Part4-QualityFixes.md)): コード品質修正 (C1-C5)
- **Part5** ([013-CodingAgentAPI-Part5-ClientContainers.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/plans/013-CodingAgentAPI-Part5-ClientContainers.md)): CAWAクライアント + コンテナ構成 (C6-C9)
