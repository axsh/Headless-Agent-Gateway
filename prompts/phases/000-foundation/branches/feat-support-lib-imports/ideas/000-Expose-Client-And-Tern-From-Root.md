# リポジトリルートから主要API（client, tern）をインポート可能にする仕様書

## 背景

Ternプロジェクトの共有Goライブラリは `shared/libs/go/` 配下に配置されており、モジュールパスは `github.com/axsh/arctic-tern` と定義されています。しかし、`go.mod` がリポジトリのサブディレクトリにあるため、外部のGoプロジェクトから `go get github.com/axsh/arctic-tern` をしても正常に import できません。

本設計では、外部から `github.com/axsh/arctic-tern/client` および `github.com/axsh/arctic-tern/tern` をシンプルにインポートできるようにするため、以下の構成変更を行います。

- リポジトリルートに `go.mod` を移動・統合します。
- 外部に公開する主要APIパッケージ（`client/`, `tern/`）をルート直下に移動します。
- その他の共有パッケージ（`logger/`, `config/` 等）は `shared/libs/go/` 内に据え置きます。

## 要件

### 必須要件

1. **R1: go.mod のルート移動と統合**
   - `shared/libs/go/go.mod`（および `go.sum`）をリポジトリルートに移動し、ルートのモジュール `github.com/axsh/arctic-tern` とします。
   - 元の `shared/libs/go/go.mod` および `go.sum` は削除します。

2. **R2: 主要APIパッケージのルート移動**
   - 以下のパッケージを `shared/libs/go/` からリポジトリルート直下に移動します。
     - `shared/libs/go/client/` -> `client/`
     - `shared/libs/go/tern/` -> `tern/`

3. **R3: 内部パッケージのインポートパス更新**
   - `shared/libs/go/` 配下に残るパッケージ（`logger`, `config` 等）のインポートパスが `github.com/axsh/arctic-tern/shared/libs/go/logger` などに変更されます。
   - これに伴い、リポジトリ内のすべての `.go` ソースファイルにおいて、`github.com/axsh/arctic-tern/[パッケージ名]` のインポート文を、対応する新しいインポートパス（ルート直下か `shared/libs/go/...` か）に更新します。
     - 例:
       - `github.com/axsh/arctic-tern/client` -> 変更なし（ルート直下）
       - `github.com/axsh/arctic-tern/tern` -> 変更なし（ルート直下）
       - `github.com/axsh/arctic-tern/logger` -> `github.com/axsh/arctic-tern/shared/libs/go/logger`
       - `github.com/axsh/arctic-tern/config` -> `github.com/axsh/arctic-tern/shared/libs/go/config`

4. **R4: 各機能モジュール（features, examples, tests）の go.mod の更新**
   - 以下のモジュールの `go.mod` 内の `replace` ディレクティブを更新します。
     - 変更前: `replace github.com/axsh/arctic-tern => ../../shared/libs/go`
     - 変更後: `replace github.com/axsh/arctic-tern => ../../` （または対応するルートへの相対パス）
   - 対象モジュール:
     - `features/tern/go.mod`
     - `features/ternctl/go.mod`
     - `features/vault-cli/go.mod`
     - `features/log-viewer/go.mod`
     - `examples/minimal-server/go.mod`
     - `examples/minimal-client/go.mod`
     - `tests/go.mod`

5. **R5: Gitタグの作成**
   - 作業完了後、リリース用に `v0.1.0` などのタグを作成します。

6. **R6: 外部からのimport検証**
   - 外部のGoプロジェクト（一時的なテストプロジェクト）から以下の import が動作することを確認します。
     ```go
     import "github.com/axsh/arctic-tern/client"
     import "github.com/axsh/arctic-tern/tern"
     ```

## 実現方針

- パッケージ移動: `git mv` コマンドを使用して、履歴を保持したままパッケージを移動します。
- インポートパスの一括更新: 置換ツール（`find` と `sed` など）を用いて、一括でインポートパスを置換します。置換対象のパッケージ一覧は以下の通りです。
  - `agentservice` -> `shared/libs/go/agentservice`
  - `codingagent` -> `shared/libs/go/codingagent`
  - `config` -> `shared/libs/go/config`
  - `llmgateway` -> `shared/libs/go/llmgateway`
  - `logger` -> `shared/libs/go/logger`
  - `tasklog` -> `shared/libs/go/tasklog`
  - `vault` -> `shared/libs/go/vault`
  - `wayfinder` -> `shared/libs/go/wayfinder`
  - `wsserver` -> `shared/libs/go/wsserver`
  ※ `client` および `tern` はルート直下に移動するため、インポートパスは変更ありません。

## 検証計画

### 自動テスト
- 各モジュールでの `go build` および `go test` を実行し、既存のテストがすべて通過することを確認します。
- ビルドパイプラインスクリプト（`scripts/process/build.sh` 等）を実行して全体の整合性を確認します。

### 手動検証
- 外部のGoプロジェクト（ローカルに作成する一時的なGoモジュール）から、`github.com/axsh/arctic-tern/client` および `github.com/axsh/arctic-tern/tern` をインポートし、ダミーのコードがビルドできることを確認します（ローカルのリポジトリパスを `replace` して検証）。
