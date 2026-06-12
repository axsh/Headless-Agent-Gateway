# 005: Foundation フェーズ残課題仕様 (Foundation Residual Features Specification)

## 背景 (Background)

HAG (Headless-Agent-Gateway) の基本機能を実装する「000-foundation」フェーズにおいて、これまでに LLM Gateway Proxy (基本動作)、Config & Secrets (キーリング/環境変数 Vault)、階層化ログのバックエンド機能などを実装してきました。
しかし、[000-Architecture](file://prompts/phases/000-foundation/branches/feat-llm-backend/ideas/000-Architecture.md)、[001-LLMGatewayProxy](file://prompts/phases/000-foundation/branches/feat-llm-backend/ideas/001-LLMGatewayProxy.md)、[002-ConfigAndSecrets](file://prompts/phases/000-foundation/branches/feat-llm-backend/ideas/002-ConfigAndSecrets.md) には、未実装の残件（フォールバック、ファイルVault、スタンドアロン起動の構成、ロギング、一部未実装のパッケージスケルトンなど）が残っています。

本仕様は、これら未実装の残課題を一元的にまとめ、Foundationフェーズを完全にクローズするために必要な最終要件と実装設計を定義します。

---

## 要件 (Requirements)

### 必須要件

#### R1: `hag.Server` ファサードと将来パッケージの統合

- **R1-1**: `hag.Server` に `AgentService()` メソッドを追加し、`AgentService` インターフェースへの参照を返す。
- **R1-2**: `shared/libs/go/agentservice` パッケージ（スケルトン）を新規追加する。
  * `AgentService` は暫定的に空のインターフェースまたは最小限のハンドラマウントメソッドを持つスケルトンとする。
- **R1-3**: `shared/libs/go/wsserver` パッケージ（スケルトン）を新規追加する。
  * リアルタイムWebSocket中継を行うための接続管理等のための最小限のスケルトンとする。

#### R2: スタンドアロン起動（example）および Docker 環境

- **R2-1**: `examples/standalone/main.go` を作成する。
  * `hag.New` から `hag.WithConfigPath` を用いて設定を読み込み、サーバーを Launch する。
  * `SIGINT` / `SIGTERM` シグナルを正しくトラップし、`srv.Shutdown(ctx)` を呼び出して graceful shutdown を行う実装例を提供する。
- **R2-2**: `examples/standalone/Dockerfile` を作成する。
  * スタンドアロン Go サーバーをビルドし、コンテナイメージを構築する。
- **R2-3**: `examples/standalone/docker-compose.yaml` を作成する。
  * `config.yaml` / `model_profiles.yaml` のボリュームマウント設定。
  * LLM Gateway Proxy ポート (`14000`) と API/WebSocket ポート (`18080`) の公開・マッピング設定。
  * 環境変数からのAPIキー解決（`TERN_VAULT_` プレフィックスなど）への対応。

#### R3: LLM Gateway Proxy の拡張機能

- **R3-1**: **PassthroughDriverの実装 (`passthrough.go`)**
  * LLM接続を変更せずそのまま転送する `PassthroughDriver`（L4転送相当）を実装し、`LLMGatewayBackend` に統合する。
- **R3-2**: **OpenAI API `stream: true` ストリーミング対応 (`proxy_openai.go`)**
  * OpenAI Chat Completions API において、SSE (Server-Sent Events) ストリーミングレスポンスに対応する。
- **R3-3**: **サブセッションフォールバックロジック (`routing.go`)**
  * Claude Code等で `model_profiles.yaml` に定義されていないモデル名が送信されてきた場合、セッション開始時に使用した最初のモデルでフォールバックして処理を継続する。
- **R3-4**: **テキスト -> Tool Call 変換ロジック (`fallback.go`)**
  * Ollamaなど、標準の `tool_calls` 出力に対応していないローカルモデルに対し、テキストレスポンスから tool call 構文を抽出して構造化データに変換するフォールバックロジックを実装する。
  * 設定ファイルのモデルプロファイルごとに本フォールバック機能の有効・無効を設定可能とする。
- **R3-5**: **Rate Limiting (`bifrost_driver.go`)**
  * Bifrost SDKの持つ Rate Limiting 機能を有効化し、プロバイダ毎のレート制限を適用する。

#### R4: 設定・シークレットおよびロギング

- **R4-1**: **FileVaultBackendとAES暗号化 (`file_backend.go`)**
  * キーやシークレットをファイルベースで保存する `FileVaultBackend` を実装する。
  * 保存時および読み込み時に AES 暗号化・復号化を行うことで安全性を確保する。
- **R4-2**: **設定のランタイム再読み込みAPI (`loader.go` / `server.go`)**
  * ランタイムで `model_profiles.yaml` を再ロード・適用する再設定 API を提供する。
- **R4-3**: **ロガーパッケージにおける `SyslogWriter` の実装 (`logger/writer_syslog.go`)**
  * syslogサーバー（TCP/UDP/Unix）へログを送信するための `SyslogWriter` を実装・統合する。

---

## 実現方針 (Implementation Approach)

### 新規追加・修正するパッケージ

```
shared/libs/go/
    agentservice/
        service.go             -- AgentService インターフェース、スケルトン実装
    wsserver/
        server.go              -- WebSocket サーバーのスケルトン実装
    tasklog/                   -- (実装済み)
    vault/
        file_backend.go        -- FileVaultBackend (AES 暗号化)
    llmgateway/
        passthrough.go         -- PassthroughDriver (L4プロキシ相当)
        fallback.go            -- テキスト -> Tool Call 変換
    logger/
        writer_syslog.go       -- SyslogWriter の実装

examples/
    standalone/
        main.go                -- スタンドアロン起動 example (シグナル制御)
        Dockerfile             -- コンテナビルド用
        docker-compose.yaml    -- コンテナオーケストレーション
```

---

## 検証シナリオ (Verification Scenarios)

### シナリオ1: スタンドアロン起動と Graceful Shutdown

1. `examples/standalone/main.go` をビルド・起動する。
2. 起動ログに `HAG server started` が出力され、ネットワークポートをリッスンし始めることを確認する。
3. 起動プロセスに対して `SIGTERM` シグナルを送信する。
4. ログに shutdown 処理のシーケンスが出力され、プロセスが正常終了することを確認する。

### シナリオ2: OpenAI API のストリーミングリクエスト

1. `POST /v1/chat/completions` に対して `stream: true` を指定して OpenAI モデルのリクエストを送信する。
2. レスポンスが `data: ...` 形式の SSE チャンクとして逐次返却されることをアサートする。

### シナリオ3: 未定義モデルのサブセッションフォールバック

1. 最初のセッション開始リクエストで定義モデル A を指定する。
2. 以降、同一セッションで `model_profiles.yaml` に定義されていないモデル名が送信されてきた際、自動的にモデル A へフォールバックされて API リクエストが成功することを確認する。

### シナリオ4: FileVaultBackend の AES 暗号化保存

1. `FileVaultBackend` を初期化し、シークレット（APIキーなど）を設定する。
2. 指定した保存ファイルが暗号化されており、直接テキストエディタでシークレットを読み取れないことを確認する。
3. `FileVaultBackend` を経由してロードした際に、正しい生の値が復号されて取得できることを確認する。

### シナリオ5: Docker環境での立ち上げ

1. `docker-compose up` コマンドで `examples/standalone` にある環境をビルド・起動する。
2. ホスト側から `GET http://localhost:14000/` で接続可能であることを確認する。

---

## テスト項目 (Testing for the Requirements)

### ビルド・全体検証

1. ビルド＋単体テスト:
   ```bash
   ./scripts/process/build.sh
   ```

2. 統合テスト (個別機能の動作確認):
   ```bash
   ./scripts/process/integration_test.sh --categories "common,llm"
   ```

### 単体テスト計画

| テスト対象 | テストファイル | 確認内容 |
|---|---|---|
| FileVaultBackend | `file_backend_test.go` | 暗号化保存、ロード時の復号、異常系 (破損ファイルなど) |
| OpenAI Stream | `proxy_openai_test.go` | `stream: true` 時の SSE 形式レスポンスパース |
| Subsession Fallback | `routing_test.go` | 既存セッションとの突き合わせ、モデル解決フォールバック |
| ToolCall Fallback | `fallback_test.go` | テキストからの JSON 抽出、Tool Call 構造体への変換 |
| SyslogWriter | `writer_syslog_test.go` | syslog サーバーへのログ送信確認 |
