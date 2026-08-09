# 000-Codex-Responses-AdditionalTools-Bifrost-Bump

> **Source Specification**: `prompts/phases/000-foundation/branches/fix-bug-#33/ideas/000-Codex-Responses-AdditionalTools-Bifrost-Bump.md`

## Goal Description

Codex が Responses wire で送る `input[].type = "additional_tools"` アイテム内の `tools[].type` が、Bifrost `v1.5.18` の誤デコードにより欠落し、OpenAI が次のエラーで拒否する問題を解消する ([Issue #33](https://github.com/axsh/arctic-tern/issues/33) / [Bifrost #5100](https://github.com/maximhq/bifrost/issues/5100))。

```text
Missing required parameter: 'input[0].tools[0].type'
```

本計画は **TDD (RED → GREEN)** で以下を行う:

1. `additional_tools` round-trip 単体テストを先に追加し、現行 `v1.5.18` では **FAIL** することを確認する (R6)
2. `github.com/maximhq/bifrost/core` を **下限 `v1.6.4` 以上**へ bump する (R1, O4 方針)
3. Tern LLM Gateway のコンパイル差分を最小修正で吸収する (R2)
4. `SanitizeToolsForProvider` の OpenAI early return は主修正にしない (R4, R5)
5. 統合テストで gateway / Codex 非退行を確認する (R7)
6. **実 OpenAI へのライブ呼び出し**で当該 `upstream_error` が無いことを必須確認する (R8, R9)

## User Review Required

1. **Bifrost 目標バージョン戦略**: 実装時に次の順で試す想定。異議があれば指示してください。
   - まず実装時点の最新 stable core（調査時点例: `v1.7.x`）を試す (仕様 O4)
   - コンパイル破壊が大きい場合は `v1.6.4`（`additional_tools` 修正取り込み最小）へピン
2. **ライブ必須（レビュー決定済み）**: `TestLLMGateway_Responses_AdditionalTools_LiveOpenAI` は受け入れ必須。OpenAI キー未登録での Skip 完了は不可。O2 / O3 / シナリオ A までの Codex 実機ライブ (O5) は任意のまま。

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: Bifrost core を `v1.6.4` 以上へ更新 | `Proposed Changes` > `go.mod` / `go.sum` / `tests/go.sum` |
| R2: bump 後 LLM Gateway がコンパイル可能、破壊的 API は最小追随 | `Proposed Changes` > `llmgateway/**`（差分が出たファイルのみ） |
| R3: `additional_tools` 内 `tools[].type` が欠落しない | Bifrost bump + round-trip assert + **LiveOpenAI** |
| R4: OpenAI sanitize early return を主修正にしない | `tool_sanitize_test.go` で現状ロック。`tool_sanitize.go` は変更しない |
| R5: Tern 独自 raw 保持は必須外 | Non-Goal。当該コードを新規追加しない |
| R6: 実 OpenAI なしの round-trip 単体／テーブルテスト | `openai/responses_additional_tools_test.go`（Phase 1 RED → Phase 3 GREEN） |
| R7: 既存 LLM Gateway / Codex テスト非退行 | Verification Plan の `build.sh` + `integration_test.sh` |
| R8: ライブで当該 `upstream_error` が再発しない（Skip 不可） | `tests/llm_gateway_additional_tools_test.go` の **LiveOpenAI（必須）** |
| R9: Live 統合テスト追加。キー無しは Fail | 同上。`requireOpenAIKeyring(t)` で未登録時 Fail |
| O1–O4, O5(Codex 実機) | 本 Part 対象外（任意） |

## Proposed Changes

> [!IMPORTANT]
> `Proposed Changes` では **`_test.go` を先に**記述する（TDD）。実装（bump / 追随）はテストの後。

### Phase 1 — 再現テスト (修正前、RED 確認)

#### [NEW] [shared/libs/go/llmgateway/openai/responses_additional_tools_test.go](file://shared/libs/go/llmgateway/openai/responses_additional_tools_test.go)

*   **Description**: 仕様シナリオ C 相当の JSON を、Tern Responses ハンドラと同じ経路（`OpenAIResponsesRequest` Unmarshal → `ToBifrostResponsesRequest` → `ToOpenAIResponsesRequest` → `MarshalJSON`）で round-trip し、`input[0].tools[i].type` が保持されることを検証する。現行 Bifrost `v1.5.18` では **FAIL**（型欠落）を確認する。
*   **Technical Design**:
    *   パッケージ: `openai_test`（外部テスト）または `openai`（内部）。Bifrost の `providers/openai` と `schemas` を直接 import してよい。
    *   ヘルパ `additionalToolsFixtureJSON(model string) []byte` が以下の固定 JSON を返す（仕様書シナリオ C を継承。要約しない）:

```json
{
  "model": "<responses-mode-openai-model>",
  "stream": true,
  "tools": [],
  "input": [
    {
      "type": "additional_tools",
      "role": "developer",
      "tools": [
        { "type": "custom", "name": "example_custom" },
        {
          "type": "function",
          "name": "example_fn",
          "description": "example",
          "parameters": {
            "type": "object",
            "properties": { "q": { "type": "string" } },
            "required": ["q"],
            "additionalProperties": false
          }
        },
        {
          "type": "namespace",
          "name": "example_ns",
          "description": "ns",
          "tools": [
            {
              "type": "function",
              "name": "child",
              "description": "child fn",
              "parameters": {
                "type": "object",
                "properties": {},
                "additionalProperties": false
              }
            }
          ]
        }
      ]
    },
    {
      "type": "message",
      "role": "user",
      "content": "Reply exactly with OK."
    }
  ]
}
```

    *   Round-trip 手順（`handler.go` と同型）:

```go
var oaiReq bifrostOpenAI.OpenAIResponsesRequest
err := json.Unmarshal(body, &oaiReq)
bifrostCtx := bifrostSchemas.NewBifrostContext(context.Background(), bifrostSchemas.NoDeadline)
bifrostReq := oaiReq.ToBifrostResponsesRequest(bifrostCtx)
outReq := bifrostOpenAI.ToOpenAIResponsesRequest(bifrostCtx, bifrostReq) // Bifrost v1.7.7+: BifrostContext 必須
outBytes, err := outReq.MarshalJSON()
```

    *   Assert: `outBytes` を `map[string]any` / `gjson` / 再 Unmarshal し、次を検証する。

*   **Logic** — テーブル駆動テスト:

| テスト名 | Phase 1 (v1.5.18) | Phase 3 (bump 後) | Assert |
|----------|-------------------|-------------------|--------|
| `TestResponsesAdditionalTools_RoundTripPreservesNestedType` | **FAIL** | **PASS** | `input[0].type == "additional_tools"` かつ `input[0].tools` の各要素に非空 `type` がある。期待値: `["custom","function","namespace"]`（順不同可だが順固定推奨） |
| `TestResponsesAdditionalTools_NamespaceChildTypePreserved` | **FAIL** | **PASS** | namespace 要素の `tools[0].type == "function"` かつ `name == "child"` |
| `TestResponsesAdditionalTools_TopLevelToolsRemainEmpty` | PASS または FAIL→PASS | **PASS** | トップレベル `tools` が空配列または省略（Codex code-mode の `tools: []` を壊さない） |

Assert 擬似コード:

```go
var wire map[string]any
json.Unmarshal(outBytes, &wire)
input := wire["input"].([]any)
item0 := input[0].(map[string]any)
if item0["type"] != "additional_tools" {
    t.Fatalf("input[0].type = %v, want additional_tools", item0["type"])
}
tools := item0["tools"].([]any)
wantTypes := []string{"custom", "function", "namespace"}
for i, want := range wantTypes {
    got := tools[i].(map[string]any)["type"]
    if got != want {
        t.Fatalf("input[0].tools[%d].type = %v, want %v (missing type reproduces Issue #33)", i, got, want)
    }
}
```

> Phase 1 では上記が FAIL することが **再現成功**。`t.Fatalf` メッセージに Issue #33 / missing type を含める。

#### [NEW] [shared/libs/go/llmgateway/tool_sanitize_test.go](file://shared/libs/go/llmgateway/tool_sanitize_test.go)

*   **Description**: R4 ロック。`SanitizeToolsForProvider` が OpenAI では tools を変更しないこと、および非 OpenAI では `namespace` を落とす既存意図を維持することを検証する。
*   **Technical Design**:

```go
func TestSanitizeToolsForProvider_OpenAI_NoOp(t *testing.T) {
    // Params.Tools に Type=namespace と Type=function を含む BifrostResponsesRequest を用意
    // SanitizeToolsForProvider(req, bifrostSchemas.OpenAI, log)
    // → Tools の長さ・Type が変化しないこと
}

func TestSanitizeToolsForProvider_NonOpenAI_FiltersNamespace(t *testing.T) {
    // provider = Gemini（または Anthropic）
    // → namespace が除去され function / web_search のみ残る（既存 tool_sanitize.go の switch どおり）
}
```

*   **Logic**: 本件で `tool_sanitize.go` を変更しないことの回帰ガード。Phase 1 から GREEN でよい。

#### [NEW] [tests/llm_gateway_additional_tools_test.go](file://tests/llm_gateway_additional_tools_test.go)

*   **Description**: 統合レイヤの回帰テスト。キー不要 round-trip に加え、**実 OpenAI ライブ呼び出しを必須**とする (R8, R9)。
*   **Technical Design**:
    *   必須: `TestLLMGateway_Responses_AdditionalTools_RoundTrip` — 同一 fixture / Bifrost API 経路で nested `type` を assert。
    *   **必須ライブ**: `TestLLMGateway_Responses_AdditionalTools_LiveOpenAI`
        *   OpenAI キー必須。未登録時は **Fail**（Skip 禁止）。メッセージ例: `openai API key required: bin/vault-cli set --provider openai --stdin`
        *   ヘルパは既存 `checkKeyringAvailable` の Skip 版を使わず、`requireOpenAIKeyring(t)` を新設する。
        *   `testServer` 起動後、`POST {baseURL}/v1/responses` にシナリオ C fixture を送る。`model` は `tests/testdata/model_profiles.yaml` の Responses 向け（例: `gpt-5.3-codex`）を使う。
        *   Assert（いずれかでも Fail）:
            *   レスポンス本文に `Missing required parameter: 'input[0].tools[0].type'` が **無い**
            *   レスポンス本文に `"code":"upstream_error"` かつ当該 message の組み合わせが **無い**
            *   HTTP ステータスが 400 かつ上記メッセージの場合は Fail（他理由の 4xx/5xx は別問題としてログし、当該 missing-parameter のみを厳密に禁止）
        *   `stream: false` でも `stream: true`（SSE を読み切り）でもよい。実装は非ストリームを推奨（assert が単純）。

```go
func requireOpenAIKeyring(t *testing.T) {
    t.Helper()
    kb := vault.NewKeyringVaultBackend()
    if _, err := kb.Resolve("vault://providers/openai/default"); err != nil {
        t.Fatalf("openai API key required for live AdditionalTools test (not skippable): bin/vault-cli set --provider openai --stdin: %v", err)
    }
}

func TestLLMGateway_Responses_AdditionalTools_RoundTrip(t *testing.T) {
    // same fixture + ToBifrostResponsesRequest + ToOpenAIResponsesRequest + MarshalJSON
    // assert input[0].tools[i].type preserved
}

func TestLLMGateway_Responses_AdditionalTools_LiveOpenAI(t *testing.T) {
    requireOpenAIKeyring(t)
    baseURL, token, cleanup := testServer(t)
    defer cleanup()
    // POST /v1/responses with additional_tools fixture (model=gpt-5.3-codex or profiles entry)
    // Fail if body contains: Missing required parameter: 'input[0].tools[0].type'
}
```
### Phase 2 — Bifrost bump と Tern 追随

#### [MODIFY] [go.mod](file://go.mod) / [go.sum](file://go.sum)

*   **Description**: `github.com/maximhq/bifrost/core` を現行 `v1.5.18` から **`v1.6.4` 以上**へ更新する (R1)。
*   **Technical Design**:
    *   現行:

```go
github.com/maximhq/bifrost/core v1.5.18
```

    *   目標（例。実装時に選定）:

```go
github.com/maximhq/bifrost/core v1.6.4
// または実装時点のより新しい stable（例: v1.7.x）
```

*   **Logic**:
    1. User Review の戦略に従い候補タグを決める
    2. モジュール更新後 `go.sum` を更新
    3. `tests/go.sum` の transitive `bifrost/core` も追随（`tests` モジュールの tidy）
    4. `go.mod` 上のバージョン文字列が **semver 比較で `>= v1.6.4`** であること（受け入れ基準）

#### [MODIFY] [tests/go.mod](file://tests/go.mod) / [tests/go.sum](file://tests/go.sum)

*   **Description**: ルート bump に合わせて tests モジュールの間接依存を更新する。
*   **Logic**: `replace github.com/axsh/arctic-tern => ../` はそのまま。tidy で `bifrost/core` の indirect 行が新バージョンになること。

#### [MODIFY] [shared/libs/go/llmgateway/**](file://shared/libs/go/llmgateway/)（差分が出たファイルのみ）

*   **Description**: Bifrost bump による破壊的 API / シグネチャ変更を **最小差分**で吸収する (R2)。本件スコープ外のリファクタは行わない。
*   **Technical Design — 変更候補（コンパイルエラーが出たものだけ）**:
    *   `bifrost_driver.go` — `bifrost.Init` / `BifrostConfig` / `InitialPoolSize` 等
    *   `bifrost_account.go` — `Account` インタフェースメソッド
    *   `openai/handler.go` — `OpenAIResponsesRequest`, `ToBifrostResponsesRequest`, `ResponsesRequest` / `ResponsesStreamRequest`
    *   `anthropic/handler.go`, `anthropic/convert.go` — `ResponsesMessage` / `ResponsesTool` フィールド
    *   `handler_context.go`, `handlerctx/context.go` — 型エイリアス
    *   `tool_sanitize.go` — **意図的に変更しない**（R4）。`OpenAI` early return を維持:

```go
func SanitizeToolsForProvider(
	bifrostReq *bifrostSchemas.BifrostResponsesRequest,
	providerKey bifrostSchemas.ModelProvider,
	log logger.Logger,
) {
	if bifrostReq.Params == nil || providerKey == bifrostSchemas.OpenAI {
		return
	}
	// ... existing filter for non-OpenAI ...
}
```

*   **Logic**:
    *   Tern 独自の `additional_tools` raw 保持レイヤは **追加しない** (R5)
    *   bump 後、Bifrost 側の `ResponsesMessageTypeAdditionalTools` / `rawPreserved` により nested `type` が保持されることが修正本体 (R3)
    *   コンパイルが通るまでのみ追随し、挙動変更が必要な場合は理由をコミットメッセージに残す

#### [DO NOT MODIFY] [shared/libs/go/llmgateway/tool_sanitize.go](file://shared/libs/go/llmgateway/tool_sanitize.go)

*   **Description**: 本件の主修正対象外。OpenAI early return を維持する (R4)。

#### [DO NOT MODIFY] Codex adapter（原則）

*   `shared/libs/go/codingagent/codex/**` は変更不要。gateway 側 bump で解消する。

### Phase 3 — GREEN 確認と受け入れ

Phase 1 の FAIL していた round-trip テストが PASS すること。既存 llmgateway / anthropic / openai パッケージテスト、Verification Plan の統合テスト、および **`TestLLMGateway_Responses_AdditionalTools_LiveOpenAI` の実 OpenAI PASS** が揃うこと。Live 未実行・Skip では受け入れ完了としない。

## Step-by-Step Implementation Guide

1. **Phase 1 RED — round-trip テスト作成**: `shared/libs/go/llmgateway/openai/responses_additional_tools_test.go` を追加し、仕様シナリオ C の JSON fixture と `TestResponsesAdditionalTools_RoundTripPreservesNestedType` / `TestResponsesAdditionalTools_NamespaceChildTypePreserved` を実装する。
2. **Phase 1 RED — sanitize ロック**: `shared/libs/go/llmgateway/tool_sanitize_test.go` に `TestSanitizeToolsForProvider_OpenAI_NoOp` と非 OpenAI filter テストを追加する。
3. **Phase 1 RED — 統合テスト**: `tests/llm_gateway_additional_tools_test.go` に RoundTrip（必須）と **LiveOpenAI（必須・キー無しは Fail）** を追加する。
4. **RED 確認**: `./scripts/process/build.sh` を実行し、`TestResponsesAdditionalTools_RoundTripPreservesNestedType` が **FAIL**（`type` 欠落）することを確認してから次へ進む。sanitize テストは PASS でよい。
5. **バージョン選定**: User Review 方針に従い Bifrost タグを決める（最新 stable 優先、失敗時 `v1.6.4`）。
6. **Bump**: ルート `go.mod` の `github.com/maximhq/bifrost/core` を更新し、`go.sum` と `tests/go.sum` を同期する。`go.mod` 上で `>= v1.6.4` を満たすこと。
7. **コンパイル追随**: `./scripts/process/build.sh` でエラーが出た `shared/libs/go/llmgateway/**` のみ最小修正する。`tool_sanitize.go` と Codex adapter は触らない。
8. **GREEN 確認（単体）**: 再実行し、Phase 1 の AdditionalTools round-trip テストが **PASS**、既存 llmgateway テストが PASS することを確認する。
9. **GREEN 確認（統合 + ライブ必須）**: Verification Plan の `integration_test.sh` を実行し、特に `--specify "AdditionalTools"` で **LiveOpenAI が実際に走り PASS** すること（キー未登録 Fail）。事前に `bin/vault-cli set --provider openai --stdin` が必要。
10. **受け入れチェックリスト**を埋める。Live PASS なしでの完了は不可。

## Verification Plan

### Automated Verification

1. **Build & Unit Tests**（Linux / Remote-SSH Linux では `--skip-etc`）:

```bash
./scripts/process/build.sh
```

```bash
./scripts/process/build.sh --skip-etc
```

2. **Integration Tests**（必ず `build.sh` 成功後）:

```bash
./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories llm --specify "AdditionalTools"
```

```bash
./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories llm --specify "TestLLMGateway"
```

```bash
./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories llm --specify "TestCodexE2E"
```

推奨スモーク:

```bash
./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories common
```

3. **E2E / Integration コード**（本計画で追加）:
   *   `tests/llm_gateway_additional_tools_test.go`
       *   `TestLLMGateway_Responses_AdditionalTools_RoundTrip`（キー不要・必須）
       *   `TestLLMGateway_Responses_AdditionalTools_LiveOpenAI`（**実 OpenAI・必須**。キー未登録は Fail / Skip 不可）

### シナリオ対応（仕様 Verification Scenarios）

| 仕様シナリオ | 自動検証 |
|--------------|----------|
| A: CreateSession + SendText | 任意 O5。本 Part の必須ライブはシナリオ C の gateway POST |
| B: `codex exec ... gpt-5.6-terra` | 手動補助のみ（計画の主検証にしない） |
| C: `/v1/responses` 合成 JSON | RoundTrip（オフライン）+ **LiveOpenAI（必須）** |
| D: Bifrost バイパス対照 | 本 Part 対象外（調査知見として記録済み） |
| E: 非 code-mode 非退行 | `TestLLMGateway` / `TestCodexE2E` |
| F: Trace 差分 | 受け入れ補助。自動テストでは wire JSON assert + Live が代替 |

### 受け入れ基準

- [x] `go.mod` の Bifrost core が **`v1.6.4` 以上**（実装: `v1.7.7`）
- [x] Phase 1 で round-trip テストが一度 FAIL（型欠落）した記録がある、または bump 後でも欠落を検出する assert が残っている
- [x] `./scripts/process/build.sh` 成功
- [x] `TestResponsesAdditionalTools_RoundTripPreservesNestedType` PASS
- [x] `TestResponsesAdditionalTools_NamespaceChildTypePreserved` PASS
- [x] `TestSanitizeToolsForProvider_OpenAI_NoOp` PASS（`tool_sanitize.go` 未変更）
- [x] `./scripts/process/integration_test.sh --specify "AdditionalTools"` PASS
- [x] **`TestLLMGateway_Responses_AdditionalTools_LiveOpenAI` が実 OpenAI 呼び出しで PASS**（Skip での完了不可）
- [x] LLM gateway 回帰（`ResponsesAPI` / `CrossProvider` / `ServerLifecycle` 等）PASS。`TestLLMGateway_*` AdditionalTools 含む
- [x] `./scripts/process/integration_test.sh --specify "TestCodexE2E"` が環境前提を満たす範囲で PASS または従来どおり Skip（OpenAI/Codex 系は PASS。Anthropic キー無効による 401 で一部 FAIL は環境要因）
- [x] Tern 独自 `additional_tools` raw バイパスコードを追加していない
- [x] ライブ応答に Issue #33 の exact エラー文字列が含まれない
## Documentation

*   必須のユーザ向けドキュメント更新はなし。
*   任意 (O1): bump で追随した破壊的 API 差分があれば、実装コミットメッセージまたは `prompts/phases/.../plans/` への短い追記で残す。
    *   **実装メモ (v1.7.7)**: `schemas.EnvVar` → `schemas.SecretVar`（`bifrost_account.go` の Key.Value / Ollama URL）。`ToOpenAIResponsesRequest(ctx, bifrostReq)` が `BifrostContext` 必須に。ルート/features/examples の `go` を `1.26.5` に揃え。
*   任意 (O3): `settings/example/model_profiles.yaml` / `settings/demo/model_profiles.yaml` への `gpt-5.6-*` 追記は本 Part 対象外。
