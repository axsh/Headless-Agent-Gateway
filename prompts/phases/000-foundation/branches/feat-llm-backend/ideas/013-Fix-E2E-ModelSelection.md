# 013: E2E テストのモデル指定修正

## 背景 (Background)

`TestE2E_CodingAgentStreaming` が Claude CLI v2.1 環境で常に失敗する。
原因調査の結果、CLI のデフォルトモデル (`claude-opus-4-8[1m]`) が API キーのアクセス権外であり、
CLI がエラーメッセージを返して exit code 1 で終了していることが判明した。

```
event[1]: type=text content="There's an issue with the selected model (claude-opus-4-8[1m]).
It may not exist or you may not have access to it. Run --model to pick a different model."
```

### 問題の流れ

1. `createE2ESession()` でセッション作成時に `model` フィールドを指定していない
2. `handleCreateSession()` で `record.Model` が空文字のまま保存される
3. `handleSendMessage()` で `codingagent.WithModel("")` が渡される
4. `BuildArgs()` は `cfg.Model == ""` の場合 `--model` を省略する
5. CLI は自身のデフォルトモデル (`claude-opus-4-8[1m]`) を使用する
6. API キーに当該モデルのアクセス権がないため、CLI が認証エラーで exit 1

### 影響範囲

- [agentservice_e2e_test.go](file:///tests/agentservice_e2e_test.go): `TestE2E_CodingAgentStreaming`, `TestE2E_CodingAgentError`
- E2E テスト以外のテスト (mock ベース) は影響なし

## 要件 (Requirements)

### 必須要件

#### R1: E2E テストでモデルを明示的に指定する

- `createE2ESession()` でセッション作成時に `model` フィールドを含める
- 使用するモデルは Gateway の `model_profiles.yaml` に登録済みのモデルと一致させる
  - 現在の設定: `claude-sonnet-4-20250514`
- テストヘルパーにモデル名を定数として定義し、変更を容易にする

#### R2: model_profiles.yaml のモデルリストを現行化する

- [model_profiles.yaml](file:///examples/standalone/model_profiles.yaml) の Anthropic モデルリストを更新する
  - `claude-3-5-sonnet-latest` は旧名称のため、必要に応じて更新を検討する
  - E2E テストで使用するモデルが含まれていることを確認する

## 実現方針 (Implementation Approach)

### 変更対象

1. **`tests/agentservice_e2e_test.go`**:
   - テスト用のデフォルトモデル定数を追加:
     ```go
     const e2eDefaultModel = "claude-sonnet-4-20250514"
     ```
   - `createE2ESession()` のリクエストボディに `"model": e2eDefaultModel` を追加
   - `TestE2E_CodingAgentError` も同様に修正 (モデル未指定の場合)

2. **`examples/standalone/model_profiles.yaml`** (任意):
   - モデルリストの現行化 (必要に応じて)

### 設計上の考慮点

- モデル名はテストコード内の定数として管理する。config.yaml や環境変数からの取得は過剰設計。
- Gateway の model_profiles.yaml に登録されているモデルと一致させることが重要。不一致の場合、Gateway がリクエストをルーティングできない。

## 検証シナリオ (Verification Scenarios)

1. `TestE2E_CodingAgentStreaming` を実行する
2. CLI がモデルエラーなしで応答を返す (exit code 0)
3. text イベントまたは tool_use イベントが受信される
4. error イベントが発生しない
5. hello.txt ファイルが作成される

## テスト項目 (Testing for the Requirements)

### ビルド・全体検証

1. ビルド + 単体テスト:
   ```bash
   scripts/process/build.sh
   ```

2. E2E テスト (該当テストのみ):
   ```bash
   scripts/process/integration_test.sh --specify "TestE2E_CodingAgent"
   ```
