# Database Migrations And Rollback Safety

## Principle

Schema changes ship as reviewable, reversible, recorded migration files, not
only Gorm `AutoMigrate`. Every applied version is auditable in
`schema_migrations`, and every shipped migration must have a reviewed down
file.

## Implemented Layout

```text
backend/migrations/
  embed.go
  pre/
    0001_extensions.up.sql
    0001_extensions.down.sql
    0002_baseline.up.sql
    0002_baseline.down.sql
    0003_framework_registry.up.sql
    0003_framework_registry.down.sql
    0004_task_state_storage.up.sql
    0004_task_state_storage.down.sql
  post/
    0001_conversation_owner_identity.up.sql
    0001_conversation_owner_identity.down.sql
    0002_durable_jobs_indexes.up.sql
    0002_durable_jobs_indexes.down.sql
```

The phases run in this order:

1. **`pre/`** migrations apply before model initialization. They provide
   extensions, the versioned baseline, and schema that must exist independently
   of runtime model creation.
2. **Gated AutoMigrate** runs only when `DB_AUTOMIGRATE=true`. This is a
   development aid, not the production schema source of truth.
3. **`post/`** migrations apply constraints, indexes, or backfills that depend
   on existing tables.

Migration versions are ordered within their phase. Do not edit a migration
after it has shipped; add a new numbered pair.

## Runner

`internal/infra/migrate.go` loads the embedded files, applies each pending
migration in its own transaction, and records it. `RunMigrations` executes:

```text
pre -> optional development AutoMigrate -> post
```

The production Compose path invokes that sequence only from the non-root,
one-shot `backend-migrate` service using the schema-owner credentials. It then
creates or rotates the least-privilege runtime role. The long-lived backend is
gated on successful migration, connects as that runtime role, and sets
`DB_RUN_MIGRATIONS=false`. Direct/non-Compose development retains the historic
default of running migrations from `GetDefaultDB` unless the variable is
explicitly disabled.

### CLI

```text
backend migrate status
backend migrate up
backend migrate down <target>
```

Rollback targets may include their phase:

```text
backend migrate down pre/0003_framework_registry
backend migrate down post/0001_conversation_owner_identity
```

For backward compatibility, an unqualified target defaults to `post`:

```text
backend migrate down 0001_conversation_owner_identity
```

Use an explicit phase in operator procedures. It prevents a correct version
name from being interpreted against the wrong migration directory.

## AutoMigrate Is Retired By Default

`migrations/pre/0002_baseline.up.sql` is the versioned baseline generated from
the migrated schema. `DB_AUTOMIGRATE` defaults to `false`.

- Default or `DB_AUTOMIGRATE=false`: embedded SQL migrations are the schema
  source of truth.
- `DB_AUTOMIGRATE=true`: development only, used to materialize a newly added
  Gorm model before regenerating and reviewing the baseline or a new migration.

The baseline uses idempotent table/index creation and guarded constraints so it
can be applied to a database previously built by AutoMigrate.

After adding or changing a model:

```text
DB_AUTOMIGRATE=true backend migrate up
scripts/generate-migration-baseline.sh
```

Review the generated diff, add a reversible migration when appropriate, and
return to `DB_AUTOMIGRATE=false`.

## Framework Registry Migration

`pre/0003_framework_registry` creates the owner-scoped Framework Registry:

- `framework_preferences`;
- `framework_selection_records`;
- `robert_constitution_versions`;
- digest, owner, status, JSON-shape, and autonomy constraints;
- immutable selection-history and Constitution-lifecycle triggers.

The exact rollback command is:

```text
backend migrate down pre/0005_framework_operating_contract
backend migrate down pre/0004_task_state_storage
backend migrate down pre/0003_framework_registry
```

This removes all three tables and their history. It is a destructive rollback,
not a feature toggle. Back up the database, stop code that depends on the
registry, and deploy a compatible previous application version first. The
migration runner rejects an out-of-order rollback while either later pre-phase
migration remains applied.

## Task-State Storage Migration

`pre/0004_task_state_storage` creates owner-scoped, durable task-success state:

- immutable completion-plan logs;
- review items with immutable request provenance;
- append-only approval/rejection decisions bound to an exact owner, review
  request digest, and task plan;
- constraints and triggers that reject cross-owner or mutable audit history.

The exact rollback command is:

```text
backend migrate down pre/0004_task_state_storage
```

This removes task completion history, review items, and decisions. Treat it as
a destructive recovery operation: stop task workers, back up the database, and
deploy a compatible application before rolling it back.

## Framework Operating Contract Migration

`pre/0005_framework_operating_contract` extends immutable framework-selection
history with JSONB columns for life domains, needs state, capacity, agent
cards, delegations, communication, coordination, per-action autonomy, stop
conditions, outcome monitoring, and the Chief-of-Staff summary. JSON shape
constraints reject arrays where objects are required and vice versa. A
validated SHA-256 operating-contract digest is indexed for trace inspection.

The exact rollback command is:

```text
backend migrate down pre/0005_framework_operating_contract
```

This removes selector-v4 operating context from stored selections. Stop
selection and task writers, back up the registry, and deploy a compatible
selector-v3 application before rollback.

`pre/0029_framework_selector_v5_digest` extends the immutable operating-digest
constraint to selector-v5 records and adds nullable `task_risk_level` and
`effective_risk_ceiling` fields. Selector-v5 rows require both fields, valid
risk values, and a ceiling at or above task risk. It does not rewrite historical
selector-v4 decisions. Its rollback restores the selector-v4-only digest
constraint and refuses to run while selector-v5 rows exist. Stop v5 writers and
retain/export their immutable records before any reviewed removal and rollback:

```text
backend migrate down pre/0029_framework_selector_v5_digest
```

## Model Outcome Calibration Migration

`pre/0031_model_outcome_calibration` adds redacted validation status/method,
estimated cost, and fallback depth to durable model-run telemetry. Database
constraints bound every value, while indexes support lane/provider/model
calibration without storing prompts, outputs, source text, or secrets. The
rollback refuses to remove the columns after any non-default outcome evidence
has been recorded.

```text
backend migrate down pre/0031_model_outcome_calibration
```

PostgreSQL text already rejects NUL bytes. The migration therefore rejects
line breaks in validator identifiers without embedding a PostgreSQL E-string
NUL escape, which would make the migration itself invalid.

## Pursuit And Workflow Standing-Mandate Binding

`pre/0032_pursuit_workflow_standing_mandate_binding` adds nullable
`mandate_id` columns to pursuits and workflow items. Composite foreign keys to
`standing_mandates(owner_identity, id)` prevent an unknown or cross-owner
mandate from entering either durable handoff. Partial indexes support
owner-scoped inspection without changing records that do not use a standing
mandate. The migration does not select, activate, or authorize a mandate.

Rollback refuses to remove the columns while either table contains a mandate
binding. Stop intake and worker processes, retain the affected pursuits and
workflows, and remove their bindings only through a reviewed data migration
before running:

```text
backend migrate down pre/0032_pursuit_workflow_standing_mandate_binding
```

## Pursuit Goal Contract

`pre/0033_pursuit_goal_contract` adds JSONB success criteria, stop conditions,
dependencies, and resource limits plus target and review-cadence columns to
`pursuits`. Existing completion definitions become pending legacy success
criteria, and existing pursuits receive a monitored safe-stop condition.
Database constraints require arrays/objects and bound cadence and parallel-work
values; an index supports due-target review.

The application validates criterion evidence, human-approved waivers, stop
timestamps, dependency evidence, related-pursuit identifiers, and nonnegative
resource ceilings before persistence. Rollback refuses to discard any nonempty
goal contract. A reviewed data migration must preserve or remove those records
before running:

```text
backend migrate down pre/0033_pursuit_goal_contract
```

## Pursuit Resource Ledger

`pre/0034_pursuit_resource_ledger` adds the owner-scoped, append-only
`pursuit_resource_events` ledger. Effort is stored as integer minutes and money
as integer EUR cents. Database constraints bind each event kind to exactly one
quantity, require safe idempotency keys and record digests, and prevent a refund
from exceeding recorded net spend. Owner/pursuit/idempotency uniqueness makes
replayed requests deterministic.

PostgreSQL triggers reject update, delete, and truncate operations. Rollback
refuses to remove a nonempty ledger, so retained accounting evidence must be
exported and handled through a reviewed data migration before running:

```text
backend migrate down pre/0034_pursuit_resource_ledger
```

## Pursuit Resource Reservations

`pre/0035_pursuit_resource_reservations` adds immutable
`pursuit_resource_reservations` and
`pursuit_resource_reservation_settlements` ledgers. A task attempt reserves
integer effort minutes and EUR micros immediately before execution. The
database takes a pursuit-scoped advisory transaction lock and checks recorded
usage plus every active hold against the pursuit ceiling. The same lock is
installed on direct resource-event inserts, preventing a hold and a direct
accounting event from independently passing the same remaining balance.

Each reservation has at most one append-only consumed or released settlement.
A consumed settlement appends actual effort and spend events in the same
transaction; a settlement that would exceed the ceiling fails closed and
leaves the hold active. A worker crash likewise leaves an active immutable hold
instead of silently releasing capacity that may have been consumed. A hold
older than 24 hours is surfaced for reconciliation review, but age alone never
releases capacity. `pre/0036_pursuit_resource_reservation_reconciliation` adds
an immutable settlement reason. An authenticated approver can confirm that an
operation has stopped and append a released settlement with that reason. The
original reservation remains unchanged.

Rollback refuses to discard either nonempty reservation table. Export and
resolve retained holds through a reviewed data migration before running:

```text
backend migrate down pre/0035_pursuit_resource_reservations
```

Migration `0036` refuses rollback when a settlement reason would be lost.
Resolve retained evidence through a reviewed data migration before running:

```text
backend migrate down pre/0036_pursuit_resource_reservation_reconciliation
```

## Task Operation Identity

`pre/0037_task_operation_identity` adds the owner-scoped `task_operations`
claim ledger. Plan and run requests bind an idempotency key to the redacted
request digest and mode. One PostgreSQL advisory-lock winner owns a fenced
lease while the synchronous task engine plans, executes allowed work, validates
the result, and appends its immutable completion plan. The worker heartbeats
the lease every 20 seconds. A matching completed replay returns that exact
stored plan; changed input conflicts; an active duplicate reports in progress;
and an expired or lost lease becomes `needs_review` rather than executing
again. Workflow retries derive the key from the durable workflow ID and, after
approval, its immutable approval-decision provenance. This keeps pre-approval
review separate from the approved execution while making each stage replayable.
The Angular task client creates one UUID per user action and preserves caller
keys.

This is at-least-once request handling with local fencing, not a claim of
exactly-once behavior in an external API or runtime. A network-ambiguous
external effect still requires provider idempotency and postcondition evidence.
Rollback refuses to discard nonempty operation audit state:

```text
backend migrate down pre/0037_task_operation_identity
```

## Receipt-Bound Workflow Intake

`pre/0042_workflow_active_source_idempotency` rejects existing duplicate active
workflow source identities, then adds a unique partial index over
`(owner_identity, source_type, source_id)` for nonempty, non-archived records.
The portfolio workflow path uses the consumed execution-authorization receipt
ID as `source_id`. This makes interruption recovery idempotent at the database
boundary while allowing an archived workflow to be replaced deliberately by a
later reviewed operation.

Rollback removes only the index; it does not delete workflow records:

```text
backend migrate down pre/0042_workflow_active_source_idempotency
```

`pre/0043_pursuit_activity_idempotency` adds a nullable activity idempotency
key and a unique partial index over `(pursuit_id, idempotency_key)`. Existing
audit history is neither rewritten nor deleted: historical rows keep a null
key, while new receipt-bound effect events use a deterministic key. This lets
concurrent or interrupted exact retries restore a missing final audit event
without adding another one.

Rollback removes the index and column but leaves the activity rows intact:

```text
backend migrate down pre/0043_pursuit_activity_idempotency
```

## Workflow Completion And Portfolio Settlement Proofs

`pre/0044_workflow_completion_settlement_proofs` adds two append-only evidence
tables. `workflow_completion_attestations` binds a terminal workflow to its
owner, task-plan result, `verified` or `test_passed` status, runtime evidence,
and record digest. `pursuit_portfolio_workflow_settlement_proofs` binds the
accounting settlement to the exact proposal item, original decision,
authorization receipt, receipt consumption, workflow, attestation, measured
usage, and record digest. Insert-validation triggers reject mismatched mutable
projections; update and delete triggers protect accepted history.

Rollback refuses to remove either table while it contains proof records:

```text
backend migrate down pre/0044_workflow_completion_settlement_proofs
```

## Portfolio Dispatch Coordination

`pre/0045_pursuit_portfolio_dispatch_coordination` adds append-only coordination
ledgers for an explicitly selected batch of already approved portfolio proposal
items. `pursuit_portfolio_dispatch_runs` binds the authenticated owner, immutable
proposal, exact confirmation phrase, selected item digest, and aggregate result.
`pursuit_portfolio_dispatch_item_results` records each item's authorization and
review-gated workflow-creation outcome without rewriting successful history.

Database validation requires each successful item result to match its immutable
proposal item, latest approval decision, exact authorization receipt, receipt
consumption, and receipt-bound `needs_approval` workflow. Update, delete, and
truncate are rejected. A failed or policy-denied item cannot claim authorization
or workflow fields, and retry may reuse only the exact prior run and item result.

Rollback refuses to remove either table while it contains coordination records:

```text
backend migrate down pre/0045_pursuit_portfolio_dispatch_coordination
```

## Workflow Reminder Activation Ledger

`pre/0046_workflow_reminder_activation_ledger` adds two owner-scoped,
append-only evidence tables. `workflow_reminder_activation_requests` binds an
open checklist reminder to its current workflow/checklist state, reminder and
due times, source digest, authenticated owner/actor, stable idempotency key,
exact preparation confirmation, record digests, and a bounded expiry.
`workflow_reminder_activation_decisions` appends `approved`, `rejected`,
`needs_clarification`, or `revoked` evidence against the immutable request and
current decision-chain tip. Approval has its own bounded expiry.

`pre/0047_workflow_reminder_activation_decision_order` adds a second insert
guard to the decision ledger. For the same owner and activation request, every
new decision's `decided_at` must be strictly greater than the current chain
tip's timestamp. Equal or earlier timestamps fail before insertion, preventing
timestamp ties or clock regression from making newest-first history ambiguous.
The existing predecessor-ID chain check remains independently enforced.

Insert validation rejects owner/source mismatch, closed or archived work,
changed reminder timing, invalid authority/confirmation, malformed digests,
cross-owner history, a non-current decision-chain predecessor, and revocation
unless the latest decision is approved. Unique owner/idempotency and request
digests make exact retries replayable. Update, delete, and truncate triggers
reject mutation of either ledger.

This schema stores preparation and decision evidence only. Neither the
migration nor an `approved` row creates a Calendar event, schedules or sends a
notification, email, or other message, invokes a provider/runtime, executes an
open-loop follow-up, mutates workflow/checklist state, or grants effect
authority. No reminder worker is active, and the existing workflow/open-loop
scheduler does not consume these tables.

The reminder mutation routes also bypass the legacy process-local
`Idempotency-Key` rejection cache. This is intentional: authoritative exact
replay and changed-input conflict handling belong to the durable owner-scoped
ledger. Preparation is keyed by owner plus its body `idempotencyKey` and
request digest; decisions are keyed by owner, activation request, and canonical
request digest while validating the current chain tip. An HTTP header cannot
replace those durable fields.

To roll back both migrations, remove the `0047` ordering guard first. The
`0046` rollback then refuses to remove either ledger table while it contains
evidence:

```text
backend migrate down pre/0047_workflow_reminder_activation_decision_order
backend migrate down pre/0046_workflow_reminder_activation_ledger
```

The migration-chain contract through `0047`, isolated PostgreSQL `0046`+`0047`
ledger test, and live workflow-repository PostgreSQL test pass. The full Go
suite, 324 Angular tests, production build, and signed-in browser
prepare/approve/persist/cleanup acceptance also pass. These checks validate the
non-executing ledger and its operator flow; they do not prove or activate
Calendar, message, provider, notification, or follow-up effects.

## Proactive Attention Feedback

`pre/0048_proactivity_attention_feedback` adds an append-only, owner-scoped
feedback ledger for advisory attention. Each record binds the latest stored
proactivity decision, signal digest, action, idempotency key, previous chain
digest, and microsecond-ordered timestamp. Database constraints and triggers
reject stale or foreign sources, malformed payloads, authority-bearing rows,
chain forks, and update/delete/truncate. Exact idempotent replays return the
original record even after a later successor exists.

The supported actions are `accept`, exact-signal `dismiss`, bounded `snooze`,
indefinite `suppress`, and `resume`. They alter later attention evaluation only.
Every row is `attention_feedback_only` with `can_execute`,
`delivery_authorized`, and `execution_authorized` fixed false; no worker sends
a notification or executes work from this ledger.

Rollback refuses a non-empty feedback ledger:

```text
backend migrate down pre/0048_proactivity_attention_feedback
```

## Outcome Attention Monitor

`pre/0049_outcome_attention_monitor` adds three owner-scoped PostgreSQL
relations:

- `outcome_monitor_targets`: mutable scheduling projection for one immutable
  owner/workspace/outcome/indicator/source tuple, with monotonic revisions,
  bounded cadence, due time, enable state, lease, and last-run projection;
- `outcome_observation_records`: append-only numeric observations with source,
  request, and record digests plus the fixed advisory authority; and
- `outcome_monitor_runs`: append-only lease-fenced run receipts with redacted
  failure state and idempotency/record digests.

Database checks enforce `advisory_monitor_only` and false delivery/execution
flags on observations. Triggers reject observation/run update, delete, and
truncate; reject target deletion; preserve target owner and scope; require one
revision/time advance per target mutation; fence active leases; and validate
the run referenced by a target's last-run projection. The down migration
refuses to run while any target, observation, or run remains, so operators must
never treat rollback as an evidence-deletion command.

The application repository adds stricter semantics on top: a closed source-kind
enum, owner/workspace isolation, deterministic completion replay, bounded
history, due-scope discovery, and lease-generation fencing. Migration success
alone does not prove scheduler, composer, UI, or real-world correctness.

Required acceptance before release is: ordered apply; isolated PostgreSQL
lifecycle; immutable mutation rejection; exact replay; cross-owner isolation;
disabled-target exclusion; active-lease refusal; expired-lease recovery; clean
rollback of an empty installation; refusal to roll back a non-empty ledger; and
confirmation that no execution or delivery effect is created.

## Durable Advisory Composition Handoff

`pre/0050_outcome_monitor_composition_delivery` separates successful monitor
collection from downstream outcome/proactivity composition. A successful
`0049` run and its optional immutable observation enqueue one owner/workspace-
scoped `outcome_monitor_composition_deliveries` row. Existing successful runs
are backfilled idempotently. Observation/run completion therefore means that
the read-only collection evidence was committed; it does **not** mean that the
advisory composition was accepted downstream.

Deliveries move through `pending`, `succeeded`, or `dead_lettered`. A pending
delivery is claimed with revision and lease-generation fencing, retried with a
bounded policy after a failed attempt, and dead-lettered when its attempt limit
is exhausted. Each success or failure appends an immutable
`outcome_monitor_composition_attempts` receipt containing the delivery/run
binding, attempt number, claim and lease generation, worker, timestamps,
request/record digests, and a redacted failure code where applicable. Terminal
deliveries and all attempt receipts reject mutation; the down migration refuses
to remove a non-empty handoff ledger.

The database enforces `advisory_monitor_only` on both tables. Execution,
delivery, notification, external-effect, and learning-mutation capability flags
remain false. Here, "delivery" names an internal durable handoff record; it is
not authorization to send a message, invoke a provider, mutate a workflow, or
perform any external effect.

Migration `0051` closes the historical-replay ambiguity left by `0050`. Before
the completed monitor run is committed, it pins the exact outcome revision and
audit digest, proactivity policy/signal/decision/feedback watermarks, composer
version, capture time, and canonical snapshot digest. Every composition attempt
binds that digest, and legacy unpinned deliveries are dead-lettered rather than
being recomposed against newer state. Deterministic package tests prove delayed
replay uses the pinned outcome and attention context after current state
advances. Disposable-PostgreSQL lifecycle and signed-browser acceptance remain
release gates; a `succeeded` handoff is still advisory processing evidence, not
authorization for an external effect.

## Immutable Plan Graph

`pre/0052_plan_graph_contract` adds `plan_graph_revisions`, an owner-scoped,
append-only ledger for formal coordination plans. The table stores one JSONB
payload per revision alongside the plan ID, owner, status, revision and parent
revision, canonical digests, idempotency key, authenticated actor, and creation
or acceptance time.

Database checks constrain status, positive revision order, 64-character digest
shape, first-revision parent rules, accepted timestamps, and non-empty owner and
actor fields. Unique indexes protect owner/plan/revision and owner/idempotency
identity. Triggers reject update, delete, and truncate. The application also
recomputes the canonical payload digest and rejects any mismatch between the
JSON payload and duplicated row metadata.

The down migration refuses to remove a non-empty ledger. An operator must not
treat rollback as permission to erase accepted plans or repair history:

```text
backend migrate down pre/0052_plan_graph_contract
```

Applying this migration creates no task, workflow, approval, runtime call, or
external effect. Accepted plan revisions remain coordination evidence only and
retain `canExecute: false` at the API boundary.

## Owner-scoped Memory Query Indexes

`pre/0061_context_memory_owner_query_indexes` enables `pg_trgm` when needed and
adds four indexes for the authenticated memory-library query path: owner/archive
freshness, owner/project/archive freshness, owner/case-insensitive-kind/archive
freshness, and trigram search over the normalized title/content/source
expression.

The migration changes no table shape or row data. Its down migration removes
only these four HAI indexes. It deliberately leaves the `pg_trgm` extension in
place because another schema object or future migration may share it.

## Rules

1. **Additive first.** Add columns, tables, and indexes before code depends on
   them. Delay destructive changes until the new path is proven.
2. **Every up has a down.** `migrate down` refuses a migration without its down
   file.
3. **Reverse order only.** `migrate down` rejects an unapplied target and any
   target with a later applied migration in the same phase.
4. **Backfill safely.** Large backfills run in bounded batches outside the
   request hot path.
5. **Use explicit phases.** Production rollback commands should say `pre/...`
   or `post/...`.
6. **Guard startup.** Startup configuration and `/readyz` expose a schema or
   dependency state the running binary cannot use.
7. **Protect evidence.** Back up owner preferences, selection audit history,
   and Constitution records before a registry rollback.

## Rollback Procedure

1. Stop new writes and capture a database backup.
2. Deploy or prepare the previous compatible binary/tag.
3. Run phase-qualified rollback commands in reverse migration order.
4. Run `backend migrate status` and confirm the intended version is pending.
5. Start the compatible application.
6. Confirm public `/healthz` and `/readyz` probes and run
   `backend reconcile`.
7. Exercise one authenticated bounded operator flow before reopening normal
   work.

## Verification Status

- Versioned runner, `schema_migrations`, pre/post ordering, and
  `migrate status|up|down`: implemented.
- Explicit pre- and post-phase target parsing: implemented and covered by CLI
  tests.
- Migration parsing, ordering, statement splitting, and embedded-file loading:
  covered by focused unit tests.
- Framework Registry migration and rollback behavior: covered in its own
  isolated Postgres database.
- Task-state persistence, owner scope, redaction, provenance, and immutability:
  covered in a separate isolated Postgres database.
- The migration runner applies the complete pre/post set in a third isolated
  Postgres database, so destructive framework tests cannot make task or runner
  evidence pass accidentally.
- A clean-clone migration and destructive rollback rehearsal on Robert's target
  Windows installation remains environment-dependent release evidence and must
  not be inferred from unit tests alone.
