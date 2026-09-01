# 002-Token-Usage-Stream-UX

> **Source Specification**: [ideas/002-Token-Usage-Stream-UX.md](file://prompts/phases/001-phase02/branches/feat-token-counter/ideas/002-Token-Usage-Stream-UX.md)

## Goal Description

`examples/token-usage` を「ストリーム = 本文表示（`Output`）」「Usage = Send 完了後の `GetUsage`」に一本化する。`sendAndCollectUsage` を削除し、README と docs に利用パターン表を追記する。

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point |
| :--- | :--- |
| R1 利用パターン表 | `examples/token-usage/README.md`, `docs/ReferenceManual-WebAPIs.md` |
| R2 Example フロー変更 | `examples/token-usage/main.go` |
| R3 `stream.Output` 使用 | `examples/token-usage/main.go` |
| R4 001 supersede 注記 | `ideas/001-Token-Usage-Example.md` |
| S1 verbose-usage（Should） | README に Events ループ例を段落で記載（フラグは省略可） |
| T1 build example | `build.sh` examples ループ |
| T2 E2E 非退行 | `integration_test.sh --specify TokenUsage` |

## Proposed Changes

### examples/token-usage

#### [MODIFY] [main.go](file://examples/token-usage/main.go)
*   **Description**: Remove `sendAndCollectUsage`; use `stream.Output(os.Stdout)` for both sends.
*   **Logic**:
    ```go
    stream, err := session.SendText(ctx, "Create a file named ping.txt ...")
    if err := stream.Output(os.Stdout); err != nil { log.Fatalf(...) }
    stream, err = session.SendText(ctx, "Reply with exactly: ok")
    if err := stream.Output(os.Stdout); err != nil { log.Fatalf(...) }
    repAll, err := session.GetUsage(ctx)
    // ... existing Session/Turn/Call/LastN sections unchanged ...
    ```
*   **printUsage**: add `model` and `model_source` when present (003 実装後のフィールド表示).

#### [MODIFY] [README.md](file://examples/token-usage/README.md)
*   **Logic**: R1 表を転記。原則「Send 完了後 `GetUsage`」を明記。Events ループでの call usage は 1 段落。

### docs

#### [MODIFY] [ReferenceManual-WebAPIs.md](file://docs/ReferenceManual-WebAPIs.md)
*   **Logic**: SendMessage ストリーム vs `GET .../usage` の 1 段落（result.usage は即時スナップショット、完全レポートは GetUsage）。

### ideas

#### [MODIFY] [001-Token-Usage-Example.md](file://prompts/phases/001-phase02/branches/feat-token-counter/ideas/001-Token-Usage-Example.md)
*   **Logic**: R2 の Send 直後 result.Usage 表示は 002 で supersede と注記。

## Step-by-Step Implementation Guide

- [x] 1. Refactor `examples/token-usage/main.go`: remove `sendAndCollectUsage`, use `Output` twice.
- [x] 2. Update `printUsage` to show `model` / `model_source` (after 003).
- [x] 3. Update `examples/token-usage/README.md` with pattern table.
- [x] 4. Add stream vs GET usage paragraph to `docs/ReferenceManual-WebAPIs.md`.
- [x] 5. Note supersede in `ideas/001-Token-Usage-Example.md`.
- [x] 6. `./scripts/process/build.sh` && `./scripts/process/integration_test.sh --specify "TokenUsage"`

## Verification Plan

### Automated Verification

1. **Build & Unit Tests**: `./scripts/process/build.sh`
2. **Integration Tests**: `./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TokenUsage"`

### E2E Tests

既存 `TestClaudeCodeE2E_TokenUsage_*` 非退行。新規 E2E 不要（example は build コンパイルで足りる）。

## Documentation

- `examples/token-usage/README.md`
- `docs/ReferenceManual-WebAPIs.md`
- `ideas/001-Token-Usage-Example.md`（supersede 注記）
