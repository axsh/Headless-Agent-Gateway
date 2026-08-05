# 000-SSE-Client-Session-Status-Fix

> **Source Specification**: `prompts/phases/000-foundation/branches/bugfix-#26/ideas/000-SSE-Client-Session-Status-Fix.md`

## Goal Description

Issue #26 ([axsh/arctic-tern#26](https://github.com/axsh/arctic-tern/issues/26)) において、v0.1.5 L1 修正後も残る **L2（セッション status 更新タイミング）** と **L3（64 KiB SSE 行上限と client/v1 非互換）** を修正する。

本計画は **Repro-first (Phase 1 RED → Phase 2 修正 → Phase 3 GREEN)** で以下を実装する:

1. **R0/R1**: `client/v1` 経路の再現 E2E（修正前 RED）
2. **R2**: SSE `tool_result` の **チャンク分割**（`tool_result_part`）+ 公式 Go クライアント **再構成**
3. **R3**: ターミナル relay イベント時の **早期 session status 更新** + 切断時整合
4. **R4**: 定数集中管理
5. **R5**: チャンクプロトコル文書化

**不採用**: クライアント側 `NewLargeLineScanner` 拡張（仕様書 案 A 採用のため）。

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R0: 再現 E2E を修正実装より先に追加（Repro-first） | Phase 1 → `tests/codex_client_v1_large_output_e2e_test.go` |
| R1: `client/v1` 経路 E2E — 大出力 → EventResult + completed | Phase 1/3 → 同上 |
| R1b: ターミナル到達前 session status E2E | Phase 1/3 → `tests/codex_session_status_e2e_test.go` |
| R2a: `tool_result_part` プロトコル拡張 | `event.go`, `client/v1/stream.go` (`EventType`) |
| R2b: サーバー SSE 分割 (`SplitStreamEventForSSE`) | `sse_chunk.go`, `handler.go` |
| R2c: クライアント再構成 (`chunkAssembler`) | `client/v1/stream.go`, `client/stream.go` |
| R2d: 分割・再構成単体テスト | `sse_chunk_test.go`, `client/v1/stream_test.go`, `handler_test.go` |
| R3: 早期 session status + 切断時更新 | `handler.go`, `exec_registry.go` (必要時), `handler_test.go` |
| R4: `DefaultSSEChunkContentBytes` / `DefaultMaxSSEDataLineBytes` | `sse_chunk.go` |
| R5: チャンクプロトコル文書化 | `docs/sse-chunk-protocol.md` (新規) |

---

## Proposed Changes

### Phase 1 — 再現 E2E (修正前、RED 確認)

#### [NEW] [tests/codex_client_v1_large_output_e2e_test.go](file://tests/codex_client_v1_large_output_e2e_test.go)

* **Description**: R0/R1。Issue #26 failure mode を **`client/v1` 経路のみ** で再現する E2E。`parseE2ESSEEvents` / `NewLargeLineScanner` は **使用禁止**。
* **Technical Design**:

```go
func TestCodexE2E_ClientV1_LargeToolOutputTerminalEvent(t *testing.T) {
    // Setup: 既存 TestCodexE2E_LargeToolOutputTerminalEvent と同一
    //   - testutil.InstallFakeCodex (ExitCode: 0)
    //   - agentservice.New + codex adapter 登録 + Launch
    //   - fake stdout: item.started → oversized aggregated_output (65537+ bytes) → turn.completed

    ctx := context.Background()
    client := v1.New(baseURL, v1.WithNoTimeout())
    sess, err := client.CreateSession(ctx, v1.SessionRequest{
        Agent:   "codex",
        WorkDir: workDir,
    })

    stream, err := sess.SendText(ctx, "trigger")

    var gotResult bool
    var toolResults []string
    err = stream.RunWithHandlers(ctx, sess, v1.StreamHandlers{
        OnToolResult: func(content string) { toolResults = append(toolResults, content) },
        OnResult:     func() { gotResult = true },
    })
    // Phase 1 RED: err != nil (token too long / without completion marker)
    //             OR !gotResult

    // Phase 3 GREEN:
    //   - err == nil, gotResult == true
    //   - len(toolResults) == 1
    //   - toolResults[0] が L1 切り詰め後 content と一致（先頭/末尾マーカー検証）
    //   - getE2ESession(...).status == "completed"
}
```

* **Logic**:
  - fake codex の oversized 行: `padding := strings.Repeat("x", 65537)` を `aggregated_output` に埋め込む（`codex/process_repro_test.go` と同型）
  - **Phase 1 (現行コード)**: **FAIL** — `Stream.RunWithHandlers` が scanner error で終了、または `EventResult` 未到達
  - **Phase 3 (修正後)**: **PASS**
  - `-short` でもスキップしない

#### [NEW] [tests/codex_session_status_e2e_test.go](file://tests/codex_session_status_e2e_test.go)

* **Description**: R1b。`EventResult` 送出後・`[DONE]` 前の session status を検証。
* **Technical Design**:

```go
func TestCodexE2E_SessionStatusOnTerminalEvent(t *testing.T) {
    // fake codex: oversized tool output + turn.completed (速やかに完了)
    // client/v1 SendText → Events() で消費

    statusCh := make(chan string, 1)
    go func() {
        for {
            sess := getE2ESession(t, baseURL, sessionID)
            if s, _ := sess["status"].(string); s == "completed" || s == "error" {
                statusCh <- s
                return
            }
            time.Sleep(50 * time.Millisecond)
        }
    }()

    // Events() で result イベント受信直後に statusCh を確認
    // Phase 1 RED: result 受信後も status == "active"
    // Phase 3 GREEN: result 受信後 500ms 以内に status == "completed"
}
```

* **Logic**: ポーリング間隔 50ms、タイムアウト 5s。SSE ストリームが `[DONE]` 前に status が `completed` になることを検証。

---

### Phase 2 — プロトコル・共通ユーティリティ (R2a, R4)

#### [MODIFY] [shared/libs/go/codingagent/event.go](file://shared/libs/go/codingagent/event.go)

* **Description**: R2a — 新イベント型とチャンクメタデータフィールド追加。
* **Technical Design**:

```go
const (
    // ... existing ...
    // EventToolResultPart is a chunk of a large tool_result for SSE wire format.
    EventToolResultPart EventType = "tool_result_part"
)

type StreamEvent struct {
    Type       EventType              `json:"type"`
    Content    string                 `json:"content,omitempty"`
    PromptID   string                 `json:"prompt_id,omitempty"`
    Choices    []string               `json:"choices,omitempty"`
    ToolName   string                 `json:"tool_name,omitempty"`
    ToolInput  map[string]interface{} `json:"tool_input,omitempty"`
    SessionID  string                 `json:"session_id,omitempty"`
    ChunkID    string                 `json:"chunk_id,omitempty"`
    ChunkIndex int                    `json:"index,omitempty"`
    ChunkTotal int                    `json:"total,omitempty"`
    Error      error                  `json:"-"`
}
```

* **Logic**: JSON タグ `index` / `total` は仕様書プロトコル表に準拠。Go フィールド名は `ChunkIndex` / `ChunkTotal`。

#### [NEW] [shared/libs/go/codingagent/sse_chunk_test.go](file://shared/libs/go/codingagent/sse_chunk_test.go)

* **Description**: R2d — 分割ユーティリティの TDD テスト（Phase 2 実装前に追加、Phase 2 開始時 RED）。
* **Logic** — テーブル駆動:

| テスト名 | 入力 | 期待 |
|----------|------|------|
| `TestSplitStreamEventForSSE_SmallPayloadNoSplit` | content 100B の `EventToolResult` | 返却 1 件、type=`tool_result`、元 content 保持 |
| `TestSplitStreamEventForSSE_LargePayloadChunksUnder64KB` | content 256KB (`DefaultMaxToolResultBytes`) | 返却 N+1 件（N×`tool_result_part` + 1×完了 `tool_result`）。各 `json.Marshal` 後サイズ `< DefaultMaxSSEDataLineBytes` |
| `TestSplitStreamEventForSSE_ReassemblyRoundTrip` | 256KB content | 全 part の content 連結 == 元 content。完了 `tool_result` の `ChunkID` が part と一致、`Content==""` |
| `TestSplitStreamEventForSSE_NonToolResultPassthrough` | `EventText`, `EventResult` | 入力 1 件をそのまま返却（分割しない） |

各 `TestSplitStreamEventForSSE_LargePayloadChunksUnder64KB` では:

```go
for _, ev := range events {
    data, err := json.Marshal(ev)
    require.NoError(t, err)
    require.Less(t, len(data), DefaultMaxSSEDataLineBytes,
        "SSE wire line must be under 64KiB")
}
```

#### [NEW] [shared/libs/go/codingagent/sse_chunk.go](file://shared/libs/go/codingagent/sse_chunk.go)

* **Description**: R2b/R4 — SSE ワイヤー送出用イベント分割。
* **Technical Design**:

```go
const (
    DefaultMaxSSEDataLineBytes  = 64 * 1024  // 64 KiB
    DefaultSSEChunkContentBytes = 48 * 1024  // 48 KiB
)

// SplitStreamEventForSSE splits oversized EventToolResult into wire-safe events.
// Non-EventToolResult events are returned unchanged as a single-element slice.
// maxLineBytes <= 0 uses DefaultMaxSSEDataLineBytes.
func SplitStreamEventForSSE(ev StreamEvent, maxLineBytes int) ([]StreamEvent, error)
```

* **Logic** — 分割アルゴリズム（仕様書 R2b から継承）:

1. `ev.Type != EventToolResult` → `return []StreamEvent{ev}, nil`
2. `limit := maxLineBytes`; `limit <= 0` → `limit = DefaultMaxSSEDataLineBytes`
3. `wire, _ := json.Marshal(ev)` — サイズ `< limit` → `return []StreamEvent{ev}, nil`
4. 超過時:
   - `chunkID := uuid.New().String()`（`github.com/google/uuid` — プロジェクト既存依存）
   - `content := ev.Content` を `DefaultSSEChunkContentBytes` ごとにバイト分割（`content[i:end]` スライス）
   - `total := ceil(len(content) / DefaultSSEChunkContentBytes)`
   - `index` 0..total-1 について `StreamEvent{Type: EventToolResultPart, ChunkID: chunkID, ChunkIndex: index, ChunkTotal: total, Content: part}` を append
   - 完了マーカー: `StreamEvent{Type: EventToolResult, ChunkID: chunkID, Content: ""}` を append
   - 各出力イベントについて `json.Marshal` 後サイズ `< limit` を assert（テストで保証）。超過する場合は `DefaultSSEChunkContentBytes` を二分探索で縮小する安全策を **実装しない**（48KB は JSON オーバーヘッド ~200B 程度の余裕あり。テスト RED なら chunk size 定数を下げる）

5. `chunk_id` は part 列と完了 `tool_result` で **同一 UUID**

---

### Phase 2 — AgentService SSE 送出 (R2b, R3)

#### [NEW] [shared/libs/go/agentservice/handler_test.go](file://shared/libs/go/agentservice/handler_test.go) (追記)

* **Description**: R2d/R3 — handler 単体テスト追加。
* **Logic**:

| テスト名 | 内容 |
|----------|------|
| `TestWriteSSEEvent_ChunkedToolResult` | 256KB `EventToolResult` を `writeSSEEvents` 経由で httptest ResponseRecorder に送出。body に `tool_result_part` が複数、`tool_result` 完了が含まれる。各行 `data:` プレフィックス後 JSON が 64KB 未満 |
| `TestStreamSSERelay_EarlyStatusUpdate` | mock agent が `EventResult` を送出 → relay 完了前に `GET session` が `completed` |
| `TestStreamSSERelay_DisconnectUpdatesStatus` | クライアント ctx cancel 後も relay に `EventResult` あり → session status が `completed`（`active` 残存しない） |

#### [MODIFY] [shared/libs/go/agentservice/handler.go](file://shared/libs/go/agentservice/handler.go)

* **Description**: R2b — SSE 分割送出。R3 — 早期 status 更新 + 切断時整合。
* **Technical Design**:

```go
// writeSSEWireEvents marshals and writes one or more wire events for a logical StreamEvent.
func (s *Server) writeSSEWireEvents(w http.ResponseWriter, flusher http.Flusher, ev codingagent.StreamEvent) error {
    wireEvents, err := codingagent.SplitStreamEventForSSE(ev, codingagent.DefaultMaxSSEDataLineBytes)
    if err != nil {
        return err
    }
    for _, wireEv := range wireEvents {
        data, _ := json.Marshal(wireEv)
        fmt.Fprintf(w, "data: %s\n\n", data)
        flusher.Flush()
    }
    return nil
}

// updateSessionStatusOnTerminal sets session record status when a terminal event is observed.
func (s *Server) updateSessionStatusOnTerminal(sessionID string, ev codingagent.StreamEvent, hasError bool, errorMsg string) {
    if ev.Type != codingagent.EventResult && ev.Type != codingagent.EventError {
        return
    }
    record, err := s.sessions.Get(sessionID)
    if err != nil {
        return
    }
    if hasError || ev.Type == codingagent.EventError {
        record.Status = codingagent.StatusError
        if errorMsg != "" {
            record.Error = errorMsg
        } else if ev.Content != "" {
            record.Error = ev.Content
        }
    } else {
        record.Status = codingagent.StatusCompleted
    }
    s.sessions.Update(record)
}

// finalizeSessionStatusOnDisconnect updates status when SSE client disconnects mid-stream.
func (s *Server) finalizeSessionStatusOnDisconnect(sessionID string, exec *activeExecution) {
    // Scan exec.relay buffered events for EventResult / EventError
    // If found: updateSessionStatusOnTerminal accordingly
    // If not found and exec still active: set status error "client disconnected before completion"
}
```

* **Logic** — `streamSSERelay` 改修:

1. 既存 `data, _ := json.Marshal(ev); fmt.Fprintf(...)` を `writeSSEWireEvents(w, flusher, ev)` に置換
2. 各 logical event 送出後、`updateSessionStatusOnTerminal(sessionID, ev, hasError, errorMsg)` を呼ぶ（`EventResult` / `EventError` 時）
3. `ctx.Done()` パス: `finalizeSessionStatusOnDisconnect(sessionID, exec)` を呼んでから return
4. `done:` ラベルの status 更新は **idempotent** に維持（早期更新済みでも上書きして問題なし）
5. `streamSSE`（legacy 関数）も同一 `writeSSEWireEvents` を適用
6. **`respondJSONRelay`**: 分割 **しない**（仕様書: JSON 応答は元イベント列）。terminal イベント時の早期 status 更新のみ追加

#### [MODIFY] [shared/libs/go/agentservice/exec_registry.go](file://shared/libs/go/agentservice/exec_registry.go)

* **Description**: R3 — 切断時に relay 内 terminal イベントを走査するための読み取り API。
* **Technical Design**:

```go
// EventsSnapshot returns a copy of buffered relay events (read-only).
func (r *eventRelay) EventsSnapshot() []codingagent.StreamEvent
```

* **Logic**: `mu` ロック下で `append([]StreamEvent(nil), r.events...)` を返す。`finalizeSessionStatusOnDisconnect` から使用。

---

### Phase 2 — クライアント再構成 (R2c)

#### [NEW] [client/v1/stream_test.go](file://client/v1/stream_test.go) (追記)

* **Description**: R2d — チャンク再構成の httptest。
* **Technical Design**:

```go
func TestStream_Events_ReassemblesToolResultParts(t *testing.T) {
    chunkID := "test-chunk-id"
    part0 := strings.Repeat("a", 100)
    part1 := strings.Repeat("b", 100)
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "text/event-stream")
        fmt.Fprintf(w, `data: {"type":"tool_result_part","chunk_id":"%s","index":0,"total":2,"content":"%s"}`+"\n\n", chunkID, part0)
        fmt.Fprintf(w, `data: {"type":"tool_result_part","chunk_id":"%s","index":1,"total":2,"content":"%s"}`+"\n\n", chunkID, part1)
        fmt.Fprintf(w, `data: {"type":"tool_result","chunk_id":"%s","content":""}`+"\n\n", chunkID)
        fmt.Fprintf(w, "data: {\"type\":\"result\"}\n\n")
        fmt.Fprintf(w, "data: [DONE]\n\n")
    }))
    // Events() から EventToolResult 1 件、Content == part0+part1
}
```

追加テスト:

| テスト名 | 内容 |
|----------|------|
| `TestStream_Events_SingleToolResultUnchanged` | 小ペイロード単一 `tool_result` → 従来どおり 1 イベント |
| `TestStream_Events_IncompleteChunksError` | part のみで `[DONE]` → `EventError` `incomplete tool_result chunks` |

#### [MODIFY] [client/v1/stream.go](file://client/v1/stream.go)

* **Description**: R2c — `chunkAssembler` による再構成。scanner はデフォルト 64 KiB のまま。
* **Technical Design**:

```go
const (
    EventToolResultPart EventType = "tool_result_part"
)

type Event struct {
    Type               EventType
    Text               string
    ToolName           string
    Error              string
    ChunkID            string
    ChunkIndex         int
    ChunkTotal         int
    UserInputRequired  UserInputRequiredEvent
}

type chunkBuffer struct {
    parts       map[int]string // index → content fragment
    total       int
    received    int
}

type chunkAssembler struct {
    pending map[string]*chunkBuffer // chunk_id → buffer
}

func newChunkAssembler() *chunkAssembler { ... }

// addPart buffers a tool_result_part. Returns reassembled content and true when complete marker received.
func (a *chunkAssembler) addPart(ev Event) (content string, complete bool, err error)

// handleToolResult processes a tool_result event (single or completion marker).
func (a *chunkAssembler) handleToolResult(ev Event) (content string, emit bool, err error)

// flushIncomplete returns error if stream ends with pending chunks.
func (a *chunkAssembler) flushIncomplete() error
```

* **Logic** — `events()` 改修:

1. `assembler := newChunkAssembler()` を goroutine 内で初期化
2. JSON raw struct に `ChunkID`, `Index`, `Total` フィールド追加（`json:"chunk_id"`, `json:"index"`, `json:"total"`）
3. `EventType == tool_result_part`:
   - `assembler.addPart(ev)` — index 欠落/total 不一致/chunk_id 混在 → `EventError` を ch に送出して return
4. `EventType == tool_result`:
   - `Content != ""` → 小ペイロード。**即座に** `EventToolResult` として ch に送出（後方互換）
   - `Content == "" && ChunkID != ""` → 完了マーカー。`assembler.handleToolResult(ev)` で再構成完了 → **1 つの** `EventToolResult`（再構成 content）を ch に送出
5. goroutine 終了時（scanner loop 後）: `assembler.flushIncomplete() != nil` → `EventError{Error: "incomplete tool_result chunks"}`
6. **`tool_result_part` は downstream に露出しない**（`Run` / `OnToolResult` / `Events()` すべて再構成後の `tool_result` のみ）

#### [MODIFY] [client/stream.go](file://client/stream.go)

* **Description**: レガシークライアントに `client/v1/stream.go` と同一の `chunkAssembler` ロジックを適用。
* **Logic**: 可能なら `chunkAssembler` を `client/internal/chunk` 等に共通化。最小スコープでは `client/stream.go` に v1 と同型コードを複製（共通化は任意）。

---

### Phase 3 — ドキュメント (R5)

#### [NEW] [docs/sse-chunk-protocol.md](file://docs/sse-chunk-protocol.md)

* **Description**: R5 — チャンクプロトコル文書化。
* **Logic** — 記載内容:
  - `tool_result_part` フィールド定義（仕様書 R2a 表をそのまま転記）
  - 完了 `tool_result`（`content: ""`, `chunk_id` 必須）の意味
  - サーバー保証: 各 `data:` 行 JSON `< 64 KiB`
  - 公式 Go クライアント (`client/v1`) は自動再構成
  - **`client/v1` 以外の SSE 消費者**は `tool_result_part` を処理する必要がある旨

---

## Step-by-Step Implementation Guide

### Phase 1: 再現 E2E（RED 確認）

- [x] **Step 1.1**: `tests/codex_client_v1_large_output_e2e_test.go` を新規作成。`TestCodexE2E_ClientV1_LargeToolOutputTerminalEvent` を実装（`client/v1` 経路のみ）。
- [x] **Step 1.2**: `tests/codex_session_status_e2e_test.go` を新規作成。`TestCodexE2E_SessionStatusOnTerminalEvent` を実装。
- [x] **Step 1.3**: `./scripts/process/build.sh` を実行。**両テストが FAIL であることを確認**（RED）。PASS の場合はテスト設計を見直し、Step 1.1 に戻る。
- [x] **Step 1.4**: Phase 1 完了をコミット: `test: add repro E2E for client/v1 SSE 64KB limit (Issue #26)`

### Phase 2: 本修正

- [x] **Step 2.1**: `shared/libs/go/codingagent/event.go` に `EventToolResultPart` と `ChunkID` / `ChunkIndex` / `ChunkTotal` を追加。
- [x] **Step 2.2**: `shared/libs/go/codingagent/sse_chunk_test.go` を新規作成（RED 確認）。
- [x] **Step 2.3**: `shared/libs/go/codingagent/sse_chunk.go` を実装。Step 2.2 テスト GREEN。
- [x] **Step 2.4**: `shared/libs/go/agentservice/exec_registry.go` に `EventsSnapshot()` 追加。
- [x] **Step 2.5**: `shared/libs/go/agentservice/handler_test.go` に `TestWriteSSEEvent_ChunkedToolResult` 等を追加（RED → GREEN）。
- [x] **Step 2.6**: `shared/libs/go/agentservice/handler.go` — `writeSSEWireEvents`, `updateSessionStatusOnTerminal`, `finalizeSessionStatusOnDisconnect` を実装。`streamSSERelay` / `streamSSE` / `respondJSONRelay` を改修。
- [x] **Step 2.7**: `client/v1/stream_test.go` に再構成テスト追加（RED）。
- [x] **Step 2.8**: `client/v1/stream.go` — `chunkAssembler` + `events()` 改修。Step 2.7 GREEN。
- [x] **Step 2.9**: `client/stream.go` に同一再構成ロジック適用。
- [x] **Step 2.10**: `./scripts/process/build.sh` — Phase 1 E2E が **PASS**、全単体テスト GREEN。
- [x] **Step 2.11**: コミット（分割）: `feat: split oversized tool_result into SSE chunks`, `feat: reassemble tool_result chunks in client/v1`, `fix: early session status update on terminal events`

### Phase 3: 回帰・文書化

- [x] **Step 3.1**: `docs/sse-chunk-protocol.md` を作成。
- [x] **Step 3.2**: `./scripts/process/build.sh` — 全 PASS。
- [x] **Step 3.3**: `./scripts/process/integration_test.sh --specify "TestCodexE2E_ClientV1"` — PASS。
- [x] **Step 3.4**: `./scripts/process/integration_test.sh --specify "TestCodexE2E_LargeToolOutput"` — L1 回帰 PASS。
- [x] **Step 3.5**: `./scripts/process/integration_test.sh --specify "TestCodexScanner"` — L1 回帰 PASS。
- [x] **Step 3.6**: `./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestCodexE2E"` — 最終回帰。

---

## Verification Plan

### Automated Verification

#### Phase 1 — RED 確認（修正前）

```bash
./scripts/process/build.sh
```

期待: `TestCodexE2E_ClientV1_LargeToolOutputTerminalEvent` **FAIL**、`TestCodexE2E_SessionStatusOnTerminalEvent` **FAIL**

```bash
./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestCodexE2E_ClientV1"
./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestCodexE2E_SessionStatus"
```

#### Phase 2 — 単体 + E2E GREEN

```bash
./scripts/process/build.sh
```

期待: `TestSplitStreamEventForSSE_*`, `TestStream_Events_ReassemblesToolResultParts`, `TestStreamSSERelay_*` すべて PASS

#### Phase 3 — 回帰・統合

```bash
./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestCodexE2E_ClientV1"
./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestCodexE2E_LargeToolOutput"
./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestCodexScanner"
./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestCodexE2E"
```

### E2E Tests (`tests/` 配下)

| テスト | Phase 1 | Phase 3 |
|--------|---------|---------|
| `TestCodexE2E_ClientV1_LargeToolOutputTerminalEvent` | FAIL | PASS |
| `TestCodexE2E_SessionStatusOnTerminalEvent` | FAIL | PASS |
| `TestCodexE2E_LargeToolOutputTerminalEvent` | PASS (維持) | PASS |
| `TestCodexScannerIntegration_LargeOutputMissingEventResult` | PASS (維持) | PASS |

### Manual Verification Scenarios (Issue #26 転記 — 自動 E2E でカバー)

1. Create a Tern session with Codex and a workspace large enough for ripgrep to produce heavy output.
2. Send a prompt that runs ripgrep (or similar) across the codebase, then completes the turn.
3. Subscribe to SSE via `client/v1` (`Stream.Events()` or equivalent).
4. In parallel, poll `GET /api/v1/sessions/{id}` until timeout.
5. After the turn, inspect rollout JSONL under the session directory for `task_complete`.

**Pass criteria:** SSE delivers `EventResult` + `[DONE]`; session GET returns `completed`.

→ `TestCodexE2E_ClientV1_LargeToolOutputTerminalEvent` + `TestCodexE2E_SessionStatusOnTerminalEvent` が自動検証。

---

## Documentation

| ファイル | 変更 |
|----------|------|
| [docs/sse-chunk-protocol.md](file://docs/sse-chunk-protocol.md) | **新規** — `tool_result_part` プロトコル、64 KiB 行保証、サードパーティ向け注意 |
