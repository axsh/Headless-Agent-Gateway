# Vault外部APIとCLIファサード提供の要求仕様

## 背景 (Background)

現在の `features/vault-cli` は、Vault操作ロジックをCLIアプリ内で直接保持しており、外部プログラムから同等機能を再利用しにくい状態である。  
`client` / `server` と同様に、外部利用を前提とした公開APIパッケージとして `vault/` を新設し、以下を満たす必要がある。

- Vaultへの個別アクセス（Set/Get/Delete/List/Status 相当）をAPIとして外部公開すること
- CLI相当の操作フローをメイン関数からそのまま使える高水準APIとして公開すること
- `features/vault-cli` は公開APIの利用例（サンプル）として最小実装に寄せること

## 要件 (Requirements)

### 必須要件 (MUST)

1. **新規公開パッケージ**
   - リポジトリルートに `vault/` パッケージを新設する。
   - `client` / `server` と同等に、外部モジュールが import して利用できるAPIを提供する。

2. **個別Vault操作API**
   - `features/vault-cli` の既存操作と同等の機能を提供する。
   - 最低限以下の操作を含む。
     - 値の保存（set）
     - 値の取得 / 登録確認（get + reveal相当）
     - 値の削除（delete）
     - キー一覧取得（list）
     - 既知プロバイダ状態取得（status）
   - `--provider` と `--key` の解決ルール（`providers/{provider}/default` への展開）を共通化する。

3. **CLIファサードAPI（高水準API）**
   - メイン関数から直接呼び出せる、CLI操作を包括する高水準APIを提供する。
   - 呼び出し側が `os.Args` 相当の入力を渡すだけで処理を実行できる形を優先する。
   - 標準入出力（stdin/stdout/stderr）と終了コード制御を注入可能にし、テスト容易性を確保する。

4. **`features/vault-cli` の役割再定義**
   - `features/vault-cli` はロジック本体を持たず、`vault/` 公開APIを呼び出す薄いエントリポイントにする。
   - `features/vault-cli` が公開APIのサンプル実装として読める構成にする。

5. **互換性**
   - 既存 `vault-cli` のコマンド体系（`set/get/delete/list/status/version/help`）を維持する。
   - 既存テストが担保している振る舞い（メッセージ、エラー条件、キー解決）を後方互換で維持する。

### 推奨要件 (SHOULD)

1. APIは「低水準（個別操作）」と「高水準（CLIファサード）」の責務を明確に分離する。
2. 既知プロバイダ一覧は差し替え可能（デフォルト値あり）にし、将来拡張を容易にする。
3. エラー型を整理し、呼び出し側が入力エラー・Vaultアクセスエラーを判別可能にする。
4. READMEまたはサンプルコードで、ライブラリ利用例（CLI以外の埋め込み利用）を提示する。

## 実現方針 (Implementation Approach)

### 1. パッケージ構成

- `vault/` に公開APIを配置する。
- 例:
  - `vault/client.go`（低水準操作API）
  - `vault/cli.go`（CLIファサードAPI）
  - `vault/models.go`（オプション・結果型）

### 2. API設計方針

- **低水準API**: `VaultStore` を受け取り、操作単位で明示的に呼び出せる関数/メソッド群を提供する。
- **高水準API**: コマンド名と引数を受け、CLI仕様に沿ってパース・実行・出力を一括処理する。
- I/O抽象化（Reader/Writer）と依存注入により、`main` とテストの双方から同一APIを使用可能にする。

### 3. `features/vault-cli` の簡素化

- `main.go` は `vault/` の高水準APIを呼ぶだけにする。
- 既存の `runSetLogic` などは `vault/` へ移設し、`main.go` 側の重複実装を排除する。

### 4. 期待する利用イメージ

```go
// 個別操作APIの利用イメージ
svc := vault.NewService(vault.NewKeyringVaultBackend())
_ = svc.SetByProvider("anthropic", "sk-...")
status, _ := svc.ProviderStatus([]string{"anthropic", "openai", "google"})

// CLIファサードAPIの利用イメージ
runner := vault.NewCLIRunner(vault.CLIConfig{
    Store:  vault.NewKeyringVaultBackend(),
    Stdin:  os.Stdin,
    Stdout: os.Stdout,
    Stderr: os.Stderr,
})
code := runner.Run(os.Args[1:])
os.Exit(code)
```

## 検証シナリオ (Verification Scenarios)

1. `vault/` パッケージを import した外部コードから、`set/get/delete/list/status` 相当が直接呼べること。
2. CLIファサードAPIに `set --provider anthropic --stdin` を渡すと、対話無しで値が保存されること。
3. CLIファサードAPIに `get --provider anthropic` を渡すと、登録状態が出力されること。
4. CLIファサードAPIに `get --provider anthropic --reveal` を渡すと、実値を取得できること。
5. CLIファサードAPIに `delete --provider anthropic` を渡すと、削除後の `get` で未登録扱いになること。
6. `features/vault-cli` の `main` が公開API呼び出しのみで動作し、ロジック重複がないこと。
7. 既存コマンド（`help` / `version` 含む）で後方互換の振る舞いが維持されること。

## テスト項目 (Testing for the Requirements)

前提として、統合テスト前にビルドを実行する。

1. ビルド＋対象テスト実行（Vault CLI/ライブラリに関連する統合テストを指定）
   - `./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "Vault|vault|CLI|Keyring"`

2. CLIファサード経由の主要フロー検証（指定テストのみ再実行）
   - `./scripts/process/integration_test.sh --specify "TestVaultCLI"`

3. 低水準APIの操作単位検証（指定テストのみ再実行）
   - `./scripts/process/integration_test.sh --specify "TestVaultService|TestVaultAPI"`

4. リグレッション確認（影響範囲の全体確認）
   - `./scripts/process/integration_test.sh`

補足:
- 現行 `integration_test.sh` は `--specify` オプションでの絞り込みを正式サポートしているため、上記の自動検証は `--specify` ベースで記載する。
- 将来的にカテゴリ分類が整備された場合は、Vault関連カテゴリ（例: `common`）を追加して `--categories` 実行に置き換える。
