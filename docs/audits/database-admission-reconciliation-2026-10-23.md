# Database admission and migration test reconciliation

## Reviewed source scope

Reviewed both retained database initialization implementations and both retained
migration unit-test variants: four source versions. This is not a full review of
the migration executor, integration fixtures or every infrastructure file.

Retain current context-bounded acquisition and initialization, shared startup
coordination, independent waiter cancellation, neutral cached runtime handles,
failed-candidate cleanup and terminal shutdown. Do not restore the old opener
that formats an unquoted DSN or holds the cache mutex throughout opening and
migration. Current connection construction and pool settings remain unchanged.

Keep versioned SQL authoritative, development-only missing-table creation and
the exclusions for immutable migration-owned proof ledgers. Preserve newer
OAuth state model coverage. Keep current migration tests for checksums over
version and both scripts, ordered applied prefixes, cross-phase rollback order
and the narrowly scoped known historical cursor checksum. The older tests
introduce no missing behavior requiring a downgrade of these contracts.

## Cancellation admission repair

GetDefaultDBContext checked cancellation before acquiring the cache mutex, but
could return an existing pool or call the opener after cancellation occurred
between that check and admission. Recheck under the cache mutex before reading
the cached pool, creating startup state or invoking the opener.

Two deterministic regression cases capture an initially clear context and
cancel before admission: one with a cached pool and one with cold startup.
Before the fix, the cached path returned success and the cold path called the
opener. After the fix both reject with context.Canceled, no opener call and no
new startup state. Existing live-context and concurrent waiter/shutdown tests
remain part of the full infrastructure suite.
The full infrastructure package passed locally with uncached tests on Windows
and Go 1.27.3 after the repair.

This is an acquisition boundary check, not a guarantee that cancellation cannot
occur after a pool has been returned. Query contexts must still be used.

## Configuration and preservation boundaries

Migration-setting precedence and compatibility are deliberately unchanged.
The older DB_MIGRATIONS_ENABLED setting rejects invalid booleans, while the
legacy DB_RUN_MIGRATIONS helper retains its explicitly tested enable-by-default
behavior for unknown values. Do not describe these two parsing contracts as
identical or claim this change redesigns migration configuration.

Tests use synthetic drivers and configured opt-in integration boundaries.
Passing local package tests is not proof that production PostgreSQL migrations
were executed. Original worktrees, transcripts and archives remain intact;
private fingerprint receipts do not themselves authorize deletion.
