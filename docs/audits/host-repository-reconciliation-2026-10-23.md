# Host repository reconciliation

## Reviewed retained work

Compared eight source versions from the model-maintenance and artifact
worktrees with main `4684546907900d4958f85e7b71ba8848376b0b93`:

| Files | Versions | Decision |
| --- | ---: | --- |
| `backend/internal/hostruntime/repository.go` | 2 | Retain locked durable start intents, approval-digest validation, exact acknowledgement binding, uncertain-outcome quarantine and owner-bound cancellation. Repair missing expiry handling below. |
| `backend/internal/hostruntime/repository_memory_test.go` | 2 | Retain the newer independent test repository, including explicit missing-expiry guards; do not treat its behavior as proof of PostgreSQL parity. |
| `backend/internal/hostruntime/migration_contract_test.go` | 2 | Retain non-destructive rollback checks and start-intent immutability/quarantine checks. Static migration text assertions are not database execution proof. |
| `backend/internal/hostruntime/repository_postgres_integration_test.go` | 1, artifact worktree | Retain the newer isolated temporary-table coverage with stop revisions, start protocol columns, confirmed starts and per-scenario runtime identities. Extend and execute it in CI. |
| `backend/internal/hostruntime/safety_test_main_test.go` | 1, artifact worktree | Retain revision-aware test safety provider setup; production stop state is not altered. |

This finishes semantic review of the selected hostruntime versions in these
worktree inventories. It does not establish preservation of every original
byte, whole-worktree integration, or archive cleanup eligibility.

## Production/test parity defect

The PostgreSQL start protocol dereferenced `LeaseExpires` in three places without
checking for nil: initial begin, begin replay, and actual-start acknowledgement.
The SQL column is nullable; the migration constraints do not prohibit a leased
row with a missing expiry. The test-only memory repository already rejected
this state, so its passing tests did not establish production parity.

All three PostgreSQL paths now reject a missing expiry through their existing
review/quarantine outcomes rather than dereference a nil timestamp. No start
acknowledgement is issued and no stale lease is made reusable.

## Stronger acceptance gate

The existing PostgreSQL temporary-table test now covers initial begin, replay
and acknowledgement after clearing a leased row's expiry. Each case requires a
safe error, review state, invalidated lease and no actual-start acknowledgement.

The existing PostgreSQL 18.6 CI job now runs that integration-tagged test using
its existing disposable database and explicitly enables the test's temporary
table guard. CI requires PASS markers for the parent test and all three cases;
a skip or missing test is not sufficient. No extra job or local container is
introduced. This temporary-table suite does not itself prove the full migration
trigger semantics or Windows execution isolation.

## Local verification and remaining gate

- Full `hostruntime` unit tests passed with Windows Go 1.27.3.
- The integration-tagged hostruntime package compiled successfully; no SQL tests
  ran locally because no dedicated test database was configured.
- All 111 CI-contract tests passed, including the new SQL-gate assertion.
- Workflow YAML parsed and the complete modified PostgreSQL job script passed
  Bash syntax validation. Whitespace validation passed.
- Real PostgreSQL execution of the new cases remains pending exact-head CI.
  No claim is made that the SQL regressions were executed locally.
- Original source files and private fingerprints remain intact. No local source,
  transcript, settings, backup or database data was removed.
