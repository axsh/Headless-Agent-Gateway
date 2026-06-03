# HAG LLM Backend 検討経緯と結論

本ドキュメントは、vv4参照実装の調査で洗い出した検討事項と、レビュー議論を通じて到達した結論をまとめたものである。
各項目は「検討事項 (何を議論したか)」「議論の経緯 (どのような意見が出たか)」「結論 (最終的にどうするか)」の3段構成で記述する。

調査・議論日: 2026-06-03

参照元:
- 調査レポート: vv4参照実装のLLM Backend層を精査し、8カテゴリ・30項目以上の論点を洗い出したレポート
- コメント回答: 調査レポートへのレビューコメントに対する技術的回答と決定事項の整理

---

## A. インターフェース設計

### A-1. LLMGatewayBackend インターフェースの命名と設計

**検討事項**: vv4の `LLMGatewayBackend` と設計ドキュメントの `LLMProvider` のどちらを採用するか。また `Initialize` メソッドをインターフェースに含めるべきか。

**議論の経緯**:
- 設計ドキュメントでは `LLMProvider` を提案していたが、この名前だとOpenAI/Anthropicといったプロバイダ自体をイメージする
- 実体はBifrostBackend等のゲートウェイバックエンドなので、`LLMGatewayBackend` の方が実体に合っている
- ライフサイクルについては、`New` (メモリ上の準備完了) + `Launch` (明確な起動) + `Shutdown` (停止) の3段階が望ましい
- パラメータや状態の確認メソッド (モデル一覧取得、ヘルスチェック等) も必要

**結論**:
- **インターフェース名**: `LLMGatewayBackend` を採用。`LLMProvider` は使わない
- **ライフサイクル**: `New` + `Launch` + `Shutdown` パターン。`Initialize` は使わない
- **追加メソッド**: モデル一覧取得、ヘルスチェック等を検討して付与する

---

### A-2. 多層インターフェースの整理

**検討事項**: vv4の3層構造 (LLMClient / ProviderAdapter / LLMGatewayBackend) をそのまま踏襲するか。

**議論の経緯**:
- `LLMGatewayBackend` はサーバサイドのエンドポイント定義。`LLMClient` はそれを使うクライアントの定義。コンテキストが異なるため統合すべきではない
- 3層構造自体は明確な責務分離があり、そのまま維持してよい
- ただし、HAGではLLMを直接呼び出す必要がないため (Coding Agent CLIが担う)、`llmadapter` 層は不要

**結論**:
- **3層構造**: 維持する
- **LLMClient/ProviderAdapter**: 移植しない (HAGにはTask EngineのLLM直接呼び出し機能がないため)
- **移植対象**: `gateway/` サブパッケージ全体と `schema/` サブパッケージのみ

---

### A-3. 統一スキーマの設計

**検討事項**: `UnifiedMessage` のContent型、Reasoning制御、Structured Output対応の範囲。

**議論の経緯**:
- マルチモーダル (画像・ファイル添付) は、最初から全メディア対応は不要だが、将来のためにコンテントタイプを持つべき
- Reasoning (thinking) のリクエスト側制御は、Coding Agent経由で使う限りAgent CLIが制御するため、LLM Gateway側での制御は不要
- Structured Outputは指定すれば出力がスキーマに従うべきであり、「どこまで」という議論は不要

**結論**:
- **マルチモーダル**: Contentフィールドにコンテントタイプを持たせ、将来対応に備える
- **Reasoning制御**: LLM Gateway側では不要
- **Structured Output**: スキーマ指定があれば準拠する。範囲の議論は不要

---

### A-4. ChatRequest vs GenerationRequest の統一

**検討事項**: vv4の `ChatRequest` (gateway層) と `GenerationRequest` (llmadapter層) を統一するか。Provider/Modelの指定方法。

**議論の経緯**:
- vv4では `provider/model` を文字列パースしていたが、文字列にフォーマットの暗黙ルールを入れるべきではない
- `provider` と `model` は別々のフィールドとして定義すべき
- そもそも `llmadapter` 層は不要なので、`GenerationRequest` 自体が不要。`ChatRequest` のみ必要

**結論**:
- **リクエスト型**: `ChatRequest` のみ使用 (`GenerationRequest` は移植しない)
- **Provider/Model**: 別フィールドで定義。文字列パースは行わない

---

## B. HTTP Proxy / API互換性

### B-1. 対応するAPIフォーマットの範囲

**検討事項**: OpenAI/Anthropic各APIのどこまでをサポートするか。

**議論の経緯**:
- Anthropic Messages APIは厳密に公式準拠。SSE含む
- OpenAI Chat Completions APIのストリーミングはCodexをCoding Agentにする時で良い。TODOコメントを配置
- OpenAI Responses APIのHTTPエンドポイントはCodexが何を必須とするか次第
- ChatCompletion APIは不要。Responses APIのみ
- `/v1/models` は `model_profiles.yaml` から実際のモデル一覧を返す

**結論**:
- **Anthropic Messages API**: SSE含む厳密な公式準拠
- **OpenAI Chat Completions ストリーミング**: Codex対応時。現時点はTODO
- **OpenAI Responses API**: Codexの要件次第で判断
- **Chat Completions API**: 不要。Responses APIのみ
- **`/v1/models`**: 実際のモデル一覧を返す

---

### B-2. Anthropic Messages API互換の深さ

**検討事項**: テキスト→Tool Callフォールバック変換やSSEの独自拡張について。

**議論の経緯**:
- フォールバック変換はローカルLLMを使う上で必須。ただし、使うモデルに依存する処理なので、`model_profiles.yaml` のモデル設定で有効/無効を切り替えるべき
- SSE独自拡張のメリットは: 進捗情報追加、デバッグ情報付加、トークン使用量通知。しかしAgent CLIはAnthropic公式のSSEのみ理解するため、独自イベントは無視されるかパースエラーの原因になる

**結論**:
- **テキスト→Tool Call変換**: 必須。`model_profiles.yaml` でモデル毎に挙動設定可能にする
- **SSE独自拡張**: 不許容。厳密にAnthropic公式準拠

---

### B-3. ヘルスチェックとルートパス

**検討事項**: Agent CLI毎のヘルスチェック要件と `/health` vs `/` の役割分担。

**議論の経緯**:
- Claude Code: `ANTHROPIC_BASE_URL` に対して `GET /` で到達性確認。`200 OK` が必要
- Codex: 特別なヘルスチェックエンドポイント不要。直接APIリクエストを試行
- Gemini CLI: 現時点ではSDK無し、該当なし
- `/` はインデックス (エンドポイント一覧等) を返す。`/health` は状態JSONを返す

**結論**:
- **`GET /`**: `200 OK` + エンドポイント一覧インデックス
- **`/health`**: 状態をJSONで返す

---

### B-4. provider/model ルーティングの仕様

**検討事項**: デフォルトプロバイダの補完、`default_profile` との関係。

**議論の経緯**:
- vv4ではプロバイダ未指定時に `openai` をデフォルト補完していたが、暗黙ルールは避けるべき
- 「HTTPプロキシのデフォルトプロバイダ」とは、`splitProviderModel` 関数でモデル名に `/` が無い場合に `openai` を自動補完する動作のこと
- Claude Codeがサブセッションでmodel_profilesに存在しないモデル名を送信する問題 (プロバイダプレフィックスなし) については、セッション最初のモデルにフォールバックさせるハックが必要

**結論**:
- **プロバイダ未指定**: エラーにする。デフォルト補完しない
- **モデルフォールバック**: HTTPプロキシのリクエストハンドラ内で実装

---

## C. バックエンドエンジン

### C-1. Bifrost SDKへの依存度

**検討事項**: Bifrost SDKをそのまま使うか、抽象化するか。Backend Driverの種類。

**議論の経緯**:
- 差し替え可能にすべき。`LLMGatewayBackend` インターフェースで抽象化する
- `DirectDriver` は不要。BifrostDriverで単一プロバイダ構成にすれば同等
- `PassthroughDriver` (Agent CLIに丸投げ、L4転送) は必要
- バージョンは常に最新追随

**結論**:
- **抽象化**: `LLMGatewayBackend` インターフェースで差し替え可能にする
- **Driver**: BifrostDriverとPassthroughDriverの2種のみ
- **バージョン**: 常に最新追随

---

### C-2. Chat Completions API vs Responses API の切り替え

**検討事項**: `mode` ベースの切り替えを踏襲するか。ChatCompletion APIの長期サポート。

**議論の経緯**:
- `mode` ベースの切り替えは不要
- ChatCompletion APIは不要。Responses APIのみ
- プロバイダ間の差異 (AnthropicにはResponses APIがない等) はBifrostが担う

**結論**:
- **mode切り替え**: 不要
- **API**: Responses APIのみ
- **プロバイダ間差異**: Bifrostに委譲。LLM Gateway ProxyはOpenAIとAnthropicの2つを同時サポート

---

### C-3. プロバイダ対応範囲

**検討事項**: 追加プロバイダ、カスタムプロバイダの追加方法、プロバイダ固有設定。

**議論の経緯**:
- 現時点で追加プロバイダなし。将来対応
- コードベースで対応。プラグインシステムは不要
- `OllamaKeyConfig` はBifrost SDKがOllamaのURL設定を吸収するための特殊な設定構造体。HAG側ではプロバイダ固有の差異はBifrost SDKに委譲し、`model_profiles.yaml` の `network_config.base_url` で設定を渡す

**結論**:
- **追加プロバイダ**: 現時点なし
- **追加方法**: コードベース
- **プロバイダ固有設定**: Bifrost SDKに委譲。HAGで個別対応しない

---

## D. 設定・シークレット管理

### D-1. ModelProfilesConfig の設計

**検討事項**: 設定スキーマの採用、governance.routing_rules、ホットリロード、weight。

**議論の経緯**:
- vv4のスキーマをそのまま使いたい
- `governance.routing_rules` はCEL式によるルーティング・フォールバック制御の構想で、面白いが未実装。定義は残しTODOコメントで目的を記載。Bifrost SDKにもフォールバック機能があるため独自実装の必要性は慎重に検討すること
- ホットリロードではなく、再設定APIの呼び出しで即適用する方式
- `weight` フィールドは不要

**結論**:
- **スキーマ**: vv4のものをそのまま採用
- **routing_rules**: 定義のみ残しTODO。実装しない
- **リロード**: 再設定APIで即適用
- **weight**: 不要

---

### D-2. VaultStore の設計

**検討事項**: マルチテナント、Vault参照形式、環境変数対応、AES暗号化。

**議論の経緯**:
- マルチテナントは必須 (チーム利用想定)
- Vault参照形式は `vault://providers/{name}` (vv4形式) に統一
- 環境変数からのAPIキー読み込みは開発時の簡易セットアップとして対応したい
- AES暗号化はKeyringバックエンドでは二重暗号化で意味が薄い。ファイルバックエンドでの平文保存防止には有用なのでオプショナル

**結論**:
- **マルチテナント**: 必須
- **参照形式**: `vault://` に統一
- **環境変数**: 対応する
- **AES暗号化**: オプショナル

---

### D-3. AppConfig の構造

**検討事項**: HAGのAppConfigに必要なフィールド、設定パス、LLMGatewayConfig。

**議論の経緯**:
- vv4のAppConfigにはTask Engine、Database、Auth等の多くのフィールドがあるがHAGには不要
- 専用のコンフィグ構造体を用意し最低限のフィールドだけ採用
- 設定パスはオプション指定可能にし、デフォルトは `./config.yaml`
- LLM Gateway固有設定 (HTTPProxyPort等) はおまかせ

**結論**:
- **AppConfig**: HAG専用構造体で最小限
- **設定パス**: オプション指定。デフォルト `./config.yaml`

---

## E. Rate Limiting / Token管理

### E-1. Rate Limitingの必要性と実装方式

**検討事項**: v1で必要か。独自実装 vs Bifrost委譲。プロバイダ側429ハンドリングとの棲み分け。

**議論の経緯**:
- v1で必須。ループ等ですぐにリミットエラーになる現象が実際にある
- Bifrost SDKにはPer-Tenant/Keyベースのレート制限、自動フォールバック、加重ロードバランシングがネイティブにある
- vv4は独自の `CompositeRateLimiter` を実装していたが、Bifrostの機能で十分なら委譲する
- プロバイダ毎に考え方が異なるので、プロバイダ別に組むべき

**結論**:
- **Rate Limiting**: v1で必須
- **実装**: Bifrost SDKに委譲。独自実装しない
- **粒度**: プロバイダ毎

---

### E-2. TokenEstimator の必要性

**検討事項**: トークン推定は必要か。

**議論の経緯**:
- HAGはLLMを直接呼び出さないため不要
- レスポンスの `usage` での事後管理も不要

**結論**: **不要**

---

### E-3. RateLimiterRegistry のスコープ

**検討事項**: グローバル変数 vs DI、セッション vs プロバイダの粒度、分散Rate Limit。

**議論の経緯**:
- Rate LimitingをBifrostに委譲するため、独自のRegistryは不要
- 分散Rate Limitは現時点では不要だが、機構だけは用意しておくべき
- 共有するならトランザクション必須でDBになる可能性

**結論**:
- **独自Registry**: 不要 (Bifrost委譲)
- **分散Rate Limit**: 現時点不要。機構のみ用意

---

## F. セッション管理

### F-1. SessionModelStore の必要性

**検討事項**: GORM/SQLite依存のSessionModelStoreは必要か。モデルフォールバック機構。

**議論の経緯**:
- vv4のmain.goでTODOコメントで無効化されている。Claude Codeがシングルショットで動作する場合はファイルにセッションが書き出される。DB管理は不要
- モデルフォールバック自体は必須 (Claude Codeのサブセッション問題)
- `x-api-key` ヘッダーに `;sid=SESSION_ID` を埋め込むハック等のノウハウは必要なもの
- フォールバックはHTTPプロキシのリクエストハンドラ内で実装する

**結論**:
- **SessionModelStore**: 削除
- **モデルフォールバック**: 必須。HTTPプロキシのリクエストハンドラで実装

---

### F-2. セッション横断のモデル切り替え

**検討事項**: 環境変数でのLLM設定注入、セッション途中のモデル切り替え、並行セッション。

**議論の経緯**:
- セッション中にモデルは切り替え可能。環境変数は「Gateway接続先URL」を伝えるだけ。設計ドキュメントの「LLM設定注入」は「Gateway接続先URL注入」に修正すべき
- LLM Gateway Proxyは Go標準 `http.Server` でリクエスト毎にgoroutineが起動される完全な並行処理。セッション毎に異なるモデルの同時利用は問題なく可能
- セッション毎のモデルルーティングは当然必要

**結論**:
- **環境変数注入**: Gateway接続先URLの注入としてサポート
- **モデル切り替え**: リクエスト毎の `model` フィールドでルーティング
- **並行セッション**: 標準的に対応 (Go http.Server)

---

## G. エラーハンドリング / 可観測性

### G-1. エラーレスポンスの形式

**検討事項**: エラーレスポンスの形式とプロバイダエラーの変換方法。

**議論の経緯**:
- OpenAI/Anthropic互換のエラーレスポンスJSON形式を実装すべき
- プロバイダのエラー粒度は異なるので、LLM Gatewayが責任を持って統一エラーコードに変換
- オリジナルの原因エラーコード (HTTPステータス) はメッセージに含めて開示 (スタックトレース的な情報開示)

**結論**:
- **形式**: OpenAI/Anthropic互換JSON
- **変換**: LLM Gateway独自のエラーコードに変換。原因コードはメッセージに含める

---

### G-2. ロギング・マスキング・トレーシング

**検討事項**: ロガー選択、シークレットマスキング、トレーシング方式。

**議論の経緯**:
- vv4の `logger` パッケージはsyslog出力対応がある
- APIキーは下4桁のみ開示でマスク
- Traceログレベルでのトレーシングはデバッグに有用

**結論**:
- **ロガー**: vv4の `logger` パッケージ使用
- **マスキング**: APIキーは下4桁のみ開示
- **トレーシング**: Traceログレベルで実装

---

### G-3. メトリクス

**検討事項**: メトリクス収集の必要性。Prometheus互換エンドポイント。

**議論の経緯**:
- Bifrost SDKがPrometricsメトリクスをネイティブサポート (リクエスト数、レイテンシ、エラー率、トークン使用量の自動収集、Prometheus互換エンドポイント、Web UIダッシュボード)
- メトリクスはBifrostの仕事なので独自実装不要
- Bifrostのメトリクス機能の有効/無効はLLM Gatewayの起動設定で制御できるようにしたい
- 参照方法はコンテナでGUI/Prometheusを起動して実例を示すのが良い

**結論**:
- **メトリクス収集**: Bifrostに委譲。独自実装不要
- **設定**: メトリクス有効/無効をLLM Gatewayの起動設定で制御可能にする
- **参照方法**: 将来、コンテナでGUI/Prometheusの実例を提供

---

## H. テスト / 品質保証

### H-1. テスト戦略

**検討事項**: モック vs 統合テスト。

**議論の経緯**:
- モックは実際の挙動との乖離が生じやすく、メンテナンスコストが高い
- 実際のLLMプロバイダを使った統合テストを行うべき
- テスト用モックバックエンド、`MemoryVaultBackend` の移植は不要

**結論**:
- **テスト**: 実際のLLMプロバイダを使った統合テスト
- **モック**: 使用しない

---

## 設計ドキュメントとの差分

### 設計ドキュメントにあってvv4に無い要素

| 提案 | vv4の状況 | HAGの方針 |
|------|----------|----------|
| `DirectDriver` | 未実装 | **不要**: BifrostDriverで代替可能 |
| `PassthroughDriver` | 未実装 | **実装する**: L4転送として必要 |
| InProcess/HTTPProxy/External | InProcessのみ | **検討**: HAGのデプロイメントモデルに合わせる |
| Codex Driver向けconfig.toml生成 | 未実装 | **将来**: Codex対応時 |
| Gemini CLI向け環境変数注入 | 未実装 | **将来**: Gemini対応時 |

### vv4にあって設計ドキュメントに無い要素

| vv4の機能 | HAGの方針 |
|----------|----------|
| Anthropic Messages API互換HTTPプロキシ (SSE含む) | **実装する**: Claude Code対応に必須 |
| テキスト→Tool Callフォールバック変換 | **実装する**: model_profiles.yamlで挙動設定 |
| SessionModelStore (DB永続化) | **削除**: 不要 |
| CompositeRateLimiter + TokenEstimator | **不要**: Bifrostに委譲 |
| governance.routing_rules (CEL式) | **定義のみ残す**: TODOコメントで目的を記載 |
| Legacy AdapterConfig (旧スキーマ互換) | **不要**: 移行不要 |

### 設計ドキュメントの修正が必要な箇所

| 箇所 | 問題 | 修正内容 |
|------|------|---------|
| 「LLM設定注入」の表現 | モデル固定と誤解されやすい | 「Gateway接続先URL注入」に修正 |
| `LLMProvider` の命名 | 実体と合わない | `LLMGatewayBackend` に変更 |
| `DirectDriver` の提案 | BifrostDriverで代替可能 | 削除 |

---

## 調査で発見した注意点

### vv4の既知の問題

1. **SessionModelStoreがTODO無効化**: vv4の `main.go` で `WithSessionModelFallback` がTODOコメントで無効化されており、安定版として提供されていない

2. **OpenAI Chat Completions ストリーミング未対応**: vv4のOpenAI Chat Completions APIハンドラでは `stream: true` の分岐処理がない

3. **グローバル変数パターン**: `factory.go` の `globalGateway` はテスタビリティの観点で課題がある。HAGではDIパターンへの移行を検討すべき

### Bifrost SDKの活用可能な機能

Bifrost SDKは以下の機能をネイティブサポートしており、HAG独自実装が不要:

- **Rate Limiting**: Per-Tenant/Keyベース、自動フォールバック、加重ロードバランシング
- **Prometheus メトリクス**: リクエスト数、レイテンシ、エラー率、トークン使用量の自動収集
- **プロバイダ間差異の吸収**: Ollamaの接続URL設定等のプロバイダ固有設定を内部処理

---

## 変更履歴

| 日付 | 変更内容 |
|------|---------|
| 2026-06-03 | 初版作成。調査レポートとレビュー議論の経緯・結論を統合 |
