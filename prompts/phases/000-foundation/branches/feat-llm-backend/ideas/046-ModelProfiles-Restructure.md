# 046: model_profiles.yaml の再構成と設定ファイル整備

## 背景 (Background)

現在の `model_profiles.yaml` にはいくつかの設計上の課題がある:

1. **`keys` というフィールド名が曖昧**: 実態は「APIキーとそれに紐づくモデル群のグループ」だが、`keys` という名前では認証情報そのものを連想しにくい。`api_keys` の方が意図が明確。

2. **`value` フィールドが抽象的すぎる**: 実際に格納される値は以下の3パターンがある:
   - Vault参照URI (`vault://providers/openai/default`)
   - リアルなAPIキー文字列 (`sk-ant-xxxx...`)
   - 空文字列やダミー値 (Ollamaなど認証不要のケース)

   `value` という名称ではこれらの意味が伝わらない。より具体的な名称に変更すべき。

3. **`value` が nil 非許可**: Ollamaのようにそもそも認証キーが不要なプロバイダーでも、ダミー値の設定が必須。これは不自然な仕様。

4. **Ollamaの接続先 (BaseURL) がYAMLから設定できない**: コード上は `NetworkConfig.BaseURL` でオーバーライド可能な仕組みが既にあるが、`model_profiles.yaml` の例示や説明が不足しており、リモートのOllamaサーバーを使う場合の設定方法が不明確。

5. **スキーマ説明がない**: `model_profiles.yaml` にスキーマの説明コメントがなく、各フィールドの意味が読み取りにくい。

6. **examples/ 以下の不備**:
   - `minimal-server/` に `model_profiles.yaml` の例がない
   - `minimal-server/main.go` のコマンドライン引数説明が `-config` になっているが、実際のtern本体は `--config` (cobra)
   - `minimal-client/main.go` のデフォルト Agent/Model が `claudecode/sonnet` だが、ローカルで試しやすいOllamaベースの設定例の方が適切
   - examples全般にコメントが少ない

7. **設定ファイルの配置場所が散在**: `features/tern/` に本番用設定が直置きされており、`settings/` のような専用フォルダで整理されていない。

## 要件 (Requirements)

### 必須要件

#### R1: YAMLフィールド名の変更

- `keys` → `api_keys` に変更する
- `value` → `secret` に変更する
  - 理由: このフィールドは「秘密情報の参照先」を表す。Vault URI、リアルキー、または空(不要)のいずれか。`credential` も候補だが、`secret` の方が簡潔で、Kubernetes Secret や Vault の文脈とも自然に合致する。

  **`value` の代替候補の比較**:

  | 候補 | 長所 | 短所 |
  |------|------|------|
  | `secret` | 簡潔、Vault/K8sと親和性が高い、nil時の意味(秘密不要)が自然 | 暗号鍵と混同する可能性 |
  | `credential` | 認証情報であることが明示的 | 長い、nil時の意味が不自然 |
  | `token` | OAuth等の文脈で馴染み深い | APIキーの場合はtokenと呼ばないケースもある |
  | `source` | 値の出所を表す | APIキー自体を直貼りする場合に不自然 |
  | `ref` | Vault参照を強調 | リアルキー直貼りの場合に不自然 |

  → `secret` を採用する。

#### R2: secret フィールドの nil (省略) 許可

- `secret` フィールドを省略可能にする (Go構造体側で `omitempty` または ポインタ型に変更)
- Ollamaのように認証不要なプロバイダーでは `secret` を省略できるようにする
- バリデーション: `secret` が空/省略の場合でもエラーにしない (プロバイダーによっては不要なため)

#### R3: Ollamaの接続先設定の明示化

- 既存の `network_config.base_url` フィールドを活用して、Ollamaの接続先URLを `model_profiles.yaml` に例示する
- デフォルトは `http://localhost:11434` (既存のプロバイダーコード内ハードコーディングと一致)
- リモートOllamaサーバーへの接続例もコメントで記載

#### R4: スキーマ説明コメントの追加

- `model_profiles.yaml` の先頭に、YAMLスキーマの構造をコメントで記載する
- 各セクション・フィールドにインラインコメントを追加する

#### R5: examples/ の整備

##### R5-1: minimal-server に model_profiles.yaml を追加

- 全プロバイダーの最安モデルを1つずつ含む例を配置
  - OpenAI: `gpt-4o-mini`
  - Anthropic: `claude-haiku-4-5`
  - Google: `gemini-2.5-flash`
  - Ollama: `qwen2.5-coder:7b`

##### R5-2: minimal-server/main.go の修正

- コマンドライン引数の説明を修正 (現在は `-config` だが実装通りにする)
- Usage コメントに具体的なコマンドライン例を追記

##### R5-3: minimal-client/main.go の修正

- デフォルトの Agent/Model を `wayfinder` / `qwen2.5-coder:7b` に変更
- Usage コメントに具体的なコマンドライン例を追記
- コードにわかりやすいコメントを追加

#### R6: settings/ フォルダの新設と設定ファイル整理

- プロジェクトルートに `settings/` フォルダを作成
- `settings/example/` に examples 用の設定ファイル (`config.yaml`, `model_profiles.yaml`) を配置
- `settings/demo/` に本番寄りの設定ファイル (`config.yaml`, `model_profiles.yaml`) を配置
- `features/tern/config.yaml` と `features/tern/model_profiles.yaml` は `settings/demo/` からの参照(または移動)に切り替え

#### R7: README.md の更新

- Quick Start セクションの `model_profiles.yaml` サンプルを新フィールド名 (`api_keys`, `secret`) に更新
- `settings/` ディレクトリの説明を Project Structure に追加
- examples セクションのコード例を更新
- vault の説明で `value` を参照している箇所を `secret` に修正

### 任意要件

- テストデータ (`tests/testdata/model_profiles.yaml`) も新フィールド名に更新

## 実現方針 (Implementation Approach)

### 変更対象ファイル

#### Go構造体の変更

1. **`shared/libs/go/config/model_profiles.go`**
   - `ProviderConfig.Keys` → `ProviderConfig.ApiKeys` (yaml tag: `api_keys`)
   - `KeyConfig.Value` → `KeyConfig.Secret` (yaml tag: `secret`, `omitempty` 追加)
   - 既存の `NetworkConfig` 構造体はそのまま活用

2. **`shared/libs/go/config/model_profiles.go` (Validate)**
   - `key.Value` の空チェックがある場合は削除 (nil許可のため)

3. **参照箇所の更新** (Go コード内で `.Keys` / `.Value` を参照しているすべての箇所)
   - `shared/libs/go/llmgateway/routing.go`: `key.Value` → `key.Secret`
   - `shared/libs/go/llmgateway/bifrost_account.go`: `.Keys` → `.ApiKeys`
   - その他テストコード

#### YAML ファイルの変更

4. **`features/tern/model_profiles.yaml`**: フィールド名変更 + スキーマコメント追加 + Ollama `network_config` 追加
5. **`tests/testdata/model_profiles.yaml`**: フィールド名変更
6. **`examples/minimal-server/model_profiles.yaml`**: 新規作成
7. **`examples/minimal-server/main.go`**: コメント修正
8. **`examples/minimal-client/main.go`**: デフォルト値変更 + コメント追加

#### 設定ファイル整理

9. **`settings/example/config.yaml`**: examples 用のシンプルな設定
10. **`settings/example/model_profiles.yaml`**: examples 用のモデルプロファイル
11. **`settings/demo/config.yaml`**: `features/tern/config.yaml` をベースにした本番寄り設定
12. **`settings/demo/model_profiles.yaml`**: `features/tern/model_profiles.yaml` をベースにした設定

#### ドキュメント

13. **`README.md`**: 上記変更を反映

### 後方互換性に関する注意

- YAML フィールド名の変更は **破壊的変更** である
- 現段階ではプロダクション利用されていないため、マイグレーション機構は不要と判断
- テストデータも含めて一括更新する

## 検証シナリオ (Verification Scenarios)

1. フィールド名変更後、`features/tern/model_profiles.yaml` が正しくパースされることを確認
2. Ollamaプロバイダーの `secret` を省略した状態でバリデーションエラーが出ないことを確認
3. `network_config.base_url` を指定した状態でOllamaのルーティングが正しく動作することを確認
4. 全てのユニットテストが通過すること
5. `examples/minimal-server/` のコードがビルドできること

## テスト項目 (Testing for the Requirements)

### ビルド検証

```bash
./scripts/process/build.sh
```

### ユニットテスト

```bash
cd shared/libs/go
go test ./config/... -v
go test ./llmgateway/... -v
```

### 統合テスト

影響範囲: LLMプロバイダーの設定読み込みとモデルルーティング

```bash
./scripts/process/integration_test.sh --categories llm
```
