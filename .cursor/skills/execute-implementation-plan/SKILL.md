---
name: execute-implementation-plan
description: Executes a given implementation plan by writing code, tests, and committing changes step by step. Use when the user provides an implementation plan file and asks to implement it, start coding, or execute the plan.
disable-model-invocation: true
---

# 実装実行ワークフロー

実装計画書 (`.../plans/.../XXX.md`) に基づき、コーディングルールとテストルールを遵守して実装を行う。

## 1. 入力とルールの確認

1.  **入力ファイル**: ユーザー指定またはエディタで開いているファイルを「実装計画書」として扱う。
2.  **ルール読み込み**:
    *   `prompts/rules/coding-rules.md`
    *   `prompts/rules/testing-rules.md`
    *   `prompts/rules/logging-rules.md`

## 2. 実装の実行

1.  計画書を読み、変更対象ファイルと変更内容を把握する。複数ファイルに分割されている場合は全て確認する。
2.  チェックボックスを `[ ]` → `[/]`（進行中）→ `[x]`（完了）で更新する。
3.  `coding-rules.md` のスタイルと設計原則を厳守する。`logging-rules.md` のレベル基準に従い DEBUG ログを積極的に挿入する。
4.  各ステップ完了ごとに `git add` → `git commit` する（コミットルールは `instructions.mdc` 参照）。

## 2.5 E2Eテストの実装

Verification Plan に **E2E Tests** セクションがある場合、テスト実行の**前に** E2E テストコードを実装する。

> [!CAUTION]
> 手動コマンド実行による確認は E2E テストコードの代替にならない。必ずテストコードとして残すこと。

`tests/` 配下の既存ヘルパー関数を確認し、実装計画の E2E ケースをコードとして実装してからコミットする。

## 3. テストと検証

詳細ルールは `prompts/rules/testing-rules.md` を参照。

### 3.1 テスト実施の順序

1.  **Build & Unit Test（必須）**: `./scripts/process/build.sh`。失敗時は次のステップに進まない。
2.  **統合テスト**: `./scripts/process/integration_test.sh --categories "xxx"`。失敗時は testing-rules.md Section 3 の修正ループに従う。

### 3.2 修正と再テスト

> [!CAUTION]
> **NEVER IGNORE FAILURES**: ビルドやテストの失敗を無視してタスクを完了させることは禁止。「後で直す」は禁止。

## 4. Git Push

全てのビルドとテストが成功した後に `git push` する。失敗状態ではプッシュしない。
