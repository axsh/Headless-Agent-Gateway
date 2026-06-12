# 048-Fix-Agent-Auto-Registration

> **Source Specification**: [035-Fix-Agent-Auto-Registration.md](file:///prompts/phases/000-foundation/branches/feat-llm-backend/ideas/035-Fix-Agent-Auto-Registration.md)

## Goal Description

サーバーファサード `tern.New()` の初期化処理において、`codingagent` グローバルレジストリに `init()` 関数で自己登録されたエージェントファクトリを `codingagent.CreateAll()` によりインスタンス化し、`agentservice.Server` に自動登録する処理を追加する。併せて、E2Eテストにおける手動登録コードを適切に整理し、自動登録が実運用で機能することを保証する。

## User Review Required

> [!IMPORTANT]
> **E2Eテストの手動登録の扱いについて**
> 以下のテストでは、意図的に通常の自動登録とは異なる設定（偽のGateway URLや特定のモデル指定）でエージェントを登録しています。これらは自動登録の対象外とし、手動登録を維持します:
> - `TestE2E_CodingAgentError` (TC-005): 偽のGateway URLでエラー伝播を検証
> - `TestGeminiE2E_CawaClient_FileCreation`: Geminiモデル指定でのclaude CLI実行
> - `TestCodexE2E_ErrorPropagation` (TC-Codex-003): 偽のGateway URLでエラー伝播を検証
>
> 一方、以下のヘルパー関数は自動登録に移行します:
> - `startE2EServer()`: claudecodeエージェントの手動登録を削除
> - `startCodexE2EServer()`: codexエージェントの手動登録を削除

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: `tern.New()` でレジストリからエージェントを自動インスタンス化して `agentservice.Server` に登録 | Proposed Changes > tern > server.go |
| R1: `AdapterConfig` に `GatewayURL`, `GatewayToken`, `Logger`, `DefaultModel`, `ToolCallFallback` を設定 | Proposed Changes > tern > server.go (`resolveAgentService` 関数内) |
| R2: `startE2EServer` の手動 `RegisterAgent` を削除 | Proposed Changes > tests > agentservice_e2e_test.go |
| R2: `startCodexE2EServer` の手動 `RegisterAgent` を削除 | Proposed Changes > tests > codex_e2e_test.go |
| R2: 自動登録でサーバーが正常に稼働することをE2Eテストで保証 | Verification Plan |

## Proposed Changes

### tern (サーバーファサード)

#### [MODIFY] [server.go](file:///shared/libs/go/tern/server.go)

*   **Description**: `resolveAgentService` 関数にゲートウェイ (`gw llmgateway.LLMGatewayBackend`) パラメータを追加し、`codingagent.CreateAll()` によるエージェント自動登録処理を組み込む。
*   **Technical Design**:
    *   `resolveAgentService` のシグネチャ変更:
        ```go
        func resolveAgentService(
            o *options, log logger.Logger, tl *tasklog.TaskLog,
            gatewayURL string, gatewayToken string, caCertPath string,
            gw llmgateway.LLMGatewayBackend,
        ) *agentservice.Server
        ```
    *   新規 import: `"github.com/axsh/arctic-tern/codingagent"`
*   **Logic**:
    1.  既存のロジック (`o.agentService != nil` チェック、`NODE_EXTRA_CA_CERTS` 設定、`agentservice.New()` 呼び出し) は維持する。
    2.  `agentservice.New()` の直後に、以下の自動登録ロジックを追加:
        ```go
        // 自己申告されたエージェントをレジストリから構築して登録
        defaultModel := ""
        toolCallFallback := false
        if gw != nil {
            if dm := gw.DefaultModel(); dm != nil {
                defaultModel = dm.Model
                toolCallFallback = dm.ToolCallFallback
            }
        }

        adapterCfg := &codingagent.AdapterConfig{
            GatewayURL:       gatewayURL,
            GatewayToken:     gatewayToken,
            Logger:           log,
            DefaultModel:     defaultModel,
            ToolCallFallback: toolCallFallback,
        }

        for _, agent := range codingagent.CreateAll(adapterCfg) {
            as.RegisterAgent(agent)
        }
        ```
    3.  `tern.New()` 内の `resolveAgentService` 呼び出し箇所（現在145行目付近）を更新し、`gw` 引数を追加:
        ```go
        // 変更前:
        as := resolveAgentService(o, log, tl, gatewayURL, gatewayToken, caCertPath)

        // 変更後:
        as := resolveAgentService(o, log, tl, gatewayURL, gatewayToken, caCertPath, gw)
        ```

---

### tests (E2Eテスト)

#### [MODIFY] [agentservice_e2e_test.go](file:///tests/agentservice_e2e_test.go)

*   **Description**: `startE2EServer()` 内の手動エージェント登録コード（96-102行目）を削除し、自動登録に依拠するよう変更する。
*   **Technical Design**:
    *   削除対象コード:
        ```go
        // Register real claudecode agent with gateway URL.
        // ProxyURL must be called after Launch to get the actual port.
        gwURL := srv.Gateway().ProxyURL()
        adapter := claudecode.New(&codingagent.AdapterConfig{
            GatewayURL:     gwURL,
            DefaultModel:   e2eDefaultModel,
            DisableSandbox: true,
        })
        srv.AgentService().RegisterAgent(adapter)
        ```
    *   削除後、`import` ブロックから未使用となる `claudecode` パッケージのインポートを blank import (`_`) に変更する。既に同一テストバイナリ内で `claudecode` パッケージが他のテストファイル（`gemini_e2e_test.go`）から直接インポートされているため、`init()` は発火する。ただし、`agentservice_e2e_test.go` 内では `claudecode.New` の呼び出しがTC-005で残るため、直接インポートは維持される。
    *   TC-005 (`TestE2E_CodingAgentError`) の手動登録は意図的なもの（偽のGateway URL）であるため維持する。
*   **Logic**:
    *   `startE2EServer()` の構造:
        1. claude CLI 存在確認（維持）
        2. 設定ファイル生成・書き込み（維持）
        3. `tern.New()` + `Launch()`（維持）-- これにより自動登録が発火する
        4. ~~手動 RegisterAgent~~（**削除**）
        5. ポート取得・URL構築・cleanup定義（維持）
    *   `import` の `"github.com/axsh/arctic-tern/codingagent/claudecode"` は TC-005 で `claudecode.New()` を使用するため維持。`"github.com/axsh/arctic-tern/codingagent"` も TC-005 で `codingagent.AdapterConfig` を使用するため維持。

#### [MODIFY] [codex_e2e_test.go](file:///tests/codex_e2e_test.go)

*   **Description**: `startCodexE2EServer()` 内の手動エージェント登録コード（75-81行目）を削除し、自動登録に依拠するよう変更する。
*   **Technical Design**:
    *   削除対象コード:
        ```go
        // Register real codex agent with gateway URL.
        gwURL := srv.Gateway().ProxyURL()
        codexAdapter := codex.New(&codingagent.AdapterConfig{
            GatewayURL:   gwURL,
            DefaultModel: "gpt-4o",
        })
        srv.AgentService().RegisterAgent(codexAdapter)
        ```
    *   TC-Codex-003 (`TestCodexE2E_ErrorPropagation`) の手動登録は意図的なもの（偽のGateway URL）であるため維持する。
    *   `import` の `"github.com/axsh/arctic-tern/codingagent/codex"` は TC-Codex-003 で `codex.New()` を使用するため維持。

## Step-by-Step Implementation Guide

### Step 1: `tern/server.go` の単体テストを作成

1.  Edit [server_test.go](file:///shared/libs/go/tern/server_test.go) (or create if not exists)
2.  `resolveAgentService` に `gw` パラメータを追加した後のロジックを検証するテストケースを追加:
    *   **T1: `gw == nil` の場合、エージェントは0件登録される（レジストリが空の前提）**
    *   **T2: `gw` が `DefaultModel` を返す場合、`AdapterConfig.DefaultModel` と `ToolCallFallback` が正しく設定される**
    *   テストでは `codingagent.resetRegistry()` を使ってグローバルレジストリを制御する（ただし `resetRegistry` はパッケージ外からアクセス不可のため、`codingagent.CreateAll` の結果を `agentservice.Server` 経由で確認する）

> [!IMPORTANT]
> `codingagent.resetRegistry()` は非公開関数であるため、`tern` パッケージの単体テストからは直接呼べない。代わりに、`llmgateway.StubGateway` を使って `gw.DefaultModel()` の返り値を制御し、`resolveAgentService` が返す `agentservice.Server` のエージェント一覧を確認するアプローチを取る。テスト環境では `init()` により `claudecode` / `codex` が登録されるが、CLI が PATH に存在しない場合は `(nil, nil)` で無視される。

### Step 2: `tern/server.go` の `resolveAgentService` を修正

1.  Edit [server.go](file:///shared/libs/go/tern/server.go):
    *   import に `"github.com/axsh/arctic-tern/codingagent"` を追加
    *   `resolveAgentService` のシグネチャに `gw llmgateway.LLMGatewayBackend` パラメータを追加
    *   `agentservice.New()` の後、`return` の前に自動登録ロジックを追加（Proposed Changes > tern > server.go の Logic 参照）
    *   `New()` 関数内の `resolveAgentService` 呼び出し（145行目付近）に `gw` 引数を追加

### Step 3: `agentservice_e2e_test.go` の `startE2EServer` を修正

1.  Edit [agentservice_e2e_test.go](file:///tests/agentservice_e2e_test.go):
    *   `startE2EServer()` 関数から手動エージェント登録コード（96-102行目）を削除
    *   import は TC-005 で引き続き使用するため維持

### Step 4: `codex_e2e_test.go` の `startCodexE2EServer` を修正

1.  Edit [codex_e2e_test.go](file:///tests/codex_e2e_test.go):
    *   `startCodexE2EServer()` 関数から手動エージェント登録コード（75-81行目）を削除
    *   import は TC-Codex-003 で引き続き使用するため維持

### Step 5: ビルド検証

1.  `./scripts/process/build.sh` を実行し、コンパイルエラーがないことを確認

### Step 6: 統合テスト検証 (Verification Plan 実行)

1.  `./scripts/process/integration_test.sh --specify "TestE2E_StandaloneHealth"` を実行
2.  結果を確認し、`agents` に `claudecode` が含まれることを検証

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```

2.  **Integration Tests (選択実行)**:
    まず、自動登録に直接関連するテストのみを実行:
    ```bash
    ./scripts/process/integration_test.sh --specify "TestE2E_StandaloneHealth"
    ```
    *   **Log Verification**: `health.agents` に `claudecode` が含まれること。手動登録なしでエージェントが認識されていることの直接的証拠。

    次に、自動登録を前提とした `startE2EServer` を使う全テストを実行:
    ```bash
    ./scripts/process/integration_test.sh --specify "TestE2E_"
    ```
    *   **Log Verification**: `TestE2E_CodingAgentStreaming`, `TestE2E_CodingAgentDefaultModel`, `TestE2E_SessionContinuation`, `TestE2E_SessionDirFallback` が自動登録のみで成功すること。

    Codex テストも同様:
    ```bash
    ./scripts/process/integration_test.sh --specify "TestCodexE2E_"
    ```
    *   **Log Verification**: `TestCodexE2E_HealthWithCodexAgent` で `codex` が agents に含まれること。

3.  **E2E Tests (新規/追加)**:

    本修正は内部リファクタリング（自動登録の導入）であり、外部から観測可能な動作に変更はない。既存のE2Eテスト（`TestE2E_StandaloneHealth`, `TestE2E_CodingAgentStreaming` 等）が手動登録なしで成功すること自体が、自動登録が正しく機能していることの検証となる。

    したがって、新規E2Eテストの追加は不要。既存テストの修正（手動登録コードの削除）が、そのままテストケースとなる。

### テスト項目設計

#### 観点チェックリスト

| # | 観点 | テスト項目 | 検証方法 |
|---|------|-----------|---------|
| 1 | 正常系の動作確認 | 自動登録によりエージェントがヘルスチェックに表示される | `TestE2E_StandaloneHealth` |
| 2 | 正常系の動作確認 | 自動登録されたエージェントでセッション作成・メッセージ送信が成功する | `TestE2E_CodingAgentStreaming` |
| 3 | 正常系の動作確認 | DefaultModel がゲートウェイから自動取得される | `TestE2E_CodingAgentDefaultModel` |
| 4 | 正常系の動作確認 | セッション継続が自動登録でも機能する | `TestE2E_SessionContinuation` |
| 5 | 異常系・境界値 | エラー伝播が手動登録（意図的）テストで引き続き機能する | `TestE2E_CodingAgentError` |
| 6 | 設定・構成の反映 | `resolveAgentService` に渡す `gw` から `DefaultModel` と `ToolCallFallback` が正しく設定される | ビルド時の単体テスト + `TestE2E_CodingAgentDefaultModel` |
| 7 | 副作用の確認 | 既存の `WithAgentService` オプションによる外部注入が引き続き機能する（`o.agentService != nil` で早期リターン） | ビルド時の単体テスト |

#### セルフレビュー

1. **網羅性の検証**: 自動登録の正常動作は `TestE2E_StandaloneHealth`（エージェント表示）、`TestE2E_CodingAgentStreaming`（実際のCLI呼び出し）、`TestE2E_CodingAgentDefaultModel`（DefaultModel自動設定）で検証される。異常系は既存のTC-005で維持。これらが全て成功すれば、自動登録が正しく機能していると言える。
2. **証拠の十分性**: ヘルスチェックでの `agents` リスト確認は「エージェントが登録されている」証拠。ストリーミングテストの成功は「登録されたエージェントが正しい設定で動作している」証拠。
3. **迂回・抜け道の排除**: テストコードから手動登録を削除することで、自動登録のみが唯一の登録経路となり、迂回は不可能。
4. **依存関係の整合性**: `resolveAgentService` -> `codingagent.CreateAll` -> `init()` の順序は Go ランタイムにより保証される。

## Documentation

#### [MODIFY] [035-Fix-Agent-Auto-Registration.md](file:///prompts/phases/000-foundation/branches/feat-llm-backend/ideas/035-Fix-Agent-Auto-Registration.md)
*   **更新内容**: 実装完了後、実装状態を反映するステータスの追記（必要に応じて）
