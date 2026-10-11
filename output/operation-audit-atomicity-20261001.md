# Operation Audit Atomicity Handoff

Date: 2026-10-01

Status: source changes delivered; no tests, builds, formatting executables,
runtime checks, services, Docker, downloads, network actions, commits, pushes,
deployments, or provider writes were performed by this agent. Local source reads
and scoped patches only. Existing processes, including Joyce/ShareT, were not
started, stopped, or modified. No descendants were created.

Workspace: `C:\Users\NO\Documents\Codex\2026-05-30\github-plugin-github-openai-curated-noodzakelijk`.
Branch/dirty state were supplied by the parent; no Git checks were run here.

## Contract

The original `operations.Repository` and public Service method signatures remain
unchanged. The new optional capability is:

```go
type AtomicMutationRepository interface {
    UpdateWithEvent(op *models.Operation, event *models.OperationEvent) (*models.Operation, error)
}
```

`Service.Transition` and `Service.Save` now require this capability. Unsupported
repositories return `ErrAtomicMutationsUnsupported` without calling either
`Repository.Update` or `Repository.AppendEvent`. There is no fallback.

The service still validates transitions/completion before persistence and still
increments the operation version exactly once. Save retains its existing event
fields, including empty BeforeStatus and ActorID. Claim methods and claimed
transition routing are unchanged.

## Persistence

- Gorm shares the existing guarded Update implementation. Its transaction locks
  the operation first, checks the preceding version and immutable owner/workspace,
  locks/checks the execution claim, and uses the existing `updateOperationCAS`.
  The event insert now executes inside that same transaction. Event insert
  errors or CAS conflicts return an error, trigger rollback, and return no saved
  operation. SQL errors remain discoverable through wrapping. A copy of the
  event is inserted, avoiding mutation of the caller's event on rollback.
- Memory shares its guarded Update implementation and holds one mutex while
  checking version, owner/workspace, claims, and duplicate event IDs and storing
  the event and operation. All error returns precede either store. It does not
  invoke the public locking Update/AppendEvent methods inside the lock.
- Both atomic implementations reject nil/mismatched events and mismatched
  resulting statuses with `ErrInvalidMutationEvent`; invalid operation identity
  or a proposed version <= 1 returns `ErrStaleOperation`.
- Memory's ordinary Update additionally rejects nil operations, nil UUIDs, and
  proposed versions <= 1, aligning those guards with Gorm's existing Update.

These are unclaimed mutations, not claim-fencing substitutes. A nonempty claim
owner still blocks them, even if the lease has expired. Only the existing claim
path may perform claimed transitions. No claim, proof, completion-verification,
or replay rule was relaxed. WorldModelStateJSON is persisted with the rest of
the operation; the parent owns its intent/proof semantics.

Ingest was deliberately left unchanged, including its existing separate creation
audit append and evidence-refresh Update. This tranche does not fix ingestion
audit atomicity.

## Regression Source

New file: `backend/internal/operations/mutation_atomicity_regression_test.go`.

- `TestAtomicMutationServiceMemorySuccess`: Transition and Save persist one event,
  one version increment, mutation timestamps, and before-effect uncertainty data.
- `TestAtomicMutationServiceRefusesUnsupportedWithoutWrites`: a Repository-only
  wrapper refuses both service paths without Update/AppendEvent calls.
- `TestAtomicMutationServiceAuditFailureDoesNotMoveOperation`: propagated atomic
  repository error and actual memory duplicate-event rejection leave the original
  operation and audit count unchanged; no separate-write fallback occurs.
- `TestAtomicMutationMemoryGuards`: stale versions, owner/workspace changes,
  claimed operations, and invalid/mismatched events refuse writes.
- `TestAtomicMutationClaimedPathStillUsesClaimRepository`: a claim-only wrapper
  still supports TransitionClaimed; unfenced Save/Transition remain rejected.
- `TestAtomicMutationGormTransactionWiring`: a standard-library SQL driver used
  through the existing Gorm PostgreSQL dependency records successful commit,
  event-failure rollback, CAS-conflict rollback without event insert, and stale
  snapshot refusal before writes. It requires no server or added dependency.

The existing service tests now use the actual MemoryRepository rather than a
permissive fake overwrite. TestCompleteRequiresVerification retains its failure
and success assertions, uses a real UUID/version-1 fixture persisted via Create,
and asserts unchanged state on rejection and one audited completion on success.

All of this is unexecuted test source, not a passing test result.

## Wrapper Compatibility

Embedding `operations.Repository` does not promote optional methods implemented
by the embedded interface's concrete value. Wrappers need explicit atomic
forwarding or an initialized embedded `operations.AtomicMutationRepository`.
Update/AppendEvent overrides no longer intercept Service.Transition/Save;
failure-injection and cancellation logic intended for those calls must move into
UpdateWithEvent while preserving any existing TransitionClaimed overrides.

The following source snapshot hid the new capability; none of these files was
edited by this agent:

| Full Repository-Relative Filename | Wrapper |
| --- | --- |
| backend/internal/background/execute_outcome_test.go | outcomeTransitionFailureRepository |
| backend/internal/background/worker_claim_regression_test.go | failedDeferralRepository, heartbeatLossRepository |
| backend/internal/background/worker_reliability_test.go | failClaimRepository |
| backend/internal/background/worker_test.go | cancelOnStatusRepository, failStatusUpdateOnceRepository |
| backend/internal/opscontrol/handler_security_test.go | failingRecoveryRepository |
| backend/internal/phase2/safe_outcome_response_test.go | phase2OutcomeFailureRepository |
| backend/internal/runtimelab/safe_outcome_attempt_test.go | labOutcomeFailureRepository |

Claim-only calls remain supported without forwarding. Test setup or other paths
that call unclaimed Transition/Save require it. failedDraftPersistenceRepository
was mentioned by the parent but was not found in the inspected backend/internal
source snapshot; its filename must not be invented. New or concurrently edited
wrappers require parent reconciliation.

## Changed Paths

Only these six paths were changed by this agent:

1. backend/internal/operations/service.go
2. backend/internal/operations/repository.go
3. backend/internal/operations/memory_repository.go
4. backend/internal/operations/service_test.go (subsequently authorized)
5. backend/internal/operations/mutation_atomicity_regression_test.go (new)
6. output/operation-audit-atomicity-20261001.md (new)

No execution_claim*.go or background/* file was edited. Existing unrelated dirty
work was not reverted. Source reads of claim/background files supplied context
only; the parent owns those changes.

## Acceptance Gap

The parent should format and execute focused native Go package tests serially
with one worker, only under its cached-dependency/resource policy, after adapting
the wrappers. This agent has not verified compilation, formatting, or any test
result. Parent-reported earlier CI results are not evidence for these edits.

Real PostgreSQL acceptance remains open: event-insert failure must leave the
operation/status/version and existing audit rows unchanged after rollback;
success must commit both; concurrent stale writes and claims must not bypass
version/identity/fencing guards. The SQL-driver test exercises transaction wiring
only and cannot establish real row locking, constraints, crash durability,
concurrency, migration/schema availability, or deployment acceptance. No live
database or provider acceptance is claimed.
