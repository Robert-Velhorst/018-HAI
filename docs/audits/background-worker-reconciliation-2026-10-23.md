# Background worker reconciliation

## Reviewed scope

The complete original/current deltas of `backend/internal/background/worker.go`,
`worker_test.go`, and `worker_reliability_test.go` were read. Their current Git
blobs match the inspected `main`. The original copies remain retained and their
raw inventory hashes were checked again; this is not an archive-wide review.

The current worker preserves run-lease acquisition, expired-claim recovery,
ingestion, claim generation/heartbeat fencing and subsequent operation handling.
It adds a mode check after ingestion: paused mode leaves new records untouched
and defers previously classified/ready safe-local work; modes that prohibit
background processing return before ordinary processing. The existing emergency
stop still blocks ordinary processing. Recovery and recording inputs are not
described as execution of those inputs.

The changed claim/dispatch contexts and existing pause, cancellation, lease,
heartbeat, source-approval and draft-recovery assertions were inspected. This
does not certify every race or persistence boundary in their dependencies.
The current heartbeat and claim logic is not replaced with the older file.

## Report privacy and boundedness

Provider/feed errors are no longer appended verbatim to the public report.
The current helper uses fixed public messages and caps retained report errors
at twenty; server-side logging uses the existing secret redactor. The report's
error sentinel still communicates failure. This is not proof that every return
path or operator log is free of personal information; only the changed report
paths and relevant existing assertions were reviewed.

The original draft-recovery assertion expected the injected internal failure
text in the report. Its replacement requires a nonempty sanitized message and
continues checking recovery state. The feed-failure assertion likewise no longer
requires a private feed identifier in the public error.

## Test retention and policy intent

All original top-level tests in these two files remain. The current tests add
secret-leak and pause/defer cases. Reliability tests still verify that failed or
unprocessable old work does not starve new work, but now require a source-derived
item to await owner approval instead of treating automatic execution as success.
The pending record is inspected by title and state, so the changed assertion is
not satisfied merely by an empty queue.

The old reliability-only `failingFeedReader` type and its two methods are renamed
to `reliabilityFailingFeedReader` to coexist with the new configurable failure
reader in `worker_test.go`. That helper rename is explicitly accounted for;
declaration-key differences are not silently treated as lost product behavior.

These background tests were inspected, not rerun locally for this checkpoint.
The current published CI job runs the backend unit/race suites, but passing
in-memory fixtures is not evidence of live mail ingestion, real model execution
or production restart/recovery acceptance. No new integration was simulated.

## Consolidation decision

No original-only implementation needs restoring from these three deltas. Keep
the current pause, approval and sanitized-report behavior, alongside its newer
tests. Scope outside these files remains open. Neither this comparison nor the
existence of tests grants deletion eligibility to the original checkout or any
transcript archive.
