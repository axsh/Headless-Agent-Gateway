# 019-Fix-E2E-ModelSelection

> **Source Specification**: [013-Fix-E2E-ModelSelection.md](file:///prompts/phases/000-foundation/branches/feat-llm-backend/ideas/013-Fix-E2E-ModelSelection.md)

## Goal Description

E2E テスト (`TestE2E_CodingAgentStreaming`, `TestE2E_CodingAgentError`) がモデル未指定のためCLI のデフォルトモデル (`claude-opus-4-8[1m]`) にフォールバックし、アクセス権エラーで exit code 1 になる問題を修正する。

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: E2E テストでモデルを明示的に指定する | Proposed Changes > agentservice_e2e_test.go |
| R2: model_profiles.yaml のモデルリスト現行化 | Proposed Changes > model_profiles.yaml |

## Proposed Changes

### E2E テスト

---

#### [MODIFY] [agentservice_e2e_test.go](file:///tests/agentservice_e2e_test.go)
*   **Description**: テスト用モデル定数を追加し、`createE2ESession()` でモデルを指定する
*   **Technical Design**:
    *   テストファイル先頭 (import の後) にモデル定数を追加:
    ```go
    // e2eDefaultModel is the model used for E2E tests.
    // Must match a model registered in examples/standalone/model_profiles.yaml.
    const e2eDefaultModel = "claude-sonnet-4-20250514"
    ```
    *   `createE2ESession()` のシグネチャを変更:
    ```go
    func createE2ESession(t *testing.T, baseURL, agent, workDir string) string
    // --> 変更なし。リクエストボディに model を追加するのみ。
    ```
*   **Logic**:
    *   `createE2ESession()` 内の `json.Marshal` 呼び出しを変更:
    ```go
    // Before:
    body, _ := json.Marshal(map[string]string{
        "agent":    agent,
        "work_dir": workDir,
    })

    // After:
    body, _ := json.Marshal(map[string]string{
        "agent":    agent,
        "model":    e2eDefaultModel,
        "work_dir": workDir,
    })
    ```

---

### 設定ファイル

---

#### [MODIFY] [model_profiles.yaml](file:///examples/standalone/model_profiles.yaml)
*   **Description**: Anthropic モデルリストの現行化
*   **Technical Design**: 旧名称 `claude-3-5-sonnet-latest` を削除し、E2E テストで使用するモデルのみに整理
*   **Logic**:
    ```yaml
    providers:
      openai:
        keys:
          - name: default
            value: vault://providers/openai/default
            models:
              - name: gpt-4o
              - name: gpt-4o-mini
              - name: gpt-4.1-mini
      anthropic:
        keys:
          - name: default
            value: vault://providers/anthropic/default
            models:
              - name: claude-sonnet-4-20250514
    ```

## Step-by-Step Implementation Guide

1. **Step 1: テストファイルにモデル定数を追加**
    *   Edit `tests/agentservice_e2e_test.go`
    *   import ブロックの後に `const e2eDefaultModel = "claude-sonnet-4-20250514"` を追加
    *   `createE2ESession()` の `json.Marshal` に `"model": e2eDefaultModel` を追加
    *   git commit: `fix: specify model in E2E test to avoid CLI default model error`

2. **Step 2: model_profiles.yaml を更新**
    *   Edit `examples/standalone/model_profiles.yaml`
    *   Anthropic モデルリストを `claude-sonnet-4-20250514` のみに整理
    *   OpenAI に `gpt-4.1-mini` を追加 (統合テストで使用)
    *   git commit: `chore: update model_profiles.yaml to current model names`

3. **Step 3: ビルド + 単体テスト**
    *   `./scripts/process/build.sh` を実行

4. **Step 4: E2E テスト実行**
    *   `./scripts/process/integration_test.sh --specify "TestE2E_CodingAgent"` を実行
    *   PASS を確認

5. **Step 5: git push**

## Verification Plan

### Automated Verification

1. **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```

2. **E2E Tests (対象テストのみ)**:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestE2E_CodingAgent"
    ```
    *   **Log Verification**:
        *   `TestE2E_CodingAgentStreaming`: text または tool_use イベントを受信し、error イベントなしで PASS
        *   `TestE2E_CodingAgentError`: 不正モデル指定のエラーハンドリングが正常動作で PASS

### テスト項目のセルフレビュー

**ボトムアップ順序**: 単体テストへの影響なし (テストコードのみの変更)。E2E テスト 2 件で直接検証。

**観点チェックリスト**:
- 正常系: モデル指定ありで CLI が正常応答 (exit 0)
- 異常系: `TestE2E_CodingAgentError` が既存のエラーハンドリングをテスト
- 後方互換: 他のテスト (mock ベース) に影響なし

**網羅性**: R1, R2 の全要件に対応する検証が存在する。
**迂回排除**: 全テストが `scripts/process/` 経由で実行される。
