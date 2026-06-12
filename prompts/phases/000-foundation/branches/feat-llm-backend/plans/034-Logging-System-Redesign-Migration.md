# 034-Logging-System-Redesign-Migration

> **Source Specification**: [024-Logging-System-Redesign.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/ideas/024-Logging-System-Redesign.md)

## Goal Description

033 (Part 1) で拡張された logger パッケージを使い、全モジュール (llmgateway, agentservice, codingagent, wsserver, hag) に DEBUG/TRACE ログを挿入するマイグレーションを行う。既存のログ呼び出しをレベル基準に照らして見直し、処理フローの追跡と障害時のデータダンプを可能にする。

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R5: 既存コードへのログ挿入 | 本計画の全 Proposed Changes |
| R1: ログレベル基準の適用 | 既存 Info/Warn/Error の見直し + 新規 Debug/Trace 追加 |

## Proposed Changes

### llmgateway パッケージ

#### [MODIFY] [proxy.go](file://shared/libs/go/llmgateway/proxy.go)
*   **Description**: ProxyServer の初期化と起動にログを追加
*   **追加ログ一覧**:
    | レベル | 場所 | メッセージ | フィールド |
    |--------|------|-----------|-----------|
    | DEBUG | `NewProxyServer()` | "creating proxy server" | `port` |
    | DEBUG | `Launch()` | "proxy server listening" | `addr` |
    | DEBUG | `Shutdown()` | "proxy server shutting down" | - |
    | INFO | `ReloadProfiles()` | "model profiles reloaded" | `count` |

#### [MODIFY] [proxy_anthropic.go](file://shared/libs/go/llmgateway/proxy_anthropic.go)
*   **Description**: Anthropic Messages API ハンドラにリクエスト/レスポンスのログを追加
*   **追加ログ一覧**:
    | レベル | 場所 | メッセージ | フィールド |
    |--------|------|-----------|-----------|
    | DEBUG | ハンドラ冒頭 | "anthropic messages request received" | `method`, `path`, `model` |
    | DEBUG | ルーティング後 | "request routed" | `model`, `provider`, `mode`, `fallback` |
    | DEBUG | cross-provider 変換 | "converting anthropic request" | `direction`, `target_path` |
    | TRACE | 変換前 | "anthropic request body" | `body` (JSON全文、最大10KB) |
    | TRACE | 変換後 | "converted request body" | `body` (JSON全文、最大10KB) |
    | DEBUG | upstream送信後 | "upstream response received" | `status`, `content_type` |
    | TRACE | レスポンスヘッダー | "upstream response headers" | `headers` |
    | WARN | ToolCallFallback発動 | "tool call fallback applied" | `model` |
    | ERROR | 変換エラー | 既存の Error ログを拡充 | `error`, `body_size`, `model`, `provider` |

#### [MODIFY] [proxy_openai.go](file://shared/libs/go/llmgateway/proxy_openai.go)
*   **Description**: OpenAI Chat Completions / Responses API ハンドラにログを追加
*   **追加ログ一覧**:
    | レベル | 場所 | メッセージ | フィールド |
    |--------|------|-----------|-----------|
    | DEBUG | ハンドラ冒頭 | "openai request received" | `method`, `path`, `model` |
    | DEBUG | ルーティング後 | "request routed" | `model`, `provider`, `mode`, `fallback` |
    | TRACE | リクエストボディ | "openai request body" | `body` |
    | DEBUG | upstream送信後 | "upstream response received" | `status`, `content_type` |
    | TRACE | レスポンスヘッダー | "upstream response headers" | `headers` |

#### [MODIFY] [routing.go](file://shared/libs/go/llmgateway/routing.go)
*   **Description**: モデルルーティング決定にログを追加
*   **追加ログ一覧**:
    | レベル | 場所 | メッセージ | フィールド |
    |--------|------|-----------|-----------|
    | DEBUG | `RouteModel()` | "model routing decision" | `requested_model`, `resolved_model`, `provider`, `mode` |
    | TRACE | `RouteModel()` | "routing config details" | `key_prefix`, `base_url`, `fallback` |

#### [MODIFY] [provider_forwarder.go](file://shared/libs/go/llmgateway/provider_forwarder.go)
*   **Description**: リトライ動作のログを追加
*   **追加ログ一覧**:
    | レベル | 場所 | メッセージ | フィールド |
    |--------|------|-----------|-----------|
    | DEBUG | `forwardWithRetry()` 開始 | "forwarding request to upstream" | `provider`, `path` |
    | WARN | リトライ発生 | "retrying upstream request" | `attempt`, `max_retries`, `delay_ms`, `status`, `error` |
    | TRACE | リクエスト送信時 | "upstream request" | `url`, `headers` (Authorization マスク済み) |
    | TRACE | レスポンス受信時 | "upstream response body preview" | `body_preview` (先頭1KB) |
    | ERROR | 全リトライ失敗 | "all retries exhausted" | `attempts`, `last_error`, `provider`, `path` |

#### [MODIFY] [convert_a2r.go](file://shared/libs/go/llmgateway/convert_a2r.go)
*   **Description**: Anthropic <-> Responses API 変換にログを追加
*   **追加ログ一覧**:
    | レベル | 場所 | メッセージ | フィールド |
    |--------|------|-----------|-----------|
    | DEBUG | 変換開始 | "converting anthropic to responses" | `model`, `msg_count`, `tool_count` |
    | TRACE | 変換結果 | "converted responses request" | `body` |
    | DEBUG | レスポンス逆変換 | "converting responses to anthropic" | `model` |
    | TRACE | 逆変換結果 | "converted anthropic response" | `body` |

#### [MODIFY] [stream_converter.go](file://shared/libs/go/llmgateway/stream_converter.go)
*   **Description**: SSE ストリーム変換にイベントレベルのログを追加
*   **追加ログ一覧**:
    | レベル | 場所 | メッセージ | フィールド |
    |--------|------|-----------|-----------|
    | DEBUG | 変換開始 | "starting SSE stream conversion" | `direction`, `model` |
    | TRACE | 各SSEイベント | "SSE event" | `event_data` (1行ずつ) |
    | DEBUG | 変換完了 | "SSE stream conversion completed" | `events_count` |
    | WARN | 変換エラー（継続可能） | "SSE event parse warning" | `line`, `error` |

---

### agentservice パッケージ

#### [MODIFY] [service.go](file://shared/libs/go/agentservice/service.go)
*   **Description**: Server の初期化と起動にログを追加、コンポーネントタグ付け
*   **追加ログ一覧**:
    | レベル | 場所 | メッセージ | フィールド |
    |--------|------|-----------|-----------|
    | DEBUG | `New()` | "creating agent service" | `agent_count` |
    | INFO | `Launch()` | "agent service listening" | `port`, `addr` |
    | INFO | `Shutdown()` | "agent service shutting down" | - |
    | DEBUG | エージェント登録 | "agent registered" | `agent_name` |

#### [MODIFY] [handler.go](file://shared/libs/go/agentservice/handler.go)
*   **Description**: 全ハンドラにリクエスト追跡ログを追加
*   **追加ログ一覧**:
    | レベル | 場所 | メッセージ | フィールド |
    |--------|------|-----------|-----------|
    | DEBUG | `handleCreateSession()` | "creating session" | `agent`, `model`, `work_dir` |
    | DEBUG | `handleGetSession()` | "getting session" | `session_id` |
    | DEBUG | `handleDeleteSession()` | "deleting session" | `session_id` |
    | DEBUG | `handleSendMessage()` | "sending message to agent" | `session_id`, `agent`, `model` |
    | TRACE | `handleSendMessage()` | "message content" | `message` |
    | DEBUG | `streamSSE()` 開始 | "starting SSE stream" | `session_id` |
    | TRACE | `streamSSE()` 各イベント | "SSE stream event" | `type`, `content_preview` |
    | DEBUG | `streamSSE()` 完了 | "SSE stream completed" | `session_id`, `event_count` |
    | DEBUG | AgentSessionID 抽出 | "agent session ID extracted" | `session_id`, `agent_session_id` |
    | DEBUG | `handleTerminate()` | "terminating session" | `session_id` |

---

### codingagent パッケージ

#### [MODIFY] [adapter.go](file://shared/libs/go/codingagent/claudecode/adapter.go)
*   **Description**: ClaudeCodeAdapter にコンポーネントタグ付きロガーを追加
*   **Technical Design**:
    ```go
    type ClaudeCodeAdapter struct {
        config *codingagent.AdapterConfig
        logger logger.Logger
    }
    ```
*   **追加ログ一覧**:
    | レベル | 場所 | メッセージ | フィールド |
    |--------|------|-----------|-----------|
    | DEBUG | `CreateSession()` | "creating claude code session" | `model`, `work_dir`, `session_dir` |
    | DEBUG | セッション作成完了 | "claude code session created" | `session_id` |
    | DEBUG | `Close()` | "closing claude code adapter" | - |

#### [MODIFY] [process.go](file://shared/libs/go/codingagent/claudecode/process.go)
*   **Description**: CLI プロセス管理にログを追加
*   **追加ログ一覧**:
    | レベル | 場所 | メッセージ | フィールド |
    |--------|------|-----------|-----------|
    | DEBUG | `BuildArgs()` | "building CLI arguments" | `args` |
    | TRACE | `BuildEnv()` | "CLI environment variables" | `env` (API キーはマスク) |
    | INFO | `StartProcess()` | "starting claude CLI process" | `work_dir`, `model` |
    | TRACE | stdout 各行読み取り | "CLI stdout line" | `line` |
    | DEBUG | プロセス完了 | "claude CLI process exited" | `exit_code` |
    | WARN | 非正常終了 | "claude CLI process exited with error" | `error`, `stderr` |
    | DEBUG | `Stop()` | "stopping claude CLI process" | `signal` |

#### [MODIFY] [protocol.go](file://shared/libs/go/codingagent/claudecode/protocol.go)
*   **Description**: JSON Lines パースにログを追加
*   **追加ログ一覧**:
    | レベル | 場所 | メッセージ | フィールド |
    |--------|------|-----------|-----------|
    | TRACE | パース前 | "parsing JSON Lines event" | `line` |
    | DEBUG | パース成功 | "parsed event" | `type`, `subtype` |
    | WARN | パース失敗 | "failed to parse JSON Lines event" | `error`, `line_preview` |

---

### wsserver パッケージ

#### [MODIFY] [server.go](file://shared/libs/go/wsserver/server.go)
*   **Description**: WebSocket サーバーにログを追加
*   **追加ログ一覧**:
    | レベル | 場所 | メッセージ | フィールド |
    |--------|------|-----------|-----------|
    | DEBUG | `New()` | "creating websocket server" | `port` |
    | INFO | `Launch()` | "websocket server listening" | `addr` |
    | DEBUG | クライアント接続 | "websocket client connected" | `remote_addr` |
    | DEBUG | クライアント切断 | "websocket client disconnected" | `remote_addr` |

#### [MODIFY] [hub.go](file://shared/libs/go/wsserver/hub.go)
*   **Description**: Hub のブロードキャストにログを追加
*   **追加ログ一覧**:
    | レベル | 場所 | メッセージ | フィールド |
    |--------|------|-----------|-----------|
    | DEBUG | ブロードキャスト | "broadcasting to clients" | `client_count`, `entry_type` |
    | TRACE | ブロードキャスト内容 | "broadcast payload" | `payload` |

---

### hag パッケージ

#### [MODIFY] [server.go](file://shared/libs/go/hag/server.go)
*   **Description**: サーバー初期化フローにログを追加
*   **追加ログ一覧**:
    | レベル | 場所 | メッセージ | フィールド |
    |--------|------|-----------|-----------|
    | DEBUG | `New()` Step 2 | "resolving config" | `config_path` |
    | TRACE | `New()` Step 2 完了 | "config resolved" | 設定の主要値ダンプ |
    | DEBUG | `New()` Step 3 | "resolving logger" | `level` |
    | DEBUG | `New()` Step 4 | "resolving vault" | `backend` |
    | DEBUG | `New()` Step 5 | "resolving gateway" | `type`, `port` |
    | INFO | `Launch()` | 既存の "starting HAG server" を維持 | - |
    | DEBUG | `Launch()` Gateway起動後 | "gateway launched" | `port` |
    | DEBUG | `Launch()` WS起動後 | "websocket server launched" | `port` |
    | DEBUG | `Launch()` AS起動後 | "agent service launched" | `port` |
    | INFO | `Shutdown()` | 既存の "shutting down HAG server" を維持 | - |

## Step-by-Step Implementation Guide

### Step 1: codingagent パッケージへの logger 注入
1. Edit `shared/libs/go/codingagent/claudecode/adapter.go`: Logger フィールドを追加し、`WithComponent("claudecode")` でタグ付け
2. Edit `shared/libs/go/codingagent/claudecode/process.go`: StartProcess にログ追加。Trace で CLI stdout の各行をログ出力
3. Edit `shared/libs/go/codingagent/claudecode/protocol.go`: パース前後にログ追加
4. `git add && git commit -m "feat: add debug/trace logging to codingagent claudecode"`

### Step 2: llmgateway パッケージへのログ追加
1. Edit `shared/libs/go/llmgateway/proxy.go`: 初期化/起動ログ追加
2. Edit `shared/libs/go/llmgateway/proxy_anthropic.go`: リクエスト/レスポンスの全フローにログ追加
3. Edit `shared/libs/go/llmgateway/proxy_openai.go`: 同上
4. Edit `shared/libs/go/llmgateway/routing.go`: ルーティング決定ログ追加
5. Edit `shared/libs/go/llmgateway/provider_forwarder.go`: リトライログ追加
6. Edit `shared/libs/go/llmgateway/convert_a2r.go`: 変換ログ追加
7. Edit `shared/libs/go/llmgateway/stream_converter.go`: SSE イベントログ追加
8. `git add && git commit -m "feat: add debug/trace logging to llmgateway"`

### Step 3: agentservice パッケージへのログ追加
1. Edit `shared/libs/go/agentservice/service.go`: コンポーネントタグ付け、初期化ログ
2. Edit `shared/libs/go/agentservice/handler.go`: 全ハンドラにリクエスト追跡ログ
3. `git add && git commit -m "feat: add debug/trace logging to agentservice"`

### Step 4: wsserver パッケージへのログ追加
1. Edit `shared/libs/go/wsserver/server.go`: 接続/切断ログ
2. Edit `shared/libs/go/wsserver/hub.go`: ブロードキャストログ
3. `git add && git commit -m "feat: add debug/trace logging to wsserver"`

### Step 5: hag パッケージへのログ追加
1. Edit `shared/libs/go/hag/server.go`: 初期化フローの各ステップにログ追加
2. `git add && git commit -m "feat: add debug/trace logging to hag core server"`

### Step 6: 既存ログの見直し
1. 全モジュールの既存 `Info`/`Warn`/`Error` 呼び出しを `prompts/rules/logging-rules.md` に照らして見直し
2. レベルが不適切なログ（例: DEBUG 相当の内容が INFO になっている）を修正
3. ERROR ログにコンテキスト情報が不足している箇所を拡充
4. `git add && git commit -m "refactor: align existing log levels with logging rules"`

### Step 7: Verification Plan の実行
1. `./scripts/process/build.sh` を実行
2. `./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories llm --specify "TestE2E"` を実行

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```
    *   **Log Verification**: 全パッケージのビルドが成功し、既存テストがPASSすること

2.  **Integration Tests**:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories llm --specify "TestE2E"
    ```
    *   **Log Verification**: 既存の E2E テスト (`TestE2E_CodingAgent`, `TestCrossProvider`, `TestResponsesAPI`) が PASS すること

3.  **E2E Tests**:
    ログ挿入はアプリケーションの動作を変更しない（出力追加のみ）。既存の E2E テストでリグレッションを確認する。新規 E2E テストの追加は不要。理由: ログ出力の内容検証は単体テスト（mockWriter でのキャプチャ）で十分カバーされるため。

### セルフレビュー結果

1. **要件対比**: R5 を本 Part 2 で全モジュールにわたってカバー。
2. **再現性**: 各モジュールのログ追加箇所をテーブル形式で明示。
3. **テスト網羅性**: 既存テストでリグレッション確認。ログ追加は動作に影響しない。
4. **統合テストの実行プラン**: `--categories llm --specify "TestE2E"` で選択実行。
5. **E2E テスト**: ログ挿入は出力追加のみのため不要。理由を明記済み。

## Documentation

#### [MODIFY] [024-Logging-System-Redesign.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/ideas/024-Logging-System-Redesign.md)
*   **更新内容**: 実装完了後、R5 の実施状況をステータス更新
