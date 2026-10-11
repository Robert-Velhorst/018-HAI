# Operator Runbook

Day-to-day operation of 018-HAI on one local host. This runbook covers the
55-record Framework Registry and the durable task-review path in addition to
ordinary stack health.

HAI is fail-closed operational software. A visible dashboard, a healthy
container, or a persisted approval does not prove that an external action
completed. Use the audit, source, runtime, and verification evidence required
by the action.

## Start And Stop

Run the Windows initializer to create the ignored `.env.local`, then use the
default Compose entrypoint. It delegates to the single source-built topology in
`docker-compose.local.yml`:

```bash
docker compose up -d --build
docker compose ps
```

Stop the stack without deleting volumes:

```bash
docker compose down
```

Compose gives the backend and identity provider 20 seconds to drain HTTP
requests. Their signal-aware application context also cancels source,
workflow, ambient, model-maintenance, catalog-maintenance, Temporal, and
outcome-monitor workers before process exit. A forced kill does not provide
this guarantee and should be reserved for an unresponsive process.

Do not add `-v` unless the reviewed operation is intentionally deleting local
database and queue volumes.

The backend remains read-only except for explicitly mounted operational data.
Emergency-stop and autonomy-mode state live in the dedicated
`018-hai-phase2-control-state` volume at `/var/lib/hai/phase2-state`; approval,
authorization, consumption, and audit evidence remain in PostgreSQL. Preserve
both stores during backup and recovery. Deleting only the control-state volume
does not erase approval evidence, but it removes the current stop/mode state and
therefore requires a fail-closed recovery review before execution is resumed.

### Database Ownership Boundary

The automation database uses two identities. `DB_USER` is the schema owner and
is passed only to Postgres and the one-shot `backend-migrate` service.
`DB_RUNTIME_USER` is the long-lived backend identity. The migrator creates or
rotates that role, grants table DML plus required sequence/function use, and
explicitly withholds schema creation, migration-ledger access, `TRUNCATE`, role
administration, database creation, replication, and row-security bypass.
It also removes inherited role memberships, resets role-level configuration,
and fails closed if a pre-existing runtime role owns database objects; ownership
must be reassigned to the migration owner before startup can continue.
Compose starts the backend only after `backend-migrate` exits successfully, and
the backend itself runs with `DB_RUN_MIGRATIONS=false`.

An `.env.local` created before this boundary was introduced must receive fresh,
independent `DB_PASSWORD` and `DB_RUNTIME_PASSWORD` values before the next
start. Generate a separate 32-byte random secret for each and add them to the
ignored file. The Windows initializer and Unix `generate-secrets.sh` do this
automatically for new environments. A missing, weak, or shipped database-owner
password fails the IDP closed in production; a missing or shipped runtime-role
password fails the migrator closed.

After an upgrade, verify both the one-shot result and runtime identity:

```bash
docker compose -f docker-compose.local.yml --env-file .env.local ps -a backend-migrate backend
docker compose -f docker-compose.local.yml --env-file .env.local exec backend /app/hai-backend doctor
```

### Kafka-Protocol Broker Cutover And Rollback

The local stack keeps the Kafka API and `kafka:9092` address but runs a
digest-pinned Redpanda Community Edition broker with one shard. Its active data
volume is `018-hai-redpanda-data`. Backend and IDP Sarama producers and the
nginx config-manager consumer require no protocol or address change.

HAI does not use Redpanda's development mode. The Compose command explicitly
sets `--unsafe-bypass-fsync=false`, and the production cluster default keeps
`write_caching_default=false`, so a successful `acks=all` response is flushed
to disk. Do not replace these with `dev-container` or `--overprovisioned`: both
enable an unsafe fsync bypass in the current image.

The cutover intentionally does not mount or modify the former
`018-hai-kafka-kraft-data`, `018-hai-kafka-data`,
`018-hai-zookeeper-data`, or `018-hai-zookeeper-log` volumes. Kafka and
Redpanda storage formats are not an in-place migration boundary, so queued
events and offsets in a detached legacy log do not transfer automatically.
The backend's transactional outbox remains authoritative for undelivered
automation events, but an operator must still review legacy broker state before
removing any volume. `down -v`, `docker volume prune`, and broad Docker cleanup
are prohibited while that review is pending.

After the first start, require all of the following before accepting the
cutover:

```powershell
docker compose --env-file .env.local -f docker-compose.local.yml ps kafka nginxconfigmanager
docker compose --env-file .env.local -f docker-compose.local.yml exec kafka rpk cluster info -X brokers=127.0.0.1:9092
docker compose --env-file .env.local -f docker-compose.local.yml exec kafka rpk cluster config get write_caching_default
Invoke-WebRequest http://127.0.0.1/readyz -UseBasicParsing
```

If `018-hai-redpanda-data` was first created by an earlier development-mode
candidate, correct its persisted cluster setting once and recreate all clients
so no stale broker connection survives the cutover:

```powershell
docker compose --env-file .env.local -f docker-compose.local.yml exec kafka rpk cluster config set write_caching_default false
docker compose --env-file .env.local -f docker-compose.local.yml up -d --force-recreate idp backend nginxconfigmanager
```

Then run `scripts\smoke-event-pipeline.ps1`. It creates and deletes one
disposable read-only health automation through the authenticated loopback API,
confirms both generated-config effects, and requires two topic-offset advances.
A healthy broker alone proves Kafka-protocol transport, not application
delivery.

## Health Checks

| Check | Command | Healthy |
| --- | --- | --- |
| Liveness | `curl localhost/healthz` | HTTP 200 and `{"status":"ok"}` |
| Readiness | `curl localhost/readyz` | HTTP 200 with `ready` or explicitly understood `degraded`; HTTP 503 with `not_ready` is blocking |
| Config | `backend doctor` | Exit 0 and no `fail` lines |
| Migration state | `backend migrate status` | Required `pre` and `post` versions are applied |
| Data integrity | `backend reconcile` | No unresolved invariant failure |

Gateway `/healthz` and `/readyz` are intentionally public. They expose only
service status. Operational `/api/v1/*` routes remain authenticated unless a
route is explicitly documented as a public callback.

Use the signed-in System Status page or authenticated
`GET /api/v1/system/readiness` when individual dependency checks are needed.

`degraded` is not equivalent to full capability. For example, a missing LLM
provider may leave the control plane available while generation is unavailable.

### Automation Event Delivery

Automation create, update, and delete operations commit their Kafka delivery
intent in the same Postgres transaction as the automation mutation. A broker
outage therefore does not turn a successful API mutation into an ambiguous
failure. The backend retries the committed event with a stable event ID,
bounded exponential backoff, a fenced worker lease, and at-least-once delivery.
The nginx-config consumer treats redelivery idempotently.

The nginx-config consumer uses the stable
`NGINX_CONFIG_MANAGER_GROUP_ID` cursor and starts at the oldest uncommitted
offset. Events published while that service is stopped are therefore consumed
after it returns. It writes payload-free completion receipts below
`NGINX_CONFIG_MANAGER_INBOX_DIR` before advancing offsets. A replay first checks
that persistent inbox. The filesystem effect also compares rendered content
before replacing a file and treats an already-absent delete as successful, so
the narrow crash window between applying an effect and writing its receipt is
still replay-safe. Processing failures remain uncommitted until the configured
retry budget is exhausted; poison messages then receive a durable dead-letter
receipt and no longer block the partition. Receipts are pruned after the
configured retention period in bounded startup batches.

Inspect owner-scoped delivery health at `GET /api/v1/event-delivery/`. The
response contains pending, published, and dead-letter counts, the oldest
pending timestamp, and up to ten recent failures. Event payloads are
intentionally excluded from this endpoint. Readiness also reports the
`kafka.connection` and `events.outbox` probes independently.

A dead letter is never silently discarded. After correcting the broker or
consumer problem, an authenticated owner with admin permission may queue one
again with `POST /api/v1/event-delivery/{event-id}/retry`. The operation resets
the bounded retry counter and the dispatcher wakes on its next poll. Published
delivery rows older than 30 days are pruned in bounded batches; domain audit and
verification records are separate and are not removed by this retention job.

If `KAFKA_BROKERS` is empty or unreachable, mutations remain available because
their events are durable in Postgres, but readiness is degraded and delivery
stays pending. Do not describe those events as published until the outbox state
confirms it.

## Identity And Permission Contract

The gateway and backend derive identity and role from a verified IDP token.
Neither `X-HAI-Role`, request JSON, nor the backend shared key grants a user
role.

| Role | Registry | Tasks |
| --- | --- | --- |
| `viewer` | Read the owner-scoped catalog, Constitution, and selection history. | Read owner-scoped completion logs and review queue. |
| `operator` | Viewer access plus create a selection recommendation. | Plan tasks and operate non-approval controls; cannot approve/run an approval-gated task or resolve a review. |
| `owner` | All registry operations, including preferences and Constitution lifecycle. | Plan, run, approve/reject, and resolve owner-scoped task reviews. |

The seeded `FIRST_RUN_ADMIN_EMAIL` account is the owner. New local
registrations default to operator. Unknown roles grant no permissions.

## Framework Registry Operations

Open `/framework-registry` after signing in.

### Interpret Status Correctly

| Displayed state | Meaning |
| --- | --- |
| `active` | Catalog lifecycle; enabled by default unless this owner disables the record. |
| `experimental` | Catalog lifecycle; disabled by default and eligible only after owner enablement plus a direct framework/name/candidate match. |
| `deprecated` | Catalog lifecycle; retained for compatibility but excluded from selection even if a preference says enabled. There are no deprecated records in catalog v1. |
| `disabled` | Owner-effective preference, not a catalog lifecycle status. |

The current built-in catalog has 55 version `1.0.0` records: 50 active and five
experimental. Enabling an experimental record only makes its decision contract
eligible for a directly matching request. It does not install or trust a named
third-party product. A deprecated record remains excluded from selection.

Protected overlays may be pinned but cannot be disabled. Preferences may lower
an autonomy ceiling, never raise it. Adaptations may add bounded owner context,
but may not bypass approval, the Constitution, emergency stop, secret
redaction, or tool policy.

### Inspect A Selection

For any consequential recommendation, record or inspect:

- catalog version and SHA-256 digest;
- selector algorithm version;
- effective-preference digest;
- Constitution version, exact source, and digest;
- selected framework IDs and versions;
- reasons, conflicts, required agents, authority ceiling, approval reasons;
- evidence requirements and completion criteria.
- operating-contract digest, matched life domains, needs and capacity state;
- verified/unassigned agent cards, delegation budget/deadline/constraints;
- communication/coordination contract and per-action autonomy decision;
- stop conditions, outcome monitoring, and Chief-of-Staff summary.

The current selector version is `selector-v5`. It excludes frameworks whose
risk ceiling is below the effective task risk and fails closed when a mandatory
overlay is incompatible. Do not compare selection UUIDs
to prove replay: the selection time contributes to the decision identity. Use
the version and digest envelope.

For selector-v5, also inspect `taskRiskLevel` and `effectiveRiskCeiling`. The
operating-contract digest binds both values. Blank values are valid only for
pre-v5 immutable history and must be shown as not recorded, never inferred.

Autonomy uses the exact per-action ladder: 0 observe only, 1 inform,
2 recommend, 3 draft, 4 plan and simulate, 5 prepare action, 6 execute after
case-specific approval, 7 execute under standing approval, 8 execute a
reversible low-risk action automatically, 9 execute and notify, and 10 operate
fully autonomously inside a tightly bounded mandate. An approval may authorize
scope but never raises a framework, Constitution, tool, or runtime ceiling.

The public selection preview accepts planning hints, not trusted approval,
owner, or risk assertions. A preview cannot authorize execution.

### Constitution Draft And Activation

When no stored owner version is active, the exact fallback source is:

```text
builtin-robert-constitution-v1:v1
```

Creating a draft does not change active authority. To activate a reviewed
draft, the owner must:

1. sign in with an owner session that has admin permission;
2. inspect the draft and its change summary;
3. enter `ACTIVATE CONSTITUTION` exactly;
4. provide a redacted approval note of at least 10 characters;
5. submit activation once and confirm the previous version became
   `superseded`.

The confirmation is case-sensitive and whitespace-sensitive. Do not paste a
leading/trailing space. A draft is immutable after insertion; make a new draft
to change content.

Ordinary Constitution prose is versioned governance context, not executable
policy. Only code-owned protected controls and valid restrictive typed rules
are machine-enforced:

```text
HAI-RULE v1 deny-capability capability=<known-capability>
HAI-RULE v1 require-approval capability=<known-capability>
HAI-RULE v1 authority-ceiling level=<0..10>
```

A malformed typed rule rejects the draft. There is no rule that grants a
capability or raises authority.

## Durable Task Review Operations

Migration `pre/0004_task_state_storage` stores:

- append-only, redacted completion-plan snapshots;
- review items with immutable owner and request provenance;
- immutable approval/rejection decisions bound to the review revision, exact
  request digest, task plan, resolver, and `task-review:<id>` source.

Authenticated `/task/logs` and `/task/review-queue` reads use this store. If the
repository is unavailable, the API returns an error instead of an empty list.

### State Machine

| State | Meaning | Valid next state |
| --- | --- | --- |
| `open` | First owner decision is pending. | `approved` or `rejected` |
| `needs_review` | An earlier approved attempt failed, was blocked, or did not validate; a new revision awaits a decision. | `approved` or `rejected` |
| `approved` | The exact stored request was approved and its attempt is in progress or has no recorded outcome yet. | `completed` or `needs_review` |
| `rejected` | Owner rejected that review revision. | Terminal |
| `completed` | Approved attempt passed task validation. | Terminal |

Approval is one-shot for the current review revision. The service reruns the
stored request and verifies the owner, review ID, plan/project/automation
bindings, approval source, and request digest. New client fields do not replace
the reviewed action.

### Normal Review Procedure

1. Open the task review queue and inspect the request, project, target,
   success criteria, risk, and source evidence.
2. Reject if the action is wrong, stale, under-specified, or lacks evidence.
3. Approve only if the exact stored action is acceptable.
4. Keep the page open until a terminal `completed` state or a new
   `needs_review` state is visible.
5. Inspect completion validation and runtime evidence. Approval by itself is
   not completion evidence.

Do not submit the same resolution twice. A second decision against a resolved
revision returns a conflict.

### Recovery After Failure Or Restart

An item in `needs_review` can be inspected, corrected through a new governed
task, and approved or rejected as a new revision.

Task-operation lease loss and other indeterminate direct attempts use this same
queue. Their task reference starts with `operation:`. **Retry as new attempt**
requires an explicit confirmation that the audit trail was checked and that the
earlier attempt did not already produce the intended effect. The retry receives
a new durable operation identity; the original uncertain operation is never
resumed, rewritten, or deleted. **Close without retry** records a rejection and
leaves the original operation as immutable audit evidence.

API clients must include `confirmation: "RETRY UNCERTAIN OPERATION"` when
approving this review type. The backend checks the exact phrase before changing
review state, so a direct API call cannot bypass the deliberate retry gate.

An item left `approved` after a backend crash is indeterminate:

1. enable emergency stop if a side effect could still be running;
2. inspect task logs, automation/runtime audit, the target system, and any
   idempotency or correlation identifier;
3. determine whether the side effect did not start, completed, or has an
   unknown outcome;
4. do not resolve the same review again and do not repeat the side effect
   blindly;
5. preview the owner-scoped reconciliation result;
6. verify each proposed disposition against the target and audit evidence;
7. apply the reviewed result only when those dispositions are correct.

Preview is side-effect free:

```http
POST /api/v1/task/review-queue/reconcile
Content-Type: application/json

{"apply":false,"olderThanMinutes":30,"limit":100}
```

Apply requires the exact confirmation string:

```http
POST /api/v1/task/review-queue/reconcile
Content-Type: application/json

{"apply":true,"confirmation":"RECONCILE APPROVED TASKS","olderThanMinutes":30,"limit":100}
```

The reconciler never invokes a runtime, provider, tool, script, Docker action,
or API side effect. It marks `completed` only when an append-only linked plan
already records execution, accepted verification, and passed validation. All
other eligible items return to `needs_review`; concurrent state changes are
reported as conflicts. Use the Task Blueprint recovery panel for the same
two-step flow. Do not mutate the database by hand to bypass this boundary.

There is no automatic task-review recovery worker. The action approval proof is signed with the
deployment key and consumed once through an append-only PostgreSQL ledger, so
HAI rejects replay after restart and across backend instances. This does not
prove that an external target did or did not commit an effect during an
ambiguous failure, so external execution is still not claimed as exactly once.

## Advisory Pursuit Portfolio Planning

Use `POST /api/v1/pursuits/portfolio-plan` to compare owner-visible open
pursuits before deciding what to schedule. The route requires an authenticated
session with read permission. It is intentionally side-effect free.

Before submitting a plan, provide and review:

1. a unique opaque `planId`, `asOf`, horizon start/end, and `expected` or
   `conservative` duration mode;
2. one or more explicit capacity windows representing time the owner can
   actually use;
3. explicit optimistic, expected, and pessimistic minutes for every pursuit to
   be considered;
4. estimated cost in EUR micros, token use, and tool calls;
5. all 25 priority-factor values rather than inferred or placeholder values;
6. request-wide budget and approval thresholds.

Before step 1, record or review the owner's capacity in **LifeOps**. The
production pursuit service never trusts `availableCapacity`, `energyFit`, or
the duration of submitted availability windows as proof of personal capacity.
It loads the latest owner-scoped capacity snapshot, requires confidence of at
least 0.60 with no review flag, recomputes the 24-hour freshness window using
server time, and limits the schedule to the confirmed available minutes.

Example request shape with all 25 priority factors:

```http
POST /api/v1/pursuits/portfolio-plan
Content-Type: application/json

{
  "planId": "weekly-review-2026-08-04",
  "asOf": "2026-08-04T09:00:00Z",
  "horizonStart": "2026-08-04T09:00:00Z",
  "horizonEnd": "2026-08-11T17:00:00Z",
  "durationMode": "conservative",
  "availability": [
    {"start": "2026-08-04T09:00:00Z", "end": "2026-08-04T12:00:00Z"}
  ],
  "pursuits": [
    {
      "pursuitId": "<owner-visible-pursuit-uuid>",
      "duration": {
        "optimisticMinutes": 60,
        "expectedMinutes": 90,
        "pessimisticMinutes": 120,
        "basis": "reviewed work breakdown"
      },
      "estimatedUsage": {
        "costMicros": 0,
        "inputTokens": 0,
        "outputTokens": 0,
        "toolCalls": 2
      },
      "factors": {
        "importance": 8,
        "urgency": 7,
        "humanNeedAffected": 6,
        "deadlinePressure": 7,
        "costOfDelay": 6,
        "expectedValue": 8,
        "harmAvoided": 5,
        "probabilityOfSuccess": 7,
        "effort": 4,
        "duration": 4,
        "dependencies": 3,
        "reversibility": 8,
        "risk": 4,
        "legalObligation": 0,
        "relationshipConsequences": 5,
        "availableCapacity": 7,
        "energyFit": 6,
        "opportunityCost": 4,
        "strategicAlignment": 8,
        "learningValue": 6,
        "compoundingValue": 7,
        "staleness": 3,
        "commitmentAge": 4,
        "peopleBlocked": 2,
        "delegability": 5
      }
    }
  ],
  "budget": {"maxCostMicros": 0},
  "approvalPolicy": {"softDeadlineMiss": true}
}
```

Interpret the response as a proposal, not an instruction to execute:

- `priorities` contains the pure 25-factor score, band, reasons, algorithm
  version, and factor contributions. No priority history is written.
- `decision.scheduled` is a deterministic dependency-ordered allocation inside
  the submitted capacity windows.
- `exclusions` explains missing estimates, pursuit state, unresolved or
  unavailable dependencies, unverifiable resource-ledger state, and effort or
  spend ceiling conflicts.
- `decision.approvalFlags` retains risk, autonomy, uncertainty, budget, and
  approval-policy reasons. A schedule never satisfies an approval.
- `capacity` identifies the durable snapshot state, captured/fresh-until times,
  confidence, confirmed minutes, and minutes actually applied. `missing`,
  `stale`, `needs_review`, `owner_mismatch`, or `unavailable` never includes a
  reservable decision; use the LifeOps capacity check-in before retrying.
- top-level `authority` must be `advisory_only` and `canExecute` must be `false`.
  Treat any other value as a safety incident and do not act on the result.

The planning endpoint does not update pursuits, reserve capacity, consume
approval, enqueue work, or create calendar events.

To accept only the capacity allocation, submit the unchanged planning request,
the returned decision digest, and the exact confirmation phrase:

```http
POST /api/v1/pursuits/portfolio-plan/accept
Content-Type: application/json

{
  "planningRequest": { "...": "the complete original planning request" },
  "expectedDecisionDigest": "<64-character lowercase SHA-256 digest>",
  "confirmation": "ACCEPT PORTFOLIO ALLOCATION"
}
```

The acceptance route requires an authenticated owner with approval permission.
For a first acceptance, HAI requires a fresh proposal and recalculates it
against current pursuit and resource state. Exact retries return the immutable
stored allocation. Reusing a plan ID with a changed request or decision fails
closed.

Validate that the response says `authority: allocation_only` and
`canExecute: false`. A successful acceptance persists the allocation, its
scheduled items, resource holds, approval reasons, and pursuit audit events.
It does not approve, enqueue, execute, change pursuit priority/state, consume
an approval, or create a calendar event. Release a capacity hold only through
the governed reservation-reconciliation flow after confirming no worker or
runtime remains active. Governed execution and external-calendar acceptance
are still separate operations.

Inspect recent accepted allocations after reload with the bounded read-only
history endpoint:

```http
GET /api/v1/pursuits/portfolio-allocations?limit=25
```

The endpoint requires authenticated read permission, is owner-scoped, and
accepts limits from 1 through 100. Reject any record that does not state
`authority: allocation_only` and `canExecute: false`. The Pursuits portfolio
planner loads this history only when opened and provides inspection and refresh
controls; it deliberately provides no approval or execution action.

### Prepare immutable execution proposals

Preparing proposals is a separate owner-only planning transition. It does not
authorize or run the allocated work. Use the accepted allocation ID and its
current immutable allocation record digest:

```http
POST /api/v1/pursuits/portfolio-allocations/<allocationId>/execution-proposals
Content-Type: application/json

{
  "expectedAllocationDigest": "<64-character lowercase SHA-256 digest>",
  "confirmation": "PREPARE EXECUTION PROPOSALS"
}
```

The route requires approval permission because it prepares the evidence for a
later privileged decision. It remains scoped to the authenticated owner and
will not reveal or prepare proposals for another owner's allocation. A stale or
incorrect allocation digest, invalid exact confirmation, changed allocation
evidence, missing pursuit state, or invalid reservation binding fails closed.

Inspect the response before doing anything else:

- `authority` must be `proposal_only` and `canExecute` must be `false`;
- `freshness.revalidationRequired` must be `true`; a newly created response is
  `prepared_snapshot`, while a replay or history response is
  `recovered_snapshot`;
- `freshness.checkedAt` and `freshness.reason` explain when the classification
  was made and why current approval, reservation, workflow, and runtime
  eligibility still need a separate refresh;
- `proposal.status` is `prepared`, `prepared_needs_approval`, or
  `prepared_blocked` based on the captured per-item gates;
- each item remains bound to its accepted allocation item and reservation by
  UUID and record digest;
- `snapshotDigest`, item `stateDigest` values, and record digests identify the
  immutable pursuit/policy/reservation snapshot used for the decision;
- `replayed: true` means the exact stored proposal for unchanged state was
  returned. If relevant owner-scoped state changes, the same allocation creates
  a new immutable snapshot and leaves the old proposal intact.

Treat all three `prepared*` statuses as non-executable. This endpoint does not
approve work, consume approval proofs, enqueue jobs, run tools, call runtimes or
providers, settle or release reservations, or execute external effects. A
proposal reporting `prepared` means only that this snapshot found no current
approval or blocker reason; a later authorization stage must still revalidate
state and establish its own authority.

After a browser refresh, restore the newest immutable proposal for each visible
allocation with one bounded read:

```http
GET /api/v1/pursuits/portfolio-execution-proposals?allocationIds=<allocationId>,<allocationId>
```

Supply one to 20 unique allocation UUIDs. The route preserves request order,
omits allocations without a proposal, and remains owner-scoped. Accept every
returned record only when `authority` is `proposal_only`, `canExecute` is
`false`, and its allocation and item digests match the corresponding immutable
allocation. This read does not prepare a fresh snapshot or grant authority.
History responses must additionally report `replayed: true`,
`freshness.status: recovered_snapshot`, and
`freshness.revalidationRequired: true`. Reject a replay that claims
`prepared_snapshot`, omits its freshness reason, or implies that current
eligibility is already established. Use the separate coordination preview to
derive current eligibility before any dispatch review.

### Per-item proposal decision contract

The decision endpoint records owner evidence for one immutable proposal
item. It is not an execution endpoint:

```http
POST /api/v1/pursuits/portfolio-execution-proposal-items/<itemId>/decisions
Content-Type: application/json

{
  "decision": "approved",
  "expectedItemDigest": "<64-character lowercase SHA-256 digest>",
  "confirmation": "APPROVE EXECUTION PROPOSAL ITEM",
  "reason": "<bounded owner rationale>"
}
```

Use only the exact confirmation paired with the requested decision:

| Decision | Exact confirmation |
| --- | --- |
| `approved` | `APPROVE EXECUTION PROPOSAL ITEM` |
| `rejected` | `REJECT EXECUTION PROPOSAL ITEM` |
| `needs_clarification` | `REQUEST CLARIFICATION FOR EXECUTION PROPOSAL ITEM` |
| `revoked` | `REVOKE EXECUTION PROPOSAL ITEM` |

The implementation derives the owner from the authenticated session and
resolve the item only within that owner scope. Immediately before appending the
decision it must revalidate the immutable proposal-item digest, captured state
digest, current decision history, requested transition, and exact confirmation.
Another owner's item, stale or changed evidence, or an invalid transition must
fail closed. A revocation appends a new `revoked` decision; it never rewrites or
deletes the earlier record.

Reject any response that does not declare
`authority: approval_decision_only` and `canExecute: false`. These append-only
records prove only what the owner decided about the exact snapshot. Even
`approved` does not consume an executable approval capability or satisfy the
separate concrete final-effect authorization required by a later execution
command. This endpoint must not enqueue jobs, mutate tasks or pursuits, call
models, tools, runtimes, APIs, or providers, settle reservations, or produce an
external effect.

Migration `0040`, the owner-only routes, service, UI, unit suites, disposable-
PostgreSQL tests, and local authenticated approval/reload/revocation flow are
verified together. Continue to treat these records as decision evidence only;
they do not make portfolio execution live.

### Evaluate the exact workflow-intake effect

Only the authenticated owner may ask HAI to evaluate the single server-fixed
effect associated with a current, unexpired `approved` proposal item:

```http
POST /api/v1/pursuits/portfolio-execution-proposal-items/<itemId>/authorize-workflow
Content-Type: application/json

{
  "expectedItemDigest": "<current proposal-item digest>",
  "expectedDecisionDigest": "<current approved-decision digest>",
  "confirmation": "AUTHORIZE PORTFOLIO WORKFLOW EFFECT"
}
```

Do not add tool, runtime, action, cost, risk, or autonomy fields. Unknown fields
are rejected, and the backend derives those values from durable owner-scoped
evidence. Inspect the response as a policy result:

- `authority` must be `execution_authorization_only`;
- `canExecute` must be `false`;
- the effect must remain `pursuit.portfolio.create-workflow`,
  `workflow.intake`, `hai-workflow-engine`, reversible, and EUR 0;
- `receipt.outcome` explains whether policy authorized, denied, or still
  requires approval;
- the effect, approval-source, request, and decision digests are audit links.

Even an `authorized` receipt is intentionally unconsumed. This endpoint must
not create or queue a workflow. If the approval is revoked or expires, the
pursuit changes, or the resource reservation is settled before evaluation, the
request must fail closed and a fresh proposal/decision is required.

### Create the approved local workflow

Creating the review-gated workflow is a second explicit owner command:

```http
POST /api/v1/pursuits/portfolio-execution-proposal-items/<itemId>/execute-workflow
Content-Type: application/json

{
  "authorizationReceiptId": "<authorized receipt UUID>",
  "expectedItemDigest": "<exact proposal-item digest>",
  "expectedDecisionDigest": "<exact approved-decision digest>",
  "confirmation": "CREATE APPROVED PORTFOLIO WORKFLOW"
}
```

Accept the result only when `authority` is `workflow_effect_executed`,
`canExecute` is `false`, the consumption names consumer
`pursuit-portfolio-workflow`, and `workflowState` is `needs_approval`. Repeating
the exact request returns the same workflow with `replayed: true`; it must not
create another workflow, consume the receipt again, repeat workflow intake, or
append duplicate pursuit link/effect audit entries. An interrupted request may
restore a missing link or final audit event before returning the same workflow.
This command creates no external effect, does not run the workflow, and does
not settle its resource reservation. Use the workflow review and execution
controls for subsequent steps, each with their own approval and authorization
boundaries.

### Coordinate selected approved portfolio workflows

The portfolio coordinator is a bounded owner-only convenience over the same
per-item authorization and single-use workflow-creation contracts. Start with
the read-only eligibility preview:

```http
GET /api/v1/pursuits/portfolio-execution-proposals/<proposalId>/coordination
```

After a page reload, restore one to 20 immutable proposals with one bounded
read instead of issuing one coordination request per proposal:

```http
GET /api/v1/pursuits/portfolio-execution-proposals/coordination?proposalIds=<proposalId>,<proposalId>
```

The batch read is all-or-nothing and owner-scoped. A missing, duplicate,
foreign, malformed, or twenty-first proposal ID fails the whole request. The
repository uses a fixed group of aggregate reads, limits the combined proposal
items to 500, and never records a decision, dispatch, workflow, or approval.
The dashboard automatically restores this current coordination state after
immutable proposal history loads, clears previous selections and confirmation
phrases first, and never preselects an item.

Accept the preview only when `authority` is `coordination_preview_only` and
`canExecute` is `false`. Its `freshness.status` must be
`current_coordination_snapshot`, and `revalidationRequired` remains `true`
because dispatch performs its own independent revalidation. Items are
selectable only when their latest decision
is a current, unexpired approval and their immutable proposal, pursuit, and
reservation evidence still matches. Items already dispatched, blocked, stale,
failed, or awaiting approval remain visible but cannot be selected.

Nothing is selected by default. Choose one to 20 exact item IDs and bind every
selection to its current item and decision digests:

```http
POST /api/v1/pursuits/portfolio-execution-proposals/<proposalId>/dispatch
Content-Type: application/json

{
  "expectedProposalDigest": "<exact proposal record digest>",
  "items": [
    {
      "proposalItemId": "<selected proposal-item UUID>",
      "expectedItemDigest": "<exact proposal-item digest>",
      "expectedDecisionDigest": "<current approved-decision digest>"
    }
  ],
  "confirmation": "DISPATCH APPROVED PORTFOLIO WORKFLOWS"
}
```

For each selected item, HAI independently reloads owner-scoped evidence,
evaluates the server-fixed EUR 0 workflow-intake effect, and consumes an
authorized receipt through the existing idempotent workflow-creation path.
The append-only run and item-attempt ledgers make exact retries resumable and
prevent a successful item from being created twice. Partial results are
reported as `needs_approval`, `blocked`, `stale`, `failed`, or `cancelled`
instead of being presented as success.

Accept the response only when `authority` is `portfolio_dispatch_result`,
`canExecute` is `false`, every item matches the selected immutable evidence,
and the counters match the individual outcomes. A successful item names one
receipt-bound workflow in `needs_approval`. This coordinator never records an
approval decision, runs a workflow, contacts an external party, calls an
external provider for an effect, settles or releases a resource reservation,
or bypasses emergency stop and downstream workflow review.

### Run one reviewed workflow

After any required exact automation selection and durable owner approval, run
only the reviewed workflow instead of sweeping the complete due queue:

```http
POST /api/v1/workflow/<workflowId>/run
```

The command is owner-scoped and atomically claims only that workflow. A second
request is skipped once the item is no longer ready. Treat `completed` as final
only when the returned state is `completed` and the persisted workflow detail
contains passing verification evidence. `blocked`, `skipped`, retry, or review
outcomes are not completion. Emergency stop and all task, automation, runtime,
approval, audit, and verification gates remain active. This command does not
settle the portfolio resource reservation automatically.

### Review internal reminder proposals

Read the authenticated owner's due and upcoming checklist reminders without
activating any effect:

```http
GET /api/v1/workflow/reminder-proposals?horizonHours=168&limit=100
```

Accept the response only when `authority` is `reminder_proposal_only`,
`canExecute` is `false`, freshness is `current_internal_reminder_snapshot`, and
`revalidationRequired` is `true`. The due and upcoming counters must exactly
match the item statuses. The Workflow Engine shows at most the nearest three
items in its compact reminder strip; opening one only loads the existing
workflow detail. This endpoint does not record approval, activate a reminder,
schedule a local notification, send a message, run a follow-up, or create or
change a Calendar event. Those effects require separate future authority and
acceptance paths.

### Prepare and decide reminder activation evidence

The activation ledger records operator intent and owner decisions only. It is
not a scheduler, queue, notification service, provider command, Calendar
adapter, or follow-up runner. Start from a fresh owner-scoped reminder proposal;
never construct reminder evidence from a stale browser snapshot.

Prepare one immutable request for a current checklist item:

```http
POST /api/v1/workflow/reminder-proposals/<checklistItemId>/activation-requests
Content-Type: application/json

{
  "expectedReminderDigest": "<64-character digest from the fresh proposal>",
  "idempotencyKey": "<owner-generated stable key>",
  "activationKind": "internal_notification",
  "confirmation": "PREPARE INTERNAL REMINDER ONLY"
}
```

Accept the result only when `authority` is
`reminder_activation_request_only`, `canExecute` is `false`, and the stored
owner, workflow, checklist item, reminder time, source digest, request digest,
record digest, and expiry match the intended proposal. Reusing the same owner
and idempotency key with the same request replays the immutable record; changed
reuse fails closed. A request expires after 15 minutes.

The process-local router `Idempotency-Key` cache deliberately allows both
reminder mutation routes through. The authoritative replay/conflict decision is
owner-scoped and durable in PostgreSQL: preparation uses the body
`idempotencyKey` plus canonical request evidence, while a decision uses the
activation request and canonical decision request digest. Supplying an HTTP
`Idempotency-Key` therefore does not replace the required body evidence and
must not be interpreted as the durable record identity.

Inspect the owner's bounded request and decision history:

```http
GET /api/v1/workflow/reminder-activation-requests?limit=50
GET /api/v1/workflow/reminder-activation-requests/<requestId>/decisions?limit=50
```

History responses must retain `canExecute:false`. Refresh the proposal when a
request is stale or expired. Do not reinterpret `prepared`, `approved`, or any
history record as evidence that a reminder was delivered.

Append a decision through the approval-protected endpoint:

```http
POST /api/v1/workflow/reminder-activation-requests/<requestId>/decisions
Content-Type: application/json

{
  "decision": "approved",
  "reason": "Owner accepts preparation evidence only.",
  "confirmation": "APPROVE INTERNAL REMINDER PREPARATION",
  "expectedActivationRequestDigest": "<request record digest>",
  "expectedPreviousDecisionId": "<current chain tip, or empty for the first decision>"
}
```

The exact confirmation phrases are:

| Decision | Confirmation |
| --- | --- |
| `approved` | `APPROVE INTERNAL REMINDER PREPARATION` |
| `rejected` | `REJECT INTERNAL REMINDER PREPARATION` |
| `needs_clarification` | `REQUEST REMINDER CLARIFICATION` |
| `revoked` | `REVOKE INTERNAL REMINDER PREPARATION` |

Only the latest approved preparation can be revoked. Approval evidence expires
after 10 minutes and still grants no effect authority. Preparation or approval
cannot create or change a Calendar event, schedule or send a notification,
email, or other message, run a provider/runtime, execute an open-loop follow-up,
or mutate the source workflow/checklist. No reminder worker is active. The
existing workflow/open-loop scheduler is a separate subsystem and does not
consume these records. Any future reminder delivery needs a separate reviewed
authorization, effect ledger, idempotent executor, and postcondition evidence.

Current retained acceptance evidence includes the migration-chain contract,
isolated PostgreSQL `0046`+`0047` ledger test, live workflow-repository
PostgreSQL test, full Go suite, 324 Angular tests, production build, and a
signed-in browser prepare/approve/persist/cleanup exercise. That browser run
proves persistence and UI/API coordination only; it did not execute or test a
reminder effect.

### Settle verified portfolio usage

After the receipt-bound workflow is durably `completed`, carries verification
status `verified` or `test_passed`, and has an immutable completion attestation,
account for measured usage with a separate owner command:

```http
POST /api/v1/pursuits/portfolio-execution-proposal-items/<itemId>/settle-workflow
Content-Type: application/json

{
  "workflowId": "<completed receipt-bound workflow UUID>",
  "expectedItemDigest": "<exact proposal-item digest>",
  "actualEffortMinutes": 12,
  "actualCostMicros": 0,
  "confirmation": "SETTLE VERIFIED PORTFOLIO WORK"
}
```

Accept the result only when `authority` is `verified_accounting_only`,
`canExecute` is `false`, `disposition` is `consumed`, and the response identifies
the exact pursuit, item, reservation, workflow, verification status, and
attestation-derived evidence URI. An exact retry returns `replayed: true`, even
if the mutable workflow projection was archived after the first settlement.
Changing measured usage on retry, supplying another workflow, using a weak
verification label, presenting an unattested completion, or linking the same
receipt to multiple workflows must fail. The settlement, immutable portfolio
proof, usage events, and activity record commit atomically. This command records
accounting; it does not rerun the workflow or grant provider/runtime authority.

The learning fields are independent from accounting. `learningStatus` confirms
whether verified evidence reached the controlled-learning ledger.
`learningProposalStatus: insufficient_evidence` is normal until at least three
comparable owner/project-scoped settlements exist. `review_required` means HAI
created a deterministic estimate-calibration proposal; it is still inert.
`monitoring` means a prior review anchors the evidence window and fewer than
three fresh comparable outcomes exist. While a proposal remains unresolved,
HAI keeps that single review item and reports `learningNewEvidenceCount` plus
`learningDriftDetected` instead of creating competing proposals.
Review and explicitly approve or reject that proposal in Governance Control.
After approval, a later portfolio plan may display a reviewed estimate
suggestion. Select **Use suggestion** and recalculate to bind the exact proposal,
application, and evidence digest. Never treat the suggestion, approval, or
binding as permission to execute work. Rolling back the learning application
makes its revision unavailable to new and revalidated plans.

## Advisory Ambient Outcome Monitor

Use this capability only to observe deterministic local-ledger indicators and
surface reviewable owner attention. It is not an execution, reminder,
notification, Calendar, mandate, or learning worker.

### Configure and inspect

1. In Governance Control, load the existing owner/workspace outcome definition
   and select one of its indicators.
2. Choose exactly one fixed collector:
   `workflow_open_loop_count`, `workflow_verified_completion_count`, or
   `overdue_commitment_count`.
3. Supply a canonical target UUID, a first-run timestamp, and a cadence from
   60 seconds through 30 days. Target owner identity comes from the signed
   session, never from the form or request body.
4. Create the target. Repeating the exact request is state-idempotent; changing
   immutable target scope or collector requires a different target.
5. Inspect the Basic status first. Use Advanced detail for immutable
   observation and run history, source digests, lease/run state, and failure
   summaries.

Read-capable roles can inspect. A write-capable role can request a bounded due
pass. Creating, enabling/disabling, and recovering expired state requires the
administrator permission applied by the router.

### Guarded API routes

All routes start at
`/api/v1/outcome-evaluations/workspaces/:workspaceId` and require an
authenticated owner plus recognized role:

| Method and suffix | Purpose | Permission |
| --- | --- | --- |
| `GET /outcomes/:outcomeId/monitor` | List owner/outcome targets | read |
| `PUT /outcomes/:outcomeId/monitor` | Register one immutable-scope target | administrator |
| `PATCH /outcomes/:outcomeId/monitor/:targetId/enabled` | Pause or resume future claims | administrator |
| `GET /outcomes/:outcomeId/monitor/:targetId/observations` | Read bounded immutable evidence | read |
| `GET /outcomes/:outcomeId/monitor/:targetId/runs` | Read bounded immutable receipts | read |
| `POST /monitors/run-due` | Process a bounded due batch | write |
| `POST /monitors/recover` | Release expired leases only | administrator |

Do not send owner identity in JSON. The server binds it from authentication.
History limits are bounded; keep operator requests smaller than the maximum.

### Durable scheduler settings

| Variable | Default | Valid range / meaning |
| --- | ---: | --- |
| `OUTCOME_MONITOR_SCHEDULER_ENABLED` | `true` | Enables the persisted `outcome-monitor.sweep` singleton. |
| `OUTCOME_MONITOR_SWEEP_SECONDS` | `300` | 60-86400 seconds between recurring sweeps. |
| `OUTCOME_MONITOR_POLL_SECONDS` | `15` | 1-300 seconds between durable queue polls. |
| `OUTCOME_MONITOR_LEASE_SECONDS` | `120` | 5-1800 seconds; repository/database rules cap an active lease to one hour. |
| `OUTCOME_MONITOR_SCOPE_LIMIT` | `50` | 1-100 due owner/workspace scopes per sweep. |
| `OUTCOME_MONITOR_BATCH_LIMIT` | `20` | 1-100 targets claimed per scope. |

Invalid or out-of-range values fall back to these bounded defaults. The shared
background safety predicate is checked on each durable run. Turning scheduling
off stops automatic claims; it does not delete targets or evidence and does not
enable a manual effect path.

### Failure and recovery

- A collector reads only its fixed owner-scoped SQL projection. Do not add a
  target that accepts arbitrary SQL, URLs, scripts, expressions, or tools.
- Run completion is lease-generation fenced. Do not clear or replace an active
  unexpired lease. Recover only after expiry.
- Disable a target before its next claim when observation should stop. An
  already active lease is not a cancellation mechanism; wait for completion or
  expiry and then recover through the guarded operation.
- Failed runs retain a bounded redacted summary. Preserve their immutable
  records and investigate the local ledger/schema before retrying.
- Exact completion replay must reuse the existing observation/run and resume
  composition without recollecting. A changed request or digest must fail.

### Required acceptance before operational trust

1. Seed and verify all three collector counts and source digests in disposable
   PostgreSQL.
2. Prove exact replay after success and after one transient composition failure
   creates no duplicate observation, run, outcome evaluation, signal, decision,
   or inbox item.
3. Exercise two owners and two workspaces through repository and signed HTTP
   paths; confirm no target, history, claim, or recovery crosses scope.
4. Prove disabled targets are skipped, active leases are fenced, expired leases
   recover once, and a new claim advances the generation.
5. Exercise Governance Control create, inspect, manual run, pause, resume, and
   history paths with a signed session.
6. Assert zero task/runtime execution, notification, message delivery, Calendar
   write, workflow mutation, mandate authorization, provider call, or learning
   mutation throughout the run and retry chain.

Until this acceptance is retained for the target deployment, report the
monitor as implemented advisory infrastructure, not live-proven real-world
automation.

## Migrations And Rollback

Inspect and apply embedded migrations:

```text
backend migrate status
backend migrate up
```

Relevant pre-phase migrations:

| Migration | Data |
| --- | --- |
| `pre/0003_framework_registry` | Preferences, immutable selection records, Constitution versions/lifecycle |
| `pre/0004_task_state_storage` | Completion logs, review items, immutable decisions |
| `pre/0005_framework_operating_contract` | Immutable selector-v4 operating context, digest, and Chief-of-Staff trace |
| `pre/0028_automation_approval_proof_consumptions` | Owner-scoped immutable single-use automation approval capability consumption |
| `pre/0029_framework_selector_v5_digest` | Requires a real nonzero operating-contract digest for selector-v5 records while preserving selector-v4 history |
| `pre/0038_pursuit_portfolio_allocations` | Immutable owner-governed portfolio allocations, scheduled items, resource-reservation bindings, and append-only constraints |
| `pre/0039_pursuit_portfolio_execution_proposals` | Immutable owner-scoped execution-proposal snapshots and per-item allocation, reservation, pursuit-state, policy, and audit bindings |
| `pre/0040_pursuit_portfolio_execution_proposal_decisions` | Immutable owner-scoped proposal-item decisions, expiry, revocation links, and append-only audit evidence |
| `pre/0041_execution_authorization_portfolio_approval` | Portfolio decision provenance on immutable execution-authorization receipts |
| `pre/0042_workflow_active_source_idempotency` | Unique active owner/source workflow identity for interruption-safe receipt-bound intake |
| `pre/0043_pursuit_activity_idempotency` | Nullable deterministic audit keys for interruption-safe pursuit effect recording without rewriting history |
| `pre/0044_workflow_completion_settlement_proofs` | Immutable verified/test-passed workflow completion attestations and receipt-bound portfolio settlement proofs |
| `pre/0045_pursuit_portfolio_dispatch_coordination` | Immutable owner-scoped portfolio dispatch requests and append-only per-item attempt, receipt, workflow, replay, and failure evidence |
| `pre/0046_workflow_reminder_activation_ledger` | Append-only owner-scoped internal-reminder preparation requests and chained decision evidence; no delivery job or external effect |
| `pre/0047_workflow_reminder_activation_decision_order` | Strict database guard requiring every new decision timestamp to advance beyond the current request chain tip |
| `pre/0048_proactivity_attention_feedback` | Append-only owner-scoped attention feedback bound to advisory decisions; controls later surfacing only and grants no delivery or execution authority |

Explicit rollback targets:

```text
backend migrate down pre/0005_framework_operating_contract
backend migrate down pre/0004_task_state_storage
backend migrate down pre/0003_framework_registry
```

These rollbacks delete operator and audit history. The runner requires reverse
order and rejects older targets while a later migration in the phase remains
applied. Before any rollback:

1. stop task execution and dependent application versions;
2. take and verify a PostgreSQL backup;
3. deploy a compatible earlier application version;
4. use the explicit `pre/...` target;
5. verify migration state and owner-scoped reads after restart.

See [Database Migrations And Rollback Safety](migrations.md).

## Routine Security Tasks

- **Rotate backend and JWT keys:** update `.env.local`, recreate affected
  services, invalidate old sessions where required, and rerun authenticated
  smoke checks.
- **Rotate approval-proof signing keys:** update
  `HAI_APPROVAL_PROOF_SIGNING_KEY` on every backend instance in one controlled
  deployment. Rotation intentionally invalidates every unexpired proof; do not
  rotate while approved actions are awaiting execution.
- **Backups:** follow [backup and restore](backup-restore.md); test restore
  evidence, not only dump creation.
- **Rate limiting:** ngrok preflight requires a positive
  `RATE_LIMIT_PER_MINUTE`; verify Redis-backed enforcement before exposure.
  The gateway separately throttles public authentication and A2A requests with
  fixed one-MiB zones so Redis failure does not leave those entry points
  unbounded.
- **Emergency stop:** set `HAI_EMERGENCY_STOP=true` in `.env.local` and recreate
  the backend service. Confirm task, workflow, LLM, automation, and runtime
  execution are blocked while read/review surfaces remain available.
- **Secret handling:** never put credentials in Constitution prose, framework
  adaptations, task requests, approval notes, or support bundles.

## Incident Response

| Symptom | First step |
| --- | --- |
| `/readyz` returns 503 | Read the failing checks, run `backend doctor`, repair the dependency, and restart. |
| Mass 401 responses | Check the IDP session and verified JWT, then the gateway/backend shared-key wiring. |
| Registry history is empty unexpectedly | Treat a 500 as a storage incident; verify migration `0003` and database connectivity. Do not create replacement history. |
| Task history/review queue returns 500 | Verify migration `0004` and database connectivity. Do not treat the queue as empty. |
| Selection fails at 12 frameworks | Inspect direct intent and required-overlay coverage; do not raise the cap or remove safety overlays without a reviewed selector change. |
| Constitution activation returns 400 | Re-enter the exact phrase, use an owner session, and inspect typed-rule syntax and approval-note length. |
| Constitution activation returns 409 | Refresh versions; the draft is stale, invalid, or lost its lifecycle precondition. |
| Review resolution returns 404 | Confirm owner scope and review ID. Do not search another owner's records. |
| Review resolution returns 409 | The revision was already resolved or changed state. Refresh before taking another action. |
| Review is stuck `approved` | Follow the indeterminate-outcome procedure, preview reconciliation, verify the proposed disposition, then apply it. Never retry the action from the recovery path. |
| Reminder preparation is `approved` but nothing was delivered | This is expected: approval records non-executing evidence only. Confirm `canExecute:false`; do not retry providers or Calendar. No reminder worker is active. |
| A proactive item remains hidden | Inspect its latest Governance Control feedback. An active snooze or suppression intentionally controls attention only; append `resume` to restore evaluation. Never infer that hidden work was completed or externally handled. |
| System workload is denied | Inspect the authorization receipt's redacted `systemWorkload.policyId` and request classification. Do not weaken the policy or relabel the caller. Register a reviewed exact workload contract only when a new built-in process is intentionally introduced. |
| Runaway or uncertain execution | Enable emergency stop, preserve logs/evidence, and inspect the external target before retrying. |
| Suspected data corruption | Run `backend reconcile` for a dry-run report; do not delete audit rows. |

## Readiness And Escalation

Collect a redacted support bundle, migration status, readiness payload, relevant
selection/review IDs, and the last correlated logs before escalating. Do not
include raw source content, tokens, passwords, approval proofs, or private
request payloads.

Repository tests prove implementation contracts, not real-world capability.
Before operational trust on a release target still require:

- repeating the retained clean-clone, migration, sign-in, and browser journey
  on that distinct Windows 11 target;
- a two-real-account owner-isolation exercise;
- one bounded configured local-model task;
- separately approved and evidenced live connector/runtime exercises;
- crash/restart and restore evidence for the intended deployment;
- a reviewed recovery procedure for indeterminate approved task reviews.

See [Framework Registry](framework-registry.md),
[verification evidence](verification-honest-health-readiness.md), and
[troubleshooting](troubleshooting.md).
