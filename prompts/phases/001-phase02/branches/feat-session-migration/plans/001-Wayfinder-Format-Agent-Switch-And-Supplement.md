# 001-Wayfinder-Format-Agent-Switch-And-Supplement

> **Source Specification**: [ideas/000-Wayfinder-Format-Session-Portability.md](file://prompts/phases/001-phase02/branches/feat-session-migration/ideas/000-Wayfinder-Format-Session-Portability.md)
>
> **Depends On**: [plans/000-Wayfinder-Format-Canonical-Store-And-Ingest.md](file://prompts/phases/001-phase02/branches/feat-session-migration/plans/000-Wayfinder-Format-Canonical-Store-And-Ingest.md)

## Goal Description

同一 Tern `session_id` / `session_dir` のまま実行エージェントを切り替える。他エージェントの native resume は使わず、**Tern 正本の `origin != 切替先`（必要なら watermark 以降）**をプロンプト注入する。切替先に自分の binding があれば自分を resume する。大きい差分の要約は **LLM Map&Reduce を必須**とし、戦略はサーバ設定と Client API の両方で差し替え可能にする。

## User Review Required

1. **注入の要約（確定）**: 差分が大きい実運用（長時間同一エージェントのあと、一部だけ別エージェント）を前提に、**LLM Map&Reduce を必須の既定経路**とする。既存 `session.MapReduceSummarizer`（maxChunkMsgs 既定 20）を使う。チャンク LLM 失敗時のみそのチャンクを `structuredFallbackSummary` 相当に落とす。history ファイルは縮めない。`full` / `structured` は明示戦略として残す。
2. **切替差分（確定）**: native JSONL は読まない。差分は Tern 正本の `origin != 切替先`。初回（自分の native id 無し）は新規 native + 他 origin 全件。復帰は **自分の** native を resume し、watermark 以降の他 origin だけを注入する。
3. **PATCH ボディ**: 現行は `config_dir` 必須。本計画では `config_dir` / `agent` / `supplement` / `model` の**少なくとも一方**必須に緩和する。`{"config_dir":"..."}` のみの既存クライアントは互換。
4. **コンテキスト試験の梯子**: エージェント切替の前に、切替なし（B1）とモデル切替（B2）で印が残ることを必須テストにする。その後エージェント切替・往来・MapReduce。
5. **LIVE CLI E2E**: `TestSessionPortabilityLive*` はモック必須テストとは別に `llm` カテゴリへ置く。実 CLI 前提が欠ける環境では既存 LIVE ハーネス（vault / PATH）と同じ整備を要求し、`t.Skip` は使わない。

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R4: 同一 Tern ID / session_dir、他エージェントの resume 禁止、自分の binding は resume、JSONL 直コピー禁止 | Proposed Changes > handler.go SendMessage, PATCH |
| R5: origin!=切替先 と watermark、大きい差分は LLM MapReduce 必須、history 非破壊。JSONL は読まない | Proposed Changes > wayfinder/portable |
| R5.1: algorithm（map_reduce / full / structured）と LLM 設定をサーバ既定・PATCH・SendMessage で差し替え | Proposed Changes > config.go, handler.go, client/v1 |
| R7: PATCH agent、busy 拒否、active の native id クリア、bindings 保持、GET に bindings と実効 supplement | Proposed Changes > handler.go, client/v1, docs |
| R7.1: PATCH model、resume 維持、Transfer なし | Proposed Changes > handler.go SendMessage, PATCH |
| シナリオ B1, B2, 2, 2b, 2c, 3, 4, 6, 7, 8 | Verification Plan E2E |
| R10 | Part 1（WorkspaceSessionStore）。Part 2 は `.tern/{id}` を session_dir として使う |
| R9, R11, R12 | 対象外 |

## Proposed Changes

### portable（差分検出と注入文）

#### [NEW] [shared/libs/go/wayfinder/portable/portable_test.go](file://shared/libs/go/wayfinder/portable/portable_test.go)

*   **Description**: 差分抽出・origin 別レンダリング・注入ラップを Failed First で追加する。
*   **Technical Design**:
    ```go
    func TestDelta_ForeignOriginOnly(t *testing.T)
    // target=codex、msgs はすべて origin=claudecode → 全件（初回切替）

    func TestDelta_ExcludesSameOrigin(t *testing.T)
    // 混在。target=codex → origin=claudecode / wayfinder のみ。origin=codex は含まれない

    func TestDelta_AfterWatermarkKeepsOnlyNewForeign(t *testing.T)
    // seq1,2 origin=claudecode、seq3 origin=codex、through=2、target=claudecode → seq3 のみ
    // seq3 を origin=claudecode にしたら空（自分由来は差分にしない）

    func TestRenderSupplement_LabelsOrigin(t *testing.T)
    // origin=claudecode tool_use Name=Read と origin=wayfinder assistant が混在
    // 出力に "[origin=claudecode]" と "[origin=wayfinder]" が両方ある
    // Codex resume id や --resume 文字列を含まない

    func TestRenderSupplement_NeutralizesForeignTools(t *testing.T)
    // target=codex, origin=claudecode, ToolCalls Name=Read
    // 出力に "tool(claudecode:Read)" を含み、Codex 固有 item 型に書き換えない

    func TestRenderSupplement_SameOriginKeepsName(t *testing.T)
    // target=claudecode, origin=claudecode, Name=Read → "tool(Read)" または元の Name

    func TestWrapPrompt_PutsSupplementBeforeUser(t *testing.T)
    // Wrap("SUP", "USER") は SUP が USER より前
    // 見出し "Tern session context transfer" と
    // "supplementary history, not your own previous turn" を含む

    func TestBuildSupplement_FullRendersAll(t *testing.T)
    // algorithm=full は Summarizer を呼ばない

    func TestBuildSupplement_StructuredDoesNotCallLLM(t *testing.T)
    // algorithm=structured、Summarize が呼ばれたら t.Fatal

    func TestBuildSupplement_MapReduceWhenOverThreshold(t *testing.T)
    // 多数メッセージで Render が threshold 超。algorithm=map_reduce
    // 古いチャンクに CTX-TOKEN-7F3A。Summarize に渡る msgs に印がある
    // 注入に mock summarizer の印（例: "MR-SUMMARY" または印自体）と末尾 recent_keep の原文がある
    // 入力 msgs スライスの Content は変わらない（history 非破壊）

    func TestBuildSupplement_MapReduceChunkFallback(t *testing.T)
    // summarize が 1 チャンクだけ error → そのチャンクは structured、他は LLM 結果
    // BuildSupplement 自体は error を返さない

    func TestBuildSupplement_MapReduceNilLLMErrors(t *testing.T)
    // 閾値超 + llm==nil → error（structured へ黙って落とさない）

    func TestMergeStrategy_Precedence(t *testing.T)
    // server default map_reduce, session structured, turn full → 実効は full
    // 部分指定 {model:"x"} は上位の algorithm を保持しつつ model だけ上書き
    ```
*   **Logic**: 仕様シナリオ 3 と 7。native JSONL を生成しない。

#### [NEW] [shared/libs/go/wayfinder/portable/portable.go](file://shared/libs/go/wayfinder/portable/portable.go)

*   **Description**: 正本 → 切替先プロンプト変換。
*   **Technical Design**:
    ```go
    package portable

    const (
        TransferHeader = "Tern session context transfer"
        TransferNotice = "The following is reconstructed from a shared session log. It is supplementary history, not your own previous turn. Origins are labeled."
        TransferFooter = "End of transferred context"

        AlgorithmMapReduce  = "map_reduce"
        AlgorithmFull       = "full"
        AlgorithmStructured = "structured"
    )

    type Strategy = session.SupplementStrategy // 循環回避のため型本体は session パッケージ

    func WithDefaults(s Strategy) Strategy
    // Algorithm 空 → map_reduce
    // MaxChunkMessages<=0 → 20
    // ThresholdBytes<=0 → 32768
    // RecentKeep<=0 → 8（本計画は 0 を未指定とみなす。メソッドは session パッケージの型には付けず portable の関数にする）

    func MergeStrategy(server, session, turn Strategy) (Strategy, error)
    // フィールド単位で turn > session > server。algorithm が非空かつ未知なら error

    type Summarizer interface {
        Summarize(ctx context.Context, model string, msgs []session.Message) (string, error)
        Merge(ctx context.Context, model string, a, b string) (string, error)
    }

    func Delta(msgs []session.Message, targetOrigin string, ingestedThroughSeq int) []session.Message
    // seq > ingestedThroughSeq かつ origin != targetOrigin
    // native JSONL は見ない。正本の origin だけで「そのエージェント由来ではない」行を得る

    func RenderSupplement(targetAgent string, msgs []session.Message) string
    // origin 付き本文。tool は target==origin なら tool(Name)、違えば tool(origin:Name)
    // tool Content は CompactionConfig.MaxContentLen（5000）で rune トリム

    func StructuredSummary(msgs []session.Message) string
    // Wayfinder AgentCore.structuredFallbackSummary と同趣旨:
    // "origin role: firstLine(content)" を改行連結。history は触らない

    func BuildSupplement(ctx context.Context, targetAgent string, msgs []session.Message, strat Strategy, llm Summarizer) (string, error)
    // 1. rendered := RenderSupplement(...)
    // 2. strat.Algorithm==full → rendered を返す（llm 不使用）
    // 3. strat.Algorithm==structured → StructuredSummary を返す
    // 4. strat.Algorithm==map_reduce:
    //      len(rendered) <= ThresholdBytes → rendered（小さい差分は全文）
    //      超える:
    //        keep := msgs の末尾 RecentKeep、old := 残り
    //        mr := session.NewMapReduceSummarizer(
    //            func(chunk []session.Message) (string, error) { return llm.Summarize(ctx, strat.Model, chunk) },
    //            func(a,b string) (string, error) { return llm.Merge(ctx, strat.Model, a, b) },
    //            StructuredSummary,
    //            strat.MaxChunkMessages,
    //        )
    //        summary, err := mr.Summarize(old)  // チャンク内 error は MapReduce が fallback
    //        注入 = "[COMPACTED CONTEXT SUMMARY]\n" + summary + "\n" + RenderSupplement(target, keep)
    // 5. algorithm=map_reduce かつ閾値超で llm==nil のときは error を返す（本番は必ず GatewaySummarizer を接続）。
    //    チャンク単位の LLM error は MapReduceSummarizer 側の structured fallback に任せる。
    // history は変更しない

    func WrapPrompt(supplement, userPrompt string) string
    ```
*   **Logic**: 切替先は新規 native。大きい差分は LLM MapReduce が既定。小さい差分は閾値以下なら全文。明示 `full` / `structured` で差し替える。

### config（サーバ既定）

#### [MODIFY] [shared/libs/go/config/config_test.go](file://shared/libs/go/config/config_test.go)

*   **Description**: `agent_service.supplement` の YAML ロードと既定値を Failed First で追加する。
*   **Technical Design**: 未指定時 `algorithm=map_reduce`, `max_chunk_messages=20`, `threshold_bytes=32768`, `recent_keep=8`, `model=""`。未知 algorithm を書いたファイルは loader が error でも、実行時 Merge で 400 にしてもよい。本計画は **ロード時は文字列のまま受け、実行時に検証**する。
*   **Logic**: 内部設定が Client API の最下位既定になる。

#### [MODIFY] [shared/libs/go/config/config.go](file://shared/libs/go/config/config.go)

*   **Description**:
    ```go
    type SupplementConfig struct {
        Algorithm        string `yaml:"algorithm"`
        Model            string `yaml:"model"`
        MaxChunkMessages int    `yaml:"max_chunk_messages"`
        ThresholdBytes   int    `yaml:"threshold_bytes"`
        RecentKeep       int    `yaml:"recent_keep"`
    }

    type AgentServiceConfig struct {
        Port           int              `yaml:"port"`
        DisableSandbox bool             `yaml:"disable_sandbox"`
        EnableSubagent bool             `yaml:"enable_subagent"`
        Supplement     SupplementConfig `yaml:"supplement"`
    }
    ```
    YAML 例:
    ```yaml
    agent_service:
      supplement:
        algorithm: map_reduce
        model: ""
        max_chunk_messages: 20
        threshold_bytes: 32768
        recent_keep: 8
    ```
*   **Logic**: 仕様 R5.1 のサーバ既定。

#### [NEW] [shared/libs/go/agentservice/gateway_summarizer.go](file://shared/libs/go/agentservice/gateway_summarizer.go)

*   **Description**: portable.Summarizer を LLM Gateway で実装する。Wayfinder `llmSummarizeChunk` / `llmMergeSummaries` と同趣旨の system prompt を使う（ファイルパス・ツール名・因果を落とさない、会話と同じ言語）。
*   **Technical Design**:
    ```go
    type GatewaySummarizer struct {
        Client /* existing llmgateway chat client used by embeddings or a thin HTTP wrapper */
        DefaultModel string
    }
    func (g *GatewaySummarizer) Summarize(ctx context.Context, model string, msgs []session.Message) (string, error)
    func (g *GatewaySummarizer) Merge(ctx context.Context, model string, a, b string) (string, error)
    ```
    `model==""` なら `DefaultModel`。agentservice は既に GatewayURL / Token / モデル解決を持つので、そこへ接続する。具体クライアントがパッケージ循環するなら `llmgateway` の既存 Generate を呼ぶ。
*   **Logic**: MapReduce の必須 LLM 経路。単体テストでは interface をモックし、この型は httptest または既存 gateway フェイクで 1 ケースだけ検証する。本番の agentservice `Server` は起動時に `GatewaySummarizer` を必ず保持する。テストサーバだけ mock を差し込める。

### agentservice API と SendMessage

#### [MODIFY] [shared/libs/go/agentservice/handler_test.go](file://shared/libs/go/agentservice/handler_test.go)

*   **Description**: PATCH agent、busy 拒否、切替後 Resume しない、同一 agent は resume、混在 origin の不変を Failed First で追加する。
*   **Technical Design**:
    モックを拡張し `CreateSession` で `codingagent.NewSessionConfig(opts...)` を記録する。
    ```go
    type recordingAgent struct {
        name     string
        lastCfg  *codingagent.SessionConfig
        prompts  []string
        mu       sync.Mutex
        events   []codingagent.StreamEvent
    }
    ```
    `newTestServer` は `claudecode` と `codex` の両方を Register。

    ケース:
    ```go
    func TestHandlePatchSession_SwitchAgentClearsActiveNativeID(t *testing.T)
    // 1ターンで AgentSessionID を持つ → PATCH agent=codex
    // GET: agent_name=codex, agent_session_id==""
    // session_id / session_dir 不変
    // metadata.active_agent==codex、bindings[claudecode] は残る

    func TestHandlePatchSession_BusyRejected(t *testing.T)
    // suspended 相当: execRegistry にダミー登録するか、Send がブロックする mock
    // 簡易: StatusSuspended のレコード + Register exec → PATCH 409
    // agent_name 不変

    func TestHandlePatchSession_UnknownAgent400(t *testing.T)
    func TestHandlePatchSession_AgentOrConfigDirRequired(t *testing.T)
    // {} → 400
    // {"agent":"codex"} → 200（config_dir なしで可）
    // {"model":"gpt-4o"} のみ → 200
    // {"supplement":{"algorithm":"full"}} のみ → 200（戦略だけ更新）
    // {"supplement":{"algorithm":"unknown"}} → 400

    func TestHandlePatchSession_StoresSupplementStrategy(t *testing.T)
    // PATCH {"agent":"codex","supplement":{"algorithm":"structured","recent_keep":2}}
    // GET の supplement.algorithm==structured、recent_keep==2
    // 未指定 model はサーバ既定（空ならセッション model）

    func TestHandleSendMessage_AfterSwitchDoesNotResumeForeign(t *testing.T)
    // claude 1 ターン → PATCH codex → SendMessage
    // lastCfg.AgentSessionID == ""（Codex 初回、Claude id を渡さない）
    // lastCfg.Prompt に TransferHeader と前回 user 内容が含まれる

    func TestHandleSendMessage_SwitchBackResumesOwnNative(t *testing.T)
    // 上記のあと PATCH claudecode → SendMessage
    // lastCfg.AgentSessionID == Claude 1 ターン目の native id
    // Prompt に origin=codex の内容があり、Claude 期間の自分の長文は必須としない


    func TestHandleSendMessage_TurnSupplementOverridesSession(t *testing.T)
    // セッション戦略 map_reduce、SendMessage {"supplement":{"algorithm":"full"}}
    // 当該ターンの Prompt は全文（mock Summarizer が呼ばれない）
    // 次の切替注入（override なし）はセッションの map_reduce に戻る

    func TestHandleSendMessage_SameAgentResumes(t *testing.T)
    // 切替なし 2 ターン目 lastCfg.AgentSessionID == 1 ターン目の native id
    // Prompt に TransferHeader 無し。history に 1 ターン目の CTX-TOKEN が残る

    func TestHandlePatchSession_ModelSwitchKeepsNativeResume(t *testing.T)
    // PATCH {"model":"gpt-4o"}（テストサーバに登録済み）
    // GET model 更新、agent_session_id 不変
    // 次 Send の lastCfg.Model が新モデル、AgentSessionID は旧 native
    // Prompt に TransferHeader 無し

    func TestHandleSendMessage_DoesNotRewriteOldOrigin(t *testing.T)
    // 切替後の新規 history は origin=codex、旧ファイル origin=claudecode のまま
    ```
*   **Logic**: シナリオ B1, B2, 2, 4, 6。busy は `execRegistry.Get` がヒットしたら 409。suspended も busy 扱い（仕様: 実行中 / suspended）。

#### [MODIFY] [shared/libs/go/agentservice/handler.go](file://shared/libs/go/agentservice/handler.go)

*   **Description**: PATCH 拡張、GET に bindings、SendMessage で補完と resume 分岐。アダプタへは `NativeSessionDir(record.SessionDir)` を渡し、正本の読み書きは `record.SessionDir`（`.tern/{id}`）のままにする。
*   **Technical Design**:

    **PATCH** `handlePatchSession`:
    ```go
    var req struct {
        ConfigDir  *string                     `json:"config_dir"`
        Agent      *string                     `json:"agent"`
        Model      *string                     `json:"model"`
        Supplement *session.SupplementStrategy `json:"supplement"`
    }
    if req.ConfigDir == nil && req.Agent == nil && req.Supplement == nil && req.Model == nil {
        http.Error(w, "agent, config_dir, supplement, or model is required", http.StatusBadRequest)
        return
    }
    ```
    `req.Model != nil` のとき CreateSession と同じ検証。agent 未変更なら `record.Model` だけ更新し `AgentSessionID` は触らない。
    `req.Supplement != nil` のとき algorithm が非空なら既知か検証し、未知は 400。検証済みの**部分指定**を Canonical.SetSupplement でセッションへ保存する（サーバ既定で埋めてから書かない。Merge は注入時と GET 実効値で行う）。agent 切替時も supplement を消さない。
    if exec, ok := s.execRegistry.Get(id); ok {
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusConflict)
        json.NewEncoder(w).Encode(map[string]any{
            "error": "session busy", "status": exec.status, "hint": "respond or terminate",
        })
        return
    }
    // Agent 変更
    if req.Agent != nil {
        name := *req.Agent
        if _, ok := s.agents[name]; !ok {
            http.Error(w, "unknown agent: "+name, http.StatusBadRequest)
            return
        }
        if rec.Status == codingagent.StatusSuspended {
            // execRegistry に残っている想定。Get で既に 409。残っていなければ 409 を明示
        }
        record.AgentName = name
        record.AgentSessionID = "" // active native id クリア。bindings はディスクに残す
        if record.SessionDir != "" {
            c := session.OpenCanonical(record.SessionDir)
            if meta, err := c.LoadMetadata(); err == nil {
                meta.ActiveAgent = name
                // AgentBindings は触らない
                _ = c.saveMetadata(meta) // saveMetadata が unexported なら Canonical に SetActiveAgent を追加
            }
        }
    }
    // ConfigDir 変更は現行の validateAndResolveConfigDir を req.ConfigDir != nil のときのみ
    ```
    `session_dir` / Tern `id` は変更しない。コメント「Does not modify work_dir, session_dir, or agent_session_id」を「agent 切替時は active agent_session_id をクリアする。bindings は残す」に更新する。

    **GET** `handleGetSession`: レコードを JSON にしたあと bindings と実効 supplement を載せる。DTO:
    ```go
    type sessionAPIResponse struct {
        codingagent.SessionRecord
        AgentBindings map[string]session.AgentBinding `json:"agent_bindings,omitempty"`
        ActiveAgent   string             `json:"active_agent,omitempty"`
        Supplement    portable.Strategy  `json:"supplement,omitempty"`
    }
    ```
    `Supplement` は MergeStrategy(serverDefault, sessionStored, emptyTurn) の結果（実効値）。

    **SendMessage** resume / 補完:
    ```go
    type SendMessageRequest struct {
        Content       []codingagent.ContentPart `json:"content"`
        CorrelationID string                    `json:"correlation_id,omitempty"`
        Supplement    *session.SupplementStrategy `json:"supplement,omitempty"`
    }

    resumeID := record.AgentSessionID
    promptText := /* 現行 multimodal 構築のあと */

    if record.SessionDir != "" {
        c := session.OpenCanonical(record.SessionDir)
        meta, _ := c.LoadMetadata()
        through := 0
        ownNative := ""
        if meta != nil {
            if b, ok := meta.AgentBindings[record.AgentName]; ok {
                through = b.IngestedThroughSeq
                ownNative = b.AgentSessionID
            }
        }
        if resumeID == "" {
            resumeID = ownNative // PATCH で active を消しても自分の binding は復帰 resume に使う
        }
        if meta != nil && meta.TotalSeq > 0 {
            msgs, _ := c.LoadRange(1, meta.TotalSeq)
            delta := portable.Delta(msgs, record.AgentName, through)
            if len(delta) > 0 {
                sessStrat := portable.Strategy{}
                if meta != nil {
                    sessStrat = meta.Supplement
                }
                turnStrat := portable.Strategy{}
                if req.Supplement != nil {
                    turnStrat = *req.Supplement
                }
                strat, err := portable.MergeStrategy(s.serverSupplement(), sessStrat, turnStrat)
                if err != nil {
                    http.Error(w, err.Error(), http.StatusBadRequest)
                    return
                }
                if strat.Model == "" {
                    strat.Model = record.Model
                }
                sup, err := portable.BuildSupplement(r.Context(), record.AgentName, delta, strat, s.summarizer)
                if err != nil {
                    http.Error(w, err.Error(), http.StatusInternalServerError)
                    return
                }
                promptText = portable.WrapPrompt(sup, promptText)
            }
        }
    }
    ```
    `s.serverSupplement()` は `config.AgentService.Supplement` から Strategy へ写像。
    同一エージェント 2 ターン目は delta が空（自分 origin のみ）かつ resumeID 非空。
    初回切替（binding に自分の native 無し）は resumeID 空 + `origin != 切替先` の全件。大きいため既定 map_reduce が LLM を使う。
    復帰は resumeID を bindings から復元し、他 origin だけを注入する。補完しても resumeID を空にしない。

#### [MODIFY] [shared/libs/go/agentservice/handler.go](file://shared/libs/go/agentservice/handler.go)（Canonical 公開）

*   **Description**: Part 1 で `SessionMetadata.Supplement` と `SetActiveAgent` 用の保存経路がある。本 Part では `SetSupplement` を公開する。
*   **Technical Design**:
    ```go
    func (c *Canonical) SetActiveAgent(agent string) error
    func (c *Canonical) SetSupplement(s session.SupplementStrategy) error
    ```
    PATCH の部分指定は、既存 `meta.Supplement` にフィールド単位で重ねてから保存する（空文字は上書きしない。algorithm を明示で変えるときは非空なので書き換わる）。
*   **Logic**: PATCH が agent と supplement を独立に更新できる。bindings は変更しない。

### client v1

#### [MODIFY] [client/v1/session_test.go](file://client/v1/session_test.go)

*   **Description**: `UpdateAgent` のリクエストボディを httptest で検証する（テストファイルが無ければ NEW）。
*   **Technical Design**: PATCH `{"agent":"codex"}` と `{"agent":"codex","supplement":{"algorithm":"full"}}`。既存 `UpdateConfigDir` は `config_dir` のみ送り続け、agent を付けない。未知 algorithm はクライアントでは送らず、サーバが 400 を返すことを handler テストで見る。
*   **Logic**: SDK は仕様の「API があれば足りる」を超えて examples と対称にする。

#### [MODIFY] [client/v1/session.go](file://client/v1/session.go)

*   **Technical Design**:
    ```go
    type SupplementStrategy struct {
        Algorithm        string `json:"algorithm,omitempty"`
        Model            string `json:"model,omitempty"`
        MaxChunkMessages int    `json:"max_chunk_messages,omitempty"`
        ThresholdBytes   int    `json:"threshold_bytes,omitempty"`
        RecentKeep       int    `json:"recent_keep,omitempty"`
    }

    type SessionInfo struct {
        // 既存フィールド...
        AgentBindings map[string]struct {
            AgentSessionID     string `json:"agent_session_id"`
            IngestedThroughSeq int    `json:"ingested_through_seq"`
        } `json:"agent_bindings,omitempty"`
        ActiveAgent string              `json:"active_agent,omitempty"`
        Supplement  SupplementStrategy  `json:"supplement,omitempty"`
    }

    type UpdateSessionRequest struct {
        ConfigDir  *string              `json:"config_dir,omitempty"`
        Agent      *string              `json:"agent,omitempty"`
        Model      *string              `json:"model,omitempty"`
        Supplement *SupplementStrategy  `json:"supplement,omitempty"`
    }

    func (c *Client) ListSessions(ctx context.Context, workDir string) ([]SessionInfo, error)
    // GET /api/v1/sessions?work_dir=

    func (c *Client) UpdateSession(ctx context.Context, sessionID string, req UpdateSessionRequest) (*SessionInfo, error)
    func (s *Session) Update(ctx context.Context, req UpdateSessionRequest) (*SessionInfo, error)
    func (s *Session) UpdateAgent(ctx context.Context, agent string) (*SessionInfo, error)
    // UpdateAgent は UpdateSessionRequest{Agent:&agent} の薄いラッパ
    func (s *Session) UpdateModel(ctx context.Context, model string) (*SessionInfo, error)

    func (s *Session) SendMessageWithOpts(ctx context.Context, content []ContentPart, opts SendMessageOpts) (*Stream, error)
    type SendMessageOpts struct {
        CorrelationID string
        Supplement    *SupplementStrategy
    }
    ```
    既存 `SendText` / `SendMessage` は Supplement=nil のまま。`UpdateConfigDir` は ConfigDir のみ送り続ける。
*   **Logic**: work_dir / session_dir は変えない。ターン上書きは SendMessage、セッション既定は PATCH。

### 統合 / E2E

#### [NEW] [tests/llm_session_portability_test.go](file://tests/llm_session_portability_test.go)

*   **Description**: モック 2 エージェントの HTTP 統合テスト。仕様の必須シナリオをコード化する。
*   **Technical Design**: package `llm_test`。`agentservice_integration_test.go` の httptest サーバ組み立てを再利用（同一パッケージのヘルパがあれば使う。無ければ最小の `httptest.NewServer` + `RegisterAgent` をこのファイルに書く）。
    記録 mock: `CreateSession` の opts から `NewSessionConfig` を保存。

    ```go
    func TestSessionPortabilityBaselineSameAgent(t *testing.T)
    // シナリオ B1: 同一 agent/model、印 CTX-TOKEN-7F3A
    // 2 ターン目 AgentSessionID が 1 ターン目と同じ。TransferHeader 無し
    // history に印が残る

    func TestSessionPortabilityModelSwitchKeepsResume(t *testing.T)
    // シナリオ B2: PATCH model のみ。AgentSessionID 不変。lastCfg.Model が新しい
    // TransferHeader 無し。history に印が残る

    func TestSessionPortabilityIngestOrigin(t *testing.T)
    // シナリオ 1: session_dir 明示、agent=claudecode、1 メッセージ
    // history の origin がすべて claudecode
    // metadata.active_agent と bindings[claudecode]

    func TestSessionPortabilityAgentSwitchSupplement(t *testing.T)
    // シナリオ 2: PATCH agent=codex、session_id/session_dir 不変
    // 次の CreateSession の AgentSessionID 空（Claude id を渡さない）
    // Prompt に TransferHeader と印

    func TestSessionPortabilitySwitchBackResumesOwn(t *testing.T)
    // シナリオ 2b: PATCH で claudecode に戻す
    // AgentSessionID はシナリオ 1 の Claude native id
    // Prompt に Codex ターンの内容。Claude 期間の origin=claudecode 本文は必須としない

    func TestSessionPortabilityAgentRoundTrip(t *testing.T)
    // シナリオ 2c: claude→codex→claude→codex
    // 最後の Codex は自分の binding native id で resume
    // Claude の uuid を渡さない。正本の旧 origin 不変

    func TestSessionPortabilityMixedOriginImmutable(t *testing.T)
    // シナリオ 3: fixture で history に claudecode tool と wayfinder assistant
    // PATCH せず agent=codex セッションを作り session_dir をその fixture にする
    // Send の prompt に両 origin ラベル。旧 JSON の origin 不変

    func TestSessionPortabilitySameAgentResume(t *testing.T)
    // シナリオ 4 / B1 の resume 側面: 2 ターン目 WithAgentSessionID

    func TestSessionPortabilityBusyRejectsSwitch(t *testing.T)
    // シナリオ 6: 実行中 409、agent_name 不変

    func TestSessionPortabilityMapReduceKeepsToken(t *testing.T)
    // シナリオ 7: 閾値超え。印は recent_keep より前
    // mock Summarize の入力 msgs に印。注入に MR 要約の印。history 件数不変
    // algorithm=full では原文の印

    func TestSessionPortabilitySupplementStrategy(t *testing.T)
    // GET supplement.algorithm はセッション値のまま map_reduce
    // PATCH structured のあと LLM を呼ばない

    func TestSessionPortabilityReloadFromWorkspace(t *testing.T)
    // シナリオ 8: Create 後に別 httptest サーバ（空メモリ）へ work_dir を渡して List
    // 同じ session_id で Send できる。workDir/.tern/session.db は無い
    ```
*   **Logic**: 実 CLI 不要。`t.Skip` 禁止。MapReduce の LLM は agentservice に注入する mock Summarizer を使う。

#### [NEW] [tests/llm_session_portability_live_test.go](file://tests/llm_session_portability_live_test.go)

*   **Description**: 実 CLI の梯子（仕様 Testing）。既存 `codex_e2e_test.go` / `agentservice_e2e_test.go` と同じサーバ起動ヘルパを使う。
*   **Technical Design**:
    ```go
    func TestSessionPortabilityLiveBaseline(t *testing.T)
    // 同一 Claude・同一 model。印を覚えさせ、2 ターン目の応答または resume で参照

    func TestSessionPortabilityLiveModelSwitch(t *testing.T)
    // PATCH model のみ。同じ native。印を問う

    func TestSessionPortabilityLiveSwitch(t *testing.T)
    // Claude 1 ターン（印）→ PATCH agent=codex → 印を問う

    func TestSessionPortabilityLiveRoundTrip(t *testing.T)
    // Claude → Codex → Claude。行きの印と Codex 側の差分が落ちない
    ```
    前提欠落は Fail（既存 LIVE と同じく CLI / gateway 必須）。
*   **Logic**: 必須ゲートはモック梯子。LIVE は計画に含めるが、実装順はモック B1→B2→切替→往来→MapReduce を先に通してから追加する。

### ドキュメント / 例

#### [MODIFY] [docs/ReferenceManual-WebAPIs.md](file://docs/ReferenceManual-WebAPIs.md)

*   **Description**: PATCH に `agent`、GET に `agent_bindings`、session_dir デフォルト `.tern`、terminate 不要、他エージェントの resume はしないが自分の binding は resume することを書く。
*   **Technical Design**: エンドポイント表の PATCH 説明を「`config_dir` / `agent` / `model` / `supplement` の少なくとも一方」に更新。
    ```json
    {
      "agent": "codex",
      "supplement": {
        "algorithm": "map_reduce",
        "model": "",
        "max_chunk_messages": 20,
        "threshold_bytes": 32768,
        "recent_keep": 8
      }
    }
    ```
    SendMessage 例に任意 `supplement` を追加。`agent_service.supplement` の YAML 既定を載せる。
*   **Logic**: 仕様 R5 / R5.1 / R6 / R7 / R10。session_dir デフォルトは `work_dir/.tern/{session_id}`。native は `{session_dir}/native`。

#### [MODIFY] [README.md](file://README.md)

*   **Description**: Client 例に「同一 session_id で UpdateAgent」を 1 ブロック追加する（英語ドキュメント規則）。Phase 2 チェックの Session portability / Agent switching は計画実行後に付ける（本計画の実装完了時）。計画段階ではドキュメント例の追加のみ必須。
*   **Technical Design**: `session.UpdateAgent(ctx, "codex")` のあと `SendText`。Terminate しない。supplement のセッション保存は `session.Update(ctx, clientv1.UpdateSessionRequest{Supplement: &strat})`、ターン上書きは `SendMessageWithOpts`。
*   **Logic**: config-dir-switch と対になる。

#### [MODIFY] [examples/config-dir-switch/README.md](file://examples/config-dir-switch/README.md) は必須としない。新規 example は任意。SDK メソッドがあれば README の Example で足りる。

## Step-by-Step Implementation Guide

- [x] **前提**: Part 1（000）を完了し `./scripts/process/build.sh` が通っていること。Part 1 の B1（切替なし resume + 印）が落ちていてはいけない。
- [x] **TDD portable**: Delta（origin!=target） / MergeStrategy / BuildSupplement（map_reduce mock LLM、印が Summarize 入力に入る）を Failed First で実装する。
- [x] **config**: `agent_service.supplement` を追加する。
- [x] **GatewaySummarizer**: 実 LLM 経路。単体は mock。
- [x] **TDD PATCH/Send（梯子）**: 先に同一 agent 2 ターン（B1）と PATCH model（B2）。そのあと agent 切替、復帰、往来、busy、supplement。
- [x] **Canonical.SetSupplement / SessionMetadata.Supplement**。
- [x] **handler 接続**: MergeStrategy の優先順位で BuildSupplement。モデル切替では WrapPrompt しない。
- [x] **client/v1**: UpdateSession / UpdateModel / SendMessageOpts。
- [x] **E2E モック**: `TestSessionPortability*` を B1 → B2 → 切替 → 往来 → MapReduce の順で追加する。
- [x] **ドキュメント**: ReferenceManual と README 例。
- [x] **LIVE**: `TestSessionPortabilityLive*`（Baseline / ModelSwitch / Switch / RoundTrip）。
- [x] **Verification Plan** のコマンドを実行する。

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests**: `./scripts/process/build.sh`（Linux / Remote-SSH は `./scripts/process/build.sh --skip-etc`）
2.  **Integration Tests（モック、必須）**:
    `./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories common --specify "TestSessionPortability"`
    Linux: `./scripts/process/build.sh --skip-etc && xvfb-run -a ./scripts/process/integration_test.sh --categories common --specify "TestSessionPortability"`
3.  **E2E Tests（実 CLI、回帰）**:
    `./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories llm --specify "TestSessionPortabilityLive"`
    Linux: `./scripts/process/build.sh --skip-etc && xvfb-run -a ./scripts/process/integration_test.sh --categories llm --specify "TestSessionPortabilityLive"`
4.  **リグレッション**:
    `./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories llm --specify "TestConfigDir|TestSession"`

必須ゲートは 1 と 2。3 は実 CLI 環境での追加確認。

## Documentation

- `docs/ReferenceManual-WebAPIs.md`: PATCH `agent` / `model` / `supplement`、GET `agent_bindings` と実効戦略、session_dir デフォルト、切替セマンティクス（他エージェントは resume しない、自分の binding は resume、モデル切替は resume 維持）
- `README.md`: SDK での agent 切替・model 切替と supplement 上書き例
- 仕様書の検証梯子 B1 → B2 → 2 → 2c → 7 は本計画のテスト名と対応させる
