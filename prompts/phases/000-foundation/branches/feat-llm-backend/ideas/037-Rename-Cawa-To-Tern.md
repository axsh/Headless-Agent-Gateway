# 037: cawa-server/cawa-client のリネームと features への移動

## 背景 (Background)

現在 `examples/` ディレクトリに配置されている `cawa-server` と `cawa-client` は、プロジェクトの主要な成果物であり「サンプル」ではない。名称も "cawa" (Coding Agent Web API) から "tern" (Arctic Tern) に統一すべきである。

- `cawa-server` -> `tern` (サーバー本体)
- `cawa-client` -> `ternctl` (CLIクライアント / 管理ツール)

また、配置先を `examples/` から `features/` に移動し、プロジェクトのコアコンポーネントであることを明確にする。

## 要件 (Requirements)

### 必須要件

1. **ディレクトリ移動とリネーム**:
   - `examples/cawa-server/` -> `features/tern/`
   - `examples/cawa-client/` -> `features/ternctl/`
   - `examples/vault-cli/` -> `features/vault-cli/` (名前変更なし)
   - `examples/log-viewer/` -> `features/log-viewer/` (名前変更なし)

2. **バイナリ名の変更**:
   - `bin/cawa-server` -> `bin/tern`
   - `bin/cawa-client` -> `bin/ternctl`
   - `bin/vault-cli` と `bin/log-viewer` は名前変更なし

3. **内部のコード変更**:
   - Go ソースコード内のパッケージ名、コメント、文字列リテラルでの `cawa-server` / `cawa-client` の参照を更新
   - `go.mod` の module パスを更新
   - `Dockerfile`、`docker-compose.yaml` 内の参照を更新

4. **ビルドスクリプトの更新**:
   - `scripts/process/build.sh` は `examples/*/` を走査してビルドしている。`features/*/` も走査対象に追加するか、専用の処理を追加する。

5. **テストコードの更新**:
   - `tests/agentservice_e2e_test.go`: `model_profiles.yaml` のパス参照
   - `tests/codex_e2e_test.go`: 同上
   - `tests/gemini_e2e_test.go`: 同上
   - `tests/agentservice_integration_test.go`: `cawa-server` パス参照
   - `tests/examples_build_test.go`: examples ビルドテスト

6. **その他の参照更新**:
   - `examples/minimal-client/main.go`: `cawa-server` への言及
   - `scripts/test/container_test.sh`: テストスクリプト

### 任意要件

- `examples/` に残る `minimal-server`, `minimal-client` は現状のまま維持する（サンプルコードとしての位置付け）
- 仕様書(prompts/)内の過去の参照は修正しない（歴史的記録として残す）

## 実現方針 (Implementation Approach)

### 移動後のディレクトリ構造

```
features/
  tern/           # (旧 examples/cawa-server)
    cmd/
    config.yaml
    model_profiles.yaml
    Dockerfile
    docker-compose.yaml
    go.mod
    main.go
  ternctl/         # (旧 examples/cawa-client)
    go.mod
    main.go
  vault-cli/       # (旧 examples/vault-cli)
    go.mod
    main.go
  log-viewer/      # (旧 examples/log-viewer)
    go.mod
    main.go
examples/
  minimal-server/  # 残す (サンプルコード)
  minimal-client/  # 残す (サンプルコード)
```

### ビルドスクリプトの対応

`scripts/process/build.sh` の examples ループに加えて、`features/*/` ディレクトリも走査するようにする。

### go.mod の更新

```
# features/tern/go.mod
module github.com/axsh/arctic-tern/examples/cawa-server
  -> module github.com/axsh/arctic-tern/features/tern

# features/ternctl/go.mod
module github.com/axsh/arctic-tern/examples/cawa-client
  -> module github.com/axsh/arctic-tern/features/ternctl

# features/vault-cli/go.mod
module github.com/axsh/arctic-tern/examples/vault-cli
  -> module github.com/axsh/arctic-tern/features/vault-cli

# features/log-viewer/go.mod
module github.com/axsh/arctic-tern/log-viewer
  -> module github.com/axsh/arctic-tern/features/log-viewer
```

## 検証シナリオ (Verification Scenarios)

1. `./scripts/process/build.sh` でビルドが通り、`bin/tern` と `bin/ternctl` が生成されることを確認
2. `bin/tern --config features/tern/config.yaml` でサーバーが起動することを確認
3. `bin/ternctl health` でヘルスチェックが成功することを確認
4. 全 E2E テストが通ることを確認

## テスト項目 (Testing for the Requirements)

```bash
# ビルドと単体テスト
./scripts/process/build.sh

# 統合テスト
./scripts/process/integration_test.sh --specify "TestE2E_"
```
