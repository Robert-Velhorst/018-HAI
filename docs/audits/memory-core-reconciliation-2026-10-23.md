# Memory core source reconciliation

## Scope

Reviewed the meaningful differences for four retained memory core source versions
against the current implementation: `handler.go`, `handler_test.go`, `service.go`,
and `service_test.go` under `backend/internal/memory`.

This is not a review of the entire memory subsystem or worktree. The separate
source-extraction lesson files and source-correction repository files remain
outside this completed comparison.

## Decisions

| Area | Decision |
| --- | --- |
| Owner boundary | Keep authenticated-owner writes and exact stored-owner matching; do not reinstate whitespace-altered legacy ownership. |
| Verified facts | Keep the dedicated verified-fact writer with durable repository validation; generic create/update cannot mint or rewrite review-managed facts. |
| Provenance | Keep source-aware exact deduplication and source/run separation for verified facts; do not restore approximate merging of review-managed facts. |
| Concurrent creation | Keep bounded striped locks, unambiguous lock keys, canonical-content comparison after hash matches, and repository deduplication support. |
| Storage and HTTP limits | Keep bounded UTF-8 text, tag and confidence validation, and bounded HTTP body parsing before persistence. |
| Semantic index | Keep removal of stale vectors before SQL updates, explicit failure reporting, keyword fallback, and owner-scoped reindex recovery. |
| API privacy | Keep response cloning and internal extraction-lineage redaction without altering stored evidence links. |
| Querying | Keep owner-scoped paginated queries and repair cancellation handling at the service boundary rather than restoring unbounded legacy listing. |
| Regression coverage | Keep the newer owner, provenance, size-limit, concurrent-deduplication, and index-recovery tests. |

## Verification and limits

The complete local memory package passed after the cancellation repair. The
new cancellation regressions failed before the repair, as detailed in
[the focused audit](memory-query-cancellation-2026-10-23.md).

The four original versions were freshly checked against their frozen inventory
fingerprints and retained. This semantic comparison is not byte-preservation
proof, a live database acceptance result, or authorization to discard the
worktree or transcript archives. No original source files were deleted.
