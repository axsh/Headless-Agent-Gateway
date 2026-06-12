# 016-Fix-CodingAgent-Streaming

> **Source Specification**: prompts/phases/000-foundation/branches/feat-llm-backend/ideas/011-Fix-CodingAgent-Streaming.md

## Goal Description

cawa-client run で SSE ストリーミングイベントが一切表示されない問題を修正する。
原因は複合的 (BuildEnv の API キーロジック、standalone の GatewayURL 未設定、stderr 未キャプチャ、終了コード未確認) であり、全てを修正しテストで検証する。

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: BuildEnv の API キー設定ロジック修正 | Step 2: process.go |
| R2: standalone の GatewayURL 設定 | Step 4: main.go |
| R3: stderr のキャプチャとログ出力 | Step 3: process.go |
| R4: プロセス終了コードの確認 | Step 3: process.go |

## Proposed Changes

### claudecode パッケージ (単体テスト)

#### [MODIFY] [process_test.go](file:///shared/libs/go/codingagent/claudecode/process_test.go)
*   **Description**: BuildEnv テストの修正と追加。stderr キャプチャ / 終了コード確認のテスト追加。
*   **Technical Design**:

**TestBuildEnv の修正**:

既存テストケース `"API key always set to not-needed"` を以下の 2 ケースに置き換え:

```go
{
    name:    "no gateway URL: ANTHROPIC_API_KEY not set",
    ac:      &codingagent.AdapterConfig{},
    cfg:     &codingagent.SessionConfig{},
    wantNot: "ANTHROPIC_API_KEY",
},
{
    name:    "no gateway URL: ANTHROPIC_BASE_URL not set",
    ac:      &codingagent.AdapterConfig{},
    cfg:     &codingagent.SessionConfig{},
    wantNot: "ANTHROPIC_BASE_URL",
},
{
    name:    "with gateway URL: ANTHROPIC_API_KEY set to not-needed",
    ac:      &codingagent.AdapterConfig{GatewayURL: "http://localhost:14000"},
    cfg:     &codingagent.SessionConfig{},
    wantKey: "ANTHROPIC_API_KEY",
    wantVal: "not-needed",
},
```

**TestBuildEnv_SessionEnvVarsOverride** (新規):

```go
func TestBuildEnv_SessionEnvVarsOverride(t *testing.T) {
    ac := &codingagent.AdapterConfig{GatewayURL: "http://gw:14000"}
    cfg := &codingagent.SessionConfig{
        EnvVars: map[string]string{"ANTHROPIC_API_KEY": "real-key"},
    }
    env := claudecode.BuildEnv(ac, cfg)
    // SessionConfig.EnvVars should override the placeholder
    envMap := toEnvMap(env)
    if envMap["ANTHROPIC_API_KEY"] != "real-key" {
        t.Errorf("session env var should override placeholder")
    }
}
```

**TestStartProcess_StderrCapture** (新規):

プラットフォーム非依存で検証するため、存在しないコマンドではなく `echo` + 即失敗するスクリプトを使う。`StartProcess` を直接テストするのは難しいため、代わりに `StartProcessWithCommand` ヘルパーを追加してテストする。

---

#### [MODIFY] [process.go](file:///shared/libs/go/codingagent/claudecode/process.go)
*   **Description**: BuildEnv ロジック修正 + stderr キャプチャ + 終了コード確認
*   **Technical Design**:

**BuildEnv の修正** (R1):

```go
func BuildEnv(ac *codingagent.AdapterConfig, cfg *codingagent.SessionConfig) []string {
    env := make(map[string]string)

    if ac.GatewayURL != "" {
        env["ANTHROPIC_BASE_URL"] = ac.GatewayURL
        // Gateway handles auth; CLI needs a non-empty key to proceed.
        env["ANTHROPIC_API_KEY"] = "not-needed"
    }

    if ac.DisableSandbox {
        env["CLAUDE_CODE_SKIP_SANDBOX"] = "1"
    }

    for k, v := range cfg.EnvVars {
        env[k] = v
    }

    var result []string
    for k, v := range env {
        result = append(result, k+"="+v)
    }
    return result
}
```

**StartProcess の修正** (R3, R4):

```go
func StartProcess(
    ctx context.Context,
    ac *codingagent.AdapterConfig,
    cfg *codingagent.SessionConfig,
) (<-chan codingagent.StreamEvent, *ProcessManager, error) {
    procCtx, cancel := context.WithCancel(ctx)

    args := BuildArgs(cfg)
    cmd := exec.CommandContext(procCtx, "claude", args...)
    cmd.Dir = cfg.WorkDir
    cmd.Env = append(cmd.Environ(), BuildEnv(ac, cfg)...)

    stdout, err := cmd.StdoutPipe()
    if err != nil {
        cancel()
        return nil, nil, fmt.Errorf("stdout pipe: %w", err)
    }

    // R3: Capture stderr
    var stderrBuf bytes.Buffer
    cmd.Stderr = &stderrBuf

    if err := cmd.Start(); err != nil {
        cancel()
        return nil, nil, fmt.Errorf("start claude: %w", err)
    }

    ch := make(chan codingagent.StreamEvent, 64)
    pm := &ProcessManager{cmd: cmd, cancel: cancel}

    go func() {
        defer close(ch)
        scanner := bufio.NewScanner(stdout)
        for scanner.Scan() {
            line := scanner.Text()
            ev := ParseJSONLinesEvent(line)
            if ev != nil {
                select {
                case ch <- *ev:
                case <-procCtx.Done():
                    return
                }
            }
        }
        // R4: Check exit code and report stderr
        if err := cmd.Wait(); err != nil {
            errMsg := strings.TrimSpace(stderrBuf.String())
            if errMsg == "" {
                errMsg = err.Error()
            }
            select {
            case ch <- codingagent.StreamEvent{
                Type:    codingagent.EventError,
                Content: errMsg,
            }:
            case <-procCtx.Done():
            }
        }
    }()

    return ch, pm, nil
}
```

**import の追加**: `"bytes"`, `"strings"` を追加。

---

### standalone (例: main.go)

#### [MODIFY] [main.go](file:///examples/standalone/main.go)
*   **Description**: registerCodingAgents で Gateway URL を渡す (R2)
*   **Technical Design**:

```go
func registerCodingAgents(srv *hag.Server) {
    if _, err := exec.LookPath("claude"); err == nil {
        gwURL := srv.Gateway().ProxyURL()
        adapter := claudecode.New(&codingagent.AdapterConfig{
            GatewayURL: gwURL,
        })
        srv.AgentService().RegisterAgent(adapter)
        fmt.Printf("Registered coding agent: claudecode (gateway=%s)\n", gwURL)
    } else {
        fmt.Println("Warning: claude CLI not found, claudecode agent not registered")
    }
}
```

`srv.Gateway().ProxyURL()` は既に `LLMGatewayBackend` インターフェースに存在する。

---

### cawa-client (エラー表示改善)

#### [MODIFY] [main.go](file:///examples/cawa-client/main.go)
*   **Description**: streamSSE で `EventError` イベントを表示する
*   **Technical Design**:

`streamSSE` の switch に `"error"` ケースを追加:

```go
case "error":
    fmt.Fprintf(os.Stderr, "\n[Error] %s\n", ev.Content)
```

---

### 統合テスト

#### [MODIFY] [agentservice_integration_test.go](file:///tests/agentservice_integration_test.go)
*   **Description**: SSE ストリーミングで具体的なイベント内容まで検証するテスト、エラーイベント伝播テストを追加
*   **Technical Design**:

**TestAgentServiceSSEStreamingContent** (新規):

```go
func TestAgentServiceSSEStreamingContent(t *testing.T) {
    ts, _ := setupAgentServiceTestServer(t)
    sessionID := createAgentServiceSession(t, ts.URL, "claudecode")

    msgBody, _ := json.Marshal(map[string]string{"message": "test"})
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

    var events []codingagent.StreamEvent
    var gotDone bool
    scanner := bufio.NewScanner(resp.Body)
    for scanner.Scan() {
        line := scanner.Text()
        if !strings.HasPrefix(line, "data: ") {
            continue
        }
        data := strings.TrimPrefix(line, "data: ")
        if data == "[DONE]" {
            gotDone = true
            break
        }
        var ev codingagent.StreamEvent
        if json.Unmarshal([]byte(data), &ev) == nil {
            events = append(events, ev)
        }
    }

    // Verify [DONE] sentinel was received
    if !gotDone {
        t.Fatal("expected [DONE] sentinel in SSE stream")
    }

    // Verify event content, not just types
    if len(events) < 4 {
        t.Fatalf("expected at least 4 events, got %d", len(events))
    }

    // system event should have session_id
    if events[0].Type != codingagent.EventSystem {
        t.Errorf("event[0].Type = %q, want system", events[0].Type)
    }
    if events[0].SessionID == "" {
        t.Error("system event should have non-empty session_id")
    }

    // text event should have content
    if events[1].Type != codingagent.EventText {
        t.Errorf("event[1].Type = %q, want text", events[1].Type)
    }
    if events[1].Content == "" {
        t.Error("text event should have non-empty content")
    }

    // tool_use event should have tool_name
    if events[2].Type != codingagent.EventToolUse {
        t.Errorf("event[2].Type = %q, want tool_use", events[2].Type)
    }
    if events[2].ToolName == "" {
        t.Error("tool_use event should have non-empty tool_name")
    }

    // result event
    if events[3].Type != codingagent.EventResult {
        t.Errorf("event[3].Type = %q, want result", events[3].Type)
    }
}
```

**TestAgentServiceSSEErrorPropagation** (新規):

エラーを返す mock agent を使い、`EventError` が SSE で伝播されることを確認:

```go
// errorMockSession returns an error event
type errorMockSession struct{}

func (s *errorMockSession) ID() string  { return "sdk-error-001" }
func (s *errorMockSession) Close() error { return nil }
func (s *errorMockSession) Send(
    _ context.Context, _ string,
) (<-chan codingagent.StreamEvent, error) {
    ch := make(chan codingagent.StreamEvent, 2)
    ch <- codingagent.StreamEvent{
        Type:    codingagent.EventError,
        Content: "claude exited with code 1: authentication failed",
    }
    close(ch)
    return ch, nil
}

func TestAgentServiceSSEErrorPropagation(t *testing.T) {
    tl := tasklog.New()
    srv := agentservice.New(agentservice.WithTaskLog(tl))
    
    errorAgent := &integrationMockAgent{name: "erroragent"}
    // Override CreateSession to return errorMockSession
    // (実装ではerrorMockAgentを別途定義する必要あり)
    srv.RegisterAgent(errorAgent) 
    ts := httptest.NewServer(srv.HTTPHandler())
    defer ts.Close()

    sessionID := createAgentServiceSession(t, ts.URL, "erroragent")
    msgBody, _ := json.Marshal(map[string]string{"message": "test"})
    req, _ := http.NewRequest("POST",
        ts.URL+"/api/v1/sessions/"+sessionID+"/messages",
        bytes.NewReader(msgBody))
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Accept", "text/event-stream")

    resp, _ := http.DefaultClient.Do(req)
    defer resp.Body.Close()

    var foundError bool
    scanner := bufio.NewScanner(resp.Body)
    for scanner.Scan() {
        line := scanner.Text()
        if !strings.HasPrefix(line, "data: ") { continue }
        data := strings.TrimPrefix(line, "data: ")
        if data == "[DONE]" { break }
        var ev codingagent.StreamEvent
        if json.Unmarshal([]byte(data), &ev) == nil {
            if ev.Type == codingagent.EventError {
                foundError = true
                if ev.Content == "" {
                    t.Error("error event should have non-empty content")
                }
            }
        }
    }

    if !foundError {
        t.Error("expected at least one error event in SSE stream")
    }
}
```

---

## Step-by-Step Implementation Guide

### Step 1: 単体テスト修正 (TDD - Red)

*   [x] Edit `shared/libs/go/codingagent/claudecode/process_test.go`:
    *   既存の `"API key always set to not-needed"` テストケースを削除
    *   以下の 3 テストケースを追加:
        1. `"no gateway URL: ANTHROPIC_API_KEY not set"` - `wantNot: "ANTHROPIC_API_KEY"`
        2. `"no gateway URL: ANTHROPIC_BASE_URL not set"` - `wantNot: "ANTHROPIC_BASE_URL"`
        3. `"with gateway URL: ANTHROPIC_API_KEY set to not-needed"` - `wantKey: "ANTHROPIC_API_KEY"`, `wantVal: "not-needed"`
    *   `TestBuildEnv_SessionEnvVarsOverride` テスト関数を新規追加
*   ビルドして新テストが FAIL することを確認

### Step 2: BuildEnv ロジック修正 (Green)

*   [x] Edit `shared/libs/go/codingagent/claudecode/process.go`:
    *   `BuildEnv` 関数の L55-56 (`env["ANTHROPIC_API_KEY"] = "not-needed"`) を `if ac.GatewayURL != ""` ブロック内に移動
    *   修正後のロジック:
        ```go
        if ac.GatewayURL != "" {
            env["ANTHROPIC_BASE_URL"] = ac.GatewayURL
            env["ANTHROPIC_API_KEY"] = "not-needed"
        }
        ```
*   ビルドして Step 1 のテストが PASS することを確認
*   `git commit -m "fix: only set ANTHROPIC_API_KEY when GatewayURL is configured"`

### Step 3: stderr キャプチャ + 終了コード確認 (R3, R4)

*   [x] Edit `shared/libs/go/codingagent/claudecode/process.go`:
    *   import に `"bytes"`, `"strings"` を追加
    *   `StartProcess` 関数に以下を追加:
        1. `var stderrBuf bytes.Buffer` を宣言
        2. `cmd.Stderr = &stderrBuf` を設定
        3. goroutine 末尾の scanner ループ後に `cmd.Wait()` を呼び出し
        4. exit code != 0 の場合 `EventError` イベントを ch に送信
        5. `Content` フィールドに `stderrBuf.String()` の内容を含める
*   ビルドして既存テストが PASS することを確認
*   `git commit -m "feat: capture stderr and check exit code in StartProcess"`

### Step 4: standalone GatewayURL 設定 (R2)

*   [x] Edit `examples/standalone/main.go`:
    *   `registerCodingAgents` 内で `srv.Gateway().ProxyURL()` から GatewayURL を取得
    *   `codingagent.AdapterConfig{GatewayURL: gwURL}` で adapter を作成
    *   ログメッセージに GatewayURL を含める
*   ビルドを確認
*   `git commit -m "fix: pass GatewayURL to claudecode adapter in standalone"`

### Step 5: cawa-client エラー表示

*   [x] Edit `examples/cawa-client/main.go`:
    *   `streamSSE` の switch 文に `case "error":` を追加
    *   `fmt.Fprintf(os.Stderr, "\n[Error] %s\n", ev.Content)` で stderr に表示
*   ビルドを確認
*   `git commit -m "feat: display error events in cawa-client SSE stream"`

### Step 6: 統合テスト追加

*   [x] Edit `tests/agentservice_integration_test.go`:
    *   `TestAgentServiceSSEStreamingContent` を追加 (イベント内容の具体的な検証)
    *   `errorMockAgent` / `errorMockSession` の mock を追加
    *   `TestAgentServiceSSEErrorPropagation` を追加 (エラーイベント伝播の検証)
*   ビルドしてテストが PASS することを確認
*   `git commit -m "test: add SSE content verification and error propagation tests"`

### Step 7: 全体ビルド・テスト実行

*   [x] Verification Plan を実行 (後述)

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```
    *   BuildEnv の新テスト 3 件が全て PASS すること
    *   既存テストにリグレッションがないこと

2.  **Integration Tests**:
    ```bash
    ./scripts/process/integration_test.sh --specify "AgentService"
    ```
    *   `TestAgentServiceSSEStreaming` が PASS (既存)
    *   `TestAgentServiceSSEStreamingContent` が PASS (新規 - イベント内容検証)
    *   `TestAgentServiceSSEErrorPropagation` が PASS (新規 - エラー伝播)
    *   `TestAgentServiceLaunchShutdown` が PASS (既存)
    *   `TestAgentServiceConfigPort` が PASS (既存)
    *   **Log Verification**: テストログに `SKIP`, `WARN`, `panic` がないこと

### テスト項目設計のセルフレビュー

#### 11.3 観点チェックリスト

| # | 観点 | カバー状況 |
|---|------|-----------|
| 1 | 正常系の動作確認 | TestBuildEnv (with gateway), TestAgentServiceSSEStreamingContent |
| 2 | 異常系・境界値 | TestBuildEnv (no gateway), TestAgentServiceSSEErrorPropagation |
| 3 | 外部連携の実動作 | TestAgentServiceLaunchShutdown (HTTP), TestAgentServiceConfigPort (hag.Server) |
| 4 | データの一貫性 | TestAgentServiceSSEStreamingContent (event content verification) |
| 5 | 状態遷移の検証 | TestAgentServiceSessionLifecycle (既存) |
| 6 | 設定・構成の反映 | TestBuildEnv_SessionEnvVarsOverride, standalone GatewayURL |
| 7 | 副作用の確認 | TestAgentServiceSSEErrorPropagation (error event reaches client) |

#### 11.4 セルフレビュー結果

1. **網羅性**: R1-R4 全ての要件に対応するテストが存在。暗黙の要件 (SessionEnvVars のオーバーライド) もカバー。
2. **証拠の十分性**: イベントの型だけでなく、Content/SessionID/ToolName の具体的な値を検証。
3. **迂回排除**: BuildEnv のテストで GatewayURL なし/ありの両方を検証し、条件分岐の両パスをカバー。
4. **依存関係**: ボトムアップ順序 (BuildEnv -> StartProcess -> AgentService HTTP -> SSE) で設計。

### 総合判定プロセス (12章)

全テスト完了後に testing-rules.md 12.2 のチェックリストを実施し、総合判定結果を walkthrough に記載する。

## Documentation

#### [MODIFY] [README.md](file:///README.md)
*   **更新内容**: GatewayURL 伝播の説明を「デモ実行フロー」セクションの注意事項として追加。standalone が自動的に Gateway URL を Claude CLI に渡すことを明記。
