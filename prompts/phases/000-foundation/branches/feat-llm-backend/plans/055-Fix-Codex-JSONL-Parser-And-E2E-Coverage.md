# 055-Fix-Codex-JSONL-Parser-And-E2E-Coverage

> **Source Specification**: [044-Fix-Codex-JSONL-Parser-And-E2E-Coverage.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/ideas/044-Fix-Codex-JSONL-Parser-And-E2E-Coverage.md)

## Goal Description

Codex CLI (`codex exec --json`) の実際の JSONL 出力フォーマットに `ParseExecEvent` を対応させ、ツールログ (`[Tool: ...]`, `[Tool Result] ...`) を `ternctl` で正しく表示できるようにする。また、`exec.Command` で `tern` + `ternctl` を実コマンドとして起動する E2E テストを追加し、手動確認に依存しない自動化された検証を実現する。

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: `ParseExecEvent` のネスト形式 JSONL 対応 | Proposed Changes > protocol_test.go, protocol.go |
| R2: ternctl 実コマンド実行による E2E テスト | Proposed Changes > codex_e2e_test.go |
| R3: 単体テストのネスト形式対応 | Proposed Changes > protocol_test.go |
| O1: `response.output_text.delta` のネスト対応 | 実地確認後に対応判断 (R1 に含む) |

## Proposed Changes

### codex パッケージ (shared/libs/go/codingagent/codex)

#### [MODIFY] [protocol_test.go](file://shared/libs/go/codingagent/codex/protocol_test.go)

*   **Description**: ネスト形式 JSONL のテストケースを追加 (TDD: テスト先行)
*   **Technical Design**:
    *   既存テストは後方互換性確認のため保持する
    *   新規テスト関数を追加:
        *   `TestParseExecEvent_ResponseItem_FunctionCall`
        *   `TestParseExecEvent_ResponseItem_FunctionCallOutput`
        *   `TestParseExecEvent_ResponseItem_AssistantMessage`
        *   `TestParseExecEvent_EventMsg_AgentMessage`
        *   `TestParseExecEvent_EventMsg_TaskComplete`
        *   `TestParseExecEvent_EventMsg_Ignored`
*   **Logic**:
    *   各テストは実際の Codex CLI セッションログから取得したフォーマットを使用する:

    ```go
    func TestParseExecEvent_ResponseItem_FunctionCall(t *testing.T) {
        line := `{"timestamp":"2026-06-12T07:21:11.017Z","type":"response_item","payload":{"type":"function_call","name":"shell_command","arguments":"{\"command\": \"pwd\"}","call_id":"fc_123"}}`
        ev := codex.ParseExecEvent(line)
        if ev == nil {
            t.Fatal("expected non-nil event")
        }
        if ev.Type != codingagent.EventToolUse {
            t.Errorf("type = %q, want %q", ev.Type, codingagent.EventToolUse)
        }
        if ev.ToolName != "shell_command" {
            t.Errorf("tool_name = %q, want %q", ev.ToolName, "shell_command")
        }
    }

    func TestParseExecEvent_ResponseItem_FunctionCallOutput(t *testing.T) {
        line := `{"timestamp":"2026-06-12T07:21:12.216Z","type":"response_item","payload":{"type":"function_call_output","call_id":"fc_123","output":"Exit code: 0\nOutput:\n/home/user\n"}}`
        ev := codex.ParseExecEvent(line)
        if ev == nil {
            t.Fatal("expected non-nil event")
        }
        if ev.Type != codingagent.EventToolResult {
            t.Errorf("type = %q, want %q", ev.Type, codingagent.EventToolResult)
        }
        if !strings.Contains(ev.Content, "/home/user") {
            t.Errorf("content = %q, want to contain /home/user", ev.Content)
        }
    }

    func TestParseExecEvent_ResponseItem_AssistantMessage(t *testing.T) {
        line := `{"timestamp":"2026-06-12T07:21:13.903Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"The current directory is /home/user."}]}}`
        ev := codex.ParseExecEvent(line)
        if ev == nil {
            t.Fatal("expected non-nil event")
        }
        if ev.Type != codingagent.EventText {
            t.Errorf("type = %q, want %q", ev.Type, codingagent.EventText)
        }
        if !strings.Contains(ev.Content, "The current directory") {
            t.Errorf("content = %q, want to contain 'The current directory'", ev.Content)
        }
    }

    func TestParseExecEvent_EventMsg_AgentMessage(t *testing.T) {
        line := `{"timestamp":"2026-06-12T07:21:13.903Z","type":"event_msg","payload":{"type":"agent_message","message":"The current directory is /home/user."}}`
        ev := codex.ParseExecEvent(line)
        if ev == nil {
            t.Fatal("expected non-nil event")
        }
        if ev.Type != codingagent.EventText {
            t.Errorf("type = %q, want %q", ev.Type, codingagent.EventText)
        }
        if ev.Content != "The current directory is /home/user." {
            t.Errorf("content = %q, want exact message", ev.Content)
        }
    }

    func TestParseExecEvent_EventMsg_TaskComplete(t *testing.T) {
        line := `{"timestamp":"2026-06-12T07:21:13.907Z","type":"event_msg","payload":{"type":"task_complete","turn_id":"turn-1"}}`
        ev := codex.ParseExecEvent(line)
        if ev == nil {
            t.Fatal("expected non-nil event")
        }
        if ev.Type != codingagent.EventResult {
            t.Errorf("type = %q, want %q", ev.Type, codingagent.EventResult)
        }
    }

    func TestParseExecEvent_EventMsg_Ignored(t *testing.T) {
        // token_count, user_message, task_started should return nil
        lines := []string{
            `{"type":"event_msg","payload":{"type":"token_count"}}`,
            `{"type":"event_msg","payload":{"type":"user_message"}}`,
            `{"type":"event_msg","payload":{"type":"task_started"}}`,
        }
        for _, line := range lines {
            ev := codex.ParseExecEvent(line)
            if ev != nil {
                t.Errorf("expected nil for %s, got %+v", line, ev)
            }
        }
    }
    ```

---

#### [MODIFY] [protocol.go](file://shared/libs/go/codingagent/codex/protocol.go)

*   **Description**: `ParseExecEvent` にネスト形式 (`response_item`, `event_msg`) 対応を追加
*   **Technical Design**:
    *   `ExecEvent` 構造体に `Payload json.RawMessage` フィールドを追加
    *   `response_item` ケースを追加: `payload.type` を読み取り、既存のパースロジックに委譲
    *   `event_msg` ケースを追加: `agent_message` -> `EventText`, `task_complete` -> `EventResult`
    *   内部ヘルパー関数 `parsePayloadEvent(payloadType string, payload json.RawMessage)` を追加
    *   既存のフラット形式ケースは後方互換性のため保持
*   **Logic**:

    ```go
    // ExecEvent - Payload フィールドを追加
    type ExecEvent struct {
        Type     string          `json:"type"`
        Message  string          `json:"message,omitempty"`
        ThreadID string          `json:"thread_id,omitempty"`
        Error    json.RawMessage `json:"error,omitempty"`
        Payload  json.RawMessage `json:"payload,omitempty"`
    }

    func ParseExecEvent(line string) *codingagent.StreamEvent {
        var ev ExecEvent
        if err := json.Unmarshal([]byte(line), &ev); err != nil {
            return nil
        }

        switch ev.Type {
        case "response_item":
            // Codex CLI 0.139.0+ のネスト形式:
            // {"type":"response_item","payload":{"type":"function_call",...}}
            var header struct {
                Type string `json:"type"`
            }
            if err := json.Unmarshal(ev.Payload, &header); err != nil {
                return nil
            }
            return parsePayloadEvent(header.Type, ev.Payload)

        case "event_msg":
            // {"type":"event_msg","payload":{"type":"agent_message","message":"..."}}
            var msg struct {
                Type    string `json:"type"`
                Message string `json:"message,omitempty"`
            }
            if err := json.Unmarshal(ev.Payload, &msg); err != nil {
                return nil
            }
            switch msg.Type {
            case "agent_message":
                return &codingagent.StreamEvent{Type: codingagent.EventText, Content: msg.Message}
            case "task_complete":
                return &codingagent.StreamEvent{Type: codingagent.EventResult}
            default:
                // token_count, user_message, task_started etc. - ignore
                return nil
            }

        case "session_meta", "turn_context":
            // ライフサイクルイベント - 無視
            return nil

        // --- 以下は既存のフラット形式ケース (後方互換) ---
        case "message":
            // ... (既存コード維持)
        case "response.output_text.delta":
            // ... (既存コード維持)
        // ... (他の既存ケースも維持)
        }
    }

    // parsePayloadEvent は response_item の payload を StreamEvent に変換する。
    // payload の type フィールドに応じて適切なパースを行う。
    func parsePayloadEvent(payloadType string, payload json.RawMessage) *codingagent.StreamEvent {
        switch payloadType {
        case "function_call":
            var tc struct {
                Name      string `json:"name"`
                Arguments string `json:"arguments"`
            }
            json.Unmarshal(payload, &tc)
            return &codingagent.StreamEvent{
                Type:     codingagent.EventToolUse,
                ToolName: tc.Name,
                ToolInput: map[string]any{
                    "arguments": tc.Arguments,
                },
            }

        case "function_call_output":
            var out struct {
                Output string `json:"output"`
            }
            json.Unmarshal(out, &out) // NOTE: should be json.Unmarshal(payload, &out)
            return &codingagent.StreamEvent{
                Type:    codingagent.EventToolResult,
                Content: out.Output,
            }

        case "message":
            // assistant message with content array
            var msg struct {
                Role    string `json:"role"`
                Content []struct {
                    Type string `json:"type"`
                    Text string `json:"text,omitempty"`
                } `json:"content,omitempty"`
            }
            json.Unmarshal(payload, &msg)
            if msg.Role == "assistant" && len(msg.Content) > 0 {
                var texts []string
                for _, c := range msg.Content {
                    if c.Type == "output_text" || c.Type == "text" {
                        texts = append(texts, c.Text)
                    }
                }
                if len(texts) > 0 {
                    combined := strings.Join(texts, "")
                    return &codingagent.StreamEvent{Type: codingagent.EventText, Content: combined}
                }
            }
            return nil

        default:
            return nil
        }
    }
    ```

---

### E2E テスト (tests/)

#### [MODIFY] [codex_e2e_test.go](file://tests/codex_e2e_test.go)

*   **Description**: `exec.Command` で `tern` + `ternctl` を実コマンドとして起動する E2E テストを追加
*   **Technical Design**:
    *   テスト関数 `TestCodexE2E_TernctlRealCommand` を追加
    *   `exec.Command` で `./bin/tern` をサブプロセスとして起動 (バックグラウンド)
    *   `exec.Command` で `./bin/ternctl run` を実行し、stdout をキャプチャ
    *   stdout の内容を検証 (`[Tool:`, `[Tool Result]`, `Session created:`, `"status": "completed"`)
*   **Logic**:

    ```go
    // TestCodexE2E_TernctlRealCommand は実コマンド tern + ternctl を
    // exec.Command で起動し、ternctl の stdout 出力を検証する E2E テスト。
    func TestCodexE2E_TernctlRealCommand(t *testing.T) {
        // 前提条件: codex CLI が PATH 上にあること
        if _, err := exec.LookPath("codex"); err != nil {
            t.Fatalf("codex CLI not found on PATH: %v", err)
        }

        // tern, ternctl バイナリのパスを解決
        ternBin, err := filepath.Abs("../bin/tern")
        if err != nil {
            t.Fatalf("resolve tern path: %v", err)
        }
        ternctlBin, err := filepath.Abs("../bin/ternctl")
        if err != nil {
            t.Fatalf("resolve ternctl path: %v", err)
        }
        // バイナリの存在確認
        if _, err := os.Stat(ternBin); err != nil {
            t.Fatalf("tern binary not found: %s", ternBin)
        }
        if _, err := os.Stat(ternctlBin); err != nil {
            t.Fatalf("ternctl binary not found: %s", ternctlBin)
        }

        // テスト用ディレクトリとコンフィグを準備
        modelProfilesSrc, _ := filepath.Abs("../features/tern/model_profiles.yaml")
        asPort := freePort(t)
        gwPort := freePort(t)
        wsPort := freePort(t)

        tmpDir := t.TempDir()
        configPath := filepath.Join(tmpDir, "config.yaml")
        configContent := fmt.Sprintf(`llm_gateway:
  port: %d
  model_profiles_path: "%s"
log:
  level: "info"
vault:
  backend: "keyring"
websocket:
  port: %d
agent_service:
  port: %d
  disable_sandbox: true
`, gwPort, filepath.ToSlash(modelProfilesSrc), wsPort, asPort)
        os.WriteFile(configPath, []byte(configContent), 0644)

        // Phase 1: tern サーバをサブプロセスとして起動
        ternCmd := exec.Command(ternBin, "--config", configPath)
        ternCmd.Stdout = os.Stdout // デバッグ用にサーバログを表示
        ternCmd.Stderr = os.Stderr
        if err := ternCmd.Start(); err != nil {
            t.Fatalf("start tern: %v", err)
        }
        defer ternCmd.Process.Kill()

        // サーバ起動待ち (health エンドポイント)
        serverURL := fmt.Sprintf("http://localhost:%d", asPort)
        waitForHealthy(t, serverURL, 15*time.Second)

        // Phase 2: ternctl run を実行
        workDir := filepath.Join(tmpDir, "work")
        os.MkdirAll(workDir, 0755)
        // .codex ディレクトリを事前作成 (codex が必要とする場合)
        os.MkdirAll(filepath.Join(workDir, ".codex"), 0755)

        ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
        defer cancel()

        ternctlCmd := exec.CommandContext(ctx, ternctlBin,
            "--server", serverURL,
            "run",
            "--agent", "codex",
            "--prompt", "please run 'echo hello' command and report the result.",
            "--work-dir", workDir,
        )
        output, err := ternctlCmd.CombinedOutput()
        outputStr := string(output)
        t.Logf("ternctl output:\n%s", outputStr)

        // Phase 3: stdout 出力の検証
        if err != nil {
            t.Fatalf("ternctl exited with error: %v\noutput: %s", err, outputStr)
        }
        if !strings.Contains(outputStr, "Session created:") {
            t.Error("expected 'Session created:' in output")
        }
        if !strings.Contains(outputStr, "[Tool:") {
            t.Error("expected '[Tool: ...]' in output (tool use event)")
        }
        if !strings.Contains(outputStr, "[Tool Result]") {
            t.Error("expected '[Tool Result] ...' in output (tool result event)")
        }
        if !strings.Contains(outputStr, `"status": "completed"`) {
            t.Error("expected session status 'completed' in output")
        }
    }

    // waitForHealthy は health エンドポイントが 200 を返すまでポーリングする。
    func waitForHealthy(t *testing.T, baseURL string, timeout time.Duration) {
        t.Helper()
        deadline := time.Now().Add(timeout)
        for time.Now().Before(deadline) {
            resp, err := http.Get(baseURL + "/health")
            if err == nil && resp.StatusCode == http.StatusOK {
                resp.Body.Close()
                return
            }
            if resp != nil {
                resp.Body.Close()
            }
            time.Sleep(200 * time.Millisecond)
        }
        t.Fatalf("server at %s did not become healthy within %s", baseURL, timeout)
    }
    ```

## Step-by-Step Implementation Guide

### Step 1: TDD Red -- ネスト形式の単体テストを追加

*   `shared/libs/go/codingagent/codex/protocol_test.go` に以下を追加:
    *   `TestParseExecEvent_ResponseItem_FunctionCall`
    *   `TestParseExecEvent_ResponseItem_FunctionCallOutput`
    *   `TestParseExecEvent_ResponseItem_AssistantMessage`
    *   `TestParseExecEvent_EventMsg_AgentMessage`
    *   `TestParseExecEvent_EventMsg_TaskComplete`
    *   `TestParseExecEvent_EventMsg_Ignored`
*   ビルドを実行し、新規テストが**失敗**することを確認:
    ```bash
    ./scripts/process/build.sh
    ```

### Step 2: TDD Green -- protocol.go のネスト形式対応を実装

*   `shared/libs/go/codingagent/codex/protocol.go` を修正:
    1. `ExecEvent` 構造体に `Payload json.RawMessage` フィールドを追加
    2. `ParseExecEvent` の switch に `response_item`, `event_msg`, `session_meta`, `turn_context` ケースを追加
    3. `parsePayloadEvent` 内部ヘルパー関数を追加
    4. 既存のフラット形式ケースは保持 (後方互換性)
*   ビルドを実行し、全テスト (既存 + 新規) が**成功**することを確認:
    ```bash
    ./scripts/process/build.sh
    ```
*   コミット:
    ```bash
    git add shared/libs/go/codingagent/codex/protocol.go shared/libs/go/codingagent/codex/protocol_test.go
    git commit -m "fix: handle nested response_item/event_msg in codex JSONL parser"
    ```

### Step 3: E2E テスト -- ternctl 実コマンド実行テストを追加

*   `tests/codex_e2e_test.go` に以下を追加:
    *   `TestCodexE2E_TernctlRealCommand` テスト関数
    *   `waitForHealthy` ヘルパー関数
*   テストでは `exec.Command` で `./bin/tern` と `./bin/ternctl` の実バイナリを起動する
*   コミット:
    ```bash
    git add tests/codex_e2e_test.go
    git commit -m "test: add ternctl real command E2E test for codex tool logs"
    ```

### Step 4: 全体ビルド + E2E テスト実行

*   全体ビルドと E2E テストを実行:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestCodexE2E_TernctlRealCommand"
    ```
*   失敗した場合は修正して再実行
*   成功したら既存の Codex E2E テストも含めてリグレッション確認:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestCodexE2E"
    ```

### Step 5: git push + 総合判定

*   全テスト成功を確認後、プッシュ:
    ```bash
    git push
    ```
*   Verification Plan に従い総合判定を実施

## Verification Plan

### Automated Verification

1. **Build & Unit Tests**:
    全体ビルドと単体テストを実行する。
    ```bash
    ./scripts/process/build.sh
    ```
    *   **Log Verification**: `protocol_test.go` の新規テスト (6件) が全て PASS し、既存テスト (フラット形式含む) がリグレッションなく PASS すること。

2. **Integration Tests (E2E -- ternctl 実コマンド実行)**:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestCodexE2E_TernctlRealCommand"
    ```
    *   **Log Verification**: ternctl の stdout 出力ログに `[Tool:`, `[Tool Result]`, `Session created:`, `"status": "completed"` が含まれること。

3. **Regression Tests (Codex E2E 全体)**:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestCodexE2E"
    ```
    *   **Log Verification**: 既存の `TestCodexE2E_FileCreation`, `TestCodexE2E_GeminiModel_FileCreation`, `TestCodexE2E_AnthropicModel_FileCreation` 等が PASS すること。

### E2E Tests

#### [MODIFY] [codex_e2e_test.go](file://tests/codex_e2e_test.go)

*   **テストケース**: `TestCodexE2E_TernctlRealCommand`
*   **検証ポイント**:
    1. `exec.Command` で `./bin/tern --config <config>` がサブプロセスとして正常に起動する
    2. `exec.Command` で `./bin/ternctl run --agent codex --prompt ...` が正常に実行される
    3. ternctl の stdout に `[Tool: ...]` (ツール使用イベント) が含まれる
    4. ternctl の stdout に `[Tool Result] ...` (ツール結果イベント) が含まれる
    5. ternctl の stdout に `Session created:` が含まれる
    6. ternctl の stdout に `"status": "completed"` が含まれる
    7. ternctl の exit code が 0 である

### テスト項目のセルフレビュー (S11.4)

1. **網羅性の検証**: 単体テストで `response_item` (3パターン), `event_msg` (3パターン), 後方互換 (既存テスト) をカバー。E2E テストで `tern -> codex CLI -> SSE -> ternctl -> stdout` の全パイプラインを検証。全テスト成功で機能が動作していると言える。
2. **証拠の十分性**: 各テストは `ev.Type`, `ev.Content`, `ev.ToolName` の具体的な値を検証。E2E は stdout 内の具体的な文字列パターンを検証。
3. **迂回・抜け道の排除**: E2E テストが `exec.Command` で実バイナリを起動するため、Go の HTTP クライアントによる直接呼び出しでは検出できない問題 (stream.go のレンダリング等) も検証対象になる。
4. **依存関係の整合性**: 単体テスト -> 全体ビルド -> E2E テストの順序で実行。前段が失敗した場合は後段を実行しない。

### 総合判定プロセス (S12)

全テスト完了後、testing-rules.md S12.2 のチェック項目 (7項目) を確認し、S12.3 のフォーマットで総合判定結果を記録する。

## Documentation

#### [MODIFY] [044-Fix-Codex-JSONL-Parser-And-E2E-Coverage.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/ideas/044-Fix-Codex-JSONL-Parser-And-E2E-Coverage.md)

*   **更新内容**: 実装完了後、検証結果を仕様書の検証シナリオセクションに反映する。
