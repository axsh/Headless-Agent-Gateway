# 061-SSE-Stream-Reliability

> **Source Specification**: prompts/phases/000-foundation/branches/feat-llm-backend/ideas/050-SSE-Stream-Reliability.md

## Goal Description

SSE ストリーミングの信頼性を向上させる。ternctl と AgentService 間の SSE 接続が途中で切断された場合に、クライアント側で不完全終了を検出し、サーバー側で適切なログを出力し、エージェント側でパニックを防止する。6つの要件 (R1-R6) を実装する。

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: SSE ストリーム不完全終了の検出 | Proposed Changes > client パッケージ > stream.go |
| R2: SSE 対応の HTTP タイムアウト設定 | Proposed Changes > client パッケージ > client.go |
| R3: SSE 切断ログレベルの引き上げ | Proposed Changes > agentservice パッケージ > handler.go |
| R4: EventEmitter のパニック防止 | Proposed Changes > wayfinder パッケージ > emitter.go |
| R5: ternctl の SSE タイムアウト無効化 | Proposed Changes > ternctl > main.go |
| R6: ternctl のセッション最終ステータス警告 | Proposed Changes > ternctl > main.go |

## Proposed Changes

### wayfinder パッケージ (R4: パニック防止)

#### [MODIFY] [emitter_test.go](file://shared/libs/go/wayfinder/emitter_test.go)
*   **Description**: 閉じたチャネルへの Emit でパニックしないことを検証するテストを追加。
*   **Technical Design**:
    ```go
    func TestEventEmitter_ClosedChannel(t *testing.T)
    ```
*   **Logic**:
    - `ch := make(chan codingagent.StreamEvent, 1)` でチャネルを作成
    - `close(ch)` でチャネルを閉じる
    - `emitter := NewEventEmitter(ch)` でエミッターを作成
    - `emitter.Emit(codingagent.StreamEvent{Type: codingagent.EventText, Content: "test"})` を呼び出す
    - パニックが発生しなければテスト成功 (テスト関数が正常にリターンすること自体が検証)

---

#### [MODIFY] [emitter.go](file://shared/libs/go/wayfinder/emitter.go)
*   **Description**: `Emit()` に `recover` ガードを追加し、閉じたチャネルへの送信時のパニックを防止する。
*   **Technical Design**:
    現在の実装:
    ```go
    func (e *EventEmitter) Emit(ev codingagent.StreamEvent) {
        if e == nil || e.ch == nil {
            return
        }
        e.ch <- ev
    }
    ```
    修正後:
    ```go
    // Emit sends a single event. Safe to call on nil receiver or closed channel.
    func (e *EventEmitter) Emit(ev codingagent.StreamEvent) {
        if e == nil || e.ch == nil {
            return
        }
        defer func() {
            recover() // Silently ignore send-on-closed-channel panic.
        }()
        e.ch <- ev
    }
    ```
*   **Logic**:
    - `defer func() { recover() }()` を `e.ch <- ev` の前に配置
    - 閉じたチャネルへの送信で発生する `panic: send on closed channel` を recover で捕捉
    - ログ出力は行わない (チャネルクローズは SSE 切断時の正常フローであるため)

---

### client パッケージ (R1: 不完全ストリーム検出, R2: タイムアウト設定)

#### [MODIFY] [stream_test.go](file://shared/libs/go/client/stream_test.go)
*   **Description**: 不完全ストリーム検出とスキャナーエラーのテストを追加。
*   **Technical Design**:
    ```go
    func TestStream_Output_IncompleteStream(t *testing.T)
    func TestStream_Output_ScannerError(t *testing.T)
    ```
*   **Logic**:

    **Test 1: `TestStream_Output_IncompleteStream`**
    ```go
    func TestStream_Output_IncompleteStream(t *testing.T) {
        // SSE stream that ends without [DONE] marker (simulates connection drop).
        sseData := "data: {\"type\":\"text\",\"content\":\"partial\"}\n\n"
        body := io.NopCloser(strings.NewReader(sseData))
        stream := newStream(body)

        var buf strings.Builder
        err := stream.Output(&buf)
        if err == nil {
            t.Fatal("expected error for incomplete stream, got nil")
        }
        if !strings.Contains(err.Error(), "stream terminated unexpectedly") {
            t.Errorf("error = %v, want containing 'stream terminated unexpectedly'", err)
        }
        // Partial text should still be written before the error.
        if got := buf.String(); got != "partial" {
            t.Errorf("output = %q, want %q", got, "partial")
        }
    }
    ```

    **Test 2: `TestStream_Output_ScannerError`**
    ```go
    func TestStream_Output_ScannerError(t *testing.T) {
        // Simulate a read error from the response body.
        errReader := &errorReader{err: fmt.Errorf("connection reset")}
        body := io.NopCloser(errReader)
        stream := newStream(body)

        var buf strings.Builder
        err := stream.Output(&buf)
        if err == nil {
            t.Fatal("expected error for scanner failure, got nil")
        }
        if !strings.Contains(err.Error(), "stream read error") {
            t.Errorf("error = %v, want containing 'stream read error'", err)
        }
    }

    // errorReader is a test helper that always returns an error on Read.
    type errorReader struct {
        err error
    }

    func (r *errorReader) Read(p []byte) (int, error) {
        return 0, r.err
    }
    ```

---

#### [MODIFY] [stream.go](file://shared/libs/go/client/stream.go)
*   **Description**: `events()` メソッドに `[DONE]` マーカー追跡と `Scanner.Err()` チェックを追加。
*   **Technical Design**:
    現在の `events()` (L148-182):
    ```go
    func (s *Stream) events() <-chan Event {
        ch := make(chan Event, 8)
        go func() {
            defer close(ch)
            scanner := bufio.NewScanner(s.body)
            for scanner.Scan() {
                line := scanner.Text()
                if !strings.HasPrefix(line, "data: ") {
                    continue
                }
                data := strings.TrimPrefix(line, "data: ")
                if data == "[DONE]" {
                    return
                }
                // ... event parsing ...
                ch <- ev
            }
        }()
        return ch
    }
    ```
    修正後:
    ```go
    func (s *Stream) events() <-chan Event {
        ch := make(chan Event, 8)
        go func() {
            defer close(ch)
            scanner := bufio.NewScanner(s.body)
            receivedDone := false
            for scanner.Scan() {
                line := scanner.Text()
                if !strings.HasPrefix(line, "data: ") {
                    continue
                }
                data := strings.TrimPrefix(line, "data: ")
                if data == "[DONE]" {
                    receivedDone = true
                    return
                }
                var raw struct {
                    Type     string `json:"type"`
                    Content  string `json:"content"`
                    ToolName string `json:"tool_name,omitempty"`
                }
                if err := json.Unmarshal([]byte(data), &raw); err != nil {
                    continue
                }
                ev := Event{
                    Type:     EventType(raw.Type),
                    Text:     raw.Content,
                    ToolName: raw.ToolName,
                }
                if ev.Type == EventError {
                    ev.Error = raw.Content
                }
                ch <- ev
            }
            // Detect abnormal stream termination.
            if err := scanner.Err(); err != nil {
                ch <- Event{
                    Type:  EventError,
                    Error: fmt.Sprintf("stream read error: %v", err),
                }
            } else if !receivedDone {
                ch <- Event{
                    Type:  EventError,
                    Error: "stream terminated unexpectedly without completion marker",
                }
            }
        }()
        return ch
    }
    ```
*   **Logic**:
    - `receivedDone` フラグで `[DONE]` マーカーの受信を追跡
    - `scanner.Scan()` ループ終了後、`scanner.Err()` を先にチェック (読み取りエラー)
    - `scanner.Err()` が nil かつ `receivedDone` が false の場合、不完全ストリームとして `EventError` を送信
    - import に `"fmt"` を追加 (既存の import を確認して不足なら追加)

---

#### [MODIFY] [client_test.go](file://shared/libs/go/client/client_test.go)
*   **Description**: `WithNoTimeout` オプションのテストを追加。
*   **Technical Design**:
    ```go
    func TestWithNoTimeout(t *testing.T)
    ```
*   **Logic**:
    ```go
    func TestWithNoTimeout(t *testing.T) {
        c := New("http://example.com", WithNoTimeout())
        if c.httpClient.Timeout != 0 {
            t.Errorf("Timeout = %v, want 0 (no timeout)", c.httpClient.Timeout)
        }
    }
    ```

---

#### [MODIFY] [client.go](file://shared/libs/go/client/client.go)
*   **Description**: `WithNoTimeout` オプションを追加。
*   **Technical Design**:
    現在の末尾 (L31-34):
    ```go
    func WithHTTPClient(hc *http.Client) ClientOption {
        return func(c *Client) { c.httpClient = hc }
    }
    ```
    追加:
    ```go
    // WithNoTimeout disables the HTTP client timeout.
    // This is required for SSE streaming connections that may run for
    // extended periods. Without this, the default 30s timeout will
    // terminate long-running SSE streams.
    func WithNoTimeout() ClientOption {
        return func(c *Client) { c.httpClient.Timeout = 0 }
    }
    ```

---

### agentservice パッケージ (R3: ログレベル引き上げ)

#### [MODIFY] [handler.go](file://shared/libs/go/agentservice/handler.go)
*   **Description**: `streamSSE` のクライアント切断ログレベルを `Debug` から `Warn` に変更。
*   **Technical Design**:
    現在のコード (L286-290):
    ```go
    case <-ctx.Done():
        if s.logger != nil {
            s.logger.Debug("client disconnected, stopping SSE stream", "session_id", sessionID)
        }
        return
    ```
    修正後:
    ```go
    case <-ctx.Done():
        if s.logger != nil {
            s.logger.Warn("client disconnected during SSE stream",
                "session_id", sessionID,
                "events_sent", eventCount)
        }
        return
    ```
*   **Logic**:
    - `Debug` -> `Warn` に変更 (logging-rules.md 1.2: 「継続可能な例外の発生」に該当)
    - メッセージを `"client disconnected, stopping SSE stream"` から `"client disconnected during SSE stream"` に変更
    - `events_sent` フィールドを追加 (既存の `eventCount` 変数を利用)

---

### ternctl (R5: タイムアウト無効化, R6: ステータス警告)

#### [MODIFY] [main.go](file://features/ternctl/main.go)
*   **Description**: SSE タイムアウト無効化とセッション最終ステータス警告の追加。
*   **Technical Design**:

    **R5: タイムアウト無効化**
    現在のコード (L33):
    ```go
    c := client.New(serverURL)
    ```
    修正後:
    ```go
    c := client.New(serverURL, client.WithNoTimeout())
    ```

    **R6: ステータス警告**
    現在のコード (L188-197):
    ```go
    // Show final session status.
    details, err := c.GetSession(ctx, session.ID)
    if err == nil {
        out, _ := json.MarshalIndent(details, "", "  ")
        fmt.Println(string(out))
    }

    if streamErr != nil {
        os.Exit(1)
    }
    ```
    修正後:
    ```go
    // Show final session status.
    details, err := c.GetSession(ctx, session.ID)
    if err == nil {
        out, _ := json.MarshalIndent(details, "", "  ")
        fmt.Println(string(out))

        // Warn if session did not complete successfully.
        if status, ok := details["status"].(string); ok && status != "completed" {
            fmt.Fprintf(os.Stderr, "\nWarning: session ended with status %q (expected \"completed\")\n", status)
            if errMsg, ok := details["error"].(string); ok && errMsg != "" {
                fmt.Fprintf(os.Stderr, "Error details: %s\n", errMsg)
            }
        }
    }

    if streamErr != nil {
        os.Exit(1)
    }
    ```
*   **Logic**:
    - `client.WithNoTimeout()` により SSE の HTTP タイムアウトを無効化 (デフォルト 30s では長時間の SSE ストリーミングが途切れる)
    - セッション最終ステータスが `"completed"` 以外の場合、stderr に警告メッセージを出力
    - `"error"` フィールドが存在する場合、その内容も出力

---

## Step-by-Step Implementation Guide

> [!IMPORTANT]
> TDD: テストを先に書き、失敗を確認してから実装する。

### Step 1: R4 テスト追加 (emitter_test.go) - TDD Red

- [x] Edit `shared/libs/go/wayfinder/emitter_test.go`:
  - `TestEventEmitter_ClosedChannel` テストを追加
- [x] テストがパニックで失敗することを確認

### Step 2: R4 実装 (emitter.go) - TDD Green

- [x] Edit `shared/libs/go/wayfinder/emitter.go`:
  - `Emit()` に `defer func() { recover() }()` を追加
  - コメントを更新: `Safe to call on nil receiver or closed channel.`
- [x] テスト成功を確認
- [x] git commit

### Step 3: R1 テスト追加 (stream_test.go) - TDD Red

- [x] Edit `shared/libs/go/client/stream_test.go`:
  - `TestStream_Output_IncompleteStream` テストを追加
  - `TestStream_Output_ScannerError` テストを追加
  - `errorReader` ヘルパー構造体を追加
- [x] テストが失敗することを確認 (不完全ストリームで error が nil になる)

### Step 4: R1 実装 (stream.go) - TDD Green

- [x] Edit `shared/libs/go/client/stream.go`:
  - `events()` メソッドに `receivedDone` フラグを追加
  - `scanner.Scan()` ループ後に `scanner.Err()` チェックを追加
  - `receivedDone` が false の場合に `EventError` を送信
  - import に `"fmt"` を追加
- [x] テスト成功を確認
- [x] 既存テスト (`TestStream_Output` 等) がリグレッションなく成功することを確認
- [x] git commit

### Step 5: R2 テスト追加 (client_test.go) - TDD Red

- [x] Edit `shared/libs/go/client/client_test.go`:
  - `TestWithNoTimeout` テストを追加
- [x] テストが失敗することを確認 (`WithNoTimeout` が未定義)

### Step 6: R2 実装 (client.go) - TDD Green

- [x] Edit `shared/libs/go/client/client.go`:
  - `WithNoTimeout()` 関数を追加
- [x] テスト成功を確認
- [x] git commit

### Step 7: R3 実装 (handler.go)

- [x] Edit `shared/libs/go/agentservice/handler.go`:
  - L287 の `s.logger.Debug` を `s.logger.Warn` に変更
  - メッセージを `"client disconnected during SSE stream"` に変更
  - `"events_sent"`, `eventCount` フィールドを追加
- [x] git commit

### Step 8: R5, R6 実装 (ternctl main.go)

- [x] Edit `features/ternctl/main.go`:
  - L33 の `client.New(serverURL)` を `client.New(serverURL, client.WithNoTimeout())` に変更
  - L188-193 のセッション最終ステータス表示後に警告ロジックを追加
- [x] git commit

### Step 9: Verification Plan 実行

- [x] `./scripts/process/build.sh` を実行
- [x] `./scripts/process/integration_test.sh` を実行
- [x] 総合判定を実施

---

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```
    - `TestEventEmitter_ClosedChannel` が PASS
    - `TestStream_Output_IncompleteStream` が PASS
    - `TestStream_Output_ScannerError` が PASS
    - `TestWithNoTimeout` が PASS
    - 既存テスト全件が PASS (リグレッションなし)

2.  **Integration Tests**:
    ```bash
    ./scripts/process/integration_test.sh --categories common
    ```
    - `common` カテゴリの全テストが PASS
    - **Log Verification**: サーバーログに Warn レベルの新しいログメッセージが出力可能であることを確認 (テスト中にクライアント切断が発生した場合)

3.  **E2E Tests**:
    E2E テストの追加は不要。理由:
    - R1/R2: client パッケージの純粋な内部ロジック修正。`stream_test.go` の単体テストで完全にカバー
    - R3: ログレベルの変更のみで外部動作に変更なし
    - R4: emitter の内部ガード追加。`emitter_test.go` の単体テストでカバー
    - R5: クライアント設定の変更。`client_test.go` でカバー
    - R6: CLI 出力のみの変更。外部 API に変更なし
    - 既存の E2E テスト (`TestAgentServiceSessionLifecycle` 等) が正常にパスすることでリグレッションがないことを確認できる

### セルフレビュー結果

1.  **網羅性**: R1-R6 全ての要件に対してテスト or 実装が設計されている。テストが全て成功すれば、SSE 切断時の不完全ストリーム検出、パニック防止、ログ出力、タイムアウト無効化が正しく動作すると言える
2.  **証拠の十分性**: `TestStream_Output_IncompleteStream` はエラーメッセージの内容を検証し、`TestEventEmitter_ClosedChannel` はパニック非発生を検証している。「エラーが出ない」レベルではなく値の検証を行っている
3.  **迂回排除**: 不完全ストリームのテストは `[DONE]` なしの EOF を直接シミュレートし、正しいエラーパスが実行されることを検証。正常系テスト (`TestStream_Output`) もリグレッション確認で維持
4.  **依存関係**: emitter (R4) -> stream (R1) -> client (R2) -> handler (R3) -> ternctl (R5/R6) の順でボトムアップに設計

### 総合判定プロセス

全テスト完了後、testing-rules.md 12.2 のチェック項目を確認:
- スキップされたテストの有無
- 部分的なエラーの見落とし
- 迂回処理による偽成功
- アダプタ・コンフィグの誤適用
- テスト間の依存・順序問題
- カバレッジの妥当性
- 外部システムの状態

結果は walkthrough に記録する。

## Documentation

本修正は内部ロジックの修正であり、既存の仕様書やドキュメントへの更新は不要。
