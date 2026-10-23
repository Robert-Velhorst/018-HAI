# Migration executor source reconciliation

## Reviewed scope

Reviewed both retained migration executor implementations and both corresponding
integration-test variants: four source versions. Keep current checksum-aware
validation rather than replacing the executor with either older version.

Retain transactional advisory locks, under-lock rechecks, stable empty-ledger
verification and per-migration atomicity. Keep ordered applied prefixes,
cross-phase source/checksum preflight, no implicit adoption of missing checksums,
exact approved legacy adoption and the narrowly scoped known cursor checksum.
Keep read-only status on a fresh database and refusal to roll back an unapplied
migration. Down migrations must follow newest-first and cross-phase dependency
ordering; proof-ledger and cursor rollback guards remain authoritative.

Retain disposable integration databases, explicit destructive-test opt-in and
the newer validation of every PostgreSQL fallback host, not just the primary.
Keep newer tests for explicit checksum adoption, source drift, cross-phase
rollback refusal and a rollback waiting on an active feed checkpoint before
checking whether data would be lost. The older tests do not justify removing
these newer cases or restoring shared-database cleanup.

## Rollback-source read failure repair

The loader treated every error reading a down file as though the optional file
did not exist. Permission or I/O failures could therefore silently produce an
empty DownSQL value and a different migration checksum instead of surfacing the
source-read failure.

Ignore only fs.ErrNotExist for the optional down file. Other failures now abort
loading and preserve the cause through error wrapping. Missing optional files
remain compatible, and readable migration bytes/checksum encoding are unchanged.
No schema or migration file is changed.

## Verification and preservation

Permission and unexpected-EOF regression cases failed before the repair; the
missing-optional-file positive control passed. The full infrastructure package
passed locally with uncached tests on Windows and Go 1.27.3. It also compiled
and passed with integration tags and no database DSN: pure guard tests ran,
but real PostgreSQL cases were skipped. This is not database acceptance.

The exact four original source variants are recorded in the private ledger.
Original worktrees, transcripts, archives and databases remain intact. Neither
semantic reconciliation nor PR publication is a local deletion gate.
