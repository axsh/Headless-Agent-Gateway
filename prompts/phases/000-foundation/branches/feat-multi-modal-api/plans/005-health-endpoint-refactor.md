# 実装計画: /health エンドポイントの構成リファクタリング

> **Source Specification**: [005-health-endpoint-refactor.md](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-multi-modal-api/prompts/phases/000-foundation/branches/feat-multi-modal-api/ideas/005-health-endpoint-refactor.md)

## Goal Description

`/health` エンドポイントが返却する情報の構成を見直します。冗長なエージェント一覧（`agents`）を削除し、代わりに前提サービスである LLM Gateway Proxy (LLMGP) の最新ヘルスチェック日時（`last_checked_at`）と、サーバー起動時のオプション設定（`disable_sandbox`, `enable_subagent`, `enabled_versions`）を返すようにリファクタリングします。
また、ヘルスチェック時のパフォーマンス低下や他サービスへの余分なHTTPリクエストを避けるため、LLMGPへのヘルスチェックはバックグラウンドの定期ポーリングによってキャッシュ化する方式を実装します。

## User Review Required

None.

## Requirement Traceability

> **Traceability Check**:
> 仕様書(Specification)の要件・決定事項をリストアップし、この計画書のどこで対応するかをマッピングしてください。

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| `agents` フィールドの削除 | Proposed Changes > `health.go` |
| LLMGPの `last_checked_at` (RFC3339) フィールドの追加 | Proposed Changes > `health.go` |
| `server_settings` (`disable_sandbox`, `enable_subagent`, `enabled_versions`) の返却 | Proposed Changes > `health.go` |
| バックグラウンドでのLLMGPポーリングとキャッシュ処理の導入 | Proposed Changes > `service.go`, `health.go` |
| `ServerOption` を用いた `disableSandbox` と `enableSubagent` の設定 | Proposed Changes > `service.go`, `server.go` |
| 既存のテスト修正（`agents` 削除への追従） | Proposed Changes > `health_test.go`, `server_test.go`, `wayfinder_e2e_test.go`, `codex_e2e_test.go` 等 |

## Proposed Changes

### 1. `agentservice` ライブラリの変更

#### [MODIFY] [health.go](file://shared/libs/go/agentservice/health.go)
*   **Description**: `/health` エンドポイントのレスポンス構造体を再定義し、返却する情報を再構築します。バックグラウンドから更新されるLLMGPのヘルス情報をスレッドセーフに読み込み、返却します。
*   **Technical Design**:
    *   データモデルの再定義:
        ```go
        type HealthResponse struct {
        	Status         string            `json:"status"`
        	CLIVersions    map[string]string `json:"cli_versions"`
        	Gateway        GatewayHealth     `json:"gateway"`
        	ServerSettings ServerSettings    `json:"server_settings"`
        }

        type GatewayHealth struct {
        	Status        string    `json:"status"`
        	URL           string    `json:"url"`
        	Error         string    `json:"error,omitempty"`
        	LastCheckedAt time.Time `json:"last_checked_at"`
        }

        type ServerSettings struct {
        	DisableSandbox  bool  `json:"disable_sandbox"`
        	EnableSubagent  bool  `json:"enable_subagent"`
        	EnabledVersions []int `json:"enabled_versions"`
        }
        ```
    *   `startGatewayHealthPolling(ctx context.Context)` の実装：
        - 30秒ごとに `s.checkGatewayHealth()` を実行し、取得した `GatewayHealth` に `time.Now()` を設定して `s.lastGatewayHealth` にキャッシュする。
        - データの更新・参照は `s.gatewayHealthMu` ミューテックスにより排他制御する。
    *   `handleHealth` の実装：
        - `agents` を返却するコードを削除。
        - ミューテックスをロックし、`s.lastGatewayHealth` からキャッシュされた情報を取得。
        - `ServerSettings` を作成（`s.disableSandbox`, `s.enableSubagent`, `s.enabledVersions` を参照）。
        - レスポンスJSONを返却。

#### [MODIFY] [service.go](file://shared/libs/go/agentservice/service.go)
*   **Description**: サーバーのオプション項目（`disableSandbox`, `enableSubagent`）の保持フィールドと、LLMGPヘルスキャッシュ用フィールド、バックグラウンドループ制御用のコンテキストを追加します。
*   **Technical Design**:
    *   `Server` 構造体の定義変更:
        ```go
        type Server struct {
        	// 既存フィールド...
        	disableSandbox    bool
        	enableSubagent    bool
        	lastGatewayHealth GatewayHealth
        	gatewayHealthMu   sync.Mutex
        	pollCancel        context.CancelFunc
        }
        ```
    *   オプション関数の追加:
        ```go
        func WithSandboxDisabled(disabled bool) ServerOption {
        	return func(s *Server) { s.disableSandbox = disabled }
        }

        func WithSubagentEnabled(enabled bool) ServerOption {
        	return func(s *Server) { s.enableSubagent = enabled }
        }
        ```
    *   `Launch` メソッド：
        - 起動時に `lastGatewayHealth` の初期値（Status: "ok" など）を設定。
        - `context.WithCancel` でバックグラウンドポーリング用コンテキストを作成し、`go s.startGatewayHealthPolling(ctx)` を起動。
        - `s.pollCancel` を保持。
    *   `Shutdown` メソッド：
        - `s.pollCancel` が存在する場合、呼び出してバックグラウンドポーリングを停止する。

---

### 2. ファサードサーバーの変更

#### [MODIFY] [server.go](file://server/server.go)
*   **Description**: `resolveAgentService` 関数において、`agentservice.New` に起動引数のオプションを伝播します。
*   **Technical Design**:
    *   `resolveAgentService` 内で `agentservice.New` を構築する際、以下のようにオプションを追加します。
        ```go
        as := agentservice.New(
        	agentservice.WithLogger(log),
        	agentservice.WithTaskLog(tl),
        	agentservice.WithGatewayURL(gatewayURL),
        	agentservice.WithGatewayToken(gatewayToken),
        	agentservice.WithSandboxDisabled(disableSandbox),
        	agentservice.WithSubagentEnabled(enableSubagent),
        )
        ```

---

### 3. テストの修正

#### [MODIFY] [health_test.go](file://shared/libs/go/agentservice/health_test.go)
*   **Description**: 変更された `HealthResponse` 構造体のアサーションに追従させます。
*   **Technical Design**:
    - `HealthResponse` の JSON アンマーシャル先を最新の定義に変更。
    - `agents` のチェックを削除し、`server_settings` の各フィールド（`disable_sandbox`, `enable_subagent` 等）および `gateway.last_checked_at` が空でないこと・正しい値であることを検証する。

#### [MODIFY] [server_test.go](file://server/server_test.go)
*   **Description**: サーバー全体結合テストでの `/health` アサーションの修正。
*   **Technical Design**:
    - テスト内の `/health` レスポンス検証ロジックを最新の JSON 形式に修正。

#### [MODIFY] [wayfinder_e2e_test.go](file://tests/wayfinder_e2e_test.go)
*   **Description**: `GET /health` で利用可能なエージェント一覧に `wayfinder` が含まれているかを検証するアサーションを変更します。
*   **Technical Design**:
    - `/health` ではなく `GET /api/v1/agents` を呼び出し、取得したエージェント一覧に `wayfinder` が含まれることを検証する形に変更。

#### [MODIFY] [codex_e2e_test.go](file://tests/codex_e2e_test.go)
*   **Description**: `GET /health` での `codex` 存在アサーションを修正。
*   **Technical Design**:
    - `GET /api/v1/agents` を使用するアサーションに変更。

#### [MODIFY] [agentservice_integration_test.go](file://tests/agentservice_integration_test.go)
*   **Description**: `GET /health` 関連アサーションの修正。
*   **Technical Design**:
    - レスポンスのアンマーシャル構造体を最新のものに変更。
    - `agents` を利用していた箇所があれば修正。

---

## Step-by-Step Implementation Guide

1.  **`agentservice` 構造体とオプション定義の更新**:
    *   [shared/libs/go/agentservice/service.go](file://shared/libs/go/agentservice/service.go) に `disableSandbox`, `enableSubagent`, `lastGatewayHealth`, `gatewayHealthMu`, `pollCancel` フィールドを追加します。
    *   `WithSandboxDisabled` および `WithSubagentEnabled` オプション関数を定義します。
    *   `Launch` と `Shutdown` を修正し、バックグラウンドポーリングの起動と停止処理を実装します。

2.  **`health.go` における型とポーリング処理の実装**:
    *   [shared/libs/go/agentservice/health.go](file://shared/libs/go/agentservice/health.go) で `HealthResponse`, `GatewayHealth`, `ServerSettings` 構造体を最新のものに更新します。
    *   `startGatewayHealthPolling` を実装し、定期ポーリングで `lastGatewayHealth` の状態と `last_checked_at` を更新します。
    *   `handleHealth` でキャッシュデータと `ServerSettings` を組み合わせたレスポンス作成処理を実装します。

3.  **`server.go` の引数伝播の修正**:
    *   [server/server.go](file://server/server.go) 内の `resolveAgentService` を編集し、`WithSandboxDisabled` と `WithSubagentEnabled` オプションを渡します。

4.  **単体・結合テストの修正**:
    *   [shared/libs/go/agentservice/health_test.go](file://shared/libs/go/agentservice/health_test.go) を修正して変更後のレスポンスアサーションを実装します。
    *   [server/server_test.go](file://server/server_test.go) の `/health` アサーションを修正します。

5.  **E2E・統合テストの修正**:
    *   [tests/wayfinder_e2e_test.go](file://tests/wayfinder_e2e_test.go), [tests/codex_e2e_test.go](file://tests/codex_e2e_test.go) の `GET /health` でのエージェント存在チェックを `GET /api/v1/agents` に変更します。
    *   [tests/agentservice_integration_test.go](file://tests/agentservice_integration_test.go) を修正します。

6.  **ビルド＆テスト実行**:
    *   以下の Verification Plan に記載された検証コマンドを実行して、全てテストがパスすることを確認します。

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests**:
    ビルドスクリプトを実行して全体ビルドおよび単体テストが通過することを確認します。
    ```bash
    & "C:\Program Files\Git\bin\bash.exe" -c "./scripts/process/build.sh"
    ```

2.  **Integration Tests**:
    統合テストを実行して全てのシナリオが正常に動作することを確認します。
    特定のテストケースを選択して実行するコマンド例：
    ```bash
    & "C:\Program Files\Git\bin\bash.exe" -c "xvfb-run -a ./scripts/process/integration_test.sh --specify 'TestHealth'"
    ```
    全体の統合テストを実行するコマンド例：
    ```bash
    & "C:\Program Files\Git\bin\bash.exe" -c "xvfb-run -a ./scripts/process/integration_test.sh"
    ```

### Manual Verification
None. (すべて自動テストで検証されます。)

## Documentation

#### [MODIFY] [ReferenceManual-WebAPIs.md](file://docs/ReferenceManual-WebAPIs.md)
*   **更新内容**:
    - `GET /health` のレスポンス定義から `agents` を削除し、`gateway.last_checked_at` および `server_settings` フィールドの説明と JSON 例を追加。
