# 046-LLMGP-Security-Hardening-Part2

> **Source Specification**: [033-LLMGP-Security-Hardening.md](file:///prompts/phases/000-foundation/branches/feat-llm-backend/ideas/033-LLMGP-Security-Hardening.md)

## Goal Description

LLMGPセキュリティ強化の第2部として、高度なセキュリティ対策 (R1 TLS, R4 内部トークン認証, R6 セッション管理) を実装する。
Part 1 で追加した Config 構造体 (`TLSConfig`, `SessionConfig`, `AuthToken`) に依存する。

**本Partのスコープ**:
- R1: TLS対応 (auto/file、証明書自動更新、期限切れ警告、healthレスポンス拡張)
- R4: 内部トークン認証 (ミドルウェア、トークン自動生成、AdapterConfig注入)
- R6: セッション管理のサイズ制限とTTL (LRUキャッシュ)

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1-1: TLSデフォルト無効 | Proposed Changes > llmgateway/proxy.go |
| R1-2: 3パターンサポート (auto/file/無効) | Proposed Changes > llmgateway/proxy_tls.go |
| R1-3: ProxyURL() https:// | Proposed Changes > llmgateway/proxy.go |
| R1-4: CA証明書書き出し+NODE_EXTRA_CA_CERTS | Proposed Changes > llmgateway/proxy_tls.go, tern/server.go |
| R1-5: tern.Server起動時にTLS解決 | Proposed Changes > tern/server.go |
| R1-6: 証明書仕様 (CN, SAN, ECDSA P-256, 24h) | Proposed Changes > llmgateway/proxy_tls.go |
| R1-7: extra_sansサポート | Proposed Changes > llmgateway/proxy_tls.go |
| R1-8: 段階的ログ出力 | Proposed Changes > llmgateway/proxy_tls.go |
| R1-9: 証明書自動更新 | Proposed Changes > llmgateway/proxy_tls.go |
| R1-10: 監視goroutineライフサイクル | Proposed Changes > llmgateway/proxy_tls.go |
| R1-11: health応答に証明書状態含める | Proposed Changes > llmgateway/proxy.go |
| R4-1: ランダムトークン自動生成 | Proposed Changes > tern/server.go |
| R4-2: 認証ミドルウェア設定 | Proposed Changes > llmgateway/proxy.go |
| R4-3: Agent CLI環境変数注入 | Proposed Changes > codingagent/claudecode/process.go, codex/process.go |
| R4-4: ヘッダからトークン検証 | Proposed Changes > llmgateway/proxy.go |
| R4-5: 401応答 | Proposed Changes > llmgateway/proxy.go |
| R4-6: /health,/ は認証不要 | Proposed Changes > llmgateway/proxy.go |
| R4-7: 静的トークン設定可能 | Proposed Changes > tern/server.go (config経由) |
| R4-8: AdapterConfigにトークン注入 | Proposed Changes > codingagent/adapter_config.go, tern/server.go |
| R6-1: セッション最大数設定可能 | Proposed Changes > llmgateway/routing.go |
| R6-2: セッションTTL設定可能 | Proposed Changes > llmgateway/routing.go |
| R6-3: LRU方式でセッション破棄 | Proposed Changes > llmgateway/routing.go |
| R6-4: config.yamlで設定可能 | Part1で対応済み (Config構造体) |

## Proposed Changes

### llmgateway パッケージ -- TLS (R1)

#### [NEW] [proxy_tls_test.go](file:///shared/libs/go/llmgateway/proxy_tls_test.go)

*   **Description**: TLS証明書生成・更新・監視のテスト
*   **Technical Design**: テーブル駆動テスト + 時間制御テスト
*   **Logic**:

    *   `TestGenerateSelfSignedCert`: 自己署名証明書の生成を検証
        *   SAN: `127.0.0.1`, `localhost` が含まれること
        *   ExtraSANs: `["gateway", "proxy"]` を追加した場合、SANに含まれること
        *   Subject CN: `tern-llmgp-local`
        *   鍵タイプ: ECDSA P-256
        *   有効期限: 呼び出し時刻から24時間 (+/- 1分の誤差許容)
    *   `TestGenerateSelfSignedCert_CustomDuration`: テスト用の短い有効期限 (5秒) で生成
        *   検証: `NotAfter` が5秒後であること
    *   `TestWriteCACertFile`: CA証明書のPEMファイル書き出しを検証
        *   検証: ファイルが存在、PEM形式で読み込み可能
    *   `TestTLSCertManager_AutoRenewal`: 自動更新のテスト
        *   短い有効期限 (3秒) + 短い更新閾値 (2秒) で `TLSCertManager` を構築
        *   1秒待機後、`GetCertificate` で返る証明書が更新されていることを検証
        *   INFOログ `"TLS certificate auto-renewed"` が出力されていることを検証
    *   `TestTLSCertManager_ExpiryWarning`: 期限切れ警告のテスト
        *   短い有効期限で構築、期限切れ間近になったときにWARNログが出力されることを検証
    *   `TestTLSCertManager_Lifecycle`: Start/Stop のテスト
        *   `Start()` 後にgoroutineが動作すること
        *   `Stop()` 後にgoroutineが停止すること (チャネルのclose)

#### [NEW] [proxy_tls.go](file:///shared/libs/go/llmgateway/proxy_tls.go)

*   **Description**: TLS証明書自動生成・自動更新ロジック
*   **Technical Design**:

    ```go
    package llmgateway

    import (
        "crypto/ecdsa"
        "crypto/elliptic"
        "crypto/rand"
        "crypto/tls"
        "crypto/x509"
        "crypto/x509/pkix"
        "encoding/pem"
        "fmt"
        "math/big"
        "net"
        "os"
        "sync"
        "time"

        "github.com/axsh/arctic-tern/config"
        "github.com/axsh/arctic-tern/logger"
    )

    // TLSCertManager manages self-signed TLS certificate lifecycle.
    type TLSCertManager struct {
        cfg        config.TLSConfig
        logger     logger.Logger
        mu         sync.RWMutex
        cert       *tls.Certificate
        caCertPEM  []byte
        caFilePath string
        expiresAt  time.Time
        stopCh     chan struct{}

        // For testing: override certificate duration and renewal threshold.
        certDuration     time.Duration // default: 24h
        renewalThreshold time.Duration // default: 1h before expiry
        checkInterval    time.Duration // default: 10m
    }

    // NewTLSCertManager creates a new TLS certificate manager.
    func NewTLSCertManager(cfg config.TLSConfig, log logger.Logger) *TLSCertManager {
        return &TLSCertManager{
            cfg:              cfg,
            logger:           log,
            stopCh:           make(chan struct{}),
            certDuration:     24 * time.Hour,
            renewalThreshold: 1 * time.Hour,
            checkInterval:    10 * time.Minute,
        }
    }

    // GenerateAndLoad generates a new self-signed certificate and loads it.
    func (m *TLSCertManager) GenerateAndLoad() error { ... }

    // GetCertificate returns the current certificate for tls.Config callback.
    func (m *TLSCertManager) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
        m.mu.RLock()
        defer m.mu.RUnlock()
        if m.cert == nil {
            return nil, fmt.Errorf("no TLS certificate available")
        }
        if time.Now().After(m.expiresAt) {
            m.logger.Error(
                "TLS certificate has expired -- HTTPS connections will fail. " +
                "Restart the server to generate a new certificate")
        }
        return m.cert, nil
    }

    // CACertFilePath returns the path to the CA cert PEM file.
    func (m *TLSCertManager) CACertFilePath() string { return m.caFilePath }

    // ExpiresAt returns the certificate expiration time.
    func (m *TLSCertManager) ExpiresAt() time.Time { return m.expiresAt }

    // IsExpired returns true if the certificate has expired.
    func (m *TLSCertManager) IsExpired() bool { return time.Now().After(m.expiresAt) }

    // IsDegraded returns true if cert is expired or expiring within threshold.
    func (m *TLSCertManager) IsDegraded() bool {
        return time.Until(m.expiresAt) < m.renewalThreshold
    }

    // Start begins the background certificate monitoring goroutine.
    func (m *TLSCertManager) Start() { ... }

    // Stop terminates the background monitoring goroutine.
    func (m *TLSCertManager) Stop() { close(m.stopCh) }

    // WriteCACertFile writes the CA cert PEM to a temporary file.
    func (m *TLSCertManager) WriteCACertFile() (string, error) { ... }
    ```

*   **Logic -- generateSelfSignedCert**:
    1. `ecdsa.GenerateKey(elliptic.P256(), crypto/rand.Reader)` で鍵ペア生成
    2. `x509.Certificate` テンプレート作成:
        - `SerialNumber`: `crypto/rand` でランダム生成
        - `Subject`: `pkix.Name{CommonName: "tern-llmgp-local"}`
        - `NotBefore`: `time.Now()`
        - `NotAfter`: `time.Now().Add(m.certDuration)`
        - `IPAddresses`: `[]net.IP{net.ParseIP("127.0.0.1")}`
        - `DNSNames`: `[]string{"localhost"}` + `cfg.ExtraSANs`
        - `KeyUsage`: `x509.KeyUsageDigitalSignature`
        - `ExtKeyUsage`: `[]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}`
    3. `x509.CreateCertificate()` で自己署名 (CA=テンプレート自身)
    4. `tls.Certificate` に格納
    5. CA証明書PEMをメモリに保持

*   **Logic -- 監視goroutine (Start)**:
    ```go
    func (m *TLSCertManager) Start() {
        go func() {
            ticker := time.NewTicker(m.checkInterval)
            defer ticker.Stop()
            warnedAt2h := false
            warnedAt1h := false
            for {
                select {
                case <-ticker.C:
                    remaining := time.Until(m.expiresAt)
                    if remaining <= 0 {
                        m.logger.Error("TLS certificate has expired -- HTTPS connections will fail. Restart the server to generate a new certificate")
                        // Attempt auto-renewal
                        m.tryAutoRenew()
                    } else if remaining <= m.renewalThreshold {
                        if !warnedAt1h {
                            m.logger.Warn("TLS certificate expires in less than 1 hour -- if auto-renewal fails, restart the server to regenerate the certificate")
                            warnedAt1h = true
                        }
                        m.tryAutoRenew()
                    } else if remaining <= 2*time.Hour && !warnedAt2h {
                        m.logger.Info("TLS certificate will expire in 2 hours, auto-renewal scheduled")
                        warnedAt2h = true
                    }
                case <-m.stopCh:
                    return
                }
            }
        }()
    }
    ```

*   **Logic -- tryAutoRenew**:
    ```go
    func (m *TLSCertManager) tryAutoRenew() {
        if err := m.GenerateAndLoad(); err != nil {
            m.logger.Error("TLS certificate auto-renewal failed: "+err.Error()+
                " -- restart the server to restore HTTPS connectivity. "+
                "Workaround: restart the tern server process")
            return
        }
        if _, err := m.WriteCACertFile(); err != nil {
            m.logger.Warn("failed to update CA cert file: " + err.Error())
        }
        m.logger.Info("TLS certificate auto-renewed successfully",
            "new_expiry", m.expiresAt.Format(time.RFC3339))
    }
    ```

---

### llmgateway パッケージ -- 認証ミドルウェア (R4) + health拡張 (R1-11)

#### [MODIFY] [proxy_test.go](file:///shared/libs/go/llmgateway/proxy_test.go)

*   **Description**: 認証ミドルウェアとhealth応答拡張のテスト
*   **Technical Design**: テーブル駆動テスト
*   **Logic**:

    *   `TestAuthMiddleware_ValidToken_XGatewayToken`: `X-Gateway-Token` ヘッダに正しいトークンを設定
        *   検証: 200応答 (ハンドラが呼び出される)
    *   `TestAuthMiddleware_ValidToken_XApiKey`: `x-api-key` ヘッダに `not-needed;token=<correct>;fallback=false` を設定
        *   検証: 200応答
    *   `TestAuthMiddleware_ValidToken_Authorization`: `Authorization: Bearer not-needed;token=<correct>` を設定
        *   検証: 200応答
    *   `TestAuthMiddleware_InvalidToken`: 不正なトークンを設定
        *   検証: 401応答、レスポンスbody内に `"authentication_error"` が含まれること
    *   `TestAuthMiddleware_NoToken`: トークンヘッダなし
        *   検証: 401応答
    *   `TestAuthMiddleware_NoTokenRequired`: `authToken` が空文字列の場合
        *   検証: 200応答 (認証スキップ)
    *   `TestAuthMiddleware_HealthExempt`: `GET /health` はトークンなしでも200
    *   `TestAuthMiddleware_IndexExempt`: `GET /` はトークンなしでも200

    *   `TestHealthResponse_TLSInfo`: TLS有効時のhealth応答テスト
        *   検証: `tls` フィールドが含まれ、`cert_expires_at` が設定されていること
    *   `TestHealthResponse_TLSDegraded`: 証明書期限切れ間近のhealth応答
        *   検証: `status` が `"degraded"`, `cert_expired` が `true`

#### [MODIFY] [proxy.go](file:///shared/libs/go/llmgateway/proxy.go)

*   **Description**: 認証ミドルウェアの追加、health応答の拡張、TLS起動対応
*   **Technical Design**:

    ```go
    // ProxyServer に追加するフィールド
    type ProxyServer struct {
        // ... existing fields ...
        authToken  string          // R4: internal auth token
        tlsMgr     *TLSCertManager // R1: TLS cert manager (nil if TLS disabled)
    }

    // SetAuthToken sets the internal authentication token.
    func (p *ProxyServer) SetAuthToken(token string) {
        p.authToken = token
    }

    // SetTLSCertManager sets the TLS certificate manager.
    func (p *ProxyServer) SetTLSCertManager(mgr *TLSCertManager) {
        p.tlsMgr = mgr
    }
    ```

*   **Logic -- authMiddleware**:
    ```go
    func (p *ProxyServer) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
        return func(w http.ResponseWriter, r *http.Request) {
            if p.authToken == "" {
                next(w, r)
                return
            }
            // Check X-Gateway-Token header
            if token := r.Header.Get("X-Gateway-Token"); token == p.authToken {
                next(w, r)
                return
            }
            // Check x-api-key metadata (Claude Code)
            if extractToken(r.Header.Get("x-api-key")) == p.authToken {
                next(w, r)
                return
            }
            // Check Authorization metadata (Codex)
            if extractToken(r.Header.Get("Authorization")) == p.authToken {
                next(w, r)
                return
            }
            WriteErrorResponse(w, &GatewayError{
                Type:    "authentication_error",
                Message: "invalid or missing gateway token",
                Code:    "unauthorized",
                Status:  http.StatusUnauthorized,
            })
        }
    }

    // extractToken extracts token=<value> from semicolon-separated metadata.
    func extractToken(headerValue string) string {
        if strings.HasPrefix(headerValue, "Bearer ") {
            headerValue = strings.TrimPrefix(headerValue, "Bearer ")
        }
        for _, part := range strings.Split(headerValue, ";") {
            part = strings.TrimSpace(part)
            if strings.HasPrefix(part, "token=") {
                return strings.TrimPrefix(part, "token=")
            }
        }
        return ""
    }
    ```

*   **Logic -- setupRoutes の変更**:
    ```go
    func (p *ProxyServer) setupRoutes(mux *http.ServeMux) {
        // Public endpoints (no auth)
        mux.HandleFunc("GET /{$}", p.handleIndex)
        mux.HandleFunc("GET /health", p.handleHealth)
        mux.HandleFunc("GET /v1/models", p.handleModels)
        // Auth-protected endpoints
        mux.HandleFunc("POST /v1/messages", p.authMiddleware(p.handleAnthropicMessages))
        mux.HandleFunc("POST /v1/responses", p.authMiddleware(p.handleOpenAIResponses))
    }
    ```

*   **Logic -- handleHealth の拡張**:
    ```go
    type healthResponse struct {
        Status  string       `json:"status"`
        Message string       `json:"message,omitempty"`
        Models  int          `json:"models"`
        TLS     *tlsStatus   `json:"tls,omitempty"`
    }

    type tlsStatus struct {
        Enabled      bool   `json:"enabled"`
        Mode         string `json:"mode"`
        CertExpiresAt string `json:"cert_expires_at,omitempty"`
        CertExpired  bool   `json:"cert_expired"`
    }

    func (p *ProxyServer) handleHealth(w http.ResponseWriter, r *http.Request) {
        resp := healthResponse{
            Status: "ok",
            Models: p.countModels(),
        }
        if p.tlsMgr != nil {
            ts := &tlsStatus{
                Enabled:      true,
                Mode:         p.cfg.LLMGateway.TLS.Mode,
                CertExpiresAt: p.tlsMgr.ExpiresAt().Format(time.RFC3339),
                CertExpired:  p.tlsMgr.IsExpired(),
            }
            resp.TLS = ts
            if p.tlsMgr.IsDegraded() {
                resp.Status = "degraded"
                if p.tlsMgr.IsExpired() {
                    resp.Message = "TLS certificate expired -- restart the server to restore HTTPS"
                } else {
                    resp.Message = "TLS certificate expiring soon -- auto-renewal in progress"
                }
            }
        }
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(resp)
    }
    ```

*   **Logic -- Launch() の TLS 対応**:
    ```go
    func (p *ProxyServer) Launch(_ context.Context) error {
        mux := http.NewServeMux()
        p.setupRoutes(mux)
        addr := fmt.Sprintf("127.0.0.1:%d", p.port)
        // ... listener setup ...

        serverCfg := p.cfg.LLMGateway.Server
        p.server = &http.Server{
            Handler:        mux,
            ReadTimeout:    time.Duration(serverCfg.ReadTimeoutSeconds) * time.Second,
            WriteTimeout:   time.Duration(serverCfg.WriteTimeoutSeconds) * time.Second,
            IdleTimeout:    time.Duration(serverCfg.IdleTimeoutSeconds) * time.Second,
            MaxHeaderBytes: serverCfg.MaxHeaderBytes,
        }

        // TLS support
        if p.tlsMgr != nil {
            p.server.TLSConfig = &tls.Config{
                GetCertificate: p.tlsMgr.GetCertificate,
            }
            p.tlsMgr.Start()
            go func() {
                if err := p.server.ServeTLS(listener, "", ""); err != nil && err != http.ErrServerClosed {
                    p.logger.Error("proxy server TLS error", "error", err)
                }
            }()
        } else {
            go func() {
                if err := p.server.Serve(listener); err != nil && err != http.ErrServerClosed {
                    p.logger.Error("proxy server error", "error", err)
                }
            }()
        }
        return nil
    }
    ```

*   **Logic -- ProxyURL() の変更**:
    ```go
    func (p *ProxyServer) ProxyURL() string {
        scheme := "http"
        if p.tlsMgr != nil {
            scheme = "https"
        }
        return fmt.Sprintf("%s://localhost:%d", scheme, p.port)
    }
    ```

*   **Logic -- Shutdown() の変更**:
    ```go
    func (p *ProxyServer) Shutdown(ctx context.Context) error {
        if p.tlsMgr != nil {
            p.tlsMgr.Stop()
        }
        // ... existing shutdown logic ...
    }
    ```

---

### llmgateway パッケージ -- セッション管理 (R6)

#### [MODIFY] [routing_test.go](file:///shared/libs/go/llmgateway/routing_test.go)

*   **Description**: セッション管理のサイズ制限とTTLのテスト
*   **Technical Design**: テーブル駆動テスト + 時間制御テスト
*   **Logic**:

    *   `TestSessionMap_MaxSize`: `MaxSessions: 3` で構築
        *   4つのセッションIDで `Route()` を呼び出す
        *   4番目のセッション登録後、最も古いセッション (1番目) が `sessionModels` mapから削除されていること
        *   2, 3, 4番目のセッションは存在すること
    *   `TestSessionMap_TTL`: `TTLSeconds: 1` (1秒) で構築
        *   セッションを登録
        *   1.1秒待機
        *   同一セッションIDで `Route()` を呼び出したとき、古いルーティング結果が使用されず新規ルーティングが実行されること
    *   `TestSessionMap_LRU`: `MaxSessions: 3` で構築
        *   セッション1, 2, 3を登録
        *   セッション1にアクセス (LRUで最新に)
        *   セッション4を登録
        *   セッション2が破棄されること (1はLRUで最新のため残る)

#### [MODIFY] [routing.go](file:///shared/libs/go/llmgateway/routing.go)

*   **Description**: セッションモデルマップにLRUサイズ制限とTTLを追加
*   **Technical Design**:

    ```go
    // sessionEntry represents a cached session routing result.
    type sessionEntry struct {
        model    *RoutedModel
        lastUsed time.Time
    }

    type ModelRouter struct {
        profiles      *config.ModelProfilesConfig
        logger        logger.Logger
        mu            sync.RWMutex
        sessionModels map[string]*sessionEntry  // changed from map[string]*RoutedModel
        maxSessions   int
        sessionTTL    time.Duration
        accessOrder   []string // LRU order: oldest first
    }
    ```

*   **Logic -- セッション登録 (Route内)**:
    ```go
    if sessionID != "" {
        r.mu.Lock()
        if _, exists := r.sessionModels[sessionID]; !exists {
            // Evict oldest if at capacity
            if r.maxSessions > 0 && len(r.sessionModels) >= r.maxSessions {
                r.evictOldest()
            }
            r.sessionModels[sessionID] = &sessionEntry{
                model:    resolved,
                lastUsed: time.Now(),
            }
            r.accessOrder = append(r.accessOrder, sessionID)
        } else {
            // Update LRU position
            r.sessionModels[sessionID].lastUsed = time.Now()
            r.touchAccessOrder(sessionID)
        }
        r.mu.Unlock()
    }
    ```

*   **Logic -- evictOldest**:
    ```go
    func (r *ModelRouter) evictOldest() {
        if len(r.accessOrder) == 0 {
            return
        }
        oldest := r.accessOrder[0]
        r.accessOrder = r.accessOrder[1:]
        delete(r.sessionModels, oldest)
        if r.logger != nil {
            r.logger.Debug("session evicted (max capacity)", "sid", oldest)
        }
    }
    ```

*   **Logic -- セッションルックアップ (TTLチェック付き)**:
    ```go
    // existing session lookup (L113-121)
    if sessionID != "" {
        r.mu.RLock()
        entry, exists := r.sessionModels[sessionID]
        r.mu.RUnlock()
        if exists {
            // TTL check
            if r.sessionTTL > 0 && time.Since(entry.lastUsed) > r.sessionTTL {
                r.mu.Lock()
                delete(r.sessionModels, sessionID)
                r.removeFromAccessOrder(sessionID)
                r.mu.Unlock()
                if r.logger != nil {
                    r.logger.Debug("session expired (TTL)", "sid", sessionID)
                }
                // Fall through to "not found" error
            } else {
                entry.lastUsed = time.Now()
                return entry.model, nil
            }
        }
    }
    ```

*   **Logic -- ModelRouter初期化の変更**:
    `NewModelRouter` (または `NewProxyServer`/`NewBifrostDriver`) で `maxSessions` と `sessionTTL` を Config から設定:
    ```go
    router := &ModelRouter{
        profiles:      profiles,
        logger:        log,
        sessionModels: make(map[string]*sessionEntry),
        maxSessions:   cfg.LLMGateway.Session.MaxSessions,   // default: 1000 (from ApplyDefaults)
        sessionTTL:    time.Duration(cfg.LLMGateway.Session.TTLSeconds) * time.Second,
        accessOrder:   make([]string, 0),
    }
    ```

---

### codingagent パッケージ (R4: トークン注入)

#### [MODIFY] [adapter_config.go](file:///shared/libs/go/codingagent/adapter_config.go)

*   **Description**: `GatewayToken` フィールドを追加
*   **Technical Design**:
    ```go
    type AdapterConfig struct {
        // ... existing fields ...

        // GatewayToken is the internal authentication token for LLMGP.
        // Injected by tern.Server on startup.
        GatewayToken string
    }
    ```

#### [MODIFY] [claudecode/process_test.go](file:///shared/libs/go/codingagent/claudecode/process_test.go)

*   **Description**: トークン注入のテスト
*   **Logic**:
    *   `TestBuildEnv_GatewayToken`: `AdapterConfig.GatewayToken = "test-token-abc"` を設定
        *   検証: `ANTHROPIC_API_KEY` が `"not-needed;token=test-token-abc;fallback=false;sid=default"` を含むこと
    *   `TestBuildEnv_NoGatewayToken`: `GatewayToken` が空の場合
        *   検証: `ANTHROPIC_API_KEY` に `token=` が含まれないこと (既存動作を維持)

#### [MODIFY] [claudecode/process.go](file:///shared/libs/go/codingagent/claudecode/process.go)

*   **Description**: `BuildEnv` でトークンをAPIキーメタデータに追加
*   **Technical Design**:
    ```go
    // BuildEnv (L57-94) の変更
    if ac.GatewayURL != "" {
        apiKey := "not-needed"
        fallbackStr := "false"
        if ac.ToolCallFallback {
            fallbackStr = "true"
        }
        sid := cfg.AgentSessionID
        if sid == "" {
            sid = "default"
        }
        // R4: Add gateway token if available
        tokenPart := ""
        if ac.GatewayToken != "" {
            tokenPart = ";token=" + ac.GatewayToken
        }
        env["ANTHROPIC_API_KEY"] = apiKey + tokenPart + ";fallback=" + fallbackStr + ";sid=" + sid
    }
    ```

#### [MODIFY] [codex/process_test.go](file:///shared/libs/go/codingagent/codex/process_test.go)

*   **Description**: Codex側のトークン注入テスト
*   **Logic**:
    *   `TestBuildEnv_CodexGatewayToken`: `GatewayToken` 設定時に `TERN_GATEWAY_TOKEN` 環境変数が含まれること

#### [MODIFY] codex/ の BuildEnv相当の処理

*   **Description**: Codex側のトークン環境変数注入
*   **Logic**: `GatewayToken` が非空の場合、`TERN_GATEWAY_TOKEN` 環境変数を追加

---

### tern パッケージ (R4: トークン生成, R1: TLS解決)

#### [MODIFY] [server_test.go](file:///shared/libs/go/tern/server_test.go)

*   **Description**: トークン自動生成とTLS解決のテスト
*   **Logic**:
    *   `TestServer_AutoGenerateToken`: `AuthToken` が空でサーバーを構築
        *   検証: `Server.GatewayToken()` が64文字 (32バイトhex) のランダム文字列を返すこと
    *   `TestServer_StaticToken`: `AuthToken: "my-static-token"` でサーバーを構築
        *   検証: `Server.GatewayToken()` が `"my-static-token"` を返すこと
    *   `TestServer_TLS_AutoMode`: `TLS.Enabled: true, Mode: "auto"` でサーバーを構築
        *   検証: `ProxyURL()` が `https://` で始まること。CA証明書ファイルが存在すること。
    *   `TestServer_TLS_Disabled`: `TLS.Enabled: false` でサーバーを構築
        *   検証: `ProxyURL()` が `http://` で始まること

#### [MODIFY] [server.go](file:///shared/libs/go/tern/server.go)

*   **Description**: トークン自動生成、TLS解決、AdapterConfig注入
*   **Technical Design**:

    ```go
    type Server struct {
        // ... existing fields ...
        gatewayToken string   // R4: generated or configured auth token
        tlsMgr       *llmgateway.TLSCertManager // R1: TLS manager
    }

    // GatewayToken returns the internal gateway auth token.
    func (s *Server) GatewayToken() string { return s.gatewayToken }

    // TLSCACertPath returns the CA certificate file path (for agent CLI).
    // Empty if TLS is disabled.
    func (s *Server) TLSCACertPath() string {
        if s.tlsMgr != nil {
            return s.tlsMgr.CACertFilePath()
        }
        return ""
    }
    ```

*   **Logic -- New() の変更**:
    ```go
    func New(opts ...Option) (*Server, error) {
        // ... existing resolution steps 1-5 ...

        // Step 6: Resolve Auth Token (R4)
        gatewayToken := cfg.LLMGateway.AuthToken
        if gatewayToken == "" {
            tokenBytes := make([]byte, 32)
            if _, err := crypto_rand.Read(tokenBytes); err != nil {
                return nil, fmt.Errorf("tern: generate auth token: %w", err)
            }
            gatewayToken = hex.EncodeToString(tokenBytes)
            if log != nil {
                log.Debug("gateway auth token auto-generated")
            }
        }
        // Inject token into gateway
        if ps, ok := gw.(*llmgateway.ProxyServer); ok {
            ps.SetAuthToken(gatewayToken)
        }

        // Step 7: Resolve TLS (R1)
        var tlsMgr *llmgateway.TLSCertManager
        if cfg.LLMGateway.TLS.Enabled {
            tlsMgr = llmgateway.NewTLSCertManager(cfg.LLMGateway.TLS, log)
            if cfg.LLMGateway.TLS.Mode == "auto" {
                if err := tlsMgr.GenerateAndLoad(); err != nil {
                    return nil, fmt.Errorf("tern: generate TLS cert: %w", err)
                }
                if _, err := tlsMgr.WriteCACertFile(); err != nil {
                    return nil, fmt.Errorf("tern: write CA cert: %w", err)
                }
            }
            if ps, ok := gw.(*llmgateway.ProxyServer); ok {
                ps.SetTLSCertManager(tlsMgr)
            }
        }

        return &Server{
            // ... existing fields ...
            gatewayToken: gatewayToken,
            tlsMgr:       tlsMgr,
        }, nil
    }
    ```

*   **Logic -- resolveAgentService の変更** (トークンとTLS CA注入):
    ```go
    // agentservice 経由で AdapterConfig に注入
    // gatewayURL に https:// が含まれる場合、NODE_EXTRA_CA_CERTS も設定
    ```
    tern.Server が公開する `GatewayToken()` と `TLSCACertPath()` を、
    agentservice 経由で codingagent の `AdapterConfig` に注入する。
    AdapterConfig にはすでに `GatewayToken` フィールドが追加されているため、
    agentservice.New() に `WithGatewayToken()` オプションを追加し、
    `codingagent.AdapterConfig.GatewayToken` に伝搬させる。

---

## Step-by-Step Implementation Guide

1. **セッション管理テスト作成 (R6)**:
    - `routing_test.go` に `TestSessionMap_MaxSize`, `TestSessionMap_TTL`, `TestSessionMap_LRU` を追加
    - テスト失敗を確認
    - `git commit -m "test: add session management size/TTL tests (R6)"`

2. **セッション管理実装 (R6)**:
    - `routing.go` の `sessionModels` を `map[string]*sessionEntry` に変更
    - LRU破棄ロジック、TTLチェックを追加
    - テスト成功を確認
    - `git commit -m "feat: add session map size limit and TTL (R6)"`

3. **TLS証明書生成テスト作成 (R1)**:
    - `proxy_tls_test.go` に全テストケースを追加
    - テスト失敗を確認

4. **TLS証明書生成・自動更新実装 (R1)**:
    - `proxy_tls.go` を新規作成
    - `TLSCertManager` の全メソッドを実装
    - テスト成功を確認
    - `git commit -m "feat: add TLS certificate manager with auto-renewal (R1)"`

5. **認証ミドルウェアテスト作成 (R4)**:
    - `proxy_test.go` に認証テストケースを追加

6. **認証ミドルウェア・health拡張実装 (R4, R1-11)**:
    - `proxy.go` に `authMiddleware`, `extractToken`, health応答拡張を追加
    - `SetAuthToken`, `SetTLSCertManager` メソッドを追加
    - `setupRoutes` で認証ミドルウェアを適用
    - `Launch` でTLS対応を追加
    - `ProxyURL` を条件分岐に変更
    - テスト成功を確認
    - `git commit -m "feat: add auth middleware, TLS launch, and health TLS status (R4, R1-11)"`

7. **AdapterConfig拡張 (R4)**:
    - `adapter_config.go` に `GatewayToken` フィールド追加
    - `claudecode/process_test.go` にトークン注入テスト追加
    - `claudecode/process.go` の `BuildEnv` を修正
    - `codex/process_test.go` にテスト追加
    - Codex側の `BuildEnv` 相当処理を修正
    - テスト成功を確認
    - `git commit -m "feat: inject gateway auth token into agent CLI env (R4)"`

8. **tern.Server統合 (R1, R4)**:
    - `server_test.go` にテスト追加
    - `server.go` にトークン生成、TLS解決、注入ロジックを追加
    - テスト成功を確認
    - `git commit -m "feat: integrate TLS and auth token in tern.Server (R1, R4)"`

9. **ビルド・テスト検証**:
    - `./scripts/process/build.sh` を実行
    - `./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories "llm"` を実行
    - `./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories "common"` を実行

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```

2.  **Integration Tests (LLM)**:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories "llm"
    ```
    *   **Log Verification**:
        *   認証ミドルウェアのログ: トークンなしのリクエストが正しく拒否されているか
        *   セッション破棄のログ: `session evicted (max capacity)` や `session expired (TTL)` が適切に出力されるか

3.  **Integration Tests (Common)**:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories "common"
    ```
    *   **Log Verification**: サーバー起動時にTLS証明書生成のログが出力されるか (TLS有効時)

4.  **E2Eテスト**:
    本Partの変更はLLMGP内部の認証レイヤーとTLS暗号化の追加であり、tern.Serverが自動的にトークンをAgent CLIに注入するため、既存のE2Eテストの動作に影響しない (内部的に認証が通る)。TLSはデフォルト無効のため、既存E2Eテストには影響しない。
    新規E2Eテストの追加は不要。
    - 理由: トークン認証とTLSの検証は単体テストで完全にカバーされている。E2Eテストでは認証が自動注入されるため追加テストは不要。

### テスト項目のセルフレビュー (11.4)

1. **網羅性**: R1 (TLS生成/更新/監視/health)、R4 (認証MW/トークン生成/注入)、R6 (サイズ制限/TTL/LRU) の全要件に対応するテストが計画されている。
2. **証拠の十分性**: TLS証明書はSAN/CN/有効期限を検証。認証は正常/異常/免除を検証。セッションは登録/破棄/TTLを検証。
3. **迂回排除**: 認証テストでは複数のヘッダ経路を個別に検証。セッションテストではLRU順序を検証。
4. **依存関係**: ボトムアップ順序: proxy_tls (末端) --> proxy (認証MW/health) --> tern/server (統合)。routing (セッション管理) は独立。

## Documentation

#### [MODIFY] [config.go](file:///shared/libs/go/config/config.go)
*   **更新内容**: Part1で追加した構造体は維持。追加のドキュメント更新不要。

#### [MODIFY] [proxy.go](file:///shared/libs/go/llmgateway/proxy.go)
*   **更新内容**: 新規公開メソッド (`SetAuthToken`, `SetTLSCertManager`) にGoDocを追加。
