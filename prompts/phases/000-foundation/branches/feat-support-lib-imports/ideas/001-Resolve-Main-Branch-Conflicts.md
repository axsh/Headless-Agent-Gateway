# 仕様書: mainブランチとのコンフリクト解消

## 1. 背景 (Background)
`feat-support-lib-imports` ブランチは、プロジェクト再構成に伴い内部ライブラリのインポートパスを `github.com/axsh/arctic-tern/shared/libs/go/...` 等の移行先に一括で書き換えるために作成されました。
しかし、このブランチの分岐後、`main` ブランチには複数の大規模な機能追加（WBS/サブエージェント実行、Compaction、ストリーミング、問合せ機能など）と、それに伴う構造体やテストコードの変更が多数マージされました。
その結果、`feat-support-lib-imports` ブランチに `main` の最新状態を取り込もうとすると、多数の主要ファイルで競合が発生し、マージできない状態になっています。

本仕様では、`main` ブランチの最新機能とロジックを完全に維持した上で、コンフリクトを解消し、`feat-support-lib-imports` ブランチのインポートパス移行を正しく適用することを目的とします。

---

## 2. 要件 (Requirements)
1. **競合の解消**:
   `main` ブランチと `feat-support-lib-imports` ブランチの間で発生しているすべての競合（コンフリクト）を解消すること。
2. **最新ロジックの維持**:
   `main` ブランチにマージされた直近の全変更（PR #3〜PR #6におけるWBS、問合せ機能、ストリーミング、Compaction、およびテスト安定化のための変更等）を完全に維持すること。
3. **インポートパス移行の適用**:
   `main` ブランチ側の最新コードに含まれるインポートパスや参照についても、`feat-support-lib-imports` の設計方針に従い、正しく新しいインポートパス（`github.com/axsh/arctic-tern/shared/libs/go/...` や `server`）へと更新すること。
4. **ビルドおよびテストの通過**:
   コンフリクト解消後、プロジェクト全体のビルドが通り、すべての単体テストおよび統合テストが成功すること。

---

## 3. 実現方針 (Implementation Approach)
1. **`main` のマージ**:
   `feat-support-lib-imports` ブランチにて、`git merge main` を実行して競合を発生させます。
2. **競合解消ルール**:
   競合箇所の解消は、`main` ブランチ側のコードをベースとし、インポートパスおよび関連するパッケージ名参照（例: `tern.New` -> `server.New`）のみを `feat-support-lib-imports` 側のルールで書き換える形でマージします。
   特に以下の構造体やテストで競合が多いため、個別に対応します：
   - `AgentCore` 構造体: `main` で追加されたすべての新規フィールドとメソッドをマージし、インポート文のみ移行先に更新します。
   - E2Eテストファイル: `main` で追加された最新のエラーチェックやテストケースをすべて維持し、インポートパスと初期化処理を `server` パッケージのものへ書き換えます。
3. **ビルド検証**:
   `scripts/process/build.sh` を実行して、コンパイルエラーおよび単体テストのエラーを修正します。
4. **統合テスト検証**:
   `scripts/process/integration_test.sh` を実行して、E2Eテストや統合テストが正常に動作することを確認します。

---

## 4. 検証シナリオ (Verification Scenarios)
1. ローカルの `feat-support-lib-imports` ブランチにおいて、`git merge main` が正常に完了し、コンフリクトが解消されていること。
2. `git diff` を確認し、`main` 側のロジックが失われておらず、インポートパスが正しく更新されていること。
3. ビルドスクリプト `scripts/process/build.sh` がエラーなしで終了すること。
4. 統合テスト `scripts/process/integration_test.sh` がエラーなしで終了すること。
5. 変更内容をコミットし、PR #7 へ正常にプッシュできること。

---

## 5. テスト項目 (Testing for the Requirements)
競合解消とインポートパス変更の検証のため、以下の自動テストコマンドを実行します。

### 全体ビルドおよび単体テスト
```bash
./scripts/process/build.sh
```

### 統合テスト / E2Eテストの実行
```bash
# ※Linux環境の場合は xvfb-run -a でラップして実行
./scripts/process/integration_test.sh
```
※特に `tests/agentservice_e2e_test.go` および `tests/codex_e2e_test.go` で競合が発生しているため、これらのテストが正常に実行されることを確認します。
