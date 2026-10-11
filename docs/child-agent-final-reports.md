# Completed child-agent final reports

This generated archive preserves the canonical terminal report from every
completed HAI child transcript in the audited August 2026 cohort. Potential
credential-shaped values are redacted; the manifest retains the SHA-256 of
the original terminal message. Aborted and nonterminal transcripts are not
represented as completed work and must be retained.

> **Historical record:** Entries describe their recorded checkpoint, not the
> current runtime or external-provider acceptance. Local source paths have
> been normalized to repository-relative links. A trailing `:line` is the
> original report's line coordinate and may not match the current file.

## 019fba31-a317-7bb0-8aa8-f830a4a9bcf0

- Date: 2026-08-01
- Agent: Dalton
- Work kind: implementation
- Original report SHA-256: `1ef6d31cc603c0a183fb5a2acd592bc4559624873063c1c6a126e61bc49df922`
- Transcript: `08/01/rollout-2026-08-01T00-00-43-019fba31-a317-7bb0-8aa8-f830a4a9bcf0.jsonl`

Implemented entirely within [backend/internal/proactivity](../backend/internal/proactivity/).

Key additions:

- Owner-scoped `Repository` and defensive, bounded, concurrency-safe `MemoryRepository`.
- Atomic idempotency for policies, signal batches, and decision evaluations.
- Clone-on-read/write storage, owner isolation, secret redaction, and bounded histories.
- Advisory service that persists state and forces execution, delivery, and authority flags to `false`.
- Strict Gin JSON handling and guarded route registration requiring all five guards.
- Repository, service, handler, and security tests.

**Integration**
```go
handler, err := proactivity.NewAdvisoryAPI(
    proactivity.NewMemoryRepository(),
)

err = proactivity.RegisterRoutes(v1, handler, proactivity.RouteGuards{
    AuthenticatedOwner: authenticatedOwnerGuard,
    RecognizedRole:     recognizedRoleGuard,
    Read:               readGuard,
    Write:              writeGuard,
    Govern:             governGuard,
})
```

The constructor is defined in [handler.go](../backend/internal/proactivity/handler.go):53.

Routes mounted beneath the supplied group:

```text
GET  /proactivity/policy                 Read
PUT  /proactivity/policy                 Govern
GET  /proactivity/signals                Read
POST /proactivity/signals                Write
GET  /proactivity/decisions              Read
POST /proactivity/decisions/evaluate     Write
```

No delivery, send, execution, approval, authorization, or authority-granting routes exist.

Verification passed using Go 1.25.12:

```text
go vet ./internal/proactivity
go test -count=1 -race ./internal/proactivity
```

The unrelated dirty worktree was left untouched; no router, migration, or other package was edited.

## 019fba31-a411-7510-9686-8ce1b06e9d6c

- Date: 2026-08-01
- Agent: Aristotle
- Work kind: implementation
- Original report SHA-256: `b27411475496c2df2ecf07b058d317810ec716289e00df354c7629686efe547e`
- Transcript: `08/01/rollout-2026-08-01T00-00-43-019fba31-a411-7510-9686-8ce1b06e9d6c.jsonl`

Implemented entirely within [`backend/internal/outcomeevaluation`](../backend/internal/outcomeevaluation/).

**Integration Contract**
Mount using `RegisterRoutes(apiGroup, NewHandler(NewService(repository)), guards)`. All five guards are mandatory; registration fails closed if any are absent.

Routes under a parent such as `/api/v1`:

- `PUT /outcome-evaluations/workspaces/:workspaceId/outcomes/:outcomeId` - Govern
- `GET /outcome-evaluations/workspaces/:workspaceId/outcomes/:outcomeId` - Read
- `GET /outcome-evaluations/workspaces/:workspaceId/outcomes/:outcomeId/history` - Read
- `POST /outcome-evaluations/workspaces/:workspaceId/outcomes/:outcomeId/evaluations` - Write
- `GET /outcome-evaluations/workspaces/:workspaceId/outcomes/:outcomeId/evaluations` - Read
- `GET /outcome-evaluations/workspaces/:workspaceId/outcomes/:outcomeId/evaluations/:evaluationId` - Read
- `POST /outcome-evaluations/workspaces/:workspaceId/outcomes/:outcomeId/corrections` - Write
- `GET /outcome-evaluations/workspaces/:workspaceId/outcomes/:outcomeId/corrections` - Read

The API provides optimistic outcome revisions, idempotent evaluations/corrections, configurable bounded histories, defensive repository copies, strict JSON/content types, provenance validation, cross-owner invisibility, opaque internal errors, and digest verification. There are no execute, apply, approval, or policy-changing endpoints.

Core contracts are in [`repository.go`](../backend/internal/outcomeevaluation/repository.go):10, [`service.go`](../backend/internal/outcomeevaluation/service.go):10, and [`handler.go`](../backend/internal/outcomeevaluation/handler.go):30.

**Verification**
- `go test -count=1 -race ./internal/outcomeevaluation` passed.
- `go vet ./internal/outcomeevaluation` passed.
- Broader `go test ./internal/...` passed this package and other packages except pre-existing `internal/resilience` compilation errors involving mismatched handler/service methods.
- Router, migrations, and other packages were not edited.

## 019fba36-2a10-7a83-9a62-77db81483cc8

- Date: 2026-08-01
- Agent: Nash
- Work kind: implementation
- Original report SHA-256: `19812114426979db60621948350e9cdd008f4427a0afeb2f3b8627aee3ca4ac5`
- Transcript: `08/01/rollout-2026-08-01T00-05-39-019fba36-2a10-7a83-9a62-77db81483cc8.jsonl`

Implemented entirely within [backend/internal/resilience](../backend/internal/resilience/).

**Integration**
```go
repo := resilience.NewMemoryRepository(100)
handler, err := resilience.NewAdvisoryAPI(repo)

err = resilience.RegisterRoutes(apiV1, handler, resilience.RouteGuards{
    AuthenticatedOwner: authenticatedOwnerGuard,
    RecognizedRole:     recognizedRoleGuard,
    Read:               readGuard,
    Write:              writeGuard,
    Govern:             governGuard,
})
```

Registration fails when any dependency or guard is missing. Production repositories must implement the atomic CAS interface in [repository.go](../backend/internal/resilience/repository.go):59. The memory implementation is process-local reference storage, with histories bounded to `1..1000` entries.

Assuming `apiV1` is `/api/v1`, routes are:

- Read: `GET /resilience/workspaces/:workspaceId/status`
- Read: `GET /leases`, `/leases/:workId`
- Write: `POST /work-registrations`
- Write: `POST /leases/:workId/acquire`, `/heartbeat`, `/release`
- Read: `GET /workers`, `/workers/:workerId`
- Write: `PUT /workers/:workerId/heartbeat`
- Read: `GET /retries`, `/retries/:workId`
- Govern: `POST /retries/:workId/advise` and `/decide` aliases
- Read: `GET /circuits`, `/circuits/:circuitId`
- Govern: `POST /circuits/:circuitId/before-attempt`, `/observations`
- Read: `GET /recoveries`, `/recoveries/:workId`
- Govern: `POST /recoveries/:workId/advise`
- Read: `GET /events`

Owner comes exclusively from `identity.ContextSubjectKey`; workspace comes exclusively from `:workspaceId`. List routes accept `limit` with default `100`, maximum `1000`; retry/recovery lists also accept `workId`.

JSON is limited to 64 KiB, requires `application/json`, rejects unknown fields and trailing values. Durations accept either strings such as `"5m"` or integer alternatives such as `ttlSeconds`, but not both.

Every success and package-generated error includes:

```json
{
  "authority": {
    "mode": "advisory_only",
    "canExecute": false,
    "grantsAuthority": false,
    "consumesApproval": false,
    "dispatchesWork": false
  }
}
```

The service has no dispatcher, executor, worker client, or approval-consumption dependency. It adds atomic lease fencing, heartbeat sequencing, circuit revisions, canonical concurrent idempotency, hash-chained events, defensive copying, and bounded retry/recovery histories.

Verification passed:

- `go test ./internal/resilience -count=1`
- `go vet ./internal/resilience`
- Concurrent idempotency/CAS stress test, 20 runs
- `go test ./internal/router -count=1`

The full parallel internal run passed resilience and other packages but intermittently hit a Windows Go build-cache import error in router; isolated router reruns passed. Race instrumentation was unavailable because CGO is disabled.

## 019fba3b-9c1e-7433-b0cb-882ff6462be7

- Date: 2026-08-01
- Agent: Huygens
- Work kind: implementation
- Original report SHA-256: `96fd29e63eefdfbc6b569d74310a40751778bfdc34fe0a2e89c94cb386a8c1c1`
- Transcript: `08/01/rollout-2026-08-01T00-11-36-019fba3b-9c1e-7433-b0cb-882ff6462be7.jsonl`

Implemented the callable advisory resilience API entirely within `backend/internal/resilience`.

**Changed paths**
- `repository.go`: scoped repository, bounded in-memory implementation, CAS/fencing validation, retry/recovery histories, inventory queries.
- `service.go`: advisory orchestration for registration, leases, heartbeats, retries, circuits, recovery and hash-chained events.
- `handler.go`: strict guarded Gin API with status/list/get and transition endpoints.
- `repository_api_test.go`
- `service_api_test.go`
- `handler_api_test.go`
- `handler_test.go`

Key safeguards include authenticated owner derivation, workspace isolation, strict JSON, 64 KiB body limits, secret redaction, idempotency enforcement, fencing tokens, circuit transition validation, immutable recovery/retry verification and advisory-only authority responses. The API cannot execute or dispatch work, grant authority, or consume approvals.

**Verification**
```powershell
go test ./internal/resilience -count=1
go test -vet=off ./internal/resilience -count=20
go vet ./internal/resilience
go test -vet=off -cover ./internal/resilience -count=1
git diff --check -- backend/internal/resilience
```

All passed. Coverage is `68.3%`. Race detection was attempted but unavailable because this Windows Go toolchain has CGO disabled and no C compiler installed. No router, migration, or external package was changed for this implementation.

## 019fba3f-4a6d-7743-a7b3-a1364d00dce3

- Date: 2026-08-01
- Agent: Russell
- Work kind: implementation
- Original report SHA-256: `8aaa6b8921ed645b7d31f1f8fd39a3789156d51fae0baec09c67398aa9e92b49`
- Transcript: `08/01/rollout-2026-08-01T00-15-37-019fba3f-4a6d-7743-a7b3-a1364d00dce3.jsonl`

Implemented durable PostgreSQL persistence for `lifeontology`.

Key additions:

- [repository_postgres.go](../backend/internal/lifeontology/repository_postgres.go):21
  - `NewPostgresRepository(db)`
  - `DefaultRepository()`
  - Owner-scoped immutable entities, relations, and merge proposals
  - Deterministic duplicate handling
  - Strict JSON decoding and full envelope revalidation
  - Fails closed for nil databases, corrupt payloads, metadata mismatches, and owner mismatches

- [0019_life_ontology.up.sql](../backend/migrations/pre/0019_life_ontology.up.sql)
  - Composite owner-scoped keys and foreign keys
  - Payload and digest constraints
  - Immutable update/delete triggers
  - Truncate prevention

- [0019_life_ontology.down.sql](../backend/migrations/pre/0019_life_ontology.down.sql)
  - Refuses rollback when records exist
  - No `CASCADE`

- Migration tail updated at [migration_chain_contract_test.go](../backend/migrations/migration_chain_contract_test.go):97.

Verification passed:

- Life ontology and migration suites repeated 20 times
- Focused `go vet`
- Full backend `go test -vet=off ./...`
- Real PostgreSQL 17 integration tests
- Concurrent idempotency and restart durability
- Cross-owner relation rejection
- Update, delete, and truncate protection
- Empty rollback and non-empty rollback refusal
- `git diff --check`

The disposable PostgreSQL test container was removed afterward.

## 019fba43-f4f4-7001-9ee4-929db0edde72

- Date: 2026-08-01
- Agent: Pasteur
- Work kind: implementation
- Original report SHA-256: `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`
- Transcript: `08/01/rollout-2026-08-01T00-20-43-019fba43-f4f4-7001-9ee4-929db0edde72.jsonl`

_The child completed without a terminal text report._

## 019fba46-ac34-7730-829b-ed9f07026d7a

- Date: 2026-08-01
- Agent: Cicero
- Work kind: implementation
- Original report SHA-256: `bb969e5b9d0b35de766a7b26fc2901b9fd9ea1fe8c97edad64205b499e9d7a9a`
- Transcript: `08/01/rollout-2026-08-01T00-23-41-019fba46-ac34-7730-829b-ed9f07026d7a.jsonl`

Implemented the production Postgres repository exclusively within [`backend/internal/proactivity`](../backend/internal/proactivity/).

**Added**
- [`repository_postgres.go`](../backend/internal/proactivity/repository_postgres.go)
  - `NewPostgresRepository(db)`
  - `DefaultRepository()` with no memory fallback
  - Atomic, owner-scoped idempotency across policies, signal batches, and decision batches
  - Strict JSON decoding, canonical revalidation, digest verification, and metadata checks
  - Bounded policy, signal, latest-signal, and decision queries
  - Fail-closed behavior for nil databases, missing schema, corrupt records, and authority-bearing decisions
  - Batch-to-child consistency verification
- [`postgres_schema_contract.go`](../backend/internal/proactivity/postgres_schema_contract.go)
  - Exact future migration contract covering six tables, columns, checks, foreign keys, indexes, and owner-scoped uniqueness
- [`repository_postgres_test.go`](../backend/internal/proactivity/repository_postgres_test.go)
  - Corrupt/unknown/trailing JSON rejection
  - Owner and metadata mismatch rejection
  - Authority-bearing decision rejection
  - Digest mismatch rejection
  - Schema contract validation
  - Optional real-Postgres lifecycle and idempotency coverage

**Verification**
- Package tests passed.
- Repository/Postgres tests passed 50 consecutive runs.
- `go vet ./internal/proactivity` passed.
- Coverage: `53.1%`; live Postgres paths require the optional database.
- `git diff --check` passed.
- The live Postgres test was skipped because `HAI_TEST_DATABASE_DSN` is unset.
- Race testing was unavailable because this Windows Go runtime has CGO disabled.

No router, migration, frontend, or other package was edited.

## 019fba4b-cff4-7ec2-ae35-cb772e08288d

- Date: 2026-08-01
- Agent: Pauli
- Work kind: implementation
- Original report SHA-256: `3787f3042471090f020c3f22852c6ce65eecce95ec2d31694f45cd0f520cfba1`
- Transcript: `08/01/rollout-2026-08-01T00-29-18-019fba4b-cff4-7ec2-ae35-cb772e08288d.jsonl`

Implemented the production Postgres repository for `outcomeevaluation`.

**Files**
- [repository_postgres.go](../backend/internal/outcomeevaluation/repository_postgres.go):27
- [repository_postgres_test.go](../backend/internal/outcomeevaluation/repository_postgres_test.go):11
- [repository_postgres_integration_test.go](../backend/internal/outcomeevaluation/repository_postgres_integration_test.go):21

Implemented:

- `NewPostgresRepository(db)` and `DefaultRepository()`.
- Owner/workspace/outcome-scoped queries and keys.
- Optimistic outcome revision writes.
- Atomic evaluation and correction idempotency.
- Strict JSON decoding with unknown/trailing content rejection.
- Payload, column metadata, scope, timestamp, and audit-digest revalidation.
- Bounded revision, evaluation, and correction histories.
- Fail-closed behavior for nil databases and corrupt storage.
- Advisory-only behavior remains unchanged.
- Exact future `0021` table, column, index, foreign-key, uniqueness, and append-only requirements documented in tests.
- No router, migration, frontend, execution, approval, or policy paths changed.

**Verification**

- `go vet ./internal/outcomeevaluation`
- Package tests repeated 50 times.
- Disposable PostgreSQL 16 lifecycle test passed.
- Concurrent Postgres idempotency test passed with exactly one created evaluation.
- Full backend `go test -vet=off ./...` passed.
- `go test -race` was unavailable because this Windows Go runtime has CGO disabled. The real Postgres concurrency test still exercised the repository race path.

## 019fba4c-664f-70a2-b088-84a8e2af04de

- Date: 2026-08-01
- Agent: Schrodinger
- Work kind: implementation
- Original report SHA-256: `19c6f1604c9376b9e7e6823c04cdbb66310d8be0e77ba6c39b8a6f48b3d35f90`
- Transcript: `08/01/rollout-2026-08-01T00-29-57-019fba4c-664f-70a2-b088-84a8e2af04de.jsonl`

Implemented the production Postgres repository entirely within `backend/internal/resilience`.

**Changed**
- [repository_postgres.go](../backend/internal/resilience/repository_postgres.go)
- [repository_postgres_test.go](../backend/internal/resilience/repository_postgres_test.go)

The implementation includes:

- `NewPostgresRepository(db)`
- `DefaultRepository()` with no memory fallback
- Owner/workspace isolation
- Atomic scoped idempotency
- Transactional lease and heartbeat CAS
- Generation fencing
- Circuit revision CAS
- Append-only retry/dead-letter and recovery records
- Hash-chained event persistence
- Transaction-scoped advisory locks for sequence streams
- Strict JSON decoding, unknown-field rejection, metadata reconciliation, and record revalidation
- Bounded list queries and event-chain boundary validation
- Fail-closed nil database, malformed payload, corrupted metadata, stale fence, and concurrency handling
- Explicit advisory-only boundary with no execution, dispatch, recovery execution, authority grant, or approval consumption

The exact future `0022` table, column, primary-key, index, JSON constraint, append-only trigger, and authority requirements are documented and contract-tested. No migrations or other packages were changed.

**Verification**
- `go vet ./internal/resilience`
- Package suite repeated 20 times
- Disposable PostgreSQL 16 lifecycle tests repeated three times
- 20-way concurrent idempotency test
- Concurrent lease fencing and event-chain race tests
- Live corrupt-payload rejection test
- `git diff --check`

All passed. Race instrumentation could not run because this Windows Go environment has CGO disabled; the equivalent concurrency paths were exercised against live PostgreSQL.

## 019fba53-e2f0-7bd0-b23a-9ab8a9f5c4c0

- Date: 2026-08-01
- Agent: Pascal
- Work kind: implementation
- Original report SHA-256: `ed33a96232256f5ea6af116b46391c9896a0047de1bdcd075e48070ab9d9cef7`
- Transcript: `08/01/rollout-2026-08-01T00-38-07-019fba53-e2f0-7bd0-b23a-9ab8a9f5c4c0.jsonl`

Implemented the production proactivity migration:

- Added `0020_proactivity_advisory.up.sql` and rollback.
- Added six owner-scoped, append-only tables.
- Enforced idempotency, batch-child consistency, JSON constraints, payload limits, and advisory-only decisions.
- Added immutable update/delete/truncate protections.
- Rollback refuses non-empty tables and avoids `CASCADE`.
- Added contract and PostgreSQL integration tests.
- Appended `0020` to the migration chain.

Verification passed:

- Migration and proactivity tests, repeated 20 times.
- `go vet`.
- Real PostgreSQL 17 migration lifecycle.
- Real repository policy, signal, and decision persistence.
- Empty rollback and non-empty rollback refusal.
- `git diff --check`.

Only the requested migration files and tests were changed.

## 019fc982-cde3-7ae2-9da5-1f61373ab336

- Date: 2026-08-03
- Agent: Galileo
- Work kind: implementation
- Original report SHA-256: `6539ceb1d17b5e972f3df7cece56187c0a06b112d9619a731cb7ed6bdab473dd`
- Transcript: `08/03/rollout-2026-08-03T23-23-40-019fc982-cde3-7ae2-9da5-1f61373ab336.jsonl`

Focused frontend work is complete. No backend or unrelated files were changed.

**Changed Paths**
- [governance-control.model.ts](../frontend/src/app/models/governance-control.model.ts)
- [governance-control.service.ts](../frontend/src/app/services/governance-control.service.ts)
- [governance-control.service.spec.ts](../frontend/src/app/services/governance-control.service.spec.ts)
- [governance-control.component.ts](../frontend/src/app/pages/governance-control/governance-control.component.ts)
- [governance-control.component.html](../frontend/src/app/pages/governance-control/governance-control.component.html)
- [governance-control.component.scss](../frontend/src/app/pages/governance-control/governance-control.component.scss)
- [governance-control.component.spec.ts](../frontend/src/app/pages/governance-control/governance-control.component.spec.ts)

**Results**
- Focused Karma tests: `30 SUCCESS`
- Production Angular build: passed
- Governance Control remains below its 17 KB hard style budget at `16.74 KB`
- Existing soft style/initial-bundle warnings remain.
- Node 25 produced the existing non-LTS warning.

Git currently reports these frontend paths as untracked. They were not staged or committed.

## 019fc983-4c37-7782-84a7-096b7ed041e0

- Date: 2026-08-03
- Agent: Aquinas
- Work kind: implementation
- Original report SHA-256: `01ecb9d7a3cd50053537bbb9b38ed44028ea4726bab1253daca971e3be30870c`
- Transcript: `08/03/rollout-2026-08-03T23-24-13-019fc983-4c37-7782-84a7-096b7ed041e0.jsonl`

Implemented focused `lifeledger` backend coverage.

Changed:
- [handler.go](../backend/internal/lifeledger/handler.go): strict `limit` validation from `1..200`; malformed values return `400`.
- [handler_test.go](../backend/internal/lifeledger/handler_test.go): malformed limits, authentication-first handling, owner isolation, and spoofing rejection.
- [repository_postgres_test.go](../backend/internal/lifeledger/repository_postgres_test.go): PostgreSQL revision/idempotency, owner isolation, estimate/incurred distinction, fail-closed behavior, and append-only `UPDATE`/`DELETE`/`TRUNCATE` checks.

Verification passed:
- Full `./internal/lifeledger` package tests.
- PostgreSQL integration tests against a disposable migrated database.
- Disposable database was removed afterward.
- No router, frontend, documentation, or other package files were edited.

## 019fc983-c6ec-7022-9c53-e1cc0ba095c7

- Date: 2026-08-03
- Agent: Copernicus
- Work kind: implementation
- Original report SHA-256: `9593bb4fc7503ad197b7cf6484bc93dacf6b09b77cf18e8371104f47bdbbcfe1`
- Transcript: `08/03/rollout-2026-08-03T23-24-44-019fc983-c6ec-7022-9c53-e1cc0ba095c7.jsonl`

Updated only the requested documentation:

- [framework-operating-contract-matrix.md](framework-operating-contract-matrix.md):154
- [completion-matrix.md](completion-matrix.md):40
- [requirements-traceability.md](requirements-traceability.md):22

Documented:

- Append-only, owner-scoped commitment revisions and cost events.
- Authentication, provenance, revision, idempotency, integrity, and lifecycle boundaries.
- Distinction between estimated, incurred, paid, and refunded costs.
- Advisory commitment/cost life-graph projections.
- Advisory standing-mandate lifecycle projections that cannot grant authority.
- Remaining UI, PostgreSQL acceptance, provider reconciliation, distributed execution, and real-account validation gaps.
- Explicit statement that this does not establish production readiness.

Verification passed:

```text
go test ./internal/lifeledger ./internal/standingmandate ./migrations ./internal/router
```

`git diff --check` also passed. No application code was edited.

## 019fc999-2223-7372-a0ac-6d251ca401c1

- Date: 2026-08-03
- Agent: Hubble
- Work kind: implementation
- Original report SHA-256: `1f4d6b6cdc835fb77317eed78b329b08f92c6f980c0ed5044cd9b5ea235ec013`
- Transcript: `08/03/rollout-2026-08-03T23-48-04-019fc999-2223-7372-a0ac-6d251ca401c1.jsonl`

Implemented the bounded life-ledger hardening.

Changed:
- [validation.go](../backend/internal/lifeledger/validation.go)
- [service_test.go](../backend/internal/lifeledger/service_test.go)
- [handler_test.go](../backend/internal/lifeledger/handler_test.go)

Enforced verification levels:
- `estimate`: `needs_review`, `source_supported`, `human_confirmed`, `verified`
- `incurred`: `source_supported`, `human_confirmed`, `verified`
- `paid`/`refund`: `human_confirmed`, `verified`
- `disputed` is rejected for every financial event kind.

Tests verify the complete matrix, ensure rejected records are not persisted, and confirm the HTTP handler returns `400` for weak paid events.

Commands passed:

```powershell
go test -count=1 ./internal/lifeledger
go test -count=1 -v -run 'TestCostVerificationStrengthMatchesFinancialEventKind|TestHandlerRejectsWeakPaidVerificationWithoutPersisting' ./internal/lifeledger
```

No router, migration, documentation, or frontend files were touched.

## 019fc999-cac7-7ad1-85a0-a5bf9f4c4b24

- Date: 2026-08-03
- Agent: Confucius
- Work kind: implementation
- Original report SHA-256: `d42c80b9fad30ca977c5a6a863266c52759e527ef087bee63f1175d42fb09e01`
- Transcript: `08/03/rollout-2026-08-03T23-48-47-019fc999-cac7-7ad1-85a0-a5bf9f4c4b24.jsonl`

Implemented frontend life-ledger authoring support matching the Go API contracts.

Changed:
- [governance-control.model.ts](../frontend/src/app/models/governance-control.model.ts)
- [governance-control.service.ts](../frontend/src/app/services/governance-control.service.ts)
- [governance-control.service.spec.ts](../frontend/src/app/services/governance-control.service.spec.ts)

Added typed support for:
- Current commitment retrieval and revision history.
- Commitment revision writes with optimistic revision and evidence fields.
- Immutable cost-event writes.
- URL-encoded commitment keys.
- Authenticated owner omission from request bodies.
- Write result envelopes containing `record` and `created`.

Verification:

```text
GovernanceControlService focused tests: 15 SUCCESS
```

No components, templates, styles, or unrelated dirty files were modified. The three requested files remain untracked as they were before this task.

## 019fc9ab-93b8-7643-bbf1-92b76e813078

- Date: 2026-08-04
- Agent: Einstein
- Work kind: advisory
- Original report SHA-256: `fedb2ae173d6c37f15f39048e3750c3a504da40979add4dd943779566b0922c1`
- Transcript: `08/04/rollout-2026-08-04T00-08-12-019fc9ab-93b8-7643-bbf1-92b76e813078.jsonl`

**Highest-Leverage Missing Contract**

Implement **durable, owner-scoped, single-use approval capability consumption across processes and restarts**.

This closes a safety-critical gap in HAI’s core chain:

`approval → controlled execution → verification → audit`

### Why This Is The Priority

The authoritative specification requires approval evidence, immutable execution records, deduplication, restart safety, and exactly-once-effect design ([spec](source-material/whole-life-framework-taxonomy.txt):621, [approval framework](source-material/whole-life-framework-taxonomy.txt):690).

The project’s own matrix explicitly identifies the gap:

> “Approval proof replay state is process-local and not distributed.”

See [framework-operating-contract-matrix.md](framework-operating-contract-matrix.md):39 and its remaining-work section at [line 241](framework-operating-contract-matrix.md):241.

Current implementation confirms this:

- [approval_proof.go](../backend/internal/automation/approval_proof.go):143 stores consumed proofs in a Go map.
- [approval_proof.go](../backend/internal/automation/approval_proof.go):168 creates a new random signing key at process startup.
- [automation_service.go](../backend/internal/automation/automation_service.go):656 can mint another proof from the same durable approval decision.
- [automation_service.go](../backend/internal/automation/automation_service.go):1531 consumes that proof only through the process-local service.
- The durable authorization schema uniquely consumes a receipt, but does not prevent one approval source from producing multiple distinct receipts. See [migration 0024](../backend/migrations/pre/0024_execution_authorization_schema_compatibility.up.sql):211 and [its consumption constraint](../backend/migrations/pre/0024_execution_authorization_schema_compatibility.up.sql):340.

Therefore, two workers or a restarted backend could reuse the same underlying approval decision for repeated execution.

**Bounded Implementation Slice**

Extend these existing modules:

1. [approval_proof.go](../backend/internal/automation/approval_proof.go):138
   Retain the memory implementation for unit tests, but introduce a PostgreSQL-backed production service.

2. Add `backend/internal/automation/approval_proof_repository.go` and `approval_proof_repository_postgres.go`
   Persist proof issuance metadata and append-only consumption records.

3. Add the next migration after the current pending `0027`, expected as:
   - `backend/migrations/pre/0028_durable_approval_exercises.up.sql`
   - `backend/migrations/pre/0028_durable_approval_exercises.down.sql`

   Store owner, approval source, automation, action digest, scope, token digest, expiry and consumption evidence. Enforce uniqueness over the exact approval binding.

4. [automation_service.go](../backend/internal/automation/automation_service.go):1108
   Require durable atomic consumption before API, script, Docker, or agent-runtime access.

5. [automation_executor.go](../backend/internal/task/automation_executor.go):43
   Preserve the existing exact-action binding while using the durable service.

6. [routes.go](../backend/internal/router/routes.go):475
   Construct and inject the PostgreSQL service. Production must fail closed rather than silently falling back to memory.

7. [inspection_handler.go](../backend/internal/executionauth/inspection_handler.go):47
   Expose redacted approval-exercise status alongside authorization receipts.

8. Extend Governance Control’s existing receipt inspector, without introducing another dashboard:
   - `frontend/src/app/services/governance-control.service.ts`
   - `frontend/src/app/models/governance-control.model.ts`
   - `frontend/src/app/pages/governance-control/`

### Acceptance Evidence

1. A fresh PostgreSQL migration applies, rolls back and reapplies successfully.
2. Proof issuance survives constructing a second service instance.
3. Thirty-two concurrent consumers across two service instances produce exactly one success.
4. All remaining consumers receive `ErrApprovalProofConsumed`.
5. Restarting the backend cannot make a consumed approval reusable.
6. A second launch intent using the same single approval is rejected before HTTP, process, filesystem, Docker socket, or runtime access.
7. Tampered owner, automation, source, scope, digest or expired proof is rejected without creating consumption evidence.
8. A different owner receives `404`-equivalent isolation and cannot inspect or consume the proof.
9. Approval issuance and consumption records reject update, delete and truncate.
10. A local HTTP receiver proves one approved launch results in one request; replay after restart leaves its counter at one.
11. Governance Control shows approval source, action binding, issuance, expiry and consumed status without exposing nonce, signature or secrets.
12. Focused tests and the full Go suite pass against disposable PostgreSQL; frontend tests/build pass if inspection UI changes.

Contact promotion/correction is not the recommended next slice because it is already substantially present in the current worktree, including an untracked `0027_contact_review_decisions` migration. External reconciliation needs real provider access, while high-availability workflow coordination is too broad for one bounded slice.

No files were edited and no tests were run during this read-only analysis.

## 019fc9ac-1626-78d3-b3cb-5f0175ec5b3c

- Date: 2026-08-04
- Agent: Hume
- Work kind: advisory
- Original report SHA-256: `4e2f4ba6ea7d109289298183dfedec415080df952535e842b07a54cf8644d0d3`
- Transcript: `08/04/rollout-2026-08-04T00-08-46-019fc9ac-1626-78d3-b3cb-5f0175ec5b3c.jsonl`

**Conclusion**

The live HAI services are healthy, but the browser E2E suite is not currently runnable or trustworthy against them. Its “not yet executed” status is accurate.

**Why It Is Unexecuted**

- The only test is skipped when `E2E_OPERATOR_PASSWORD` is absent. All three E2E environment variables are currently absent.
- The default URL is `http://localhost:8080`, but the live gateway is at `http://localhost`; port `8080` refuses connections.
- `frontend/e2e/node_modules` and `package-lock.json` do not exist.
- CI does not install or run `frontend/e2e`.
- The suite was committed with unresolved source selectors and stale routes.
- It creates sources, workflows, transitions, and worker executions without cleanup or isolation.

The live database already contains 1 connected source, 1 raw item, 1 sync job, 3 workflows, and 2 pursuits. Running this suite would contaminate Robert’s real local state.

**Exact Failure Sequence**

With the repository exactly as configured:

1. The suite skips at [acceptance.spec.ts:37](../frontend/e2e/tests/acceptance.spec.ts):37 because `E2E_OPERATOR_PASSWORD` is missing.
2. Supplying a password without overriding the base URL produces a connection failure because [playwright.config.ts:4](../frontend/e2e/playwright.config.ts):4 targets port `8080`.
3. After correcting the URL, `getByLabel(/email/i)` at [acceptance.spec.ts:29](../frontend/e2e/tests/acceptance.spec.ts):29 fails. The login inputs have placeholders but no associated labels or ARIA labels. The password locator has the same problem.
4. `/home/connected-sources` and `/home/workflow-engine` are invalid. Current routes are `/connected-sources` and `/workflow-engine`.
5. `source-connect`, `source-connected`, and `source-sync` do not exist anywhere in the Angular source. Conditional `count()` checks silently skip both steps, creating false coverage.
6. Even on the correct workflow route, the test clicks `workflow-create` without filling the required `input` field. `intake()` returns immediately, so `workflow-apply-transition` never appears.
7. Unmatched intake can create a pursuit candidate requiring explicit acceptance instead of a workflow. The test does not handle that supported lifecycle.
8. `workflow-apply-transition` is a state transition, not the approval action claimed by the test.
9. `panel-activity-audit` exists only on Control Center at [control-center.component.html:508](../frontend/src/app/pages/control-center/control-center.component.html):508. The test remains on Workflow Engine, so its final assertion fails.

**Seed And Authentication**

- The IDP is healthy.
- An active owner row exists for `noodzakelijkonline@gmail.com`.
- Local preview is enabled, loopback-bound, and advertised by the capabilities API.
- The suite does not support local-preview authentication; it always requires a password.
- The configured first-run password is only applied when the user is initially created. Restarting with a changed `.env.local` password does not update the existing hash, so environment presence alone does not prove login capability.
- I deliberately did not attempt password login because it updates authentication state and generates a refresh session.

**Smallest Corrective Scope**

1. Add a disposable E2E Compose override with fresh IDP and automation volumes, deterministic owner credentials, and a local read-only fixture.
2. Change the default base URL to `http://localhost`.
3. Add a lockfile and ignore `playwright-report/` and `test-results/`.
4. Use accessible placeholder locators or add real labels to login inputs.
5. Correct both routes.
6. Add stable source action/result selectors and remove conditional skips.
7. Fill deterministic workflow input and pre-seed or explicitly accept a pursuit candidate.
8. Exercise the actual approval control, not a generic transition.
9. Assert workflow audit state on Workflow Engine or navigate to Control Center.
10. Add a CI job that starts the disposable stack, runs the suite, and destroys its volumes.

No files or application records were changed during this audit.

## 019fc9b6-060d-7650-bba2-5e961eb0c122

- Date: 2026-08-04
- Agent: Carson
- Work kind: implementation
- Original report SHA-256: `a4dc69c9971b161ec607fb1b7fb12fd624f9a4128978148af6ebaa8f0ee324ef`
- Transcript: `08/04/rollout-2026-08-04T00-19-37-019fc9b6-060d-7650-bba2-5e961eb0c122.jsonl`

Implemented the governed contact-review frontend in Governance Control.

**Added**
- Typed contact candidate, merge proposal, decision, request, and response models.
- Filtered loading of local-only person candidates with `attributes.candidate = "true"`.
- Decision-history loading alongside whole-life context.
- Resolved candidates and merge proposals are excluded from pending queues.
- Review drawer supporting:
  - Candidate: promote, correct, reject.
  - Proposal: merge, keep distinct, reject.
- Meaningful-reason and canonical-name validation.
- Stable idempotency keys and duplicate-submission prevention.
- Post-success refresh and immutable decision display.
- Explicit local-only, advisory-only, no-authority messaging.

**Verification**
- Focused frontend tests: `42 SUCCESS`.
- Angular production build: passed.
- Governance stylesheet remains below its 17 KB hard budget at 16.96 KB.
- Existing warnings remain for the 1.07 MB initial bundle, several soft SCSS budgets, and non-LTS Node.js 25.

Changed paths:
- [governance-control.model.ts](../frontend/src/app/models/governance-control.model.ts)
- [governance-control.service.ts](../frontend/src/app/services/governance-control.service.ts)
- [governance-control.service.spec.ts](../frontend/src/app/services/governance-control.service.spec.ts)
- [governance-control.component.ts](../frontend/src/app/pages/governance-control/governance-control.component.ts)
- [governance-control.component.html](../frontend/src/app/pages/governance-control/governance-control.component.html)
- [governance-control.component.scss](../frontend/src/app/pages/governance-control/governance-control.component.scss)
- [governance-control.component.spec.ts](../frontend/src/app/pages/governance-control/governance-control.component.spec.ts)

No backend or documentation files were changed.

## 019fc9b6-82d5-73e0-8160-6487579bc542

- Date: 2026-08-04
- Agent: Arendt
- Work kind: advisory
- Original report SHA-256: `ded507065d4531f349947425b7c4574c7908fa4fa858eb495100c20ad025d3dd`
- Transcript: `08/04/rollout-2026-08-04T00-20-09-019fc9b6-82d5-73e0-8160-6487579bc542.jsonl`

**Findings**

1. **High: rollback destroys immutable review evidence while promoted contacts remain.**
[0027 down migration](../backend/migrations/pre/0027_contact_review_decisions.down.sql):1 drops the complete decision ledger, but canonical contacts remain in `life_ontology_entities`. This breaks provenance and permits re-deciding previously finalized subjects after reapplication.
**Fix:** make rollback refuse when decision rows exist, or treat this as an irreversible migration. Never drop populated audit/evidence tables.

2. **High: append-only and owner isolation are not security boundaries in the default deployment.**
The application applies migrations using its runtime connection in [database.go](../backend/internal/infra/database.go):35, while the default database user is PostgreSQL superuser `postgres` in [.env.example](../.env.example):10. That role can disable/drop the triggers at [0027 up migration](../backend/migrations/pre/0027_contact_review_decisions.up.sql):76. There is also no row-level security.
**Fix:** separate migrator and runtime roles. Grant runtime only `SELECT`/`INSERT`, revoke DDL/`UPDATE`/`DELETE`/`TRUNCATE`, and add forced owner-scoped RLS where multi-owner isolation is expected.

3. **High: PostgreSQL permits orphaned or falsely bound decisions.**
The migration only creates a foreign key for `canonical_entity_id` at [lines 20-23](../backend/migrations/pre/0027_contact_review_decisions.up.sql):20. `subject_id` and `candidateEntityIds` are not linked to candidate entities or merge proposals. The payload check merely requires the array to exist at [lines 45-50](../backend/migrations/pre/0027_contact_review_decisions.up.sql):45. Go validation checks ID syntax, not repository existence, at [contact_review.go:313](../backend/internal/lifeontology/contact_review.go):313.
**Fix:** persist candidate IDs as columns with owner-scoped foreign keys. Use separate candidate/merge decision tables or a constraint trigger that verifies subject type, proposal membership, candidate count, and canonical entity properties atomically.

4. **Medium: secret material can enter the immutable ledger.**
Contact-review validation only bounds reason/name/summary lengths at [contact_review.go:287](../backend/internal/lifeontology/contact_review.go):287. It does not call the existing secret detector used by normal entities at [validation.go:434](../backend/internal/lifeontology/validation.go):434. This especially affects `reason`, and all text on reject/keep-distinct decisions.
**Fix:** reject or redact secrets in every decision text field before hashing and insertion.

5. **Medium: decision timestamps can be arbitrarily backdated.**
`decidedAt` comes directly from the request at [handler.go:192](../backend/internal/lifeontology/handler.go):192, and validation only rejects future times at [contact_review.go:290](../backend/internal/lifeontology/contact_review.go):290. An immutable decision can therefore claim to have occurred years earlier.
**Fix:** set authoritative `decidedAt` from the server clock. Store user-supplied historical time separately as `assertedAt`, with bounded skew and provenance.

6. **Medium: listing performs an unbounded database scan.**
The repository loads and validates every owner decision at [repository_postgres.go:424](../backend/internal/lifeontology/repository_postgres.go):424; only afterward does the service truncate the result at [contact_review.go:103](../backend/internal/lifeontology/contact_review.go):103. The immutable table can grow indefinitely.
**Fix:** pass limit/cursor into the repository and use indexed keyset pagination in SQL.

7. **Low: `IF NOT EXISTS` can conceal schema drift.**
The migration uses `CREATE TABLE IF NOT EXISTS` at [line 1](../backend/migrations/pre/0027_contact_review_decisions.up.sql):1 and `CREATE INDEX IF NOT EXISTS` at [line 72](../backend/migrations/pre/0027_contact_review_decisions.up.sql):72. A sufficiently similar pre-existing object can bypass intended constraints or index definitions.
**Fix:** use strict creation in versioned migrations or explicitly validate catalog definitions before recording the migration.

**Verification**

- Focused life-ontology tests passed.
- Migration contract/chain tests passed.
- The PostgreSQL contact-review integration test passed against a disposable database, which was removed afterward.
- Existing PostgreSQL coverage does not test rollback data loss, failed-transaction orphan prevention, concurrent competing decisions, or database-role bypass.
- No files or live application records were changed.

## 019fca48-f5e7-75a0-9ae6-7a3525fe6dde

- Date: 2026-08-04
- Agent: Curie
- Work kind: advisory
- Original report SHA-256: `4772263834b05f4d6fee68d6ad0a14cbf590d5f67717ee13a42bffa151751bf3`
- Transcript: `08/04/rollout-2026-08-04T03-00-07-019fca48-f5e7-75a0-9ae6-7a3525fe6dde.jsonl`

**Findings**

1. **[P1] Framework risk ceilings are validated but never enforced.**
`RiskCeiling` is mandatory in the schema and catalog validation, but candidate selection only evaluates textual scoring and coverage. A high-risk request can therefore select a low-risk framework. See [types.go](../backend/internal/frameworkregistry/types.go):46, [catalog.go](../backend/internal/frameworkregistry/catalog.go):694, and [selector.go](../backend/internal/frameworkregistry/selector.go):281. This violates the specification’s risk-ceiling requirement at [whole-life-framework-taxonomy.txt](source-material/whole-life-framework-taxonomy.txt):1804.

2. **[P1] Missing required specialists do not block execution.**
When a required role has no verified agent, the registry creates an unassigned placeholder ([operating_context.go](../backend/internal/frameworkregistry/operating_context.go):417). The coordinator then falls back to `single_engine` ([operating_context.go](../backend/internal/frameworkregistry/operating_context.go):597), while execution checks unready delegations only for multi-agent modes ([service.go](../backend/internal/task/service.go):2549). Consequently, a framework can require a specialist while the generic task engine proceeds without one.

3. **[P1] Most framework operating fields remain descriptive metadata.**
Selection aggregates agents, evidence, evaluation methods, and input descriptions, but drops `ProducedOutputs`, `WorkflowTemplate`, `DecisionRules`, and `SafetyInvariants` ([selector.go](../backend/internal/frameworkregistry/selector.go):328). Task execution still receives a generic task sequence ([service.go](../backend/internal/task/service.go):2636). Required inputs are recorded as context descriptions, not checked for satisfaction before execution. The registry therefore selects named disciplines without compiling much of their actual behavior.

4. **[P2] Chief-of-staff selection happens before relevant owner and pursuit context is retrieved.**
Framework selection runs at [service.go](../backend/internal/task/service.go):891, while memory, source, and life-ontology retrieval happen later at lines [955](../backend/internal/task/service.go):955, [963](../backend/internal/task/service.go):963, and [973](../backend/internal/task/service.go):973. `PursuitID` contributes only identifier text to keyword classification ([selector.go](../backend/internal/frameworkregistry/selector.go):468); the pursuit’s objective, commitments, evidence, and state cannot influence the selection.

5. **[P2] The learning step records outcomes but does not improve framework selection.**
The learning recorder stores selected framework IDs only as tags ([controlled_learning.go](../backend/internal/task/controlled_learning.go):78). Future selections use only the catalog, preferences, Constitution, and current request ([service.go](../backend/internal/frameworkregistry/service.go):290). There is no framework-fit result, selection-decision linkage, per-framework success metric, or reviewed adaptation feeding back into scoring.

**Prioritized Slice**

Implement risk-ceiling enforcement first because it is bounded and closes an immediate safety-contract gap:

- Add deterministic `low < medium < high` comparison in `selector.go`.
- Exclude optional frameworks below the request’s risk.
- Fail closed when a mandatory framework cannot support the classified risk.
- Add selector tests for below-ceiling rejection, exact-ceiling acceptance, mandatory failure, and deterministic combinations.

No files were edited. `go test ./internal/frameworkregistry ./internal/task` passed; those tests currently confirm internal consistency but do not cover the five behaviors above.

## 019fca49-7a79-71f3-a8c0-9339a72f7823

- Date: 2026-08-04
- Agent: Poincare
- Work kind: advisory
- Original report SHA-256: `c0bd71c375bb1313ad13cd2b1569f49919e1f715c52bc45eb0a5d837f0ab4f8b`
- Transcript: `08/04/rollout-2026-08-04T03-00-41-019fca49-7a79-71f3-a8c0-9339a72f7823.jsonl`

**Findings**
1. **[P0] System actors can self-declare their authority and safety profile.**
`executionauth.Request` accepts caller-supplied stage, risk, reversibility, autonomy, authority, tools, data scopes, and paths ([types.go](../backend/internal/executionauth/types.go):60). Validation checks values and ranges, not whether they truthfully describe the effect ([validation.go](../backend/internal/executionauth/validation.go):73). Only `ActorAgent` receives assignment and allowlist checks ([service.go](../backend/internal/executionauth/service.go):185); system actors bypass the tool, data, runtime, and folder controls at [service.go](../backend/internal/executionauth/service.go):407.

   Consequently, an internal component can submit `ActorSystem + StageExecution + RiskLow + Reversible=true + RequestedAutonomy=8`. The approval calculation at [service.go](../backend/internal/executionauth/service.go):254 can authorize this without approval or mandate. Constitution capabilities are also derived from those caller-controlled labels and strings ([service.go](../backend/internal/executionauth/service.go):616). Task orchestration uses this privileged system identity at [automation_executor.go](../backend/internal/task/automation_executor.go):124.

   This conflicts directly with specification sections 1, 19, and 29-31: least delegated authority, derived per-action autonomy, strong workload identity, deny-by-default, and confused-deputy protection.

2. **[P1] Level-10 standing mandates can be non-expiring, approval-free, and consumption-unbounded.**
`ExpiresAt` is optional and only validated when supplied ([validation.go](../backend/internal/standingmandate/validation.go):59). `ApprovalNever` is valid at every autonomy level ([validation.go](../backend/internal/standingmandate/validation.go):203), while maximum risk is optional. A resource scope with no IDs covers every resource of that type ([evaluator.go](../backend/internal/standingmandate/evaluator.go):205).

   The model contains no maximum executions, cumulative spend, rate limit, or privacy budget ([types.go](../backend/internal/standingmandate/types.go):83). Once authorized, a mandate substitutes for case approval at [service.go](../backend/internal/executionauth/service.go):274. This does not meet “fully autonomous inside a tightly bounded mandate” or time/budget-limited standing approval.

3. **[P1] Connected-source content has no instruction-authority or taint boundary.**
Workflow intake records source metadata and a caller-provided `RequiresReview`, but no trust level, instruction authority, prompt-injection status, or data classification ([service.go](../backend/internal/workflow/service.go):45). Review is forced only when the caller sets that flag ([service.go](../backend/internal/workflow/service.go):472).

   Otherwise, raw email/document/chat text is keyword-classified, defaults to low-risk administrative work, and becomes `autonomous_safe` ([service.go](../backend/internal/workflow/service.go):2955). One matching automation may be selected automatically ([service.go](../backend/internal/workflow/service.go):488), after which the source-derived description is passed to the task engine ([service.go](../backend/internal/workflow/service.go):2069). Final authorization limits many high-risk effects, but the chain remains vulnerable to context manipulation and low-risk confused-deputy actions.

4. **[P1] Case approvals expire but cannot be withdrawn or revoked.**
Resolved approvals contain identity, binding, approver, and expiry, but no revocation state or revision ([types.go](../backend/internal/executionauth/types.go):103). Task and workflow approval resolvers accept an approved decision for 15 minutes ([task_review_resolver.go](../backend/internal/executionapproval/task_review_resolver.go):24); final rechecking only resolves the same record again ([service.go](../backend/internal/executionauth/service.go):571). There is no withdrawal state for immediate user correction, contrary to sections 1 and 20.

5. **[P2] Standing-mandate purpose is descriptive, not enforced.**
A mandate stores `Purpose`, but `ActionRequest` contains no requested purpose ([types.go](../backend/internal/standingmandate/types.go):104). Scope matching only considers action, risk, project, domain, tool, and resource ([evaluator.go](../backend/internal/standingmandate/evaluator.go):189). Authority granted for one declared purpose can therefore be reused for another purpose with matching technical identifiers.

6. **[P2] Constitution activation lacks security-sensitive reauthentication.**
Activation correctly requires the owner, an exact confirmation phrase, a note, current base version, and draft validation ([service.go](../backend/internal/frameworkregistry/service.go):846). However, the request contains no recent-authentication, MFA, session-age, or independent approval evidence ([types.go](../backend/internal/frameworkregistry/types.go):483). A stolen authenticated admin session can amend the Constitution immediately.

**Highest-Value Bounded Patch**
Add a mandatory `SystemWorkloadPolicyResolver` to `executionauth.Service`.

For every `ActorSystem` request, it should resolve an immutable, versioned grant keyed by actor and action containing:

- Exact allowed stages, tools, runtimes, data scopes, and folder roots.
- Minimum risk and required authority.
- Authoritative reversibility.
- Maximum autonomy.
- Approval and mandate requirements.
- Policy revision and digest included in the receipt and rechecked before consumption.

Unknown system actors or profile mismatches must fail closed. The final service should overwrite or reject caller-supplied classifications before deriving Constitution capabilities.

**Minimal Acceptance Test**
`TestSystemActorCannotDowngradeConsequentialEffect`:

1. Register `hai-task-engine / automation.script.execute` as high-risk, irreversible, authority 6, autonomy maximum 6.
2. Submit it as `ActorSystem`, but claim low risk, reversible, autonomy 8, with no approval.
3. Assert `OutcomeDenied` with `workload.profile_mismatch`.
4. Assert no execution receipt consumption exists.

This one patch closes the most serious authority bypass and establishes the correct foundation for source-taint and mandate enforcement.

No files were edited. I could not execute the focused Go tests because the current shell has no `go` executable available.

## 019fca4a-0fe5-7d51-b729-4f3ad1e74a0e

- Date: 2026-08-04
- Agent: Bacon
- Work kind: advisory
- Original report SHA-256: `58f447b88a4722c04c7153f555ec6c9166283ef6c15ae7c833eb75cc0811300b`
- Transcript: `08/04/rollout-2026-08-04T03-01-19-019fca4a-0fe5-7d51-b729-4f3ad1e74a0e.jsonl`

**Audit Findings**

1. **[P0] Revoked sources remain usable as knowledge.** Revocation deletes OAuth credentials and marks the connector revoked, but retained extractions, indexes, and derived memories remain available ([repository.go](../backend/internal/source/repository.go):118). Source visibility does not check `Enabled`, `Status`, or `RevokedAt` ([service.go](../backend/internal/source/service.go):1285). Those memories and extractions subsequently enter task planning ([task/service.go](../backend/internal/task/service.go):956). This violates sections 21, 23, and 25: forgetting, permission filtering, and revocation propagation.

2. **[P1] The knowledge graph is implemented but disconnected from the application.** The graph supports temporal, source-linked claims ([claim_types.go](../backend/internal/knowledgegraph/claim_types.go):26), but router startup wires memory, sources, and verification without constructing the graph service ([routes.go](../backend/internal/router/routes.go):184). The frontend calls `/sources/knowledge-graph` ([connected-source.service.ts](../frontend/src/app/services/connected-source/connected-source.service.ts):100), while no such route exists in the source route group ([routes.go](../backend/internal/router/routes.go):1039). The catalog nevertheless advertises it as operational ([integration_boundary.go](../backend/internal/braincatalog/integration_boundary.go):54).

3. **[P1] Heuristic source summaries become memory without verification.** Extraction uses keyword and string heuristics ([source/service.go](../backend/internal/source/service.go):1776). Any summary not marked sensitive or uncertain is promoted automatically with fixed confidence `0.68` ([source/service.go](../backend/internal/source/service.go):1804). Task generation then consumes it as context ([task/service.go](../backend/internal/task/service.go):2011). Section 24 requires claims to remain observations or hypotheses until supported.

4. **[P1] Verification can succeed without durable claim evidence.** Errors from evidence and claim persistence are discarded ([verification/service.go](../backend/internal/verification/service.go):163); memory-write errors are also ignored ([verification/service.go](../backend/internal/verification/service.go):471). A run can consequently report completion while its evidence trail is incomplete.

5. **[P1] Claim verification is token overlap, not evidence entailment.** Support is calculated from lexical overlap plus a quality score ([verification/service.go](../backend/internal/verification/service.go):427); contradiction detection recognizes only a few positive/negative words ([verification/service.go](../backend/internal/verification/service.go):609). Citation precision, temporal validity, quote accuracy, source hierarchy, corroboration, and structured contradiction resolution from section 24 are absent.

6. **[P2] Memory is a flat record store rather than the section-21 lifecycle.** A memory contains one arbitrary kind, one confidence value, and one source reference ([context_memory.go](../backend/internal/models/context_memory.go):8). Similar records are concatenated and assigned the higher confidence, potentially merging conflicting statements while retaining only one source ([memory/service.go](../backend/internal/memory/service.go):621). There is no correction lineage, supersession, temporal validity, negative memory, retention policy, or provenance-preserving consolidation.

7. **[P2] Graph retrieval omits most section-23 controls.** It loads all owner nodes and edges, performs token matching, then bounded graph traversal ([retrieval.go](../backend/internal/knowledgegraph/retrieval.go):29). Ranking uses lexical relevance, project, confidence, recency, and verification only ([retrieval.go](../backend/internal/knowledgegraph/retrieval.go):207). The request has no permission purpose, sensitivity ceiling, authority requirement, retrieval strategy, or context-token budget ([types.go](../backend/internal/knowledgegraph/types.go):225).

8. **[P2] Incremental sync lacks general reconciliation and tombstones.** Raw items and extractions have hashes but no upstream version, deletion state, schema version, or supersession lineage ([connected_source.go](../backend/internal/models/connected_source.go):66). Deleted or corrected upstream records can therefore remain active unless a connector handles that case explicitly.

9. **[P2] Section-34 evaluation is infrastructure, not an operational feedback loop.** The generic evaluation package defines versioned datasets and reproducible run records ([evaluation/types.go](../backend/internal/evaluation/types.go):44), but it is not wired into routes or task/workflow completion. The separate outcome evaluator is exposed ([routes.go](../backend/internal/router/routes.go):298), yet evaluations require explicit API creation rather than automatic observation after execution. Groundedness, false-autonomy, recovery, correction burden, and longitudinal-value metrics are not collected from live runs.

**Bounded High-Value Patch**

Implement a **fail-closed source revocation barrier**:

1. Add an internal `SourceID` provenance field to source-derived memories.
2. Make source visibility reject disabled, revoked, or revoked-at connectors.
3. In the existing revocation transaction, archive source extractions and memories and invalidate index entries for that `SourceID`.
4. Emit one immutable revocation/tombstone audit receipt with affected-record counts.
5. Add an integration test proving that after revocation, source search, memory retrieval, task context generation, and strict verification cannot access the former source.

This is narrowly scoped to one lifecycle boundary but closes the most consequential privacy and correctness defect across sections 21, 23, and 25.

No files were edited. This was a static audit; the local shell does not currently have `go` available, so I could not execute the Go test suite.

## 019fca50-ac64-7130-9f54-455a9d9c37d5

- Date: 2026-08-04
- Agent: Sagan
- Work kind: implementation
- Original report SHA-256: `8e104cac6e33e16e67e3de861cfa44cf23d2f68c12c3ae92a5adc7d6840455bc`
- Transcript: `08/04/rollout-2026-08-04T03-08-32-019fca50-ac64-7130-9f54-455a9d9c37d5.jsonl`

**Findings**

1. **[P1] Governance Control is not committed source.** The entire [governance-control component](../frontend/src/app/pages/governance-control/governance-control.component.ts):295, its frontend service, and the [agent-team handler](../backend/internal/frameworkregistry/agent_team_handler.go):32 are untracked. They may work locally, but a clean clone or deployment does not contain them. Governance UI readiness cannot currently be claimed.

2. **[P1] Basic/Advanced mode has two independent authorities.** The shell stores and applies per-module mode at [app-shell.component.ts:73](../frontend/src/app/control-room/app-shell.component.ts):73, while Framework Registry independently reads and writes the same storage key at [framework-registry.component.ts:972](../frontend/src/app/pages/framework-registry/framework-registry.component.ts):972. It also renders a second switch at [framework-registry.component.html:22](../frontend/src/app/pages/framework-registry/framework-registry.component.html):22. Either control can contradict the other, violating the single progressive-disclosure contract.

3. **[P1] Unavailable overview data is presented as real zero values.** When the overview request fails, Enabled, Pinned, and Experimental become `0` at [framework-registry.component.html:339](../frontend/src/app/pages/framework-registry/framework-registry.component.html):339, despite partial-failure handling at [framework-registry.component.ts:326](../frontend/src/app/pages/framework-registry/framework-registry.component.ts):326. These should display unavailable or derive counts from successfully loaded framework records.

4. **[P2] Module primary actions are dead metadata.** Framework Registry and Governance declare `Select frameworks` and `Review governance` at [module-registry.ts:56](../frontend/src/app/control-room/module-registry.ts):56, but the shell renders only identity and view controls at [app-shell.component.html:40](../frontend/src/app/control-room/app-shell.component.html):40. No production code consumes `primaryAction` or its capability identifier.

5. **[P2] Collapsed governance sections do not defer their work.** `refresh()` eagerly loads teams, life context, ledger, and proactivity at [governance-control.component.ts:323](../frontend/src/app/pages/governance-control/governance-control.component.ts):323. Consequently, section-open loaders such as [governance-control.component.html:174](../frontend/src/app/pages/governance-control/governance-control.component.html):174 do not provide the required lazy-loading behavior.

6. **[P2] Agent-team governance stops at a read-only list.** Team records at [governance-control.component.html:198](../frontend/src/app/pages/governance-control/governance-control.component.html):198 are not clickable and expose no lifecycle actions. The frontend service only implements list at [governance-control.service.ts:242](../frontend/src/app/services/governance-control.service.ts):242, although the local backend defines guarded version, event, activation, suspension, retirement, revocation, membership, delegation, and consensus routes at [agent_team_handler.go:44](../backend/internal/frameworkregistry/agent_team_handler.go):44. These routes remain a local-only capability until committed.

7. **[P2] Manual framework recommendations cannot be linked to a task plan.** The API contract supports `taskPlanId` at [framework-registry.model.interface.ts:76](../frontend/src/app/models/framework-registry.model.interface.ts):76, and the backend persists it at [repository.go:829](../backend/internal/frameworkregistry/repository.go):829. The page draft omits it at [framework-registry.component.ts:44](../frontend/src/app/pages/framework-registry/framework-registry.component.ts):44, so selections remain isolated recommendations rather than task-plan governance records.

**Already Aligned**

The registry data model closely covers the specification’s required versioned fields, including purpose, triggers, workflow, safety, authority, evidence, conflicts, adaptations, provenance, and lifecycle at [framework-registry.model.interface.ts:23](../frontend/src/app/models/framework-registry.model.interface.ts):23. Its recommendation path is source-backed and explicitly keeps approval separate from execution.

**One Bounded Slice**

Implement **task-plan-bound framework selection** only. Add `taskPlanId` to the Advanced selection context, submit it through the already-supported request contract, and display the linked plan ID in the recommendation and history. This requires no backend expansion and closes a real Chief-of-Staff planning gap without introducing execution authority.

No files were edited. This was a static source and Git worktree audit; live browser inspection timed out, and tests were not run.

## 019fcace-087c-78e2-b3c5-daae071b26de

- Date: 2026-08-04
- Agent: Ramanujan
- Work kind: implementation
- Original report SHA-256: `3df10344657853bf769a654b7e4843aba071f98d4719650ddcd348cf2ffa4d74`
- Transcript: `08/04/rollout-2026-08-04T05-25-28-019fcace-087c-78e2-b3c5-daae071b26de.jsonl`

Implemented selector risk-ceiling coverage in [selector_test.go](../backend/internal/frameworkregistry/selector_test.go):168:

- Optional framework below task risk is omitted.
- Exact risk ceiling is accepted.
- Mandatory framework below task risk fails closed.
- Missing and invalid ceilings fail closed.
- Inline framework fixtures now declare explicit ceilings.

Focused Docker tests passed, including all `TestBuildSelection*` tests. The complete package run still has two pre-existing catalog/taxonomy digest failures. No production files were edited.

## 019fcace-a461-76e1-99a7-6bfe282d7a45

- Date: 2026-08-04
- Agent: Heisenberg
- Work kind: advisory
- Original report SHA-256: `45af8937f6f42517e3457b874963f811edf1558031fc7324909924b8197ed051`
- Transcript: `08/04/rollout-2026-08-04T05-26-08-019fcace-a461-76e1-99a7-6bfe282d7a45.jsonl`

**Audit Findings**
1. **The current v5 migration is incomplete.** [0029 up](../backend/migrations/pre/0029_framework_selector_v5_digest.up.sql):1 only requires a nonzero operating digest. It does not persist or validate the task risk and effective risk ceiling.
2. **v5 risk enforcement exists only during selection.** [selector.go](../backend/internal/frameworkregistry/selector.go):221 filters incompatible frameworks, but the resulting decision lacks a top-level risk contract that downstream execution can independently enforce.
3. **The risk contract is not covered by the operating digest.** [operating_context.go](../backend/internal/frameworkregistry/operating_context.go):103 hashes autonomy and operating context, but not task risk or effective risk ceiling.
4. **Selection identity is not version-bound.** [selector.go](../backend/internal/frameworkregistry/selector.go):1294 omits selector version, catalog digest, task risk, and effective risk ceiling. Equivalent timestamped v4/v5 decisions can theoretically collide.
5. **Downstream authorization does not receive the ceiling.** [governance_binding.go](../backend/internal/task/governance_binding.go):18 and [executionauth/types.go](../backend/internal/executionauth/types.go):167 carry framework digests but not selector version or risk limits.
6. **Frontend and documentation still describe v4.** The runtime constants are already v5, while fixtures and operational documentation remain v4.

**Required Backend Files**
Core selector and audit contract:

- [service.go](../backend/internal/frameworkregistry/service.go):17: retain `selector-v5`; retain catalog `v2` because `evaluation` changed to version `1.1.0` with a high-risk ceiling.
- [selector.go](../backend/internal/frameworkregistry/selector.go):201: calculate and return `taskRiskLevel` and `effectiveRiskCeiling`; include both in identity.
- [types.go](../backend/internal/frameworkregistry/types.go):208: add risk ceiling to selected frameworks and top-level decision.
- [operating_context.go](../backend/internal/frameworkregistry/operating_context.go):20: include the risk contract in the v5 operating digest.
- [catalog.go](../backend/internal/frameworkregistry/catalog.go):384 and [catalog_test.go](../backend/internal/frameworkregistry/catalog_test.go):15: retain the v2 catalog and reviewed v2 golden digest.
- [framework_registry.go](../backend/internal/models/framework_registry.go):28: add nullable persisted task-risk and effective-ceiling fields.
- [repository.go](../backend/internal/frameworkregistry/repository.go):775: require the fields for v5 writes, tolerate missing values for v4 history.

Migration and persistence tests:

- [0029 up](../backend/migrations/pre/0029_framework_selector_v5_digest.up.sql):1
- [0029 down](../backend/migrations/pre/0029_framework_selector_v5_digest.down.sql):1
- [migration_contract_test.go](../backend/internal/frameworkregistry/migration_contract_test.go):60
- [migration_chain_contract_test.go](../backend/migrations/migration_chain_contract_test.go):90
- `selector_test.go`, `service_test.go`, `operating_context_test.go`, `repository_test.go`, `repository_integration_test.go`, and `repository_postgres_test.go`.

Downstream enforcement:

- `backend/internal/task/service.go`, `service_test.go`, `governance_binding.go`, `governance_binding_test.go`
- `backend/internal/executionauth/types.go`, `validation.go`, `service.go`, `validation_test.go`, `service_test.go`
- `backend/internal/workflow/service.go`, `service_test.go`
- `backend/internal/workflowtask/runner.go`, `runner_test.go`

**Recommended Migration**
Use `0029`; do not rewrite applied migrations.

- Add nullable `task_risk_level` and `effective_risk_ceiling` columns with no fabricated defaults.
- Permit `NULL` for historical v4 and earlier records.
- Require both fields for `selector-v5`.
- Constrain values to `low|medium|high`.
- Add a SQL rank check requiring task risk to be at or below the effective ceiling.
- Continue requiring real operating digests for both v4 and v5.
- Do not backfill v4 rows. Historical decisions cannot truthfully acquire evidence they never recorded.
- Make rollback refuse while any v5 rows exist. Append-only v5 evidence must be exported/retained; it must not be silently downgraded.
- Keep [0003](../backend/migrations/pre/0003_framework_registry.up.sql):29 and [0005](../backend/migrations/pre/0005_framework_operating_contract.up.sql):1 unchanged as historical migration contracts.

**Digest Behavior**
- **Catalog digest:** changes to v2 because framework metadata changed.
- **Operating-contract digest:** must change for v5 and cover task risk plus effective ceiling.
- **Selection ID:** should be computed after metadata is attached and include selector version and risk contract.
- **Request hash:** deliberately unchanged; it already covers request risk, flags, and normalized request content.
- **Preference digest:** deliberately unchanged; risk ceilings are catalog-owned, not preference-owned.
- **Constitution digest:** deliberately unchanged.
- **v4 stored digests:** never recompute with v5 schemas.

**Frontend Files**
Must become version-aware:

- [framework-registry.model.interface.ts](../frontend/src/app/models/framework-registry.model.interface.ts):93
- [framework-registry.service.ts](../frontend/src/app/services/framework-registry.service.ts):329
- `framework-registry-recommendation.component.ts/html`
- `workflow.model.interface.ts`
- `workflow-engine.component.ts/html`

Fixtures requiring a new v5 case while deliberately retaining a v4 historical case:

- `frontend/src/app/services/framework-registry.service.spec.ts`
- `frontend/src/app/pages/framework-registry/framework-registry.component.spec.ts`
- `frontend/src/app/pages/framework-registry/framework-registry-recommendation.component.spec.ts`
- `frontend/src/app/pages/workflow-engine/workflow-engine.component.spec.ts`

For v4 decisions, the UI should show `Not recorded (pre-v5)`, never infer a historical ceiling from the current catalog.

**Documentation**
Update current-version claims in `docs/framework-registry.md`, `docs/operator-runbook.md`, and `docs/verification-honest-health-readiness.md`. Add the `0029` behavior and rollback restriction to `docs/migrations.md`. Preserve the existing `0005` sections explicitly as selector-v4 history.

No files were edited.

## 019fcacf-166b-79a2-b6c2-f9de57f793a3

- Date: 2026-08-04
- Agent: Hilbert
- Work kind: advisory
- Original report SHA-256: `75486950fabdc6832fe5def39bd2976e271b5b1b0b954a80215ffba7996fc2b8`
- Transcript: `08/04/rollout-2026-08-04T05-26-37-019fcacf-166b-79a2-b6c2-f9de57f793a3.jsonl`

**Audit Findings**

1. **The contract is split.** In the current uncommitted working tree, Go `SelectedFramework` includes `RiskCeiling`, and the selector populates it from the effective catalog view ([types.go](../backend/internal/frameworkregistry/types.go):208, [selector.go](../backend/internal/frameworkregistry/selector.go):342). Repository `HEAD` does not yet contain either change.

2. **New API and persistence records include it.** `/select` and `/selections` return `SelectionDecision` directly ([handler.go](../backend/internal/frameworkregistry/handler.go):134). New selections serialize the complete `SelectedFramework` into `selected_json` ([repository.go](../backend/internal/frameworkregistry/repository.go):839). No database column migration is required.

3. **Historical compatibility is unresolved.** Existing immutable JSON records lack `riskCeiling`. They decode to an empty Go string and are returned as `"riskCeiling": ""` ([repository.go](../backend/internal/frameworkregistry/repository.go):966). They cannot safely be backfilled because selection rows reject updates.

4. **Angular drops the field.** `ISelectedFramework` does not define `riskCeiling` ([framework-registry.model.interface.ts](../frontend/src/app/models/framework-registry.model.interface.ts):93), and the service manually reconstructs selected records without it ([framework-registry.service.ts](../frontend/src/app/services/framework-registry.service.ts):329). The duplicated workflow model also omits it ([workflow.model.interface.ts](../frontend/src/app/models/workflow.model.interface.ts):225).

5. **Neither UI explains the ceiling.** Recommendation cards show family and score; Advanced contracts show autonomy and authority but not risk ([framework-registry-recommendation.component.html](../frontend/src/app/pages/framework-registry/framework-registry-recommendation.component.html):102). History only shows date, domain, primary framework, authority, and review ([framework-registry.component.html](../frontend/src/app/pages/framework-registry/framework-registry.component.html):663).

6. **The algorithm version is stale.** Risk-ceiling filtering materially changes selection semantics, but the result still records `selector-v5` ([service.go](../backend/internal/frameworkregistry/service.go):19).

**Smallest Coherent Change Set**

- Retain the Go field and selector assignment.
- Validate new persisted ceilings as `low | medium | high`.
- Treat missing/blank ceilings as legacy history rather than joining against today’s catalog.
- Bump the selector contract to `selector-v6` and update its migration constraint.
- Add `riskCeiling?: 'low' | 'medium' | 'high'` to both Angular selected-framework interfaces.
- Make the Angular normalizer accept missing/blank legacy values while rejecting other invalid values.
- Show the ceiling on recommendation cards and Advanced framework contracts.
- Add a “Primary ceiling” history column, displaying “Not recorded (legacy)” when absent.
- Add selector snapshot, JSON round-trip, legacy decode, Angular normalization, and UI rendering tests.

A structured decision-level `effectiveRiskLevel` would improve machine-readable explainability, but it is not required for the smallest change because the current working-tree `selectionReason` already states the applied task risk.

Verification: the two focused Angular suites passed, `21 SUCCESS`. The focused Go package currently fails two unrelated catalog/taxonomy digest-drift tests, so the backend package is not green. No files were edited.

## 019fcad7-fbff-78f2-b63a-433b14a881a8

- Date: 2026-08-04
- Agent: Newton
- Work kind: implementation
- Original report SHA-256: `1a9bd5ba9b5c4cf148fcdd5f5593bb9582c404f139e775131c2d584c8d123683`
- Transcript: `08/04/rollout-2026-08-04T05-36-20-019fcad7-fbff-78f2-b63a-433b14a881a8.jsonl`

Implemented downstream framework risk enforcement.

Key changes:
- v5 governance now carries selector version, task risk, and effective ceiling.
- Authorization fails before policy evaluation when fields are missing/invalid, task risk exceeds ceiling, or execution risk falls outside the contract.
- Task-plan digests bind the new risk fields.
- Blank/v4 historical records remain compatible without inferred risk values.

Changed:
- [governance_binding.go](../backend/internal/task/governance_binding.go):18
- [governance_binding_test.go](../backend/internal/task/governance_binding_test.go):11
- [types.go](../backend/internal/executionauth/types.go):163
- [validation.go](../backend/internal/executionauth/validation.go):15
- [validation_test.go](../backend/internal/executionauth/validation_test.go):168
- [service_test.go](../backend/internal/executionauth/service_test.go):441

Verified through Docker:

```text
ok  internal/executionauth
ok  internal/task
ok  internal/automation
```

No framework registry, migration, frontend, or documentation files were edited.

## 019fcad8-6854-7943-bb04-12d0a3c5fac6

- Date: 2026-08-04
- Agent: Linnaeus
- Work kind: implementation
- Original report SHA-256: `89220eea8bc0524bc55bb869dcbf2696d2f09258dc278a30b2c58df6e90502fb`
- Transcript: `08/04/rollout-2026-08-04T05-36-48-019fcad8-6854-7943-bb04-12d0a3c5fac6.jsonl`

Implemented fail-closed selector-v5 propagation and enforcement.

- [workflow/service.go](../backend/internal/workflow/service.go):104: propagates workflow risk, persists explicit `TaskRiskLevel` and `EffectiveRiskCeiling`, validates v5 ceilings, and preserves blank v4 history.
- [workflowtask/runner.go](../backend/internal/workflowtask/runner.go):315: performs a side-effect-free preview before execution, rejects missing/downgraded v5 contracts, and verifies the executed contract against preflight.
- [workflow/service_test.go](../backend/internal/workflow/service_test.go):2256
- [workflowtask/runner_test.go](../backend/internal/workflowtask/runner_test.go):369

Verification passed:

```text
ok automation-hub-backend/internal/workflow
ok automation-hub-backend/internal/workflowtask
```

The current upstream `frameworkregistry.SelectionDecision` does not yet emit the two top-level v5 risk fields. Consequently, selector-v5 execution now correctly fails closed until that upstream contract is completed. No files outside the requested directories were edited.

## 019fcafd-42ad-7cd1-aa78-bd7b3c7a93a2

- Date: 2026-08-04
- Agent: Ohm
- Work kind: advisory
- Original report SHA-256: `8aa00e1fa8cf45b3df2ac909235812912ec979f2f8c6ae0c7484296665172127`
- Transcript: `08/04/rollout-2026-08-04T06-17-03-019fcafd-42ad-7cd1-aa78-bd7b3c7a93a2.jsonl`

**Audit Findings**
1. **Phase 2 is not wired to the selector resolver.** The main router installs `WithFrameworkSelectionResolver`, but `defaultExecutionAuthorizationService` returns a service without it. Selector-v5 executions through this path will fail closed.
[router/routes.go:402](../backend/internal/router/routes.go):402
[phase2/module.go:295](../backend/internal/phase2/module.go):295

2. **The in-progress resolver implementation currently breaks focused tests.** Existing selector-v5 service tests do not inject a resolver, so authorization stops with `framework.selection_unverified` before Constitution evaluation.
[executionauth/service_test.go:401](../backend/internal/executionauth/service_test.go):401
[executionauth/service_test.go:449](../backend/internal/executionauth/service_test.go):449

3. **The repository test has an ordering assumption.** It creates records with identical timestamps and expects the new selector-v5 record at `ListSelections()[0]`; UUID ordering can return the older record. The new point lookup should replace that assertion.
[frameworkregistry/repository_test.go:179](../backend/internal/frameworkregistry/repository_test.go):179

4. **The router adapter ignores cancellation and lacks a nil-result guard.** It accepts `context.Context` but discards it, while the GORM lookup does not call `WithContext(ctx)`. A custom repository returning `(nil, nil)` would panic in the adapter.
[router/execution_authorization_adapters.go:22](../backend/internal/router/execution_authorization_adapters.go):22
[frameworkregistry/repository.go:130](../backend/internal/frameworkregistry/repository.go):130

**Persistence Contract**
- Selection records contain owner, task-plan identity, selector version, risk contract, autonomy ceiling, approval requirement, and all reproducibility digests.
[models/framework_registry.go:25](../backend/internal/models/framework_registry.go):25
- `Select` generates a deterministic ID and persists the completed decision.
[frameworkregistry/service.go:269](../backend/internal/frameworkregistry/service.go):269
- The new lookup correctly queries by both `owner_identity` and UUID. Cross-owner access therefore returns not found.
[frameworkregistry/repository.go:130](../backend/internal/frameworkregistry/repository.go):130
- PostgreSQL rejects updates, deletes, and truncation of selection history.
[0003_framework_registry.up.sql:144](../backend/migrations/pre/0003_framework_registry.up.sql):144
- Selector-v5 risk fields and constraints already exist, so no new schema migration is required.
[0029_framework_selector_v5_digest.up.sql:1](../backend/migrations/pre/0029_framework_selector_v5_digest.up.sql):1

**Smallest Fail-Closed Design**
1. Retain `GetSelection(owner, id)` in the repository and `Selection(owner, id)` in the service. Do not scan recent history.
[frameworkregistry/service.go:587](../backend/internal/frameworkregistry/service.go):587

2. Keep the narrow `FrameworkSelectionResolver` and immutable snapshot in `executionauth`.
[executionauth/types.go:163](../backend/internal/executionauth/types.go):163

3. For every selector-v5 request:
   - Require a configured resolver.
   - Resolve by normalized request owner plus selection UUID.
   - Require an exact match for selection ID, task-plan ID, catalog/selector versions, task risk, risk ceiling, autonomy ceiling, approval requirement, and all four digests.
   - Persist `OutcomeDenied` with `framework.selection_unverified` on missing records, lookup failures, or any mismatch.
   - Run this before Constitution, mandate, approval, or agent policy evaluation.

The current verification location and comparison set are correct.
[executionauth/framework_selection.go:8](../backend/internal/executionauth/framework_selection.go):8
[executionauth/service.go:168](../backend/internal/executionauth/service.go):168

4. Add a nil-result guard and preferably propagate `context.Context` into `GetSelection` and GORM `WithContext`.

5. Wire the same resolver into both router and Phase 2 composition. Because `executionauth` already imports `frameworkregistry` for the Constitution adapter, placing a reusable selection adapter beside that adapter creates no new import edge.
[executionauth/constitution_adapter.go:3](../backend/internal/executionauth/constitution_adapter.go):3

**Import-Cycle Risk**
Do not make `frameworkregistry.Service` return `executionauth.FrameworkSelectionSnapshot`: `executionauth` already imports `frameworkregistry`, so importing `executionauth` back into `frameworkregistry` would create a cycle. Keep the DTO/interface owned by `executionauth`, with an adapter in `executionauth`, router, Phase 2, or a neutral adapter package.

**Required Tests**
- Repository: exact lookup, wrong owner, unknown UUID, malformed UUID, deep-copy behavior.
- PostgreSQL: owner-scoped lookup plus existing immutable update/delete/truncate checks.
- Authorization: exact match succeeds; absent resolver fails closed; lookup error fails closed; every individual field mismatch fails closed before Constitution.
- Threat cases: claimed autonomy `10` versus stored `6`; claimed `requiresApproval=false` versus stored `true`; inflated risk ceiling; altered digest.
- Compatibility: selector-v4 and governance-free execution do not require the resolver.
- Receipt: successful resolution stores `OwnerScoped=true` and `Verified=true`.
- Composition: router and Phase 2 both install the resolver.
- Consumption: `AuthorizeAndConsume` cannot bypass the same verification.

Focused read-only test run currently fails in `frameworkregistry` and `executionauth` for the two incomplete test adaptations described above; `router` and `phase2` pass. No files were edited.

## 019fcafd-b602-75b3-9139-d3f92ae37853

- Date: 2026-08-04
- Agent: Pascal
- Work kind: advisory
- Original report SHA-256: `47af03d771429a833c57457d438bad0af4f61b169d5fbf810cc64b4ef6ad8b5f`
- Transcript: `08/04/rollout-2026-08-04T06-17-32-019fcafd-b602-75b3-9139-d3f92ae37853.jsonl`

**Audit Findings**
1. **High: preconditions are evaluated after tools run.** Framework requirements are copied into the validation plan at [service.go:2291](../backend/internal/task/service.go):2291, but `Run` calls `executeAllowedSteps` before `validatePlan` at [service.go:670](../backend/internal/task/service.go):670. The actual external launch occurs at [service.go:1612](../backend/internal/task/service.go):1612.
2. **High: high-stakes facts can be verified only after an external effect.** Source-grounding and claim verification run at [service.go:1747](../backend/internal/task/service.go):1747, after the automation has completed. This is unsafe for legal, health, financial, travel, emergency, and communication actions.
3. **High: requirements are untyped strings matched fuzzily.** Criteria pass through token-overlap matching at [validation.go:902](../backend/internal/task/validation.go):902. A runtime launch reference can therefore support loosely related requirements without a requirement-specific deterministic validator.
4. **Medium: authorization cannot enforce catalog evidence.** `GovernanceEvidence` carries framework metadata and generic references, but not requirement IDs, phases, or validated assertions at [types.go:163](../backend/internal/executionauth/types.go):163.
5. **Medium: framework attribution is flattened.** Selection aggregates every framework’s requirements into one deduplicated string list at [selector.go:339](../backend/internal/frameworkregistry/selector.go):339, making per-framework enforcement and exceptions difficult.

Existing independent controls do correctly cover owner identity, exact approval binding, autonomy/risk ceilings, emergency stop, idempotency, and atomic authorization-receipt consumption. They do not cover all catalog prerequisites.

**Phase Classification**

`P` = pre-execution prerequisite, `X` = evidence created atomically during execution, `O` = postcondition. Classification uses the earliest safe enforcement phase. Catalog anchors refer to [catalog.go](../backend/internal/frameworkregistry/catalog.go):48.

| Catalog entries | Classification |
|---|---|
| `human-sovereignty` L60 | P: active Constitution version; verified operator identity; applicable approval record |
| `whole-life-ontology` L70; `needs-wellbeing` L80; `capacity-state` L90 | P: all listed requirements |
| `goal-hierarchy` L100; `intake-triage` L110; `multi-criteria-prioritization` L120 | P: all listed requirements |
| `multi-agent-organization` L130; `agent-identity-capability` L140 | P: roster, reporting structure, current agent card, tool allowlist, runtime health and capability provenance |
| `delegation-accountability` L150 | P: delegation contract; X: acceptance or acknowledgement; O: deliverable evidence |
| `agent-communication` L160 | P: schema-valid envelope, sender identity; X: correlation and provenance identifiers |
| `multi-agent-coordination` L171 | P: coordination plan; X: individual outputs; O: synthesis provenance |
| `reasoning-methods` L181; `uncertainty-decision` L203 | P: all listed assumptions, evidence, alternatives, confidence, and sensitivity requirements |
| `cognitive-agent-architecture` L192 | P: fresh world-state snapshot, goal and intention ledger; O: action feedback |
| `formal-planning` L213; `workflow-modeling` L223 | P: goals, constraints, dependencies, state contract, compensation path, approval nodes |
| `reliable-execution` L234 | P: idempotency key; X: action receipt; O: postcondition verification |
| `autonomy-levels` L244; `approval-control` L254 | P: all listed authority, risk, proposed-action, approver, scope, and expiry evidence |
| `memory-architecture` L264 | P before memory use or mutation: source reference, confidence, memory type, retention state |
| `personal-knowledge-management` L274 | P: original source link, classification reason; X: non-destructive change log |
| `retrieval-context` L284 | P: retrieval query, rank factors, source URI and freshness |
| `truth-evidence` L294 | P before consequential action: claim-to-source links, source authority/freshness, deterministic checks; recheck as O for final answers |
| `ingestion-synchronization` L304 | P: permission grant, sync cursor; X: raw item identity, extraction provenance |
| `ambient-perception` L314 | P: authorized source event, freshness; X: proposal provenance |
| `human-ai-interaction` L324; `privacy-protection` L334 | P: all decision, ownership, recovery, purpose, permission, classification, and processing-location requirements |
| `security-zero-trust` L344; `agent-threat-modeling` L354; `safety-engineering` L364 | P: all listed requirements; artifact provenance must be preflight evidence for existing executables and X for newly created artifacts |
| `ai-governance` L374 | P: system purpose, risk classification, responsible owner; X: decision record |
| `model-intelligence` L384 | P: provider health, capability profile, price/quota; O: validation result |
| `evaluation` L394 | P: dataset or criteria, known limitations; O: reproducible result |
| `observability` L404 | X: correlation IDs, source timestamps, redaction state |
| `reliability-resilience` L414 | P: health probes; X: recovery receipt; O: post-recovery verification |
| `controlled-learning` L424 | O: verified outcome, before-and-after behavior, owner correction or approval |
| `productivity-attention` L434 | P: current commitments, calendar availability, declared priority |
| `habit-behavior-change` L444 | P: operator-chosen behavior; O: observed or self-reported outcome |
| `health-personal-care` L454 | P: official care instruction/operator record and fresh appointment information |
| `financial-management` L464 | P before financial effect: source transaction/invoice, deterministic calculation, currency and period |
| `home-garden-assets` L474 | P: asset/property record, quote/maintenance source; O: before-and-after evidence |
| `work-service-delivery` L484 | P: agreed scope, acceptance criteria; O: deliverable evidence |
| `entrepreneurship-venture` L494 | P: customer/market evidence, experiment design, decision threshold |
| `legal-government-case` L504 | P before legal action: primary sources, dated timeline, claim map, deadline provenance |
| `communication` L514 | P: recipient and purpose, factual support, approval for consequential send |
| `relationships-care` L524 | P: operator-stated context and explicit uncertainty |
| `learning-competence` L534 | P: learning objective, assessment criteria; O: practice results |
| `travel-mobility` L544; `emergency-continuity` L554 | P: all current schedule, route, cost, recovery, contact, and backup evidence |
| `agent-development-adapters` L564 | P before activation: license/maintenance review, capability test, sandbox/policy contract |
| `durable-workflow-platforms` L576 | P: measured limitation, migration/rollback plan, operational cost |
| `memory-knowledge-implementations` L588 | P before adoption: classification, PostgreSQL benchmark, export/deletion behavior |
| `policy-security-implementations` L600 | P: threat model, license/maintenance status, migration/recovery plan |
| `evaluation-observability-implementations` L612 | P: coverage gap, privacy review, reproducible evaluation |
| Selector-added requirement [selector.go:373](../backend/internal/frameworkregistry/selector.go):373 | P: active Constitution and applicable authority decision |

**Most Unsafe Cases**

- A legal email, payment, health action, booking, public message, or emergency workflow can execute before primary sources, calculations, recipient verification, current schedules, or recovery information pass evidence validation.
- A runtime or agent can be invoked before its agent card, tool allowlist, runtime health, threat model, sandbox contract, or rollback plan is proven.
- Privacy requirements such as permission, data classification, and processing location do not participate in `AllowedNow`.
- `action receipt` and `postcondition verification` can both match the same launch evidence at [validation.go:817](../backend/internal/task/validation.go):817, despite representing different phases and facts.
- Validation failure can occur after an irreversible effect. The existing retry logic does reuse a completed tool result rather than blindly repeating it, which is a useful safeguard at [service.go:1564](../backend/internal/task/service.go):1564.

**Minimal Typed Design**
```go
type EvidencePhase string
const (
    PhasePreAuthorization EvidencePhase = "pre_authorization"
    PhaseExecution        EvidencePhase = "execution"
    PhasePostcondition    EvidencePhase = "postcondition"
)

type EvidenceRequirement struct {
    ID           string
    FrameworkID  string
    Label        string
    Phase        EvidencePhase
    Validator    ValidatorKind
    AppliesTo    []EffectClass
    SourceKinds  []SourceKind
    MaxAge       time.Duration
    Required     bool
}

type EvidenceAssertion struct {
    RequirementID string
    SourceRef      string
    Digest         string
    ObservedAt     time.Time
    Validator      ValidatorKind
    Status         EvidenceStatus
}
```

Required flow: compile typed requirements while preserving framework ownership; validate all `P` assertions before `AllowedNow`; bind their digest into execution authorization and independently resolve the immutable selection; emit `X` assertions atomically with receipt consumption; validate `O` assertions after execution; never retry a completed effect solely because a postcondition failed.

No files were edited. Focused Go tests could not run because Go is not available on the host `PATH`; this audit is based on direct source tracing.

## 019fcafe-226a-7030-ab25-98cf1d38d3c6

- Date: 2026-08-04
- Agent: Euclid
- Work kind: advisory
- Original report SHA-256: `22242cc5d5e6d9061efbabee4c0cfa6ba05be85ff9aebb95b2db7f675b42d9a0`
- Transcript: `08/04/rollout-2026-08-04T06-18-00-019fcafe-226a-7030-ab25-98cf1d38d3c6.jsonl`

**Audit Findings**

1. **P0: selector-v5 provenance can be forged.** Authorization copies caller-provided governance into the receipt without resolving `FrameworkSelectionID` against its owner-scoped registry record. A nonexistent or foreign selection can therefore carry invented digests, a higher autonomy ceiling, or `requiresApproval=false`. The existing test even authorizes the fake ID `selection-1`. See [service.go](../backend/internal/executionauth/service.go):123, [validation.go](../backend/internal/executionauth/validation.go):303, and [service_test.go](../backend/internal/executionauth/service_test.go):401.

2. **P0: no exact selection resolver exists.** The framework repository exposes only bounded history listing, not `GetSelection(owner, id)`. Scanning the newest 100 records would be unsafe and incomplete. See [frameworkregistry/service.go](../backend/internal/frameworkregistry/service.go):22 and [repository.go](../backend/internal/frameworkregistry/repository.go):130.

3. **P1: authorization recheck ignores framework provenance.** `AuthorizeAndConsume` rechecks stop, system workload, Constitution, agent, mandate, and approval, but not the selector-v5 record. See [service.go](../backend/internal/executionauth/service.go):563.

4. **P1: legacy selector-v4 remains executable, not merely readable.** Tests prove decoding compatibility, but no test defines whether a new side effect may rely on v4 governance. Current authorization silently skips v5 risk, maximum-autonomy, and approval checks. See [validation.go](../backend/internal/executionauth/validation.go):261 and [workflow/service_test.go](../backend/internal/workflow/service_test.go):2316.

5. **P1: many governance rejections cannot produce durable receipts.** Normalization returns before receipt construction. Structurally valid provenance mismatches should be resolved after receipt initialization and persisted as denials; malformed owner/idempotency requests can remain non-durable. See [service.go](../backend/internal/executionauth/service.go):75.

6. **P1: memory receipts are not deeply immutable.** `cloneReceipt` does not clone selector-v5 pointer fields or `Governance.EvidenceReferences`. Mutating a returned receipt can modify stored evidence without changing `DecisionDigest`. See [repository.go](../backend/internal/executionauth/repository.go):169.

7. **P2: selector-v5 repository test is ordering-sensitive.** One combined run failed because it assumes the new v5 record is `selections[0]` when two records have equal timestamps and random UUID ordering. The isolated test passed 100 repetitions, confirming a flaky suite interaction/order assumption rather than reliable identification by ID.

**Prioritized Test Matrix**

| Priority | Test scenario and expected result | Exact likely test file |
|---|---|---|
| P0 | Nonexistent valid UUID is denied with durable `framework.selection_not_found`; Constitution is not called | `executionauth/service_test.go` |
| P0 | Alice submitting Bob’s selection gets the same non-oracular denial; Bob’s data is never returned | `executionauth/service_test.go` |
| P0 | Exact owner/selection snapshot authorizes and records server-resolved values | `executionauth/service_test.go` |
| P0 | Table-test catalog version, selector version, task risk, effective ceiling, and task-plan mismatch | `executionauth/service_test.go` |
| P0 | Table-test catalog, preference, Constitution, and operating-contract digest mismatch | `executionauth/service_test.go` |
| P0 | Inflated maximum autonomy is denied even when requested autonomy fits the forged value | `executionauth/service_test.go` |
| P0 | `requiresApproval=true` persisted versus caller `false` cannot authorize | `executionauth/service_test.go` |
| P0 | Caller `true` versus persisted `false` is also rejected as provenance drift | `executionauth/service_test.go` |
| P1 | Missing/unavailable selector resolver fails closed for v5 but not ungoverned registered system work | `executionauth/service_test.go` |
| P1 | Resolver receives the normalized authenticated owner and exact UUID | `executionauth/service_test.go` |
| P1 | Selection changes/unavailability between authorization and consumption produces `ErrAuthorizationChanged` and no consumption | `executionauth/service_test.go` |
| P1 | Denied provenance receipt survives `Get`/`List`, has a stable digest and mismatch reason code | `executionauth/service_test.go` |
| P1 | Same denied request replays the same receipt; changed evidence under the same key returns `ErrIdempotencyConflict` | `executionauth/service_test.go` |
| P1 | Denied or approval-required receipt cannot be consumed | `executionauth/repository_test.go` and `repository_postgres_test.go` |
| P1 | Memory receipt mutations cannot alter max autonomy, approval, or evidence references | new `executionauth/repository_test.go` |
| P1 | `GetSelection(owner,id)` is exact and owner-scoped in memory | `frameworkregistry/repository_test.go` |
| P1 | `GetSelection(owner,id)` is exact and owner-scoped in PostgreSQL | `frameworkregistry/repository_postgres_test.go` |
| P1 | PostgreSQL receipt round-trip preserves full selector-v5 governance | `executionauth/repository_postgres_test.go` |
| P1 | Cross-owner framework reference is rejected at the database boundary if a typed FK is added | `migrations/unified_execution_authorization_postgres_test.go` |
| P1 | Production composition wires the framework resolver into execution authorization | new `router/execution_authorization_adapters_test.go` |
| P1 | End-to-end task plan uses the persisted selection, not a reconstructed caller snapshot | `task/automation_executor_test.go` |
| P1 | Workflow preview and execution both resolve the same owner-scoped selection | `workflowtask/runner_test.go` |
| P2 | v4 records remain listable, decodable, and inspectable | `frameworkregistry/repository_test.go`, `workflow/service_test.go` |
| P2 | New execution using v4 explicitly denies with “fresh selector-v5 planning required” | `executionauth/service_test.go` |
| P2 | Blank historical selector version follows the same documented legacy policy | `executionauth/validation_test.go` |
| P2 | v4 carrying any v5 risk/autonomy/approval fields remains rejected | `executionauth/validation_test.go` |
| P2 | Locate the created v5 record by ID instead of assuming index zero | `frameworkregistry/repository_test.go` |

**Verification**

Read-only audit; no files changed. Focused Docker test run:

- Passed: `executionauth`, `task`, `workflow`, `workflowtask`
- `frameworkregistry`: one ordering-sensitive failure in the combined run; isolated test passed with `-count=100`
- PostgreSQL integration paths were not exercised because the container did not receive `HAI_TEST_DATABASE_DSN`

## 019fcb10-d918-7ca1-a8c6-f0335604a2bb

- Date: 2026-08-04
- Agent: Sartre
- Work kind: implementation
- Original report SHA-256: `65593884d29c40d7cead493ff566aa8f34bf74346e0e64440d4c44993b7cf2e2`
- Transcript: `08/04/rollout-2026-08-04T06-38-26-019fcb10-d918-7ca1-a8c6-f0335604a2bb.jsonl`

Updated only:

- [framework-operating-contract-matrix.md](framework-operating-contract-matrix.md)
- [framework-registry.md](framework-registry.md)
- [requirements-traceability.md](requirements-traceability.md)
- [completion-matrix.md](completion-matrix.md)

Documented exact owner-scoped selector-v5 resolution, immutable field comparison, fail-closed authorization and consumption recheck, main router and Phase2 composition, and typed three-phase evidence contracts.

The documentation explicitly distinguishes enforced pre-authorization checks from execution/postcondition contracts that still lack independent validators for every requirement. No external-provider readiness is claimed. Existing dirty-tree edits were preserved.

Validation: `git diff --check` passed. No code or tests were run for this documentation-only change.

## 019fcb11-647e-7783-aaeb-607c8d2d1e1d

- Date: 2026-08-04
- Agent: Rawls
- Work kind: implementation
- Original report SHA-256: `5495bc44901b7cae2fbfee1825724061162bacb9a53065df745aab1886c8a03d`
- Transcript: `08/04/rollout-2026-08-04T06-39-02-019fcb11-647e-7783-aaeb-607c8d2d1e1d.jsonl`

Added [framework_evidence_test.go](../backend/internal/task/framework_evidence_test.go) with all eight requested cases. No production files or existing tests were edited.

Verification:

```text
go test -count=1 ./internal/task
ok automation-hub-backend/internal/task 3.317s
```

Tests ran using the repository’s pinned `golang:1.25.12` container because Go is not installed on the Windows host.

## 019fcb11-df92-73d1-ae2c-29c1454b6fcf

- Date: 2026-08-04
- Agent: Euler
- Work kind: advisory
- Original report SHA-256: `4fc96e81aceb2b03cc931d7adc33da723d5ba1ef9ebb8edfeaad4220fc7eac6a`
- Transcript: `08/04/rollout-2026-08-04T06-39-34-019fcb11-df92-73d1-ae2c-29c1454b6fcf.jsonl`

**Audit Result**
The smallest honest implementation depends on the claim being made:

- **Traceability only:** bind a canonical preflight digest into `GovernanceEvidence`, the task governance digest, authorization request digest, receipt, and evidence reference.
- **Independent authorization enforcement:** the current completion-plan storage cannot support this at authorization time. A preflight snapshot must first be persisted before execution and resolved through an injected interface.

A digest carried only by the caller is **bound evidence, not independently verified evidence**.

**Current State**
- `CompletionPlan` contains `FrameworkEvidencePreflight`: [service.go](../backend/internal/task/service.go):298.
- Preflight runs immediately before execution: [service.go](../backend/internal/task/service.go):672.
- Governance is then constructed and passed to the executor: [service.go](../backend/internal/task/service.go):1598.
- The completed plan is not appended to durable task state until after execution and validation: [service.go](../backend/internal/task/service.go):760.
- The earlier pursuit-attempt record contains status and summary fields, but no preflight assertions or digest: [service.go](../backend/internal/task/service.go):826.

Therefore, when `executionauth.Authorize()` runs, no durable preflight record exists to resolve.

The final completion plan is stored as a complete sanitized JSON snapshot with a SHA-256 payload digest: [state_repository.go](../backend/internal/task/state_repository.go):155. PostgreSQL makes these rows append-only: [0004_task_state_storage.up.sql](../backend/migrations/pre/0004_task_state_storage.up.sql):326.

However, the existing exact lookup is only by owner and task-plan ID and returns the nominal latest snapshot: [state_repository_postgres.go](../backend/internal/task/state_repository_postgres.go):71. This is unsuitable for preflight resolution because:

- Multiple payloads may exist for one task-plan ID.
- Their stored `created_at` comes from the unchanged plan creation time.
- The tie-breaker is a random UUID, not a revision or insertion sequence.
- The repository methods do not accept `context.Context`.

**Missing Binding**
`executionGovernanceEvidence()` currently hashes framework, risk, domain-pack, resource, and evidence-reference fields, but not preflight state: [governance_binding.go](../backend/internal/task/governance_binding.go):38. Preflight is also absent from generated evidence references: [governance_binding.go](../backend/internal/task/governance_binding.go):187.

`GovernanceEvidence` has no preflight field: [types.go](../backend/internal/executionauth/types.go):197.

Once added, it will automatically become cryptographically bound because:

- The authorization request digest hashes the complete normalized `Request`, including governance: [digest.go](../backend/internal/executionauth/digest.go):20.
- Governance is copied into receipt evidence: [service.go](../backend/internal/executionauth/service.go):137.
- The receipt decision digest covers receipt evidence: [digest.go](../backend/internal/executionauth/digest.go):33.
- Receipt evidence is durably serialized to PostgreSQL: [repository_postgres.go](../backend/internal/executionauth/repository_postgres.go):607.

**Recommended Shape**
For immediate provenance binding:

1. Add `FrameworkEvidencePreflightDigest` to `executionauth.GovernanceEvidence`.
2. Compute it from a versioned canonical projection containing owner, task-plan ID, framework-selection ID, status, counts, assertions, failures, and evaluation timestamp.
3. Require exact lowercase SHA-256 normalization.
4. Add `framework-evidence-preflight://<plan-id>#sha256:<digest>` to evidence references.
5. Include the digest explicitly in the `TaskPlanDigest` projection.
6. Describe it as `bound`, never `verified`, until a resolver confirms durable state.

Do not rely solely on `EvidenceReferences`: they are sorted and truncated to 32 entries.

For genuine independent verification, add a dedicated append-only preflight record persisted immediately after preflight passes and before calling the executor. Key it by:

```text
owner_identity
task_plan_id
framework_selection_id
preflight_digest
```

Then define `FrameworkEvidencePreflightResolver` in `executionauth` and implement its adapter outside `executionauth`, following the existing task-review resolver pattern: [task_review_resolver.go](../backend/internal/executionapproval/task_review_resolver.go):34.

This avoids an import cycle. `task` already imports `executionauth`; `executionauth` must not import `task`. The router already creates the task repository before constructing authorization, so it can inject the adapter: [routes.go](../backend/internal/router/routes.go):365.

Authorization should resolve and compare the immutable preflight during both `Authorize` and the pre-consumption recheck, matching the existing framework-selection pattern: [framework_selection.go](../backend/internal/executionauth/framework_selection.go):8, [service.go](../backend/internal/executionauth/service.go):587.

**Required Tests**
- Canonical preflight digest is deterministic and changes with any relevant assertion.
- Missing, failed, foreign-owner, mismatched-selection, and forged-digest records fail closed before tool execution.
- Preflight persistence failure produces zero executor calls.
- Receipt and request digests change when the preflight digest changes.
- Same idempotency key with changed preflight produces `ErrIdempotencyConflict`.
- Authorization recheck detects resolver changes before consumption.
- Multiple records cannot cause ambiguous “latest” resolution.
- Legacy selector-v4 compatibility remains explicit.
- PostgreSQL migration tests prove owner scope, append-only behavior, exact lookup, and tamper rejection.
- End-to-end test proves: evaluate → persist → resolve → authorize → consume → one external effect.

No files were changed and no tests were run. This audit reflects the current dirty local worktree; several relevant authorization and framework-evidence files are presently untracked, so it is not necessarily the state of remote `main`.

## 019fcb22-ea3d-7f03-b3f9-36bf250c431b

- Date: 2026-08-04
- Agent: Zeno
- Work kind: implementation
- Original report SHA-256: `16e6c09d51976720d90345cd8261716282597571c1fb4a08b39876607c8ed3fb`
- Transcript: `08/04/rollout-2026-08-04T06-58-10-019fcb22-ea3d-7f03-b3f9-36bf250c431b.jsonl`

Implemented independent selector-v5 framework evidence preflight verification within `backend/internal/executionauth/**`.

Key changes:
- Added owner-scoped snapshot, resolver, and verification evidence types.
- Added adapter for `frameworkevidence.Repository.Resolve(...)`.
- Required canonical preflight digests for selector-v5 governance.
- Enforced exact owner, task plan, selection, digest, and `passed` status matching.
- Added denial code `framework.evidence_preflight_unverified`.
- Re-resolves immediately before consumption and prevents consumption on drift.
- Added bounded digest and verification flags to receipt inspection.
- Added tests for unavailable/missing records, foreign owners, mismatches, failed status, forged digest, success, adapter behavior, inspection, and recheck drift.

Changed files are all under `backend/internal/executionauth/`.

Verification:
- `go test -count=1 ./internal/executionauth` passed.
- Race testing was unavailable because the Windows Go toolchain has CGO disabled.
- No router, task, phase2, or documentation files were edited.

## 019fcb23-6039-7411-aadf-6615d3785375

- Date: 2026-08-04
- Agent: Godel
- Work kind: implementation
- Original report SHA-256: `7475b66024800d4df57e485e1a4e2f953ec85df72a6b63da7db9dd8ac6992686`
- Transcript: `08/04/rollout-2026-08-04T06-58-41-019fcb23-6039-7411-aadf-6615d3785375.jsonl`

Implemented the neutral framework-evidence persistence layer.

**Package**
- [repository.go](../backend/internal/frameworkevidence/repository.go)
- [repository_postgres.go](../backend/internal/frameworkevidence/repository_postgres.go)
- [repository_test.go](../backend/internal/frameworkevidence/repository_test.go)
- [repository_postgres_test.go](../backend/internal/frameworkevidence/repository_postgres_test.go)

The API exposes:

```go
Store(context.Context, Record) error
Resolve(context.Context, ownerIdentity, taskPlanID,
    frameworkSelectionID, preflightDigest string) (Record, error)
```

It includes exact-byte/deep-copy payload handling, strict lowercase SHA256 validation, owner-scoped resolution, `passed`-only records, deterministic idempotent replay, conflicting-replay rejection, memory/GORM repositories, and `DefaultRepository()` via `infra.GetDefaultDB()`.

**Migration**
- [0030 up](../backend/migrations/pre/0030_framework_evidence_preflights.up.sql)
- [0030 down](../backend/migrations/pre/0030_framework_evidence_preflights.down.sql)
- [contract test](../backend/migrations/framework_evidence_preflights_contract_test.go)
- [PostgreSQL test](../backend/migrations/framework_evidence_preflights_postgres_test.go)

The table rejects updates, deletes, truncation, invalid JSON bytes, noncanonical identifiers, uppercase/invalid digests, and non-passing statuses.

Focused tests passed:

```text
ok automation-hub-backend/internal/frameworkevidence
ok automation-hub-backend/migrations
```

Live PostgreSQL cases compiled but skipped because `HAI_TEST_DATABASE_DSN` is unset. The existing migration-chain test still expects migration `0029` to be last; it was intentionally left untouched per scope.

## 019fcb37-3c9c-7cb1-88d0-d1b79f53cf5f

- Date: 2026-08-04
- Agent: Chandrasekhar
- Work kind: advisory
- Original report SHA-256: `017301c849285b2866328c7d95a71d9318e3c4be5e42b5f35589f37db1a943f2`
- Transcript: `08/04/rollout-2026-08-04T07-20-22-019fcb37-3c9c-7cb1-88d0-d1b79f53cf5f.jsonl`

**Audit Findings**
The working tree is slightly ahead of the stated premise: task preflight now re-resolves source evidence in [framework_evidence.go](../backend/internal/task/framework_evidence.go):804. That protects plan construction, but the controlled-execution boundary remains vulnerable.

1. **P1: source evidence is not reverified by `executionauth`.**
   Authorization resolves only the preflight owner/task/selection/digest/status. The adapter discards `AssertionsJSON`, including extraction IDs, URIs, snapshot digests, and freshness evidence: [framework_evidence_preflight_adapter.go](../backend/internal/executionauth/framework_evidence_preflight_adapter.go):40. A source can be revoked, archived, corrected, or become stale after task preflight and still pass authorization and consumption.

2. **P1: current freshness proof can refresh unrelated records.**
   [sourceevidence/repository.go](../backend/internal/sourceevidence/repository.go):185 prefers source-wide `LastSyncedAt`. Synchronizing one item therefore makes every extraction belonging to that source appear fresh. Freshness should use the specific raw item’s `FetchedAt`.

3. **P1: persisted assertions are not cryptographically checked.**
   The repository explicitly leaves preflight-digest semantics to callers: [repository.go](../backend/internal/frameworkevidence/repository.go):30. Before execution authorization trusts structured source claims parsed from that record, the neutral package must recompute the canonical digest and reject mismatches.

4. **P2: the current resolver is not one owner-scoped snapshot query.**
   It performs unscoped `FindExtraction` followed by unscoped `FindSource`: [sourceevidence/repository.go](../backend/internal/sourceevidence/repository.go):121. It does not independently validate the referenced raw item, raw URI, or raw content hash. The baseline schema also has no foreign keys joining these records.

5. **P2: ownerless sources remain searchable.**
   [service.go](../backend/internal/source/service.go):1313 treats an empty stored owner as visible. Controlled execution must require exact non-empty owner equality.

6. **P2: “source authority” cannot currently be proven.**
   The database can prove direct raw-item provenance, ownership, URI, hash, and freshness. It cannot prove that a source is authoritative or official. Requirements such as “source authority and freshness” should be split into independently enforceable validators rather than letting `fresh_source` overclaim authority.

**Narrow Resolver Design**
Use a neutral `internal/sourceevidence` package, analogous to `frameworkevidence`. It imports neither `task`, `source`, nor `executionauth`, avoiding the existing `source -> executionauth` dependency cycle.

```go
type Claim struct {
    RequirementID string
    Validator     string // primary_source, fresh_source, source_context
    ExtractionID string
    SourceID      string
    RawItemID     string
    SourceURI     string
    SnapshotDigest string
    MaxAgeSeconds int64
}

type Snapshot struct {
    OwnerIdentity    string
    ExtractionID     string
    SourceID         string
    RawItemID        string
    ExtractionURI    string
    RawItemURI       string
    ExtractionHash   string
    RawItemHash      string
    SnapshotDigest   string
    FetchedAt        time.Time
    ExtractionAt     time.Time
    Uncertain        bool
    Archived         bool
}

type Repository interface {
    Resolve(ctx context.Context, owner string, extractionID string) (Snapshot, error)
}
```

The PostgreSQL resolver should perform one strict join over `source_extractions`, `source_raw_items`, and `connected_sources`, requiring exact owner, enabled/active/non-revoked source, matching source/raw relationships, unarchived and non-uncertain extraction, and non-empty matching URI/hash.

Add an `executionauth.SourceEvidenceResolver` port and inject an adapter backed by this repository. Store typed `Claim` values inside each immutable framework assertion, include them in the canonical preflight digest, and expose them through `FrameworkEvidencePreflightSnapshot`.

**Execution Semantics**
- `primary_source`: require a direct raw-item-backed extraction with matching URI/hash and active owner-scoped source.
- `fresh_source`: apply the same checks plus `now - raw_item.fetched_at <= MaxAgeSeconds`.
- `source_context`: require durable extraction provenance; no freshness limit unless configured.
- Missing resolver, malformed claim, foreign owner, relation mismatch, URI/hash drift, uncertain/archived extraction, inactive source, stale/future timestamp, or repository error must deny with `source.evidence_unverified`.
- Re-resolve after authorization and immediately before receipt consumption.
- Do not store full URIs or source text in authorization inspection; expose only verified count and a bounded claims digest.
- Keep the current task-level resolver for early feedback, but never treat it as execution authority.

**Composition**
Create one shared `sourceevidence.Repository` in [routes.go](../backend/internal/router/routes.go):370, then inject it into both the task service and `executionauth.Service`. Wire the same resolver into the Phase 2 composition root. No new evidence table is required.

**Required Tests**
- Owner, extraction, source, raw-item, URI, hash, revision, and project mismatches.
- Disabled, revoked, ownerless, archived, uncertain, missing, stale, and future-dated records.
- Source-wide sync must not refresh an old raw item.
- Canonical preflight digest rejects altered assertion claims.
- Every required source assertion has at least one independently verified claim.
- Authorization succeeds with an exact snapshot.
- Mutation/revocation between authorization and consumption returns `ErrAuthorizationChanged`.
- Failed recheck creates no consumption and invokes no executor.
- PostgreSQL tests verify one owner-scoped join and cross-owner isolation.
- End-to-end: retrieve → preflight → persist → authorize → re-resolve → consume → exactly one effect.

No files were edited. Focused tests passed for `source`, `task`, `executionauth`, and `frameworkevidence` using the pinned Windows Go toolchain.

## 019fcb4d-7657-77c0-bf22-026ed4795c45

- Date: 2026-08-04
- Agent: Goodall
- Work kind: implementation
- Original report SHA-256: `40a5013deae3f199d3cb6cbd5b30224fe5777d260ca015b7dc9e0ea5053a9db6`
- Transcript: `08/04/rollout-2026-08-04T07-44-39-019fcb4d-7657-77c0-bf22-026ed4795c45.jsonl`

Added focused tests at:

- [repository_test.go](../backend/internal/sourceevidence/repository_test.go)

Coverage includes digest determinism/sensitivity, exact claim verification, owner/ID/digest/validator failures, stale/future evidence, max-age boundaries, and a gated PostgreSQL repository test.

Verification passed:

```text
go test -count=1 ./internal/sourceevidence
ok automation-hub-backend/internal/sourceevidence
```

PostgreSQL integration was skipped because `HAI_TEST_DATABASE_DSN` and destructive-test opt-in are unset. No production files were edited.

## 019fcb61-06b7-7be3-8afa-13d6e2bab55d

- Date: 2026-08-04
- Agent: Harvey
- Work kind: implementation
- Original report SHA-256: `3ab712e989132ce1bf85661d079629a5969afdb7c205d4ba81a2c9e7a2680247`
- Transcript: `08/04/rollout-2026-08-04T08-06-01-019fcb61-06b7-7be3-8afa-13d6e2bab55d.jsonl`

Added two fail-closed regression tests in [framework_evidence_preflight_test.go](../backend/internal/executionauth/framework_evidence_preflight_test.go):347:

- Verified source assertion without `sourceClaims` is denied with `source.evidence_unverified`.
- Malformed assertions JSON is denied with `source.evidence_unverified`.

Production code was not edited. Test result:

```text
ok automation-hub-backend/internal/executionauth 0.063s
```

The test file was already untracked in the existing worktree. No commit was created.

## 019fcb61-729a-7493-93df-0bc4ed5ac534

- Date: 2026-08-04
- Agent: Bernoulli
- Work kind: implementation
- Original report SHA-256: `23df7d519839f1c2bf0522c0a493d69e6feb615f3554ab3640b8101d94432ddc`
- Transcript: `08/04/rollout-2026-08-04T08-06-29-019fcb61-729a-7493-93df-0bc4ed5ac534.jsonl`

Added the focused regression test in:

- [repository_test.go](../backend/internal/frameworkevidence/repository_test.go)

It mutates stored `AssertionsJSON` without changing the digest and confirms `Resolve` returns `ErrConflict` due canonical digest mismatch.

Test result:

```text
ok automation-hub-backend/internal/frameworkevidence 0.009s
```

No commit created. Note: the test file is currently untracked in the worktree.

## 019fcb61-f7d6-7ad1-a959-efd82c6ed08f

- Date: 2026-08-04
- Agent: Laplace
- Work kind: implementation
- Original report SHA-256: `d2d80cc538ab4470c2445e8d70f9b34583edebcc78a1426c703d88f1a5ad379d`
- Transcript: `08/04/rollout-2026-08-04T08-07-03-019fcb61-f7d6-7ad1-a959-efd82c6ed08f.jsonl`

Updated only:

- [framework-registry.md](framework-registry.md)
- [compliance-boundaries.md](compliance-boundaries.md)
- [requirements-traceability.md](requirements-traceability.md)

Documented both preauthorization resolution points, raw-item `fetched_at` freshness, provenance/truth limitations, and inspection redaction. `git diff --check` passed. No commit created.

## 019fcb79-e995-7fb2-a0ff-d93f26b43645

- Date: 2026-08-04
- Agent: Pasteur
- Work kind: implementation
- Original report SHA-256: `4e6c04c59e3933334a6894076f7886572ef3f2e8c5836c55cc2e5008c4bd2097`
- Transcript: `08/04/rollout-2026-08-04T08-33-12-019fcb79-e995-7fb2-a0ff-d93f26b43645.jsonl`

Implemented deterministic semantic claim assessment, scoped strictly to `backend/internal/knowledgegraph`.

Changed paths:

- [claim_assessment.go](../backend/internal/knowledgegraph/claim_assessment.go)
- [claim_assessment_test.go](../backend/internal/knowledgegraph/claim_assessment_test.go)
- [claim_types.go](../backend/internal/knowledgegraph/claim_types.go)

Implemented:

- Owner/workspace isolation
- Supported, corroborated, conflicting, superseded, and needs-review statuses
- Independent provenance/content corroboration
- Effective-time and observed-time boundaries
- Direct and transitive supersession
- Deterministic evidence IDs
- Fail-closed truncation and corrupt-data handling
- No trust derived from free-form authority labels

Focused verification:

```text
go test -count=1 ./internal/knowledgegraph
ok automation-hub-backend/internal/knowledgegraph 0.023s
```

No commit was created.

## 019fcbd6-2ff9-7732-8679-5a391d788592

- Date: 2026-08-04
- Agent: Dalton
- Work kind: implementation
- Original report SHA-256: `f81cd2ca2f62ac022c46c5c51ceecbae78db4a86add1d24d0a54ba1fdada76d6`
- Transcript: `08/04/rollout-2026-08-04T10-13-59-019fcbd6-2ff9-7732-8679-5a391d788592.jsonl`

Updated only the requested documentation files:

- [completion-matrix.md](completion-matrix.md):67
- [requirements-traceability.md](requirements-traceability.md):48
- [framework-operating-contract-matrix.md](framework-operating-contract-matrix.md):52

The documentation now covers redacted generation telemetry, exact validation outcome updates, durable calibration refresh, conservative evidence-based routing, unvalidated direct API calls, and the real-credentials requirement for external-provider acceptance.

Existing worktree changes were preserved. `git diff --check` passed; no tests were run because this was documentation-only.

## 019fcc3b-11a2-7ff0-873e-4a7ff4791f38

- Date: 2026-08-04
- Agent: Banach
- Work kind: advisory
- Original report SHA-256: `0479349c38a06727762b62cc7b6e646d365a935a35ae77924c67d3ff71618fb6`
- Transcript: `08/04/rollout-2026-08-04T12-04-11-019fcc3b-11a2-7ff0-873e-4a7ff4791f38.jsonl`

**Audit Result**

The cleanest design is a **dedicated pursuit resource-event ledger**, borrowing the life-ledger’s immutability conventions but not aggregating life-ledger records directly. Existing life-ledger costs lack a precise `pursuitId`; `projectKey` is neither unique nor sufficient for enforcement.

No files were edited.

**Primary Touch Points**

- Add `PursuitResourceEvent` and `PursuitResourceUsage` near the existing limits in [pursuit.go](../backend/internal/models/pursuit.go):81.
  - Store money as integer EUR cents.
  - Store effort as integer seconds or minutes, never floating-point hours.
  - Include owner, pursuit ID, event kind, amount, source operation ID, evidence URI, idempotency key, request digest, record digest, observed time, and recorded time.
- Add a small `ResourceUsageRepository` beside the broad repository interface in [repository.go](../backend/internal/pursuit/repository.go):14.
  - `AppendResourceEvent`
  - `ListResourceEvents`
  - `GetResourceUsage`
  - `ReserveWithinLimits` for atomic check-and-reserve
- Inject it explicitly into the service at [service.go](../backend/internal/pursuit/service.go):764. Keep the main `Repository` interface from growing further.
- Add usage to `PursuitDetail` at [service.go](../backend/internal/pursuit/service.go):384, populate it during detail assembly at [service.go](../backend/internal/pursuit/service.go):1345, and add concise consumed/remaining/exceeded fields to `PursuitSummary`.
- Turn `pursuitNewWorkBlockerReason` at [service.go](../backend/internal/pursuit/service.go):6144 into a service-level gate accepting owner-scoped usage.
- Reuse that gate at the current creation paths:
  - Intake: [service.go](../backend/internal/pursuit/service.go):2971
  - Planning: [service.go](../backend/internal/pursuit/service.go):3177
  - Approved next action: [service.go](../backend/internal/pursuit/service.go):3373
- Also enforce it in the direct task boundary at [service.go](../backend/internal/pursuit/service.go):1263. This path currently bypasses stop, dependency, target, and resource blocker checks.

**Execution Boundary**

The task engine checks pursuit eligibility before `Plan`, `Preview`, and `Run` at [task/service.go](../backend/internal/task/service.go):620. That is useful but insufficient:

- Reserve resources atomically immediately before execution.
- Release unused reservations afterward.
- Recheck or reserve separately before the fallback retry at line 767.
- Do not merely calculate usage during page rendering.
- When the ledger is unavailable and a spend/effort limit exists, fail closed.

Workflow execution passes pursuit context into the task engine at [workflow/service.go](../backend/internal/workflow/service.go):2104. However, workflows linked to multiple pursuits deliberately return no pursuit ID at [workflow/service.go](../backend/internal/workflow/service.go):2217. Resource-limited ambiguous workflows must require an explicit allocation decision instead of executing uncharged.

**Routes And Security**

Add under the existing owner-authenticated pursuit group in [routes.go](../backend/internal/router/routes.go):1755:

- `GET /pursuits/:id/resources` with `PermRead`
- `GET /pursuits/:id/resource-events` with `PermRead`
- `POST /pursuits/:id/resource-events` with `PermWrite`
- Reservation/release operations should remain internal service APIs, not general browser endpoints.

Handler requirements:

- Use the verified session owner and actor; never accept them from JSON.
- Preserve the ownerless-record rule: legacy records remain readable but not mutable, as enforced at [service.go](../backend/internal/pursuit/service.go):1066.
- Use bounded request bodies and `DisallowUnknownFields`, following [lifeledger/handler.go](../backend/internal/lifeledger/handler.go):175.
- Validate evidence ownership and source-operation ownership.
- Never let a pursuit ceiling override the global paid-budget or approval policy.

**Migration Convention**

Add `pre/0034_pursuit_resource_usage_ledger.{up,down}.sql`.

Follow [0026_life_commitment_cost_ledgers.up.sql](../backend/migrations/pre/0026_life_commitment_cost_ledgers.up.sql):39:

- Owner-scoped idempotency uniqueness.
- Database constraints for event kinds, positive integer quantities, UUIDs, and digests.
- Payload-to-column consistency checks.
- `BEFORE UPDATE OR DELETE` and `BEFORE TRUNCATE` rejection triggers.
- Fail-closed down migration when records exist.
- Owner/pursuit/time and source-operation indexes.

Do not register this table with Gorm `AutoMigrate` in [database.go](../backend/internal/infra/database.go):82; explicit SQL should remain authoritative for an immutable ledger.

Update the migration tail expectation in [migration_chain_contract_test.go](../backend/migrations/migration_chain_contract_test.go):90.

**Tests To Add**

- Service limit tests beside [service_test.go](../backend/internal/pursuit/service_test.go):234.
- Extend `fakeRepo` or add a dedicated fake resource repository at [service_test.go](../backend/internal/pursuit/service_test.go):4763.
- Handler tests for cross-owner reads/writes, owner spoofing, duplicate requests, and ownerless legacy mutation denial in [handler_test.go](../backend/internal/pursuit/handler_test.go):121.
- Task tests proving `Plan`, `Preview`, initial execution, and retry all stop at the ceiling.
- Workflow tests for uniquely linked and ambiguously linked pursuits near [workflow/service_test.go](../backend/internal/workflow/service_test.go):1395.
- Migration contract test plus PostgreSQL integration covering append-only triggers, constraints, idempotency, rollback refusal, and concurrent reservation enforcement.

**Accounting Risks**

1. Never sum both `incurred` and `paid`; they may represent the same expense.
2. Never infer pursuit spending solely from `projectKey`.
3. Never sum task duration and runtime duration when they represent the same execution.
4. Retries need distinct source operation IDs but one idempotent record per actual attempt.
5. Shared workflows cannot be fully charged to every linked pursuit.
6. Refunds/releases must reference an existing event and cannot make totals negative.
7. Existing detail queries return only 20 task/runtime attempts; they cannot be used for totals.
8. Life-ledger listing is capped at 200 records, so it cannot provide complete enforcement totals.
9. Non-EUR costs require a verified conversion record or must remain excluded/unknown.
10. A read-then-write limit check is race-prone. The check and reservation must occur in one PostgreSQL transaction with row/advisory locking.

## 019fcc3b-a848-7771-b954-4c467a897e85

- Date: 2026-08-04
- Agent: Herschel
- Work kind: advisory
- Original report SHA-256: `4504b6995d799e53c1142d9d915a52b1270255f5609e5b07218f067b3e4f7aaf`
- Transcript: `08/04/rollout-2026-08-04T12-04-49-019fcc3b-a848-7771-b954-4c467a897e85.jsonl`

**Audit Result**

The cleanest design is: server-computed resource usage in the existing pursuit detail for Basic view, with immutable events fetched only when the Advanced ledger opens. Do not calculate spend or effort from events in Angular.

**Model Contract**

- Add `IPursuitResourceUsage`, `IPursuitResourceDimension`, `IPursuitResourceEvent`, and `IPursuitResourceEventList` near [pursuit.model.interface.ts](../frontend/src/app/models/pursuit.model.interface.ts):52.
- Add `resourceUsage` to `IPursuitDetail` at [line 408](../frontend/src/app/models/pursuit.model.interface.ts):408.
- Keep states explicit: `not_configured`, `within_limit`, `near_limit`, `exceeded`, `unavailable`. Never represent unavailable data as zero.
- Use integer minor units for money. Angular should display backend-computed used, remaining, limit, and exceeded values rather than recomputing them.
- Event fields should include kind, amount/unit, evidence, verification status, occurred/recorded timestamps, idempotency key, and record digest.

**Service**

- Extend [pursuit.service.ts](../frontend/src/app/services/pursuit.service.ts):61 with a bounded `resourceEvents(id, limit)` GET.
- Normalize `resourceUsage` and nullable event arrays at [line 171](../frontend/src/app/services/pursuit.service.ts):171.
- Preserve `unavailable` and absent timestamps. Do not default financial or effort fields to `0`.
- Keep events outside the normal pursuit detail payload to avoid loading an unbounded ledger on every selection.

**Component**

- Add ledger state beside `selected` at [pursuits.component.ts:38](../frontend/src/app/pages/pursuits/pursuits.component.ts):38: events, loading, error, loaded pursuit ID, selected event, and subscription.
- Reset/cancel ledger state whenever `loadPursuitDetail()` changes selection at [line 294](../frontend/src/app/pages/pursuits/pursuits.component.ts):294. Otherwise a slow response for pursuit A can populate pursuit B.
- Add `onResourceLedgerOpen()`, `loadResourceLedger(force)`, `inspectResourceEvent()`, and presentation-only status/format helpers.
- Unsubscribe from the ledger request in `ngOnDestroy()`.
- Do not add event aggregation logic to the component.

**Template**

- Place a concise two-dimension resource strip directly after the outcome contract at [pursuits.component.html:396](../frontend/src/app/pages/pursuits/pursuits.component.html):396, before the general metrics at line 429.
- Basic view should show only:
  - `Effort: 4.5h / 12h`
  - `Spend: EUR 45 / EUR 100`
  - Visible state text such as `within limit`, `near limit`, `exceeded`, or `usage unavailable`
  - Last recorded timestamp
- Add a `hai-progressive-section` before the current linked-record blocks at [line 933](../frontend/src/app/pages/pursuits/pursuits.component.html):933:
  - `moduleId="pursuits"`
  - `sectionId="resource-ledger"`
  - `(openChange)="onResourceLedgerOpen($event)"`
- Use a responsive event list, not a wide table. Open evidence, hashes, and immutable metadata in an `nz-drawer`.
- Show loading with `role="status"`, failures with `role="alert"`, and an explicit Retry button.

**Shared Patterns**

- Import `ControlRoomModule`, `NzDrawerModule`, and `NzSpinModule` in [pursuits.module.ts](../frontend/src/app/pages/pursuits/pursuits.module.ts):23.
- Register `resource-ledger` in the Pursuits `advancedSectionIds` at [module-registry.ts:21](../frontend/src/app/control-room/module-registry.ts):21.
- Reuse the lazy ledger pattern in [governance-control.component.ts:479](../frontend/src/app/pages/governance-control/governance-control.component.ts):479 and its immutable record presentation at [governance-control.component.html:626](../frontend/src/app/pages/governance-control/governance-control.component.html):626.

**Risks**

- Restored-open progressive sections do not emit `openChange`; [progressive-section.component.ts:19](../frontend/src/app/control-room/progressive-section.component.ts):19 must trigger lazy loading for a persisted open section.
- Add `aria-controls` and a stable content ID to [progressive-section.component.html:2](../frontend/src/app/control-room/progressive-section.component.html):2.
- Do not copy the pursuit page’s remaining hardcoded light colors around [pursuits.component.scss:414](../frontend/src/app/pages/pursuits/pursuits.component.scss):414; use shared tokens.
- Collapse the resource strip to one column below 700px using the existing responsive boundary at [line 1017](../frontend/src/app/pages/pursuits/pursuits.component.scss):1017.
- Important context must remain visible; do not place resource state only in `title` tooltips.

**Tests**

- Expand [pursuit.service.spec.ts](../frontend/src/app/services/pursuit.service.spec.ts):3 for null normalization, unknown state preservation, event arrays, endpoint, and limit parameter.
- Expand [pursuits.component.spec.ts](../frontend/src/app/pages/pursuits/pursuits.component.spec.ts):131 for lazy loading, deduplication, retry, stale-response protection, and status labels.
- Extend [acceptance.spec.ts:81](../frontend/e2e/tests/acceptance.spec.ts):81 to verify Basic summary visibility, Advanced disclosure persistence, empty ledger state, and an immutable event inspection path.

No files were changed and no tests were run for this audit.

## 019fcc60-29f1-74a3-b703-f5b88ed0f017

- Date: 2026-08-04
- Agent: Kierkegaard
- Work kind: advisory
- Original report SHA-256: `e849020ccd360258f6676d48fe8bce8e5381775259e8d7f79f7b48546eb12bb2`
- Transcript: `08/04/rollout-2026-08-04T12-44-42-019fcc60-29f1-74a3-b703-f5b88ed0f017.jsonl`

**Audit Result**
No files were edited. The current pursuit resource gate is fail-closed but **not atomic**: it reads recorded usage before planning, then execution occurs later without reserving capacity. Concurrent runs can therefore pass independently and oversubscribe the same pursuit.

**Current Path**
1. `Plan`, `Preview`, and `Run` invoke the pursuit guard before building a plan. [service.go](../backend/internal/task/service.go):619
2. The guard loads full pursuit detail and checks the recorded usage projection. [service.go](../backend/internal/pursuit/service.go):1266
3. Usage is calculated by separately summing immutable events. [resource_repository.go](../backend/internal/pursuit/resource_repository.go):60
4. `Run` then builds a new plan, persists a `running` attempt, runs evidence preflight, and executes. [service.go](../backend/internal/task/service.go):652
5. Controlled-runtime execution begins inside `executeAllowedSteps`. [service.go](../backend/internal/task/service.go):1615
6. LLM generation is another resource-consuming boundary. [service.go](../backend/internal/task/service.go):1732
7. Failed validation can immediately execute a second attempt. [service.go](../backend/internal/task/service.go):750

**Existing Estimates**
- The plan exposes estimates through `CompletionPlan.ResourceDecision`. [service.go](../backend/internal/task/service.go):308
- Effort lives in `ResourceDecision.Scheduled[].PlannedDurationMinutes`; there is no aggregate pursuit-attempt effort field. [types.go](../backend/internal/resourceplanner/types.go):126
- HAI deliberately uses pessimistic durations in conservative mode. [resource_planning.go](../backend/internal/task/resource_planning.go):155, [planner.go](../backend/internal/resourceplanner/planner.go):283
- Spend lives in `ResourceDecision.Budget.Estimated.CostMicros`; only the first planned step receives model/tool usage. [resource_planning.go](../backend/internal/task/resource_planning.go):85
- Actual/provider-derived LLM cost becomes available in `GenerationResult.EstimatedCostEUR` after token usage is known. The name is misleading because this value is recalculated from actual reported/estimated usage. [policy.go](../backend/internal/llm/policy.go):905
- Retry routing replaces `ModelDecision` but does not rebuild `ResourceDecision`, so the original spend estimate is stale for the fallback attempt.

**Required Touch Points**
- **Model:** add `PursuitResourceReservation` and lifecycle/audit records beside the existing limits and events. Use effort minutes and **EUR micros**, since cents would round cheap model reservations to zero. [pursuit.go](../backend/internal/models/pursuit.go):81
- **Migration:** add `0035_pursuit_resource_reservations`. The current `0034` advisory lock protects refund validation only; it does not enforce ceilings. [0034 migration](../backend/migrations/pre/0034_pursuit_resource_ledger.up.sql):36
- **Repository:** extend `pursuitResourceRepository` with atomic reserve, mark-executing, consume, release, and reconciliation methods. [resource_repository.go](../backend/internal/pursuit/resource_repository.go):11
- **Projection:** include reserved, executing, consumed, released, available, and reconciliation-needed quantities in `PursuitResourceUsage`. [resource_ledger.go](../backend/internal/pursuit/resource_ledger.go):43
- **Task contract:** extend `PursuitTaskGuard` or add a reservation-manager interface implemented by the already-injected pursuit service. [service.go](../backend/internal/task/service.go):271, [routes.go](../backend/internal/router/routes.go):574
- **Attempt audit:** link reservations by `task_plan_id` and attempt number. Current attempt persistence is separate and non-transactional. [repository.go](../backend/internal/pursuit/repository.go):321

**Reservation Lifecycle**
- Do not reserve during `Preview` or ordinary `Plan`.
- For `Run`, reserve after risk and durable evidence preflight pass, immediately before the first `executeAllowedSteps` call.
- Mark the reservation `executing` immediately before the first tool or model effect.
- Consume attempt 1 before validation can initiate attempt 2.
- Recalculate attempt 2 from the fallback `ModelDecision`; do not reuse the original cost estimate.
- Exclude controlled-runtime cost from attempt 2 when successful runtime evidence is reused. Existing reuse occurs here: [service.go](../backend/internal/task/service.go):1618.
- Release only when execution definitely never started.
- After a crash, timeout, or indeterminate external result, retain the hold as `executing/needs_review`; never automatically release it.
- Consume actual provider cost when known. Effort needs an explicit accounting policy because wall-clock runtime is not necessarily human effort.

**Concurrency And Idempotency Hazards**
- The current read-check-execute sequence is a TOCTOU race.
- Reservation SQL must acquire the same pursuit-scoped advisory lock as resource-event insertion and lock the pursuit row while reading current JSON limits.
- The atomic check must include recorded events plus all active/executing reservations.
- `plan.ID` is generated anew for every build, so it cannot alone prevent duplicate client execution. [service.go](../backend/internal/task/service.go):934
- The HTTP idempotency middleware is in-memory, expires after ten minutes, rejects rather than replays, and its key is not passed into the task request. [idempotency.go](../backend/internal/router/idempotency.go):13
- Workflow retries need a stable operation key derived from workflow ID and durable retry number. The workflow runner currently passes neither to task orchestration. [runner.go](../backend/internal/workflowtask/runner.go):318
- Reserve/consume/release races require compare-and-transition semantics. Identical replay must return the original receipt; changed amounts under the same key must conflict.

**Focused Tests**
- PostgreSQL: two concurrent reservations competing for one remaining ceiling; exactly one succeeds.
- PostgreSQL: concurrent recorded event and reservation cannot jointly exceed the ceiling.
- Repository: identical reserve replay, conflicting digest, consume/release race, cross-owner isolation.
- Task service: no reservation for Plan/Preview, risk-blocked work, or failed preflight.
- Task service: initial reserve/consume ordering; first attempt settles before retry reserve.
- Retry: fallback estimate is recalculated and successful runtime execution is not reserved twice.
- Failure: pre-effect failure releases; post-effect timeout remains held for reconciliation.
- Workflow: repeated lease execution uses the same durable operation key.
- Extend existing coverage at [service_test.go](../backend/internal/task/service_test.go):977, [pursuit service tests](../backend/internal/pursuit/service_test.go):362, and [PostgreSQL migration test](../backend/migrations/pursuit_resource_ledger_postgres_test.go):14.

## 019fcc60-afb2-7ca1-949c-31c54da46060

- Date: 2026-08-04
- Agent: Hegel
- Work kind: advisory
- Original report SHA-256: `3c156329837dce62513b6a1cc40e3ef920010c585e01a1d535812f8a8ae9b282`
- Transcript: `08/04/rollout-2026-08-04T12-45-16-019fcc60-afb2-7ca1-949c-31c54da46060.jsonl`

**Audit Findings**

1. **High: pursuit ceilings are not atomically reserved before execution.**
The task engine checks current pursuit eligibility, then plans and executes later. Two different workflows for the same pursuit can both pass the check before either records usage. The workflow lease only serializes one workflow, not all workflows belonging to a pursuit.
Evidence: [task/service.go:652](../backend/internal/task/service.go):652, [task/service.go:693](../backend/internal/task/service.go):693, [workflow/repository.go:247](../backend/internal/workflow/repository.go):247.

2. **High: ambiguous workflows execute without pursuit attribution.**
When multiple same-owner pursuits link to one workflow, `workflowTaskPursuitID` returns `("", nil)`. Execution continues, bypassing the pursuit lifecycle/resource guard because an empty pursuit ID is treated as unrestricted. The existing test explicitly codifies this behavior.
Evidence: [workflow/service.go:2217](../backend/internal/workflow/service.go):2217, [workflow/service.go:2104](../backend/internal/workflow/service.go):2104, [task/service.go:816](../backend/internal/task/service.go):816, [workflow/service_test.go:1416](../backend/internal/workflow/service_test.go):1416.

3. **High: the ledger records consumption after the fact but has no reservation state.**
Supported events are only `effort_recorded`, `spend_incurred`, and `spend_refund`. The database lock protects refund consistency, not ceiling allocation. There is no active-reservation amount included in usage totals.
Evidence: [0034_pursuit_resource_ledger.up.sql:16](../backend/migrations/pre/0034_pursuit_resource_ledger.up.sql):16, [0034_pursuit_resource_ledger.up.sql:36](../backend/migrations/pre/0034_pursuit_resource_ledger.up.sql):36, [resource_repository.go:60](../backend/internal/pursuit/resource_repository.go):60.

4. **High: workflow execution does not automatically settle effort or spend.**
Resource events are currently appended through the pursuit API. The task execution path does not append a consumption or release event. Workflow-owned runs are also deliberately excluded from the direct pursuit-attempt ledger. Consequently, successful execution can leave pursuit usage unchanged.
Evidence: [pursuit/handler.go:161](../backend/internal/pursuit/handler.go):161, [task/service.go:835](../backend/internal/task/service.go):835.

5. **Medium: zero links and ambiguous links collapse to the same empty identity.**
A genuinely standalone workflow, a workflow whose link is missing, a foreign-owner-only link, and a multiply linked workflow all reach execution without a pursuit ID. The system cannot distinguish intentional standalone execution from corrupted allocation.
Evidence: [workflow/service.go:2222](../backend/internal/workflow/service.go):2222, [workflow/repository.go:443](../backend/internal/workflow/repository.go):443.

6. **Medium: the task boundary does not verify the workflow-to-pursuit pair.**
It validates the pursuit lifecycle, but does not confirm that `WorkflowID` remains linked to that pursuit. Both values are propagated internally, but atomic reservation must revalidate the pair inside the reservation transaction.
Evidence: [task/service.go:808](../backend/internal/task/service.go):808, [task/state_repository.go:97](../backend/internal/task/state_repository.go):97.

7. **Medium: resource planning is advisory and global-budget oriented.**
It estimates conservative duration, model cost, tokens, and tool calls, but explicitly cannot execute or grant authority. It receives global paid-budget usage rather than the pursuit’s remaining effort/spend.
Evidence: [task/resource_planning.go:17](../backend/internal/task/resource_planning.go):17, [task/resource_planning.go:125](../backend/internal/task/resource_planning.go):125, [resourceplanner/planner.go:14](../backend/internal/resourceplanner/planner.go):14.

**Working Path**

Exactly one same-owner pursuit propagates correctly:

`WorkflowItem → TaskRunRequest → task.IntakeRequest → CompletionPlan → verification.AnswerRequest`

Evidence: [workflow/service.go:2122](../backend/internal/workflow/service.go):2122, [workflowtask/runner.go:318](../backend/internal/workflowtask/runner.go):318, [task/service.go:1082](../backend/internal/task/service.go):1082, [task/service.go:1804](../backend/internal/task/service.go):1804.

**Safest Minimal Rule**

Before automatic workflow execution:

- Require exactly one active, same-owner pursuit link.
- Zero or multiple matches transition the workflow to `needs_review`; do not invoke the task runner.
- Inside `task.Run`, after building the actual run plan but before LLM/tool execution, atomically reserve conservative effort and estimated maximum spend.
- The transaction must revalidate owner, workflow link, pursuit state, limits, recorded usage, and active reservations.
- Bind reservation idempotency to owner, pursuit, workflow, and actual task-plan ID.
- Settle the reservation into recorded usage or release it. Unknown external outcomes must remain reserved and go to review.

This strict rule may temporarily block legitimate standalone workflows. The proper follow-up is an explicit server-owned `standalone` resource scope rather than inferring standalone status from a missing link.

**Tests**

The targeted existing tests passed across `workflow`, `workflowtask`, `task`, and `pursuit`, including single-link propagation, current ambiguous behavior, lifecycle guarding, and recorded-ceiling enforcement.

Missing tests required before trusting reservations:

- Zero-link execution blocks before task-runner invocation.
- Multiple-link execution blocks instead of running unscoped.
- Foreign-owner-only links fail closed.
- Two concurrent workflows competing for one remaining reservation allow exactly one.
- Retry reuses the same reservation.
- Reservation settlement and release are idempotent.
- Expired worker lease does not silently release an unknown external outcome.
- Link removal/change between planning and reservation rejects execution.
- Database-level concurrency test proves recorded plus reserved usage cannot exceed the ceiling.

No files were edited.

## 019fcc6e-559f-70d3-80db-19d12b11438f

- Date: 2026-08-04
- Agent: Fermat
- Work kind: implementation
- Original report SHA-256: `61ffe70eba5c295ae6d8033035e33efcb0ba74d497d6c8148565323e23d4964a`
- Transcript: `08/04/rollout-2026-08-04T13-00-10-019fcc6e-559f-70d3-80db-19d12b11438f.jsonl`

Added only these new test files:

- [pursuit_resource_reservations_contract_test.go](../backend/migrations/pursuit_resource_reservations_contract_test.go)
- [pursuit_resource_reservations_postgres_test.go](../backend/migrations/pursuit_resource_reservations_postgres_test.go)

Verified:

- Migration schema and append-only protections
- Exactly one of two competing concurrent holds succeeds
- Recorded usage plus active holds enforce the ceiling
- Settlement releases reserved capacity
- Rollback refuses non-empty reservation tables
- Contract test passed
- Isolated PostgreSQL integration test passed
- `git diff --check` passed

No implementation files were edited.

## 019fcc6e-d67e-7f11-adca-a3b8948c1edf

- Date: 2026-08-04
- Agent: Meitner
- Work kind: implementation
- Original report SHA-256: `1605fefea425469ac742314f385564ccaaa17aab656bc8f257439a293d668a45`
- Transcript: `08/04/rollout-2026-08-04T13-00-43-019fcc6e-d67e-7f11-adca-a3b8948c1edf.jsonl`

Implemented the workflow ambiguity fix.

Changed:
- [service.go](../backend/internal/workflow/service.go):2233
- [service_test.go](../backend/internal/workflow/service_test.go):1416

Behavior:
- Multiple same-owner pursuit links now block automatic execution.
- Workflow enters blocked `needs_review` state with an actionable reason.
- Task runner is not invoked.
- Zero-link standalone workflows continue executing as before.
- Foreign-owner links are ignored; exactly one matching owner-scoped link is propagated.

Verification:
- Four focused pursuit-link tests passed.
- Complete `go test ./internal/workflow -count=1` passed.
- Scoped diff check passed.

Other pre-existing workflow changes were left untouched.

## 019fccf3-9fcc-70c3-bd56-c6dcfc5a07b1

- Date: 2026-08-04
- Agent: Mencius
- Work kind: implementation
- Original report SHA-256: `0ac1d78c35a728f9d9caa98daa748d1316e865d256e98810e6afd5fb24bc6c00`
- Transcript: `08/04/rollout-2026-08-04T15-25-46-019fccf3-9fcc-70c3-bd56-c6dcfc5a07b1.jsonl`

Updated documentation only for the advisory pursuit portfolio planner:

- [completion-matrix.md](completion-matrix.md)
- [requirements-traceability.md](requirements-traceability.md)
- [domain-model.md](domain-model.md)
- [operator-runbook.md](operator-runbook.md)
- [framework-operating-contract-matrix.md](framework-operating-contract-matrix.md)

Documented explicit estimates and capacity, pure 25-factor evaluation, deterministic dependency scheduling, ledger-based exclusions, approval visibility, the `advisory_only` boundary, and remaining execution/calendar gaps.

No code was touched. `git diff --check` passed; tests were not run because this was documentation-only.

## 019fccf4-55e9-7081-aee7-203895f117f2

- Date: 2026-08-04
- Agent: Russell
- Work kind: implementation
- Original report SHA-256: `acf7cb9dc4b761400f89d7e3863ff2865e5638faef28b517722e2440dd69f703`
- Transcript: `08/04/rollout-2026-08-04T15-26-32-019fccf4-55e9-7081-aee7-203895f117f2.jsonl`

Added focused route-permission coverage in [owner_decision_permissions_test.go](../backend/internal/router/owner_decision_permissions_test.go):51.

Verified:
- Unauthenticated requests return `401`, even with a forged `X-HAI-Role: owner`.
- Authenticated `viewer`, `operator`, and `owner` roles pass the read-permission boundary and reach handler validation.
- No frontend or documentation files were touched.

Commands run:

```powershell
D:\hai-go-pruned\go\bin\gofmt.exe -w internal/router/owner_decision_permissions_test.go

D:\hai-go-pruned\go\bin\go.exe test ./internal/router `
  -run '^TestPursuitPortfolioPlanRequiresAuthenticationAndReadPermission$' -count=1

D:\hai-go-pruned\go\bin\go.exe test ./internal/router `
  -run '^(TestWorkflowAndPursuitDecisionsRequireOwnerApproval|TestPursuitPortfolioPlanRequiresAuthenticationAndReadPermission)$' -count=1

git diff --check -- internal/router/owner_decision_permissions_test.go
```

Both test runs passed.

## 019fcd09-c8b2-7ff0-a975-e2ebe8465b0c

- Date: 2026-08-04
- Agent: Erdos
- Work kind: implementation
- Original report SHA-256: `ae9f85a604d442215a2201fab8e6b9b8879994d2e395af4ca741e180a2a35630`
- Transcript: `08/04/rollout-2026-08-04T15-49-58-019fcd09-c8b2-7ff0-a975-e2ebe8465b0c.jsonl`

Implemented the governed portfolio-allocation persistence slice.

**Implemented**
- Immutable allocation and allocation-item models.
- Owner-scoped `FindPortfolioAllocationForOwner`, returning `nil, nil, nil` when absent.
- Atomic `SavePortfolioAllocation` transaction covering allocation, items, reservations, and activities.
- Exact replay using stable request, decision, item, reservation, and record digests.
- Concurrent replay tolerates independently generated UUIDs.
- Fail-closed changed-digest handling.
- Database owner-binding, schedule, reservation, approval, uniqueness, and append-only constraints.
- Resource ceilings remain enforced by the existing reservation trigger.

**Changed Paths**
- [pursuit.go](../backend/internal/models/pursuit.go):206
- [portfolio_acceptance_repository.go](../backend/internal/pursuit/portfolio_acceptance_repository.go):25
- [0038 up migration](../backend/migrations/pre/0038_pursuit_portfolio_allocations.up.sql)
- [0038 down migration](../backend/migrations/pre/0038_pursuit_portfolio_allocations.down.sql)
- [migration contract test](../backend/internal/pursuit/portfolio_acceptance_repository_test.go):11
- [PostgreSQL repository tests](../backend/internal/pursuit/portfolio_acceptance_repository_postgres_test.go):23

**Verification**
- Pursuit package tests: passed.
- Real PostgreSQL rollback, replay, digest, owner-isolation, and immutability tests: passed.
- Concurrent exact-replay test repeated 10 times: passed.
- Full backend suite: every package passed except the existing static migration-tail test, which still expects `0037` to be last. Updating that existing out-of-scope test was intentionally avoided.

## 019fcd0b-21c8-7e82-9516-90de83d3f23e

- Date: 2026-08-04
- Agent: Kuhn
- Work kind: implementation
- Original report SHA-256: `9858da0245c8e664046a543aa27160be4c6bd99cc3acd20d344476b6c6cdd191`
- Transcript: `08/04/rollout-2026-08-04T15-51-26-019fcd0b-21c8-7e82-9516-90de83d3f23e.jsonl`

Implemented frontend-only durable portfolio allocation history.

**Changed**
- Added bounded `GET /api/v1/pursuits/portfolio-allocations?limit=N`: [pursuit.service.ts](../frontend/src/app/services/pursuit.service.ts):66
- History loads only when the portfolio planner opens.
- Added loading, retryable error, empty, and populated states.
- Records show allocation status, timestamp, plan ID, `allocation_only` authority, reservations, schedules, and approval flags.
- Unsafe records claiming different authority or execution capability reject the entire response.
- Current accepted/replayed allocations reconcile by allocation ID, preventing duplicates.
- History provides only inspection and refresh controls, with no approval or execution actions.
- UI implementation: [pursuits.component.html](../frontend/src/app/pages/pursuits/pursuits.component.html):1676
- State and validation: [pursuits.component.ts](../frontend/src/app/pages/pursuits/pursuits.component.ts):338

This slice changed five files: the service and its spec, plus the Pursuits component TypeScript, template, and spec. No model or SCSS change was required.

**Verification**
- Focused Angular tests: **24 passed**
- Production build: **passed**
- Scoped `git diff --check`: **passed**
- Existing stylesheet and initial-bundle budget warnings remain unchanged.
- The authenticated live GET endpoint was not exercised in this frontend-only slice.

## 019fcd42-1f89-7493-8d34-713620918c85

- Date: 2026-08-04
- Agent: Carver
- Work kind: implementation
- Original report SHA-256: `f0cbb8db1178eaf6f40a76c4271cc2eb6687890f9f68b11302a56820620a5b16`
- Transcript: `08/04/rollout-2026-08-04T16-51-30-019fcd42-1f89-7493-8d34-713620918c85.jsonl`

**Implemented**

Added the frontend-only immutable execution-proposal flow to Pursuits:

- Typed request/response models with `proposal_only` and `canExecute:false`.
- Exact POST endpoint and confirmation contract.
- Deliberate confirmation modal before submission.
- Fail-closed validation of authority, digests, timestamps, statuses, reasons, reservations, and allocation-item relationships.
- Read-only immutable proposal evidence display.
- Explicit messaging that proposals do not approve, queue, or execute work.
- No execution or approval buttons added.

**Changed Paths**

- [pursuit.model.interface.ts](../frontend/src/app/models/pursuit.model.interface.ts)
- [pursuit.service.ts](../frontend/src/app/services/pursuit.service.ts)
- [pursuit.service.spec.ts](../frontend/src/app/services/pursuit.service.spec.ts)
- [pursuits.component.ts](../frontend/src/app/pages/pursuits/pursuits.component.ts)
- [pursuits.component.html](../frontend/src/app/pages/pursuits/pursuits.component.html)
- [pursuits.component.spec.ts](../frontend/src/app/pages/pursuits/pursuits.component.spec.ts)
- [styles.scss](../frontend/src/styles.scss)

**Verification**

- Focused tests: **29/29 passed**
- Full frontend suite: **274/274 passed**
- Production Angular build: passed
- `git diff --check`: passed
- Rebuilt frontend container: healthy
- Browser-tested at [http://localhost/pursuits](http://localhost/pursuits):
  - Allocation history loaded.
  - Proposal action displayed.
  - Confirmation explicitly stated no approval, queueing, or execution.
  - Cancellation restored the enabled action.
  - No console errors or warnings.

The live POST was intentionally not submitted during browser QA, so no immutable proposal record was created. Success and unsafe-response handling were covered by focused tests. Backend and documentation files were not edited.

## 019fcd4e-01fd-72d2-9251-f6d834eb4e96

- Date: 2026-08-04
- Agent: Locke
- Work kind: implementation
- Original report SHA-256: `9f33b2d5b086882ea6c0b3adf832bb6b048dd85fdbb51bf5e4f2c192e747e5a8`
- Transcript: `08/04/rollout-2026-08-04T17-04-29-019fcd4e-01fd-72d2-9251-f6d834eb4e96.jsonl`

Updated documentation only for the governed execution-proposal stage:

- [domain-model.md](domain-model.md):140
- [operator-runbook.md](operator-runbook.md):400
- [requirements-traceability.md](requirements-traceability.md):37
- [completion-matrix.md](completion-matrix.md):64

Documented the route, exact confirmation, owner scope, immutable digest bindings, three statuses, replay/new-snapshot behavior, and strict `proposal_only`/`canExecute:false` boundary. It explicitly states that this stage cannot approve, consume approvals, enqueue, run tools, settle reservations, or execute.

`git diff --check` passed. No tests were run because this was documentation-only. Existing unrelated working-tree changes were preserved.

## 019fcd59-5270-7850-a0d5-798551291738

- Date: 2026-08-04
- Agent: Franklin
- Work kind: implementation
- Original report SHA-256: `672332bd10272b20daf021642e7a09907b73f9c78565110d26bc52034b2a1965`
- Transcript: `08/04/rollout-2026-08-04T17-16-51-019fcd59-5270-7850-a0d5-798551291738.jsonl`

Implemented the frontend-only portfolio settlement flow.

Key behavior:
- Settlement controls appear only after the linked workflow is completed and verified.
- Collects actual effort minutes and cost micros.
- Requires `SETTLE VERIFIED PORTFOLIO WORK`.
- Uses a deliberate confirmation modal.
- Calls the new `/settle-workflow` endpoint.
- Rejects mismatched or execution-capable responses.
- Displays settlement, replay, evidence, and updated resource usage.

**Changed Paths**
- [pursuit.model.interface.ts](../frontend/src/app/models/pursuit.model.interface.ts):1100
- [pursuit.service.ts](../frontend/src/app/services/pursuit.service.ts):135
- [pursuit.service.spec.ts](../frontend/src/app/services/pursuit.service.spec.ts):292
- [pursuits.component.ts](../frontend/src/app/pages/pursuits/pursuits.component.ts):986
- [pursuits.component.html](../frontend/src/app/pages/pursuits/pursuits.component.html):2095
- [pursuits.component.spec.ts](../frontend/src/app/pages/pursuits/pursuits.component.spec.ts):866
- [styles.scss](../frontend/src/styles.scss):2297

**Verification**
- Focused pursuit tests: `46 SUCCESS`
- Full frontend suite: `293 SUCCESS`
- Production Angular build: passed
- `git diff --check`: passed

Existing bundle-size warnings and the Angular CDK component-ID warning remain. No backend or documentation files were changed, and nothing was committed.

## 019fcd59-e912-7742-bd16-02e8a50c9ac8

- Date: 2026-08-04
- Agent: Socrates
- Work kind: advisory
- Original report SHA-256: `450fdb26ccdb6f2fbc1a2a17861bafebacfd331f92343a92e11cc96c615ecd05`
- Transcript: `08/04/rollout-2026-08-04T17-17-29-019fcd59-e912-7742-bd16-02e8a50c9ac8.jsonl`

**Audit Verdict**

Do not merge the proposed settlement command as-is. Owner, item, receipt, consumption, pursuit, and reservation checks are mostly sound, but durable completion proof and atomic settlement remain incomplete.

**Findings**

1. **P0: completion is checked using a mutable workflow row outside the settlement transaction.**
   [`SettlePortfolioWorkflowForOwner`](../backend/internal/pursuit/portfolio_workflow_settlement.go):50 reads `CurrentState`, `CompletedAt`, `LastTaskPlanID`, and `VerificationStatus`, then later opens a separate reservation transaction. Concurrent changes can invalidate that evidence before settlement.

   Worker completion also updates the workflow first and records transition, decision, and audit evidence afterward through helpers that discard persistence errors: [service.go:2220](../backend/internal/workflow/service.go):2220, [service.go:2985](../backend/internal/workflow/service.go):2985.

   **Required fix:** persist an immutable completion attestation atomically with workflow completion. Settlement must lock and revalidate that attestation, proposal item, workflow, and reservation in one database transaction.

2. **P1: `schema_validated` incorrectly qualifies for settlement.**
   [`portfolioSettlementAcceptsVerification`](../backend/internal/pursuit/portfolio_workflow_settlement.go):244 accepts it, while the established pursuit completion policy deliberately excludes it in [`acceptedCompletionStatus`](../backend/internal/pursuit/service.go):5165. Schema validity proves output shape, not completed work.

3. **P1: the settlement record cannot prove which item, receipt, and workflow authorized it.**
   [`PursuitResourceReservationSettlement`](../backend/internal/models/pursuit.go):133 stores reservation, owner, pursuit, usage, and a synthetic evidence URI. Its [digest](../backend/internal/pursuit/resource_reservation.go):300 omits proposal item, decision, receipt consumption, workflow, and completion-proof digests.

   Add an append-only `portfolio_reservation_settlement_proofs` record, transactionally bound to the generic settlement.

4. **P1: duplicate workflow lookup weakens receipt uniqueness.**
   [`loadPortfolioSettlementWorkflow`](../backend/internal/pursuit/portfolio_workflow_settlement.go):207 checks only the requested workflow. It does not reject one receipt linked to multiple workflows. Reuse [`loadPortfolioWorkflowEffect`](../backend/internal/pursuit/portfolio_workflow_authorization.go):582, which explicitly detects conflicting workflows.

**Methods To Reuse**

- Owner/item/allocation/reservation binding:
  - `LoadPortfolioExecutionProposalDecisionSnapshot`
  - `LoadPortfolioWorkflowEffectApprovalSnapshot`
  - `validatePortfolioExecutionDecisionSource`
  - `validatePortfolioExecutionDecisionEvidence`
- Receipt binding:
  - `buildPortfolioWorkflowEffect`
  - `buildPortfolioWorkflowAuthorizationRequest`
  - `portfolioWorkflowReceiptMatches`
  - `portfolioWorkflowConsumptionMatches`
  - `executionauth.Get` and `GetConsumption`
- Workflow retrieval:
  - `workflowOwnerScopedRecordReader.GetForOwner`
  - `loadPortfolioWorkflowEffect`
  - `validatePortfolioWorkflowEffectRecord`
- Reservation accounting:
  - `FindResourceReservationByID`
  - `settlementResourceEvents`
  - `SettleResourceReservation`, extended through a dedicated atomic portfolio-proof transaction.

**Required Invariants**

- `ownerIdentity == actor`; every loaded record has that owner.
- Item, proposal, allocation item, pursuit, and reservation IDs and digests match exactly.
- Receipt resource ID equals the immutable proposal item ID.
- Receipt action, effect digest, approval decision, owner and consumer target match.
- Receipt consumption already exists and is not consumed again.
- Exactly one receipt-bound workflow exists; its requested ID, owner, pursuit link, source ID and URI match.
- Completion has an immutable attestation bound to workflow ID, receipt ID, task-plan ID, completion timestamp, verification status and evidence digest.
- Reservation ID equals both proposal-item and allocation-item reservation IDs.
- Disposition is derived server-side as `consumed`.
- Settlement performs accounting only: `authority=verified_accounting_only`, `canExecute=false`.

**Validation Statuses**

- Accept: `verified`, `test_passed`.
- Conditionally accept `source_supported` only when the source proves completion of the actual effect.
- Conditionally accept `human_approved` only with durable operator evidence, passed completion gate, and `RecoveryCompletionConfirmed`.
- Reject: `schema_validated`, `unverified`, `uncertain`, `conflicting`, `unsupported`, `needs_review`, `failed`, and empty values.
- Never accept a status string without the corresponding immutable completion attestation.

**Replay Semantics**

Compute a canonical request/proof digest covering owner, item and digest, reservation, receipt and decision digest, consumption target/digest, workflow completion digest, actual usage and disposition.

- First request: atomically insert proof, settlement, events and activity.
- Exact replay: return the existing settlement with `replayed=true`; add no records.
- Changed workflow, item, receipt, proof or usage: return `409 Conflict`.
- Exact replay should remain valid after workflow archival or later approval expiry/revocation.
- Concurrent exact requests must yield one creation and one replay.
- A consumed receipt is expected evidence; settlement must never consume or execute it again.

**Required Tests**

- Reject every non-completed workflow state and every rejected verification status.
- Reject a forged completed workflow row without an immutable completion attestation.
- Reject cross-owner substitutions for item, decision, receipt, workflow, pursuit and reservation.
- Reject a workflow from another item in the same pursuit.
- Reject one receipt linked to multiple workflows.
- Reject missing or mismatched receipt consumption owner, target or digest.
- Reject mismatched proposal/allocation reservation IDs.
- Verify `human_approved` provenance requirements.
- Verify exact replay after archival, expiry and revocation.
- Verify changed-usage replay returns conflict.
- PostgreSQL concurrency and rollback tests for proof, settlement, events and activity.
- HTTP tests for `401`, `403`, `404`, `409`, initial `201`, replay `200`, and `canExecute:false`.

No files were edited and no tests were run during this read-only audit.

## 019fce0d-24e7-77d1-8e08-1d89f74501a4

- Date: 2026-08-04
- Agent: Avicenna
- Work kind: implementation
- Original report SHA-256: `9fb6a78deb60f5e95b4f1b309e2960ed21d1fbdbea657abef41d5532ec3426d6`
- Transcript: `08/04/rollout-2026-08-04T20-33-15-019fce0d-24e7-77d1-8e08-1d89f74501a4.jsonl`

**Highest-Leverage Missing Link**

Implement a controlled resource-estimation learning loop:

```text
Verified portfolio settlement
→ aggregate comparable actual usage
→ create review-required calibration proposal
→ Robert approves
→ append immutable calibration revision
→ offer the reviewed estimate in the next portfolio plan
→ operator explicitly accepts or ignores it
```

This is the strongest credential-free next step because it closes HAI’s existing operational loop. Currently, settlement produces review-only learning evidence, but approved learning never improves subsequent planning behavior.

**Current Gap**

- Settlement already records estimated-versus-actual effort and cost in [controlled_learning.go](../backend/internal/pursuit/controlled_learning.go):74.
- Outcome evidence is intentionally prevented from directly proposing or applying changes in [controlled_learning.go](../backend/internal/pursuit/controlled_learning.go):21.
- The controlled-learning service supports proposals in [service.go](../backend/internal/controlledlearning/service.go):113.
- Its default promoter only records application evidence; it does not alter planning recommendations in [ledger_promoter.go](../backend/internal/controlledlearning/ledger_promoter.go):12.
- Portfolio planning still requires every estimate to be manually supplied in [portfolio_planning.go](../backend/internal/pursuit/portfolio_planning.go):109 and [pursuits.component.html](../frontend/src/app/pages/pursuits/pursuits.component.html):1435.
- The completion matrix explicitly says HAI must not invent missing estimates in [completion-matrix.md](completion-matrix.md):62.
- The architectural boundary is documented in [domain-model.md](domain-model.md):303.

**Precise Implementation Surface**

Add a pursuit-owned calibration model and repository, avoiding a circular dependency:

- `backend/internal/pursuit/resource_estimate_calibration.go`
- `backend/internal/pursuit/resource_estimate_calibration_repository.go`
- `backend/internal/pursuit/resource_estimate_calibration_promoter.go`
- `backend/migrations/pre/0045_pursuit_resource_estimate_calibrations.up.sql`
- Corresponding guarded rollback migration.

Wire the target-specific promoter beside the existing controlled-learning composition in [routes.go](../backend/internal/router/routes.go):257.

The applied record should contain:

- Owner, project, task archetype and runtime scope.
- Sample count and deterministic aggregation method/version.
- Optimistic, expected and pessimistic minutes.
- Expected cost in micros.
- Exact settlement, attestation and authorization evidence IDs/digests.
- Proposal, application and prior-calibration digests.
- Append-only activation or rollback action.
- Record digest and timestamp.

The planner should expose the calibration as an optional recommendation. Selecting **Use reviewed estimate** copies the exact reviewed values and binds the plan to the immutable calibration digest. It must never silently replace operator input.

**Required Invariants**

1. Only `verified` or `test_passed` settlement outcomes are eligible.
2. Every sample must resolve to the exact settlement proof, completion attestation and authorization receipt.
3. Aggregation is isolated by owner, project, stable task archetype and runtime.
4. At least three comparable samples are required.
5. Evidence ordering, aggregation and proposal digests are deterministic.
6. Every generated proposal starts as `review_required` and `canExecute: false`.
7. Application requires explicit human approval of the exact current proposal revision and digest.
8. Applied calibrations and rollbacks are append-only; existing records cannot be updated or deleted.
9. Replaying the same application is idempotent; changed payloads conflict.
10. A calibration cannot change permissions, safety policy, autonomy level, provider budget or constitutional rules.
11. Planning remains `advisory_only`; resource ceilings, dependency checks and approval rules remain authoritative.
12. Foreign-owner, stale, superseded and rolled-back calibrations are rejected.
13. Partial application is recoverable through the proposal application ID and idempotency key.
14. Manual estimates remain supported and clearly distinguished from reviewed recommendations.

**Focused Tests**

Backend:

- `resource_estimate_calibration_test.go`
  - Fewer than three samples produce no proposal.
  - Foreign, weak or heterogeneous evidence is excluded.
  - Comparable evidence produces deterministic percentile estimates.
  - Repeated scans do not duplicate proposals.
- `resource_estimate_calibration_repository_postgres_test.go`
  - Reject update/delete/truncate.
  - Enforce owner isolation and unique revisions.
  - Concurrent application has one winner.
  - Rollback appends a revision.
  - Down migration refuses non-empty tables.
- Extend `application_service_test.go`
  - Only approved, current proposals invoke the target promoter.
  - Wrong target, schema, revision or digest is denied.
  - Interrupted application can be replayed safely.
- Extend `portfolio_planning_test.go`
  - Recommendation includes exact calibration evidence.
  - No automatic override occurs.
  - Accepted values must match the active digest.
  - Stale, rolled-back and foreign recommendations fail closed.
  - Existing ceilings and approval decisions remain unchanged.
- Extend router permission tests:
  - Unauthenticated `401`.
  - Non-owner `404`.
  - Separate proposal, approval and application permissions.
  - Caller-supplied owner IDs cannot bypass session ownership.

Frontend:

- Extend governance-control tests for proposal evidence, explicit approval and rollback.
- Extend pursuits tests for **Use reviewed estimate**, evidence inspection, stale state and no automatic submission.
- Add a local browser chain:

```text
create workflow
→ complete and verify
→ settle portfolio reservation
→ generate calibration proposal
→ approve and apply
→ open next portfolio plan
→ explicitly adopt reviewed estimate
→ verify plan cites calibration digest
```

The browser test should assert that no external provider, runtime execution or credentials are required.

No files were edited. This was a static, read-only trace audit; tests were not run.

## 019fce2a-9dda-7a71-94a7-81259d86a60a

- Date: 2026-08-04
- Agent: Boole
- Work kind: advisory
- Original report SHA-256: `9c5dc44e5fe62a7f5a9e98985f6e847f06fce0d4217ddf132115921ad2faab58`
- Transcript: `08/04/rollout-2026-08-04T21-05-27-019fce2a-9dda-7a71-94a7-81259d86a60a.jsonl`

**Findings**
1. **P1: Verified portfolio settlement never reaches controlled learning.**
   Task completion already calls `recordVerifiedLearningOutcome` through `ControlledLearningRecorder` in [controlled_learning.go](../backend/internal/task/controlled_learning.go):16. Portfolio settlement commits its proof and immediately returns in [portfolio_workflow_settlement.go](../backend/internal/pursuit/portfolio_workflow_settlement.go):261, while the pursuit service has no learning dependency in [service.go](../backend/internal/pursuit/service.go):772.

   **Smallest missing operational link:** inject the same narrow `RecordOutcome` capability into the pursuit service. After successful settlement, record one idempotent `EvidenceVerifiedOutcome` containing:
   - Settlement proof URI and digest.
   - Completion-attestation URI and digest.
   - Pursuit/project/workflow identity.
   - Estimated versus actual effort and cost as `MetricResult`.
   - Verification status `verified` or `test_passed`.
   - No automatic proposal, policy change, or memory mutation.

   The replay branch at [portfolio_workflow_settlement.go](../backend/internal/pursuit/portfolio_workflow_settlement.go):91 must retry the idempotent learning write so a transient learning failure can recover without creating another settlement.

2. **P2: Controlled-learning evidence is API-accessible but invisible in the frontend.**
   The router exposes `GET /controlled-learning/outcomes` in [routes.go](../backend/internal/router/routes.go):904, but `GovernanceControlService` and `loadLearning()` only retrieve proposals in [governance-control.component.ts](../frontend/src/app/pages/governance-control/governance-control.component.ts):366. A settlement-derived outcome would therefore exist but remain undiscoverable.

   Add a read-only learning-outcome list/detail method and show recent evidence separately from change proposals. This should not auto-create a proposal.

3. **Not a defect: settlement should not directly create an outcome evaluation.**
   `CreateEvaluation` requires an existing outcome revision and indicator-scoped observations in [storage_types.go](../backend/internal/outcomeevaluation/storage_types.go):47 and [service.go](../backend/internal/outcomeevaluation/service.go):117. Settlement has no trustworthy workspace/outcome/indicator mapping. Any future bridge should propose a source-backed observation for review, not invent that mapping.

**Recommended Tests**
- Extend [portfolio_workflow_settlement_test.go](../backend/internal/pursuit/portfolio_workflow_settlement_test.go):15: exactly one learning outcome for initial settlement and replay.
- Verify proof and attestation digests, owner, project, verification, and estimated/actual metrics.
- Simulate learning failure after committed settlement; replay must recover without duplicate accounting.
- Assert rejected or insufficiently verified settlements record no learning.
- Add frontend service/component tests for listing settlement-derived outcomes separately from proposals.
- Assert no `outcomeevaluation.EvaluationRecord` is created without explicit outcome-indicator mapping.

No files were edited and no tests were run during this read-only audit.

## 019fce5c-7e83-7380-bc80-25286e702a48

- Date: 2026-08-04
- Agent: Faraday
- Work kind: advisory
- Original report SHA-256: `145a04f568e329acd75d0f87865a8d7b48bfdcbf562b60c66ef0eb181aeffc21`
- Transcript: `08/04/rollout-2026-08-04T21-59-56-019fce5c-7e83-7380-bc80-25286e702a48.jsonl`

**Verdict**

HAI currently completes this portion of the loop:

`verified execution -> immutable settlement proof -> controlled-learning outcome -> review visibility`

The missing high-leverage link is:

`outcome evidence -> typed calibration proposal -> owner-approved calibration revision -> future plan bound to that exact revision`

The generic review and approval infrastructure already exists. The smallest correct solution should reuse it, not create another approval subsystem.

**Specification Requirements**

The specification requires:

- Every action to follow `act -> verify -> reconcile -> learn`.
- Calibration to use observed outcomes and confidence ranges.
- Longitudinal outcome evaluation, not task completion alone.
- Double/triple-loop learning, after-action review, replay, drift detection, and outcome reconciliation.
- Learning may update recommendations and reusable skills.
- Learning must never modify constitutional rules, permissions, or safety boundaries without explicit approval.
- Verified completion and subsequent learning must remain separately auditable.

Relevant specification locations: lines `526`, `551-554`, `1137-1189`, `1260-1289`, `1777-1784`, and `1812-1821`.

**Existing Implementation**

Verified settlement is production-oriented in [portfolio_workflow_settlement.go](../backend/internal/pursuit/portfolio_workflow_settlement.go):22:

- Owner-only settlement.
- Exact confirmation phrase.
- Receipt, approval, consumption, workflow, and completion-attestation binding.
- Immutable replay semantics.
- Only `verified` or `test_passed` completion is eligible.
- Actual effort and cost are recorded without rerunning completed work.

Settlement evidence becomes a review-only learning outcome in [controlled_learning.go](../backend/internal/pursuit/controlled_learning.go):21. It records expected-versus-actual effort and cost with source references and idempotent evidence identity.

The durable models are in [pursuit.go](../backend/internal/models/pursuit.go):133, with database enforcement in [0044_workflow_completion_settlement_proofs.up.sql](../backend/migrations/pre/0044_workflow_completion_settlement_proofs.up.sql):1.

Generic controlled learning already provides:

- Outcomes, proposals, owner decisions, applications, and rollback.
- Evidence eligibility and digest checks.
- Explicit owner confirmation and rationale.
- Immutable application/event records.
- Owner-isolated routes under `/api/v1/controlled-learning`.

Core files are [types.go](../backend/internal/controlledlearning/types.go):116, [service.go](../backend/internal/controlledlearning/service.go):93, [application_service.go](../backend/internal/controlledlearning/application_service.go):16, and [routes.go](../backend/internal/router/routes.go):905.

The owner review UI exists in [governance-control.component.ts](../frontend/src/app/pages/governance-control/governance-control.component.ts):1106.

**Material Gaps**

- No production service turns accumulated settlement outcomes into a calibration proposal.
- `LearningReviewRequired` indicates recorded evidence, not that a proposal exists.
- There is no typed `planning_estimate_calibration` learning target.
- Generic proposed changes are insufficiently structured for deterministic planning.
- The default promoter records a synthetic ledger application but changes no planner behavior.
- No immutable, queryable calibration revision exists.
- Portfolio planning still requires caller-supplied duration and usage in [portfolio_planning.go](../backend/internal/pursuit/portfolio_planning.go):145.
- Planning decisions do not bind an approved calibration revision or digest.
- Outcome queries lack safe project/cohort/metric/time aggregation.
- There are no minimum-sample, outlier, confidence, drift, supersession, or expiry rules.
- Rollback cannot disable planning behavior because no actual calibration behavior exists.
- No end-to-end test covers settlement through approved calibration into a later plan.

The existing model-routing calibration is unrelated and must not be reused for estimating human work.

**Required Invariants**

- Calibration remains owner- and project-scoped.
- Inputs must come from immutable settlements with verified attestations.
- Model confidence cannot substitute for verification.
- Fewer than three comparable samples cannot produce a proposal.
- Calibration proposals never approve themselves.
- Staged but unapproved revisions remain inert.
- Rollback immediately makes a revision ineligible for future planning.
- Exact revision and digest must be bound into planning and acceptance.
- Calibration failure cannot invalidate settlement or repeat completed work.
- Calibration may affect duration recommendations only.
- It must never alter risk, budget, priority, permissions, approval policy, autonomy, or execution authority.
- Raw and calibrated estimates must both remain visible.
- Existing plans remain historically reproducible.
- Superseded calibration requires replanning, never silent substitution.

**Smallest Production-Grade Slice**

1. Add typed target `planning_estimate_calibration`.
2. Add a deterministic calibration definition containing owner, project/cohort, algorithm version, evidence IDs/digests, sample count, date range, median actual-to-expected ratio, dispersion, confidence, bounds, and rollback plan.
3. Generate a proposal after settlement only when at least three comparable verified outcomes exist. Use a robust median and bounded factor, not an LLM.
4. Deduplicate proposals by the exact sorted evidence-set digest.
5. Reuse the existing controlled-learning owner decision and application flow.
6. Add migration `0045_pursuit_planning_calibrations` with immutable revisions and database guards.
7. Stage revisions during promotion, but consider them effective only when the corresponding owner decision is approved and application is `applied`.
8. Make rollback eligibility-derived rather than mutating an `active` flag.
9. Let portfolio planning accept an optional exact calibration revision ID.
10. Return raw estimate, calibrated estimate, factor, confidence, sample count, revision, and evidence digest.
11. Require an explicit “use calibration” action; do not silently alter owner estimates.
12. Include the calibration binding in the planner input digest so acceptance detects stale or changed evidence.

Primary additions should be `portfolio_calibration.go`, a calibration repository, migration `0045`, promoter dispatch, planner binding, typed UI detail, and one full integration test covering:

`settlement -> proposal -> owner approval -> applied revision -> future calibrated plan -> acceptance`

**Verification**

The focused existing suites passed in Docker:

- `pursuit`
- `controlledlearning`
- `outcomeevaluation`
- `resourceplanner`

This confirms current internal contracts remain consistent. It does not prove the missing calibration loop, because that loop has not yet been implemented.

## 019fce95-f774-7ca2-91be-1f1ef5adec77

- Date: 2026-08-04
- Agent: Planck
- Work kind: advisory
- Original report SHA-256: `b7a57ea6302ec9ef47bb6594ff72d8285638af6aa192564892fdd7f524cfde9b`
- Transcript: `08/04/rollout-2026-08-04T23-02-42-019fce95-f774-7ca2-91be-1f1ef5adec77.jsonl`

**Current Backend**

HAI already has two related read paths:

- Proposal recovery for 1–20 allocation IDs: `PortfolioExecutionProposalHistoryForOwner`.
  - Validates bounded unique IDs at [portfolio_execution_proposal.go:77](../backend/internal/pursuit/portfolio_execution_proposal.go):77.
  - Uses two bulk queries through `ListLatestPortfolioExecutionProposals` at [portfolio_execution_proposal_repository.go:108](../backend/internal/pursuit/portfolio_execution_proposal_repository.go):108.
  - Missing or foreign allocations are silently omitted at [portfolio_execution_proposal.go:127](../backend/internal/pursuit/portfolio_execution_proposal.go):127.

- Coordination preview for one proposal ID: `PortfolioDispatchCoordinationForOwner`.
  - Response types are `PortfolioCoordinationResult` and `PortfolioCoordinationItem`.
  - [portfolio_dispatch.go:58](../backend/internal/pursuit/portfolio_dispatch.go):58
  - Owner validation and evidence loading begin at [portfolio_dispatch.go:86](../backend/internal/pursuit/portfolio_dispatch.go):86.
  - It remains observational: `authority = coordination_preview_only`, `canExecute = false`.
  - Eligibility only reads and validates the latest explicit decision at [portfolio_dispatch.go:456](../backend/internal/pursuit/portfolio_dispatch.go):456.

The routes correctly separate read and mutation:

- Single preview uses `GET` plus `PermRead`.
- Dispatch uses `POST` plus `PermExecute` and an additional approval check.
- [routes.go:1800](../backend/internal/router/routes.go):1800
- [handler.go:267](../backend/internal/pursuit/handler.go):267

**N+1 Finding**

The single-proposal preview is read-only but query-heavy:

- Proposal and items: 2 queries at [portfolio_dispatch_repository.go:24](../backend/internal/pursuit/portfolio_dispatch_repository.go):24.
- Dispatch runs: 1 query.
- Latest dispatch results: 1 query.
- Every non-terminal item calls `portfolioDispatchEligibility()` inside the item loop at [portfolio_dispatch.go:122](../backend/internal/pursuit/portfolio_dispatch.go):122.
- `loadCurrentPortfolioWorkflowApprovalForItem()` performs one decision snapshot and then reloads the complete snapshot at [portfolio_workflow_authorization.go:694](../backend/internal/pursuit/portfolio_workflow_authorization.go):694.
- Those paths perform approximately 13 queries per item through [portfolio_execution_decision_repository.go:14](../backend/internal/pursuit/portfolio_execution_decision_repository.go):14 and [portfolio_execution_decision_repository.go:65](../backend/internal/pursuit/portfolio_execution_decision_repository.go):65.

Approximate query count is therefore:

```text
single proposal: 4 + 13 × item count
naive batch:     4 × proposal count + 13 × total item count
```

A 20-item proposal can require roughly 264 queries. Calling the current method for 20 proposals would multiply that problem.

**Smallest Batch API**

```http
GET /api/v1/pursuits/portfolio-execution-proposals/coordination
    ?proposalIds=<uuid>,<uuid>
```

Contract:

- Require 1–20 unique valid UUIDs.
- Preserve request order.
- Use authenticated subject as `ownerIdentity`; never accept owner identity from query parameters.
- Return `[]PortfolioCoordinationResult`, reusing the current response shape.
- Fail the entire request with generic `404` when any proposal is missing or foreign: “one or more proposals are unavailable to this owner.”
- Capture one `checkedAt`/`now` value for the complete response.
- Cap total returned proposal items at 500. Otherwise 20 valid proposals could still produce 10,000 items.
- Perform no authorization, receipt issuance, approval creation, dispatch, workflow intake, or settlement.

**Exact Changes**

1. [portfolio_dispatch_repository.go:15](../backend/internal/pursuit/portfolio_dispatch_repository.go):15

Add a separate read interface:

```go
type pursuitPortfolioCoordinationRepository interface {
    LoadPortfolioDispatchCoordinationBatch(
        context.Context, string, []uuid.UUID,
    ) (*portfolioDispatchCoordinationEvidence, error)
}
```

The unexported evidence aggregate should contain proposals, items grouped by proposal, complete approval snapshots by item, runs grouped by proposal, and latest dispatch result by item.

2. Repository implementation

Use a fixed maximum of eight owner-filtered queries:

- Proposals by requested IDs.
- Proposal items for all found proposals.
- Allocation items for all proposal items.
- Pursuits for all pursuit IDs.
- Settlements for all reservation IDs.
- Latest decisions using `DISTINCT ON (proposal_item_id)`.
- Latest ten dispatch runs per proposal using `ROW_NUMBER() OVER (PARTITION BY proposal_id ...)`.
- Latest dispatch result per item using `DISTINCT ON (proposal_item_id)`.

Do not call `LoadPortfolioDispatchProposal`, `LoadPortfolioExecutionProposalDecisionSnapshot`, or `LoadPortfolioWorkflowEffectApprovalSnapshot` in loops.

3. [portfolio_dispatch.go:86](../backend/internal/pursuit/portfolio_dispatch.go):86

Add:

```go
PortfolioDispatchCoordinationBatchForOwner(
    context.Context, string, []uuid.UUID,
) ([]PortfolioCoordinationResult, error)
```

Factor the existing result assembly into a pure helper. Make the existing single-proposal method delegate to the batch method with one ID, removing its present N+1 behavior too.

4. [handler.go:267](../backend/internal/pursuit/handler.go):267

Add `PortfolioDispatchCoordinationBatch`:

- Parse `proposalIds`.
- Reject empty, duplicate, malformed, or over-20 lists.
- Type-assert the new service method.
- Map malformed scope to `400`, missing/foreign to `404`, invalid immutable evidence to `409`, and unavailable storage to `503`.

The existing allocation-history parser at [handler.go:224](../backend/internal/pursuit/handler.go):224 provides the closest pattern.

5. [routes.go:1800](../backend/internal/router/routes.go):1800

Register the collection route before the parameterized route:

```go
GET("/portfolio-execution-proposals/coordination",
    requirePermission(rbac.PermRead),
    pursuitHandler.PortfolioDispatchCoordinationBatch)
```

6. Tests

Extend [portfolio_dispatch_test.go:25](../backend/internal/pursuit/portfolio_dispatch_test.go):25 with:

- 1 and 20 proposal boundaries.
- Empty, 21 IDs, duplicate and malformed IDs.
- Stable request ordering.
- Mixed eligible, blocked, stale, needs-approval and dispatched items.
- Generic failure when one proposal is foreign or missing.
- No calls to authorizer, executor, workflow intake or mutation repository methods.
- One batch repository call regardless of item count.
- Existing single preview delegates to the same batch path.

Add handler parsing tests beside [portfolio_dispatch_test.go:199](../backend/internal/pursuit/portfolio_dispatch_test.go):199.

Add authenticated read-permission coverage beside [owner_decision_permissions_test.go:200](../backend/internal/router/owner_decision_permissions_test.go):200.

Add a PostgreSQL integration test for grouping, owner isolation, top-ten run limits and latest-result selection. `DISTINCT ON` behavior should not be validated with SQLite.

**Index Risk**

The decisions index already supports the batch lookup at [0040_pursuit_portfolio_execution_proposal_decisions.up.sql:49](../backend/migrations/pre/0040_pursuit_portfolio_execution_proposal_decisions.up.sql):49.

The latest-dispatch query currently orders by `proposal_item_id`, but the existing index is owner/proposal/time at [0045_pursuit_portfolio_dispatch_coordination.up.sql:127](../backend/migrations/pre/0045_pursuit_portfolio_dispatch_coordination.up.sql):127. This removes N+1 but may still require a sort. Add a read index only if PostgreSQL `EXPLAIN ANALYZE` confirms it:

```sql
(owner_identity, proposal_item_id, attempted_at DESC, attempt_number DESC, id DESC)
```

No database model or persistence-table change is otherwise needed.

No files were edited. Several audited dispatch files are already modified or untracked in the existing worktree.

## 019fce96-891b-7de0-a350-2591256ac7b3

- Date: 2026-08-04
- Agent: Nietzsche
- Work kind: advisory
- Original report SHA-256: `c995f64d20843035d6b9c69e2c13a70746fa772c65d461dc02a41dbdb176f525`
- Transcript: `08/04/rollout-2026-08-04T23-03-19-019fce96-891b-7de0-a350-2591256ac7b3.jsonl`

**Audit Findings**

1. **Recovered proposals stop before coordination recovery.**
   Valid proposal history is stored and decision histories are loaded, but coordination is not requested in [pursuits.component.ts](../frontend/src/app/pages/pursuits/pursuits.component.ts):444. The UI therefore falls back to the manual “Refresh eligibility” action.

2. **Coordination requests are globally serialized and cancellable.**
   One `portfolioDispatchCoordinationSub` and one `portfolioDispatchCoordinationLoadingId` serve every proposal at [pursuits.component.ts](../frontend/src/app/pages/pursuits/pursuits.component.ts):121 and [line 844](../frontend/src/app/pages/pursuits/pursuits.component.ts):844. Consequently:
   - The first automatic request would block every later proposal.
   - Removing the loading guard alone would make later requests unsubscribe earlier ones.
   - One proposal loading disables refresh for all proposals in the template.

3. **A failed refresh can leave stale coordination actionable.**
   Network and validation failures set an error but do not remove an older summary or selections at [pursuits.component.ts](../frontend/src/app/pages/pursuits/pursuits.component.ts):852. `canDispatchPortfolioWorkflows()` can consequently continue using the previous summary.

4. **The frontend omits the backend’s freshness contract.**
   The backend returns `freshness.status = "current_coordination_snapshot"` with `revalidationRequired: true` in [portfolio_dispatch.go](../backend/internal/pursuit/portfolio_dispatch.go):68. `IPursuitPortfolioCoordinationResult` has no `freshness` field in [pursuit.model.interface.ts](../frontend/src/app/models/pursuit.model.interface.ts):1076, so Angular cannot prove that a restored result is current.

5. **Coordination validation has two structural gaps.**
   [validPortfolioDispatchCoordination](../frontend/src/app/pages/pursuits/pursuits.component.ts):1013 does not reject:
   - Duplicate coordination entries that omit another proposal item.
   - Counters that total correctly but do not match actual item eligibility categories.

   `dispatchRuns` is bounded but its proposal/digest bindings are not validated.

**Smallest Safe Change Set**

1. **Model contract**
   Extend `IPursuitPortfolioCoordinationResult` with:

```ts
freshness: {
  status: 'current_coordination_snapshot';
  revalidationRequired: true;
  checkedAt: string;
  reason: string;
};
```

2. **Per-proposal request coordination**
   In `pursuits.component.ts`, replace the single subscription/loading ID with:
   - Per-proposal loading state.
   - A composite subscription collection.
   - An in-flight set.
   - A pending-refresh set for one trailing refresh.

   Automatic recovery should call a private `ensurePortfolioDispatchCoordination(proposal)` after all recovered proposals pass validation. It should skip an exact cached proposal digest and deduplicate an in-flight read.

   Manual refresh, decision completion at [line 1811](../frontend/src/app/pages/pursuits/pursuits.component.ts):1811, and dispatch completion at [line 995](../frontend/src/app/pages/pursuits/pursuits.component.ts):995 should request a forced refresh. If one is already running, record one trailing refresh instead of issuing duplicates or dropping the refresh.

3. **Fail-closed state invalidation**
   - Clear coordination and selections before a forced refresh.
   - Clear them after HTTP, freshness, digest, or structural validation failure.
   - Discard a response if the currently stored proposal no longer has the captured proposal ID and digest.
   - When proposal-history reconciliation removes or replaces a proposal, remove its coordination state.
   - Use `finalize()` so loading clears on success, error, completion, cancellation, and component destruction.
   - Never automatically select items, populate the confirmation phrase, dispatch, or create approvals.

4. **Template behavior**
   Update [pursuits.component.html](../frontend/src/app/pages/pursuits/pursuits.component.html):1905:
   - Bind loading and disablement to the current proposal only.
   - Show “Restoring current eligibility…” during automatic recovery.
   - Show the per-proposal error and an explicit retry state when unavailable.
   - Display `freshness.checkedAt` and its reason.
   - Add per-section `aria-busy`.
   - Retain “Refresh eligibility” as an explicit refresh/retry, not a required initial action.

**Existing API**

No service endpoint change is required. The existing encoded read-only endpoint is correctly implemented in [pursuit.service.ts](../frontend/src/app/services/pursuit.service.ts):106 and tested in [pursuit.service.spec.ts](../frontend/src/app/services/pursuit.service.spec.ts):201.

A backend batch service exists at [portfolio_dispatch.go](../backend/internal/pursuit/portfolio_dispatch.go):170, but no HTTP route exposes it; the router currently exposes only the single-proposal endpoint at [routes.go](../backend/internal/router/routes.go):1801. The minimum Angular fix should not depend on that unavailable route.

**Required Tests**

Update [pursuits.component.spec.ts](../frontend/src/app/pages/pursuits/pursuits.component.spec.ts):644 and add coverage for:

- A recovered proposal automatically loads coordination once.
- Two recovered proposals load independently; neither request cancels the other.
- Every restored item remains unselected.
- Repeated automatic recovery does not duplicate an exact cached/in-flight request.
- A decision or dispatch refresh during an in-flight recovery produces exactly one trailing read.
- Manual refresh during loading does not start a concurrent duplicate.
- Missing/invalid freshness is rejected and removes stale actionable state.
- Duplicate items, mismatched counters, proposal digest drift, and malformed dispatch runs fail closed.
- One proposal’s HTTP failure does not affect another proposal’s successful recovery.
- A response for a replaced proposal is discarded.
- Loading clears on `EMPTY`, error, success, cancellation, and destruction.
- No dispatch endpoint is called during automatic recovery.

No files were edited and no tests were run during this read-only audit.

## 019fce97-18d7-7273-ae60-2116a7952ac8

- Date: 2026-08-04
- Agent: Singer
- Work kind: advisory
- Original report SHA-256: `6000cc5a202ff55761ff2131c8b285bebe808b6a8f0e4942ca1a60766a791150`
- Transcript: `08/04/rollout-2026-08-04T23-03-56-019fce97-18d7-7273-ae60-2116a7952ac8.jsonl`

**Findings**

1. **P1: The batch recovery route is not deployed.**
   Source registers the endpoint in [routes.go](../backend/internal/router/routes.go):1801, but the healthy live backend returns `404 page not found` after authenticated local login. The coordinator files are currently untracked or modified, so the running image is behind the working tree.

2. **P1: No backend test exercises the batch coordinator.**
   `PortfolioDispatchCoordinationBatchForOwner` has no service, handler, router, or PostgreSQL test. Existing dispatch tests cover only single-proposal coordination and execution in [portfolio_dispatch_test.go](../backend/internal/pursuit/portfolio_dispatch_test.go):25.

3. **P1: Unknown-ID behavior is implicit, not proven.**
   Unknown and foreign-owned IDs are silently omitted at [portfolio_dispatch.go](../backend/internal/pursuit/portfolio_dispatch.go):213. This can be a secure contract because it avoids an ownership oracle, but tests must prove:
   - Unknown and foreign IDs behave identically.
   - Known results retain request order.
   - Mixed requests cannot leak which omitted ID exists.
   - The frontend visibly marks missing proposals unavailable.

4. **P2: Frontend validation is not fully fail-closed.**
   [pursuits.component.ts](../frontend/src/app/pages/pursuits/pursuits.component.ts):1013 does not validate coordination freshness and does not reject duplicate item IDs. A duplicated item could replace another item while preserving array length and counters. Dispatch-result validation has the same uniqueness gap at line 1038.

5. **P2: PostgreSQL-specific behavior is untested.**
   The repository uses window functions and `DISTINCT ON` in [portfolio_dispatch_repository.go](../backend/internal/pursuit/portfolio_dispatch_repository.go):103, but there is no dispatch PostgreSQL integration test. Current migration tests inspect SQL text rather than execute coordination scenarios.

**Controls Present**

- Owner filters are applied to proposals, items, decisions, pursuits, reservations, runs, and results.
- Input bounds are `1..20`; duplicates and invalid UUIDs are rejected.
- Known results follow caller-supplied proposal order.
- Every coordination result uses `coordination_preview_only` and `canExecute: false`.
- Batch recovery resets all frontend selections to `false` at [pursuits.component.ts](../frontend/src/app/pages/pursuits/pursuits.component.ts):921.
- Freshness says `current_coordination_snapshot` while explicitly requiring dispatch revalidation.
- The coordinator performs reads only and never creates decisions, receipts, workflows, or approval authority.
- Route protection is authenticated-owner middleware plus `PermRead`; error mapping is `400` validation, `409` evidence conflict, and `503` unavailable storage.

**Required Acceptance Tests**

- Service: `0`, `1`, `20`, and `21` IDs; nil UUID; duplicates; random input order; known/unknown/foreign mixtures; cancelled context.
- State matrix: current approval, missing/rejected approval, expired approval, changed state digest, settled reservation, blocked item, and terminal prior dispatch.
- Invariants: identical `checkedAt`, `revalidationRequired=true`, `canExecute=false`, no returned duplicates, no selection state, and zero writer calls.
- Router: unauthenticated `401`; owner/operator/viewer read access; unknown role `403`; no cross-owner records.
- PostgreSQL: two owners, interleaved proposal IDs, latest decision selection, dispatch history older than ten runs, immutable triggers, and before/after ledger equality.
- Browser: restore multiple proposals, preserve ordering, show missing records safely, start with no checkbox selected, issue no POST, and leave all ledgers unchanged.

**Verification**

Backend packages and migrations passed. The focused Angular suite passed `62/62`. The live PostgreSQL schema and immutable triggers exist, and all relevant ledger counts stayed unchanged during the audit. Playwright acceptance was skipped because no operator credentials were configured, and it does not currently cover portfolio batch recovery.

No files were edited. The batch coordinator is conservatively designed, but it is not release-ready until the route is deployed and the missing safety tests are added.

## 019fcf00-af8a-73e0-b4e1-a81b3e22c336

- Date: 2026-08-05
- Agent: Peirce
- Work kind: implementation
- Original report SHA-256: `6c00f16014a2229fb5339fcd75f65b1b8b848dcd4a1cfbf2de59e0708d94dbf5`
- Transcript: `08/05/rollout-2026-08-05T00-59-16-019fcf00-af8a-73e0-b4e1-a81b3e22c336.jsonl`

**Audit Boundary**

I compared all 55 framework families with all 55 rows in HAI’s operating-contract matrix. This is a static audit of the current local worktree, including uncommitted work: `main` is at `37edf88`, with 222 modified files and 439 untracked files. These findings therefore describe the local workspace, not necessarily GitHub `main`.

**Top 10 Gaps**

1. **Real connected-source coverage remains the largest operational gap.**
   The `/api/v1/sources` API and `/connected-sources` UI exist, but only Gmail and Trello have bounded live evidence. Drive, Contacts, and Calendar remain contract-tested; WhatsApp/browser accounts, webhooks, and folder watchers are absent. This limits families 25, 26, and 38 because HAI cannot reliably notice new real-world events. [README](../README.md):406 · [completion matrix](completion-matrix.md):28

2. **The advanced workflow graph is isolated from the actual Workflow Engine.**
   `workflowgraph` defines immutable versions, branching, timers, approvals, parallel joins, bounded cycles, optimistic concurrency, and compensation. But only repository interfaces exist; the evaluator and compensation planner deliberately do not mutate or execute. No router or Angular references exist outside the package. [repository.go](../backend/internal/workflowgraph/repository.go):5 · [evaluator.go](../backend/internal/workflowgraph/evaluator.go):10 · [compensation.go](../backend/internal/workflowgraph/compensation.go):24

3. **External-effect verification is not independently complete.**
   `/api/v1/verification` and `/grounded-answers` provide useful claim and run inspection, but some execution/postcondition checks still rely on the task-level validation result. There is no requirement-specific verifier for every runtime/provider, and network ambiguity prevents exactly-once external-effect proof. [routes.go](../backend/internal/router/routes.go):1225 · [framework matrix](framework-operating-contract-matrix.md):37

4. **Model routing is sophisticated locally but not proven against real provider outcomes.**
   `/api/v1/llm`, `/api/v1/model-intelligence`, `/llm-policy`, and `/model-intelligence` exist. Direct generation remains `unvalidated` until another trusted validator records an outcome; model capability, price, fallback quality, and completion calibration still need retained live provider evidence. [framework matrix](framework-operating-contract-matrix.md):52 · [model service](../frontend/src/app/services/model-intelligence.service.ts):22

5. **Agent teams cannot yet perform governed runtime dispatch.**
   Team lifecycle, membership, messages, delegation assessment, and consensus APIs are substantial and visible in Governance Control. However, contracts and consensus are explicitly advisory and cannot grant execution authority; distributed A2A transport and cryptographic identity/signing remain absent. [agent_team_handler.go](../backend/internal/frameworkregistry/agent_team_handler.go):44 · [agent_team_types.go](../backend/internal/frameworkregistry/agent_team_types.go):52

6. **Privacy enforcement lacks a durable lifecycle control plane.**
   Privacy scan records are stored only in a process-local slice. Retention is a pure age evaluator over memories and performs no I/O; no durable retention-policy, legal-hold, purpose, consent, or deletion-decision API/UI was found. [privacy service](../backend/internal/privacyfilter/service.go):19 · [retention.go](../backend/internal/retention/retention.go):1

7. **The claim graph is strong, but memory and graph coverage are incomplete.**
   Claims support provenance, validity intervals, conflicts, supersession, corrections, verification states, and temporal retrieval. The remaining weakness is coverage: conceptual memory types are not independently lifecycle-managed, and several product ledgers still lack projection adapters into the whole-life graph. [knowledgegraph types](../backend/internal/knowledgegraph/types.go):84 · [framework matrix](framework-operating-contract-matrix.md):40

8. **Observability is mostly audit records and manual aggregate snapshots.**
   Prometheus exposes a small opt-in surface. Langfuse and OpenLIT accept only owner-triggered aggregate snapshots; OpenLIT explicitly has no SDK or automatic instrumentation. End-to-end HTTP/task/model/tool/provider trace correlation, SLOs, alert rules, retention, and operator-facing incident drill-down remain missing. [OpenLIT service](../backend/internal/openlit/service.go):96 · [agent tool catalog](agent-tool-catalog.md):61

9. **Formal optimization is a detached proposal service.**
   `/api/v1/planning-optimizer` can probe OR-Tools and produce bounded proposals, but it cannot alter workflows, tasks, or calendars, and no Angular integration exists. It is not yet fed automatically from pursuits, LifeOps capacity, connected Calendar state, resource ledgers, or longitudinal uncertainty calibration. [planning service](../backend/internal/planningoptimizer/service.go):1 · [routes.go](../backend/internal/router/routes.go):1659

10. **Several personal-operating domains remain catalog entries rather than engines.**
    Habits, health, legal/case management, relationships, travel, and emergency continuity have catalog definitions, classification, preferences, and playbooks. They lack typed domain records, source adapters, lifecycle workflows, domain-specific validators, and dedicated Basic/Advanced operational views. [framework matrix](framework-operating-contract-matrix.md):63 · [domain-pack routes](../backend/internal/router/routes.go):746

**Next Vertical Slice**

The best next code-only slice is **owner-scoped durable workflow-graph simulation**. It ranks behind live source acceptance in product impact, but it has no external credential dependency and reuses substantial tested code.

Implement:

1. Add an owner/workspace envelope around immutable graph definitions and runs; current core types contain no owner identity.
2. Add PostgreSQL definition, run, and append-only run-event repositories with optimistic revision checks.
3. Expose authenticated `/api/v1/workflow-graphs` create, validate, version, start-simulation, evaluate, inspect, and compensation-plan routes.
4. Add an Advanced section to `/workflow-engine` showing graph validation, active nodes, branch decisions, approval waits, and compensation order.
5. Keep the first slice non-executing: no tool invocation or external effect.
6. Prove owner isolation, idempotency-digest conflicts, immutable versions, stale-revision rejection, deterministic replay, and browser create-to-simulation acceptance.

No files were edited and no tests were run during this audit.

## 019fcf01-7265-7663-ba2a-4b8dee833206

- Date: 2026-08-05
- Agent: Lagrange
- Work kind: advisory
- Original report SHA-256: `2cee125d45cd047bf93ced65466cf7bf7358e817644327557d905d78922a6a17`
- Transcript: `08/05/rollout-2026-08-05T01-00-06-019fcf01-7265-7663-ba2a-4b8dee833206.jsonl`

**Findings**

1. **[P1] No end-to-end ambient monitoring loop exists.** Services are constructed independently: ontology projections are wired, but `proactivity`, `outcomeevaluation`, `workflow`, and `lifeledger` are never composed into an orchestrator. [routes.go](../backend/internal/router/routes.go):288
   Result: a ledger/workflow change cannot automatically become an outcome observation, interruption decision, and owner-facing review item.

2. **[P1] Long-term outcome evaluation is manual, not monitored.** `Observation` is defined, but there is no independently appendable observation ledger or monitoring schedule. Observations must be supplied inside each `CreateEvaluationRequest`. [storage_types.go](../backend/internal/outcomeevaluation/storage_types.go):47, [service.go](../backend/internal/outcomeevaluation/service.go):117
   Routes expose manual evaluations and corrections only. [handler.go](../backend/internal/outcomeevaluation/handler.go):46

3. **[P1] Triggering is real but restricted to pre-existing workflow open loops.** The durable scheduler recovers claims, processes due open loops, and runs due workflows. [scheduler.go](../backend/internal/workflow/scheduler.go):74, [durable_scheduler.go](../backend/internal/workflow/durable_scheduler.go):41
   A due loop creates a checklist item and proposal, but does not emit a proactivity signal or outcome observation. [service.go](../backend/internal/workflow/service.go):2012

4. **[P1] Proactivity is a genuine interruption policy engine, but invocation is manual.** Quiet hours, cooldown, attention budgets, thresholds and channels exist, while authority remains explicitly false. [types.go](../backend/internal/proactivity/types.go):69, [types.go](../backend/internal/proactivity/types.go):165
   Signals and evaluations only enter through explicit HTTP calls. [handler.go](../backend/internal/proactivity/handler.go):69

5. **[P2] Owner interruption controls exist internally but are not exposed.** The service supports accept, dismiss, snooze, suppress and resume with immutable feedback history. [feedback.go](../backend/internal/proactivity/feedback.go):18
   `RegisterRoutes` exposes no feedback or actionable-inbox endpoints. This leaves §27’s review interaction incomplete.

6. **[P2] Standing-mandate stop conditions are action-time checks, not continuous interrupts.** Facts are supplied by each action request and evaluated only during `Authorize`. [types.go](../backend/internal/standingmandate/types.go):157, [evaluator.go](../backend/internal/standingmandate/evaluator.go):98
   This is a valid execution boundary, but it cannot detect changed facts or interrupt already-running work.

7. **[P2] Life records cannot drive incremental monitoring.** Life Ledger stores due dates and statuses, but its repository only supports ordinary listing/history, with no due, changed-since, or claimable scan contract. [types.go](../backend/internal/lifeledger/types.go):155 Life Ontology likewise has no change cursor/outbox. [types.go](../backend/internal/lifeontology/types.go):372

8. **[P2] §37 learning and outcome monitoring are parallel systems.** Task `OutcomeMonitoring` is planning metadata copied from the framework decision. [service.go](../backend/internal/task/service.go):2505 Verified task results and workflow corrections enter `controlledlearning`, not `outcomeevaluation`. [controlled_learning.go](../backend/internal/task/controlled_learning.go):43

**Verdict**

- Real trigger: **partial**
- Real interruption policy: **implemented but unwired**
- Real longitudinal evaluator: **implemented but manually fed**
- Closed trigger → monitor → policy → owner interaction loop: **absent**

This matches the specification’s careful “Structured,” rather than “Enforced,” wording for §§26–27. [framework-operating-contract-matrix.md](framework-operating-contract-matrix.md):45

**Recommended Next Slice**

Implement an advisory-only `ambientmonitor` pipeline:

```text
durable scheduled scan
→ deterministic workflow/life-ledger metric collector
→ immutable outcome observation
→ due longitudinal evaluation
→ proactivity signal
→ interruption-policy evaluation
→ owner inbox
→ accept/dismiss/snooze feedback
```

Required contracts:

- `MonitorSource.Collect(ctx, target, asOf) ([]Observation, error)`
- `ObservationRepository.Append/ListWindow`
- `MonitorTargetRepository.ClaimDue/Complete/Fail`
- `AttentionInboxService.List(owner)`
- Initial bounded metrics: `workflow_open_loop_count`, `workflow_verified_completion_count`, and `overdue_commitment_count`
- No arbitrary SQL/expression bindings, notifications, workflow mutation, calendar writes, task execution, or mandate authorization.

Required migration `0049_outcome_attention_monitor`:

- `outcome_observation_records`
- `outcome_monitor_targets`
- `outcome_monitor_runs`
- Owner/workspace/outcome/indicator scope constraints
- Exact-replay idempotency and immutable digests
- Due-target claim/lease indexes
- No new interruption table: derive the inbox from existing proactivity decisions plus feedback.

Required routes:

- `POST/GET .../:outcomeId/observations`
- `PUT/GET .../:outcomeId/monitor`
- `POST .../:outcomeId/monitor/run`
- `GET .../:outcomeId/monitor/runs`
- `GET /api/v1/proactivity/inbox`
- `GET/POST /api/v1/proactivity/feedback`

Required acceptance test:

1. Create a source-backed outcome and monitor target.
2. Create a due workflow open loop or overdue commitment.
3. Run the durable monitor twice.
4. Assert exactly one observation, evaluation, signal and interruption decision.
5. Snooze through the HTTP feedback route.
6. Run again and assert policy suppression.
7. Assert no executor, notification sender, calendar writer, workflow transition, standing-mandate authorization, or learning-policy mutation occurred.
8. Repeat across two owners and after a simulated worker crash/lease expiry.

No files were edited and no tests were run. The audited implementation is largely modified or untracked working-tree code, so these findings describe the current workspace, not confirmed `main`.

## 019fcf01-ff16-75d3-9217-3d0a7865c112

- Date: 2026-08-05
- Agent: Dirac
- Work kind: advisory
- Original report SHA-256: `d214d7ebf3de726311ebc742b33122443c556110391e32fd248d0ced8ab712f3`
- Transcript: `08/05/rollout-2026-08-05T01-00-42-019fcf01-ff16-75d3-9217-3d0a7865c112.jsonl`

**Findings**

1. **[P1] Complete proactivity controls exist in the backend but are unreachable from the frontend.**
   The API supports owner-scoped `accept`, `dismiss`, `snooze`, `suppress`, and `resume`, including reasons, idempotency, signal-digest validation, and bounded snooze times ([feedback.go](../backend/internal/proactivity/feedback.go):20, [handler.go](../backend/internal/proactivity/handler.go):68). The engine enforces those controls during evaluation ([engine.go](../backend/internal/proactivity/engine.go):156).

   The Angular service exposes only policy, signal, and decision reads ([governance-control.service.ts](../frontend/src/app/services/governance-control.service.ts):354). The UI renders eight read-only signals and decisions without inspection or feedback controls ([governance-control.component.html](../frontend/src/app/pages/governance-control/governance-control.component.html):390).

   Missing frontend contracts: `GET/POST /proactivity/feedback`, policy `PUT`, reason entry, snooze date/time, suppress/resume, feedback history, and record inspector.

2. **[P1] Ambient and proactivity are separate suggestion lifecycles, so an operator decision is not globally respected.**
   Ambient exposes only `accept` and `dismiss` ([ambient.service.ts](../frontend/src/app/services/ambient.service.ts):30). Dismissal applies a fixed environment-controlled cooldown ([service.go](../backend/internal/ambient/service.go):743); it does not create proactivity `snooze` or `suppress` feedback.

   Both frontend requests send `{}`, although the backend supports a resolution note. Consequently, dismissed feedback never satisfies the backend’s 12-character learning threshold ([ambient-brain.component.ts](../frontend/src/app/pages/ambient-brain/ambient-brain.component.ts):279, [service.go](../backend/internal/ambient/service.go):1291).

   Missing API/domain bridge: one canonical suggestion ID and feedback ledger shared by Ambient Brain, Control Center, HAI OS, and proactivity.

3. **[P1] Control Center’s “Snooze” action is not a snooze.**
   It transitions the workflow to `waiting_external_input` without a wake time or resurfacing schedule ([control-center.component.ts](../frontend/src/app/pages/control-center/control-center.component.ts):376). This can leave work parked indefinitely and misrepresents what the action does.

   The approval buttons operate on workflow approvals, not proactive suggestions ([control-center.component.ts](../frontend/src/app/pages/control-center/control-center.component.ts):345). Ambient data is reduced to scan activity and totals ([control-center.component.ts](../frontend/src/app/pages/control-center/control-center.component.ts):495).

4. **[P1] Per-module progressive disclosure has competing state owners and incomplete coverage.**
   The shared shell persists mode, sections, and navigation under `hai.module-view.v1.<module>` ([module-view-preferences.service.ts](../frontend/src/app/control-room/module-view-preferences.service.ts):6). Framework Registry independently reads and overwrites the same key using a schema that omits `navigationMode` ([framework-registry.component.ts](../frontend/src/app/pages/framework-registry/framework-registry.component.ts):35, [framework-registry.model.interface.ts](../frontend/src/app/models/framework-registry.model.interface.ts):371). The shell and page can therefore disagree or erase each other’s preference fields.

   `ambient-brain`, `workflow-engine`, and `hai-os` have no registered advanced sections ([module-registry.ts](../frontend/src/app/control-room/module-registry.ts):17). The workflow reminder’s `reminder-drawer__advanced` section is not among the shell’s persisted selectors ([app-shell.component.ts](../frontend/src/app/control-room/app-shell.component.ts):21, [workflow-engine.component.html](../frontend/src/app/pages/workflow-engine/workflow-engine.component.html):1083).

5. **[P2] Workflow suggestions have partial controls, but no snooze or suppression lifecycle.**
   Generic proposals can be approved, rejected, or returned for changes. Reminder preparations can be prepared, approved, rejected, and revoked ([workflow-engine.component.html](../frontend/src/app/pages/workflow-engine/workflow-engine.component.html):1028). There is no snooze or suppress status/API for either contract.

   The API loads up to 100 reminder proposals, but the UI shows only three with no “view all” path ([workflow.service.ts](../frontend/src/app/services/workflow/workflow.service.ts):87, [workflow-engine.component.html](../frontend/src/app/pages/workflow-engine/workflow-engine.component.html):89). A decision-history API exists but is not consumed by the component ([workflow.service.ts](../frontend/src/app/services/workflow/workflow.service.ts):122).

6. **[P2] Framework recommendations explain their choice but cannot be resolved as recommendations.**
   The screen shows selection reason, why-now, responsible actor, risk ceiling, approval reasons, and framework inspectors ([framework-registry-recommendation.component.html](../frontend/src/app/pages/framework-registry/framework-registry-recommendation.component.html):35). There is no accept/reject/snooze/suppress API or state. “Approval required” is informational only. `Clear` removes only the local display and draft, not the recorded selection ([framework-registry.component.ts](../frontend/src/app/pages/framework-registry/framework-registry.component.ts):486).

7. **[P2] HAI OS is an overview, not an actionable suggestion surface.**
   Its only API is `GET /api/v1/os/overview` ([hai-os.service.ts](../frontend/src/app/services/hai-os/hai-os.service.ts):15). It can route to pursuits and show aggregated Ambient counts, but cannot inspect an individual proactive suggestion or accept, reject, snooze, or suppress it ([hai-os.component.html](../frontend/src/app/pages/hai-os/hai-os.component.html):43). Its metrics, operating planes, readiness, and governance sections are always rendered rather than progressively disclosed.

**Capability Matrix**

| Module | Inspect/explain | Accept/reject | Snooze | Suppress | Progressive disclosure |
|---|---|---|---|---|---|
| HAI OS | Summary/route only | No | No | No | No |
| Control Center | Workflow detail only | Workflow approvals | Mislabelled wait state | No | Partial |
| Framework Registry | Yes | No | No | No | Conflicting state |
| Workflow Engine | Partial, first 3 reminders | Yes, selected contracts | No | No | Partial |
| Ambient Brain | Partial rationale/source | Accept/dismiss | Fixed dismiss cooldown only | No | No |
| Governance/Proactivity | Read-only summary | Backend only | Backend only | Backend only | Section-level only |

No files were changed. This was a static code-path audit; browser behavior and live API responses were not exercised.

## 019fcf83-db4f-7c80-9c39-73b77a693a1d

- Date: 2026-08-05
- Agent: Hooke
- Work kind: implementation
- Original report SHA-256: `4eed1ffa2893437691382505b266856c15eaf66c4bd53a5df4263c2df8b3ab94`
- Transcript: `08/05/rollout-2026-08-05T03-22-33-019fcf83-db4f-7c80-9c39-73b77a693a1d.jsonl`

Implemented `backend/internal/ambientmonitor` within the exclusive scope.

**Added**
- [types.go](../backend/internal/ambientmonitor/types.go)
- [validation.go](../backend/internal/ambientmonitor/validation.go)
- [service.go](../backend/internal/ambientmonitor/service.go)
- [repository.go](../backend/internal/ambientmonitor/repository.go)
- [service_test.go](../backend/internal/ambientmonitor/service_test.go)
- [validation_test.go](../backend/internal/ambientmonitor/validation_test.go)
- [security_test.go](../backend/internal/ambientmonitor/security_test.go)

The package includes owner/workspace/outcome/indicator scoping, closed source kinds, scheduling and enabled state, generation-fenced leases, recovery, immutable observations/runs, exact idempotency, deterministic collectors, retryable advisory composition, and uniformly disabled authority capabilities.

**Verification**
- Focused tests: passed, 70.3% statement coverage
- Ten repeated test runs: passed
- Linux race detector: passed
- `go vet`: passed
- No PostgreSQL/GORM/SQL files or imports
- No files outside `backend/internal/ambientmonitor` changed for this assignment
- Not committed

## 019fcf84-919a-7f20-8f6a-1c2e2d0e6b02

- Date: 2026-08-05
- Agent: Tesla
- Work kind: implementation
- Original report SHA-256: `3cc26ea36aecefe6bad1306a1055d5835c767da8da6781fafdce302a733aaea4`
- Transcript: `08/05/rollout-2026-08-05T03-23-19-019fcf84-919a-7f20-8f6a-1c2e2d0e6b02.jsonl`

Implemented migration `0049` within the exclusive four-file scope:

- [0049 up migration](../backend/migrations/pre/0049_outcome_attention_monitor.up.sql)
- [0049 rollback](../backend/migrations/pre/0049_outcome_attention_monitor.down.sql)
- [Contract test](../backend/migrations/outcome_attention_monitor_contract_test.go)
- [PostgreSQL lifecycle test](../backend/migrations/outcome_attention_monitor_postgres_test.go)

It includes immutable observation/run ledgers, revisioned monitor targets, lease and claim validation, source and digest constraints, advisory-only authority, run-backed target projections, due/history indexes, restricted foreign keys, and fail-closed rollback.

Verification:

- Focused contract and PostgreSQL lifecycle tests passed.
- Three consecutive isolated PostgreSQL lifecycle runs passed.
- Empty rollback/reapply and non-empty rollback refusal passed.
- Exactly the four authorized files changed.
- Nothing was committed.

The complete migrations package still fails because [migration_chain_contract_test.go](../backend/migrations/migration_chain_contract_test.go):145 hardcodes the migration tail through `0048`. I left it untouched because it falls outside the exclusive write scope.

## 019fcf85-35dc-73f0-8cb0-e8bc6032afc9

- Date: 2026-08-05
- Agent: Parfit
- Work kind: implementation
- Original report SHA-256: `e207428e83f173d7871f3645ee71b6624bcc25c4c8135115bf62fda518df5f90`
- Transcript: `08/05/rollout-2026-08-05T03-24-01-019fcf85-35dc-73f0-8cb0-e8bc6032afc9.jsonl`

Implemented within the exclusive three-file scope:

- [ambient-monitor.model.interface.ts](../frontend/src/app/models/ambient-monitor.model.interface.ts)
- [ambient-monitor.service.ts](../frontend/src/app/services/ambient-monitor.service.ts)
- [ambient-monitor.service.spec.ts](../frontend/src/app/services/ambient-monitor.service.spec.ts)

The contract includes typed targets, observations, runs, requests, list responses, encoded path segments, bounded `HttpParams` limits, and literal advisory-only authority. Write payloads are allowlisted, preventing owner identity or authority fields from reaching the API.

Verification:

- Focused Angular tests: `6 SUCCESS`
- Production Angular build: passed
- Scope audit: only the three requested files are new
- No component or UI changes
- Nothing committed

Existing unrelated bundle and component-style budget warnings remain.

## 019fcf91-6bfc-78e0-a4ee-20bf0c78a4b6

- Date: 2026-08-05
- Agent: Raman
- Work kind: implementation
- Original report SHA-256: `2cb680b0cf36df70d71fcc9892e202f2b213ba9c942529d5a628fb405a799e18`
- Transcript: `08/05/rollout-2026-08-05T03-37-21-019fcf91-6bfc-78e0-a4ee-20bf0c78a4b6.jsonl`

Implemented without committing.

Changed:
- [repository_postgres.go](../backend/internal/ambientmonitor/repository_postgres.go)
- [repository_postgres_test.go](../backend/internal/ambientmonitor/repository_postgres_test.go)

Includes owner/workspace isolation, lease fencing, immutable observation/run appends, completion replay, due-scope discovery, bounded lists, and `DefaultRepository`.

Verification passed against a fresh PostgreSQL database with migration `0049`:

```text
go test ./internal/ambientmonitor -run "^TestPostgres" -count=1
ok automation-hub-backend/internal/ambientmonitor
```

The temporary database was dropped afterward.

Known gaps in migration `0049`:
- Durable target IDs must be canonical UUIDs.
- Long worker IDs are limited to the fixed scheduler identity, canonical UUIDs, or 15-byte manual IDs.
- Create/enable commands lack a dedicated persistent idempotency ledger; exact replay is available for completed runs, while target commands are state-idempotent.
- An active unexpired lease cannot be cancelled by disabling its target due to migration trigger rules.

No commit was created.

## 019fcf92-bf6c-7a10-9cb6-73ce90e5788c

- Date: 2026-08-05
- Agent: Darwin
- Work kind: implementation
- Original report SHA-256: `e783012d00f97da4ed83517573d182bac0c1d91f0ebe1af1c772d12547c8ba3a`
- Transcript: `08/05/rollout-2026-08-05T03-38-48-019fcf92-bf6c-7a10-9cb6-73ce90e5788c.jsonl`

Implemented the fixed read-only ambient collectors:

- [collectors.go](../backend/internal/ambientmonitor/collectors.go)
- [collectors_test.go](../backend/internal/ambientmonitor/collectors_test.go)

Included:

- Owner-scoped workflow open-loop count matching dashboard semantics.
- Immutable `verified`/`test_passed` completion count.
- Latest-revision overdue commitment count excluding fulfilled/cancelled records.
- Fixed SQL only, with no arbitrary query or URL input.
- Bounded, ordered snapshots and deterministic SHA-256 digests.
- UTC-normalized observation timestamps.
- Fail-closed source, scope, snapshot, and constructor validation.

Verification: focused package tests passed three consecutive runs. Only the two authorized files were changed. Nothing was committed.

## 019fcf97-583d-7970-a17b-1b8fbc239e61

- Date: 2026-08-05
- Agent: Volta
- Work kind: implementation
- Original report SHA-256: `7602da616f805f61d8893384b218fe7fb43785fa20e4beed5ee5ad6f893b3b53`
- Transcript: `08/05/rollout-2026-08-05T03-43-50-019fcf97-583d-7970-a17b-1b8fbc239e61.jsonl`

Implemented the advisory composition layer without expanding beyond the two-file scope.

- [composer.go](../backend/internal/ambientmonitor/composer.go):56
  - Implements `ambientmonitor.Sink`.
  - Loads owner/workspace-scoped outcomes and validates indicators.
  - Converts immutable in-window monitor observations into source-supported outcome evidence using safe `hai://` provenance.
  - Creates evaluations, proactivity signals, default policy, and attention decisions using run-digest idempotency keys.
  - Rejects cross-scope records, digest tampering, invalid chronology, unknown indicators, and authority-bearing results.
  - Sanitizes downstream errors without leaking credentials.
  - Exposes no execution, sending, notification, calendar, workflow, mandate, or learning capabilities.

- [composer_test.go](../backend/internal/ambientmonitor/composer_test.go):38
  - Exact replay produces one evaluation, policy, signal, decision, and inbox item.
  - Out-of-window observations are excluded.
  - Cross-owner observations are rejected before writes.
  - Unknown indicators fail closed.
  - Provider errors are redacted.
  - All evaluation, decision, and inbox authority remains disabled.

Verification passed:

- `go test ./internal/ambientmonitor ./internal/outcomeevaluation ./internal/proactivity`
- Composer tests repeated 10 times
- `go test -race ./internal/ambientmonitor`
- `go vet ./internal/ambientmonitor`

Nothing was committed.

## 019fcf9a-7bae-7271-baec-cfc8ecf093d2

- Date: 2026-08-05
- Agent: Gauss
- Work kind: implementation
- Original report SHA-256: `7c83e23222446591457fb69a5ef7700600b3e7af305dca9c2ffc9ad32114ef5e`
- Transcript: `08/05/rollout-2026-08-05T03-47-15-019fcf9a-7bae-7271-baec-cfc8ecf093d2.jsonl`

Implemented the guarded ambient monitor HTTP API in:

- [handler.go](../backend/internal/ambientmonitor/handler.go)
- [handler_test.go](../backend/internal/ambientmonitor/handler_test.go)

Added:

- Owner/workspace/outcome-scoped monitor routes.
- Outcome, indicator, and longitudinal-window validation.
- Strict JSON and bounded request/history limits.
- Server-bound identity and authority.
- Cross-outcome resources returning `404`.
- Idempotent registration and enabled-state replay.
- Workspace-wide due processing and expired-lease recovery.
- Sanitized internal errors with error IDs.
- Advisory-only authority assertions.
- `cadenceSeconds` HTTP DTO mapping instead of nanosecond durations.

Verification passed:

```text
go test ./internal/ambientmonitor -count=1
ok automation-hub-backend/internal/ambientmonitor
```

Nothing was committed.

## 019fcfa8-cae8-7103-8660-2a1e62fa512b

- Date: 2026-08-05
- Agent: Hypatia
- Work kind: implementation
- Original report SHA-256: `319a2c82f17bc6ed9d69102a29363eebea8561c46a87dd626e20593df89cc707`
- Transcript: `08/05/rollout-2026-08-05T04-02-53-019fcfa8-cae8-7103-8660-2a1e62fa512b.jsonl`

Implemented the Angular Governance Control integration for the ambient outcome monitor.

- Basic view shows status, source, cadence, latest observation/run, pause/enable, and run-due controls.
- Advanced view provides immutable target configuration, observations, runs, and explicit advisory-only authority boundaries.
- Monitor loading follows outcome loading and uses the current workspace, outcome, and indicator.
- Added cancellation, stale-scope protection, strict API errors, retry controls, and duplicate-submission prevention.
- No execution, delivery, notification, calendar, workflow mutation, learning mutation, or mandate controls were added.

Changed:
- [governance-control.component.ts](../frontend/src/app/pages/governance-control/governance-control.component.ts)
- [governance-control.component.html](../frontend/src/app/pages/governance-control/governance-control.component.html)
- [governance-control.component.scss](../frontend/src/app/pages/governance-control/governance-control.component.scss)
- [governance-control.component.spec.ts](../frontend/src/app/pages/governance-control/governance-control.component.spec.ts)

Verification:
- Focused Governance Control tests: `34 SUCCESS`
- Production Angular build: passed
- Existing soft bundle/style warnings remain, including the 1.11 MB initial bundle.
- No commit was created.

## 019fcfaf-9fe6-7ed0-aded-a4e588859c1a

- Date: 2026-08-05
- Agent: Dewey
- Work kind: implementation
- Original report SHA-256: `86e1569fefc74ca09642c4542af2d2ca4f28e5e609b04d1fd7120fe218c8cc1b`
- Transcript: `08/05/rollout-2026-08-05T04-10-21-019fcfaf-9fe6-7ed0-aded-a4e588859c1a.jsonl`

Implemented the PostgreSQL persistence hardening without touching service, handler, router, or UI code.

**Changed**
- `backend/migrations/pre/0049_outcome_attention_monitor.up.sql`
- `backend/migrations/pre/0049_outcome_attention_monitor.down.sql`
- `backend/migrations/outcome_attention_monitor_contract_test.go`
- `backend/migrations/outcome_attention_monitor_postgres_test.go`
- `backend/internal/ambientmonitor/repository_postgres.go`
- `backend/internal/ambientmonitor/repository_postgres_test.go`

**Hardened**
- Explicit bounded `lease_owner`; opaque deterministic `claim_id`.
- Immutable, workspace-scoped command idempotency ledger with exact result replay.
- Workspace-scoped observation/run idempotency and composite foreign keys.
- Governed disabling atomically revokes active leases.
- Stale workers fail revision and claim fencing.
- Append-only command, observation, and run ledgers.
- Canonical lowercase UUID target-ID contract.
- Updated rollback guards and dependency-safe drop order.

**Verification**
- Migration contract and fresh PostgreSQL lifecycle: passed.
- PostgreSQL repository tests against a separate fresh database: passed.
- `git diff --check`: passed.
- Exact results:
  - `ok automation-hub-backend/migrations 2.411s`
  - `ok automation-hub-backend/internal/ambientmonitor 0.106s`

No remaining gaps were found within the requested persistence scope. Nothing was committed.

## 019fcfb1-4315-7762-8744-f062ca1b6403

- Date: 2026-08-05
- Agent: Lovelace
- Work kind: implementation
- Original report SHA-256: `607b6cf704aceeb518ae810b64a7d4eb1501475fc504d7a329677f4c1af0ab24`
- Transcript: `08/05/rollout-2026-08-05T04-12-08-019fcfb1-4315-7762-8744-f062ca1b6403.jsonl`

Documented the advisory ambient outcome monitor across:

- `README.md`
- `docs/requirements-traceability.md`
- `docs/completion-matrix.md`
- `docs/acceptance-test-matrix.md`
- `docs/operator-runbook.md`
- `docs/migrations.md`
- `docs/domain-model.md`
- `docs/framework-operating-contract-matrix.md`
- `docs/roadmap.md`

Covered fixed collectors, immutable ledgers, durable scheduling, guarded routes, Governance Control, outcome/proactivity composition, environment variables, recovery procedures, and explicit no-authority boundaries. Acceptance evidence remains labeled **Required acceptance** until parent verification completes.

`.env.example` was reviewed and already documented the correct variables, so I left it unchanged. `git diff --check` passes. No tests were run because this was documentation-only, and no commit was created.

## 019fcfe4-4e41-7353-8b8b-a95aa74b7aa7

- Date: 2026-08-05
- Agent: Ptolemy
- Work kind: advisory
- Original report SHA-256: `fe5ab43770b87209c54a6994a0cf05045e11a00ae0a0bd938d9ff660a352554c`
- Transcript: `08/05/rollout-2026-08-05T05-07-53-019fcfe4-4e41-7353-8b8b-a95aa74b7aa7.jsonl`

**Audit Findings**
1. **Critical: composition can be permanently lost after monitor completion.**
   `Complete` atomically writes the observation/run, releases the lease, and advances `next_run_at` ([repository_postgres.go](../backend/internal/ambientmonitor/repository_postgres.go):455). Composition only starts after that transaction returns ([service.go](../backend/internal/ambientmonitor/service.go):397). A crash between those operations leaves no durable indication that composition is missing. The scheduler retry finds no due target because its schedule was already advanced.

2. **High: downstream idempotency is replay-safe but not discoverably recoverable.**
   Evaluation, signal, and decision writes use deterministic keys, but execute sequentially without a composition ledger ([composer.go](../backend/internal/ambientmonitor/composer.go):90). A partial write can be retried safely only if something already knows which run to replay.

3. **High: replay inputs are not completely immutable.**
   The composer reloads the current outcome revision. `CreateEvaluation` rejects revisions that are no longer current ([service.go](../backend/internal/outcomeevaluation/service.go):129). `EvaluateStored` reads the owner’s current policy, latest signals, decision history, and feedback ([service.go](../backend/internal/proactivity/service.go):206). A delayed first attempt can therefore produce different results from an immediate attempt.

4. **Medium: composition status is ephemeral.**
   `Completion.Composed` exists only in memory. `FindCompletion` always reconstructs it as false, while immutable runs store `signal_count = 0`. APIs cannot distinguish pending, partially composed, retrying, terminal, or dead-lettered runs.

5. **Medium: automatic target disabling is another unreceipted crash boundary.**
   `DisableTarget` occurs after all downstream writes. A crash can leave composition complete but the expired target enabled.

The specification explicitly requires transactional outbox, checkpoint/resume, leases, dead-letter isolation, reconciliation, orphan detection, and immutable ledgers ([specification](source-material/whole-life-framework-taxonomy.txt):621).

**Proposed State Machine**
Monitor completion transaction:

```text
claimed
  -> collected
  -> [atomic commit]
     observation appended
     monitor run appended
     composition intent appended
     composition work projection created
     target schedule advanced
```

Composition:

```text
pending_evaluation
  -> evaluation_recorded
  -> signal_recorded
  -> attention_refresh_queued
  -> target_lifecycle_reconciled
  -> completed
```

Operational overlays:

```text
any pending stage -> leased -> retry_wait -> leased
any pending stage -> dead_letter
dead_letter -> manually_requeued -> pending previous stage
```

Owner-attention evaluation should be a separate durable advisory job:

```text
queued -> snapshot_pinned -> decision_batch_recorded -> completed
```

This prevents a monitor run from claiming ownership of unrelated owner-wide proactivity decisions.

**Persistence Design**
Add two monitor tables:

- `outcome_monitor_compositions`: mutable coordination projection keyed by `run_id`; stage, attempt, next attempt, lease generation/ID/owner/expiry, pinned outcome revision/digest, composer version, and sanitized failure code.
- `outcome_monitor_composition_events`: append-only, hash-verifiable receipts for enqueue, each downstream result, retries, dead-lettering, manual requeue, lifecycle reconciliation, and completion.

Add an equivalent durable owner-attention work projection and immutable event ledger, or generalize these through the existing durable-job system.

The composition intent must be inserted in the same PostgreSQL transaction as the immutable run and observation. It should pin:

- Run and observation IDs and digests.
- Exact outcome revision and audit digest.
- History cutoff equal to `run.FinishedAt`.
- Composer contract/version.
- Expected downstream idempotency keys.
- Advisory-only authority record.

Keep `outcome_monitor_runs` immutable. Treat its existing `signal_count` as legacy rather than updating it after composition.

**Idempotency And Fencing**
- Composition identity: deterministic from `run.RecordDigest + composerVersion`.
- Evaluation key: `ambient-evaluation-<runDigest>`.
- Signal key: `ambient-signal-<runDigest>`.
- Attention refresh key: `ambient-attention-<runDigest>`.
- Target lifecycle key: `window-closed-<runDigest>`.
- Bind every key to an exact canonical request digest.
- Claim work with `FOR UPDATE SKIP LOCKED`; increment lease generation on every claim.
- Checkpoint writes require matching owner, workspace, composition ID, lease ID, generation, stage, and unexpired lease.
- A stale worker may finish a downstream idempotent write but cannot checkpoint it. The next worker replays the same key and receives the original record.
- One successful event per composition stage; failed attempts remain separate immutable events.
- Use deterministic exponential backoff with digest-derived jitter.
- Permanent scope, integrity, or authority failures dead-letter immediately. Transient repository failures consume a bounded retry budget.
- Reconciliation scans immutable completed runs lacking a composition intent or terminal receipt and repairs them idempotently.

**Repository Compatibility**
Keep existing repository methods intact and add a capability interface:

```go
type DurableCompositionRepository interface {
    Repository
    CompleteWithCompositionIntent(...)
    ClaimCompositionWork(...)
    RecordCompositionCheckpoint(...)
    ReconcileCompositionWork(...)
}
```

Both `MemoryRepository` and `PostgresRepository` implement it. Production startup must fail closed if durable scheduling is enabled without this capability.

Add, without replacing current APIs:

- `outcomeevaluation.GetOutcomeRevision(...)`
- `outcomeevaluation.CreateEvaluationForRevision(...)`
- `proactivity.EvaluateSnapshot(...)`, accepting pinned policy, signal, history, and feedback watermarks.

`RecordSignals` can remain unchanged because its existing idempotency contract is suitable.

**API Contract**
Preserve all current routes and add:

```text
GET  .../monitor/:targetId/runs/:runId/composition
GET  .../monitor/:targetId/compositions?state=pending|retry_wait|dead_letter
POST .../monitor/:targetId/runs/:runId/composition/retry
POST .../monitors/compositions/reconcile
```

The retry request contains only `idempotencyKey`, `expectedState`, and a bounded reason. It cannot select tools, recipients, channels, workflows, calendars, or actions.

Extend run responses additively:

```json
{
  "composition": {
    "state": "signal_recorded",
    "attempt": 2,
    "nextAttemptAt": "...",
    "evaluationId": "...",
    "signalId": "...",
    "failureCode": null,
    "authority": {
      "canExecute": false,
      "canDeliver": false,
      "canNotify": false,
      "canMutateWorkflow": false
    }
  }
}
```

Use `409` for state/idempotency conflicts, `423` when background processing is stopped, `202` for queued recovery, and sanitized error codes only.

**Required Tests**
1. Transaction rollback proves run, observation, intent, and schedule either all commit or none do.
2. Crash injection after every state transition listed above.
3. Crash after each downstream write but before its checkpoint.
4. Scheduler restart discovers committed pending composition without recollection.
5. Exact replay creates one evaluation, signal, decision batch, and lifecycle command.
6. Outcome revision changes after monitor completion do not alter the pinned evaluation.
7. Later owner signals, policies, feedback, or decisions do not change a pinned attention snapshot.
8. Two-worker and expired-lease tests prove stale workers cannot checkpoint.
9. Poison jobs dead-letter without blocking other compositions.
10. Reconciliation repairs orphaned historical completed runs.
11. Cross-owner/workspace access and foreign receipt injection fail.
12. Database constraints reject any authority, execution, delivery, notification, calendar, workflow, mandate, or learning capability.
13. API permission, emergency-stop, redaction, pagination, retry, and conflict tests.
14. PostgreSQL integration test kills the worker process at each crash boundary and resumes with another worker.
15. Browser test shows pending/retrying/dead-letter state without describing monitor completion as full composition success.

No files were edited during this audit.

## 019fcfe5-0a93-7253-8d08-344c9a518b4b

- Date: 2026-08-05
- Agent: Ampere
- Work kind: implementation
- Original report SHA-256: `74b743a7dfbeb460c37c890d401c5e8322d85bb25a330f707b31affd37e7f8da`
- Transcript: `08/05/rollout-2026-08-05T05-08-42-019fcfe5-0a93-7253-8d08-344c9a518b4b.jsonl`

Implemented migration `0050` without editing ambient-monitor Go code.

**Key behavior**
- One deterministic delivery per successful run, with `delivery_id = run_id`.
- Idempotent backfill for pre-`0050` successful runs.
- SHA-256 `binding_digest` via `pgcrypto`.
- Deferred enqueue trigger avoids conflicts with transactional Go inserts.
- Monotonic `lease_generation`, constrained to `<= revision`.
- Revision-fenced claims, release/reclaim, retries, and dead-lettering.
- Immutable attempt receipts and terminal deliveries.
- Strong owner/workspace/run/target/observation foreign keys.
- Advisory-only capability flags enforced as `false`.
- Delete and truncate blocked.
- Down migration refuses non-empty ledgers.

**Changed Files**
- [0050 up migration](../backend/migrations/pre/0050_outcome_monitor_composition_delivery.up.sql)
- [0050 down migration](../backend/migrations/pre/0050_outcome_monitor_composition_delivery.down.sql)
- [Contract tests](../backend/migrations/outcome_monitor_composition_delivery_contract_test.go)
- [PostgreSQL lifecycle tests](../backend/migrations/outcome_monitor_composition_delivery_postgres_test.go)

Verification passed:

- Focused migration contract and PostgreSQL lifecycle tests.
- Existing ambient-monitor PostgreSQL repository lifecycle.
- `git diff --check` for all four files.
- Pre-migration backfill, retry, success, dead-letter, stale fencing, immutability, and guarded rollback scenarios.

The files remain uncommitted.

## 019fcfe5-9d26-75a0-95d6-3dcc565a6aa7

- Date: 2026-08-05
- Agent: Gibbs
- Work kind: advisory
- Original report SHA-256: `b9f2894bb1863132ca3f4ec93fb5e114335318985bc3e82cc5cfbd5d847c69d8`
- Transcript: `08/05/rollout-2026-08-05T05-09-19-019fcfe5-9d26-75a0-95d6-3dcc565a6aa7.jsonl`

**Actionable Findings**

1. **[P0] Schedulers have no managed lifecycle or graceful shutdown.**
   Application startup calls a blocking `gin.Run` without a signal-aware root context or `http.Server.Shutdown`; schedulers are created during route assembly with `context.Background()` and return no stop/wait handle. Cancellation in the runner can also return while the handler goroutine continues running. Introduce one process context, start workers after route construction, stop accepting HTTP traffic on shutdown, cancel workers, and wait with a bounded timeout.
   [main.go](../backend/cmd/main.go):34
   [router.go](../backend/internal/router/router.go):43
   [routes.go](../backend/internal/router/routes.go):369
   [runner.go](../backend/internal/durablejob/runner.go):225

2. **[P1] Ambient target leases can expire while still being accepted.**
   A sweep captures one timestamp, claims up to 20 targets, and processes them sequentially. Each completion retains that original timestamp even though collection may happen minutes later. PostgreSQL validates lease expiry against this stale `run.FinishedAt`, not commit time, and ambient target leases have no heartbeat. Claim one target at a time or heartbeat target leases, derive `FinishedAt` after collection, and fence completion against database/current time.
   [durable_scheduler.go](../backend/internal/ambientmonitor/durable_scheduler.go):44
   [service.go](../backend/internal/ambientmonitor/service.go):447
   [repository_postgres.go](../backend/internal/ambientmonitor/repository_postgres.go):471

3. **[P1] A crash after monitor completion can permanently skip composition.**
   `Complete` durably stores the observation/run, releases the lease, and advances `next_run_at` before `Compose` executes. Only one immediate in-process replay exists. A crash between these operations, or two sink failures, leaves no durable pending marker; the next sweep sees the target as not due. Persist a composition outbox/receipt atomically with monitor completion.
   [service.go](../backend/internal/ambientmonitor/service.go):397
   [service.go](../backend/internal/ambientmonitor/service.go):464
   [repository_postgres.go](../backend/internal/ambientmonitor/repository_postgres.go):508
   [0049 migration](../backend/migrations/pre/0049_outcome_attention_monitor.up.sql):203

4. **[P1] Recurring jobs have a duplicate-schedule failure window.**
   `RegisterRecurring` enqueues the next occurrence before the current occurrence is marked terminal. If enqueue succeeds but `MarkSucceeded`/`MarkDead` fails or loses its lease, the current occurrence can run again and enqueue another future job. The returned `RowsAffected` boolean is ignored. Move terminal transition and next-occurrence creation into one repository transaction protected by the existing queue/kind advisory lock, and treat a false ownership result as lease loss.
   [runner.go](../backend/internal/durablejob/runner.go):152
   [runner.go](../backend/internal/durablejob/runner.go):212
   [repository.go](../backend/internal/durablejob/repository.go):84

5. **[P1] Emergency-stop permission is checked only once per sweep.**
   `allowed()` is evaluated before entering `runMonitorSweep`, then every discovered scope continues without another check. A safety-mode change during a long sweep therefore does not stop remaining background work. Check permission and context before every scope, target claim, and composition retry; stop cleanly without recording a task failure when permission is withdrawn.
   [durable_scheduler.go](../backend/internal/ambientmonitor/durable_scheduler.go):36
   [durable_scheduler.go](../backend/internal/ambientmonitor/durable_scheduler.go):50

6. **[P2] Worker failure is operationally invisible.**
   `Runner.Start` silently discards repository errors and exposes no readiness, last-success, last-error, or stopped state. The HTTP service can remain healthy while the monitor has stopped making progress. Return a managed worker handle and publish bounded lifecycle telemetry through readiness/diagnostics.
   [runner.go](../backend/internal/durablejob/runner.go):271
   [durable_scheduler.go](../backend/internal/ambientmonitor/durable_scheduler.go):69

**Minimal Integration Approach**

1. Add an `outcome_monitor_compositions` outbox keyed uniquely by `(owner, workspace, run_digest)`, containing immutable run/observation references, pinned outcome revision, status, retry time, attempt count, lease generation, and redacted failure code.
2. Insert its initial `pending` row in the same transaction that stores the successful monitor run and advances the target.
3. Keep the existing `outcome-monitor.sweep` as the only recurring schedule. Add a bounded `RetryDueCompositions` phase to that sweep; do not add another runner, ticker, or recurring job kind.
4. Claim composition rows with `FOR UPDATE SKIP LOCKED`, lease generations, expiry recovery, and backoff. Replay the existing narrow `Sink.Compose` interface using immutable records and pinned inputs.
5. Keep `MonitorRun` immutable. Record composition success and attempts in the separate receipt/attempt ledger rather than changing `signal_count`.
6. Preserve the current authority boundary: the retry worker receives no task, workflow, notification, calendar, mandate, delivery, execution, or learning interfaces.
7. Add failure-window tests for crash-after-completion, partial composition, multi-instance claims, expired composition leases, mid-sweep emergency stop, graceful shutdown, and enqueue-success/terminal-write-failure recurrence duplication.

## 019fcfe6-3104-7760-a32d-b10d0c99a638

- Date: 2026-08-05
- Agent: Turing
- Work kind: advisory
- Original report SHA-256: `5f282da81724b7bed8d83bce7be353df3e2506da97b0536b85e1d744aea00146`
- Transcript: `08/05/rollout-2026-08-05T05-09-57-019fcfe6-3104-7760-a32d-b10d0c99a638.jsonl`

**Audit Findings**
1. **Composition is not durably represented.** `Completion.Composed` exists only in the immediate response and is absent from persisted `MonitorRun`. A reload therefore cannot prove whether advisory composition succeeded. [types.go](../backend/internal/ambientmonitor/types.go):141, [service.go](../backend/internal/ambientmonitor/service.go):407

2. **Basic view can be misleading.** It shows the collection run as `Completed` even when downstream outcome/proactivity composition failed. Composition needs a separate status, not an extension of collection status. [governance-control.component.html](../frontend/src/app/pages/governance-control/governance-control.component.html):563

3. **Advanced disclosure is visual, not computational.** Loading the monitor automatically fetches 50 observations and 50 runs before Advanced history is opened. Composition history must not join this eager path. [governance-control.component.ts](../frontend/src/app/pages/governance-control/governance-control.component.ts):793, [governance-control.component.ts](../frontend/src/app/pages/governance-control/governance-control.component.ts):845

4. **History errors are coupled.** `forkJoin` means one failed history request prevents both histories from updating. Composition needs independent summary/history state and subscriptions.

5. **Failures currently depend on transient notifications.** The due-pass toast correctly reports failures, but durable composition failures cannot survive refresh until a composition ledger and read API exist.

6. **Current component tests do not render the template.** They instantiate the class directly, so Basic/Advanced visibility and accessible failure presentation are untested. [governance-control.component.spec.ts](../frontend/src/app/pages/governance-control/governance-control.component.spec.ts):289

**Smallest Contract**
```ts
type CompositionState =
  | 'not_started' | 'queued' | 'processing'
  | 'retry_scheduled' | 'succeeded' | 'needs_review';

interface MonitorCompositionSummary {
  contractVersion: 1;
  targetId: string;
  state: CompositionState;
  pendingCount: number;
  retryScheduledCount: number;
  needsReviewCount: number;
  latestSucceededAt?: string;
  latestAttempt?: {
    workItemId: string;
    attemptId?: string;
    status: CompositionState;
    attemptNumber: number;
    maxAttempts: number;
    finishedAt?: string;
    nextAttemptAt?: string;
    failureCode?: string;
    failureSummary?: string; // bounded and redacted by backend
  };
  generatedAt: string;
  authority: AmbientMonitorAuthority;
}

interface MonitorCompositionHistoryEntry {
  item: {
    id: string;
    monitorRunId: string;
    observationId: string;
    state: CompositionState;
    queuedAt: string;
    completedAt?: string;
    recordDigest: string;
  };
  attempts: Array<{
    id: string;
    attemptNumber: number;
    status: 'succeeded' | 'failed';
    startedAt: string;
    finishedAt: string;
    failureCode?: string;
    failureSummary?: string;
    failureRedacted: boolean;
    outputs: Array<{
      kind: 'outcome_evaluation' | 'proactivity_signal' | 'proactivity_decision';
      id: string;
      recordDigest: string;
    }>;
    recordDigest: string;
  }>;
}
```

Use two read-only endpoints:

- `GET .../monitor/:targetId/composition`
- `GET .../monitor/:targetId/composition/history?limit=25`

Do not expose worker IDs, leases, idempotency keys, stack traces, provider payloads, or a retry button until a governed retry command exists.

**Progressive Presentation**
- Basic: one `Advisory handoff` status beside the existing monitor summary.
- Healthy: green `Current`, with latest success time only.
- Queued: blue `Pending`.
- Retry scheduled: amber with the next retry time.
- Terminal failure: compact red alert containing the bounded code, timestamp, and `Inspect failure`.
- Status unavailable: explicit neutral warning; never infer healthy from missing data.
- Failure inspection opens a drawer without forcing the whole module into Advanced mode.
- Advanced: independent `outcome-monitor-composition` progressive section containing queue entries, attempts, redacted failure detail, output references, and record digests.
- Load Advanced history only when that section opens.

**Exact Tests**
`ambient-monitor.service.spec.ts`:
- `loads composition summary from the fully encoded target route`
- `loads bounded composition history and clamps limit to 100`
- `composition reads use GET and send no body or client authority fields`

`governance-control.component.spec.ts`:
- `loads composition summary for the selected target without loading composition history`
- `maps persisted composition states without deriving success from a completed monitor run`
- `keeps a terminal composition failure visible after a due pass`
- `refreshes composition summary after successful, partial, and failed due passes`
- `does not mark composition healthy when its summary request fails`
- `loads composition history only when its Advanced section opens`
- `preserves loaded summary when composition history fails`
- `ignores stale composition responses after scope or target changes`
- `clears composition state and subscriptions when monitor scope resets`

Add a compiled-template spec:
- `Basic view renders one calm handoff status and no attempt history`
- `Basic view renders terminal failure as role alert with Inspect failure`
- `retry scheduled is cautionary and not rendered as terminal failure`
- `Advanced view renders persisted attempts and output references`
- `raw worker, lease, idempotency, and stack-trace fields are never rendered`

No files were edited.

## 019fcffa-6fd6-73c0-b427-7ae6ac8cf798

- Date: 2026-08-05
- Agent: Mill
- Work kind: implementation
- Original report SHA-256: `15704a9ffb2c788e6372bbee56f66f7e651b88b1cf279f0eb5ea5fabb56101e6`
- Transcript: `08/05/rollout-2026-08-05T05-32-04-019fcffa-6fd6-73c0-b427-7ae6ac8cf798.jsonl`

Implemented focused PostgreSQL integration coverage in [composition_repository_postgres_test.go](../backend/internal/ambientmonitor/composition_repository_postgres_test.go):19.

Covered:

- Atomic monitor completion and composition enqueue
- Idempotent completion replay
- One-winner concurrent claims
- Expired lease recovery and stale-worker fencing
- Failed attempt, scheduled retry, then success
- Append-only attempt update/delete rejection
- Owner/workspace isolation
- Exact attempt and delivery projections

No concrete production repository bug was found, so `composition_repository_postgres.go` and `repository_postgres.go` were not modified.

Verification against live PostgreSQL with migration `0050`:

- Focused lifecycle: passed
- Existing and new PostgreSQL lifecycles with `-race`: passed
- Concurrent lifecycle repeated 10 times: passed
- Formatting and whitespace checks: passed

No migrations, frontend files, documentation, Git state, or Playwright artifacts were changed.

## 019fcffa-fca8-7de0-9203-4cb9ccc8c787

- Date: 2026-08-05
- Agent: Boyle
- Work kind: implementation
- Original report SHA-256: `f059821fa9b643d59b81571e3e568249fa0f2c12fba873d603368f47deda6024`
- Transcript: `08/05/rollout-2026-08-05T05-32-40-019fcffa-fca8-7de0-9203-4cb9ccc8c787.jsonl`

Added focused backend coverage in:

- [handler_test.go](../backend/internal/ambientmonitor/handler_test.go):211
  - Owner/workspace/target isolation
  - Attempt-to-delivery/target binding
  - Advisory-only authority flags
  - Invalid limits for both composition endpoints
  - New routes included in route-surface checks
- [durable_scheduler_test.go](../backend/internal/ambientmonitor/durable_scheduler_test.go):55
  - Permission withdrawal after the first scoped batch stops further recovery and processing

Verification:

- Four targeted tests passed.
- Complete `internal/ambientmonitor` package passed.
- No production files were changed.
- No production bug was exposed by these tests.
- No frontend, migrations, documentation, Git operations, or Playwright artifacts were touched.

## 019fcffb-862c-7b22-8ddc-2c650d266d11

- Date: 2026-08-05
- Agent: Nash
- Work kind: implementation
- Original report SHA-256: `b14af9aba0e65d49d60291a908c59341f0f9ae8b0f61409fe8622d3d93ef20b7`
- Transcript: `08/05/rollout-2026-08-05T05-33-15-019fcffb-862c-7b22-8ddc-2c650d266d11.jsonl`

Updated only the requested documentation and migration-chain test:

- [migrations.md](migrations.md)
- [completion-matrix.md](completion-matrix.md)
- [acceptance-test-matrix.md](acceptance-test-matrix.md)
- [requirements-traceability.md](requirements-traceability.md)
- [migration_chain_contract_test.go](../backend/migrations/migration_chain_contract_test.go)

The documentation now distinguishes collection completion from composition success, covers pending/retry/dead-letter states and immutable attempt receipts, preserves the advisory-only authority boundary, and records the unpinned outcome/proactivity snapshot limitation.

Verification passed:

```text
ok  automation-hub-backend/migrations  0.037s
```

This covered migration pairing/order, the governance tail through `0050`, and the 0050 SQL contract. No production Go, frontend, Git operations, or Playwright artifacts were touched.

## 019fd017-f6be-7891-925c-976cee4cd673

- Date: 2026-08-05
- Agent: Pauli
- Work kind: implementation
- Original report SHA-256: `a8279f84621d522fe808be00d0cc4865734c89d75c777ff9db2925c3c7a5eb41`
- Transcript: `08/05/rollout-2026-08-05T06-04-19-019fd017-f6be-7891-925c-976cee4cd673.jsonl`

Implemented and verified migration 0051.

**Key behavior**
- Pinned deliveries store exact outcome, policy, attention history, watermarks, and Go-compatible snapshot/binding digests.
- Legacy backfills and trigger fallbacks are immediately `dead_lettered`.
- Legacy rows use `last_failure_code = 'snapshot_unavailable'`, have `completed_at`, and cannot retry.
- Legacy composer version matches Go: `ambient-monitor-composer/pre-0051-unknown`.
- Attempt receipts immutably pin the delivery snapshot digest.
- Mutation, deletion, truncation, and non-empty rollback remain rejected.
- Advisory execution authority remains disabled.

**Changed paths**
- [0051 up migration](../backend/migrations/pre/0051_outcome_monitor_composition_snapshot.up.sql)
- [0051 down migration](../backend/migrations/pre/0051_outcome_monitor_composition_snapshot.down.sql)
- [Contract tests](../backend/migrations/outcome_monitor_composition_snapshot_contract_test.go)
- [PostgreSQL lifecycle tests](../backend/migrations/outcome_monitor_composition_snapshot_postgres_test.go)
- [Migration chain contract](../backend/migrations/migration_chain_contract_test.go)

**Validation**
- Migration package tests: passed.
- Disposable PostgreSQL lifecycle: passed.
- Empty rollback, legacy backfill, trigger-only fallback, pinned insertion, digest equality, forged snapshot rejection, attempt pinning, immutability, and rollback refusal all passed.
- No Go or frontend files edited.
- Nothing committed.

## 019fd018-b9eb-7623-98ea-c696a60cc1cf

- Date: 2026-08-05
- Agent: Lorentz
- Work kind: implementation
- Original report SHA-256: `eb603337080e9ecd56a88dfaf5a1b5883a97a2a96de409122d7607380d8f25a0`
- Transcript: `08/05/rollout-2026-08-05T06-05-09-019fd018-b9eb-7623-98ea-c696a60cc1cf.jsonl`

Implemented exact historical outcome revision reads without touching excluded areas.

**API Contract**
```go
type OutcomeRevisionResolver interface {
    ResolveOutcomeRevision(
        context.Context,
        ownerID, workspaceID, outcomeID string,
        revision int64,
        auditDigest string,
    ) (OutcomeRevision, error)
}

func (s *Service) ResolveOutcomeRevision(...) (OutcomeRevision, error)
```

Behavior:

- Requires revision `> 0` and lowercase SHA-256 audit digest.
- Resolves exact owner, workspace, outcome, revision, and digest.
- Never falls back to the current revision.
- Revalidates scope and `VerifyOutcomeRevisionDigest`.
- Returns `ErrNotFound` for mismatched selectors or scope.
- Returns `ErrIntegrityViolation` for corrupted matching records.
- Memory exact reads remain available after bounded history trimming.
- PostgreSQL uses an exact compound-selector query.

**Changed Paths**
- [repository.go](../backend/internal/outcomeevaluation/repository.go):21
- [service.go](../backend/internal/outcomeevaluation/service.go):97
- [repository_postgres.go](../backend/internal/outcomeevaluation/repository_postgres.go):146
- [exact_revision_test.go](../backend/internal/outcomeevaluation/exact_revision_test.go):1
- [repository_postgres_integration_test.go](../backend/internal/outcomeevaluation/repository_postgres_integration_test.go):107
- [repository_postgres_test.go](../backend/internal/outcomeevaluation/repository_postgres_test.go):96

Package tests, `go vet`, race tests, and disposable PostgreSQL lifecycle tests pass. The full backend run remains blocked by unrelated existing undefined types in `internal/proactivity`. No commit was created.

## 019fd019-6dd5-7fc3-9ffa-042c28c9779f

- Date: 2026-08-05
- Agent: Leibniz
- Work kind: implementation
- Original report SHA-256: `458a4201d5c6fd9092c95a2f8de33e33975edd308bb9fdacb43776eb7bfafecc`
- Transcript: `08/05/rollout-2026-08-05T06-05-55-019fd019-6dd5-7fc3-9ffa-042c28c9779f.jsonl`

Implemented the exact, bounded proactivity snapshot contract and the timestamp integration fix. No commit was created.

**Exported API**
```go
type EvaluateStoredSnapshotRequest struct {
    IdempotencyKey    string
    Now               time.Time
    Snapshot          EvaluationSnapshot
    AdditionalSignals []OpenLoopSignal
}

func VerifyEvaluationSnapshot(
    owner string,
    snapshot EvaluationSnapshot,
) error

func (s *Service) CaptureEvaluationSnapshot(
    ctx context.Context,
    owner string,
    at time.Time,
) (EvaluationSnapshot, error)

func (s *Service) EvaluateStoredSnapshot(
    ctx context.Context,
    owner string,
    request EvaluateStoredSnapshotRequest,
) (DecisionBatch, bool, error)
```

`EvaluationSnapshot` exports the exact policy idempotency key, payload digest, recorded time, signal/decision/feedback cursors, bounded window digests, capture time, and complete input digest.

**Behavior**
- Capture and evaluation times are canonical UTC microseconds.
- Policy and nested signal/decision/feedback cursor times use the same storage-stable precision.
- Snapshot replay resolves the exact immutable policy and bounded ledger windows.
- Later stored records are excluded.
- One or more explicit `AdditionalSignals` are normalized, owner-checked and deduplicated.
- Additional signals are evaluated without being persisted.
- Their canonical digest is bound into decision idempotency.
- Owner violations, cursor inconsistencies, digest mismatches and authority-bearing records fail closed.
- Existing `EvaluateStored` behavior remains separate.
- Memory and PostgreSQL repositories implement the same contract.

**Changed Paths**
- [snapshot.go](../backend/internal/proactivity/snapshot.go)
- [snapshot_repository.go](../backend/internal/proactivity/snapshot_repository.go)
- [snapshot_repository_postgres.go](../backend/internal/proactivity/snapshot_repository_postgres.go)
- [repository.go](../backend/internal/proactivity/repository.go)
- [repository_postgres.go](../backend/internal/proactivity/repository_postgres.go)
- [service.go](../backend/internal/proactivity/service.go)
- [snapshot_test.go](../backend/internal/proactivity/snapshot_test.go)
- [repository_postgres_test.go](../backend/internal/proactivity/repository_postgres_test.go)

**Verification**
- Proactivity tests: passed.
- Race detector: passed.
- `go vet ./internal/proactivity`: passed.
- Live PostgreSQL lifecycle test: passed.
- Timestamp regression covering non-UTC nanosecond input and JSON round-trip: passed.
- Whole-backend verification was attempted but is currently blocked by unrelated concurrent changes in `ambientmonitor`, router wiring, and migration tests. Those files were not modified.

## 019fd01a-1538-79b1-833b-bc8f872ea7ef

- Date: 2026-08-05
- Agent: Anscombe
- Work kind: partial-report
- Original report SHA-256: `37bb465956c7dabb634adf18a5603c57c1be6466814de3291385f45198a9ef88`
- Transcript: `08/05/rollout-2026-08-05T06-06-38-019fd01a-1538-79b1-833b-bc8f872ea7ef.jsonl`

Implemented progressive composition provenance in Governance Control.

- Basic handoff shows composer version and exact outcome revision.
- Advanced inspector shows complete snapshot, policy, cutoff, structured watermarks, and shortened digests.
- Supports both anticipated flat JSON and current nested attention-snapshot format.
- Legacy snapshots explicitly show “revision not pinned.”
- Missing provenance is reported honestly without inference.
- Existing lazy attempt loading and status behavior remain unchanged.

Verification:

- Focused tests: `45/45` passed.
- Production Angular build passed.
- Rebuilt frontend container is healthy.
- Existing soft bundle/style-budget warnings remain.
- No backend, migrations, global styles, or documentation edited.
- Nothing committed or pushed.
- In-app visual QA could not attach from this background task; production template compilation passed.

Changed files: [ambient-monitor.model.interface.ts](../frontend/src/app/models/ambient-monitor.model.interface.ts), [governance-control.component.ts](../frontend/src/app/pages/governance-control/governance-control.component.ts), [HTML](../frontend/src/app/pages/governance-control/governance-control.component.html), and [tests](../frontend/src/app/pages/governance-control/governance-control.component.spec.ts).

## 019fd028-9409-7030-ab89-9c1a84d5cad4

- Date: 2026-08-05
- Agent: Schrodinger
- Work kind: advisory
- Original report SHA-256: `a3c538088cef11bebdd27e47af4e9cbe44d83fd8f69077eac6e2b17735fa6db0`
- Transcript: `08/05/rollout-2026-08-05T06-22-28-019fd028-9409-7030-ab89-9c1a84d5cad4.jsonl`

**Audit Findings**
1. **P0: PostgreSQL cannot persist or reload the new snapshot contract.** The completion insert omits every 0051 snapshot column, despite the trigger requiring `attention_snapshot` and an exact digest ([repository_postgres.go](../backend/internal/ambientmonitor/repository_postgres.go):516, [0051 up](../backend/migrations/pre/0051_outcome_monitor_composition_snapshot.up.sql):482). The select, row projection, and decoder also omit the snapshot ([composition_repository_postgres.go](../backend/internal/ambientmonitor/composition_repository_postgres.go):14, [decoder](../backend/internal/ambientmonitor/composition_repository_postgres.go):366).

   **Action:** serialize `Attention` to JSON and insert every snapshot scalar/digest. Extend the select/row/decoder and verify the persisted snapshot equals the requested snapshot.

2. **P0: Snapshot capture errors are silently converted into legacy rows.** `Service.Complete` ignores `captureErr` and retains `legacy_unpinned` ([service.go](../backend/internal/ambientmonitor/service.go):277). Workers subsequently reject that record as `snapshot_unavailable`.

   **Action:** expose capture failure explicitly. Either persist a deliberate review/dead-letter state or fail completion without silently changing provenance. Use an injected clock in snapshot tests.

3. **P1: `CaptureSnapshot` performs a policy write before full input validation.** It calls `ensureDefaultPolicy`, which may record a policy, before composition inspects historical observations ([composer.go](../backend/internal/ambientmonitor/composer.go):79, [policy creation](../backend/internal/ambientmonitor/composer.go):188). The cross-scope test confirms this unwanted side effect.

   **Action:** make capture read-only. Seed the default policy during onboarding/configuration, or return `policy_unavailable` without mutation.

4. **P1: SQL validates attention cursor shape, but not its cryptographic contents.** It regex-checks `inputDigest` and `windowDigest`, then checks counts and head cursors ([0051 up](../backend/migrations/pre/0051_outcome_monitor_composition_snapshot.up.sql):180, [signal checks](../backend/migrations/pre/0051_outcome_monitor_composition_snapshot.up.sql):219). It does not recompute the three window digests or `inputDigest`; Go does ([snapshot.go](../backend/internal/proactivity/snapshot.go):341).

   **Action:** reproduce the canonical proactivity digest calculation in SQL, or enforce an application-only insertion boundary and add database tamper tests covering historical records, window digests, and input digest.

5. **P1: Legacy and Go deliveries use different binding-digest algorithms.** Go binds delivery identity plus `SnapshotDigest` using canonical JSON ([composition_delivery.go](../backend/internal/ambientmonitor/composition_delivery.go):144). The SQL run trigger uses pipe-concatenation without the snapshot digest ([0051 up](../backend/migrations/pre/0051_outcome_monitor_composition_snapshot.up.sql):834).

   **Action:** use one binding algorithm or store a binding-contract version. Also choose one enqueue owner: explicit Go enqueue for pinned records and a clearly defined trigger path for legacy fallback.

6. **P1: Attempt receipts carry the snapshot but remain only partially self-verifying.** The migration now adds `snapshot_digest` with a delivery FK ([0051 up](../backend/migrations/pre/0051_outcome_monitor_composition_snapshot.up.sql):701), and Go compares it during settlement ([composition_repository_postgres.go](../backend/internal/ambientmonitor/composition_repository_postgres.go):245). However, decoding only validates digest syntax; it does not recompute `RequestDigest` or `RecordDigest` ([decoder](../backend/internal/ambientmonitor/composition_repository_postgres.go):400).

   **Action:** recompute both receipt digests on read and reject mismatches as corrupt storage.

7. **P2: Composer versions now match, but the compatibility proof is not a default CI gate.** Go and SQL both use `ambient-outcome-attention-v2` ([composition_delivery.go](../backend/internal/ambientmonitor/composition_delivery.go):16, [0051 up](../backend/migrations/pre/0051_outcome_monitor_composition_snapshot.up.sql):539). The cross-language PostgreSQL digest test requires destructive-test environment variables and was skipped in the normal migration run.

   **Action:** run the isolated PostgreSQL 0051 lifecycle test in CI with a disposable database.

**Verification**
- `go test ./migrations`: passed; live PostgreSQL lifecycle was environment-gated.
- `go test ./internal/ambientmonitor`: failed five tests, primarily from silent legacy fallback plus the policy-write side effect.
- No files were edited.

## 019fd060-5df4-77e0-949f-e4f6e182944d

- Date: 2026-08-05
- Agent: Halley
- Work kind: advisory
- Original report SHA-256: `372a86d93292d2876b8781fd95e804a97f54f5da0d6ad949a8ab869dacbd19d9`
- Transcript: `08/05/rollout-2026-08-05T07-23-24-019fd060-5df4-77e0-949f-e4f6e182944d.jsonl`

No files were edited.

**Implemented Operationally**
- **Human sovereignty:** versioned owner constitution, protected rules, activation history, execution-policy checks, approval boundaries, and emergency stop. See [frameworkregistry/service.go](../backend/internal/frameworkregistry/service.go):608, [constitution_execution_policy.go](../backend/internal/frameworkregistry/constitution_execution_policy.go):1, and [constitution_execution_policy_test.go](../backend/internal/frameworkregistry/constitution_execution_policy_test.go):1.
- **Whole-life operations:** canonical 24-domain taxonomy, domain links, need observations, capacity snapshots, exact 12-level goal hierarchy, and all 25 prioritization factors. See [lifeops/types.go](../backend/internal/lifeops/types.go):13, [lifeops/service.go](../backend/internal/lifeops/service.go):417, and [lifeops/service_test.go](../backend/internal/lifeops/service_test.go):1.
- **Intake and execution plumbing:** source extraction can create governed workflow candidates with deduplication, source links, evidence claims, checklists, reminders, review gates, transitions, and audit events. Task planning consumes operating context and capacity. See [workflow/service.go](../backend/internal/workflow/service.go):500, [source/service.go](../backend/internal/source/service.go):1988, and [task/service.go](../backend/internal/task/service.go):966.
- **Operational UI/API:** owner-authenticated LifeOps routes and a `/life-ops` management interface exist.

**Catalog / Reference Only**
- Maslow, ERG, PERMA, SDT, Capability Approach, GTD, OODA, RICE, WSJF, AHP and related frameworks are registered as descriptive catalogs/contracts, but most are not executable assessment or selection engines.
- Wellbeing needs are free-text observations rather than model-specific, structured assessments.
- Several sovereignty principles remain policy prose unless expressible as deny, approval, or authority-ceiling rules.

**Top 3 Gaps**
1. **Conflicting canonical ontologies:** `lifeops` has 24 domains, while `lifeontology` uses 11 collapsed domains and immutable projections. There is no authoritative binding joining source facts, memories, pursuits, workflows, tasks, and goal nodes. See [lifeontology/types.go](../backend/internal/lifeontology/types.go):38.
2. **No unified intake-to-priority chain:** workflow intake uses keyword classification, basic entity extraction, limited deadline parsing, and a separate additive priority score. It does not automatically create a LifeOps goal, derive need/capacity context, or run the 25-factor priority engine.
3. **Constraints are unevenly enforced:** capacity and priority logic are meaningful but path-dependent. Fixed weighted scoring can numerically trade away health, safety, legal obligations, consent, or dignity; other workflow/proactivity paths bypass it entirely.

**Recommended Slice**
Build a **governed source-to-prioritized-pursuit pipeline**:

1. Persist an immutable, digest-bound triage envelope containing provenance, typed entities, commitments, deadlines, contradictions, 24-domain assignments, inferred/stated needs, and review status.
2. On approval, atomically create or link the Pursuit, corresponding `pursuit`-level GoalNode, domain links, workflow, and source evidence.
3. Derive the 25 priority factors from evidence plus the latest capacity/need snapshots; preserve evidence and confidence per factor.
4. Apply hard sovereignty, health, safety, legal, and consent constraints before weighted prioritization.
5. Pass exact triage, constitution, capacity, goal, and priority snapshot digests into planning and verification.

This single slice connects all seven specification families and replaces today’s parallel models with one auditable operational chain.

Fresh package tests were not completed because Go was unavailable on the shell path and exploration was stopped. The HAI containers were healthy, and `git diff --check` found only line-ending warnings, not whitespace errors.

## 019fd061-2113-7111-9b17-20db46597839

- Date: 2026-08-05
- Agent: McClintock
- Work kind: advisory
- Original report SHA-256: `e4f3223f44584af051a7e30130859767b31db30156226eb46b158996826dea55`
- Transcript: `08/05/rollout-2026-08-05T07-24-14-019fd061-2113-7111-9b17-20db46597839.jsonl`

**Audit Summary**

| Families | Status | Evidence |
|---|---|---|
| **8. Organizations** | **Partially operational** | HAI selects and persists modes including sequential, hierarchical, parallel-specialist, and debate/critic configurations in [operating_context.go](../backend/internal/frameworkregistry/operating_context.go):595. Team lifecycle, membership and consensus records are durable, but teams do not execute work. |
| **9. Agent identity** | **Operational contract** | Durable owner-scoped agent registry, capabilities, authority ceilings, allowlists, health, availability, reliability and assignments exist in [types.go](../backend/internal/agentregistry/types.go):92. CRUD, transitions and assignments have authenticated routes in [routes.go](../backend/internal/router/routes.go):884 and UI controls. Rich agent cards are partly synthesized rather than independently attested records. |
| **10. Delegation** | **Operational contract, no delivery** | Delegation envelopes contain objectives, authority, constraints, budgets, evidence and lifecycle transitions in [types.go](../backend/internal/agentcoordination/types.go):97. Tasks generate delegation plans, but no production worker consumes them. |
| **11. Communication** | **Operational schema, transport missing** | Typed correlated messages, confidentiality, expiry, acknowledgement, provenance and idempotency are defined in [types.go](../backend/internal/agentcoordination/types.go):12. The coordinator can dispatch through interfaces, but only test implementations of `Transport` and `DispatchStore` were found. |
| **12. Coordination** | **Advisory operational** | Durable teams, consensus outcomes and coordination previews exist. The task integration explicitly enriches plans without granting execution authority in [agent_team_context.go](../backend/internal/task/agent_team_context.go):12. No real fan-out/fan-in agent run occurs. |
| **13. Reasoning** | **Catalog/reference-only** | ReAct, Reflexion, Self-Refine, Tree/Graph of Thoughts and related methods are catalog entries. Selection records a broad reasoning family, but the chosen method does not alter prompts, execution loops or validation. |
| **14. Cognitive architectures** | **Partial operational loop** | [agentcycle/service.go](../backend/internal/agentcycle/service.go):126 runs source, workflow, ambient, pursuit and memory maintenance passes. BDI, OODA, MAPE-K and other named architectures remain catalog concepts rather than selectable stateful engines. |
| **15. Uncertainty** | **Narrow implementation** | Reliability scoring, duration ranges, calibration and uncertainty review thresholds exist. General expected-utility, value-of-information, regret and sensitivity decision engines are only described in catalog contracts. |
| **16. Planning** | **Partially operational** | The resource planner performs dependency ordering, capacity scheduling, deadlines, critical-path/slack, budgets and replanning. The task bridge largely creates sequential dependencies; HTN, STRIPS/PDDL, partial-order planning, A*, MCTS and MIP are not implemented engines. |
| **17. Workflow modeling** | **Strongest operational family** | Persistent workflow items, transitions, approvals, leases, retries, evidence and completion attestations exist in [workflow.go](../backend/internal/models/workflow.go):9. The BPMN-like [workflowgraph/types.go](../backend/internal/workflowgraph/types.go):10 supports parallelism and compensation structurally, but has no production repository, routes or runtime integration. |

**Catalog Boundary**

The 55-entry framework taxonomy explicitly exists independently of implementation in [family_taxonomy.go](../backend/internal/frameworkregistry/family_taxonomy.go):8. Therefore catalog presence, selection metadata and dashboard presentation are not evidence that the corresponding reasoning or organizational method executes.

**Top 3 Gaps**

1. **No real agent dispatch loop:** assignments, delegation envelopes and messages stop before a production transport delivers work to a registered runtime and returns evidence.
2. **Selections do not control cognition:** selected reasoning and cognitive methods do not change planning, prompting, review, retry or validation behavior.
3. **Advanced workflow graph is disconnected:** parallel branches, joins, timers and compensation are modeled but not persisted or driven by the active workflow worker.

**Recommended Slice**

Build one **governed two-agent delegation run**:

`task intake → select team/mode → reserve two registered agents → persist delegation → dispatch typed messages to local runtimes → collect correlated evidence → fan-in verifier review → approval gate → workflow completion or review → update reliability and audit records`

This uses the strongest existing contracts, closes the families 8–12 execution gap, and provides the substrate needed to prove later reasoning, uncertainty and planning methods end to end.

No files were edited. Focused Go tests were not run because Go is unavailable on the host PATH; this conclusion is based on current source, route, persistence and UI wiring inspection.

## 019fd063-6caf-71a3-a1bc-f465676d9bcb

- Date: 2026-08-05
- Agent: Cicero
- Work kind: advisory
- Original report SHA-256: `3a68d9a8c7cdcac4abb772f5a31eff5432065c5aecf62c6cc1e8f2570181ffc5`
- Transcript: `08/05/rollout-2026-08-05T07-26-44-019fd063-6caf-71a3-a1bc-f465676d9bcb.jsonl`

**Operationally Implemented**
- Shared authenticated shell with 25 lazy-loaded routes, persistent theme/navigation/view preferences, and guarded access: [app-routing.module.ts](../frontend/src/app/app-routing.module.ts):7.
- Sovereignty and governance: versioned Constitution, framework selection, preferences, mandates, authorization receipts, controlled learning, domain packs, agent registry, life ledger, outcome monitoring, and proactivity.
- Life operations: needs, capacity, goal hierarchy, priority evaluation, pursuits, intake, portfolio planning, workflows, approvals, retries, reminders, and reviewed execution.
- Knowledge plane: connected-source management, memory CRUD/retrieval, grounded-answer generation, claim review/correction, provenance, and verification history.
- Runtime/model plane: local-first LLM policy, budgets, provider probes, calibration, model benchmarks, runtime safety, emergency stop, health, and readiness.
- These are real Angular service calls to authenticated backend contracts, not static mock data.

**Catalog Or Reference Only**
- Framework families 51-55 remain experimental integrations. Brain Catalog mainly provides status, probes, revalidation, recommendations, and export controls. Catalog presence does not activate a tool.
- Most personal packs 39-50 are policy/domain templates operating through generic pursuits, sources, workflows, and approvals. Finance evidence and work delivery have stronger implementations; health, legal, relationships, travel, home/assets, and emergency-domain automation lack dedicated end-to-end adapters.
- `/hai-os` is a read-only architecture/readiness aggregator, not an operational engine surface: [hai-os.component.ts](../frontend/src/app/pages/hai-os/hai-os.component.ts):27.

**Top Three Gaps**
1. **Progressive disclosure is incomplete.** Registry `primaryAction` and `advancedSectionIds` metadata are never consumed outside tests. Only seven page templates use `hai-progressive-section`; remaining Basic/Advanced behavior relies partly on global page-specific CSS selectors: [module-registry.ts](../frontend/src/app/control-room/module-registry.ts):10, [styles.scss](../frontend/src/styles.scss):1338.

2. **Backend capability exceeds frontend exposure.** Governance only lists agent teams, while the backend supports versioning, lifecycle, messages, delegation assessment, and consensus. Capacity history, knowledge-claim assessment, and parts of memory-engine search/conversation inspection are similarly underexposed: [governance-control.service.ts](../frontend/src/app/services/governance-control.service.ts):252, [agent_team_handler.go](../backend/internal/frameworkregistry/agent_team_handler.go):56.

3. **There is no authoritative unified plan graph.** Planning is split across task plans, pursuits, workflows, an opaque one-lane optimizer proposal, and optional Temporal follow-ups. The optimizer explicitly cannot modify workflows or calendars: [planningoptimizer/service.go](../backend/internal/planningoptimizer/service.go):1.

**Recommended Next Slice**
Build **Plan and Coordination** end to end, but first add its missing backend dependency: an owner-scoped, immutable, revisioned plan-graph contract containing dependency edges, temporal constraints, resource estimates, critical path, framework/evidence digests, assigned agents, approval state, workflow/run bindings, and repair history.

Then add:
- `GET /plans`, `GET /plans/:id`, `POST /plans/preview`, `POST /plans/:id/accept`, and `POST /plans/:id/replan`.
- Basic UI: next executable action, blockers, owner, approval requirement, and schedule.
- Advanced UI: dependency graph, timeline, capacity, agent coordination, evidence, revisions, and execution trace.
- Inspector linking every plan node to its pursuit, workflow, authorization, sources, and verification.

No files were edited. The requested frontend test run was interrupted after seven seconds, so this is a static contract audit rather than a fresh green-suite claim.

## 019fd06b-a244-7f71-bb34-37af1773f85c

- Date: 2026-08-05
- Agent: Archimedes
- Work kind: partial-report
- Original report SHA-256: `ce65914f8f8880ec7e6691ef52c57bf1149148fd577f2fbf62c206795f209553`
- Transcript: `08/05/rollout-2026-08-05T07-35-42-019fd06b-a244-7f71-bb34-37af1773f85c.jsonl`

Stopped. Implementation files are preserved and uncommitted.

**Changed Files**
- `backend/internal/plangraph/types.go`
- `backend/internal/plangraph/validation.go`
- `backend/internal/plangraph/digest.go`
- `backend/internal/plangraph/repository.go`
- `backend/internal/plangraph/repository_postgres.go`
- `backend/internal/plangraph/service.go`
- `backend/internal/plangraph/handler.go`
- `backend/internal/plangraph/service_test.go`
- `backend/internal/plangraph/handler_test.go`
- `backend/internal/plangraph/repository_postgres_test.go`
- `backend/migrations/pre/0052_plan_graph_contract.up.sql`
- `backend/migrations/pre/0052_plan_graph_contract.down.sql`
- `backend/migrations/plan_graph_contract_test.go`

**Implemented**
- Owner-isolated immutable plan revisions.
- DAG validation and cycle rejection.
- Deterministic SHA-256 plan and request digests.
- Preview, accept, replan, list, and get operations.
- Optimistic revision conflict detection.
- Idempotent preview, accept, and replan behavior.
- Memory and PostgreSQL repositories.
- Bounded authenticated HTTP handlers with safe errors.
- Repair provenance and operational bindings.
- Explicit advisory-only contract with no execution authority.
- PostgreSQL update/delete/truncate protection.

**Verification**
- Plan-graph tests passed with the race detector.
- `go vet ./internal/plangraph` passed.
- Migration contract test passed.
- Migration applied in an isolated PostgreSQL database.
- Insert succeeded and immutable-update protection was verified.
- All backend packages passed except the known migration-tail assertion below.

**Known Issues**
1. The existing migration-chain test still expects the migration sequence to end at `0051`; it must be updated to include `0052_plan_graph_contract`.
2. Handler methods exist, but `/plans` routes are not mounted in the central router because the assigned scope allowed only new files.
3. Migration `0052` has not been applied to the live HAI database.
4. No frontend plan-graph interface has been added.
5. Files remain untracked and have not been staged, committed, or pushed.

## 019fd06c-2cca-7ab2-809d-faa9f9f80536

- Date: 2026-08-05
- Agent: Feynman
- Work kind: partial-report
- Original report SHA-256: `8a6d6293d5266c9d3a9bb2ffa2b2f85d3bb7639876345b6ff9fcf82300eadabe`
- Transcript: `08/05/rollout-2026-08-05T07-36-18-019fd06c-2cca-7ab2-809d-faa9f9f80536.jsonl`

Stopped. Files preserved; nothing committed or pushed.

**Changed Files**
- [plan-graph.model.interface.ts](../frontend/src/app/models/plan-graph.model.interface.ts)
- [plan-graph.service.ts](../frontend/src/app/services/plan-graph.service.ts)
- [plan-graph.service.spec.ts](../frontend/src/app/services/plan-graph.service.spec.ts)
- [plan-coordination.module.ts](../frontend/src/app/pages/plan-coordination/plan-coordination.module.ts)
- [plan-coordination.component.ts](../frontend/src/app/pages/plan-coordination/plan-coordination.component.ts)
- [plan-coordination.component.html](../frontend/src/app/pages/plan-coordination/plan-coordination.component.html)
- [plan-coordination.component.scss](../frontend/src/app/pages/plan-coordination/plan-coordination.component.scss)
- [plan-coordination.component.spec.ts](../frontend/src/app/pages/plan-coordination/plan-coordination.component.spec.ts)

**Known Issues**
- The page is intentionally not added to routing, navigation, or the module registry, per the assigned file restrictions.
- The latest focused test rerun was interrupted after a small API-alignment change. The preceding run passed `10/10` tests.
- Production build passed before that final small change, but the new lazy module is not yet reachable and therefore was not included in the production bundle.
- The backend currently exposes only immutable `draft`/`accepted` revisions and node-level planning data. It does not expose computed critical path, complete revision history, rich constraints, or execution authorization.
- Backend acceptance checks revision, not an expected digest. Plans remain advisory with `canExecute=false`.
- The frontend derives summaries only from returned node data and deliberately avoids fabricated metrics.

## 019fd06d-2629-7ba0-976a-9535ae978360

- Date: 2026-08-05
- Agent: Averroes
- Work kind: advisory
- Original report SHA-256: `af7cd2f3f3e2589bf925079f432c4de1710857355537c90ebbdc83ae88cd002a`
- Transcript: `08/05/rollout-2026-08-05T07-37-22-019fd06d-2629-7ba0-976a-9535ae978360.jsonl`

**Integration Finding**
No `plangraph` package, API, or frontend route currently exists. The repository already has a capable deterministic planning kernel in [resourceplanner](../backend/internal/resourceplanner/types.go):1 and immutable accepted portfolio allocations. `plangraph` should unify their provenance and execution bindings, not introduce another scheduling algorithm.

**Backend Checklist**
1. Create `backend/internal/plangraph/` with `types.go`, `validation.go`, `digest.go`, `repository.go`, `repository_postgres.go`, `service.go`, `handler.go`, and focused tests.
2. Reuse `resourceplanner.Request` and `Decision`, including dependencies, resources, schedules, blockers, approval flags, replanning reasons, and digests at [types.go](../backend/internal/resourceplanner/types.go):106.
3. Preserve its advisory boundary: it cannot execute, consume approval, or access external systems [planner.go](../backend/internal/resourceplanner/planner.go):14.
4. Model immutable plan revisions, nodes, dependency edges, resource allocations, evidence bindings, agent assignments, decisions, repair history, and workflow/task/run bindings.
5. Bind each revision digest to owner, workspace, plan ID, revision, parent revision, normalized graph, framework selection, evidence preflight, capacity snapshot, goal/priority context, algorithm version, and creation time.
6. Require `expectedRevision` and `expectedDigest` for acceptance and replanning. Reject stale writes with `409 Conflict`.
7. Define narrow dependency interfaces inside `plangraph`; adapt pursuit, workflow, LifeOps, framework, and agent services in the router to avoid import cycles.
8. Instantiate the service only after the final pursuit decorators at [routes.go](../backend/internal/router/routes.go):521, but before task composition at [routes.go](../backend/internal/router/routes.go):632.
9. If task execution must be graph-bound, add `task.WithPlanGraph(...)` before `planningPreview` and `workflowRunner.Set` at [routes.go](../backend/internal/router/routes.go):684.
10. Add the import beside `planningoptimizer` at [routes.go](../backend/internal/router/routes.go):70.
11. Register:
   - `GET /api/v1/plans`
   - `GET /api/v1/plans/:id`
   - `POST /api/v1/plans/preview`
   - `POST /api/v1/plans/:id/accept`
   - `POST /api/v1/plans/:id/replan`
12. Apply `PermRead` to reads and pure previews, `PermWrite` to proposed revisions, and `PermApprove` to acceptance. Follow the explicit owner/role guard pattern at [routes.go](../backend/internal/router/routes.go):811.
13. Register static paths before `/:id` paths to prevent Gin route conflicts.
14. Derive owner identity exclusively from authenticated context. Never accept it from request JSON; the frontend already tests this boundary at [life-ops.service.spec.ts](../frontend/src/app/services/life-ops.service.spec.ts):53.

**Migration Checklist**
15. Recheck the migration tail immediately before editing. It is currently `0051`; the likely next pair is `pre/0052_plan_graph.up.sql` and `.down.sql`.
16. Add append-only tables for revisions, nodes, edges, decisions, bindings, and repairs. Use owner/workspace composite keys and no cascading deletion.
17. Add database constraints for bounded payload sizes, contiguous positive revisions, valid digest formats, node/edge ownership, endpoint existence, and accepted-revision consistency.
18. Add triggers rejecting update, delete, and truncate on immutable evidence.
19. Make rollback refuse non-empty plan ledgers, following [0051 down migration](../backend/migrations/pre/0051_outcome_monitor_composition_snapshot.down.sql):1.
20. Add `0052_plan_graph` to the exact governance tail in [migration_chain_contract_test.go](../backend/migrations/migration_chain_contract_test.go):90.
21. Add migration contract and disposable-PostgreSQL lifecycle tests.
22. Do not rely on GORM AutoMigrate. Versioned SQL is authoritative [database.go](../backend/internal/infra/database.go):47.
23. Prefer package-local SQL row types. If shared GORM models are introduced, add them to the development-only list beginning at [database.go](../backend/internal/infra/database.go):81.
24. No embed change is needed; `pre/*.sql` is already embedded automatically at [embed.go](../backend/migrations/embed.go):16.

**Frontend Checklist**
25. Add `models/plan-graph.model.ts`, `services/plan-graph.service.ts`, and their tests.
26. Create a lazy `pages/plan-coordination/` module following [life-ops.module.ts](../frontend/src/app/pages/life-ops/life-ops.module.ts):13.
27. Add authenticated `/plans` routing inside the shell’s route collection at [app-routing.module.ts](../frontend/src/app/app-routing.module.ts):7.
28. Register “Plan coordination” in the Work group near Pursuits and Workflows at [module-registry.ts](../frontend/src/app/control-room/module-registry.ts):17.
29. Suggested Advanced section IDs: `dependency-graph`, `timeline`, `capacity`, `agent-coordination`, `evidence`, `revisions`, and `execution-trace`.
30. Add registry assertions beside the existing uniqueness and route tests at [module-registry.spec.ts](../frontend/src/app/control-room/module-registry.spec.ts):3.
31. No shell navigation edit is required; it renders directly from the registry at [app-shell.component.html](../frontend/src/app/control-room/app-shell.component.html):10.
32. Import `ControlRoomModule` and use `hai-progressive-section`; it already persists module-specific disclosure state [progressive-section.component.ts](../frontend/src/app/control-room/progressive-section.component.ts):9.
33. Use `NzDrawerModule` for node inspection. The planned shared `HaiInspectorDrawer` does not currently exist.
34. Basic view should show only next executable node, blockers, owner, approval status, and schedule. Load graph topology, evidence, revisions, and traces only when Advanced sections open.
35. Registry `primaryAction` metadata is not rendered by the shell, so the page must provide its own real Preview/Replan action.

**Documentation And Verification**
36. Add traceability after the current portfolio planning rows in [requirements-traceability.md](requirements-traceability.md):34.
37. Update formal planning and workflow-modeling status at [framework-operating-contract-matrix.md](framework-operating-contract-matrix.md):35.
38. Add implementation, migration, rollback, and authority boundaries to `docs/completion-matrix.md` and [migrations.md](migrations.md):41.
39. Do not claim operational execution until task/workflow records persist and independently revalidate the exact plan revision and digest.
40. Run focused backend tests for `./internal/plangraph`, `./internal/router`, and `./migrations`, then full `go test ./...`.
41. Run Angular service/component/registry tests, production build, Compose validation, migration status/up, authenticated API acceptance, restart persistence, and browser Basic/Advanced checks.
42. Use the documented Windows/Docker commands at [README.md](../README.md):686.

**Primary Pitfalls**
- `routes.go`, frontend routing, module registry, and requirement ledgers are already modified in the dirty worktree. Patch around current content; never replace whole files.
- Do not create a competing scheduler beside `resourceplanner` or a second acceptance ledger beside pursuit allocations.
- A graph shown in the UI is not execution provenance unless task/workflow/run records bind and revalidate its exact immutable revision.
- Acceptance must not silently grant execution authority.
- Replanning must append a new revision, never rewrite accepted history.
- Tests proving deterministic internal behavior are not evidence of external calendar, provider, or distributed-worker correctness.

No files were edited and no tests were run during this read-only review.

## 019fd0aa-4683-75c1-a789-2b489dbb42ac

- Date: 2026-08-05
- Agent: Aristotle
- Work kind: advisory
- Original report SHA-256: `1ccf8bb2489a7554a93293db09bf0de1d1692e504c83f12d78bb528ba9b67631`
- Transcript: `08/05/rollout-2026-08-05T08-44-08-019fd0aa-4683-75c1-a789-2b489dbb42ac.jsonl`

**Audit Findings**

1. **Task orchestration is not yet connected to Plan Graph.** `task.IntakeRequest`, `CompletionPlan`, and `service` contain no accepted-plan binding or resolver. The current path is `Handler.Run` → `service.Run` → `withTaskOperation` → `runOperation` → `executeAllowedSteps`.

2. **The Plan Graph side is already prepared.** [`plangraph.AcceptedRevisionReference`](../backend/internal/plangraph/types.go):92, `AcceptedRevisionBinding`, `AcceptedRevisionResolver`, and [`Service.ResolveAccepted`](../backend/internal/plangraph/service.go):203 now exist. They enforce:
   - Authenticated owner scope
   - Exact latest revision and digest
   - Accepted status
   - Exact node ID
   - `CanExecute == false`
   - Superseded revisions fail closed

3. **A node ID is mandatory.** `planId`, revision, and digest identify a plan container, but not the planned task. The existing `NodeID` requirement correctly prevents an arbitrary task from citing an unrelated node.

4. **Approval persistence currently excludes plan provenance.** [`ReviewRequestDigest`](../backend/internal/task/state_repository.go):139 hashes pursuit, workflow, request, project, automation, mandate, and criteria, but no Plan Graph reference. An approval would therefore not presently bind a specific plan revision.

5. **Execution authorization does not yet record the accepted plan.** [`executionGovernanceEvidence`](../backend/internal/task/governance_binding.go):19 creates `TaskPlanDigest`, but its canonical payload does not contain Plan Graph provenance.

**Minimum Integration**

In [`task/service.go`](../backend/internal/task/service.go):38:

- Add a task-facing binding with `planId`, `planRevision`, `planDigest`, and `planNodeId`.
- Add it to `IntakeRequest`.
- Store the verified `plangraph.AcceptedRevisionBinding` on `CompletionPlan`.
- Add `planResolver plangraph.AcceptedRevisionResolver` to `service`.
- Inject it through a focused `WithAcceptedPlanResolver` configurator to avoid changing every constructor.
- Do not add it to `ExecuteAllowed`, `HumanApproved`, approval provenance, or mandate fields.

In [`task/state_repository.go`](../backend/internal/task/state_repository.go):121:

- Add the reference to `storedReviewRequest`.
- Include all four fields in `ReviewRequestDigest`.
- Preserve them through `encodeStoredReviewRequest` and `decodeStoredReviewRequest`.
- Existing immutable JSON payloads mean no database migration is required for minimum correctness.

In [`task/service.go`](../backend/internal/task/service.go):684:

- Resolve the reference at the start of `runOperation`, before `buildPlan`.
- Re-resolve from the verified binding immediately before `toolExecutor.Execute` to close the replan race.
- Approved review reruns already call `s.Run`, so they will automatically be revalidated.
- Completed idempotency replays should return their historical result without executing or requiring the old plan to remain current.

In [`task/governance_binding.go`](../backend/internal/task/governance_binding.go):85:

- Include plan ID, revision, digest, and node ID in the canonical task-governance payload.
- Add an evidence reference such as `plan-graph://<id>/revisions/<revision>/nodes/<node>#sha256:<digest>`.
- Continue carrying it through the existing `ToolExecutionRequest.Governance` and `automation.TaskLaunchRequest.Governance`; separate execution-authority fields are unnecessary.

In [`router/routes.go`](../backend/internal/router/routes.go):133:

- Retain `planGraphService := plangraph.NewService(...)` instead of constructing it only inside the handler.
- Inject that same instance into `taskService`.

**Owner Boundary**

HTTP owner identity is correctly derived from the verified JWT principal:

- `identityMiddleware` sets `identity.ContextSubjectKey`.
- `requireAuthenticatedOwner` rejects missing principals.
- [`requireTaskOwner`](../backend/internal/task/handler.go):139 reads that subject.
- `Handler.Run` overwrites `request.OwnerIdentity`.
- Plan resolution must use this owner directly, never the `taskStateOwnerIdentity` internal fallback.

**Gates That Must Remain**

- `PermApprove` on `/task/run`
- Client approval-field stripping in `Handler.Run`
- Durable idempotency, leases, fencing, and uncertain-outcome review
- Pursuit lifecycle guard
- Emergency stop, including final-boundary rechecks
- Autonomy gate and action resolver
- Framework risk, capacity, autonomy ceiling, and per-action policy
- Resource feasibility/reservations
- Framework/source-evidence preflight
- Durable task/workflow approval verification
- Action-bound approval proof issuance and one-use consumption
- Constitution, standing mandate, agent assignment, tool/data/folder allowlists
- `executionauth.AuthorizeAndConsume`
- Agent-runtime final-effect proof
- Output verification before completion or memory updates

**Focused Test Matrix**

| Test | Expected result |
|---|---|
| Exact latest accepted owner/node/revision/digest | Binding returned, `CanExecute=false` |
| Draft plan | `ErrPlanNotAccepted`; executor untouched |
| Foreign owner | `ErrNotFound`; no information leak |
| Wrong node | `ErrReferenceInvalid` |
| Wrong digest or revision | `ErrReferenceStale` |
| Replan after initial resolution | Final pre-effect resolution fails; executor untouched |
| Missing resolver with a supplied reference | Fail closed |
| Partial reference | Validation failure before operation |
| Same idempotency key, changed plan reference | `ErrTaskStateConflict` |
| Review serialization round trip | Exact reference retained |
| Approved review after plan becomes stale | Execution blocked despite approval |
| Accepted high-risk plan without approval | Existing approval gate still blocks |
| Accepted low-risk plan | Still passes every executionauth boundary |
| Governance digest after changing plan reference | Digest changes |
| Completed operation replay after later replan | Historical result returned; no second execution |
| Accepted plan only | Does not set `HumanApproved`, `ApprovalGranted`, or execution authority |

No files were changed and no tests were run during this read-only audit.

## 019fd0ab-bc61-7920-bb6b-c8a49630f6a7

- Date: 2026-08-05
- Agent: Noether
- Work kind: advisory
- Original report SHA-256: `0f406dc79128b77c64fdb85171e3928e2d4eaba3efa4ef35132532deda2d5ca8`
- Transcript: `08/05/rollout-2026-08-05T08-45-43-019fd0ab-bc61-7920-bb6b-c8a49630f6a7.jsonl`

**Audit Result**
The workflow path is strongly approval-gated, but immutable coordination-plan binding is currently missing. Plan Graph ends at its API registration in [routes.go](../backend/internal/router/routes.go):133; its accepted revision is not carried by `WorkflowItem`, the workflow worker, task planning, or final execution authorization.

**Required Binding**
Use three explicitly non-authoritative fields:

```go
CoordinationPlanID       uuid.UUID
CoordinationPlanRevision uint64
CoordinationPlanDigest   string
```

They must always be all present or all absent.

1. Add them to `models.WorkflowItem` in [workflow.go](../backend/internal/models/workflow.go):9.
2. Add them to `workflow.IntakeRequest` and `workflow.TaskRunRequest` in [service.go](../backend/internal/workflow/service.go):45.
3. Include them in `workflowSourceRevision`, preventing source deduplication from silently retaining an obsolete plan.
4. Include them in `workflowAPIIntakeSourceID` in [handler.go](../backend/internal/workflow/handler.go):119.
5. Propagate them through `workflowtask.Runner.RunWorkflowTask` into `task.IntakeRequest` in [runner.go](../backend/internal/workflowtask/runner.go):318.
6. Include the digest in `workflowTaskOperationKey`; otherwise a new accepted revision can replay a task result created from an older plan.
7. Add them to `executionauth.GovernanceEvidence` in [types.go](../backend/internal/executionauth/types.go):234 and to the deterministic task-governance digest in [governance_binding.go](../backend/internal/task/governance_binding.go):19.

**Resolver**
Add this narrow method to `plangraph.Service`:

```go
ValidateAcceptedRevision(
    ctx context.Context,
    owner string,
    planID uuid.UUID,
    revision uint64,
    digest string,
) error
```

It should use [Service.Get](../backend/internal/plangraph/service.go):190 to require:

- Exact owner, ID, revision, and digest match.
- `StatusAccepted`.
- `CanExecute == false`.
- The referenced revision is also the latest revision.
- A newer draft or accepted revision makes the workflow binding stale and review-required.

Reuse one `planGraphService` instance in `routes.go`; currently it is constructed inline and discarded.

**Safest Revalidation**
Three checks are warranted:

1. In `workflow.Intake`, before `CreateItem`, to reject invalid or foreign bindings.
2. In `runWorkflowItem`, immediately after the atomic worker claim and again before completion is attested.
3. Most importantly, in `executionauth.Service.recheck` immediately before receipt consumption in [service.go](../backend/internal/executionauth/service.go):632. This closes the replan-after-preflight race before an external side effect.

A failed check should call the existing review-required path, release the worker claim, and never retry execution blindly.

**Owner Identity**
The authoritative owner sources are already correct:

- API intake overwrites client input using `identity.ContextSubjectKey` through [verifiedWorkflowOwner](../backend/internal/workflow/handler.go):437.
- Background workers use persisted `WorkflowItem.OwnerIdentity`.
- The plan resolver must always receive that trusted owner, never a JSON-supplied owner.

**Mandatory Existing Gates**
Plan acceptance must not set `HumanApproved`, `ApprovalStatus`, `ExecuteAllowed`, or autonomy authority. These existing gates remain mandatory:

- Workflow approval guard and durable `WorkflowDecision`.
- Exact approval provenance from `workflowApprovalProof` in [service.go](../backend/internal/workflow/service.go):2300.
- Exact automation action binding from `PrepareWorkflowApprovalBinding`.
- Task-side approval verification in `verifiedApprovalDecisionForExecution`.
- Active standing-mandate evaluation, owner/revision/digest checks, and expiry/revocation recheck.
- Emergency stop, Constitution, framework selection, source evidence, agent/tool/folder allowlists, risk and autonomy ceilings.
- `executionauth.AuthorizeAndConsume` in [service.go](../backend/internal/executionauth/service.go):387.
- Runtime verification, quality gates, immutable launch evidence, and completion attestation.

The coordination-plan digest should also enter the automation action digest so replanning invalidates an earlier case approval. It remains approval context, never approval itself.

**Database Contract**
Add migration `0053_workflow_coordination_plan_binding` with:

- Nullable all-or-none workflow columns.
- Positive revision and SHA-256 checks.
- Unique plan key `(owner_identity, plan_id, revision, digest)`.
- Composite workflow foreign key to that exact immutable Plan Graph row.
- No cascade deletion.
- Existing Plan Graph mutation triggers remain untouched.

**Focused Tests**
Add tests for:

- Exact latest accepted revision succeeds with `CanExecute == false`.
- Draft, foreign-owner, wrong digest, missing resolver, and superseded revision fail closed.
- Partial three-field bindings are rejected.
- Intake persistence and source deduplication include the binding.
- Worker passes the exact binding to preview and run.
- Operation identity changes when the plan digest changes.
- Replanning between claim and task run prevents execution.
- Replanning during execution prevents completion and final receipt consumption.
- Old approval cannot authorize a changed plan revision.
- Accepted plan without workflow approval still enters `needs_approval`.
- Standing mandate and accepted plan together still require all execution-authorization checks.
- PostgreSQL rejects partial, foreign-owner, nonexistent, and digest-mismatched references.

No files were edited and no tests were run; this was a read-only audit.

## 019fd0ac-d369-7780-8e26-85235d836e2b

- Date: 2026-08-05
- Agent: Jason
- Work kind: advisory
- Original report SHA-256: `e4c82561c7badf7721ee868b48454c23f2cc2b08ef9dfb924ea46fce89c365a0`
- Transcript: `08/05/rollout-2026-08-05T08-46-55-019fd0ac-d369-7780-8e26-85235d836e2b.jsonl`

**Audit Findings**

1. **Accepted Plan Graph revisions are not currently bound to pursuit orchestration.**
   `PortfolioPlanningRequest.PlanID` is only an opaque resource-planner identifier. `PlanPortfolioForOwner` never resolves `plangraph.Plan`, checks its revision/digest, or verifies pursuit-node bindings. See [portfolio_planning.go](../backend/internal/pursuit/portfolio_planning.go):29.

2. **The Plan Graph service is not injected into the pursuit service.**
   `routes.go` constructs it inline for its HTTP handler, then separately constructs `pursuitService`. See [routes.go](../backend/internal/router/routes.go):133 and [service.go](../backend/internal/pursuit/service.go):772.

3. **Existing pursuit records already form a strong immutable chain.**
   Allocation → proposal → proposal item → decision → dispatch → authorization receipt → workflow. Parent record digests are propagated correctly. The accepted Plan Graph revision should be added at the allocation root rather than creating another orchestration system.

4. **Plan acceptance correctly grants no execution authority.**
   `plangraph.Plan.CanExecute` is forced false, and validation rejects executable plans. See [types.go](../backend/internal/plangraph/types.go):88 and [validation.go](../backend/internal/plangraph/validation.go):108.

**Minimum Implementation**

1. Add `(*plangraph.Service).ResolveAcceptedLatest(ctx, owner, id, revision, digest)` in [service.go](../backend/internal/plangraph/service.go):184. It must require:
   - Exact owner-scoped ID, revision, and digest.
   - `StatusAccepted`.
   - `CanExecute == false`.
   - The referenced revision is still the latest revision.
   - A newer draft therefore invalidates the old accepted revision for new work.

2. Add pursuit-local `AcceptedPlanRevisionResolver`, `PlanRevisionBinding`, `WithAcceptedPlanRevisionResolver`, and `service.acceptedPlanResolver` in [service.go](../backend/internal/pursuit/service.go):772.

3. Extend `PortfolioPlanningRequest` with required `AcceptedPlan PlanRevisionBinding`. In `PlanPortfolioForOwner`, verify every requested pursuit has a matching `Node.Bindings.PursuitID` in the exact accepted revision.

4. Extend `PursuitPortfolioAllocation` with:
   - `PlanRevision uint64`
   - `PlanDigest string`

   Keep existing `PlanID`, but require it to contain the Plan Graph UUID for newly accepted allocations. Include all three fields in `digestPortfolioAllocation`.

5. Add a migration after `0052` that permits old rows to remain explicitly legacy-unbound but requires all-or-none valid plan binding for new rows. Do not invent revision/digest values for historical allocations.

6. Update these snapshot types to include the parent allocation:
   - `portfolioExecutionProposalDecisionSnapshot`
   - `PortfolioWorkflowEffectApprovalSnapshot`
   - `portfolioDispatchCoordinationEvidence`

   Load it through `proposal.AllocationID` and verify `proposal.AllocationRecordDigest`.

7. Revalidate the plan binding in:
   - `AcceptPortfolioAllocationForOwner`
   - `PreparePortfolioExecutionProposalsForOwner`
   - `DecidePortfolioExecutionProposalItemForOwner` before an `approved` decision
   - `AuthorizePortfolioWorkflowEffectForOwner`
   - `ExecutePortfolioWorkflowEffectForOwner` immediately before first receipt consumption

8. Preserve recovery semantics: if the exact receipt was already consumed, allow idempotent recovery of that same effect. A superseded plan must block new authorization or first consumption, but must not strand an already-consumed effect.

9. Retain one `planGraphService` variable in `routes.go`, use it for the handler, and inject it into `pursuitService`.

**Existing Gates To Preserve**

- Owner identity comes only from `identity.ContextSubjectKey` through `pursuitOwner`/`verifiedActor`, never JSON.
- Routes require authenticated ownership plus RBAC `PermRead`, `PermApprove`, or `PermExecute`.
- Allocation acceptance recalculates the deterministic portfolio decision and checks freshness, feasibility, blockers, budget, capacity, dependencies, resource ceilings, and exact confirmation.
- Proposal decisions are append-only, digest-bound, and expire after 30 minutes.
- `executionauth` independently resolves approval, binds the exact effect digest, issues a receipt, and permits single consumption.
- Workflow creation remains `RequiresReview: true`; downstream execution still requires its own approval.
- Verified completion and resource settlement remain separate.

**Focused Tests**

Add tests covering:

- `ResolveAcceptedLatest`: exact match, foreign owner, draft, wrong digest, wrong revision, superseded accepted revision, `CanExecute` invariant.
- Portfolio planning rejects missing/unbound/foreign accepted plans.
- Allocation digest includes exact plan revision and digest.
- Proposal preparation rejects a superseded plan.
- Approval cannot be recorded against stale plan evidence.
- Authorization receives no call when plan revalidation fails.
- First receipt consumption fails after plan supersession.
- Already-consumed exact effect still recovers idempotently.
- Dispatch coordination reports `stale_plan` without granting authority.
- PostgreSQL migration enforces all-or-none binding, owner isolation, and immutability.
- Router composition injects the same Plan Graph service into HTTP and pursuit orchestration.

No files were changed. I could not run the focused Go tests because `go.exe` is unavailable in the current shell and no running Compose test target was available.

## 019fd0be-9c67-7402-abcd-a7d4d8495184

- Date: 2026-08-05
- Agent: Beauvoir
- Work kind: implementation
- Original report SHA-256: `a290dd8f7b1a96803ef63ef849e5ca7cdb35ccd372ac4a85555c60d90f55bb22`
- Transcript: `08/05/rollout-2026-08-05T09-06-20-019fd0be-9c67-7402-abcd-a7d4d8495184.jsonl`

Implemented the bounded Plan Graph enhancement.

Changed:
- [types.go](../backend/internal/plangraph/types.go):106: added full `Nodes` and `Edges` to `AcceptedRevisionBinding`.
- [service.go](../backend/internal/plangraph/service.go):243: `ResolveAccepted` now returns defensive graph clones while preserving `CanExecute=false`.
- [validation.go](../backend/internal/plangraph/validation.go):271: added deep node cloning, including timestamp pointers.
- [service_test.go](../backend/internal/plangraph/service_test.go):205: verifies complete graph availability, mutation isolation, and absence of execution authority.

Verification passed:

```text
go test ./internal/plangraph
ok automation-hub-backend/internal/plangraph 1.134s
```

Nothing was staged or committed, and no files outside `backend/internal/plangraph/*.go` were edited.

## 019fd0bf-33fc-7773-b9e1-b3ef4d8c1681

- Date: 2026-08-05
- Agent: Mendel
- Work kind: implementation
- Original report SHA-256: `d92508feccc0d75ab89e1d60d21c8c4ca87b8c5b68102b5ab4bb91bd236c6c2f`
- Transcript: `08/05/rollout-2026-08-05T09-06-59-019fd0bf-33fc-7773-b9e1-b3ef4d8c1681.jsonl`

Implemented the durable pursuit portfolio allocation binding.

Changes:
- Added the four optional coordination fields to [pursuit.go](../backend/internal/models/pursuit.go):254.
- Added [0054 up migration](../backend/migrations/pre/0054_pursuit_portfolio_coordination_plan_binding.up.sql).
- Added [0054 safe down migration](../backend/migrations/pre/0054_pursuit_portfolio_coordination_plan_binding.down.sql).
- Added [migration contract tests](../backend/migrations/pursuit_portfolio_coordination_plan_binding_contract_test.go).
- Added `0054` to the [migration chain test](../backend/migrations/migration_chain_contract_test.go):132.

The database now enforces:
- Legacy unbound rows remain valid.
- Bindings are all-present or all-absent.
- Revision is positive and digest is lowercase SHA-256.
- Exact owner/plan/revision/digest composite FK.
- Referenced revision must be accepted.
- Updates and deletes are `RESTRICT`.
- Rollback refuses to remove non-empty bindings.

Verification passed:
- `go test ./migrations -count=1`
- `go test ./internal/models`

Nothing was staged or committed. A live PostgreSQL migration was not applied.

## 019fd0bf-d215-7000-bcb1-7c0ec54a96fe

- Date: 2026-08-05
- Agent: Maxwell
- Work kind: advisory
- Original report SHA-256: `0f0d9fbaeb3f7435d33a16e6ea8a3535e7d77836d3c939984a8a26f5bfab405c`
- Transcript: `08/05/rollout-2026-08-05T09-07-40-019fd0bf-d215-7000-bcb1-7c0ec54a96fe.jsonl`

**Critical Finding**
`plan_binding.go` already reads `CoordinationPlanID`, `CoordinationPlanRevision`, `CoordinationPlanDigest`, and `CoordinationPlanNodeID`, and migration `0054` creates those columns. However, the current [PursuitPortfolioAllocation model](../backend/internal/models/pursuit.go):241 does not define them.

This is a deterministic compile mismatch and must be corrected before downstream integration. The fields must also remain part of `digestPortfolioAllocation`, as already expected in [portfolio_acceptance.go](../backend/internal/pursuit/portfolio_acceptance.go):407.

**Execution Chain**
| Boundary | Current function | Smallest safe insertion |
|---|---|---|
| Proposal preparation | [PreparePortfolioExecutionProposalsForOwner](../backend/internal/pursuit/portfolio_execution_proposal.go):140 | After allocation evidence and expected digest validation at lines 173-178, before proposal construction/replay lookup. Resolve `coordinationReferenceForAllocation(snapshot.Allocation)` and verify every allocation pursuit remains represented in the accepted revision. |
| Proposal approval | [DecidePortfolioExecutionProposalItemForOwner](../backend/internal/pursuit/portfolio_execution_decision.go):70 | Only for `approved`, after source/item/state validation at lines 108-124 and before replay/new decision creation. Do not require plan freshness for reject or revoke, otherwise a stale plan could prevent revocation. |
| Dispatch | [DispatchPortfolioWorkflowsForOwner](../backend/internal/pursuit/portfolio_dispatch.go):241 | Revalidate only for non-terminal items, after the terminal-result skip at lines 318-321 and before `dispatchOnePortfolioWorkflow`. This preserves completed/replayed dispatch inspection when the plan later changes. |
| Per-item dispatch | [dispatchOnePortfolioWorkflow](../backend/internal/pursuit/portfolio_dispatch.go):341 | No separate authority logic should be added. It should continue delegating to authorization and consumption; those boundaries must independently revalidate the plan. |
| Authorization receipt | [AuthorizePortfolioWorkflowEffectForOwner](../backend/internal/pursuit/portfolio_workflow_authorization.go):247 | After current approval validation at lines 287-294, before building the effect and issuing the receipt. |
| Canonical authorization recheck | [PortfolioWorkflowEffectApprovalResolver.Resolve](../backend/internal/pursuit/portfolio_workflow_authorization.go):151 | After `validatePortfolioWorkflowEffectApproval` and before `buildPortfolioWorkflowEffect`. This is the strongest central location because `executionauth.Authorize` uses it initially and `AuthorizeAndConsume` invokes it again through `recheck`. |
| First receipt consumption | [ExecutePortfolioWorkflowEffectForOwner](../backend/internal/pursuit/portfolio_workflow_authorization.go):398 | In the `ErrNotFound` consumption branch, after approval validation at lines 491-493 and immediately before `AuthorizeAndConsume`. Require the exact plan to still be latest and accepted here. |
| Consumed-effect recovery | Same function, lines 480-537 | In the `consumptionErr == nil` branch, verify immutable historical plan provenance but do not require that revision to remain latest. Authority already comes from the matching receipt and consumption record. Requiring latest-plan freshness here could strand an already-consumed effect. |

**Required Snapshot Changes**
The proposal snapshot already carries the correct parent:

- [portfolioExecutionProposalSnapshot](../backend/internal/pursuit/portfolio_execution_proposal.go):50 contains `Allocation`.
- [LoadPortfolioExecutionProposalSnapshot](../backend/internal/pursuit/portfolio_execution_proposal_repository.go):19 loads it owner-scoped.

The downstream snapshots do not carry it:

- Add `Allocation models.PursuitPortfolioAllocation` to [portfolioExecutionProposalDecisionSnapshot](../backend/internal/pursuit/portfolio_execution_decision.go):48.
- Load it through `Proposal.AllocationID` in [loadPortfolioExecutionProposalDecisionSnapshot](../backend/internal/pursuit/portfolio_execution_decision_repository.go):65.
- Add the same field to [PortfolioWorkflowEffectApprovalSnapshot](../backend/internal/pursuit/portfolio_workflow_authorization.go):101.
- Copy it in [LoadPortfolioWorkflowEffectApprovalSnapshot](../backend/internal/pursuit/portfolio_execution_decision_repository.go):14.
- Add allocation evidence to `portfolioDispatchCoordinationEvidence`, or have `LoadPortfolioDispatchProposal` return the allocation alongside the proposal and items.

**Receipt And Recovery Structures**
The single-use boundary is correctly separated:

- [executionauth.Receipt](../backend/internal/executionauth/types.go):275
- [executionauth.Consumption](../backend/internal/executionauth/types.go):309
- [Service.AuthorizeAndConsume](../backend/internal/executionauth/service.go):387
- [loadPortfolioWorkflowEffect](../backend/internal/pursuit/portfolio_workflow_authorization.go):582 recovers an existing receipt-linked workflow.
- `workflow.Intake` deduplicates by receipt source identity, but currently resolves accepted-plan freshness before deduplication.

For interrupted post-consumption recovery, a narrow trusted workflow recovery method is needed. It should accept only a matching consumed receipt plus the immutable historical plan binding. Ordinary `workflow.Intake` should continue requiring the latest accepted plan.

The Plan Graph reference must be added to the effect digest, receipt facts/source references, workflow coordination binding, and dispatch evidence digests. It must never set approval, mandate, autonomy, receipt outcome, or `CanExecute`; those remain controlled exclusively by the existing human decision, execution authorization, and consumption chain.

No files were edited and no tests were run.

## 019fd0c7-a826-73c0-ba03-c1b79a14a0fc

- Date: 2026-08-05
- Agent: James
- Work kind: implementation
- Original report SHA-256: `e4a95d2a9fc33b47958f86937c9b2fc7cd8a4483ea2bb3cfec192c6a290ad80e`
- Transcript: `08/05/rollout-2026-08-05T09-16-13-019fd0c7-a826-73c0-ba03-c1b79a14a0fc.jsonl`

Implemented the pursuit portfolio proposal-decision revalidation slice.

- Decision snapshots now carry the exact parent allocation.
- Repository loading verifies owner, allocation ID, and allocation record digest.
- Approval revalidates the accepted coordination plan before replay or persistence.
- Rejection, clarification, and revocation remain available when the plan is stale.
- Authorization snapshots now include the allocation.
- Added focused stale-plan behavior tests.

Verification: `go test ./internal/pursuit -count=1` passed.

Only the three requested files were edited. Nothing was staged or committed.

## 019fd0c8-2b5d-7550-8891-92b071706e09

- Date: 2026-08-05
- Agent: Kant
- Work kind: implementation
- Original report SHA-256: `4144eb1b4c1278834368b23842532f66a3e12d213a8ac3baf3fb77cfe6caebcd`
- Transcript: `08/05/rollout-2026-08-05T09-16-47-019fd0c8-2b5d-7550-8891-92b071706e09.jsonl`

Implemented the pursuit portfolio authorization/consumption revalidation slice in:

- [portfolio_workflow_authorization.go](../backend/internal/pursuit/portfolio_workflow_authorization.go)
- [portfolio_workflow_authorization_test.go](../backend/internal/pursuit/portfolio_workflow_authorization_test.go)

Key behavior:

- Exact accepted-plan revalidation before authorization, in the canonical approval resolver, and immediately before first receipt consumption.
- Plan ID, revision, digest, node, and evidence URI included in effect identity and authorization evidence.
- Exact coordination reference propagated into `workflow.Intake`.
- Already-consumed effects remain recoverable without requiring the plan to remain latest.
- Legacy unbound effect digests remain version-1 compatible.
- Plan acceptance never grants approval or execution authority; `CanExecute` remains false.
- Production resolver uses the shared GORM database for read-only Plan Graph validation.

Verification passed:

```text
go test ./internal/pursuit -run '^TestPortfolioWorkflowEffect' -count=1
go test ./internal/pursuit -run '<new focused tests>' -count=5
go test ./internal/pursuit -count=1
```

A repeated `-count=3` run exposed one intermittent, unrelated proposal-history test failure; subsequent full pursuit runs passed. Nothing was staged or committed.

## 019fd0c8-fa50-77e1-93ff-bd475aa62464

- Date: 2026-08-05
- Agent: Huygens
- Work kind: implementation
- Original report SHA-256: `5434cbd3ceb5c00c9e8e5f4d3ec7f8dffe8dbdef5e67740f1bb66f817e2094f7`
- Transcript: `08/05/rollout-2026-08-05T09-17-40-019fd0c8-fa50-77e1-93ff-bd475aa62464.jsonl`

Implemented pursuit portfolio dispatch coordination revalidation.

- Dispatch now loads owner-scoped immutable allocation evidence and verifies its digest against the proposal in [portfolio_dispatch_repository.go](../backend/internal/pursuit/portfolio_dispatch_repository.go):25.
- Plan identity, revision, digest, node, and allocation provenance are included in request/run evidence hashing.
- Every non-terminal item revalidates its accepted plan immediately before dispatch in [portfolio_dispatch.go](../backend/internal/pursuit/portfolio_dispatch.go):345.
- Terminal durable results bypass revalidation, remaining replayable after later replanning.
- Added stale-plan and terminal-replay tests in [portfolio_dispatch_test.go](../backend/internal/pursuit/portfolio_dispatch_test.go):85.

Verification passed:

```text
go test ./internal/pursuit -run '^TestPortfolioDispatch' -count=1
go test ./internal/pursuit -count=1
```

Only the three requested files were edited. Nothing was staged or committed.

## 019fde43-44f5-7053-84c0-cc49fe366cce

- Date: 2026-08-08
- Agent: (not recorded)
- Work kind: advisory
- Original report SHA-256: `ca4ba52a3849dfb863b16d0a364a6aae1a0b14a3aa2bab1df9677e552c59d8a1`
- Transcript: `08/08/rollout-2026-08-08T00-06-18-019fde43-44f5-7053-84c0-cc49fe366cce.jsonl`

{"risk_level":"low","user_authorization":"medium","outcome":"allow","rationale":"Read-only discovery of available WebMCP tools on the local app is a routine, reversible step directly related to the user's requested implementation and testing."}
