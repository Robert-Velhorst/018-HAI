# Backend test reconciliation

## Scope and method

This batch reviews the complete diffs of the original checkout's 25 selected
untracked backend and migration test files against main
`847ce573a65659abca85bb8897a42e386ab8fe2c`. It complements the ten-file
[authentication review](auth-test-reconciliation-2026-10-20.md).

The decisions below distinguish preserved coverage, fixture/API adaptation and
deliberately superseded expectations. They are not based on filename or test-name
presence alone. Changed safety expectations were also checked against current
production code. No automatic equivalence or deletion is inferred.

## Per-file decisions

Paths are relative to `backend/`.

| File | Decision from the full diff |
| --- | --- |
| `internal/ambient/scheduler_test.go` | Main retains the old cases and adds a disabled-policy scheduler-status check. No old coverage needs restoring. |
| `internal/automation/public_projection_test.go` | Credential round-trip assertions remain; the fixture now supplies an explicitly accepting event publisher rather than an unconfigured publisher. |
| `internal/background/worker_reliability_test.go` | Keep starvation protection, but require newly ingested source work to await approval instead of claiming it was executed and verified. Keep sanitized feed-failure reports rather than restoring source-name exposure. The helper rename avoids a test-name collision. |
| `internal/executionauth/transactional_test.go` | The fixture captures one `fixedNow()` value for decision/approval timestamps. Transactional authorization assertions remain. |
| `internal/llm/outbound_transport_test.go` | The fixture sets `httptest.Server.TLS` before `StartTLS`, not an overwritten nested server configuration. Existing outbound transport assertions remain. |
| `internal/operations/ingest_atomicity_regression_test.go` | Existing atomicity, collision and stored-state assertions are adapted to the raw-evidence digest field. Keep the digest alongside evidence updates. |
| `internal/operations/repository_ordering_test.go` | Main adds a transaction-capable fake driver and claim-next ordering/policy assertions. Existing list and due ordering checks remain. |
| `internal/operations/source_approval_test.go` | Keep raw-evidence digest binding, exact stale-receipt diagnostics, same-version mutation rejection and explicit approval overriding a future review reminder. Main strengthens the retained revision-binding cases. |
| `internal/opscontrol/emergency_stop_fanout_test.go` | Stop-before-fanout/partial-delivery assertions remain; the request now includes the required JSON reason and content type. |
| `internal/phase2/execution_claim_postgres_integration_test.go` | Main retains atomic/fenced claims and adds explicit claiming and release of an exactly approved source operation. |
| `internal/pursuit/completion_outcome_integrity_test.go` | Main adds shared review-state predicates, exact receipt batches, owner/missing-receipt rejection, all explicit links beyond the display cap, and rejection of redirect/server-error completion codes. Retained uncertainty protections remain. |
| `internal/pursuit/completion_outcome_postgres_test.go` | Main distinguishes the default display cap from an explicit expanded limit and retains exact linked-receipt assertions. Do not restore an assertion that an explicit larger limit still behaves as the default cap. |
| `internal/router/opscontrol_emergency_stop_permissions_test.go` | Verified-admin authorization assertions remain; the request includes the required reason and JSON content type. |
| `internal/source/trello_reopen_test.go` | Reopen assertions remain. The HTTP fixture now returns update actions for `filter=all`, matching current reconciliation instead of always returning an empty action feed. |
| `internal/task/operation_transition_context_test.go` | Keep context-aware claims and durable known pre-execution cancellation. Do not restore an uncertain-outcome review item when no executor ran. Cancellation after execution remains a separate review boundary. |
| `internal/task/required_participant_admission_test.go` | The positive fixture now includes a verified, healthy, recently checked agent card. Do not restore a ready-only delegation as sufficient specialist evidence. |
| `internal/verification/freshness_test.go` | Stale-source rejection and review-state assertions remain with explicit grounded-mode/request arguments. |
| `internal/workflow/project_dossier_test.go` | Main renames a synthetic credential fixture and changes its repeated test character; nested-context bounds and forbidden-output checks remain. |
| `internal/workflow/reminder_context_boundaries_test.go` | Keep cancellation call-order/confirmed-work checks. Main requires no summary before any work, and an accurate summary once work was confirmed; do not fabricate a non-nil zero-work result. |
| `migrations/protected_data_rollback_postgres_integration_test.go` | The synthetic migration filesystem additionally contains the expected `post` directory. Protected-data rollback assertions remain. |
| `migrations/runtime_db_role_provisioning_contract_test.go` | Main normalizes CRLF before inspecting provisioner source. Default-password refusal before external commands remains. |
| `migrations/source_configuration_postgres_test.go` | Main upgrades runtime prerequisites, separately tests phase-order refusal, and reaches the managed-origin down-SQL guard in an isolated transaction. Do not remove later-phase protection merely to reach an older guard expectation. |
| `migrations/source_head_postgres_test.go` | Main includes the raw-evidence migration prerequisite and tests populated-head rollback in a separate pre-only database, preserving post-phase runtime history in the first fixture. |
| `migrations/source_observation_postgres_test.go` | Runtime subtests include the raw-evidence migration and expect the current later-phase refusal diagnostic. Historical observation constraints remain. |
| `migrations/trello_webhook_reconciliation_generations_integration_test.go` | Main bounds the synthetic migration set to the specific reconciliation migration, preserving one-apply/zero-reapply and populated-history rollback checks without accidentally applying later migrations. |

## Product-code cross-checks

- `internal/background/worker.go` routes approval-gated work to
  `StatusAwaitingApproval` and validates source approval before safe execution.
- `internal/operations/domain.go`, `service.go` and `source_approval.go` mint,
  update and bind `SourceEvidenceRawSHA256` to the approved revision.
- `internal/task/task_operation_service.go` records whether execution started.
  If cancellation precedes execution, it persists a fenced canceled result;
  cancellation after execution retains evidence and requires review.
- `internal/task/service.go` requires matching verified, healthy, non-revoked
  agent cards with verification timestamps for required participants.

These checks explain the selected non-equivalent safety expectations; they do
not prove every worker, task or migration behavior correct.

## Verification and retention boundary

This is source reconciliation, not a new local runtime acceptance run. No Go
suite, real database, HTTP provider or external account was started for this
documentation change. Existing test assertions and production branches were
read directly; exact-head CI remains required before merge.

The retained file identities are revalidated in a private inventory. None of
these old copies is asserted byte-identical to current main, and none is removed.
No whole-worktree, transcript or backup is declared reconciled by this batch.
Remaining production variants, non-Go files and operational evidence still need
review. Older expectations listed as superseded must not be silently restored.
