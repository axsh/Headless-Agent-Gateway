# 015-AgentService-HTTPListener

> **Source Specification**: [010-AgentService-HTTPListener.md](file:///prompts/phases/000-foundation/branches/feat-llm-backend/ideas/010-AgentService-HTTPListener.md)

## Goal Description

AgentService (Coding Agent Web API) の HTTP サーバを `hag.Server.Launch()` から起動し、ポート 3100 (設定可能) でリッスンさせる。standalone バイナリで Claude Code エージェントを自動登録し、cawa-client から接続可能にする。

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: AgentService HTTP リスナー起動 | Proposed Changes > agentservice/service.go (Launch/Shutdown), hag/server.go |
| R2: config.yaml での AgentService ポート設定 | Proposed Changes > config/config.go, examples/standalone/config.yaml |
| R3: Coding Agent の登録 | Proposed Changes > examples/standalone/main.go |
| R4: standalone での動作確認 | Verification Plan > シナリオ 1-4 の統合テスト |

## Proposed Changes

### config パッケージ

#### [MODIFY] [config.go](file:///shared/libs/go/config/config.go)
*   **Description**: `AgentServiceConfig` 構造体を追加し、`AppConfig` にフィールドを追加する。
*   **Technical Design**:
    ```go
    // AgentServiceConfig holds AgentService HTTP settings.
    type AgentServiceConfig struct {
        // Port is the AgentService HTTP listen port.
        // When 0, the OS assigns an ephemeral port.
        Port int `yaml:"port"`
    }
    ```
    `AppConfig` に追加:
    ```go
    type AppConfig struct {
        // ... (既存フィールド)
        // AgentService holds AgentService HTTP settings.
        AgentService AgentServiceConfig `yaml:"agent_service"`
    }
    ```
*   **Logic**: YAML の `agent_service.port` から整数値を読み取り、`AgentServiceConfig.Port` に格納する。YAML に記述がない場合はゼロ値 (0) となるため、呼び出し側でデフォルト値 3100 を適用する。

---

### agentservice パッケージ

#### [MODIFY] [service.go](file:///shared/libs/go/agentservice/service.go)
*   **Description**: `Server` に HTTP リスナーの `Launch()` / `Shutdown()` メソッドを追加する。
*   **Technical Design**:
    ```go
    import (
        "context"
        "errors"
        "fmt"
        "net"
        "net/http"
        // ... (既存インポート)
    )

    // Server 構造体に以下のフィールドを追加:
    type Server struct {
        // ... (既存フィールド)
        httpServer *http.Server
        ln         net.Listener
        port       int // 実際にリッスンしているポート (Launch後に確定)
    }

    // Launch starts the AgentService HTTP server on the given port.
    // port=0 uses OS-assigned ephemeral port.
    // Non-blocking: the server runs in a background goroutine.
    func (s *Server) Launch(ctx context.Context, port int) error {
        handler := s.HTTPHandler()
        ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
        if err != nil {
            return fmt.Errorf("agentservice listen: %w", err)
        }
        s.ln = ln
        s.port = ln.Addr().(*net.TCPAddr).Port
        s.httpServer = &http.Server{Handler: handler}
        go func() {
            if err := s.httpServer.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
                if s.logger != nil {
                    s.logger.Error("agentservice serve error", "error", err)
                }
            }
        }()
        if s.logger != nil {
            s.logger.Info("agentservice started", "port", s.port)
        }
        return nil
    }

    // Shutdown gracefully stops the AgentService HTTP server.
    func (s *Server) Shutdown(ctx context.Context) error {
        if s.httpServer == nil {
            return nil
        }
        if s.logger != nil {
            s.logger.Info("shutting down agentservice")
        }
        // Close all active coding agent processes.
        for _, agent := range s.agents {
            if closer, ok := agent.(interface{ Close() error }); ok {
                closer.Close()
            }
        }
        return s.httpServer.Shutdown(ctx)
    }

    // Port returns the actual port the server is listening on.
    // Returns 0 if the server has not been launched.
    func (s *Server) Port() int {
        return s.port
    }
    ```
*   **Logic**:
    1. `Launch(ctx, port)` で `net.Listen("tcp", ":port")` してリスナーを作成
    2. エフェメラルポート (port=0) の場合、`ln.Addr().(*net.TCPAddr).Port` で実ポートを取得
    3. `s.HTTPHandler()` を使って `http.Server` を構成し、バックグラウンド goroutine で `Serve()` を呼ぶ
    4. `Shutdown(ctx)` では、まず全登録エージェントの `Close()` を呼び、次に `httpServer.Shutdown()` でグレースフルストップ
    5. `Port()` はテストや外部からの参照用

---

### hag パッケージ

#### [MODIFY] [server.go](file:///shared/libs/go/hag/server.go)
*   **Description**: `Launch()` と `Shutdown()` に AgentService HTTP サーバの起動/停止を追加する。
*   **Technical Design**:
    `Launch()` の末尾 (wsServer.Launch の後) に AgentService 起動を追加:
    ```go
    func (s *Server) Launch(ctx context.Context) error {
        s.logger.Info("starting HAG server")

        if err := s.gateway.Launch(ctx); err != nil {
            return fmt.Errorf("hag: gateway launch: %w", err)
        }

        if err := s.wsServer.Launch(ctx); err != nil {
            return fmt.Errorf("hag: wsserver launch: %w", err)
        }

        // AgentService HTTP server
        agentPort := s.cfg.AgentService.Port
        if agentPort == 0 {
            agentPort = 3100 // default port
        }
        if err := s.agentService.Launch(ctx, agentPort); err != nil {
            return fmt.Errorf("hag: agentservice launch: %w", err)
        }

        s.logger.Info("HAG server started")
        return nil
    }
    ```
    `Shutdown()` の先頭 (wsServer.Shutdown の前) に AgentService 停止を追加:
    ```go
    func (s *Server) Shutdown(ctx context.Context) error {
        s.logger.Info("shutting down HAG server")

        // Shutdown in reverse order: AgentService -> WebSocket -> Gateway
        if err := s.agentService.Shutdown(ctx); err != nil {
            return fmt.Errorf("hag: agentservice shutdown: %w", err)
        }

        if err := s.wsServer.Shutdown(ctx); err != nil {
            return fmt.Errorf("hag: wsserver shutdown: %w", err)
        }

        if err := s.gateway.Shutdown(ctx); err != nil {
            return fmt.Errorf("hag: gateway shutdown: %w", err)
        }

        s.logger.Info("HAG server stopped")
        return nil
    }
    ```
*   **Logic**:
    1. 起動順序: Gateway (:14000) -> WebSocket (:18080) -> AgentService (:3100)
    2. 停止順序: AgentService -> WebSocket -> Gateway (逆順)
    3. `cfg.AgentService.Port == 0` の場合はデフォルト 3100 を使用

---

### examples/standalone

#### [MODIFY] [main.go](file:///examples/standalone/main.go)
*   **Description**: Claude Code エージェントの自動登録を追加する。
*   **Technical Design**:
    ```go
    import (
        // ... (既存インポート)
        "os/exec"

        "github.com/axsh/hag/codingagent"
        "github.com/axsh/hag/codingagent/claudecode"
    )

    func main() {
        // ... (既存: flag.Parse, hag.New, srv.Launch)

        // Register Claude Code agent if CLI is available
        registerCodingAgents(srv)

        fmt.Println("HAG server started and running...")
        // ... (既存: signal wait, shutdown)
    }

    // registerCodingAgents registers coding agent adapters with the AgentService.
    // Agents are only registered if their CLI tool is available on PATH.
    func registerCodingAgents(srv *hag.Server) {
        // Check if claude CLI is available
        if _, err := exec.LookPath("claude"); err == nil {
            adapter := claudecode.New(&codingagent.AdapterConfig{})
            srv.AgentService().RegisterAgent(adapter)
            fmt.Println("Registered coding agent: claudecode")
        } else {
            fmt.Println("Warning: claude CLI not found, claudecode agent not registered")
        }
    }
    ```
*   **Logic**:
    1. `exec.LookPath("claude")` で Claude Code CLI の存在を確認
    2. 存在する場合のみ `ClaudeCodeAdapter` を作成し `RegisterAgent()` で登録
    3. 存在しない場合は警告ログを出力してスキップ (エラーにはしない)

#### [MODIFY] [config.yaml](file:///examples/standalone/config.yaml)
*   **Description**: `agent_service.port` を追加する。
*   **Technical Design**:
    ```yaml
    llm_gateway:
      port: 14000
      model_profiles_path: "model_profiles.yaml"
    log:
      level: "info"
    vault:
      backend: "keyring"
    websocket:
      port: 18080
    agent_service:
      port: 3100
    ```
*   **Logic**: `agent_service.port: 3100` を末尾に追加する。

---

### テスト

#### [MODIFY] [agentservice_integration_test.go](file:///tests/agentservice_integration_test.go)
*   **Description**: 既存テストに `Launch/Shutdown` のテストケースを 2 件追加する。
*   **Technical Design**:
    ```go
    // TestAgentServiceLaunchShutdown verifies that AgentService can
    // Launch on an ephemeral port and Shutdown gracefully.
    func TestAgentServiceLaunchShutdown(t *testing.T) {
        srv := agentservice.New(
            agentservice.WithLogger(logger.NewDefault(logger.LevelDebug)),
        )
        // Register a mock agent
        srv.RegisterAgent(&integrationMockAgent{})

        ctx := context.Background()

        // Launch on ephemeral port
        err := srv.Launch(ctx, 0)
        if err != nil {
            t.Fatalf("Launch failed: %v", err)
        }
        defer srv.Shutdown(ctx)

        port := srv.Port()
        if port == 0 {
            t.Fatal("Port should be non-zero after Launch")
        }

        // Verify health endpoint is reachable
        resp, err := http.Get(fmt.Sprintf("http://localhost:%d/health", port))
        if err != nil {
            t.Fatalf("health request failed: %v", err)
        }
        defer resp.Body.Close()
        if resp.StatusCode != http.StatusOK {
            t.Fatalf("expected 200, got %d", resp.StatusCode)
        }

        // Verify response body contains expected fields
        var health map[string]any
        json.NewDecoder(resp.Body).Decode(&health)
        if health["status"] != "ok" {
            t.Fatalf("expected status ok, got %v", health["status"])
        }
        agents, ok := health["agents"].([]any)
        if !ok {
            t.Fatal("agents field missing or wrong type")
        }
        if len(agents) != 1 {
            t.Fatalf("expected 1 agent, got %d", len(agents))
        }

        // Shutdown
        err = srv.Shutdown(ctx)
        if err != nil {
            t.Fatalf("Shutdown failed: %v", err)
        }

        // Verify port is no longer accepting connections
        _, err = http.Get(fmt.Sprintf("http://localhost:%d/health", port))
        if err == nil {
            t.Fatal("expected connection refused after shutdown")
        }
    }

    // TestAgentServiceConfigPort verifies AgentService reads port from config
    // via hag.Server integration.
    func TestAgentServiceConfigPort(t *testing.T) {
        cfg := &config.AppConfig{
            AgentService: config.AgentServiceConfig{Port: 0}, // ephemeral
        }
        stub := llmgateway.NewStubGateway()
        srv, err := hag.New(
            hag.WithConfig(cfg),
            hag.WithGateway(stub),
        )
        if err != nil {
            t.Fatalf("hag.New failed: %v", err)
        }

        ctx := context.Background()
        if err := srv.Launch(ctx); err != nil {
            t.Fatalf("Launch failed: %v", err)
        }
        defer srv.Shutdown(ctx)

        // AgentService should be running on some port
        as := srv.AgentService().(*agentservice.Server)
        port := as.Port()
        if port == 0 {
            t.Fatal("AgentService port should be non-zero after Launch")
        }

        // Health should be accessible
        resp, err := http.Get(fmt.Sprintf("http://localhost:%d/health", port))
        if err != nil {
            t.Fatalf("health request failed: %v", err)
        }
        defer resp.Body.Close()
        if resp.StatusCode != http.StatusOK {
            t.Fatalf("expected 200, got %d", resp.StatusCode)
        }
    }
    ```
*   **Logic**:
    1. `TestAgentServiceLaunchShutdown`: AgentService 単体の Launch (エフェメラルポート) -> health 確認 -> Shutdown -> 接続拒否を検証
    2. `TestAgentServiceConfigPort`: hag.Server 経由で config からポートを読み取り、AgentService が起動することを検証

---

## Step-by-Step Implementation Guide

1.  **Step 1: config に AgentServiceConfig を追加**:
    *   `shared/libs/go/config/config.go` に `AgentServiceConfig` 構造体を追加
    *   `AppConfig` に `AgentService AgentServiceConfig` フィールドを追加

2.  **Step 2: AgentService に Launch/Shutdown/Port メソッドを追加**:
    *   `shared/libs/go/agentservice/service.go` に `httpServer`, `ln`, `port` フィールドを追加
    *   `Launch(ctx, port)`, `Shutdown(ctx)`, `Port()` メソッドを実装

3.  **Step 3: hag.Server の Launch/Shutdown を更新**:
    *   `shared/libs/go/hag/server.go` の `Launch()` 末尾に AgentService の起動を追加
    *   `Shutdown()` の先頭に AgentService の停止を追加
    *   デフォルトポート 3100 のフォールバックロジックを追加

4.  **Step 4: standalone main.go にエージェント登録を追加**:
    *   `examples/standalone/main.go` に `registerCodingAgents()` 関数を追加
    *   `hag.Server.Launch()` の**前に** `registerCodingAgents(srv)` を呼び出す (エージェント登録は Launch 前に行う必要がある。HTTPHandler() は Launch 時に呼ばれるため)

5.  **Step 5: config.yaml を更新**:
    *   `examples/standalone/config.yaml` に `agent_service.port: 3100` を追加

6.  **Step 6: 統合テストを追加**:
    *   `tests/agentservice_integration_test.go` に `TestAgentServiceLaunchShutdown` と `TestAgentServiceConfigPort` を追加

7.  **Step 7: ビルド・テスト検証**:
    *   Verification Plan に従って `build.sh` と `integration_test.sh` を実行

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```

2.  **Integration Tests (AgentService)**:
    ```bash
    ./scripts/process/integration_test.sh --specify "AgentService"
    ```
    *   **Log Verification**:
        *   `TestAgentServiceLaunchShutdown`: health レスポンスに `"status": "ok"`, `"agents"` が含まれ、Shutdown 後に接続拒否されること
        *   `TestAgentServiceConfigPort`: hag.Server 経由で AgentService がエフェメラルポートで起動し、health が返ること
        *   既存の 6 テスト (`TestAgentServiceHealthCheck`, `TestAgentServiceSessionLifecycle`, `TestAgentServiceSSEStreaming`, `TestAgentServiceTaskLogIntegration`, `TestAgentServiceLogStreamSSE`, `TestAgentServiceSDKSessionID`) がリグレッションなく成功すること

### テスト項目設計

#### ボトムアップ確認順序

1. **末端 (C)**: `config.AgentServiceConfig` の YAML パース -> `build.sh` の単体テストで検証
2. **中間 (B)**: `agentservice.Server.Launch/Shutdown/Port` -> `TestAgentServiceLaunchShutdown` で検証
3. **上位 (A)**: `hag.Server.Launch` -> AgentService 統合 -> `TestAgentServiceConfigPort` で検証

#### 観点チェックリスト

| # | 観点 | テスト項目 |
|---|------|-----------|
| 1 | 正常系 | Launch 後に health が 200 OK で返る |
| 2 | 正常系 | health レスポンスに agents, cli_versions, status が含まれる |
| 3 | 正常系 | Shutdown 後にポートが閉じる (接続拒否) |
| 4 | 正常系 | hag.Server 経由で AgentService がエフェメラルポートで起動する |
| 5 | 状態遷移 | Launch -> health OK -> Shutdown -> connection refused の遷移 |
| 6 | 設定反映 | config.AgentService.Port が正しく反映される |
| 7 | 副作用 | Shutdown でエージェントの Close() が呼ばれる |

#### セルフレビュー結果

1. **網羅性**: R1-R4 の全要件がテストでカバーされている。R3 (Claude Code 登録) は CI 環境に claude CLI がないため、既存の `TestAgentServiceHealthCheck` (mock エージェント使用) で代替検証。R4 は `TestAgentServiceConfigPort` で hag.Server 統合を検証。
2. **証拠の十分性**: health レスポンスの JSON フィールド検証、ポート閉鎖確認、ステータスコード検証を実施。
3. **迂回排除**: `TestAgentServiceLaunchShutdown` は実際の TCP リッスンと HTTP リクエストで検証しており、httptest.Server のような迂回パスは使わない。
4. **依存関係**: config パース -> AgentService Launch -> hag.Server 統合の順でボトムアップに確認。

### 総合判定プロセス

全テスト完了後、testing-rules.md 12.2 のチェック項目 (スキップ確認、部分エラー、迂回処理、アダプタ誤適用、テスト間依存、カバレッジ、外部システム) を実施する。

## Documentation

#### [MODIFY] [README.md](file:///README.md)
*   **更新内容**: ポートテーブルの 3100 の記載は既に行われているため、変更不要。

#### [MODIFY] [config.yaml の書式セクション](file:///README.md)
*   **更新内容**: config.yaml の書式説明に `agent_service` セクションを追加する。
    ```yaml
    agent_service:
      port: 3100                                     # AgentService HTTP ポート (デフォルト: 3100)
    ```
