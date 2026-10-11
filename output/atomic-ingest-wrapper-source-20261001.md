# Atomic Ingest Wrapper Source Handoff

Date: 2026-10-01. Source-only integration worker.

## Parent Integration Status

The stopped source assignment's missing parent API has now been implemented.
All nine forwarding methods were reviewed/formatted and their background, phase2,
runtimelab and opscontrol consumers pass the final native intake reruns recorded
in `production-durability-integration-20261001.md`. The unimplemented-dependency
notes below describe the handoff snapshot, not the present source. The parent also
fixed the feed audit concern: failed/partial item storage no longer records `synced`,
withholds the report cursor, and keeps last attempt separate from last success.
Local accountfeed regressions pass; real-store/provider acceptance remains unproven.

## Scope And Changes

Only these four existing source files and this report were written:

- `backend/internal/background/atomic_repository_wrappers_test.go`
- `backend/internal/phase2/atomic_repository_wrapper_test.go`
- `backend/internal/runtimelab/atomic_repository_wrapper_test.go`
- `backend/internal/opscontrol/atomic_repository_wrapper_test.go`
- `output/atomic-ingest-wrapper-source-20261001.md`

Added `CreateWithEvent(*models.Operation, *models.OperationEvent) (*models.Operation, error)` for all nine requested types:

| Package | Wrapper types |
| --- | --- |
| background | outcomeTransitionFailureRepository, heartbeatLossRepository, failClaimRepository, failedDeferralRepository, failStatusUpdateOnceRepository, cancelOnStatusRepository |
| phase2 | phase2OutcomeFailureRepository |
| runtimelab | labOutcomeFailureRepository |
| opscontrol | failingRecoveryRepository |

Each method forwards the original operation/event pointers to the embedded repository's `operations.AtomicCreationRepository.CreateWithEvent`, returning its result/error unchanged. Missing capability returns `(nil, operations.ErrAtomicCreationUnsupported)`. No fallback calls to `Create`, `AppendEvent`, `Update`, or `UpdateWithEvent` were added. Background shares a small `createWithAudit` helper, following its existing update-helper pattern.

All existing `UpdateWithEvent` methods and `updateWithAudit` remain unchanged, including deferral save failures, one-shot status failures, and cancellation after successful status updates. Creation does not consume those update fault states.

## Parent Integration Dependency

At the last source inspection, `AtomicCreationRepository`, `ErrAtomicCreationUnsupported`, and operations repository implementations of `CreateWithEvent` were not yet present in `backend/internal/operations`. These additions intentionally target the parent's planned API. Compilation remains unverified and requires the parent-owned operations changes. Operations source/tests, atomic Ingest creation, audited evidence refresh, and executable verification remain with the parent.

## Additional Wrappers Found Outside The Nine

Backend Go source searches found these additional operation repository wrappers; none were edited:

| Type | Definition | Creation capability impact |
| --- | --- | --- |
| mutationLegacySpy | `backend/internal/operations/mutation_atomicity_regression_test.go:151` | Embeds only `Repository`; intentionally hides optional capabilities. Do not add forwarding without preserving the legacy rejection tests. |
| mutationAuditFailure | `backend/internal/operations/mutation_atomicity_regression_test.go:167` | Embeds `*mutationLegacySpy` and explicitly implements atomic mutation only; creation capability is also hidden. |
| mutationClaimsOnly | `backend/internal/operations/mutation_atomicity_regression_test.go:282` | Embeds `Repository` and `ClaimRepository`; creation capability is hidden. Existing test seeds through the base repository before constructing this wrapper. |
| intentCommitCancellationRepository | `backend/internal/background/execute_intent_test.go:169` | Embeds `*operations.MemoryRepository`; should inherit the parent's new creation method automatically, unlike interface embedding. |
| denyingTargetClaimRepository | `backend/internal/runtimelab/service_test.go:398` | Embeds `*operations.MemoryRepository`; should inherit the parent's new creation method automatically. |

No additional direct `operations.Repository`-embedding types beyond the requested nine were found in the searched backend Go source. The operations-local types above use unqualified `Repository`. No aliased operations imports were found. This is source inspection, not executable interface-conformance proof.

## Production Input Error And Status Review

The four production `Ingest` call sites found were inspected. Findings describe the current snapshot and the expected path when the parent makes atomic-create failures propagate from `Ingest`; no production file was edited.

### Account Feed Sync: Misleading Success Audit

`backend/internal/accountfeed/registry_service.go:287` checks the Ingest error, adds a bounded public error, and continues without incrementing created/refreshed counters. However, line 298 unconditionally records a `synced` audit event and advances last-run bookkeeping even when one or all items could not be recorded. `backend/internal/accountfeed/handler.go:170` and line 179 return HTTP 200 with reports containing those errors.

Concern: atomic-create failures will be visible in `SyncReport.Errors`, but an operator or caller relying on HTTP status or the audit event alone can still see apparent success. The original error/category is replaced with a generic item error and is not retained by this inspected registry path. Parent should decide whether a failed/partial-sync audit state and bounded operator diagnostics are required. Sanitized public errors should be retained.

### Background Worker: Failure Reported, Typed Error Lost

`backend/internal/background/worker.go:228` checks Ingest errors, retains their text in `Report.Errors`, and does not increment created counts. `ItemsIngested` increments before validation/storage at line 222, so it is an attempted-item count, not proof of durable creation. Processing of other operations can continue after item failures.

`reportFailures` at line 321 returns `ErrReportedFailures` when report errors exist. Both owner and configured module paths return `RunOnce` directly (`backend/internal/phase2/module.go:283` and line 293). `RunBackground` maps the generic failure to a sanitized HTTP 500 (`backend/internal/phase2/handler.go:466` onward), so it does not report HTTP success for this failure.

Concern: the pass-level error preserves only `ErrReportedFailures` and an error count, not the underlying atomic-create sentinel via error wrapping. The HTTP failure response omits the report. This limits typed diagnostics and item-level visibility, though it does fail the pass. Separately, the handler calculates HTTP 503 for cancellation/deadline errors but its server-error branch always writes 500; this pre-existing status issue is not specific to atomic creation.

### Runtime Self-Test: Fails Closed Before Execution

`backend/internal/runtimelab/service.go:201` checks Ingest errors and records `AttemptFailed` with a bounded message before classification, claiming, or execution. `backend/internal/runtimelab/handler.go:68` returns HTTP 200 containing that failed attempt. This is application-level failure reporting, not an execution-success claim. Concern: the original create error is not retained in this inspected attempt path, and clients must inspect attempt status rather than HTTP status.

### OpenClaw Discovery: Persistence Failure Is Silent

`backend/internal/runtimelab/openclaw_discovery_ledger.go:57` ingests the discovery operation; line 75 returns the original probe result unchanged on an Ingest error. Later Save/Transition errors similarly return the original result. `backend/internal/runtimelab/service.go:150` returns it with the runtime-found boolean true, and `backend/internal/runtimelab/handler.go:59` responds HTTP 200.

Concern: successful read-only protocol discovery remains distinguishable from durable evidence through `EvidencePersisted`, which this path sets only after ledger success (`openclaw_discovery_ledger.go:208`), but there is no explicit persistence-failed field or retained error here. Atomic-create/audited-refresh failures can therefore be silently hidden behind otherwise successful discovery. Parent should keep discovery success separate from evidence durability and add bounded failure visibility if required.

### Operations Snapshot Awaiting Parent Changes

The inspected `backend/internal/operations/service.go:37` still creates via `repo.Create`, ignores `AppendEvent` errors, and refreshes evidence via unaudited `repo.Update`. These are the parent-owned changes identified in the request, not changes made or verified by this worker. Caller observations must be reassessed against the parent's final implementation.

## Verification And Restrictions

Used local filesystem reads, source searches, read-only Git status, and narrowly scoped patch edits. Re-read all four edited files and inspected the forwarding guards and unchanged update bodies. Historical memory was used only to orient to the checkout; findings above come from current source reads. No relevant external connector was needed.

No tests, builds, formatters, Docker, services, network, downloads, subagents, cleanup, commits, or executable acceptance checks were performed. Existing unrelated changes were not reverted or modified. No claim of compilation, atomic durability, runtime acceptance, or production readiness is made.
