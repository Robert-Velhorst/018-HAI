# Workflow repository variant reconciliation

## Scope and method

Two retained `backend/internal/workflow/repository.go` variants were compared
with the published repository at `13933b0a744b37f671d39ac2651767b10845e3d1`:

| Retained variant | Bytes | Declarations | Token-identical | Changed | Removed |
| --- | ---: | ---: | ---: | ---: | ---: |
| Model-maintenance audit | 78,118 | 89 | 84 | 5 | 0 |
| OpenClaw artifact audit | 75,941 | 89 | 81 | 8 | 0 |

The complete files were parsed with Go's standard AST parser. Each declaration
was compared using length-delimited token streams that retain literal content,
statement order, comments and attached directives, while ignoring insignificant
spacing. Five comparator checks cover whitespace, literal changes, directives,
statement order and receiver/declaration coverage. Cgo and ambiguous declaration
identities are rejected rather than silently accepted.

The complete textual diffs and changed method contexts were manually read.
This does **not** claim a full manual reread of every token-identical method.
Declaration-body identity does not prove unchanged shared dependencies, file
directives, initialization behavior, database execution or whole-file runtime
equivalence. No file is made deletable by this comparison.

## Keep the newer implementation

- Keep persisted-row reloads after workflow creation so later comparisons use
  stored identity/revision/timestamp state, not only the original input object.
- Keep `requires_approval` and `approval_reason` in claimed-item updates. The
  older variants could omit newly required approval state from persistence.
- Keep the materialized row-lock boundary before database wall-clock expiry
  checks in runnable-item and open-loop lease renewal. Do not restore the older
  unconditional renewal updates.
- Keep the stronger completion-attestation validator rather than only checking
  workflow ID and completion time.
- Keep the new `CommitChecklistUpdate` operation and its repository interface
  contract: parent lock, expected status/timestamp comparison, status update,
  audit event, and persisted-row reload are one transaction.
- Keep the manual workflow-transition interface addition, whose implementation
  belongs to the separate atomic-transition source, not a missing old method.

All retained declarations remain represented. The published repository has 90
declarations, adding `CommitChecklistUpdate`. No older implementation was copied
over the stricter current one, and no production repository edit was needed.

## Verification and open boundaries

The same published repository blob participated in the successful full workflow
package test command recorded in the follow-up verification audit. Local
claim-renewal SQL-shape checks and result-semantics tests are not proof of real
PostgreSQL lock behavior; database-dependent tests require their owned fixture.
No database, account, provider, Docker or deployment mutation was performed by
this source comparison.

The treatment of expired claims during finalization, rather than renewal, still
requires separate concurrency review. Review-only failure transitions must not
be confused with verified completion. Do not add a blanket expiry predicate
without checking how uncertain outcomes remain visible and recoverable.

The large service/test variants, other HAI sources and transcript archives remain
open. Publication, exact-head CI, preservation on `main`, and source deletion are
separate gates. The retained originals remain intact.
