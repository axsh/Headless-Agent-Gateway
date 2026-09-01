# 001-Token-Usage-Example-And-Query

> **Source Specification**:
> - [ideas/001-Token-Usage-Example.md](file://prompts/phases/001-phase02/branches/feat-token-counter/ideas/001-Token-Usage-Example.md)
> - [ideas/000-Token-Usage-Metering.md](file://prompts/phases/001-phase02/branches/feat-token-counter/ideas/000-Token-Usage-Metering.md) R3 / R3b（UsageQuery）

## Goal Description

1. `GET /usage` と `client/v1.GetUsage(ctx, opts ...UsageQuery)` にターン絞り込み（`last_n` / `after_turn_id` / turn_id 範囲 / 時刻）を追加する。レスポンス `usage` は返却 turns の再合計。
2. `examples/token-usage` で Session / Turn / LLM call の 3 階層表示と、`GetUsage(ctx)` 全件 + `GetUsage(ctx, UsageQuery{LastN:1})` をデモする。

## User Review Required

None.（レビュー済みの UsageQuery 可変長 API と 3 階層 example。）

## Requirement Traceability

| Requirement | Implementation Point |
| :--- | :--- |
| 000 R3 クエリ | agentservice handler_usage + filterTurns |
| 000 R3b Client GetUsage opts | client/v1/usage.go UsageQuery |
| 000 R7 ended_at（時刻フィルタ用） | TurnUsageRecord.EndedAt + persist |
| 001 R1–R6 example | examples/token-usage/* |
| Docs | ReferenceManual-WebAPIs.md |

## Proposed Changes

### codingagent

#### [MODIFY] [usage.go](file://shared/libs/go/codingagent/usage.go) + usage_test.go（先にテスト）

*   **Technical Design**:

```go
type TurnUsageRecord struct {
	TurnID  string       `json:"turn_id"`
	EndedAt time.Time    `json:"ended_at,omitempty"`
	Usage   TokenUsage   `json:"usage"`
	Calls   []TokenUsage `json:"calls,omitempty"`
}

// UsageQuery filters turns for GET /usage (shared so server can reuse).
type UsageQuery struct {
	LastN       int       `json:"-"`
	AfterTurnID string    `json:"-"`
	FromTurnID  string    `json:"-"`
	ToTurnID    string    `json:"-"`
	Since       time.Time `json:"-"`
	Until       time.Time `json:"-"`
}

// FilterTurnUsage applies UsageQuery to turns (order preserved = append order).
// LastN applied after other filters. Empty query returns turns unchanged.
func FilterTurnUsage(turns []TurnUsageRecord, q UsageQuery) []TurnUsageRecord

// SumTurnUsage sums turn.Usage into TokenUsage with source=derived_session_sum, confidence=high.
func SumTurnUsage(turns []TurnUsageRecord) TokenUsage
```

*   **Logic**: AfterTurnID = index of id の次から末尾。From/To = id の inclusive slice（見つからなければ空）。Since/Until = EndedAt がゼロならそのターンは時刻フィルタ対象外で除外（strict）。LastN = 末尾 N。

### agentservice

#### [MODIFY] [usage_store.go](file://shared/libs/go/agentservice/usage_store.go)

*   **Logic**: `appendTurnUsage` で `EndedAt` 未設定なら `time.Now().UTC()` をセット。

#### [MODIFY] [handler_usage.go](file://shared/libs/go/agentservice/handler_usage.go) + handler_usage_test.go（先にテスト）

*   **Logic**: `r.URL.Query()` から UsageQuery 構築。
  - `last_n` int
  - `after_turn_id`, `from_turn_id`, `to_turn_id` string
  - `since`, `until` RFC3339
  - 不正 `last_n` → 400
  - `FilterTurnUsage` → `SumTurnUsage` を `rep.Usage` に設定。`GetSession` 累計は触らない。
  - クエリなし時: 従来どおり全 turns。トップ `usage` は record.Usage（セッション累計）を優先してよいが、**フィルタ時は必ず再合計**。クエリなしでも一貫して turns 再合計でも可（実装は: クエリ空なら既存 record.Usage、フィルタ時は再合計）。

#### [MODIFY] handler_usage_test.go

*   2 ターン永続後 `?last_n=1` で turns len=1、usage が末尾ターンと一致。

### client/v1

#### [MODIFY] [usage.go](file://client/v1/usage.go) + usage_test.go（先にテスト）

```go
type UsageQuery struct {
	LastN       int
	AfterTurnID string
	FromTurnID  string
	ToTurnID    string
	Since       time.Time
	Until       time.Time
}

func (c *Client) GetUsage(ctx context.Context, sessionID string, opts ...UsageQuery) (*SessionUsageReport, error)
func (s *Session) GetUsage(ctx context.Context, opts ...UsageQuery) (*SessionUsageReport, error)
```

*   **Logic**: `opts` 先頭のみ使用。クエリを URL にエンコード。既存呼び出し `GetUsage(ctx, id)` / `sess.GetUsage(ctx)` は互換。

#### [MODIFY] TurnUsageRecord に `EndedAt` を client 側にも追加。

### examples/token-usage

#### [NEW] main.go, go.mod, README.md

*   **Logic**: 001 仕様の R2 フロー。`claudecode` デフォルト。見出し Session / Turn / LLM call / LastN=1。

### docs

#### [MODIFY] ReferenceManual-WebAPIs.md — usage クエリパラメータと Client 例。

### tests

#### [MODIFY] token_usage_e2e_test.go — `?last_n=1` または client `UsageQuery{LastN:1}` を1ケース追加。

## Step-by-Step Implementation Guide

- [x] 1. FilterTurnUsage / SumTurnUsage (TDD) in codingagent
- [x] 2. Persist EndedAt; handler query parsing + unit tests
- [x] 3. client UsageQuery + GetUsage variadic
- [x] 4. examples/token-usage
- [x] 5. Docs + E2E LastN
- [x] 6. build.sh && integration_test.sh --specify TokenUsage
- [x] 7. commit / push

## Verification Plan

### Automated Verification

1. `./scripts/process/build.sh`
2. `./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TokenUsage"`

### E2E Tests

- 既存 `TestClaudeCodeE2E_TokenUsage_TurnAndSession` 維持
- `TestClaudeCodeE2E_TokenUsage_LastN` または同テスト内で LastN=1 を assert

## Documentation

- `docs/ReferenceManual-WebAPIs.md`
- `examples/token-usage/README.md`
