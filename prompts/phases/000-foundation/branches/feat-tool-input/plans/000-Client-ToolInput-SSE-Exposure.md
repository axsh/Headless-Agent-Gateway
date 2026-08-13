# 000-Client-ToolInput-SSE-Exposure

> **Source Specification**: `prompts/phases/000-foundation/branches/feat-tool-input/ideas/000-Client-ToolInput-SSE-Exposure.md`

## Goal Description

[Issue #36](https://github.com/axsh/arctic-tern/issues/36) 対応。AgentService SSE wire 上の `tool_input` を公式 Go クライアント（`client/v1` および legacy `client`）が落としている不完全さを修正する。

必須スコープ **R1–R7**:

1. `Event.ToolInput` 追加と SSE デコード伝播
2. `OnToolUse(toolName, toolInput)` への破壊的変更（互換ラッパなし）
3. `Output()` の `command` / `path` 一行サマリ
4. README / 短文ドキュメントでの告知
5. 単体テスト（TDD）と決定的な `tests/` 経路テスト

**非対象**: サーバー変更、`tool_input` の切り詰め・チャンク化、プライバシーマスク。

## User Review Required

None.（仕様合意済み。クライアント API 互換は無視、R5–R7 は必須。）

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1: `client/v1.Event` に `ToolInput map[string]any` | Proposed Changes → `client/v1/stream.go` |
| R2: SSE パーサが `tool_input` を Event に載せる | Proposed Changes → `client/v1/stream.go` `events()` |
| R3: legacy `client` 同等露出 | Proposed Changes → `client/stream.go` |
| R4: wire→Event 単体テスト（4ケース） | Proposed Changes → `client/v1/stream_test.go`, `client/stream_test.go` |
| R5: `OnToolUse` / `StreamHandlers.OnToolUse` を `func(toolName string, toolInput map[string]any)` に変更。互換ラッパなし | Proposed Changes → `client/v1/stream.go`, `client/stream.go` + 呼び出し追随 |
| R6: `Output()` が `command` / `path` サマリ（全文ダンプ禁止） | Proposed Changes → 両 `stream.go` + Output テスト |
| R7: ドキュメント / 破壊的変更告知 | Proposed Changes → `README.md`, `docs/client-sse-tool-input.md` |
| VS1: モック SSE で Events / OnToolUse / Output 検証 | Step-by-Step + Verification Plan（単体） |
| VS2: 実エージェント E2E は任意（必須ゲートにしない） | Verification Plan に記載のみ。必須 E2E は決定的 httptest 経路 |
| 非要件: サーバー変更・切り詰め・チャンク化なし | Goal / Out of Scope（実装しない） |

---

## Proposed Changes

> TDD: 各パッケージで **`_test.go` を先に追加し RED を確認**してから実装ファイルを変更する。

### 1. client/v1 — 単体テスト（先）

#### [MODIFY] [client/v1/stream_test.go](file://client/v1/stream_test.go)

*   **Description**: R4 / R5 / R6 / VS1。モック SSE で `ToolInput` 伝播・コールバック・Output を検証する。
*   **Technical Design**: `v1.NewStreamFromReader` または httptest + `http.Get` で SSE 本文を供給（既存 `TestStream_Events_ReassemblesToolResultParts` と同型）。
*   **Logic / 追加テスト**:

```go
func TestStream_Events_ToolUseIncludesToolInput(t *testing.T)
```

テーブル駆動（またはサブテスト）:

| name | SSE `data:` JSON | 期待 |
|------|------------------|------|
| with_command | `{"type":"tool_use","tool_name":"command_execution","tool_input":{"command":"ls -la"}}` | `Type==EventToolUse`, `ToolName=="command_execution"`, `ToolInput["command"]=="ls -la"` |
| missing_tool_input | `{"type":"tool_use","tool_name":"Bash"}` | `ToolName=="Bash"`, `ToolInput == nil` |
| empty_tool_input | `{"type":"tool_use","tool_name":"Bash","tool_input":{}}` | `ToolInput != nil` かつ `len(ToolInput)==0` |
| nested | `{"type":"tool_use","tool_name":"apply_patch","tool_input":{"changes":[{"path":"a.go"}]}}` | `ToolInput["changes"]` が slice で、先頭要素の `path` が `"a.go"`（`[]any` / `map[string]any` として取り出せること） |
| non_tool_use | `{"type":"text","content":"hi"}` | `ToolInput == nil` |

各ケース末尾に `data: {"type":"result"}\n\n` と `data: [DONE]\n\n` を付ける。

```go
func TestStream_RunWithHandlers_OnToolUseReceivesToolInput(t *testing.T)
```

- SSE: `tool_use` + `tool_input: {"command":"echo hi"}` → `result` → `[DONE]`
- `StreamHandlers{ OnToolUse: func(name string, input map[string]any) { ... } }`
- 期待: `name=="command_execution"`（または供給した tool_name）、`input["command"]=="echo hi"`

```go
func TestStream_OnToolUse_RunReceivesToolInput(t *testing.T)
```

- `stream.OnToolUse(func(name string, input map[string]any){...}).Run()` でも同様に渡ることを検証

```go
func TestStream_Output_ToolUseSummaryCommandAndPath(t *testing.T)
```

| SSE tool_input | Output に含まれる文字列 | 含まれないこと |
|----------------|-------------------------|----------------|
| `{"command":"ls -la"}` | `[Tool: command_execution]` と `command=ls -la` | `tool_input` JSON 全体のダンプ、巨大キーの列挙 |
| `{"path":"main.go"}`（command なし） | `[Tool: Write]` と `path=main.go` | 同上 |
| `{"command":"x","path":"y"}` | `command=x` を優先（path は出さない） | `path=y` |
| tool_input なし / 対象キーなし | `\n[Tool: SomeTool]\n`（従来相当） | `command=` / `path=` |

---

### 2. client/v1 — 実装

#### [MODIFY] [client/v1/stream.go](file://client/v1/stream.go)

*   **Description**: R1 / R2 / R5 / R6。Event・パーサ・コールバック・Output を更新。
*   **Technical Design**:

`Event`（仕様どおり `map[string]any`）:

```go
type Event struct {
	Type              EventType
	Text              string
	ToolName          string
	ToolInput         map[string]any
	Error             string
	UserInputRequired UserInputRequiredEvent
}
```

`StreamHandlers`:

```go
type StreamHandlers struct {
	OnText              func(text string)
	OnToolUse           func(toolName string, toolInput map[string]any)
	OnToolResult        func(content string)
	OnUserInputRequired func(ev UserInputRequiredEvent) (response string, err error)
	OnError             func(err string) error
	OnResult            func()
}
```

`Stream` 内部フィールドと setter:

```go
onToolUse func(toolName string, toolInput map[string]any)

func (s *Stream) OnToolUse(fn func(toolName string, toolInput map[string]any)) *Stream
```

*   **Logic**:

1. **`events()` raw struct** にフィールド追加:

```go
var raw struct {
	Type      string         `json:"type"`
	Content   string         `json:"content"`
	ToolName  string         `json:"tool_name,omitempty"`
	ToolInput map[string]any `json:"tool_input,omitempty"`
	PromptID  string         `json:"prompt_id,omitempty"`
	Choices   []string       `json:"choices,omitempty"`
	ChunkID   string         `json:"chunk_id,omitempty"`
	Index     int            `json:"index,omitempty"`
	Total     int            `json:"total,omitempty"`
}
```

2. 汎用 Event 構築箇所:

```go
ev := Event{
	Type:      EventType(raw.Type),
	Text:      raw.Content,
	ToolName:  raw.ToolName,
	ToolInput: raw.ToolInput,
}
```

3. `tool_result` / `tool_result_part` 分岐は **変更しない**（再構成ロジックそのまま）。そこで作る `Event{Type: EventToolResult, ...}` は `ToolInput` を設定しなくてよい（zero value `nil`）。

4. **`Run` / `RunWithHandlers`**:

```go
case EventToolUse:
	if s.onToolUse != nil {
		s.onToolUse(ev.ToolName, ev.ToolInput)
	}
// ...
case EventToolUse:
	if h.OnToolUse != nil {
		h.OnToolUse(ev.ToolName, ev.ToolInput)
	}
```

5. **`Output()` サマリ** — 全文ダンプ禁止。優先キー順は固定 `command` → `path`（文字列かつ非空のみ）:

```go
case EventToolUse:
	fmt.Fprint(w, formatToolUseLine(ev.ToolName, ev.ToolInput))
```

同一ファイル内の未公開ヘルパー:

```go
func formatToolUseLine(toolName string, toolInput map[string]any) string {
	for _, key := range []string{"command", "path"} {
		if toolInput == nil {
			break
		}
		if v, ok := toolInput[key]; ok {
			if s, ok := v.(string); ok && s != "" {
				return fmt.Sprintf("\n[Tool: %s] %s=%s\n", toolName, key, s)
			}
		}
	}
	return fmt.Sprintf("\n[Tool: %s]\n", toolName)
}
```

- `command` と `path` が両方ある場合は **`command` のみ** 表示
- その他のキー（`changes`, `arguments` 等）は **表示しない**
- 切り詰め・マスクはしない（表示対象外にするだけ）

旧シグネチャ併存・互換ラッパは **作らない**。

---

### 3. legacy client — 単体テスト（先）

#### [MODIFY] [client/stream_test.go](file://client/stream_test.go)

*   **Description**: R3 / R4 / R5 / R6 の legacy 同等テスト。
*   **Technical Design**: 既存 `TestStream_Output` と同様に `newStream(io.NopCloser(strings.NewReader(sseData)))` を使う（legacy に `NewStreamFromReader` が無い場合は現状どおり unexported `newStream`）。
*   **Logic / 追加テスト**:

```go
func TestStream_Events_ToolUseIncludesToolInput(t *testing.T)
func TestStream_OnToolUse_RunReceivesToolInput(t *testing.T)
func TestStream_Output_ToolUseSummaryCommandAndPath(t *testing.T)
```

ケース内容は v1 と同型（パッケージは `client`）。legacy に `RunWithHandlers` / `StreamHandlers` が無い場合は `OnToolUse` + `Run` のみで R5 をカバーする。

既存 `TestStream_Output` で tool_use を含まないケースは変更不要。tool_use を含む既存テストがあれば期待文字列を新フォーマットに合わせて更新する。

---

### 4. legacy client — 実装

#### [MODIFY] [client/stream.go](file://client/stream.go)

*   **Description**: R3 / R5 / R6。v1 と同一の意味論。
*   **Technical Design**:

```go
type Event struct {
	Type      EventType
	Text      string
	ToolName  string
	ToolInput map[string]any
	Error     string
}

onToolUse func(toolName string, toolInput map[string]any)

func (s *Stream) OnToolUse(fn func(toolName string, toolInput map[string]any)) *Stream
```

*   **Logic**:
    - `events()` raw struct に `ToolInput map[string]any \`json:"tool_input,omitempty"\``
    - Event 構築時に `ToolInput: raw.ToolInput`
    - `Run` で `s.onToolUse(ev.ToolName, ev.ToolInput)`
    - `Output` で v1 と同じ `formatToolUseLine`（legacy パッケージ内に同名・同ロジックのヘルパーを置く。共有パッケージ抽出は必須としない）
    - `tool_result` 再構成分岐は変更しない

---

### 5. リポジトリ内呼び出し追随

現状 `OnToolUse:` を明示設定している本番/テストコードはほぼ無く、`StreamHandlers{...}` は `OnUserInputRequired` / `OnText` 等のみ。シグネチャ変更後:

- **コンパイルエラーになる箇所だけ** 修正する（`OnToolUse` を設定しているクロージャがあれば第二引数を追加）
- 対象候補（変更時点で再検索）:
  - `tests/codex_client_v1_large_output_e2e_test.go`
  - `tests/codex_real_large_output_e2e_test.go`
  - `tests/interactive_agent_test.go`
  - `client/v1/stream_test.go`
  - `examples/**`（該当すれば）

互換ラッパは作らない。

---

### 6. 決定的 E2E（tests/、LLM なし）

#### [NEW] [tests/client_v1_tool_input_sse_test.go](file://tests/client_v1_tool_input_sse_test.go)

*   **Description**: VS1 を `tests/` 配下でも検証する。httptest が `/api/v1/sessions/{id}/messages` 相当で SSE を返し、`client/v1` の `SendText` → `Events()` / `RunWithHandlers` で `tool_input` を読む。LLM・実エージェント不要。
*   **Technical Design**:

```go
func TestClientV1_SSE_ToolUseExposesToolInput(t *testing.T) {
	// httptest: POST .../messages →
	//   data: {"type":"tool_use","tool_name":"command_execution","tool_input":{"command":"ls -la"}}
	//   data: {"type":"result"}
	//   data: [DONE]
	// client := v1.New(srv.URL, v1.WithNoTimeout())
	// sess := v1.ResumeSession(client, "sess-tool-input")
	// stream, err := sess.SendText(ctx, "ignored")
	// for ev := range stream.Events() { ... assert ToolInput }
}

func TestClientV1_SSE_OnToolUseReceivesToolInput(t *testing.T) {
	// 同上 SSE。RunWithHandlers + OnToolUse で command を受け取る
}
```

既存 `client/v1/stream_test.go` の httptest パターン（`SendText` / `ResumeSession`）を踏襲する。

**VS2（実 Codex/Claude）は必須ゲートにしない。** 本ファイルで自動化ゲートを満たす。

---

### 7. ドキュメント（R7）

#### [NEW] [docs/client-sse-tool-input.md](file://docs/client-sse-tool-input.md)

*   **Description**: `tool_use` の `ToolInput` が wire と対応すること、切り詰めしないこと、大きな payload は #26 系別途であること、`OnToolUse` 破壊的変更を記載。
*   **Logic（記載内容をそのまま書く）**:

```markdown
# client/v1: tool_input on tool_use SSE events

## Wire

AgentService SSE `data:` lines for `tool_use` may include:

```json
{"type":"tool_use","tool_name":"command_execution","tool_input":{"command":"ls -la"}}
```

`codingagent.StreamEvent` fields `tool_name` / `tool_input` are forwarded by AgentService.

## Official Go client

- `client/v1.Event.ToolInput` (`map[string]any`) is populated from JSON `tool_input`.
- Legacy package `github.com/axsh/arctic-tern/client` behaves the same.
- The client does **not** truncate or chunk `tool_input`. Oversized SSE lines remain a separate concern (see Issue #26 / `docs/sse-chunk-protocol.md` for `tool_result` chunking).

## Breaking change

`OnToolUse` / `StreamHandlers.OnToolUse` signature:

```go
// before
func(toolName string)
// after
func(toolName string, toolInput map[string]any)
```

No compatibility wrapper is provided.
```

#### [MODIFY] [README.md](file://README.md)

*   **Description**: Go client の Interactive / StreamHandlers 例付近に、`OnToolUse` と `ToolInput` の短い言及と、`docs/client-sse-tool-input.md` へのリンクを追加。破壊的変更を一文で告知。
*   **Logic**: 既存 `StreamHandlers` サンプルに任意で次を追記可能:

```go
OnToolUse: func(toolName string, toolInput map[string]any) {
    fmt.Printf("tool=%s input=%v\n", toolName, toolInput)
},
```

CHANGELOG ファイルがリポジトリに無いため、**破壊的変更の告知は `docs/client-sse-tool-input.md` + README** で R7 を満たす（新規 `CHANGELOG.md` は必須としない）。

---

## Step-by-Step Implementation Guide

1. [x] **RED — v1 単体テスト追加**: Edit `client/v1/stream_test.go` に `TestStream_Events_ToolUseIncludesToolInput` / `TestStream_RunWithHandlers_OnToolUseReceivesToolInput` / `TestStream_OnToolUse_RunReceivesToolInput` / `TestStream_Output_ToolUseSummaryCommandAndPath` を追加。`./scripts/process/build.sh` で **失敗することを確認**。
2. [x] **GREEN — v1 実装**: Edit `client/v1/stream.go` — `Event.ToolInput`、raw Unmarshal、`OnToolUse` / `StreamHandlers` / `Run` / `RunWithHandlers`、`formatToolUseLine` + `Output`。再実行で v1 テスト PASS。
3. [x] **RED — legacy 単体テスト追加**: Edit `client/stream_test.go` に同等テストを追加し、失敗を確認。
4. [x] **GREEN — legacy 実装**: Edit `client/stream.go` を v1 と同意味論で更新。PASS を確認。
5. [x] **Compile fix**: リポジトリ内の `OnToolUse` / `StreamHandlers` 破壊箇所を修正（あれば）。
6. [x] **E2E（決定的）**: Add `tests/client_v1_tool_input_sse_test.go`（httptest + `client/v1`）。
7. [x] **Docs**: Add `docs/client-sse-tool-input.md`、Edit `README.md`。
8. [x] **Verify**: 下記 Verification Plan のコマンドを実行。

---

## Verification Plan

### Automated Verification

本リポジトリの `scripts/process/integration_test.sh` は **`--specify` のみ**（`--categories` なし）。Backend 検証は次で行う。

1. **Build & Unit Tests**:

```bash
./scripts/process/build.sh
```

2. **Integration / E2E（決定的 + 回帰）**:

```bash
./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestClientV1_SSE_ToolUseExposesToolInput|TestClientV1_SSE_OnToolUseReceivesToolInput|TestCodexClientV1|TestAgentService|TestStream_Events_ReassemblesToolResultParts"
```

（Windows ローカル。Linux / Remote-SSH の場合はプロジェクト規則どおり `build.sh --skip-etc` および `xvfb-run -a` ラップを適用。）

3. **E2E Tests（本計画で追加）**:
   - `tests/client_v1_tool_input_sse_test.go`
     - `TestClientV1_SSE_ToolUseExposesToolInput`
     - `TestClientV1_SSE_OnToolUseReceivesToolInput`
   - 実エージェント VS2 は必須ゲートに含めない

### 単体でカバーする仕様ケース（再掲）

| ID | 検証 |
|----|------|
| R4-a | tool_input あり → Events で参照可 |
| R4-b | tool_input なし → ToolInput nil |
| R4-c | tool_input `{}` → 空 map |
| R4-d | ネスト配列/オブジェクト保持 |
| R4-e | 非 tool_use → ToolInput nil |
| R5 | OnToolUse / RunWithHandlers に map が渡る |
| R6 | command 優先、path フォールバック、全文ダンプなし |

---

## Documentation

| ファイル | 内容 |
|----------|------|
| `docs/client-sse-tool-input.md` | wire 対応、非切り詰め、#26 参照、OnToolUse 破壊的変更 |
| `README.md` | StreamHandlers 例 / リンク / 破壊的変更の短文告知 |

サーバー API リファレンス（`docs/ReferenceManual-WebAPIs.md`）の wire 契約変更は不要（既に `tool_input` が載る前提のため、クライアント側ドキュメントのみ）。
