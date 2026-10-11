# Manual-Only Source Worker: Real PostgreSQL Proof

Verified on 2026-09-30 at 21:49:23-21:49:27 UTC (23:49 Amsterdam).

**Result:** six named PostgreSQL tests passed three times each: 18 real database test invocations, zero skips, zero failures, no race detector findings. No production implementation, existing test, guard, CI, launcher, frontend, or parent-stack files were edited. No commits were made.

## Owned Deliverables

- [New guarded tests](../backend/internal/source/manual_worker_postgres_test.go)
- [Isolated reproducible runner](manual-worker-postgres-20260930-7c83e-run.ps1)
- [Successful attempt and intentional guard failures](manual-worker-pg-20260930-e6905598c8c5/)
- [Earlier failed formatting attempt](manual-worker-pg-20260930-1eb051df1ab7/)

## Named PostgreSQL Tests

Each following test has exactly three pass events, zero skip events, and zero fail events in [named-postgres-proof.json](manual-worker-pg-20260930-e6905598c8c5/named-postgres-proof.json). The underlying evidence is [go-tests-positive.log](manual-worker-pg-20260930-e6905598c8c5/go-tests-positive.log).

| Test | Real PostgreSQL assertions |
| --- | --- |
| `TestManualWorkerPostgresClaimIsolation` | Only a due `source.manual_sync` job is claimed. Older `source.sync`, `source.scan`, an unregistered kind on the source queue, non-source work, and a manual kind on another queue remain unchanged. Future and terminal manual jobs remain unchanged. Wrong-queue requests fail. A fresh pool reloads the persisted owner, running status, lock timestamp, and generation. |
| `TestManualWorkerPostgresConcurrentSingleOwner` | Twelve simultaneously released claimers contend for one manual job. Exactly one receives it; a new pool confirms that owner's identity and generation 1. All foreign jobs remain unchanged. Repeated three times, for 36 claim calls. |
| `TestManualWorkerPostgresSkipLocked` | A separate open transaction locks the earliest manual row. Claiming skips it and obtains the next due row. After rollback, the previously locked row becomes claimable. Uses the production claim SQL, not a mock or dry run. |
| `TestManualWorkerPostgresReapIsolationAndAttemptLimit` | Expired and missing manual leases consume exactly one attempt. Retryable rows become pending; final-attempt rows become dead with completion timestamps. Fresh leases, the exact expiry cutoff, non-running manual rows, and foreign kinds/queues remain unchanged. A second reap consumes nothing. Stale pre-reap success writes are rejected. |
| `TestManualWorkerPostgresDurableRetryLimit` | The production manual runner and real repository persist synthetic retryable handler failures. New runners/pools reload each attempt, future backoff prevents immediate retry, and attempt 3 is dead-lettered. Exhausted jobs do not run again. Foreign pending and expired-running jobs remain unchanged; no additional durable jobs are created. |
| `TestManualWorkerPostgresExecutionReload` | Real owner-bound submission persists linked history/outbox rows. A paused safety gate defers without an attempt or extraction. Resumption runs the real local-folder service on one synthetic temporary Markdown file. The execution pool is closed; a new pool/service reloads succeeded status, successful attempt 1, completed history, exact extracted text, raw-item identity/provenance, captured project, source cursor, and sync timestamp. A newer source default is preserved. Foreign-owner lookup fails; foreign jobs remain unchanged; no extra durable jobs appear. |

The focused run also preserved and exercised all ten existing `TestManualWorker*` top-level tests, the new connection-free target guard, and the three existing pgtestguard top-level tests. In total: 20 top-level tests x three repetitions; 147 successful test/subtest events plus two package pass events. There are no skip or failure events in the positive JSON stream.

## Guard and Negative Proof

Every new database fixture and every pool reopen requires:

- `HAI_SOURCE_MANUAL_WORKER_POSTGRES_TEST_DSN`, with no application/shared-test DSN fallback.
- Exact database identity `hai_source_manual_worker_test`.
- Explicit literal `HAI_ALLOW_DESTRUCTIVE_DATABASE_TESTS=true`.
- A literal loopback IP with no unsafe fallback destination, checked using existing `pgtestguard` before pool construction, connection, extension creation, or model migration.

The shared guard takes the exact expected database as an argument; it contains no shared database-name allowlist to extend. It was not modified. After connecting, the fixture additionally checks `current_database()` before any schema work. Each case creates a uniquely named owned schema; cleanup drops only that schema and closes its pools.

Retained expected-red attempts selected only `TestManualWorkerPostgresClaimIsolation`, used `-race -count=1 -p=2`, and exited 1 with explicit refusal:

- [Wrong/lookalike database](manual-worker-pg-20260930-e6905598c8c5/guard-red-wrong-database.log).
- [Hostname instead of literal loopback](manual-worker-pg-20260930-e6905598c8c5/guard-red-hostname.log).
- [Nonliteral opt-in](manual-worker-pg-20260930-e6905598c8c5/guard-red-nonliteral-opt-in.log).

[The schema query after those attempts](manual-worker-pg-20260930-e6905598c8c5/guard-schemas-after-red.log) returned zero owned schemas. The connection-free guard table additionally tests IPv4/IPv6 literals, absent/false opt-in, empty DSN, application/lookalike names, remote IP, remote fallback, and Unix socket rejection. IPv6 is validated, not exercised as a live database connection.

## Isolation and Execution

Observed toolchain: Go 1.25.13, linux/amd64. Observed server: PostgreSQL 17.11, database `hai_source_manual_worker_test`. Cached image IDs are pinned in the runner and retained image/container inspections; no images were pulled.

```text
GOMAXPROCS=2 GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off
go test -json -race -count=3 -p=2 -timeout=12m \
  -run '^(TestManualWorker|TestValidateDedicatedPostgresTestDSN|TestDestructiveTestOptIn)' \
  ./internal/source ./internal/pgtestguard
```

Successful owner label: `com.hai.manual-worker-postgres.owner=manual-worker-pg-20260930-e6905598c8c5`.

- PostgreSQL: network `none`, listens only on `127.0.0.1`; data and socket directories use tmpfs; no ports, bind mounts, or Docker volume mounts.
- Go: shares only the owned PostgreSQL network namespace; read-only root and backend source; existing module/build cache directories are read-only bind mounts, not volume mounts. Build writes use executable `/tmp` tmpfs. The existing 2,160,835,845-byte build cache exceeded the bounded copy threshold, so compilation used a fresh temporary build cache and the existing offline module cache.
- No Docker volumes or networks were created, removed, or mutated. No `rm -v`, volume deletion, prune, provider access, personal-service access, parent-stack access, or descendant jobs were used.
- The runner inspected exact names, owner labels, pinned images, namespaces, mounts, and port absence before start and immediately before cleanup. Only its two owned containers were stopped/removed, without volume flags. [Final owned-container listing](manual-worker-pg-20260930-e6905598c8c5/owned-containers-after.log) is empty; an independent owner-label query also found none.
- [Schema query after the positive run](manual-worker-pg-20260930-e6905598c8c5/schemas-after-positive.log) returned zero owned schemas. The database itself was ephemeral and disappeared with its owned tmpfs container.
- [Before](manual-worker-pg-20260930-e6905598c8c5/source-hashes-before.json) and [after](manual-worker-pg-20260930-e6905598c8c5/source-hashes-after.json) SHA-256 manifests cover 92 source/durablejob/pgtestguard Go files and `go.mod`/`go.sum`; independent comparison found zero changes during execution. Formatting affected only the newly owned test before that baseline.

## Retained Failure and Limits

The first attempt stopped before tests because `docker cp` could not retrieve the formatted file from the container's tmpfs. [Failure evidence](manual-worker-pg-20260930-1eb051df1ab7/gofmt-copy.log), inspections, and successful owned-container cleanup are retained. The runner now reads formatter output directly and formats only the new test file. No test-result claim is based on that attempt.

This is targeted PostgreSQL repository/runner/service proof, not full migration-chain, production deployment, process restart, or power-loss acceptance. The seven production models are AutoMigrated in isolated test schemas; migrations 0087/0105 and the full production constraint/index set were not applied. Retry failures are an explicitly synthetic executor; the successful local-file case uses the actual service and repository with memory, semantic, workflow, and external-provider services unwired. Reload means new connection pools/services after committed execution, not recovery across PostgreSQL/server destruction. Long-running heartbeat/cancellation, live provider sync, worker startup through the default application database, browser authentication, and the parent's separate full-stack extraction acceptance are outside this proof.
