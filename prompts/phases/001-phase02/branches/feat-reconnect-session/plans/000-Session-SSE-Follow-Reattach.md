# 000-Session-SSE-Follow-Reattach

> **Source Specification**: [ideas/000-Session-SSE-Follow-Reattach.md](file://prompts/phases/001-phase02/branches/feat-reconnect-session/ideas/000-Session-SSE-Follow-Reattach.md)
>
> **関連 Issue**: [axsh/arctic-tern#46](https://github.com/axsh/arctic-tern/issues/46)

## Goal Description

進行中ターンの SSE を、新しいユーザーメッセージを積まずに `GET /api/v1/sessions/{id}/events` で再購読できるようにする。切断後も既定 90 秒は `execRegistry` とエージェントを残し、後続 Follow が単一購読を奪って論理オフセットから再生する。`client/v1` に `Follow` / `FollowFrom` を出す。Issue #41 の上流 reconnect は触らない。

## User Review Required

仕様の表（`/events`、steal、既定 90 秒、論理 `from`、完了後リプレイなし、v1 Follow）は計画に固定した。実装時にだけ必要な補足は次のとおり。反対がなければこのまま進める。

1. **turn context の `id:`**: `EventSystem` `"turn context"` はリレー配列に入らない。各 SSE 接続の先頭で送り、**`id:` は付けない**。クエリ `from` / SSE `id` は `eventRelay.events` の 0 始まり index のみ。クライアントの `LastEventID` は `id:` 付き論理イベントが組み立て完了したときだけ進める。
2. **JSON（非 SSE）の `POST /messages`**: 現行の `respondJSONRelay` ブロッキングのまま。Follow は SSE 専用。
3. **プロセスリトライでリレーが差し替わるとき**: 同一 HTTP 購読は新しい `relay` を **index 0 から** 読み直す（現行 `streamOffset = 0` と同じ）。古い `from` が新バッファ長を超えた Follow は仕様どおり 400。
4. **R8（レガシー client）/ R9（完了後 TTL）/ R10（ternctl）**: 本計画では実装しない。
5. **既存ドレインテスト**: `WithSSEDrainTimeout` で短い猶予を上書きするテストは維持する。本番ゼロ値だけ 15s → 90s に変える。

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: `GET .../events`、406/404/409 `no active turn`、メッセージ非エンキュー | Proposed Changes > service.go `routeSessionByID`、handler_follow.go |
| R2: `from` / `Last-Event-ID`、論理 `id:`、不正値 400 | Proposed Changes > handler_follow.go parseFrom、handler.go `writeSSEWireEvents` |
| R3: 単一購読 + steal、steal で猶予を起動しない | Proposed Changes > exec_registry.go `attachSubscriber` |
| R4: 購読者 0 で 90s 猶予、Follow でキャンセル、尽きたら現行 kill。POST ハンドラは切断で HTTP 終了 | Proposed Changes > handler_retry.go 所有権分離、`defaultSSEClientDrainTimeout = 90s`、config YAML |
| R5: 副作用カーソルと SSE ライタ開始位置を分離。再生で二重適用しない | Proposed Changes > `pumpExecSideEffects`、`attachSSE` |
| R6: busy 維持、hint `follow, respond or terminate`、`followable` / `turn_id` | Proposed Changes > writeSessionBusy、sessionAPIResponse、handler_session.go |
| R7: `Follow` / `FollowFrom`、`LastEventID()`、自動 Follow しない | Proposed Changes > client/v1/session.go、stream.go |
| R8 / R9 / R10 | 本計画では実装しない |
| シナリオ A | Verification / `TestSessionFollow_ReattachContinuesTurn` |
| シナリオ B | Verification / `TestSessionFollow_ReplayFromStart` |
| シナリオ C | Verification / `TestSessionFollow_StealsExistingSubscriber` |
| シナリオ D | Verification / `TestSessionFollow_TimeoutThenNoActiveTurn` |
| シナリオ E | Verification / `TestSessionFollow_DoesNotEnqueueMessage` |
| シナリオ F | Verification / `TestSessionFollow_SuspendedThenRespond` |
| シナリオ G | Verification / `TestSessionFollow_CompletedRejected` |
| シナリオ H | Verification / `TestSessionFollow_ClientV1FollowFrom` |
| Issue 再現 4–5（busy PATCH/POST + 新 hint） | `TestSessionFollow_ReattachContinuesTurn` 内の 409 断言 |

## Proposed Changes

`Proposed Changes` は TDD のため **`_test.go` を先**に書く。

### config

#### [MODIFY] [shared/libs/go/config/config_test.go](file://shared/libs/go/config/config_test.go)

*   **Description**: Failed First。`sse_reattach_timeout_seconds` のゼロ値 → 90。非ゼロは上書きしない。
*   **Technical Design**:
    ```go
    func TestAgentServiceSSEReattachTimeout_ZeroBecomesNinety(t *testing.T) {
        var cfg config.Config
        cfg.AgentService.ApplyDefaults()
        if cfg.AgentService.SSEReattachTimeoutSeconds != 90 {
            t.Fatalf("got %d, want 90", cfg.AgentService.SSEReattachTimeoutSeconds)
        }
    }
    func TestAgentServiceSSEReattachTimeout_NoOverwrite(t *testing.T) {
        var cfg config.Config
        cfg.AgentService.SSEReattachTimeoutSeconds = 30
        cfg.AgentService.ApplyDefaults()
        if cfg.AgentService.SSEReattachTimeoutSeconds != 30 {
            t.Fatalf("got %d, want 30", cfg.AgentService.SSEReattachTimeoutSeconds)
        }
    }
    ```
*   **Logic**: YAML 未設定と `0` は未設定。無制限（負値）はテストで 90 に正規化するか、ApplyDefaults で `< 0` を 90 にする。仕様は無制限禁止。

#### [MODIFY] [shared/libs/go/config/config.go](file://shared/libs/go/config/config.go)

*   **Description**: `AgentServiceConfig` にフィールド追加。`ApplyDefaults` で 90。
*   **Technical Design**:
    ```go
    type AgentServiceConfig struct {
        // 既存 Port, DisableSandbox, EnableSubagent, Supplement, ProcessRetry
        SSEReattachTimeoutSeconds int `yaml:"sse_reattach_timeout_seconds"`
    }
    func (c *AgentServiceConfig) ApplyDefaults() {
        // 既存 ProcessRetry ゼロ値処理
        if c.SSEReattachTimeoutSeconds <= 0 {
            c.SSEReattachTimeoutSeconds = 90
        }
    }
    ```
*   **Logic**: 仕様の名称例 `sse_reattach_timeout_seconds` をそのまま使う。0 または未設定は 90。負値も 90。

### agentservice（hint / 応答型 / レジストリ）

#### [MODIFY] [shared/libs/go/agentservice/handler_session_test.go](file://shared/libs/go/agentservice/handler_session_test.go)

*   **Description**: Failed First。busy PATCH の hint、GET の `followable` / `turn_id`。
*   **Technical Design**:
    - 既存 `session busy` 断言に `follow, respond or terminate` を追加。
    - `MarkSessionBusy(id, "active")` 後 `GET /api/v1/sessions/{id}` が `"followable":true` と `"turn_id"` を含む（busy 登録時に turnID を渡せるよう `MarkSessionBusy` を拡張するか、テスト専用 Register）。
    - busy でない GET は `followable` が false または欠ける。
*   **Logic**: 仕様 R6。List は同じ `sessionResponse` 経由なら followable も付いてよい（exec 無ければ省略）。

#### [MODIFY] [shared/libs/go/agentservice/handler_test.go](file://shared/libs/go/agentservice/handler_test.go)

*   **Description**: Failed First。`POST /messages` 409 の hint 更新。切断後に HTTP が終わり、猶予内は busy のまま（既存 disconnect テストを所有権分離後も通す）。
*   **Technical Design**: `"hint":"respond or terminate"` を探す文字列があれば新 hint に置換。`TestStreamSSERelay_DisconnectUpdatesStatus` は「切断しても即 StatusError にしない。終端後 completed」を維持。ドレインはバックグラウンド副作用ポンプが担う。
*   **Logic**: 仕様 R4 / R6。

#### [MODIFY] [shared/libs/go/agentservice/handler_retry_test.go](file://shared/libs/go/agentservice/handler_retry_test.go)

*   **Description**: Failed First。`WithSSEDrainTimeout(80*time.Millisecond)` の猶予切れでプロセス停止・busy 解除は維持。steal / Follow は統合テスト側。hint 文字列があれば更新。
*   **Logic**: 仕様 R4。本番既定だけ 90s。テスト上書きは残す。

#### [NEW] [shared/libs/go/agentservice/handler_follow_test.go](file://shared/libs/go/agentservice/handler_follow_test.go)

*   **Description**: Failed First。単体で `handleFollow` のステータスコードと `from` 解析、SSE `id:`。
*   **Technical Design**（テーブル駆動）:
    ```go
    func TestHandleFollow_StatusCodes(t *testing.T) {
        // 404: 未知 session_id
        // 406: Accept に text/event-stream なし
        // 409 body error=no active turn: セッションはあるが exec なし
        // 400: from=abc, from=-1, from=99（バッファ長 0 超）
    }
    func TestParseFollowFrom(t *testing.T) {
        // query from 優先、なければ Last-Event-ID
        // 空 → start=0
        // from=3 → start=4
        // from= と Last-Event-ID=1 → start=2
    }
    func TestWriteSSEWireEvents_LogicalIDOnChunks(t *testing.T) {
        // 巨大 EventToolResult を Split した各 data 行の直前に同じ id: <n>
        // turn context 相当の EventSystem は id なし
    }
    ```
*   **Logic**: 仕様 R1 / R2。`from` は最後に完全受信した論理 index。サーバは **その次** から送る。省略はリレー先頭 0。`from` がバッファ長より大きい、または非整数は 400。

#### [MODIFY] [shared/libs/go/agentservice/exec_registry.go](file://shared/libs/go/agentservice/exec_registry.go)

*   **Description**: 購読世代、副作用オフセット、猶予タイマー。
*   **Technical Design**:
    ```go
    const hintSessionBusy = "follow, respond or terminate"

    type activeExecution struct {
        sessionID     string
        turnID        string
        correlationID string
        agentSess     codingagent.Session
        stdin         codingagent.StdinWriter
        relay         *eventRelay
        status        string
        streamOffset  int // 直近 SSE ライタが書き終わった次のリレー index（respond 継続用）
        sideEffectOffset int
        sseStarted    bool // attachSSE 内のローカルへ移行し、exec からは削除してよい

        subMu         sync.Mutex
        subCancel     context.CancelFunc
        subscriberGen int
        reattachTimer *time.Timer
        savedFiles    []string
    }
    ```
    `eventRelay` のバッファ・`stream(startIdx, stopOnUserInput)` は現行維持。
    ```go
    func (e *activeExecution) stealSubscriber() (gen int, subCtx context.Context) {
        // 旧 subCancel を呼ぶ（旧ライタ終了）。猶予タイマーが生きていれば Stop して nil。
        // 新しい context.WithCancel(context.Background()) を subCancel に保存。
        // subscriberGen++ して返す。
    }
    func (e *activeExecution) clearSubscriber(gen int, onZero func()) {
        // gen が現行と一致するときだけ subCancel=nil。onZero で猶予開始。
        // steal 後の旧ライタが onZero を呼んでも gen 不一致なら猶予を開始しない。
    }
    ```
*   **Logic**: 仕様 R3 / R4。購読者カウントは実質 0 または 1。後勝ち steal。steal 直後は猶予を起動しない。

#### [MODIFY] [shared/libs/go/agentservice/handler.go](file://shared/libs/go/agentservice/handler.go)

*   **Description**: busy JSON の hint、`writeSSEWireEvents` に論理 id、`handleSendMessage` の SSE 経路を「バックグラウンド実行 + attachSSE」。
*   **Technical Design**:
    ```go
    func writeSessionBusy(w http.ResponseWriter, status string) {
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusConflict)
        json.NewEncoder(w).Encode(map[string]any{
            "error":  "session busy",
            "status": status,
            "hint":   hintSessionBusy,
        })
    }

    func (s *Server) writeSSEWireEvents(w http.ResponseWriter, flusher http.Flusher, ev codingagent.StreamEvent, logicalID *int) error {
        wireEvents, err := codingagent.SplitStreamEventForSSE(ev, codingagent.DefaultMaxSSEDataLineBytes)
        // 各 wireEvents について:
        //   if logicalID != nil { fmt.Fprintf(w, "id: %d\n", *logicalID) }
        //   fmt.Fprintf(w, "data: %s\n\n", json)
        //   flusher.Flush()
    }
    ```
    `handleSendMessage` の exec 既存チェックは `writeSessionBusy` に置換。
    SSE のとき: `runTurn` を **独立 `execCtx` のゴルーチン**で開始し、登録完了を待ってから `attachSSE(r.Context(), w, exec, 0, true)`。クライアント切断で `attachSSE` が return してもゴルーチンのターンは続く。
    JSON のときは現行どおり呼び出し元ゴルーチンで `runTurn` 完了までブロック。
*   **Logic**: 仕様 R1（Follow は別経路）、R4（POST ハンドラは切断で HTTP 終了）、R6。`sseStarted` は接続ごと。turn context は毎回 `id:` 無しで送る。

#### [MODIFY] [shared/libs/go/agentservice/handler_retry.go](file://shared/libs/go/agentservice/handler_retry.go)

*   **Description**: `defaultSSEClientDrainTimeout` を 90s。ドレインループを HTTP から外し、副作用ポンプ + 購読者 0 の猶予にする。
*   **Technical Design**:
    ```go
    const defaultSSEClientDrainTimeout = 90 * time.Second
    ```
    `clientDrainTimeout()` は現行どおり `s.sseDrainTimeout > 0` なら上書き、否则 90s。
    `WithSSEDrainTimeout` コメントを「reattach 猶予のテスト上書き」に更新。

    **副作用ポンプ**（exec 登録後に 1 回 `go s.pumpExecSideEffects(sessionID, exec)`）:
    - `ch := exec.relay.stream(exec.sideEffectOffset, false)`（user_input で止めない）。
    - 各イベントで `handleRelaySideEffects(..., writeSSE=false, w=nil, flusher=nil)`、`sideEffectOffset++`。
    - リレー差し替え（プロセスリトライ）時は `sideEffectOffset=0` で新しい relay に付け直す。
    - `EventResult` または非 retryable `EventError` で `finishActiveExecution`（現行と同じ ingest / Close / Unregister）。購読中ならライタが `[DONE]` を書いてから finish する競合を避けるため、**ターミナル適用後に現行購読が終わるのを短い待ち合わせ**するか、ライタ側がターミナルを書いたあと finish を呼ぶ。推奨: **finish はポンプが呼ぶ**。ライタはバッファ再生のみ。Unregister 後も `EventsSnapshot` が残るよう、finish 前にライタへ `turnDone` を通知し、ライタが Result を flush してから Unregister。実装は `sync.WaitGroup` 1 本（現行 subscriber）を finish が待つ（タイムアウト付き、steal 中は新 subscriber を待つ）。
    - ポンプは HTTP に書かない。task log / AgentSessionID / suspended / status はここだけ。

    **`attachSSE(reqCtx, w, exec, from, stopOnUserInput) (streamTerminal, suspended)`**:
    - `stealSubscriber()` で旧ライタ cancel。
    - ヘッダ: `Content-Type: text/event-stream`、`Cache-Control: no-cache`、`Connection: keep-alive`（接続ごと。exec.sseStarted に依存しない）。
    - turn context を `logicalID=nil` で 1 回書く。
    - `ch := exec.relay.stream(from, stopOnUserInput)`。
    - `select`: `reqCtx.Done()` または `subCtx.Done()`（steal）→ 書き込み停止、`clearSubscriber`、購読 0 なら `startReattachTimer`。**プロセスは殺さない。streamOffset をドレインで進めない。**
    - イベント: `writeSSEWireEvents(..., &idx)`。`idx` はリレー index。`exec.streamOffset = idx+1`。keepalive 15s は現行どおり。
    - `EventUserInputRequired` かつ `stopOnUserInput`: `[DONE]`、suspended、return（Respond 待ち。exec は残す。猶予は購読 0 なら開始）。
    - チャネル終了または Result/Error: `[DONE]`、return。finish はポンプと調整。

    **`startReattachTimer`**: `time.AfterFunc(s.clientDrainTimeout(), ... stopExecOnDrainTimeout ...)`。既にタイマーがあれば Reset ではなく **Stop + 新規**（仕様: 切れたらゼロからやり直す）。
    **Follow / 再 attach**: `stealSubscriber` 内で timer.Stop。

    **`runTurn`**: エージェント Create/Send/relay 登録/プロセスリトライは現行。SSE 時は `streamSSERelay` を呼ばず、ポンプ + 最初の `attachSSE` は `handleSendMessage` 側。リトライで `active.relay` 差し替え、`streamOffset=0`、`sideEffectOffset=0`。retryable の判定はポンプまたは専用ループが `relay` の終端イベントを見る（現行 `streamSSERelay` の戻り `term.retryable` 相当をポンプが `runTurn` へチャネルで返す）。

    **`stopExecOnDrainTimeout`**: 現行維持（Warn `SSE drain timed out; stopping agent process`、`agentSess.Close`、Unregister、`drainTimeoutTerminalContent = "client drain timeout"`）。
*   **Logic**: 仕様 R4 / R5。切断した HTTP が `streamOffset` を進めながら Follow と競合してはならない。猶予中はプロセスと relay と exec を残す。尽きたら現行 kill。

#### [NEW] [shared/libs/go/agentservice/handler_follow.go](file://shared/libs/go/agentservice/handler_follow.go)

*   **Description**: `GET /api/v1/sessions/{id}/events`。
*   **Technical Design**:
    ```go
    func (s *Server) handleFollow(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodGet {
            http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
            return
        }
        sessionID := extractPathParam(r.URL.Path, "/api/v1/sessions/")
        if _, err := s.sessions.Get(sessionID); err != nil {
            http.Error(w, "session not found", http.StatusNotFound)
            return
        }
        if !strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
            http.Error(w, "Accept: text/event-stream required", http.StatusNotAcceptable)
            return
        }
        exec, ok := s.execRegistry.Get(sessionID)
        if !ok {
            w.Header().Set("Content-Type", "application/json")
            w.WriteHeader(http.StatusConflict)
            json.NewEncoder(w).Encode(map[string]any{"error": "no active turn"})
            return
        }
        from, err := parseFollowFrom(r, exec.relay.eventCount())
        if err != nil {
            http.Error(w, err.Error(), http.StatusBadRequest)
            return
        }
        // Send も stdin も呼ばない
        stopOnUserInput := exec.status != codingagent.StatusSuspended
        // suspended の Follow も user_input まで再生して止めてよい（シナリオ F）
        stopOnUserInput = true
        if /* respond 継続ではない */ true {
            _, _ = s.attachSSE(r.Context(), w, exec, from, true)
        }
    }

    func parseFollowFrom(r *http.Request, bufLen int) (start int, err error) {
        raw := r.URL.Query().Get("from")
        if raw == "" {
            raw = r.Header.Get("Last-Event-ID")
        }
        if raw == "" {
            return 0, nil
        }
        n, err := strconv.Atoi(raw)
        if err != nil || n < 0 {
            return 0, fmt.Errorf("invalid from")
        }
        start = n + 1
        if start > bufLen {
            return 0, fmt.Errorf("from exceeds buffer")
        }
        return start, nil
    }
    ```
*   **Logic**: 仕様 R1 / R2。セッション無し 404。exec 無し 409 `no active turn`（404 にしない）。メッセージ非エンキュー。`from` クエリが `Last-Event-ID` より優先。

#### [MODIFY] [shared/libs/go/agentservice/service.go](file://shared/libs/go/agentservice/service.go)

*   **Description**: ルートと設定配線。
*   **Technical Design**:
    ```go
    func (s *Server) routeSessionByID(w http.ResponseWriter, r *http.Request) {
        path := r.URL.Path
        if strings.HasSuffix(path, "/messages") { ... }
        else if strings.HasSuffix(path, "/respond") { ... }
        else if strings.HasSuffix(path, "/terminate") { ... }
        else if strings.HasSuffix(path, "/logs") { ... }
        else if strings.HasSuffix(path, "/events") { s.handleFollow(w, r) }
        else { GET/PATCH/DELETE }
    }
    ```
    `/events` を `/logs` より後でも suffix が違うので衝突しない。`/events` を明示する。
    `WithSSEDrainTimeout` は残す。本番は `server.go` から
    `WithSSEDrainTimeout(time.Duration(cfg.AgentService.SSEReattachTimeoutSeconds) * time.Second)`
    を渡す（ApplyDefaults 後なので 90 以上）。ゼロ秒上書きで即 kill しないこと。
*   **Logic**: 仕様 R1 / R4。

#### [MODIFY] [shared/libs/go/agentservice/handler_session.go](file://shared/libs/go/agentservice/handler_session.go)

*   **Description**: PATCH busy hint、GET followable。
*   **Technical Design**:
    ```go
    type sessionAPIResponse struct {
        codingagent.SessionRecord
        AgentBindings map[string]session.AgentBinding `json:"agent_bindings,omitempty"`
        ActiveAgent   string                          `json:"active_agent,omitempty"`
        Supplement    portable.Strategy               `json:"supplement,omitempty"`
        Followable    bool   `json:"followable,omitempty"`
        TurnID        string `json:"turn_id,omitempty"`
    }
    func (s *Server) sessionResponse(record *codingagent.SessionRecord) sessionAPIResponse {
        resp := sessionAPIResponse{SessionRecord: *record}
        if exec, ok := s.execRegistry.Get(record.ID); ok {
            resp.Followable = true
            resp.TurnID = exec.turnID
        }
        // 既存 AgentBindings / Supplement
        return resp
    }
    ```
    PATCH の 2 箇所の 409 を `writeSessionBusy` に置換。
*   **Logic**: 仕様 R6。exec 無ければ `followable` omitempty で欠ける。PATCH は busy のまま（Issue #32 は対象外）。

#### [MODIFY] [shared/libs/go/agentservice/handler.go](file://shared/libs/go/agentservice/handler.go)（respond）

*   **Description**: `handleRespond` は stdin 後 `attachSSE(..., exec.streamOffset, false)`。`session is not suspended` は維持。
*   **Logic**: 仕様 R6。active 中の再購読は Follow。Respond は suspended のみ。steal になる（旧 Follow が残っていれば奪う）。

### server 配線

#### [MODIFY] [server/server.go](file://server/server.go)

*   **Description**: ApplyDefaults 後に reattach 秒を `WithSSEDrainTimeout` へ。
*   **Logic**:
    ```go
    asOpts = append(asOpts, agentservice.WithSSEDrainTimeout(
        time.Duration(cfg.AgentService.SSEReattachTimeoutSeconds)*time.Second))
    ```
    `agentservice.New` 単体（httptest）はオプション無し → 90s 既定。

### client/v1

#### [MODIFY] [client/v1/session_test.go](file://client/v1/session_test.go) または [NEW] [client/v1/session_follow_test.go](file://client/v1/session_follow_test.go)

*   **Description**: Failed First。httptest で `Follow` が `GET /api/v1/sessions/{id}/events`、Accept SSE、`from` なし。`FollowFrom(ctx, "3")` が `?from=3`。406/409 はエラー。
*   **Logic**: 仕様 R7。`ResumeSession` はサーバを呼ばない（既存テスト維持）。

#### [MODIFY] [client/v1/stream_test.go](file://client/v1/stream_test.go)

*   **Description**: Failed First。`id:` 行を読み、`tool_result_part` では `LastEventID` を進めず、組み立て完了した `tool_result` で進める。id 無しの turn context では進まない。
*   **Technical Design**:
    ```go
    func TestStream_LastEventID_IgnoresPartsUntilComplete(t *testing.T)
    func TestStream_LastEventID_SkipsEventsWithoutID(t *testing.T)
    ```
*   **Logic**: 仕様 R7。`RunWithHandlers` に Follow を足さない。

#### [MODIFY] [client/v1/session.go](file://client/v1/session.go)

*   **Description**: Follow API。`SessionInfo` に `Followable bool` `TurnID string`（json omitempty）。
*   **Technical Design**:
    ```go
    func (s *Session) Follow(ctx context.Context) (*Stream, error) {
        return s.follow(ctx, "")
    }
    func (s *Session) FollowFrom(ctx context.Context, lastEventID string) (*Stream, error) {
        return s.follow(ctx, lastEventID)
    }
    func (s *Session) follow(ctx context.Context, from string) (*Stream, error) {
        u := s.client.baseURL + "/api/v1/sessions/" + s.ID + "/events"
        if from != "" {
            u += "?from=" + url.QueryEscape(from)
        }
        req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
        req.Header.Set("Accept", "text/event-stream")
        resp, err := s.client.httpClient.Do(req)
        // 非 200 は body を読んで error。200 は newStream(resp.Body)
    }
    ```
*   **Logic**: 仕様 R7。`from` なし = 先頭再生。stdin / POST body なし。

#### [MODIFY] [client/v1/stream.go](file://client/v1/stream.go)

*   **Description**: SSE `id:` と `LastEventID()`。
*   **Technical Design**:
    ```go
    type Event struct {
        // 既存 Type, Text, ...
        ID string
    }
    type Stream struct {
        // 既存
        lastEventID string
        pendingID   string // 直近の id: 行。論理イベント emit 時に採用
    }
    func (s *Stream) LastEventID() string { return s.lastEventID }
    ```
    `events()` の scanner ループ:
    - `id: ` プレフィックス → `pendingID = strings.TrimSpace(...)`。continue。
    - `data: ` は現行。`EventToolResultPart` は assembler に積むだけで lastEventID を変えない。
    - 非 part を emit するとき `ev.ID = pendingID`。`pendingID != ""` なら `s.lastEventID = pendingID`。turn context（id なし）では last を進めない。
    - チャンク完了の `EventToolResult` emit 時も同様に `lastEventID = pendingID`（同一論理 id）。
*   **Logic**: 仕様 R7。呼び出し側は切断後 `FollowFrom(stream.LastEventID())`。

### 統合テスト（E2E 相当、`tests/`）

#### [NEW] [tests/common_session_follow_test.go](file://tests/common_session_follow_test.go)

*   **Description**: Failed First。httptest + fake agent。プレフィックス `TestSessionFollow`。実 CLI なし。`t.Skip` 禁止。
*   **Technical Design**:
    - fake: 遅延付きで `EventText` →（任意で tool）→ `EventResult`。Send 回数カウンタ。Close カウンタ。
    - ヘルパ: `POST /messages` SSE を途中 cancel、`GET /events?from=`、行スキャンで `id:` と `data:`。
    - `WithSSEDrainTimeout` はシナリオ D だけ短く（例 80ms）。A/C/E は fake 遅延 < 90s かつテスト内で即 Follow。
    - シナリオ A `TestSessionFollow_ReattachContinuesTurn`: 先頭 text の後切断 → `from=<その id>` → 残り + Result + `[DONE]`。その間 POST messages と PATCH が 409、hint に `follow`。Close が増えない。
    - シナリオ B `TestSessionFollow_ReplayFromStart`: 切断後 `GET /events`（from なし）が先頭から再送。taskLog 件数がイベント数の 2 倍にならない（サーバに TaskLog を載せて Entries 長を比較）。
    - シナリオ C `TestSessionFollow_StealsExistingSubscriber`: 第 1 SSE 生存中に第 2 GET events。第 1 は追加イベントなし or 接続終了。第 2 が Result。Close しない。
    - シナリオ D `TestSessionFollow_TimeoutThenNoActiveTurn`: 切断、短い猶予、Follow 409 `no active turn`、その後 POST messages が 409 以外。
    - シナリオ E `TestSessionFollow_DoesNotEnqueueMessage`: Follow 前後で Send 回数 1、セッション履歴ファイルに user が増えない。
    - シナリオ F `TestSessionFollow_SuspendedThenRespond`: fake が `EventUserInputRequired`。切断後 Follow で再取得。Respond で続き。active 中 Respond は 409 `session is not suspended`。
    - シナリオ G `TestSessionFollow_CompletedRejected`: Result 後 Follow 409。GET session `completed`、followable 欠ける。
    - シナリオ H `TestSessionFollow_ClientV1FollowFrom`: `v1` クライアントで SendText、1 イベント後 body Close、`FollowFrom(LastEventID())` で残り。`Follow()` のリクエストが `from` クエリ無し（httptest で観測）。
*   **Logic**: 仕様のシナリオ A–H を要約せずテスト名に 1:1 対応。Issue 手順 4–5 の busy は A に含める。

#### [MODIFY] [tests/llm_stream_reconnect_test.go](file://tests/llm_stream_reconnect_test.go) / [tests/llm_stream_reconnect_regression_test.go](file://tests/llm_stream_reconnect_regression_test.go)

*   **Description**: Issue #41 テストが所有権分離後も通ること。hint 文字列があれば更新。15s 前提のコメントがあれば 90s / `WithSSEDrainTimeout` に合わせる。
*   **Logic**: 本計画は #41 を変えない。切断非 kill は猶予中も成立。

### 文書

#### [MODIFY] [docs/ReferenceManual-WebAPIs.md](file://docs/ReferenceManual-WebAPIs.md)

*   **Description**: エンドポイント表に `GET /api/v1/sessions/:id/events`。詳細節: Accept 必須、`from` / `Last-Event-ID`、論理 `id:`、turn context に id なし、409 `no active turn`、steal、再接続猶予既定 90 秒、busy hint、`followable` / `turn_id`。切断ポリシー（猶予内 Follow、切れれば drain timeout）を `POST /messages` の SSE 節と相互参照。`/logs` は turn SSE の代替でないと明記。
*   **Logic**: 仕様 R1 / R4 / R6 の公開契約。

## Step-by-Step Implementation Guide

1. **Red: config 既定 90**: Edit `shared/libs/go/config/config_test.go` にゼロ値→90 のテストを追加し、`./scripts/process/build.sh --skip-frontend --skip-etc` で失敗を確認。Edit `config.go` で GREEN。
2. **Red: hint / followable 単体**: Edit `handler_session_test.go` / `handler_test.go`。hint と GET followable を先に失敗させる。
3. **Green: hint ヘルパと sessionAPIResponse**: Edit `exec_registry.go`（定数）、`handler.go` `writeSessionBusy`、`handler_session.go`、全 `"respond or terminate"` を置換。
4. **Red: parseFrom / 406 / 409 / id: 単体**: Add `handler_follow_test.go`。Add `handler_follow.go` のスタブ（まだ attach しない）でステータスだけ GREEN。
5. **Red: 論理 id ワイヤ**: `writeSSEWireEvents` テスト。Edit `handler.go` で `id:` 出力。
6. **Red: steal / 猶予 / 再生の単体**（exec ヘルパまたは httptest 短時間 fake）: 購読 0 でタイマー、steal でキャンセル。
7. **所有権分離**: Edit `handler_retry.go` / `exec_registry.go` / `handleSendMessage` / `handleRespond`。`streamSSERelay` の切断ドレインを `attachSSE` + `pumpExecSideEffects` + `startReattachTimer` に置換。既存 `handler_retry_test.go` の 80ms ドレインと `TestStreamSSERelay_DisconnectUpdatesStatus` を GREEN に保つ。
8. **配線**: Edit `service.go` に `/events`。Edit `server.go` で YAML 秒を `WithSSEDrainTimeout`。定数 `defaultSSEClientDrainTimeout = 90s`。
9. **Red: client/v1**: Add Follow テストと `LastEventID` テスト。Edit `session.go` / `stream.go`。`RunWithHandlers` は変更しない。
10. **Red: 統合**: Add `tests/common_session_follow_test.go` シナリオ A–H。実装が足りなければ RED → サーバ/クライアントを直して GREEN。
11. **回帰**: `./scripts/process/build.sh --skip-frontend --skip-etc` のち `TestStreamReconnect` 系が残っていることを確認（下記 Verification）。
12. **文書**: Edit `docs/ReferenceManual-WebAPIs.md`。

## Verification Plan

### Automated Verification

手動確認を主検証にしない。`t.Skip` 禁止。Linux / Remote-SSH（Linux）では `build.sh` に `--skip-etc`、`integration_test.sh` は `xvfb-run -a` でラップし `--headed` / `--ui` を付けない。

1. **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh --skip-frontend --skip-etc
    ```
    Linux / Remote-SSH（Linux）も同じ（`--skip-etc` 必須）。macOS はプロジェクト既定どおり `./scripts/process/build.sh` でも可。
2. **Integration Tests（本機能、必須）**:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories common --specify "TestSessionFollow"
    ```
    Linux / Remote-SSH（Linux）:
    ```bash
    ./scripts/process/build.sh --skip-etc && xvfb-run -a ./scripts/process/integration_test.sh --categories common --specify "TestSessionFollow"
    ```
3. **E2E Tests**: GUI なし。バックエンド契約の E2E は `tests/common_session_follow_test.go`（手順 2 と同じフィルタ）。追加の VSCode E2E は作らない。
4. **Issue #41 回帰**:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories common --specify "TestStreamReconnect"
    ```
    Linux / Remote-SSH（Linux）:
    ```bash
    ./scripts/process/build.sh --skip-etc && xvfb-run -a ./scripts/process/integration_test.sh --categories common --specify "TestStreamReconnect"
    ```
    `TestStreamReconnectLive` は巻き込まない（名前プレフィックス）。
5. **任意 LIVE**（本計画の必須ゲートではない）: `TestSessionFollowLive` は作成しない。必要なら後続計画。

検証すること（仕様シナリオ）:

- `TestSessionFollow_ReattachContinuesTurn`: A + Issue busy/hint
- `TestSessionFollow_ReplayFromStart`: B（二重副作用なし）
- `TestSessionFollow_StealsExistingSubscriber`: C
- `TestSessionFollow_TimeoutThenNoActiveTurn`: D
- `TestSessionFollow_DoesNotEnqueueMessage`: E
- `TestSessionFollow_SuspendedThenRespond`: F
- `TestSessionFollow_CompletedRejected`: G
- `TestSessionFollow_ClientV1FollowFrom`: H

## Documentation

- [docs/ReferenceManual-WebAPIs.md](file://docs/ReferenceManual-WebAPIs.md): `/events`、hint、90s 猶予、followable、steal、`from` / `id:`、`/logs` との違い。
- README にセッション API 一覧があれば 1 行追加。無ければマニュアルのみ。
- `prompts/` 以外の英語ドキュメントに日本語を入れない。
