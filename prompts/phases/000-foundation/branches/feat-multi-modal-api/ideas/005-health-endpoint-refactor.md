# 仕様書: /health エンドポイントの構成リファクタリング

## 背景

現在、`/health` エンドポイントは登録されているエージェント一覧（`agents`）を返していますが、これらは `/api/v1/agents` エンドポイントから取得できるため冗長です。
また、CAWAの稼働状況をより詳細に把握するためには、前提となるLLM Gateway Proxy (LLMGP) の健全性ステータスや最終チェック時刻、さらにサーバー起動時の主要な設定（サンドボックス無効化、サブエージェント有効化などの起動オプション）を提供することが望まれます。

## 要件

1. **`agents` フィールドの削除**
   - `/health` レスポンスから `agents` フィールドを削除する。

2. **LLMGPヘルスチェック情報の強化**
   - LLMGPのヘルスステータスに加え、最後にヘルスチェックを実施した日時（タイムスタンプ）をRFC3339形式で返す `last_checked_at` フィールドを追加する。

3. **サーバー設定情報の返却**
   - `/health` レスポンスに `server_settings` フィールドを追加し、以下のサーバー起動時のオプション設定を返す。
     - `disable_sandbox` (boolean): サンドボックス機能が制限（無効化）されているか。
     - `enable_subagent` (boolean): サブエージェント機能が有効化されているか。
     - `enabled_versions` (array of integers): 有効化されているAPIのメジャーバージョン（例: `[1]`）。

4. **後方互換性への配慮**
   - 既存のE2Eテストや結合テストで `/health` を呼び出し、`agents` 配列を参照している箇所があるため、仕様変更に伴ってそれらのテストも修正する。

## 実現方針

### 1. APIレスポンス構造の定義

`shared/libs/go/agentservice/health.go` における `HealthResponse` と `GatewayHealth` 構造体を以下のように変更します。

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

### 2. LLMGPヘルスチェックのキャッシュ化とバックグラウンドポーリング

リクエストがあるたびにLLMGPの `/health` を同期的に呼び出すと、応答時間の遅延やLLMGP側への負荷が生じます。そのため、以下の設計を採用します。

- `agentservice.Server` 構造体に `lastGatewayHealth` (GatewayHealth) とそれを保護するミューテックス `gatewayHealthMu` を追加する。
- サーバー起動時（`Launch` 時）にバックグラウンドのポーリングループを起動する（例: 30秒周期）。
- ポーリングループ内でLLMGPの `/health` を取得し、`lastGatewayHealth` をタイムスタンプとともに更新する。
- `handleHealth` では、同期的なHTTPリクエストは行わず、キャッシュされている `lastGatewayHealth` の最新値を即座に返す。
- （注意）テスト環境など、ポーリングを待ちたくない、あるいはバックグラウンドポーリングが無効な場合（インプロセスでの動作時など）は、適切な初期値（ステータス "ok"、現在時刻など）を返す。

### 3. サーバーオプション情報の受け渡し

`agentservice.ServerOption` にサーバー設定（サンドボックス無効化、サブエージェント有効化フラグ）を受け取るためのオプションを追加します。

```go
func WithSandboxDisabled(disabled bool) ServerOption {
	return func(s *Server) { s.disableSandbox = disabled }
}

func WithSubagentEnabled(enabled bool) ServerOption {
	return func(s *Server) { s.enableSubagent = enabled }
}
```

`server/server.go` の `resolveAgentService` 関数で、これらのオプションを `agentservice.New` に引き渡すように修正します。

## 検証シナリオ

### シナリオ1: ヘルスチェックAPIレスポンス of フィールド確認
1. `arctic-tern` サーバーを起動する。
2. `GET /health` エンドポイントを呼び出す。
3. レスポンスJSONに `agents` フィールドが存在しないことを確認する。
4. `gateway.last_checked_at` が正しい時間形式（RFC3339）で含まれていることを確認する。
5. `server_settings` に `disable_sandbox`, `enable_subagent`, `enabled_versions` が含まれ、設定ファイルや引数の値と一致することを確認する。

## テスト項目

- `shared/libs/go/agentservice/health_test.go`
  - `HealthResponse` の新しい構成に対応するテストケースの修正。
  - バックグラウンドポーリング、または初期ステータス反映の検証。
- `server/server_test.go`
  - ファサードサーバー起動後の `/health` 応答テストの修正。
- 既存のE2Eテスト（`tests/wayfinder_e2e_test.go`, `tests/codex_e2e_test.go` など）で `/health` から `agents` を確認しているアサーションを、`/api/v1/agents` を利用するか、またはバージョンチェックなどの別の手段に書き換える。

### 実行する検証コマンド
```bash
# 全体ビルドおよび単体テスト実行
./scripts/process/build.sh

# 統合テスト実行
xvfb-run -a ./scripts/process/integration_test.sh
```
