# E2E テストの SendMessage API 互換性修正

## 背景 (Background)

`agentservice` の `SendMessageRequest` 構造体が v2 API 形式 (`content` フィールド、`[]ContentPart` 型) に変更されたが、`tests/` 配下の E2E テストヘルパー `sendE2EMessage` は旧 v1 API 形式 (`{"message": "..."}`) のままリクエストを送信している。

この不一致により、全ての E2E テスト (codex, claudecode, wayfinder) が `"content must not be empty"` エラーで失敗している。サーバーは `content` フィールドを期待するが、テストは `message` フィールドを送信するため、JSON デコード後の `Content` フィールドが空の `[]ContentPart{}` となり、バリデーションに失敗する。

### 影響範囲

- `tests/agentservice_e2e_test.go`: `sendE2EMessage` ヘルパー (L261)、および L843 の直接送信
- `tests/wayfinder_e2e_test.go`: L120 の直接送信
- 上記ヘルパーを使用する全テスト: `TestE2E_CodingAgentStreaming`, `TestE2E_CodingAgentError`, `TestE2E_CodingAgentDefaultModel`, `TestE2E_SessionContinuation`, `TestCodexE2E_FileCreation`, `TestCodexE2E_GeminiModel_FileCreation`, `TestCodexE2E_AnthropicModel_FileCreation`, `TestCodexE2E_GPT5Codex_FileCreation`, `TestCodexE2E_ErrorPropagation`, `TestE2E_WSLDelegation_FailReproduction` など

### 原因

`handler.go` の `SendMessageRequest` が以下のように変更された:

```go
// 旧 (v1):
type SendMessageRequest struct {
    Message string `json:"message"`
}

// 現在 (v2):
type SendMessageRequest struct {
    Content []codingagent.ContentPart `json:"content"`
}
```

一方、E2E テストの `sendE2EMessage` は旧形式のまま:

```go
// tests/agentservice_e2e_test.go L261
body, _ := json.Marshal(map[string]string{"message": message})
```

正しい client ライブラリ (`client/v1/session.go` L77) は既に v2 形式で送信している:

```go
body, err := json.Marshal(map[string]any{"content": content})
```

## 要件 (Requirements)

### 必須要件

1. **`sendE2EMessage` ヘルパーの修正**: リクエストボディを `{"content": [{"type": "text", "text": "..."}]}` 形式に変更する。
2. **`wayfinder_e2e_test.go` の修正**: L120 の直接送信箇所を同じく `content` 形式に変更する。
3. **`agentservice_e2e_test.go` L843 の修正**: WSL テスト内の直接送信箇所を同じく `content` 形式に変更する。
4. **既存テストの動作**: 修正後、`sendE2EMessage` を使用する全テストがサーバーの content バリデーションを通過すること。

### 制約

- `sendE2EMessage` のシグネチャ (`message string` パラメータ) は変更しない。内部でテキストを `ContentPart` に変換する。
- E2E テストは外部 CLI (codex, claude) やネットワーク接続を必要とするため、環境依存の失敗は許容される（ただし `"content must not be empty"` エラーは発生しないこと）。

## 実現方針 (Implementation Approach)

### 1. `sendE2EMessage` ヘルパーの修正

```go
func sendE2EMessage(t *testing.T, baseURL, sessionID, message string, timeout time.Duration) *http.Response {
    t.Helper()
    type contentPart struct {
        Type string `json:"type"`
        Text string `json:"text,omitempty"`
    }
    body, _ := json.Marshal(map[string]any{
        "content": []contentPart{{Type: "text", Text: message}},
    })
    // ... 以降は既存のまま
}
```

### 2. `wayfinder_e2e_test.go` のヘルパー修正

`wayfinder_e2e_test.go` にも同様のヘルパーがある場合、同じパターンで修正する。直接 `map[string]string{"message": message}` を使っている箇所を `content` 形式に変更する。

### 3. `agentservice_e2e_test.go` L843 の修正

WSL テスト内の直接送信も同じく `content` 形式に変更する。

## 検証シナリオ (Verification Scenarios)

1. `sendE2EMessage` で送信したリクエストが、サーバーの `ValidateContentParts` バリデーションを通過する
2. codex E2E テスト (`TestCodexE2E_ErrorPropagation`, `TestCodexE2E_HealthWithCodexAgent`) が PASS する
3. claudecode E2E テスト (`TestE2E_CodingAgentError`, `TestE2E_SessionDirFallback`) が PASS する
4. 環境依存テスト (codex CLI 必要、API キー必要) は Skip またはエラーイベントで適切に処理される（`"content must not be empty"` エラーは発生しない）

## テスト項目 (Testing for the Requirements)

### ビルドとユニットテスト

```bash
./scripts/process/build.sh
```

### E2E テスト

```bash
./scripts/process/integration_test.sh --specify "TestCodexE2E|TestE2E_"
```

検証ポイント:
- `"content must not be empty"` エラーがテスト出力に含まれないこと
- 環境非依存テスト (`TestCodexE2E_HealthWithCodexAgent`, `TestE2E_SessionDirFallback`, `TestE2E_StandaloneHealth`) が PASS すること
