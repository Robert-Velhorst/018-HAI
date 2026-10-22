# Artifact source reconciliation

## Scope

Review the complete meaningful differences in the nine present selected
`backend/internal/openclawreconcile/` paths from the retained artifact worktree
against main commit `30781d4fb393386aecab0c449d100d158f4eabf5`. Verify raw
original content separately against the private inventory. No missing retained
implementation was found in this selection; keep the newer implementations.

This does not establish completion of the entire worktree or transcript review.

## Per-file decisions

| File | Decision |
| --- | --- |
| `artifact_archive.go` | Keep exact owner/execution/source-event SQL scope for listing and reads, deterministic retention order, cold-instance key validation and owner-scoped quota serialization. Avoid restoring archive-wide serialization of every warm retain. |
| `artifact_archive_test.go` | Keep the dry-run SQL test binding the exact source event. It does not prove PostgreSQL isolation by itself. |
| `artifact_forget.go` | Keep policy limits independent of aggregate destination fields, exact event and launch binding, idempotence only with matching prior removal audit and authentication of ciphertext before removal. Preserve unreadable content rather than deleting under a wrong key. |
| `artifact_forget_test.go` | Keep wrong-key/corruption preservation, stale fingerprint rejection and exact-audit idempotence assertions. |
| `artifact_recovery.go` | Keep bounded claims with unique fencing tokens, explicit lease expiry, exact attempt/token completion, failure release and bounded descriptor reads. Stale readers must not settle a newer claim. Recovery does not rerun the original task. |
| `artifact_recovery_test.go` | Keep explicit claim-release assertions for failed reads and wrong-owner binding, while pause leaves attempts untouched. |
| `artifact_view.go` | Keep event-scoped retained-only audit views, exact download event binding, honest collecting/exhausted states and actionable unreadable-copy errors. Independently repair legacy metadata masking a non-completed execution state. |
| `artifact_view_test.go` | Keep event mismatch rejection before content access, active-final-claim versus exhaustion tests, retained-audit filtering and safe unreadable-content responses. |
| `scheduler.go` | Keep lifecycle-tracked durable reconciliation startup; do not restore an untracked background goroutine. |

## Display-state repair

The legacy-metadata fallback ran after execution-state selection and replaced
both `waiting_for_completion` and `unavailable` with `metadata_available` when
old metadata existed. Failed, cancelled and not-yet-completed work could therefore
lose its meaningful execution state in the artifact response.

Apply the metadata fallback only to `pending` collection after a completed
execution. Preserve all metadata items for inspection. Existing collecting,
captured and exhausted states remain governed by their current rules. This
does not grant download authority or alter the underlying execution receipt.

## Verification

- New regression cases reproduced four state-masking failures before the fix:
  running, failed, cancelled and an unspecified terminal outcome.
- The completed legacy-metadata case remains supported.
- The full package passed locally on Windows with Go 1.27.3:
  `go test -p 1 -timeout 180s ./internal/openclawreconcile -count=1`.
- All nine original raw hashes matched the private inventory.
- PostgreSQL integration-tagged tests, a rendered UI session and real gateway
  account acceptance were not executed in this batch. Hosted exact-head CI
  remains a separate merge gate.

## Retention

The original versions remain intact. This semantic comparison and a passing
local test run do not prove byte-for-byte preservation on main and do not
authorize whole-worktree or archive removal. No database, retained ciphertext,
credentials, transcript or installed runtime was removed in this batch.
