# Emergency-stop source reconciliation

## Reviewed scope

Reviewed both retained emergency-stop implementations, the older artifact
emergency-stop test file, and both retained operator stop fan-out test files:
five source versions. This does not complete the worktree or archive review.

Keep the current durable stop-revision provider and context-aware commit fence.
Do not restore the older artifact implementation that omits those boundaries.
Retain lifecycle-owned monitor admission, cancellation-aware monitor shutdown,
fail-closed missing/unreadable controls, redacted reasons and deployment-level
hard stops that cannot be cleared by an application operator.

The retained fan-out tests contain the same behavior. Current tests additionally
supply an operator identity and explicit stop reason. Keep their assertions that
the stop is persisted before fan-out, partial delivery does not clear it, a
cancellation acknowledgment is not terminal completion, and local stop
verification does not cancel real OpenClaw runs. Do not weaken authentication
fixtures to restore older handler signatures.

## Commit-fence cancellation repair

The context-aware read-lock loop checked cancellation once before waiting.
Cancellation between that check and successful lock admission could return a
usable release function with no error. Recheck before every acquisition attempt
and after successful acquisition; release the read lock before returning an
admission error.

A deterministic context wrapper captures the initial clear snapshot and then
cancels before admission. The regression failed before the repair, returning a
release function and no error. The repaired test also verifies that rejection
does not retain a lock. A live-context positive control verifies mutual exclusion
and correct release. Existing real-deadline lock-contention coverage remains.

This is a cancellation recheck at lock admission, not a claim that cancellation
cannot happen afterward. Callers must still honor their context and perform
their existing final-effect authorization checks.

## Verification and preservation

The safety, operator-control, execution-authorization and execution-approval
Go packages passed together with uncached tests on Windows and Go 1.27.3.
Database tests without a configured dedicated test DSN
remain separate from local package acceptance. No live runtime was cancelled
and no operator stop was changed by these synthetic tests.

The private ledger records exact retained source fingerprints and main revision.
Semantic review alone is not proof of exact preservation on main; all original
worktrees, archives and transcripts remain intact.
