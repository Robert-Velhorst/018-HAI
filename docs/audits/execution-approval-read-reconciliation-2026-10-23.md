# Execution approval read reconciliation

## Scope

Reviewed ten retained source versions from the model-maintenance and artifact
worktrees against main `f94e00a25d5703f3c603a8ec0cd0c140364d8f8b`:

| Files | Versions | Decision |
| --- | ---: | --- |
| `backend/internal/executionauth/final_effect_bridge.go` and its test | 4 | Retain current cancellation checks before and after consumption lookup, and the distinction between in-memory expiry tests and PostgreSQL enforcement. |
| `backend/internal/executionauth/service.go` and its test | 2 | Retain context-aware authorization, exact approval revalidation, read-only inspection, and isolated emergency-stop evaluator cloning. |
| `backend/internal/executionauth/repository_postgres.go` and its test | 4 | Retain current owner-scoped provenance checks, single-use claim fences, transaction binding, database-clock expiry, and unambiguous advisory-lock identity. Repair the read-error ordering described below. |

This review does not establish that either entire worktree is integrated.
Private source fingerprints are retained locally; original copies remain intact.

## Defect and repair

`lockAndCheckApprovalClaim` checked an empty approval source before checking the
database read error. Both a query failure and a scan failure can leave that
output empty, causing the helper to return success instead of its read error.

The helper now handles missing rows explicitly, returns other read errors, and
only then accepts a successfully read empty approval source. The existing
missing-row and no-approval behavior is unchanged. This is a fail-closed helper
repair, not evidence of a demonstrated unauthorized final effect: other
transaction and write checks can also reject the operation.

## Verification

- Added four database/sql contract cases: query failure, scan failure, missing
  receipt, and a receipt without approval. The first two failed before the fix.
- Full `executionauth` and `executionapproval` package tests passed on Windows
  with Go 1.27.3 after the repair.
- The contract fixture uses an in-process SQL driver and no network/database.
  It does not prove real PostgreSQL concurrency, migrations, or live execution.
- No retained worktree files, transcripts, databases, or settings were deleted.
  Publication and semantic review alone are not main-preservation evidence.
