# 011-CodingAgentAPI-Part3-AgentService-Integration

> **Source Specification**: [007-CodingAgentAPI.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/ideas/007-CodingAgentAPI.md)

## Goal Description

AgentService (Web APIフロント層) を実装し、hag.Server との統合を完成させる。ヘルスチェック (LLMGP連鎖)、SessionStore (MemorySessionStore)、HTTPハンドラ (REST + SSE)、ログストリーミング SSE、Terminate API を含む。既存の `agentservice/service.go` のスタブ実装を本番実装に置き換える。

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R5-1: AgentService Server | `agentservice/service.go` |
| R5-2: HTTPエンドポイント一覧 | `agentservice/handler.go` |
| R5-3: ヘルスチェック (LLMGP連鎖) | `agentservice/health.go` |
| R5-4: Content Negotiation (SSE/JSON) | `agentservice/handler.go handleSendMessage()` |
| R5-5: TaskLog + WebSocket連携 | `agentservice/handler.go` |
| R5-6: ログストリーミングSSE | `agentservice/log_stream.go` |
| R7-2: MemorySessionStore | `agentservice/session_store.go` |
| R8-1: hag.Server統合 (初期化順序) | `hag/server.go` |
| R8-2: WithAgentService Option | `hag/options.go` (既存ファイルに追加) |
| R8-3: HTTPマウント | `hag/server.go` |

## Proposed Changes

### agentservice パッケージ

#### [NEW] [session_store_test.go](file://shared/libs/go/agentservice/session_store_test.go)
*   **Description**: MemorySessionStore のテスト
*   **Technical Design**:
    ```go
    func TestMemorySessionStore_Create(t *testing.T)
    // - セッションを作成し、Get で取得できること
    // - CreatedAt, UpdatedAt が設定されていること

    func TestMemorySessionStore_Update(t *testing.T)
    // - Status を "active" -> "completed" に更新
    // - SDKSessionID を設定し、Get で取得できること
    // - UpdatedAt が更新されていること

    func TestMemorySessionStore_List(t *testing.T)
    // - 3件作成し、List で3件取得できること

    func TestMemorySessionStore_Delete(t *testing.T)
    // - 作成したセッションを削除し、Get で ErrNotFound が返ること

    func TestMemorySessionStore_GetNotFound(t *testing.T)
    // - 存在しないIDで Get を呼び、ErrNotFound が返ること

    func TestMemorySessionStore_StatusTransition(t *testing.T)
    // テーブル駆動テスト:
    // | from       | to          | valid |
    // | "active"   | "completed" | true  |
    // | "active"   | "error"     | true  |
    // | "active"   | "closed"    | true  |
    // | "completed"| "active"    | false |
    ```

#### [NEW] [session_store.go](file://shared/libs/go/agentservice/session_store.go)
*   **Description**: MemorySessionStore 実装
*   **Technical Design**:
    ```go
    package agentservice

    import (
        "errors"
        "sync"
        "time"
        "github.com/axsh/hag/codingagent"
    )

    var ErrNotFound = errors.New("session not found")

    // MemorySessionStore はインメモリの SessionStore 実装。
    type MemorySessionStore struct {
        mu       sync.RWMutex
        sessions map[string]*codingagent.SessionRecord
    }

    func NewMemorySessionStore() *MemorySessionStore {
        return &MemorySessionStore{
            sessions: make(map[string]*codingagent.SessionRecord),
        }
    }

    // compile-time check
    var _ codingagent.SessionStore = (*MemorySessionStore)(nil)

    func (m *MemorySessionStore) Create(s *codingagent.SessionRecord) error {
        m.mu.Lock()
        defer m.mu.Unlock()
        now := time.Now()
        s.CreatedAt = now
        s.UpdatedAt = now
        m.sessions[s.ID] = s
        return nil
    }

    func (m *MemorySessionStore) Get(id string) (*codingagent.SessionRecord, error) {
        m.mu.RLock()
        defer m.mu.RUnlock()
        s, ok := m.sessions[id]
        if !ok { return nil, ErrNotFound }
        return s, nil
    }

    func (m *MemorySessionStore) Update(s *codingagent.SessionRecord) error {
        m.mu.Lock()
        defer m.mu.Unlock()
        if _, ok := m.sessions[s.ID]; !ok { return ErrNotFound }
        s.UpdatedAt = time.Now()
        m.sessions[s.ID] = s
        return nil
    }

    func (m *MemorySessionStore) List() ([]*codingagent.SessionRecord, error) {
        m.mu.RLock()
        defer m.mu.RUnlock()
        var result []*codingagent.SessionRecord
        for _, s := range m.sessions { result = append(result, s) }
        return result, nil
    }

    func (m *MemorySessionStore) Delete(id string) error {
        m.mu.Lock()
        defer m.mu.Unlock()
        if _, ok := m.sessions[id]; !ok { return ErrNotFound }
        delete(m.sessions, id)
        return nil
    }
    ```

---

#### [NEW] [health_test.go](file://shared/libs/go/agentservice/health_test.go)
*   **Description**: ヘルスチェックハンドラのテスト
*   **Technical Design**:
    ```go
    func TestHealthHandler_AllHealthy(t *testing.T)
    // - LLMGP のモックサーバーを起動し、/health で {"status":"ok"} を返す
    // - GET /health を呼び出し、200 OK が返ること
    // - レスポンスの status が "ok"、gateway.status が "ok" であること
    // - agents 配列にエージェント名が含まれること

    func TestHealthHandler_GatewayDown(t *testing.T)
    // - LLMGP のモックサーバーを起動しない (接続拒否)
    // - GET /health を呼び出し、502 Bad Gateway が返ること
    // - レスポンスの status が "degraded"、gateway.status が "unreachable" であること
    // - gateway.error にエラーメッセージが含まれること

    func TestHealthHandler_GatewayTimeout(t *testing.T)
    // - LLMGP のモックサーバーを起動し、3秒遅延で応答する
    // - GET /health を呼び出し、502 Bad Gateway が返ること (2秒タイムアウト)

    func TestHealthHandler_NoAuth(t *testing.T)
    // - 認証ミドルウェアが設定されていても、/health は認証不要であること
    ```

#### [NEW] [health.go](file://shared/libs/go/agentservice/health.go)
*   **Description**: ヘルスチェックハンドラ (LLMGP連鎖)
*   **Technical Design**:
    ```go
    package agentservice

    import (
        "context"
        "encoding/json"
        "net/http"
        "time"
    )

    const healthCheckTimeout = 2 * time.Second

    // HealthResponse はヘルスチェックのレスポンス構造
    type HealthResponse struct {
        Status      string            `json:"status"`
        Agents      []string          `json:"agents"`
        CLIVersions map[string]string `json:"cli_versions"`
        Gateway     GatewayHealth     `json:"gateway"`
    }

    type GatewayHealth struct {
        Status string `json:"status"`
        URL    string `json:"url"`
        Error  string `json:"error,omitempty"`
    }

    // handleHealth はヘルスチェックハンドラ。
    // CAWA自身の状態 + LLMGP への連鎖チェックを行う。
    func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
        agentNames := make([]string, 0, len(s.agents))
        for name := range s.agents { agentNames = append(agentNames, name) }

        // LLMGP ヘルスチェック
        gwHealth := s.checkGatewayHealth()

        resp := HealthResponse{
            Status:      "ok",
            Agents:      agentNames,
            CLIVersions: make(map[string]string), // O7 で実装
            Gateway:     gwHealth,
        }

        if gwHealth.Status != "ok" {
            resp.Status = "degraded"
            w.WriteHeader(http.StatusBadGateway) // 502
        } else {
            w.WriteHeader(http.StatusOK) // 200
        }

        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(resp)
    }

    // checkGatewayHealth は LLMGP の /health を呼び出す。
    func (s *Server) checkGatewayHealth() GatewayHealth {
        if s.gatewayURL == "" {
            return GatewayHealth{Status: "ok", URL: "(in-process)"}
        }

        ctx, cancel := context.WithTimeout(context.Background(), healthCheckTimeout)
        defer cancel()

        req, _ := http.NewRequestWithContext(ctx, "GET", s.gatewayURL+"/health", nil)
        resp, err := http.DefaultClient.Do(req)
        if err != nil {
            return GatewayHealth{
                Status: "unreachable", URL: s.gatewayURL, Error: err.Error(),
            }
        }
        defer resp.Body.Close()

        if resp.StatusCode != http.StatusOK {
            return GatewayHealth{
                Status: "unhealthy", URL: s.gatewayURL,
                Error: "HTTP " + resp.Status,
            }
        }
        return GatewayHealth{Status: "ok", URL: s.gatewayURL}
    }
    ```

---

#### [NEW] [handler_test.go](file://shared/libs/go/agentservice/handler_test.go)
*   **Description**: HTTPハンドラのテスト
*   **Technical Design**:
    ```go
    func TestHandleListAgents(t *testing.T)
    // - GET /api/v1/agents で登録済みエージェント一覧が返ること

    func TestHandleCreateSession(t *testing.T)
    // - POST /api/v1/sessions で 201 Created が返ること
    // - レスポンスに session_id, status:"created" が含まれること

    func TestHandleGetSession(t *testing.T)
    // - GET /api/v1/sessions/:id でセッション情報が返ること
    // - 存在しないIDで 404 が返ること

    func TestHandleDeleteSession(t *testing.T)
    // - DELETE /api/v1/sessions/:id でセッションが削除されること
    // - 存在しないIDで 404 が返ること

    func TestHandleTerminateAgent(t *testing.T)
    // - POST /api/v1/agents/:id/terminate でセッション強制終了
    // - セッションのステータスが "closed" に変更されること
    ```

#### [NEW] [handler.go](file://shared/libs/go/agentservice/handler.go)
*   **Description**: HTTPハンドラ (REST + SSE)
*   **Technical Design**:
    ```go
    package agentservice

    import (
        "encoding/json"
        "fmt"
        "net/http"
        "strings"

        "github.com/axsh/hag/codingagent"
        "github.com/google/uuid"
    )

    // handleListAgents は GET /api/v1/agents を処理する。
    func (s *Server) handleListAgents(w http.ResponseWriter, r *http.Request) {
        type agentInfo struct {
            Name string `json:"name"`
        }
        var agents []agentInfo
        for name := range s.agents {
            agents = append(agents, agentInfo{Name: name})
        }
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(agents)
    }

    // handleCreateSession は POST /api/v1/sessions を処理する。
    func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
        var req struct {
            Agent   string   `json:"agent"`
            Model   string   `json:"model"`
            WorkDir string   `json:"work_dir"`
            Prompt  string   `json:"prompt"`
        }
        if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
            http.Error(w, err.Error(), http.StatusBadRequest); return
        }

        agent, ok := s.agents[req.Agent]
        if !ok {
            http.Error(w, "unknown agent: "+req.Agent, http.StatusBadRequest); return
        }

        sessionID := uuid.New().String()
        record := &codingagent.SessionRecord{
            ID:        sessionID,
            AgentName: req.Agent,
            Model:     req.Model,
            Status:    codingagent.StatusActive,
            WorkDir:   req.WorkDir,
        }
        s.sessions.Create(record)
        // エージェントセッションは実際にメッセージ送信時に作成する (シングルショット)

        w.WriteHeader(http.StatusCreated)
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(map[string]string{
            "session_id": sessionID,
            "status":     "created",
        })
    }

    // handleGetSession は GET /api/v1/sessions/:id を処理する。
    func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
        id := extractPathParam(r.URL.Path, "/api/v1/sessions/")
        record, err := s.sessions.Get(id)
        if err != nil {
            http.Error(w, "session not found", http.StatusNotFound); return
        }
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(record)
    }

    // handleDeleteSession は DELETE /api/v1/sessions/:id を処理する。
    func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
        id := extractPathParam(r.URL.Path, "/api/v1/sessions/")
        if err := s.sessions.Delete(id); err != nil {
            http.Error(w, "session not found", http.StatusNotFound); return
        }
        w.WriteHeader(http.StatusNoContent)
    }

    // handleSendMessage は POST /api/v1/sessions/:id/messages を処理する。
    // Content Negotiation: Accept: text/event-stream -> SSE, それ以外 -> JSON
    func (s *Server) handleSendMessage(w http.ResponseWriter, r *http.Request) {
        // パスから session_id を抽出
        path := r.URL.Path
        // /api/v1/sessions/{id}/messages
        parts := strings.Split(strings.TrimPrefix(path, "/api/v1/sessions/"), "/")
        if len(parts) < 2 { http.Error(w, "invalid path", http.StatusBadRequest); return }
        sessionID := parts[0]

        record, err := s.sessions.Get(sessionID)
        if err != nil {
            http.Error(w, "session not found", http.StatusNotFound); return
        }

        var req struct {
            Message string `json:"message"`
        }
        if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
            http.Error(w, err.Error(), http.StatusBadRequest); return
        }

        agent, ok := s.agents[record.AgentName]
        if !ok {
            http.Error(w, "agent not available", http.StatusInternalServerError); return
        }

        session, err := agent.CreateSession(r.Context(),
            codingagent.WithModel(record.Model),
            codingagent.WithPrompt(req.Message),
            codingagent.WithWorkDir(record.WorkDir),
        )
        if err != nil {
            http.Error(w, err.Error(), http.StatusInternalServerError); return
        }
        defer session.Close()

        ch, err := session.Send(r.Context(), req.Message)
        if err != nil {
            http.Error(w, err.Error(), http.StatusInternalServerError); return
        }

        // Content Negotiation
        if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
            s.streamSSE(w, r, ch, sessionID)
        } else {
            s.respondJSON(w, ch, sessionID)
        }
    }

    // streamSSE は SSE 形式でストリーミング応答を送信する。
    func (s *Server) streamSSE(
        w http.ResponseWriter, r *http.Request,
        ch <-chan codingagent.StreamEvent, sessionID string,
    ) {
        w.Header().Set("Content-Type", "text/event-stream")
        w.Header().Set("Cache-Control", "no-cache")
        w.Header().Set("Connection", "keep-alive")

        flusher, ok := w.(http.Flusher)
        if !ok { http.Error(w, "streaming unsupported", 500); return }

        for ev := range ch {
            data, _ := json.Marshal(ev)
            fmt.Fprintf(w, "data: %s\n\n", data)
            // TaskLog に記録 (R5-5)
            if s.taskLog != nil {
                s.taskLog.AddStreamEvent(sessionID, ev)
            }
            flusher.Flush()
        }
        fmt.Fprintf(w, "data: [DONE]\n\n")
        flusher.Flush()

        // セッション完了
        record, _ := s.sessions.Get(sessionID)
        if record != nil {
            record.Status = codingagent.StatusCompleted
            s.sessions.Update(record)
        }
    }

    // respondJSON は JSON 形式で一括応答を送信する。
    func (s *Server) respondJSON(
        w http.ResponseWriter,
        ch <-chan codingagent.StreamEvent, sessionID string,
    ) {
        var events []codingagent.StreamEvent
        for ev := range ch { events = append(events, ev) }
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(events)

        record, _ := s.sessions.Get(sessionID)
        if record != nil {
            record.Status = codingagent.StatusCompleted
            s.sessions.Update(record)
        }
    }

    // handleTerminate は POST /api/v1/agents/:id/terminate を処理する。
    func (s *Server) handleTerminate(w http.ResponseWriter, r *http.Request) {
        id := extractPathParam(r.URL.Path, "/api/v1/agents/")
        id = strings.TrimSuffix(id, "/terminate")
        // session ID として扱う (アクティブセッションの強制終了)
        record, err := s.sessions.Get(id)
        if err != nil {
            http.Error(w, "session not found", http.StatusNotFound); return
        }
        record.Status = codingagent.StatusClosed
        s.sessions.Update(record)
        w.WriteHeader(http.StatusOK)
        json.NewEncoder(w).Encode(map[string]string{"status": "terminated"})
    }

    func extractPathParam(path, prefix string) string {
        trimmed := strings.TrimPrefix(path, prefix)
        if idx := strings.Index(trimmed, "/"); idx >= 0 {
            return trimmed[:idx]
        }
        return trimmed
    }
    ```

---

#### [NEW] [log_stream_test.go](file://shared/libs/go/agentservice/log_stream_test.go)
*   **Description**: ログストリーミングSSEのテスト
*   **Technical Design**:
    ```go
    func TestLogStreamSSE_Snapshot(t *testing.T)
    // - セッションに既存ログが3件ある状態で SSE 接続
    // - 初回接続時に3件のログが即座に送信されること

    func TestLogStreamSSE_Realtime(t *testing.T)
    // - SSE 接続中に新しいログが追加された場合
    // - 500ms 以内に新しいログが配信されること

    func TestLogStreamSSE_Termination(t *testing.T)
    // - セッション完了時に status: terminated と [DONE] が送信されること

    func TestLogStreamSSE_Failed(t *testing.T)
    // - エラー終了の場合に status: failed が送信されること
    ```

#### [NEW] [log_stream.go](file://shared/libs/go/agentservice/log_stream.go)
*   **Description**: セッションログ SSE ストリーミング
*   **Technical Design**:
    ```go
    package agentservice

    import (
        "encoding/json"
        "fmt"
        "net/http"
        "time"
    )

    // handleLogStream は GET /api/v1/sessions/:id/logs を処理する。
    // SSE でセッションログをリアルタイム配信する。
    func (s *Server) handleLogStream(w http.ResponseWriter, r *http.Request) {
        // パスからセッションIDを抽出
        path := r.URL.Path
        // /api/v1/sessions/{id}/logs
        // extractPathParam でセッションIDを取得
        sessionID := extractPathParam(path, "/api/v1/sessions/")

        record, err := s.sessions.Get(sessionID)
        if err != nil {
            http.Error(w, "session not found", http.StatusNotFound)
            return
        }

        w.Header().Set("Content-Type", "text/event-stream")
        w.Header().Set("Cache-Control", "no-cache")
        w.Header().Set("Connection", "keep-alive")

        flusher, ok := w.(http.Flusher)
        if !ok {
            http.Error(w, "streaming unsupported", http.StatusInternalServerError)
            return
        }

        // 既存ログのスナップショット送信
        if s.taskLog != nil {
            logs := s.taskLog.GetSessionLogs(sessionID)
            for _, log := range logs {
                data, _ := json.Marshal(log)
                fmt.Fprintf(w, "event: log\ndata: %s\n\n", data)
            }
            flusher.Flush()
        }

        // 既に完了している場合は即座に終了
        if record.Status == "completed" || record.Status == "error" || record.Status == "closed" {
            emitTerminationStatus(w, flusher, record.Status)
            return
        }

        // ポーリングで新規ログを配信 (500ms間隔)
        ticker := time.NewTicker(500 * time.Millisecond)
        defer ticker.Stop()
        lastIndex := 0
        if s.taskLog != nil {
            lastIndex = len(s.taskLog.GetSessionLogs(sessionID))
        }

        for {
            select {
            case <-r.Context().Done():
                return
            case <-ticker.C:
                if s.taskLog != nil {
                    logs := s.taskLog.GetSessionLogs(sessionID)
                    if len(logs) > lastIndex {
                        for _, log := range logs[lastIndex:] {
                            data, _ := json.Marshal(log)
                            fmt.Fprintf(w, "event: log\ndata: %s\n\n", data)
                        }
                        lastIndex = len(logs)
                        flusher.Flush()
                    }
                }

                // セッション状態チェック
                rec, err := s.sessions.Get(sessionID)
                if err == nil && (rec.Status == "completed" || rec.Status == "error" || rec.Status == "closed") {
                    emitTerminationStatus(w, flusher, rec.Status)
                    return
                }
            }
        }
    }

    func emitTerminationStatus(w http.ResponseWriter, flusher http.Flusher, status string) {
        sseStatus := "terminated"
        if status == "error" { sseStatus = "failed" }
        fmt.Fprintf(w, "event: status\ndata: {\"status\":\"%s\"}\n\n", sseStatus)
        fmt.Fprintf(w, "data: [DONE]\n\n")
        flusher.Flush()
    }
    ```

---

#### [MODIFY] [service.go](file://shared/libs/go/agentservice/service.go)
*   **Description**: スタブ実装を本番実装に置き換える
*   **Technical Design**:
    ```go
    package agentservice

    import (
        "net/http"

        "github.com/axsh/hag/codingagent"
        "github.com/axsh/hag/logger"
        "github.com/axsh/hag/tasklog"
    )

    // Server はCoding Agent APIのサービス層。
    type Server struct {
        agents     map[string]codingagent.CodingAgent
        sessions   codingagent.SessionStore
        logger     logger.Logger
        taskLog    *tasklog.TaskLog
        gatewayURL string
    }

    // Option は Server の設定オプション。
    type Option func(*Server)

    func WithLogger(log logger.Logger) Option {
        return func(s *Server) { s.logger = log }
    }
    func WithTaskLog(tl *tasklog.TaskLog) Option {
        return func(s *Server) { s.taskLog = tl }
    }
    func WithGatewayURL(url string) Option {
        return func(s *Server) { s.gatewayURL = url }
    }

    // New は AgentService Server を生成する。
    func New(opts ...Option) *Server {
        s := &Server{
            agents:   make(map[string]codingagent.CodingAgent),
            sessions: NewMemorySessionStore(),
        }
        for _, opt := range opts { opt(s) }
        return s
    }

    // RegisterAgent はエージェントを登録する。
    func (s *Server) RegisterAgent(agent codingagent.CodingAgent) {
        s.agents[agent.Name()] = agent
    }

    // HTTPHandler は全エンドポイントのルーティングを返す。
    func (s *Server) HTTPHandler() http.Handler {
        mux := http.NewServeMux()
        mux.HandleFunc("/health", s.handleHealth)
        mux.HandleFunc("/api/v1/agents", s.routeAgents)
        mux.HandleFunc("/api/v1/sessions", s.routeSessions)
        mux.HandleFunc("/api/v1/sessions/", s.routeSessionByID)
        return mux
    }

    func (s *Server) routeAgents(w http.ResponseWriter, r *http.Request) {
        switch r.Method {
        case http.MethodGet:
            s.handleListAgents(w, r)
        default:
            http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
        }
    }

    func (s *Server) routeSessions(w http.ResponseWriter, r *http.Request) {
        switch r.Method {
        case http.MethodPost:
            s.handleCreateSession(w, r)
        default:
            http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
        }
    }

    func (s *Server) routeSessionByID(w http.ResponseWriter, r *http.Request) {
        path := r.URL.Path
        if strings.HasSuffix(path, "/messages") {
            s.handleSendMessage(w, r)
        } else if strings.HasSuffix(path, "/logs") {
            s.handleLogStream(w, r)
        } else if strings.HasSuffix(path, "/terminate") {
            s.handleTerminate(w, r)
        } else {
            switch r.Method {
            case http.MethodGet:
                s.handleGetSession(w, r)
            case http.MethodDelete:
                s.handleDeleteSession(w, r)
            default:
                http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
            }
        }
    }
    ```
*   **Logic**: 既存の `AgentService` インターフェースとの互換性を維持する。`HTTPHandler()` は既に `AgentService` インターフェースで定義済み

---

### hag パッケージ (統合)

#### [MODIFY] [options.go](file://shared/libs/go/hag/options.go)
*   **Description**: `WithAgentService` Option を追加
*   **Technical Design**:
    ```go
    // options struct に agentService フィールドを追加
    type options struct {
        // ... 既存フィールド
        agentService *agentservice.Server
    }

    // WithAgentService は外部で構築した AgentService を注入する。
    func WithAgentService(as *agentservice.Server) Option {
        return func(o *options) { o.agentService = as }
    }
    ```

#### [MODIFY] [server.go](file://shared/libs/go/hag/server.go)
*   **Description**: AgentService の初期化を強化
*   **Technical Design**:
    *   `New()` 内で `agentservice.New()` を `agentservice.New(agentservice.WithLogger(log), agentservice.WithTaskLog(tl), agentservice.WithGatewayURL(gatewayURL))` に変更
    *   `WithAgentService` Option が指定された場合はそれを使用
    *   `resolveAgentService()` ヘルパー関数を追加

## Step-by-Step Implementation Guide

1.  **Step 1: session_store_test.go + session_store.go (MemorySessionStore)**:
    *   テストを先に作成し、CRUD + ステータス遷移を検証
    *   `MemorySessionStore` を実装
    *   テスト Green を確認

2.  **Step 2: health_test.go + health.go (ヘルスチェック)**:
    *   LLMGP モックサーバーを使ったテストを作成
    *   `handleHealth()` + `checkGatewayHealth()` を実装
    *   テスト Green を確認

3.  **Step 3: handler_test.go + handler.go (HTTPハンドラ)**:
    *   テストを先に作成
    *   REST エンドポイント + SSE ストリーミングを実装
    *   テスト Green を確認

4.  **Step 4: log_stream_test.go + log_stream.go (ログSSE)**:
    *   テストを先に作成
    *   ログポーリング + SSE 配信を実装
    *   テスト Green を確認

5.  **Step 5: service.go の書き換え**:
    *   既存スタブ実装を本番実装に置き換える
    *   全テスト Green を確認

6.  **Step 6: hag/options.go + hag/server.go の修正**:
    *   `WithAgentService` Option を追加
    *   `New()` の AgentService 初期化を強化
    *   既存テスト (`hag/server_test.go`) が引き続き Green であることを確認

7.  **Step 7: ビルド検証**:
    *   Verification Plan を実行

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh --skip-frontend --skip-etc
    ```

2.  **Integration Tests** (Part1-3 全体完了後):
    ```bash
    ./scripts/process/build.sh --skip-frontend --skip-etc && ./scripts/process/integration_test.sh --categories "common" --specify "CodingAgent|AgentService|Session|Health"
    ```

### テスト項目のセルフレビュー結果

1.  **網羅性の検証**: agentservice の全エンドポイント (health, agents, sessions, messages, logs, terminate) にテストがある。MemorySessionStore の CRUD + ステータス遷移をカバー。ヘルスチェックの正常/LLMGP障害/タイムアウトの3パターンを検証。
2.  **証拠の十分性**: HTTPステータスコード、レスポンスボディの具体的なフィールド値、SSEイベント形式を検証。ヘルスチェックでは実際のHTTPクライアントを使用しモックサーバーと通信する。
3.  **迂回・抜け道の排除**: compile-time check で `MemorySessionStore` が `SessionStore` インターフェースを実装していることを保証。ハンドラテストは `httptest.NewRecorder` でリクエスト/レスポンスを完全にキャプチャ。
4.  **依存関係の整合性**: Part1 (codingagent) -> Part2 (adapters) -> Part3 (agentservice) のボトムアップ順。session_store -> health -> handler -> log_stream -> service の順でテスト。

### 総合判定プロセス

全テスト完了後、testing-rules.md 12 に従い、以下を確認する:
1. スキップされたテストがないこと
2. テストログに ERROR/WARN/panic がないこと
3. フォールバック経由の偽成功がないこと
4. 正しいアダプタ/ハンドラが使用されていること
5. テスト間の依存がないこと
6. 新規機能に対するカバレッジが十分であること
7. 外部システムの状態が正常であること

## 継続計画について

- **Part1 (009)**: `codingagent` パッケージのコア抽象層 -- 完了前提
- **Part2 (010)**: `claudecode/` + `codex/` Adapter実装 -- 完了前提
- **Part3 (本計画)**: `agentservice/` Web API + hag.Server統合

> **Note**: コンテナ構成 (`container/` フォルダ) と統合テスト (シナリオ6, 7) は、Part1-3 の実装が完了した後に別計画として作成する。
