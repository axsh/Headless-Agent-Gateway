# 005-Embedded-Launch-GatewayDefault-Fetch

> **Source Specification**: `prompts/phases/001-phase02/branches/feat-token-counter/ideas/005-Embedded-Launch-GatewayDefault-Fetch.md`

## Goal Description

埋め込み `server.Launch` 完了後に `FetchModelsFromGateway()` を呼び、`gatewayDefault` / モデル一覧をキャッシュする。Issue #63（model 省略時にセッション／usage の model が空）を解消し、既存の再現 E2E を PASS にする。

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1 Launch 後に fetch、失敗は Warn で起動継続 | Proposed Changes > server/server.go |
| R2 GET /api/v1/models が LLMGP と整合 | Proposed Changes > E2E test（R4） |
| R3 model 省略セッションに既定が入る | Proposed Changes > E2E test（R4）＋既存 004 ロジック |
| R4 回帰 E2E（テスト側で fetch しない） | Proposed Changes > tests/embedded_gateway_default_e2e_test.go |
| R5 ドキュメント | Proposed Changes > README.md |
| O2（任意・本計画で実施）CLI の二重 fetch 削除 | Proposed Changes > features/tern/cmd/server.go |

## Proposed Changes

### server — Launch 配線

#### [MODIFY] [server/server_test.go](file://server/server_test.go)

*   **Description**: T2 — fetch 失敗でも `Launch` が error を返さないこと（TDD 先書き可）。
*   **Logic**:
    *   既存の Launch 成功テストパターンを流用し、AgentService の `gatewayURL` を到達不能 URL に向けたうえで `Launch` → `err == nil` を assert。
    *   実装が難しい場合は E2E（T1）を主検証とし、本テストは簡易（`FetchModelsFromGateway` が空 URL で nil を返す既存挙動の確認）でも可。
    *   **本計画の最小**: `TestLaunch_FetchModelsFailureDoesNotFailLaunch` を追加するなら、`agentservice` に一時的に壊れた `gatewayURL` を注入できる公開手段が無いため、**統合 E2E を主とし単体はスキップ可**。代わりに Launch 成功後に `GET /api/v1/models` の default が埋まることを E2E で担保（R2/R4）。

#### [MODIFY] [server/server.go](file://server/server.go)

*   **Description**: AgentService 起動成功後にモデルキャッシュを取得。
*   **Logic**（仕様書スニペットを継承）:

```go
	if err := s.agentService.Launch(ctx, agentPort); err != nil {
		return fmt.Errorf("tern: agentservice launch: %w", err)
	}
	s.logger.Debug("agent service launched", "port", s.agentService.Port())

	if err := s.agentService.FetchModelsFromGateway(); err != nil {
		s.logger.Warn("failed to fetch models from gateway", "error", err.Error())
	} else {
		s.logger.Debug("gateway models cached")
	}

	s.logger.Info("tern server started")
	return nil
```

*   fetch 失敗でも `Launch` は `nil` を返す（起動失敗にしない）。

### tests — 回帰 E2E

#### [MODIFY] [tests/embedded_gateway_default_e2e_test.go](file://tests/embedded_gateway_default_e2e_test.go)

*   **Description**: 再現テストを修正後の期待に合わせて維持／リネーム。
*   **Logic**:
    *   テスト関数名を `TestEmbeddedLaunch_CachesGatewayDefault` に変更（旧名 `Omits...` はバグ前提）。
    *   **テスト側で `FetchModelsFromGateway` を呼ばない**（現状どおり）。
    *   assert は成功期待のまま（現状 FAIL → 修正後 PASS）:
        *   Agent Service `default_model.model` == LLMGP のそれ
        *   `len(models) > 0`
        *   CreateSession（model 省略）後 GetSession の `model` == default
    *   コメントで Issue #63 / 「embedded Launch が fetch する」ことを明記。

#### [MODIFY] [tests/agentservice_e2e_test.go](file://tests/agentservice_e2e_test.go)

*   **Description**: `startE2EServer` の手動 fetch コメントを更新。
*   **Logic**: 「Mirror tern cmd」→ 「server.Launch now caches models; kept as redundant safety / historical mirror」程度に更新。手動 fetch 呼び出しは残してよい（冪等）。

### CLI — 二重 fetch 削除（O2）

#### [MODIFY] [features/tern/cmd/server.go](file://features/tern/cmd/server.go)

*   **Description**: `Launch` が fetch するため CLI 側の明示呼び出しを削除。
*   **Logic**: `FetchModelsFromGateway` ブロックと関連コメントを削除。`os` が未使用になれば import 整理。

### Documentation

#### [MODIFY] [README.md](file://README.md)

*   **Description**: 埋め込み `Launch` が gateway models / default_model をキャッシュする旨を短く追記。
*   **Logic**: Embedded Server 例（`srv.Launch(ctx)`）の直後に 1〜2 文:

```text
Launch also fetches LLMGP GET /v1/models into the Agent Service cache
(default_model and model list). Callers do not need to call
FetchModelsFromGateway themselves. CreateSession may omit model to use
that gateway default (see docs/ReferenceManual-WebAPIs.md).
```

#### [MODIFY] [docs/ReferenceManual-WebAPIs.md](file://docs/ReferenceManual-WebAPIs.md)（任意・軽微）

*   **Description**: `GET /api/v1/models` 節に「埋め込み `server.Launch` 後にキャッシュされる」一文を追加（既存の Create Session 説明と矛盾しない範囲）。

## Step-by-Step Implementation Guide

1. **[x] Plan commit**: 仕様＋本計画＋再現 E2E（未コミット分）を整理。
2. **[x] Wire Launch fetch**: `server/server.go` に上記 Logic を追加。
3. **[x] Rename/adjust E2E**: `embedded_gateway_default_e2e_test.go` を PASS 期待のまま維持し関数名更新；`startE2EServer` コメント更新。
4. **[x] CLI O2**: `features/tern/cmd/server.go` から二重 fetch 削除。
5. **[x] Docs**: README（＋必要なら ReferenceManual）。
6. **[x] Verify**: build → integration specify。
7. **[x] Commit / Push**: 成功後 push（ライブアップデート）。## Verification Plan

### Automated Verification

1. **Build & Unit Tests**:

```bash
./scripts/process/build.sh
```

2. **Integration / E2E**:

```bash
./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify 'TestEmbeddedLaunch_CachesGatewayDefault|TestEmbeddedLaunch_OmitsGatewayDefault'
```

（リネーム後は新名のみでよい）

3. **E2E Tests**: `tests/embedded_gateway_default_e2e_test.go`（本変更の主検証。テスト側で fetch しない）

4. **Optional regression**:

```bash
./scripts/process/integration_test.sh --specify 'TestClaudeCodeE2E_TokenUsage_OmittedModel'
```

### Documentation

*   [ ] README embedded Launch に fetch 説明
*   [ ] ReferenceManual（任意一文）

## Notes

*   O1（Create 時リトライ）は本計画スコープ外。
*   O3（AgentService.Launch 内 fetch）は不採用（仕様推奨案 A）。
