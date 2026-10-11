# Framework Operating Contract Matrix

This matrix maps the 55-family research specification to code in the canonical
Go/Angular HAI stack. It deliberately separates three states:

- **enforced**: the canonical task or workflow path makes a decision from the
  contract and fails closed when its condition is not met;
- **structured**: a versioned, persisted, inspectable contract exists, but the
  complete real-world domain or distributed runtime is not present;
- **catalogued**: the family is decision metadata or an experimental candidate,
  not an installed or trusted product.

No row should be read as evidence that an external provider, account, agent
framework, or domain service is configured on a particular machine.

## Cross-Cutting Families

| No. | Family | Repository state | Operational boundary |
| ---: | --- | --- | --- |
| 1 | Human sovereignty | **Enforced** through the versioned Constitution, protected rules, authority ceilings, approval gates, and immutable activation history. | HAI cannot infer or amend Robert's values automatically. |
| 2 | Whole-life ontology | **Structured** as an owner-scoped append-only graph for people, needs, goals, assets, obligations, projects, cases, opportunities, risks, sources, documents, pursuits, workflows, tasks, memories, commitments, costs, and outcomes. | Coverage is incremental: graph presence remains advisory and does not prove every product ledger or external account is projected. |
| 3 | Human needs and wellbeing | **Structured** as sourced needs-state assessments or explicitly review-marked deterministic inferences. | It is not a clinical assessment and never silently becomes medical fact. |
| 4 | Personal state and capacity | **Enforced** from the authenticated owner's latest durable LifeOps snapshot. Portfolio planning recomputes 24-hour freshness on server time, requires owner match, minimum confidence, and no review flag, overrides browser capacity/energy factors, and caps supplied calendar windows to confirmed minutes. Missing, stale, review-required, overloaded, and unavailable states constrain or block work. | No browser request may self-assert capacity; live wearable/health sensing is not claimed. |
| 5 | Goal hierarchy | **Enforced at the pursuit boundary** through a durable outcome contract, immutable owner-scoped resource ledger, atomic execution reservations, and owner-scoped task-operation claims. Direct and workflow-owned task execution binds a request digest and mode to one idempotency key, heartbeats a fenced lease, and derives pursuit reservation identity from that durable operation. Matching completed retries replay the immutable plan; changed, concurrent, expired, or lease-lost requests fail closed. Uncertain operations are projected into the durable owner review queue; approving one creates a separately identified attempt and rejecting it closes the queue item without changing the original operation. An owner-scoped advisory portfolio planner can compare explicitly estimated open pursuits and propose a deterministic dependency-aware allocation across explicit capacity windows. The owner can durably accept an unchanged fresh proposal as immutable allocation items and capacity reservations. Explicitly selected, currently approved proposal items can now be coordinated through immutable batch-dispatch and per-item attempt ledgers into receipt-bound review-gated local workflows. | A process crash deliberately leaves uncertain operation/resource state visible for review and never auto-releases capacity based only on age. Runtime effort is a conservative machine-execution measure, not a claim about human effort. Planning is `advisory_only`; acceptance is `allocation_only`; coordination preview and dispatch still return `canExecute: false`. Dispatch never records approval, runs a workflow, settles capacity, or performs an external effect. Full strategy/programme optimization, external calendar acceptance, downstream automatic workflow execution, and exactly-once external effects remain incomplete. |
| 6 | Intake and triage | **Enforced** by task/workflow classification, risk, context/tool needs, and explicit success criteria. | Connector quality still depends on each configured source. |
| 7 | Prioritisation | **Enforced** in workflow priority and attention queues; selected frameworks add need, risk, and capacity context. The pursuit portfolio planner applies the same pure 25-factor LifeOps evaluator to explicit owner inputs and returns every weighted contribution, reason, band, and algorithm version without persisting an assessment. | Factor quality remains operator/source dependent. Portfolio planning is bounded and deterministic, not globally optimal or self-authorizing. |
| 8 | Multi-agent organisation | **Enforced locally as an advisory team contract** with server-expanded coordinator/reviewer roles, capability and authority ceilings, exact memberships, lifecycle revisions, evidence provenance, and an authenticated Basic/Advanced operator workspace. | A team membership never creates a runtime agent, grants tool access, or authorizes execution. Distributed A2A discovery and external team delivery remain absent. |
| 9 | Agent identity and capability | **Enforced** through complete cards for identity, owner, purpose, role, competence, tools, permissions, data boundaries, cost/model profile, reliability, schemas, evidence, escalation, availability, version, dependencies, health, evaluation, authority, revocation, provenance, and 24-hour verification freshness. | Runtime-specific capability probes must still supply trusted evidence. |
| 10 | Delegation and accountability | **Enforced** as deterministic delegation IDs, outcome, zero-spend budget, deadline, constraints, authority, evidence, completion, escalation, and state. | A delegate cannot grant itself authority or approve its own consequential work. |
| 11 | Agent communication | **Enforced locally** by canonical message validation for schema, correlation, idempotency key, type, confidentiality, least authority, timestamp, expiry, payload size/digest, provenance, evidence references, voting-role eligibility, secret rejection, exact recipient acknowledgments, and terminal/retry semantics. Guided API commands keep IDs, timestamps, agent references, digests, and authority server-owned. | A distributed A2A transport, external delivery receipt, and cryptographic peer/signature authority are not bundled. |
| 12 | Multi-agent coordination | **Enforced locally for advisory deliberation** through durable team lifecycle, quorum policy, evidence-backed support/oppose/abstain messages, acknowledgments, deterministic consensus/conflict outcomes, attention states, and hash-linked audit events. The operator workspace loads ledgers progressively and rejects authority-bearing responses. | Modes requiring unavailable or stale agents remain blocked. No live consensus cluster, external multi-agent transport, or execution authority is claimed. |
| 13 | Cognitive and reasoning methods | **Catalogued and selected** by task type, difficulty, evidence, and uncertainty signals. | HAI records methods and outcomes, not hidden chain-of-thought. |
| 14 | Cognitive-agent architectures | **Structured** through the task loop, framework selector, context/model/tool routing, critic/validation, and review state. | Named external cognitive frameworks remain adapters or candidates. |
| 15 | Decision-making under uncertainty | **Enforced** through uncertainty labels, evidence requirements, conflict checks, and review escalation. | Probabilistic calibration needs longitudinal real outcomes. |
| 16 | Formal planning | **Enforced** through explicit task steps, dependencies where present, success criteria, risk, and bounded capacity step limits. The advisory pursuit planner additionally requires explicit three-point duration and usage estimates, resolves same-owner pursuit dependencies, checks immutable resource-ledger capacity, retains approval/risk flags, and schedules one-unit owner capacity deterministically inside candidate windows capped by the fresh owner-confirmed LifeOps snapshot. | General-purpose PDDL/constraint solving is not claimed. Missing estimates, stale/unreviewed personal capacity, unverifiable ledgers, unresolved dependencies, and ceiling conflicts are exclusions rather than invented inputs. The proposal cannot reserve or execute work. |
| 17 | Workflow modelling | **Enforced** through durable workflow state, transitions, checklist, source links, approvals, retries, leases, and completion checks. | Cross-region distributed workflow execution is absent. |
| 18 | Reliable execution | **Enforced** through planning/execution separation, idempotency, request-cancellable PostgreSQL claim acquisition, immutable pre-effect cancellation, fenced leases and bounded audit settlement, bounded retries, action-bound approval proofs, selector-v5 immutable-selection revalidation, pre-authorization evidence gating, postcondition verification, and visible failure states. | A cancellation is classified as effect-free only before the task callback starts. Interruption after planning or execution begins remains reviewable because durable or external effects may already have occurred. Exactly-once external side effects cannot be guaranteed. Typed execution and postcondition evidence phases exist, but not every catalog requirement yet has an independent requirement-specific runtime validator. |
| 19 | Autonomy levels 0-10 | **Enforced per action** using the exact observe, inform, recommend, draft, plan/simulate, prepare, case-approved, standing-approved, reversible-auto, execute/notify, bounded-full ladder. Durable owner-scoped standing mandates provide bounded, revisioned scope and stop-condition evaluation. One explicit mandate UUID is preserved across Assistant, pursuit candidate, workflow, task, automation approval, and unified authorization state. Immutable request, mandate snapshot, decision, and approval digests are independently re-resolved and compared immediately before effect consumption. Selector-v5 framework ceilings are also bound into task, workflow, and unified authorization evidence, and over-ceiling execution is rejected before policy evaluation. | A mandate reference never grants authority. The mandate must still be active and match the authenticated owner, exact action, resource, project/domain, tool, risk, autonomy ceiling, approval policy, expiry, and stop conditions at final effect time. HAI deliberately does not guess among multiple mandates. Its life-graph projection is context only; no global autonomy toggle or graph node grants authority. External runtime acceptance remains a separate deployment gate. |
| 20 | Approval | **Enforced** by owner-scoped review decisions, exact action-bound approval provenance, durable single-use proof consumption, and selector-v5 framework approval contracts. | HAI prevents proof replay across local instances, but cannot guarantee exactly-once effects in an external system after network ambiguity. |
| 21 | Memory architecture | **Structured** across task context and durable memory records with relevance and provenance. | All conceptual memory subtypes are not yet separate physical stores. |
| 22 | Personal knowledge management | **Structured** through sources, memory review/correction, project scoping, pursuit links, and the owner-scoped cross-entity life graph. | Automatic projection covers durable task plans, workflow transitions, connected-source documents, source provenance, standing-mandate lifecycle revisions, commitments, costs, outcomes, and project/pursuit links. Other product ledgers still need explicit projection adapters. |
| 23 | Retrieval and context | **Enforced** by relevance-ranked memory/source retrieval and explicit context plans. | Retrieval quality depends on indexed, authorized source coverage. |
| 24 | Knowledge and truth | **Enforced** through claim/source status, schema checks, deterministic validation, tests, and review for unsupported output. | Tests prove covered contracts, not universal factual truth. |
| 25 | Ingestion and synchronization | **Structured** through source registry, cursors, metadata, extraction, indexing, sync state, and audit. | Each real connector still requires credentials, permissions, and acceptance evidence. |
| 26 | Perception and ambient intelligence | **Structured** through scans, open-loop detection, proposals, interruption policy, and outcome monitoring. | HAI does not perform unrestricted surveillance or stealth scraping. |
| 27 | Human-AI interaction | **Structured** through Basic/Advanced disclosure, approvals, review queues, provenance, and action-first UI. | Usability still requires target-user acceptance, not only component tests. |
| 28 | Privacy | **Enforced** through owner scope, local-first policy, minimization, exclusions, redaction, and deletion/revocation paths. | External providers remain independent privacy boundaries. |
| 29 | Security | **Enforced** through authenticated owner identity, RBAC, allowlists, secret handling, runtime policy, and fail-closed behavior. | A green unit suite is not a penetration test. |
| 30 | Agentic threat modelling | **Enforced** through untrusted-content boundaries, no authority in messages, protected prohibitions, and approval/runtime controls. | Threat coverage must evolve with new adapters and tools. |
| 31 | Safety engineering | **Enforced** with emergency stop, risk gates, stop conditions, reversibility preference, and human review. | Physical-world safety depends on the connected executor and environment. |
| 32 | AI governance | **Enforced** through versioned policy, immutable audit, owner activation, provider budgets, and review state. | Regulatory compliance remains jurisdiction and deployment specific. |
| 33 | Model intelligence | **Enforced locally** by recording every real routed generation attempt that reaches a provider/model as redacted `model_run_telemetries`, updating the exact row when trusted task validation produces an outcome, refreshing calibration from the durable ledger, and preferring lane-specific accepted-output evidence with a conservative lower confidence bound before efficiency tie-breakers. | Direct generation API calls remain `unvalidated` until a trusted validator records an outcome. External model capability, price, and completion acceptance still require real credentials, live calls, current configuration, and retained provider-specific evidence. |
| 34 | Evaluation | **Enforced** through criteria, validation statuses, retries/review, typed framework evidence contracts, and framework assurance checks. | Pre-authorization contracts are evaluated before effects. Execution and postcondition contracts are phase-labelled, but some still rely on the existing task-level validation result rather than an independent provider-specific verifier. Synthetic checks must not be represented as production effectiveness. |
| 35 | Observability | **Structured** through task/workflow state, selections, decisions, audit, runtime evidence, and health summaries. | Production traces and alerts require deployed observability services. |
| 36 | Reliability and resilience | **Enforced locally** through durable state, leases, retries, recovery states, health checks, postcondition verification, one signal-aware application context for every in-process worker, and graceful backend/IDP HTTP draining. | High availability and distributed lease coordination remain absent. A forced process kill can still interrupt in-flight work, which must recover through its durable lease or review state. |
| 37 | Controlled learning | **Enforced** as verified or operator-confirmed learning proposals; authority and policy cannot self-modify. Verified portfolio settlements are aggregated only within an owner/project scope. At least three comparable outcomes are required before bounded median/MAD estimate factors become a review proposal. After any approval, rejection, or rollback, only a fresh post-review cohort can propose another revision. One unresolved proposal per scope suppresses competing review items while separately reporting new evidence and material drift. Approval activates an exact ledger revision; planning exposes it as an optional suggestion and binds it only after an explicit owner action. Rollback makes that revision ineligible. | Autonomous weight training, silent estimate replacement, and unsupervised policy mutation are not present. Calibration cannot modify risk, priority, budget, permissions, safety, or execution authority. |

## Personal Operating Packs

| No. | Pack | Repository state | Operational boundary |
| ---: | --- | --- | --- |
| 38 | Productivity and attention | **Structured** selection, capacity, prioritization, follow-up, and task planning. | Calendar/account coverage depends on configured connectors. |
| 39 | Behaviour change and habits | **Catalogued** with reviewable recommendation and outcome criteria. | No clinical or coercive behavior manipulation is authorized. |
| 40 | Health and personal care | **Catalogued with high-risk controls** and stricter evidence/approval expectations. | No diagnosis, treatment, or emergency service replacement is claimed. |
| 41 | Financial management | **Structured for sourced commitment and cost records** with a zero-spend delegation default and consequential-action approval. Estimates, incurred costs, payments, and refunds are stored as distinct immutable event kinds. | The ledger records evidence-backed facts; it does not reconcile bank balances, initiate payments, calculate tax, perform accounting, or prove that an external financial effect occurred. |
| 42 | Home, garden, and assets | **Structured** through source/workflow/task patterns and reversible execution controls. | Device and asset integrations require explicit adapters and allowlists. |
| 43 | Work and service delivery | **Structured** through pursuits, workflows, delegation, deadlines, checklists, and verification. | Customer systems require configured connectors. |
| 44 | Entrepreneurship and venture | **Structured** through pursuits, priorities, evidence, planning, and outcome monitoring. | Market conclusions remain evidence-dependent, not guaranteed forecasts. |
| 45 | Legal, government, and case management | **Catalogued with mandatory evidence and approval controls**. | HAI is not legal counsel and cannot submit or send without the governed path. |
| 46 | Communication | **Structured** for recipient/tone context, drafting, source support, and approval. | Consequential external send remains approval-gated. |
| 47 | Relationships and care | **Catalogued with human-sovereignty and privacy overlays**. | HAI cannot infer consent or replace direct human judgment. |
| 48 | Learning and competence | **Structured** through goals, context, plans, memory, and verified lessons. | Credentials and competence must come from authoritative evidence. |
| 49 | Travel and mobility | **Catalogued** for planning, deadlines, calendar, and risk checks. | Booking or payment requires a real adapter and approval. |
| 50 | Emergency and continuity | **Catalogued with stop/escalation controls**. | HAI is not an emergency service and must direct urgent human action appropriately. |

## Implementation Candidate Families

| No. | Family | Repository state | Operational boundary |
| ---: | --- | --- | --- |
| 51 | Agent-development frameworks | **Experimental catalog only**; candidate products can be evaluated behind adapters. | No candidate is installed, trusted, or enabled by catalog mention. |
| 52 | Durable workflow platforms | **Experimental catalog only**; canonical Go workflow remains authoritative. | Temporal-like distributed execution is not present. |
| 53 | Memory and knowledge implementations | **Experimental catalog only**; current PostgreSQL/source/memory paths remain canonical. | RAG products require separate deployment, migration, privacy, and quality proof. |
| 54 | Policy, identity, and security implementations | **Experimental catalog only**; current IDP/RBAC/policy remains authoritative. | External policy engines or sandboxes need explicit integration and security review. |
| 55 | Evaluation and observability implementations | **Experimental catalog only**; current validation/audit/health paths remain canonical. | External telemetry products require configured storage, retention, and access controls. |

## Immutable Plan And Coordination Contract

The canonical Plan Graph implements a bounded directed acyclic plan as immutable
owner-scoped revisions. Each revision records nodes, dependency edges, owner,
risk, approval state, resource estimates, framework and evidence digests, and
pursuit, workflow, task, or agent references where they exist. A canonical
SHA-256 digest binds the plan content, authenticated owner, actor, revision,
parent revision, and timestamps at PostgreSQL microsecond precision.

`POST /api/v1/plans/preview` creates revision 1. Acceptance and repair require
both the current revision and digest, reject stale clients, and append a new
revision instead of updating history. PostgreSQL triggers reject update,
delete, and truncate. Row metadata must match the immutable JSON payload before
the repository returns a record. A payload that asserts execution authority is
rejected.

The contract closes part of family 16 (formal planning) and provides shared
coordination evidence for families 5, 10, 12, 17, and 18. Exact accepted-plan
references now bind task planning and execution, workflow intake and worker
execution, pursuit intake and planning, accepted portfolio allocations,
portfolio proposal approval, dispatch, effect authorization, and first receipt
consumption. Each boundary resolves the authenticated owner, immutable plan ID,
revision, digest, and selected node again before allowing new work. Migrations
`0053` and `0054` enforce durable all-or-none accepted references and exact
revision foreign keys for workflows and portfolio allocations. Migration
`0058` adds a separate all-or-none workflow draft reference. Its constraint
trigger resolves the immutable draft revision and requires the selected root
node to bind the same authenticated owner and workflow.

Accepting a plan means only that it is the authoritative coordination revision;
it never authorizes an agent, tool, runtime, provider, message, or external
effect. Every effect still requires its own current policy, approval, execution,
and verification boundary. Terminal history and already-consumed crash recovery
use a separate historical-provenance resolver which explicitly cannot authorize
new work. General-purpose optimization and automatic plan generation or
projection from every intake remain incomplete. Durable task `Plan` operations
now project a revision-1 advisory draft when no accepted coordination reference
was supplied. Every node is bound to the authenticated owner and exact task ID,
the graph contains no inferred time or cost estimates, and the durable task
operation ID is the Plan Graph idempotency boundary. Projection failure fails
the task operation closed. Task `Preview` remains side-effect-free, task `Run`
does not synthesize coordination state, and a supplied accepted reference
suppresses a competing draft. Governed pursuit-candidate acceptance and ordinary
workflow intake now project one immutable revision-1 advisory graph from the
persisted workflow checklist when no accepted plan or prior draft exists. The
workflow records the exact draft plan, revision, digest, and root-node binding;
every projected node is owner/workflow bound, no time or cost estimate is
invented, and accepted-plan provenance suppresses a competing draft. Projection
failure blocks otherwise-ready work and an unchanged source replay retries the
projection idempotently. Pursuit planning, connected-source records, and other
intake paths still require explicit graph creation or an accepted reference.

## Internal Reminder Delivery Contract

Reminder preparation and approval do not themselves deliver anything. A second
owner-only authorization must bind the exact current activation request,
approval decision, reminder source digest, `in_app` channel, idempotency key,
and `AUTHORIZE ONE INTERNAL HAI REMINDER` confirmation. Migrations `0055`
through `0057` persist that authorization and each attempt as append-only
evidence and permit at most one authorization per exact approved decision and
channel.

The recurring workflow sweep revalidates the latest decision and current source
before every attempt. Revoked, replaced, or changed evidence is terminally
suppressed. Transient internal sink failures can be attempted at most three
times; the third failure writes a `dead_lettered` receipt and later sweeps do
nothing. Stable request and attempt evidence, rather than generated IDs or wall
clock timestamps, determines idempotent replay. The delivered signal uses the
authorized reminder timestamp so a crash after signal persistence but before
receipt persistence cannot create conflicting proactivity evidence.

The only configured sink is the local owner-scoped proactivity inbox. The
contract cannot send email or messages, write Calendar data, invoke webhooks or
desktop push, call a provider, run a workflow, approve work, or execute an
external follow-up. Those effects require separate adapter-specific authority
and acceptance evidence.

Workflow Engine shows the progressive delivery state, offers authorization only
for a current unexpired approval with no existing authorization, runs an
owner-scoped bounded due pass, and keeps immutable IDs/digests in Advanced
inspection. Browser controls never broaden the backend authority.

## Agent Message Acknowledgment Contract

Migration `0059` adds an append-only, owner-scoped acknowledgment ledger for
coordination messages that explicitly require a reply. Each record binds the
exact team version, message, correlation ID, recipient, acknowledgment status,
reason, idempotency key, retry timestamp, and canonical digest. Exact retries
replay the existing record; conflicting key reuse fails closed. An accepted or
rejected acknowledgment is terminal, while a deferred acknowledgment may be
superseded by a later terminal response. PostgreSQL rejects update, delete, and
truncate operations and enforces the message, team, version, and owner link.

The read model projects `waiting`, `deferred`, `acknowledged`, `rejected`,
`overdue`, `expired`, and `not_required` attention without mutating the message
or creating a reminder. Timeout thresholds are deterministic and bounded from
the original message TTL. Overdue acknowledgments become owner-review
information only. They cannot approve work, grant runtime permission, send a
follow-up, execute a task, or change canonical plan or workflow state.
Governance Control exposes tracked and review-required counts while preserving
this authority boundary.

## Selector-V5 Authorization And Evidence Boundary

Selector-v5 execution does not trust the framework fields copied into an
authorization request. The Framework Registry resolves the exact immutable
selection by authenticated `owner_identity` and selection UUID. The
authorization service compares the resolved selection ID, task-plan ID,
catalog and selector versions, task risk, effective risk ceiling, maximum
autonomy, approval requirement, and the catalog, preference, Constitution, and
operating-contract digests. A missing resolver, missing record, wrong owner, or
mismatch is persisted as a denied authorization with reason code
`framework.selection_unverified` before Constitution and ordinary policy
evaluation.

`AuthorizeAndConsume` performs the same resolution again immediately before
the single execution reservation. If the selection can no longer be resolved
or its immutable snapshot differs, consumption stops with an authorization
change error. Both production composition paths install the same resolver:

- `backend/internal/router/routes.go` for the primary API/router composition;
- `backend/internal/phase2/module.go` for the Phase2/background composition.

Task planning compiles selected-framework evidence requirements into
`FrameworkEvidenceContract` records. Each contract preserves the source
framework, uses a deterministic requirement ID, and declares one phase:

| Phase | Current behavior | Boundary |
| --- | --- | --- |
| `pre_authorization` | Evaluated before `executeAllowedSteps`. Applicable required evidence receives a typed validator, optional freshness limit, and a `verified`, `missing`, or `not_applicable` assertion. Missing evidence blocks execution before an external effect and routes the task to review. | The validator can only prove evidence represented in HAI's current trusted task/context records. It does not create a missing provider record. |
| `execution` | Exact requirement-specific evaluators require the corresponding controlled-runtime record: for example, a completed UUID launch receipt, runtime boundary fields, extraction identity/provenance, a non-destructive change record, or a recovery receipt. Unsupported or absent execution evidence fails the requirement rather than falling back to token overlap. | A validator can only confirm records emitted by HAI's current runtime/source boundary. It does not prove an unconfigured external provider or device effect. |
| `postcondition` | Exact requirement-specific evaluators require accepted verification, source-linked claims, deterministic zero-exit test/check receipts, deliverable evidence, recovery verification, or explicit before/after evidence as applicable. A failed postcondition does not rewrite a previously verified pre-authorization assertion. | Provider-specific real-world acceptance still requires a live assertion producer and retained acceptance evidence. |

This is repository-level enforcement and structure. It is not evidence that
all real-world source, identity, calendar, provider-health, runtime, or external
postcondition providers are configured or have passed live acceptance.

A canonical `framework-evidence-preflight-v1` digest binds the owner, task plan,
framework selection, evaluated timestamp, assertions, evidence, and failures
into the task-plan digest, authorization request, immutable receipt, and an
evidence reference. This creates tamper-evident provenance. Controlled
tool/local execution first stores the passing tuple in the append-only
`framework_evidence_preflights` ledger, then
selector-v5 authorization resolves it by owner, task plan, framework selection,
and digest. The same exact resolution runs before receipt consumption. This
proves HAI's recorded preflight and its phase assertions; it does not manufacture
provider evidence that was never ingested.

## Selector V4 Trust Boundary

The public preview accepts only bounded planning hints. Owner identity comes
from the authenticated session. Risk, approval, observed needs, capacity,
available agents, coordination preference, and workflow deadline are trusted
in-process inputs. Secrets are redacted before durable contract fields are
created. Agent cards are considered verified only when the runtime reports an
`available` status with provenance and a timestamp no older than 24 hours.

The operating-contract digest covers life domains, needs, capacity, agent
cards, delegations, communication, coordination, per-action autonomy, stop
conditions, outcome monitoring, and the eight Chief-of-Staff answers. It
supports trace comparison; it does not itself grant authority or prove an
external action occurred.

## Universal Operational Life Graph

The append-only `lifeontology` ledger now recognizes people, needs, goals,
assets, obligations, projects, cases, opportunities, risks, sources, documents,
pursuits, workflows, tasks, memories, commitments, costs, and outcomes. Typed
relations cover provenance, production, fulfillment, assignment, requirements,
cost, and project/pursuit/workflow membership. Durable task planning and runs
project their operational records only after the preview boundary. Connected-
source sync projects each persisted extraction as an immutable document linked
to its registered source and project, preserving owner scope, source digest,
sensitivity, verification state, and local-only policy. Repeat syncs are
idempotent; corrections and archive changes append new observations. Projection
failures are audited and returned as warnings without falsely failing successful
ingestion. Contact-bearing sources can additionally project low-confidence person
candidates, but those candidates stay `needs_review`, sensitive, local-only, and
cannot become trusted contacts without correction or confirmation. Every
persisted workflow transition now appends an immutable workflow
observation and links available project, source/document, and pursuit context.
Workflow projection failures are recorded as bounded workflow audit events and
cannot roll back the authoritative transition. The graph remains advisory: it
cannot execute, approve, or grant authority.

## Governed Contact Review

Migration `0027_contact_review_decisions` adds an owner-scoped, append-only
decision ledger for source-derived person candidates and merge proposals.
Authenticated Governance Control users can promote, correct, reject, merge, or
keep distinct through explicit review actions. Source candidates are never
rewritten. Promotion and merge append a separate local-only canonical person
with `human_approved` verification, while rejection and keep-distinct decisions
append only the decision record. No contact decision can execute an action or
grant authority.

The service uses a server-authored decision timestamp, bounded idempotency keys,
one final decision per subject, deterministic digests, secret rejection, and an
atomic canonical-person plus decision transaction. Database foreign keys bind
the decision to the same-owner source candidates and merge proposal; a trigger
checks proposal membership and the human-approved local canonical-person
boundary. Update, delete, and truncate are rejected. The down migration permits
an empty rollback but refuses to discard a populated immutable ledger.

Focused service, route, RBAC, component, and migration tests pass. Disposable
PostgreSQL acceptance proves durable owner isolation, atomic promotion, a
single winner under concurrent promote/reject decisions, immutable mutation
guards, empty rollback/reapply, and refusal to roll back populated evidence.
This is local governed-review proof, not proof that an external address book
was changed or that a connected account accepted the canonical contact.

## Outcome And Resilience Ledgers

Migration `0023_outcome_resilience_ledgers` adds owner- and workspace-scoped,
append-only records for outcome revisions, evaluations, corrections, worker
heartbeats, leases, retries, circuit state, recovery attempts, idempotency, and
operational events. The corresponding `/api/v1/outcome-evaluations` and
`/api/v1/resilience` routes expose these records for authenticated inspection.
They are advisory and observational contracts: recording an evaluation,
heartbeat, retry, or recovery does not authorize execution, consume an
approval, dispatch work, or prove an external effect occurred.

Outcome records retain exact revision provenance and bounded structured data.
Resilience records retain workspace scope and explicit state instead of
inferring health from process presence. The Governance Control surface can
define immutable outcome revisions, stage source-attributed observations,
request bounded evaluations, and inspect corrections for an authenticated
owner/workspace scope. Empty scopes remain honest authoring states rather than
fabricated examples. Outcome definitions and evaluations now project as
immutable, owner-scoped outcome nodes in the shared whole-life graph. Each
definition links to its authenticated workspace; each evaluation links to that
workspace and supports its exact definition revision. Projection is idempotent,
repairs definitions that predate the graph, exposes redacted repair warnings,
and cannot roll back the authoritative outcome ledger. The live local-stack
acceptance stored revision 2 and an `on_track` evaluation with non-empty
definition/evaluation and graph digests, two outcome nodes, two workspace
relations, and one evaluation-to-definition support relation. These records
remain advisory and cannot authorize execution. Explicit user-selected domain
classification is required for new outcome definitions. Historical definitions
without one remain readable and project to a review-marked
personal-administration default instead of being silently reclassified.

## Durable Life Commitment And Cost Ledger

Migration `0026_life_commitment_cost_ledgers` adds two owner-scoped,
append-only PostgreSQL ledgers. Commitment changes are immutable revisions with
optimistic revision checks, bounded lifecycle transitions, idempotency keys,
request and record digests, explicit life-domain and project context, optional
counterparty and due date, verification status, and one or more source
references. Fulfilled, cancelled, and breached commitments are terminal in the
current service contract. Cost events separately record `estimate`, `incurred`,
`paid`, or `refund`, positive minor-unit amounts, an ISO-style three-letter
currency, verification, provenance, and optional project context. A cost may
link only to a commitment visible to the same authenticated owner. An estimate
is never silently promoted to an incurred or paid cost.

Authenticated `/api/v1/life-ledger` routes expose current commitments,
revision history, new revisions, cost entry creation, and cost listing. Owner
identity comes from the authenticated session rather than a request payload.
Database triggers reject update, delete, and truncate operations, while digest
verification detects corrupt stored payloads. Idempotent replay returns the
existing record; reuse of an idempotency key for different content and stale
expected revisions fail closed.

Each accepted commitment revision projects an immutable commitment node into
the owner-scoped life graph. Each accepted cost event projects a distinct cost
node and, when present, links it to the commitment and project. Projection
results must remain advisory-only and unable to execute or grant authority;
projection failure is returned as a redacted warning and does not rewrite the
authoritative ledger record. Focused service, projection, migration-contract,
route-wiring, and RBAC tests exist. A fresh disposable-PostgreSQL acceptance
run proved owner isolation, revisions, idempotency, distinct estimate/incurred
events, and immutable mutation triggers. Governance Control now provides a
source-backed Basic summary, Advanced immutable history/evidence inspection,
and authenticated append-only authoring for commitment revisions and cost
evidence. Estimates may remain review-marked; incurred events require at least
source support; paid and refund assertions require human confirmation or
verified evidence. The rebuilt local UI proved the weak-payment refusal and
confirmed the rejected probe was not persisted. External account
reconciliation and real financial-provider evidence are still open gates.

## Standing-Mandate Life-Graph Projection

The durable standing-mandate service now projects every persisted draft,
activation, and revocation revision as a separate owner-scoped outcome node in
the whole-life graph. The projection retains the mandate ID, revision, status,
autonomy ceiling, source provenance, bounded life-domain classification, and up
to 16 distinct project links. Drafts are schema-validated waiting context;
active and revoked revisions are marked human-approved context. All projected
records are restricted, local-only, and declare that they grant no graph
authority.

This projection does not participate in, replace, or weaken standing-mandate
authorization evaluation. The mandate repository and evaluator remain the
authority for scope, expiry, risk, approval, and stop-condition decisions. The
graph response is excluded from the normalized mandate digest, cannot approve
or execute an action, and cannot roll back a successful create, activate, or
revoke operation. Projection errors are exposed only as bounded, secret-redacted
warnings. Focused lifecycle/projection tests cover this boundary; distributed
approval-proof consumption and real runtime/provider acceptance remain open.

## Execution Authorization Compatibility

Migration `0024_execution_authorization_schema_compatibility` upgrades databases
that applied an earlier form of the execution-authorization schema without
resetting immutable history. It backfills owner-scoped approval provenance and
effect digests, adds final-effect exercise records and current uniqueness
constraints, and leaves already-current fresh installations unchanged. The live
Windows Compose acceptance preserved 16 historical test-owner receipts and six
consumptions while adding all current integrity fields. Owner isolation remains
enforced: those test-owner records correctly stay hidden from the signed-in
operator rather than being reassigned or exposed.

Migration `0025_execution_authorization_life_domain` durably adds the bounded
life-domain input to the append-only authorization receipt. Authorized,
approval-required, and denied receipts can now project an advisory outcome into
the shared life graph. Exact verified approval provenance is linked as approval
evidence; commitment-stage receipts add commitment context; positive estimated
costs add explicitly non-incurred cost estimates. The graph response is excluded
from the immutable receipt digest and can neither grant authority nor roll back
the authorization ledger. Repository round trips, owner isolation, projection
repair, approval provenance, commitment, cost, and failure boundaries are tested
against local PostgreSQL; this is not proof that an external effect occurred.

## Advisory Ambient Outcome Monitor Contract

The perception, outcome, observability, resilience, governance, and proactive-
attention families now share one bounded advisory path. Fixed owner-scoped
collectors read workflow open loops, verified workflow completions, or overdue
commitments from canonical local ledgers. Durable target scheduling uses a
persisted singleton sweep, bounded scope/batch discovery, leases, fencing,
recovery, and deterministic replay. Immutable observations and run receipts are
composed into source-supported outcome evaluations and the owner attention
inbox.

This integration does not widen any framework's authority. Its authority label
is `advisory_monitor_only`, with execution, delivery, notification, Calendar,
workflow mutation, mandate authorization, and learning mutation all disabled.
It cannot use candidate framework catalogs, external runtimes, arbitrary tools,
or caller-authored queries. Governance Control is an inspection/configuration
surface, not evidence that a real external source or effect is correct.

Required acceptance remains exact replay without duplicate evidence or inbox
records, two-owner isolation, disable and lease-fencing behavior, expired-lease
recovery, signed-in UI/API behavior, and zero execution/delivery effects. Live
external-account reconciliation, distributed scheduling, provider-specific
truth, and production observability remain separate product gates.

## Remaining Product Work

The largest remaining gaps are downstream execution and completion of coordinated
portfolio workflows, external calendar acceptance, external contact/address-book reconciliation,
operator-facing mandate-graph inspection, external financial reconciliation,
recurring-obligation and accounting semantics, live sourced capacity feeds,
distributed multi-agent/A2A transport, ambiguous external-effect
reconciliation, high-availability workflow coordination, full domain-specific
services, and real acceptance evidence for each configured account, provider,
financial system, and runtime. Local Compose now applies migrations through a
one-shot migrator and runs the long-lived backend as the separate
least-privilege `hai_runtime` role. Database-enforced owner isolation beyond
the existing authenticated owner-scoped repository contracts remains required
before multi-tenant production trust.
The new ledgers and projections are durable local contracts, not a production-
readiness claim or evidence that external contacts, obligations, balances,
payments, or mandate-authorized effects have been independently verified.
