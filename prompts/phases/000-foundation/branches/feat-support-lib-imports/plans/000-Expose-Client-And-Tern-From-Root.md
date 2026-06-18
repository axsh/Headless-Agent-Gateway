# 000-Expose-Client-And-Tern-From-Root

> **Source Specification**: [000-Expose-Client-And-Tern-From-Root.md](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-support-lib-imports/prompts/phases/000-foundation/branches/feat-support-lib-imports/ideas/000-Expose-Client-And-Tern-From-Root.md)

## Goal Description

Ternプロジェクトの共有Goライブラリのうち、主要API（`client`, `tern`）をリポジトリルートに移動させ、`go.mod` もルートに移動・統合します。その他の共有パッケージは `shared/libs/go/` 内に据え置きます。これにより、外部プロジェクトから `github.com/axsh/arctic-tern/client` および `github.com/axsh/arctic-tern/tern` を容易にインポートできるようにします。

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: go.mod のルート移動と統合 | Proposed Changes > Build/Config / Step 1 |
| R2: 主要APIパッケージのルート移動 | Proposed Changes > Packages / Step 3 |
| R3: 内部パッケージ의 インポートパス更新 | Proposed Changes > Import Paths / Step 4 |
| R4: 各機能モジュール（features, examples, tests）の go.mod の更新 | Proposed Changes > Modules go.mod / Step 5 |
| R5: Gitタグの作成 | Step-by-Step Implementation Guide > Step 7 |
| R6: 外部からのimport検証 | Verification Plan > Manual Verification |

## Proposed Changes

### Build/Config

#### [MODIFY] [go.mod](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-support-lib-imports/go.mod)
*   **Description**: `shared/libs/go/go.mod` をリポジトリルートに移動し、ルートの `go.mod` とします。モジュール名は `github.com/axsh/arctic-tern` のままです。
*   **Technical Design**:
    *   ファイルの物理的な場所をリポジトリルートに変更します。
    *   `shared/libs/go/go.sum` も同様にリポジトリルートへ移動します。

#### [MODIFY] [build.sh](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-support-lib-imports/scripts/process/build.sh)
*   **Description**: `shared/libs/go/` にあった `go.mod` の位置をリポジトリルートに変更したため、ビルドスクリプトでのテスト・ビルド検証対象をリポジトリルートに変更します。
*   **Technical Design**:
    *   `if [[ -f "shared/libs/go/go.mod" ]]` を `if [[ -f "go.mod" ]]` に変更し、テスト対象のディレクトリを `$PROJECT_ROOT` に変更します。

### Modules go.mod

以下のファイルの `replace` ディレクティブにおいて、`github.com/axsh/arctic-tern` のローカル参照先を `../../` (またはルートへの相対パス) に変更します。

#### [MODIFY] [go.mod (features/tern)](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-support-lib-imports/features/tern/go.mod)
*   **Description**: replace ディレクティブの更新
*   ```go
    replace github.com/axsh/arctic-tern => ../../
    ```

#### [MODIFY] [go.mod (features/ternctl)](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-support-lib-imports/features/ternctl/go.mod)
*   **Description**: replace ディレクティブの更新
*   ```go
    replace github.com/axsh/arctic-tern => ../../
    ```

#### [MODIFY] [go.mod (features/vault-cli)](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-support-lib-imports/features/vault-cli/go.mod)
*   **Description**: replace ディレクティブの更新
*   ```go
    replace github.com/axsh/arctic-tern => ../../
    ```

#### [MODIFY] [go.mod (features/log-viewer)](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-support-lib-imports/features/log-viewer/go.mod)
*   **Description**: replace ディレクティブの更新
*   ```go
    replace github.com/axsh/arctic-tern => ../../
    ```

#### [MODIFY] [go.mod (examples/minimal-server)](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-support-lib-imports/examples/minimal-server/go.mod)
*   **Description**: replace ディレクティブの更新
*   ```go
    replace github.com/axsh/arctic-tern => ../../
    ```

#### [MODIFY] [go.mod (examples/minimal-client)](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-support-lib-imports/examples/minimal-client/go.mod)
*   **Description**: replace ディレクティブの更新
*   ```go
    replace github.com/axsh/arctic-tern => ../../
    ```

#### [MODIFY] [go.mod (tests)](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-support-lib-imports/tests/go.mod)
*   **Description**: replace ディレクティブの更新
*   ```go
    replace github.com/axsh/arctic-tern => ../
    ```

### Packages (Movements)

#### [NEW/DELETE] [client](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-support-lib-imports/client)
*   **Description**: `shared/libs/go/client/` 配下のすべてのGoソースファイルを `client/` に移動します。

#### [NEW/DELETE] [tern](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-support-lib-imports/tern)
*   **Description**: `shared/libs/go/tern/` 配下のすべてのGoソースファイルを `tern/` に移動します。

### Import Paths (一括更新)

リポジトリ内のすべての `.go` ソースファイルにおいて、`shared/libs/go` 配下に据え置かれたパッケージのインポートパスを更新します。
*   **更新対象**:
    *   `github.com/axsh/arctic-tern/agentservice` -> `github.com/axsh/arctic-tern/shared/libs/go/agentservice`
    *   `github.com/axsh/arctic-tern/codingagent` -> `github.com/axsh/arctic-tern/shared/libs/go/codingagent`
    *   `github.com/axsh/arctic-tern/config` -> `github.com/axsh/arctic-tern/shared/libs/go/config`
    *   `github.com/axsh/arctic-tern/llmgateway` -> `github.com/axsh/arctic-tern/shared/libs/go/llmgateway`
    *   `github.com/axsh/arctic-tern/logger` -> `github.com/axsh/arctic-tern/shared/libs/go/logger`
    *   `github.com/axsh/arctic-tern/tasklog` -> `github.com/axsh/arctic-tern/shared/libs/go/tasklog`
    *   `github.com/axsh/arctic-tern/vault` -> `github.com/axsh/arctic-tern/shared/libs/go/vault`
    *   `github.com/axsh/arctic-tern/wayfinder` -> `github.com/axsh/arctic-tern/shared/libs/go/wayfinder`
    *   `github.com/axsh/arctic-tern/wsserver` -> `github.com/axsh/arctic-tern/shared/libs/go/wsserver`

## Step-by-Step Implementation Guide

1.  **[Preparation] go.mod / go.sum のルート移動**:
    *   `shared/libs/go/go.mod` および `shared/libs/go/go.sum` をプロジェクトルートに移動（`git mv`）します。
    *   `git mv shared/libs/go/go.mod go.mod`
    *   `git mv shared/libs/go/go.sum go.sum`

2.  **[Config] build.sh の更新**:
    *   `scripts/process/build.sh` 内の `shared/libs/go/go.mod` を参照しているテスト・ビルドロジックを、ルートの `go.mod` を参照するように修正します。

3.  **[Move Packages] client と tern パッケージの移動**:
    *   `shared/libs/go/client` と `shared/libs/go/tern` をリポジトリルート直下に移動（`git mv`）します。
    *   `git mv shared/libs/go/client client`
    *   `git mv shared/libs/go/tern tern`

4.  **[Update Imports] インポートパスの一括更新**:
    *   リポジトリ内のすべての Go ソースファイル (`*.go`) を対象に、`github.com/axsh/arctic-tern/[パッケージ名]` のインポートパスを `github.com/axsh/arctic-tern/shared/libs/go/[パッケージ名]` に置換します。
    *   置換には、以下の `find` と `sed` コマンドを一括実行します。
    ```bash
    PKGS=("agentservice" "codingagent" "config" "llmgateway" "logger" "tasklog" "vault" "wayfinder" "wsserver")
    
    for pkg in "${PKGS[@]}"; do
      find . -name "*.go" -not -path "*/.git/*" | xargs sed -i "s|github.com/axsh/arctic-tern/${pkg}|github.com/axsh/arctic-tern/shared/libs/go/${pkg}|g"
    done
    ```
    *   置換実行後、`git diff` で意図しない箇所の書き換えがないかを確認し、`go fmt ./...` を実行してコードを整形します。

5.  **[Update Modules] 各機能モジュールの go.mod 更新**:
    *   `features/tern/go.mod`、`features/ternctl/go.mod`、`features/vault-cli/go.mod`、`features/log-viewer/go.mod`、`examples/minimal-server/go.mod`、`examples/minimal-client/go.mod`、`tests/go.mod` の `replace` ディレクティブのパスを `../../` (またはルートへの相対パス) に変更します。

6.  **[Verification] ビルドとテストの実行**:
    *   ルートの単体テスト、各機能モジュールのビルド、および統合テストがすべて正常に通過することを確認します。
    *   詳細は Verification Plan に従います。

7.  **[Release] Gitタグの作成**:
    *   作業が完了し、テストがすべて通過したのを確認後、ローカルでタグ `v0.1.0` を作成します。
    *   `git tag v0.1.0`

## Verification Plan

### Automated Verification

#### 1. Build & Unit Tests
ルートモジュールおよび各モジュールのビルドと単体テストを実行します。
```bash
./scripts/process/build.sh
```

#### 2. Integration Tests
統合テストを実行します。
```bash
./scripts/process/integration_test.sh
```

#### 3. E2E Tests (不要の理由)
本変更は、共有Goライブラリのモジュール構成およびインポートパスの再配置のみを行う純粋なリファクタリングです。外部から観測可能な機能やAPIの挙動に変更がないため、新しいE2Eテストコードの追加は不要です。既存のE2Eテスト（統合テストスイートに含まれるもの）がすべて通過することをもって検証とします。

---

### Manual Verification

外部プロジェクトから正常にインポートできることを検証するため、ローカルで一時的なGoプロジェクトを作成して動作を確認します。

1.  プロジェクト外の適当な一時ディレクトリで、新しいGoモジュールを初期化します。
    ```bash
    mkdir tmp-go-test
    cd tmp-go-test
    go mod init tmp-go-test
    ```
2.  `main.go` を作成し、Ternの `client` と `tern` パッケージをインポートして呼び出すダミーコードを記述します。
    ```go
    package main

    import (
        "context"
        "fmt"
        "github.com/axsh/arctic-tern/client"
        "github.com/axsh/arctic-tern/tern"
    )

    func main() {
        fmt.Println("Tern Client: ", client.NewClient)
        fmt.Println("Tern Server: ", tern.NewServer)
    }
    ```
3.  `go.mod` に `replace` を追加し、ローカルで変更を加えた `arctic-tern` のワークスペースを指すようにします。
    ```go
    replace github.com/axsh/arctic-tern => /path/to/arctic-tern
    ```
4.  `go run main.go` または `go build` を実行し、正常にコンパイルおよび実行できることを確認します。

---

### 11.4 テスト項目のセルフレビュー
*   **網羅性の検証**: ルート移動とインポートパス変更により、全モジュールのコンパイルが正常に行えるか、および既存のテストがすべて通過するかを検証することで、既存機能が破壊されていないこと（リグレッションがないこと）を網羅的に確認できます。
*   **証拠の十分性**: `scripts/process/build.sh` および `integration_test.sh` による全テストケースのパス、および一時的な外部モジュールからのインポート検証の成功をもって十分な証拠とします。
*   **迂回・抜け道の排除**: ローカルでのモジュールインポート検証において、`replace` を通じてローカルの変更後モジュールを強制的にロードしてビルドすることにより、実際に変更後の `client`/`tern` パッケージがインポートできていることを保証します。
*   **依存関係の整合性**: 共有モジュールのビルド・テストが最初に成功した後に、他の機能モジュールのビルド・テストを順次検証するボトムアップ順序（`build.sh` 内で制御）に整合しています。

---

### 12. 全テスト完了後の総合判定プロセス

全テスト完了後、以下の総合判定チェックリストを実行します。結果は変更完了時の walkthrough.md に記録します。

| # | チェック項目 | 確認内容 |
|---|------------|----------|
| 1 | **スキップされたテストの有無** | テストログに `SKIP`, `WARN`, `TODO` 等のマーカーが出ていないか。 |
| 2 | **部分的なエラーの見落とし** | 成功判定に影響しない箇所でエラーが発生していないか。 |
| 3 | **迂回処理による偽成功** | 本来のパスが失敗していないか。 |
| 4 | **...** | ... |
