# 006-Wayfinder-Realtime-Streaming-and-Context-Separation

> **Source Specification**: [006-Wayfinder-Realtime-Streaming-and-Context-Separation.md](file:///prompts/phases/000-foundation/branches/feat-wayfinder-agent/ideas/006-Wayfinder-Realtime-Streaming-and-Context-Separation.md)

## Goal Description

Wayfinder agentの `Send()` メソッドをバッチ型からリアルタイムストリーミング型にリファクタリングし、WBSオーケストレーション中の進捗をクライアントにリアルタイム配信する。同時に、HTTPリクエストcontextからエージェント実行contextを分離し、クライアント切断時もエージェントが処理を継続できるようにする。SSE heartbeat、ternctlのUI改善、クライアントライブラリの新イベント型対応も含む。

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: EventEmitter機構の導入 | Phase 1: `wayfinder/emitter.go`, `wayfinder/agent_core.go` |
| R1-1: ツール実行時にイベント発行 | Phase 2: `wayfinder/agent_core.go` runSimple |
| R1-2: LLMテキスト応答時にイベント発行 | Phase 2: `wayfinder/agent_core.go` runSimple |
| R1-3: WBSノード開始/完了/失敗時にイベント発行 | Phase 2: `wayfinder/planning/wbs_orchestrator.go` |
| R1-4: Simple実行モードでも同じイベント発行 | Phase 2: `wayfinder/agent_core.go` runSimple |
| R2: 新イベント型追加 | Phase 1: `codingagent/event.go` |
| R3: Context分離 | Phase 3: `agentservice/handler.go`, `agentservice/service.go` |
| R3-1: HTTPリクエストcontextから分離 | Phase 3: `agentservice/handler.go` handleSendMessage |
| R3-2: クライアント切断時もエージェント継続 | Phase 3: `agentservice/handler.go` streamSSE |
| R3-3: terminate APIで明示停止 | Phase 3: `agentservice/handler.go` handleTerminate |
| R3-4: 分離contextのキャンセル手段 | Phase 3: `agentservice/service.go` executionCancels map |
| R4: adapter.go Send() リファクタリング | Phase 2: `wayfinder/adapter.go` |
| R4-1: EventEmitter経由でイベント送信 | Phase 2: `wayfinder/adapter.go` Send |
| R4-2: Run完了時にEventResult送信 | Phase 2: `wayfinder/adapter.go` Send |
| R4-3: Runエラー時にEventError送信 | Phase 2: `wayfinder/adapter.go` Send |
| R5: SSE Heartbeat | Phase 3: `agentservice/handler.go` streamSSE |
| R5-1: 15秒ごとにkeepalive | Phase 3: `agentservice/handler.go` |
| R5-2: クライアントはコメント行無視 | 既存動作 (data: プレフィックスチェック) |
| R6: ternctl側のUI改善 | Phase 4: `client/stream.go`, `features/ternctl/main.go` |
| R6-1: ツール/ノード進捗表示 | Phase 4: `client/stream.go` Output |
| R6-2: WBS進捗表示 | Phase 4: `client/stream.go` Output |
| R7: client stream.go新イベント型対応 | Phase 4: `client/stream.go` |
| R7-1: Event構造体とパーサーに新イベント型追加 | Phase 4: `client/stream.go` |

## Proposed Changes

### codingagent (共通イベント型)

#### [MODIFY] [event.go](file:///shared/libs/go/codingagent/event.go)
* **Description**: 新しいイベント型を追加
* **Technical Design**:
    ```go
    const (
        // 既存のイベント型はそのまま維持
        EventText       EventType = "text"
        EventToolUse    EventType = "tool_use"
        EventToolResult EventType = "tool_result"
        EventResult     EventType = "result"
        EventError      EventType = "error"
        EventSystem     EventType = "system"
        // 新規追加
        EventNodeStart    EventType = "node_start"
        EventNodeComplete EventType = "node_complete"
        EventNodeFailed   EventType = "node_failed"
        EventProgress     EventType = "progress"
    )
    ```
* **Logic**: 既存の6つのイベント型に4つを追加。`StreamEvent` 構造体は変更不要 (既存の `Content` フィールドでノード名/進捗情報を運搬)。

---

### wayfinder (EventEmitter + AgentCore + Adapter)

#### [NEW] [emitter.go](file:///shared/libs/go/wayfinder/emitter.go)
* **Description**: AgentCoreからイベントを外部に送信するためのチャネルラッパー
* **Technical Design**:
    ```go
    package wayfinder

    import "github.com/axsh/arctic-tern/codingagent"

    // EventEmitter sends streaming events from AgentCore to the adapter channel.
    // If ch is nil or emitter is nil, Emit is a no-op.
    type EventEmitter struct {
        ch chan<- codingagent.StreamEvent
    }

    // NewEventEmitter creates an EventEmitter wrapping the given channel.
    func NewEventEmitter(ch chan<- codingagent.StreamEvent) *EventEmitter {
        return &EventEmitter{ch: ch}
    }

    // Emit sends a single event. Safe to call on nil receiver.
    func (e *EventEmitter) Emit(ev codingagent.StreamEvent) {
        if e == nil || e.ch == nil {
            return
        }
        e.ch <- ev
    }
    ```

#### [NEW] [emitter_test.go](file:///shared/libs/go/wayfinder/emitter_test.go)
* **Description**: EventEmitter のユニットテスト
* **テストケース**:
    * `TestEventEmitter_Emit`: チャネルに送信されることを確認
    * `TestEventEmitter_NilSafe`: nil receiver でpanicしないことを確認
    * `TestEventEmitter_NilChannel`: nil channel でpanicしないことを確認

#### [MODIFY] [agent_core.go](file:///shared/libs/go/wayfinder/agent_core.go)
* **Description**: `emitter` フィールド追加、`runSimple` / `runWithWBSTree` でイベント発行
* **Technical Design**:
    ```go
    type AgentCore struct {
        // 既存フィールド...
        emitter *EventEmitter // ストリーミングイベント送信用 (nil = no-op)
    }

    // SetEmitter configures the event emitter for streaming.
    func (ac *AgentCore) SetEmitter(emitter *EventEmitter) {
        ac.emitter = emitter
    }
    ```
* **Logic (runSimple 変更点)**:
    * ツール呼び出し前: `ac.emitter.Emit(StreamEvent{Type: EventToolUse, Content: tc.Name, ToolName: tc.Name, ToolInput: tc.Input})`
    * ツール結果後: `ac.emitter.Emit(StreamEvent{Type: EventToolResult, Content: result})`
    * LLMテキスト応答時 (ToolCalls==0): `ac.emitter.Emit(StreamEvent{Type: EventText, Content: resp.Content})`
* **Logic (runWithWBSTree 変更点)**:
    * `WBSOrchestrator` に `emitter` を渡す (新パラメータ)

#### [MODIFY] [adapter.go](file:///shared/libs/go/wayfinder/adapter.go)
* **Description**: `Send()` をリアルタイムストリーミング型にリファクタリング
* **Technical Design**:
    ```go
    func (s *wayfinderSession) Send(ctx context.Context, message string) (<-chan codingagent.StreamEvent, error) {
        s.mu.Lock()
        defer s.mu.Unlock()

        ch := make(chan codingagent.StreamEvent, 64)

        // EventEmitter をAgentCoreに注入
        emitter := NewEventEmitter(ch)
        s.core.SetEmitter(emitter)

        prompt := message
        if prompt == "" {
            prompt = s.prompt
        }

        go func() {
            defer close(ch)
            defer s.core.SetEmitter(nil) // クリーンアップ

            // Send initial system event.
            ch <- codingagent.StreamEvent{
                Type:      codingagent.EventSystem,
                SessionID: s.id,
            }

            _, err := s.core.Run(ctx, prompt)
            if err != nil {
                ch <- codingagent.StreamEvent{
                    Type:  codingagent.EventError,
                    Error: err,
                }
                return
            }

            // Send completion event.
            ch <- codingagent.StreamEvent{
                Type: codingagent.EventResult,
            }
        }()

        return ch, nil
    }
    ```
* **Logic**: `core.Run()` 中に `emitter` 経由でツール実行/テキスト応答/ノード進捗のイベントがリアルタイムに `ch` に送信される。`Run()` の戻り値 `result` は既にEmitterで送信済みのため、完了時は `EventResult` のみ送信。

---

### wayfinder/planning (WBSOrchestrator イベント発行)

#### [MODIFY] [wbs_orchestrator.go](file:///shared/libs/go/wayfinder/planning/wbs_orchestrator.go)
* **Description**: `WBSOrchestrator` にイベント発行機能を追加
* **Technical Design**:
    ```go
    // EventEmitFunc is a callback for streaming events.
    // Using a function type avoids importing the wayfinder package (cyclic import prevention).
    type EventEmitFunc func(eventType string, content string)

    type WBSOrchestrator struct {
        executor  NodeExecutor
        persister StatePersister
        emitEvent EventEmitFunc // nil = no-op
        logger    logger.Logger
    }

    func NewWBSOrchestrator(
        executor NodeExecutor,
        persister StatePersister,
        log logger.Logger,
        opts ...OrchestratorOption,
    ) *WBSOrchestrator {
        o := &WBSOrchestrator{...}
        for _, opt := range opts {
            opt(o)
        }
        return o
    }

    type OrchestratorOption func(*WBSOrchestrator)

    func WithEventEmitter(fn EventEmitFunc) OrchestratorOption {
        return func(o *WBSOrchestrator) { o.emitEvent = fn }
    }
    ```
* **Logic (Execute 変更点)**:
    ```go
    // ノード実行前:
    o.emit("node_start", fmt.Sprintf("%s: %s", node.ID, node.Name))

    result, err := o.executor.ExecuteNode(ctx, node)

    if err != nil {
        o.emit("node_failed", fmt.Sprintf("%s: %s - %v", node.ID, node.Name, err))
    } else {
        o.emit("node_complete", fmt.Sprintf("%s: %s", node.ID, node.Name))
    }

    // 全ノード数と完了数の進捗:
    completed, total := tree.Progress()
    o.emit("progress", fmt.Sprintf("%d/%d", completed, total))
    ```
* **WBSTree への追加**:
    ```go
    // Progress returns (completed, total) node counts.
    func (t *WBSTree) Progress() (int, int) {
        completed, total := 0, 0
        t.walkNodes(func(node *WBSNode) {
            // ルートノードのみカウント (サブステップは除外)
            total++
            if node.Status == StatusCompleted {
                completed++
            }
        })
        return completed, total
    }
    ```

#### [MODIFY] [wbs_orchestrator_test.go](file:///shared/libs/go/wayfinder/planning/wbs_orchestrator_test.go)
* **Description**: イベント発行テストの追加
* **テストケース**:
    * `TestWBSOrchestrator_EmitsNodeEvents`: ノード実行時にnode_start, node_complete, progressイベントが発行されることを確認
    * `TestWBSOrchestrator_EmitsNodeFailedEvent`: ノード失敗時にnode_failedイベントが発行されることを確認
    * `TestWBSOrchestrator_NoEmitterNoPanic`: emitter未設定でもpanicしないことを確認

---

### wayfinder/planning (WBSTree.Progress)

#### [MODIFY] [wbs_tree.go](file:///shared/libs/go/wayfinder/planning/wbs_tree.go)
* **Description**: `Progress()` メソッドの追加
* **Technical Design**:
    ```go
    // Progress returns (completed, total) counts for root-level nodes.
    func (t *WBSTree) Progress() (int, int) {
        completed, total := 0, 0
        for _, node := range t.RootNodes {
            total++
            if node.Status == StatusCompleted {
                completed++
            }
        }
        return completed, total
    }
    ```

#### [MODIFY] [wbs_tree_test.go](file:///shared/libs/go/wayfinder/planning/wbs_tree_test.go)
* **Description**: `Progress` テストの追加
* **テストケース**:
    * `TestWBSTree_Progress`: 各ステータスでの完了数/全体数を確認

---

### agentservice (Context分離 + SSE Heartbeat)

#### [MODIFY] [service.go](file:///shared/libs/go/agentservice/service.go)
* **Description**: 実行contextのキャンセル管理マップを追加
* **Technical Design**:
    ```go
    type Server struct {
        // 既存フィールド...
        execCancelMu sync.Mutex
        execCancels  map[string]context.CancelFunc // sessionID -> cancel
    }
    ```
    `New()` と `NewWithStore()` で `execCancels` を初期化:
    ```go
    execCancels: make(map[string]context.CancelFunc),
    ```
    メソッド追加:
    ```go
    func (s *Server) RegisterExecCancel(id string, cancel context.CancelFunc) {
        s.execCancelMu.Lock()
        defer s.execCancelMu.Unlock()
        s.execCancels[id] = cancel
    }

    func (s *Server) UnregisterExecCancel(id string) {
        s.execCancelMu.Lock()
        defer s.execCancelMu.Unlock()
        delete(s.execCancels, id)
    }

    func (s *Server) CancelExecution(id string) bool {
        s.execCancelMu.Lock()
        defer s.execCancelMu.Unlock()
        if cancel, ok := s.execCancels[id]; ok {
            cancel()
            return true
        }
        return false
    }
    ```
    `Shutdown()` に追加: 全 `execCancels` のキャンセルを呼ぶ。

#### [MODIFY] [handler.go](file:///shared/libs/go/agentservice/handler.go)
* **Description**: `handleSendMessage` のcontext分離、`streamSSE` のheartbeat追加、`handleTerminate` のexecCancel連携
* **Technical Design (handleSendMessage)**:
    ```go
    func (s *Server) handleSendMessage(w http.ResponseWriter, r *http.Request) {
        // ... (既存のセッション/リクエスト解析)

        // Context分離: エージェント実行用のcontextをHTTPリクエストから独立させる
        execCtx, execCancel := context.WithCancel(context.Background())
        s.RegisterExecCancel(sessionID, execCancel)
        defer func() {
            s.UnregisterExecCancel(sessionID)
            // 注意: execCancel は呼ばない (エージェントは continue)
            // ただし handleTerminate で明示的にキャンセルされる
        }()

        session, err := agent.CreateSession(execCtx, opts...)
        // ...
        ch, err := session.Send(execCtx, req.Message)
        // ...
        if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
            s.streamSSE(r.Context(), execCtx, w, ch, sessionID)
        } else {
            s.respondJSON(r.Context(), execCtx, w, ch, sessionID)
        }
    }
    ```
* **Technical Design (streamSSE)**:
    ```go
    func (s *Server) streamSSE(
        clientCtx context.Context,
        execCtx context.Context,
        w http.ResponseWriter,
        ch <-chan codingagent.StreamEvent,
        sessionID string,
    ) {
        // ...
        ticker := time.NewTicker(15 * time.Second)
        defer ticker.Stop()

        for {
            select {
            case <-clientCtx.Done():
                // クライアント切断 → SSEストリーム終了
                // エージェントは execCtx で継続
                s.logger.Debug("client disconnected, SSE stream closed", "session_id", sessionID)
                return
            case <-ticker.C:
                fmt.Fprintf(w, ": keepalive\n\n")
                flusher.Flush()
            case ev, ok := <-ch:
                if !ok {
                    goto done
                }
                // ... 既存のイベント処理
            }
        }
    done:
        // ... 既存の完了処理
    }
    ```
* **Technical Design (handleTerminate)**:
    ```go
    func (s *Server) handleTerminate(w http.ResponseWriter, r *http.Request) {
        // ... (既存の処理)
        // 実行contextをキャンセル
        s.CancelExecution(sessionID)
        // ... (既存のステータス更新)
    }
    ```

#### [MODIFY] [handler_test.go](file:///shared/libs/go/agentservice/handler_test.go)
* **Description**: Context分離とHeartbeatのテスト追加
* **テストケース**:
    * `TestSSEHeartbeat`: イベントがない間に15秒ごとにkeepaliveが送信されることを確認 (タイマーをモックまたは短縮)
    * `TestContextSeparation_ClientDisconnect`: クライアントcontextキャンセル後もチャネルが開いていることを確認
    * `TestTerminate_CancelsExecution`: terminateがexecCancelを呼ぶことを確認

---

### client (新イベント型対応 + UI改善)

#### [MODIFY] [stream.go](file:///shared/libs/go/client/stream.go)
* **Description**: 新イベント型のサポート追加
* **Technical Design**:
    ```go
    const (
        // 既存の型
        EventText       EventType = "text"
        EventToolUse    EventType = "tool_use"
        EventToolResult EventType = "tool_result"
        EventSystem     EventType = "system"
        EventResult     EventType = "result"
        EventError      EventType = "error"
        // 新規追加
        EventNodeStart    EventType = "node_start"
        EventNodeComplete EventType = "node_complete"
        EventNodeFailed   EventType = "node_failed"
        EventProgress     EventType = "progress"
    )
    ```
    `Output()` メソッドに新イベント型のハンドリングを追加:
    ```go
    case EventNodeStart:
        fmt.Fprintf(w, "\n[Node Start: %s]\n", ev.Text)
    case EventNodeComplete:
        fmt.Fprintf(w, "[Node Complete: %s]\n", ev.Text)
    case EventNodeFailed:
        fmt.Fprintf(w, "[Node Failed: %s]\n", ev.Text)
    case EventProgress:
        fmt.Fprintf(w, "[WBS %s]\n", ev.Text)
    ```

#### [MODIFY] [stream_test.go](file:///shared/libs/go/client/stream_test.go)
* **Description**: 新イベント型テストの追加
* **テストケース**:
    * `TestStream_Output_NodeEvents`: node_start, node_complete, node_failed, progressイベントの出力フォーマット確認
    * `TestStream_Output_KeepAliveIgnored`: keepaliveコメント行が無視されることを確認

---

### agentservice (respondJSON のcontext分離)

#### [MODIFY] [handler.go](file:///shared/libs/go/agentservice/handler.go) (追加)
* **Description**: `respondJSON` メソッドも `streamSSE` と同様にcontext分離を適用
* **Technical Design**: `respondJSON` のシグネチャに `execCtx` を追加し、`clientCtx.Done()` でクライアント切断を検知しつつ、エージェントは `execCtx` で継続。

## Step-by-Step Implementation Guide

### Phase 1: 基盤 (イベント型 + EventEmitter)

- [x] Step 1: `codingagent/event.go` に新イベント型4つ (`EventNodeStart`, `EventNodeComplete`, `EventNodeFailed`, `EventProgress`) を追加
- [x] Step 2: `wayfinder/emitter_test.go` を作成 (3テストケース: Emit, NilSafe, NilChannel)
- [x] Step 3: `wayfinder/emitter.go` を作成 (`EventEmitter` struct + `NewEventEmitter` + `Emit`)
- [x] Step 4: テスト実行して PASS を確認

### Phase 2: AgentCore + Adapter リファクタリング

- [x] Step 5: `wayfinder/agent_core.go` に `emitter *EventEmitter` フィールドと `SetEmitter` メソッドを追加
- [x] Step 6: `wayfinder/agent_core.go` の `runSimple` に emitter.Emit 呼び出しを追加 (tool_use, tool_result, text)
- [x] Step 7: `wayfinder/planning/wbs_tree.go` に `Progress()` メソッドを追加
- [x] Step 8: `wayfinder/planning/wbs_tree_test.go` に `TestWBSTree_Progress` テストを追加
- [x] Step 9: `wayfinder/planning/wbs_orchestrator.go` に `EventEmitFunc` 型、`WithEventEmitter` オプション、`emit` ヘルパーを追加。`Execute` 内でノードイベント + 進捗イベントを発行
- [x] Step 10: `wayfinder/planning/wbs_orchestrator_test.go` にイベント発行テスト3件を追加
- [x] Step 11: `wayfinder/agent_core.go` の `runWithWBSTree` で `WithEventEmitter` を使って `WBSOrchestrator` にEmitterを接続
- [x] Step 12: `wayfinder/adapter.go` の `Send()` をリファクタリング (EventEmitter注入 + Run完了/エラーイベント)
- [x] Step 13: テスト実行して PASS を確認

### Phase 3: Context分離 + SSE Heartbeat

- [x] Step 14: `agentservice/service.go` に `execCancels` マップと `RegisterExecCancel` / `UnregisterExecCancel` / `CancelExecution` メソッドを追加。`Shutdown` に execCancels キャンセルを追加
- [x] Step 15: `agentservice/handler.go` の `handleSendMessage` でcontext分離を実装 (`context.WithCancel(context.Background())`)
- [x] Step 16: `agentservice/handler.go` の `streamSSE` にheartbeat ticker (15秒) とclientCtx/execCtxの分離を実装。シグネチャに `execCtx` を追加
- [x] Step 17: `agentservice/handler.go` の `respondJSON` にも同様のcontext分離を適用
- [x] Step 18: `agentservice/handler.go` の `handleTerminate` に `CancelExecution` 呼び出しを追加
- [x] Step 19: `agentservice/handler_test.go` にテスト3件を追加 (Heartbeat, ContextSeparation, TerminateCancels)
- [x] Step 20: テスト実行して PASS を確認

### Phase 4: クライアント側 (stream.go + ternctl)

- [x] Step 21: `client/stream.go` に新イベント型の定数を追加
- [x] Step 22: `client/stream.go` の `Output()` メソッドに新イベント型のcase分岐を追加
- [x] Step 23: `client/stream_test.go` にテスト2件を追加 (NodeEvents, KeepAliveIgnored)
- [x] Step 24: テスト実行して PASS を確認

### Phase 5: ビルドと検証

- [x] Step 25: 全体ビルド + ユニットテスト
- [x] Step 26: E2Eテスト実行
- [x] Step 27: git commit + push

## Verification Plan

### Automated Verification

1. **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```

2. **Integration Tests (リグレッション確認)**:
    ```bash
    ./scripts/process/integration_test.sh --specify "Wayfinder"
    ```
    * **Log Verification**: サーバーログで以下を確認:
        * `node_start`, `node_complete` イベントがSSEストリームに出力されていること
        * `keepalive` コメント行が定期的に送信されていること
        * クライアント切断後も `context canceled` エラーが発生しないこと

3. **E2E Tests**:

    E2Eテストは既存の `wayfinder_e2e_test.go` のSSEイベント収集ロジックで新イベント型の受信を確認する。ternctlの手動テストが不要な理由: SSEレベルでのイベント送信はagentservice/handlerのE2Eテストで検証可能。ternctl固有のUI表示は `client/stream_test.go` のユニットテストでカバー。

    #### [MODIFY] [wayfinder_e2e_test.go](file:///tests/wayfinder_e2e_test.go)
    * **テストケース**: `sendWayfinderMessage` が新イベント型 (`node_start`, `node_complete`, `tool_use`, `tool_result`) を受信できることを既存テストで暗黙的に検証。Simple実行のテストではこれらのイベントが発行されることを確認。
    * **検証ポイント**: SSEストリームにEventToolUse/EventToolResultイベントが含まれていること

### Test Design Self-Review (testing-rules ss11)

1. **ボトムアップ順序**: EventEmitter (単体) -> AgentCore emitter注入 (単体) -> WBSOrchestrator イベント (単体) -> AgentService context/heartbeat (単体) -> client stream (単体) -> E2E (統合)
2. **観点チェックリスト**:
    * 正常系: イベント発行、heartbeat送信、context分離
    * 異常系: nil emitter、クライアント切断、terminate
    * 境界値: 0ノード WBS、大量イベント
3. **迂回排除**: 全テストケースが標準スクリプト経由で実行
4. **依存関係**: Phase 1 -> Phase 2 -> Phase 3 -> Phase 4 の順序依存あり

### 総合判定プロセス (testing-rules ss12)

1. 全ユニットテスト PASS
2. 全E2Eテスト PASS (リグレッションなし)
3. ビルド成功
4. git push

## Documentation

#### [MODIFY] [005-Wayfinder-Tool-Specialization-and-Subagent-Wiring.md](file:///prompts/phases/000-foundation/branches/feat-wayfinder-agent/ideas/005-Wayfinder-Tool-Specialization-and-Subagent-Wiring.md)
* **更新内容**: adapter.go の Send() が EventEmitter ベースのリアルタイムストリーミングに変更されたことを注記
