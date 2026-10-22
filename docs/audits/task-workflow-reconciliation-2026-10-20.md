# Task and workflow production reconciliation

## Scope and outcome

Review full diffs of eleven selected tracked backend file versions against
public main commit `847ce573a65659abca85bb8897a42e386ab8fe2c`. Verify original
source integrity against the private inventory. Keep the newer implementations
on main instead of restoring omitted context, history or approval protections.

No missing retained implementation was identified in these selected differences.
The review exposed an independent source-revision encoding defect in the newer
success-criteria path. Repair that boundary instead of removing explicit
success criteria from workflows.

## Per-file decisions

Paths below are relative to `backend/internal/`.

| File | Decision |
| --- | --- |
| `assistant/service.go` | Keep context-aware task/cycle dispatch, attached partial cycle results and review/error propagation. Legacy engine interfaces remain supported; context is not authority. |
| `task/service.go` | Keep opt-in bounded read-only conversation MCP context, untrusted provenance, carried retry traces and suppression of retries that cannot repair untrusted evidence. Keep compact history, trusted context handling, fresh verified required-agent admission and sanitized MCP trace arguments. |
| `task/handler.go` | Keep owner-scoped compact history by default, single bounded limit validation and explicit bounded full-record access. Do not restore the large list response. |
| `task/resource_planning.go` | Keep explicit automated-execution intent in addition to risk admission before assigning automatic runtime work. |
| `task/agent_context_provider.go` | Keep caller-bound owner-scoped registry reads and cancellation rechecks for compatible legacy providers. |
| `task/task_operation_service.go` | Keep context-aware fenced claims, distinct durable pre-execution cancellation and review after uncertain execution. Clear both trusted context fields from persisted review requests. |
| `task/state_repository.go` | Keep bounded numeric normalization for JSONB-stable digests, normalized empty success criteria, compact immutable history contracts and context-aware repository extensions. |
| `task/state_repository_postgres.go` | Keep bounded owner-scoped immutable history projection, metadata validation and caller-bound completion-plan lookup. |
| `workflowtask/runner.go` | Keep copied explicit success criteria when building the task request; the older version omitted them. |
| `workflow/service.go` | Keep normalized bounded success criteria across intake, persistence, execution and source revision binding. Fix delimiter ambiguity separately. |
| `workflow/handler.go` | Keep response suppression after caller cancellation and optional JSON-body validation that tolerates EOF but not malformed input. |

## Important retained boundaries

Conversation-history MCP results remain untrusted context, not primary evidence
or instructions. Planning reports adapter availability without speculative tool
calls. Execution has bounded model-directed read-only retrieval and audit traces.
Neither this comparison nor existing local tests establish live MCP/provider
acceptance.

Required framework participants need fresh, healthy, verified runtime cards.
Caller-editable registry declarations are not accepted as runtime verification.
Do not restore older logic that only checks whether a delegation says ready.

Pre-execution cancellation is distinct from an interrupted operation whose side
effects may already have started. Settlement can outlive the caller through the
bounded durable reconciliation path, but it cannot invent confirmed completion.
This review does not prove universal cancellation of every external provider.

The compact history path uses owner-scoped durable projections instead of
loading full execution evidence for each list entry. Explicit full views remain
available with bounded result counts. JSON number normalization is retained to
avoid digest mismatches after PostgreSQL JSONB rendering; it is not a substitute
for live migration/restore acceptance.

## Source-revision boundary repair

The previous encoding joined success criteria, selection context and scalar
fields with raw separator characters. A literal separator inside a value could
therefore alias a different list or field boundary. In particular, one criterion
containing the criterion separator could hash identically to two criteria,
causing intake to reuse a workflow with outdated acceptance requirements.

Escape embedded separators and a reserved escape prefix before joining each
level. Preserve ordinary existing hashes so routine source rescans do not all
appear to be new revisions. Values containing the reserved syntax intentionally
receive corrected revisions. The repair does not change execution approval,
source ownership or permission boundaries.

Regression coverage includes nested criteria, selection context, adjacent
scalar fields, literal escape syntax, a frozen ordinary legacy digest and the
actual intake/replay path with the existing in-memory repository fixture.
The focused regression command passed locally on Windows with Go 1.27.3:

```text
go test -p 1 -timeout 180s ./internal/workflow -run 'TestWorkflowSourceRevisionSeparatesNestedValues|TestWorkflowSourceRevisionPreservesOrdinaryLegacyDigest|TestIntakeDoesNotDeduplicateChangedCriterionBoundaries' -count=1
```

An earlier attempt exhausted build storage before tests executed; it is not a
demonstrated failing regression run. The successful run used a different local
build-cache location with available storage. These in-memory regression tests
do not establish live source/provider or PostgreSQL acceptance. The full package
also passed locally with `go test -p 1 -timeout 180s ./internal/workflow -count=1`.
Hosted exact-head CI remains a separate release gate.

## Retention

This batch does not establish equivalence of all remaining worktrees, files or
transcripts. Original sources, archives, runtime state, credentials, databases
and backups remain intact. No source deletion follows from a semantic review
alone. Hosted exact-head CI and post-merge preservation checks remain separate
gates before consolidation or cleanup.
