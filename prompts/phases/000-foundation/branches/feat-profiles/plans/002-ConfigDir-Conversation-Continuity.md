# 002-ConfigDir-Conversation-Continuity

> **Source Specification**: [ideas/002-ConfigDir-Conversation-Continuity.md](file://prompts/phases/000-foundation/branches/feat-profiles/ideas/002-ConfigDir-Conversation-Continuity.md)  
> **Related**: [plans/001-ConfigDir-Switch-Same-Session.md](file://prompts/phases/000-foundation/branches/feat-profiles/plans/001-ConfigDir-Switch-Same-Session.md) (PATCH API 済み)

## Goal Description

命題「同一 Tern `session_id` で `config_dir` を切り替えつつ、同じセッションが継続した会話ができる」を、Claude / Codex の実 API LIVE で証明可能にする。ターン間 `terminate` を除去し、busy レースを製品側で直し、Codex は `exec resume` + `thread_id` 永続化で会話継続できるようにする。

## User Review Required

None. (仕様の User Review は承認済み: Codex 継続必須 / user-input 誘発プロンプトは LIVE で使わない)

## Requirement Traceability

| Requirement (from Spec) | Implementation Point |
| :--- | :--- |
| R1 会話継続の操作定義 (記憶トークン + config マーカー分離、terminate 禁止、agent_session_id 非空) | Proposed Changes > E2E LIVE rewrite |
| R2 命題経路から terminate 除去 | E2E: delete/replace `ensureSessionReadyForNextMessage` の terminate 用法 |
| R3 Claude LIVE P-CONT | `TestE2E_ConfigDir_Live_Claude_SwitchSameSession` 書き換え |
| R4 Codex LIVE P-CONT | `TestE2E_ConfigDir_Live_Codex_SwitchSameSession` + R6 |
| R5 busy を terminate なしで解消 | agentservice handler / unit+integration |
| R6 Codex resume (`exec resume` + thread_id → AgentSessionID) | codingagent/codex protocol + BuildArgs + adapter |
| R7 docs / mock 位置づけ | ReferenceManual, ternctl, integration コメント |
| P-BUSY | integration: 2 通目 without terminate |
| 受け入れ: RUN_CONFIG_DIR_LIVE=1 で Claude+Codex PASS | Verification Plan |

## Proposed Changes

### agentservice — busy レース解消 (R5)

#### [MODIFY] [shared/libs/go/agentservice/handler_test.go](file://shared/libs/go/agentservice/handler_test.go)

*   **Description**: terminate なしで同一 session に連続 SendMessage できることを先に失敗させる (TDD)。
*   **Technical Design**:
    ```go
    func TestHandleSendMessage_SecondMessageWithoutTerminate(t *testing.T) {
        // mock agent CreateSession + Send returns short EventText + EventResult then close
        // Create session → POST messages (SSE) drain to [DONE]
        // Immediately POST messages again (same id) WITHOUT terminate
        // Want: HTTP 200 (not 409 session busy)
    }

    func TestHandleSendMessage_NormalCompletionUnregistersBeforeClientReuse(t *testing.T) {
        // Same as above; optionally assert execRegistry.Get returns false after first stream ends
        // (export test helper or use second-message success as proxy)
    }
    ```

#### [MODIFY] [shared/libs/go/agentservice/handler.go](file://shared/libs/go/agentservice/handler.go)

*   **Description**: 正常完了時に `[DONE]` を送る前（または送った直後・ハンドラ復帰前の同期区間）で `execRegistry` から unregister し、クライアントが `[DONE]` 受信後すぐ次メッセージを送っても busy にならないようにする。suspended 経路は現状どおり exec を残す (命題 LIVE は user-input を出さない)。
*   **Logic** (推奨手順・要約禁止で具体化):
    1. `handleSendMessage` の成功完了パスで、`streamSSERelay` / `respondJSONRelay` が `suspended==false` で戻ったら、レスポンスを閉じる前に次を同期実行する:
       - `agentSess.Close()`
       - `UnregisterActiveSession(sessionID)`
       - `UnregisterExecCancel(sessionID)`
       - `execRegistry.Unregister(sessionID)`
       - multimodal temp cleanup if any
    2. 上記を `finishExecution` defer のみに頼らず、**`[DONE]` 送出と unregister の順序を明確化**する。実装案 A (推奨):
       - `streamSSERelay` の `done:` ラベルでは **status 更新のみ**行い、`[DONE]` は書かない
       - 呼び出し側で unregister 後に `fmt.Fprintf(w, "data: [DONE]\n\n"); flusher.Flush()`
       - suspended 早期 return のときだけ従来どおり `[DONE]` を relay 内で書き、exec は残す
    3. 実装案 B (代替): `execRegistry.Register` 先頭で、既存 exec の `relay.sourceDone==true` または status が completed/error なら先に `Unregister` してから Register。案 A を優先し、案 B は保険として追加可。
    4. Debug ログ: `session_id`, `unregistered_before_done`, `suspended`
*   **注意**: `terminate` API は残す (強制終了用途)。命題経路の正規手段にはしない。

#### [MODIFY] [tests/agentservice_integration_test.go](file://tests/agentservice_integration_test.go)

*   **Description**: P-BUSY — mock overlay サーバで terminate なし 2 通目。
*   **Technical Design**:
    ```go
    func TestAgentService_ConfigDir_SecondMessageWithoutTerminate(t *testing.T) {
        // setupConfigDirOverlayTestServer
        // Create with config_dir
        // postSessionMessage first
        // postSessionMessage second immediately — must not fail with busy
        // (postSessionMessage already Fatals on non-200)
    }
    ```
*   **Comment**: mock の SwitchSameSession テスト先頭コメントに「本テストは overlay/API 専用。会話継続の最終証明は LIVE (002)」と明記。

### Codex — thread_id 永続化と exec resume (R6-A)

#### [MODIFY] [shared/libs/go/codingagent/codex/protocol_test.go](file://shared/libs/go/codingagent/codex/protocol_test.go)

*   **Description**: `thread.started` が `EventSystem` + `SessionID=thread_id` になることを先に失敗させる。
*   **Technical Design**:
    ```go
    func TestParseExecEvent_ThreadStartedEmitsSystemSessionID(t *testing.T) {
        line := `{"type":"thread.started","thread_id":"abc-123"}`
        ev := ParseExecEvent(line)
        // want: ev != nil, Type==EventSystem, SessionID=="abc-123"
    }
    ```

#### [MODIFY] [shared/libs/go/codingagent/codex/protocol.go](file://shared/libs/go/codingagent/codex/protocol.go)

*   **Logic**:
    ```go
    case "thread.started", "turn.started":
        // thread.started: map to EventSystem so agentservice persists AgentSessionID
        if typ == "thread.started" && threadID != "" { // use parsed ThreadID from event struct
            return &codingagent.StreamEvent{
                Type:      codingagent.EventSystem,
                SessionID: threadID,
            }
        }
        return nil // turn.started stays no-op unless it also carries thread_id usefully
    ```
*   **継承**: `ExecEvent` 既存の `ThreadID string \`json:"thread_id,omitempty"\`` を使用。lifecycle を無視していた箇所を上記に変更。

#### [MODIFY] [shared/libs/go/codingagent/codex/process_test.go](file://shared/libs/go/codingagent/codex/process_test.go)

*   **Technical Design**:
    ```go
    func TestCodexBuildArgs_ResumeUsesExecResume(t *testing.T) {
        // BuildArgs with resumeSessionID "abc-123", prompt "hi", ignoreUserConfig false
        // want args contain: "exec", "resume", "abc-123"
        // want --json and --dangerously-bypass-approvals-and-sandbox still present
        // want NOT a bare "exec" without "resume" when resume id set
    }

    func TestCodexBuildArgs_NoResumeKeepsExec(t *testing.T) {
        // empty resume id → current behavior: exec --json ... -
    }
    ```

#### [MODIFY] [shared/libs/go/codingagent/codex/process.go](file://shared/libs/go/codingagent/codex/process.go)

*   **Technical Design**: `BuildArgs` シグネチャ拡張または SessionConfig から resume id を読む。
    ```go
    // BuildArgs builds CLI args for codex.
    // When resumeSessionID != "", uses: codex exec resume [common flags] <resumeSessionID> [-]
    // When resumeSessionID == "", uses: codex exec [common flags] [-]  (現行)
    func BuildArgs(prompt string, configOverrides []string, ignoreUserConfig bool, resumeSessionID string) []string
    ```
*   **Logic**:
    1. `common := []string{"--json", "--dangerously-bypass-approvals-and-sandbox"}` (+ ignore-user-config if needed) + configOverrides
    2. if `resumeSessionID != ""`:
       - `args = append([]string{"exec", "resume"}, common...)`
       - `args = append(args, resumeSessionID)`
       - if prompt != "" { args = append(args, "-") }
    3. else: 現行どおり `exec` + common + optional `-`
    4. `StartProcess` 内: `resumeSessionID := cfg.AgentSessionID` を `BuildArgs` に渡す
    5. Debug ログ: `resume_session_id`, `args`
*   **呼び出し更新**: `BuildArgs` の全呼び出し箇所 (process.go / process_test.go) を新シグネチャに合わせる。

#### [MODIFY] [shared/libs/go/codingagent/codex/adapter.go](file://shared/libs/go/codingagent/codex/adapter.go)

*   **Description**: ログ用 `codex-{pid}` は残してよいが、Tern が永続化する ID は stream の `EventSystem.SessionID` (thread_id)。アダプタの `codexSession.ID()` は可能なら thread_id 確定後に更新、または pid 仮 ID のままでも agentservice が EventSystem から上書きするため LIVE は成立する。推奨: 初回 EventSystem 受信までは pid 仮 ID、受信後は thread_id (任意改善)。
*   **Logic**: CreateSession は現状のまま `WithAgentSessionID` を cfg 経由で StartProcess に渡す (SendMessage 側で record.AgentSessionID を付与済み)。変更の主座は BuildArgs / ParseExecEvent。

### E2E LIVE — 命題 P-CONT (R1–R4, R7)

#### [MODIFY] [tests/agentservice_e2e_test.go](file://tests/agentservice_e2e_test.go)

*   **Description**: LIVE を会話継続証明に書き換え。`ensureSessionReadyForNextMessage` の terminate を命題経路から削除。
*   **Technical Design** (プロンプトは user-input / 承認待ちを誘発しないこと — 仕様承認済み):

    ```go
    // Memory token vs config marker (roles separated per spec R1)
    memToken := fmt.Sprintf("TERN_MEM_%d", time.Now().UnixNano())
    alphaMarker := fmt.Sprintf("TERN_CFG_ALPHA_%d", ...)
    betaMarker := fmt.Sprintf("TERN_CFG_BETA_%d", ...)

    // Turn 1 — no tools, no terminate after
    prompt1 := fmt.Sprintf(
      "Do not use tools. Remember this secret token exactly for later turns: %s. "+
      "Also read CLAUDE.md (or AGENTS.md for Codex) and note its config marker. "+
      "Reply with a short ack that includes the secret token once.",
      memToken)

    // PATCH config_dir=beta — must NOT call terminate / ensureSessionReadyForNextMessage

    // Turn 2
    prompt2 := fmt.Sprintf(
      "Do not use tools. What was the secret token I asked you to remember earlier? "+
      "Reply with that exact token. Also read the current CLAUDE.md/AGENTS.md and include the config marker that starts with TERN_CFG_BETA_.")

    // Assertions (all required — empty agent_session_id is FATAL):
    // - collectE2EText(events2) contains memToken
    // - text or FS shows betaMarker (CLAUDE.md/AGENTS.md under session_dir)
    // - session_id same, session_dir same
    // - agent_session_id after turn1 != ""
    // - agent_session_id after turn2 == agent_session_id after turn1
    // - test must not call POST .../terminate between turns
    ```

*   **Logic**:
    1. `ensureSessionReadyForNextMessage` を削除するか、命題 LIVE から呼び出しを全削除。busy が出たら製品バグとして FAIL (terminate で隠蔽しない)。
    2. Claude / Codex 両 LIVE を上記に揃える。Codex は `AGENTS.md`、Claude は `CLAUDE.md`。
    3. Codex LIVE は R6 後に `agent_session_id` (thread_id) が非空になることを前提。空なら Fatal。
    4. フラグ `RUN_CONFIG_DIR_LIVE=1` 維持。不足時 Fatal。
    5. モデル: Claude は `configDirLiveClaudeModel()` (`claude-haiku-4-5` 既定)、Codex は `gpt-4o` (現行踏襲。失敗時は Fatal、Skip で受け入れ完了にしない)。

### Docs / ternctl (R7)

#### [MODIFY] [docs/ReferenceManual-WebAPIs.md](file://docs/ReferenceManual-WebAPIs.md)

*   **Logic** (PATCH 節に追記):
    - Updating `config_dir` does not require `terminate`.
    - The same Tern `session_id` continues; the next SendMessage overlays the new config and resumes the agent conversation (`agent_session_id` / Claude `--resume` / Codex `exec resume`).
    - `terminate` ends the active execution and closes the session status; it is not part of the normal config-switch flow.

#### [MODIFY] [features/ternctl/main.go](file://features/ternctl/main.go)

*   **Logic**: `session-config` ヘルプに「Applies on the next message; do not terminate the session just to switch config」を追記。

## Step-by-Step Implementation Guide

1. [x] **Unit (R5)**: `TestHandleSendMessage_SecondMessageWithoutTerminate` 追加 → 失敗確認。
2. [x] **Implement busy fix**: `handler.go` で正常完了時 unregister → `[DONE]` 順序を修正。再テスト PASS。
3. [x] **Integration P-BUSY**: `TestAgentService_ConfigDir_SecondMessageWithoutTerminate` + SwitchSameSession コメント。
4. [x] **Unit (R6 protocol)**: `TestParseExecEvent_ThreadStartedEmitsSystemSessionID` → 失敗確認。
5. [x] **Implement thread.started → EventSystem**: `protocol.go`。
6. [x] **Unit (R6 BuildArgs)**: resume / non-resume ケース → 失敗確認。
7. [x] **Implement BuildArgs resume**: `process.go` + 呼び出し更新。
8. [x] **Rewrite LIVE E2E**: terminate 除去、記憶トークン + beta マーカー、`agent_session_id` 空 FAIL。
9. [x] **Docs / ternctl** 文言更新。
10. [x] **Verify**: build + specify (下記) + `RUN_CONFIG_DIR_LIVE=1` Claude/Codex LIVE。
11. [/] **Commit / push** after green (execute phase rules)。

## Verification Plan

### Automated Verification

1. **Build & Unit Tests**:
   ```bash
   ./scripts/process/build.sh
   ```

2. **Integration (busy + overlay + patch)**:
   ```bash
   ./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestHandleSendMessage_SecondMessageWithoutTerminate|TestAgentService_ConfigDir|TestHandlePatchSession|TestParseExecEvent_ThreadStarted|TestCodexBuildArgs"
   ```

3. **E2E LIVE (命題・課金あり・必須)**:
   ```bash
   RUN_CONFIG_DIR_LIVE=1 ./scripts/process/build.sh && RUN_CONFIG_DIR_LIVE=1 ./scripts/process/integration_test.sh --specify "TestE2E_ConfigDir_Live"
   ```
   - Claude / Codex 両方 PASS が受け入れ完了
   - Linux / Remote-SSH Linux: `build.sh --skip-etc`、integration は `xvfb-run -a` ラップ、`--headed`/`--ui` なし

### Verification mapping

| Spec scenario | Test |
| :--- | :--- |
| P-CONT Claude | `TestE2E_ConfigDir_Live_Claude_SwitchSameSession` (rewritten) |
| P-CONT Codex | `TestE2E_ConfigDir_Live_Codex_SwitchSameSession` (rewritten) |
| P-BUSY | `TestHandleSendMessage_SecondMessageWithoutTerminate`, `TestAgentService_ConfigDir_SecondMessageWithoutTerminate` |
| R6 thread_id / resume | `TestParseExecEvent_ThreadStartedEmitsSystemSessionID`, `TestCodexBuildArgs_ResumeUsesExecResume` |
| overlay API only (not continuity) | existing `TestAgentService_ConfigDir_SwitchSameSession_*` (comment clarified) |

## Documentation

- [MODIFY] `docs/ReferenceManual-WebAPIs.md` — PATCH: terminate 不要、resume 継続
- [MODIFY] `features/ternctl/main.go` — session-config ヘルプ
- [MODIFY] 仕様 `ideas/002-...md` User Review は承認済みに更新済み
