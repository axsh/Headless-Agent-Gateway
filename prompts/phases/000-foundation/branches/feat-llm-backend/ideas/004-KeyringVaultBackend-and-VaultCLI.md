# 004: KeyringVaultBackend + vault-cli (Part 3.5)

## 背景 (Background)

### 課題

Part 3 で BifrostDriver, ModelRouter, ハンドラの実装が完了したが、統合テスト (Step 10) を実行するには実際の LLM プロバイダ API キーが必要である。

現在の API キー管理の問題点:

1. **環境変数ベースの `EnvVaultBackend` のみ** -- 現在の HAG には環境変数を使った単純な VaultStore 実装しかない。テスト実行のたびに環境変数をセットする必要があり、CI/CD やスクリプト化が煩雑
2. **credentials.yaml 等のファイルベース管理はリスク** -- 過去に `.gitignore` に含めていても誤コミットが発生した実績がある
3. **OS Keyring が最適解** -- Windows Credential Manager, macOS Keychain, Linux Secret Service を使えば、リポジトリ外に安全にキーを保持でき、誤コミットのリスクがゼロになる

### vv4 からの移植

参照リポジトリ `reference_repo/vv4` には以下の実績ある実装がある:

- `shared/libs/go/credential/keyring_vault_backend.go` -- OS Keyring を利用した VaultBackend 実装
- `features/backend/cmd/vault-cli/main.go` -- キーの set/get/delete/list/status を行う CLI ツール

これらを HAG のインターフェース (`vault.VaultStore`) に適合させて移植する。

## 要件 (Requirements)

### 必須要件 (Must)

1. **KeyringVaultBackend の実装**
   - `vault.VaultStore` インターフェース (`Resolve`, `Set`, `Delete`, `List`) を実装すること
   - OS Keyring (Windows Credential Manager / macOS Keychain / Linux Secret Service) を利用すること
   - `github.com/zalando/go-keyring` ライブラリを使用すること
   - `vault://` プロトコルの解決をサポートすること
   - サービス名は `hag-vault-{tenantHash}` 形式でテナント分離すること (vv4 の設計を踏襲)
   - スレッドセーフであること (mutex による書き込みの排他制御)

2. **vault-cli ツールの実装**
   - `examples/vault-cli/main.go` に配置 (HAG ユーザが参照する example として)
   - 以下のサブコマンドを提供:
     - `set --provider <name> --stdin` -- API キーを OS Keyring に保存 (stdin から読み取り)
     - `set --provider <name>` -- 対話的に API キーを入力して保存
     - `get --provider <name>` -- キーの登録有無を確認
     - `get --provider <name> --reveal` -- キーの値を表示
     - `delete --provider <name>` -- キーを削除
     - `list` -- 登録済みキー一覧を表示
     - `status` -- 既知の LLM プロバイダ (openai, anthropic) の登録状態を表示
   - `--provider <name>` は `providers/<name>/default` のキーパスに変換すること (HAG の model_profiles.yaml の構造と整合)
   - `--key <path>` オプションで任意のキーパスも指定可能とすること

3. **BifrostAccount の vault:// 連携**
   - model_profiles.yaml で `value: "vault://providers/anthropic/default"` と記述した場合、KeyringVaultBackend を通じて OS Keyring からキーを取得できること
   - 既存の `EnvVaultBackend` と `KeyringVaultBackend` は `hag.Server` のオプションで切り替え可能であること

4. **hag.Server への統合**
   - `WithKeyringVault()` オプションを追加し、OS Keyring ベースの VaultStore を使用可能にすること
   - デフォルトは引き続き `EnvVaultBackend` (後方互換性)

### 任意要件 (Should)

5. **キーインデックスの管理**
   - 登録済みキー一覧をメタキー `_vault_keys` で管理すること (vv4 設計の踏襲)
   - `List()` でインデックスから一覧を返すこと

## 実現方針 (Implementation Approach)

### アーキテクチャ

```
vault-cli (examples/)
    |
    v
vault.VaultStore インターフェース
    |
    +-- EnvVaultBackend (既存, 環境変数)
    +-- KeyringVaultBackend (新規, OS Keyring)
            |
            v
        github.com/zalando/go-keyring
            |
            v
        OS Keyring (Win Credential Manager / macOS Keychain / Linux Secret Service)
```

### コンポーネント

#### 1. `shared/libs/go/vault/keyring_backend.go`

vv4 の `keyring_vault_backend.go` を移植し、HAG の `VaultStore` インターフェースに適合させる。

主要メソッドのマッピング:

| HAG VaultStore | KeyringVaultBackend の内部実装 |
|---|---|
| `Resolve(ref)` | `vault://` パスをパースし、keyring から取得 |
| `Set(path, value)` | keyring に JSON ノードとして保存 + インデックス更新 |
| `Delete(path)` | keyring からノード削除 + インデックス更新 |
| `List()` | メタキーからインデックスを読み込み、パス一覧を返す |

#### 2. `examples/vault-cli/main.go`

vv4 の `cmd/vault-cli/main.go` を移植。vv4 固有のテナント検出ロジック (`detectTenantID`) は HAG 向けに簡素化する (固定デフォルトテナント)。

#### 3. `shared/libs/go/hag/options.go`

`WithKeyringVault()` オプションを追加。

### 依存ライブラリ

- `github.com/zalando/go-keyring` -- 純 Go の OS Keyring ライブラリ (CGO 不要)

### キーパス設計

model_profiles.yaml のプロバイダ構造に合わせ、以下のパス規約を使用:

```
providers/<provider_name>/<key_name>
```

例:
- `providers/anthropic/primary` -- Anthropic の primary キー
- `providers/openai/default` -- OpenAI のデフォルトキー

`--provider <name>` は `providers/<name>/default` に展開される。

## 検証シナリオ (Verification Scenarios)

### シナリオ 1: vault-cli でキーを登録し、統合テストで使用する

1. `go run examples/vault-cli/main.go set --provider anthropic --stdin` を実行
2. stdin から API キーを入力
3. `go run examples/vault-cli/main.go status` で "anthropic: registered" を確認
4. `model_profiles.yaml` に `value: "vault://providers/anthropic/default"` を設定
5. `hag.Server` を `WithKeyringVault()` で起動
6. BifrostDriver の Anthropic ハンドラに POST /v1/messages を送信
7. 実際の Anthropic API からレスポンスが返ることを確認

### シナリオ 2: vault-cli のライフサイクル (set -> get -> delete)

1. `vault-cli set --provider openai --stdin` で API キーを登録
2. `vault-cli get --provider openai` で "registered" を確認
3. `vault-cli get --provider openai --reveal` でキー値を確認
4. `vault-cli list` でキーが一覧に表示されることを確認
5. `vault-cli delete --provider openai` でキーを削除
6. `vault-cli get --provider openai` で "not registered" を確認

### シナリオ 3: EnvVaultBackend からの透過的な切り替え

1. `EnvVaultBackend` を使用している既存テストが引き続き PASS すること
2. `WithKeyringVault()` オプションなしの場合、`EnvVaultBackend` がデフォルトのまま

## テスト項目 (Testing for the Requirements)

### 単体テスト

| 要件 | テストファイル | テスト内容 |
|---|---|---|
| 1 | `shared/libs/go/vault/keyring_backend_test.go` | Set/Resolve/Delete/List の基本動作。go-keyring の mock backend を使用 |
| 1 | `shared/libs/go/vault/keyring_backend_test.go` | スレッドセーフ性 (並行 Set/Delete) |
| 1 | `shared/libs/go/vault/keyring_backend_test.go` | vault:// プロトコル解決 |
| 2 | `examples/vault-cli/main_test.go` | 各サブコマンドのロジック関数テスト (runSetLogic, runGetLogic, runDeleteLogic, runListLogic, runStatusLogic) |
| 4 | `shared/libs/go/hag/server_test.go` | WithKeyringVault() オプションのテスト |

### ビルド・全体検証

1. ビルド + 単体テスト:
   ```bash
   scripts/process/build.sh
   ```

2. LLM 統合テスト (Part 3 Step 10):
   ```bash
   scripts/process/integration_test.sh --categories "llm"
   ```

### テスト実行の前提条件

- 単体テスト: `go-keyring` の mock backend を使用するため、OS Keyring へのアクセスは不要
- 統合テスト: `vault-cli` で事前に API キーを OS Keyring に登録済みであること
