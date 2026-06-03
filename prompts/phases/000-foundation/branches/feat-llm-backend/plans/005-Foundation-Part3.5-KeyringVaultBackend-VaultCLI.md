# 005-Foundation-Part3.5-KeyringVaultBackend-VaultCLI

> **Source Specification**: [004-KeyringVaultBackend-and-VaultCLI.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/ideas/004-KeyringVaultBackend-and-VaultCLI.md)

## Goal Description

OS Keyring (Windows Credential Manager / macOS Keychain / Linux Secret Service) を利用した `KeyringVaultBackend` を実装し、API キーを安全に管理する。
合わせて `vault-cli` ツールを `examples/` に提供し、キーの登録・取得・削除をコマンドラインから操作可能にする。
Part 3 Step 10 の統合テストを実施するための前提条件を満たす。

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: KeyringVaultBackend が VaultStore インターフェースを実装 | Step 2: `vault/keyring_backend.go` |
| R1: go-keyring ライブラリの使用 | Step 1: 依存追加 |
| R1: vault:// プロトコル解決 | Step 2: `Resolve()` メソッド |
| R1: サービス名 `hag-vault-{tenantHash}` | Step 2: `buildKeyringServiceName()` |
| R1: スレッドセーフ (mutex) | Step 2: `mu sync.Mutex` |
| R2: vault-cli (set/get/delete/list/status) | Step 4: `examples/vault-cli/main.go` |
| R2: `--provider` -> `providers/<name>/default` 変換 | Step 4: `resolveKey()` |
| R2: `--key` オプション | Step 4: 引数パーサー |
| R3: BifrostAccount vault:// 連携 | 既存実装で対応済み (bifrost_account.go) |
| R4: WithKeyringVault() オプション | Step 3: `hag/options.go` |
| R4: デフォルトは EnvVaultBackend | 既存実装で対応済み (server.go) |
| R5: キーインデックス管理 | Step 2: `_vault_keys` メタキー |

## Proposed Changes

### vault パッケージ (shared/libs/go/vault)

#### [NEW] [keyring_backend_test.go](file://shared/libs/go/vault/keyring_backend_test.go)
*   **Description**: KeyringVaultBackend の単体テスト
*   **Technical Design**:
    ```go
    func TestMain(m *testing.M) {
        // go-keyring の MockInit() を使ってメモリ内モックに切り替え
        keyring.MockInit()
        os.Exit(m.Run())
    }
    ```
*   **テストケース**:
    1. `TestBuildKeyringServiceName` -- サービス名生成の決定性、テナント分離
        - 同じテナント -> 同じサービス名
        - 異なるテナント -> 異なるサービス名
        - "hag-vault-" プレフィックスを含むこと
    2. `TestToBase62` -- base62 エンコードの基本動作
        - 結果が base62 文字のみ含むこと
        - 異なる入力 -> 異なる出力
        - 空入力 -> "0"
    3. `TestKeyringVaultBackend_Set_Resolve` -- Set -> Resolve のラウンドトリップ
        - `Set("providers/anthropic/primary", "sk-test-key")` -> `Resolve("vault://providers/anthropic/primary")` == "sk-test-key"
    4. `TestKeyringVaultBackend_Delete` -- 削除後の Resolve がエラー
    5. `TestKeyringVaultBackend_List` -- Set した後の List に含まれること
    6. `TestKeyringVaultBackend_Resolve_NotFound` -- 未登録キーの Resolve がエラー
    7. `TestKeyringVaultBackend_Resolve_InvalidRef` -- vault:// でない文字列がエラー
    8. `TestKeyringVaultBackend_ConcurrentAccess` -- goroutine からの並行 Set/Delete

#### [NEW] [keyring_backend.go](file://shared/libs/go/vault/keyring_backend.go)
*   **Description**: OS Keyring を利用した VaultStore 実装
*   **Technical Design**:
    ```go
    package vault

    import (
        "crypto/sha256"
        "encoding/json"
        "errors"
        "fmt"
        "math/big"
        "strings"
        "sync"

        "github.com/zalando/go-keyring"
    )

    const base62Chars = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
    const defaultTenantID = "default"
    const vaultMetaKey = "_vault_keys"

    // toBase62 encodes a byte slice as a base62 string.
    func toBase62(data []byte) string {
        n := new(big.Int).SetBytes(data)
        base := big.NewInt(62)
        mod := new(big.Int)
        var result []byte
        for n.Sign() > 0 {
            n.DivMod(n, base, mod)
            result = append(result, base62Chars[mod.Int64()])
        }
        // Reverse
        for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
            result[i], result[j] = result[j], result[i]
        }
        if len(result) == 0 {
            return "0"
        }
        return string(result)
    }

    // buildKeyringServiceName creates the OS Keyring service name.
    func buildKeyringServiceName(tenantID ...string) string {
        tID := defaultTenantID
        if len(tenantID) > 0 && tenantID[0] != "" {
            tID = tenantID[0]
        }
        h := sha256.Sum256([]byte(strings.ToLower(tID)))
        return "hag-vault-" + toBase62(h[:])
    }

    // storedNode is the JSON representation persisted in the OS Keyring.
    type storedNode struct {
        Key   string `json:"key"`
        Value string `json:"value"`
    }

    // KeyringVaultBackend implements VaultStore using OS Keyring.
    type KeyringVaultBackend struct {
        serviceName string
        mu          sync.Mutex
    }

    // NewKeyringVaultBackend creates a backend scoped to the given tenant.
    func NewKeyringVaultBackend(tenantID ...string) *KeyringVaultBackend {
        return &KeyringVaultBackend{
            serviceName: buildKeyringServiceName(tenantID...),
        }
    }

    // ServiceName returns the computed service name (for testing).
    func (k *KeyringVaultBackend) ServiceName() string {
        return k.serviceName
    }

    // Resolve resolves a vault:// reference.
    func (k *KeyringVaultBackend) Resolve(ref string) (string, error) {
        path, ok := ParseVaultRef(ref)
        if !ok {
            return "", fmt.Errorf("not a vault reference: %s", ref)
        }
        secret, err := keyring.Get(k.serviceName, path)
        if err != nil {
            if errors.Is(err, keyring.ErrNotFound) {
                return "", fmt.Errorf("key not found in keyring: %s", path)
            }
            return "", fmt.Errorf("keyring get %q: %w", path, err)
        }
        var node storedNode
        if err := json.Unmarshal([]byte(secret), &node); err != nil {
            return "", fmt.Errorf("keyring unmarshal %q: %w", path, err)
        }
        return node.Value, nil
    }

    // Set stores a secret.
    func (k *KeyringVaultBackend) Set(path string, value string) error {
        k.mu.Lock()
        defer k.mu.Unlock()

        data, err := json.Marshal(storedNode{Key: path, Value: value})
        if err != nil {
            return fmt.Errorf("keyring marshal %q: %w", path, err)
        }
        if err := keyring.Set(k.serviceName, path, string(data)); err != nil {
            return fmt.Errorf("keyring set %q: %w", path, err)
        }
        return k.addToKeyIndex(path)
    }

    // Delete removes a secret.
    func (k *KeyringVaultBackend) Delete(path string) error {
        k.mu.Lock()
        defer k.mu.Unlock()

        _ = keyring.Delete(k.serviceName, path) // best-effort
        keys, _ := k.loadKeyIndex()
        remaining := make([]string, 0, len(keys))
        for _, key := range keys {
            if key != path {
                remaining = append(remaining, key)
            }
        }
        return k.saveKeyIndex(remaining)
    }

    // List returns all stored secret paths.
    func (k *KeyringVaultBackend) List() ([]string, error) {
        keys, err := k.loadKeyIndex()
        if err != nil {
            if errors.Is(err, keyring.ErrNotFound) {
                return []string{}, nil
            }
            return nil, fmt.Errorf("keyring list: %w", err)
        }
        return keys, nil
    }

    // --- Key index helpers (caller must hold mu for write) ---

    func (k *KeyringVaultBackend) loadKeyIndex() ([]string, error) {
        secret, err := keyring.Get(k.serviceName, vaultMetaKey)
        if err != nil {
            return nil, err
        }
        var keys []string
        if err := json.Unmarshal([]byte(secret), &keys); err != nil {
            return nil, err
        }
        return keys, nil
    }

    func (k *KeyringVaultBackend) saveKeyIndex(keys []string) error {
        data, _ := json.Marshal(keys)
        return keyring.Set(k.serviceName, vaultMetaKey, string(data))
    }

    func (k *KeyringVaultBackend) addToKeyIndex(key string) error {
        keys, _ := k.loadKeyIndex()
        for _, existing := range keys {
            if existing == key {
                return nil
            }
        }
        keys = append(keys, key)
        return k.saveKeyIndex(keys)
    }

    // Compile-time check
    var _ VaultStore = (*KeyringVaultBackend)(nil)
    ```

---

### hag パッケージ (shared/libs/go/hag)

#### [MODIFY] [options.go](file://shared/libs/go/hag/options.go)
*   **Description**: `WithKeyringVault()` オプション追加
*   **Technical Design**:
    ```go
    // WithKeyringVault configures the server to use OS Keyring for secret storage.
    // This replaces the default EnvVaultBackend.
    func WithKeyringVault(tenantID ...string) Option {
        return func(o *options) {
            o.vault = vault.NewKeyringVaultBackend(tenantID...)
        }
    }
    ```

---

### vault-cli (examples/vault-cli)

#### [NEW] [main_test.go](file://examples/vault-cli/main_test.go)
*   **Description**: vault-cli ロジック関数の単体テスト
*   **テストケース**:
    1. `TestRunSetLogic` -- set 後に Resolve で取得可能
    2. `TestRunSetLogic_EmptyValue` -- 空値はエラー
    3. `TestRunSetLogic_NoKeyOrProvider` -- --provider も --key もなしはエラー
    4. `TestRunGetLogic_Registered` -- 登録済みキーが "registered" を出力
    5. `TestRunGetLogic_NotRegistered` -- 未登録キーが "not registered" を出力
    6. `TestRunGetLogic_Reveal` -- --reveal でキー値を出力
    7. `TestRunDeleteLogic` -- delete 後に get で未登録
    8. `TestRunListLogic` -- set した後に list に表示
    9. `TestRunStatusLogic` -- status で登録状態を表示
    10. `TestResolveKey` -- --provider と --key のパス変換

#### [NEW] [main.go](file://examples/vault-cli/main.go)
*   **Description**: vault-cli CLI ツール
*   **Technical Design**:
    vv4 の `cmd/vault-cli/main.go` を移植。以下の変更:
    - パッケージインポートを `github.com/axsh/hag/vault` に変更
    - テナント検出ロジックを簡素化 (固定デフォルト)
    - `resolveKey()`: `--provider <name>` -> `providers/<name>/default`
    - `initStore()`: `vault.NewKeyringVaultBackend()` を直接使用
    - VaultStore ラッパーなし: HAG の `VaultStore` インターフェースを直接操作

    ```go
    // resolveKey converts --provider or --key to a full vault key path.
    func resolveKey(provider, key string) string {
        if provider != "" {
            return "providers/" + provider + "/default"
        }
        return key
    }
    ```

    サブコマンド:
    - `set`: `VaultStore.Set(resolveKey(...), value)` を呼ぶ
    - `get`: `VaultStore.Resolve("vault://" + resolveKey(...))` で取得チェック
    - `delete`: `VaultStore.Delete(resolveKey(...))` を呼ぶ
    - `list`: `VaultStore.List()` を呼ぶ
    - `status`: 既知プロバイダ (anthropic, openai) を順次 Resolve して状態表示

#### [NEW] [go.mod](file://examples/vault-cli/go.mod)
*   **Description**: vault-cli の Go module 定義
*   **Technical Design**:
    ```
    module github.com/axsh/hag/examples/vault-cli

    go 1.24.0

    require github.com/axsh/hag v0.0.0
    require github.com/zalando/go-keyring v0.2.8

    replace github.com/axsh/hag => ../../shared/libs/go
    ```

---

## Step-by-Step Implementation Guide

### Step 1: go-keyring 依存追加

- [x] `shared/libs/go/go.mod` に `github.com/zalando/go-keyring v0.2.8` を追加
- [x] `go mod tidy` 実行
- [x] `git add && git commit -m "deps: add go-keyring dependency for OS keyring access"`

### Step 2: KeyringVaultBackend 実装 (TDD)

1. [x] `shared/libs/go/vault/keyring_backend_test.go` を作成
    - `TestMain` で `keyring.MockInit()` を呼び出し
    - テストケース 1-8 を実装 (上記 Proposed Changes 参照)
2. [x] テスト実行 -> コンパイルエラー確認 (実装がまだないため)
3. [x] `shared/libs/go/vault/keyring_backend.go` を作成 (上記 Proposed Changes 参照)
4. [x] テスト実行 -> 全 PASS 確認
5. [x] `git add && git commit -m "feat: add KeyringVaultBackend for OS keyring secret storage"`

### Step 3: WithKeyringVault() オプション追加

1. [x] `shared/libs/go/hag/options.go` に `WithKeyringVault()` を追加
2. [x] `shared/libs/go/hag/server_test.go` に `TestNew_WithKeyringVault` を追加
    - `WithKeyringVault()` で Server を作成し、gateway が正常に動作することを確認
3. [x] テスト実行 -> 全 PASS 確認
4. [x] `git add && git commit -m "feat: add WithKeyringVault() option to hag.Server"`

### Step 4: vault-cli 実装 (TDD)

1. [x] `examples/vault-cli/go.mod` を作成
2. [x] `examples/vault-cli/main_test.go` を作成 (テストケース 1-10)
    - `TestMain` で `keyring.MockInit()` を呼び出し
3. [x] テスト実行 -> コンパイルエラー確認
4. [x] `examples/vault-cli/main.go` を作成
5. [x] テスト実行 -> 全 PASS 確認
6. [x] `git add && git commit -m "feat: add vault-cli example for OS keyring management"`

### Step 5: ビルド検証

1. [x] `./scripts/process/build.sh` で全テスト通過を確認
2. [/] `git push`

### Step 6: Verification Plan の実行

---

## Verification Plan

### Automated Verification

1. **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```

2. **vault パッケージ単体テスト (先行確認)**:
    ```bash
    # build.sh 内で実行されるが、開発中の先行確認用
    # (planning-rules.md により raw go test は計画書では使わないが、
    #  TDD の Red->Green ループでは build.sh を使用する)
    ```

3. **LLM 統合テスト (Part 3 Step 10)** -- vault-cli でキー登録後:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories "llm"
    ```

### テスト項目のセルフレビュー (testing-rules.md 11.4)

1. **網羅性の検証**: KeyringVaultBackend の CRUD 全操作 (Set/Resolve/Delete/List) + 並行性 + エラーケースをカバー。vault-cli は全サブコマンドのロジックをテスト。WithKeyringVault() による Server 統合もテスト。全テスト成功で「OS Keyring 経由の秘密管理が動作している」と言える。
2. **証拠の十分性**: 各テストは「期待する値が返る」「期待するエラーが返る」まで検証。単に「エラーが出ない」では終わらない。
3. **迂回の排除**: KeyringVaultBackend のテストは go-keyring の MockInit() を使い、実際に Set/Get/Delete が呼ばれることを保証。vault-cli テストは VaultStore を直接操作し、出力を io.Writer で検証。
4. **依存関係の整合性**: ボトムアップ順序 -- (1) toBase62/buildKeyringServiceName (2) KeyringVaultBackend CRUD (3) vault-cli ロジック (4) hag.Server 統合。

### 総合判定プロセス (testing-rules.md 12)

全テスト完了後、testing-rules.md 12.2 のチェック項目 (スキップテスト有無、部分エラー見落とし、迂回処理、アダプタ誤適用、テスト間依存、カバレッジ妥当性、外部システム状態) を実施し、総合判定結果を記録する。

---

## Documentation

#### [MODIFY] [004-KeyringVaultBackend-and-VaultCLI.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/ideas/004-KeyringVaultBackend-and-VaultCLI.md)
*   **更新内容**: 実装完了後に「実装済み」ステータスを追記

#### [MODIFY] [004-Foundation-Part3-BifrostDriver-Routing-Handlers.md](file://prompts/phases/000-foundation/branches/feat-llm-backend/plans/004-Foundation-Part3-BifrostDriver-Routing-Handlers.md)
*   **更新内容**: Step 10 の実行が可能になったことを記録
