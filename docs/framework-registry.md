# Framework Registry

The Framework Registry is HAI's versioned decision-discipline catalog. It
selects a small set of planning, reasoning, governance, domain, and evaluation
frameworks for a task. It does not install third-party products, grant tool
authority, approve an action, or execute a runtime.

This document describes the implementation currently present in this
repository worktree. It distinguishes repository behavior from
environment-dependent deployment and external-product validation.

See the [Framework Operating Contract Matrix](framework-operating-contract-matrix.md)
for a one-to-one status and boundary statement across all 55 families.

## Status

| Surface | Current status | Boundary |
| --- | --- | --- |
| Catalog | Implemented: 55 stable records; catalog contract `v2` | The mandatory evaluation record is `1.1.0`; the other built-in records remain `1.0.0`. Records 51-55 describe candidate implementation families. They do not install or trust those products. |
| Selector | Implemented: deterministic `selector-v5` scoring, risk-ceiling enforcement, exact minimum capability coverage, conflicts, required overlays, a 16-framework cap, and a Chief-of-Staff operating contract | The selector fails closed when a mandatory framework cannot support the effective task risk, filters incompatible optional frameworks, retains every applicable mandatory overlay, then finds the smallest non-conflicting capability cover. A new call has a new timestamp, so its selection ID can differ even when the normalized request is otherwise identical. |
| Preferences | Implemented and owner-scoped | Preferences can enable experimental records, pin relevant records, lower autonomy, and add bounded adaptations. They cannot raise autonomy or disable protected overlays. |
| Constitution | Implemented with an exact built-in source, owner-scoped drafts, explicit activation, and immutable history | A draft has no authority until the authenticated owner activates it. |
| Persistence | Implemented in PostgreSQL with versioned SQL and immutable selection/Constitution controls | Applying the migration still requires the target database and the normal migration process. |
| API | Implemented under `/api/v1/framework-registry` with authentication and RBAC | The nginx allowlist routes the namespace to the Go backend; a deployed signed-in browser exercise remains target-environment evidence. |
| Angular UI | Implemented at `/framework-registry`, including catalog, inspector, selection preview/history, preferences, and Constitution controls | Component/service tests do not replace a clean-machine operator acceptance run. |
| Task engine | Implemented: the task planner records a `frameworkDecision` before context, model, and tool routing | Framework selection narrows authority. It never replaces task approval, runtime policy, verification, or audit. |
| Task review state | Implemented as owner-scoped completion logs, review items, immutable approval/rejection decisions, and an explicit dry-run-first reconciliation action | PostgreSQL persistence survives an ordinary restart. Reconciliation never retries work and is intentionally operator-triggered rather than automatic. |
| Controlled execution | Implemented as an internal, action-bound approval-proof boundary for mutating automation paths, plus exact server-owned contracts for built-in system workloads | Approval-proof consumption is durable and owner-scoped. Unknown system identities and policy mismatches fail closed. External runtimes and real provider/account paths still need configured, approved end-to-end validation. |
| Selector-v5 execution resolution | Implemented as an exact immutable lookup by authenticated owner and selection UUID, followed by field-for-field authorization comparison and a second check immediately before receipt consumption | Resolution does not use a bounded history page or accept caller-supplied selection fields as proof. Missing, cross-owner, unavailable, or mismatched selections fail closed. |
| Typed framework evidence contract | Implemented for deterministic requirement IDs, source-framework attribution, `pre_authorization`, `execution`, and `postcondition` phases, validator names, required state, freshness limits, and pre-authorization assertions | Pre-authorization is enforced before tool/runtime effects. Execution and postcondition requirements are phase-structured, but some still use the existing task-level evidence/verification path rather than an independent requirement-specific verifier. |
| Exact source-evidence preauthorization | Implemented as an exact owner-scoped extraction, raw-item, and connected-source resolution during authorization and again immediately before receipt consumption | Freshness is measured from the raw item's `fetched_at`. Provenance and freshness do not establish semantic authority or truth. Inspection exposes only claim counts and digest fingerprints, not raw sensitive content. |

Focused unit, route, repository-integration, task, frontend, and approval-proof
tests exist in the repository. Those checks establish local implementation
behavior. They do not prove a real external framework product, model provider,
account connector, or runtime on Robert's target Windows machine.

## Architecture

```text
authenticated operator or task planner
  -> owner-scoped Framework Registry service
     -> 55-record built-in catalog
     -> owner preferences
     -> active owner Constitution or built-in fallback
     -> selector-v5 risk-compatible selection
     -> immutable selection record and reproducibility metadata
     -> whole-life, capacity, agent, delegation, communication, coordination,
        per-action autonomy, stop-condition, and outcome-monitoring contract
  -> task context, model, tool, approval, execution, and verification layers
```

Framework selection is advisory policy metadata with a hard autonomy ceiling.
The selected record cannot create authority that is absent from the active
Constitution, RBAC role, approval record, tool allowlist, emergency-stop state,
or controlled runtime.

## Catalog Contract

`BuiltinCatalog()` returns 55 records. `ValidateCatalog()` rejects a catalog
that does not contain exactly 55 unique, complete entries. Every entry has:

- stable ID, name, family, version, source, provenance, and status;
- purpose, suitable problem types, trigger conditions, and required inputs;
- output, agent, workflow, decision, evidence, and evaluation contracts;
- safety invariants, risk ceiling, authority requirement, and maximum autonomy;
- conflicts and owner-adaptation metadata;
- optional candidate implementations.

All records currently use version `1.0.0`. The catalog contains 50 `active`
records and five `experimental` implementation-family records.

### Catalog Lifecycle Versus Effective State

The registry exposes two different dimensions. Operators should not collapse
them into one status:

| Value | Dimension | Current meaning |
| --- | --- | --- |
| `active` | Catalog lifecycle | Supported built-in decision contract; enabled by default unless an owner preference disables it. |
| `experimental` | Catalog lifecycle | Evaluation contract for an implementation family; disabled by default and considered only after owner enablement and a direct framework/name/candidate-implementation match. Five v1 records have this status. |
| `deprecated` | Catalog lifecycle | Recognized by the API and UI for a contract retained for compatibility but excluded from selection even if a stored preference says enabled. The v1 catalog currently contains zero deprecated records. |
| `disabled` | Owner-effective preference | Not a catalog lifecycle status. It means this owner has disabled an ordinary record. The catalog record and its original status remain visible. |

`default`, `enabled`, and `disabled` are preference states. `Enabled` in an API
response is the effective boolean after applying the catalog default and the
owner preference. A protected safety overlay rejects an attempted
`disabled` preference. Enabling an experimental record only makes it eligible
for a directly matching request. A deprecated record remains unselectable.
Neither preference installs a candidate product or grants runtime authority.

### Complete V1.1 Taxonomy

| No. | ID | Name | Family | Default status |
| ---: | --- | --- | --- | --- |
| 1 | `human-sovereignty` | Human sovereignty | constitutional | active |
| 2 | `whole-life-ontology` | Whole-life ontology | life_model | active |
| 3 | `needs-wellbeing` | Human needs and wellbeing | life_model | active |
| 4 | `capacity-state` | Personal state and capacity | life_model | active |
| 5 | `goal-hierarchy` | Goal hierarchy | life_model | active |
| 6 | `intake-triage` | Intake and triage | orchestration | active |
| 7 | `multi-criteria-prioritization` | Multi-criteria prioritization | orchestration | active |
| 8 | `multi-agent-organization` | Multi-agent organization | agents | active |
| 9 | `agent-identity-capability` | Agent identity and capability | agents | active |
| 10 | `delegation-accountability` | Delegation and accountability | agents | active |
| 11 | `agent-communication` | Agent communication | agents | active |
| 12 | `multi-agent-coordination` | Multi-agent coordination patterns | agents | active |
| 13 | `reasoning-methods` | Cognitive and reasoning methods | reasoning | active |
| 14 | `cognitive-agent-architecture` | Cognitive-agent architectures | reasoning | active |
| 15 | `uncertainty-decision` | Decision-making under uncertainty | reasoning | active |
| 16 | `formal-planning` | Formal planning | planning | active |
| 17 | `workflow-modeling` | Workflow modelling | planning | active |
| 18 | `reliable-execution` | Reliable execution | execution | active |
| 19 | `autonomy-levels` | Autonomy levels 0-10 | governance | active |
| 20 | `approval-control` | Approval control | governance | active |
| 21 | `memory-architecture` | Memory architecture | knowledge | active |
| 22 | `personal-knowledge-management` | Personal knowledge management | knowledge | active |
| 23 | `retrieval-context` | Retrieval and context | knowledge | active |
| 24 | `truth-evidence` | Knowledge and truth | knowledge | active |
| 25 | `ingestion-synchronization` | Data ingestion and synchronization | knowledge | active |
| 26 | `ambient-perception` | Perception and ambient intelligence | proactivity | active |
| 27 | `human-ai-interaction` | Human-AI interaction | interaction | active |
| 28 | `privacy-protection` | Privacy protection | governance | active |
| 29 | `security-zero-trust` | Security and zero trust | governance | active |
| 30 | `agent-threat-modeling` | Agentic threat modelling | governance | active |
| 31 | `safety-engineering` | Safety engineering | governance | active |
| 32 | `ai-governance` | AI governance | governance | active |
| 33 | `model-intelligence` | Model intelligence | reasoning | active |
| 34 | `evaluation` | Evaluation | evaluation | active |
| 35 | `observability` | Observability | evaluation | active |
| 36 | `reliability-resilience` | Reliability and resilience | execution | active |
| 37 | `controlled-learning` | Learning and controlled self-improvement | learning | active |
| 38 | `productivity-attention` | Productivity and attention pack | domain_pack | active |
| 39 | `habit-behavior-change` | Behaviour change and habits pack | domain_pack | active |
| 40 | `health-personal-care` | Health and personal care pack | domain_pack | active |
| 41 | `financial-management` | Financial management pack | domain_pack | active |
| 42 | `home-garden-assets` | Home, garden and asset management pack | domain_pack | active |
| 43 | `work-service-delivery` | Work and service delivery pack | domain_pack | active |
| 44 | `entrepreneurship-venture` | Entrepreneurship and venture pack | domain_pack | active |
| 45 | `legal-government-case` | Legal, government and case management pack | domain_pack | active |
| 46 | `communication` | Communication pack | domain_pack | active |
| 47 | `relationships-care` | Relationship and care pack | domain_pack | active |
| 48 | `learning-competence` | Learning and competence pack | domain_pack | active |
| 49 | `travel-mobility` | Travel and mobility pack | domain_pack | active |
| 50 | `emergency-continuity` | Emergency and continuity pack | domain_pack | active |
| 51 | `agent-development-adapters` | Agent-development framework adapters | implementation | experimental |
| 52 | `durable-workflow-platforms` | Durable workflow platform adapters | implementation | experimental |
| 53 | `memory-knowledge-implementations` | Memory and knowledge implementations | implementation | experimental |
| 54 | `policy-security-implementations` | Policy, identity and security implementations | implementation | experimental |
| 55 | `evaluation-observability-implementations` | Evaluation and observability implementations | implementation | experimental |

The products named by records 51-55 are candidates for later evaluation. A
catalog mention of AutoGen, LangGraph, Temporal, pgvector, RAG stores, policy
engines, sandboxes, or observability products is not an installed adapter,
capability test, security approval, or production dependency.

## Selector v4

### Accepted Inputs

The internal selector can receive task plan ID, request, project/pursuit
context, task type, risk, difficulty, required reasoning, success criteria, and
signals for memory, tools, documents, web, local execution, approval, requested
execution, and recorded human approval. Trusted in-process callers can also
provide sourced needs-state observations, a timestamped capacity snapshot,
fresh runtime agent cards, a coordination preference, and a deadline. Those
fields are not accepted from the browser preview.

The public `POST /select` preview accepts only untrusted planning hints. It does
not accept client-asserted risk level, approval requirement, human approval, or
owner identity. Owner identity comes from the authenticated session; trusted
risk and approval state belong to task and approval services. JSON parsing is
strict, allows one object, rejects unknown fields, and limits the body to 64
KiB.

### Protected And Required Overlays

Ten frameworks are protected against owner disable:

1. `human-sovereignty`
2. `intake-triage`
3. `autonomy-levels`
4. `approval-control`
5. `truth-evidence`
6. `privacy-protection`
7. `security-zero-trust`
8. `agent-threat-modeling`
9. `reliable-execution`
10. `evaluation`

Protection against disable and hard-required selection are separate contracts:

- `human-sovereignty`, `intake-triage`, and `evaluation` are hard-required for every
  selection.
- `approval-control` is hard-required for approval, high-risk, or consequential
  external-effect work.
- `truth-evidence` is hard-required for factual, document, research, or web
  work.
- `privacy-protection` and `security-zero-trust` are hard-required for
  sensitive, tool-mediated, local-execution, or execution-requested work.
- `autonomy-levels` and `reliable-execution` are hard-required for tool or
  runtime execution.
- `agent-threat-modeling` is hard-required for untrusted document, web, or tool
  content.

This distinction is intentional documentation of the current code. The
registry does not claim that all ten protected records appear in every
selection.

### Classification And Scoring

The selector:

1. normalizes request and project text;
2. classifies a life domain and need or commitment;
3. derives high-risk, approval, truth/evidence, privacy, and security needs;
4. inserts applicable hard-required overlays;
5. scores enabled active records and explicitly matching experimental records;
6. derives auditable domain, request-flag, explicitly named, and exact
   semantic-family coverage;
7. retains mandatory overlays, then searches the bounded candidate space for
   the exact smallest non-conflicting optional set covering every applicable
   task capability;
8. uses score, pin, status, and framework ID as deterministic tie-breakers;
9. resolves declared conflicts and returns at most 16 selected frameworks
   without padding the result with redundant positive-score candidates.

Transparent relevance scoring is additive:

| Signal | Score |
| --- | ---: |
| Exact trigger descriptor | `+4` |
| Partial trigger token match | `+2` |
| Exact suitable-problem descriptor | `+6` |
| Partial suitable-problem token match | `+3` |
| Ranked life-domain match | `+6`, `+5`, or `+4` |
| Explicit framework ID or name | `+10` |
| Relevant owner-pinned framework | `+1` |
| Applicable hard-required overlay | `+100` |

Task flags add framework-specific boosts, including memory `+8`, retrieval
`+7`, evidence `+8`, reliable execution `+8`, security/privacy `+7`, formal
planning `+6`, reasoning `+5`, evaluation `+5`, approval `+8`, and autonomy
`+5`.

Ordering is deterministic. Required overlays retain their declared policy order.
Optional candidates are ordered by transparent score, pinning, active before
experimental, and framework ID. That precedence resolves ties between equally
small capable sets; it does not replace the exact minimum-cover search. A
selected framework records the coverage it contributed in its reasons.
Positive-score candidates with no required capability are omitted. Conflicting
required records fail closed; a non-required conflicting record is skipped with
an inspectable reason. If applicable coverage cannot fit inside the
12-framework safety limit, the selector fails instead of silently returning an
incapable set.

### Decision Output

A selection decision records:

- life domain and need or commitment;
- selected framework IDs, versions, scores, and concise reasons;
- conflict decisions and required agents;
- minimum framework autonomy ceiling and authority summary;
- approval requirement and reasons;
- evidence, completion, context, and learning requirements;
- Constitution version and exact source;
- catalog, selector, preferences, and Constitution reproducibility metadata.

The selection authority is a ceiling. Approval can satisfy a required gate but
cannot raise that ceiling.

## Owner Preferences

Preferences are stored per authenticated `owner_identity` and framework ID.
The effective state is `default`, `enabled`, or `disabled`.

An owner preference may:

- enable an experimental framework;
- pin a relevant framework as a deterministic tie-break boost;
- lower the built-in maximum autonomy level;
- add up to 20 deduplicated adaptations, each at most 500 characters.

It may not:

- disable one of the ten protected overlays;
- raise a framework's built-in autonomy ceiling;
- bypass approval, policy, Constitution, emergency stop, or secret controls;
- grant authority or reveal secrets through an adaptation.

Adaptations are whitespace-normalized, secret-redacted, owner-scoped, and
validated against protected-rule phrases before persistence.

## Constitution

When an owner has not activated a stored Constitution, HAI uses:

```text
ID:      builtin-robert-constitution-v1
Version: 1
Source:  builtin-robert-constitution-v1:v1
```

That exact source is persisted with selection decisions. It replaces earlier
ambiguous labels.

An owner can create a draft from the active version. The draft persists that
immutable `baseVersion` so a process restart cannot erase its amendment
provenance. Draft fields are bounded, secret-redacted, and checked against
protected rules. Activation requires:

- the authenticated owner and admin permission;
- exact, case-sensitive confirmation text `ACTIVATE CONSTITUTION`, with no
  leading or trailing whitespace;
- an approval note of at least 10 characters.
- a base version that still matches the active Constitution. A stale draft
  must be rebased into a new version rather than silently overwriting an
  intervening amendment.

Multiple drafts may be proposed from the built-in version 1 before any owner
version is active. Any one of those drafts may be chosen as the first active
owner version, even when earlier draft version numbers exist. Activating one
makes the other same-base drafts stale.

PostgreSQL enforces one active Constitution per owner. Constitution identity
and content, including its base-version provenance, are immutable after
insertion. The only valid metadata transitions are `draft -> active` and
`active -> superseded`; stale activation, deletion, and reactivation of
superseded history are rejected. A deferred database invariant also prevents a
standalone `active -> superseded` update from committing without a replacement
active version, so a restart cannot silently fall back after governance history
has been established.

### Prose And Machine-Enforced Rules

All Constitution fields are versioned governance records, but ordinary prose is
not parsed as executable authority. Machine enforcement has two sources:

1. code-owned protected controls that always require approval for legal or
   government action, financial action, account changes, destructive action,
   and public posting; and
2. valid, restrictive typed entries using exactly one of these forms:

```text
HAI-RULE v1 deny-capability capability=<known-capability>
HAI-RULE v1 require-approval capability=<known-capability>
HAI-RULE v1 authority-ceiling level=<0..10>
```

Typed rules are whitespace-normalized and parsed case-insensitively after the
`HAI-RULE` prefix is detected. A malformed `HAI-RULE` entry, unsupported
version, unknown capability, or unsupported operation makes the draft invalid.
There is deliberately no `grant-capability` operation. Rules can deny a
capability, require approval, or lower authority; they cannot authorize an
action, weaken a protected overlay, or raise an authority ceiling.

## Reproducibility And Audit

Every persisted selection includes:

- catalog version `v2`;
- canonical SHA-256 catalog digest;
- selector algorithm version `selector-v5`;
- effective task risk and the minimum selected-framework risk ceiling;
- the selected maximum autonomy level and whether exact case approval is required;
- canonical SHA-256 effective-preference digest;
- canonical SHA-256 Constitution digest;
- exact Constitution source;
- selected framework versions and decision output.
- a SHA-256 digest over the complete Chief-of-Staff operating contract.

The operating contract includes all matched life domains rather than only the
legacy primary label; sourced or explicitly review-marked needs state; capacity
freshness and plan-size constraints; fresh verified agent cards and explicit
unassigned roles, including identity, ownership, purpose, capabilities,
permissions, data boundaries, cost, model requirements, reliability,
input/output schemas, health, evaluation, version, and revocation state;
zero-spend delegation contracts with deadline, constraints, evidence,
completion, and escalation rules; typed communication with correlation,
idempotency, expiry, confidentiality, provenance, payload integrity, and
optional externally verified signature digests; and
coordination contracts; per-action 0-10 autonomy decisions; stop conditions;
outcome monitoring; and eight concise Chief-of-Staff answers.

The audit row stores a SHA-256 hash of the normalized request plus a bounded,
typed summary. It does not store the raw request in the selection table.
Selection records are immutable at the database layer.

The selection ID is deterministic for the full normalized decision input,
including its supplied timestamp. Separate service calls normally use
different timestamps, so reproducibility should be evaluated from the version
and digest fields rather than by expecting the same UUID on later calls.

### Embedded And External Agent Roles

The canonical Go task engine may satisfy only an explicit allowlist of
control-plane roles backed by deterministic HAI modules. Examples include task
intake, context planning, policy, risk, approval, resource planning, execution
authorization, verification, audit, and recovery. The generated
`hai_task_engine` card lists the exact role capabilities and module dependencies
that were used; a generic coordinator label is not accepted as proof that every
specialist exists.

Domain and external specialist roles remain separate. A role such as
`health_admin_assistant` must have a fresh, non-revoked, verified agent card
before its delegation becomes ready. Missing specialists are recorded as
`requires_assignment`, and task execution is blocked even when coordination is
otherwise `single_engine`. This prevents the generic task engine from silently
impersonating clinical, legal, financial, or other specialist competence.

Low-risk configured automations are also separated from operator capacity. A
bounded read-only automation may be feasible without reserving human time, but
the resource decision remains advisory and cannot grant authority. Runtime
authorization, framework-evidence preflight, execution receipts, and verified
postconditions still apply independently. Connected-source retrieval is skipped
when the task does not depend on source context, avoiding unrelated private
context and unnecessary source-index work.

## Persistence And Migration

Migration `backend/migrations/pre/0003_framework_registry.up.sql` creates:

- `framework_preferences`;
- `framework_selection_records`;
- `robert_constitution_versions`;
- owner, status, base-version, digest, JSON-shape, and autonomy constraints;
- immutable selection and Constitution lifecycle triggers;
- a deferred one-active-Constitution invariant after activation history exists.

It is a **pre-phase** migration. The exact rollback command is:

```text
backend migrate down pre/0003_framework_registry
```

The down migration removes the registry tables, indexes, and triggers. It is a
destructive rollback of registry preferences, selection history, and stored
Constitution versions. Back up the database and stop dependent application
versions before using it.

See [Database Migrations and Rollback Safety](migrations.md) for the complete
migration contract.

Migration `backend/migrations/pre/0004_task_state_storage.up.sql` separately
creates the durable task approval ledger:

- `task_completion_plan_logs`, with redacted payloads, SHA-256 payload digests,
  idempotency by owner/plan/digest, and database-level update/delete/truncate
  rejection;
- `task_review_items`, whose owner, original request, request digest, original
  plan, and creation time are immutable;
- `task_review_decisions`, whose approval/rejection event binds owner, review
  revision, task plan, exact request digest, resolver, and
  `task-review:<review-id>` source.

Its exact destructive rollback command is:

```text
backend migrate down pre/0004_task_state_storage
```

This removes completion logs, review items, and review decisions. Stop task
execution, take a database backup, and deploy a compatible application version
before using it.

Migration `backend/migrations/pre/0005_framework_operating_contract.up.sql`
adds the typed JSONB operating-contract columns and indexed 64-character
digest to immutable framework selections. Existing rows receive a zero digest
sentinel because they predate selector v4; repository reads omit the v4
contract for those rows instead of fabricating evidence from empty migration
defaults. New selector-v4 and selector-v5 records must contain the real computed digest. Migration
`pre/0029_framework_selector_v5_digest` extends that check to selector-v5 and
stores the selector-v5 task risk plus effective risk ceiling as nullable columns.
The fields stay null for pre-v5 history; they are never inferred from today's
catalog. Selector-v5 writes require both values, require `low`, `medium`, or
`high`, and require task risk to be at or below the effective ceiling. The
exact destructive rollback command is:

```text
backend migrate down pre/0005_framework_operating_contract
```

Rollback removes only the selector-v4 operating-contract columns and index.
Stop writers and deploy a selector-v3-compatible binary before using it.

The selector-v5 migration refuses rollback while selector-v5 rows exist. Export
and retain those immutable decisions, stop v5 writers, and remove the rows only
through an explicitly reviewed retention process before attempting rollback.

Selector-v5 execution provenance additionally carries the selected maximum
autonomy level and approval requirement in task-plan digests, workflow decision
history, and unified execution-authorization evidence. Authorization rejects a
requested autonomy level above that ceiling before policy evaluation. A
framework-required approval must resolve to an exact server-side case approval;
a standing mandate does not silently replace that requirement. Historical
selector-v4 records keep these fields absent and are never upgraded by inference.

### Exact Immutable Resolution At Authorization Time

`frameworkregistry.Service.Selection(ctx, owner, id)` is the execution-facing
lookup. The PostgreSQL repository parses the UUID and queries
`owner_identity = ? AND id = ?`; the in-memory repository applies the same owner
and ID boundary and returns a decoded copy. This lookup is independent of the
paginated selection-history API and cannot accidentally omit an older valid
selection because of a history limit.

For selector-v5, `executionauth` resolves that record and compares the immutable
snapshot against the authorization request:

- selection and task-plan IDs;
- catalog and selector versions;
- task risk and effective framework risk ceiling;
- maximum autonomy and exact approval requirement;
- catalog, effective-preference, Constitution, and operating-contract digests.

Any lookup error, missing resolver, incomplete selector-v5 contract, wrong-owner
record, or field mismatch produces a durable denied receipt with the bounded
public reason `framework.selection_unverified`. The authorization service does
not continue to Constitution, mandate, agent, approval, or ordinary policy
evaluation after that denial. Before `AuthorizeAndConsume` writes the single
consumption reservation, it resolves and compares the selection again; drift or
unavailability is returned as `ErrAuthorizationChanged` and no consumption is
recorded.

The resolver is installed in both application compositions that construct the
execution-authorization service:

- the main API/router path in `backend/internal/router/routes.go`;
- the Phase2/background path in `backend/internal/phase2/module.go`.

This verifies an HAI-owned immutable selection record. It does not verify that
a selected external framework product, model, connector, or runtime is installed
or healthy.

## API

All registry routes require an authenticated identity and use that subject as
the owner scope. Read routes need read permission, selection needs write
permission, and preference/Constitution mutations need admin permission.

| Role | Registry access | Task approval access |
| --- | --- | --- |
| `viewer` | Inspect overview, catalog, details, active Constitution, and owner-scoped selection history. | Inspect owner-scoped task logs and review queue only. |
| `operator` | Viewer access plus create a persisted selection recommendation. | Plan tasks, but cannot call approval-gated task run/success or resolve reviews. |
| `owner` | All registry access, including preferences, Constitution drafts, and exact activation. | Plan, run, inspect, approve/reject, and resolve owner-scoped task reviews. |

The role is derived from the verified IDP token. Client headers, request JSON,
and an API key do not grant a role or owner identity.

| Method | Path | Permission | Purpose |
| --- | --- | --- | --- |
| `GET` | `/api/v1/framework-registry/overview` | read | Counts, families, Constitution source, and selection contract |
| `GET` | `/api/v1/framework-registry/frameworks` | read | Owner-effective catalog |
| `GET` | `/api/v1/framework-registry/frameworks/:id` | read | Framework inspector |
| `POST` | `/api/v1/framework-registry/select` | write | Persisted owner-scoped preview selection |
| `PATCH` | `/api/v1/framework-registry/frameworks/:id/preference` | admin | Owner preference update |
| `GET` | `/api/v1/framework-registry/selections?limit=10` | read | Bounded owner selection history; maximum 100 |
| `GET` | `/api/v1/framework-registry/selections/:id` | read | Exact owner-scoped selection provenance |
| `GET` | `/api/v1/framework-registry/constitution` | read | Active or fallback Constitution |
| `POST` | `/api/v1/framework-registry/constitution/drafts` | admin | Create owner draft |
| `POST` | `/api/v1/framework-registry/constitution/:id/activate` | admin | Explicit activation |

### Gateway Integration

`nginx-config/nginx.conf.template` includes `framework-registry` in the
authenticated backend API regex. The gateway runs `auth_request`, propagates a
refreshed access cookie and verified access token from the IDP subrequest, and
then proxies the request to the configured backend upstream. The repository
also contains a static gateway contract check for the namespace.

This proves configuration wiring, not a deployed end-to-end session. The
target release still needs a signed-in browser exercise through nginx that
loads the page, reads the catalog, creates a bounded selection, and confirms
owner/RBAC behavior.

## UI And Task Integration

The Angular route `/framework-registry` provides:

- Basic and Advanced views persisted under
  `hai.module-view.v1.framework-registry`;
- overview counts, family/status filters, and framework inspection;
- selection recommendation and selection-history inspection;
- owner preference editing;
- Constitution viewing, draft creation, and explicit activation.

The Control Center navigation links to the route. Task Blueprint renders the
task plan's `frameworkDecision` and links back to the registry.

The task service invokes `PlanSelection` after intake classification and before
context, model, and tool routing. The resulting decision is stored on
`CompletionPlan.frameworkDecision`; evidence/evaluation requirements and the
framework authority ceiling constrain later planning and execution.

The workflow task runner carries the task plan's selection provenance into the
workflow decision and event ledgers. Workflow detail exposes the matching
catalog, selector, preference, and Constitution identities; missing or invalid
provenance blocks verified completion instead of silently discarding the
decision context.

The current implementation creates a new plan decision when a plan is built.
It does not claim cached decision reuse or automatic re-selection of existing
plans after a catalog, preference, or Constitution change.

### Typed Evidence Phases

The task service no longer treats the selected framework evidence list only as
undifferentiated text. During planning it compiles
`ValidationPlan.frameworkEvidenceContracts`. Each
`FrameworkEvidenceContract` retains the originating `frameworkId`, normalized
requirement, deterministic `fer-...` ID, phase, validator name, required flag,
and an optional maximum age.

The three phases deliberately separate evidence that must exist before an
effect from evidence that an effect itself must create:

1. `pre_authorization` covers trusted inputs and governance records such as the
   active Constitution, authenticated owner, exact approval, source context,
   calendar freshness, agent cards, allowlists, privacy/policy context, and
   planning records. Applicability is evaluated per task. Required applicable
   evidence is recorded as `verified` or `missing`; non-applicable requirements
   are explicit rather than silently treated as passed.
2. `execution` covers runtime-bound records such as action receipts, runtime
   boundaries, provenance, decisions, and recovery receipts.
3. `postcondition` covers evidence that can be assessed only after execution,
   including postcondition verification, validation results, reproducible
   results, before/after evidence, and deliverable evidence.

`Run` evaluates the pre-authorization phase before calling
`executeAllowedSteps`. A failed preflight creates a blocked, review-required
execution result stating that no external effect was attempted. Freshness-aware
validators currently use bounded ages for health/route evidence and fresh
source or calendar evidence. The assertions and failures remain attached to the
completion plan for inspection.

The `execution` and `postcondition` labels are implemented contract structure
and feed exact requirement-specific completion checks. Typed contracts do not
fall back to fuzzy description/token matching: absent or unsupported execution
and postcondition evidence fails closed. Historical plans without typed
contracts retain the legacy matcher for read/compatibility behavior only.

The preflight receives a canonical SHA-256 digest over its owner, task-plan and
framework-selection identity, timestamp, counts, assertions, evidence, and
failures. That digest is included explicitly in governance, the task-plan
digest, the authorization request/receipt, and a
`framework-evidence-preflight://sha256:...` reference. It is therefore bound and
tamper-evident. Before controlled tool or local execution, the task engine now
stores the passing record in the append-only, owner-scoped
`framework_evidence_preflights` ledger. Selector-v5 authorization resolves the
exact owner/task-plan/framework-selection/digest tuple independently and
requires `passed`; `AuthorizeAndConsume` repeats that resolution immediately
before the one-shot receipt is consumed. Missing, foreign-owner, failed,
forged, or drifted records fail closed with
`framework.evidence_preflight_unverified`. This independently verifies the HAI
preflight record, not the truth of an unconfigured external provider.

For source-backed assertions, authorization also re-resolves each exact claim
against its owner-scoped extraction, raw item, and active connected source. The
same resolution and claim comparison run before authorization and again
immediately before the one-shot receipt is consumed. Freshness is calculated
from the exact raw item's `fetched_at`; a newer source-wide `LastSyncedAt` does
not refresh an older item. These checks prove bounded provenance, identity,
integrity, and freshness only. They do not prove that the source is
semantically authoritative or that its contents are true. Authorization
inspection reports only the verified claim count and digest fingerprint; it
does not expose source claims, URIs, extracted payloads, or other raw sensitive
content.

These checks should not be read as a claim that every external provider has a
live assertion producer. Provider-specific postconditions still require
configured credentials, runtime instrumentation, and retained acceptance runs.

## Durable Task-State Approval Flow

Framework selection supplies evidence requirements, completion criteria, and
an authority ceiling to task planning. It does not approve the task. Approval
uses a separate owner-scoped state machine:

```text
open or needs_review
  +-> rejected
  `-> approved
        +-> completed
        `-> needs_review (new review revision)
```

`rejected` and `completed` are terminal. A transition from `approved` to
`needs_review` increments the review revision and permits a new owner decision.
Each approval or rejection produces an immutable decision row. The original
review request and its digest never change.

When the owner approves:

1. the repository resolves the stored item and writes the immutable decision in
   one transaction;
2. the task service rebuilds the run from the stored request, not from new
   client-supplied action fields;
3. execution verifies owner, review ID, revision, task/project/automation
   binding, approval source, and request digest;
4. a validated result advances to `completed`;
5. an execution error, blocked result, or failed validation advances to
   `needs_review` with a new reason rather than being marked complete.

Completion-plan logs are append-only snapshots. Authenticated history and
review reads use the durable repository; a repository error returns HTTP 500
instead of presenting an empty history. In-process memory repositories remain
available for tests and explicitly controlled system paths, but the routed
application constructs the PostgreSQL repository.

### Recovery Boundary

Persistence prevents review history from disappearing on a normal backend
restart. It does not make a side effect automatically resumable. In
particular:

- short-lived action approval proof consumption is durable and single-use, but
  it cannot establish whether an external target committed a side effect after
  a network or process failure;
- there is no automatic task-review reconciliation worker;
- `POST /api/v1/task/review-queue/reconcile` provides an owner-scoped,
  dry-run-first recovery action. It only closes a review when linked durable
  evidence already proves verified completion; otherwise it returns the item
  to `needs_review` without re-executing anything;
- a crash after durable approval but before durable outcome can leave the item
  `approved`.

Treat such an item as indeterminate. Inspect runtime/audit evidence, stop
external execution if necessary, and do not repeat the action until an operator
has established whether the side effect occurred. Follow the recovery steps in
the [operator runbook](operator-runbook.md). This is an explicit release
limitation, not a claim of exactly-once external execution.

## Controlled Runtime Approval Boundary

Framework selection never authorizes runtime execution. Mutating automation
paths use a separate internal action-bound approval proof before side effects:

- mutating API requests;
- script execution;
- Docker container start;
- agent-runtime execution.

The proof is HMAC-signed, short-lived, and single-use. It binds owner identity,
automation ID, SHA-256 action digest, action scope, approval source, issue and
expiry times, and a nonce. The default TTL is five minutes and the maximum is
15 minutes. The action digest covers automation configuration, task/project
input, and the relevant policy environment snapshot.

HTTP launch handlers cannot construct or submit this internal proof. The
approved task-review path issues it against a `task-review:<id>` approval
source. Direct mutating launches therefore fail closed. Read-only API `GET` and
`HEAD` probes do not require an action proof, but remain subject to normal
authentication, enablement, host allowlists, timeout, audit, and safety policy.

Focused tests cover binding mismatch, tampering, expiry, single-use
consumption, and rejection before network, process/filesystem, Docker-socket,
or agent-runtime access. PostgreSQL acceptance tests cover cross-instance
issuance/consumption, restart replay refusal, concurrent atomic consumption,
immutability, and rollback refusal for a populated ledger.

Current limitations:

- the stable signing key must be configured consistently across instances and
  rotated deliberately; rotation invalidates unexpired proofs;
- durable proof consumption prevents HAI replay but cannot guarantee
  exactly-once behavior inside an external API, process, Docker daemon, or
  agent runtime after an ambiguous failure;
- the review-to-proof bridge is implemented for approved task execution, not
  as a durable direct-launch approval workflow;
- upstream Hermes, Odysseus, OpenClaw, and other runtime products still require
  installation, allowlisting, credentials, and bounded real-world validation.

## Verification Boundary

### Implemented And Locally Testable

- exact 55-record catalog validation;
- preference isolation and protected-overlay enforcement;
- deterministic selector ordering, scoring, exact minimum coverage, conflict
  handling, and cap;
- exact Constitution fallback, draft, activation, and immutable lifecycle;
- digest and redacted audit persistence;
- authenticated route/RBAC contracts;
- task-plan and Angular integration;
- selector-v5 risk-compatible whole-life, needs, capacity, agent-card, delegation,
  communication, coordination, exact autonomy-ladder, stop-condition, and
  outcome-monitoring contracts;
- typed agent-message envelope validation for schema, correlation, authority,
  timestamp, message type, and secret redaction;
- pre-phase migration and targeted rollback parsing;
- owner-scoped task completion/review persistence and immutable decision
  contracts;
- controlled approval-proof behavior before mutating side effects.

### Environment-Dependent Or Not Yet Proven

- clean-clone migration and signed-in gateway/UI exercise on Robert's Windows
  machine;
- two-real-account owner-isolation exercise;
- any candidate external product named in the catalog;
- any real LLM provider, connected account, or external agent runtime;
- a live multi-agent team lifecycle or A2A transport; unverified required roles
  remain visibly unassigned and block multi-agent execution;
- a complete personal knowledge graph that assigns every non-task entity to a
  life domain;
- a standing-mandate issuance workflow for autonomy level 7 or above;
- durable or distributed approval-proof consumption;
- automatic recovery of a task review left `approved` by process failure is
  intentionally absent; the manual no-retry reconciler still needs retained
  live crash-recovery acceptance evidence;
- production load, high availability, and multi-worker operation.

The registry should be described as locally implemented decision and governance
infrastructure, not as proof that every named AI framework or runtime powers
HAI today.
