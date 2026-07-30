---
name: create-specification
description: Creates a structured specification document from the user's idea or description. Use when the user wants to write a spec, document a feature idea, define requirements, or start the development workflow from scratch.
disable-model-invocation: true
---

# 仕様書作成ワークフロー

ユーザーが述べた内容を元に、構造化された仕様書 (`.../ideas/.../XXX-{Name}.md`) を作成する。

## 1. 準備: ステータスの確認

`scripts/utils/show_current_status.sh` を実行し、JSON出力から `phase`, `branch`, `next_idea_id` を取得する（以下 `[Phase]`, `[Branch]`, `[NextID]`）。

## 2. 出力先の決定

*   **ディレクトリ**: `prompts/phases/[Phase]/branches/[Branch]/ideas/`（なければ作成）
*   **ファイル名**: `[NextID]-[名前].md`

## 3. 仕様書の内容構成

最低限以下の項目を含める:

1.  **背景 (Background)**: なぜこの機能が必要か。現在の課題。
2.  **要件 (Requirements)**: 実現すべき機能、制約。必須要件と任意要件を明確に区別する。
3.  **実現方針 (Implementation Approach)**: 技術・アーキテクチャ。設計上の重要な決定事項。
4.  **検証シナリオ (Verification Scenarios)**: ユーザーが具体的な手順を提示した場合は**要約せず**そのまま転記する。
5.  **テスト項目 (Testing)**: 手動確認だけの計画は禁止。`integration_test.sh` の実行コマンド（`--categories`/`--specify` 付き）を明記する。利用可能なカテゴリ: `common`, `llm`, `taskengine`, `template`, `gui`

## 4. 作成と保存

*   ユーザーの内容を注意深く確認し、必要に応じて質問する。
*   マークダウン形式（見出し、リスト、テーブル）で構造化する。Mermaid 図も推奨。
*   決定したファイル名で指定ディレクトリに保存する。

## 5. 完了確認

1.  背景・要件・実現方針の3観点をカバーしているか確認する。
2.  作成ファイルへのリンクをユーザーに提示する（`file://` 形式）。
3.  **フェーズ移行禁止**: ユーザーが明示的に指示するまで次のフェーズに進まない。
