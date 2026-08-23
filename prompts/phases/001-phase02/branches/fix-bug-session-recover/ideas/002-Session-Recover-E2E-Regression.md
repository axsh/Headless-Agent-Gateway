# 002: Phase 3 — Follow / sandbox 拒否の E2E 回帰テスト

> **関連 Issue**: [axsh/arctic-tern#51](https://github.com/axsh/arctic-tern/issues/51)
>
> **実装フェーズ**: Phase 3（P1）。**Phase 1・2 完了後** に実装する。
>
> **前提仕様**:
> - [000-Codex-Sandbox-Rejection-ToolResult.md](./000-Codex-Sandbox-Rejection-ToolResult.md)
> - [001-Agentservice-SSE-Terminal-Guarantee.md](./001-Agentservice-SSE-Terminal-Guarantee.md)

## 背景 (Background)

Issue #51 の Acceptance Criteria はエンドツーエンドの契約である:

- `rm -f` 再現で SSE に `tool_result`（自然または合成）
- Follow / `FollowFrom` が拒否テキストを **数分の無音なし** で受信
- ターンは `result` または明示 non-retryable `error` で終わる（stream close のみ不可）
- arctic-tern に回帰テストを追加

Phase 1・2 でアダプタと agentservice を修正しても、**統合テストが無ければ再発を防げない**。本 Phase は Issue #51 クローズ用の検証層を固定する。

---

## 要件 (Requirements)

### 必須要件 (Must)

#### R1: モック統合テスト（必須ゲート）

- `tests/` に新規テストを追加する。パッケージは既存の `llm_test`（`common_session_follow_test.go` と同様）または `agentservice` 統合パッケージ。
- テスト名プレフィックス: `TestSessionRecover`（`TestSessionFollow` と衝突しない）。
- fake / stub agent が次のシーケンスを再現する:
  1. `EventToolUse`（`command_execution`）
  2. `EventToolResult`（`Rejected(...)` テキスト）— Phase 1 相当の合成結果を模倣
  3. ch クローズ（`EventResult` 無し）— Phase 2 の terminal 合成をトリガー
- `POST /messages` で先頭イベントを読んだあと切断し、`FollowFrom` で残りを取得する（`feat-reconnect-session` シナリオ A 相当）。
- 検証:
  - SSE に `tool_result` 型（wire JSON の `type`）が含まれる
  - 拒否テキストの一部（例: `rm -f` または `Rejected`）が body に含まれる
  - 最終的に `error` または `result` と `[DONE]` を受信
  - **イベント間の待ちが 90 秒猶予フルではない**（テストは数秒以内で完了。`WithSSEDrainTimeout` で短縮可）

#### R2: Codex testfake 統合（必須ゲート）

- `codex/testfake` を使い、stderr に sandbox 拒否メッセージ + exit 1 を設定した統合テストを 1 件追加する（`tests/codex_session_recover_test.go` 等）。
- 検証: in-process `agentservice` + codex agent 経路で `tool_result` が task log または SSE に現れる。
- 実 `codex` CLI バイナリは **不要**（testfake が PATH の `codex` を置換）。

#### R3: 回帰: 既存 Follow テスト

- 既存 `TestSessionFollow_*` および `client/v1` の follow テストがすべて PASS のままであること。

#### R4: Issue #51 Acceptance Criteria の写像

| Issue AC | 本 Phase の検証 |
| :--- | :--- |
| repro で SSE `tool_result` | R1 + R2 + **R5**（実 CLI） |
| Follow が無音ギャップなし | R1 + **R5** |
| explicit terminal | R1 + **R5** の `[DONE]` + error/result |
| regression test in arctic-tern | R1, R2, **R5** |
| 公開 API 契約の文書化 | **R6** |

#### R5: 実 Codex CLI ライブテスト（必須ゲート）

- 実 `codex` CLI と LLM Gateway（または Tern が要求する認証）が揃った環境で E2E を実行する。**リリース品質の最終ゲート** とする。
- `t.Skip` **禁止**。前提欠落（`codex` 不在、認証不可、gateway 未起動等）は **Fail** とする。
- テスト名: `TestSessionRecoverLive_CodexSandboxReject`（`tests/session_recover_live_test.go` 等）。
- Issue #51 再現に沿い、プロンプトで `rm -f` を含む compound bash の実行を誘発する。例:
  ```bash
  curl -fsS http://127.0.0.1:8080/some/path -o /tmp/check.html; \
  rg -o 'href="[^"]+\.css' /tmp/check.html; \
  rm -f /tmp/check.html
  ```
- 検証（SSE 購読者として `POST /messages` または切断後 `FollowFrom`）:
  - `tool_result`（自然または Tern 合成）に拒否テキスト（`Rejected` または `rm -f` 関連）が含まれる
  - 数分の無音ギャップなし（テスト全体は妥当なタイムアウト内、例: 120 秒以内）
  - ターン終端が `result` または non-retryable `error` + `[DONE]`（無言 stream close のみ不可）
- 既存 `codex_e2e_test.go` が `LookPath` 失敗時に `Skip` するパターンとは **意図的に異なる**。本テストは CI／リリース検証で **常に実行・合格** が前提。

#### R6: ドキュメント（必須）

- `docs/ReferenceManual-WebAPIs.md` に次を **1 段落以上** 追記する:
  - Codex 等の tool 実行がサンドボックス／ポリシーで拒否された場合、Tern は可能な限り `tool_result` として下流へ渡す（stdout 無し時は合成）
  - ターンは必ず `result`、non-retryable `error`、または `data: [DONE]` で終端する（無言の stream close だけでは完了しない）
  - Follow / `FollowFrom` 購読者も同一契約を受ける
- 追記箇所は SSE イベント型または Follow API の節と整合させる。

---

## 実現方針 (Implementation Approach)

### 新規・変更ファイル（想定）

| ファイル | 内容 |
| :--- | :--- |
| `tests/session_recover_follow_test.go` | R1: httptest + fake agent + FollowFrom |
| `tests/codex_session_recover_test.go` | R2: testfake + agentservice |
| `tests/session_recover_live_test.go` | **R5: 実 Codex CLI ライブ E2E（必須）** |
| `docs/ReferenceManual-WebAPIs.md` | **R6: tool 拒否・終端契約の追記** |

### fake agent シーケンス（R1）

```go
ch <- EventToolUse{ToolName: "command_execution", ...}
ch <- EventToolResult{Content: "Rejected(\"rm -f style commands are not permitted\")"}
close(ch) // no EventResult — agentservice must synthesize terminal (Phase 2)
```

### testfake 設定例（R2）

```json
{
  "lines": [
    "{\"type\":\"item.started\",\"item\":{\"type\":\"command_execution\",\"command\":\"rm -f /tmp/x\"}}"
  ],
  "stderr": "ERROR ... Rejected(\"rm -f style commands are not permitted\")",
  "exit_code": 1
}
```

Phase 1 実装後は stdout `item.completed` 無しでも `EventToolResult` が relay に載ることを SSE で確認する。

---

## 検証シナリオ (Verification Scenarios)

Issue #51 の Steps to reproduce（原文）— Phase 3 完了後の **システム全体** での期待:

1. Start a Tern session with the `codex` agent and resume an existing conversation.
2. Send a user message that causes the model to run a compound bash command ending with cleanup via `rm -f`, for example:
   ```bash
   curl -fsS http://127.0.0.1:8080/some/path -o /tmp/check.html; \
   rg -o 'href="[^"]+\.css' /tmp/check.html; \
   rm -f /tmp/check.html
   ```
3. Observe Codex stderr and Tern SSE events until the turn ends or the stream closes.

### Issue #51 Expected behavior（本ブランチ完了後）

1. Codex emits stdout JSONL `item.completed` for `command_execution` with `aggregated_output` containing the rejection text (ideal, upstream Codex behavior), **or** Tern synthesizes an equivalent `EventToolResult` before treating the turn as failed.
2. Tern forwards a `tool_result` SSE event to subscribers (including late `FollowFrom` clients).
3. The model receives the rejection as tool output and may retry with a safer command in the **same turn** (上流 Codex がプロセスを維持する場合。プロセス死亡時は Tern process retry に依存）。
4. If the turn truly cannot continue, Tern emits an explicit terminal event (`error` non-retryable or `result`), not silent stream close.

### シナリオ C: FollowFrom で拒否後 terminal（必須・モック）

1. fake agent が R1 シーケンスを送る。
2. `POST /messages` で `EventToolUse` まで受信し HTTP 切断。
3. `GET .../events?from=0` または適切な `from` で Follow。
4. `tool_result`（拒否テキスト）→ terminal `error` または `result` → `[DONE]`。
5. 90 秒待機なし（テスト全体 < 30 秒）。

### シナリオ D: 実 Codex CLI で sandbox 拒否（必須・ライブ）

1. 実 `codex` + gateway + Tern server を起動する。
2. `codex` セッションを作成し、Issue #51 相当の compound bash（末尾 `rm -f`）を誘発するプロンプトを送る。
3. SSE（または途中切断後 `FollowFrom`）で以下を確認する:
   - `tool_result` に拒否テキスト
   - 長時間のイベント無音がない
   - `result` または `error` + `[DONE]` で終端
4. `TestSessionRecoverLive_CodexSandboxReject` がこのシナリオを自動化する。

---

### 統合テスト（必須ゲート）

```bash
./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestSessionRecover"
```

検証すること:

- `TestSessionRecover_FollowReceivesToolResultAndTerminal`: シナリオ C（R1）
- `TestSessionRecover_CodexTestfakeSandboxReject`: R2
- 既存 `TestSessionFollow_*` が同じパイプラインで PASS（R3）

Windows（Git Bash）では:

```bash
./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestSessionRecover"
```

Linux / Remote-SSH（Linux）では:

```bash
./scripts/process/build.sh --skip-etc && xvfb-run -a ./scripts/process/integration_test.sh --specify "TestSessionRecover"
```

### ライブ統合（必須ゲート・R5）

```bash
./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestSessionRecoverLive"
```

検証すること:

- `TestSessionRecoverLive_CodexSandboxReject`: シナリオ D（実 `codex` CLI）

前提: 実 `codex` CLI、有効な API 認証、LLM Gateway（または Tern 既定の gateway 設定）、サンドボックスが `rm -f` を拒否する Codex 設定。欠落時は **Fail**（`t.Skip` 禁止）。

Linux / Remote-SSH（Linux）では:

```bash
./scripts/process/build.sh --skip-etc && xvfb-run -a ./scripts/process/integration_test.sh --specify "TestSessionRecoverLive"
```

**CI への含意**: 本ブランチのマージ／リリース前に、上記ライブテストが実行可能な環境（codex + gateway）で PASS すること。ローカル開発のみで Skip する運用は不可。

### ドキュメント（必須・R6）

- `docs/ReferenceManual-WebAPIs.md` の追記内容が R6 を満たすこと（レビューで目視確認）。

### ビルド

Phase 3 マージ前にフルビルド成功:

```bash
./scripts/process/build.sh
```

---

## 対象外

- Codex 上流が stdout `item.completed` を必ず出すようになる修正
- retryable upstream overload の別 Issue 対応
- ternctl / GUI の表示変更

## 完了条件（Acceptance Criteria）

- [ ] Issue #51 の 4 項目 AC を `TestSessionRecover_*` および **ライブテスト** でカバー
- [ ] `./scripts/process/integration_test.sh --specify "TestSessionRecover"` が PASS
- [ ] `./scripts/process/integration_test.sh --specify "TestSessionRecoverLive"` が PASS（**R5**）
- [ ] `docs/ReferenceManual-WebAPIs.md` に R6 の契約が追記済み
- [ ] 既存 `TestSessionFollow_*` がリグレッションなし
- [ ] `./scripts/process/build.sh` 成功
- [ ] Issue #51 を Close 可能な状態

## 実装順序（ブランチ全体）

| 順序 | 仕様 | 概要 |
| :--- | :--- | :--- |
| 1 | 000 Phase 1 | Codex `tool_result` 合成 |
| 2 | 001 Phase 2 | SSE 終端保証 |
| 3 | 002 Phase 3 | E2E 回帰テスト（本ファイル） |
