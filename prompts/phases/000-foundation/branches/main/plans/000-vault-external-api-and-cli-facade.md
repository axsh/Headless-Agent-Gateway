# 000-vault-external-api-and-cli-facade

> **Source Specification**: `prompts/phases/000-foundation/branches/main/ideas/000-vault-external-api-and-cli-facade.md`

## Goal Description

`vault/` ルートパッケージを新設し、`features/vault-cli` に埋め込まれている Vault 操作ロジックを外部向け公開 API として再編成する。  
低水準 API（個別操作）と高水準 API（CLI ファサード）を同時提供し、`features/vault-cli` を公開 API の薄いサンプル実装へ置き換える。

## Execution Progress

- [x] `vault/` 公開 API パッケージ追加（`models.go`, `service.go`, `cli.go`）
- [x] `vault/` ユニットテスト追加（`service_test.go`, `cli_test.go`）
- [x] `features/vault-cli` を公開 API 呼び出しのみの薄いサンプル構成へ移行
- [x] `tests/common_vault_cli_test.go` に統合テスト追加
- [x] `README.md` に low-level API / CLI facade の利用例を追記
- [/] `./scripts/process/build.sh` 実行（既存の別領域テスト失敗で全体は FAIL）
- [x] `./scripts/process/integration_test.sh --specify "TestVaultCLIFlow"` 実行（PASS）

## User Review Required

1. 低水準 API の公開シグネチャ（`Service` ベース API の関数名と戻り値型）。
2. CLI ファサード API の I/O 注入方式（`Run(args []string) int` を固定するか、`Run(args []string) error` + 呼び出し側でコード変換にするか）。
3. `status` の既知プロバイダ初期値（`anthropic`, `openai`, `google`）を維持するか、config 由来に拡張するか。

## Requirement Traceability

> **Traceability Check**:
> 仕様書の要件・決定事項を列挙し、本計画の実装箇所へマッピングする。  
> 先送り項目は理由を併記する。

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| `vault/` 新規公開パッケージを作る | Proposed Changes > `vault/service.go`, `vault/cli.go`, `vault/models.go` |
| set/get/delete/list/status の個別 Vault API を公開する | Proposed Changes > `vault/service.go` |
| provider/key 解決ルールを共通化する | Proposed Changes > `vault/service.go` (`ResolveKey`) |
| CLI として main から直接使える高水準 API を提供する | Proposed Changes > `vault/cli.go` (`CLIRunner`) |
| stdin/stdout/stderr と終了コードを注入可能にする | Proposed Changes > `vault/models.go`, `vault/cli.go` |
| `features/vault-cli` を薄いサンプル実装にする | Proposed Changes > `features/vault-cli/main.go` |
| 既存コマンド体系の互換性維持 | Proposed Changes > `vault/cli.go`, `features/vault-cli/main_test.go` |
| エラー型整理（推奨） | Proposed Changes > `vault/models.go`, `vault/service.go`, `vault/cli.go` |
| 既知プロバイダ差し替え可能化（推奨） | Proposed Changes > `vault/models.go`, `vault/cli.go` |
| README/サンプル更新（推奨） | Documentation > `README.md` |

## Proposed Changes

### vault (新規公開 API パッケージ)

#### [NEW] `vault/service_test.go` (file://vault/service_test.go)
* **Description**: 低水準 API の TDD 用ユニットテストを先行追加する。
* **Technical Design**:
    * テーブル駆動で `ResolveKey` と各操作メソッドを検証する。
    * 既存 `shared/libs/go/vault.NewKeyringVaultBackend("test-cli")` と `go-keyring` mock を利用する。
    * 主要ケース:
        * `SetByProvider("anthropic", value)` が `providers/anthropic/default` に保存される
        * `GetByProvider(..., reveal=false)` が登録状態を返す
        * `GetByProvider(..., reveal=true)` が実値を返す
        * `DeleteByProvider` 後に未登録状態へ遷移する
        * `List` がキー一覧を返す
        * `ProviderStatus` が known providers の状態を返す
    * ```go
      tests := []struct {
          name string
          provider string
          key string
          want string
      }{
          {name: "provider shorthand", provider: "anthropic", want: "providers/anthropic/default"},
          {name: "custom key", key: "custom/path", want: "custom/path"},
      }
      ```
* **Logic**:
    * 仕様書の `--provider` / `--key` 解決ルールをそのまま検証対象にする。
    * `status` は `anthropic/openai/google` を個別に registered / not registered 判定する。

#### [NEW] `vault/cli_test.go` (file://vault/cli_test.go)
* **Description**: CLI ファサード API のコマンド互換と I/O 振る舞いを検証するユニットテストを追加する。
* **Technical Design**:
    * `CLIRunner` に `bytes.Buffer` と `strings.Reader` を注入して非対話テストを行う。
    * 主要ケース:
        * `set --provider anthropic --stdin` が成功コードを返す
        * `get --provider anthropic` で `registered` 表示
        * `get --provider anthropic --reveal` で値表示
        * `delete --provider anthropic` 後の `get` は `not registered`
        * `list`, `status`, `help`, `version` の出力互換
        * unknown command / 引数不足の失敗コード
    * ```go
      cfg := CLIConfig{Store: store, Stdin: in, Stdout: &out, Stderr: &errOut}
      runner := NewCLIRunner(cfg)
      code := runner.Run([]string{"set", "--provider", "anthropic", "--stdin"})
      ```
* **Logic**:
    * 仕様書の「メイン関数からそのまま使える API」を `Run(args []string) int` で検証する。
    * 既存 CLI の成功/失敗時の出口（終了コードとエラーメッセージ）を維持する。

#### [NEW] `vault/models.go` (file://vault/models.go)
* **Description**: 低水準 API / CLI ファサード双方で使うオプション・結果・エラー型を定義する。
* **Technical Design**:
    * 公開構造体案:
    * ```go
      type Service struct {
          Store sharedvault.VaultStore
      }

      type GetResult struct {
          FullKey    string
          Registered bool
          Value      string
      }

      type ProviderState struct {
          Provider   string
          Registered bool
      }

      type CLIConfig struct {
          Store          sharedvault.VaultStore
          Stdin          io.Reader
          Stdout         io.Writer
          Stderr         io.Writer
          KnownProviders []string
          AppName        string
          AppVersion     string
      }
      ```
* **Logic**:
    * 仕様書の「既知プロバイダ差し替え可能」を `KnownProviders` で満たす。
    * 入力エラーと Vault アクセスエラーを判別できるよう、`var ErrInvalidInput` 等の sentinel error を定義する。

#### [NEW] `vault/service.go` (file://vault/service.go)
* **Description**: 個別 Vault 操作 API（set/get/delete/list/status）を実装する。
* **Technical Design**:
    * 公開メソッド案:
    * ```go
      func NewService(store sharedvault.VaultStore) *Service
      func ResolveKey(provider, key string) (string, error)
      func (s *Service) Set(provider, key, value string) (string, error)
      func (s *Service) Get(provider, key string, reveal bool) (GetResult, error)
      func (s *Service) Delete(provider, key string) (string, error)
      func (s *Service) List() ([]string, error)
      func (s *Service) Status(providers []string) ([]ProviderState, error)
      ```
* **Logic**:
    * `provider != ""` の場合は `providers/{provider}/default` を構築し、`key` は無視する。
    * `provider == "" && key != ""` は `key` をそのまま採用する。
    * `provider == "" && key == ""` は入力エラーを返す。
    * `Get` は `store.Resolve("vault://"+fullKey)` を使い、`reveal=false` なら値を返さず登録状態のみ返す。
    * `Status` は `providers` 配列を順に `providers/{name}/default` へ変換して登録有無を判定する。

#### [NEW] `vault/cli.go` (file://vault/cli.go)
* **Description**: CLI コマンド互換を維持した高水準 API（CLI ファサード）を実装する。
* **Technical Design**:
    * 公開型案:
    * ```go
      type CLIRunner struct {
          svc   *Service
          cfg   CLIConfig
      }

      func NewCLIRunner(cfg CLIConfig) *CLIRunner
      func (r *CLIRunner) Run(args []string) int
      ```
    * `Run` 内で `set/get/delete/list/status/version/help` を dispatch する。
    * `set` は `--stdin` 指定時に `cfg.Stdin` から1行読み取り、未指定時は `cfg.Stderr` へ `Enter value: ` を出し対話入力する。
* **Logic**:
    * 既存 `features/vault-cli/main.go` のロジックを API 化して移設する。
    * 出力文言は既存互換（`Set: <key>`, `<key>: registered`, `<key>: not registered`, `Deleted: <key>`）を維持する。
    * 未知コマンド時は usage を表示し失敗コードを返す。

### features/vault-cli (サンプル化)

#### [MODIFY] `features/vault-cli/main_test.go` (file://features/vault-cli/main_test.go)
* **Description**: `vault` 公開 API を利用する前提にテストを更新し、CLI互換を継続保証する。
* **Technical Design**:
    * 既存のロジック直呼びテストを `vault.CLIRunner` ベースへ置換する。
    * 既存ケース（resolve/set/get/delete/list/status）を維持し、移行時の挙動差異を防ぐ。
* **Logic**:
    * `features/vault-cli` は実装本体ではなくサンプルのため、ロジックは `vault` パッケージ側テストで主に担保する。

#### [MODIFY] `features/vault-cli/main.go` (file://features/vault-cli/main.go)
* **Description**: 既存ロジックを除去し、`vault.CLIRunner` 呼び出しのみの薄いエントリポイントにする。
* **Technical Design**:
    * `main` 実装を以下に簡素化:
    * ```go
      func main() {
          runner := vault.NewCLIRunner(vault.CLIConfig{
              Store:  sharedvault.NewKeyringVaultBackend(),
              Stdin:  os.Stdin,
              Stdout: os.Stdout,
              Stderr: os.Stderr,
              AppName: "vault-cli",
              AppVersion: "0.1.0",
          })
          os.Exit(runner.Run(os.Args[1:]))
      }
      ```
* **Logic**:
    * 仕様書の「`features/vault-cli` はサンプルの役割」に合わせ、ビジネスロジックを持たせない。

### tests (統合/E2E 観点)

#### [NEW] `tests/common_vault_cli_test.go` (file://tests/common_vault_cli_test.go)
* **Description**: ルート `tests/` に Vault CLI 経由の統合テストを追加し、公開 API 再編後の実動作を検証する。
* **Technical Design**:
    * `//go:build integration` を付与。
    * `features/vault-cli` の `main` 相当フローを `vault.CLIRunner` 経由で通し、状態遷移（set -> get -> delete -> get）を検証。
    * keyring mock を使い環境依存を避ける。
* **Logic**:
    * 仕様書の検証シナリオ 2〜7 を統合テストへ直接マッピングする。

## Step-by-Step Implementation Guide

1. **TDD: 低水準 API テスト先行**
    * Edit `vault/service_test.go` to add failing tests for `ResolveKey`, `Set/Get/Delete/List/Status`.
    * 仕様書の provider/key 解決ルールと status 判定ロジックをケースとして固定化する。

2. **TDD: CLI ファサードテスト先行**
    * Edit `vault/cli_test.go` to add failing tests for command dispatch, stdin/stdout/stderr, exit code.
    * `set/get/delete/list/status/version/help` と unknown command を全て網羅する。

3. **データ構造の実装**
    * Edit `vault/models.go` to define `Service`, `GetResult`, `ProviderState`, `CLIConfig`, error values.
    * API 利用者が型だけ見て利用可能なレベルまで公開定義を確定する。

4. **低水準 API 実装**
    * Edit `vault/service.go` to implement tested behavior using `shared/libs/go/vault.VaultStore`.
    * `ResolveKey(provider,key)` を共通関数化し、CLI と低水準 API で再利用する。

5. **CLI ファサード API 実装**
    * Edit `vault/cli.go` to implement argument parsing and command execution over `Service`.
    * `--stdin` と対話入力の両ルートを実装し、既存文言互換を満たす。

6. **features/vault-cli のサンプル化**
    * Edit `features/vault-cli/main.go` to delegate all behavior to `vault.CLIRunner`.
    * Edit `features/vault-cli/main_test.go` to validate sample entrypoint behavior without logic duplication.

7. **統合テスト追加**
    * Edit `tests/common_vault_cli_test.go` to verify end-to-end state transition and command compatibility.
    * `set -> get -> get --reveal -> delete -> get` の時系列検証を 1 シナリオとして固定する。

8. **ドキュメント更新**
    * Edit `README.md` to add concise usage snippets for `vault` low-level API and CLI facade API.

9. **最終検証実行**
    * Verification Plan の Automated Verification を順に実行し、全テスト完了後に総合判定を実施する。

## Verification Plan

### Automated Verification

1. **Build & Unit Tests (Fail Fast)**
    実装中のユニットテスト失敗を早期検出する。
    ```bash
    ./scripts/process/build.sh --skip-frontend --skip-etc
    ```

2. **Integration Tests (Vault related)**
    Vault 関連統合テストを対象絞り込みで検証する。
    ```bash
    ./scripts/process/build.sh --skip-etc && ./scripts/process/integration_test.sh --specify "Vault|vault|CLI|Keyring|common_vault_cli"
    ```
    * **Log Verification**: `Set:`, `Deleted:`, `registered`, `not registered` の期待文言がテストログ上で再現され、`panic` / `ERROR` / `unknown command` が想定外に出ていないことを確認する。

3. **Regression Integration Tests**
    影響範囲のリグレッションを全体確認する。
    ```bash
    ./scripts/process/build.sh --skip-etc && ./scripts/process/integration_test.sh
    ```
    * **Log Verification**: Vault 変更に起因する他カテゴリ失敗がないこと（特に `agentservice`, `llm`, `wsserver` 系）を確認する。

4. **E2E Tests (新規/追加)**
    Vault API 再編はサーバー外のライブラリ/CLI 層変更であり、`tests/agentservice_e2e_test.go` 系のエージェント E2E 対象ではない。  
    代わりに `tests/common_vault_cli_test.go` を integration として追加し、外部観測可能な CLI 挙動を自動化検証する。

    #### [NEW] `common_vault_cli_test.go` (file://tests/common_vault_cli_test.go)
    * **テストケース**:
        * `TestVaultCLIFlow_SetGetRevealDelete`
        * `TestVaultCLIFlow_ListAndStatus`
        * `TestVaultCLIFlow_HelpVersionAndInvalidCommand`
    * **検証ポイント**:
        * provider/key 解決規則どおりに保存・取得・削除できること。
        * CLI ファサード経由でも既存互換のメッセージと終了コードになること。
        * `features/vault-cli` が薄いラッパー化されても挙動が不変であること。

5. **テスト項目設計セルフレビュー（testing-rules §11.4）**
    * **網羅性**: 低水準 API（末端）→ CLI ファサード（中間）→ sample main（上位）のボトムアップ順を満たす。
    * **証拠の十分性**: 戻り値だけでなく、Vault 内状態遷移と出力文字列・終了コードまで検証する。
    * **迂回排除**: `features/vault-cli/main.go` が `vault` API を経由することをテストで固定し、旧ロジック残存を検知可能にする。
    * **依存整合**: 低水準 API テスト失敗時は上位テストを意味ある失敗として扱える順序で構成する。

6. **総合判定プロセス（testing-rules §12）**
    全テスト完了後、以下を記録する:
    * SKIP/WARN/ERROR の有無
    * フォールバックによる偽成功の有無
    * 設定誤適用（別 backend 利用）の有無
    * テスト順序依存の有無
    * Vault 新機能テスト項目の欠落有無

## Documentation

#### [MODIFY] `README.md` (file://README.md)
* **更新内容**:
    * `vault/` 公開 API の目的と位置づけ（`client` / `server` と同様の外部公開層）を追加。
    * 低水準 API 利用例（set/get/status）。
    * CLI ファサード API 利用例（`runner.Run(os.Args[1:])`）。
    * `features/vault-cli` が公開 API 利用サンプルであることを明記。
