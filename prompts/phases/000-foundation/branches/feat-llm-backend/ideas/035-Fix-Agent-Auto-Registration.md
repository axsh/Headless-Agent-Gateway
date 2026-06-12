# 035: エージェント自動登録バグの修正

## 背景 (Background)

仕様 [030: Factory/Registry パターン導入と Bifrost 一本化](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/prompts/phases/000-foundation/branches/feat-llm-backend/ideas/030-Factory-Registry-And-Bifrost-Unification.md) の実装により、各エージェントアダプター（`claudecode`, `codex`）は `init()` 関数を用いてグローバルレジストリ（`codingagent.registry`）にファクトリ関数を自己登録（自己申告）する設計に移行しました。

しかし、サーバーファサードである [tern/server.go](file:///c:/Users/yamya/myprog/Headless-Agent-Gateway/work/feat-llm-backend/shared/libs/go/tern/server.go) において、自己申告されたエージェントを `codingagent.CreateAll()` によってインスタンス化し、エージェントサービス（`agentservice.Server`）に登録する処理の実装が漏れていました。

これにより、以下の問題が発生しています：
1. `cawa-server` 起動時にエージェントが一切登録されないため、クライアントから `agents` の取得やセッションの作成を行うと `unknown agent` エラーになる。
2. 既存の E2E テスト（`agentservice_e2e_test.go`）では、テスト側で手動で `RegisterAgent()` を呼び出していたため、自動登録が機能していないというバグがテストで検出できていなかった。

本仕様では、サーバー起動時にレジストリからエージェントを自動登録するよう修正し、E2Eテストにおいても自動登録された状態を前提とした検証を行うようにします。

## 要件 (Requirements)

### R1: サーバー起動時のエージェント自動登録
- サーバーの初期化関数 `tern.New(...)` において、グローバルレジストリに自己登録されているすべてのエージェントを自動的にインスタンス化し、エージェントサービス（`agentservice.Server`）に登録する。
- インスタンス化に必要なパラメータを保持する `codingagent.AdapterConfig` に対して、以下のパラメータを正しく設定する：
  - `GatewayURL`: LLM Gateway Proxy の URL（ローカルホスト＋ポート）
  - `GatewayToken`: 自動生成または設定された認証トークン (`gatewayToken`)
  - `Logger`: サーバーの Logger インスタンス
  - `DefaultModel`: ゲートウェイから取得したデフォルトモデルの名称（`gw.DefaultModel()` より取得）
  - `ToolCallFallback`: ゲートウェイから取得したツールコールフォールバックの設定値（`gw.DefaultModel()` より取得）

### R2: E2Eテストでの自動登録検証と手動登録の廃止
- `tests/agentservice_e2e_test.go` のテスト用サーバー起動処理（`startE2EServer`）において、テストコード側で手動で `RegisterAgent()` を呼び出して登録している処理を削除する。
- これにより、サーバー単体で起動した際にエージェントが自動的に認識・登録されていることをテストで保証する。

## 実現方針 (Implementation Approach)

### 1. `tern.New` フローでの自動登録の組み込み
`shared/libs/go/tern/server.go` の `resolveAgentService` 関数の引数に `gw llmgateway.LLMGatewayBackend` を追加し、以下のように `codingagent.CreateAll` と `RegisterAgent` を呼び出して自動登録を処理します。

```go
func resolveAgentService(o *options, log logger.Logger, tl *tasklog.TaskLog, gatewayURL string, gatewayToken string, caCertPath string, gw llmgateway.LLMGatewayBackend) *agentservice.Server {
	if o.agentService != nil {
		return o.agentService
	}

	if strings.HasPrefix(gatewayURL, "https://") && caCertPath != "" {
		os.Setenv("NODE_EXTRA_CA_CERTS", caCertPath)
		if log != nil {
			log.Debug("set NODE_EXTRA_CA_CERTS env var", "path", caCertPath)
		}
	}

	as := agentservice.New(
		agentservice.WithLogger(log),
		agentservice.WithTaskLog(tl),
		agentservice.WithGatewayURL(gatewayURL),
		agentservice.WithGatewayToken(gatewayToken),
	)

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

	return as
}
```

この変更に伴い、`tern.New` 内での `resolveAgentService` の呼び出し箇所に `gw` 引数を追加します。

### 2. E2Eテストの修正
- `tests/agentservice_e2e_test.go` の `startE2EServer()` 内で手動で実行しているエージェントの登録コードを削除します。
- これにより、E2Eテスト実行時にサーバーが自動登録だけで正常に稼働することを検証します。

## 検証シナリオ (Verification Scenarios)

### シナリオ1: 自動登録の動作確認（E2Eテスト）
1. `go test -v ./tests -run TestE2E_StandaloneHealth` を実行。
2. ヘルスチェックのレスポンスの `agents` に `"claudecode"` が正しく含まれていること、およびテストが正常に成功することを確認。

### シナリオ2: 実際のビルドと手動検証
1. `cawa-server` をビルドし、起動する：
   ```bash
   ./bin/cawa-server --config ./examples/cawa-server/config.yaml
   ```
2. `cawa-client` でエージェント一覧を要求する：
   ```bash
   ./bin/cawa-client agents
   ```
   出力に `claudecode`（および利用可能な `codex`）が表示されることを確認。
3. エージェントの実行が成功することを確認する：
   ```bash
   ./bin/cawa-client run --agent claudecode --prompt "say hello"
   ```

## テスト項目 (Testing for the Requirements)

- **自動検証の実行**:
  以下のコマンドですべてのビルドおよびテストが成功することを確認します。
  ```bash
  ./scripts/process/build.sh && ./scripts/process/integration_test.sh
  ```
  特に `agentservice_e2e_test.go` の E2E テストが手動登録なしでパスすることを確認します。
