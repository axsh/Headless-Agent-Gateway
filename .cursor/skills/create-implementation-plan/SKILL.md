---
name: create-implementation-plan
description: Creates a detailed implementation plan document from a specification file. Use when the user asks to create an implementation plan, wants to plan out how to implement a specification, or references a spec file under prompts/phases/ and asks to plan the implementation.
disable-model-invocation: true
---

# 実装計画作成ワークフロー

アイデア/仕様書 (`.../ideas/.../XXX-{Name}.md`) を元に、ルールに基づいた実装計画書 (`.../plans/.../YYY-{Name}.md`) を作成する。

## 1. 入力とルールの確認

1.  **入力ファイルの特定**:
    *   ユーザーが指定したファイル、または現在エディタで開いているファイルを「仕様書」として扱います。
2.  **ルールの読み込み**:
    *   `prompts/rules/planning-rules.md` を読み込みます。
3.  **ステータスの取得**:
    *   `scripts/utils/show_current_status.sh` を実行します。
    *   JSON出力から `phase`, `branch`, `next_plan_id` を取得します（以下 `[Phase]`, `[Branch]`, `[NextID]`）。

## 2. 出力先の決定

*   **出力ディレクトリ**: `prompts/phases/[Phase]/branches/[Branch]/plans/`（なければ作成）
*   **ファイル名**: `[NextID]-[名前].md`

## 3. 実装計画書の作成

> [!IMPORTANT]
> **Technical Inheritance Rule**: 仕様書に含まれるロジック、計算式、定数、アルゴリズム、コードスニペット、データ構造定義は要約せずそのまま記述すること。「仕様書の通り実装する」という記述は禁止。

以下のテンプレートを埋めてください:

```markdown
# [ファイル名 (拡張子なし)]

> **Source Specification**: [仕様書の相対パス]

## Goal Description
[機能や変更の概要を簡潔に記述]

## User Review Required
[ユーザーの確認が必要な事項。なければ "None."]

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| [Requirements text] | [e.g. "Proposed Changes > File A"] |

## Proposed Changes

[ファイル単位で変更点を記述。依存関係順（Interface -> Struct -> Logic）に並べること]

### [コンポーネント名]

#### [MODIFY/NEW] [ファイルパス](file://プロジェクトルートからの相対パス)
*   **Description**: [変更の概要]
*   **Technical Design**: [関数シグネチャやインターフェース定義の変更点]
*   **Logic**: [仕様書から継承したロジックを具体的に記述]

## Step-by-Step Implementation Guide

[時間軸に沿った具体的な作業手順リスト]

1.  **[Step Name]**: Edit `[File Path]` to [Specific Action].

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests**: `./scripts/process/build.sh`
2.  **Integration Tests**: `./scripts/process/integration_test.sh --specify "[Test Name]"`
3.  **E2E Tests**: [新機能の E2E テストコードを tests/ 配下に追加]

## Documentation

[影響を受ける既存ドキュメントの更新計画]
```

## 4. 一括作成ルール

複数 Part に分割する場合は**全 Part を一括作成**してからレビュー依頼すること。

## 5. セルフレビュー

1.  **要件対比**: `Requirement Traceability` テーブルが全要件を網羅しているか
2.  **再現性**: この計画書だけで迷わず実装できる具体性があるか
3.  **データ構造**: 構造体定義やデータモデルが省略されていないか
4.  **テスト網羅性**: TDDで計画されているか。単体・統合の区分けは適切か
5.  **統合テストプラン**: `--categories` と `--specify` を組み合わせたコマンドが明記されているか
6.  **E2Eコード化**: 手動確認だけで終わっていないか。`tests/` 配下にE2Eテストが計画されているか
