# Verification audit acknowledgement and disclosure

The previous creation/record reviews left completion-audit binding and advisory
audit reporting open. The repository contract returned an audit object, but the
service checked only its error. Missing IDs, a different run, a different action
or changed message could therefore be treated as a durable audit. An unconfirmed
memory opt-in could authorize downstream memory promotion.

A shared internal audit persistence function now snapshots the expected event
before calling storage. A nonempty returned ID is mandatory. Database-assigned
IDs and creation timestamps are accepted, but the run, action, message and any
explicitly supplied ID must match the independent snapshot. Completion uses this
function inside its existing transaction; an invalid acknowledgement aborts that
transaction before the run is finalized. Other audit callers use the same check.

Memory promotion remains optional: an unconfirmed opt-in blocks promotion while
allowing the verification answer itself to complete. The response identifies
the blocked-memory reason. An acknowledgement still does not independently
prove that storage committed correctly, and a rejected response does not prove
that no row was inserted.

Semantic projection previously stored raw provider errors in its failure audit,
although the response was redacted. That audit now stores only the bounded
failure summary. If either projection-success or projection-failure auditing
fails, the response includes the exact audit action in `AuditWarnings` and a
concise diagnostic log entry. Already committed memory/projection work is not
replayed or falsely reported as rolled back.

Regression checks first demonstrated all five acknowledgement failures, missing
completion acknowledgement, unconfirmed memory promotion, persisted-error
leakage and both hidden secondary-audit failures. Positive coverage retains
database-managed audit metadata. These are local repository-double checks, not
production database or provider acceptance.

The complete native verification package then passed locally: 182 named
test/subtest PASS events, zero failures and zero skips. The response-redaction
regression also checks the persisted audit, not only the returned JSON.

Cancellation-status/audit failure reporting, durable replay behavior and the
separate global-control owner-policy choice still require review. No database,
schema, account permission, credential, source archive or original checkout is
changed or made deletion-eligible by this checkpoint.
