# 004-Usage-Default-Model-Backfill

> **Source Specification**: [prompts/phases/001-phase02/branches/feat-token-counter/ideas/004-Usage-Default-Model-Backfill.md](file://prompts/phases/001-phase02/branches/feat-token-counter/ideas/004-Usage-Default-Model-Backfill.md)

## Goal Description

モデル未指定でセッションを作成したとき、`Server.gatewayDefault` を `record.Model` に確定し、Create / GetSession / usage（`model_source: tern_session`）で空欄にならないようにする。

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1 CreateSession で gatewayDefault 適用 | Proposed Changes > handler.go / effectiveSessionModel |
| R1 ターン開始時の保険バックフィル | Proposed Changes > handler.go SendMessage / handler_retry.go |
| R2 usage は確定済み sessionModel + tern_session | Existing ApplyModelAttribution（変更なし）+ sessionModel 入力改善 |
| R3 Create / GetSession が model を返す | Create レスポンスに model 追加; GetSession は record.Model |
| R4 ギャップテストを期待挙動へ更新 + 追加ケース | handler_usage_test.go |
| S2 既存空 model セッションのターン開始時埋込 | ターン開始バックフィル |
| E2E モデル省略 | tests/token_usage_e2e_test.go |

## Proposed Changes

### agentservice — tests first (TDD)

#### [MODIFY] [handler_usage_test.go](file://shared/libs/go/agentservice/handler_usage_test.go)
*   **Description**: ギャップ確認テストを望ましい挙動テストへ置換し、Must 追加ケースを足す。
*   **Technical Design**: テスト名を `TestHandleSendMessage_OmittedModel_UsesGatewayDefaultForUsage` に変更（旧 `...UsageModelEmptyDespiteGatewayDefault` を置換）。
*   **Logic**:
    1. **置換テスト**（gatewayDefault あり + model 省略）:
       - Create 後: `created["model"] == "claude-sonnet-4-20250514"`（Create レスポンスに model を載せる前提）
       - GetSession: `model` 同値
       - `result.usage.model` / `model_source` → デフォルト / `tern_session`
       - `GET /usage` ターン同値
    2. **追加** `TestHandleSendMessage_OmittedModel_NoGatewayDefault_UsageModelEmpty`:
       - SetGatewayModels しない（または default nil）
       - model 省略 → Create/usage の model 空のまま
    3. **追加** `TestHandleSendMessage_AgentTelemetryModel_PrefersAgent`:
       - mock が Usage.Model を返す → `model_source=agent`、gatewayDefault で上書きしない
    4. 既存 `TestHandleSendMessage_ResultUsageAndGetUsage`（明示 model `"m"`）は回帰としてそのまま通す

### agentservice — implementation

#### [MODIFY] [service.go](file://shared/libs/go/agentservice/service.go) または同パッケージの適切なファイル
*   **Description**: 有効セッションモデル解決ヘルパを追加。
*   **Technical Design**:
```go
// effectiveSessionModel returns recordModel, or gatewayDefault.Model when record is empty.
func (s *Server) effectiveSessionModel(recordModel string) string {
	if recordModel != "" {
		return recordModel
	}
	if s.gatewayDefault != nil && s.gatewayDefault.Model != "" {
		return s.gatewayDefault.Model
	}
	return ""
}
```
*   **Logic**: 明示 model は上書きしない。gatewayDefault 無し / Model 空は空文字を返す。

#### [MODIFY] [handler.go](file://shared/libs/go/agentservice/handler.go)
*   **Description**: CreateSession でデフォルト適用し、Create レスポンスに `model` を含める。SendMessage 開始時にも空なら書き戻す。
*   **Technical Design**:
    - `handleCreateSession`: `req.Model` Resolve/Validate の後、`req.Model = s.effectiveSessionModel(req.Model)`（または `record.Model = s.effectiveSessionModel(req.Model)`）を実行してから `SessionRecord` を構築。
    - Create レスポンスを拡張:
```go
json.NewEncoder(w).Encode(map[string]string{
	"session_id": sessionID,
	"status":     "created",
	"model":      record.Model,
})
```
    - `handleSendMessage`（runTurn 登録前）: `record.Model` が空なら `effectiveSessionModel` で埋め、非空になったら `sessions` に永続化（既存 Update/Create 相当の API があれば使用。無ければ Get→書き換え→Save パターンに合わせる）。
*   **Logic**（仕様継承）:
```text
CreateSession(model omitted)
  → record.Model = gatewayDefault.Model（ある場合）
SendMessage
  → if record.Model == "" { record.Model = effectiveSessionModel("") ; persist }
  → sessionModel = record.Model
  → ApplyModelAttribution(u, sessionModel)  // 既存
```
*   **Logging**: DEBUG `session model default applied` with `session_id`, `model`.

#### [MODIFY] [handler_retry.go](file://shared/libs/go/agentservice/handler_retry.go)
*   **Description**: `activeExecution.sessionModel` 設定直前に空 model の保険適用を保証する（SendMessage 側で埋めた場合は no-op）。
*   **Logic**:
```go
sessionModel: record.Model, // 直前に effectiveSessionModel 適用済みであること
```
  SendMessage 側で適用する場合はここはコメントのみでも可。ただし `record.Model` がスナップショット時点で確定済みであることを保証する。

### E2E

#### [MODIFY] [token_usage_e2e_test.go](file://tests/token_usage_e2e_test.go)
*   **Description**: モデル省略パスの E2E を追加。
*   **Logic**:
    - `createE2ESessionNoModel` でセッション作成
    - `GetSession` の `Model` が非空（サーバに default_model がある前提; 無ければ Skip）
    - SendMessage 後、ターン usage の `model` が非空、かつ `model_source` が `agent` または `tern_session`

### docs

#### [MODIFY] [ReferenceManual-WebAPIs.md](file://docs/ReferenceManual-WebAPIs.md)
*   **Description**: CreateSession の `model` 省略時に `default_model` をセッションへ確定する旨、および Create レスポンス `model` を追記。usage の `tern_session` 説明を「明示またはデフォルト適用後のセッション model」に更新。

## Step-by-Step Implementation Guide

- [x] 1. **TDD fail**: Update/rename omitted-model usage test to expect gateway default on Create/GetUsage/`tern_session`; add no-default and agent-prefer tests; run until they fail for the right reason.
- [x] 2. **Helper**: Add `effectiveSessionModel` on `Server`.
- [x] 3. **CreateSession**: Apply helper to `record.Model`; include `model` in 201 response; DEBUG log.
- [x] 4. **SendMessage / turn start**: If `record.Model == ""`, apply helper, persist, then snapshot into `activeExecution.sessionModel`.
- [x] 5. **E2E**: Add omitted-model TokenUsage case using `createE2ESessionNoModel`.
- [x] 6. **Docs**: Update ReferenceManual-WebAPIs.md.
- [x] 7. **Verify**: `./scripts/process/build.sh` then `./scripts/process/integration_test.sh --specify "TokenUsage"`.

## Verification Plan

### Automated Verification

1. **Build & Unit Tests**: `./scripts/process/build.sh`
2. **Integration Tests**: `./scripts/process/integration_test.sh --specify "TokenUsage"`
3. **E2E Tests**: `tests/token_usage_e2e_test.go` にモデル省略ケースを追加し、上記 `--specify "TokenUsage"` で実行する

## Documentation

- `docs/ReferenceManual-WebAPIs.md`: CreateSession `model` Optional の挙動（default_model 確定）、Create 201 の `model` フィールド、usage `tern_session` の意味更新
- 仕様 `ideas/004-...` の受け入れ基準は実装完了時に計画チェックボックスで追跡（仕様ファイルの checkbox 更新は任意）
