# 001: Token Usage Example（examples/token-usage）

## 背景 (Background)

### 何を見せたいか（目的）

トークン計測は **3 つの粒度** で取れる。example の目的は、その 3 つを **同じ実行の中で順番に取得・表示** し、利用者が「どこを見ればよいか」を一発で分かること。

| 粒度 | 意味 | 主な取り方（Client API） |
| :--- | :--- | :--- |
| **セッション単位** | その HTTP セッション全体の累計 | `GetSession().Usage` または `GetUsage(ctx)`（クエリなし）の `Usage` |
| **ターン単位** | 1 回の SendMessage 分の合計 | ストリーム終端 `result.Usage`、または `GetUsage(...).Turns[i].Usage` |
| **LLMコール単位** | エージェントループ内の各モデル応答 | `GetUsage(...).Turns[i].Calls[]`（無い場合あり・低信頼可） |

加えて **直近ターンだけ** 取る書き方として `GetUsage(ctx, UsageQuery{LastN: 1})` を必ずデモする（000 の R3b）。

現状 `examples/` にこの 3 階層を示すサンプルはなく、仕様書や E2E だけでは「どう書くか」が伝わりにくい。

### 本仕様で決めること

1. `examples/token-usage/` を追加する。
2. 出力を **Session / Turn / LLM call** の 3 セクションに分けて必ず表示する。
3. コールが 0 件でも「calls: (none)」と明示し、欠落を隠さない。
4. `GetUsage(ctx)`（全件）と `GetUsage(ctx, UsageQuery{LastN: 1})`（直近）の両方を示す。

### スコープ外

- LLMGP への追加実装（既存 API のデモに限定）。
- GUI。
- `total_cost_usd` を請求に使うこと（表示する場合は estimate と明記）。

---

## 要件 (Requirements)

### 必須要件 (Must)

#### R1: 成果物

| パス | 内容 |
| :--- | :--- |
| `examples/token-usage/main.go` | 3 階層の取得・表示デモ |
| `examples/token-usage/go.mod` | 他 example と同様 |
| `examples/token-usage/README.md` | 英語。3 階層の対応表と実行方法、`UsageQuery` 例 |

`build.sh` の example ビルド対象に含まれること。

#### R2: デモの流れ（学習順）

> **Superseded by [002-Token-Usage-Stream-UX.md](file://prompts/phases/001-phase02/branches/feat-token-counter/ideas/002-Token-Usage-Stream-UX.md)**: Send 直後の `result.Usage` 表示（旧ステップ 3–5）は削除。`stream.Output` + 完了後 `GetUsage` に一本化。

```text
1. CreateSession
2. SendText #1（できればツールを1回使う短い指示）→ ストリーム消費
3. 【表示 A】ターン単位 … result.Usage（SendMessage #1）
4. SendText #2（短い返信）→ ストリーム消費
5. 【表示 B】ターン単位 … result.Usage（SendMessage #2）
6. GetUsage(ctx) … 全件
7. 【表示 C】セッション単位 … report.Usage（および GetSession().Usage で突合）
8. 【表示 D】ターン単位（永続・全件）… report.Turns[i].Usage
9. 【表示 E】LLMコール単位 … report.Turns[i].Calls[j]（空なら none）
10. GetUsage(ctx, UsageQuery{LastN: 1}) … 直近1ターンのみ
11. 【表示 F】「LastN=1」の turns / usage（フィルタ後合計）を Session/Turn/Call 見出しで再掲または短い差分セクションで示す
12. Terminate
```

2 ターン送る理由: セッション合計が「ターンの和」であること、および `LastN: 1` が末尾だけになることが目で分かること。

#### R3: 出力フォーマット

```text
=== Session usage ===
...
=== Turn usage ===
...
=== LLM call usage ===
... or (none for this turn)

=== LastN=1 (GetUsage with UsageQuery) ===
=== Session usage ===   ← フィルタ後の usage（返却 turns の再合計）
=== Turn usage ===
=== LLM call usage ===
```

#### R4: API 対応の明示

| 表示セクション | Client / イベント |
| :--- | :--- |
| Session usage（全件） | `sess.GetUsage(ctx)` → `.Usage`、および `GetSession().Usage` |
| Turn / Call（全件） | `GetUsage(ctx).Turns` |
| 直近のみ | `sess.GetUsage(ctx, client.UsageQuery{LastN: 1})` |

#### R5: エージェント

- デフォルト: `claudecode`。
- 引数: `[server-url] [agent] [model]`（省略時 URL=`http://localhost:3100`）。
- 1 ターン目はツールを1回使う短い指示を推奨。calls 空なら `(none)`。

#### R6: 失敗時

- サーバ不通・CreateSession 失敗: `log.Fatalf`。
- usage 欠落時はどの段が空かを見出し付きで示す。

### 任意要件 (Should / May)

#### S1: JSON 1 行ダンプ
#### S2: example 内の軽いテスト

---

## 実現方針 (Implementation Approach)

前提: 000 の R3 / R3b（`UsageQuery`・クエリ付き `GET .../usage`）が実装済み、または本作業と同じ実装計画で先行実装する。

```go
repAll, _ := sess.GetUsage(ctx)
repLast, _ := sess.GetUsage(ctx, client.UsageQuery{LastN: 1})
```

WorkDir は一時ディレクトリ。`minimal-client` を雛形にする。

---

## 検証シナリオ (Verification Scenarios)

### V1: 3 階層 + LastN
1. サーバ起動 → `go run .`
2. Session / Turn / LLM call 見出しが出る
3. ターン 2 件以上
4. `LastN=1` セクションで turns が 1 件（または末尾のみ）

### V2: セッション合計
全件 `GetUsage` の session usage ≈ 各ターン和。`GetSession().Usage` と一致。

### V3: ビルド
`build.sh` で example 成功。

---

## テスト項目 (Testing)

```bash
./scripts/process/build.sh
./scripts/process/integration_test.sh --specify "TokenUsage"
```

| ID | 基準 |
| :--- | :--- |
| A1 | `examples/token-usage` + README（3 階層 + UsageQuery） |
| A2 | Session / Turn / LLM call 見出し |
| A3 | `GetUsage(ctx)` と `GetUsage(ctx, UsageQuery{LastN:1})` の両方を呼ぶ |
| A4 | 2 ターン送信 |
| A5 | `build.sh` + 既存 TokenUsage E2E |

---

## 参考 (References)

- 親仕様: [000-Token-Usage-Metering.md](file://prompts/phases/001-phase02/branches/feat-token-counter/ideas/000-Token-Usage-Metering.md)（R3 / R3b）
- 雛形: [examples/minimal-client/main.go](file://examples/minimal-client/main.go)
- Client: `client/v1/usage.go`
