# Atomic Ingest Safety Review

Date: 2026-10-01. Bounded, source-only review of the shared dirty checkout.

## Parent Resolution Status

Both source findings below were resolved after this review snapshot. Creation-race
reloads now use the same audited evidence refresh and claim fence as ordinary
duplicates; new tests prove changed-evidence persistence and claimed-winner refusal.
The uncertainty fixture now provides complete creation input without relaxing any
receipt/no-retry assertions. Parent-executed operations and background suites pass
in `production-low-resource-atomic-intake-operations-reviewed-20261001.jsonl` and
`production-low-resource-atomic-intake-background-reviewed-20261001.jsonl`.
The findings and fingerprints below remain historical source evidence, not current
unresolved defects or agent-executed runtime results. Actual PostgreSQL acceptance
remains open. The separately reported feed-success concern was also fixed and
locally tested: partial/failed audit, withheld failed cursor and truthful timestamps.

## Findings

### [P2] Creation-race losers skip the audited evidence-refresh path

References: `backend/internal/operations/service.go:78-84`, compared with `service.go:53-59`.

When two valid ingests initially miss the same owner/workspace/dedupe key, one can create the operation and the other receive `ErrDuplicateDedupeKey`. The losing call reloads the winner and immediately returns success, even if its nonempty evidence differs. Unlike the ordinary duplicate path, it never calls `Save`, persists the new evidence, or emits `source_evidence_refreshed`. If a worker has claimed the winning operation in the meantime, it also returns success where the ordinary changed-evidence path would return `ErrOperationClaimed`.

This does not overwrite claimed work or break the atomic creation pair. It does silently accept an unpersisted evidence refresh and makes refresh/error semantics depend on race timing. The branch was retained from the earlier implementation, but remains a gap in the hardened ingest path. Route the post-collision snapshot through the same duplicate-refresh logic, preserving its audited save, CAS, and claim refusal. Keep identical/empty evidence a no-op. Parent-owned verification should cover both differing evidence and a claim acquired before the collision reload.

### [P2] Existing uncertainty regression uses an input now rejected upfront

References: `backend/internal/background/execute_outcome_test.go:129-131`; `backend/internal/operations/service.go:43-45`; `backend/internal/operations/domain.go:36-43`.

`TestSafeExecutionUncertainOutcomeCannotCompleteOrRetry` re-ingests with only owner, workspace, dedupe key, and evidence. It omits `Title`, `OperationType`, and `SourceType`. The new upfront `NewOperation` call necessarily returns `operation: title required`, so every table case reaching this call fails before checking receipt preservation and the subsequent no-retry assertions.

Adapt this test input to include the stored operation's required fields while retaining its dedupe key and changed source evidence. Do not weaken upfront production validation. This is a source-derived test compatibility defect, not an executed failure report. No test was edited or run here.

Follow-up status: the parent reports that native background tests failed only at this sparse fixture and is adapting it to full valid snapshot fields while retaining the uncertainty assertions. That runtime result is parent-reported, not independently executed here. The follow-up source read still showed the sparse input at line 129; the adaptation and rerun are not yet verified by this sidecar. No production need for accepting sparse Ingest inputs was found.

## Source-Confirmed Safeguards

These are code-path observations, not runtime acceptance claims.

| Area | Observed implementation and references |
| --- | --- |
| Fail-closed initial creation | `service.go:63-76` requires `AtomicCreationRepository`, allocates the operation ID before constructing its event, and calls only `CreateWithEvent`. Unsupported repositories return `ErrAtomicCreationUnsupported`; no independent `Create`/`AppendEvent` fallback remains in Ingest. |
| Initial-state and event binding | `repository.go:95-110` rejects nil operations, nil IDs, noninitial versions/statuses, invalid domain fields, blank operation/source types, invalid evidence JSON, and nil/mismatched events. Event type must be `created`, actor `hai`, before-status empty, after-status matched, and payload valid JSON. |
| Gorm creation pair | `repository.go:169-189` copies the arguments and inserts operation then event in one transaction. Operation insertion errors and audit insertion errors return failure; the audit error is wrapped, not classified as a dedupe collision. Return success occurs only after the transaction returns successfully. Actual rollback/commit behavior was not exercised. |
| Memory creation pair | `memory_repository.go:46-73` validates first, then uses one mutex scope for operation-ID, active-dedupe, and event-ID collision checks before inserting either record. ID/event collisions remain ordinary errors, not `ErrDuplicateDedupeKey`. |
| Collision classification | Latest `repository.go:281-289` requires typed PostgreSQL SQLSTATE `23505` and the exact active-dedupe constraint name. Typed nonmatches do not fall through to text matching. The non-typed fallback requires the specific duplicate-key diagnostic, not merely an index-name substring. The initially observed broad classifier was tightened during this round; it is not an outstanding finding. |
| Owner/workspace normalization | `service.go:41-50` trims owner and workspace, defaults blank workspace to `local`, and validates before lookup. Creation and collision reload use these same normalized identities (`service.go:79`). Blank owners and malformed nonempty evidence fail before duplicate acceptance. This observation concerns Service.Ingest, not normalization by every repository entry point. |
| Audited ordinary refresh | `service.go:53-59` changes only the loaded snapshot's evidence and calls `Save` with event type `source_evidence_refreshed`. `service.go:130-145` advances timestamp/version and requires atomic mutation support; no unaudited update fallback is used. |
| CAS, immutable identity, claim fence | Gorm `repository.go:203-245` locks the current operation, checks expected version and immutable owner/workspace, locks/checks the claim, then writes CAS plus event in one transaction. `execution_claim.go:539-547` scopes CAS by ID, owner, workspace, and expected version. Memory `memory_repository.go:87-125` checks the equivalent conditions under one mutex and checks duplicate audit IDs before replacing the stored operation. Both refuse any nonempty claim owner, including an expired claim not yet released. |
| Uncertainty preservation | Ordinary refresh does not reconstruct or reset `WorldModelStateJSON`, status, decision, verification fields, result/error, or completion time: it copies the loaded operation and changes evidence/timestamp/version. Runtime uncertainty is read from `WorldModelStateJSON.safeWorkerExecution` in `backend/internal/background/execute.go:257-271`; source evidence replacement does not remove that receipt. CAS prevents a stale refresh from overwriting a concurrently advanced snapshot. The intended regression proof is currently obstructed by the invalid test input above. |

## Ingest Call Sites And Wrapper Coverage

Repository-wide Go-source searches covered direct `Ingest` calls, operations imports, repository embeddings, and repository method implementations. Four production entry points were found: account feed sync (`backend/internal/accountfeed/registry_service.go:287`), background worker (`backend/internal/background/worker.go:228`), runtime self-test (`backend/internal/runtimelab/service.go:201`), and discovery ledger (`backend/internal/runtimelab/openclaw_discovery_ledger.go:57`). They use complete creation inputs; the feed paths share `Feed.ToOperationInput` (`backend/internal/accountfeed/feed.go:111-124`). Existing caller error-reporting observations remain in `output/atomic-ingest-wrapper-source-20261001.md` and are not repeated as new findings here.

The searched test/helper call sites use concrete Memory/Gorm repositories or the adapted wrappers below. The partial duplicate input at `execute_outcome_test.go:129` is the outstanding input adaptation found. PostgreSQL duplicate/claim cases at `backend/migrations/operations_execution_claim_postgres_integration_test.go:82-93` already provide all required creation fields.

| Wrapper | Definition | Creation forwarding |
| --- | --- | --- |
| outcomeTransitionFailureRepository | `background/execute_outcome_test.go:176` | `background/atomic_repository_wrappers_test.go:18` |
| heartbeatLossRepository | `background/worker_claim_regression_test.go:149` | `background/atomic_repository_wrappers_test.go:22` |
| failClaimRepository | `background/worker_reliability_test.go:295` | `background/atomic_repository_wrappers_test.go:26` |
| failedDeferralRepository | `background/worker_claim_regression_test.go:103` | `background/atomic_repository_wrappers_test.go:30` |
| failStatusUpdateOnceRepository | `background/worker_test.go:648` | `background/atomic_repository_wrappers_test.go:34` |
| cancelOnStatusRepository | `background/worker_test.go:641` | `background/atomic_repository_wrappers_test.go:38` |
| phase2OutcomeFailureRepository | `phase2/safe_outcome_response_test.go:97` | `phase2/atomic_repository_wrapper_test.go:8` |
| labOutcomeFailureRepository | `runtimelab/safe_outcome_attempt_test.go:119` | `runtimelab/atomic_repository_wrapper_test.go:8` |
| failingRecoveryRepository | `opscontrol/handler_security_test.go:80` | `opscontrol/atomic_repository_wrapper_test.go:8` |

Table paths above are beneath `backend/internal/`. All nine methods forward the original pointers and preserve returned errors/results. Background's helper (`atomic_repository_wrappers_test.go:10-15`) and the other three package methods return `ErrAtomicCreationUnsupported` if the embedded repository hides the capability. None falls back to independent writes or invokes update fault injection during creation. Existing update/cancellation/deferral fault paths remain separate.

No additional accidentally unadapted creation-dependent wrapper was found in the searched Go source. Two concrete embeddings inherit creation automatically: `intentCommitCancellationRepository` (`background/execute_intent_test.go:169-170`) and `denyingTargetClaimRepository` (`runtimelab/service_test.go:398-399`). Three operations-local wrappers still hide creation deliberately: `mutationLegacySpy`, `mutationAuditFailure`, and `mutationClaimsOnly` (`operations/mutation_atomicity_regression_test.go:151`, `:167`, `:282`). Their current tests seed through the base repository (`mutationSeed`, lines 40-53), not Ingest through those wrappers. Adding forwarding there is not required for their existing mutation-only checks and could change intentional capability-rejection coverage.

## Snapshot And Limitations

### Follow-Up Sparse Consumer Check

A fresh repository-wide search of non-test Go source for Ingest calls and method references again found only the four production entry points listed above. Account feed sync obtains its input from `Feed.ToOperationInput` at `backend/internal/accountfeed/registry_service.go:282`; the worker does the same at `backend/internal/background/worker.go:223`. That builder supplies title, source type, operation type, owner/workspace, dedupe key, and evidence (`backend/internal/accountfeed/feed.go:111-124`). Runtime self-test supplies the required fields at `backend/internal/runtimelab/service.go:192-199`, and discovery does so at `backend/internal/runtimelab/openclaw_discovery_ledger.go:57-72`. None uses Ingest as a sparse lookup API. No real consumer requiring a separate sparse-lookup path was identified; retaining upfront creation-input validation is compatible with these inspected callers.

The follow-up read also reconfirmed exact PostgreSQL code/constraint classification and the canonical non-PostgreSQL duplicate-key message in `backend/internal/operations/repository.go:281-289`. There is no remaining substring-only-classifier finding. These follow-up checks used only local source reads/searches and this report edit; no polling loop or runtime work was performed.

### Initial Review Fingerprints

Reviewed the actual landed production code after initial call mapping, including the later tightened classifier. This is a bounded shared-worktree snapshot, not a review of future changes. SHA256 at the final source read:

- `backend/internal/operations/service.go`: `FA52AE35C7402485E67FA659AD7D48DC1E4C18B2F4ACA79424FDBC53DD7CD690`
- `backend/internal/operations/repository.go`: `901703EE5F745EAA8F7CEA9EFF125F2EF4A81403FC4BE3EB93079FFC3D916CEA`
- `backend/internal/operations/memory_repository.go`: `B92ABB465B08650277D73239C4388BB0F3D1EE2F957F26CDA76A7107B732A059`

Capabilities used: local source reads/searches, read-only Git diff/status, the existing wrapper handoff, and apply_patch for this report only. The installed review skill was inspected; its network/subagent/committed-branch workflow was inapplicable to the explicit bounded source-only assignment. Historical memory was consulted only to locate checkout guidance; all findings derive from current source.

Only this new report was written. No production code, tests, other reports, or existing changes were modified. No tests, builds, formatters, Docker, services, network, subagents, commits, or runtime reproductions were performed. Parent retains ownership of executable verification, transaction rollback/commit proof, PostgreSQL locking/concurrency, and final acceptance.
