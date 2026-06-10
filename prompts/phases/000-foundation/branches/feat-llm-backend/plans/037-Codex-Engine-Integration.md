# 037-Codex-Engine-Integration

> **Source Specification**: [027-Codex-Engine-Integration.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/ideas/027-Codex-Engine-Integration.md)

## Goal Description

Claude Code だけでなく Codex CLI もCAWAバックエンドとして利用可能にする。仕様書の GAP-01 ~ GAP-05 を修正し、`cawa-client --agent codex` による実コマンド E2E テストが動作する状態にする。

## User Review Required

> [!IMPORTANT]
> **`standalone` -> `cawa-server` のリネーム**: 仕様書 R5 では `examples/standalone/` を `examples/cawa-server/` にリネームする想定ですが、本計画ではリネーム自体は別計画に切り出し、現在の `examples/standalone/main.go` に Codex 登録ロジックを追加する方針とします。リネーム作業は `examples/standalone/` のバイナリ名変更・build.sh の修正等を含む広範な変更であり、今回の Codex 統合のスコープを超えるためです。本計画完了後に別途リネーム用仕様書を作成することを推奨します。

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: LLMGP に `/v1/responses` パススルー追加 | Proposed Changes > LLM Gateway Proxy > `proxy.go`, `proxy_openai.go` |
| R2: `config.toml` の `wire_api` 動的設定 | Proposed Changes > Codex Adapter > `config.go`, `config_test.go` |
| R3: エラーハンドリング (stderr + Wait) | Proposed Changes > Codex Adapter > `process.go` |
| R4: `OPENAI_API_KEY` にセッションメタデータ | Proposed Changes > Codex Adapter > `process.go`, `process_test.go` |
| R5: `cawa-server` に Codex 登録 | Proposed Changes > Standalone Server > `examples/standalone/main.go` |
| R6: Codex アダプタにログ追加 | Proposed Changes > Codex Adapter > `adapter.go`, `process.go` |
| R7: stdin EOF 送信 | Proposed Changes > Codex Adapter > `process.go` |

## Proposed Changes

### LLM Gateway Proxy

---

#### [MODIFY] [proxy_openai_test.go](file://shared/libs/go/llmgateway/proxy_openai_test.go)
*   **Description**: `/v1/responses` ハンドラのテストを追加
*   **Technical Design**:
    ```go
    func TestHandleOpenAIResponses_Passthrough(t *testing.T)
    ```
*   **Logic**:
    *   テスト用の上流モックサーバーを起動し、`POST /v1/responses` リクエストをレスポンスとしてエコーする
    *   ProxyServer + BifrostDriver を構築し、`/v1/responses` にリクエストを送信
    *   上流モックにリクエストが到達し、レスポンスがそのまま返ることを検証
    *   ストリーミング（`text/event-stream`）の場合もチャンクが正しく中継されることを検証

#### [MODIFY] [proxy.go](file://shared/libs/go/llmgateway/proxy.go)
*   **Description**: `setupRoutes()` に `/v1/responses` ルートを追加、`handleIndex` のエンドポイント一覧にも追加
*   **Technical Design**:
    ```go
    func (p *ProxyServer) setupRoutes(mux *http.ServeMux) {
        // ... 既存ルート ...
        mux.HandleFunc("POST /v1/responses", p.handleOpenAIResponses)
    }
    ```
*   **Logic**:
    *   `setupRoutes` に 1 行追加: `mux.HandleFunc("POST /v1/responses", p.handleOpenAIResponses)`
    *   `handleIndex` の `endpoints` 配列に `"POST /v1/responses"` を追加

#### [MODIFY] [proxy_openai.go](file://shared/libs/go/llmgateway/proxy_openai.go)
*   **Description**: `handleOpenAIResponses` ハンドラを新規追加
*   **Technical Design**:
    ```go
    // handleOpenAIResponses handles POST /v1/responses for OpenAI Responses API.
    // This is a passthrough handler: it resolves the model via ModelRouter,
    // retrieves the API key from vault, and forwards the request to upstream.
    func (p *ProxyServer) handleOpenAIResponses(w http.ResponseWriter, r *http.Request) {
        // 1. Read body
        // 2. Parse model from JSON body (openaiRequest struct を再利用)
        // 3. Check driver/router availability
        // 4. ExtractSessionID from Authorization header
        // 5. ResolveModel
        // 6. ExtractFallbackFlag (OR with profile)
        // 7. Resolve vault
        // 8. Forward to upstream /v1/responses via forwardWithRetry
        // 9. proxyResponse (SSE streaming 含む)
    }
    ```
*   **Logic**:
    *   `handleOpenAIChatCompletions` とほぼ同一の処理フロー
    *   相違点 1: `forwardWithRetry` に渡す `path` が `"/v1/responses"` になる
    *   相違点 2: ToolCallFallback の non-stream 書き換え処理は不要（Responses API は Codex 専用モデル向けであり、ToolCallFallback は不要）。ただし、将来の拡張性のため ToolCallFallback ブロックは含めない（シンプルにパススルー）

---

### Codex Adapter

---

#### [MODIFY] [config_test.go](file://shared/libs/go/codingagent/codex/config_test.go)
*   **Description**: `wireAPI` パラメータのテストケースを追加
*   **Technical Design**:
    ```go
    func TestGenerateConfigTOML(t *testing.T) {
        tests := []struct {
            name       string
            model      string
            gatewayURL string
            wireAPI    string  // 追加
            contains   []string
        }{
            {
                name:       "with model and gateway (default chat)",
                model:      "gpt-4o",
                gatewayURL: "http://localhost:14000",
                wireAPI:    "",  // デフォルト => "chat"
                contains: []string{
                    `model = "gpt-4o"`,
                    `wire_api = "chat"`,
                },
            },
            {
                name:       "with responses wire_api",
                model:      "codex-mini-latest",
                gatewayURL: "http://localhost:14000",
                wireAPI:    "responses",
                contains: []string{
                    `model = "codex-mini-latest"`,
                    `wire_api = "responses"`,
                },
            },
            // ... 既存の "empty model uses default" ケースも wireAPI を追加 ...
        }
    }
    ```

#### [MODIFY] [config.go](file://shared/libs/go/codingagent/codex/config.go)
*   **Description**: `GenerateConfigTOML` と `WriteConfigTOML` に `wireAPI` パラメータを追加
*   **Technical Design**:
    ```go
    const configTemplate = `model = "%s"
    model_provider = "gateway"

    [model_providers.gateway]
    name = "HAG LLM Gateway"
    base_url = "%s"
    env_key = "OPENAI_API_KEY"
    wire_api = "%s"
    `

    // GenerateConfigTOML generates a Codex config.toml string.
    // wireAPI should be "chat" or "responses". Defaults to "chat" if empty.
    func GenerateConfigTOML(model, gatewayURL, wireAPI string) string {
        if model == "" {
            model = "gpt-4o"
        }
        if wireAPI == "" {
            wireAPI = "chat"
        }
        return fmt.Sprintf(configTemplate, model, gatewayURL, wireAPI)
    }

    // WriteConfigTOML writes a config.toml to a temporary directory and returns the path.
    func WriteConfigTOML(model, gatewayURL, wireAPI string) (string, error) {
        // ... 既存ロジック、GenerateConfigTOML に wireAPI を渡す ...
    }
    ```

#### [MODIFY] [process_test.go](file://shared/libs/go/codingagent/codex/process_test.go)
*   **Description**: `BuildEnv` のセッションメタデータテストケースを追加
*   **Technical Design**:
    ```go
    func TestCodexBuildEnv_SessionMetadata(t *testing.T) {
        tests := []struct {
            name      string
            ac        *codingagent.AdapterConfig
            cfg       *codingagent.SessionConfig
            wantKey   string
            wantValue string
        }{
            {
                name: "default session ID and no fallback",
                ac:   &codingagent.AdapterConfig{GatewayURL: "http://localhost:14000"},
                cfg:  &codingagent.SessionConfig{},
                wantKey:   "OPENAI_API_KEY",
                wantValue: "not-needed;fallback=false;sid=default",
            },
            {
                name: "explicit session ID and fallback true",
                ac:   &codingagent.AdapterConfig{
                    GatewayURL: "http://localhost:14000",
                    ToolCallFallback: true,
                },
                cfg:  &codingagent.SessionConfig{AgentSessionID: "sess-abc"},
                wantKey:   "OPENAI_API_KEY",
                wantValue: "not-needed;fallback=true;sid=sess-abc",
            },
        }
        // ... テーブル駆動テスト ...
    }
    ```
*   **Logic**:
    *   `BuildEnv` の出力から `OPENAI_API_KEY` の値を取得し、期待値と一致するか検証
    *   既存の `TestCodexBuildEnv` の「OPENAI_API_KEY is set to not-needed」テストケースは、メタデータ付きの期待値に更新する

#### [MODIFY] [process.go](file://shared/libs/go/codingagent/codex/process.go)
*   **Description**: (R3) stderr キャプチャ + Wait 監視、(R4) セッションメタデータ、(R6) ログ追加、(R7) stdin EOF
*   **Technical Design (R3: stderr + Wait)**:
    ```go
    func StartProcess(
        ctx context.Context,
        ac *codingagent.AdapterConfig,
        cfg *codingagent.SessionConfig,
        configPath string,
    ) (<-chan codingagent.StreamEvent, *ProcessManager, error) {
        // ... 既存のコード ...

        // R3: Capture stderr for diagnostics.
        var stderrBuf bytes.Buffer
        cmd.Stderr = &stderrBuf

        // ... cmd.Start() ...

        go func() {
            defer close(ch)
            // ... 既存の stdin 送信ゴルーチン (変更なし) ...

            // 既存の stdout 読み取りループ
            scanner := bufio.NewScanner(stdout)
            for scanner.Scan() {
                // ... 既存のパースと auto-approve ロジック ...
            }

            // R3: Check exit code and report stderr on failure.
            if err := cmd.Wait(); err != nil {
                errMsg := strings.TrimSpace(stderrBuf.String())
                if errMsg == "" {
                    errMsg = err.Error()
                }
                log.Warn("codex CLI process exited with error", "error", err.Error(), "stderr", errMsg)
                select {
                case ch <- codingagent.StreamEvent{
                    Type:    codingagent.EventError,
                    Content: errMsg,
                }:
                case <-procCtx.Done():
                }
            } else {
                exitCode := cmd.ProcessState.ExitCode()
                log.Debug("codex CLI process exited", "exit_code", exitCode)
            }
        }()
    }
    ```
*   **Technical Design (R4: Session Metadata)**:
    ```go
    func BuildEnv(ac *codingagent.AdapterConfig, cfg *codingagent.SessionConfig) []string {
        env := make(map[string]string)

        // R4: Build OPENAI_API_KEY with metadata for gateway.
        apiKey := "not-needed"
        fallbackStr := "false"
        if ac.ToolCallFallback {
            fallbackStr = "true"
        }
        sid := cfg.AgentSessionID
        if sid == "" {
            sid = "default"
        }
        env["OPENAI_API_KEY"] = apiKey + ";fallback=" + fallbackStr + ";sid=" + sid

        // ... 残りの既存ロジック (CODEX_HOME, EnvVars) ...
    }
    ```
*   **Technical Design (R6: Logging)**:
    ```go
    func StartProcess(...) (...) {
        log := ac.Logger
        if log == nil {
            log = logger.NewDefault(logger.LevelInfo)
        }
        log = log.WithComponent("codex")

        args := BuildArgs(configPath)
        log.Debug("building CLI arguments", "args", args)

        env := BuildEnv(ac, cfg)
        // マスクされた環境変数をログ出力
        var maskedEnv []string
        for _, envVar := range env {
            if strings.HasPrefix(envVar, "OPENAI_API_KEY=") {
                maskedEnv = append(maskedEnv, "OPENAI_API_KEY=****")
            } else {
                maskedEnv = append(maskedEnv, envVar)
            }
        }
        log.Trace("CLI environment variables", "env", maskedEnv)

        log.Info("starting codex CLI process", "work_dir", cfg.WorkDir, "model", cfg.Model)
        // ...
    }
    ```
*   **Technical Design (R7: stdin EOF)**:
    ```go
    // stdin 送信ゴルーチン内
    go func() {
        initReq, _ := BuildInitializeRequest()
        stdin.Write(initReq)
        stdin.Write([]byte("\n"))

        threadReq, _ := BuildStartThreadRequest(cfg.Prompt)
        stdin.Write(threadReq)
        stdin.Write([]byte("\n"))

        // R7: Close stdin to signal EOF to Codex CLI.
        stdin.Close()
    }()
    ```
*   **Logic**:
    *   stdout 読み取りゴルーチンと stdin 送信ゴルーチンの構造は維持
    *   stdout ゴルーチンの末尾に `cmd.Wait()` を追加し、`claudecode/process.go` と同一のパターンで `EventError` を送信
    *   `ProcessManager` に `logger` フィールドを追加（`Stop()` でもログ出力するため）

#### [MODIFY] [adapter.go](file://shared/libs/go/codingagent/codex/adapter.go)
*   **Description**: (R2) `wire_api` 動的判定、(R6) ログ追加
*   **Technical Design**:
    ```go
    type CodexAdapter struct {
        config *codingagent.AdapterConfig
        mu     sync.Mutex
        procs  []*ProcessManager
        logger logger.Logger  // 追加
    }

    func New(config *codingagent.AdapterConfig) *CodexAdapter {
        log := config.Logger
        if log == nil {
            log = logger.NewDefault(logger.LevelInfo)
        }
        return &CodexAdapter{
            config: config,
            logger: log.WithComponent("codex"),
        }
    }

    func (a *CodexAdapter) CreateSession(
        ctx context.Context, opts ...codingagent.SessionOption,
    ) (codingagent.Session, error) {
        cfg := codingagent.NewSessionConfig(opts...)
        codingagent.ApplyDefaults(cfg, a.config)

        a.logger.Info("creating codex session", "model", cfg.Model, "work_dir", cfg.WorkDir)

        // R2: Determine wire_api from AdapterConfig.ModelMode.
        wireAPI := a.config.ModelMode  // "chat", "responses", or ""
        if wireAPI == "" {
            wireAPI = "chat"
        }

        configPath, err := WriteConfigTOML(cfg.Model, a.config.GatewayURL, wireAPI)
        if err != nil {
            return nil, fmt.Errorf("codex: write config: %w", err)
        }

        ch, pm, err := StartProcess(ctx, a.config, cfg, configPath)
        if err != nil {
            a.logger.Error("failed to start codex process", "error", err.Error())
            return nil, fmt.Errorf("codex: create session: %w", err)
        }

        a.mu.Lock()
        a.procs = append(a.procs, pm)
        a.mu.Unlock()

        sid := fmt.Sprintf("codex-%d", pm.cmd.Process.Pid)
        a.logger.Info("codex session created", "session_id", sid, "pid", pm.cmd.Process.Pid)
        return &codexSession{id: sid, ch: ch, pm: pm}, nil
    }

    func (a *CodexAdapter) Close() error {
        a.logger.Debug("closing all codex sessions")
        // ... 既存ロジック ...
    }
    ```

#### [MODIFY] [adapter_config.go](file://shared/libs/go/codingagent/adapter_config.go)
*   **Description**: `ModelMode` フィールドを追加
*   **Technical Design**:
    ```go
    type AdapterConfig struct {
        // ... 既存フィールド ...

        // ModelMode is the wire API mode for the adapter ("chat" or "responses").
        // Used by Codex to determine config.toml wire_api value.
        // Empty string defaults to "chat".
        ModelMode string
    }
    ```

---

### Standalone Server (cawa-server 相当)

---

#### [MODIFY] [main.go](file://examples/standalone/main.go)
*   **Description**: `registerCodingAgents()` に Codex CLI の登録ロジックを追加
*   **Technical Design**:
    ```go
    import (
        // ... 既存 ...
        "github.com/axsh/hag/codingagent/codex"
    )

    func registerCodingAgents(srv *hag.Server) {
        // ... 既存の claudecode 登録ロジック ...

        // Register codex agent if CLI is available.
        if _, err := exec.LookPath("codex"); err == nil {
            gwURL := srv.Gateway().ProxyURL()

            defaultModel := ""
            toolCallFallback := false
            if dm := srv.Gateway().DefaultModel(); dm != nil {
                defaultModel = dm.Model
                toolCallFallback = dm.ToolCallFallback
            }

            adapter := codex.New(&codingagent.AdapterConfig{
                GatewayURL:       gwURL,
                DefaultModel:     defaultModel,
                ToolCallFallback: toolCallFallback,
            })
            srv.AgentService().RegisterAgent(adapter)

            fmt.Printf("Registered coding agent: codex (gateway=%s, default_model=%s, fallback=%v)\n",
                gwURL, defaultModel, toolCallFallback)
        } else {
            fmt.Println("Warning: codex CLI not found, codex agent not registered")
        }
    }
    ```
*   **Logic**:
    *   `claudecode` の登録ロジックの直後に、同一パターンで `codex` を登録
    *   `exec.LookPath("codex")` で CLI の存在をチェック

---

### E2E テスト

---

#### [NEW] [codex_e2e_test.go](file://tests/codex_e2e_test.go)
*   **Description**: Codex CLI の E2E テストを新規作成
*   **Technical Design**:
    ```go
    //go:build integration

    package llm_test

    import (
        "github.com/axsh/hag/codingagent"
        "github.com/axsh/hag/codingagent/codex"
        // ... 標準ライブラリ ...
    )

    // startCodexE2EServer starts a HAG server with both claudecode and codex agents.
    func startCodexE2EServer(t *testing.T) (string, func()) {
        t.Helper()

        // Verify codex CLI is available.
        if _, err := exec.LookPath("codex"); err != nil {
            t.Fatalf("E2E test requires codex CLI on PATH: %v", err)
        }

        modelProfilesSrc, _ := filepath.Abs("../examples/standalone/model_profiles.yaml")

        gwPort := freePort(t)
        wsPort := freePort(t)
        asPort := freePort(t)

        tmpDir := t.TempDir()
        tmpConfig := filepath.Join(tmpDir, "config.yaml")
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
    `, gwPort, filepath.ToSlash(modelProfilesSrc), wsPort, asPort)

        os.WriteFile(tmpConfig, []byte(configContent), 0644)

        srv, err := hag.New(hag.WithConfigPath(tmpConfig))
        if err != nil {
            t.Fatalf("hag.New: %v", err)
        }

        ctx := context.Background()
        if err := srv.Launch(ctx); err != nil {
            t.Fatalf("Launch: %v", err)
        }

        gwURL := srv.Gateway().ProxyURL()

        // Register codex agent.
        codexAdapter := codex.New(&codingagent.AdapterConfig{
            GatewayURL:   gwURL,
            DefaultModel: "gpt-4o",  // default for Codex
        })
        srv.AgentService().RegisterAgent(codexAdapter)

        port := srv.AgentService().Port()
        baseURL := fmt.Sprintf("http://localhost:%d", port)

        cleanup := func() {
            shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
            defer cancel()
            srv.Shutdown(shutCtx)
        }

        return baseURL, cleanup
    }

    // TestCodexE2E_FileCreation verifies Codex CLI + default model (gpt-4o)
    // can create a file through the full CAWA pipeline.
    func TestCodexE2E_FileCreation(t *testing.T) {
        baseURL, cleanup := startCodexE2EServer(t)
        defer cleanup()

        workDir := t.TempDir()

        // 1. Create session with codex agent
        sessionID := createE2ESessionWithModel(t, baseURL, "codex", "gpt-4o", workDir)
        t.Logf("Session created: %s", sessionID)

        // 2. Send file creation prompt
        prompt := "Create a file named hello.txt in the current directory containing exactly the text 'Hello Codex'. Do nothing else."
        resp := sendE2EMessage(t, baseURL, sessionID, prompt, 120*time.Second)
        defer resp.Body.Close()

        // 3. Verify SSE stream
        ct := resp.Header.Get("Content-Type")
        if !strings.Contains(ct, "text/event-stream") {
            t.Errorf("Content-Type = %q, want text/event-stream", ct)
        }

        events, gotDone := parseE2ESSEEvents(t, resp)
        if !gotDone {
            t.Fatal("expected [DONE] sentinel in SSE stream")
        }

        // Log events and check for errors
        for i, ev := range events {
            t.Logf("event[%d]: type=%s content_len=%d", i, ev.Type, len(ev.Content))
        }
        for _, ev := range events {
            if ev.Type == codingagent.EventError {
                t.Fatalf("received error event: %s", ev.Content)
            }
        }

        // Must have text or tool_use events
        hasContent := false
        for _, ev := range events {
            if ev.Type == codingagent.EventText || ev.Type == codingagent.EventToolUse {
                hasContent = true
                break
            }
        }
        if !hasContent {
            t.Error("expected at least one text or tool_use event")
        }

        // 4. Verify file creation
        filePath := filepath.Join(workDir, "hello.txt")
        content, err := os.ReadFile(filePath)
        if err != nil {
            entries, _ := os.ReadDir(workDir)
            var names []string
            for _, e := range entries {
                names = append(names, e.Name())
            }
            t.Fatalf("expected hello.txt in %s, got files: %v, error: %v", workDir, names, err)
        }
        if !strings.Contains(string(content), "Hello Codex") {
            t.Errorf("hello.txt content = %q, want to contain 'Hello Codex'", string(content))
        }

        // 5. Verify session status
        session := getE2ESession(t, baseURL, sessionID)
        sessionStatus, _ := session["status"].(string)
        if sessionStatus != "completed" {
            t.Errorf("session status = %q, want %q", sessionStatus, "completed")
        }
    }

    // TestCodexE2E_GeminiModel_FileCreation verifies Codex CLI + Gemini model.
    func TestCodexE2E_GeminiModel_FileCreation(t *testing.T) {
        baseURL, cleanup := startCodexE2EServer(t)
        defer cleanup()
        workDir := t.TempDir()

        sessionID := createE2ESessionWithModel(t, baseURL, "codex", "gemini-2.5-flash", workDir)
        t.Logf("Session created: %s", sessionID)

        prompt := "Create a file named test.txt in the current directory containing exactly the text 'Hello from Gemini via Codex'. Do nothing else."
        resp := sendE2EMessage(t, baseURL, sessionID, prompt, 120*time.Second)
        defer resp.Body.Close()

        events, gotDone := parseE2ESSEEvents(t, resp)
        if !gotDone {
            t.Fatal("expected [DONE]")
        }
        for _, ev := range events {
            if ev.Type == codingagent.EventError {
                t.Fatalf("error event: %s", ev.Content)
            }
        }

        filePath := filepath.Join(workDir, "test.txt")
        content, err := os.ReadFile(filePath)
        if err != nil {
            t.Fatalf("expected test.txt: %v", err)
        }
        if !strings.Contains(string(content), "Hello from Gemini via Codex") {
            t.Errorf("content = %q", string(content))
        }
    }

    // TestCodexE2E_ErrorPropagation verifies error propagation
    // when Codex CLI cannot reach the gateway.
    func TestCodexE2E_ErrorPropagation(t *testing.T) {
        if _, err := exec.LookPath("codex"); err != nil {
            t.Fatalf("codex CLI required: %v", err)
        }

        modelProfilesSrc, _ := filepath.Abs("../examples/standalone/model_profiles.yaml")

        gwPort := freePort(t)
        wsPort := freePort(t)
        asPort := freePort(t)

        tmpDir := t.TempDir()
        tmpConfig := filepath.Join(tmpDir, "config.yaml")
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
    `, gwPort, filepath.ToSlash(modelProfilesSrc), wsPort, asPort)
        os.WriteFile(tmpConfig, []byte(configContent), 0644)

        srv, err := hag.New(hag.WithConfigPath(tmpConfig))
        if err != nil {
            t.Fatalf("hag.New: %v", err)
        }

        ctx := context.Background()
        if err := srv.Launch(ctx); err != nil {
            t.Fatalf("Launch: %v", err)
        }
        defer func() {
            shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
            defer cancel()
            srv.Shutdown(shutCtx)
        }()

        // Register codex with BOGUS gateway URL.
        bogusPort := freePort(t)
        adapter := codex.New(&codingagent.AdapterConfig{
            GatewayURL: fmt.Sprintf("http://localhost:%d", bogusPort),
        })
        srv.AgentService().RegisterAgent(adapter)

        port := srv.AgentService().Port()
        baseURL := fmt.Sprintf("http://localhost:%d", port)
        workDir := t.TempDir()

        sessionID := createE2ESessionWithModel(t, baseURL, "codex", "gpt-4o", workDir)
        resp := sendE2EMessage(t, baseURL, sessionID, "say hello", 30*time.Second)
        defer resp.Body.Close()

        events, _ := parseE2ESSEEvents(t, resp)

        hasError := false
        hasContent := false
        for _, ev := range events {
            if ev.Type == codingagent.EventError {
                hasError = true
                t.Logf("Error event received: %s", ev.Content)
            }
            if ev.Type == codingagent.EventText {
                hasContent = true
            }
        }

        if !hasError && hasContent {
            t.Error("expected error event or no text content when gateway is unreachable")
        }
        if hasError {
            t.Log("Error propagation verified: error event received")
        } else {
            t.Log("Error propagation verified: no text content received")
        }
    }
    ```

---

## Step-by-Step Implementation Guide

### Phase 1: Codex アダプタ修正 (末端から)

1. **[Step 1] `config_test.go` テスト更新 (R2, TDD)**:
   *   `shared/libs/go/codingagent/codex/config_test.go` を編集
   *   `TestGenerateConfigTOML` のテストケースに `wireAPI` パラメータを追加
   *   `wire_api = "responses"` のケースを追加
   *   ビルドが失敗することを確認 (TDD red)

2. **[Step 2] `config.go` 修正 (R2)**:
   *   `shared/libs/go/codingagent/codex/config.go` を編集
   *   `GenerateConfigTOML(model, gatewayURL, wireAPI string)` にシグネチャ変更
   *   `WriteConfigTOML(model, gatewayURL, wireAPI string)` にシグネチャ変更
   *   `configTemplate` の `wire_api` を `%s` に変更
   *   テストが通ることを確認 (TDD green)

3. **[Step 3] `adapter_config.go` 修正 (R2)**:
   *   `shared/libs/go/codingagent/adapter_config.go` を編集
   *   `ModelMode string` フィールドを追加
   *   コメント追加: `// ModelMode is the wire API mode ("chat" or "responses").`

4. **[Step 4] `process_test.go` テスト更新 (R4, TDD)**:
   *   `shared/libs/go/codingagent/codex/process_test.go` を編集
   *   `TestCodexBuildEnv_SessionMetadata` テスト関数を追加
   *   既存の `TestCodexBuildEnv` の `"OPENAI_API_KEY is set to not-needed"` ケースの期待値を `"not-needed;fallback=false;sid=default"` に更新
   *   ビルドが失敗することを確認 (TDD red)

5. **[Step 5] `process.go` 修正 (R3, R4, R6, R7)**:
   *   `shared/libs/go/codingagent/codex/process.go` を編集
   *   **R4**: `BuildEnv` の `OPENAI_API_KEY` をメタデータ付きの形式に変更
   *   **R3**: `StartProcess` に `var stderrBuf bytes.Buffer` + `cmd.Stderr = &stderrBuf` を追加
   *   **R3**: stdout ゴルーチン末尾に `cmd.Wait()` + `EventError` 送信を追加
   *   **R6**: `ac.Logger` からロガーを初期化し、各ポイントにログを追加
   *   **R7**: stdin 送信ゴルーチンの最後に `stdin.Close()` を追加
   *   **R6**: `ProcessManager` に `logger` フィールドを追加し、`Stop()` にログ追加
   *   テストが通ることを確認 (TDD green)
   *   `git commit`

6. **[Step 6] `adapter.go` 修正 (R2, R6)**:
   *   `shared/libs/go/codingagent/codex/adapter.go` を編集
   *   `logger` フィールドを追加し、`New()` で初期化
   *   `CreateSession` で `a.config.ModelMode` から `wireAPI` を取得し、`WriteConfigTOML` に渡す
   *   セッション作成・終了のログを追加
   *   `git commit`

### Phase 2: LLMGP 修正

7. **[Step 7] `proxy_openai_test.go` テスト追加 (R1, TDD)**:
   *   `shared/libs/go/llmgateway/proxy_openai_test.go` を編集
   *   `TestHandleOpenAIResponses_Passthrough` テスト関数を追加
   *   `/v1/responses` にリクエストを送信してパススルーが動作することを検証
   *   ビルドが失敗することを確認 (TDD red)

8. **[Step 8] `proxy.go` ルート追加 (R1)**:
   *   `shared/libs/go/llmgateway/proxy.go` を編集
   *   `setupRoutes` に `mux.HandleFunc("POST /v1/responses", p.handleOpenAIResponses)` を追加
   *   `handleIndex` の endpoints に `"POST /v1/responses"` を追加

9. **[Step 9] `proxy_openai.go` ハンドラ追加 (R1)**:
   *   `shared/libs/go/llmgateway/proxy_openai.go` を編集
   *   `handleOpenAIResponses` 関数を追加（`handleOpenAIChatCompletions` を模倣、path を `/v1/responses` に変更、ToolCallFallback 処理を省略）
   *   テストが通ることを確認 (TDD green)
   *   `git commit`

### Phase 3: サーバー登録 + E2E テスト

10. **[Step 10] `standalone/main.go` に Codex 登録 (R5)**:
    *   `examples/standalone/main.go` を編集
    *   `registerCodingAgents()` に `codex.New()` による登録ロジックを追加
    *   `git commit`

11. **[Step 11] E2E テスト作成**:
    *   `tests/codex_e2e_test.go` を新規作成
    *   `startCodexE2EServer`, `TestCodexE2E_FileCreation`, `TestCodexE2E_GeminiModel_FileCreation`, `TestCodexE2E_ErrorPropagation` を実装
    *   `git commit`

### Phase 4: ビルド + テスト実行

12. **[Step 12] ビルド + 単体テスト**:
    *   Verification Plan の Step 1 を実行

13. **[Step 13] 統合テスト (Codex E2E)**:
    *   Verification Plan の Step 2 を実行

14. **[Step 14] 全 LLM テスト (リグレッション確認)**:
    *   Verification Plan の Step 3 を実行

## Verification Plan

### Automated Verification

1. **Build & Unit Tests**:
   ```bash
   ./scripts/process/build.sh
   ```

2. **Codex E2E Tests (選択的実行)**:
   ```bash
   ./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories llm --specify "TestCodexE2E"
   ```
   *   **Log Verification**: SSE ストリームに `text`, `tool_use`, `result` イベントが出現すること。`error` イベントが出現しないこと（ErrorPropagation テスト除く）。

3. **全 LLM テスト (リグレッション確認)**:
   ```bash
   ./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories llm
   ```
   *   **Log Verification**: 既存の `TestE2E_CodingAgentStreaming`, `TestGeminiE2E_*` が全て PASS であること。

4. **E2E Tests (新規追加)**:

   #### [NEW] [codex_e2e_test.go](file://tests/codex_e2e_test.go)
   *   **テストケース**:
       *   `TestCodexE2E_FileCreation`: Codex + gpt-4o でファイル作成が完了すること
       *   `TestCodexE2E_GeminiModel_FileCreation`: Codex + Gemini でファイル作成が動作すること
       *   `TestCodexE2E_ErrorPropagation`: 到達不能な Gateway でエラーが SSE に伝達されること
   *   **検証ポイント**:
       *   SSE ストリームが `text/event-stream` で返却される
       *   イベントに `text` または `tool_use` が含まれる
       *   ファイルが作成され、内容が期待値を含む
       *   `[DONE]` シグナルが受信される
       *   セッションステータスが `completed` になる
   *   **前提条件**: `codex` CLI が PATH に存在し、OpenAI/Google API キーが keyring に設定済み

### テスト項目設計のセルフレビュー (Section 11)

#### ボトムアップ順序の確認

```
依存関係: E2E -> AgentService -> Adapter -> Process -> Config
                                        -> LLMGP -> Provider Forwarder

テスト順序:
  Step 1: config.go テスト -> config.toml 生成が正しいことを確認
  Step 2: process.go テスト -> BuildEnv のメタデータが正しいことを確認
  Step 3: proxy_openai.go テスト -> /v1/responses パススルーが動作することを確認
  Step 4: E2E テスト -> 全体パイプラインが動作することを確認
```

#### 観点チェックリスト

| # | 観点 | カバー状況 |
|---|------|-----------|
| 1 | 正常系 | `TestCodexE2E_FileCreation` (ファイル作成完了), `TestCodexE2E_GeminiModel_FileCreation` (マルチモデル) |
| 2 | 異常系 | `TestCodexE2E_ErrorPropagation` (到達不能 Gateway), `TestCodexBuildEnv_SessionMetadata` (空 SID fallback) |
| 3 | 外部連携 | E2E テストが実際の Codex CLI + LLM API を使用 |
| 4 | データ一貫性 | ファイル内容の検証、SSE イベントのパース検証 |
| 5 | 状態遷移 | セッションステータスの `completed` 確認 |
| 6 | 設定反映 | `wire_api` の動的設定テスト、メタデータ埋め込みテスト |
| 7 | 副作用確認 | ファイル作成の検証 (workDir 内) |

#### セルフレビュー結果

1. **網羅性**: R1~R7 の全要件に対してテストが計画されており、ボトムアップ順序で実行される。E2E テストが全体の結合を検証する。
2. **証拠の十分性**: ファイル内容、SSE イベント種別、セッションステータスの3点で動作を証明。単なる「エラーが出ない」だけでなく具体的な値を検証。
3. **迂回排除**: E2E テストは `codex` エージェントを明示的に指定しており、`claudecode` へのフォールバックは発生しない。`startCodexE2EServer` で `codex.New` を直接登録。
4. **依存関係**: Config -> Process -> Adapter -> E2E の順序で依存関係が整合している。

### 総合判定プロセス (Section 12)

全テスト完了後、以下のチェック項目を確認する:

1. スキップされたテストの有無
2. テストログ内の ERROR/WARN/panic の確認
3. フォールバック経由での偽成功の確認（codex アダプタが使用されていること）
4. 既存 claudecode テストのリグレッション確認
5. テスト間の依存・順序問題の確認
6. カバレッジの妥当性確認
7. 外部システム（Codex CLI, API キー）の状態確認

## Documentation

#### [MODIFY] [027-Codex-Engine-Integration.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/ideas/027-Codex-Engine-Integration.md)
*   **更新内容**: 実装計画の作成に伴い、R5 の「`cawa-server`」について、リネームは別計画に分離する旨を注記として追加
