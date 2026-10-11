# Domain Model Specification

The core concepts and how they relate.

## Entities

| Entity | Meaning | Key fields |
| --- | --- | --- |
| Context Memory | A remembered fact/preference/decision/contact | project, kind, content, tags, confidence, archived |
| Connected Source | A local folder / JSON feed to ingest | connector, status, schedule, last-sync |
| Extraction | A record extracted from a source | source, content, provenance, review-state |
| Workflow Item | A unit of work moving through states | state, checklist, approval, retries/dead-letter |
| Pursuit | A longer-running, bounded goal grouping related work | goal, why it matters, desired outcome, measurable success criteria, stop conditions, dependencies, target, review cadence, resource ceilings, evidence, next-actions |
| Pursuit Resource Event | An immutable owner-scoped effort, spend, or refund record for one pursuit | pursuit, kind, integer minutes or EUR cents, evidence, idempotency key, digest, occurred/recorded time |
| Pursuit Resource Reservation | An immutable owner-scoped hold taken immediately before one pursuit task attempt | pursuit, operation ID, estimated effort minutes, estimated EUR micros, actor, digest, reserved time |
| Pursuit Resource Settlement | The immutable consumed or released result of one resource reservation | reservation, disposition, actual effort minutes, actual EUR micros, evidence, reconciliation reason, actor, digest, settled time |
| Pursuit Portfolio Plan | A transient owner-scoped comparison and schedule proposal across open pursuits | plan ID, as-of time, horizon, explicit capacity windows, explicit estimates, 25-factor priorities, exclusions, deterministic schedule, approval flags, advisory authority |
| Pursuit Portfolio Allocation | An immutable owner-accepted capacity allocation that remains non-executable | owner, plan/request/decision digests, schedule items, resource reservations, approval reasons, actor, exact confirmation, acceptance time, record digests |
| Pursuit Portfolio Execution Proposal | An immutable, non-executable snapshot prepared from one accepted allocation | owner, allocation and snapshot digests, proposal status, item records, policy evidence, prepared time |
| Pursuit Portfolio Proposal Item Decision | Append-only owner evidence recording one decision about one immutable proposal item | owner, proposal item and state digests, decision, reason, actor, exact confirmation, decision time, record digest |
| Pursuit Portfolio Workflow Authorization | An immutable unified-policy receipt for one server-fixed reversible workflow-intake effect; never an execution result | owner, current proposal/decision digests, fixed action/tool/runtime, effect digest, policy outcome, receipt ID, evaluated time |
| Pursuit Portfolio Dispatch Run | An immutable explicit selection of approved proposal items for bounded local workflow coordination | owner, proposal and selection digests, selected item IDs, actor, exact confirmation, request and record digests, request time |
| Pursuit Portfolio Dispatch Item Result | One append-only attempt outcome for a selected item | run, proposal/item/decision digests, attempt number, outcome, authorization receipt, review-gated workflow, replay flag, message, record digest |
| Task Operation | A durable owner-scoped identity for one direct plan or run request | idempotency key, request digest, mode, status, lease owner/generation, plan link, completion result |
| Verification Run | A grounded-answer check | claim, evidence links, verdict |
| Audit Event | An immutable record of an action | actor, action, resource, result, at |

## Relationships

```
Connected Source ──produces──▶ Extraction ──feeds──▶ Memory / Workflow Item
Workflow Item ──belongs to──▶ Pursuit
Task/Answer ──uses──▶ Memory (retrieval) + Source (evidence) ──▶ Verification Run
Task Operation ──claims/fences──▶ one Task plan/run attempt ──links──▶ immutable Completion Plan
Every state-changing action ──emits──▶ Audit Event
```

## Lifecycles

- **Workflow Item:** intake → planned → awaiting_approval → executing →
  done/failed (failed may replan). Enforced by a state machine
  (`internal/statemachine`).
- **Memory:** active ⇄ archived → deleted; retention policy decides archival/
  deletion candidacy (`internal/retention`).

## Ownership & scope

Pursuits follow a candidate -> accepted/active -> waiting/blocked -> completion
candidate -> verified complete/archive lifecycle. Completion fails closed while
explicit success criteria, dependencies, stop conditions, or required evidence
remain unresolved. New workflow work is refused after a triggered stop, an
overdue target pending review, an explicitly blocked dependency, or a reached
parallel-workflow ceiling. Pending dependencies remain plannable so HAI can
create the work that satisfies them. Review completion uses the pursuit's
configured cadence rather than a global interval.

Configured effort and spend ceilings are enforced from the pursuit's immutable
resource-event and reservation ledgers. Intake, planning, approved next-action
creation, and direct task attempts fail closed when usage cannot be verified or
a ceiling is exhausted. Immediately before execution, each pursuit task attempt
revalidates eligibility and atomically reserves conservative effort and provider
cost. Settlement consumes or releases that reservation and records actual use
transactionally. Reservation and settlement mutations include their audit
activities and pursuit freshness update in the same commit, so an audit-store
failure cannot leave a hidden hold or return a false post-commit error. Refund
events cannot exceed recorded net spend. A crash leaves
the hold active for review; automatic reconciliation remains a separate
operational control. Measured runtime effort is not represented as human effort.

Direct task planning and execution use a separate durable operation ledger. An
owner and idempotency key bind to one normalized request digest and mode. A
single worker may hold the active lease; generation checks fence stale workers.
Completed requests replay their stored completion plan, changed input conflicts,
and expired or uncertain attempts stop in `needs_review` instead of being run
again automatically. This is local at-least-once handling with durable replay
fencing; it is not a claim of exactly-once behavior in external systems.

## Advisory Pursuit Portfolio Planning

The pursuit portfolio planner is a read-only decision boundary over the
authenticated owner's open pursuits. A request must provide a bounded plan ID,
an `asOf` time, planning horizon, at least one explicit owner-capacity window,
and one to 500 pursuit estimates. Each included pursuit supplies:

- optimistic, expected, and pessimistic duration plus an optional estimate
  basis;
- estimated cost, input/output tokens, and tool calls;
- all 25 priority inputs used by the canonical LifeOps weighting algorithm;
- optionality for fallback planning.

The pure priority evaluator produces a score, band, reasons, algorithm version,
and a contribution record for every factor. It does not assign an assessment ID
or append priority history. Given identical normalized inputs, the resource
planner produces the same dependency-aware schedule, budget assessment,
critical blockers, approval flags, fallback stages, and decision digest.

Pursuit dependencies are resolved only inside the same owner-visible open set.
An external unresolved dependency, unavailable related pursuit, missing
dependency estimate, cycle, or unschedulable predecessor remains explicit; it
is never silently ignored. Missing pursuit estimates are reported as
`estimate_required`. Waiting, blocked, overdue, or stop-triggered pursuits are
excluded. When resource usage or active reservations cannot be verified, the
pursuit is excluded with `resource_ledger_unavailable`. Pessimistic effort and
estimated spend are checked against the remaining immutable pursuit-ledger
ceilings before scheduling.

The schedule uses one unit of `owner-capacity`, so owner work cannot overlap
inside the supplied windows. In production, those windows are only candidate
calendar intervals: the planner loads the authenticated owner's latest durable
LifeOps capacity snapshot, recomputes its 24-hour freshness against server time,
and caps the union of the intervals to the confirmed available minutes. The
same snapshot overrides browser-supplied `availableCapacity` and `energyFit`
priority inputs. Missing, stale, low-confidence, review-required, wrong-owner,
or unavailable state returns a typed non-reservable capacity result. In-horizon
pursuit targets become hard deadlines.
Risk, autonomy, and existing approval requirements remain visible as approval
flags and reasons; planning never weakens them.

The result always declares `authority: advisory_only` and `canExecute: false`.
It does not persist the proposed ranking or allocation, mutate a pursuit,
reserve resources, consume an approval, enqueue a task, or write to a calendar.

## Governed Pursuit Portfolio Allocation Acceptance

An authenticated owner with approval permission can submit the original
planning request, its expected decision digest, and the exact confirmation
`ACCEPT PORTFOLIO ALLOCATION` to the allocation-acceptance boundary. For a new
acceptance, the proposal must be fresh and HAI recalculates it against current
pursuit and resource state. A changed request, changed decision, critical
blocker, unavailable pursuit, ambiguous owner, or resource-ceiling conflict
fails closed.

A successful acceptance atomically appends an immutable allocation, one item
per scheduled pursuit, one owner-scoped capacity reservation per item, and an
audit activity on every pursuit. Approval flags are retained on the affected
items. Exact owner-and-plan retries replay the stored result; reuse of the same
plan ID with different digests is rejected. Allocation and item rows are
append-only at the database boundary.

Acceptance returns `authority: allocation_only` and `canExecute: false`. It
does not approve work, consume an approval capability, change pursuit state or
priority, enqueue a task, invoke a runtime, or write to a calendar. Governed
execution and external-calendar acceptance remain separate authority and
evidence gates.

Accepted allocations remain discoverable through the owner-scoped, bounded
history projection. The read path revalidates parent and item identities,
digests, schedules, approval-state consistency, and exact acceptance evidence
before returning records. History is inspection evidence only and repeats the
same `allocation_only` / `canExecute: false` authority contract.

## Governed Portfolio Execution Proposals

An authenticated owner with approval permission can prepare, but not authorize,
one execution proposal per accepted allocation item through
`POST /api/v1/pursuits/portfolio-allocations/:allocationId/execution-proposals`.
The request must bind the allocation's current immutable record digest and use
the exact confirmation `PREPARE EXECUTION PROPOSALS`. The owner in the route
operation comes from the authenticated identity; another owner's allocation is
not visible to the preparation boundary.

Preparation loads the accepted allocation, its items and resource-reservation
bindings, then captures the current owner-scoped pursuit state used for policy
evaluation. The immutable snapshot binds the allocation and item record digests,
reservation IDs, pursuit status, risk, autonomy, next action, completion state,
active stop conditions, dependencies, and whether a reservation is already
settled. Proposal and item records carry independent lowercase SHA-256 record
digests and are append-only under migration `0039`.

The parent status is derived from its item evidence: `prepared` when no current
gate is found, `prepared_needs_approval` when at least one item requires separate
approval, and `prepared_blocked` when at least one item is blocked. These states
describe proposal preparation only. Every response declares
`authority: proposal_only` and `canExecute: false`.

The allocation ID plus current state-snapshot digest is the replay identity.
Preparing again with unchanged state returns the exact stored proposal and
marks it as replayed. A legitimate change in pursuit, policy, dependency, stop
condition, or reservation-settlement state produces a different snapshot digest
and therefore a new immutable proposal; the earlier proposal remains audit
evidence. Allocation-digest drift fails closed before either path.

Proposal preparation does not approve an item, consume an approval capability,
enqueue a task, invoke a model, tool, script, container, API, or agent runtime,
settle or release a reservation, mutate pursuit state, or execute any effect.
Approval and execution remain separate governed stages.

## Per-Item Proposal Decisions

This governed stage records an owner's decision about one immutable
execution-proposal item through:

`POST /api/v1/pursuits/portfolio-execution-proposal-items/:itemId/decisions`

The decision value and its exact confirmation phrase are paired as follows:

| Decision | Exact confirmation |
| --- | --- |
| `approved` | `APPROVE EXECUTION PROPOSAL ITEM` |
| `rejected` | `REJECT EXECUTION PROPOSAL ITEM` |
| `needs_clarification` | `REQUEST CLARIFICATION FOR EXECUTION PROPOSAL ITEM` |
| `revoked` | `REVOKE EXECUTION PROPOSAL ITEM` |

Each accepted write appends owner-scoped decision evidence; it never edits or
deletes the immutable proposal item or an earlier decision. The decision is
bound to the exact proposal-item record digest and captured state digest. At
write time, the service must resolve the item through the authenticated owner,
revalidate both digests and the current decision transition, and fail closed on
owner mismatch, stale evidence, an invalid transition, or a confirmation that
does not exactly match the requested decision.

Every response from this stage declares `authority: approval_decision_only` and
`canExecute: false`. An `approved` record is evidence of the owner's decision;
it is not a runtime capability, queue instruction, consumed approval proof, or
concrete final-effect authorization. The endpoint must not enqueue or execute
work, invoke a model/tool/runtime/provider, settle a reservation, or mutate a
pursuit. Any later execution command must independently revalidate current
state and obtain a separate authorization bound to the exact intended effect.
Migration `0040`, the authenticated routes, service, UI, automated suites,
disposable-PostgreSQL acceptance, and local browser-to-database decision flow
are verified. This does not claim that concrete execution or an external effect
is live.

## Exact Portfolio Workflow Authorization

An unexpired latest `approved` proposal-item decision may be presented to:

`POST /api/v1/pursuits/portfolio-execution-proposal-items/:itemId/authorize-workflow`

The request contains only the expected proposal-item digest, expected approval-
decision digest, and exact confirmation
`AUTHORIZE PORTFOLIO WORKFLOW EFFECT`. The caller cannot choose an action,
resource type, tool, runtime, autonomy level, risk, reversibility, or price.
Those fields are fixed by the server to one reversible local
`pursuit.portfolio.create-workflow` / `workflow.intake` effect through
`hai-workflow-engine` at estimated cost EUR 0.

Immediately before policy evaluation, HAI independently reloads the owner-
scoped proposal, item, accepted allocation item, pursuit, reservation state,
requested decision, and latest decision. It rejects a foreign owner, changed
digest, non-latest or non-approved decision, expiry, revocation, settlement,
or changed pursuit state. The canonical execution-authorization service then
resolves the same `portfolio-decision:<decisionId>` source independently and
binds it to the server-derived effect digest.

The response declares `authority: execution_authorization_only` and
`canExecute: false`. It includes the exact effect and the persisted immutable
policy receipt, whose outcome can be `authorized`, `requires_approval`, or
`denied`. This stage deliberately calls `Authorize`, not
`AuthorizeAndConsume`: it does not consume the receipt, create or queue a
workflow, invoke a tool/runtime, settle a reservation, mutate a pursuit, or
claim completion.

## Single-Use Portfolio Workflow Creation

An owner may separately present an authorized receipt to:

`POST /api/v1/pursuits/portfolio-execution-proposal-items/:itemId/execute-workflow`

The request repeats the immutable item and decision digests, identifies the
receipt, and requires the exact phrase `CREATE APPROVED PORTFOLIO WORKFLOW`.
The caller still cannot select a tool, runtime, resource, cost, risk, or action.
HAI reloads the receipt-bound decision and exact effect, verifies the current
approval before first use, and consumes the receipt once for consumer
`pursuit-portfolio-workflow` and target `workflow-intake:<effectDigest>`.

The only applied effect is one local workflow intake with source type
`portfolio_workflow_effect` and source ID equal to the receipt ID. It starts in
`needs_approval`; it is linked to the pursuit but is not run, does not settle
the resource reservation, and grants no downstream authority. An owner/source
unique partial index prevents concurrent duplicate active workflows. If an
interruption occurs after receipt consumption, an exact retry may resume the
same idempotent intake even after approval expiry; a different consumer,
target, receipt, decision, item, or digest fails closed. A completed replay
loads the linked receipt-bound workflow directly, so it does not repeat intake.
Link creation and the final effect activity are independently idempotent; a
nullable deterministic activity key protects concurrent retries while leaving
older audit history unchanged. The response declares `authority:
workflow_effect_executed` and still returns `canExecute: false`.

## Governed Portfolio Dispatch Coordination

An owner may coordinate a bounded explicit subset of one immutable proposal
through two proposal-scoped endpoints:

- `GET /api/v1/pursuits/portfolio-execution-proposals/:proposalId/coordination`
  is a read-only current-eligibility projection;
- `POST /api/v1/pursuits/portfolio-execution-proposals/:proposalId/dispatch`
  records and processes one exact selection of one to 20 items.

The dispatch request repeats the immutable proposal digest and, for every
selected item, the item and latest approved-decision digests. No item is
implicitly selected. The exact confirmation is
`DISPATCH APPROVED PORTFOLIO WORKFLOWS`. Authentication supplies the owner and
actor; the caller cannot provide a runtime, tool, action, risk, autonomy, cost,
receipt, workflow ID, or external target.

The coordinator does not create approval decisions. For each selection it
reloads the owner-scoped proposal, item, decision, pursuit, reservation, and
policy evidence, then invokes the existing exact-effect authorization and
single-use receipt consumption contracts. A successful result is only one
receipt-bound local workflow in `needs_approval`. It is not workflow execution
or completion and it does not settle capacity or perform an external effect.

The immutable dispatch run is idempotent by owner and normalized request
digest. Per-item results are append-only attempts. Exact retries reuse the run,
skip terminal successes, and can recover unfinished items without creating a
second workflow. Current outcomes are `workflow_created`, `replayed`,
`needs_approval`, `blocked`, `stale`, `failed`, or `cancelled`. The database
validates every run against its proposal and selected items and every successful
result against the approved decision, authorized receipt, and current
receipt-bound workflow. Mutation and truncation of either ledger are rejected.

Coordination previews declare `authority: coordination_preview_only` and
dispatch results declare `authority: portfolio_dispatch_result`; both return
`canExecute: false` because no workflow is run and no downstream or external
authority is granted.

## Verified Portfolio Workflow Settlement

After that exact linked workflow has completed through the normal worker with
verification status `verified` or `test_passed`, the same worker transaction
appends an immutable completion attestation. Weaker labels such as
`source_supported`, `schema_validated`, or `human_approved` are not accepted as
proof of executed work. An owner may then separately append measured usage
through:

`POST /api/v1/pursuits/portfolio-execution-proposal-items/:itemId/settle-workflow`

The request identifies the linked workflow, repeats the immutable proposal-item
digest, supplies actual effort minutes and EUR micros, and requires the exact
phrase `SETTLE VERIFIED PORTFOLIO WORK`. HAI reloads the owner-scoped item,
original approval decision, receipt, single-use receipt consumption, pursuit
link, completed workflow projection, and immutable completion attestation. It
requires exactly one receipt-bound workflow link and derives the verification
evidence URI from the attestation; the caller cannot substitute unrelated
evidence.

Settlement and its portfolio-specific proof are append-only and idempotent.
The settlement, proof, effort/cost events, and audit activity commit in one
transaction after locking and revalidating the complete evidence chain. An
exact retry returns the existing consumed settlement, including after the
mutable workflow projection is archived; changed measured usage under the same
reservation fails closed. Incomplete, weakly verified, foreign, unlinked,
duplicately linked, differently consumed, unattested, or receipt-mismatched
workflows cannot settle capacity. The response declares
`authority: verified_accounting_only` and `canExecute: false`: it accounts for
already verified work and grants no new runtime or provider authority.

After that authoritative transaction succeeds, HAI writes a separate,
idempotent controlled-learning outcome from the immutable settlement proof,
completion attestation, authorization receipt, and estimated-versus-actual
resource values. Three comparable verified outcomes in the same owner/project
scope can create a deterministic, bounded median/MAD estimate-calibration
proposal. The proposal is inert until explicit owner approval. An applied
revision remains separately queryable and rollback-aware; planning can show it
as an optional suggestion, but uses it only when the owner explicitly copies
the suggestion and submits an exact proposal/application/evidence binding.
Approval, rejection, and rollback anchor the reviewed evidence window;
another revision requires three fresh comparable outcomes. A pending proposal
suppresses competing proposals while retaining the new-evidence count and a
deterministic material-drift signal.
Raw and reviewed estimates remain visible. Calibration cannot change priority,
risk, budget, permissions, safety boundaries, or execution authority. A
learning-ledger outage does not invalidate committed accounting; exact
settlement replay retries only the evidence write and never repeats workflow
work.

Memories and sources are scoped by project key; project-scoped queries never
leak across projects (proven by isolation tests). Multi-user roles are persisted
by the IDP, signed into access tokens, verified by backend identity middleware,
and enforced through route-specific `internal/rbac` permission guards.

## Advisory Ambient Outcome Monitoring

An `OutcomeMonitorTarget` is mutable scheduling state for one immutable
`Owner -> Workspace -> OutcomeDefinition -> OutcomeIndicator -> SourceKind`
tuple. Its cadence, due time, enabled state, lease, and last-run projection may
advance only through monotonic revisions. Its identity and scope do not change;
targets are disabled rather than deleted.

`SourceKind` is a closed enum:

- `workflow_open_loop_count`: current due/open workflow loops for the owner;
- `workflow_verified_completion_count`: owner workflow completion attestations
  whose result is `completed` and verification is `verified` or `test_passed`;
- `overdue_commitment_count`: latest owner commitment revisions with a past due
  time and an open/breached/disputed lifecycle state.

Each collector returns one numeric value, observation time, and digest of a
bounded deterministic source snapshot. It cannot accept arbitrary queries,
network destinations, scripts, expressions, or tool instructions.

An `OutcomeObservationRecord` is immutable source evidence linked to the exact
target, outcome, indicator, source kind, source digest, and recorded time. An
`OutcomeMonitorRun` is the immutable receipt for one lease generation and
records success/failure, completion time, observation reference, redacted
failure information, and replay digest. Repository completion and composition
use these digests so an exact retry reuses prior evidence rather than observing
the world again.

The `AmbientMonitorComposer` maps eligible observation history into the
existing `OutcomeEvaluation` evidence contract with a safe local
`hai://ambient-monitor/observations/...` provenance link. It then idempotently
records/evaluates a proactivity signal so the existing owner attention inbox
can surface the result. The outcome definition remains authoritative for
indicator direction, target, evidence window, and evaluation state.

Every target, observation, run, completion, and composed signal carries
`advisory_monitor_only`. Execute, deliver, notify, Calendar-write,
workflow-mutation, mandate-authorization, and learning-mutation capabilities
are false. Composition can change only advisory evaluation and attention
records; it cannot perform or authorize the work being measured.
