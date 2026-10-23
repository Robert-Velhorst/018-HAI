# Semantic retrieval and source evidence reconciliation

## Reviewed source decisions

Reviewed six selected source versions: both audit worktrees' semantic service
and service tests, plus the model-audit source-evidence repository and tests.
Corresponding semantic variants match after line-ending normalization.

Retain main's exact owner predicate for authenticated memory ranking. Do not
restore the older inclusion of ownerless legacy memories in that path.

Retain source-evidence snapshot version 2, raw-record payload SHA-256 binding,
extraction payload binding and the bounded PostgreSQL execution handle. The
older implementation omits the raw payload digest and its mutation regressions.
Keep connector fingerprint compatibility rather than confusing a connector's
short content fingerprint with the separately computed cryptographic payload
digest.

## Additional defect and fix

Both semantic searches rank IDs in one query and load records in another.
The second reads previously filtered only by ID. A record's owner, project,
archival, source revocation or sensitivity could change between these reads
without the load rechecking its current eligibility.

Memory record loading now rechecks archival state, exact requested owner and
the existing project/shared-memory rules. Source extraction loading now joins
the current source record and repeats revocation, archival, sensitivity,
project and owner predicates. Existing trusted-unscoped and legacy shared-source
contracts are unchanged; this is not a broader legacy-ownership migration.

The response preserves the original ranking among records still eligible.
This does not provide a transaction-wide snapshot or promise that records
cannot change again after the final read.

## Verification and preservation

Six service/SQL-driver regression cases failed before their respective fixes:
three memory-read scopes and three source-read scopes. They exercise actual
service calls, a local HTTP embedding fixture and GORM's second-stage SQL,
including bound parameters. They are not a live model, mailbox or PostgreSQL
concurrency acceptance test. No external account or provider was used.
The full semantic, memory, source and source-evidence Go test packages pass
locally; database-dependent cases without a configured DSN remain skipped.

Original worktree content fingerprints remain recorded privately. No original
source, transcript, database or worktree was deleted; source review does not
establish byte preservation on main or whole-archive completion.
