# 000-Factory-Registry-Bifrost-Part1

> **Source Specification**: prompts/phases/000-foundation/branches/feat-llm-backend/ideas/030-Factory-Registry-And-Bifrost-Unification.md

## Goal Description

本 Part1 では、仕様030の基盤作業として以下の3要件を実装する:

1. **R8: リポジトリ/パッケージリネーム** (Headless-Agent-Gateway -> arctic-tern, HAG -> tern)
2. **R1: codingagent の Factory/Registry** (init() 自己登録パターン)
3. **R2: llmgateway の Provider Registry** (Provider インターフェース + switch-case 排除)

依存関係: R8 -> R1, R2 (リネームを先に行い、以降の作業を新命名で実施)

## User Review Required

> [!IMPORTANT]
> **R8 (リネーム) の実行範囲**: go.mod のモジュールパス変更と全 import パスの一括置換は、全ファイルに影響する破壊的変更です。この Part の実行前に、作業ブランチが最新であることを確認してください。

> [!WARNING]
> **/v1/chat/completions の削除**: 仕様の決定事項4に基づき、本 Part2 の R3 で実施予定。Part1 では削除しません。

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R8: go.mod モジュールパス変更 | Proposed Changes > go.mod |
| R8: import パス一括置換 (約112箇所) | Proposed Changes > 全 .go ファイル |
| R8: hag/ -> tern/ ディレクトリリネーム | Proposed Changes > shared/libs/go/tern/ |
| R8: package hag -> package tern | Proposed Changes > shared/libs/go/tern/*.go |
| R8: コメント/ドキュメント HAG -> tern | Proposed Changes > 各ファイル |
| R1: codingagent Registry 型定義 | Proposed Changes > codingagent/registry.go |
| R1: claudecode init() 自己登録 | Proposed Changes > codingagent/claudecode/init.go |
| R1: codex init() 自己登録 | Proposed Changes > codingagent/codex/init.go |
| R1: CreateAll() でアダプター一括生成 | Proposed Changes > codingagent/registry.go |
| R2: Provider インターフェース定義 | Proposed Changes > llmgateway/provider.go |
| R2: Provider Registry (Register/Get) | Proposed Changes > llmgateway/provider.go |
| R2: anthropicProvider 実装 + init() | Proposed Changes > llmgateway/provider_anthropic.go |
| R2: openaiProvider 実装 + init() | Proposed Changes > llmgateway/provider_openai.go |
| R2: googleProvider 実装 + init() | Proposed Changes > llmgateway/provider_google.go |
| R2: ollamaProvider 実装 + init() | Proposed Changes > llmgateway/provider_ollama.go (R4 前倒し) |
| R2: providerBaseURLs 削除 | Proposed Changes > llmgateway/provider_forwarder.go |
| R2: 認証ヘッダー switch-case 削除 | Proposed Changes > llmgateway/provider_forwarder.go |
| R2: bifrost_account.go の providerNameMap 削除 | Proposed Changes > llmgateway/bifrost_account.go |
| 決定事項2: Provider インターフェース設計 | Proposed Changes > llmgateway/provider.go |
| 決定事項2: サブディレクトリは作らない | 設計方針 (同一パッケージ内ファイル分割) |

## Proposed Changes

### R8: リポジトリ/パッケージリネーム

#### [MODIFY] [go.mod](file://shared/libs/go/go.mod)
*   **Description**: Go モジュールパスを変更
*   **Technical Design**:
    ```
    - module github.com/axsh/hag
    + module github.com/axsh/arctic-tern
    ```

#### [RENAME + MODIFY] shared/libs/go/hag/ -> [shared/libs/go/tern/](file://shared/libs/go/tern/)
*   **Description**: ファサードパッケージのディレクトリ名とパッケージ宣言を変更
*   **Technical Design**:
    *   `shared/libs/go/hag/` を `shared/libs/go/tern/` にリネーム
    *   全ファイルの `package hag` を `package tern` に変更
    *   `hag.Server` -> `tern.Server`, `hag.New()` -> `tern.New()`, `hag.Option` -> `tern.Option` 等
*   **対象ファイル**:
    *   `server.go`: パッケージ宣言、コメント内の "HAG" -> "tern"/"arctic-tern"
    *   `server_test.go`: 同上
    *   `options.go`: 同上

#### [MODIFY] 全 .go ファイルの import パス
*   **Description**: `github.com/axsh/hag/xxx` -> `github.com/axsh/arctic-tern/xxx` の一括置換 (約112箇所)
*   **Technical Design**:
    ```bash
    # sed で一括置換 (shared/ と examples/ 配下)
    find shared/ examples/ -name '*.go' -exec sed -i 's|github.com/axsh/hag/|github.com/axsh/arctic-tern/|g' {} +
    ```
*   **Logic**:
    *   go.mod のモジュールパス変更に連動して、全 .go ファイルの import 文を更新
    *   `github.com/axsh/hag/hag` -> `github.com/axsh/arctic-tern/tern` (ディレクトリリネームも反映)

#### [MODIFY] コメント/ドキュメント内の名称変更
*   **Description**: .go ファイル内のコメント、README.md、scripts/ 内の "HAG" / "Headless-Agent-Gateway" の言及を更新
*   **Technical Design**:
    *   "HAG" (大文字、単語境界) -> "tern" に変更
    *   "Headless-Agent-Gateway" -> "arctic-tern" に変更
    *   "hag:" (ログプレフィックス等) -> "tern:" に変更
    *   `prompts/phases/` 配下の ideas/plans は **変更しない**

---

### R1: codingagent の Factory/Registry

#### [NEW] [registry_test.go](file://shared/libs/go/codingagent/registry_test.go)
*   **Description**: Registry の単体テスト (TDD: テストを先に作成)
*   **Technical Design**:
    ```go
    package codingagent

    func TestRegister_And_CreateAll(t *testing.T) {
        // テーブル駆動テスト
        tests := []struct {
            name       string
            factories  map[string]FactoryFunc
            wantCount  int
            wantNames  []string
        }{
            {
                name: "no factories registered",
                factories: map[string]FactoryFunc{},
                wantCount: 0,
            },
            {
                name: "one factory returns agent",
                factories: map[string]FactoryFunc{
                    "test-agent": func(cfg *AdapterConfig) (CodingAgent, error) {
                        return &mockAgent{name: "test-agent"}, nil
                    },
                },
                wantCount: 1,
                wantNames: []string{"test-agent"},
            },
            {
                name: "factory returns nil (CLI not found) - skipped",
                factories: map[string]FactoryFunc{
                    "missing": func(cfg *AdapterConfig) (CodingAgent, error) {
                        return nil, nil // CLI not available
                    },
                },
                wantCount: 0,
            },
            {
                name: "factory returns error",
                factories: map[string]FactoryFunc{
                    "broken": func(cfg *AdapterConfig) (CodingAgent, error) {
                        return nil, fmt.Errorf("init failed")
                    },
                },
                wantCount: 0, // error logged, not fatal
            },
        }
    }

    func TestRegister_DuplicateName_Panics(t *testing.T) {
        // 同名の二重登録は panic する
    }
    ```
*   **Logic**:
    *   テスト間でグローバル registry を汚染しないよう、各テストケースで registry をリセットする仕組みを入れる
    *   `resetRegistry()` 関数 (テスト専用) を registry.go に追加

#### [NEW] [registry.go](file://shared/libs/go/codingagent/registry.go)
*   **Description**: CodingAgent の Factory/Registry パターン
*   **Technical Design**:
    ```go
    package codingagent

    import "sync"

    // FactoryFunc creates a CodingAgent from config.
    // Return (nil, nil) if the agent's CLI is not available (graceful skip).
    // Return (nil, err) if initialization fails unexpectedly.
    type FactoryFunc func(cfg *AdapterConfig) (CodingAgent, error)

    var (
        registryMu sync.RWMutex
        registry   = map[string]FactoryFunc{}
    )

    // Register registers a factory function for the given agent name.
    // Typically called from init() in each adapter's package.
    // Panics if name is already registered (programming error).
    func Register(name string, factory FactoryFunc) {
        registryMu.Lock()
        defer registryMu.Unlock()
        if _, dup := registry[name]; dup {
            panic("codingagent: Register called twice for " + name)
        }
        registry[name] = factory
    }

    // CreateAll creates all registered agents using the given config.
    // Agents whose factory returns (nil, nil) are silently skipped.
    // Agents whose factory returns an error are logged and skipped.
    // Returns the successfully created agents.
    func CreateAll(cfg *AdapterConfig) []CodingAgent {
        registryMu.RLock()
        defer registryMu.RUnlock()
        var agents []CodingAgent
        for name, factory := range registry {
            agent, err := factory(cfg)
            if err != nil {
                if cfg.Logger != nil {
                    cfg.Logger.Warn("failed to create coding agent",
                        "agent", name, "error", err.Error())
                }
                continue
            }
            if agent == nil {
                // CLI not found, skip silently
                continue
            }
            agents = append(agents, agent)
        }
        return agents
    }

    // resetRegistry clears the registry (for testing only).
    func resetRegistry() {
        registryMu.Lock()
        defer registryMu.Unlock()
        registry = map[string]FactoryFunc{}
    }
    ```
*   **Logic**:
    *   `sync.RWMutex` で並行安全性を確保
    *   同名二重登録は panic (init() 呼び出しのプログラミングエラー)
    *   `CreateAll` は全ファクトリを呼び出し、nil 返却 (CLI未発見) はスキップ、error はログ出力してスキップ

#### [NEW] [claudecode/init.go](file://shared/libs/go/codingagent/claudecode/init.go)
*   **Description**: claudecode アダプターの自己登録
*   **Technical Design**:
    ```go
    package claudecode

    import (
        "os/exec"

        "github.com/axsh/arctic-tern/codingagent"
    )

    func init() {
        codingagent.Register("claudecode", func(cfg *codingagent.AdapterConfig) (codingagent.CodingAgent, error) {
            if _, err := exec.LookPath("claude"); err != nil {
                return nil, nil // CLI not available, skip
            }
            return New(cfg), nil
        })
    }
    ```
*   **Logic**:
    *   `exec.LookPath("claude")` で CLI の存在を確認
    *   見つからなければ `(nil, nil)` を返して graceful skip
    *   見つかれば既存の `New(cfg)` でアダプターを生成

#### [NEW] [codex/init.go](file://shared/libs/go/codingagent/codex/init.go)
*   **Description**: codex アダプターの自己登録
*   **Technical Design**:
    ```go
    package codex

    import (
        "os/exec"

        "github.com/axsh/arctic-tern/codingagent"
    )

    func init() {
        codingagent.Register("codex", func(cfg *codingagent.AdapterConfig) (codingagent.CodingAgent, error) {
            if _, err := exec.LookPath("codex"); err != nil {
                return nil, nil // CLI not available, skip
            }
            return New(cfg), nil
        })
    }
    ```

---

### R2: llmgateway の Provider Registry

#### [NEW] [provider_test.go](file://shared/libs/go/llmgateway/provider_test.go)
*   **Description**: Provider Registry の単体テスト (TDD)
*   **Technical Design**:
    ```go
    package llmgateway

    func TestRegisterProvider_And_GetProvider(t *testing.T) {
        tests := []struct {
            name         string
            providerName string
            wantOK       bool
        }{
            {"registered provider", "anthropic", true},
            {"unregistered provider", "unknown", false},
        }
        // ...
    }

    func TestRegisterProvider_DuplicatePanics(t *testing.T) {
        // 同名の二重登録で panic することを確認
    }

    func TestAllProviders_HaveRequiredFields(t *testing.T) {
        // init() で登録済みの全プロバイダーが Name(), BaseURL(), BifrostProvider() を正しく返すか
        expected := map[string]struct {
            baseURL string
        }{
            "anthropic": {baseURL: "https://api.anthropic.com"},
            "openai":    {baseURL: "https://api.openai.com"},
            "google":    {baseURL: "https://generativelanguage.googleapis.com"},
            "ollama":    {baseURL: "http://localhost:11434"},
        }
        for name, want := range expected {
            p, ok := GetProvider(name)
            if !ok {
                t.Fatalf("provider %q not registered", name)
            }
            if got := p.BaseURL(); got != want.baseURL {
                t.Errorf("provider %q BaseURL = %q, want %q", name, got, want.baseURL)
            }
        }
    }

    func TestSetAuthHeaders_Anthropic(t *testing.T) {
        p, _ := GetProvider("anthropic")
        req, _ := http.NewRequest("POST", "https://example.com", nil)
        originalHeaders := http.Header{}
        originalHeaders.Set("anthropic-beta", "test-beta")
        p.SetAuthHeaders(req, "test-key", originalHeaders)

        if got := req.Header.Get("x-api-key"); got != "test-key" {
            t.Errorf("x-api-key = %q, want %q", got, "test-key")
        }
        if got := req.Header.Get("anthropic-version"); got != "2023-06-01" {
            t.Errorf("anthropic-version = %q, want %q", got, "2023-06-01")
        }
        if got := req.Header.Get("anthropic-beta"); got != "test-beta" {
            t.Errorf("anthropic-beta = %q, want %q", got, "test-beta")
        }
    }

    func TestSetAuthHeaders_OpenAI(t *testing.T) { ... }
    func TestSetAuthHeaders_Google(t *testing.T) { ... }
    ```

#### [NEW] [provider.go](file://shared/libs/go/llmgateway/provider.go)
*   **Description**: Provider インターフェースと Registry
*   **Technical Design**:
    ```go
    package llmgateway

    import (
        "net/http"
        "sync"

        bifrostSchemas "github.com/maximhq/bifrost/core/schemas"
    )

    // Provider abstracts provider-specific behavior (base URL, auth headers, etc.).
    type Provider interface {
        // Name returns the provider identifier (e.g. "anthropic", "openai", "google", "ollama").
        Name() string

        // BaseURL returns the API base URL for this provider.
        BaseURL() string

        // SetAuthHeaders sets provider-specific authentication headers on the request.
        SetAuthHeaders(req *http.Request, apiKey string, originalHeaders http.Header)

        // BifrostProvider returns the corresponding Bifrost SDK ModelProvider constant.
        BifrostProvider() bifrostSchemas.ModelProvider
    }

    var (
        providerMu       sync.RWMutex
        providerRegistry = map[string]Provider{}
    )

    // RegisterProvider registers a Provider implementation.
    // Typically called from init() in each provider file.
    // Panics if a provider with the same name is already registered.
    func RegisterProvider(p Provider) {
        providerMu.Lock()
        defer providerMu.Unlock()
        name := p.Name()
        if _, dup := providerRegistry[name]; dup {
            panic("llmgateway: RegisterProvider called twice for " + name)
        }
        providerRegistry[name] = p
    }

    // GetProvider returns the Provider for the given name.
    func GetProvider(name string) (Provider, bool) {
        providerMu.RLock()
        defer providerMu.RUnlock()
        p, ok := providerRegistry[name]
        return p, ok
    }

    // AllProviders returns all registered providers.
    func AllProviders() []Provider {
        providerMu.RLock()
        defer providerMu.RUnlock()
        providers := make([]Provider, 0, len(providerRegistry))
        for _, p := range providerRegistry {
            providers = append(providers, p)
        }
        return providers
    }
    ```

#### [NEW] [provider_anthropic.go](file://shared/libs/go/llmgateway/provider_anthropic.go)
*   **Description**: Anthropic プロバイダー実装
*   **Technical Design**:
    ```go
    package llmgateway

    import (
        "net/http"

        bifrostSchemas "github.com/maximhq/bifrost/core/schemas"
    )

    func init() {
        RegisterProvider(&anthropicProvider{})
    }

    type anthropicProvider struct{}

    func (p *anthropicProvider) Name() string { return "anthropic" }

    func (p *anthropicProvider) BaseURL() string { return "https://api.anthropic.com" }

    func (p *anthropicProvider) SetAuthHeaders(req *http.Request, apiKey string, originalHeaders http.Header) {
        req.Header.Set("x-api-key", apiKey)
        req.Header.Set("anthropic-version", "2023-06-01")
        if beta := originalHeaders.Get("anthropic-beta"); beta != "" {
            req.Header.Set("anthropic-beta", beta)
        }
    }

    func (p *anthropicProvider) BifrostProvider() bifrostSchemas.ModelProvider {
        return bifrostSchemas.Anthropic
    }
    ```

#### [NEW] [provider_openai.go](file://shared/libs/go/llmgateway/provider_openai.go)
*   **Description**: OpenAI プロバイダー実装
*   **Technical Design**:
    ```go
    package llmgateway

    import (
        "net/http"

        bifrostSchemas "github.com/maximhq/bifrost/core/schemas"
    )

    func init() {
        RegisterProvider(&openaiProvider{})
    }

    type openaiProvider struct{}

    func (p *openaiProvider) Name() string { return "openai" }

    func (p *openaiProvider) BaseURL() string { return "https://api.openai.com" }

    func (p *openaiProvider) SetAuthHeaders(req *http.Request, apiKey string, originalHeaders http.Header) {
        req.Header.Set("Authorization", "Bearer "+apiKey)
    }

    func (p *openaiProvider) BifrostProvider() bifrostSchemas.ModelProvider {
        return bifrostSchemas.OpenAI
    }
    ```

#### [NEW] [provider_google.go](file://shared/libs/go/llmgateway/provider_google.go)
*   **Description**: Google (Gemini) プロバイダー実装
*   **Technical Design**:
    ```go
    package llmgateway

    import (
        "net/http"

        bifrostSchemas "github.com/maximhq/bifrost/core/schemas"
    )

    func init() {
        RegisterProvider(&googleProvider{})
    }

    type googleProvider struct{}

    func (p *googleProvider) Name() string { return "google" }

    func (p *googleProvider) BaseURL() string { return "https://generativelanguage.googleapis.com" }

    func (p *googleProvider) SetAuthHeaders(req *http.Request, apiKey string, originalHeaders http.Header) {
        req.Header.Set("x-goog-api-key", apiKey)
        req.Header.Del("Authorization")
        if req.URL.RawQuery != "" {
            req.URL.RawQuery = req.URL.RawQuery + "&key=" + apiKey
        } else {
            req.URL.RawQuery = "key=" + apiKey
        }
    }

    func (p *googleProvider) BifrostProvider() bifrostSchemas.ModelProvider {
        return bifrostSchemas.Google
    }
    ```

#### [NEW] [provider_ollama.go](file://shared/libs/go/llmgateway/provider_ollama.go)
*   **Description**: Ollama プロバイダー実装 (R4 前倒し: Provider 定義のみ)
*   **Technical Design**:
    ```go
    package llmgateway

    import (
        "net/http"

        bifrostSchemas "github.com/maximhq/bifrost/core/schemas"
    )

    func init() {
        RegisterProvider(&ollamaProvider{})
    }

    type ollamaProvider struct{}

    func (p *ollamaProvider) Name() string { return "ollama" }

    func (p *ollamaProvider) BaseURL() string { return "http://localhost:11434" }

    func (p *ollamaProvider) SetAuthHeaders(req *http.Request, apiKey string, originalHeaders http.Header) {
        // Ollama does not require authentication by default.
        // If apiKey is provided, set it as Bearer token (compatible with some setups).
        if apiKey != "" {
            req.Header.Set("Authorization", "Bearer "+apiKey)
        }
    }

    func (p *ollamaProvider) BifrostProvider() bifrostSchemas.ModelProvider {
        return bifrostSchemas.Ollama
    }
    ```

#### [MODIFY] [provider_forwarder.go](file://shared/libs/go/llmgateway/provider_forwarder.go)
*   **Description**: Provider Registry を使用するようリファクタリング
*   **Technical Design**:
    *   `providerBaseURLs` マップ (L18-22) を **削除**
    *   `forwardToProvider()` の `baseURL, ok := providerBaseURLs[provider]` を `p, ok := GetProvider(provider)` + `p.BaseURL()` に置換
    *   認証ヘッダー switch-case (L69-88) を `p.SetAuthHeaders(req, apiKey, originalHeaders)` に置換
*   **Logic**:
    ```go
    func (f *providerForwarder) forwardToProvider(
        provider string, path string, body []byte,
        apiKey string, originalHeaders http.Header, log logger.Logger,
    ) (*http.Response, error) {
        p, ok := GetProvider(provider)
        if !ok {
            return nil, &GatewayError{
                Type:    "api_error",
                Message: "unsupported provider: " + provider,
                Code:    "unsupported_provider",
                Status:  http.StatusBadRequest,
            }
        }
        upstreamURL := p.BaseURL() + path
        req, err := http.NewRequest(http.MethodPost, upstreamURL, bytes.NewReader(body))
        if err != nil {
            return nil, err
        }
        req.Header.Set("Content-Type", "application/json")
        p.SetAuthHeaders(req, apiKey, originalHeaders)
        // ... rest unchanged
    }
    ```

#### [MODIFY] [bifrost_account.go](file://shared/libs/go/llmgateway/bifrost_account.go)
*   **Description**: `providerNameMap` を Provider Registry に置換
*   **Technical Design**:
    *   既存の `providerNameMap` (L14-23) を **削除**
    *   `providerNameMap` 参照箇所を `GetProvider(name)` + `p.BifrostProvider()` に置換
    *   `resolveProviderForModel()` 等の内部関数で Provider Registry を使用

---

## Step-by-Step Implementation Guide

### Phase A: R8 リネーム

1.  **ディレクトリリネーム**:
    *   `shared/libs/go/hag/` を `shared/libs/go/tern/` にリネーム (`git mv`)
    *   全ファイルの `package hag` を `package tern` に変更

2.  **go.mod モジュールパス変更**:
    *   `shared/libs/go/go.mod` の `module github.com/axsh/hag` -> `module github.com/axsh/arctic-tern`

3.  **import パス一括置換**:
    *   `find shared/ examples/ tests/ -name '*.go' -exec sed -i 's|github.com/axsh/hag/|github.com/axsh/arctic-tern/|g' {} +`
    *   `github.com/axsh/hag/hag` -> `github.com/axsh/arctic-tern/tern` (ディレクトリリネーム反映)

4.  **コメント/ドキュメント更新**:
    *   .go ファイル内の "HAG" (コメント、ログプレフィックス) -> "tern" に変更
    *   README.md の更新
    *   scripts/ 内の参照更新

5.  **ビルド確認**:
    *   `./scripts/process/build.sh` で全体ビルドが通ることを確認
    *   コミット: `refactor: rename HAG to tern (arctic-tern)`

### Phase B: R1 codingagent Registry

6.  **テスト作成 (TDD)**:
    *   `codingagent/registry_test.go` を作成
    *   `./scripts/process/build.sh` でテストが **失敗** することを確認

7.  **Registry 実装**:
    *   `codingagent/registry.go` を作成 (FactoryFunc, Register, CreateAll, resetRegistry)
    *   `./scripts/process/build.sh` でテストが **成功** することを確認
    *   コミット: `feat: add codingagent factory/registry`

8.  **init() 自己登録**:
    *   `codingagent/claudecode/init.go` を作成
    *   `codingagent/codex/init.go` を作成
    *   `./scripts/process/build.sh` でビルド・テスト成功を確認
    *   コミット: `feat: add init() self-registration for claudecode and codex`

### Phase C: R2 Provider Registry

9.  **テスト作成 (TDD)**:
    *   `llmgateway/provider_test.go` を作成
    *   `./scripts/process/build.sh` でテストが **失敗** することを確認

10. **Provider インターフェースと Registry 実装**:
    *   `llmgateway/provider.go` を作成
    *   `./scripts/process/build.sh` でテストが **失敗** することを確認 (プロバイダー未登録)

11. **各プロバイダー実装**:
    *   `llmgateway/provider_anthropic.go` を作成
    *   `llmgateway/provider_openai.go` を作成
    *   `llmgateway/provider_google.go` を作成
    *   `llmgateway/provider_ollama.go` を作成
    *   `./scripts/process/build.sh` でテストが **成功** することを確認
    *   コミット: `feat: add provider interface and registry for llmgateway`

12. **provider_forwarder.go リファクタリング**:
    *   `providerBaseURLs` マップを削除
    *   認証ヘッダー switch-case を `p.SetAuthHeaders()` に置換
    *   `./scripts/process/build.sh` でビルド・テスト成功を確認
    *   コミット: `refactor: replace switch-case with provider registry in forwarder`

13. **bifrost_account.go リファクタリング**:
    *   `providerNameMap` を削除
    *   Provider Registry 経由で `BifrostProvider()` を取得するように変更
    *   `./scripts/process/build.sh` でビルド・テスト成功を確認
    *   コミット: `refactor: replace providerNameMap with provider registry`

### Phase D: 統合テスト + プッシュ

14. **統合テスト実行**:
    *   `./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories llm`
    *   リグレッションがないことを確認

15. **全テスト実行**:
    *   `./scripts/process/build.sh && ./scripts/process/integration_test.sh`
    *   全テスト成功を確認

16. **プッシュ**:
    *   `git push`

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```
    *   R8: 全ファイルの import パスが正しく解決され、ビルドが通ること
    *   R1: `codingagent/registry_test.go` が全て成功すること
    *   R2: `llmgateway/provider_test.go` が全て成功すること

2.  **Integration Tests (LLM カテゴリ)**:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories llm
    ```
    *   R2: Provider Registry 経由でのプロバイダー解決がリグレッションを起こしていないこと
    *   `llm_gateway_test.go` のテストが全て成功すること

3.  **Integration Tests (全カテゴリ)**:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh
    ```
    *   R8: リネーム後も全統合テストが成功すること

4.  **E2E Tests**:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestClaudeCodeE2E"
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestCodexE2E"
    ```
    *   R1: init() 自己登録で claudecode/codex アダプターが正しく登録されること
    *   既存 E2E テストがリグレッションなく成功すること

    **E2E テストのコード変更は不要**: 本 Part1 は純粋な内部リファクタリング (Factory/Registry パターン導入 + リネーム) であり、外部から観測可能な API 動作に変更はない。既存の E2E テスト (`agentservice_e2e_test.go`, `gemini_e2e_test.go`, `codex_e2e_test.go`) がそのまま通ることで、リグレッションがないことを検証する。

### テスト項目セルフレビュー (testing-rules 11.4)

1.  **網羅性**: R8 (リネーム) はビルド成功で検証。R1 (Registry) は Register/CreateAll の正常系・異常系・nil 返却をカバー。R2 (Provider) は全4プロバイダーの BaseURL/Auth/BifrostProvider をカバー。全テスト成功で機能が動作していると言える。
2.  **証拠の十分性**: 各テストは「期待する値が返る」「期待するヘッダーがセットされる」レベルまで検証。
3.  **迂回排除**: Provider Registry のテストで GetProvider が正しいインスタンスを返すことを確認。forwarder テストで旧 providerBaseURLs が使われていないことを確認。
4.  **依存関係**: provider_test.go (末端) -> provider_forwarder_test.go (中間) -> 統合テスト (全体) の順で検証。

### 総合判定プロセス (testing-rules 12)

全テスト完了後、testing-rules 12.2 のチェック項目 (スキップ有無、部分エラー、迂回処理、コンフィグ適用) を確認し、総合判定を記述する。

## Documentation

#### [MODIFY] [README.md](file://README.md)
*   **更新内容**: プロジェクト名を "Headless-Agent-Gateway" / "HAG" から "arctic-tern" / "tern" に変更。インストール手順の import パスを更新。

## 継続計画について

本実装計画は Part1 です。以下の Part で残りの要件を実装します:

- **Part2** (000-Factory-Registry-Bifrost-Part2): R3 (Bifrost SDK 一本化) + R4 (Ollama 機能テスト) + R5 (client ライブラリ)
- **Part3** (000-Factory-Registry-Bifrost-Part3): R6 (Example 簡素化/Viper/Cobra) + R7 (レガシーコード削除)
