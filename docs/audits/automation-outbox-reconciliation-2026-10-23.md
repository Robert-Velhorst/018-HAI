# Automation mutation and outbox reconciliation

## Retained source

The complete original/current deltas of these four files were reviewed:

- `backend/internal/automation/automation_service.go`
- `backend/internal/automation/automation_service_test.go`
- `backend/internal/automation/automation_repository.go`
- `backend/internal/automation/automation_handle.go`

The service retains 112 original declarations unchanged, extends twelve, adds
two small publisher/repository interfaces and removes no original declaration.
Constructor names and old non-durable repository compatibility remain. Create,
update and delete prefer the repository's transaction-aware event operations;
they do not publish a second direct event when that path is used. Updates retain
the previous URL path in the event. The proof-issuance minimum TTL now respects
the existing microsecond timestamp precision instead of accepting sub-precision
lifetimes.

The Gorm repository commits each mutation and its event through the same
transaction. Enqueue failure returns the transaction error; the notification is
issued only after commit. Update checks that its target exists rather than
silently recreating it. Launch-state projection writes also gain a bounded
context and reject unavailable/dry-run storage. The older direct-publisher
compatibility path is not thereby made atomic.

The HTTP handler retains its multipart image-plus-metadata bound and 1 MiB JSON
update limit. The maximum-create helper is moved and its constants named and
typed; the oversized-update response becomes explicit about the 1 MiB limit.
Original request handling is not replaced by the archived preview servers.

The service test delta adds a durable CRUD/no-double-publish fixture, replaces a
publisher fixture and strengthens in-flight cancellation identity assertions.
Existing tests are retained. The relevant current transaction/outbox tests,
enqueue implementation, and router wiring were also inspected. The router
connects repository notifications to the dispatcher and starts that dispatcher
only when brokers are configured. Queued intent is not proof of broker delivery.

All four current source files already match the inspected `main`; restoring
their old variants would discard these changes. No unique original function was
identified for restoration in this comparison. This is not an audit of every
automation dependency, filesystem side effect, concurrent update, or broker.

## Database verification gap and integration

The outbox's existing PostgreSQL tests skip when their database environment is
missing. The previous dedicated migration job did not select them. The workflow
now provisions `hai_automation_events_test` within its disposable PostgreSQL
service and runs these existing cases against it:

1. `TestPostgresAutomationMutationsCommitOutboxAtomically`
2. `TestPostgresOutboxCommitDispatchAndRollback`
3. `TestPostgresOutboxRetriesPublisherFailure`
4. `TestPostgresOutboxExpiresAbandonedFinalLease`
5. `TestPostgresOutboxStatsRetryAndPrune`

Packages and tests are serialized with `-p 1 -parallel 1`. A separate database
keeps unscoped queue claims and pruning assertions away from other test groups.
The step requires a top-level `PASS` line for every selected case, so absence or
skipping is not accepted as verification. These tests use a recording publisher:
they exercise real database persistence/rollback/retry behavior, not a real Kafka
broker, consumer-side exactly-once delivery, or a deployed end-user flow.

## Evidence boundary

Locally verified: YAML parsing, Bash syntax of every migration-job run step,
isolated database provisioning linkage, exact five-test selection and mandatory
pass guards. Source hashes and declaration comparisons are recorded separately.
The newly wired PostgreSQL cases have not yet run through this changed workflow.
No database, container, provider or account on the local device was changed.

No source checkout or transcript archive is released for deletion. A passing
future workflow still needs to be followed by publication and preservation
verification on `main`; the whole HAI reconciliation remains incomplete.
