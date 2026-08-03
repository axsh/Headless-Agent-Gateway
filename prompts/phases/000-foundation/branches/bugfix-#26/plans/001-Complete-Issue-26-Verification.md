# 001-Complete-Issue-26-Verification

> **Source Specification**: `prompts/phases/000-foundation/branches/bugfix-#26/ideas/001-Complete-Issue-26-Verification.md`

## Goal Description

Issue #26 ([axsh/arctic-tern#26](https://github.com/axsh/arctic-tern/issues/26)) に対し、000 仕様（R0–R5）で landing 済みの **修正コード** に対する **検証ギャップ** を閉じる。

本計画は **テスト追加 + ドキュメント追加** が主目的であり、000 の production コード変更は **原則行わない**。R10（境界・エスケープ）で RED が出た場合のみ `sse_chunk.go` の定数調整または split ロジック修正を許容する。

001 完了（R16 ゲート全 PASS）時点で、Issue #26 は **完全解決** と断言可能とする。

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R6: 256 KiB `client/v1` フルスタック E2E | Phase 2 → `tests/codex_client_v1_large_output_e2e_test.go` |
| R7: 256 KiB legacy `client` フルスタック E2E | Phase 2 → `tests/codex_legacy_client_large_output_e2e_test.go` |
| R8a: handler 切断 + 早期 status 単体 | Phase 1 → `shared/libs/go/agentservice/handler_test.go` |
| R8b: 切断 E2E | Phase 2 → `tests/codex_session_status_e2e_test.go` |
| R9: FM2 回帰（data サイレンス時間 bound） | Phase 2 → `tests/codex_client_v1_large_output_e2e_test.go` |
| R10a: エスケープ heavy content 単体 | Phase 1 → `shared/libs/go/codingagent/sse_chunk_test.go` |
| R10b: 境界 just-under-limit 単体 | Phase 1 → `sse_chunk_test.go` |
| R10c: 全サイズ wire 上限プロパティ | Phase 1 → `sse_chunk_test.go` |
| R11a: ripgrep 型 fake E2E | Phase 2 → `codex_client_v1_large_output_e2e_test.go` |
| R11b: 実 Codex E2E（skip 可） | Phase 3 → `tests/codex_real_large_output_e2e_test.go` |
| R12: 複数大 tool_result E2E | Phase 2 → `codex_client_v1_large_output_e2e_test.go` |
| R13: `respondJSONRelay` 早期 status | Phase 1 → `handler_test.go` |
| R14: 参照 SSE 消費者契約テスト | Phase 2 → `tests/sse_consumer_reference_test.go` |
| R15: 完了判定ドキュメント | Phase 4 → `docs/issue-26-verification.md` |
| R16: 完了ゲート | Phase 4 → Verification Plan |
| R17–R19（任意） | Out of Scope（本計画の完了条件外） |

---

## Proposed Changes

000 実装済みコード（`sse_chunk.go`, `handler.go`, `client/v1/stream.go`, `client/stream.go`）は **変更しない** ことを第一目標とする。以下は **テスト・ヘルパ・ドキュメント** の追加のみ。

### Phase 0 — fake codex ヘルパ拡張

#### [MODIFY] [tests/testutil/fake_codex.go](file://tests/testutil/fake_codex.go)

* **Description**: R6–R12, R9, R11a 向け JSONL 生成ヘルパ。既存 `BuildThreeLineReproLines`（65537 B 固定）は **変更しない**。
* **Technical Design**:

```go
// BuildLargeAggregatedOutputLines returns Issue #24-style JSONL with aggregated_output of exact byte size.
// content is embedded via json.Marshal to preserve valid JSON escaping.
func BuildLargeAggregatedOutputLines(contentBytes int) []string

// BuildRipgrepLikeOutput returns multi-line search-style output of at least minBytes.
func BuildRipgrepLikeOutput(minBytes int) string

// BuildMultiToolOutputLines returns JSONL with N consecutive command_execution outputs then turn completion.
func BuildMultiToolOutputLines(sizes ...int) []string

// BuildDelayedLargeOutputLines wraps BuildLargeAggregatedOutputLines with DelayMS on fake binary (via FakeCodexOptions).
// Used by R9 — caller sets FakeCodexOptions{Lines: ..., DelayMS: 2000}.
func BuildDelayedLargeOutputLines(contentBytes int) []string
```

* **Logic**:

1. **`BuildLargeAggregatedOutputLines(contentBytes int)`**:
   - `content := strings.Repeat("x", contentBytes)`（256 KiB 時は `codingagent.DefaultMaxToolResultBytes` を caller が渡す）
   - `payload, _ := json.Marshal(map[string]any{"type": "item.completed", "item": map[string]any{"type": "command_execution", "aggregated_output": content}})`
   - return `[]string{`{"type":"item.started"}`, string(payload), `{"type":"item.completed"}`}`
   - **65537 B 固定の `BuildThreeLineReproLines` は維持**（000 回帰）

2. **`BuildRipgrepLikeOutput(minBytes int)`**:
   - `var b strings.Builder`
   - loop until `b.Len() >= minBytes`:
     - `line := fmt.Sprintf("./src/module_%d/foo.go:%d:match keyword\n", i/100, i%500)`
     - 100 行ごとに UTF-8 マルチバイト行を 1 行挿入: `"./docs/日本語/ファイル.go:1:マッチ\n"`
   - return `b.String()`

3. **`BuildMultiToolOutputLines(sizes ...int)`**:
   - lines := `[]string{`{"type":"item.started"}`}`
   - for each size in sizes:
     - `content := strings.Repeat("y", size)`（size ごとに異なる rune でも可）
     - JSONL 1 行を `json.Marshal` で生成して append
   - append `{"type":"item.completed"}`
   - return lines

4. **`BuildDelayedLargeOutputLines`**: `BuildLargeAggregatedOutputLines` と同一 lines。Delay は `FakeCodexOptions.DelayMS` で指定（R9 テスト側で `DelayMS: 2000`）。

---

### Phase 1 — 単体テスト（build.sh）

#### [MODIFY] [shared/libs/go/codingagent/sse_chunk_test.go](file://shared/libs/go/codingagent/sse_chunk_test.go)

* **Description**: R10a–c。既存 `TestSplitStreamEventForSSE_*` に追加。
* **Technical Design**:

```go
func TestSplitStreamEventForSSE_EscapeHeavyContentUnder64KB(t *testing.T)
func TestSplitStreamEventForSSE_BoundaryJustUnderLimit(t *testing.T)
func TestSplitStreamEventForSSE_AllSizesWireUnderLimit(t *testing.T)

// Helper: assertAllWireEventsUnderLimit(t, events []codingagent.StreamEvent, limit int)
func assertAllWireEventsUnderLimit(t *testing.T, events []codingagent.StreamEvent, limit int)
```

* **Logic**:

**R10a — `TestSplitStreamEventForSSE_EscapeHeavyContentUnder64KB`**:
- unit := `"\"\\n\\u0041"`（引用符 + バックスラッシュ + 改行 + Unicode）
- `content := strings.Repeat(unit, 50000)`（200 KiB 超）
- `ev := StreamEvent{Type: EventToolResult, Content: content}`
- `events, err := SplitStreamEventForSSE(ev, 0)`
- `assertAllWireEventsUnderLimit(t, events, DefaultMaxSSEDataLineBytes)`
- RED 時: `sse_chunk.go` の `DefaultSSEChunkContentBytes` を下げる（例: 32 KiB）または split ループ内で marshaled size を実測して chunk を縮小

**R10b — `TestSplitStreamEventForSSE_BoundaryJustUnderLimit`**:
- 二分探索ヘルパ `findContentLenJustUnderLimit(t, limit int) int`:
  - low=0, high=DefaultMaxToolResultBytes
  - `content := strings.Repeat("a", mid)` → `ev := StreamEvent{Type: EventToolResult, Content: content}`
  - `wire, _ := json.Marshal(ev)` — **分割前の** marshaled size で判定
  - largest mid where `len(wire) < limit` を返す
- `contentLen := findContentLenJustUnderLimit(t, DefaultMaxSSEDataLineBytes)`
- `events, _ := SplitStreamEventForSSE(ev, 0)`
- `len(events) == 1` かつ `events[0].Type == EventToolResult`
- `assertAllWireEventsUnderLimit(...)`

**R10c — `TestSplitStreamEventForSSE_AllSizesWireUnderLimit`**:
- `for contentLen := 0; contentLen <= DefaultMaxToolResultBytes; contentLen += 1024`:
  - `content := strings.Repeat("z", contentLen)`（contentLen==0 は空文字）
  - `events, err := SplitStreamEventForSSE(StreamEvent{Type: EventToolResult, Content: content}, 0)`
  - `assertAllWireEventsUnderLimit(t, events, DefaultMaxSSEDataLineBytes)`
- 262 イテレーション（0, 1024, ..., 262144）

**`assertAllWireEventsUnderLimit`**:
```go
for _, e := range events {
    data, err := json.Marshal(e)
    if err != nil { t.Fatalf(...) }
    if len(data) >= limit {
        t.Fatalf("SSE wire line len %d >= max %d (type=%s)", len(data), limit, e.Type)
    }
}
```

---

#### [MODIFY] [shared/libs/go/agentservice/handler_test.go](file://shared/libs/go/agentservice/handler_test.go)

* **Description**: R8a, R13。mock agent 追加 + handler 経由 SSE/JSON 検証。
* **Technical Design**:

```go
// mockTerminalEventAgent — EventResult のみ即座に返す（R13 用）
type mockTerminalEventAgent struct { name string }
type mockTerminalEventSession struct{}

// mockSlowLargeToolAgent — 256KB tool_result + EventResult（R8a Disconnect 用）
type mockSlowLargeToolAgent struct { name string }
type mockSlowLargeToolSession struct {
    blockUntilRead bool // optional: not required for v1 test design below
}

func TestStreamSSERelay_EarlyStatusUpdate(t *testing.T)
func TestStreamSSERelay_DisconnectUpdatesStatus(t *testing.T)
func TestRespondJSONRelay_EarlyStatusOnTerminalEvent(t *testing.T)
```

* **Logic**:

**Mock — `mockTerminalEventSession.Send`**:
```go
ch := make(chan codingagent.StreamEvent, 2)
ch <- codingagent.StreamEvent{Type: codingagent.EventText, Content: "ok"}
ch <- codingagent.StreamEvent{Type: codingagent.EventResult}
close(ch)
return ch, nil
```

**Mock — `mockSlowLargeToolSession.Send`**:
```go
ch := make(chan codingagent.StreamEvent, 8)
ch <- codingagent.StreamEvent{
    Type:    codingagent.EventToolResult,
    Content: strings.Repeat("z", codingagent.DefaultMaxToolResultBytes),
}
ch <- codingagent.StreamEvent{Type: codingagent.EventResult}
close(ch)
return ch, nil
```

**R8a — `TestStreamSSERelay_EarlyStatusUpdate`**:
1. `srv := agentservice.New(); srv.RegisterAgent(&mockSlowLargeToolAgent{name: "codex"})`
2. POST `/api/v1/sessions` → `sessionID`
3. POST `/api/v1/sessions/{id}/messages` with `Accept: text/event-stream`
4. `msgRec := httptest.NewRecorder()` — **標準 `httptest.ResponseRecorder` は ctx cancel 不可** のため、以下の **cancelable request** パターンを使用:

```go
reqCtx, cancel := context.WithCancel(context.Background())
req := httptest.NewRequest(http.MethodPost, path, body).WithContext(reqCtx)
req.Header.Set("Accept", "text/event-stream")
rec := httptest.NewRecorder()
go func() {
    srv.HTTPHandler().ServeHTTP(rec, req)
}()
// rec.Body から default bufio.Scanner で読み、"result" を含む data 行を検出した直後:
session := getSession(t, srv, sessionID)
if session["status"] != "completed" { t.Fatal(...) }
// まだ [DONE] 前でも status==completed であること
cancel() // クリーンアップ
```

5. PASS: `EventResult` の data 行を読んだ時点（`[DONE]` 前）で `GET /api/v1/sessions/{id}` → `status == "completed"`

**R8a — `TestStreamSSERelay_DisconnectUpdatesStatus`**:
1. 同一 mock（256 KiB + EventResult）
2. cancelable ctx で SSE 消費開始
3. `tool_result_part` を **1 件以上** 読んだ後、`EventResult` 行を読む **前** に `cancel()`
4. `ServeHTTP` goroutine 終了を `time.Sleep(100ms)` または channel で待つ
5. `GET session` → `status == "completed"`（`finalizeSessionStatusOnDisconnect` 経由。relay に EventResult 済み）
6. FAIL 時: `active` のまま → `handler.go` の `finalizeSessionStatusOnDisconnect` を調査（本計画ではテスト追加のみが原則）

**R13 — `TestRespondJSONRelay_EarlyStatusOnTerminalEvent`**:
1. `srv.RegisterAgent(&mockTerminalEventAgent{name: "codex"})`
2. POST session → `sessionID`
3. POST messages with **`Accept: application/json`**（SSE ではない）
4. 応答 JSON decode 前に goroutine で polling:
```go
deadline := time.Now().Add(2 * time.Second)
for time.Now().Before(deadline) {
    sess := getSession(...)
    if sess["status"] == "completed" { return /* PASS */ }
    time.Sleep(20 * time.Millisecond)
}
t.Fatal("status not completed before JSON response finished")
```
5. その後 JSON body に `EventResult` 相当イベントが含まれることを確認

**`getSession` ヘルパ**（handler_test.go 内 private）:
```go
func getSession(t *testing.T, srv *agentservice.Server, sessionID string) map[string]interface{} {
    req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/"+sessionID, nil)
    rec := httptest.NewRecorder()
    srv.HTTPHandler().ServeHTTP(rec, req)
    var out map[string]interface{}
    json.NewDecoder(rec.Body).Decode(&out)
    return out
}
```

---

### Phase 2 — 統合 E2E テスト（tests/）

#### [MODIFY] [tests/codex_client_v1_large_output_e2e_test.go](file://tests/codex_client_v1_large_output_e2e_test.go)

* **Description**: R6, R9, R11a, R12。000 テスト維持 + 新規 4 テスト追加。
* **Technical Design**:

```go
// startFakeCodexE2EServerWithLines — Lines / DelayMS を指定可能に拡張
func startFakeCodexE2EServerWithLines(t *testing.T, opts testutil.FakeCodexOptions) (baseURL string, cleanup func())

// assertSSEDataLinesUnder64KB — default bufio.Scanner で raw SSE body を検証（R6）
func assertSSEDataLinesUnder64KB(t *testing.T, sseBody string)

func TestCodexE2E_ClientV1_MaxTruncatedToolOutputTerminalEvent(t *testing.T)
func TestCodexE2E_ClientV1_NoDataSilenceDuringLargeToolTurn(t *testing.T)
func TestCodexE2E_ClientV1_RipgrepLikeMultiLineOutput(t *testing.T)
func TestCodexE2E_ClientV1_MultipleLargeToolResults(t *testing.T)
```

* **Logic**:

**`startFakeCodexE2EServerWithLines`**:
- 既存 `startFakeCodexE2EServer` をリファクタし、`FakeCodexOptions` を引数化
- 既存 `TestCodexE2E_ClientV1_LargeToolOutputTerminalEvent` は `FakeCodexOptions{}`（デフォルト 65537 B lines）で **挙動不変**

**`assertSSEDataLinesUnder64KB`**:
```go
scanner := bufio.NewScanner(strings.NewReader(sseBody)) // 拡張なし
for scanner.Scan() {
    line := scanner.Text()
    if !strings.HasPrefix(line, "data: ") { continue }
    data := strings.TrimPrefix(line, "data: ")
    if data == "[DONE]" { break }
    if len(data) >= 64*1024 {
        t.Fatalf("SSE data line len %d >= 64KiB", len(data))
    }
}
if err := scanner.Err(); err != nil {
    t.Fatalf("default scanner failed: %v", err)
}
```

**R6 — `TestCodexE2E_ClientV1_MaxTruncatedToolOutputTerminalEvent`**:
1. `lines := testutil.BuildLargeAggregatedOutputLines(codingagent.DefaultMaxToolResultBytes)`
2. `startFakeCodexE2EServerWithLines(t, testutil.FakeCodexOptions{Lines: lines})`
3. **raw SSE 検証**: 同一 session で `http.Client` 直接 POST（`Accept: text/event-stream`）は不可（session 二重実行）のため、**別テスト内で raw 取得**:
   - Option A（採用）: client/v1 実行後、parallel helper session で同型 fake codex + raw POST → `assertSSEDataLinesUnder64KB`
   - Option B: R6 内で client/v1 のみ実行し、raw 検証は R14 に委譲
   - **本計画では Option A**: 同一テスト内で 2 セッション（client/v1 検証 + raw scanner 検証）を **同一 server** 上で実行
4. `wantToolContent := strings.Repeat("x", codingagent.DefaultMaxToolResultBytes)`
5. `RunWithHandlers` → `gotResult`, `len(toolResults)==1`, `len(toolResults[0])==DefaultMaxToolResultBytes`
6. `GetSession` → `completed`

**R9 — `TestCodexE2E_ClientV1_NoDataSilenceDuringLargeToolTurn`**:
1. `lines := testutil.BuildLargeAggregatedOutputLines(codingagent.DefaultMaxToolResultBytes)`
2. `FakeCodexOptions{Lines: lines, DelayMS: 2000}`
3. `Events()` で消費、`lastDataTime := time.Now()` を各 `data:` イベント（keepalive コメント除く）で更新
4. 任意 2 連続 data イベント間隔 `< 30s`
5. 最初の data から `EventResult` まで **`< 60s`**
6. `status == completed`

**R11a — `TestCodexE2E_ClientV1_RipgrepLikeMultiLineOutput`**:
1. `content := testutil.BuildRipgrepLikeOutput(200 * 1024)`
2. `lines := testutil.BuildLargeAggregatedOutputLines(len(content))` — **content 長を正確に**するため、ヘルパを拡張:

```go
// BuildAggregatedOutputLinesFromContent — 任意 content 文字列版（fake_codex.go に追加）
func BuildAggregatedOutputLinesFromContent(content string) []string
```

3. `RunWithHandlers` → tool content **バイト一致** + `completed`

**R12 — `TestCodexE2E_ClientV1_MultipleLargeToolResults`**:
1. `lines := testutil.BuildMultiToolOutputLines(100*1024, 100*1024)`
2. `OnToolResult` 2 回、各 `len==100*1024`、`OnResult` 1 回、`completed`

---

#### [NEW] [tests/codex_legacy_client_large_output_e2e_test.go](file://tests/codex_legacy_client_large_output_e2e_test.go)

* **Description**: R7。legacy `client` パッケージ（`SendMessage` + `Events()`）。
* **Technical Design**:

```go
func TestCodexE2E_LegacyClient_MaxTruncatedToolOutputTerminalEvent(t *testing.T) {
    lines := testutil.BuildLargeAggregatedOutputLines(codingagent.DefaultMaxToolResultBytes)
    baseURL, cleanup := startFakeCodexE2EServerWithLines(t, testutil.FakeCodexOptions{Lines: lines})
    defer cleanup()

    ctx := context.Background()
    c := client.New(baseURL, client.WithNoTimeout())
    sess, err := c.CreateSession(ctx, client.SessionRequest{Agent: "codex", WorkDir: t.TempDir()})

    stream, err := sess.SendMessage(ctx, "trigger")
    wantLen := codingagent.DefaultMaxToolResultBytes
    var toolResults []string
    var gotResult bool
    for ev := range stream.Events() {
        switch ev.Type {
        case client.EventToolResult:
            toolResults = append(toolResults, ev.Text)
        case client.EventResult:
            gotResult = true
        case client.EventError:
            t.Fatalf("stream error: %s", ev.Error)
        }
    }
    // gotResult, len(toolResults)==1, len(toolResults[0])==wantLen, status==completed
}
```

* **Logic**:
  - legacy `Run()` は `EventToolResult` をコールバックしないため **`Events()` 必須**
  - `startFakeCodexE2EServerWithLines` は `codex_client_v1_large_output_e2e_test.go` と同一 package `llm_test` で共有

---

#### [MODIFY] [tests/codex_session_status_e2e_test.go](file://tests/codex_session_status_e2e_test.go)

* **Description**: R8b。000 `TestCodexE2E_SessionStatusOnTerminalEvent` 維持 + 切断テスト追加。
* **Technical Design**:

```go
func TestCodexE2E_ClientV1_DisconnectAfterTerminalEventUpdatesStatus(t *testing.T)
```

* **Logic**:
1. fake codex（デフォルト 65537 B 以上）
2. `SendText` → `stream.Events()` を goroutine で消費
3. `EventResult` 受信時: **`stream` の underlying body を Close**（`client/v1` では `Events()` goroutine が body close するため、**cancel ctx + 読取停止** で代用）

```go
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

stream, _ := sess.SendText(ctx, "trigger")
resultSeen := make(chan struct{})
go func() {
    for ev := range stream.Events() {
        if ev.Type == v1.EventResult {
            close(resultSeen)
            cancel() // HTTP request context cancel → server ctx.Done()
            return
        }
    }
}()
<-resultSeen

deadline := time.Now().Add(500 * time.Millisecond)
for time.Now().Before(deadline) {
    session, _ := client.GetSession(ctx, sess.ID)
    if session["status"] == "completed" { return }
    time.Sleep(50 * time.Millisecond)
}
t.Fatal("status not completed after disconnect")
```

4. PASS: 500 ms 以内 `completed`

---

#### [NEW] [tests/sse_consumer_reference_test.go](file://tests/sse_consumer_reference_test.go)

* **Description**: R14。サードパーティ向け参照 SSE 消費パターン。
* **Technical Design**:

```go
package llm_test

func TestSSEConsumerReference_DefaultScannerReadsChunkedStream(t *testing.T) {
    // Reuse mockLargeToolResultAgent pattern from handler_sse_test.go
    // OR startFakeCodexE2EServerWithLines(256KB) + raw http POST

    resp := rawHTTPPostSSE(t, baseURL, sessionID, "trigger")
    defer resp.Body.Close()

    scanner := bufio.NewScanner(resp.Body) // NO buffer extension
    var parts []toolResultPart
    var gotResult bool
    for scanner.Scan() {
        line := scanner.Text()
        if strings.HasPrefix(line, ": ") { continue } // keepalive comment
        if !strings.HasPrefix(line, "data: ") { continue }
        data := strings.TrimPrefix(line, "data: ")
        if data == "[DONE]" { break }
        if len(data) >= 64*1024 {
            t.Fatalf("scanner would fail: line len %d", len(data))
        }
        // parse JSON, collect tool_result_part, detect empty tool_result completion
    }
    if err := scanner.Err(); err != nil { t.Fatal(err) }

    assembled := reassembleParts(parts) // inline helper mirroring sse-chunk-protocol.md
    if len(assembled) != codingagent.DefaultMaxToolResultBytes { t.Fatal(...) }
    if !gotResult { t.Fatal(...) }
}
```

* **Logic**:
- `toolResultPart` struct: `{ChunkID string; Index, Total int; Content string}`
- `reassembleParts`: index 順 concat（`codingagent.ReassembleToolResultParts` を tests から import 可能）
- `rawHTTPPostSSE`: `tests/agentservice_e2e_test.go` の `sendE2EMessage` パターンを流用

---

### Phase 3 — 実 Codex E2E（R11b）

#### [NEW] [tests/codex_real_large_output_e2e_test.go](file://tests/codex_real_large_output_e2e_test.go)

* **Description**: R11b。実 Codex CLI + `client/v1`。無ければ skip。
* **Technical Design**:

```go
func TestCodexE2E_RealCLI_ClientV1_LargeSearchOutput(t *testing.T) {
    if _, err := exec.LookPath("codex"); err != nil {
        t.Skipf("codex CLI not found: %v", err)
    }
    // Optional: skip if no API key — check vault or env

    baseURL, cleanup := startCodexE2EServer(t) // from codex_e2e_test.go
    defer cleanup()

    workDir := t.TempDir()
    // Generate many small files with repeated "SEARCHABLE_TOKEN" content
    for i := 0; i < 500; i++ {
        path := filepath.Join(workDir, fmt.Sprintf("file_%04d.txt", i))
        os.WriteFile(path, []byte(strings.Repeat("SEARCHABLE_TOKEN\n", 200)), 0644)
    }

    ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
    defer cancel()

    client := v1.New(baseURL, v1.WithNoTimeout())
    sess, _ := client.CreateSession(ctx, v1.SessionRequest{Agent: "codex", WorkDir: workDir})

    prompt := "Search the workspace for all occurrences of SEARCHABLE_TOKEN using ripgrep or grep. Report the count, then finish."
    stream, _ := sess.SendText(ctx, prompt)

    var gotResult bool
    err := stream.RunWithHandlers(ctx, sess, v1.StreamHandlers{
        OnResult: func() { gotResult = true },
    })
    if err != nil { t.Fatalf("RunWithHandlers: %v", err) }
    if !gotResult { t.Fatal("no EventResult") }

    session, _ := client.GetSession(ctx, sess.ID)
    if session["status"] != "completed" { t.Fatalf("status=%v", session["status"]) }
}
```

* **Logic**:
  - 120 s timeout（仕様 Pass criteria）
  - codex / API key 無し → `t.Skip`（R15 doc に skip 代替を記載）
  - rollout `task_complete` 確認は **optional log**（必須 PASS 条件外。status + EventResult で sufficient）

---

### Phase 4 — ドキュメント（R15）

#### [NEW] [docs/issue-26-verification.md](file://docs/issue-26-verification.md)

* **Description**: R15。完了判定トレーサビリティ。
* **Logic** — 記載内容:

1. **Failure mode トレーサビリティ表**:

| Issue #26 | 000 対策 | 001 検証 | テスト名 |
|-----------|---------|---------|---------|
| FM1 scanner fail | tool_result_part | R6,R7,R10,R14 | `TestCodexE2E_ClientV1_Max...`, etc. |
| FM2 data silence | chunking | R9,R11 | `TestCodexE2E_ClientV1_NoDataSilence...` |
| L2 status stuck | early update | R8,R13,000 R1b | `TestStreamSSERelay_Early...` |
| 本番 ripgrep | — | R11 | `TestCodexE2E_RealCLI...` / R11a |

2. **完全解決の定義**（仕様書 5 条件）とテスト対応
3. **R11b skip 時の代替根拠**（R11a + R6 + R9 PASS）
4. Link: `docs/sse-chunk-protocol.md`
5. **R16 ゲートコマンド** コピー

---

### Phase 5 — 条件付き production 修正（R10 RED 時のみ）

#### [MODIFY] [shared/libs/go/codingagent/sse_chunk.go](file://shared/libs/go/codingagent/sse_chunk.go)

* **Description**: R10a RED 時のみ。001 のデフォルトパスでは **触らない**。
* **Technical Design**（RED 時のフォールバック）:

```go
// Option 1: reduce constant
const DefaultSSEChunkContentBytes = 32 * 1024

// Option 2: dynamic shrink loop inside SplitStreamEventForSSE
for chunkSize := DefaultSSEChunkContentBytes; chunkSize >= 4096; chunkSize /= 2 {
    events := buildChunks(ev, chunkSize)
    if allMarshaledUnderLimit(events, limit) {
        return events, nil
    }
}
return nil, fmt.Errorf("cannot split tool_result under %d bytes", limit)
```

* **Logic**: R10a/c が GREEN になる最小変更。Option 1 を先に試し、不足なら Option 2。

---

## Step-by-Step Implementation Guide

進捗はチェックボックスで管理する。各 Step 完了後に `./scripts/process/build.sh` または該当 integration test で確認。

### Step 0: fake codex ヘルパ

- [x] Edit `tests/testutil/fake_codex.go` — add `BuildLargeAggregatedOutputLines`, `BuildAggregatedOutputLinesFromContent`, `BuildRipgrepLikeOutput`, `BuildMultiToolOutputLines`
- [x] Verify existing `BuildThreeLineReproLines` unchanged
- [x] Run `./scripts/process/build.sh`

### Step 1: R10 単体テスト

- [x] Edit `shared/libs/go/codingagent/sse_chunk_test.go` — add R10a, R10b, R10c + `assertAllWireEventsUnderLimit`
- [x] Run `./scripts/process/build.sh`
- [x] If RED: apply Phase 5 `sse_chunk.go` fix, re-run build.sh

### Step 2: R8a + R13 handler 単体

- [x] Edit `shared/libs/go/agentservice/handler_test.go` — add mocks + `TestStreamSSERelay_EarlyStatusUpdate`, `TestStreamSSERelay_DisconnectUpdatesStatus`, `TestRespondJSONRelay_EarlyStatusOnTerminalEvent`
- [x] Run `./scripts/process/build.sh`
- [x] Run `./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestStreamSSERelay"`
- [x] Run `./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestRespondJSONRelay"`

### Step 3: E2E ヘルパリファクタ

- [x] Edit `tests/codex_client_v1_large_output_e2e_test.go` — refactor `startFakeCodexE2EServerWithLines`, add `assertSSEDataLinesUnder64KB`
- [x] Confirm 000 test `TestCodexE2E_ClientV1_LargeToolOutputTerminalEvent` still PASS

### Step 4: R6, R9, R11a, R12 E2E

- [x] Add `TestCodexE2E_ClientV1_MaxTruncatedToolOutputTerminalEvent`
- [x] Add `TestCodexE2E_ClientV1_NoDataSilenceDuringLargeToolTurn`
- [x] Add `TestCodexE2E_ClientV1_RipgrepLikeMultiLineOutput`
- [x] Add `TestCodexE2E_ClientV1_MultipleLargeToolResults`
- [x] Run `./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestCodexE2E_ClientV1"`

### Step 5: R7 legacy client E2E

- [x] Create `tests/codex_legacy_client_large_output_e2e_test.go`
- [x] Run `./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestCodexE2E_LegacyClient"`

### Step 6: R8b 切断 E2E

- [x] Edit `tests/codex_session_status_e2e_test.go` — add `TestCodexE2E_ClientV1_DisconnectAfterTerminalEventUpdatesStatus`
- [x] Run `./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestCodexE2E_SessionStatus"`

### Step 7: R14 参照 SSE 消費者

- [x] Create `tests/sse_consumer_reference_test.go`
- [x] Run `./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestSSEConsumer"`

### Step 8: R11b 実 Codex E2E

- [x] Create `tests/codex_real_large_output_e2e_test.go`
- [x] Run `./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestCodexE2E_RealCLI"`

### Step 9: R15 ドキュメント

- [x] Create `docs/issue-26-verification.md` with traceability table

### Step 10: R16 完了ゲート

- [x] Run full gate (Verification Plan below)
- [x] Mark all Step checkboxes `[x]` in this plan file

---

## Verification Plan

### Automated Verification

#### Unit + build（各 Step 後）

```bash
./scripts/process/build.sh
```

#### Integration — 段階的

```bash
./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestSplitStreamEvent"
./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestStreamSSERelay"
./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestRespondJSONRelay"
./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestCodexE2E"
./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestCodexScanner"
./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestSSEConsumer"
```

#### R16 完了ゲート（001 唯一の完了条件）

```bash
./scripts/process/build.sh
./scripts/process/integration_test.sh --specify "TestCodexE2E"
./scripts/process/integration_test.sh --specify "TestCodexScanner"
./scripts/process/integration_test.sh --specify "TestSSEConsumer"
./scripts/process/integration_test.sh --specify "TestSplitStreamEvent"
./scripts/process/integration_test.sh --specify "TestStreamSSERelay"
./scripts/process/integration_test.sh --specify "TestRespondJSONRelay"
```

**期待**: 全コマンド exit 0。000 回帰テスト含む:

| テスト | 所属 |
|--------|------|
| `TestCodexE2E_ClientV1_LargeToolOutputTerminalEvent` | 000 |
| `TestCodexE2E_SessionStatusOnTerminalEvent` | 000 |
| `TestCodexE2E_LargeToolOutputTerminalEvent` | L1 |
| `TestCodexScannerIntegration_LargeOutputMissingEventResult` | L1 |
| `TestHandleSendMessage_SSEChunkedToolResult` | 000 |
| `TestSplitStreamEventForSSE_*`（既存+R10） | 000+001 |
| `TestStream_Events_*` | 000 |

### E2E Tests Summary（tests/ 配下）

| ファイル | 新規テスト |
|----------|-----------|
| `tests/testutil/fake_codex.go` | ヘルパ（テスト支援） |
| `shared/libs/go/codingagent/sse_chunk_test.go` | R10a–c |
| `shared/libs/go/agentservice/handler_test.go` | R8a, R13 |
| `tests/codex_client_v1_large_output_e2e_test.go` | R6, R9, R11a, R12 |
| `tests/codex_legacy_client_large_output_e2e_test.go` | R7 |
| `tests/codex_session_status_e2e_test.go` | R8b |
| `tests/sse_consumer_reference_test.go` | R14 |
| `tests/codex_real_large_output_e2e_test.go` | R11b |

---

## Documentation

| ファイル |  action |
|----------|--------|
| [docs/issue-26-verification.md](file://docs/issue-26-verification.md) | **新規** — R15 トレーサビリティ + R16 ゲート |
| [docs/sse-chunk-protocol.md](file://docs/sse-chunk-protocol.md) | 変更なし（000 済）。issue-26-verification からリンク |
| [prompts/.../plans/001-Complete-Issue-26-Verification.md](file://prompts/phases/000-foundation/branches/bugfix-#26/plans/001-Complete-Issue-26-Verification.md) | 本ファイル — Step 完了時に `[x]` 更新 |

---

## Out of Scope（本計画）

- R17 fuzz（任意）
- R18 並列負荷（任意）
- R19 Windows 手動記録（任意）
- L1 / bugfix-#24 再修正
- `EventText` チャンク分割
- Production コード変更（R10 GREEN 時を除く）
