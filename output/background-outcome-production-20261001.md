# Background Outcome Production Handoff

Date: 2026-10-01. Source-only worker handoff, not executable verification or production acceptance.

## Scope And Changed Paths

- Incremental edits: `backend/internal/background/execute.go`.
- Added focused regressions: `backend/internal/background/execute_outcome_test.go`.
- Added this report: `output/background-outcome-production-20261001.md`.
- Existing dirty background files and other workers' changes were preserved. No README, task, pursuit, operations, broker, handler, or runtime-lab files were edited. No descendant agents were started.

## Source Findings And Fixes

1. Background is not a direct `task.ExecutionOutcomeUncertain` consumer. Its execution path is exclusively `executionbroker.ExecuteLocalSafeWorker`; it has no task-plan learning or workflow-completion consumer. The task contract was read to apply the same no-unproven-replay principle without introducing a task-package dependency.
2. The previous execution consumer classified every non-cancellation broker error as `failed`. The concrete safe worker can error after filesystem mutation, and its cleanup is best-effort. `operations.safeOperationClaimable` allows explicit claims of failed safe operations. Thus a failure was exposed as explicitly retryable without no-effect proof. Ordinary background `ClaimNext` already excludes failed rows; no existing automatic duplicate was reproduced or claimed.
3. The previous consumer returned an empty `SafeOutcome` on verifying/completed persistence failures, discarding a returned artifact receipt. It also retained only bounded output, not the artifact path/hash and verification fields. Receipt retention is now explicit in `SafeOutcome.Receipt`, including error returns. Successful fenced transitions persist the structured receipt under `WorldModelStateJSON.safeWorkerExecution`, retaining existing policy fields and exact JSON numbers. Source `EvidenceJSON` is not modified.
4. Unknown errors, zero-value result without error, result-plus-error, failed verification after a returned artifact, and contradictory success flags now produce `interrupted` with verification pending and `ErrSafeOutcomeUncertain`. They cannot be marked verified/completed or enter failed-to-ready retry. A retained uncertain receipt also blocks subsequent execution if a caller supplies ready/classified/approved status.
5. Proven pre-effect authorization-required/denied/mismatch refusals remain failed and explicitly retryable. A local configuration preflight does not call the executor when the worker is unconfigured; configuration repair remains explicitly retryable. Joined errors and any effect/verification evidence override no-effect classification. No error-message parsing is used as dispatch proof.
6. Finalization retains operation/receipt snapshots and joins execution and persistence errors. `Interrupted`/`Verified` are set only after the corresponding ledger transition succeeds. Persistence failure remains an error, not a claimed durable recovery.

## Focused Regressions Added

- `TestSafeExecutionUncertainOutcomeCannotCompleteOrRetry`: partial/completed receipt plus error, missing result with/without error, contradictory refusal/success evidence, joined errors, failed verification, contradictory passed flags, receipt preservation, re-ingestion, exact-number preservation, explicit/automatic retry rejection, and ready/classified/approved replay rejection.
- `TestSafeExecutionAuthorizationRefusalPreservesDeterministicRetry`: known pre-effect failures can complete on a separate explicitly claimed repaired attempt.
- `TestSafeExecutionFinalizationFailureRetainsRealReceipt`: uses the existing authorized local broker with an injected ledger failure after real artifact execution, and checks the returned receipt against the artifact. This is test code only; it was not run here.
- `TestSafeExecutionPriorReceiptFailsClosedBeforeDispatch`: malformed/non-object state and missing/contradictory prior proof do not dispatch.
- `TestSafeExecutionUncertainPersistenceFailureRetainsReceiptAndErrors`: no false interruption claim when interruption persistence fails; both errors and receipt remain available; durable running state remains nonclaimable.
- `TestSafeExecutionUnconfiguredWorkerCanRetryAfterConfigurationRepair`: unconfigured preflight refusal remains explicitly retryable after repair.

## Cross-Chain Findings For Parent

- `backend/internal/operations/service.go:45` replaces source evidence on re-ingestion. Runtime receipts deliberately use execution state instead, and a regression covers preservation across this path. No shared ingest edit was made.
- `backend/internal/executionbroker/broker.go:70-72` drops worker output when `Run` returns an error. Currently `LocalSafeWorker.Run` returns zero output on its error paths, including possible post-effect failures. Background cannot recover evidence that the broker/worker never supplies; the retained receipt is explicitly incomplete, and interruption requires filesystem/audit reconciliation. Broker receipt improvements belong to that package's owner.
- `backend/internal/phase2/handler.go:383` and `backend/internal/runtimelab/service.go:235` discard the returned outcome when an execution error occurs. A successfully persisted receipt remains in the ledger, but when the first post-execution ledger write itself fails, these callers do not expose/persist the returned receipt snapshot. They also release the claim on error, which can leave an unleased running/verifying record requiring manual recovery. No out-of-scope consumer edits were made.
- `operations.RecoverExpiredClaims` already routes running to interrupted and verifying to awaiting approval without automatic replay. Interrupted/awaiting-approval operations are not explicitly safe-claimable. Ordinary approval therefore does not re-dispatch through this background API.
- Existing historical failed rows without execution receipts cannot establish no-effect proof from a status alone. This patch does not migrate or reinterpret them. Parent should include them in reconciliation/acceptance review, not claim historical coverage.
- Parent's pursuit/task projection work remains separate and read-only here. No claim is made that the unknown/indeterminate launch with exit code zero issue was repaired by this background patch.

## Verification And Commands

Actually performed: source reads, narrow repository status inspection, installed Go-pattern/testing and diagnosis skill review, and `git diff --check -- backend/internal/background/execute.go` (exit 0; only the Git LF/CRLF warning).

Not performed: Go compilation, tests, race tests, gofmt, database/provider/browser checks, installation, container/process lifecycle actions, commit, push, or deployment. `gofmt` was not available on this worker's PATH. No credentials were printed. Host resource limits and the parent-only executable verification rule were respected.

Parent verification, serially, using its established Go environment and backend working directory:

```powershell
gofmt -w internal/background/execute.go internal/background/execute_outcome_test.go
$env:GOMAXPROCS = '1'
go test -p 1 -parallel 1 -count=1 -timeout 60s ./internal/background -run '^TestSafeExecution'
go test -p 1 -parallel 1 -count=1 -timeout 90s ./internal/background
go test -p 1 -parallel 1 -count=1 -timeout 90s ./internal/phase2 ./internal/runtimelab
```

These commands are recommendations, not executed results. The parent must select only its approved environment and inspect other-package test effects before running the wider consumer checks. No runtime restart or provider action is required for the focused regressions.

## Remaining Limits

- Tests are unexecuted; syntax, formatting, behavior, and cross-package compatibility require the parent's serial checks.
- No live PostgreSQL claim/recovery behavior, deployment, provider acceptance, or production-readiness claim is established.
- Untyped broker errors cannot prove pre-dispatch safety; only known local authorization/configuration refusals receive no-effect treatment. Additional proven preflight classifications should originate at the broker boundary rather than be inferred from error strings.
- A process crash or failed first post-effect ledger write cannot be made transactionally receipt-durable within this package. Returned receipts, existing durable state, expired-claim recovery, and operator reconciliation remain distinct recovery mechanisms.

## Appendix: Parent Pursuit And Task Source Review

Requested 2026-10-01. Reviewed the current uncommitted `backend/internal/pursuit/service.go` and only the relevant task projection/call-site contract in `backend/internal/task/service.go`. Supporting automation, model, repository, UI, and focused test files were read only. The code-review skill was consulted; its agent-spawning procedure was not used because the parent explicitly prohibited descendants. This appendix is source reasoning, not test execution. No parent source or test files were edited.

Reviewed source SHA-256 snapshots: pursuit `0123A672512B1EE75A71F93308AAFC8F655A27DF70BF755235B58AE09C184901`; task `08952121B40E874D5F6F83C41233397359865CE4B8EF54C2CE5D19AD637E07F0`. Parent edits after these snapshots may supersede individual findings and line references.

### Findings, Ordered By Severity

#### 1. [P1] Process-exit semantics reject valid API and Docker completion

Location: `backend/internal/pursuit/service.go:5859` (`runtimeAttemptNeedsReview`).

The new unconditional `attempt.ExitCode != 0` check turns valid completed API launches into review blockers. API launches store `resp.StatusCode`, not process exit status (`backend/internal/automation/automation_service.go:2240`). Docker-service completion stores 204/304 (`automation_service.go:2644`). Thus a persisted API `completed`/200 or Docker `completed`/204 is rejected by `completedRuntimeAttempts`, the completion-evidence guard, decision queues, and runtime blockers. This is a concrete compatibility regression, independent of pending intents.

Minimal fix: keep approval refusal and unknown-status handling fail-closed, but validate completed receipt codes by `LaunchType`: process-like launches require zero; API launches use the launcher's recorded completed status plus a valid HTTP status; Docker-service launches require 204/304. The existing `task.automationCompletedReceiptExitValid` at `backend/internal/task/automation_executor.go:224` documents the current receipt contract. Match that contract locally or share it through an acyclic lower-level helper; do not import task into pursuit blindly. API configured expected-status success can legitimately include a non-2xx code, so a blanket 2xx-only rule would also regress established launcher behavior.

Focused additions: completed API/200 and API/204; explicitly expected API/404 with persisted completed status; failed API/404; completed Docker/204 and /304; completed script/1; completed with approval still required; unknown/indeterminate with code zero. Test both the predicate and completion/blocker consumers.

#### 2. [P1] Approval rejection is not an aggregate completion blocker

Locations: `pursuit/service.go:3908`, `:4104`, `:4394`, `:5435`; completion guard at `:5102`.

`workflowCompletionAccepted` correctly requires approved status when approval is required. However, `approvalWorkflows`, workflow actions, and decision cards recognize only blank/pending approval. A completed/verified workflow with `RequiresApproval=true` and `ApprovalStatus=rejected`, revoked, expired, or an unknown value is not accepted by the new predicate, but also produces no approval blocker. `taskRunNeedsReview` does not catch it either. If the pursuit has a separate accepted verification run/runtime/evidence record, `completionActiveBlockerReasonForOwner` returns no blocker and that other record can still authorize verified pursuit completion despite the unresolved approval contradiction.

Minimal fix: define one primitive unresolved-approval predicate (`RequiresApproval && approval status is not approved`) and use it in completion blocking and review projection. Do not offer an ordinary Approve action for rejected/revoked states if they require a fresh review; those states can map to an explicit blocked/review action instead. Keep legitimately approved historical workflows accepted. Align the canonical status handling used by all consumers.

Focused addition: a rejected/unknown approval workflow plus an independent accepted verification run must still reject pursuit completion. The current approval regression case uses only pending status and no independent evidence, so it does not exercise this bypass.

#### 3. [P2] Task-run output still reports unaccepted workflows as completed

Locations: `pursuit/service.go:5355-5378`, projected at `:5333-5341`; UI consumes the status directly in `frontend/src/app/pages/pursuits/pursuits.component.html:1432`.

`taskRunStatus` checks review flags and then returns completed for any `StateCompleted`, rather than requiring `workflowCompletionAccepted`. `taskRunNeedsReview` omits unresolved approval, unknown/uncertain/conflicting verification, and missing verification on completed workflows. A historical completed workflow with `LastTaskPlanID` present and empty/unknown verification therefore projects `Status=completed, NeedsReview=false`, even though the new summary/evidence predicate rejects it. The same happens for a completed/verified workflow still awaiting approval.

Minimal fix: on completed workflows derive the displayed status from accepted completion, not state alone. Extend the primitive review predicate to include unaccepted completed verification and unresolved approval; avoid recursion because `workflowCompletionAccepted` itself calls `taskRunNeedsReview`. Preserve nonterminal planned/running rows as nonterminal rather than globally treating empty verification as a failure.

Focused additions: task-run mappings for completed with empty/unknown/uncertain/conflicting verification and pending/rejected approval; positive accepted historical verified/test-passed workflows. Assert both status and NeedsReview, not only Summary.CompletionCandidate.

#### 4. [P2] Non-UUID verification provenance still substitutes for a real run

Location: `pursuit/service.go:5161-5165`.

The UUID lookup path now correctly refuses missing, failed, and needs-review runs. The invalid-UUID branch still returns accepted completion solely for a nonempty SourceURI and `completion_evidence` relationship. A link such as `LinkVerification + LinkID="external-label" + SourceURI="local://claim"` can therefore pass the ownerless/internal service completion guard with no verification run at all. This is not an asserted authenticated production exploit: the Gorm owner-scoped link validator rejects non-UUID verification IDs, but ownerless service wrappers and repositories without that validator still reach the fallback.

Minimal fix: remove the remaining provenance-only acceptance unless a separately verified external attestation contract is intentionally supported. Preserve such links as provenance, not completion proof. The newly added `TestPursuitCompletionRequiresActualInternalVerification` covers a missing valid UUID only; add invalid/non-UUID IDs to close the other branch.

#### 5. [P2] Direct task projection, activity, and timeline use different success guards

Locations: `task/service.go:1021,1031-1040`; `pursuit/service.go:1395-1403,4717`.

`persistPursuitAttempt` downgrades only `ExecutionOutcomeUncertain`. That predicate intentionally does not classify missing ExecutionResult, ordinary blocked/needs-approval tool outcomes, or verification failure as runtime uncertainty. A contradictory incoming final plan marked validated/completed with a blocked reason, ungranted required approval, failed validation, or missing/unaccepted execution verification therefore retains its optimistic status in the stored task-attempt row. This is a missing projection boundary for contradictory inputs, not a claim that the normal Run path currently generates every such input; Run already handles several of these conditions upstream.

The activity mapper catches blocked reason and exactly needs_review, but a persisted `validated` attempt with verification uncertain/conflicting/unsupported/empty still announces verified output. The direct-attempt timeline independently ignores even exactly needs_review when status is validated and blocked reason is empty, despite the newly fixed activity mapper treating it as review-required. The UI displays the persisted attempt status directly.

Minimal fix: when projecting a final run that claims success, require passed validation, an execution result with the task package's accepted verification status, no blocker, and no ungranted required approval; keep explicit uncertainty dominant. Do not convert ordinary plan/running snapshots or retry_needed states into false terminal outcomes. Preserve IDs, receipts, and CompletedAt as the time the attempt ended, not proof of successful completion. Use one pursuit-local direct-attempt review predicate for activity and timeline; only announce verified output when verification is accepted. Respect task's existing schema_validated completion support rather than accidentally rejecting all successful task plans through pursuit's narrower workflow status list.

Focused additions: contradictory validated plan with blocked reason, nil execution, approval required/ungranted, validation false, and unaccepted verification; known verified final run unchanged; plan-only unchanged; ended review attempt keeps CompletedAt. Assert stored status, activity kind/message, and timeline NeedsReview consistently. The current new projection test covers execution/tool/legacy uncertainty and known completion only.

### Pending-Intent Compatibility And Positive Checks

- Standard immutable intent rows are created with pending status, `*_intent` LaunchType, zero default ExitCode, and approval false (`automation/automation_service.go:1136-1154`, also stop intents at `:948-966`). The current status switch intentionally leaves these nonblocking. It does not count them as completed because `runtimeAttemptCompleted` is false. A pending intent alongside its completed outcome therefore does not by itself permanently block historical completion; retain this behavior.
- Add an aggregate compatibility test containing both a standard pending intent and its completed script outcome, then the equivalent API/200 outcome after fixing finding 1. Assert accepted evidence, zero runtime blockers, and no runtime-review card for the intent. Do not change all pending rows to needs-review; distinguish immutable intent bookkeeping from actual unknown/indeterminate execution outcomes.
- Unknown/indeterminate/provider-new status with exit zero is correctly reviewable in the current edit. Approved/verified workflows with no recovery flag or blocker remain accepted by the current workflow helper; do not impose a universal new verification-run foreign key requirement on these historical workflow records.
- RecoveryNeedsReview and nonempty BlockedReason now correctly suppress workflow completion evidence and candidate summaries. Known completion/recovery labels alone no longer substitute for accepted verification in `runtimeAttemptRecoveredByWorkflow`.
- Parent's focused tests were inspected as source only. No tests, Docker, installs, processes, provider calls, or descendant agents were run. Only this appendix was written; all reported fixes remain parent-owned.
