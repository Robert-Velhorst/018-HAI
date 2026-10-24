# Model history repository reconciliation

## Scope and retained behavior

Reviewed both retained `backend/internal/llm/repository.go` variants against
current code. Preserve the current immutable daily paid-budget reservations and
settlements, conservative accounting for unfinished reservations, bounded history
projection and deterministic checked/created/ID ordering. These are newer than
the retained source; the older direct aggregate must not replace settlement-aware
accounting.

Preserve cancellable probe/maintenance history interfaces and post-read context
checks, UTC timestamps, redacted history records and fail-closed handling of
uncertain advisory-lock acquisition or release. This review does not claim a
full review of the larger policy and model-maintenance implementations.

## Lease admission correction

Lease acquisition previously dereferenced missing repository/database storage.
It could also return a successfully acquired lease after its admission context
had already been cancelled as the driver finished returning the result.

Check caller cancellation before accessing storage, reject unavailable storage
without a panic, and recheck cancellation after reading the lock result. On late
cancellation discard the underlying database session, preventing a potentially
held session lock from being returned to the pool. No release callback or
execution authority is returned for that cancelled admission.

The existing caller already checks its parent context and defers valid lease
release. The new repository guard additionally covers expiration of the separate
lease-acquisition timeout while the parent request remains active.

## Verification and limits

Four cases failed before correction: cancelled/missing repository, missing
repository, missing database and cancellation at successful result closure.
The positive control acquires a lease and calls its release twice. Driver
fixtures check that successful unlock preserves a reusable connection while
cancelled admission closes its session and the next borrower opens a fresh one.

These deterministic database/sql driver tests do not demonstrate a live
PostgreSQL lock lifecycle or real model/provider acceptance. An additional
required CI test uses two independent bounded PostgreSQL pools to verify
contention, idempotent release and reacquisition by both sessions. It uses the
existing disposable CI service, an exact dedicated loopback-only database,
explicit test opt-in, bounded connection/operation times and no schema changes.
A skipped lifecycle test cannot satisfy its required PASS marker. Locally it
skips without the dedicated DSN; actual PostgreSQL execution remains a CI gate.
Original worktree
files remain intact; publication and semantic review do not establish main
preservation or deletion eligibility.
