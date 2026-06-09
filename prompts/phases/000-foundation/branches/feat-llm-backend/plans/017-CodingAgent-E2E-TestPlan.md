# 017-CodingAgent-E2E-TestPlan

> **Source Specification**: prompts/phases/000-foundation/branches/feat-llm-backend/ideas/011-Fix-CodingAgent-Streaming.md

## Goal Description

cawa-client レベルの E2E 動作確認を自動テストとして実装する。現在のテストは全てモックエージェントを使用しており、実際の standalone + claude CLI を通した「エージェントにプロンプトを送り、ファイルが生成され、SSE イベントがストリーミングされる」という基本動作を検証していない。このテスト計画は「テストが全て PASS すれば、cawa-client が実際に動作する」と言い切れるテストを設計する。

## User Review Required

> [!IMPORTANT]
> **Claude CLI の可用性**: E2E テストは実際の `claude` CLI と LLM API キーが必要。CI 環境で常時実行は難しい可能性がある。テストは `claude` CLI が PATH に存在しない場合は `t.Fatalf` でテスト環境の不備として報告する（スキップ禁止ルールに従う）。

> [!IMPORTANT]
> **API キー**: テストは `vault-cli` に登録された API キーを使って LLM Gateway 経由でアクセスする。キーが未登録の場合はテスト失敗として報告する。

---

## 1. 要件一覧 (Extracted Requirements)

| ID | 要件 | 分類 |
| :--- | :--- | :--- |
| REQ-001 | standalone サーバを起動し、health エンドポイントが正常応答すること | 統合 |
| REQ-002 | cawa-client run 相当のフローで、セッション作成 -> メッセージ送信 -> SSE ストリーミング -> 完了 の一連が動作すること | 統合 |
| REQ-003 | SSE ストリーミングで text / tool_use イベントが実際に受信できること | 機能 |
| REQ-004 | エージェントがファイルを生成する指示を受けた場合、指定ディレクトリにファイルが実際に作成されること | 機能 |
| REQ-005 | セッション完了後、セッション状態が completed に遷移していること | 機能 |
| REQ-006 | Claude CLI がエラーで終了した場合、error イベントが SSE ストリームに含まれること | 機能 |
| REQ-007 | GatewayURL が Claude CLI に正しく伝播され、LLM Gateway 経由で API アクセスが行われること | 統合 |

---

## 2. 要件別 実現根拠と検証設計

### REQ-001: standalone サーバ起動と health レスポンス

#### 2.1 実現根拠

1. **E-001-1**: GET /health が HTTP 200 を返し、status=ok, agents に claudecode が含まれること
2. **E-001-2**: gateway ステータスが ok であること (LLM Gateway が起動していること)

#### 2.2 確認手段

| 根拠ID | 確認の視点 | 確認手段 |
| :--- | :--- | :--- |
| E-001-1 | API 応答確認 | HTTP GET /health のレスポンスボディを JSON パースし各フィールドを検証 |
| E-001-2 | API 応答確認 | gateway.status フィールドが "ok" であることを検証 |

#### 2.3 確認手順

##### E-001-1: health レスポンス検証

1. **前提条件**: standalone サーバがテスト内で起動済み (config.yaml を使用)
2. **入力**: なし
3. **操作手順**: `http.Get("http://localhost:{port}/health")`
4. **期待結果**: status=200, body 内に `"status":"ok"`, `"agents":["claudecode"]`, `"gateway":{"status":"ok"}`
5. **判定基準**: 全フィールドが期待値と一致

#### 2.4 テストシナリオ

##### TC-001: Standalone サーバ起動と health 検証

* **対応要件**: REQ-001
* **対応根拠**: E-001-1, E-001-2
* **テスト種別**: 統合テスト (E2E)
* **配置先**: `tests/agentservice_e2e_test.go`
* **テスト関数名**: `TestE2E_StandaloneHealth`
* **前提条件**: `claude` CLI が PATH に存在, vault にキー登録済み
* **テストシナリオ**:
    1. [Arrange] テスト内で `hag.New` + `hag.WithConfigPath` でサーバを構築し `Launch`
    2. [Arrange] `registerCodingAgents` 相当の処理でエージェント登録
    3. [Act] `http.Get("http://localhost:{agentServicePort}/health")` を呼び出す
    4. [Assert] status=200, body に claudecode エージェント, gateway status=ok
    5. [Cleanup] `srv.Shutdown`
* **実装メモ**: サーバ起動はエフェメラルポートを使用 (`Port: 0`)。config.yaml のテスト用コピーを使用。

---

### REQ-002: cawa-client run 相当の E2E フロー

#### 2.1 実現根拠

1. **E-002-1**: POST /api/v1/sessions でセッションが作成され、session_id が返ること
2. **E-002-2**: POST /api/v1/sessions/{id}/messages (Accept: text/event-stream) で SSE ストリームが開始されること
3. **E-002-3**: SSE ストリームに `data: ` プレフィックスのイベント行が 1 件以上含まれること
4. **E-002-4**: SSE ストリームの最後に `data: [DONE]` が送信されること

#### 2.2 確認手段

| 根拠ID | 確認の視点 | 確認手段 |
| :--- | :--- | :--- |
| E-002-1 | API 応答確認 | POST /sessions のレスポンス status=201, session_id 非空 |
| E-002-2 | 通信確認 | Content-Type: text/event-stream ヘッダの検証 |
| E-002-3 | データ確認 | SSE イベントのパースと件数カウント |
| E-002-4 | データ確認 | [DONE] センチネルの受信確認 |

#### 2.3 確認手順

##### E-002-1-4: 一連のフロー

1. **前提条件**: standalone サーバ起動済み、claude CLI 利用可能、API キー登録済み
2. **入力**: `{"agent": "claudecode", "work_dir": "{tempDir}"}` でセッション作成、`{"message": "Create a file named hello.txt with the content 'Hello World'"}` でメッセージ送信
3. **操作手順**:
   a. POST /api/v1/sessions -> session_id 取得
   b. POST /api/v1/sessions/{id}/messages (Accept: text/event-stream)
   c. SSE ストリームを行単位で読み、イベントをパース
4. **期待結果**:
   - session_id が非空の文字列
   - Content-Type が text/event-stream
   - 1 件以上の data: 行がパース可能
   - 最後に data: [DONE] を受信
5. **判定基準**: 全条件を満たすこと

#### 2.4 テストシナリオ

##### TC-002: E2E ストリーミングフロー

* **対応要件**: REQ-002, REQ-003
* **対応根拠**: E-002-1 ~ E-002-4
* **テスト種別**: E2E テスト
* **配置先**: `tests/agentservice_e2e_test.go`
* **テスト関数名**: `TestE2E_CodingAgentStreaming`
* **前提条件**: claude CLI, API キー, standalone サーバ
* **テストシナリオ**:
    1. [Arrange] standalone サーバ起動、一時ディレクトリ作成
    2. [Act] セッション作成 (agent=claudecode, work_dir=tempDir)
    3. [Act] メッセージ送信 "Create a file named hello.txt containing exactly 'Hello World'"
    4. [Assert] SSE イベントに text 型が 1 件以上
    5. [Assert] [DONE] センチネル受信
    6. [Assert] セッション状態が completed
    7. [Cleanup] 一時ディレクトリ削除、サーバ shutdown
* **実装メモ**: タイムアウト 120 秒。Claude CLI のレスポンス時間を考慮。

---

### REQ-004: ファイル生成の実動作確認

#### 2.1 実現根拠

1. **E-004-1**: 指定ディレクトリに `hello.txt` (または類似ファイル) が存在すること
2. **E-004-2**: ファイル内容が "Hello World" を含むこと

#### 2.2 確認手段

| 根拠ID | 確認の視点 | 確認手段 |
| :--- | :--- | :--- |
| E-004-1 | ファイル出力確認 | `os.Stat` でファイル存在チェック |
| E-004-2 | ファイル出力確認 | `os.ReadFile` で内容を読み、"Hello World" を含むか検証 |

#### 2.3 確認手順

##### E-004-1-2: ファイル検証

1. **前提条件**: TC-002 のストリーミング完了後
2. **入力**: tempDir のパス
3. **操作手順**: `os.ReadDir(tempDir)` でファイル一覧を取得、`hello.txt` の存在確認、内容読み取り
4. **期待結果**: hello.txt が存在し、内容に "Hello World" を含む
5. **判定基準**: ファイルが存在し内容が一致

#### 2.4 テストシナリオ

##### TC-003: ファイル生成の実証

* **対応要件**: REQ-004
* **対応根拠**: E-004-1, E-004-2
* **テスト種別**: E2E テスト (TC-002 の延長)
* **配置先**: `tests/agentservice_e2e_test.go`
* **テスト関数名**: TC-002 と統合 (`TestE2E_CodingAgentStreaming` 内でアサート)
* **テストシナリオ**:
    1. TC-002 完了後
    2. [Assert] `os.Stat(filepath.Join(tempDir, "hello.txt"))` が nil エラー
    3. [Assert] ファイル内容に "Hello World" を含む

---

### REQ-005: セッション状態遷移

#### 2.1 実現根拠

1. **E-005-1**: GET /api/v1/sessions/{id} で status=completed が返ること
2. **E-005-2**: sdk_session_id が非空であること (Claude CLI からの実際の session_id)

#### 2.4 テストシナリオ

##### TC-004: セッション状態検証

* TC-002 と統合。ストリーム完了後に GET /sessions/{id} を呼び status=completed, sdk_session_id != "" を検証。

---

### REQ-006: エラーイベント伝播

#### 2.1 実現根拠

1. **E-006-1**: 無効なモデル名を指定した場合、SSE ストリームに error イベントが含まれること

#### 2.4 テストシナリオ

##### TC-005: エラーイベントの E2E 検証

* **対応要件**: REQ-006
* **対応根拠**: E-006-1
* **テスト種別**: E2E テスト
* **配置先**: `tests/agentservice_e2e_test.go`
* **テスト関数名**: `TestE2E_CodingAgentError`
* **テストシナリオ**:
    1. [Arrange] standalone サーバ起動
    2. [Act] セッション作成 (agent=claudecode, model="nonexistent-model-xxxxx")
    3. [Act] メッセージ送信
    4. [Assert] SSE ストリームに error イベントが含まれる、または [DONE] だけで text イベントなし
    5. [Cleanup] サーバ shutdown

---

### REQ-007: GatewayURL 伝播の実証

#### 2.1 実現根拠

1. **E-007-1**: standalone サーバの health で gateway.status=ok であること
2. **E-007-2**: TC-002 のストリーミングが成功すること (Gateway 経由で LLM にアクセスできている証拠)

#### 2.4 テストシナリオ

TC-001 と TC-002 で暗黙的にカバー。Gateway が動作していなければ Claude CLI は LLM にアクセスできず、TC-002 は text イベントを返せない。

---

## 3. テスト実装サマリー

### テストケース一覧

| TC-ID | テストケース名 | 対応要件 | テスト種別 | 配置先 |
| :--- | :--- | :--- | :--- | :--- |
| TC-001 | Standalone サーバ起動と health 検証 | REQ-001, REQ-007 | E2E | tests/agentservice_e2e_test.go |
| TC-002 | E2E ストリーミングフロー + ファイル生成 | REQ-002, REQ-003, REQ-004, REQ-005 | E2E | tests/agentservice_e2e_test.go |
| TC-005 | エラーイベントの E2E 検証 | REQ-006 | E2E | tests/agentservice_e2e_test.go |

### 要件カバレッジマトリクス

| 要件 | 単体テスト | 統合テスト (既存) | E2E テスト | カバー状態 |
| :--- | :--- | :--- | :--- | :--- |
| REQ-001 | - | TestAgentServiceHealthCheck | TC-001 | 完全 |
| REQ-002 | - | TestAgentServiceSSEStreaming | TC-002 | 完全 |
| REQ-003 | - | TestAgentServiceSSEStreamingContent | TC-002 | 完全 |
| REQ-004 | - | - | TC-002 | 完全 |
| REQ-005 | - | TestAgentServiceSDKSessionID | TC-002 | 完全 |
| REQ-006 | - | TestAgentServiceSSEErrorPropagation | TC-005 | 完全 |
| REQ-007 | TestBuildEnv | - | TC-001, TC-002 | 完全 |

---

## 4. Step-by-Step Implementation Guide

### Step 1: E2E テストヘルパーの作成

* [x] Create `tests/agentservice_e2e_test.go`
* テスト内サーバ起動ヘルパー `startStandaloneServer(t) (baseURL string, cleanup func())`:
    * `hag.New` + `hag.WithConfigPath` でサーバ構築
    * `agent_service.port: 0` (エフェメラルポート) の config を一時ファイルに書き出す
    * `registerCodingAgents` 相当のエージェント登録
    * `srv.Launch(ctx)` でサーバ起動
    * `srv.AgentService().Port()` でポート取得し `http://localhost:{port}` を返す
    * cleanup で `srv.Shutdown`
* SSE パースヘルパー `parseSSEEvents(t, body io.Reader) (events []codingagent.StreamEvent, gotDone bool)`

### Step 2: TC-001 Health E2E テスト

* [x] `TestE2E_StandaloneHealth` を実装
* standalone サーバをテスト内で起動
* GET /health を呼び、status=ok, agents=["claudecode"], gateway.status=ok を検証

### Step 3: TC-002 ストリーミング + ファイル生成 E2E テスト

* [x] `TestE2E_CodingAgentStreaming` を実装
* standalone サーバ起動、一時ディレクトリ作成
* セッション作成 -> メッセージ送信 (SSE)
* SSE イベント検証: text 1 件以上, [DONE] 受信
* ファイル検証: hello.txt 存在、内容に "Hello World"
* セッション状態: completed, sdk_session_id 非空
* タイムアウト: 120 秒

### Step 4: TC-005 エラー E2E テスト

* [x] `TestE2E_CodingAgentError` を実装
* 無効モデル名でセッション作成、メッセージ送信
* error イベント受信、または text イベントなしで [DONE]
* タイムアウト: 30 秒

### Step 5: ビルドと実行

* [x] `./scripts/process/build.sh`
* [x] `./scripts/process/integration_test.sh --specify "E2E"`

---

## 5. Test Execution Plan

### 5.1 ビルドと単体テスト

```bash
./scripts/process/build.sh
```

### 5.2 E2E テスト (選択的実行)

```bash
./scripts/process/integration_test.sh --specify "E2E"
```

### 5.3 全テスト実行 (リグレッション確認)

```bash
./scripts/process/integration_test.sh --specify "AgentService"
```

---

## セルフレビュー結果

1. **要件網羅性**: REQ-001 ~ REQ-007 全てカバー。特に REQ-004 (ファイル生成) は既存テストに完全に欠如していた最重要項目。
2. **根拠の十分性**: 「テストが通ること」ではなく「ファイルが存在し内容が正しい」「SSE に text イベントが含まれる」「セッション状態が completed」という観測可能な事象で判定。
3. **確認手段の多角性**: API 応答、ファイル出力、通信、データ状態の 4 視点でカバー。
4. **手順の具体性**: HTTP メソッド、エンドポイント、リクエストボディ、期待レスポンスが全て記述済み。
5. **テスト分類**: 外部プロセス (claude CLI) + 実 API キーを使うテストは全て E2E に分類。既存の mock テストとは明確に分離。
6. **実行順序**: TC-001 (health) -> TC-002 (streaming + file) -> TC-005 (error) のボトムアップ順。
7. **互換性**: チェックボックス付き Step-by-Step Guide あり。
