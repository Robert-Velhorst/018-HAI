# Manual Source Worker Handoff - 2026-09-30

Status: scoped implementation stable for the parent's isolated build. Not a
claim of completed browser, PostgreSQL integration, or production acceptance.

## Startup Contract

```dotenv
SOURCE_SCHEDULER_ENABLED=false
SOURCE_MANUAL_WORKER_ENABLED=true
SOURCE_WORKER_POLL_SECONDS=15
HAI_PHASE2_MODE=autonomous_safe
```

The manual flag defaults to false. Polling defaults to 300 seconds and accepts
15-3600 seconds. The existing router call to source.StartScheduler initializes
the optional worker; no router edits are needed. SOURCE_SCHEDULER_DURABLE=false
does not disable this explicitly opted-in durable manual worker. When the full
durable scheduler is enabled, its existing manual handler remains in use; no
second manual runner is started.

Use the above only in the separately owned synthetic manual-local harness:
all other automatic schedulers disabled, internal network, read-only synthetic
local file, no accounts/providers/feeds/SMTP. The router's background gate must
allow processing, persisted emergency-stop state must be readable and clear,
and PostgreSQL/source/durable-job schema and source-sync leases must be usable.
The authenticated owner must own an enabled, non-paused, non-revoked source.
The flag does not configure or enable connectors, credentials, or accounts.

## Exact Owned Changes

- backend/internal/source/scheduler.go: independent opt-in startup branch; existing scheduled gate retained.
- backend/internal/source/manual_worker.go: manual-only durable runner, one job per poll, kind-filtered atomic claim/recovery, bounded queue-query timeouts, successful-attempt telemetry, lifecycle readiness.
- backend/internal/source/manual_worker_test.go: ten focused worker tests, reusing existing test helpers; no new persistent fixtures or destructive database tests.
- backend/internal/source/manual_sync.go: submission accepts manual-only readiness as well as existing full-worker readiness.
- backend/internal/source/service.go: one independent readiness field; adjacent gofmt alignment only. Existing dirty changes were preserved.
- .env.example: default-false manual option and safety prerequisites only; existing dirty changes preserved.
- This result file and the generated logs below.

No frontend, router, Compose, CI, acceptance scripts, personal environment,
provider settings, installed services, commits, pushes, or descendants changed
by this worker assignment. No calls to the old or new acceptance stack.

## Proof

- Initial worker regression build failed on the then-missing new readiness/startup methods; retained as red evidence, not a runtime reproduction.
- Broader source regression: 51 top-level tests passed, zero skips.
- Final focused source regression: 34 distinct top-level tests passed ten times under -race (340 passes), zero skips.
- Existing durable runner regression: 31 top-level tests passed under -race.
- Tests prove independent/default-off startup; HTTP 503 with no history/queue/extraction writes when worker disabled; paused/stop/missing-control deferral without consumed attempts; same durable local job completes with a real local extraction after policy permits; owner/source rejection; idempotent retry; separate webhook readiness; shutdown cleanup; five-attempt exhaustion; SQL kind/queue restrictions and recovery bounds.
- PostgreSQL SQL checks used GORM DryRun, without opening a database connection. The local completion test uses the existing temporary local-folder helper and in-memory repositories, not a production PostgreSQL or browser check.
- git diff --check passed on tracked owned files. gofmt applied to owned Go changes.
- Test containers used cached golang:1.25.13 with read-only root/source, network none, no capabilities, tmpfs scratch, existing read-only module cache and writable build cache. No database container or schema was created/modified. Formatter containers alone used a writable source mount.

Retained logs in output/:

- manual-source-worker-red-20260930.log
- manual-source-worker-focused-20260930.log
- manual-source-worker-regression-20260930.log
- manual-source-worker-race-20260930.log
- manual-source-worker-runner-20260930.log

Created containers have unique prefix hai-manual-source- and suffix
20260930-9a31: red, focused, regression, race, runner-proof, format,
format-final. All exited; none remains running. Logs/containers retained.

## Hard Limits And Parent Acceptance

Only queue source / kind source.manual_sync is claimed or recovered. No
source.scan, source.sync, webhook, or extraction-correction handler is
registered by this worker. Existing pending manual jobs can resume; the option
is not a local-only connector allowlist. Do not enable it on a populated
personal installation merely for testing.

With both source scheduler and manual flag false, the paused harness must
assert 503 and zero new history/queue writes. With the manual flag true but
global policy paused, durable manual intent can be accepted as queued and
deferred indefinitely without falsely becoming completed. Gate deferral is
not success and does not consume provider attempts. Pause/stop are checked
before each attempt; this patch adds no instantaneous in-flight rollback.
Existing source sync deadline defaults to 600 seconds, bounded 30-1800;
connector/file limits and source-sync leases remain unchanged. Lease recovery
is five minutes, with unknown outcomes consuming attempts. No at-most-once
external-effect guarantee is added.

Parent owns PostgreSQL execution/concurrency/restart proof, isolated build,
and real-browser E2E. Required positive evidence: HTTP 202 with mode
manual_async_sync and exact sourceId; poll the returned exact job ID until its
persisted status is completed; confirm the actual acceptance.txt extraction
belongs to the same owner/source. A toast, queue acknowledgment, unit test,
or worker startup log is not that acceptance proof. Live/provider acceptance
and personal-installation changes remain unauthorized and unperformed.
