---
name: investigate
description: Investigates the codebase, analyzes bugs, traces dependencies, and produces a structured report without modifying any code. Use when the user asks to investigate a bug, understand the design, trace a call flow, analyze logs, or research how something works — read-only exploration only.
disable-model-invocation: true
---

# 調査ワークフロー (Investigation)

コードベースの調査・分析を行い、その結果をレポートとしてまとめる。**コードの修正や変更は一切行わない。**

## 制約事項

> [!CAUTION]
> - ソースコードの変更（追加・編集・削除）は**絶対に行わない**
> - `git commit`, `git push` などのリポジトリ変更操作は**禁止**
> - ビルドスクリプトや生成系コマンドの実行は**禁止**

**許可される操作**: ファイルの閲覧・検索（grep/ripgrep）、`git log/blame/diff/show`、既存バイナリのステータス確認系実行、ログ確認、`go doc`/`go vet`/`go list`、`tmp/` への中間ファイル出力

**禁止される操作**: `.go`/`.ts`/`.tsx`/`.json`/`.yaml` 等のソースコードや設定ファイルの作成・編集・削除、`prompts/` 配下の仕様書の作成・編集、ビルドコマンド、テストの実行

## 1. 調査の開始

依頼内容と調査の目的（バグ特定・設計理解・パフォーマンス分析など）を明確にする。不明点はユーザーに確認する。

## 2. 調査の実施

以下の手法を組み合わせて調査を進める:

1.  **コード解析**: ソースコードの閲覧、依存関係のトレース、ripgrep によるパターン検索、`git blame`/`git log` による変更履歴の追跡
2.  **ログ・出力の分析**: アプリケーションログ、エラーメッセージ、スタックトレースの分析
3.  **動的な現状把握**: 既存コマンドのバージョン・ステータス確認、環境変数・設定の確認
4.  **ドキュメント参照**: `prompts/phases/` 配下の仕様書、コード内コメント・GoDoc、README

## 3. レポートの作成

調査結果をチャットで報告する。レポートの構成:

1.  **調査概要**: 目的と調査対象スコープ
2.  **調査手法**: 実施した手法、使用したコマンドや検索パターン
3.  **調査結果**: 発見した事実（コードスニペット、ログ抜粋、ファイルパスリンク付き）
4.  **分析・考察**: 原因の推定や問題の構造の説明
5.  **推奨事項**（該当する場合）: 方向性・方針の提案のみ。実際のコード変更は行わない

## 4. フォローアップ

追加質問に対応する。修正が必要な場合は `/create-specification` → `/create-implementation-plan` → `/execute-implementation-plan` の開発フローを提案する。
