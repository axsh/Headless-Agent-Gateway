# 001-Expose-Client-And-Server-From-Root

> **Source Specification**: [000-Expose-Client-And-Server-From-Root.md](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-support-lib-imports/prompts/phases/000-foundation/branches/feat-support-lib-imports/ideas/000-Expose-Client-And-Server-From-Root.md)

## Goal Description

主要パッケージのインポートパスとパッケージ名を `tern` から `server` に変更します。これにより、外部から `github.com/axsh/arctic-tern/server` として直感的にインポートでき、`server.New` や `server.Server` のように利用できるようにします。

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: go.mod のルート移動と統合 | (R1自体は前回実施済み) |
| R2: 主要APIパッケージのルート移動とリネーム | Proposed Changes > Packages / Step 1 |
| R3: 内部パッケージのインポートパスとシンボル参照の更新 | Proposed Changes > Source Code / Step 2, 3, 4 |
| R4: 各機能モジュール（features, examples, tests）の go.mod の更新 | (R4自体は前回実施済み、追加調整なし) |
| R5: Gitタグの作成 | Step-by-Step Implementation Guide > Step 7 |
| R6: 外部からのimport検証 | Verification Plan > Manual Verification |

## Proposed Changes

### Packages (Movements)

#### [NEW/DELETE] [server](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-support-lib-imports/server)
*   **Description**: `tern/` 配下のすべてのファイルを `server/` に移動します。（`git mv tern server` を使用）

### Source Code

#### [MODIFY] [server/*.go](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-support-lib-imports/server/)
*   **Description**: パッケージ名を `tern` から `server` にリネームします。
*   **Technical Design**:
    *   `package tern` -> `package server` に修正します。
    *   先頭コメントの `// Package tern` -> `// Package server` に修正します。

#### [MODIFY] [README.md](file:///c:/Users/yamya/myprog/arctic-tern/work/feat-support-lib-imports/README.md)
*   **Description**: インポート例と使用例の `tern` を `server` に更新します。

### Import Paths & Symbol References (一括更新)

リポジトリ内のすべての Go ソースファイルにおいて、`tern` から `server` へのインポートパスおよびパッケージ参照（シンボル呼び出し）を置換します。
*   **更新対象**:
    *   `github.com/axsh/arctic-tern/tern` -> `github.com/axsh/arctic-tern/server`
    *   `tern.Server` -> `server.Server`
    *   `tern.New` -> `server.New`
    *   `tern.Option` -> `server.Option`
    *   `tern.WithConfig` -> `server.WithConfig` など、すべての `tern.xxx` (パッケージ境界 `\btern\.` 考慮) -> `server.xxx`

## Step-by-Step Implementation Guide

1.  **[Git MV] tern の移動**:
    *   `git mv tern server` を実行します。

2.  **[Config] server/*.go のパッケージ名更新**:
    *   `server/` 配下の Go ソースファイル内の `package tern` を `package server` に変更します。
    *   `server/server.go` の先頭コメントなども更新します。

3.  **[Update References] インポートパスとシンボルの置換**:
    *   インポートパスと `tern.xxx` というプレフィックスを持つパッケージ参照を一括置換します。
    *   以下の `find` と `sed` / `perl` コマンドを使用します。
    ```bash
    # インポートパスの置換
    find . -name "*.go" -not -path "*/.git/*" -type f -print0 | xargs -0 sed -i 's|github.com/axsh/arctic-tern/tern|github.com/axsh/arctic-tern/server|g'
    
    # シンボル参照 (tern.Server, tern.New 等) の置換
    find . -name "*.go" -not -path "*/.git/*" -type f -print0 | xargs -0 perl -pi -e 's/\btern\.(New|Server|Option|WithConfig|WithConfigPath|WithGateway|WithLogger|WithVaultStore|WithKeyringVault|WithAgentServiceConfig|WithLLMServiceConfig|ReloadModelProfiles|TaskLog|WebSocketURL|resolveConfig|resolveLogger|resolveVault|resolveGateway|resolveAgentService)/server.$1/g'
    ```

4.  **[Format] go fmt の実行**:
    *   一括置換完了後、`go fmt ./...` を実行してコードを整形します。

5.  **[Docs] README.md の更新**:
    *   `README.md` 内の使用例 (`tern.New` やインポート文) を `server.New` や `server` に更新します。

6.  **[Verification] ビルドとテストの実行**:
    *   ルートの単体テスト、各機能モジュールのビルド、および統合テストがすべて正常に通過することを確認します。
    *   詳細は Verification Plan に従います。

7.  **[Release] 古いGitタグの削除と新しいGitタグの再作成**:
    *   既存の `v0.1.0` タグを削除し、新規のコードベースに対して `v0.1.0` を再度作成します。
    *   `git tag -d v0.1.0`
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
本変更は、共有Goライブラリのパッケージ名およびインポートパスのリネームを行う純粋なリファクタリングです。外部から観測可能な機能やAPIの挙動に変更がないため、新しいE2Eテストコードの追加は不要です。既存のE2Eテストがすべて通過することをもって検証とします。

---

### Manual Verification

外部プロジェクトから正常にインポートできることを検証するため、ローカルで一時的なGoプロジェクトを作成して動作を確認します。

1.  プロジェクト外の適当な一時ディレクトリで、新しいGoモジュールを初期化します。
    ```bash
    mkdir tmp-go-test
    cd tmp-go-test
    go mod init tmp-go-test
    ```
2.  `main.go` を作成し、Ternの `client` と `server` パッケージをインポートして呼び出すダミーコードを記述します。
    ```go
    package main

    import (
        "fmt"
        "github.com/axsh/arctic-tern/client"
        "github.com/axsh/arctic-tern/server"
    )

    func main() {
        var _ client.Client
        var _ server.Server
        fmt.Println("Success")
    }
    ```
3.  `go.mod` に `replace` を追加し、ローカルで変更を加えた `arctic-tern` のワークスペースを指すようにします。
    ```go
    replace github.com/axsh/arctic-tern => /path/to/arctic-tern
    ```
4.  `go mod tidy && go run main.go` を実行し、正常にコンパイルおよび実行できることを確認します。

---

### 11.4 テスト項目のセルフレビュー
*   **網羅性の検証**: `tern` から `server` へのリネームにより、すべての Go モジュールおよびテストコードにおけるシンボル参照が正しく更新され、コンパイル・テストがパスすることを確認することで、破壊がないことを網羅的に検証します。
*   **証拠 of 十分性**: `build.sh` および `integration_test.sh` による全テストケースのパス、および一時的な外部モジュールからのインポート検証の成功をもって十分な証拠とします。
*   **依存関係の整合性**: パッケージ単体のテストから統合テストへの順序に整合しています。

---

### 12. 全テスト完了後の総合判定プロセス

全テスト完了後、 walkthough.md にて総合判定結果を記録します。
