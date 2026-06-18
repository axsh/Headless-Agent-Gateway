# リポジトリルートから主要API（client, server）をインポート可能にする仕様書

## 背景

Ternプロジェクトの共有Goライブラリは `shared/libs/go/` 配下に配置されており、モジュールパスは `github.com/axsh/arctic-tern` と定義されています。しかし、`go.mod` がリポジトリのサブディレクトリにあるため、外部のGoプロジェクトから `go get github.com/axsh/arctic-tern` をしても正常に import できません。

本設計では、外部から `github.com/axsh/arctic-tern/client` および `github.com/axsh/arctic-tern/server` をシンプルかつ直感的にインポートできるようにするため、以下の構成変更を行います。

- リポジトリルートに `go.mod` を移動・統合します。
- 外部に公開する主要APIパッケージ（`client/`, `server/`）をルート直下に配置します。
  - 元の `tern` パッケージは、機能の直感性を高めるために `server` パッケージにリネームします。
- その他の共有パッケージ（`logger/`, `config/` 等）は `shared/libs/go/` 内に据え置きます。

## 要件

### 必須要件

1. **R1: go.mod のルート移動と統合**
   - `shared/libs/go/go.mod`（および `go.sum`）をリポジトリルートに移動し、ルートのモジュール `github.com/axsh/arctic-tern` とします。
   - 元の `shared/libs/go/go.mod` および `go.sum` は削除します。

2. **R2: 主要APIパッケージのルート移動とリネーム**
   - 以下のパッケージを `shared/libs/go/` からリポジトリルート直下に移動します。
     - `shared/libs/go/client/` -> `client/`
     - `shared/libs/go/tern/` -> `server/` (※ パッケージ名を `tern` から `server` にリネーム)

3. **R3: 内部パッケージのインポートパスとシンボル参照の更新**
   - `shared/libs/go/` 配下に残るパッケージ（`logger`, `config` 等）のインポートパスが `github.com/axsh/arctic-tern/shared/libs/go/logger` などに変更されます。
   - `tern` パッケージが `server` パッケージに変更されます。
   - これに伴い、リポジトリ内のすべての Go ソースファイルにおいて、インポートパスと、パッケージ名変更に伴うシンボル参照（例: `tern.New` -> `server.New`、`tern.Server` -> `server.Server`）を更新します。
     - 例:
       - `github.com/axsh/arctic-tern/client` -> 変更なし（ルート直下）
       - `github.com/axsh/arctic-tern/tern` -> `github.com/axsh/arctic-tern/server`
       - `github.com/axsh/arctic-tern/logger` -> `github.com/axsh/arctic-tern/shared/libs/go/logger`

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
   - 外部のGoプロジェクト（一時的なテストプロジェクト）から以下の import が動作し、直感的に利用できることを確認します。
     ```go
     import "github.com/axsh/arctic-tern/client"
     import "github.com/axsh/arctic-tern/server"
     ```

## 实现方針

- パッケージ移動: `git mv` コマンドを使用して、履歴を保持したままパッケージを移動およびリネームします。
- インポートパスの一括更新: 置換ツールを用いて、一括でインポートパスを置換します。
- パッケージ名・シンボル参照の置換: `tern` から `server` へのリネームに伴い、呼び出しコード中の `tern.New`、`tern.Server`、`tern.Option` などを `server.New`、`server.Server`、`server.Option` に置換します。

## 検証計画

### 自動テスト
- 各モジュールでの `go build` および `go test` を実行し、既存のテストがすべて通過することを確認します。
- ビルドパイプラインスクリプト（`scripts/process/build.sh` 等）を実行して全体の整合性を確認します。

### 手動検証
- 外部のGoプロジェクト（ローカルに作成する一時的なGoモジュール）から、`github.com/axsh/arctic-tern/client` および `github.com/axsh/arctic-tern/server` をインポートし、ダミーのコードがビルドできることを確認します（ローカルのリポジトリパスを `replace` して検証）。
