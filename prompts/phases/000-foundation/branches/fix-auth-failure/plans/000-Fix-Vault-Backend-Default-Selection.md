# [DONE] Fix Vault Backend Default Selection & Support Chained Backends

Vault 設定の `backend` フィールドを `backends` (リスト形式) に変更し、複数のバックエンドを連鎖（Chain of Responsibility）して利用可能にする。

## 実装済み要件

- **R1: 複数バックエンドの連鎖サポート**: `VaultConfig` に `backends` (スライス) を追加し、`ChainVaultBackend` を実装。
- **R2: 最小1つのバックエンド必須化**: `backends` が未設定または空の場合、サーバー起動時にエラー。
- **R3: 各バックエンドの個別バリデーション**: `env`, `keyring`, `file` の各バックエンドの妥当性を構築時にチェック。
- **R4: オーバーヘッドの最小化**: 指定されたバックエンドが1つの場合は `ChainVaultBackend` を介さず直接利用。
- **R5: 非推奨フィールドの検出**: 旧 `backend` (単数形) フィールドが残っている場合はエラーメッセージで移行を促す。
- **R6: 後方互換性(コードレベル)**: 既存の `VaultStore` インターフェースを維持。

## 進捗管理

### 実装ステップ [x]

1. `VaultConfig` 構造体の更新 (backends スライスの追加) [x]
2. `ChainVaultBackend` の実装とユニットテスト作成 [x]
3. `server.resolveVault()` の刷新とバリデーション強化 [x]
4. 既存テストの修正（Config, Server, E2E, Integration）[x]
5. 全体ビルドと統合テストによる最終検証 [x]

## 検証結果 [x]

### 自動テスト

1. **全体ビルド & 単体テスト**: [x]
   `./scripts/process/build.sh` 成功。
2. **Vault パッケージ単体テスト**: [x]
   `go test -v ./shared/libs/go/vault/...` 成功（ChainVaultBackend の Fallback 動作等）。
3. **Server パッケージ単体テスト**: [x]
   `go test -v ./server/...` 成功（バリデーション、エラーメッセージの確認）。
4. **統合テスト**: [x]
   `./scripts/process/integration_test.sh` 成功。

### 修正したテストファイル
- `tests/agentservice_integration_test.go`: `Vault` 設定の追加と `HealthResponse` の構造変更への対応。
- `tests/wsserver_integration_test.go`: `Vault` 設定の追加。
- `tests/gemini_e2e_test.go`: `Vault` 設定の更新。

## 総合判定 [x]

本計画の全ての要件が実装され、ユニットテストおよび統合テストによって正常動作が確認された。非推奨フィールドに対する親切なエラーメッセージの導入により、ユーザーの移行体験も考慮されている。
