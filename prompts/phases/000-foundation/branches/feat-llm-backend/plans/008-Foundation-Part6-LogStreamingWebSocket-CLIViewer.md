# 008-Foundation-Part6-LogStreamingWebSocket-CLIViewer

> **Source Specification**: [006-LogStreamingWebSocket-and-CLIViewer.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/ideas/006-LogStreamingWebSocket-and-CLIViewer.md)

## Goal Description

`wsserver` パッケージのスケルトンを実装に置き換え、`TaskLog` に追加されたログエントリを WebSocket 経由でリアルタイム配信する。加えて、階層ログを色付きインデント表示する CLI Viewer (`examples/log-viewer`) と内蔵シミュレーターを作成し、データ構造の正しさ（`parentLogId` による階層構造、`begin/send/end` フェーズによるストリーミング）をエンドツーエンドで検証可能にする。

## User Review Required

> [!IMPORTANT]
> **WebSocket ライブラリの選択**: 仕様では `gorilla/websocket` を想定しているが、Go 標準ライブラリの `nhooyr.io/websocket` (coder/websocket) も候補。本計画では `gorilla/websocket` を使用する。変更が必要な場合は指摘してください。

> [!IMPORTANT]
> **ポートの衝突**: WebSocket Server はデフォルト `:18080` を使用する。統合テストではポート `0` (自動割当) を使用してテスト間の衝突を回避する。

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1-1: wsserver.Server 実装 | Proposed Changes > wsserver/server.go |
| R1-2: 複数クライアント (Hub) | Proposed Changes > wsserver/hub.go |
| R1-3: New/Launch/Shutdown ライフサイクル | Proposed Changes > wsserver/server.go |
| R1-4: /ws エンドポイント | Proposed Changes > wsserver/server.go (serveWS) |
| R1-5: Shutdown で全接続クローズ | Proposed Changes > wsserver/hub.go (Stop) |
| R1-6: Broadcast メソッド | Proposed Changes > wsserver/hub.go |
| R1-7: クライアント切断時の除去 | Proposed Changes > wsserver/client.go (readPump) |
| R2-1: JSON エンベロープ (type: "log") | Proposed Changes > wsserver/message.go |
| R2-2: type フィールドの拡張性 | Proposed Changes > wsserver/message.go |
| R2-3: payload.entry のJSON直列化 | Proposed Changes > wsserver/message.go |
| R2-4: snapshot メッセージ | Proposed Changes > wsserver/hub.go (sendSnapshot) |
| R3-1: TaskLog.SetOnEntry callback | Proposed Changes > wsserver/server.go (wireTaskLog) |
| R3-2: hag.Server ワイヤリング | Proposed Changes > hag/server.go |
| R3-3: TaskLog() アクセサ | Proposed Changes > hag/server.go |
| R4-1: CLI Viewer 作成 | Proposed Changes > examples/log-viewer/main.go |
| R4-2: parentLogId インデント表示 | Proposed Changes > examples/log-viewer/main.go (displayEntry) |
| R4-3: kind 色分け | Proposed Changes > examples/log-viewer/main.go (colorForKind) |
| R4-4: --url フラグ | Proposed Changes > examples/log-viewer/main.go |
| R4-5: snapshot 一括表示 | Proposed Changes > examples/log-viewer/main.go |
| R4-6: send フェーズの追記表示 | Proposed Changes > examples/log-viewer/main.go |
| R5-1: シミュレーター | Proposed Changes > examples/log-viewer/main.go (runSimulator) |
| R5-2: --simulate フラグ | Proposed Changes > examples/log-viewer/main.go |
| R5-3: 階層ログシーケンス生成 | Proposed Changes > examples/log-viewer/main.go (runSimulator) |
| R5-4: 遅延挿入 | Proposed Changes > examples/log-viewer/main.go (runSimulator) |
| R6-1: TaskLog フィールド追加 | Proposed Changes > hag/server.go |
| R6-2: TaskLog() アクセサ | Proposed Changes > hag/server.go |
| R6-3: wsserver コンストラクタに TaskLog/Logger 注入 | Proposed Changes > wsserver/server.go |
| R6-4: WebSocketConfig 追加 | Proposed Changes > config/config.go |
| R6-5: Launch 失敗時エラー | Proposed Changes > hag/server.go (既存実装で対応済み) |

---

## Proposed Changes

### config パッケージ

#### [MODIFY] [config.go](file://shared/libs/go/config/config.go)

*   **Description**: `AppConfig` に `WebSocket` 設定セクションを追加する
*   **Technical Design**:

```go
// AppConfig is the root configuration for HAG.
type AppConfig struct {
    LLMGateway LLMGatewayConfig `yaml:"llm_gateway"`
    Vault      VaultConfig      `yaml:"vault"`
    Log        LogConfig        `yaml:"log"`
    WebSocket  WebSocketConfig  `yaml:"websocket"` // NEW
}

// WebSocketConfig holds WebSocket server settings.
type WebSocketConfig struct {
    // Port is the WebSocket server listen port. Default: 18080.
    Port int `yaml:"port"`
}
```

*   **Logic**: `WebSocketConfig.Port` が `0` の場合、`wsserver.Server` は OS によるポート自動割り当てを使用する。

---

### wsserver パッケージ

#### [NEW] [message.go](file://shared/libs/go/wsserver/message.go)

*   **Description**: WebSocket メッセージの型定義
*   **Technical Design**:

```go
package wsserver

import (
    "encoding/json"
    "github.com/axsh/hag/tasklog"
)

// Message is the WebSocket message envelope.
type Message struct {
    Type    string          `json:"type"`    // "log", "snapshot"
    Payload json.RawMessage `json:"payload"` // type-specific payload
}

// LogPayload carries a single log entry.
type LogPayload struct {
    Entry *tasklog.AgentLogEntry `json:"entry"`
}

// SnapshotPayload carries the full log history for new clients.
type SnapshotPayload struct {
    Entries []tasklog.Entry `json:"entries"`
}

// NewLogMessage creates a "log" type message from an AgentLogEntry.
func NewLogMessage(entry *tasklog.AgentLogEntry) ([]byte, error) {
    payload, err := json.Marshal(LogPayload{Entry: entry})
    if err != nil {
        return nil, err
    }
    msg := Message{Type: "log", Payload: payload}
    return json.Marshal(msg)
}

// NewSnapshotMessage creates a "snapshot" type message from log history.
func NewSnapshotMessage(entries []tasklog.Entry) ([]byte, error) {
    payload, err := json.Marshal(SnapshotPayload{Entries: entries})
    if err != nil {
        return nil, err
    }
    msg := Message{Type: "snapshot", Payload: payload}
    return json.Marshal(msg)
}
```

*   **Logic**:
    *   `SnapshotPayload.Entries` は `[]tasklog.Entry` (interface) だが、JSON 直列化時には各エントリの具象型 (`AgentLogEntry`, `TerminatedEntry` 等) がそのまま Marshal される。
    *   `Message.Payload` は `json.RawMessage` で遅延デコードを可能にし、クライアント側で `type` に基づいて適切な型にデコードできるようにする。

#### [NEW] [message_test.go](file://shared/libs/go/wsserver/message_test.go)

*   **Description**: メッセージ型の直列化/逆直列化テスト
*   **Technical Design**:

```go
package wsserver

import (
    "encoding/json"
    "testing"
    "github.com/axsh/hag/tasklog"
)

func TestNewLogMessage(t *testing.T) {
    entry := tasklog.NewAgentLogEntry("agent-1",
        tasklog.WithKind("thinking"),
        tasklog.WithParentLogID("root-uuid"),
    )
    data, err := NewLogMessage(entry)
    if err != nil {
        t.Fatalf("NewLogMessage error: %v", err)
    }

    var msg Message
    if err := json.Unmarshal(data, &msg); err != nil {
        t.Fatalf("Unmarshal error: %v", err)
    }
    if msg.Type != "log" {
        t.Errorf("Type = %q, want %q", msg.Type, "log")
    }

    var payload LogPayload
    if err := json.Unmarshal(msg.Payload, &payload); err != nil {
        t.Fatalf("payload unmarshal: %v", err)
    }
    if payload.Entry.Kind != "thinking" {
        t.Errorf("Kind = %q, want %q", payload.Entry.Kind, "thinking")
    }
    if payload.Entry.ParentLogID != "root-uuid" {
        t.Errorf("ParentLogID = %q, want %q", payload.Entry.ParentLogID, "root-uuid")
    }
    if payload.Entry.Phase != "begin" {
        t.Errorf("Phase = %q, want %q", payload.Entry.Phase, "begin")
    }
}

func TestNewSnapshotMessage(t *testing.T) {
    entries := []tasklog.Entry{
        tasklog.NewAgentLogEntry("agent-1", tasklog.WithKind("text")),
        tasklog.NewAgentLogEntry("agent-1", tasklog.WithKind("thinking")),
    }
    data, err := NewSnapshotMessage(entries)
    if err != nil {
        t.Fatalf("NewSnapshotMessage error: %v", err)
    }

    var msg Message
    if err := json.Unmarshal(data, &msg); err != nil {
        t.Fatalf("Unmarshal error: %v", err)
    }
    if msg.Type != "snapshot" {
        t.Errorf("Type = %q, want %q", msg.Type, "snapshot")
    }
}
```

#### [NEW] [client.go](file://shared/libs/go/wsserver/client.go)

*   **Description**: WebSocket クライアント接続管理
*   **Technical Design**:

```go
package wsserver

import (
    "github.com/gorilla/websocket"
)

const (
    // sendBufSize is the buffer size for the client send channel.
    sendBufSize = 256
)

// Client represents a single WebSocket connection.
type Client struct {
    hub  *Hub
    conn *websocket.Conn
    send chan []byte
}

// writePump sends messages from hub to the WebSocket connection.
// Runs as a goroutine per client.
func (c *Client) writePump() {
    defer func() {
        c.conn.Close()
    }()
    for msg := range c.send {
        if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
            return
        }
    }
}

// readPump reads from the WebSocket and detects client disconnect.
// Runs as a goroutine per client.
func (c *Client) readPump() {
    defer func() {
        c.hub.unregister <- c
        c.conn.Close()
    }()
    for {
        _, _, err := c.conn.ReadMessage()
        if err != nil {
            return // client disconnected
        }
        // Currently no client-to-server messages are processed
    }
}
```

*   **Logic**:
    *   `writePump`: `c.send` チャンネルからメッセージを読み、WebSocket に書き込む。チャンネルが閉じられたら接続をクローズする。
    *   `readPump`: クライアントからのメッセージを読み続ける。エラー (切断) 時に `hub.unregister` へ自身を送信してHubから除去される。

#### [NEW] [hub.go](file://shared/libs/go/wsserver/hub.go)

*   **Description**: Hub パターンによるクライアント管理とブロードキャスト
*   **Technical Design**:

```go
package wsserver

import (
    "github.com/axsh/hag/logger"
    "github.com/axsh/hag/tasklog"
)

// Hub manages connected WebSocket clients and message broadcasting.
type Hub struct {
    clients    map[*Client]bool
    broadcast  chan []byte
    register   chan *Client
    unregister chan *Client
    taskLog    *tasklog.TaskLog
    logger     logger.Logger
    stop       chan struct{}
}

func newHub(tl *tasklog.TaskLog, log logger.Logger) *Hub {
    return &Hub{
        clients:    make(map[*Client]bool),
        broadcast:  make(chan []byte, 256),
        register:   make(chan *Client),
        unregister: make(chan *Client),
        taskLog:    tl,
        logger:     log,
        stop:       make(chan struct{}),
    }
}

// run is the Hub's main event loop. Runs as a goroutine.
func (h *Hub) run() {
    for {
        select {
        case client := <-h.register:
            h.clients[client] = true
            if h.logger != nil {
                h.logger.Info("client connected", "total", len(h.clients))
            }
            // Send snapshot of existing log entries
            h.sendSnapshot(client)

        case client := <-h.unregister:
            if _, ok := h.clients[client]; ok {
                delete(h.clients, client)
                close(client.send)
                if h.logger != nil {
                    h.logger.Info("client disconnected", "total", len(h.clients))
                }
            }

        case msg := <-h.broadcast:
            for client := range h.clients {
                select {
                case client.send <- msg:
                default:
                    // Client send buffer full; disconnect
                    close(client.send)
                    delete(h.clients, client)
                }
            }

        case <-h.stop:
            // Close all client connections
            for client := range h.clients {
                close(client.send)
                delete(h.clients, client)
            }
            return
        }
    }
}

// sendSnapshot sends the current TaskLog history to a newly connected client.
func (h *Hub) sendSnapshot(client *Client) {
    if h.taskLog == nil {
        return
    }
    entries := h.taskLog.Entries()
    data, err := NewSnapshotMessage(entries)
    if err != nil {
        if h.logger != nil {
            h.logger.Error("snapshot marshal error", "error", err)
        }
        return
    }
    select {
    case client.send <- data:
    default:
        // Buffer full; drop snapshot
    }
}
```

*   **Logic**:
    *   `register`: 新しいクライアントを `clients` map に追加し、`sendSnapshot` で既存ログ履歴を送信。
    *   `unregister`: クライアントを map から除去し、`send` チャンネルを close して `writePump` の goroutine を終了させる。
    *   `broadcast`: 全クライアントの `send` チャンネルにメッセージを送信。バッファフルのクライアントは切断する。
    *   `stop`: 全クライアントを切断し、goroutine を終了する。

#### [MODIFY] [server.go](file://shared/libs/go/wsserver/server.go)

*   **Description**: スケルトンを実際のWebSocket Serverに置き換える
*   **Technical Design**:

```go
package wsserver

import (
    "context"
    "errors"
    "fmt"
    "net"
    "net/http"

    "github.com/gorilla/websocket"
    "github.com/axsh/hag/logger"
    "github.com/axsh/hag/tasklog"
)

var upgrader = websocket.Upgrader{
    CheckOrigin: func(r *http.Request) bool { return true }, // Allow all origins
}

// Server is the WebSocket server for log streaming.
type Server struct {
    port       int
    hub        *Hub
    httpServer *http.Server
    ln         net.Listener
    logger     logger.Logger
    taskLog    *tasklog.TaskLog
}

// New creates a new WebSocket Server.
// port=0 uses OS-assigned ephemeral port.
// taskLog and log may be nil.
func New(port int, tl *tasklog.TaskLog, log logger.Logger) *Server {
    if log == nil {
        log = logger.NewDefault(logger.LevelInfo)
    }
    return &Server{
        port:    port,
        taskLog: tl,
        logger:  log.WithComponent("wsserver"),
    }
}

// Launch starts the WebSocket server. Non-blocking.
func (s *Server) Launch(ctx context.Context) error {
    s.hub = newHub(s.taskLog, s.logger)
    go s.hub.run()

    // Wire TaskLog -> Hub broadcast
    s.wireTaskLog()

    mux := http.NewServeMux()
    mux.HandleFunc("/ws", s.serveWS)

    ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", s.port))
    if err != nil {
        return fmt.Errorf("wsserver listen: %w", err)
    }
    s.ln = ln
    s.port = ln.Addr().(*net.TCPAddr).Port

    s.httpServer = &http.Server{Handler: mux}

    go func() {
        if err := s.httpServer.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
            s.logger.Error("wsserver serve error", "error", err)
        }
    }()

    s.logger.Info("websocket server started", "port", s.port)
    return nil
}

// Shutdown gracefully stops the WebSocket server.
func (s *Server) Shutdown(ctx context.Context) error {
    s.logger.Info("shutting down websocket server")

    // Stop the hub (closes all client connections)
    if s.hub != nil {
        close(s.hub.stop)
    }

    // Shutdown HTTP server
    if s.httpServer != nil {
        if err := s.httpServer.Shutdown(ctx); err != nil {
            return fmt.Errorf("wsserver shutdown: %w", err)
        }
    }
    return nil
}

// URL returns the WebSocket server URL.
func (s *Server) URL() string {
    if s.port == 0 {
        return ""
    }
    return fmt.Sprintf("ws://127.0.0.1:%d/ws", s.port)
}

// serveWS handles WebSocket upgrade requests on /ws.
func (s *Server) serveWS(w http.ResponseWriter, r *http.Request) {
    conn, err := upgrader.Upgrade(w, r, nil)
    if err != nil {
        s.logger.Error("websocket upgrade error", "error", err)
        return
    }

    client := &Client{
        hub:  s.hub,
        conn: conn,
        send: make(chan []byte, sendBufSize),
    }
    s.hub.register <- client

    go client.writePump()
    go client.readPump()
}

// wireTaskLog connects the TaskLog's onEntry callback to broadcast.
func (s *Server) wireTaskLog() {
    if s.taskLog == nil {
        return
    }
    s.taskLog.SetOnEntry(func(entry tasklog.Entry) {
        agentLog, ok := entry.(*tasklog.AgentLogEntry)
        if !ok {
            return // Only broadcast AgentLogEntry for now
        }
        data, err := NewLogMessage(agentLog)
        if err != nil {
            s.logger.Error("log message marshal error", "error", err)
            return
        }
        s.hub.broadcast <- data
    })
}
```

*   **Logic**:
    *   `New()`: `wsserver.New()` のシグネチャを `New(port int, tl *tasklog.TaskLog, log logger.Logger) *Server` に変更。既存の `New() *Server` (引数なし) からの破壊的変更。
    *   `wireTaskLog()`: `TaskLog.SetOnEntry` コールバックで `AgentLogEntry` を JSON に変換し、Hub の `broadcast` チャンネルに送信。`TerminatedEntry` 等は直接ブロードキャストしない (TaskLog 側で自動クローズされた `AgentLogEntry` が後続の `SetOnEntry` で通知される)。

#### [NEW] [server_test.go](file://shared/libs/go/wsserver/server_test.go)

*   **Description**: WebSocket Server の単体テスト
*   **Technical Design**:

```go
package wsserver

import (
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "time"

    "github.com/gorilla/websocket"
    "github.com/axsh/hag/logger"
    "github.com/axsh/hag/tasklog"
)

// TestHub_RegisterUnregister: Hub に Client を register/unregister し、
//   clients map が正しく更新されることを確認する。
func TestHub_RegisterUnregister(t *testing.T) { ... }

// TestHub_Broadcast: 2つの Client に同時にメッセージが配信されることを確認する。
func TestHub_Broadcast(t *testing.T) { ... }

// TestServer_LaunchShutdown: Server の Launch と Shutdown が正常に動作し、
//   /ws エンドポイントが利用可能/不可になることを確認する。
func TestServer_LaunchShutdown(t *testing.T) { ... }

// TestServer_WebSocketConnection: /ws に WebSocket 接続し、
//   snapshot メッセージを受信できることを確認する。
func TestServer_WebSocketConnection(t *testing.T) { ... }

// TestServer_LogBroadcast: TaskLog.Add() でエントリを追加し、
//   WebSocket クライアントが log メッセージを受信することを確認する。
func TestServer_LogBroadcast(t *testing.T) {
    log := logger.NewDefault(logger.LevelDebug)
    tl := tasklog.New()
    srv := New(0, tl, log)

    if err := srv.Launch(t.Context()); err != nil {
        t.Fatalf("Launch: %v", err)
    }
    defer srv.Shutdown(t.Context())

    // Connect WebSocket client
    wsURL := strings.Replace(srv.URL(), "ws://", "ws://", 1)
    conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
    if err != nil {
        t.Fatalf("Dial: %v", err)
    }
    defer conn.Close()

    // Read snapshot (should be empty)
    conn.SetReadDeadline(time.Now().Add(2 * time.Second))
    _, data, err := conn.ReadMessage()
    if err != nil {
        t.Fatalf("ReadMessage (snapshot): %v", err)
    }
    var snapMsg Message
    json.Unmarshal(data, &snapMsg)
    if snapMsg.Type != "snapshot" {
        t.Errorf("expected snapshot, got %q", snapMsg.Type)
    }

    // Add an entry to TaskLog
    entry := tasklog.NewAgentLogEntry("agent-1",
        tasklog.WithKind("thinking"),
        tasklog.WithParentLogID("root-id"),
    )
    tl.Add(entry)

    // Read the broadcast log message
    conn.SetReadDeadline(time.Now().Add(2 * time.Second))
    _, data, err = conn.ReadMessage()
    if err != nil {
        t.Fatalf("ReadMessage (log): %v", err)
    }
    var logMsg Message
    json.Unmarshal(data, &logMsg)
    if logMsg.Type != "log" {
        t.Errorf("expected log, got %q", logMsg.Type)
    }
    var payload LogPayload
    json.Unmarshal(logMsg.Payload, &payload)
    if payload.Entry.Kind != "thinking" {
        t.Errorf("Kind = %q, want %q", payload.Entry.Kind, "thinking")
    }
    if payload.Entry.ParentLogID != "root-id" {
        t.Errorf("ParentLogID = %q, want %q", payload.Entry.ParentLogID, "root-id")
    }
}

// TestServer_SnapshotOnConnect: 既存ログがある状態で接続し、
//   snapshot で全ログを受信できることを確認する。
func TestServer_SnapshotOnConnect(t *testing.T) { ... }

// TestServer_MultipleClients: 2つの WebSocket クライアントが同時に接続し、
//   両方に同一のログが配信されることを確認する。
func TestServer_MultipleClients(t *testing.T) { ... }

// TestServer_ClientDisconnect: クライアントが切断された後、
//   Hub の clients map から除去されていることを確認する。
func TestServer_ClientDisconnect(t *testing.T) { ... }
```

*   **Logic**:
    *   テスト毎に `New(0, tl, log)` でエフェメラルポートを使い、テスト間の衝突を回避。
    *   各テストでは `gorilla/websocket.DefaultDialer.Dial` でクライアント接続する。

---

### hag パッケージ

#### [MODIFY] [server.go](file://shared/libs/go/hag/server.go)

*   **Description**: `TaskLog` フィールドの追加、`wsserver.New()` のシグネチャ変更に伴うワイヤリング修正
*   **Technical Design**:

```go
// Server struct に taskLog フィールドを追加
type Server struct {
    cfg          *config.AppConfig
    logger       logger.Logger
    vault        vault.VaultStore
    gateway      llmgateway.LLMGatewayBackend
    agentService *agentservice.Server
    wsServer     *wsserver.Server
    taskLog      *tasklog.TaskLog  // NEW
}

// New() 内のワイヤリング変更
func New(opts ...Option) (*Server, error) {
    // ... (既存の Steps 2-5 はそのまま) ...

    as := agentservice.New()
    tl := tasklog.New()  // NEW: TaskLog 初期化

    // wsserver.New のシグネチャ変更に対応
    wsPort := cfg.WebSocket.Port
    ws := wsserver.New(wsPort, tl, log)  // CHANGED: 引数追加

    return &Server{
        cfg:          cfg,
        logger:       log,
        vault:        vs,
        gateway:      gw,
        agentService: as,
        wsServer:     ws,
        taskLog:      tl,  // NEW
    }, nil
}

// TaskLog returns the TaskLog instance.
// Callers can use TaskLog().Add() to inject log entries.
func (s *Server) TaskLog() *tasklog.TaskLog {
    return s.taskLog
}
```

*   **Logic**:
    *   `New()` 内で `tasklog.New()` を呼び、`wsserver.New(wsPort, tl, log)` に渡す。
    *   `TaskLog()` アクセサを追加し、In-Process 利用者がログを注入できるようにする。
    *   `wsserver.New()` のシグネチャ変更により、既存の `ws := wsserver.New()` を `ws := wsserver.New(wsPort, tl, log)` に変更する。

#### [MODIFY] [server_test.go](file://shared/libs/go/hag/server_test.go)

*   **Description**: `TaskLog()` アクセサのテスト追加、`wsserver.New()` シグネチャ変更に伴う既存テストの修正
*   **Technical Design**:

```go
// 新規テスト: TaskLog アクセサ
func TestServer_TaskLog(t *testing.T) {
    stub := llmgateway.NewStubGateway()
    srv, err := New(WithGateway(stub))
    if err != nil {
        t.Fatalf("New() error = %v", err)
    }
    tl := srv.TaskLog()
    if tl == nil {
        t.Fatal("TaskLog() returned nil")
    }

    // TaskLog should be functional
    entry := tasklog.NewAgentLogEntry("test-agent", tasklog.WithKind("text"))
    tl.Add(entry)
    entries := tl.Entries()
    if len(entries) != 1 {
        t.Errorf("Entries() len = %d, want 1", len(entries))
    }
}
```

*   **Logic**: 既存テストは `wsserver.New()` を直接呼ばないため影響なし。`hag.New()` 内部で呼ばれるので、`hag.New()` を使うテストは自動的に新シグネチャを使用する。

---

### config パッケージ (テスト)

#### [MODIFY] [config_test.go](file://shared/libs/go/config/config_test.go)

*   **Description**: WebSocketConfig の YAML パース確認テスト追加
*   **Technical Design**:

```go
func TestLoad_WithWebSocketConfig(t *testing.T) {
    dir := t.TempDir()
    cfgPath := filepath.Join(dir, "config.yaml")
    content := []byte(`
websocket:
  port: 19000
`)
    os.WriteFile(cfgPath, content, 0644)
    cfg, err := Load(cfgPath)
    if err != nil {
        t.Fatalf("Load: %v", err)
    }
    if cfg.WebSocket.Port != 19000 {
        t.Errorf("WebSocket.Port = %d, want 19000", cfg.WebSocket.Port)
    }
}
```

---

### examples/log-viewer

#### [NEW] [main.go](file://examples/log-viewer/main.go)

*   **Description**: CLI Viewer + シミュレーター
*   **Technical Design**:

```go
package main

import (
    "context"
    "encoding/json"
    "flag"
    "fmt"
    "log"
    "os"
    "os/signal"
    "strings"
    "syscall"
    "time"

    "github.com/gorilla/websocket"
    "github.com/axsh/hag/hag"
    "github.com/axsh/hag/config"
    "github.com/axsh/hag/tasklog"
    "github.com/axsh/hag/wsserver"
)

// ANSI color codes
const (
    colorReset   = "\033[0m"
    colorRed     = "\033[31m"
    colorGreen   = "\033[32m"
    colorYellow  = "\033[33m"
    colorCyan    = "\033[36m"
    colorGray    = "\033[90m"
    colorBoldRed = "\033[1;31m"
)

func main() {
    simulate := flag.Bool("simulate", false, "Run in simulator mode (start server + generate logs)")
    url := flag.String("url", "ws://localhost:18080/ws", "WebSocket server URL")
    port := flag.Int("port", 18080, "WebSocket server port (simulator mode)")
    flag.Parse()

    if *simulate {
        runSimulator(*port)
    } else {
        runViewer(*url)
    }
}

// runViewer connects to a WebSocket server and displays logs.
func runViewer(wsURL string) {
    conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
    if err != nil {
        log.Fatalf("Failed to connect to %s: %v", wsURL, err)
    }
    defer conn.Close()

    fmt.Printf("%s--- Connected to %s ---%s\n", colorGreen, wsURL, colorReset)

    depthMap := make(map[string]int)

    for {
        _, data, err := conn.ReadMessage()
        if err != nil {
            fmt.Printf("%s--- Disconnected: %v ---%s\n", colorRed, err, colorReset)
            return
        }

        var msg wsserver.Message
        if err := json.Unmarshal(data, &msg); err != nil {
            continue
        }

        switch msg.Type {
        case "snapshot":
            var payload struct {
                Entries []json.RawMessage `json:"entries"`
            }
            json.Unmarshal(msg.Payload, &payload)
            fmt.Printf("%s--- Snapshot: %d entries ---%s\n",
                colorGray, len(payload.Entries), colorReset)
            for _, raw := range payload.Entries {
                var entry tasklog.AgentLogEntry
                if err := json.Unmarshal(raw, &entry); err == nil {
                    displayEntry(&entry, depthMap)
                }
            }

        case "log":
            var payload wsserver.LogPayload
            json.Unmarshal(msg.Payload, &payload)
            if payload.Entry != nil {
                displayEntry(payload.Entry, depthMap)
            }
        }
    }
}

// displayEntry displays a log entry with indentation and colors.
func displayEntry(entry *tasklog.AgentLogEntry, depthMap map[string]int) {
    depth := 0
    if entry.ParentLogID != "" {
        if parentDepth, ok := depthMap[entry.ParentLogID]; ok {
            depth = parentDepth + 1
        }
    }
    if entry.Phase == "begin" {
        depthMap[entry.ID] = depth
    }

    indent := strings.Repeat("  ", depth)
    timestamp := entry.Time.Format("15:04:05")
    kindColor := colorForKind(entry.Kind)

    switch entry.Phase {
    case "begin":
        fmt.Printf("%s[%s] %sBEGIN %-12s%s %s\n",
            indent, timestamp, kindColor, entry.Kind, colorReset, truncate(entry.Body, 80))
    case "send":
        fmt.Printf("%s[%s]   %sSEND%s  %s\n",
            indent, timestamp, kindColor, colorReset, truncate(entry.Body, 120))
    case "end":
        fmt.Printf("%s[%s] %sEND   %-12s%s\n",
            indent, timestamp, kindColor, entry.Kind, colorReset)
    }
}

func colorForKind(kind string) string {
    switch kind {
    case "thinking":   return colorGray
    case "tool_use":   return colorCyan
    case "tool_result": return colorYellow
    case "system":     return colorGreen
    case "error":      return colorBoldRed
    default:           return colorReset // "text" and unknown
    }
}

func truncate(s string, max int) string {
    if len(s) <= max { return s }
    return s[:max] + "..."
}

// runSimulator starts a HAG server and generates simulated logs.
func runSimulator(port int) {
    cfg := &config.AppConfig{
        WebSocket: config.WebSocketConfig{Port: port},
    }
    srv, err := hag.New(
        hag.WithConfig(cfg),
        hag.WithGateway(llmgateway.NewPassthroughDriver(0)), // dummy gateway
    )
    if err != nil {
        log.Fatalf("hag.New: %v", err)
    }

    ctx := context.Background()
    if err := srv.Launch(ctx); err != nil {
        log.Fatalf("Launch: %v", err)
    }
    defer srv.Shutdown(ctx)

    tl := srv.TaskLog()
    stack := &tasklog.LogStack{}

    fmt.Printf("Simulator running. WebSocket: ws://localhost:%d/ws\n", port)
    fmt.Println("Connect with: bin/log-viewer --url ws://localhost:" +
        fmt.Sprintf("%d", port) + "/ws")
    fmt.Println("Press Ctrl+C to stop.")

    // Wait for client connection
    time.Sleep(2 * time.Second)

    go func() {
        generateSimulatedLogs(tl, stack)
    }()

    // Wait for signal
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
    <-sigChan
    fmt.Println("\nShutting down...")
}

// generateSimulatedLogs generates a hierarchical log sequence.
func generateSimulatedLogs(tl *tasklog.TaskLog, stack *tasklog.LogStack) {
    // Round 1: Normal tool calling flow
    // 1. Root log (text) begin
    root := tasklog.NewAgentLogEntry("agent-1", tasklog.WithKind("text"))
    root.Body = "Processing user request: 'Fix the bug in main.go'"
    tl.Add(root)
    stack.Push(root.ID)
    time.Sleep(300 * time.Millisecond)

    // 2. Thinking begin -> send -> end
    thinking := tasklog.NewAgentLogEntry("agent-1",
        tasklog.WithKind("thinking"),
        tasklog.WithParentLogID(stack.CurrentParentID()),
    )
    tl.Add(thinking)
    stack.Push(thinking.ID)
    time.Sleep(200 * time.Millisecond)

    tl.Add(tasklog.NewAgentLogSendEntry(thinking.ID, "agent-1",
        "Let me analyze the error in main.go..."))
    time.Sleep(150 * time.Millisecond)
    tl.Add(tasklog.NewAgentLogSendEntry(thinking.ID, "agent-1",
        "I should read the file first to understand the issue."))
    time.Sleep(150 * time.Millisecond)

    tl.Add(tasklog.NewAgentLogEndEntry(thinking.ID, "agent-1"))
    stack.Pop()
    time.Sleep(200 * time.Millisecond)

    // 3. tool_use begin -> send
    toolUse := tasklog.NewAgentLogEntry("agent-1",
        tasklog.WithKind("tool_use"),
        tasklog.WithParentLogID(stack.CurrentParentID()),
    )
    toolUse.Body = "read_file"
    tl.Add(toolUse)
    stack.Push(toolUse.ID)
    time.Sleep(100 * time.Millisecond)

    tl.Add(tasklog.NewAgentLogSendEntry(toolUse.ID, "agent-1",
        `{"path": "main.go", "start_line": 1, "end_line": 50}`))
    time.Sleep(200 * time.Millisecond)

    // 4. tool_result begin -> send -> end
    toolResult := tasklog.NewAgentLogEntry("agent-1",
        tasklog.WithKind("tool_result"),
        tasklog.WithParentLogID(stack.CurrentParentID()),
    )
    tl.Add(toolResult)
    time.Sleep(100 * time.Millisecond)

    tl.Add(tasklog.NewAgentLogSendEntry(toolResult.ID, "agent-1",
        "package main\n\nimport \"fmt\"\n\nfunc main() {\n    fmt.Println(\"Hello\")\n}"))
    time.Sleep(200 * time.Millisecond)

    tl.Add(tasklog.NewAgentLogEndEntry(toolResult.ID, "agent-1"))
    time.Sleep(100 * time.Millisecond)

    // 5. tool_use end
    tl.Add(tasklog.NewAgentLogEndEntry(toolUse.ID, "agent-1"))
    stack.Pop()
    time.Sleep(200 * time.Millisecond)

    // 6. Second tool_use
    toolUse2 := tasklog.NewAgentLogEntry("agent-1",
        tasklog.WithKind("tool_use"),
        tasklog.WithParentLogID(stack.CurrentParentID()),
    )
    toolUse2.Body = "write_file"
    tl.Add(toolUse2)
    stack.Push(toolUse2.ID)
    time.Sleep(100 * time.Millisecond)

    tl.Add(tasklog.NewAgentLogSendEntry(toolUse2.ID, "agent-1",
        `{"path": "main.go", "content": "package main\n\nimport \"fmt\"\n\nfunc main() {\n    fmt.Println(\"Fixed!\")\n}"}`))
    time.Sleep(300 * time.Millisecond)

    toolResult2 := tasklog.NewAgentLogEntry("agent-1",
        tasklog.WithKind("tool_result"),
        tasklog.WithParentLogID(stack.CurrentParentID()),
    )
    tl.Add(toolResult2)
    tl.Add(tasklog.NewAgentLogSendEntry(toolResult2.ID, "agent-1", "File written successfully"))
    tl.Add(tasklog.NewAgentLogEndEntry(toolResult2.ID, "agent-1"))
    time.Sleep(100 * time.Millisecond)

    tl.Add(tasklog.NewAgentLogEndEntry(toolUse2.ID, "agent-1"))
    stack.Pop()
    time.Sleep(200 * time.Millisecond)

    // 7. Root end
    tl.Add(tasklog.NewAgentLogEndEntry(root.ID, "agent-1"))
    stack.Pop()
    time.Sleep(500 * time.Millisecond)

    // Round 2: Error scenario
    root2 := tasklog.NewAgentLogEntry("agent-1", tasklog.WithKind("text"))
    root2.Body = "Processing next request..."
    tl.Add(root2)
    time.Sleep(300 * time.Millisecond)

    errLog := tasklog.NewAgentLogEntry("agent-1",
        tasklog.WithKind("error"),
        tasklog.WithParentLogID(root2.ID),
    )
    errLog.Body = "Connection to LLM provider failed: timeout"
    tl.Add(errLog)
    tl.Add(tasklog.NewAgentLogEndEntry(errLog.ID, "agent-1"))
    time.Sleep(200 * time.Millisecond)

    // Abnormal termination
    tl.Add(tasklog.NewTerminatedEntry("agent-1", "provider timeout"))

    fmt.Println("\n--- Simulation complete ---")
}
```

*   **Logic**:
    *   `--simulate` モード: `hag.Server` を起動し、`TaskLog()` 経由でログを注入。WebSocket Server が自動的にブロードキャストする。
    *   `--url` モード: 指定 URL に WebSocket 接続し、受信メッセージを階層表示する。
    *   `displayEntry`: `depthMap` (map[string]int) で各ログIDの深度を管理。`parentLogId` の親の深度 + 1 で子の深度を決定。
    *   シミュレーターは `LogStack` を使って `parentLogId` を自動管理。仕様 R5-3 の全シーケンスを実装。

#### [NEW] [go.mod](file://examples/log-viewer/go.mod)

*   **Description**: log-viewer の Go モジュール定義
*   **Technical Design**:

```
module github.com/axsh/hag/log-viewer

go 1.26.3

require (
    github.com/axsh/hag v0.0.0
    github.com/gorilla/websocket v1.5.3
)

replace github.com/axsh/hag => ../../shared/libs/go
```

---

### 依存関係: gorilla/websocket の追加

#### [MODIFY] [go.mod](file://shared/libs/go/go.mod)

*   **Description**: `gorilla/websocket` を依存に追加
*   **Logic**: `go get github.com/gorilla/websocket@latest` を実行する

#### [MODIFY] [go.mod](file://tests/go.mod)

*   **Description**: テストモジュールにも `gorilla/websocket` の間接依存を追加 (統合テストで使用するため)
*   **Logic**: `go get github.com/gorilla/websocket@latest` を実行する

---

### 統合テスト

#### [NEW] [wsserver_integration_test.go](file://tests/wsserver_integration_test.go)

*   **Description**: WebSocket ログストリーミングの統合テスト
*   **Technical Design**:

```go
package llm_test

import (
    "encoding/json"
    "testing"
    "time"

    "github.com/gorilla/websocket"
    "github.com/axsh/hag/config"
    "github.com/axsh/hag/hag"
    "github.com/axsh/hag/tasklog"
    "github.com/axsh/hag/wsserver"
)

func TestWebSocket_LogStreaming(t *testing.T) {
    cfg := &config.AppConfig{
        WebSocket: config.WebSocketConfig{Port: 0},
    }
    srv, err := hag.New(hag.WithConfig(cfg))
    if err != nil {
        t.Fatalf("hag.New: %v", err)
    }
    if err := srv.Launch(t.Context()); err != nil {
        t.Fatalf("Launch: %v", err)
    }
    defer srv.Shutdown(t.Context())

    // Connect WebSocket client
    wsURL := /* get wsserver URL from srv somehow */
    conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
    if err != nil {
        t.Fatalf("Dial: %v", err)
    }
    defer conn.Close()

    // Read snapshot
    conn.SetReadDeadline(time.Now().Add(2 * time.Second))
    _, data, _ := conn.ReadMessage()
    var snap wsserver.Message
    json.Unmarshal(data, &snap)
    if snap.Type != "snapshot" {
        t.Fatalf("expected snapshot, got %q", snap.Type)
    }

    // Add hierarchical log entries
    tl := srv.TaskLog()
    root := tasklog.NewAgentLogEntry("agent-1", tasklog.WithKind("text"))
    tl.Add(root)

    child := tasklog.NewAgentLogEntry("agent-1",
        tasklog.WithKind("thinking"),
        tasklog.WithParentLogID(root.ID),
    )
    tl.Add(child)

    // Read 2 log messages
    for i := 0; i < 2; i++ {
        conn.SetReadDeadline(time.Now().Add(2 * time.Second))
        _, data, err = conn.ReadMessage()
        if err != nil {
            t.Fatalf("ReadMessage[%d]: %v", i, err)
        }
        var msg wsserver.Message
        json.Unmarshal(data, &msg)
        if msg.Type != "log" {
            t.Errorf("msg[%d].Type = %q, want %q", i, msg.Type, "log")
        }
    }
}

func TestWebSocket_HierarchicalLogStructure(t *testing.T) {
    cfg := &config.AppConfig{
        WebSocket: config.WebSocketConfig{Port: 0},
    }
    srv, err := hag.New(hag.WithConfig(cfg))
    if err != nil {
        t.Fatalf("hag.New: %v", err)
    }
    if err := srv.Launch(t.Context()); err != nil {
        t.Fatalf("Launch: %v", err)
    }
    defer srv.Shutdown(t.Context())

    // Connect
    wsURL := /* srv wsserver URL */
    conn, _, _ := websocket.DefaultDialer.Dial(wsURL, nil)
    defer conn.Close()

    // Read snapshot
    conn.SetReadDeadline(time.Now().Add(2 * time.Second))
    conn.ReadMessage()

    // Build hierarchical logs: root -> tool_use -> tool_result
    tl := srv.TaskLog()
    root := tasklog.NewAgentLogEntry("agent-1", tasklog.WithKind("text"))
    tl.Add(root)

    toolUse := tasklog.NewAgentLogEntry("agent-1",
        tasklog.WithKind("tool_use"),
        tasklog.WithParentLogID(root.ID),
    )
    tl.Add(toolUse)

    toolResult := tasklog.NewAgentLogEntry("agent-1",
        tasklog.WithKind("tool_result"),
        tasklog.WithParentLogID(toolUse.ID),
    )
    tl.Add(toolResult)

    // Verify hierarchy in received messages
    received := make([]*tasklog.AgentLogEntry, 0, 3)
    for i := 0; i < 3; i++ {
        conn.SetReadDeadline(time.Now().Add(2 * time.Second))
        _, data, err := conn.ReadMessage()
        if err != nil {
            t.Fatalf("ReadMessage[%d]: %v", i, err)
        }
        var msg wsserver.Message
        json.Unmarshal(data, &msg)
        var payload wsserver.LogPayload
        json.Unmarshal(msg.Payload, &payload)
        received = append(received, payload.Entry)
    }

    // Verify: root has no parent
    if received[0].ParentLogID != "" {
        t.Errorf("root.ParentLogID = %q, want empty", received[0].ParentLogID)
    }
    // Verify: tool_use's parent is root
    if received[1].ParentLogID != received[0].ID {
        t.Errorf("toolUse.ParentLogID = %q, want %q", received[1].ParentLogID, received[0].ID)
    }
    // Verify: tool_result's parent is tool_use (3rd level)
    if received[2].ParentLogID != received[1].ID {
        t.Errorf("toolResult.ParentLogID = %q, want %q", received[2].ParentLogID, received[1].ID)
    }
    // Verify kinds
    if received[0].Kind != "text" || received[1].Kind != "tool_use" || received[2].Kind != "tool_result" {
        t.Errorf("kinds = [%s, %s, %s], want [text, tool_use, tool_result]",
            received[0].Kind, received[1].Kind, received[2].Kind)
    }
}
```

*   **Logic**:
    *   `TestWebSocket_LogStreaming`: 基本的なログ配信の検証。snapshot 受信後、TaskLog.Add() -> WebSocket 受信を確認。
    *   `TestWebSocket_HierarchicalLogStructure`: 3階層 (root -> tool_use -> tool_result) の `parentLogId` チェーンが正しいことを検証。

> [!NOTE]
> **hag.Server からの WebSocket URL 取得**: 統合テストでは `hag.Server` に WebSocket Server の URL を取得するメソッドが必要。`hag.Server` に `WebSocketURL() string` メソッドを追加するか、`wsserver.Server` を公開する方法を検討する。実装時に `func (s *Server) WebSocketURL() string` を追加する。

---

## Step-by-Step Implementation Guide

### Step 1: 依存関係の追加

1. `shared/libs/go/` で `go get github.com/gorilla/websocket@latest` を実行
2. `go.sum` を更新

### Step 2: config パッケージの修正

1. Edit `shared/libs/go/config/config.go`: `WebSocketConfig` struct 追加、`AppConfig` にフィールド追加
2. Edit `shared/libs/go/config/config_test.go`: YAML パーステスト追加
3. `scripts/process/build.sh --skip-frontend --skip-etc` でビルド確認

### Step 3: wsserver/message.go (テストファースト)

1. Create `shared/libs/go/wsserver/message_test.go`: `TestNewLogMessage`, `TestNewSnapshotMessage`
2. Create `shared/libs/go/wsserver/message.go`: `Message`, `LogPayload`, `SnapshotPayload`, `NewLogMessage`, `NewSnapshotMessage`
3. ビルド確認

### Step 4: wsserver/client.go

1. Create `shared/libs/go/wsserver/client.go`: `Client` struct, `writePump`, `readPump`

### Step 5: wsserver/hub.go

1. Create `shared/libs/go/wsserver/hub.go`: `Hub` struct, `newHub`, `run`, `sendSnapshot`

### Step 6: wsserver/server.go (テストファースト)

1. Create `shared/libs/go/wsserver/server_test.go`: 単体テスト (Hub, Launch/Shutdown, WebSocket接続, ログブロードキャスト)
2. Modify `shared/libs/go/wsserver/server.go`: スケルトンを実装に置き換え (`New`, `Launch`, `Shutdown`, `serveWS`, `wireTaskLog`, `URL`)
3. ビルド確認

### Step 7: hag/server.go の修正

1. Edit `shared/libs/go/hag/server.go`:
   - `taskLog` フィールド追加
   - `New()` で `tasklog.New()` と `wsserver.New(wsPort, tl, log)` 呼出
   - `TaskLog()` アクセサ追加
   - `WebSocketURL()` アクセサ追加
2. Edit `shared/libs/go/hag/server_test.go`: `TestServer_TaskLog` 追加
3. ビルド確認

### Step 8: examples/log-viewer の作成

1. Create `examples/log-viewer/go.mod`
2. Create `examples/log-viewer/main.go`: viewer + simulator
3. `examples/log-viewer/` で `go mod tidy` 実行
4. `go build -o ../../bin/log-viewer .` でバイナリビルド確認

### Step 9: 統合テスト

1. `tests/` で `go get github.com/gorilla/websocket@latest`
2. Create `tests/wsserver_integration_test.go`: `TestWebSocket_LogStreaming`, `TestWebSocket_HierarchicalLogStructure`
3. `scripts/process/build.sh` でビルド
4. `scripts/process/integration_test.sh --specify "WebSocket"` で統合テスト実行

### Step 10: Verification Plan の実行

1. `scripts/process/build.sh` で全体ビルド + 単体テスト
2. `scripts/process/integration_test.sh --categories "common" --specify "WebSocket|LogStreaming|Hierarchical"` で統合テスト

---

## Verification Plan

### Automated Verification

1. **Build & Unit Tests**:
   ```bash
   ./scripts/process/build.sh
   ```

2. **Integration Tests (WebSocket)**:
   ```bash
   ./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "WebSocket"
   ```
   *   **Log Verification**:
       - `TestWebSocket_LogStreaming`: snapshot 受信 -> log 受信の順序が正しいこと
       - `TestWebSocket_HierarchicalLogStructure`: 3階層の `parentLogId` チェーンが正しいこと
       - `TestWebSocket_HierarchicalLogStructure`: `kind` が `text/tool_use/tool_result` であること

3. **Integration Tests (既存テストのリグレッション確認)**:
   ```bash
   ./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "Server|Lifecycle|Anthropic|OpenAI"
   ```

### テスト項目のセルフレビュー (Section 11.4)

1. **網羅性の検証**: メッセージ直列化 -> Hub register/unregister -> ブロードキャスト -> snapshot 配信 -> クライアント切断 -> hag.Server ワイヤリング -> 統合テスト (階層構造) のボトムアップ順序で設計している。全テスト成功 = WebSocket でログが正しく配信される + 階層構造が正しい、と言い切れる。
2. **証拠の十分性**: 各テストで「期待する type/kind/parentLogId の値」まで検証しており、「エラーが出ない」だけでなく「正しいデータが届く」ことを確認している。
3. **迂回・抜け道の排除**: `TestWebSocket_HierarchicalLogStructure` で `parentLogId` の親子チェーンを 3 段階で検証しており、フラットなログが誤って成功することはない。
4. **依存関係の整合性**: message.go (末端) -> hub.go -> server.go -> hag.Server -> 統合テスト の順でテストを設計。各層の成功が次の層の前提となっている。

### 総合判定プロセス (Section 12)

Verification Plan の全テスト完了後、以下のチェック項目を確認:
1. スキップされたテストの有無
2. ログ内の ERROR/WARN/panic
3. フォールバック処理による偽成功の排除
4. テスト間の依存/順序問題
5. 新機能に対するテストカバレッジ

判定結果は walkthrough に記録する。

---

## Documentation

#### [MODIFY] [000-Architecture.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/ideas/000-Architecture.md)

*   **更新内容**: R2-1 WebSocket Server の実装状態を「スケルトン」から「実装完了」に更新

#### [MODIFY] [003-HierarchicalAgentLog.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/ideas/003-HierarchicalAgentLog.md)

*   **更新内容**: R5 WebSocket中継の実装状態を更新 (wsserver パッケージの実装完了)
