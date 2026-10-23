# Automation launch projection storage boundary

## Finding and implementation

The launch-state projection did not apply the storage/context boundary already
used by the runtime-stop summary. Unavailable storage could panic; a dry-run or
cancelled storage operation could be acknowledged as successful. This concerns
the recorded launch projection, not proof that an external runtime started.

`UpdateLaunchState` now rejects unavailable, uninitialized and dry-run storage,
requires the repository context, derives the existing bounded storage deadline,
and preserves cancellation errors after both writes and fallback reads. It does
not mutate the shared repository context. The newer-launch timestamp fence and
the distinction between stale updates and missing records remain unchanged.

## Verification

All eight new regression cases failed before the change: bounded context,
cancellation before/during storage, missing context, missing repository,
missing database, uninitialized database, and dry-run storage.

After the change, the full automation, execution authorization and execution
approval Go packages passed. The regression fixture exercises the real GORM
repository using an injected connection pool; it is not a live PostgreSQL or
external runtime acceptance test.

## Reconciliation boundary

This follows review of the selected approval-proof, approval-boundary, HTTP
handler, health-handler, health-handler-test and repository source versions in
the two audit worktrees. Existing main protections for immutable review
configuration, issuance fencing, redacted responses, bounded request bodies,
conflict recovery and launch-event idempotency are retained rather than replaced
by older implementations. Automation-service and execution-authorization source
variants require separate review. Original worktrees and archives are retained;
semantic review and PR publication alone do not authorize their deletion.
