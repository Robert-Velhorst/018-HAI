# Local Temporal Durability Bridge

HAI includes an opt-in local [Temporal](https://github.com/temporalio/temporal)
bridge for exactly one durable workflow: a governed check of due HAI open loops.
It is a bounded scheduling layer, not a second agent framework or a general
execution gateway.

## What it can do

An approval-capable HAI user can schedule a future follow-up check. When it is
due, the Temporal activity calls HAI's existing owner-scoped
`RunDueOpenLoopsForOwner` service. That service may create an HAI checklist item
and a follow-up **proposal**. It cannot send email, post publicly, operate a
browser, call a connector, run a script, modify a calendar, or resolve an HAI
approval.

The activity is idempotency-aware: HAI's existing open-loop claim and
follow-up-artifact checks remain authoritative. The HAI database receives a
durable run record before Temporal is asked to start the workflow. New schedules
use explicit preparation, dispatch, confirmed and uncertain phases rather than
claiming acceptance before the SDK call.

## Scheduling state contract

| State | Meaning | Safe automatic resubmission? |
| --- | --- | --- |
| `preparing` | Intent stored; the authorized dispatch boundary has not been confirmed. It is not an accepted schedule and cannot run through the activity. | No; inspect run and approval history first. |
| `dispatching` | Exact-effect authorization consumed and the durable pre-dispatch fence passed; provider acceptance is not yet recorded. A crash or failed settlement can leave this state. | No; reconcile this same workflow ID. |
| `scheduled` | SDK accepted the start and a conditional database settlement confirmed it. | No duplicate submission. |
| `schedule_uncertain` | SDK call failed after dispatch was attempted; the workflow may still have been accepted. | No; reconcile this same workflow ID. |
| `running` / `completed` | Activity progress recorded in HAI; late schedule replies cannot overwrite these with `scheduled`. | No duplicate submission. |
| `failed` | Pre-dispatch rejection if unstarted; canceled/failed or unconfirmed activity outcome if `startedAt` exists. An already-started activity requires review before a new authorized schedule. | No blind activity retry. |

Scheduling transitions update only status, summary and freshness in one
conditional statement, fenced by run ID, owner, workflow ID/type, scheduled time,
prior state and absent activity start/completion. They never use a whole-row Save
that could replace concurrent worker results. A fresh owner-scoped read checks
identity and returns actual worker progress if the activity ran before the
schedule response settled. Failed cleanup is surfaced, not silently ignored.

The initial Create and activity transitions remain separate from this scheduling
fence. The activity uses its own conditional claim/settlement described below,
not a whole-row Save. This is not a claim of atomic provider-plus-database commit,
an automatically renewable activity lease or exactly-once generation. Older ledger rows are
not automatically rewritten or re-authorized: historic `scheduled` alone does
not prove acceptance under this new contract. Legacy approval/dispatch provenance
and crash-window reconciliation require separate operator acceptance.

## Versioned activity dispatch and recorded results

New workflow executions pass the opaque `FollowUpInput` (run UUID, scheduled
instant and limit) to the actual registered `Run` activity. The activity loads
the owner from HAI storage and calls the owner-scoped proposal service; it does
not infer an owner from a missing argument. Direct and delayed dispatch use the
same input. Cancellation during the scheduled wait prevents activity execution.

This fixes a prior missing-argument defect. The workflow uses the SDK's
`GetVersion` marker `governed-follow-up-activity-input`, version 1, immediately
before scheduling the activity. Histories that already recorded the older,
unversioned no-argument activity keep that command during replay. Their missing
run payload is still rejected by the actual activity: preserving history is
**not** silently repairing or completing an old run. A workflow still waiting
on its timer can record the new version when it first reaches this dispatch.

For a legacy failure, inspect the same Temporal workflow ID and the HAI ledger
before any new schedule. Do not change an old row to `completed`, guess its owner,
rewrite its history, or resubmit without rechecking whether any proposal was
already created and obtaining fresh scheduling authorization. Real exported
server-history replay and operator reconciliation remain release gates; the local
constructed-history replay check is narrower evidence.

A completed activity returns its stored result only when record provenance,
start/completion timestamps and a non-null JSON result are present. JSON `null`,
including whitespace-wrapped null, is not a successful empty result and does
not trigger another proposal-generation call. Valid legacy result objects remain
readable; this is not a new result-schema migration or an independent proof of
historic authorization.

## Activity ownership and cancellation

The activity loads its record with the SDK activity context. Cancellation observed
after that read stops execution before a claim/effect; already-canceled contexts
do not reach the database. An unstarted `dispatching`, `scheduled` or
`schedule_uncertain` run can change to `running` only through one conditional
UPDATE. Its predicates include run ID, owner, workflow ID/type, schedule, previous
status, previous freshness time and absent start/completion. A rejected/unconfirmed
claim does not call the proposal service.

Completion/failure updates only status, start/completion, result, summary and
freshness. Settlement must match the same `running` claim's start/freshness and
identity; it cannot upsert a missing row or overwrite newer ownership/completion.
Claim/settlement timestamps use UTC microsecond precision, matching the database
wire format. No schema migration or expiring/reclaimable lease was added.

- A running or already-started failed run is not automatically executed again.
  An ambiguous claim/completion or proposal outcome produces the fixed,
  non-retryable SDK error `GovernedActivityNeedsReview` where execution may have
  begun. The existing three-attempt SDK policy does not override that decision.
- If cancellation is observed after a confirmed claim but before proposal
  generation, the worker records a failed/canceled-before-effect summary without
  calling the proposal service.
- Once the proposal service has been called, settlement uses a fresh bounded
  three-second context, even if the caller has canceled. Confirmed returned
  results can be recorded and replayed without another call. Failed/unconfirmed
  settlement cannot report success and leaves the prior state for inspection.
- A nil proposal-service result is a failure requiring review, not a panic or
  empty successful completion. Private storage/provider error details are not
  copied into these fixed activity failure messages.

Activities now require `workflow.ContextualFollowUpService` and invoke
`RunDueOpenLoopsForOwnerContext` with the SDK activity context. Missing contextual
adapters are refused before an activity claim and before worker connection/start;
there is no fallback to the legacy context-free method. The owner remains a
required, nonblank identity loaded from HAI, not from Temporal input.

The canonical workflow service creates a detached GORM session using that
context. Due-loop reads, claim updates/refetches and the existing transactional
follow-up projection/release share the session; the shared root repository and
active task-run map are not copied or mutated. Checks between lookup, claim,
projection and each next loop stop new work after cancellation. Advisory graph
projection receives the same execution context. A confirmed durable proposal
does not disappear if cancellation follows its commit.

Cancellation during a claim/transaction acknowledgement is not treated as proof
of rollback. The worker does not guess that releasing or repeating that claim is
safe. An interrupted batch may already contain committed proposals: inspect its
existing loop-specific receipts and artifacts before recovery. The Temporal run
is retained for review rather than automatically re-executed. The HTTP due-loop
endpoint also uses the request context, rejects missing owners, malformed/null
JSON and missing contextual adapters, and cannot report a nil result as success.

This is cooperative cancellation through Go contexts/GORM, not a forced
goroutine kill or a universal bound on every dependency. HAI keeps the call owned
until it returns; SDK timeouts do not kill its goroutine. Native tests exercise
the actual service and GORM query/transaction-entry context transport with
controlled, nonconnecting storage, not PostgreSQL lock interruption or server
rollback. The compatibility `RunDueOpenLoops`/`RunDueOpenLoopsForOwner` entry
points remain available for older compatibility callers. The ticker and
durable workflow scheduler instead use the separate, trusted system-wide
`RunDueOpenLoopsContext` capability, with the same contextual GORM session and
existing follow-up transaction. They share one sweep, recheck cancellation and
background permission between stages, and defer a safety-paused job without
consuming its retry budget. Missing contextual adapters never fall back to the
legacy follow-up method. Workflow task execution now requires its separate
contextual batch capability and passes the owned context to the task adapter,
model HTTP and controlled runtime launch. Several entered task planning,
verification and task-state storage collaborators remain context-free; this
does not establish universal cancellation or actual database acceptance.
Owner-scoped HTTP and Temporal execution stay distinct from
trusted system-wide scheduling. Existing
HTTP shutdown ownership still drains admitted requests rather than canceling
them merely because the group stops.

Internal reminder delivery now has separate contextual capabilities for the
authenticated owner HTTP route and trusted scheduled batch. The canonical
service scopes the existing GORM repository to the caller context, requires
atomic delivery storage, and never uses the test-only memory fallback for this
contextual entry. A missing batch capability is refused before scheduler
registration or any earlier sweep mutation. A nil delivery outcome is not a
successful stage and does not allow the later task stage to run.

The delivery transaction's existing 30-second cooperative timeout is now a
child of its repository context, not a replacement background context. Caller
cancellation, deadline and context values reach the due query, transaction,
locked revalidation and internal signal sink. Checkpoints stop another
authorization or receipt after cancellation. Cancellation is returned as an
error to the existing signal-plus-receipt transaction, not recorded as an
ordinary retry receipt. Confirmed prior deliveries remain in an interrupted
batch's returned summary. Shared storage context and active task state are not
copied or mutated by these scoped reminder views.

The HTTP route rejects missing owners, malformed/null/oversized or trailing
JSON, and batch limits outside 0-100 before invoking delivery. Zero retains the
service default. Missing contextual adapters return 503; storage failure or a
missing outcome uses a fixed failure response without private error details.
The frontend still posts the same JSON request and authenticated owner scope.

Native checks inspect the actual service checkpoints, HTTP route, scheduler,
GORM query transport and transaction entry through controlled offline storage.
The dedicated PostgreSQL acceptance definition also covers cancellation after
the real internal signal write, rollback of signal and receipt, and a fresh
attempt only after confirming empty persisted counts. This definition is not
live acceptance until executed against the guarded dedicated test database.
Lost commit acknowledgements are not proof of rollback; consult the same
authorization, internal signal and receipt before deciding recovery. Legacy
context-free reminder methods remain for compatibility callers; this change
does not claim forced termination, complete participant cancellation, real
PostgreSQL crash acceptance, or external reminder delivery.

Claim recovery now has separate owner HTTP and trusted scheduled batch
contextual capabilities. Canonical storage scopes an independent root GORM
session to the owned context, refuses dry-run/non-PostgreSQL/borrowed-transaction
roots, and owns one read-committed transaction for each recovered claim. The
cooperative 30-second transaction timeout inherits the caller. A narrow service
view does not copy the active-task map or mutate the shared repository context.

Recovery locks the workflow before the loop, matches owner and candidate
claim/revision, rejects archived parents, and reads `clock_timestamp()` after
locking. An ahead host clock cannot release a live database lease. Workflow
state, transition, decision and event are committed together; open-loop state,
decision and event likewise share one transaction. Every evidence write must
acknowledge one row. Errors or missing commit acknowledgements return no
confirmed per-claim outcome. Unknown workflow execution stays blocked for
review with its claim fence intact; recovery does not repeat external effects.
Life-graph projection remains advisory and occurs only after confirmed commit.

The service rechecks cancellation between reads and claims, retains confirmed
partial counts and stops at unconfirmed mutation outcomes. The scheduler
requires contextual recovery before recurring-job admission or earlier work,
does not fall back to legacy recovery, and refuses a nil recovery summary before
follow-ups or tasks. The HTTP route requires an authenticated owner and one
bounded JSON object, rejects null/malformed/trailing/oversized input and limits
outside 0-50, and uses fixed errors without private storage detail.

Offline service/HTTP/scheduler and actual-GORM-SQL transport tests inspect these
boundaries with controlled acknowledgements, not real locks or rollback. The
dedicated PostgreSQL definition checks state/history together after successful
commit, history-write failure, cancellation after an actual event write, a live
lease and an archived parent. CI requires its real top-level PASS marker; a
skip or compile-only run cannot satisfy it. This resource-constrained local run
has not executed that real database test or remote CI. Lost commit acknowledgement
still requires inspection of the same durable records; it is not proof of rollback.
Legacy recovery/agent-cycle compatibility paths do not acquire an owned caller
context automatically. Actual task/runtime cancellation acceptance,
cross-process crash/restore and real operator reconciliation remain open.

### Entered task and reviewed execution

Task-operation reconciliation failures retain the received plan in the service
result instead of replacing it with a nil result. Failed review creation, failed
operation marking, unconfirmed completion and failed final result readback are
not completed/retryable states. Execution and persistence errors remain joined
with the reconciliation-required error identity. This is in-memory evidence
retention at the service boundary, not proof that a failed database write was
committed or that the private receipt is exposed by HTTP.

Owned task-operation admission and replay now use isolated PostgreSQL scopes
with caller cancellation and a 30-second storage deadline. Terminal writes and
readback get a fresh bounded scope. If execution already failed, was cancelled
or lost its lease, reconciliation uses `context.WithoutCancel` plus the same
storage deadline: caller cancellation cannot itself erase received evidence.
Heartbeat storage has an independent owned context while execution is live.
Its stop function cancels an entered cooperative write and joins the heartbeat
worker; it does not declare an unreturned runtime worker terminated.

Owned requests refuse storage adapters without the context capability. Canonical
PostgreSQL scopes refuse nil, dry-run and borrowed-transaction roots, and do not
mutate the shared GORM session. The memory repository has no external transport;
its scope admission is not evidence of cancellable mutex acquisition. Legacy
context-free service callers remain compatible and do not gain a universal
deadline. Other planning/review task-state calls and collaborators still need
owned contexts. Real lock interruption, commit/crash and restore acceptance
remain separate release gates.

The operation wrapper also checks the caller immediately after claim admission,
before invoking the execution function. A cancellation there surfaces the claimed
operation for review without dispatching new work. Empty replay results are not
successful acknowledgements. Replay read errors retain their error identity and
require reconciliation. Cancellation during successful replay/final readback is
returned with received evidence; it does not reverse an acknowledged completion.
The durable same-operation replay fence is preserved. These boundary tests use
controlled memory storage, not accepted PostgreSQL crash/commit behavior.

Owned review resolution likewise uses a scoped repository for review lookup,
prior task-outcome inspection and immutable decision storage. Its admission
scope is released after acknowledgement, before execution begins. Outcome storage
uses a separate `WithoutCancel` scope bounded to 30 seconds, so a caller stop
does not erase its decision or received task result. Failed/missing outcome
acknowledgements return reconciliation-required errors with the retained result;
they do not update the service mirror as if storage succeeded. A cancelled
rejection retains its acknowledged item and does not enter correction learning.
Missing contextual storage is refused before decision/execution and HTTP returns
a fixed 503. Legacy resolution APIs remain available; pursuit/coordination writes,
list/reconciliation endpoints and learning
collaborators do not automatically gain caller-context coverage. Scoped-memory
fixtures verify the service wiring, not PostgreSQL commit/crash acceptance.

Review resolution validates a successful adapter acknowledgement against the
pre-read immutable request digest and the exact requested decision. Item/task,
owner, decision source, nonempty decision ID, positive revision, note and normalized
resolution timestamp must agree before learning, mirror mutation or execution.
A missing/mismatched projection returns binding-mismatch and reconciliation-required
errors. The already stored decision is not reversed; an unknown acknowledgement
does not authorize retry. The public projection does not expose the item revision:
this guard checks positivity, not an independent exact revision readback. Canonical
storage retains its existing model-level revision checks. Forty-two controlled
adapter-fault cases cover approval/rejection; no real database is exercised.

Post-execution outcome acknowledgements are checked in both normal and interrupted
execution paths. Owner/intent, review item, creation time, requested task/status
and normalized reason must agree. Completed projections retain approval provenance
and the requested normalized completion time; needs-review projections must not
claim resolution. Missing/mismatched projections do not overwrite the last
acknowledged approval or erase the received plan/tool receipt. Binding-mismatch
and reconciliation-required errors remain joined with any execution/cancellation
error. Storage is not reversed and uncertain effects gain no retry authority.
Twenty-two memory-adapter fault cases exercise these paths and confirm that the
real stored outcome remains intact; this is not database commit/crash acceptance.

Execution-time task-review item and approved-decision reads use an isolated
caller-bound repository with the same 30-second deadline. Cancellation checkpoints
precede reads, separate them and precede returning execution authority. Nil item
or decision acknowledgements are refused rather than dereferenced. Owned calls
refuse missing contextual storage; legacy context-free callers remain compatible.
Workflow-derived approval validation also checks cancellation, without inventing
task-review storage for workflow decisions. Seven memory-transport cases prove
service wiring and cancellation boundaries, not forced SQL interruption or a
cross-read atomic snapshot. Final runtime authorization fences remain required.

Plan/run task-state completion-log writes and review-item creation/reuse now use
the same isolated caller-bound storage scopes. Built plans are returned on these
storage failures, and acknowledged review items remain attached when cancellation
arrives just after creation. Nil create/read acknowledgements are refused instead
of dereferenced. Ordinary review creation checks cancellation before and after
configuration inspection; the separately bounded terminal evidence path still
uses its existing reconciliation repository. An already entered configuration
inspector now supports the bounded path described below; some pursuit, coordination
and learning collaborators remain context-free. Seven controlled memory cases cover success, storage failures,
missing acknowledgement and post-write cancellation. They do not prove live SQL
lock/commit behavior or full cancellation coverage across all collaborators.

Owned pre-review configuration capture requires the contextual inspection
capability, with no legacy fallback. The task service and automation executor
carry caller values and an at-most-30-second deadline through the canonical
automation service into its configuration repository. The PostgreSQL reader uses
a detached GORM session, refuses nil/dry-run/borrowed-transaction roots and returns
no configuration after cancellation. Snapshot normalization is shared with the
separate legacy read API; neither read issues authority or registers approval.
Terminal review reconciliation uses `WithoutCancel` for this bounded inspection
without restoring the stopped caller as execution authority. Eighteen tests
cover task capture, adapter forwarding, service reads and a controlled SQL pool.
The SQL-pool test proves context propagation/root isolation, not real PostgreSQL
lock behavior. Owned approval registration now requires a contextual recorder,
with no legacy fallback. Configuration lookup, immutable decision storage and
exact decision readback use the bounded detached repository scope. Stored UTC
timestamps are normalized to microsecond precision before exact comparison.
Cancellation or an uncertain/mismatched acknowledgement stops dispatch without
undoing a potentially committed decision. Such outcomes require reconciliation,
not blind retry. Eighteen boundary cases cover the service and task adapter;
they are not real PostgreSQL cancellation or commit acceptance. Owned
action-requirement inspection also uses bounded contextual configuration reads,
refuses missing capabilities and validates the returned target before applying
defaults to a copy. Cancellation or an invalid target stops proof issuance and
dispatch. Owned proof issuance now requires a detached bounded repository scope
and contextual signer; configuration and decision reads check cancellation before
and after entry, and configuration identity must match the requested target.
Signing produces a local envelope rather than writing an issuance record.
Cancellation during signing discards the result before dispatch. The separate
one-use consumption store and configuration changes between inspection and
dispatch still require real storage/race acceptance; these reads are not an
atomic configuration/decision snapshot. Missing contextual capabilities never
fall back to the legacy issuer for owned requests.
Verification and one-use consumption now have a bounded caller lifetime and
check cancellation before entry and after storage returns. A successful write
followed by cancellation retains the spent proof and reports
`ErrApprovalProofConsumptionUnconfirmed`; storage failures retain their cause
and require reconciliation. PostgreSQL consumption shares the detached-root
guard, refuses dry-run/borrowed contexts, and uses zero affected rows only for
duplicate claims. Other unexpected counts are uncertain acknowledgements, not
replay evidence. Local memory consumption checks cancellation around its mutex,
without claiming that mutex waiting itself is interruptible. Controlled pool
and memory tests do not prove real PostgreSQL transaction/crash/restore behavior.
The launcher also guards the proof-adapter handoff itself: a bounded child context,
entry/return cancellation checks and post-consumption action-binding validation
prevent an adapter's success response from being recorded as verified authority
after cancellation or binding mutation. These failures preserve the original
cause and `ErrApprovalProofConsumptionUnconfirmed` without undoing consumption.
Noncooperative adapter entry is not forcibly interruptible; final effect admission
and policy rechecks remain separate dispatch protections.
At launcher admission, an explicitly owned execution request requires bounded
contextual configuration lookup and exact automation identity validation. Missing
capability has no legacy fallback. Cancellation after that read or around the
idempotency lookup stops before intent storage; another checkpoint precedes the
intent write. Legacy launch calls remain separate. Owned admission requires a
bounded contextual repository for entered idempotency lookup, intent storage and
replay outcome reads/projection repair. Missing scope is refused, not replaced
by the root repository. Uncertain intent acknowledgements retain the candidate
identity with indeterminate persistence and prohibit dispatch, rollback and blind
retry. Candidate identity is not durable-write proof. New execution outcome and
projection writes have their own bounded storage scope, retaining trusted values
without inheriting caller cancellation. No owned root-repository fallback occurs.
Unconfirmed outcome acknowledgements preserve the intent reference and received
execution evidence as indeterminate with a reconciliation error. Failed projection
acknowledgements preserve the known stored outcome. These writes never grant new
execution authority; legacy callers retain their separate compatibility behavior.
The launch HTTP parser reads bodies independently of Content-Length, accepts an
empty optional body or one JSON object, and enforces a 64 KiB whole-body bound.
Null/array/malformed/trailing values are refused before service entry; oversized
bodies return 413. Exact-limit objects remain valid. Server-owned authority fields
remain excluded from client binding. Real proxy/chunked-transfer and slow-client
timeout acceptance remain separate from these in-process parser tests.
Launch, runtime-stop and diagnostics handlers independently refuse a missing,
empty, whitespace-only or non-string middleware subject before calling owner-scoped
services. Launch also refuses before body reading. Client authorization/identity
headers do not create that trusted subject. Existing authenticated route and role
filters remain required; this additional guard does not prove real IDP token or
session acceptance and does not grant permissions by itself.
Runtime stop also requires the exact non-null automation configuration matching
the requested nonzero ID before owner/task lookup or audit storage. Defaults are
applied to a copy, not to the borrowed repository record. The existing configuration
validation is reused through its legacy reader; this check does not make entered
stop task lookup, dispatch or stop-audit storage context-aware. Those lifetimes
and actual provider cancellation/acknowledgement remain separate work and gates.
Real PostgreSQL
cancellation/commit behavior remains a separate acceptance gate. Cancellation
after a successful replay outcome read preserves the validated and redacted
historical receipt with the cancellation error, without repairing projections or
dispatching again. Failed reads or mismatched ownership/action binding do not
expose a receipt. Authenticated HTTP error responses expose only a minimal recovery
reference for a matching automation and nonzero attempt ID, never sensitive receipt
fields. Conflicts do not expose references. The response keeps its error status,
requires reconciliation and explicitly denies retry authority. The Command Center
validates those fields before presenting a reference-specific warning rather than
the ordinary retry suggestion. This is not a durable retry lock or proof that the
operator-reconciliation workflow is complete; rendered/live acceptance remains open.
The browser keeps its existing request key for absent/unknown statuses, failures,
nonterminal outcomes and mismatched result identities. Only matching nonzero
outcome UUIDs with exact completed/ready status and no required approval clear
that key. The same check gates the Command Center success notice/timestamp; ready
does not claim that execution started. Existing compare-before-remove behavior
preserves replacement keys when older acknowledgements arrive. Browser key
retention is not a durable authority fence or server-side retry prohibition.

Authenticated chat, task plan/run and workflow execution have a trusted, nonserialized
execution context. It does not alter the reviewed-intent digest or grant
approval. Workflow HTTP and scheduled execution require contextual entry points;
direct runner capability is checked before claiming work. Deferred runners also
refuse legacy-only delegates before dispatch. A due-run
request must be one non-null JSON object bounded to 4 KiB, with limit 0-50 and
no trailing values. Existing direct URLs, ownership and risk rules remain.

The task adapter forwards the caller lifetime through preview and actual run.
Model routing/generation and controlled automation launch inherit that context.
Checkpoints prevent new stages, verification fallback and automatic retries
after observed cancellation. Existing runtime/model receipts and recorded usage
are retained. An entered runner stays registered and leased until its actual
return; cancellation is not proof that an uncooperative worker has terminated.

Review resolution has a separate authenticated contextual capability; HTTP does
not fall back to context-free resolution. An already-cancelled request cannot
record a decision. Once a decision is acknowledged, its immutable provenance
remains; execution uses this new caller context rather than restoring a stored
lifetime. An interrupted approved execution is linked to any returned task
result and moved to review only after outcome acknowledgement. Missing outcome
receipts and storage errors are not swallowed. Execution and storage error
identities survive together; neither confirms completion or a safe automatic retry.

The chat handler likewise supplies the trusted lifetime, and commands forward it
to direct task planning/execution. Cancelled admission cannot start a task;
interruption after a task result retains that result but cannot start a new
maintenance cycle or append a successful command log. Chat errors distinguish
cancellation, deadline expiry and an operation requiring reconciliation without
exposing private diagnostics. Pursuit/agent-cycle APIs remain context-free once
entered; these checkpoints do not claim interruption inside those services.

Native regressions exercise actual task/workflow services, review state in a
controlled memory repository, and entered HTTP cancellation against a local
provider-protocol fixture. They are not real LLM, account, PostgreSQL or runtime
acceptance. Task-state claim/heartbeat/settlement and legacy planning/verification
collaborators are still context-free while entered. Context-free internal
compatibility calls remain available but do not acquire an owned caller lifetime.
Real crash/commit/rollback/interruption and operator acceptance remain open.

Trusted ambient scheduling likewise requires `ScanContext` and passes the
actual ticker/runner context and live permission gate. Scan persistence is
scoped through a detached GORM session; the original atomic admission guard is
not copied. Dry-run, missing-dialect, non-PostgreSQL and borrowed transaction
execution roots are refused. Checkpoints stop additional stages after cancellation or a policy
pause. Follow-ups use the contextual batch service, but memory/pursuit reads and
task execution remain context-free while already entered. Known partial counts
are preserved before handling a returned error.

Creation and outcome receipts must match the application-assigned scan ID,
owner, trigger, state, microsecond-normalized timestamps and known results.
Only database-managed creation/update timestamps are excluded from the receipt
comparison. Requests are snapshotted, including the completion-time pointer,
so an adapter cannot alter the expected receipt in place. A missing receipt,
wrong record or storage error is `outcome_unconfirmed`, never success. An
ambiguous completion write is not followed by a speculative failure overwrite.
Failure-only recording uses a separate two-second cooperative context after
cancellation; a missing settlement repository is refused without panic. Both
the original failure and storage error retain their Go error identities;
rendered scheduler errors and scan previews redact recognized secret patterns.

Terminal persistence uses a conditional running-row update, scoped by ID,
owner and start time, and requires exactly one affected row. It has no upsert
fallback. Retention selects only completed/failed rows; personal retention is
owner-scoped with no shared-history fallback. No actual database cleanup was
performed during verification.

The native HTTP/service and runner/service tests use controlled storage and
principals; GORM transport/DryRun tests are not server concurrency or rollback
proof. Owner HTTP scans and legacy agent-cycle scans remain context-free.
The recurring ambient runner now uses persisted `review_unknown` replay policy.
An unconfirmed result takes precedence over a joined safety pause and enters
nonclaimable `needs_review` state. Cancellation/deadline, panic or heartbeat
failure after invocation also require review. Expired/missing leases with this
policy are quarantined, even on the final attempt and without a registered
recurring schedule; they are not retried or replaced. Only explicit
`at_least_once` policy permits ordinary lease recovery. If the immediate hold
write fails, its failure is returned and the running row remains fenced until
the policy-aware reaper can quarantine it.

Committed holds block scheduling/replacement and already-pending claims for
the same queue/kind. Startup upgrades existing active policy under the singleton
advisory lock before publishing a handler. This does not prove serialization
against a claim already racing a hold, nor reconciliation after an ambiguous
release transaction commit. Operator review/resume and actual PostgreSQL
concurrency/crash acceptance remain release gates. There is no automatic hold
clearance or speculative deletion.

Every job outcome write must acknowledge ownership and successful persistence.
A `false, nil` write is not a successful poll; a failed hold retains both the
original error and the storage error, with recognized secrets redacted in the
rendered message. Refused lifecycle admission releases only a never-started
handler's claim without consuming an attempt. A failed release is still
uncertain; an actual invocation cannot be treated as unstarted shutdown.

Review-sensitive recurrence uses two conditional transactions with the same
queue/kind singleton lock. Preparation writes `settling` with the original
worker/generation still attached and creates a pending successor only if no
other active occurrence exists. `settling` is nonclaimable, blocks other
same-kind pending claims and startup replacement, and is never automatically
reaped. Only an acknowledged preparation permits release. Release matches
the exact preparation identity, attempts, microsecond timestamp, reason and
absent completion, then records terminal status and clears the lease.

A lost preparation acknowledgement leaves the preparation barrier intact if
that commit reached the database; the runner does not guess rollback or start
release. Cancellation between preparation and release likewise leaves the
barrier. A lost release acknowledgement may mean the terminal release already
committed based on confirmed preparation. That remaining uncertainty needs
same-record inspection and authorized reconciliation, not speculative replay.
Ordinary replay-safe recurrences retain their existing single-transaction path.
That path requires the stored occurrence's explicit `at_least_once` policy and
matching queue/kind in its conditional write, not just an ordinary successor
supplied by the caller. A concurrent policy upgrade cannot be bypassed by a
stale ordinary completion call.
Native tests use real GORM/database/sql calls with controlled commit failures,
not a PostgreSQL server, and do not prove transaction isolation or exactly-once
execution. The operator reconciliation/resume protocol remains unfinished.

Migration `pre/0115_durable_job_replay_policy` is source-only in this verification
pass. Stop/drain older workers before applying it: old binaries do not honor
these holds. Rollback locks the job table and refuses to discard retained review
or replay authority. Durable ambient startup failure has no ticker fallback;
explicit legacy ticker mode has a process-local hold only and is not the
supported production path. Native probes exercise actual runner/service and
GORM-generated/transported SQL with controlled acknowledgements. Dedicated
PostgreSQL acceptance definitions have been added but not executed here.

The HAI ledger outcome is not independent proof that Temporal accepted workflow
completion. Real cross-process PostgreSQL crash/retry/rollback and driver/server
cancellation acceptance remain required.

For an interrupted run, reconcile the same workflow ID, HAI ledger and existing
proposal artifacts before any new approved schedule. Do not merely clear
`startedAt`, reset `running`, expire a claim or resubmit to hide uncertainty.

Do not mix older whole-row-save workers and new conditional-claim workers in a
rolling rollout. Drain/stop the old worker population first, inspect outstanding
run/proposal state, then start the updated workers. The new predicate cannot
prevent an older binary from performing its old unconditional write.

## Privacy and isolation

- Temporal services are started only with the `durability` Compose profile.
- No Temporal port is published to the host or the Internet.
- Temporal uses its own local PostgreSQL volume, separate from HAI's data.
- The workflow input contains an HAI run UUID, timestamp, and bounded limit.
  It does not contain the HAI owner identity, source text, emails, attachments,
  credentials, prompts, or tool payloads.
- `HAI_TEMPORAL_ADDRESS` accepts only `temporal`, loopback addresses,
  `localhost`, or `host.docker.internal`.
- The profile pins `temporalio/server` and `temporalio/admin-tools` at `1.31.2`.

## Enable locally

1. Set a strong local-only `HAI_TEMPORAL_POSTGRES_PASSWORD`.
2. Set `HAI_TEMPORAL_ENABLED=true` in your local `.env`.
3. Run `docker compose --profile durability up --build`.
4. HAI makes up to 30 local worker-start attempts, with five-second waits between
   unsuccessful attempts. Connection/startup time also contributes to elapsed
   time; this is not a strict 150-second deadline. Use
   `POST /api/v1/temporal/worker/start` as an HAI admin only
   if it remains unavailable after that bounded startup window.
5. Use `GET /api/v1/temporal/status` to confirm the worker is configured and
   started. Schedule a check through `POST /api/v1/temporal/follow-up-runs`.

All routes are authenticated and owner-scoped. Worker start is admin-only;
scheduling is approval-capable. A disabled or unreachable local service returns
an explicit configuration/unavailable response rather than falling back to a
cloud endpoint.

## Worker startup and shutdown

- Connection and health I/O share a ten-second attempt deadline. The health
  probe has its own three-second maximum inside that remaining budget. An
  earlier request deadline always wins. Host shutdown also cancels that attempt.
- Dial, health, SDK startup and SDK cleanup do not hold the service status mutex.
  Read-only status remains accessible while one startup or cleanup is pending.
  Concurrent start requests reuse that active attempt rather than queueing
  another dial. Once cleanup completes, an explicit retry is possible unless
  host shutdown has permanently sealed the service.
- Missing repository/workflow service wiring is rejected before connecting.
  This checks initialization, not actual database/provider health.
- `workerStarting` indicates an owned startup/cleanup is still pending, not
  readiness. `workerStarted` is published only after SDK startup returns and
  request/host cancellation and deadline checks pass. Known errors remain
  visible while failed SDK cleanup is still pending.
- The admin start handler passes request cancellation through. An overlapping
  start returns HTTP 202 with pending state; a failed or shutting-down attempt
  returns 503. A successfully published worker uses the host's activity lifetime,
  not the short-lived request or connection-probe context.
- Under the HAI API's lifecycle ownership, manual retries remain registered
  through partial-worker Stop and client Close. Shutdown waits for their return
  or reports an incomplete drain. A late SDK success cannot republish a worker
  after shutdown; unaccepted candidates are stopped and their clients closed.

The installed SDK (`go.temporal.io/sdk` v1.41.1) has contextless `Worker.Start`
and worker-shutdown internals. Their complete elapsed time is **not** proven to
fit the ten-second I/O budget. These calls are kept outside the status mutex
and owned through cleanup, not abandoned in detached timeout goroutines. Real
SDK/server startup, failure and shutdown timing remains a production acceptance
gate. A configuration flag or injected healthy response is not live readiness.

Scheduling still requires the unified final-effect authorizer to accept and
consume an approval bound to the exact run, task, owner and scheduling effect.
An approval-capable role alone is not approval. Request fields `taskId`,
`projectKey`, `approvalSourceId` and `approvalBindingDigest` carry the relevant
governed provenance when supplied; an omitted task ID receives a run-bound
synthetic ID, not an approval exemption. The emergency stop is checked again
after authorization, the durable dispatch fence, and before the scheduler call.

## API surface

| Route | Permission | Purpose |
| --- | --- | --- |
| `GET /api/v1/temporal/status` | read | Shows local configuration and worker state without probing or starting work. |
| `GET /api/v1/temporal/follow-up-runs` | read | Lists only the authenticated owner's durable-run ledger. |
| `POST /api/v1/temporal/worker/start` | admin | Starts the one proposal-only local worker; 202 identifies an already-pending attempt, not readiness. |
| `POST /api/v1/temporal/follow-up-runs` | approve | Schedules one bounded due-open-loop check. |

Scheduling-time fields (illustrative only; this is not an approved request):

```json
{
  "runAt": "2026-10-03T09:00:00Z",
  "limit": 10
}
```

Use a timestamp that is valid at the time of submission: `runAt` must be no
more than 365 days ahead. An omitted or zero `limit` defaults to 10; positive
values are capped at 50, and negative values are rejected. The example does
not supply approval provenance and must not be treated as an authorized
dispatch or a live acceptance test. `runAt` is normalized to UTC microsecond
precision before approval binding, database persistence and SDK input, matching
the installed PostgreSQL driver's timestamp wire format.

## Failure and privacy contract

- Invalid request fields return HTTP 400 with a concise, deliberately public
  validation message. Malformed JSON remains a 400 response.
- Disabled configuration returns 409; a missing scheduler/repository or an
  unavailable Temporal scheduling service returns 503; denied authorization
  returns 403; an active emergency stop
  returns 423. These use fixed public messages, not appended provider details.
- Unexpected storage errors and unconfirmed storage replies return HTTP 500
  without exposing database errors, private paths or credential values. Inspect
  run history before retrying: a failed storage call does not prove that no row
  was committed, and repeated schedule requests generate different run IDs.
- An attempted SDK start with an uncertain reply, or an accepted start whose
  database settlement cannot be confirmed, returns HTTP 503 with a fixed
  uncertainty message, `retrySafe: false`, `runId` and `temporalWorkflowId`.
  Those identifiers support inspection of the same effect; they do not grant
  approval or trigger an automatic retry. Provider/storage details are not exposed.
- Post-dispatch settlement uses a fresh three-second bounded context so client
  cancellation does not discard outcome recording. If that storage step fails,
  the durable record may remain `dispatching`; HAI still returns uncertainty.
- Before authorization or dispatch, the returned storage record must confirm
  a nonzero ID and the requested owner, workflow identity/type, schedule and
  unstarted `preparing` state. Missing, mismatched or already-started/completed records are
  rejected. This checks the returned contract; it is not an independent proof
  of PostgreSQL commit durability.
- Status output filters recognized credential formats without changing internal
  configuration. Credential-bearing address, namespace or queue values are
  rejected before worker connection. This is known-pattern protection, not
  universal secret detection.

Native regressions exercise the real HTTP handlers and scheduling service with
injected storage/scheduler failures and the SDK's virtual test environment. SQL
builder tests check the actual GORM conditional statement using an unconnected
dry-run pool, and a pgx binary timestamp-codec round trip checks precision.
Live Temporal/PostgreSQL, crash-after-dispatch reconciliation and clean-machine
operator acceptance remain separate production gates. This bridge does not
claim exactly-once scheduling or authorize a blind retry after uncertainty.
