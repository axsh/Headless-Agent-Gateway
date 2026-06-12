# 049-SessionDir-Rename-Refactor

> **Source Specification**:
> - [036-SessionDir-Agent-Name-Default.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/ideas/036-SessionDir-Agent-Name-Default.md)
> - [037-Rename-Cawa-To-Tern.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/ideas/037-Rename-Cawa-To-Tern.md)
> - [038-LLMGateway-Provider-Subpackages.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/ideas/038-LLMGateway-Provider-Subpackages.md)

## Goal Description

3つの仕様を一括で実装する:

1. **036**: `session_dir` 未指定時のデフォルト値を `{work_dir}/.{agent_name}` にする
2. **037**: `examples/cawa-server` -> `features/tern`, `examples/cawa-client` -> `features/ternctl`, `examples/vault-cli` -> `features/vault-cli`, `examples/log-viewer` -> `features/log-viewer`
3. **038**: `llmgateway` パッケージのプロバイダ別サブパッケージ化

## User Review Required

- 037のリネームにより、既存のスクリプトや CI 設定に影響がある可能性がある。ビルドスクリプト (`build.sh`) は `examples/*/` と `features/*/` を両方走査する設計にする。

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| 036: session_dir デフォルト = work_dir + "." + agent_name | Proposed Changes > agentservice/handler.go |
| 036: ApplyDefaults の SessionDir フォールバック更新 | Proposed Changes > codingagent/options.go |
| 036: 明示的指定の優先 | handler.go: 既存の `if record.SessionDir == ""` 条件で保証 |
| 037: cawa-server -> tern | Proposed Changes > features/tern/ |
| 037: cawa-client -> ternctl | Proposed Changes > features/ternctl/ |
| 037: vault-cli, log-viewer 移動 | Proposed Changes > features/vault-cli, features/log-viewer |
| 037: ビルドスクリプト更新 | Proposed Changes > scripts/process/build.sh |
| 037: テストコード参照更新 | Proposed Changes > tests/ |
| 037: go.mod 更新 | Proposed Changes > features/*/go.mod |
| 038: anthropic サブパッケージ | Proposed Changes > llmgateway/anthropic/ |
| 038: openai サブパッケージ | Proposed Changes > llmgateway/openai/ |
| 038: google サブパッケージ | Proposed Changes > llmgateway/google/ |
| 038: ollama サブパッケージ | Proposed Changes > llmgateway/ollama/ |
| 038: ハンドラレジストリ | Proposed Changes > llmgateway/handler_registry.go |
| 038: デッドコード削除 (passthrough, TryFallbackOpenAIResponse, AllProviders) | Proposed Changes > llmgateway/ (削除) |

## Proposed Changes

### Part 1: SessionDir デフォルト値変更 (036)

---

#### [MODIFY] [handler_test.go](file://shared/libs/go/agentservice/handler_test.go)
*   **Description**: SessionDir のデフォルト値テストを追加
*   **Technical Design**:
    ```go
    func TestHandleCreateSession_SessionDirDefault(t *testing.T) {
        // session_dir 未指定、work_dir="tmp", agent="claudecode"
        // -> record.SessionDir == "tmp/.claudecode"
    }
    func TestHandleCreateSession_SessionDirExplicit(t *testing.T) {
        // session_dir="custom/dir" が明示指定時は上書きされない
    }
    ```

#### [MODIFY] [handler.go](file://shared/libs/go/agentservice/handler.go)
*   **Description**: SessionDir フォールバックロジックを変更
*   **Technical Design**:
    ```go
    // 変更前 (L95-98):
    // if record.SessionDir == "" && record.WorkDir != "" {
    //     record.SessionDir = record.WorkDir
    // }

    // 変更後:
    if record.SessionDir == "" && record.WorkDir != "" {
        record.SessionDir = filepath.Join(record.WorkDir, "."+record.AgentName)
    }
    ```
*   **Logic**: `filepath` パッケージのインポートを追加。`record.AgentName` は L89 で既に設定済み。

#### [MODIFY] [options.go](file://shared/libs/go/codingagent/options.go)
*   **Description**: `ApplyDefaults` の SessionDir フォールバックも同様に変更。ただし `AgentName` を知らないため、`AdapterConfig` にフィールドを追加する。
*   **Technical Design**:
    ```go
    // AdapterConfig に AgentName フィールド追加
    type AdapterConfig struct {
        AgentName        string // Agent name for directory naming
        // ... 既存フィールド
    }

    // ApplyDefaults 内:
    // 変更前:
    // } else if cfg.WorkDir != "" {
    //     cfg.SessionDir = cfg.WorkDir
    // }
    
    // 変更後:
    } else if cfg.WorkDir != "" && ac.AgentName != "" {
        cfg.SessionDir = filepath.Join(cfg.WorkDir, "."+ac.AgentName)
    } else if cfg.WorkDir != "" {
        cfg.SessionDir = cfg.WorkDir
    }
    ```

#### [MODIFY] [options_test.go](file://shared/libs/go/codingagent/options_test.go)
*   **Description**: `ApplyDefaults` の SessionDir フォールバックテストを更新
*   **Logic**: `AgentName: "claudecode"` 設定時に `SessionDir` が `WorkDir + "/.claudecode"` になることを確認

---

### Part 2: cawa -> tern リネームと features/ 移動 (037)

---

#### ディレクトリ移動 (git mv)

以下のコマンドで移動:
```bash
git mv examples/cawa-server features/tern
git mv examples/cawa-client features/ternctl
git mv examples/vault-cli features/vault-cli
git mv examples/log-viewer features/log-viewer
```

#### [MODIFY] [features/tern/go.mod](file://features/tern/go.mod)
*   **Description**: module パスを更新
*   **Logic**: `module github.com/axsh/arctic-tern/examples/cawa-server` -> `module github.com/axsh/arctic-tern/features/tern`

#### [MODIFY] [features/tern/cmd/root.go](file://features/tern/cmd/root.go)
*   **Description**: コマンド名の参照を `cawa-server` -> `tern` に更新

#### [MODIFY] [features/tern/main.go](file://features/tern/main.go)
*   **Description**: import パスを更新

#### [MODIFY] [features/tern/Dockerfile](file://features/tern/Dockerfile)
*   **Description**: バイナリ名を `cawa-server` -> `tern` に更新

#### [MODIFY] [features/tern/docker-compose.yaml](file://features/tern/docker-compose.yaml)
*   **Description**: サービス名・イメージ名を `cawa-server` -> `tern` に更新

#### [MODIFY] [features/ternctl/go.mod](file://features/ternctl/go.mod)
*   **Description**: module パスを更新
*   **Logic**: `module github.com/axsh/arctic-tern/examples/cawa-client` -> `module github.com/axsh/arctic-tern/features/ternctl`

#### [MODIFY] [features/ternctl/main.go](file://features/ternctl/main.go)
*   **Description**: `cawa-client` 文字列参照を `ternctl` に更新

#### [MODIFY] [features/vault-cli/go.mod](file://features/vault-cli/go.mod)
*   **Description**: module パスを更新

#### [MODIFY] [features/log-viewer/go.mod](file://features/log-viewer/go.mod)
*   **Description**: module パスを更新

#### [MODIFY] [scripts/process/build.sh](file://scripts/process/build.sh)
*   **Description**: `examples/*/` に加えて `features/*/` も走査対象にする
*   **Technical Design**:
    ```bash
    # 変更前: examples/*/ のみ走査
    # for example_dir in examples/*/; do

    # 変更後: examples/ と features/ を両方走査
    for build_dir in examples/*/ features/*/; do
        [[ -d "$build_dir" ]] || continue
        if [[ ! -f "$build_dir/go.mod" ]]; then
            continue
        fi
        # ... 既存ロジック (変数名を example -> build に統一)
    done
    ```

#### [MODIFY] [tests/agentservice_e2e_test.go](file://tests/agentservice_e2e_test.go)
*   **Description**: `model_profiles.yaml` のパス参照を更新
*   **Logic**:
    - L58: `../examples/cawa-server/model_profiles.yaml` -> `../features/tern/model_profiles.yaml`
    - L454: 同上
    - コメント内の `cawa-server`, `cawa-client` 参照を `tern`, `ternctl` に更新

#### [MODIFY] [tests/examples_build_test.go](file://tests/examples_build_test.go)
*   **Description**: `TestExamples_CawaServer_Builds` を `TestFeatures_Tern_Builds` にリネーム、パスを `features/tern` に変更。`features/ternctl` 用テストも追加。

#### [MODIFY] [tests/codex_e2e_test.go](file://tests/codex_e2e_test.go)
*   **Description**: `model_profiles.yaml` パスを更新

#### [MODIFY] [tests/gemini_e2e_test.go](file://tests/gemini_e2e_test.go)
*   **Description**: `model_profiles.yaml` パスを更新

#### [MODIFY] [tests/agentservice_integration_test.go](file://tests/agentservice_integration_test.go)
*   **Description**: `cawa-server` 参照を更新

#### [MODIFY] [examples/minimal-client/main.go](file://examples/minimal-client/main.go)
*   **Description**: コメント内の `cawa-server` 参照を更新

#### [MODIFY] [scripts/test/container_test.sh](file://scripts/test/container_test.sh)
*   **Description**: `cawa-server` 参照を更新

---

### Part 3: llmgateway プロバイダ別サブパッケージ化 (038)

---

#### デッドコード削除

以下のファイルを削除:
```bash
git rm shared/libs/go/llmgateway/passthrough.go
git rm shared/libs/go/llmgateway/passthrough_test.go
```

以下の関数を削除:
- `TryFallbackOpenAIResponse` (fallback.go:41-116) -- 本番コードから参照なし
- `AllProviders` (provider.go:52-60) -- テストからのみ参照

#### [NEW] [handler_registry.go](file://shared/libs/go/llmgateway/handler_registry.go)
*   **Description**: プロバイダ別HTTPハンドラを動的登録するレジストリ
*   **Technical Design**:
    ```go
    package llmgateway

    import "sync"

    // HandlerContext provides dependencies for provider-specific handlers.
    type HandlerContext interface {
        GetLogger() logger.Logger
        GetConfig() *config.AppConfig
        GetVault() vault.VaultStore
        GetBifrostDriver() *BifrostDriver
    }

    // HandlerRegistration describes a registered HTTP handler.
    type HandlerRegistration struct {
        Method  string // "POST", "GET", etc.
        Path    string // e.g. "/v1/messages"
        Factory func(ctx HandlerContext) http.HandlerFunc
    }

    var (
        handlerMu       sync.RWMutex
        handlerRegistry []HandlerRegistration
    )

    // RegisterHandler registers a provider-specific HTTP handler factory.
    func RegisterHandler(method, path string, factory func(ctx HandlerContext) http.HandlerFunc) {
        handlerMu.Lock()
        defer handlerMu.Unlock()
        handlerRegistry = append(handlerRegistry, HandlerRegistration{
            Method: method, Path: path, Factory: factory,
        })
    }

    // AllHandlers returns all registered handler factories.
    func AllHandlers() []HandlerRegistration {
        handlerMu.RLock()
        defer handlerMu.RUnlock()
        result := make([]HandlerRegistration, len(handlerRegistry))
        copy(result, handlerRegistry)
        return result
    }
    ```

#### [NEW] [shared/libs/go/llmgateway/anthropic/](file://shared/libs/go/llmgateway/anthropic/)

`llmgateway/anthropic/` サブパッケージを新規作成。以下のファイルを移動・リネーム:

##### [NEW] [provider.go](file://shared/libs/go/llmgateway/anthropic/provider.go)
*   **Description**: `provider_anthropic.go` を移動。パッケージ名を `anthropic` に変更。
*   **Technical Design**:
    ```go
    package anthropic

    import (
        "net/http"
        bifrostSchemas "github.com/maximhq/bifrost/core/schemas"
        "github.com/axsh/arctic-tern/llmgateway"
    )

    func init() {
        llmgateway.RegisterProvider(&provider{})
    }

    type provider struct{}
    func (p *provider) Name() string { return "anthropic" }
    func (p *provider) BaseURL() string { return "https://api.anthropic.com" }
    func (p *provider) SetAuthHeaders(req *http.Request, apiKey string, originalHeaders http.Header) {
        req.Header.Set("x-api-key", apiKey)
        req.Header.Set("anthropic-version", "2023-06-01")
        if beta := originalHeaders.Get("anthropic-beta"); beta != "" {
            req.Header.Set("anthropic-beta", beta)
        }
    }
    func (p *provider) BifrostProvider() bifrostSchemas.ModelProvider {
        return bifrostSchemas.Anthropic
    }
    ```

##### [NEW] [handler.go](file://shared/libs/go/llmgateway/anthropic/handler.go)
*   **Description**: `proxy_anthropic.go` の HTTPハンドラロジックを移動。`init()` で `RegisterHandler` を呼び出す。
*   **Logic**: `handleAnthropicMessages`, `handleAnthropicMessagesViaBifrost`, `handleAnthropicMessagesBifrostStream`, `handleAnthropicMessagesBifrostNonStream`, `emitSSEJSON` を移動。`ProxyServer` への依存を `HandlerContext` インターフェース経由に変更。

##### [NEW] [convert.go](file://shared/libs/go/llmgateway/anthropic/convert.go)
*   **Description**: `convert_anthropic_bifrost.go` を移動

##### [NEW] [types.go](file://shared/libs/go/llmgateway/anthropic/types.go)
*   **Description**: `types_anthropic.go` を移動

##### [NEW] [fallback.go](file://shared/libs/go/llmgateway/anthropic/fallback.go)
*   **Description**: `fallback.go` から Anthropic 固有部分 (`TryFallbackAnthropicResponse`, `ExtractToolCallFromText`, `ExtractSessionID`, `ExtractFallbackFlag`) を移動

##### テストファイルも対応するサブパッケージに移動

#### [NEW] [shared/libs/go/llmgateway/openai/](file://shared/libs/go/llmgateway/openai/)

##### [NEW] [provider.go](file://shared/libs/go/llmgateway/openai/provider.go)
*   **Description**: `provider_openai.go` を移動

##### [NEW] [handler.go](file://shared/libs/go/llmgateway/openai/handler.go)
*   **Description**: `proxy_openai.go` の HTTPハンドラロジックを移動

#### [NEW] [shared/libs/go/llmgateway/google/provider.go](file://shared/libs/go/llmgateway/google/provider.go)
*   **Description**: `provider_google.go` を移動

#### [NEW] [shared/libs/go/llmgateway/ollama/provider.go](file://shared/libs/go/llmgateway/ollama/provider.go)
*   **Description**: `provider_ollama.go` を移動

#### [MODIFY] [proxy.go](file://shared/libs/go/llmgateway/proxy.go)
*   **Description**: `setupRoutes` をレジストリベースに変更
*   **Technical Design**:
    ```go
    func (p *ProxyServer) setupRoutes(mux *http.ServeMux) {
        // Static routes (core)
        mux.HandleFunc("GET /{$}", p.handleIndex)
        mux.HandleFunc("GET /health", p.handleHealth)
        mux.HandleFunc("GET /v1/models", p.handleModels)

        // Dynamic routes from provider registry
        for _, h := range AllHandlers() {
            handler := h.Factory(p) // ProxyServer implements HandlerContext
            mux.HandleFunc(h.Method+" "+h.Path, p.authMiddleware(handler))
        }
    }
    ```
*   **Logic**: `ProxyServer` に `HandlerContext` インターフェースのメソッドを実装。

#### [DELETE] 以下のファイルを削除 (サブパッケージに移動済み)
- `shared/libs/go/llmgateway/provider_anthropic.go`
- `shared/libs/go/llmgateway/provider_google.go`
- `shared/libs/go/llmgateway/provider_openai.go`
- `shared/libs/go/llmgateway/provider_ollama.go`
- `shared/libs/go/llmgateway/proxy_anthropic.go`
- `shared/libs/go/llmgateway/proxy_openai.go`
- `shared/libs/go/llmgateway/convert_anthropic_bifrost.go`
- `shared/libs/go/llmgateway/types_anthropic.go`
- `shared/libs/go/llmgateway/fallback.go`
- `shared/libs/go/llmgateway/passthrough.go`

#### インポート更新

サブパッケージの `init()` 自動登録を有効にするため、呼び出し元でブランクインポートを追加:

##### [MODIFY] [features/tern/cmd/server.go](file://features/tern/cmd/server.go) (旧 examples/cawa-server)
```go
import (
    _ "github.com/axsh/arctic-tern/llmgateway/anthropic"
    _ "github.com/axsh/arctic-tern/llmgateway/openai"
    _ "github.com/axsh/arctic-tern/llmgateway/google"
    _ "github.com/axsh/arctic-tern/llmgateway/ollama"
)
```

---

## Step-by-Step Implementation Guide

### Phase 1: SessionDir デフォルト値変更 (036)

1. **AdapterConfig に AgentName フィールド追加**:
   - Edit `shared/libs/go/codingagent/adapter_config.go`: `AgentName string` フィールドを追加
2. **options_test.go にテスト追加**:
   - `ApplyDefaults` で `AgentName` 設定時の `SessionDir` フォールバック確認テスト
3. **options.go の ApplyDefaults 修正**:
   - `SessionDir` フォールバックロジックを `filepath.Join(cfg.WorkDir, "."+ac.AgentName)` に変更
4. **handler.go のフォールバック修正**:
   - L95-98: `record.SessionDir = filepath.Join(record.WorkDir, "."+record.AgentName)`
5. **ビルド確認**: `./scripts/process/build.sh`
6. **コミット**: `git commit -m "feat: session_dir default includes agent name"`

### Phase 2: cawa -> tern リネーム (037)

7. **ディレクトリ移動**:
   ```bash
   git mv examples/cawa-server features/tern
   git mv examples/cawa-client features/ternctl
   git mv examples/vault-cli features/vault-cli
   git mv examples/log-viewer features/log-viewer
   ```
8. **go.mod 更新**: 4つの `features/*/go.mod` の module パスを更新
9. **features/tern/ 内のコード更新**: cmd/root.go, main.go, Dockerfile, docker-compose.yaml
10. **features/ternctl/ 内のコード更新**: main.go
11. **build.sh 更新**: `features/*/` 走査を追加
12. **テストコード更新**: model_profiles.yaml パス、テスト関数名
13. **ビルド確認**: `./scripts/process/build.sh`
14. **コミット**: `git commit -m "refactor: rename cawa to tern, move to features/"`

### Phase 3: llmgateway プロバイダサブパッケージ化 (038)

15. **デッドコード削除**: `passthrough.go`, `passthrough_test.go` 削除、`TryFallbackOpenAIResponse`, `AllProviders` 削除
16. **コミット**: `git commit -m "chore: remove dead code from llmgateway"`
17. **handler_registry.go 新規作成**: `HandlerContext`, `RegisterHandler`, `AllHandlers`
18. **anthropic/ サブパッケージ作成**: provider.go, handler.go, convert.go, types.go, fallback.go + テスト
19. **openai/ サブパッケージ作成**: provider.go, handler.go + テスト
20. **google/ サブパッケージ作成**: provider.go
21. **ollama/ サブパッケージ作成**: provider.go
22. **proxy.go の setupRoutes 修正**: レジストリベースに変更
23. **ProxyServer に HandlerContext 実装**
24. **旧ファイル削除**: provider_anthropic.go 等
25. **ブランクインポート追加**: features/tern/cmd/server.go
26. **ビルド確認**: `./scripts/process/build.sh`
27. **コミット**: `git commit -m "refactor: split llmgateway into provider subpackages"`

### Phase 4: 検証

28. **全体ビルド + テスト実行**: Verification Plan 参照

## Verification Plan

### Automated Verification

E2Eテストの新規追加は不要。理由: 本計画は純粋な内部リファクタリング（036 の SessionDir デフォルト値変更を除く）であり、外部から観測可能なAPIの振る舞いは変わらない。036 については既存の E2E テストでセッション作成時の `session_dir` が正しく設定されることが検証される。

1. **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```

2. **Integration Tests (E2E)**:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestE2E_"
    ```
    *   **Log Verification**:
        - `bin/tern` と `bin/ternctl` が生成されていること
        - `bin/cawa-server`, `bin/cawa-client` が生成されないこと
        - E2E テストが `features/tern/model_profiles.yaml` を正しく参照すること

3. **ビルドテスト (Examples/Features)**:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestFeatures_\|TestExamples_"
    ```

### テスト設計セルフレビュー

- **網羅性**: 036 の SessionDir 変更は handler_test.go と options_test.go でカバー。037/038 はリファクタリングのため既存テストの通過で十分。
- **証拠の十分性**: SessionDir の値が期待通りかを文字列比較で確認。ビルド成功 + 既存 E2E テスト通過で動作を確認。
- **迂回排除**: プロバイダサブパッケージの `init()` 登録はブランクインポートで保証。
- **依存関係**: handler_registry -> anthropic/openai サブパッケージ -> proxy.go の順で依存。

## Documentation

#### [MODIFY] [README.md](file://README.md)
*   **更新内容**: `cawa-server` / `cawa-client` への言及を `tern` / `ternctl` に更新。`features/` ディレクトリの説明を追加。
