# 003-Wayfinder-WBS-Planning-Orchestration

> **Source Specification**:
> - [004-Wayfinder-Planning-and-WBS-Execution-Orchestration.md](file://prompts/phases/000-foundation/branches/feat-wayfinder-agent/ideas/004-Wayfinder-Planning-and-WBS-Execution-Orchestration.md)
> - [000-Wayfinder-Agent-Overview.md](file://prompts/phases/000-foundation/branches/feat-wayfinder-agent/ideas/000-Wayfinder-Agent-Overview.md) (実行ブランチの分岐要件)

## Goal Description

Wayfinder Agentの計画作成およびWBS実行オーケストレーション機能を実装する。ユーザーの要求が複雑な場合に自動的にWBS（Work Breakdown Structure）計画を作成し、各ステップをサブエージェントで順次実行する。また、単純な要求と計画的な要求を自動判定して実行ルートを分岐させる。

具体的には:
1. **実行ブランチの分岐 (ExecutionRouter)**: ユーザーの要求を分析し、単純実行ルートと計画・実行オーケストレーションルートに自動分岐
2. **WBSTree構造体とJSON化**: 階層型IDを持つWBSノードのツリー構造定義
3. **WBS計画の自動生成**: LLM Structured Outputを用いたWBSTree JSON生成
4. **WBSOrchestrator**: WBSツリーをトラバースし、依存関係を考慮しながらサブエージェントで順次実行
5. **レジューム機能**: WBSツリー状態の永続化と、プロセス中断からの再開
6. **エラーリカバリーと一時停止**: ノード失敗時の後続ノードブロックとユーザーへの通知

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| 要求に応じた自動分岐 | Proposed Changes > execution_router.go > `Route` |
| WBSツリー計画の自動生成 | Proposed Changes > wbs_planner.go > `GenerateWBS` |
| WBSノード構造 (ID/Name/Description/Status/Dependencies/SubSteps/ResultSummary) | Proposed Changes > wbs_tree.go |
| WBSに沿ったサブエージェント実行のオーケストレーション | Proposed Changes > wbs_orchestrator.go > `Execute` |
| 依存関係解消チェック | Proposed Changes > wbs_tree.go > `NextExecutableNodes` |
| シングルショット対応レジューム | Proposed Changes > wbs_orchestrator.go + session永続化連携 |
| エラーリカバリーと一時停止 | Proposed Changes > wbs_orchestrator.go > `handleNodeFailure` |
| 実行ブランチの分岐 (000) | Proposed Changes > execution_router.go |

## Proposed Changes

### wayfinder/planning パッケージ

#### [NEW] [wbs_tree.go](file://shared/libs/go/wayfinder/planning/wbs_tree.go)
*   **Description**: WBSノードとツリーの構造体定義、依存関係解決アルゴリズム
*   **Technical Design**:
    ```go
    package planning

    // WBSNode represents a single step in the Work Breakdown Structure.
    type WBSNode struct {
        ID            string    `json:"id"`              // Hierarchical ID: "1", "1.1", "1.2", "2"
        Name          string    `json:"name"`
        Description   string    `json:"description"`
        Status        string    `json:"status"`          // "pending", "running", "completed", "failed"
        Dependencies  []string  `json:"dependencies"`    // IDs of prerequisite nodes
        SubSteps      []WBSNode `json:"sub_steps,omitempty"`
        ResultSummary string    `json:"result_summary,omitempty"`
    }

    // WBSTree manages the entire WBS structure.
    type WBSTree struct {
        RootNodes []WBSNode `json:"root_nodes"`
    }

    // NextExecutableNodes returns nodes that are "pending" and have all
    // dependencies in "completed" status.
    func (t *WBSTree) NextExecutableNodes() []WBSNode {
        statusMap := t.buildStatusMap()
        var result []WBSNode
        t.walkNodes(func(node *WBSNode) {
            if node.Status != "pending" {
                return
            }
            allDepsCompleted := true
            for _, depID := range node.Dependencies {
                if statusMap[depID] != "completed" {
                    allDepsCompleted = false
                    break
                }
            }
            if allDepsCompleted {
                result = append(result, *node)
            }
        })
        return result
    }

    // IsComplete returns true if all nodes are "completed".
    func (t *WBSTree) IsComplete() bool {
        complete := true
        t.walkNodes(func(node *WBSNode) {
            if node.Status != "completed" {
                complete = false
            }
        })
        return complete
    }

    // HasFailed returns true if any node has "failed" status.
    func (t *WBSTree) HasFailed() bool {
        failed := false
        t.walkNodes(func(node *WBSNode) {
            if node.Status == "failed" {
                failed = true
            }
        })
        return failed
    }

    // IsDeadlocked returns true if there are pending nodes but
    // none are executable (circular dependency).
    func (t *WBSTree) IsDeadlocked() bool {
        hasPending := false
        t.walkNodes(func(node *WBSNode) {
            if node.Status == "pending" {
                hasPending = true
            }
        })
        return hasPending && len(t.NextExecutableNodes()) == 0
    }

    // UpdateNodeStatus updates the status and result of a specific node by ID.
    func (t *WBSTree) UpdateNodeStatus(nodeID, status, resultSummary string) bool {
        found := false
        t.walkNodesMut(func(node *WBSNode) {
            if node.ID == nodeID {
                node.Status = status
                node.ResultSummary = resultSummary
                found = true
            }
        })
        return found
    }

    // buildStatusMap creates a flat map of nodeID -> status for dependency checking.
    func (t *WBSTree) buildStatusMap() map[string]string {
        m := make(map[string]string)
        t.walkNodes(func(node *WBSNode) {
            m[node.ID] = node.Status
        })
        return m
    }

    // walkNodes traverses all nodes (including nested sub_steps) in DFS order.
    func (t *WBSTree) walkNodes(fn func(*WBSNode)) {
        for i := range t.RootNodes {
            walkNodeRecursive(&t.RootNodes[i], fn)
        }
    }

    // walkNodesMut traverses with mutable access.
    func (t *WBSTree) walkNodesMut(fn func(*WBSNode)) {
        for i := range t.RootNodes {
            walkNodeRecursive(&t.RootNodes[i], fn)
        }
    }

    func walkNodeRecursive(node *WBSNode, fn func(*WBSNode)) {
        fn(node)
        for i := range node.SubSteps {
            walkNodeRecursive(&node.SubSteps[i], fn)
        }
    }
    ```

#### [NEW] [wbs_planner.go](file://shared/libs/go/wayfinder/planning/wbs_planner.go)
*   **Description**: LLMを使用してWBSツリーJSONを自動生成する
*   **Technical Design**:
    ```go
    package planning

    import (
        "context"
        "encoding/json"
        "fmt"
    )

    // WBSPlanner generates WBS plans using LLM Structured Output.
    type WBSPlanner struct {
        llm wayfinder.LLMClient
    }

    func NewWBSPlanner(llm wayfinder.LLMClient) *WBSPlanner {
        return &WBSPlanner{llm: llm}
    }

    const wbsPlannerSystemPrompt = `You are a task planning agent.
Given a user's request, break it down into a hierarchical Work Breakdown Structure (WBS).
Output a JSON object with the following schema:
{
  "root_nodes": [
    {
      "id": "1",
      "name": "Step name",
      "description": "Detailed instruction for this step",
      "status": "pending",
      "dependencies": [],
      "sub_steps": []
    }
  ]
}

Rules:
- Use hierarchical IDs: "1", "1.1", "1.2", "2", etc.
- All statuses must be "pending"
- Set dependencies to IDs of steps that must complete first
- Keep each step atomic and actionable
- Sub-steps represent breakdown of a parent step`

    // GenerateWBS creates a WBS tree from the user's request.
    func (p *WBSPlanner) GenerateWBS(ctx context.Context, model string, userRequest string) (*WBSTree, error) {
        messages := []wayfinder.ChatMessage{
            {Role: "system", Content: wbsPlannerSystemPrompt},
            {Role: "user", Content: userRequest},
        }

        resp, err := p.llm.GenerateMessage(ctx, model, messages, nil)
        if err != nil {
            return nil, fmt.Errorf("WBS generation failed: %w", err)
        }

        var tree WBSTree
        if err := json.Unmarshal([]byte(extractJSON(resp.Content)), &tree); err != nil {
            return nil, fmt.Errorf("failed to parse WBS JSON: %w", err)
        }

        // Validate: all statuses should be "pending"
        tree.walkNodesMut(func(node *WBSNode) {
            node.Status = "pending"
        })

        return &tree, nil
    }

    // extractJSON extracts JSON content from LLM response.
    // Handles cases where JSON is wrapped in markdown code blocks.
    func extractJSON(content string) string {
        // Remove ```json ... ``` wrappers if present
        // Return the JSON content
        // Implementation: use regex or string trimming
        return content
    }
    ```

#### [NEW] [wbs_orchestrator.go](file://shared/libs/go/wayfinder/planning/wbs_orchestrator.go)
*   **Description**: WBSツリーをトラバースし、依存関係を考慮しながらサブエージェントで順次実行するオーケストレーター
*   **Technical Design**:
    ```go
    package planning

    import (
        "context"
        "fmt"
    )

    // WBSOrchestrator drives WBS execution with subagent delegation.
    type WBSOrchestrator struct {
        subagentExec *subagent.SubagentExecutor
        store        *session.Store
        sessionState *session.SessionState
        logger       wayfinder.Logger
    }

    func NewWBSOrchestrator(
        subagentExec *subagent.SubagentExecutor,
        store *session.Store,
        state *session.SessionState,
        logger wayfinder.Logger,
    ) *WBSOrchestrator {
        return &WBSOrchestrator{
            subagentExec: subagentExec,
            store:        store,
            sessionState: state,
            logger:       logger,
        }
    }

    // Execute drives the WBS execution loop.
    // Returns nil when all nodes complete, or error on failure/deadlock.
    func (o *WBSOrchestrator) Execute(ctx context.Context, tree *WBSTree, parentMessages []session.Message) error {
        for {
            // 1. Check termination conditions
            if tree.IsComplete() {
                o.logger.Info("WBS execution completed successfully")
                return nil
            }
            if tree.HasFailed() {
                return o.handleFailure(tree)
            }
            if tree.IsDeadlocked() {
                return fmt.Errorf("WBS execution deadlocked: pending nodes exist but none are executable")
            }

            // 2. Get next executable nodes
            executableNodes := tree.NextExecutableNodes()
            if len(executableNodes) == 0 {
                return fmt.Errorf("no executable nodes found")
            }

            // 3. Execute each node sequentially
            for _, node := range executableNodes {
                // Mark as running
                tree.UpdateNodeStatus(node.ID, "running", "")
                o.persistWBSState(tree)

                // Execute via subagent
                toolInput := map[string]any{
                    "wbs_node_id":   node.ID,
                    "wbs_node_name": node.Name,
                    "instruction":   node.Description,
                }

                result, err := o.subagentExec.Execute(
                    ctx, parentMessages, "wbs_step_execution", toolInput,
                )
                if err != nil {
                    // Mark as failed
                    tree.UpdateNodeStatus(node.ID, "failed", fmt.Sprintf("Error: %v", err))
                    o.persistWBSState(tree)
                    return o.handleFailure(tree)
                }

                // Mark as completed
                tree.UpdateNodeStatus(node.ID, "completed", result)
                o.persistWBSState(tree)
            }
        }
    }

    // handleFailure blocks dependent nodes and returns error with status report.
    func (o *WBSOrchestrator) handleFailure(tree *WBSTree) error {
        var failedNodes []string
        tree.walkNodes(func(node *WBSNode) {
            if node.Status == "failed" {
                failedNodes = append(failedNodes, fmt.Sprintf("%s (%s): %s", node.ID, node.Name, node.ResultSummary))
            }
        })
        return fmt.Errorf("WBS execution paused due to failed nodes:\n%s\nDependent nodes are blocked.", joinStrings(failedNodes, "\n"))
    }

    // persistWBSState saves the current WBS tree state to session file.
    func (o *WBSOrchestrator) persistWBSState(tree *WBSTree) {
        // Serialize WBSTree to session state and save atomically
        // SessionState includes the WBS tree as part of its state
        o.store.Save(o.sessionState)
    }
    ```
*   **Logic**:
    *   メインループ: 完了判定 -> 失敗判定 -> デッドロック判定 -> 実行可能ノード取得 -> 順次実行
    *   各ノードの実行前に `running` にマーク -> 実行 -> `completed` or `failed` にマーク
    *   各ステータス更新のたびにセッションファイルへアトミック保存（レジューム対応）
    *   失敗時は後続の依存ノードの実行をブロック（依存先が `completed` でないため `NextExecutableNodes` から除外される）

---

### wayfinder コアパッケージへの統合変更

#### [NEW] [execution_router.go](file://shared/libs/go/wayfinder/execution_router.go)
*   **Description**: ユーザー要求の複雑度を判定し、単純実行ルートと計画ルートに分岐する
*   **Technical Design**:
    ```go
    package wayfinder

    import (
        "context"
        "encoding/json"
        "fmt"
    )

    // ExecutionRoute represents the determined execution path.
    type ExecutionRoute int

    const (
        RouteSimple   ExecutionRoute = iota // Direct tool execution
        RoutePlanning                       // WBS planning + orchestrated execution
    )

    // ExecutionRouter determines the execution path based on task complexity.
    type ExecutionRouter struct {
        llm LLMClient
    }

    func NewExecutionRouter(llm LLMClient) *ExecutionRouter {
        return &ExecutionRouter{llm: llm}
    }

    const routerSystemPrompt = `You are a task complexity analyzer.
Given a user's request, determine if it requires planning or can be executed directly.

Respond with a JSON object:
{
  "route": "simple" or "planning",
  "reason": "brief explanation"
}

Guidelines for "planning" route:
- Multiple files need to be created or modified
- Multiple sequential steps with dependencies
- Complex refactoring or architectural changes
- Tasks requiring investigation followed by implementation

Guidelines for "simple" route:
- Single file read/write
- Simple question answering
- Single command execution
- Minor edits or fixes`

    // Route analyzes the user prompt and returns the execution route.
    func (r *ExecutionRouter) Route(ctx context.Context, model string, prompt string) (ExecutionRoute, string, error) {
        messages := []ChatMessage{
            {Role: "system", Content: routerSystemPrompt},
            {Role: "user", Content: prompt},
        }

        resp, err := r.llm.GenerateMessage(ctx, model, messages, nil)
        if err != nil {
            // Default to simple on error
            return RouteSimple, "routing failed, defaulting to simple", nil
        }

        var result struct {
            Route  string `json:"route"`
            Reason string `json:"reason"`
        }
        if err := json.Unmarshal([]byte(extractJSON(resp.Content)), &result); err != nil {
            return RouteSimple, "failed to parse routing response", nil
        }

        if result.Route == "planning" {
            return RoutePlanning, result.Reason, nil
        }
        return RouteSimple, result.Reason, nil
    }
    ```
*   **Logic**:
    *   ルーティング判定に失敗した場合は安全側（単純実行ルート）にフォールバック
    *   LLMにJSON形式で `route` と `reason` を返させ、パースする

#### [MODIFY] [agent_core.go](file://shared/libs/go/wayfinder/agent_core.go)
*   **Description**: Runメソッドを拡張し、ExecutionRouterによる分岐を追加
*   **Technical Design**:
    ```go
    // AgentCore に router/planner/orchestrator フィールドを追加
    type AgentCore struct {
        config       *AgentConfig
        llm          LLMClient
        tools        *ToolRegistry
        tracker      *FileTracker
        subagent     *subagent.SubagentExecutor
        router       *ExecutionRouter       // Part 4 addition
        planner      *planning.WBSPlanner   // Part 4 addition
        logger       Logger
    }

    // Run executes the agent for a user prompt.
    // Determines execution route and dispatches accordingly.
    func (a *AgentCore) Run(ctx context.Context, prompt string) (string, error) {
        // 1. Load session (Part 2)
        // 2. Validate tracker state (Part 2)

        // 3. Determine execution route
        if a.router != nil {
            route, reason, _ := a.router.Route(ctx, a.config.Model, prompt)
            a.logger.Info("Execution route: %v (reason: %s)", route, reason)

            if route == RoutePlanning {
                return a.runWithPlanning(ctx, prompt)
            }
        }

        // 4. Simple execution (existing loop)
        return a.runSimple(ctx, prompt)
    }

    // runSimple is the existing tool-calling loop (Part 1 Run logic).
    func (a *AgentCore) runSimple(ctx context.Context, prompt string) (string, error) {
        // ... existing Run loop code ...
    }

    // runWithPlanning generates a WBS and orchestrates execution.
    func (a *AgentCore) runWithPlanning(ctx context.Context, prompt string) (string, error) {
        // 1. Generate WBS plan
        tree, err := a.planner.GenerateWBS(ctx, a.config.Model, prompt)
        if err != nil {
            a.logger.Warn("WBS generation failed, falling back to simple: %v", err)
            return a.runSimple(ctx, prompt)
        }

        // 2. Orchestrate execution
        orchestrator := planning.NewWBSOrchestrator(
            a.subagent, store, sessionState, a.logger,
        )
        if err := orchestrator.Execute(ctx, tree, currentMessages); err != nil {
            return "", err
        }

        // 3. Collect results from completed nodes
        return collectWBSResults(tree), nil
    }
    ```
*   **Logic**:
    *   `router` が nil の場合は常に単純実行ルート（後方互換性）
    *   WBS生成に失敗した場合は単純実行にフォールバック
    *   WBSの各ノード結果は最後にまとめてユーザーへの最終回答として返却

#### [MODIFY] [session_state.go](file://shared/libs/go/wayfinder/session/session_state.go)
*   **Description**: SessionStateにWBSTree状態を追加し、レジュームに対応
*   **Technical Design**:
    ```go
    // SessionState に WBSTree を追加
    type SessionState struct {
        SessionID        string           `json:"session_id"`
        ParentID         *string          `json:"parent_id,omitempty"`
        Status           string           `json:"status"`
        Messages         []Message        `json:"messages"`
        CreatedFiles     []TrackedFile    `json:"created_files"`
        RunningProcesses []TrackedProcess `json:"running_processes"`
        WBSTree          *WBSTree         `json:"wbs_tree,omitempty"` // Part 4 addition
        CreatedAt        time.Time        `json:"created_at"`
        LastActivityAt   time.Time        `json:"last_activity_at"`
    }
    ```
*   **Logic**:
    *   `WBSTree` はオプショナル。単純実行モードの場合は nil。
    *   セッション復旧時、`WBSTree` が nil でなければ計画実行モードの途中であると判断し、レジュームする。
    *   完了済みノード (`completed`) はスキップし、`pending` かつ依存解消済みのノードから再開する。

---

### テストファイル (TDD: テストを先に記述)

#### [NEW] [wbs_tree_test.go](file://shared/libs/go/wayfinder/planning/wbs_tree_test.go)
*   **テストケース**:
    *   `TestWBSTree_NextExecutableNodes_NoDeps`: 依存なしの全pendingノード -> 全て返る
    *   `TestWBSTree_NextExecutableNodes_WithDeps`: 依存先がcompletedのノードのみ返る
    *   `TestWBSTree_NextExecutableNodes_BlockedByFailed`: 依存先がfailed -> 返らない
    *   `TestWBSTree_IsComplete`: 全completed -> true
    *   `TestWBSTree_IsComplete_Mixed`: pending残り -> false
    *   `TestWBSTree_HasFailed`: failedあり -> true
    *   `TestWBSTree_IsDeadlocked`: pending存在かつ実行可能なし -> true
    *   `TestWBSTree_UpdateNodeStatus`: ステータス更新が正しく反映されること
    *   `TestWBSTree_Serialization`: WBSTreeをJSON化・復元し全ノード一致
    *   `TestWBSTree_NestedSubSteps`: サブステップを含むツリーのトラバースが正しいこと

#### [NEW] [wbs_planner_test.go](file://shared/libs/go/wayfinder/planning/wbs_planner_test.go)
*   **テストケース**:
    *   `TestWBSPlanner_GenerateWBS_Success`: MockLLMで有効なWBS JSONが返り、パースされること
    *   `TestWBSPlanner_GenerateWBS_InvalidJSON`: 不正なJSONが返った場合にエラー
    *   `TestWBSPlanner_GenerateWBS_AllStatusesPending`: 生成されたノードのステータスが全てpendingに正規化されること
    *   `TestWBSPlanner_ExtractJSON_CodeBlock`: Markdownコードブロックで囲まれたJSONが正しく抽出されること

#### [NEW] [wbs_orchestrator_test.go](file://shared/libs/go/wayfinder/planning/wbs_orchestrator_test.go)
*   **テストケース**:
    *   `TestWBSOrchestrator_Execute_AllComplete`: 全ノード正常完了 -> nilを返す
    *   `TestWBSOrchestrator_Execute_NodeFailure`: ノード失敗 -> エラーを返し後続ノードがブロックされること
    *   `TestWBSOrchestrator_Execute_Deadlock`: デッドロック検出 -> エラー
    *   `TestWBSOrchestrator_Execute_DependencyOrder`: 依存関係の順序に従って実行されること
    *   `TestWBSOrchestrator_Resume_SkipCompleted`: completedノードをスキップしてpendingから再開すること
    *   `TestWBSOrchestrator_PersistAfterEachNode`: 各ノード完了後にセッションが保存されること

#### [NEW] [execution_router_test.go](file://shared/libs/go/wayfinder/execution_router_test.go)
*   **テストケース**:
    *   `TestExecutionRouter_SimpleTask`: 単純なタスク -> RouteSimple
    *   `TestExecutionRouter_ComplexTask`: 複雑なタスク -> RoutePlanning
    *   `TestExecutionRouter_LLMError`: LLMエラー -> RouteSimple (フォールバック)
    *   `TestExecutionRouter_InvalidJSON`: 不正なJSON応答 -> RouteSimple (フォールバック)

## Step-by-Step Implementation Guide

1.  **WBSTree構造体と依存関係解決の実装** (TDD: テスト先行):
    *   `wbs_tree_test.go` を作成し、全テストケースを記述 -> 失敗確認
    *   `wbs_tree.go` に `WBSNode`, `WBSTree`, `NextExecutableNodes`, `IsComplete`, `HasFailed`, `IsDeadlocked`, `UpdateNodeStatus`, `walkNodes` を実装
    *   `git commit -m "feat(wayfinder): add WBSTree struct with dependency resolution"`

2.  **WBSPlannerの実装** (TDD: テスト先行):
    *   `wbs_planner_test.go` を作成し、全テストケースを記述 -> 失敗確認
    *   `wbs_planner.go` に `WBSPlanner`, `GenerateWBS`, `wbsPlannerSystemPrompt`, `extractJSON` を実装
    *   `git commit -m "feat(wayfinder): add WBSPlanner for LLM-based plan generation"`

3.  **ExecutionRouterの実装** (TDD: テスト先行):
    *   `execution_router_test.go` を作成し、全テストケースを記述 -> 失敗確認
    *   `execution_router.go` に `ExecutionRouter`, `Route`, `routerSystemPrompt` を実装
    *   `git commit -m "feat(wayfinder): add ExecutionRouter for simple/planning route branching"`

4.  **WBSOrchestratorの実装** (TDD: テスト先行):
    *   `wbs_orchestrator_test.go` を作成し、全テストケースを記述 -> 失敗確認
    *   `wbs_orchestrator.go` に `WBSOrchestrator`, `Execute`, `handleFailure`, `persistWBSState` を実装
    *   `git commit -m "feat(wayfinder): add WBSOrchestrator for sequential plan execution"`

5.  **SessionStateへのWBSTree統合**:
    *   `session_state.go` に `WBSTree` フィールドを追加
    *   WBSTreeの永続化・復元テストを追加
    *   `git commit -m "feat(wayfinder): add WBSTree to SessionState for resume support"`

6.  **AgentCoreへの統合**:
    *   `agent_core.go` に `router`, `planner` フィールドを追加
    *   `Run` メソッドに分岐ロジック (`runSimple` / `runWithPlanning`) を実装
    *   `git commit -m "feat(wayfinder): integrate execution routing and WBS orchestration into AgentCore"`

7.  **ビルド・テスト実行**:
    *   Verification Planに従い全テスト実行

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```

2.  **Integration Tests**:
    ```bash
    ./scripts/process/integration_test.sh --categories taskengine,llm --specify "TestWayfinderWBS"
    ```
    *   **Log Verification**: WBSツリーの生成ログ、各ノードのステータス遷移ログ、セッションファイルへの保存ログを確認。

3.  **E2E Tests (新規)**:

    #### [NEW] [wayfinder_wbs_test.go](file://tests/wayfinder_wbs_test.go)
    *   **テストケース**: `TestWayfinderE2E_WBSPlanGeneration`
        *   MockLLMClientを使用し、複雑な要求に対してWBSツリーが生成されること
        *   ツリー構造(ルートノード、依存関係)が有効であること
    *   **テストケース**: `TestWayfinderE2E_WBSExecution`
        *   3ステップのWBS (Step1 -> Step2 -> Step3) を順次実行
        *   各ステップがcompletedになり、ResultSummaryが記録されること
    *   **テストケース**: `TestWayfinderE2E_WBSResume`
        *   WBS実行中にStep2で処理を中断(ctx.Cancel)
        *   同SessionIDで再起動 -> Step1がスキップされStep2から再開すること
    *   **テストケース**: `TestWayfinderE2E_WBSErrorRecovery`
        *   Step2で意図的に失敗 -> Step3がブロックされること
        *   エラーレポートにStep2の失敗理由が含まれること
    *   **テストケース**: `TestWayfinderE2E_ExecutionRouting`
        *   単純な要求 -> runSimpleが呼ばれること
        *   複雑な要求 -> runWithPlanningが呼ばれること
    *   **検証ポイント**: WBS計画生成の正確性、依存関係を考慮した順次実行、レジューム動作、エラー時の一時停止

### テスト項目のセルフレビュー (testing-rules 11.4)

1. **網羅性**: WBSTree操作(依存解決、完了判定、失敗判定、デッドロック検出)、WBS計画生成(成功/失敗/JSON抽出)、オーケストレーション(正常完了/失敗/レジューム/永続化)、ルーティング(単純/複雑/フォールバック)を全てカバー。
2. **証拠の十分性**: WBSTreeの状態遷移をステータスマップで検証。オーケストレーションはMockSubagentExecutorで呼び出し順序を記録して検証。
3. **迂回排除**: MockLLMClient、MockSubagentExecutorを使用し外部依存なし。
4. **依存関係**: wbs_tree -> wbs_planner -> execution_router -> wbs_orchestrator -> agent_core統合 の順にボトムアップ。

### 総合判定プロセス (testing-rules 12)

全テスト完了後、testing-rules 12.2のチェック項目を確認し、総合判定を記録する。

## Documentation

本計画は新規パッケージの作成のため、既存ドキュメントへの影響はない。

---

## 継続計画について

本計画はWayfinder Agent実装の **Part 4/4 (最終)** です。

- **Part 1** ([000-Wayfinder-AgentCore-Tools-LLMGP.md](file://prompts/phases/000-foundation/branches/feat-wayfinder-agent/plans/000-Wayfinder-AgentCore-Tools-LLMGP.md)): エージェントコア、ツール、ガードレール、LLMGP統合
- **Part 2** ([001-Wayfinder-Session-Persistence.md](file://prompts/phases/000-foundation/branches/feat-wayfinder-agent/plans/001-Wayfinder-Session-Persistence.md)): セッション管理、永続化、コンパクション
- **Part 3** ([002-Wayfinder-Subagent-Summarization.md](file://prompts/phases/000-foundation/branches/feat-wayfinder-agent/plans/002-Wayfinder-Subagent-Summarization.md)): サブエージェント連携、要約

全4パートの実装が完了することで、Wayfinder Agentは以下の完全な機能を持つ:
- AgentCoreによるTool Callingループ
- ガードレール（パス境界検証、危険コマンドブロック、削除許可リスト、適合パスパターン）
- LLMGP/Bifrost経由のLLM接続
- ファイルベースセッション永続化（アトミック書き込み、トラッカー整合性検証、コンパクション）
- サブエージェント連携（ヒント生成、結果要約、階層セッションログ）
- 実行ブランチ分岐（単純実行 / WBS計画・実行オーケストレーション）
- WBSレジュームとエラーリカバリー
