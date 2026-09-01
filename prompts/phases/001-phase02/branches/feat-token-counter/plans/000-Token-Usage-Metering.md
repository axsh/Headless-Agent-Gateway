# 000-Token-Usage-Metering

> **Source Specification**: [ideas/000-Token-Usage-Metering.md](file://prompts/phases/001-phase02/branches/feat-token-counter/ideas/000-Token-Usage-Metering.md)

## Goal Description

Coding Agent（Claude Code / Codex）の SendMessage について、input/output トークンをターン合計・セッション合計・LLMコール単位で計測し、SSE `result.usage`・`GET /api/v1/sessions/:id`・`GET /api/v1/sessions/:id/usage`・`client/v1`（`GetSession` / `GetUsage`）から参照できるようにする。コール単位は低信頼可。ターン合計を正とする。

## User Review Required

None.（仕様レビュー済み。Client API Must・`GET .../usage` 固定済み。）

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1 共通 Usage モデル | Proposed Changes > codingagent/usage.go, event.go |
| R2 ターン合計の取得・公開 | claudecode/protocol.go, codex/protocol.go, handler_retry.go, StreamEvent.Usage |
| R3 セッション合計 + GET usage | usage_store.go, session_store SessionRecord.Usage, routeSessionByID /usage, handleGetSessionUsage |
| R4 LLMコール単位 | Claude assistant usage パース; Codex/LLMGP meter (tid 相関); calls[] in usage.json |
| R5 SSE 後方互換 | result に usage 追加のみ; ReferenceManual 更新 |
| R6 エージェント網羅 | Claude/Codex Must; Wayfinder は本計画では未実装（Should） |
| R7 永続化 | `{session_dir}/usage.json` + SessionRecord.Usage |
| R8 Client API Must | client/v1 TokenUsage, SessionUsageReport, GetUsage, Event.Usage |
| S1 total_cost_usd | TokenUsage.TotalCostUSD 任意フィールド |
| V1–V7 / A1–A6 | Verification Plan + tests/*TokenUsage* |

## Proposed Changes

### codingagent（共通モデル）

#### [NEW] [usage.go](file://shared/libs/go/codingagent/usage.go)

*   **Description**: 共通トークン利用量モデルと source/confidence 定数。
*   **Technical Design**:

```go
package codingagent

const (
	UsageSourceClaudeResult       = "claude_result"
	UsageSourceClaudeAssistant    = "claude_assistant"
	UsageSourceCodexTurnCompleted = "codex_turn_completed"
	UsageSourceCodexTokenCount    = "codex_token_count"
	UsageSourceLLMGateway         = "llmgateway"
	UsageSourceDerivedSessionSum  = "derived_session_sum"

	UsageConfidenceHigh = "high"
	UsageConfidenceLow  = "low"
)

// TokenUsage is token accounting for a turn, call, or session aggregate.
type TokenUsage struct {
	InputTokens              int      `json:"input_tokens"`
	OutputTokens             int      `json:"output_tokens"`
	CachedInputTokens        int      `json:"cached_input_tokens,omitempty"`
	CacheCreationInputTokens int      `json:"cache_creation_input_tokens,omitempty"`
	ReasoningOutputTokens    int      `json:"reasoning_output_tokens,omitempty"`
	TotalTokens              int      `json:"total_tokens,omitempty"` // only when provider supplies; never synthesize
	TotalCostUSD             *float64 `json:"total_cost_usd,omitempty"`
	Model                    string   `json:"model,omitempty"`
	Source                   string   `json:"source"`
	Confidence               string   `json:"confidence"`
	TurnID                   string   `json:"turn_id,omitempty"`
	CallID                   string   `json:"call_id,omitempty"`
	Partial                  bool     `json:"partial,omitempty"`
	CallsSumMismatch         bool     `json:"calls_sum_mismatch,omitempty"`
}

// TurnUsageRecord is one SendMessage turn persisted under usage.json.
type TurnUsageRecord struct {
	TurnID  string      `json:"turn_id"`
	Usage   TokenUsage  `json:"usage"`
	Calls   []TokenUsage `json:"calls,omitempty"`
}

// SessionUsageReport is GET /usage response body.
type SessionUsageReport struct {
	SessionID string           `json:"session_id"`
	Usage     TokenUsage       `json:"usage"`
	Turns     []TurnUsageRecord `json:"turns"`
}

// AddUsage sums numeric token fields from src into dst (for session aggregate).
// Does not invent total_tokens. Sums TotalCostUSD only when both non-nil.
func AddUsage(dst *TokenUsage, src TokenUsage) { /* ... */ }
```

*   **Logic**: R1 のフィールド一覧をそのまま構造体化。`AddUsage` は input/output/cache/reasoning を加算。`source`/`confidence` は呼び出し側がセッション合計用に上書き。

#### [NEW] [usage_test.go](file://shared/libs/go/codingagent/usage_test.go)

*   **Description**: `AddUsage` の加算・cost 合算・total_tokens 非合成をテーブル駆動で検証。
*   **Logic**: TDD — 実装前に失敗するテストを書く。

#### [MODIFY] [event.go](file://shared/libs/go/codingagent/event.go)

*   **Description**: `StreamEvent` に usage を追加。
*   **Technical Design**:

```go
type StreamEvent struct {
	// ...existing fields...
	Usage *TokenUsage `json:"usage,omitempty"`
}
```

#### [MODIFY] [event_test.go](file://shared/libs/go/codingagent/event_test.go)

*   **Description**: JSON round-trip で `usage` が保持されることを確認。

#### [MODIFY] [session_store.go](file://shared/libs/go/codingagent/session_store.go)

*   **Description**: `SessionRecord` にセッション合計を載せる。
*   **Technical Design**:

```go
type SessionRecord struct {
	// ...existing...
	Usage *TokenUsage `json:"usage,omitempty"`
}
```

#### [MODIFY] [options.go](file://shared/libs/go/codingagent/options.go)

*   **Description**: Gateway 相関用に Tern セッション/ターン ID を SessionConfig へ。
*   **Technical Design**:

```go
type SessionConfig struct {
	// ...existing...
	// TernSessionID is the HTTP session id (for LLMGP metering metadata).
	TernSessionID string
	// TurnID is the current SendMessage turn id (for LLMGP metering metadata).
	TurnID string
}
```

`WithTernSessionID` / `WithTurnID` オプションを追加。

---

### claudecode（パーサ）

#### [MODIFY] [protocol_test.go](file://shared/libs/go/codingagent/claudecode/protocol_test.go)（先にテスト）

*   **Description**:
  - `TestParseJSONLinesEvent_Result_Usage`: `result` に usage + total_cost_usd → `EventResult.Usage`（source=`claude_result`, confidence=`high`）
  - `TestParseJSONLinesEvent_Assistant_Usage`: assistant の `message.id` + `message.usage` → イベントの `Usage`（source=`claude_assistant`, CallID=message.id）
  - `TestParseJSONLinesEvent_Assistant_Usage_DedupSameID`: 同一 id の usage を 2 回パースしても CallID が同一（アグリゲータ側で dedup）

#### [MODIFY] [protocol.go](file://shared/libs/go/codingagent/claudecode/protocol.go)

*   **Description**: usage 抽出。
*   **Logic**:
  - `rawEvent` に `Usage json.RawMessage`, `TotalCostUSD *float64`, `ModelUsage` 任意を追加。
  - `messagePayload` に `ID string`, `Usage *struct{ InputTokens, OutputTokens, ... }` を追加。
  - `case "result"`: usage をパースし `StreamEvent{Type: EventResult, Usage: &TokenUsage{..., Source: claude_result, Confidence: high, TotalCostUSD}}`。
  - `case "assistant"`: 既存 tool_use/text 抽出に加え、`message.usage` があれば各出力イベント（または先頭イベント）に `Usage` を付与。CallID=`message.id`。usage のみで content が空なら `EventSystem` ではなく **Usage 付きの EventText(空は避ける)** — 実装は「最初の emitted イベントに Usage を載せる。emitted が 0 なら Type=EventSystem ではなく専用に `StreamEvent{Type: EventText, Content:"", Usage:...}` は避け、`Type: EventResult` は使わない。**採用: emitted が 0 のとき `StreamEvent{Type: EventSystem, Usage: ...}` でアグリゲータが拾う**（SSE に system が増えるが既存クライアントは無視可）。より単純: **常に Usage 専用でアグリゲータが Scan するなら、assistant パース後に Usage 付き EventSystem subtype ではなく、追加で `out` に `StreamEvent{Type: EventText, Content: "", Usage}` を足さず、tool_use/text の **全イベントに同じ Usage ポインタを付け、アグリゲータが CallID で dedup**。

---

### codex（パーサ）

#### [MODIFY] [protocol_test.go](file://shared/libs/go/codingagent/codex/protocol_test.go)（先にテスト）

*   **Description**:
  - `TestParseExecEvent_TurnCompleted_Usage`: `{"type":"turn.completed","usage":{"input_tokens":10,"output_tokens":5,"cached_input_tokens":2}}` → EventResult + Usage source=`codex_turn_completed`
  - `TestParseExecEvent_TokenCount_LastUsage`: `event_msg`/`token_count` で `info.last_token_usage` がある場合 Event に Usage（source=`codex_token_count`）。ターン合計確定はアグリゲータ側で turn.completed 優先。
  - 既存 `TestParseExecEvent_EventMsg_Ignored` から `token_count` を外し、専用テストへ移行。

#### [MODIFY] [protocol.go](file://shared/libs/go/codingagent/codex/protocol.go)

*   **Logic**:
  - `case "turn.completed"`: line 全体から `usage` を Unmarshal。あれば EventResult.Usage を設定。
  - `case "event_msg"` / `token_count`: `info.last_token_usage` を TokenUsage に変換した StreamEvent を返す（Type は **EventSystem** + Usage、または Type=EventText 空禁止 → **EventSystem + Usage**）。`task_complete` は従来どおり EventResult。
  - アグリゲータ優先順位: EventResult の Usage（turn.completed）> token_count 由来。

#### [MODIFY] [process.go BuildEnv](file://shared/libs/go/codingagent/codex/process.go) / [claudecode/process.go](file://shared/libs/go/codingagent/claudecode/process.go)

*   **Logic**: API キーメタデータに追記（ルーティング用 `sid=` は **現行どおり AgentSessionID|default** を維持）:

```
;sid=<AgentSessionID|default>;tern_sid=<TernSessionID>;tid=<TurnID>
```

`TernSessionID` / `TurnID` が空なら当該パート省略。既存 `ExtractSessionID` は最初の `sid=` のみ読むため sticky ルーティングは維持。

#### [MODIFY] BuildEnv テスト（両 process_test.go）

*   **Description**: TernSessionID/TurnID 設定時に `tern_sid=` / `tid=` が含まれること。未設定時は含まれないこと。

---

### agentservice（永続化・集計・API）

#### [NEW] [usage_store.go](file://shared/libs/go/agentservice/usage_store.go)

*   **Description**: `{session_dir}/usage.json` の読み書き。
*   **Technical Design**:

```go
// loadUsageReport(sessionDir string) (*codingagent.SessionUsageReport, error)
// saveUsageReport(sessionDir string, rep *codingagent.SessionUsageReport) error
// appendTurnUsage(sessionDir, sessionID string, turn codingagent.TurnUsageRecord) (*codingagent.TokenUsage, error)
//   // appends turn, recomputes session sum with AddUsage, source=derived_session_sum, confidence=high
```

ファイルが無い場合は空レポート。原子的 write（tmp + rename）。

#### [NEW] [usage_store_test.go](file://shared/libs/go/agentservice/usage_store_test.go)

*   **Description**: append 2 ターン → セッション合計が和になること。再読込で再現。

#### [NEW] [usage_aggregator.go](file://shared/libs/go/agentservice/usage_aggregator.go)

*   **Description**: 1 ターン中のコール usage 収集と終端確定。
*   **Technical Design**:

```go
type turnUsageAggregator struct {
	turnID   string
	calls    []codingagent.TokenUsage
	seenCall map[string]struct{}
	turn     *codingagent.TokenUsage // from EventResult
}

func (a *turnUsageAggregator) Observe(ev codingagent.StreamEvent)
func (a *turnUsageAggregator) Finalize() (turn codingagent.TurnUsageRecord, ok bool)
```

*   **Logic**:
  - `ev.Usage != nil && CallID != ""` → seenCall で dedup して calls に追加。
  - `ev.Type == EventResult && ev.Usage != nil` → turn 合計として採用（後勝ちではなく **最初の高信頼を優先**: source が claude_result / codex_turn_completed なら上書き確定。token_count は turn 未設定時のみ）。
  - Finalize: turn が nil で calls のみなら calls 合算を turn にし confidence=`low`, partial 可。turn と calls 合計が不一致なら `CallsSumMismatch=true`（ターン側は書き換えない）。
  - turn_id を Usage.TurnID にセット。

#### [NEW] [usage_aggregator_test.go](file://shared/libs/go/agentservice/usage_aggregator_test.go)

*   **Description**: dedup、turn 優先、mismatch フラグのテーブル駆動。

#### [MODIFY] [handler_retry.go](file://shared/libs/go/agentservice/handler_retry.go) / activeExecution

*   **Description**: `activeExecution` に `usageAgg *turnUsageAggregator` を持たせる。生成時に turnID で初期化。
*   **Logic**: `handleRelaySideEffects` 冒頭で `exec.usageAgg.Observe(ev)`。`EventResult` または非 retryable `EventError` で `Finalize` → `appendTurnUsage` → `SessionRecord.Usage` 更新 → `sessions.Update`。エラー時はログ Warn、ストリームは継続。
*   EventResult の `ev.Usage` がアグリゲータ確定後と異なる場合、**SSE に出す前に** 確定済み turn Usage を `ev.Usage` に載せる（クライアントが一貫したターン合計を見るため）。実装箇所: `writeEvent` / `handleRelaySideEffects` 内で Result 時に merge。

#### [MODIFY] [service.go routeSessionByID](file://shared/libs/go/agentservice/service.go)

*   **Logic**:

```go
} else if strings.HasSuffix(path, "/usage") {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.handleGetSessionUsage(w, r)
}
```

`/events` 等と同列、GET フォールバック前。

#### [NEW] [handler_usage.go](file://shared/libs/go/agentservice/handler_usage.go)

*   **Description**: `handleGetSessionUsage` — セッション取得 → `loadUsageReport(sessionDir)` → JSON 200。無ければ空 turns + zero usage（source=`derived_session_sum`）。404 if session missing。

#### [NEW] [handler_usage_test.go](file://shared/libs/go/agentservice/handler_usage_test.go)

*   **Description**: モックで SendMessage → result.usage → GET /usage / GET session の検証。

#### [MODIFY] Session 作成 / runTurn オプション組み立て

*   **Logic**: `SessionConfig` に `TernSessionID=sessionID`, `TurnID=turnID` を渡す（既存 CreateSession/Send オプション構築箇所）。

---

### llmgateway（Codex コール単位ベストエフォート）

#### [NEW] [usage_meter.go](file://shared/libs/go/llmgateway/usage_meter.go)

*   **Description**: インメモリのターン別コール usage バッファ。
*   **Technical Design**:

```go
type CallUsage struct {
	TernSessionID string
	TurnID        string
	Usage         codingagent.TokenUsage // source=llmgateway, confidence=low
}

type UsageMeter struct { /* sync.Mutex; map[ternSession|turn][]CallUsage */ }
func (m *UsageMeter) Record(ternSession, turn string, u codingagent.TokenUsage)
func (m *UsageMeter) Take(ternSession, turn string) []codingagent.TokenUsage // destructive read
```

#### [MODIFY] [fallback.go](file://shared/libs/go/llmgateway/fallback.go)

*   **Description**: `ExtractTernSessionID` / `ExtractTurnID` — `tern_sid=` / `tid=` をパース。既存 `ExtractSessionID`（`sid=`）は変更しない。

#### [MODIFY] anthropic/openai handlers

*   **Logic**: 応答 usage 取得直後に `tern_sid`/`tid` があれば `UsageMeter.Record`。ストリーム完了時も同様。

#### [MODIFY] AgentService Finalize

*   **Logic**: Codex ターン確定時、Gateway UsageMeter から Take して calls にマージ（CLI calls が空のとき優先して埋める。既に Claude 由来 calls がある場合は追記し CallID で区別）。Proxy への Meter 参照注入は tern.Server / agentservice 起動配線で行う。Meter が無い場合はスキップ（テスト容易性）。

> Wayfinder Should は本計画の実装対象外。Meter 基盤のみ共有可能にする。

---

### client/v1（R8 Must）

#### [NEW] [usage.go](file://client/v1/usage.go)

```go
type TokenUsage struct { /* mirror codingagent JSON tags */ }
type TurnUsageRecord struct {
	TurnID string      `json:"turn_id"`
	Usage  TokenUsage  `json:"usage"`
	Calls  []TokenUsage `json:"calls,omitempty"`
}
type SessionUsageReport struct {
	SessionID string           `json:"session_id"`
	Usage     TokenUsage       `json:"usage"`
	Turns     []TurnUsageRecord `json:"turns"`
}
```

#### [MODIFY] [session.go](file://client/v1/session.go)

*   `SessionInfo.Usage *TokenUsage`
*   `Client.GetUsage(ctx, sessionID) (*SessionUsageReport, error)`
*   `Session.GetUsage(ctx) (*SessionUsageReport, error)`

#### [MODIFY] [stream.go](file://client/v1/stream.go)

*   `Event.Usage *TokenUsage`
*   raw unmarshal に Usage を追加。`OnResult` は既存 `func(ev Event)` があれば usage を読めるようにする。破壊的に `StreamHandlers.OnResult` のシグネチャを変える場合はコンパイルエラー箇所をすべて修正。

#### [NEW/MODIFY] session_test.go / stream 関連テスト

*   GetSession に usage、GetUsage デコード、result イベントの usage。

---

### Documentation

#### [MODIFY] [docs/ReferenceManual-WebAPIs.md](file://docs/ReferenceManual-WebAPIs.md)

*   Get Session に `usage`（セッション合計）。
*   新エンドポイント `GET /api/v1/sessions/:id/usage`。
*   SendMessage `result.usage`。
*   `total_cost_usd` は推定であり請求に使わない旨。

#### [MODIFY] client README（存在すれば）または docs に Client `GetUsage` 例を短く追記。

---

### E2E Tests

#### [NEW] [tests/token_usage_e2e_test.go](file://tests/token_usage_e2e_test.go)

*   **Description**:
  - `TestClaudeCodeE2E_TokenUsage_TurnAndSession`（V1, V3, V7）: SendMessage → result.usage > 0 → GET /usage → client GetUsage。2 回目 Send でセッション合計増加。
  - `TestCodexE2E_TokenUsage_Turn`（V2）: Codex で result.usage または GET /usage にターン記録。
  - `TestClaudeCodeE2E_TokenUsage_Calls`（V4）: ツール使用プロンプトで calls が 1+（環境により 0 なら turn usage 必須・calls は Soft または skip）。ライブ LLM 依存のため既存 E2E と同じビルドタグ／ヘルパを使用。
  - ヘルパ: `agentservice_e2e_test.go` の `startE2EServer`, `sendE2EMessage`, `parseE2ESSEEvents`, `getE2ESession` を再利用。

## Step-by-Step Implementation Guide

1. **TokenUsage model (TDD)**: Add `usage_test.go` then `usage.go`; extend `event.go` / `session_store.go` / `options.go`.
2. **Claude parser (TDD)**: Update protocol tests then `protocol.go`.
3. **Codex parser (TDD)**: Update protocol tests then `protocol.go`; BuildEnv `tern_sid`/`tid`.
4. **usage_store + aggregator (TDD)**: New agentservice files.
5. **Wire AgentService**: activeExecution observe/finalize; route `/usage`; handler_usage; pass TernSessionID/TurnID into SessionConfig.
6. **LLMGP meter (best-effort)**: usage_meter, ExtractTern*, handler Record, Take on finalize.
7. **client/v1**: usage types, GetUsage, SessionInfo.Usage, Event.Usage + unit tests.
8. **Docs**: ReferenceManual-WebAPIs.md.
9. **E2E**: `tests/token_usage_e2e_test.go`.
10. **Verify**: build.sh then integration_test.sh --specify TokenUsage.
11. **Commit per logical step; push after green.**

各ステップ完了時にチェックボックスを更新:

- [x] 1 TokenUsage model
- [x] 2 Claude parser
- [x] 3 Codex parser + BuildEnv
- [x] 4 usage_store + aggregator
- [x] 5 AgentService wire + GET /usage
- [x] 6 LLMGP meter (Record/Take + tern_sid/tid extract; handler Record wiring deferred as best-effort infrastructure)
- [x] 7 client/v1
- [x] 8 Docs
- [x] 9 E2E
- [x] 10 Build & integration tests
- [x] 11 Push

## Verification Plan

### Automated Verification

1. **Build & Unit Tests**: `./scripts/process/build.sh`
2. **Integration / E2E**: `./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TokenUsage"`
3. **E2E Tests**: `tests/token_usage_e2e_test.go`（上記ケース）。必要に応じて `--categories common`。

Windows では Git Bash。Linux / Remote-SSH では `build.sh --skip-etc`、integration は `xvfb-run -a` ラップ（プロジェクト規則どおり）。

## Documentation

- `docs/ReferenceManual-WebAPIs.md`: usage エンドポイントとフィールド。
- Client 利用例（短文）。
