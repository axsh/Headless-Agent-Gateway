---
name: test-generator
description: Creates a test-focused implementation plan from a specification, defining test cases for each requirement. Use when the user wants to generate a test plan, design tests before implementation, or create a TestPlan document from a spec file.
disable-model-invocation: true
---

# テスト実装計画作成ワークフロー

アイデア/仕様書 (`.../ideas/.../XXX-{Name}.md`) を元に、**要件ごとの実現性を検証するためのテスト実装計画書** (`.../plans/.../YYY-{Name}-TestPlan.md`) を作成する。

`/create-implementation-plan` が機能実装の計画を立てるのに対し、このワークフローは「最低限の実装で要件が実現できたと言い切れるか」を検証するためのテストに特化した計画を立てる。

## 1. 入力とルールの確認

1.  **入力ファイル**: ユーザー指定またはエディタで開いているファイルを「仕様書」として扱う。
2.  **ルール読み込み**: `prompts/rules/testing-rules.md`
3.  **ステータス取得**: `scripts/utils/show_current_status.sh` を実行し、`phase`, `branch`, `next_plan_id` を取得する。

## 2. 出力先

*   **ディレクトリ**: `prompts/phases/[Phase]/branches/[Branch]/plans/`
*   **ファイル名**: `[NextID]-[名前]-TestPlan.md`

## 3〜8. 分析プロセス

以下の順序で進める:

3.  **仕様書の分析**: 背景・目的、アーキテクチャ、データフロー、外部依存、統合ポイントを把握する。
4.  **要件の抽出**: 全要件に `REQ-001` 等の識別子を付与。機能/非機能/統合に分類。暗黙的な要件（エラーハンドリング等）も抽出する。
5.  **実現根拠の列挙**: 各要件について「何がどう観測・計測できること」が根拠になるかを複数列挙する。
6.  **確認手段の列挙**: ログ確認・API応答確認・ファイル出力確認・プロセス確認・通信確認・データ確認・エラー確認の各視点から網羅的に列挙する。
7.  **確認手順の具体化**: 別の開発者がそのまま実行できるレベルの詳細な手順（前提条件/入力/操作手順/期待結果/判定基準）を記述する。
8.  **テストシナリオへの変換**: 以下の形式でまとめる:
    ```
    #### TC-XXX: [テストケース名]
    * 対応要件: REQ-XXX / テスト種別: 単体/統合/E2E
    * 配置先: [ファイルパス] / テスト関数名: TestXxx_Yyy
    * テストシナリオ: [Arrange] → [Act] → [Assert]
    ```

## 9. テンプレート

計画書には以下のセクションを含める:

1.  **要件一覧 (Extracted Requirements)**: REQ-ID / 要件 / 分類
2.  **要件別 実現根拠と検証設計**: 各要件について根拠・確認手段・確認手順・テストシナリオ
3.  **テスト実装サマリー**: テストケース一覧テーブル + 要件カバレッジマトリクス
4.  **Step-by-Step Implementation Guide**: チェックボックス付き実装手順
5.  **Verification Plan**: `build.sh` + `integration_test.sh --categories ... --specify ...`

## 10. セルフレビュー

1.  全要件が抽出されているか（暗黙的な要件含む）
2.  各要件に複数の根拠が列挙されているか
3.  確認手段が6の視点一覧を網羅しているか
4.  確認手順が「別の開発者がそのまま実行できるレベル」の詳細さか
5.  テスト種別の分類が適切か
6.  カバレッジマトリクスに空白がある場合、理由が妥当か
7.  チェックボックス付きの実装ガイドが含まれているか

## 11. 完了

ファイルを保存し `file://` 形式のリンクをユーザーに提示する。**フェーズ移行禁止**: ユーザーが明示的に指示するまで次のフェーズに進まない。
