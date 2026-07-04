# 000-Fix-Vault-Backend-Default-Selection

> **Source Specification**: [000-Fix-Vault-Backend-Default-Selection.md](file:///prompts/phases/000-foundation/branches/fix-auth-failure/ideas/000-Fix-Vault-Backend-Default-Selection.md)

## Goal Description

`vault.backend`（単数、暗黙フォールバック）を `vault.backends`（複数、順序付き必須リスト）に置き換え、Chain of Responsibility パターンでシークレットを解決する。旧パラメータ使用時・未設定時・不正値時にはフィードバック性の高いエラーメッセージを返す。

## User Review Required

> [!IMPORTANT]
> **破壊的変更**: `vault.backend`（単数形）を廃止し `vault.backends`（複数形）を必須パラメータとする。既存の設定ファイルはすべて更新が必要になる。

- 既存のE2Eテスト（`gemini_e2e_test.go` 等）で `VaultConfig{Backend: "keyring"}` を使用している箇所を `VaultConfig{Backends: []string{"keyring"}}` に変更する
- `server_test.go` で `backend: "env"` を含むYAMLリテラルを更新する

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: `vault.backends` リストの導入 | config/config.go VaultConfig + server/server.go resolveVault() |
| R2: `backends` の必須化とバリデーション | server/server.go resolveVault() バリデーション |
| R3: 個別バックエンドのバリデーション | server/server.go resolveVault() switch + file_path チェック |
| R4: Chain of Responsibility による解決 | vault/chain_backend.go ChainVaultBackend |
| R5: 旧 `backend` パラメータの後方互換 | server/server.go resolveVault() 旧フィールド検出 |
| R6: 解決結果のデバッグログ | vault/chain_backend.go Resolve() 内ログ出力 |

## Proposed Changes

### shared/libs/go/config (設定構造体)

---

#### [MODIFY] [config.go](file:///shared/libs/go/config/config.go)
*   **Description**: `VaultConfig` に `Backends` フィールドを追加し、旧 `Backend` をdeprecatedとして残す
*   **Technical Design**:
    ```go
    // VaultConfig holds VaultStore settings.
    type VaultConfig struct {
        // Backend is the old singular backend field (deprecated).
        // If set, resolveVault() returns a migration error.
        Backend string `yaml:"backend,omitempty"`

        // Backends is the ordered list of vault backends to try.
        // Required. Supported values: "keyring", "env", "file".
        // The resolution order matches the list order.
        Backends []string `yaml:"backends"`

        // FilePath is the file path for FileVaultBackend.
        FilePath string `yaml:"file_path,omitempty"`

        // AESEnabled enables AES encryption for FileVaultBackend.
        AESEnabled bool `yaml:"aes_enabled,omitempty"`
    }
    ```
*   **Logic**:
    - `Backend` フィールドは `yaml:"backend,omitempty"` に変更し、新しい設定ファイルでは出力されないようにする
    - `Backends` フィールドを `yaml:"backends"` として追加
    - `FilePath` と `AESEnabled` は変更なし

---

#### [MODIFY] [config_test.go](file:///shared/libs/go/config/config_test.go)
*   **Description**: 既存テストを `Backends` フィールドに対応させ、新しいYAML形式のテストを追加
*   **Technical Design**:
    - 既存テストケース "full config", "minimal config" で `backend: "env"` を `backends: [env]` に変更
    - "file vault backend" テストで `backend: "file"` を `backends: [file]` に変更
    - 新テストケースを追加:
      ```go
      {
          name: "backends list order",
          input: `
      vault:
        backends: [keyring, env]
      `,
          want: AppConfig{
              Vault: VaultConfig{Backends: []string{"keyring", "env"}},
          },
      },
      {
          name: "old backend field preserved",
          input: `
      vault:
        backend: "keyring"
      `,
          want: AppConfig{
              Vault: VaultConfig{Backend: "keyring"},
          },
      },
      ```

---

#### [MODIFY] [loader_test.go](file:///shared/libs/go/config/loader_test.go)
*   **Description**: `TestLoad_ValidConfig` のYAMLを `backends` 形式に更新し、アサーションを修正
*   **Technical Design**:
    - YAML内の `backend: "env"` を `backends: [env]` に変更
    - アサーション `cfg.Vault.Backend != "env"` を `len(cfg.Vault.Backends) != 1 || cfg.Vault.Backends[0] != "env"` に変更

---

### shared/libs/go/vault (Chain of Responsibility バックエンド)

---

#### [NEW] [chain_backend.go](file:///shared/libs/go/vault/chain_backend.go)
*   **Description**: 複数の `VaultStore` を順番に試行する Chain of Responsibility パターンの実装
*   **Technical Design**:
    ```go
    package vault

    import (
        "fmt"
        "strings"

        "github.com/axsh/arctic-tern/shared/libs/go/logger"
    )

    // ChainVaultBackend tries multiple VaultStore backends in order.
    // It implements VaultStore.
    type ChainVaultBackend struct {
        backends []namedBackend
        logger   logger.Logger
    }

    // namedBackend pairs a VaultStore with its name for diagnostics.
    type namedBackend struct {
        name  string
        store VaultStore
    }

    // NewChainVaultBackend creates a ChainVaultBackend from ordered backends.
    // names and stores must have the same length.
    func NewChainVaultBackend(names []string, stores []VaultStore, log logger.Logger) *ChainVaultBackend {
        backends := make([]namedBackend, len(stores))
        for i := range stores {
            backends[i] = namedBackend{name: names[i], store: stores[i]}
        }
        return &ChainVaultBackend{backends: backends, logger: log}
    }

    // Resolve tries each backend in order until one succeeds.
    // If all fail, returns an error with details of each failure.
    func (c *ChainVaultBackend) Resolve(ref string) (string, error) {
        var errs []string
        for i, nb := range c.backends {
            val, err := nb.store.Resolve(ref)
            if err == nil {
                if c.logger != nil {
                    c.logger.Debug("vault ref resolved",
                        "ref", ref,
                        "via", nb.name,
                        "tried", fmt.Sprintf("%d/%d backends", i+1, len(c.backends)))
                }
                return val, nil
            }
            errs = append(errs, fmt.Sprintf("  %d. %s: %s", i+1, nb.name, err.Error()))
        }

        return "", fmt.Errorf(
            "failed to resolve vault reference %q.\n\n"+
                "Tried %d backends in order:\n%s",
            ref, len(c.backends), strings.Join(errs, "\n"))
    }

    // Set stores a secret using the first backend.
    func (c *ChainVaultBackend) Set(path string, value string) error {
        if len(c.backends) == 0 {
            return fmt.Errorf("no backends configured")
        }
        return c.backends[0].store.Set(path, value)
    }

    // Delete removes a secret from the first backend.
    func (c *ChainVaultBackend) Delete(path string) error {
        if len(c.backends) == 0 {
            return fmt.Errorf("no backends configured")
        }
        return c.backends[0].store.Delete(path)
    }

    // List returns all secret paths from the first backend.
    func (c *ChainVaultBackend) List() ([]string, error) {
        if len(c.backends) == 0 {
            return nil, fmt.Errorf("no backends configured")
        }
        return c.backends[0].store.List()
    }
    ```
*   **Logic**:
    - `Resolve()`: `backends` を先頭から順に試行。成功すればDEBUGログ付きで即座に返す。全て失敗した場合、各バックエンドの失敗理由を番号付きで列挙したエラーメッセージを返す (R4, R6)
    - `Set()`, `Delete()`, `List()`: 最初のバックエンドに対してのみ実行（シークレットの書き込み先は1つに限定）

---

#### [NEW] [chain_backend_test.go](file:///shared/libs/go/vault/chain_backend_test.go)
*   **Description**: ChainVaultBackend の単体テスト
*   **Technical Design**:
    ```go
    package vault

    import (
        "fmt"
        "strings"
        "testing"
    )

    // stubVault is a test double for VaultStore.
    type stubVault struct {
        secrets map[string]string
    }

    func newStubVault(secrets map[string]string) *stubVault {
        return &stubVault{secrets: secrets}
    }

    func (s *stubVault) Resolve(ref string) (string, error) {
        path, ok := ParseVaultRef(ref)
        if !ok {
            return "", fmt.Errorf("not a vault reference: %s", ref)
        }
        val, exists := s.secrets[path]
        if !exists {
            return "", fmt.Errorf("key not found: %s", path)
        }
        return val, nil
    }

    func (s *stubVault) Set(path, value string) error {
        s.secrets[path] = value
        return nil
    }

    func (s *stubVault) Delete(path string) error {
        delete(s.secrets, path)
        return nil
    }

    func (s *stubVault) List() ([]string, error) {
        keys := make([]string, 0, len(s.secrets))
        for k := range s.secrets {
            keys = append(keys, k)
        }
        return keys, nil
    }
    ```
*   **テストケース**:

    | テスト関数 | 検証内容 | 対応要件 |
    |---|---|---|
    | `TestChainVaultBackend_FirstSuccess` | 最初のバックエンドで解決成功 → 即座に返す | R4 |
    | `TestChainVaultBackend_Fallback` | 1番目失敗 → 2番目で解決成功 | R4 |
    | `TestChainVaultBackend_AllFail` | 全バックエンド失敗 → エラーに各失敗理由を含む | R4 |
    | `TestChainVaultBackend_AllFail_ErrorFormat` | エラーメッセージの形式（"Tried N backends"、番号付きリスト） | R4 |
    | `TestChainVaultBackend_Set_FirstOnly` | `Set()` が最初のバックエンドのみに書き込む | R4 |
    | `TestChainVaultBackend_Delete_FirstOnly` | `Delete()` が最初のバックエンドのみから削除 | R4 |
    | `TestChainVaultBackend_List_FirstOnly` | `List()` が最初のバックエンドのリストのみ返す | R4 |

---

### server (Vault解決ロジック)

---

#### [MODIFY] [server.go](file:///server/server.go)
*   **Description**: `resolveVault()` を改修し、`backends` リストのバリデーション + ChainVaultBackend構築 + 旧フィールド検出エラーを実装
*   **Technical Design**:
    - `resolveVault` のシグネチャを変更:
    ```go
    func resolveVault(o *options, cfg *config.AppConfig, log logger.Logger) (vault.VaultStore, error)
    ```
    - 呼び出し元 `New()` を修正（L86）:
    ```go
    // Step 4: Resolve VaultStore.
    vs, err := resolveVault(o, cfg, log)
    if err != nil {
        return nil, fmt.Errorf("tern: %w", err)
    }
    ```
*   **Logic** -- `resolveVault()` の完全なロジック:
    ```go
    func resolveVault(o *options, cfg *config.AppConfig, log logger.Logger) (vault.VaultStore, error) {
        // 1. WithVaultStore option takes priority.
        if o.vault != nil {
            return o.vault, nil
        }

        // 2. Detect deprecated backend (singular) field (R5).
        if cfg.Vault.Backend != "" {
            return nil, fmt.Errorf(
                "vault.backend (singular) is no longer supported. "+
                    "Use vault.backends (plural) instead.\n\n"+
                    "Migration example:\n"+
                    "  # Before\n"+
                    "  vault:\n"+
                    "    backend: %s\n\n"+
                    "  # After\n"+
                    "  vault:\n"+
                    "    backends: [%s]",
                cfg.Vault.Backend, cfg.Vault.Backend)
        }

        // 3. Validate backends is set and non-empty (R2).
        if len(cfg.Vault.Backends) == 0 {
            return nil, fmt.Errorf(
                "vault.backends is required but not configured.\n\n"+
                    "Add a 'backends' list to the vault section of your config file to specify\n"+
                    "which secret backends to use and in what order they should be tried.\n\n"+
                    "Example configurations:\n\n"+
                    "  # Use OS keyring (recommended for desktop environments)\n"+
                    "  vault:\n"+
                    "    backends: [keyring]\n\n"+
                    "  # Use OS keyring first, fall back to environment variables\n"+
                    "  vault:\n"+
                    "    backends: [keyring, env]\n\n"+
                    "  # Use encrypted file backend\n"+
                    "  vault:\n"+
                    "    backends: [file]\n"+
                    "    file_path: /path/to/secrets.json\n\n"+
                    "Supported backends: keyring, env, file")
        }

        // 4. Build each backend and validate (R3).
        supported := map[string]bool{"env": true, "keyring": true, "file": true}
        names := make([]string, 0, len(cfg.Vault.Backends))
        stores := make([]vault.VaultStore, 0, len(cfg.Vault.Backends))

        for _, name := range cfg.Vault.Backends {
            if !supported[name] {
                return nil, fmt.Errorf(
                    "unsupported vault backend %q in vault.backends.\n\n"+
                        "Supported backends: keyring, env, file\n\n"+
                        "Check your config file for typos in the vault.backends list.",
                    name)
            }
            switch name {
            case "env":
                stores = append(stores, vault.NewEnvVaultBackend())
            case "keyring":
                stores = append(stores, vault.NewKeyringVaultBackend())
            case "file":
                if cfg.Vault.FilePath == "" {
                    return nil, fmt.Errorf(
                        "vault backend \"file\" requires vault.file_path to be set.\n\n"+
                            "Example:\n"+
                            "  vault:\n"+
                            "    backends: [file]\n"+
                            "    file_path: /path/to/secrets.json")
                }
                fb, err := vault.NewFileVaultBackend(cfg.Vault.FilePath)
                if err != nil {
                    return nil, fmt.Errorf("vault file backend: %w", err)
                }
                stores = append(stores, fb)
            }
            names = append(names, name)
        }

        // 5. If only one backend, return it directly (no chain overhead).
        if len(stores) == 1 {
            return stores[0], nil
        }

        // 6. Build chain (R4).
        return vault.NewChainVaultBackend(names, stores, log), nil
    }
    ```
    - **最適化**: バックエンドが1つだけの場合、ChainVaultBackend でラップせず直接返す（オーバーヘッド回避）

---

#### [MODIFY] [server_test.go](file:///server/server_test.go)
*   **Description**: 既存テストの `Backend` → `Backends` 更新 + 新しいバリデーションテスト追加
*   **Technical Design**:
    - 既存テスト修正箇所:
      - L61-62: `vault:\n  backend: "env"` → `vault:\n  backends: [env]`
      - L181-182: 同上
    - 新規テスト:

    | テスト関数 | 検証内容 | 対応要件 |
    |---|---|---|
    | `TestResolveVault_BackendsRequired` | `Backends` 未設定 → エラー、メッセージに "vault.backends is required" と設定例 | R2 |
    | `TestResolveVault_BackendsEmpty` | `Backends: []string{}` → 同上のエラー | R2 |
    | `TestResolveVault_UnsupportedBackend` | `Backends: []string{"redis"}` → エラー、メッセージに "unsupported" と "Supported backends" | R3 |
    | `TestResolveVault_FileMissingPath` | `Backends: []string{"file"}` + `FilePath: ""` → エラー、メッセージに "file_path" | R3 |
    | `TestResolveVault_OldBackendField` | `Backend: "keyring"` + `Backends` 未設定 → エラー、メッセージに "no longer supported" とマイグレーション例 | R5 |
    | `TestResolveVault_SingleBackend` | `Backends: []string{"env"}` → `EnvVaultBackend` が直接返る（ChainVaultBackend ではない） | R1 |
    | `TestResolveVault_MultipleBackends` | `Backends: []string{"keyring", "env"}` → `ChainVaultBackend` が返る | R1, R4 |
    | `TestNew_VaultBackendsRequired` | `New(WithConfig(&config.AppConfig{}))` → `vault.backends is required` エラー | R2 |

---

### tests (E2Eテスト修正)

---

#### [MODIFY] [gemini_e2e_test.go](file:///tests/gemini_e2e_test.go)
*   **Description**: 3箇所の `VaultConfig{Backend: "keyring"}` を `VaultConfig{Backends: []string{"keyring"}}` に更新
*   **Technical Design**:
    - L58-60: `Vault: config.VaultConfig{Backend: "keyring"}` → `Vault: config.VaultConfig{Backends: []string{"keyring"}}`
    - L139: 同上
    - L220: 同上

---

### prompts/specifications (ドキュメント)

この変更はコード内のVaultConfig仕様ドキュメントへの影響のみで、`prompts/specifications` 配下に該当する仕様書は確認されなかったため、ドキュメント更新は不要。

---

## Step-by-Step Implementation Guide

### Step 1: `VaultConfig` 構造体の更新

- Edit `shared/libs/go/config/config.go`:
  - `VaultConfig.Backend` の yaml タグを `yaml:"backend,omitempty"` に変更
  - `Backends []string` フィールドを追加 (`yaml:"backends"`)

### Step 2: ChainVaultBackend のテスト作成 (TDD)

- Create `shared/libs/go/vault/chain_backend_test.go`:
  - `stubVault` テストダブルを定義
  - 7つのテスト関数を作成（FirstSuccess, Fallback, AllFail, AllFail_ErrorFormat, Set_FirstOnly, Delete_FirstOnly, List_FirstOnly）

### Step 3: ChainVaultBackend の実装

- Create `shared/libs/go/vault/chain_backend.go`:
  - `ChainVaultBackend` 構造体と `namedBackend` 構造体を定義
  - `NewChainVaultBackend()` コンストラクタ
  - `Resolve()`, `Set()`, `Delete()`, `List()` メソッドの実装
  - コンパイル時チェック: `var _ VaultStore = (*ChainVaultBackend)(nil)`

### Step 4: vault パッケージのテスト実行

```bash
go test -v ./shared/libs/go/vault/...
```

### Step 5: resolveVault() バリデーションテスト作成 (TDD)

- Edit `server/server_test.go`:
  - 8つの新規テスト関数を作成
  - 既存テストのYAMLリテラルを `backends` 形式に更新（L61-62, L181-182）

### Step 6: resolveVault() の改修

- Edit `server/server.go`:
  - `resolveVault()` のシグネチャに `logger.Logger` と `error` を追加
  - 旧 `Backend` フィールド検出 → エラー（R5）
  - `Backends` 未設定/空 → エラー（R2）
  - 各バックエンド名バリデーション + 構築（R3）
  - 単一バックエンドなら直接返却、複数なら `ChainVaultBackend` 構築（R1, R4）
  - `New()` 内の呼び出し箇所を修正（`vs, err := resolveVault(o, cfg, log)`）

### Step 7: server パッケージのテスト実行

```bash
go test -v ./server/...
```

### Step 8: config テストの更新

- Edit `shared/libs/go/config/config_test.go`:
  - "full config", "minimal config", "file vault backend" テストの `Backend` → `Backends` 更新
  - 新テストケース "backends list order", "old backend field preserved" 追加
- Edit `shared/libs/go/config/loader_test.go`:
  - YAMLとアサーションを `backends` 形式に更新

### Step 9: E2Eテストの修正

- Edit `tests/gemini_e2e_test.go`:
  - 3箇所の `VaultConfig{Backend: "keyring"}` → `VaultConfig{Backends: []string{"keyring"}}` に更新

### Step 10: 既存テスト（server_test.go）の `VaultConfig` 参照修正

- `TestNew_WithConfigPath`（L61-62）と `TestNew_ConfigPathOverridesConfig`（L181-182）の YAML を `backends: [env]` に更新

### Step 11: 全体ビルドと検証

- Verification Plan の実行に進む

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests**:
    全体ビルドと全単体テスト実行:
    ```bash
    ./scripts/process/build.sh
    ```

2.  **Vault パッケージ単体テスト**:
    ChainVaultBackend を含むテスト:
    ```bash
    go test -v ./shared/libs/go/vault/...
    ```

3.  **Config パッケージ単体テスト**:
    VaultConfig 構造体のデシリアライズテスト:
    ```bash
    go test -v ./shared/libs/go/config/...
    ```

4.  **Server パッケージ単体テスト**:
    resolveVault() バリデーションテスト:
    ```bash
    go test -v ./server/...
    ```

5.  **Integration Tests**:
    サーバー起動と基本動作の回帰確認:
    ```bash
    ./scripts/process/integration_test.sh --specify "TestAgentService"
    ```

### E2E Tests

本変更は設定のバリデーションロジックとChainVaultBackendの追加が主な内容であり、新しいE2Eテストコードの追加は不要と判断する。理由:

- `ChainVaultBackend` の振る舞いは `chain_backend_test.go` の単体テストで網羅される
- `resolveVault()` のバリデーションは `server_test.go` の単体テストで網羅される
- 既存のE2Eテスト（`gemini_e2e_test.go`）の `VaultConfig` 修正により、既存の統合フローが引き続き動作することを確認できる
- 実際のOS Keyring/環境変数との統合はCI環境依存性が高く、単体テストのスタブで十分にカバーできる

### テスト設計セルフレビュー

**網羅性**: 仕様書R1-R6の全要件がテストケースにマッピングされている。ボトムアップ順序（vault/chain_backend → config → server → E2E）で設計。

**証拠の十分性**: 各エラーメッセージのキーフレーズ（"vault.backends is required", "unsupported vault backend", "no longer supported" 等）を `strings.Contains` で検証。

**迂回排除**: `WithVaultStore` オプションによるバイパスが既存テスト `TestNew_WithVaultStore` でカバー済み。

**依存関係**: `ChainVaultBackend` は `VaultStore` インターフェースのみに依存。`resolveVault()` は config と vault パッケージに依存。テスト順序は依存関係に従い bottom-up。

### 総合判定

全テスト完了後、以下を確認:
1. `build.sh` が exit 0 で終了
2. vault, config, server の全単体テストが PASS
3. 統合テスト `TestAgentService` が PASS
4. 新規コードに対するテストカバレッジがエラーパス・正常パスの両方を含む
