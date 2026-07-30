---
name: review-point
description: Pauses workflow execution and waits for explicit user approval before proceeding to the next phase. Use after completing a specification, implementation plan, or implementation to present the artifact for review and hold until the user explicitly approves moving forward.
disable-model-invocation: true
---

# Review Point (ワークフロー一時停止)

各フェーズ（仕様策定、実装計画、実装実行）の間に挟み、意図しない自動進行を防ぎ、ユーザーによるレビューと確認時間を確保する。

## 実行手順

1. **現状の確認**: 直前のワークフローで生成・更新された成果物を確認する。ユーザーからの質問や修正依頼があれば対応する。

2. **待機状態の維持**: ユーザーから「次のフェーズへ進む」といった明示的な指示があるまで、**絶対に次のワークフローを自動的に開始しない**。

3. **次のステップの案内**: 成果物が OK であれば次に実行すべきワークフローを提示する。
   - 仕様書完成後: 「宜しければ `/create-implementation-plan` を実行して実装計画を作成します。」
   - 実装計画完成後: 「宜しければ `/execute-implementation-plan` を実行して実装を開始します。」

## 禁止事項

ユーザーの明示的な許可なしに以下を自動開始することは禁止:
- `/create-specification`
- `/create-implementation-plan`
- `/execute-implementation-plan`
- `/build-pipeline`
