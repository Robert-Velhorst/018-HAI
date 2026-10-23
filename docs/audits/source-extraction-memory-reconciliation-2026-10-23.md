# Source-extraction memory reconciliation

## Reviewed source

Read the complete retained source-extraction lesson implementation, unit tests
and PostgreSQL tests, and their complete differences against the current code.
Keep the newer exact content/revision-bound correction audit, active owner-bound
source and raw-evidence checks, non-content sensitive lesson, explicit evidence
URI/label requirements and conflict refusal instead of overwriting a lesson.

Preserve source/extraction/raw lock ordering followed by the separately
namespaced memory advisory lock. Do not restore the older shared source-worker
lock or acquire the memory lock before source evidence locks. Preserve atomic
memory/vector cleanup and best-effort indexing only after persistence.

Retain the stronger database fixtures that seed actual provenance, verify
content-bound audits, reject unrelated/missing/stale correction evidence and
exercise concurrent upsert and transaction rollback. The older permissive
fixtures are not evidence for weakening the current authority boundary.

## Canonical identity fix

Kind validation accepted case-insensitive `lesson` and `correction_lesson`, but
the stored kind retained caller casing. The exact owner/extraction/kind lookup
could consequently treat accepted spellings as different lesson identities.

New rows now use the same lowercase, trimmed kind as validation. Lookup also
recognizes a historical uppercase spelling for the exact owner and extraction;
a matching existing row is reused without rewriting it. Multiple historical
case variants fail as an ambiguous conflict rather than choosing or overwriting
one. No migration, historical data rewrite or automatic duplicate deletion is
performed.

The PostgreSQL fixture now uses the existing dedicated-database guard before
opening a connection: explicit destructive-test opt-in, a literal loopback
destination and the exact disposable database identity.

## Verification and remaining gate

- Four accepted noncanonical kind spellings failed the new unit regression
  before the fix. Canonical spellings passed as positive controls.
- Complete memory and PostgreSQL-guard Go packages passed uncached afterward.
- All 113 CI workflow contracts passed.
- Integration-tagged tests compile locally; the database case is skipped
  because no dedicated DSN is configured. No local PostgreSQL result is claimed.
- The existing mandatory PostgreSQL concurrency case now mixes accepted kind
  spellings, verifies reuse of a legacy uppercase row, and rejects ambiguous
  legacy variants without changing the row count. Its named PASS marker remains
  required by the existing CI job; these SQL results await CI.

No actual user memory, database, source worktree or transcript was removed.
Local tests and publication do not establish live-account acceptance or permit
deleting unintegrated source copies.
