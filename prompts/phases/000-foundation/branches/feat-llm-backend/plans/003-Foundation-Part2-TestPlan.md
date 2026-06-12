# 003-Foundation-Part2-TestPlan

> **Source Specification**:
> - [000-Architecture.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/ideas/000-Architecture.md)
> - [001-LLMGatewayProxy.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/ideas/001-LLMGatewayProxy.md)
> - [002-Foundation-Part2-Server-Facade-Gateway-Skeleton.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/plans/002-Foundation-Part2-Server-Facade-Gateway-Skeleton.md)

## Goal Description

Part2 (hag.Server Facade + llmgateway Skeleton) の既存テストでカバーしきれていないギャップを特定し、追加テストを通じて「最低限の実装が仕様の要件を満たしていると言い切れる」水準まで検証を強化する。

既存テスト (24件) は基本的な正常系をカバーしているが、以下のギャップがある:
1. ModelProfilesを持つProxyServerのテスト (実データでのモデル一覧・HealthのModels件数)
2. hag.Server Facade の End-to-End テスト (New -> Launch -> HTTP access -> Shutdown)
3. Config解決の優先順位 (WithConfigPath > WithConfig > default) の厳密な検証
4. Gateway Launch失敗時のエラー伝播
5. 並行HTTPリクエストの安定性
6. スタブエンドポイントのJSON error response body検証
7. ProxyServer の Shutdown 前のProxyURL動作

## User Review Required

None.

---

## 1. 要件一覧 (Extracted Requirements)

| ID | 要件 | 分類 | 既存テスト |
| :--- | :--- | :--- | :--- |
| REQ-001 | hag.Server は Option なしで New() できる (R1-4, R5-5) | 機能 | TestNew_DefaultConfig |
| REQ-002 | WithConfig で AppConfig を直接注入できる (R1-3) | 機能 | TestNew_WithConfig |
| REQ-003 | WithConfigPath で YAML ファイルからロードできる (R1-3) | 機能 | TestNew_WithConfigPath |
| REQ-004 | WithLogger でカスタム Logger を注入できる (R8-6) | 機能 | TestNew_WithLogger |
| REQ-005 | WithVaultStore でカスタム VaultStore を注入できる (R5-2) | 機能 | TestNew_WithVaultStore |
| REQ-006 | WithGateway でカスタム Gateway を注入できる (R3-6) | 機能 | TestNew_WithGateway |
| REQ-007 | Option 優先順位: Option > WithConfig > default (R1-4) | 機能 | TestNew_OptionPriority (部分的) |
| REQ-008 | Config解決優先順位: WithConfigPath > WithConfig > default (R3-3) | 機能 | **なし** |
| REQ-009 | Launch が gateway.Launch を呼ぶ (R3-4) | 機能 | TestServer_LaunchShutdown |
| REQ-010 | Shutdown が gateway.Shutdown を呼ぶ (R3-5) | 機能 | TestServer_LaunchShutdown |
| REQ-011 | Gateway() が注入されたインスタンスを返す (R1-3) | 機能 | TestServer_Gateway_ReturnsInjected |
| REQ-012 | 不正な ConfigPath で New がエラーを返す (R9-1) | 異常系 | TestNew_InvalidConfigPath |
| REQ-013 | Gateway Launch 失敗時に Launch がエラーを返す (R9-1) | 異常系 | **なし** |
| REQ-014 | LLMGatewayBackend interface に ProxyServer が準拠する | 統合 | compile-time check |
| REQ-015 | ProxyServer の Launch/Shutdown ライフサイクル (R4-1) | 機能 | TestProxyServer_Launch_Shutdown |
| REQ-016 | GET / -> 200 OK + JSON endpoints (B-3) | 機能 | TestProxyServer_Index |
| REQ-017 | GET /health -> 200 OK + HealthStatus JSON (B-3) | 機能 | TestProxyServer_Health |
| REQ-018 | GET /v1/models -> 200 OK + models JSON (B-1) | 機能 | TestProxyServer_Models (nil config のみ) |
| REQ-019 | POST /v1/messages -> 501 stub (B-1) | 機能 | TestProxyServer_AnthropicStub (status のみ) |
| REQ-020 | POST /v1/chat/completions -> 501 stub (B-1) | 機能 | TestProxyServer_OpenAIStub (status のみ) |
| REQ-021 | ProxyURL は "http://localhost:{port}" 形式 (R10) | 機能 | TestProxyServer_ProxyURL |
| REQ-022 | ListModels が profiles から正しくモデルを返す | 機能 | TestProxyServer_ListModels (nil config のみ) |
| REQ-023 | /v1/models が profiles のモデル一覧を返す (via HTTP) | 統合 | **なし** |
| REQ-024 | Health.Models が profiles のモデル件数を反映する | 機能 | **なし** |
| REQ-025 | 並行 HTTP リクエストが正しく処理される (R2-2) | 非機能 | **なし** |
| REQ-026 | スタブの 501 レスポンスが JSON error 形式を返す (R7-1) | 機能 | **なし** |
| REQ-027 | hag.Server の End-to-End ライフサイクル (ProxyServer 使用) | 統合 | **なし** |
| REQ-028 | Shutdown 前に ProxyURL が port=0 でも有効な URL を返す | 機能 | **なし** (ProxyURL テストは Launch 後のみ) |
| REQ-029 | 不存在ルートが 404 を返す | 異常系 | TestProxyServer_NotFoundRoute |
| REQ-030 | GatewayError.Error() の文字列形式 | 機能 | TestGatewayError_Error |
| REQ-031 | WriteErrorResponse の JSON 出力 | 機能 | TestWriteErrorResponse |

---

## 2. 要件別 実現根拠と検証設計

### REQ-008: Config解決の優先順位 (WithConfigPath > WithConfig > default)

#### 2.1 実現根拠

1. **E-008-1**: WithConfigPath と WithConfig を同時に指定した場合、WithConfigPath の値が使われること
2. **E-008-2**: WithConfigPath 指定時に WithConfig の値が無視されること

#### 2.2 確認手段

| 根拠ID | 確認の視点 | 確認手段 |
| :--- | :--- | :--- |
| E-008-1 | データ確認 | WithConfigPath(port=17000 の YAML) + WithConfig(port=18000) で New() し、cfg.LLMGateway.Port が 17000 であること |

#### 2.3 確認手順

##### E-008-1: WithConfigPath が WithConfig より優先

1. **前提条件**: なし
2. **入力**: port=17000 を含む一時 YAML ファイル、port=18000 の AppConfig 構造体
3. **操作手順**: `New(WithConfigPath(tmpYAML), WithConfig(&config.AppConfig{LLMGateway: {Port: 18000}}))`
4. **期待結果**: `srv.cfg.LLMGateway.Port == 17000`
5. **判定基準**: Port が YAML ファイルの値と一致すること

#### 2.4 テストシナリオ

##### TC-P2-01: WithConfigPath が WithConfig より優先される

* **対応要件**: REQ-008
* **対応根拠**: E-008-1
* **テスト種別**: 単体テスト
* **配置先**: `shared/libs/go/hag/server_test.go`
* **テスト関数名**: `TestNew_ConfigPathOverridesConfig`
* **前提条件**: なし
* **テストシナリオ**:
    1. [Arrange] port=17000 の YAML ファイルを t.TempDir() に作成。port=18000 の AppConfig 構造体を生成
    2. [Act] `New(WithConfigPath(yamlPath), WithConfig(cfg))` を呼び出す
    3. [Assert] `srv.cfg.LLMGateway.Port == 17000` を確認
* **実装メモ**: resolveConfig() の優先順位ロジックを直接検証

---

### REQ-013: Gateway Launch 失敗時のエラー伝播

#### 2.1 実現根拠

1. **E-013-1**: Gateway.Launch() がエラーを返す場合、Server.Launch() も同じエラーを返すこと
2. **E-013-2**: エラーが `hag: gateway launch:` プレフィックスでラップされること

#### 2.2 確認手段

| 根拠ID | 確認の視点 | 確認手段 |
| :--- | :--- | :--- |
| E-013-1 | エラー確認 | Launch がエラーを返す StubGateway を注入し、Server.Launch() のエラーを検証 |
| E-013-2 | エラー確認 | エラーメッセージに "hag: gateway launch:" が含まれることを確認 |

#### 2.3 確認手順

##### E-013-1: Gateway Launch 失敗時のエラー伝播

1. **前提条件**: なし
2. **入力**: Launch() で errors.New("port in use") を返す FailingGateway
3. **操作手順**: `New(WithGateway(failingStub))` -> `Launch(ctx)`
4. **期待結果**: Launch() が non-nil error を返す。エラーメッセージに "gateway launch" と "port in use" を含む
5. **判定基準**: エラーメッセージの内容一致

#### 2.4 テストシナリオ

##### TC-P2-02: Gateway Launch 失敗時のエラー伝播

* **対応要件**: REQ-013
* **対応根拠**: E-013-1, E-013-2
* **テスト種別**: 単体テスト
* **配置先**: `shared/libs/go/hag/server_test.go`
* **テスト関数名**: `TestServer_Launch_GatewayError`
* **前提条件**: なし
* **テストシナリオ**:
    1. [Arrange] Launch() で `errors.New("port in use")` を返す FailingGateway を作成
    2. [Act] `New(WithGateway(failing))` -> `srv.Launch(ctx)`
    3. [Assert] err != nil、err.Error() に "gateway launch" と "port in use" が含まれること
* **実装メモ**: テスト内にローカルな `failingGateway` 構造体を定義。`StubGateway` を埋め込んで Launch だけオーバーライド

---

### REQ-023: /v1/models が ModelProfiles の実データを返す

#### 2.1 実現根拠

1. **E-023-1**: model_profiles.yaml に定義したモデルが GET /v1/models のレスポンスに含まれること
2. **E-023-2**: レスポンスの各モデルに provider と model フィールドが存在すること

#### 2.2 確認手段

| 根拠ID | 確認の視点 | 確認手段 |
| :--- | :--- | :--- |
| E-023-1 | API 応答確認 | model_profiles.yaml を持つ ProxyServer で GET /v1/models を呼び、JSONレスポンスにモデルが含まれることを検証 |
| E-023-2 | データ確認 | JSON レスポンスの各 ModelInfo の provider/model が空でないことを確認 |

#### 2.3 確認手順

##### E-023-1: ModelProfiles 付き /v1/models

1. **前提条件**: なし
2. **入力**: 2 providers x 1 model ずつの model_profiles.yaml を t.TempDir() に生成
3. **操作手順**: NewProxyServer(cfg_with_profiles) -> Launch -> GET /v1/models
4. **期待結果**: JSON body に {"models": [...]} が含まれ、models.length >= 2
5. **判定基準**: モデル件数が YAML 定義数と一致

#### 2.4 テストシナリオ

##### TC-P2-03: /v1/models が ModelProfiles の実データを返す

* **対応要件**: REQ-023
* **対応根拠**: E-023-1, E-023-2
* **テスト種別**: 単体テスト
* **配置先**: `shared/libs/go/llmgateway/proxy_test.go`
* **テスト関数名**: `TestProxyServer_ModelsWithProfiles`
* **前提条件**: なし
* **テストシナリオ**:
    1. [Arrange] t.TempDir() に model_profiles.yaml を作成 (anthropic/claude-sonnet, openai/gpt-4o)。cfg.LLMGateway.ModelProfilesPath に設定
    2. [Act] NewProxyServer(cfg, nil, nil) -> Launch -> GET /v1/models
    3. [Assert] models 配列に 2 件以上の ModelInfo、各 provider/model が空でない
* **実装メモ**: YAML は最小限の valid 構造。Validate() を通すために必須フィールドを含める

---

### REQ-024: Health.Models が profiles のモデル件数を反映

#### 2.1 実現根拠

1. **E-024-1**: GET /health の models フィールドが model_profiles.yaml のモデル数と一致すること

#### 2.2 確認手段

| 根拠ID | 確認の視点 | 確認手段 |
| :--- | :--- | :--- |
| E-024-1 | API 応答確認 | model_profiles.yaml 付き ProxyServer で GET /health を呼び、HealthStatus.Models を検証 |

#### 2.4 テストシナリオ

##### TC-P2-04: Health.Models が profiles のモデル件数を反映

* **対応要件**: REQ-024
* **対応根拠**: E-024-1
* **テスト種別**: 単体テスト
* **配置先**: `shared/libs/go/llmgateway/proxy_test.go`
* **テスト関数名**: `TestProxyServer_HealthWithProfiles`
* **前提条件**: なし
* **テストシナリオ**:
    1. [Arrange] TC-P2-03 と同じ model_profiles.yaml を使用
    2. [Act] NewProxyServer(cfg, nil, nil) -> Launch -> GET /health
    3. [Assert] HealthStatus.Models >= 2
* **実装メモ**: TC-P2-03 のヘルパーを再利用

---

### REQ-025: 並行 HTTP リクエストの安定性

#### 2.1 実現根拠

1. **E-025-1**: 10 並行 GET /health リクエストが全て 200 OK を返すこと
2. **E-025-2**: データレースが発生しないこと

#### 2.4 テストシナリオ

##### TC-P2-05: 並行 HTTP リクエストの安定性

* **対応要件**: REQ-025
* **対応根拠**: E-025-1, E-025-2
* **テスト種別**: 単体テスト
* **配置先**: `shared/libs/go/llmgateway/proxy_test.go`
* **テスト関数名**: `TestProxyServer_ConcurrentRequests`
* **前提条件**: なし
* **テストシナリオ**:
    1. [Arrange] newTestProxy -> Launch
    2. [Act] sync.WaitGroup で 10 goroutine を起動し、各 goroutine が GET /health を呼ぶ
    3. [Assert] 全リクエストが 200 OK を返すこと
* **実装メモ**: `go test -race` でデータレースを検出。`t.Parallel()` は不要 (テスト内部で並行性を検証)

---

### REQ-026: スタブ 501 レスポンスの JSON error body 検証

#### 2.1 実現根拠

1. **E-026-1**: POST /v1/messages の 501 レスポンスが `{"error": {"type": "api_error", "code": "not_implemented"}}` 形式であること
2. **E-026-2**: POST /v1/chat/completions も同様の形式であること

#### 2.4 テストシナリオ

##### TC-P2-06: スタブ 501 レスポンスの JSON error body 検証

* **対応要件**: REQ-026
* **対応根拠**: E-026-1, E-026-2
* **テスト種別**: 単体テスト
* **配置先**: `shared/libs/go/llmgateway/proxy_test.go`
* **テスト関数名**: `TestProxyServer_StubErrorResponseBody`
* **前提条件**: なし
* **テストシナリオ**:
    1. [Arrange] newTestProxy -> Launch
    2. [Act] POST /v1/messages, POST /v1/chat/completions
    3. [Assert] 両レスポンスの JSON body が error.type="api_error", error.code="not_implemented" を含むこと。Content-Type が application/json であること
* **実装メモ**: テーブル駆動テストで両エンドポイントをまとめて検証

---

### REQ-027: hag.Server の End-to-End ライフサイクル

#### 2.1 実現根拠

1. **E-027-1**: New(WithConfig) -> Launch -> HTTP GET /health -> 200 OK -> Shutdown の全フローが動作すること
2. **E-027-2**: Shutdown 後に HTTP リクエストが失敗すること

#### 2.4 テストシナリオ

##### TC-P2-07: hag.Server End-to-End ライフサイクル

* **対応要件**: REQ-027
* **対応根拠**: E-027-1, E-027-2
* **テスト種別**: 単体テスト
* **配置先**: `shared/libs/go/hag/server_test.go`
* **テスト関数名**: `TestServer_EndToEnd_WithProxyServer`
* **前提条件**: なし
* **テストシナリオ**:
    1. [Arrange] port=0 の AppConfig を作成。StubGateway ではなく ProxyServer を自動生成させるため WithGateway は指定しない
    2. [Act] `New(WithConfig(cfg))` -> `Launch(ctx)` -> `srv.Gateway().ProxyURL()` で URL 取得 -> HTTP GET /health
    3. [Assert] HTTP GET /health が 200 OK を返す。HealthStatus.Status == "ok"。Shutdown 後にリクエストが失敗する
* **実装メモ**: このテストは StubGateway を使わず、実際の ProxyServer との統合を検証する。Part2 の最も重要な検証ポイント

---

### REQ-028: Launch 前の ProxyURL

#### 2.4 テストシナリオ

##### TC-P2-08: Launch 前の ProxyURL は port=0 で "http://localhost:0"

* **対応要件**: REQ-028
* **テスト種別**: 単体テスト
* **配置先**: `shared/libs/go/llmgateway/proxy_test.go`
* **テスト関数名**: `TestProxyServer_ProxyURL_BeforeLaunch`
* **前提条件**: なし
* **テストシナリオ**:
    1. [Arrange] port=0 (デフォルト) で NewProxyServer
    2. [Act] Launch を呼ばずに ProxyURL() を呼ぶ
    3. [Assert] "http://localhost:0" を返すこと (まだ実際のポートは割り当てられていない)
* **実装メモ**: Launch 前後の ProxyURL の挙動差異を明確化

---

## 3. テスト実装サマリー

### テストケース一覧

| TC-ID | テストケース名 | 対応要件 | テスト種別 | 配置先 |
| :--- | :--- | :--- | :--- | :--- |
| TC-P2-01 | WithConfigPath が WithConfig より優先 | REQ-008 | 単体テスト | `shared/libs/go/hag/server_test.go` |
| TC-P2-02 | Gateway Launch 失敗時のエラー伝播 | REQ-013 | 単体テスト | `shared/libs/go/hag/server_test.go` |
| TC-P2-03 | /v1/models が ModelProfiles の実データを返す | REQ-023 | 単体テスト | `shared/libs/go/llmgateway/proxy_test.go` |
| TC-P2-04 | Health.Models が profiles のモデル件数を反映 | REQ-024 | 単体テスト | `shared/libs/go/llmgateway/proxy_test.go` |
| TC-P2-05 | 並行 HTTP リクエストの安定性 | REQ-025 | 単体テスト | `shared/libs/go/llmgateway/proxy_test.go` |
| TC-P2-06 | スタブ 501 レスポンスの JSON error body | REQ-026 | 単体テスト | `shared/libs/go/llmgateway/proxy_test.go` |
| TC-P2-07 | hag.Server E2E ライフサイクル | REQ-027 | 単体テスト | `shared/libs/go/hag/server_test.go` |
| TC-P2-08 | Launch 前の ProxyURL | REQ-028 | 単体テスト | `shared/libs/go/llmgateway/proxy_test.go` |

### 要件カバレッジマトリクス (追加テストのみ)

| 要件 | 単体テスト | 統合テスト | カバー状態 |
| :--- | :--- | :--- | :--- |
| REQ-008 (Config優先順位) | TC-P2-01 | - | 完全 |
| REQ-013 (Launch失敗エラー) | TC-P2-02 | - | 完全 |
| REQ-023 (Models実データ) | TC-P2-03 | - | 完全 |
| REQ-024 (Health.Models件数) | TC-P2-04 | - | 完全 |
| REQ-025 (並行リクエスト) | TC-P2-05 | - | 完全 |
| REQ-026 (スタブ JSON body) | TC-P2-06 | - | 完全 |
| REQ-027 (E2E ライフサイクル) | TC-P2-07 | - | 完全 |
| REQ-028 (Launch前ProxyURL) | TC-P2-08 | - | 完全 |

### セルフレビュー結果

1. **網羅性**: 既存テストでカバーされている REQ-001~007, 009~012, 014~022, 029~031 は省略し、ギャップ (REQ-008, 013, 023~028) を全て追加テストでカバー
2. **証拠の十分性**: 各テストは具体的な値 (ポート番号、モデル件数、HTTP ステータスコード、JSON フィールド) を検証。「エラーが出ない」ではなく「期待する値が返る」レベル
3. **迂回排除**: TC-P2-07 は StubGateway を使わず実際の ProxyServer を使い、HTTP 通信が実際に動作していることを検証。TC-P2-03 は実際の model_profiles.yaml をロードし、正しいアダプタ経由でモデルが返ることを確認
4. **依存関係**: llmgateway テスト (TC-P2-03~06, 08) -> hag テスト (TC-P2-01~02, 07) のボトムアップ順

---

## 4. Step-by-Step Implementation Guide

1. **Step 1: llmgateway 追加テスト** [x]
    - [x] `shared/libs/go/llmgateway/proxy_test.go` に TC-P2-03, TC-P2-04 を追加 (model_profiles.yaml ヘルパー + テスト)
    - [x] TC-P2-05 (並行リクエスト) を追加
    - [x] TC-P2-06 (スタブ JSON body) を追加
    - [x] TC-P2-08 (Launch 前 ProxyURL) を追加
    - [x] テスト通過確認 (race検出含む全PASS)
    - [x] `git add && git commit -m "test: add llmgateway gap tests for Part2"` -> commit と hag テストと統合

2. **Step 2: hag 追加テスト** [x]
    - [x] `shared/libs/go/hag/server_test.go` に TC-P2-01 を追加 (Config 優先順位)
    - [x] TC-P2-02 (Gateway Launch 失敗) を追加 (failingGateway ローカル定義)
    - [x] TC-P2-07 (E2E ライフサイクル) を追加
    - [x] テスト通過確認 (race検出含む全PASS)
    - [x] `git add && git commit -m "test: add Part2 gap tests for llmgateway and hag packages"` -> d6501ce

3. **Step 3: ビルド検証** [x]
    - [x] `./scripts/process/build.sh` で全テスト通過を確認 (全テスト PASS)

---

## 5. Test Execution Plan

### 5.1 ビルドと単体テスト

```bash
./scripts/process/build.sh
```

### 5.2 個別パッケージテスト (開発中フィードバック用)

```bash
cd shared/libs/go && go test ./llmgateway/... ./hag/... -v -race -count=1
```

注: `-race` フラグで TC-P2-05 のデータレース検出を確認する。正式検証は `build.sh` を使用すること。
