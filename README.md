# 018-HAI

018-HAI is a local-first Human Autonomous Intelligence Shell: a governed
Personal AI Operating System for turning authorized source material, durable
memory, workflows, approvals, and controlled execution into inspectable work.

The canonical product is this repository's Go, Angular, Postgres, and Docker
Compose stack. It is not an unrestricted desktop agent: planning, execution,
verification, and approval are separate; external effects remain blocked until a
reviewed runtime, policy, and evidence path are configured.

> **Historical repository snapshot, evidence reviewed through 2026-08-04:** this repository
> implements a governed local operating layer, including the Angular dashboard,
> Go engines, IDP, Compose topology, pursuit/workflow routing, persistence, and
> safety gates. On the development workspace used for this review, the Compose
> services were healthy, nginx served `/` and Angular deep links such as
> `/control-center`, gateway health routes responded, and protected APIs rejected
> unsigned sessions. Those observations are local-environment evidence, not a
> claim that every Windows machine or account integration is ready. Backend/IDP
> tests, frontend production build and 447 unit tests, Compose validation, and a
> Postgres-backed critical-path smoke have been exercised. A signed-in browser
> acceptance run on the local Windows Compose stack also completed source intake,
> pursuit creation, exact runtime selection, durable approval, a real read-only
> backend probe, and terminal verification. A clean-clone Windows run and any
> newly configured third-party account, paid model, browser-control, mutable
> runtime, or broad-host-control journey remain release gates.

### Current Checkpoint: 2026-10-01 (Europe/Amsterdam)

The latest [durability and integration ledger](output/production-durability-integration-20261001.md)
records bounded agent contributions, their integration state, and parent-owned
verification in this shared checkout.
Partial local-worker receipts now retain consumed/unknown permission and observed
file-operation stages. Backend and UI completion checks reject contradictory
stage evidence. Cold task-history restrictions survive reload, refresh and
planning; missing URL context no longer erases a selected pursuit's identity.
Caller-editable agent-health declarations cannot stand in for live probe evidence.

The IDP now has additive PostgreSQL device-session authority and encrypted
canonical rotation receipts, in addition to Redis. These source changes require
deliberate migration and reauthentication when deployed; no current private
installation has been migrated by this work. PostgreSQL snapshot recovery still
needs a separate invalidation fence. See [IDP upgrade boundaries](idp/README.md).

Earlier checkpoint frontend results: **1228 passing unit tests**, 10 passing source contracts,
two passing browser-guard/source tests and a successful production build. The
initial bundle is 703.22 kB raw / 115.31 kB estimated transfer. Four SCSS budget
warnings remain, down from five; limits were not increased. Browser source
typechecking is not visual or real-operator acceptance.

The first recovery run reproduced an invalid PostgreSQL sorting query; it has
been corrected. A Docker daemon interruption then aborted a backend suite and
the next recovery fixture. Neither is recorded as a pass. Earlier full-suite
results below are historical proof for earlier source, not proof for the new
session-authority implementation. The current ledger distinguishes completed
checks, failed attempts, reruns and unverified gates.

Earlier checkpoint-wide backend verification passed **3988 top-level tests across 147 packages**
and static analysis, with 208 optional test/subtest skips and zero failures.
Native Windows IDP passes 246 top-level tests and static analysis, with 92
optional skips. All 13 new real PostgreSQL/Redis session-authority tests pass
with race detection, no skips, and identity-verified fixture cleanup. An actual
isolated synthetic dump/restore and extra verification also pass; rows, sequences,
media and safety settings match without modifying the source databases. These
checks do not prove production-model recovery, PostgreSQL-snapshot rollback safety
or real-provider behavior. Dependency caching and bounded Go build parallelism
are improved; known local credential-file patterns are excluded from container
build contexts. This is not a guarantee against every unrecognized secret filename.

Disposable PostgreSQL/Redis session tests are now wired into hosted CI as source,
with all 13 required named results and rejection of skips/failures. That new CI
wiring has not yet executed. On the user's resource-cleanup request, the current
frontend container-image build was cancelled and no new acceptance stack was
started; source, logs, databases and volumes are retained.

After cleanup, **245 top-level native Windows tests across seven backend packages
pass**, without starting containers, plus a fresh **80 passing local CI contracts**.
Three backend checks
were skipped: two Windows symlink privilege cases and one unconfigured PostgreSQL
integration case. New source records safe-worker uncertainty before dispatch,
requires atomic operation/audit updates, checks transitions against stored status,
rejects status changes through ordinary Save, and preserves creation chronology.
The failed/interrupted attempts remain recorded separately; an overall verifier
deadline now bounds host work. The six-package rerun is focused proof for these
later changes, not a fresh all-repository or actual PostgreSQL crash/rollback run.
New action intake now also persists creation and audit together, validates before
mutation, and audits duplicate evidence refresh with claim/version guards intact.
Creation-race duplicates share that same refresh path. Feed failures no longer
claim a successful sync or advance its successful timestamp/cursor; the account
bridges view distinguishes the last attempt from the last successful sync.
These source changes still require actual PostgreSQL crash/rollback and current
rendered-interface acceptance. All source agents are closed; Joyce and ShareT
remain running as requested.

The initial timestamp UI attempt timed out during build and remains recorded as
failed/incomplete evidence. Targeted verification now uses a focused compiler
configuration and the real Account Bridges page module, retaining strict Angular
template/type checks. The earlier native Chrome run passes **30 component tests**;
**11 frontend source contracts** also pass. These cover unavailable versus zero
counts, retained evidence, refresh guards, accessible action names and separate
attempt/success UTC timestamps. The full 77-spec configuration remains unchanged.
This is component/template proof, not deployment, a fresh full frontend suite or
whole-app visual/provider acceptance.

A subsequent account-feed audit fixed malformed envelopes advertising success
and overlapping same-feed syncs overwriting observations. The original suite
passed **20 top-level / 36 test-subtest checks**. Production routing now requires
a PostgreSQL registry rather than process-memory feed configuration. Migration
`pre/0110_account_feed_registry` adds owner/workspace-scoped configuration,
observations, sync claims and immutable audit rows. Configured-file bootstrap is
idempotent and preserves disabled/renamed settings. Mutations and their audit
inserts share a transaction; storage failures are reported as unavailable, not
empty/missing/successful results. The updated native account-feed suite passes
**42 top-level / 81 test-subtest checks**, with no skips or failures. These are
synthetic/source/model-mapping checks, not executed PostgreSQL transaction or
restart acceptance.

The stored sync token is claimed before intake and must match when recording
completion. It never expires automatically; interrupted or unconfirmed runs
remain blocked for operator reconciliation rather than allowing a stale writer
to race new intake. The UI exposes that state, disables affected sync controls,
and cannot call an import successful without its completion-audit confirmation.
The updated native UI run passed **37 component/template tests**, including
unconfirmed outcomes, historical identity review, concurrent bulk-disable
refresh, and unknown/running/interrupted states. No new Docker
stack, private migration, application restart or deployment was used. Real
PostgreSQL race/crash/recovery acceptance and a verified operator recovery flow
remain open. Archived/dismissed operations may still intentionally be re-intaken.

The next source-safety pass uses rooted regular-file handles for both local feed
readers, refuses links/substitution and rejects credentials in new HTTP feed
URLs. Normalization preserves provider/account identity and RFC3339 source
timestamps without inventing missing dates. Structured revision keys include
title/type/time semantics; historical key collisions stop for operator review
rather than rewriting approved work. The bulk claim rechecks the enabled flag
under lock, and contextual intake propagates cancellation through atomic ledger
and audit writes. The final native accountfeed/operations run passed **112 top-level /
287 test-subtest checks**, zero failures and one symbolic-link privilege skip;
the separate Windows junction refusal test passed. This is local/synthetic
proof, not live-provider or real PostgreSQL acceptance. Historical identity
reconciliation, mixed-version writer exclusion and old-revision approval
supersession remain explicit rollout requirements.

Both legacy-reader and normalized-reader inputs now share that structured
identity contract: omitting an item provider cannot downgrade the hash version.
The registered feed supplies missing identity fields; an explicit item account
still takes effect. Public feed/report serialization also redacts recognized
URL credentials and secret-bearing errors without altering persisted source
settings. It is not universal secret detection or automatic credential rotation.

Account Bridges now has a read-only source-identity inspector for configured
local JSON feeds. It observes at most 100 current items, distinguishes canonical,
historical, coexisting and unseen active-operation keys, and displays observation
time, bounded counts and truncation explicitly. The authenticated, owner-scoped
endpoint requires read permission and sends `Cache-Control: no-store`. It does
not contact a provider, start a sync, create operations or write audit records.
Observed feed changes or overlapping syncs invalidate the result; changing the
selected feed or refreshing its status also discards stale UI results. This is
a bounded observation, not an atomic snapshot, complete historical inventory,
automatic migration or permission to execute work.

Background-reader intake now carries cancellation into contextual ledger
lookups and atomic writes rather than falling back to uncancellable storage.
Intake also rejects null, contradictory, inactive, incorrectly scoped or
incorrectly identified successful repository results before returning them.
An invalid write response may still represent an uncertain committed write:
the guard does not claim rollback or authorize an automatic retry. Operation
lists and dashboard recency now have deterministic ID tie-breaks; negative
offsets are clamped and the memory repository respects future review dates.

The current accountfeed and operations package suites pass after these changes.
The focused Account Bridges run passes **66 component/template tests** with
strict Angular AOT/type checks. These are local in-memory, synthetic adapter and
native Chrome component checks, not live accounts, real PostgreSQL transaction
acceptance or a newly deployed dashboard. The ledger retains the failed attempts
and exact verification receipts rather than recycling earlier whole-repo totals.

The next source-identity foundation stores the exact provider/account/external
record tuple and a versioned digest on each new identified operation. Identity
and its identified revision cannot be changed by ordinary, audited or claimed
updates. Canonical intake rejects mismatched repository provenance; older records
without these fields require explicit reconciliation instead of inferred identity
from display URLs. Migration `pre/0111_operation_source_identity` is additive;
its rollback refuses removal once any structured identity is present. It has not
been applied to an existing installation. Native Windows suites pass for
operations, accountfeed, background, migrations, executionbroker, phase2,
opscontrol and runtimelab. The new real PostgreSQL upgrade/rollback acceptance
test compiles but is opt-in and was not executed. Compact receipts do not provide
individual test or skip counts. See the ledger for exact evidence.

Managed safe effects now enter `WithClaimedSafeEffect`, which holds authoritative
operation and live-claim locks across the synchronous broker callback. It checks
the exact operation/version, owner/workspace, claim owner/generation/lease, stored
safe policy and immutable source identity/revision. The bridge derives a private,
active server scope and includes it in the final-effect digest (managed contract
2); copied or retained scope cannot extend the original authority deadline or
survive callback return. The worker checks active scope before authorization and
immediately before its first potentially mutating filesystem call. Reserved
managed artifacts/markers cannot use an unscoped legacy receipt; ordinary ad-hoc
effects retain their separate contract 1.

Final-boundary Constitution lookup now carries context through policy evaluation
and storage, without a context-free repository fallback. Managed artifact names
use the full operation UUID. All seven Runtime Lab HTTP read/mutation handlers
require the exact configured owner's authenticated subject, in addition to router
permissions: anonymous/malformed subjects receive 401, foreign subjects 403 and
unavailable owner configuration 503 before service access. Direct in-process
service/adapter calls are not covered by that HTTP gate.

The retained `production-low-resource-claimed-effect-core-final-20261001` receipt
reports **all seven package suites successful**, exit 0 and no timeout. The
`production-low-resource-claimed-effect-consumers-owner-fixed-20261001` receipt
reports **all five consumer package suites successful**, exit 0 and no timeout.
These parent-owned offline native checks used one worker and a 384 MiB soft heap;
earlier failures remain recorded. Compact receipts do not enumerate individual
test/skip totals. The later `production-low-resource-canceled-authority-core-reviewed-20261001`
receipt passes all ten affected package suites, exit 0 with no timeout. It covers
cancellable authorization memory-lock waits, no receipt/approval consumption on
refused contexts, early service/final-proof refusal of absent or canceled contexts,
and explicit deadline checks even when cancellation timer delivery is delayed.
Earlier receipts do not verify these later changes. Poincare and Hubble were
reused; the integration total remains 29, with no new unique or nested agent.
Both reused source-observation/documentation assignments are closed after
parent integration and review.
The selected `AccountFeed`/`LegacyControlPlane` in-process router integration
tests also pass in `production-low-resource-claimed-effect-router-reviewed-20261001`,
including configured-owner/role checks across all seven Runtime Lab endpoints.
This is not a browser journey, live database or externally deployed API test.

**Open execution-safety gate (gate 10):** accepted observation-backed intake now
publishes a scoped source head. A newer accepted revision blocks the older
operation at claim preflight and the final effect; an older observation cannot
refresh newer evidence. SQL publication and identified final effects use
origin/head/operation/claim locking. Canonical feed registration and configuration
patches now publish versioned origin authority in the same transaction as the
feed/audit write. Changes invalidate prior heads immediately; disabled feeds
refuse both manual and bulk sync. Cached or unversioned managed feed observations
cannot remint authority, including after configuration A-B-A. Semantic revision
A-B-A recurrence enters reconciliation instead of silently reviving old work.
These are local prerequisites, not closure of the production gate. Draft 0114
now fences first-time raw SQL enrollment with a scoped enrollment-only no-op
feed write, followed by a separate current-origin check. This supplies an MVCC
row change without advancing the logical configuration version; lock-only
validation is insufficient for an older repeatable-read writer. Actual
PostgreSQL acceptance is unexecuted, including the raw-writer race. SQL
constraints do not independently recompute the private configuration digest. Tombstone
publication, a review/reconciliation flow and a durable exactly-once effect
journal remain missing. Source-less legacy execution is not yet restricted to
explicitly approved classes. Mode/block-rule changes are serialized for the shared
in-process managed path, not across processes. Managed effects reserve separate
lock and authorization connections before taking row locks; required policy and
evidence readers use the private authorization handle. Admission is cancellable,
but unrelated pool users can still delay it. Actual PostgreSQL concurrency and
crash acceptance remain open. Deadlines are cooperative, not hard syscall cancellation; cancellation or a
post-effect database rollback cannot establish that no file effect happened.
No exactly-once or crash-proof guarantee is made. Actual PostgreSQL lock/crash/
migration acceptance for this prerequisite, real-provider acceptance and signed
clean-machine Windows deployment gates remain unexecuted. Neither identity
inspection nor a timestamp-only preflight closes this gate. See the ledger for
the current disposition of the unchanged historical source-only audit.

**Pre-read observation prerequisite:** generic feed registry sync and the
background reader allocate a tenant-scoped monotonic observation before reading
the file/HTTP feed. The ticket binds the feed UUID and exact configuration digest;
payloads cannot choose the observation UUID or generation. New identified
operations persist that immutable provenance, and managed final-effect digests
include it. Retained/detached callback contexts cannot extend the server's intake
authority. Historical/unsequenced records and duplicate provenance are not
backfilled or promoted. Migration `pre/0112_operation_source_observation` is
additive and refuses rollback once observation/clock history or observed operation
provenance exists. Existing operations retain NULL/zero observation fields; no
URI guessing or automatic upgrade is performed. SQL guards reject zero observation
and origin UUIDs and freeze observed operations' feed binding as well as observation
ID/generation. The current five-field SQL foreign key also binds the exact
observation origin to a nonnull, nonzero account-feed ID on identified observed
operations; legacy NULL/zero provenance remains unchanged. These checks do not
recompute configuration digests or establish
current accepted source authority.

**Accepted-source head increment:** additive migration
`pre/0113_operation_source_heads` defines scoped origins, epoch-bound heads and
immutable accepted-revision history. Historical observations remain epoch zero;
no automatic backfill or inferred execution authority is granted. Empty-only
rollback refuses retained authority/history. Operation creation and head
publication are separate transactions; a partially persisted, unaccepted
identified operation is ineligible for execution. The reviewed native/offline
`production-low-resource-source-head-lifecycle-reviewed-final-20261001` receipt
passes all eleven selected package suites. Dedicated PostgreSQL acceptance test
source compiles but was not executed against a database. See the
[current source-head evidence](output/production-durability-integration-20261001.md#accepted-source-head-integration).

**Canonical registry configuration increment:** additive migration
`pre/0114_operation_source_configuration` adds positive feed versions and managed
origin version/enablement. Register, patch and sync claims use the stored canonical
configuration; production scheduled and owner-bound workers share that registry
instead of cached startup readers. A registry storage failure stops the pass
before existing work is processed. Internal configuration hashes include exact
private URL details, while public JSON remains redacted. Cancellation cleanup can
settle only the exact owned sync token, under a fresh bounded context; it cannot
continue reads or clear another attempt. Managed origins cannot be downgraded or
deleted, and rollback refuses retained managed authority. No private migration
or application restart was performed. The current offline native receipt,
`production-low-resource-registry-enrollment-final-integrated-20261001`, passes eleven
backend package suites; it is not actual PostgreSQL, live-provider or deployment
acceptance. Enrollment concurrency, deferred COMMIT and immediate-origin-DML
test definitions are wired to CI with exact PASS requirements, not accepted
locally as executed SQL. The shared migration fixture now requires the exact
dedicated loopback database and destructive-test opt-in before connecting;
cleanup targets a newly created, OID-checked database without terminating sessions.
See the [configuration integration evidence](output/production-durability-integration-20261001.md#canonical-registry-configuration-integration)
and [enrollment serialization follow-up](output/production-durability-integration-20261001.md#enrollment-serialization-and-test-database-safety).

**Execution-context continuity:** claim lifecycle calls, background-pass entry,
authorization issuance/consumption and the local file worker now reject missing,
canceled, expired or ended private execution authority before proceeding.
Detached contexts retain the original authority's cancellation and deadline;
nested claimed-effect entry is refused. The reviewed native/offline run
`production-low-resource-execution-authority-lifecycle-final-reviewed-20261001`
passed eleven package suites. This is local regression proof, not real
PostgreSQL/provider acceptance or closure of gate 10. See the
[authority-continuity evidence](output/production-durability-integration-20261001.md#execution-authority-continuity).

**Live final-effect controls:** scheduled background work, manual operation runs
and Runtime Lab self-tests now supply a live policy at the locked effect boundary.
Missing or ambiguous policy fails closed. The bounded local file effect shares
an in-process fence with mode, stop and block-rule writers, so a restriction is
ordered before or after admitted work rather than bypassed by an earlier policy
read. Stale mode changes use compare-and-swap; failed mode writes pause execution
instead of retaining cached permission. Policy content is checked against the
locked operation snapshot. The receipt
`production-low-resource-final-mode-policy-snapshot-reviewed-20261001` passes
eleven focused backend suites. This is local Windows regression proof, not
cross-process control, real PostgreSQL concurrency, syscall preemption, or release
acceptance. See [final-effect control evidence](output/production-durability-integration-20261001.md#live-final-effect-control-serialization).

**Backend database resource bounds:** backend pools default to 8 open and 2 idle
connections, a 2-minute idle lifetime and a 30-minute total lifetime. Configure
`DB_MAX_OPEN_CONNS`, `DB_MAX_IDLE_CONNS`, `DB_CONN_MAX_IDLE_TIME`,
`DB_CONN_MAX_LIFETIME`, `DB_CONNECT_TIMEOUT` (default `5s`) and `DB_OPEN_TIMEOUT`
(default `10s`). Durations require units. Invalid/unlimited settings fail before
opening; open capacity must be at least 2 and idle capacity cannot exceed it.
The canonical Compose API and migration job forward these settings.
Shared API initialization additionally uses `DB_STARTUP_TIMEOUT` (default `5m`),
covering acquisition and optional startup migrations. The API stop signal and
earlier caller deadline take precedence. A canceled waiting caller does not
cancel another caller's initialization; an expired initializer cannot publish
its candidate. Migration SQL uses a contextual clone, not the runtime pool root.
Normal Compose still runs migrations separately with `HAI_COMMAND_TIMEOUT`.
Initial ping respects the earlier caller/open deadline; every new physical
connection has a whole-acquisition deadline covering DNS and host fallback.
Initialization and migration failures close the uncached owned pool. Credentials
are encoded as separate fields rather than interpolated into connection options.
These limits are per backend pool, not per host or installation. They do not cap
the separate IDP, cancel checked-out queries/transactions, or establish measured
savings. Managed effects separately reserve a connection pair before locks;
authorization remains independently committed and does not join the effect
transaction. Private deadlines also cancel previously issued bound query contexts.
The existing local plaintext PostgreSQL
policy is unchanged; remote TLS/certificate acceptance remains open. See
[pool evidence and limits](output/production-durability-integration-20261001.md#backend-postgresql-resource-bounds).
See [reserved authorization admission](output/production-durability-integration-20261001.md#reserved-authorization-connection-admission)
for local proof and the still-open live PostgreSQL gates.

**IDP database bounds:** the separate login service now encodes PostgreSQL
credential fields, caps each owned pool at 8 open and 2 idle connections, and
uses 2-minute idle / 30-minute connection lifetimes. Initial ping has a 10-second
deadline; each physical connection acquisition has a 5-second deadline, with
earlier caller cancellation taking precedence. Failed opening, migration or
account initialization closes its owned pool. These IDP limits are currently
fixed and do not inherit the backend's `DB_*` tuning variables. The separate
`IDP_DB_STARTUP_TIMEOUT` defaults to `5m` and covers connection, migration and
account initialization through a shared context; earlier caller cancellation
takes precedence. Invalid budgets fail before opening. These cooperative
deadlines do not force-stop non-cooperative code, undo committed writes, or
establish TLS, crash-recovery or measured savings.
Synthetic driver regressions and native Windows compilation passed; real
PostgreSQL/installed-product acceptance remains open. See
[IDP pool evidence](output/production-durability-integration-20261001.md#idp-initial-pool-acquisition-and-resource-bounds).
The route startup path now initializes one shared database for authentication
and user-account services, instead of opening/migrating two pools. It closes
that owned pool when route setup or the HTTP serve call returns, preserving
cleanup errors. Route-owned services and rate-limit clients close before that
pool. The process entry point now forwards interrupt/SIGTERM cancellation into
startup and HTTP serving. Shutdown allows 90 seconds for active HTTP requests;
the local Compose IDP service allows 120 seconds before forced termination.
Failed draining is reported and skips client/pool closure because closing
sockets alone does not prove handler completion. Real signal/restart acceptance
and bounded Kafka cleanup remain release gates; no measured RAM savings claimed.
The IDP entry point reports configuration or startup/shutdown failures with a
fixed error category and exit status 1, without printing raw driver errors or
panic payloads. Successful shutdown and bare expected cancellation return 0;
joined cancellation/cleanup failures remain errors. This protects process-level
diagnostics, not every upstream logger or crash outside this controlled path.
The owned IDP GORM pool also disables raw SQL traces: an error trace could
otherwise include account values or password-reset tokens. SQL failures still
return to callers. Synthetic-driver regression reproduced this leak before the
fix and passed afterward; other application/Kafka logs remain separate audit
surfaces. Do not enable raw SQL debug logging on identity data in production.
HTTP panic recovery now uses Gin's recovery with no raw output writer, avoiding
request-header dumps, panic payloads and source-file stack reads. Ordinary panics
return a fixed 500 JSON error and a fixed diagnostic. Broken connections retain
Gin's abort behavior; accesslogs suppress raw error details on every route, not
only password-reset routes. Debug/release and broken-connection tests passed;
unrelated application logs and fatal process crashes are outside this boundary.
The optional Kafka logger now synchronizes send/close, waits for in-flight sends
before producer closure and closes at most once. Repeated closes preserve the
original close error; messages after closure are not sent. Transport failure
diagnostics omit raw broker errors. Controlled producer tests passed, but real
broker drain timing, race-detector coverage and log-message privacy remain
separate acceptance checks.
IDP producers use 16 channel-buffer slots and a 128 KiB message ceiling, with
3-second dial, 10-second read/write/metadata and 5-second acknowledgement budgets.
Metadata and producer retries are limited to one with 100 ms backoff; WaitForAll
and idempotent production remain enabled. These are per-step limits, not a hard
end-to-end delivery/close deadline. A factory returning both a producer and an
error now closes the partial producer and preserves cleanup failures. Actual
broker throughput and memory savings have not been measured.
Authentication configuration rejects durations whose unit conversion would
overflow Go's time.Duration: login blocking/attempt spacing, reset-token hours,
access-token minutes and refresh-token days. Boundary tests preserve existing
units and reject out-of-range values without echoing input. This is numeric
safety, not a recommendation to use the maximum representable token lifetime.
IDP booleans are also validated strictly: `HAI_EVENT_BUS_ENABLED`,
`LOCAL_LOGIN_BYPASS_ENABLED` and `SMTP_REQUIRE_STARTTLS` retain their defaults
when absent, but explicitly empty or invalid values stop configuration rather
than silently falling back. Use `true`/`false`; standard Go boolean spellings
and surrounding whitespace are accepted. Errors name the setting, not its value.

**IDP HTTP bounds:** the login service starts through an explicit HTTP server
with a 5-second header read, 15-second request read, 90-second response write,
60-second idle connection timeout and 32 KiB maximum headers. Existing JSON body
limits remain separate. Socket deadlines do not cancel arbitrary handler work
or establish handler cancellation. Native router/lifecycle tests and compilation passed;
slow-client network and installed-product acceptance remain open. See
[IDP HTTP evidence](output/production-durability-integration-20261001.md#idp-http-connection-bounds-october-2).

**IDP Redis bounds:** token revocation and password-reset rate limiting now use
the same client factory, with separate pools of at most 4 connections each and
no eager idle connections. Pool acquisition, dial, read and write budgets are
750 ms; idle connections expire after 5 minutes and connection age is capped at
30 minutes. Automatic command retries are disabled to avoid silently replaying
security mutations after a lost response. Missing addresses do not implicitly
connect to localhost and remain fail-closed. Offline configuration and limiter
tests passed. Caller-owned cleanup is wired into route startup/serve return;
actual Redis load, latency and signal-driven lifecycle acceptance remain unverified.

**Managed SQL authority:** scoped connection and transaction handles now check
authority at SQL entry and commit, not just when a repository acquires its handle.
Replacing/detaching query contexts does not grant access. Reserved prepared
statements are refused; isolated Gorm caches prevent borrowing ordinary cached
statements. Scoped handles do not expose the unrestricted pool. Agent-rights,
mandate decisions and advisory life-graph projection share reserved authorization
capacity; inside an authorization transaction they reuse that same transaction,
never the effect-lock transaction. Ordinary non-scoped repositories keep their
existing behavior. These controls are not a sandbox for arbitrary Go code already
holding unrestricted database access. Custom Gorm plugins are not accepted in
managed reservations. See [scope evidence and limits](output/production-durability-integration-20261001.md#managed-sql-authority-and-collaborator-affinity).

**API shutdown and resource ownership:** the API waits for ordinary HTTP request
draining (20 seconds), then joins its registered requests, schedulers and detached
children (a separate 20-second allowance). New owned work is refused after shutdown
starts. Only after the registered work returns does cleanup close the shared
backend database pool and Redis rate-limiter client. An unfinished drain returns
an error and skips those closers rather than closing resources underneath active
work. The backend Compose stop grace is 60 seconds; it is not proof that an
uncooperative driver or context-free callback will finish within that time.
Source deadlines remain time-outs, runtime-stop refusal is cancellation, and an
individual source time-out remains a reportable operational failure. Startup
pool initialization is single-flight; terminal shutdown refuses late publication
or reopening. These are locally tested controls, not live deployment acceptance,
IDP-wide shutdown coverage, or a guarantee for arbitrary plugins, hijacked sockets
or external processes. See [runtime evidence and remaining limits](output/production-durability-integration-20261001.md#registered-runtime-drain-and-terminal-pool-shutdown)
and the earlier [HTTP drain evidence](output/production-durability-integration-20261001.md#http-shutdown-ownership).

**Frontend resource and release controls:** native `npm run build` and
`npm run watch` default to one Angular build worker. Set
`HAI_FRONTEND_BUILD_WORKERS` or `NG_BUILD_MAX_WORKERS` to `1`, `2`, `4` or `8`;
conflicting or invalid settings fail before compiler launch. The existing Docker
build defaults to two and uses the same validated native entry point. These
worker limits are not a RAM ceiling or measured 90% resource reduction.
Both production and watch builds validate the declared Node engine before
launching Angular, including a production prebuild check. Unsupported Node
versions are rejected rather than silently used to produce release output.
Frontend Nginx reserves year-long immutable caching for content-hashed root
bundles; unversioned images, fonts and favicon revalidate. Missing files do not
receive immutable headers. Lightweight regression checks do not replace an
actual current-source production build or browser/Nginx release acceptance.
See [startup and frontend release evidence](output/production-durability-integration-20261001.md#cancellable-startup-and-frontend-release-controls).

**Truth review access and navigation:** module selection uses Angular URL
segments, so matrix parameters retain the correct module's Basic/Advanced
preferences. Correction controls lose approval authority during refresh and
after an authorization failure, preserve the draft, and require a fresh access
check and renewed confirmation. Superseded refresh/detail requests are cancelled.
This is UI fail-closed behavior, not a substitute for backend authorization or
live-account acceptance. See [local regression evidence and limits](output/production-durability-integration-20261001.md#control-room-routing-and-truth-review-access).
The shared shell also owns delayed deep-link focus: it waits up to 30 seconds
for the actual visible disclosure, then stops observing after completion, route
change, reset, destruction or trusted user input. Sections no longer independently
scroll after that cancellation. This behavior has local class regressions;
the new rendered keyboard tests still require the isolated acceptance stack.
Pending restoration also retries when the tab becomes visible again or a CSS
class/inline style reveals the target. These retries retain the same deadline
and cancellation ownership; they do not restart after the user takes focus.

For a low-resource source check, run `npm run check:control-room` from
`frontend`. It sequentially checks the shared shell/disclosure modules, Truth
review module and Framework Registry service with the actual Angular template
compiler, then typechecks their four selected test files. Both processes keep
the inherited strict compiler settings, emit no files and use a 384 MiB
JavaScript heap limit. That limit is not
a total process-memory ceiling. CI runs these checks before the existing full
production build and headless test suite; they do not replace either gate or
prove rendered keyboard, accessibility, provider or deployment behavior.

The shared backend secret filter now recognizes cookie/Set-Cookie data and
fine-grained GitHub tokens, and discards incomplete private-key blocks instead
of exposing their body. Framework Registry inbound records also remove entire
cookie header lines and complete/incomplete private-key blocks before inspection.
These are tested credential-pattern protections, not a universal guarantee that
arbitrary personal data or every possible secret format will be detected.

Shared `apierror` envelopes return a separately owned, filtered copy of the
message and field details, preserving error codes, HTTP status and ordinary
validation context. The audit-event builder likewise copies and filters both
new and inherited details so updating one event does not rewrite earlier
entries. The shared filter covers recognized credentials in JSON field names
and private-key/passphrase/credential/encryption-key assignments as well as
values. This does not assert that every response or audit path uses those
helpers; the audit-event builder currently has no production call sites. Native
HTTP regressions use synthetic input, not live-provider acceptance.

The opt-in [local Temporal follow-up planner](docs/temporal-durability.md) now
rejects unconfirmed or mismatched storage records before authorization and
dispatch, distinguishes storage failures from invalid user input, and filters
credential-bearing status output. New schedules separate preparation, dispatch,
confirmed acceptance and uncertainty. Conditional state updates preserve worker
progress; ambiguous dispatch/settlement returns inspectable run IDs and
`retrySafe: false`, not an automatic retry. Valid request messages and approval/stop
boundaries remain intact. This is locally tested boundary behavior, not live
Temporal/PostgreSQL durability or exactly-once scheduling acceptance.

Planner startup now keeps status readable during connection/start/cleanup,
coalesces overlapping retries, refuses missing activity dependencies and owns
manual retries through shutdown. Connection/health I/O is deadline-bound;
`workerStarting` and HTTP 202 mean pending, not healthy. Contextless SDK
startup/shutdown timing still needs real-server acceptance, as documented in the
same planner guide. This change does not start any local service by itself.

New planner workflows now forward their run ID, schedule and limit to the actual
registered activity. Earlier argument-ignoring tests missed that broken link;
new SDK boundary tests cover direct/delayed dispatch and the complete local
scheduling-to-recorded-result chain. Versioned dispatch preserves the old
no-argument command on legacy replay, without guessing owners or silently
repairing old runs. A completed activity rejects a stored JSON `null` result.
Constructed-history replay and controlled adapter tests are not real-server
history or live-account acceptance; legacy reconciliation remains explicit.

Planner activities now use context-aware reads and conditional claim/settlement
updates instead of whole-row saves. An already-started or ambiguous run requires
review rather than blind SDK retry; a late completion cannot recreate a missing
row or replace newer ownership/results. Cancellation stops new work at the
read/claim boundaries, while outcome recording remains bounded after an attempted
effect. Activities and the owner-scoped follow-up HTTP endpoint now require the
contextual proposal-service capability. The same cancellation context reaches
due-loop lookup, claims, transactional projection/release and advisory graph
updates through a detached GORM session. Cancellation stops additional work
without discarding confirmed proposals or guessing that an uncertain claim can
be released/retried. Missing adapters are refused before worker startup or an
activity claim; malformed/null requests and missing owners do not execute.
The ticker and durable workflow scheduler now share one sweep and pass their
execution context into the trusted system-wide follow-up batch capability.
They refuse a missing contextual adapter, recheck cancellation and the background
safety gate between stages, and defer paused durable work without consuming its
retry budget. Terminal cancellation/capability failures stop later stages;
ordinary errors remain aggregated and recognized credentials are redacted from
the sweep error saved by the durable runner. Authenticated HTTP/Temporal calls
remain owner-scoped and do not use this system-wide capability.
Internal reminder delivery now uses separate owner-scoped HTTP and trusted
scheduled contextual capabilities. Due reads and the existing atomic internal
signal-plus-receipt transaction inherit the owned request/scheduler context;
the 30-second transaction timeout no longer replaces it with a background
context. Cancellation checkpoints stop additional deliveries and receipts,
while confirmed earlier delivery counts remain available in partial summaries.
Missing contextual/atomic adapters are refused without a memory-only fallback;
the scheduler refuses unsupported reminders before admitting a sweep or
starting earlier recovery work. Malformed/null/oversized or trailing HTTP JSON
and limits outside 0-100 do not invoke delivery. Private storage details are
not exposed, and a nil result cannot be reported as success. Zero retains the
existing service default. This does not grant external sending authority or
replace the separate exact owner approval and source revalidation requirements.
Native service/HTTP/scheduler/query/transaction-entry checks are offline proof;
the cancellation-after-signal-write PostgreSQL acceptance definition is not
accepted until actually run against its guarded dedicated database.
Claim recovery now also has separate authenticated owner and trusted scheduled
contextual capabilities. HTTP rejects missing owners, invalid/null/oversized or
trailing JSON and limits outside 0-50 before service admission. Unsupported
adapters return 503, and unconfirmed outcomes cannot be reported as success or
allow a scheduled sweep to continue into later work. Canonical PostgreSQL
recovery locks the workflow before the open loop, revalidates claim/revision
and archived state, and checks lease expiry against the database wall clock
rather than trusting the PC clock. State and required transition/decision/event
history share one owned transaction. A failed history write or missing commit
acknowledgement does not count as confirmed recovery. Workflow execution claims
remain fenced for review; only the existing idempotent internal follow-up claim
is reopened. Confirmed earlier batch results survive cancellation. This grants
no approval, retry authority for unknown external effects, or external sending.
Native SQL-transport checks use controlled commit acknowledgements, not a real
PostgreSQL server. The guarded state/history/rollback/cancellation acceptance
definition is wired into CI with a required actual PASS marker; it has not been
executed or accepted by this resource-constrained local verification run.
This is cooperative cancellation, not forced termination. Authenticated chat,
task plan/run requests and workflow execution now pass the owned context through the
task adapter into entered model HTTP calls and the controlled automation launch.
The workflow scheduler refuses missing contextual execution capabilities, keeps
an entered runner fenced until it actually returns, and retains confirmed
partial results. A cancelled chat does not start another maintenance cycle,
retains any returned task result and does not log successful command completion.
Cancellation stops new stages, validation fallback and automatic
retry; received runtime receipts and model usage remain available for review.
Reviewed execution likewise uses the current HTTP context, not a serialized
historical lifetime. Cancellation before a decision cannot create approval;
cancellation after an acknowledged approval preserves its immutable decision
and requires outcome inspection rather than claiming completion. Failed outcome
recording remains an error, not a confirmed review update.
Task-operation reconciliation also retains received service results when marking
or review storage fails. Unconfirmed completion/readback cannot advertise
completion or automatic retry; execution and persistence error identities remain
available together. This does not prove that an unsuccessful write committed.
Workflow execution HTTP rejects null, trailing and oversized JSON before admission. These paths
do not grant approval or bypass existing risk/provenance rules.
Owned task operations now scope PostgreSQL claim, replay, terminal writes and
readback to detached contexts with a 30-second storage deadline. Interrupted
execution uses separately bounded reconciliation; its receipt is not erased by
the caller's cancellation. Heartbeat writes have their own bounded lifetime,
and stopping the heartbeat cancels an entered cooperative write before joining
its worker. Owned requests refuse missing storage-context capability; dry-run
and borrowed-transaction roots are refused. This is not a forced SQL shutdown.
The operation wrapper rechecks cancellation after acquiring a claim and before
dispatch. Empty replay readback is refused instead of acknowledged as success;
replay storage errors retain their identity. Cancellation during successful
readback is returned with the received result, without reversing an already
acknowledged completion or permitting the same operation's effects to repeat.
Owned review resolution now scopes item/prior-plan reads and decision storage
to the current caller with the same deadline. Post-execution outcome storage gets
an independent bounded context; failures retain acknowledged decisions and
received execution results. Cancelled rejection does not start correction learning.
Missing storage capability is reported as unavailable before creating authority.
Review decision acknowledgements are bound to the requested owner, immutable
intent digest, item/task, decision provenance, note and resolution timestamp
before mirror updates, correction learning or execution. Missing or mismatched
acknowledgements require reconciliation; they do not rewrite the stored decision
or grant automatic retry. Local adapter-fault tests cover approval and rejection;
they are not evidence of live PostgreSQL crash/commit acceptance.
Post-execution review acknowledgements are also matched to the owner, reviewed
intent, requested task/status/reason and completion provenance before presentation
state is updated. Missing/mismatched responses preserve the earlier acknowledged
approval and received execution receipt with reconciliation-required errors.
The actual stored outcome is not reversed and no automatic retry is authorized.
Execution-time task-review approval reads now use the same isolated, caller-bound
storage scope and deadline. Cancellation is checked before and between reads
and before returning authority; missing item/decision acknowledgements are
refused without crashing. Owned calls reject legacy storage without contextual
support. These reads do not themselves constitute an atomic database snapshot
or replace the final runtime authorization fence.
Planning/run completion-log writes and review-item creation/reuse now use scoped
task-state storage as well. Built plans survive log/review persistence failures;
received review items survive caller cancellation after their storage acknowledgement.
Missing review-create acknowledgements fail closed without a panic. Ordinary
planning checks cancellation before storing a review; terminal reconciliation
retains its separately bounded, noncancelled evidence path.
Owned pre-review configuration inspection now carries its bounded context through
the task service, automation adapter and detached PostgreSQL configuration read.
It refuses missing contextual capabilities, borrowed/dry-run database roots and
post-read cancellation rather than using legacy or stale evidence. Legacy
context-free inspection remains separate. This inspection does not grant approval
or prove that the later configuration stayed unchanged until dispatch.
Owned task-review approval registration now requires a contextual recorder and
a detached, bounded repository scope. Cancellation, missing capabilities and
uncertain storage acknowledgements stop the chain before requirement inspection,
proof issuance or launch. The saved decision must be read back exactly, including
its UTC microsecond timestamp; uncertainty never rolls back an immutable decision
or implies that retrying is safe. Legacy registration remains separately available.
Owned action-requirement inspection now uses the contextual configuration reader
and checks cancellation before and after the read. Missing capabilities and
missing/wrong configuration targets stop proof issuance and launch. Legacy reads
use the same target validation without modifying the returned configuration.
Owned proof issuance now reads configuration and the exact approval decision
through a detached bounded repository scope and requires a contextual signer.
Cancellation before/after each read or signing prevents a proof from reaching
launch. Signing itself is local; proof consumption is the separately persisted
one-use boundary. Legacy issuers remain separate, with no fallback for owned
requests. This is not an atomic configuration/decision snapshot or proof of
real PostgreSQL cancellation/commit behavior; those acceptance gates remain open.
Proof verification/consumption now checks caller cancellation before storage and
after its acknowledgement, with a bounded lifetime. A lost acknowledgement keeps
any spent proof spent and returns a reconciliation-required error; it does not
restore authority. PostgreSQL consumption refuses dry-run and borrowed roots,
uses a detached context, and distinguishes duplicate claims from unexpected row
counts. Controlled SQL-pool tests are not live commit/lock/restore proof. The
in-memory test store checks cancellation before/after its lock but is not an
interruptible or production-durable lock implementation.
The launcher independently bounds proof-adapter calls, checks cancellation at
entry/return, and revalidates the exact action binding after consumption. An
adapter's success cannot turn a cancelled or changed binding into verified
authority. Such handoffs retain a reconciliation requirement; they do not undo
proof consumption. This check does not forcibly preempt a noncooperative adapter
or replace the final effect-admission and dispatch-time policy checks.
Owned launcher admission now requires a bounded contextual configuration read,
validates the exact returned automation identity, and checks cancellation before
and after idempotency lookup and before intent storage. Interrupted or invalid
configuration cannot start an intent through the legacy reader. Legacy launch
calls remain separate. Owned admission also requires a bounded contextual storage
scope for idempotency lookup, intent writes and replayed outcome reads/projection
repair, with no legacy fallback. An uncertain intent-write acknowledgement keeps
the candidate identity, reports indeterminate persistence and prevents dispatch;
it is not proof of a durable write and must not trigger a blind retry or undo.
New execution outcome/projection writes now use a separate bounded storage scope
that retains trusted context values but does not inherit caller cancellation.
Unavailable scope, failed or late-cancelled outcome acknowledgement returns the
existing intent reference as indeterminate with a reconciliation error, not proof
of completion. Projection errors preserve the known stored outcome. Legacy launch
compatibility remains separate; real database cancellation/commit acceptance
remains outstanding. Replay cancellation
after a successful outcome read retains the validated, redacted historical receipt
alongside the cancellation error without repairing projections or dispatching.
Mismatched ownership/action binding never exposes the receipt. Authenticated HTTP
launch failures can include a minimal recovery block: matching automation/attempt
IDs, a whitelisted reported status, reconciliation required and retry not allowed.
It exposes no output, targets, runtime references or raw messages, retains the
non-success HTTP status, and is not proof of persistence/completion or authority.
The Command Center presents this reference instead of the generic retry suggestion.
This notice is not a durable retry lock or a completed operator-reconciliation flow;
the live UI and actual recovery process still require acceptance.
Browser launch keys are now cleared only for a matching automation with a nonzero
outcome UUID, exact `completed`/`ready` status and no approval requirement. Missing,
unknown, failed, nonterminal or mismatched responses keep the existing key across
service instances; late acknowledgements cannot clear a replacement key. The
Command Center uses the same validation before showing success or changing its
launch timestamp, and distinguishes ready from completed. These client checks
do not enforce a server-side retry prohibition or replace durable reconciliation.
The launch HTTP endpoint reports unavailable contextual configuration capability
or admission storage capability as 503 before intent storage, while preserving existing ownership, approval and
idempotency checks. Linux script fixtures are kept compatible with the new reader;
Windows verification does not execute their platform-specific acceptance cases.
Launch bodies are optional but bounded to 64 KiB. A present body must contain
one JSON object; null/arrays, malformed input and trailing values are rejected
before the launch service. Oversize bodies return 413; other invalid bodies return
400. Chunked or zero-declared-length bodies are read rather than silently ignored.
This parser bound does not establish proxy/server slow-client timeout acceptance.
Launch, runtime-stop and diagnostics handlers also require the middleware-verified
nonempty subject before service entry; launch rejects missing identity before
reading its body. Raw headers cannot supply that subject. This is defense in depth
for misconfigured routes, not a replacement for the existing route authentication
and role filters or proof of live IDP/session acceptance.
Runtime stop validates the requested automation against an exact, non-null
configuration record and applies defaults to a copy. Missing storage, missing
records, nil IDs or different returned IDs fail before task lookup/audit writes.
This prevents wrong-target selection and borrowed-record mutation, but stop task
lookup, runtime dispatch and stop-audit storage still use legacy lifetimes and
need separate contextual and live-runtime acceptance.
Pursuit/coordination projection writes, list/reconciliation APIs,
settlement writes and several planning and
verification collaborators still have context-free calls once entered. The
entered pursuit/agent-cycle calls and legacy ambient task callers do not automatically acquire caller
cancellation. Actual database cancellation, crash/restore, real provider/runtime
and operator acceptance remain required. Legacy reminder entry points remain available for
compatibility and do not acquire caller context automatically. Lost commit
acknowledgements still require inspection of durable records, not an assumption
that nothing happened. The trusted ambient ticker and
durable handler now require `ScanContext`, pass the owned execution context and
current safety gate, and refuse a missing adapter instead of calling legacy
`Scan`. Ambient persistence uses a detached GORM context and refuses dry-run,
missing-dialect and non-PostgreSQL execution roots; checkpoints stop new
stages after cancellation or a safety pause. Confirmed partial follow-up/task
counts survive a later interruption. Failure-only outcome recording uses a
separate two-second cooperative context; that context cannot continue the scan.
Scan creation uses an application-assigned ID, and creation/terminal
acknowledgements must match the identity, owner, state, timestamps and known
results. Ambiguous terminal writes are not overwritten as failed writes.
Unconfirmed outcomes return `outcome_unconfirmed`, not success. Scan retention
excludes running/unknown rows; personal retention is owner-scoped and cannot
fall back to deleting shared history. This is source implementation, not live
PostgreSQL acceptance or automatic uncertain-scan recovery. The durable ambient
path now persists `review_unknown` replay policy and holds unconfirmed results
as nonclaimable `needs_review` occurrences, including combined safety-pause/
storage failures. Cancellation, panic, failed heartbeat and expired leases also
require review rather than blind replay. Holds block recurring replacement and
pending same-queue/kind claims; failed hold persistence leaves the occurrence
leased until policy-aware recovery can quarantine it. Operator reconciliation
and actual PostgreSQL crash/concurrency acceptance remain unfinished.
Outcome writes must be acknowledged; a rejected write or storage failure is
not a successful worker poll. Review-sensitive recurring completion prepares
the terminal result and successor while retaining a nonclaimable `settling`
barrier, then releases that barrier in a separately acknowledged transaction.
A lost preparation commit acknowledgement cannot expose the successor. A lost
release acknowledgement remains uncertain: the database may already have
released confirmed preparation, so inspect the same records before recovery.
`settling` also blocks startup replacement and is never automatically reaped.
There is not yet an operator-approved reconciliation/resume workflow.
The ordinary completion SQL requires explicit persisted replay permission and
the same queue/kind. A stale caller cannot downgrade a review-policy occurrence
by merely supplying an ordinary successor.
Owner HTTP scans, legacy `Scan`, agent-cycle entry points, and
already-entered memory/pursuit/task calls still lack complete context coverage.
Actual PostgreSQL interruption,
concurrency/crash and operator recovery remain acceptance gates.
No running service, schema or historic data was changed by these edits.

Migration `pre/0115_durable_job_replay_policy` adds the replay policy, upgrades
active ambient occurrences and indexes review/preparation holds. It has not been applied
to the local databases. Stop/drain old workers before migration and rollout:
old binaries do not honor review holds. Runtime registration atomically upgrades
existing active occurrences under the singleton lock before publishing the
handler. Rollback refuses to discard any retained review/replay authority; it
does not delete records. Ambient durable-start failure no longer falls back to
an unprotected ticker. Explicit legacy ticker mode remains available but its
review hold is process-local and is not the supported production path.

The pre-read source integration and its dedicated regressions are documented in
the [integration ledger](output/production-durability-integration-20261001.md#pre-read-source-observation-integration).
The first and reviewed native observation-wiring runs failed and remain retained;
the audit assertion now matches event types/messages instead of timestamp order.
The `production-low-resource-observation-wiring-final-20261001` receipt records
the **first observation baseline pass**: all eleven package suites, exit 0, no
timeout, 19:10:40.7168174-19:11:17.8096668 UTC. Subsequent context-guard fixes and
0112 origin-FK tightening followed this baseline and are covered by the newer
amendment receipt below.
This baseline does not accept those later amendments. The later
`production-low-resource-intake-authority-first-20261001` receipt passes the full
operations/background/accountfeed suites, exit 0, no timeout,
19:14:05.0628566-19:14:30.6343425 UTC. Contextual intake now checks absolute
deadlines and the original private observation authority before store access and
again after lookup/at write boundaries; it does not replace nil context with
Background. Missing GORM stores fail closed without panic. A later synthetic
GORM delayed-expiry lookup regression and two held-mutex original-authority-end
cases are covered by `production-low-resource-intake-authority-lock-reviewed-20261001`:
the same three full package suites pass, exit 0, no timeout,
19:15:58.2817877-19:16:26.4492224 UTC. Neither earlier three-package receipt
accepts the then-new origin cases; the newer eleven-package receipt does.
Selected integration-tagged router checks pass in
`production-low-resource-observation-wiring-router-20261001`, exit 0, no timeout,
19:16:56.7312074-19:18:36.8474156 UTC; they do not execute the new operations tests
or replace full package-suite proof.
`production-low-resource-observation-intake-amendments-final-20261001` is now
terminal success: all eleven full package suites, exit 0, completed successfully,
no timeout, 19:20:11.3294580-19:21:52.3837396 UTC. It includes Hubble's three origin
tests/24 subcases and nine embedded observation migration contracts, executed
locally alongside the context guards. Case counts come from source, not compact
log enumeration. Actual PostgreSQL
upgrade/constraints/rollback acceptance remains unexecuted, destructive database
tests remain disabled, and no Docker environment was started for this increment.
These local checks are not evidence that private installations were migrated.

HAI remains **not fully production-ready**. Real account/provider acceptance,
trusted participant dispatch/probes, crash and rollback durability, complete
current-source browser coverage, production-model recovery and signed clean-machine
Windows release acceptance are still required. No commit, push, private deployment
or application restart is implied by these local changes.

### Prior Production Continuation Checkpoint

The earlier [production continuation ledger](output/production-integration-20261001.md)
records parallel authentication, backup-integrity, Trello synchronization,
model-health, Windows lifecycle and safe-outcome consumer work. Parent completion
checks now include unresolved old task attempts and every explicitly linked
runtime receipt, not just the dashboard's newest twenty rows. Unverified or
contradictory outcomes cannot authorize verified pursuit completion.

Twelve bounded workers completed their source/review assignments, with at most
six running simultaneously. Their results are integrated in this shared
checkout; heavy executable checks were parent-owned and serialized to protect
the shared Windows host. This phase also fixes optional PostgreSQL CHAR-padding
in immutable portfolio digests and rejects unsafe Trello test destinations
before opening a connection. The latter tests now have explicit CI PASS gates.

Final local results: **3970 top-level backend tests across 147 packages**, with
**208 optional test/subtest skips** and zero failures; **241 IDP top-level tests**
with **78 optional test/subtest skips**; and **1207 frontend tests**, zero failures.
The frontend production build passes with **five remaining SCSS budget warnings**
(703.22 kB initial raw bundle / 115.42 kB estimated transfer). No warning budget
was increased. Current backend and IDP static analysis passes, as do 78 local CI
contracts and the frontend source contracts. These are not hosted CI results.

Real, owned disposable PostgreSQL checks pass for pursuit evidence, portfolio
save/reload/replay, concurrent login/reset and all four Trello repository tests,
with race detection and no skips. New safety regressions also pass three repeated
race runs across eight backend and two IDP packages. Failed earlier runs remain
recorded alongside their fixes. The UI preserves supplied execution receipts and
blocks ordinary retry of uncertain outcomes, but its in-memory state is not a
crash-durable ledger or an exactly-once guarantee.

HAI is still **not fully production-ready**. Current-source visual/operator
acceptance, real external accounts/providers, crash/rollback durability, actual
backup restoration and a signed clean-machine Windows release remain gates.
No personal application restart, deployment, live-account action, commit or push
was performed. See the ledger for exact logs, integration ownership, limitations
and remaining production gates; older evidence below is retained history.

### Prior Runtime-Outcome Checkpoint

Latest bounded local continuation strengthens runtime-outcome retention and
recovery across automation, tasks, workflows and the task UI. Unknown execution
cannot become verified completion, accepted learning or an automatic retry;
ordinary approval cannot repeat a prior uncertain task. Historical automation
configuration binding is integrated, and final current-source backend tests
pass: **3924 top-level tests across 147 packages**, with **208 optional
test/subtest skip events** and zero failures. Unknown outcomes also remain
needs-review in the life graph and assistant response. Full backend static
analysis and six scoped race-enabled packages passed. Selected real PostgreSQL
checks across five packages passed **71 top-level tests**, with zero skips or
failures, including persistence/concurrency and original migration-boundary
rollback protection. The complete backend suite was rerun after database
test-fixture initialization repairs: again 3924 top-level passes across 147
passing packages, 208 optional test/subtest skips, and zero failures. Production
source was unchanged by those fixture repairs. These checks are not live-provider
or deployment acceptance.
The saved frontend passes **1157 tests** and its production build, with six
existing SCSS budget warnings. Corrected installer signing guards pass **93
cases on PowerShell 7 and 93 on Windows PowerShell 5.1**, using deterministic
doubles, not real signing. See the [current integration ledger](output/runtime-outcome-integration-20261001.md)
for exact evidence, failed-run history, limits and remaining gates.

The preceding [model-readiness checkpoint](output/workflow-installer-integration-20261001.md)
passed **3871 top-level backend tests across 147 packages**, with **208 optional
test/subtest skip events**, 1140 frontend tests and focused race checks. Those
results predate the new runtime-outcome changes and do not verify this new
revision. Neither checkpoint is a deployment, signed release, hosted CI,
live-account acceptance or completion of the full product. Evidence below is
retained history, not a replacement for the current ledger.

Production readiness is **staged and incomplete**, not established. The
[October partial checkpoint](docs/verification-2026-09-30-runtime-hardening.md#2026-10-01-staged-production-readiness-partial-checkpoint)
separates direct log/report review from parent-reported results and historical
evidence. The earlier integrated Angular run 5 ends with **1131 SUCCESS** on
Chrome Headless 151; this is not a final Playwright acceptance result.
The earlier isolated browser report remains **9/12 PASS, 3 FAIL**. The parent
now reports owner `6cfa099c027a455bb3cba587f5b1f0ff` started and fully healthy
in synthetic manual-local mode. Run 1 ended **11/12 PASS, 1 FAIL** because the
operator test had not exercised the mandatory action-bound approval proof.
Run 2 also ended **11/12 PASS, 1 FAIL**, on an assertion confusing API state
`needs_approval` with the displayed `needs approval`. The parent corrected
that assertion; focused operator run 3 completed and exposed a genuine remaining
blocker: the exact owner approval succeeds, but required specialist participants
and their review evidence are not available. A proposed embedded-runtime
exemption was rejected during safety review rather than used to manufacture a
passing journey. Automatic and approved execution now preserve that prerequisite,
report every missing participant, and disable repeated task runs until it is
resolved. No final operator PASS is claimed. That earlier owned stack has been
removed without deleting evidence or volumes; current-revision build and browser
results are recorded separately as they finish.

The [structured-redaction report](output/structured-redaction-20260930.md)
and [automation-projection report](output/automation-public-projection-20260930.md)
now record both defects fixed with focused synthetic tests, race-enabled
three-repeat suites and vet. The parent reports source/report review completed;
these are not final-image privacy, historical-record cleanup or live-provider
proof. The retained full backend snapshot has **147 passing packages, 3815
top-level passes, 202 optional test-skip events and zero failures**; vet terminal
exit 0 is parent-reported. That snapshot does not establish full-suite acceptance
after the final privacy changes. CI has **76 local contracts PASS**; hosted CI
remains unverified.

The subsequent current backend integration run completed with **144 passing
packages and three failing packages**, exposing benign-JSON redaction
compatibility and quoted HTTP-error query-secret defects. At that checkpoint,
these required a shared sanitizer repair, not relaxed caller checks; the
subsequent final repairs and verification are recorded below. The current
Task Blueprint build and 22 focused component tests passed; six SCSS budget
warnings remain. Build workers are capped at two by default, without a measured
resource-saving claim. The [integration safety ledger](output/integrated-safety-checkpoint-20261001.md)
records current failures and subsequent terminal results separately.

After the shared sanitizer and typed-metadata validation repairs, fresh backend
run 3 completed with **147 passing packages, 3854 top-level test passes and zero
failures**. Its **208 test/subtest skip events** remain coverage gaps, not
acceptance of optional integrations. The original payloads still determine
digests; generic credential-field checks and execution approval gates remain.
The build-resource regressions are now wired into frontend CI; all **76 local
CI contracts passed**, but hosted CI and the new owned-stack browser run are
separate release gates.

The subsequent final frontend unit run completed with **1132 SUCCESS**. Backend
vet and three repeated race-enabled privacy/integration suites also passed.
The rebuilt synthetic owner `7428197162ac47be996715b3b261c634` then recorded
**11 browser passes and one operator failure**: local source ingestion and
exact runtime selection succeeded, but execution was blocked **before** the
approval step because no capable model/framework evidence was available.
This is distinct from the earlier post-approval failure; neither closes the
release gate. That stack was stopped with evidence preserved. Basic diagnostic
summaries and Sources/Memory toolbar-label fixes are included in the final
frontend unit run; their fresh rendered verification is recorded in the
integration safety ledger. No personal installation, live account or provider
was changed or deployed during this continuation.

The final rebuilt UI browser run retains **11 PASS/1 FAIL**, with that same
pre-approval operator blocker. A subsequent Control Room run passed **8/8,
zero skips**, including all 28 operational modules at four widths, contained
toolbar labels, loaded HAI OS state, keyboard/navigation recovery and a real
persisted blocker opening its exact workflow without a write. The build passed
with six SCSS warnings still open. These results do not establish terminal
execution, live-account readiness or complete Advanced/accessibility coverage.

The [isolated acceptance guide](docs/isolated-acceptance.md) keeps the default
stack paused. Its explicit `manual-local` opt-in enables only the manual source
worker, keeps every automatic scheduler disabled and the backend on the
internal network, and uses no accounts/providers. `HAI_PHASE2_MODE=autonomous_safe`
permits the exact owned synthetic test work; it is not a broad autonomy grant.
The worker's [dedicated PostgreSQL acceptance](output/manual-worker-postgres-20260930.md)
now records six named tests repeated three times under race detection:
**18 PASS, zero skips/failures**, using owned PostgreSQL 17.11 tmpfs fixtures
with exact cleanup. This adds bounded claims/concurrency/retry/reload/extraction
proof, not full process-restart or canonical migration/constraint acceptance.
Separately, bounded real-PostgreSQL ambient-monitor acceptance was reported;
fully pinned policy/composer snapshots and concurrent source-write consistency
remain unproven.

Remaining release gates include approved current-revision packaging and
signing (the retained installer is stale and unsigned), a clean Windows
install/upgrade/uninstall, real backup/restore and migration rollback, approved
live providers and OAuth/SMTP, OS-enforced sandboxing, full accessibility
acceptance, populated Advanced-view action chains, and final isolated browser
and hosted CI evidence. Native process supervision and synthetic tests do not
close those gates.

### Historical Pursuit Hardening Milestone

The historical local milestone below records a safety-focused pursuit hardening pass.
Pursuit dashboards, detail views, links, task-attempt summaries, runtime
evidence, source resolution, candidate routing, and decision handling are all
evaluated in the authenticated owner's scope. Related pursuits are navigable in
the dashboard, but a pursuit cannot link to itself and a relationship cannot
be used to expose another owner's operational record. Candidate pursuits remain
non-executable until an approval-capable user explicitly accepts them; decision
resolution is also permission-checked in the handler, not only in route wiring.
The Command Dashboard is the unified operator queue for governed workflow
approvals, proposal choices, candidate acceptance or archival, approved next
actions, runtime recovery, and verified pursuit completion; each control calls
the existing audited API rather than bypassing the relevant gate.

This is an implementation and acceptance-tested local milestone. It does not
replace the release gates in the verification snapshot below: a fresh-machine
browser flow, newly configured provider acceptance, local-model task, and each
mutable runtime dry run are still required before relying on those paths for
personal work.

## Product Boundary

HAI is the product. A pursuit is the high-level objective or case that connects
the systems below; it is not a second product or a replacement workflow engine.
The earlier Manus React/tRPC/MySQL implementation is reference material only.
Useful behavior from it should be ported deliberately into this stack rather
than maintained in parallel.

See [ADR 0001](docs/architecture-decision-records/0001-canonical-stack-and-readiness.md)
for the canonical-stack decision.

## Framework Registry

The [Framework Registry](docs/framework-registry.md) defines HAI's versioned,
owner-scoped contract for selecting the smallest suitable set of planning,
reasoning, governance, domain, and evaluation frameworks for a task. Its
implemented `framework-catalog-v2` contains 55 records (54 at `1.0.0` and the
evaluation framework at `1.1.0`; 50 active and five experimental), mandatory
safety overlays, deterministic `selector-v5` selection with enforced task-risk
ceilings, owner-scoped preferences, Constitution lifecycle, authority ceilings,
reproducibility digests, API/UI/task/workflow integration, and versioned
pre-phase migrations.

The [Framework Operating Contract Matrix](docs/framework-operating-contract-matrix.md)
maps all 55 research families to enforced, structured, or catalog-only
behavior and states the remaining live-system boundary for each.

Durable task plans and runs are projected into an append-only, owner-scoped
operational life graph. The graph links tasks to projects, pursuits,
workflows, verified memories, and outcomes with typed relations and source
digests. Preview requests never write graph state, local-only records remain
hidden until explicitly requested on the local governance screen, and graph
records cannot grant approval or execution authority.

Connected-source sync uses the same graph boundary. After a raw item and its
extraction are durably stored and indexed, HAI appends an immutable document
record linked to the registered source and project. Owner identity, content
digest, sensitivity, verification state, and local-only policy are retained.
Graph outages appear as audited sync warnings without discarding a successful
email, file, Trello, Drive, GitHub, or other source ingestion. Operator
corrections and archive changes create new observations rather than rewriting
history.

Selector v4 also produces a durable Chief-of-Staff operating contract: all
matched life domains, needs state, freshness-aware human capacity, verified
agent cards with explicit identity/capability/access/cost/health/revocation
fields, authority-bounded delegation contracts, replay-resistant typed
communication, coordination mode, exact per-action autonomy decisions, stop conditions,
outcome monitoring, and an operating-contract digest. Workflow due dates flow
into delegation deadlines; every delegation defaults to zero financial
authority. The Advanced registry view exposes these details without turning
the Basic view into a diagnostic wall.

Task planning also applies a deterministic, advisory-only resource schedule.
Conservative step durations, dependencies, deadlines, paid/token/tool budgets,
and owner-confirmed Life Ops capacity are evaluated before execution. When an
owner has an active read-only Google Calendar source, opaque busy intervals are
subtracted from that capacity; cancelled and transparent/free events are
ignored. Unknown or stale capacity requires review, confirmed zero remaining
capacity blocks the plan, and Calendar read failures fail closed. The Task
Blueprint shows the feasibility result, reserved source-linked intervals,
scheduled steps, blockers, and approval flags. This path cannot move events,
consume approval, or grant execution authority.

Catalog lifecycle and owner-effective state are separate. `active` records are
enabled by default; `experimental` records are disabled by default and need an
owner opt-in plus a direct match; `deprecated` records are excluded from
selection. `disabled` is an owner preference, not a fourth catalog lifecycle
status. An owner can enable an experimental record or disable an ordinary
active record, but cannot disable a protected safety overlay.

The built-in fallback Constitution has the exact source
`builtin-robert-constitution-v1:v1`. Registry selection records retain the
catalog version/digest, selector version, effective-preference digest,
Constitution digest/source, selected framework versions, reasons, evidence
requirements, and authority ceiling. Protected overlays cannot be disabled;
owner preferences may only enable an experimental record, pin a relevant
record, lower autonomy, or add bounded safe adaptations.

Constitution activation is owner-only and requires the exact, case-sensitive
confirmation `ACTIVATE CONSTITUTION` with no leading or trailing whitespace,
plus a redacted approval note of at least 10 characters. Ordinary Constitution
prose is immutable, versioned governance context; it is not executable policy.
Only code-owned protected controls and valid restrictive `HAI-RULE v1`
`deny-capability`, `require-approval`, or `authority-ceiling` entries are
machine-enforced. No Constitution entry can grant authority.

Framework records are decision metadata, not installed tools or granted
authority. A named agent framework, workflow platform, memory store, policy
engine, or evaluation product is only a candidate implementation until its
adapter is configured and passes security, capability, integration, audit, and
real-world verification gates.

The Go routes, Angular `/framework-registry` page, and nginx authenticated API
allowlist are wired together. Repository tests cover the component, service,
route, permission, and static gateway contracts. A clean-machine signed-in
browser exercise remains environment-dependent acceptance evidence.

Viewers can inspect the owner-scoped registry and selection history. Operators
can also request and persist a selection recommendation. Only an owner can
change framework preferences, create a Constitution draft, activate a
Constitution, run an approval-gated task, or resolve a task review item.

## What Is Implemented

| Area | Implemented capability | Important operating boundary |
| --- | --- | --- |
| Operator UI | Angular onboarding, Quick Capture, Control Center, Command Dashboard, HAI OS, pursuits, workflow exceptions, sources, memory, LLM policy, grounded answers, task planning, and the Framework Registry. | A dashboard card is operational visibility, not proof that an external action occurred. |
| Pursuits and workflows | Durable pursuits, workflow states, checklists, decisions, open loops, blockers, follow-ups, approvals, review queues, retries, task-attempt evidence, read-only VA delegation briefs, ambient opportunity routing, navigable related-pursuit links, owner-scoped internal reminder proposals, an append-only reminder preparation/decision ledger, owner-authorized internal reminder delivery receipts, and calendar-aware resource/dependency planning. | In the canonical routed stack, new source, assistant, or ambient context is matched to an active pursuit first; otherwise it becomes an approval-gated candidate, not executable work. Resource plans and reminder projections are advisory and owner-scoped. A preparation request or approval alone is evidence only. After a separate exact owner authorization, the durable workflow worker may create one source-bound internal proactivity signal with an idempotent receipt. It cannot create Calendar events, send email or messages, invoke providers, execute follow-ups, or mutate the source checklist. |
| Memory and knowledge | Compact memory, retrieval, deduplication, correction, export/deletion planning, provenance, encrypted user-authorized conversation capture, and source/extraction links. | Raw imported conversations are not automatically promoted to trusted facts. |
| Source ingestion | Allowlisted local files; MBOX/EML, ICS, Trello JSON, WhatsApp exports, Odoo/HERP snapshots, normalized JSON feeds, synced document folders, read-only GitHub, Gmail, Google Drive, Google Contacts, Google Calendar, Trello, ShareT, LARO, and Worker Control sync. | Gmail and Trello have bounded live acceptance evidence but are unconfigured by default. ShareT has a bounded, paginated, read-only adapter with contract coverage; its live token activation remains operator-gated. Drive, Contacts, and Calendar have unit/contract coverage but still need real sandbox acceptance runs. Imported contacts remain review candidates. Meaningful events within 14 days may create source-backed preparation work; past events stay context-only. Overlaps within 30 days create stable review-gated conflict records, while moved or cancelled events retract stale work. No Calendar or ShareT write-back exists. WhatsApp and browser accounts remain export/local-folder paths. |
| LLM routing | Local-first routing, seven-tier model policy, local/OpenAI-compatible endpoint probes, fallback logging, cached/repeated-prompt controls, and a EUR 0 paid default. | A configured endpoint is not live-proven until it passes a bounded probe and validated task. Paid generation remains disabled by default. |
| Verification | Source-grounded answers, claim/evidence status, schema/deterministic validation, review routing, and verification-gated task completion. | Model confidence alone never authorizes a factual claim or consequential action. |
| Controlled execution | Reviewed API, script, Docker, Hermes, Odysseus, and OpenClaw adapter surfaces with bounded output, workspace/host allowlists, audit records, verification, emergency stop, and an internal action-bound approval proof before mutating side effects. | Direct mutating HTTP launches cannot create the proof and fail closed. The approved task-review path issues a short-lived proof signed by a stable deployment key; PostgreSQL atomically records its one allowed consumption across restarts and backend instances. External runtimes remain disabled until explicitly configured and validated, and external side effects still require postcondition/idempotency evidence. |
| Optional local runners | Disabled-by-default Compose profiles for aggregate security scans, no-tool planning drafts, selected-folder document extraction, and disposable patch proposals. | They publish no host ports and have private networks, read-only mounts, and resource limits. Configuration or container health is not live proof; each real snapshot, model, or document path still needs retained approval, audit, and verification evidence. |
| Proactive planning | Ambient scans identify stale work, blockers, approvals, open loops, contradiction candidates, and delegation opportunities. Governance Control records owner `accept`, `dismiss`, bounded `snooze`, indefinite `suppress`, and `resume` feedback in an immutable owner-scoped ledger that changes later attention evaluation. | Ambient mode is suggestion-first and cannot bypass approval, verification, leases, audit, or emergency stop. Attention feedback has `canExecute:false`, grants no delivery or execution authority, and invokes no notification or external effect. |
| Advisory ambient outcome monitor | Governance Control can bind an existing outcome indicator to one of three fixed read-only local collectors: `workflow_open_loop_count`, `workflow_verified_completion_count`, or `overdue_commitment_count`. A durable singleton sweep leases due targets, appends immutable source-digested observations and run receipts, composes them into the existing outcome-evaluation service, and may surface an owner-scoped proactivity inbox decision. | The monitor is `advisory_monitor_only`. It cannot execute or deliver work, notify anyone, write Calendar data, mutate a workflow, authorize a mandate, or mutate learning. It reads only canonical local ledgers and accepts no caller-supplied SQL, URL, script, expression, or arbitrary tool instruction. Live external-account correctness and target-machine acceptance remain separate gates. |
| Operations | nginx gateway, IDP, Postgres, Redis, optional Kafka-compatible event bus, health/readiness, support bundle, doctor/reconcile/migrate commands, versioned SQL migrations, a durable job runner (persisted retry + crash recovery), CI, Compose validation, and local smoke coverage. | **Source, workflow, and ambient** scheduling use durable recurring singleton jobs. Ordinary terminal occurrences and replacements share one transaction; review-sensitive ambient completion uses confirmed preparation and separate release with a durable `settling` admission barrier. Unknown outcomes require review under persisted replay policy; failed ambient queue startup does not fall back to a ticker. Source/workflow still have logged legacy ticker fallback. Actual cross-process crash/commit and operator-reconciliation acceptance remains required. The workflow schedule consumes only separately owner-authorized internal reminder deliveries; each delivery is revalidated, source-bound, idempotently receipted, and recorded as a local proactivity signal. No external notification, Calendar write or provider invocation is performed by reminder delivery. This is not an accepted distributed or HA platform. |

### Readiness Terms

- **Implemented**: code, persistence, API contract, and focused automated coverage exist in this repository.
- **Locally validated**: a bounded build, Compose, Postgres, gateway, or smoke check exercised the path. This is not third-party proof.
- **Live-proven**: a configured account, provider, or runtime completed a bounded approved end-to-end task on the target machine with audit and verification evidence.

No configured provider, runtime, dashboard state, or generated answer upgrades itself to live-proven.

### Advisory Outcome Monitor

The outcome monitor is a local evidence bridge, not an autonomous executor.
An administrator configures a target against an existing owner/workspace outcome
and indicator in Governance Control. Read-capable roles may inspect targets and
their immutable observations/runs; write-capable roles may request a bounded
due pass; administrator permission is required to create, enable/disable, or
recover target state.

The guarded API surface is under
`/api/v1/outcome-evaluations/workspaces/:workspaceId`:

- `GET|PUT /outcomes/:outcomeId/monitor` lists or creates a target;
- `PATCH /outcomes/:outcomeId/monitor/:targetId/enabled` pauses or resumes it;
- `GET /outcomes/:outcomeId/monitor/:targetId/observations` and `/runs` expose
  bounded immutable history;
- `POST /monitors/run-due` performs a bounded advisory pass; and
- `POST /monitors/recover` releases only expired leases.

The durable scheduler is configured with
`OUTCOME_MONITOR_SCHEDULER_ENABLED`, `OUTCOME_MONITOR_SWEEP_SECONDS`,
`OUTCOME_MONITOR_POLL_SECONDS`, `OUTCOME_MONITOR_LEASE_SECONDS`,
`OUTCOME_MONITOR_SCOPE_LIMIT`, and `OUTCOME_MONITOR_BATCH_LIMIT`. Invalid or
out-of-range values fall back to bounded defaults documented in
`.env.example`. Disabling the scheduler does not remove the records or grant a
different execution path.

Durable workers process work already due when HAI starts, then use five-minute
idle polls by default. This keeps a local installation quiet when no source,
workflow, ambient, or outcome work is pending; explicit manual passes and
normal recurring due times remain governed by their existing schedules.

Required acceptance before relying on this path includes exact replay without
duplicate observations or inbox items, two-owner isolation, active-lease
fencing and expired-lease recovery, disable behavior, and proof that a monitor
pass causes no task/runtime execution, notification, message delivery,
Calendar write, workflow mutation, mandate authorization, or learning update.
Repository implementation and focused tests do not by themselves prove
real-world source correctness or production readiness.

### Optional Runtime Profiles

Recovered security, agent-planning, document, and patch-proposal helpers are
now wired as isolated, disabled-by-default Compose profiles. The ordinary
`docker compose up` does not start them. See
[Optional Runtime Profiles](docs/optional-runtime-profiles.md) for the exact
profile names, environment contract, resource ceilings, read-only mount rules,
activation commands, and evidence required before any capability is described
as live-proven.

MLflow and OpenLIT remain bridge-only integrations to separately operated
local/private services; this repository does not silently install or expose
either observability server.

### Historical Status At A Glance

This table preserves the earlier local evidence boundary, including the
2026-08-04 browser run; it is not a current service inventory or release verdict.
Use the October checkpoint above for continuation status.

| Status | Current position |
| --- | --- |
| Canonical product | This Go/Angular/Postgres/Docker Compose repository. The separate Manus React/tRPC/MySQL implementation is reference-only. |
| Local platform | The current Windows Compose workspace has a retained browser acceptance run covering password login, read-only local source registration and sync, explicit pursuit creation, governed high-risk workflow intake, durable approval, and one bounded worker pass (2026-08-04). A separate fresh-clone Windows 11 acceptance run is still required. |
| Core operating flow | Pursuits, workflows, task attempts, approvals, verification, audit, compact memory, source extraction, and ambient proposals are implemented and persisted. |
| Intake safety | New source, assistant, and ambient input is matched to an active pursuit or becomes a non-executable candidate. An approval-capable user must accept a candidate before its first governed workflow is created. |
| External accounts | Local/export ingestion and read-only GitHub sync are available. Gmail and Trello have bounded live acceptance evidence. Google Drive, Google Contacts, and primary Google Calendar have separate read-only OAuth adapters with bounded backfills and native change/sync cursors, but no retained live sandbox acceptance evidence yet. Contact candidates require review. Calendar event times feed deterministic due dates, bounded preparation proposals, and overlap review; moving or cancelling source events retracts stale Calendar-derived work without deleting obligations. These paths cannot write back. WhatsApp and browser connectors are not live. |
| Models and runtimes | Local/free-first routing and guarded adapter surfaces exist. No provider or runtime is live-proven until its scoped probe, approved task, audit, and verification evidence exist. |
| Production readiness | Not claimed. Clean-machine deployment and bounded acceptance for each newly enabled provider or mutable runtime remain release gates. |

### Historical Verification Snapshot

This is the earlier local evidence boundary, not a feature checklist or a
current health check. Re-run the
target-machine checks before relying on a path for real work.

| Surface | Current evidence | Still required before operational trust |
| --- | --- | --- |
| Local Compose and gateway | At that historical checkpoint, the local services were running; `/`, `/control-center`, `/healthz`, and `/readyz` were served through nginx. Both health probes are intentionally public; protected `/api/v1/*` engine routes still require a signed session. Angular deep links return the application shell. | Fresh-clone Windows 11 run with a newly created `.env.local`. |
| Browser session | The unauthenticated session check returns `401`; Angular routes a browser without a refreshable session to `/login`. A signed-in Playwright acceptance run completed source intake, pursuit creation, exact runtime selection, durable approval, read-only execution, terminal verification, and creation of an immutable completion attestation. CI now also defines this local, read-only path as an isolated Compose browser-acceptance gate. | The new CI job must complete successfully before it can be cited as hosted release evidence. Repeat the acceptance run on each release target and add retained coverage for any new mutable or external action. |
| Go and Angular code | The repository includes the Go suite, frontend production build, headless Angular tests, and migration coverage. The checked-in pre-migration files currently extend through `pre/0114_operation_source_configuration`: `0110` adds the durable feed registry, `0111` structured source identity, `0112` immutable pre-read provenance, `0113` accepted heads/revision history, and `0114` canonical managed configuration versions and enablement. Tests cover the embedded chain, governed tail through `0114`, source contracts and opt-in PostgreSQL definitions. The current canonical-configuration receipt passes eleven backend package suites; this does not establish real PostgreSQL acceptance or safe migration of an existing installation. Focused OpenClaw coverage includes private session receipts/recovery, metadata-only artifact handling and signed maintenance updates; new Gateway session creation is fail-closed pending authenticated identity and run-bound sandbox-policy attestation. | Run the full release suite and close existing CSS/initial-bundle warnings. Clean-clone Windows 11 migration apply/rollback remains unproven. Real PostgreSQL acceptance for `0112`/`0113`/`0114` is unexecuted; destructive tests are disabled and no Docker environment was started for this increment. Canonical repository patch/disable revocation exists, but raw administrative SQL enrollment concurrency, tombstones, deliberate reconciliation, durable effect replay and real lock/crash acceptance keep gate 10 open. Local tests do not prove scheduled delivery, Calendar writes, messages, provider invocation, follow-ups, live OpenClaw Gateway execution or unattended maintenance deployment. |
| Sources and LLMs | Local/export ingestion, provider probes, GitHub sync, and bounded Gmail/Trello acceptance evidence exist. | A scoped local-model task and any newly configured account need their own retained audit and verification evidence. |
| Runtimes and external effects | Script, Docker, Hermes, Odysseus, and OpenClaw adapters have bounded, approval-aware interfaces. DeepSeek Harness is present as an architecture/runtime adapter, but production task execution is hard-disabled until authenticated bridge transport and real OS-enforced isolation are accepted. The local registry-to-read-only-API path is acceptance-tested with deterministic receipt verification. | Explicit upstream installation, narrow allowlists, a reviewed dry run, and a verified approved task for every mutable or external adapter; DSH additionally needs TLS peer verification, native Windows sandbox evidence, and independent task-outcome verification. |

## Current Safe Operator Flows

### Backend Maintenance Commands

These are host/operator commands for the built backend executable, not web actions.
The container executable is `/app/app`; use the installed binary's path on Windows.

| Arguments | Behavior |
| --- | --- |
| No arguments | Start the backend server. |
| `doctor` | Report configured readiness checks without opening the command database pool. |
| `reconcile` | Stream all stored memories through a read-only integrity scan and propose repairs; do not apply repairs or startup migrations. |
| `migrate` or `migrate status` | Read applied/pending versions without creating a missing migration ledger. |
| `migrate up` | Apply reviewed versioned migrations, then read status using the same owned pool. Requires schema-owner privileges. |
| `migrate down pre/<version>` or `migrate down post/<version>` | Explicitly roll back one target subject to migration ordering guards. A target without its phase defaults to `post`. |

Unknown commands, extra arguments and unsupported flags such as `--dry-run` are
rejected before configuration/database startup; they never fall through to server
startup or silently authorize migrations. These direct administrator commands are
not governed by dashboard action approvals. Mutating migrations require deliberate
operator authorization, the appropriate database role and a reviewed recovery plan.

Every database command owns and closes its pool, including on query/migration/status
failure; cleanup failure produces a nonzero exit. `HAI_COMMAND_TIMEOUT` is a positive
duration with units (default `30m`), shared across acquisition and subsequent SQL.
The Compose API and migration job forward it. Expiry does not undo earlier committed
migrations and does not hard-limit CPU scanning, blocked output or driver cleanup.
Reconciliation reads one row at a time, selecting only the five fields needed for
validation. It does not retain the full corpus or findings list, and does not omit
archived or ownerless records. Findings are printed as provisional while the scan
is running; final counts appear only after every row was read, rows were closed and
the read-only transaction committed. Always check the exit status: read, conversion,
close, commit, output or cancellation errors mean the report may be incomplete.
SQL NULL confidence is not treated as a valid zero: it stops the scan for manual
source-based correction. NaN and infinities produce nonrepairable findings rather
than a proposed clamp. These commands never apply a repair.

Client memory still depends on the largest selected record and driver buffers;
database-side ordering may consume additional resources. Blocked stdout is not
forcibly interrupted, and bytes already printed cannot be retracted. The shared
server pool is not closed by these commands. Actual PostgreSQL acceptance and a
measured resource reduction remain open. See [command evidence](output/production-durability-integration-20261001.md#one-shot-command-safety-and-pool-ownership)
and [streaming evidence and limits](output/production-durability-integration-20261001.md#streaming-memory-reconciliation).

### Dashboard Flows

After authentication, an operator can:

1. Create a pursuit with an objective, desired outcome, completion definition,
   priority, risk, and autonomy setting.
2. Import authorized local/exported material, inspect extractions, and route
   actionable context into a pursuit or workflow.
3. Create plans, checklists, follow-ups, review items, and source-linked
   verification work through the workflow and task engines.
4. Review Robert-only decisions, blockers, next actions, approvals, runtime
   evidence, and completion conditions from the Command Dashboard or a pursuit.
5. Configure and probe a local model endpoint, then run a bounded validated
   task subject to the local/free policy.
6. Configure one narrow approved automation or agent runtime after its
   allowlists, workspace, timeout, and safety settings are explicitly reviewed.

The normal durable path is:

```text
assistant command, source intake, or ambient opportunity
  -> pursuit match
  -> active pursuit + persisted workflow
     or candidate pursuit + explicit acceptance
  -> bounded task plan/run
  -> verification and audit evidence
  -> completion, review, retry, or follow-up
```

Ambient opportunities use the same path. An opportunity matched to an active
pursuit may create or reuse a governed workflow. An unmatched opportunity, or
one matched only to a candidate pursuit, is recorded with its provenance and
waits for an approval-capable operator to accept the candidate. It does not
create an orphaned executable workflow.

For an open checklist item with `ReminderAt`, an authenticated owner may first
read the current reminder proposal and then append a narrowly scoped
`internal_notification` preparation request. An approval-capable owner may
append `approved`, `rejected`, `needs_clarification`, or `revoked` decision
evidence. Requests and decisions are immutable, digest-bound, owner-scoped,
idempotent, time-limited, and always return `canExecute:false`. Preparation and
approval do not create a Calendar event, send a notification, email, or other
message, call a provider, run a follow-up, or change the workflow/checklist.
After a separate owner authorization, the durable worker may emit one internal
proactivity signal and an immutable delivery receipt. It cannot deliver to
Calendar, email, chat, or any provider. Any external delivery path would still
require its own authorization, effect ledger, provider acceptance, and
postcondition proof.
The two reminder mutation routes bypass the legacy process-local
`Idempotency-Key` rejection cache and defer replay/conflict handling to the
durable owner-scoped ledger. Preparation uses the body `idempotencyKey`; a
decision uses its canonical request digest and current decision-chain tip.

If a pursuit linker is supplied without the native lifecycle router, derived
workflow creation is deferred and the source or conversation import remains
visible for repair. This fail-closed compatibility state creates no workflow;
it is not supported production wiring.

Direct `/task/*` planning and run sessions are useful for bounded operator
work. Owner-scoped completion-plan snapshots, review items, and review
decisions are persisted by `pre/0004_task_state_storage`; completion snapshots
and decisions are append-only, while review-item provenance is immutable and
only its governed state may advance. An approved review replays the exact
stored action; a validated result becomes `completed`, while an execution error
or failed validation returns the item to `needs_review`.

When a direct task is explicitly scoped to a valid pursuit, HAI also persists
a compact task-attempt projection. The pursuit/workflow ledger remains the
canonical restart-safe record for workflow-owned runs; those runs retain the
same pursuit context through planning and verification without writing a
duplicate direct task-attempt projection. Durable review storage also provides
a manual, dry-run-first reconciliation action for an item left `approved` by a
process failure. It never repeats the side effect: linked durable evidence can
close a verified completion, while an unproven outcome returns to
`needs_review`. There is deliberately no automatic recovery worker, so
operators must inspect evidence and follow the
[operator runbook](docs/operator-runbook.md).

Refreshing a pursuit summary is documentation activity, not operational
progress. It cannot reset the pursuit's last-activity signal or remove stale
work from the command dashboard.

## Safety and Ownership

- Verified owner identity is required for the personal pursuit, workflow,
  source, memory, verification, task, review, ambient, HAI OS, and runtime
  mutation APIs. Client-supplied actor or approval fields are not trusted.
- The bundled IDP persists `owner`, `operator`, and `viewer` roles and signs
  that role into access tokens. Request headers never grant a role; the seeded
  `FIRST_RUN_ADMIN_EMAIL` account is promoted to `owner`, while registrations
  default to `operator`.
- Interactive APIs use the same signed-role boundary: viewers can inspect
  owner-scoped state, operators can plan and edit it, and execution or approval
  resolution requires approval capability. HTTP sync and due-work controls are
  scoped to the authenticated owner; only in-process schedulers operate across
  owners.
- The IDP refreshes a valid refresh-token session before resolving the user on
  protected routes, and nginx relays that refreshed cookie to the browser, so
  access-token expiry does not strand an active local session on the login
  screen or send the backend a stale credential.
- Gateway API authentication failures remain JSON `401` responses. Angular's
  session guard, rather than nginx rewriting API errors to HTML, directs the
  browser to `/login` when a session is no longer refreshable.
- Session issuance requires expiring positive Redis records for the refresh
  token and its family. Rotation consumes the predecessor and atomically
  registers its replacement; logout removes family authority before recording
  revocation. Missing, evicted, malformed, or unavailable authority fails closed
  instead of treating a missing blacklist entry as permission. Cached rotation
  responses are encrypted and bound to the exact predecessor. Deploying this
  change deliberately requires existing sessions to log in again; there is no
  permissive legacy-token fallback. Restoring an old Redis snapshot is a separate
  rollback risk: invalidate sessions or rotate signing material before exposing
  a restored installation. The low-level access-token-only blacklist method is
  not eviction-resistant; production logout always revokes the entire family.
- Follow-up proposals, checklist items, workflow state, and audit receipts commit
  together in PostgreSQL. Concurrent workers and replay cannot leave a partial
  projection or reapply a completed follow-up. Claims are checked after lock
  waits and again after writes; expired workers cannot retain their changes.
- Internal reminders revalidate locked approval/source records and commit their
  proactivity signal with the delivery receipt in the same database transaction.
  A failing sink rolls back its writes before a bounded retry is recorded.
  This does not authorize calendar, email, or other external delivery.
- Owner-scoped pursuit detail, dashboards, activity, evidence, decisions, and
  links filter legacy records that are not visible to the current owner.
- Pursuit-to-pursuit relationships are owner-scoped too, so authenticated users
  cannot create or view a cross-owner case reference through pursuit metadata.
  A pursuit cannot create a self-referential relationship, and related-pursuit
  navigation is available only for records visible in the current owner's scope.
- Pursuit auto-linking and candidate creation refresh their operational summary
  inside the same authenticated owner scope, so malformed legacy links cannot
  persist another user's workflow state into a personal pursuit.
- Runtime launch and stop records retain the authenticated initiating owner.
  Owner-scoped pursuits reject unknown or other-owner runtime evidence, and
  shared automation history cannot make one operator's runtime output visible
  in another operator's pursuit.
- Direct task-attempt projections are similarly re-checked during pursuit
  aggregation, so malformed or legacy cross-owner task records cannot expose
  task summaries, review state, or blocked reasons.
- High-risk communication, legal/government, financial, account, public-post,
  deletion, destructive-file, and broad-host actions require explicit approval
  and do not run from a generic transition or chat request.
- Auto-created pursuit candidates are not active operational work. Generic
  pursuit intake, planning, task attempts, and ambient opportunity routing
  keep them out of the executable path; an approval-capable user must use the
  separate candidate-acceptance action before HAI can create or unlock the
  governed workflow path.
- An assistant command that creates or selects a pursuit candidate returns an
  auditable review handoff instead of attempting a direct task plan. It links
  the candidate back to the chat result, asks Robert to accept or archive it,
  and creates no workflow, task attempt, runtime action, or side effect before
  the explicit candidate-acceptance action.
- An assistant command that creates or reuses active pursuit work stops at the
  governed workflow ledger. The workflow worker supplies its WorkflowID to the
  task engine, so planning, retries, verification, and runtime evidence are
  recorded once on the workflow instead of also creating a duplicate direct
  task attempt from the chat command.
- Pursuit decision resolution requires approval capability both in route
  registration and in the handler. Alternate or future route wiring cannot
  turn a non-approver's request into a workflow or decision audit event.
- Source, AI-chat, and ambient producers configured with pursuit correlation
  but without the native pursuit lifecycle router fail closed. Imported signals
  and proposed ambient opportunities remain visible for repair, but no workflow
  or executable work is created. The full router is the supported production
  integration path.
- Connected-source searches and extraction lists apply source ownership and a
  fail-closed source-revocation barrier. Revoked source rows and audit history
  remain administratively inspectable, but their cached extractions and stale
  semantic embeddings are excluded immediately from task context. Lexical
  retrieval is always available; vector retrieval is claimed only when the
  configured local semantic adapter is healthy.
- The runtime registry enforces emergency stop at its own boundary, including
  direct Hermes, Odysseus, and OpenClaw registry execution calls.
- Runtime execution is constrained by enablement flags, allowlisted tools,
  hosts, paths, workspaces, timeouts, output limits, redacted audit records,
  and verification before completion.
- Built-in system processes cannot assign themselves a lower risk, authority,
  autonomy, reversibility, cost, tool, runtime, or operation classification.
  The local safe worker, task-runtime launcher, and local model-maintenance
  worker each have an exact server-owned workload policy; an unknown system
  identity or any policy mismatch is denied before Constitution, mandate, or
  approval evaluation. The matched policy ID is retained in authorization
  evidence and rechecked immediately before one-time receipt consumption.
- Mutating API, script, Docker-start, and agent-runtime actions additionally
  require an internal HMAC-signed approval proof bound to the owner, automation,
  exact action digest, scope, and recorded approval source. Proofs default to a
  five-minute lifetime, are single-use, and are issued only by the trusted
  approved task-review path. Read-only API `GET`/`HEAD` probes are exempt from
  the proof but not from ordinary authentication, enablement, allowlists, audit,
  or safety policy.
- Production approval-proof signing uses the explicit
  `HAI_APPROVAL_PROOF_SIGNING_KEY`; startup fails closed when the key is missing
  or shorter than 32 bytes. Consumption is an owner-scoped, append-only
  PostgreSQL claim, so replay protection survives restart and coordinates
  multiple backend instances. Rotating the key invalidates unexpired proofs.
- Stopping a runtime task requires an approval-capable role. Uploading,
  selecting, or refreshing the shared OpenClaw ecosystem requires an owner
  role because it changes the host-wide runtime configuration. The dashboard
  first requests a short-lived, single-use owner authorization bound to the
  exact validated action, then submits it immediately; browser input cannot
  assert an approval or reuse it for a different ecosystem change.
- The shared automation registry follows the same boundary: reads are role
  scoped, launch/stop actions require approval capability, health checks
  require write capability, and create/update/delete/reorder operations require
  an owner. Reordering uses `PATCH`, never a side-effecting `GET` request.
- Ownerless legacy workflows, sources, extractions, and imported conversation
  archives are read-compatible only for local-development compatibility.
  Authenticated users cannot adopt, delete, or mutate them. Ownerless scheduler
  work stays in-process and is not exposed as an operator action.

For route-by-route ownership behavior, see
[backend endpoint audit](docs/backend-endpoint-audit.md). For the broader
threat model, see [threat model](docs/threat-model.md).

## Deliberate Gaps

These capabilities are not bundled or live-proven by this repository:

- Live WhatsApp, browser, and other unlisted account OAuth/API integrations.
  Gmail, Google Drive, Google Contacts, Google Calendar, and Trello read-only connectors exist
  but remain unconfigured by default; every configured account still needs its
  own bounded acceptance evidence before operational trust.
- Provider webhooks, local file watchers, a dedicated vector database, generic
  MCP, QwenPaw, browser automation, and desktop-agent execution.
- Hermes, Odysseus, and OpenClaw upstream installations. HAI provides guarded
  adapters, not the upstream software or unrestricted credentials.
- Paid LLM use, public posting, financial commitments, account changes,
  deletion, and unrestricted device control.
- Distributed workers, leader election, worker heartbeats, or high
  availability. Versioned pre/post SQL migrations are implemented, but
  clean-clone and rollback acceptance still belong in each target release.
- Verified multi-user isolation on two real accounts. Owner scoping is covered
  in code and focused tests, but a real two-account exercise remains required
  before shared operation is trusted.
- A clean-machine, signed-in Windows 11 deployment journey. The local Compose
  and gateway path has been exercised; target-machine acceptance remains a
  required release gate.

The [external provider reality review](docs/external-provider-reality-review.md)
records the current integration truthfulness boundary.

## Architecture

```text
Angular dashboard
        |
nginx gateway + IDP session boundary
        |
Go API and operating engines
  |-- pursuits and workflow engine
  |-- task, approval, verification, and audit engines
  |-- memory and connected-source ingestion
  |-- local-first LLM router and provider probes
  |-- ambient planning and controlled runtime registry
        |
Postgres + Redis + optional event bus
```

The local deployment targets Windows 11 with Docker Desktop. The control-plane
backend, IDP, and nginx configuration manager use Go 1.27.2 and share an
executable CI alignment contract. They use Gin, Gorm, Postgres, and
Sarama/Kafka when the optional event-bus profile is enabled. The frontend uses Angular 22 and ng-zorro-antd 22. Use Node 24.15.0 or later within the Node 24 LTS line and npm 10.9.8 for frontend development and verification; the repository `.nvmrc` and package metadata declare the supported frontend toolchain.
Versioned SQL migrations are the schema source of truth and `DB_AUTOMIGRATE`
defaults to `false`. Startup applies pre-phase migrations, optionally runs
development-only AutoMigrate when explicitly enabled, then applies
post-phase migrations. See
[migration safety](docs/migrations.md).

## Quick Start

### Prerequisites

- Windows 11 with Docker Desktop, or another Docker Compose-capable environment.
- Git.
- Node.js 24.15.0 or later within the Node 24 LTS line, and npm 10.9.8,
  for frontend development outside Docker.
- Go 1.27.2 for control-plane backend, IDP, and nginx-config-manager
  development outside Docker. Their modules, Docker builders, and CI toolchains
  are checked for version alignment.

### Start the local stack

```powershell
./scripts/initialize-windows.ps1
docker compose --env-file .env.local -f docker-compose.local.yml config --quiet
docker compose --env-file .env.local -f docker-compose.local.yml up --build -d
docker compose --env-file .env.local -f docker-compose.local.yml ps
```

The initializer prompts for the first-run owner email and password, generates
the production signing/encryption, database, and first-run credentials, and
writes an ignored loopback-only `.env.local`. For a non-Windows shell, copy the
template, then run `./scripts/generate-secrets.sh >> .env.local` before
starting Compose. The generator prompts for the intended first-run owner email
when `FIRST_RUN_ADMIN_EMAIL` is not set; it fails instead of silently using the
template's example owner address when no interactive terminal is available.
For non-interactive setup, provide the address explicitly, for example:
`FIRST_RUN_ADMIN_EMAIL=you@example.com ./scripts/generate-secrets.sh >> .env.local`.
The appended email and generated password override their template values. Never
run the generator without redirecting its output into your ignored local
environment file.

`docker compose --env-file .env.local up --build -d` is equivalent. The
default `docker-compose.yml` intentionally delegates to the same local,
source-built stack. It does not pull the old `jacksonbarreto/*` images or start
the retired multi-broker Kafka topology.

Open [http://localhost:8088](http://localhost:8088) with the default
configuration. If you deliberately override `GATEWAY_HOST_PORT`, use that port
instead.

For explicitly configured, guarded public HTTPS access, see
[Governed ngrok cloud access](docs/ngrok-cloud-access.md). The tunnel is
disabled by default and never publishes the local gateway directly.

### Windows 11 installer

For a product-style local installation, build the Inno Setup executable and
use its Start menu shortcuts rather than manually operating Compose. The
installer keeps the source-built stack loopback-only, stores first-run secrets
outside the application directory, and refuses to start a competing HAI stack.
It also starts the separate loopback-only A2A planning connector at
`http://127.0.0.1:8091` by default. That connector is never served through the
dashboard gateway or the optional ngrok tunnel. See
[Windows installer](docs/windows-installer.md).

This describes the installation path, not release acceptance. The
[2026-09-30 installer audit](output/installer-production-audit-20260930.md)
records a stale, unsigned retained Setup artifact and bounded synthetic
validation/rollback tests, not a newly built signed installer, clean-machine
installation, or real database backup/restore proof.

### Desktop resource defaults

The ordinary local stack now applies explicit memory, CPU, and process ceilings
to every always-on service: backend, frontend, IDP, gateway, both Postgres
databases, and Redis. The Redpanda broker and nginx configuration consumer are
an opt-in `event-bus` profile, so an idle local HAI installation does not pay
for them. Optional model, evaluation, document, and agent runners remain
profile-gated and are not started by the standard command.

The limit variables are grouped in `.env.example` (`BACKEND_MEMORY_LIMIT`,
`POSTGRES_AUTOMATION_MEMORY_LIMIT`, and similar). Change them only for an
observed workload, then validate the rendered configuration before restarting:

```powershell
docker compose --env-file .env.local -f docker-compose.local.yml config --quiet
```

To enable Kafka-compatible account/event delivery and dynamic gateway
configuration deliberately, set `HAI_EVENT_BUS_ENABLED=true` in `.env.local`
and start the additional profile:

```powershell
docker compose --env-file .env.local -f docker-compose.local.yml --profile event-bus up -d
```

The owner-session preview bypass is disabled by default. First-run setup or
`scripts/generate-secrets.sh` creates a dedicated 32-byte
`LOCAL_PREVIEW_GATEWAY_SECRET`, shared only by the nginx gateway and IDP. If the
operator explicitly sets `LOCAL_LOGIN_BYPASS_ENABLED=true`, `GATEWAY_HOST_BIND`
must be a numeric loopback IP (`127.0.0.1` or `::1`). Nginx injects the secret
only on the exact preview endpoint and overwrites any client-supplied header;
the IDP also requires loopback `Host` and browser `Origin` values. Forwarded
headers cannot establish trust. The capability is advertised only to loopback
origins, and the ngrok launcher refuses to start while the bypass is enabled.
Do not enable it on a LAN- or internet-exposed installation.

The `.env.example` values for credentials and secrets are intentionally invalid
placeholders. The IDP refuses to create its first owner account from a missing,
placeholder, or too-short password, and the public ngrok launcher rejects both
placeholder database and owner credentials. If the Postgres data volume already exists,
changing first-run values does not rewrite the existing account. Do not commit
`.env.local`, Docker state, database directories, uploaded material, frontend
build output, or secrets.

### Optional Google sign-in and password recovery

The local password login works without external accounts. The login page only
offers Google sign-in or email recovery after their private credentials are set
in `.env.local`; it will not route an operator to a broken OAuth flow or claim a
reset code was delivered when no mail sender exists.

For a dedicated Google OAuth **web** client, register this redirect URI for the
local gateway:

```text
http://localhost:8088/api/v1/auth/google/callback
```

Then set `GOOGLE_OAUTH_CLIENT_ID`, `GOOGLE_OAUTH_CLIENT_SECRET`, and
`GOOGLE_LOGIN_REDIRECT_URL` in `.env.local`, and recreate the IDP container.
The Gmail, Drive, Contacts, and Calendar connected-source callback is separate. Register
`http://localhost:8088/api/v1/sources/oauth/google/callback`, set it as
`GOOGLE_OAUTH_REDIRECT_URL`, and enable both APIs you intend to use. Each source
requests only its own read-only scope. Also set independent
`HAI_OAUTH_TOKEN_ENCRYPTION_KEY` and `HAI_OAUTH_STATE_SIGNING_KEY` values; HAI
does not fall back to JWT or backend secrets. Google redirects the browser, so a
public tunnel is not required for this local callback.

For optional public access through the governed ngrok profile, register the
equivalent callback URIs for the reserved `HAI_NGROK_URL` instead. Set each
enabled callback to that exact public origin and path. The tunnel launcher
rejects localhost, a different host, or a mismatched path while a Google flow
is configured, so remote sign-in and source consent cannot fail after the
tunnel has started.

For reset emails, set `SMTP_HOST`, `SMTP_PORT`, `SMTP_USERNAME`,
`SMTP_PASSWORD`, `SMTP_FROM`, and `SMTP_REQUIRE_STARTTLS=true` in `.env.local`.
The SMTP password is passed unchanged: leading and trailing whitespace is part
of the credential, not silently removed. Follow your environment-file quoting
rules when a password contains whitespace.
Use a dedicated mailbox or provider app password over STARTTLS (typically port
587), never a primary mailbox password. Recreate the IDP container after
changing either integration:

```powershell
docker compose --env-file .env.local -f docker-compose.local.yml up -d --build idp frontend gateway
```

### Verify the local gateway

```powershell
docker compose --env-file .env.local -f docker-compose.local.yml ps
docker compose --env-file .env.local -f docker-compose.local.yml logs backend
curl.exe -i http://localhost/
curl.exe -i http://localhost/healthz
curl.exe -i http://localhost/readyz
curl.exe -i http://localhost/api/v1/llm/policy
```

Expected behavior:

- `/` serves the Angular shell.
- `/healthz` and `/readyz` reach the backend through nginx without a session.
  They are intentionally public liveness/readiness probes and return backend
  health JSON (`/readyz` uses HTTP `200` or `503` according to readiness).
- Protected engine routes such as `/api/v1/llm/policy` return `401` without a
  signed session, not anonymous application data.

If port 80 is already in use, change the nginx port mapping in
`docker-compose.local.yml` from `\"80:80\"` to, for example, `\"8088:80\"`, then
open `http://localhost:8088`.

For the target-machine acceptance sequence, use
[fresh-clone dry run](docs/fresh-clone-dryrun.md). For diagnosis, use
[troubleshooting](docs/troubleshooting.md) and the in-product support bundle.

### Import local or exported material

1. Place authorized files under `connected-sources/`.
2. Open **Connected Sources** in the dashboard.
3. Create or select an export/local-folder source and keep **Local only** enabled.
4. Use a path relative to `connected-sources/`, for example `.`.

The backend mounts this root read-only. Paths escaping it are rejected. The
general importer accepts `.txt`, `.md`, `.markdown`, `.csv`, `.tsv`, `.json`,
`.yaml`, `.yml`, and `.log`; export connectors also support `.mbox`, `.eml`,
and `.ics` within the same allowlisted root.

### Import Phase 2 local feed records

Phase 2's controlled background worker uses a separate intake boundary. Put
operator-reviewed JSON feed files under `phase2-feeds/`, set
`HAI_PHASE2_FEED_FILES` to their comma-separated filenames in `.env.local`,
then restart the backend. The feed folder is mounted read-only. Any verified
safe-worker artifact is confined to `agent-workspaces/phase2`; emergency-stop
and autonomy-mode controls persist in the named Docker volume
`018-hai-phase2-control-state` and are included in the recovery procedure.

Account Bridges uses the migrated application PostgreSQL database for registered
feeds and their audit/health records, not the read-only feed directory. Apply
the reviewed migration chain through `pre/0114_operation_source_configuration`
before running the updated API with `DB_MIGRATIONS_ENABLED=false`; the API fails
startup if the registry schema is unavailable. The existing runtime-role
provisioning sets DML default privileges for migration-owner-created tables;
actual installation grants still require operator verification. Previously lost
in-memory registrations cannot be reconstructed from this migration.

`HAI_PHASE2_FEED_FILES` entries receive deterministic, owner/workspace/path-bound
IDs. Restarting preserves an existing feed's name, enabled flag and operation
type instead of reseeding those values. Changing a configured path or scope
creates a separate registration; it does not delete the old one. Account Bridges'
enabled flag gates **manual and bulk import**, not proof of a provider-native
connection or a background schedule. Enable a source explicitly before importing
it. Stored canonical versions revoke old source authority on configuration change.

Each import first commits a `sync_started` claim. A completed attempt atomically
records the outcome and releases its matching token. `recorded: true` confirms
that outcome/audit storage, not that all items succeeded or that the provider's
entire history was imported. Failures/partial imports do not advance the
successful timestamp or returned cursor. The API reports
`syncState: "running_or_interrupted"` while a claim remains, without exposing its token. An
interrupted writer is not automatically cleared: safe operator recovery requires
confirming that the old writer cannot resume, and that recovery flow remains an
explicit production gate.

A real-store test is defined in
`backend/internal/accountfeed/registry_repository_postgres_integration_test.go`.
It checks committed reconstruction through a fresh pool, concurrent claims,
stale-token rejection, scope privacy, immutable audits, and rollback of health
when the audit insert fails. It has not been executed in this checkpoint. It is
guarded by the `integration` build tag, `HAI_ALLOW_DESTRUCTIVE_DATABASE_TESTS=true`
and `HAI_ACCOUNT_FEED_TEST_DSN` pointing to literal loopback and the exact database
`hai_account_feed_registry_test`. That guarded database is only the administration
connection: the fixture creates a unique new database from `template0`, applies
the full embedded pre/post migrations in `public`, and reopens that same owned
database to check committed records. It neither reuses nor erases existing
fixtures. Every pool verifies database identity and namespace, with bounded
connection/statement/lock timeouts and a two-minute fixture deadline. Cleanup
uses a separate bounded context and checks the newly created database's recorded
OID before dropping it, without `FORCE`, session termination or `CASCADE`.
The fixture includes disabled manual/bulk refusal and explicit re-enabling before
concurrent claims. Tagged compilation and skipped optional cases are not
real-store proof. Never point it at the HAI application database.
From `backend`, the explicitly approved test command is:

```powershell
go test -tags=integration -count=1 -timeout=60s ./internal/accountfeed -run '^TestPostgresRegistryCommittedReconstructionAndAtomicFailure$'
```

### Connect LARO case intelligence

HAI includes a dedicated `laro` read-only connected-source adapter. Create the
credential in **LARO Settings > HAI**, then set these values in HAI's protected
local environment and restart only the backend service:

```text
HAI_LARO_ENABLED=true
HAI_LARO_BASE_URL=https://your-laro-origin.example/laro
HAI_LARO_CONNECTOR_TOKEN=<one-time LARO credential>
HAI_LARO_SYNC_LIMIT=50
```

Create the source from **Connected Sources** with connector `laro`, **Local
only** disabled, and an empty sync target. The endpoint and credential remain
environment-owned rather than being stored in the source row. Sync is bounded
and cursor-based. Imported LARO records are always sensitive and review-gated;
HAI does not create automatic memory from them and has no LARO write path.

## Dashboard Entry Points

| Route | Purpose |
| --- | --- |
| `/control-center` | Primary operational overview and bounded maintenance actions. |
| `/command-dashboard` | Robert-only decisions, open loops, source-backed context, memory-derived work, and unified approval actions for pursuits and linked workflows. |
| `/pursuits` | Long-running objectives with workflow, source, memory, verification, blocker, approval, activity, and related-pursuit links. |
| `/workflow-engine` | Work queue, approvals, quality gates, interruptions, retries, follow-ups, and read-only internal reminder proposals. |
| `/connected-sources` | Source configuration, sync history, extraction inspection, reindexing, pause/resume, and revocation. |
| `/memory` | Compact memory search, correction, archive, retrieval, and export controls. |
| `/llm-policy` | Provider/model configuration, budget/policy visibility, probes, routing, and fallback history. |
| `/ambient-brain` | Proactive opportunities, scan history, need-profile preferences, and decision handoffs. |
| `/task-blueprint` | Explicit bounded task planning, execution, validation, and review. |
| `/framework-registry` | Versioned decision frameworks, owner preferences, selection evidence, and Constitution controls. |

These screens are authenticated operator surfaces. Technical logs and deep
diagnostics remain behind their relevant detail or audit views.

## API Overview

Backend engine APIs are served under `/api/v1` through the gateway. Principal
areas are:

- `/automation`: registered automations, launch/stop, health checks, and diagnostics.
- `/agent-runtimes`: runtime inventory, health, skill discovery, controlled stop, and OpenClaw ecosystem inspection.
- `/llm`: policy, probes, routing, generation, and redacted decision history.
- `/memory` and `/memory-engine`: compact memory, encrypted conversation import, search, and insights.
- `/sources`: source registry, connectors, sync, extraction management, search, and audit records.
- `/pursuits`: high-level objectives, matching, intake, navigable related-pursuit links, summary, review, decisions, evidence, blockers, next actions, approvals, activity, planning, approval-gated candidate acceptance, and an Advanced-only **Reindex life domains** maintenance action. The reindex projects already-owned canonical pursuit classifications into HAI's local whole-life index; it does not alter pursuit content, call an external provider, or execute work.
- `/workflow`: intake, state transitions, approvals, due work, follow-ups, owner-scoped reminder proposals, non-executing reminder activation request/decision evidence, quality/review state, and dashboard data.
- `/task`: bounded plans/runs, durable owner-scoped completion logs, review
  queue, and exact-action review resolution.
- `/verification`: grounded answers and verification run history.
- `/framework-registry`: catalog, owner-effective preferences, selection
  history, and Constitution lifecycle.
- `/ambient`, `/agent-cycle`, `/assistant`, and `/os`: proactive planning, controlled refreshes, command bridge, and operating-system summary.

Use the route tests in `backend/internal/router/` and each subsystem's
documentation for the current Go API contracts. In particular, the
[Framework Registry API table](docs/framework-registry.md#api) lists every
registry endpoint and permission. [docs/swagger.yaml](docs/swagger.yaml) is a
legacy IDP authentication specification; it does not describe the Go control
plane and must not be used as evidence that those routes exist.

## Controlled Models and Runtimes

### Models

The router chooses the cheapest suitable model, not mechanically the cheapest
model. Its policy prioritizes local/free availability, task difficulty,
validation, fallback history, quotas, and the daily budget. Paid calls are
disabled by default with a EUR 0 budget; request JSON cannot self-approve paid
or approval-required use.

Supported configuration families include Ollama, llama.cpp/LM Studio or other
OpenAI-compatible local servers, and configured free/freemium providers. Model
catalog entries cover Qwen, DeepSeek, Llama, Mistral/Mixtral, Gemma, Phi, and
other configured provider models. Provider status must be read as configuration
and probe history, not as a live-service guarantee.

An Ollama cloud model remains external even when requested through a local
Ollama endpoint. `OLLAMA_MODEL_IDS` alone is not proof of local inference;
cloud-tagged models require the canonical external approval and budget policy.

When model maintenance is enabled (default), the background scheduler checks
eligible configured models on a 24-hour cadence while background operations
are permitted. HAI may refresh a fixed configured Ollama tag and verify its
installed digest. Other local runtimes receive read-only availability checks;
because HAI cannot update or verify their upstream artifacts, those model
identifiers remain blocked from routing. Cloud providers receive a bounded,
read-only `GET /v1/models` catalog request when due; this sends no prompt or
inference request and never changes a hosted model. Paid-provider checks remain
behind the existing paid-usage and approval gates. Odysseus exposes a health-
only probe here, so its result confirms workspace health, not model
availability. Catalog omissions are not treated as proof that a configured
model is unavailable. Cloud results are rechecked after 24 hours, and
credential or relevant policy changes invalidate the cached check immediately.
HAI cannot confirm or install the latest version of a cloud-hosted model when
the provider does not expose that information or an update API.

### Agent runtimes

Hermes, DeepSeek Harness, Odysseus, and OpenClaw have runtime-specific adapter
and capability surfaces. A listed, configured, or approved adapter is not by
itself execution-ready: each runtime has its own implementation and safety
gates. HAI does not bundle these tools, send messages through them, control
browsers, create cron jobs, or bypass their or HAI's security boundaries.

OpenClaw harness updates have a separate [verified maintenance service and
Windows worker](docs/openclaw-maintenance.md), with per-component policies,
persisted 24-hour checks, exact-version verification and a review gate for
uncertain installations. It is enabled by default for fresh Windows installs;
the installer generates a dedicated worker token and registers a least-
privilege scheduled task only when prerequisites pass. Set
`HAI_OPENCLAW_MAINTENANCE_ENABLED=false` to opt out. Missing worker credentials
or prerequisites keep installation unavailable. Runtime Lab and System Status
expose owner-only controls. Migration `0072`, matching application versions and
controlled installation acceptance remain required; building this code alone
does not prove a live update path.
The official updater may also update OpenClaw plugins and restart its Gateway;
HAI's separate task permissions are not enabled by maintenance.

OpenClaw Companion on Windows is supported as a separate, read-only gateway
discovery path. With `OPENCLAW_AGENT_ENABLED=true`,
`OPENCLAW_GATEWAY_ENABLED=true`, and
`OPENCLAW_GATEWAY_URL=ws://host.docker.internal:18789` for a Compose backend,
also explicitly allowlist `host.docker.internal` and set
`OPENCLAW_GATEWAY_DOCKER_HOST_IPS` to the operator-verified private IP addresses
of that Docker host gateway. Every DNS answer must match a pin; empty or invalid
pins block this exception. Do not guess an address or pin another network
service. An address change requires review before reconnecting. Remote plaintext
connections and non-loopback DNS answers for `localhost` remain blocked.
HAI converts the configured WebSocket address into a strict `GET /health` probe.
It accepts only `{"ok":true,"status":"live"}`, follows no redirects, sends no
gateway token, and reports `available` rather than executable. The Companion's
WSL gateway being live does not authorize HAI to run a task, access Companion
node capabilities, or use a browser, desktop, channel, or host tool. Those
remain blocked until the existing CLI/workspace, approval-proof, and
postcondition paths are independently configured and validated. A health-only
Gateway probe does not require or block on `OPENCLAW_GATEWAY_TOKEN`; that token
is required only for the separate authenticated `operator.read` discovery path.

Set `OPENCLAW_GATEWAY_PROTOCOL_DISCOVERY_ENABLED=true` only when HAI should
also validate the unauthenticated gateway boundary. It opens the configured
WebSocket, accepts one bounded `connect.challenge` event, and closes the socket.
It does not send an authorization header, gateway token, `connect` frame, RPC,
or task. A successful challenge is retained for 24 hours as an owner- and
workspace-scoped Operation Ledger record containing only a normalized endpoint
digest, reviewed protocol schema, protocol facts, expiry, and evidence digest.
It does not retain the endpoint URL, any credential, raw frame, task ID,
prompt, owner, result, or error.

`OPENCLAW_GATEWAY_AUTH_DISCOVERY_ENABLED=true` is a separate, opt-in identity
check. It requires `OPENCLAW_GATEWAY_TOKEN`, reads the bounded challenge, sends
one Gateway `connect` request asking for exactly `operator.read`, validates the
matching `hello-ok` response, returned scope, server identity fields, and policy,
then closes the socket. With task-ledger discovery disabled, it sends no
Gateway RPC, task, tool, browser, message, node, or channel command and does
not make OpenClaw executable in HAI. An
authenticated discovery result is therefore still reported as `available`, not
execution-ready. HAI has not configured a Companion token or live-validated the
authenticated handshake on this installation. The token stays inside HAI's
bounded discovery client and is never injected into a delegated OpenClaw CLI
process. If the initial handshake reports a retryable `UNAVAILABLE` response
with a bounded retry delay, HAI waits once within the caller context and starts
one fresh read-only socket; it never retries a task, a Gateway mutation, or an
unbounded connection failure.

`OPENCLAW_GATEWAY_TASK_LEDGER_DISCOVERY_ENABLED=true` is a narrower, separate
opt-in after the authenticated identity check. HAI sends one read-only
`tasks.list` request with a fixed limit of 50 and exposes only the sampled
status counts in runtime health. An unexpired successful discovery can restore
only that bounded aggregate after a HAI restart; it does not retain or return
task IDs, titles, prompts, owners, error text, session keys, or gateway
credentials. Runtime Lab presents this separately as a task-state availability
card, so it is not confused with task authority. It cannot
start, cancel, modify, or otherwise operate on an OpenClaw task, and it does
not make the OpenClaw runtime executable in HAI.

`OPENCLAW_GATEWAY_CAPABILITY_DISCOVERY_ENABLED=true` is an independent,
authenticated, read-only capability summary. HAI requires `skills.status`,
`tools.catalog`, and `commands.list` to be advertised. It calls the first two
with empty parameters and requests `commands.list` with `includeArgs=false`.
It retains only total and eligible skill counts, total command count, and the
number of `core` and `plugin` tools. Skill names, descriptions, command names,
tool names, plugin identities, argument schemas, raw frames, and credentials are discarded before
the HAI health response and durable Runtime Lab evidence are created. A missing,
malformed, over-limit, or unrecognized-provenance response fails closed. This
never imports, installs, updates, configures, invokes, or enables an OpenClaw
capability, and does not make OpenClaw executable in HAI.

`OPENCLAW_GATEWAY_MODEL_CATALOG_DISCOVERY_ENABLED=true` is a separate,
authenticated, read-only prepared-model availability check. HAI requires
`models.list`, calls it only with `view="configured"` and `preparedOnly=true`,
and retains only sampled, available, unavailable, and unknown availability
counts. The prepared-only parameter forbids new provider discovery; HAI does
not retain model or provider identities, select a model, route an inference,
refresh OpenClaw's catalog, download a model, update a provider, or spend
money. HAI's own daily model-maintenance contract remains the sole authority
for configured provider freshness and authorised update attempts.

`OPENCLAW_GATEWAY_AGENT_ROSTER_DISCOVERY_ENABLED=true` is an independent,
authenticated, read-only agent-availability check. HAI requests only the
`agent-kind` client capability, requires `agents.list` to be advertised, and
sends `agents.list` with empty parameters. It keeps only the sampled total and
counts for `agent`, `system`, and legacy/unknown kind rows. Agent identifiers,
labels, model and runtime metadata, workspace paths, creation provenance, raw
frames, and credentials are discarded before health and Runtime Lab evidence
are created. This cannot create, update, delete, bind, route, or execute an
OpenClaw agent; it only supports an informed, later approval-gated delegation
decision.

`OPENCLAW_GATEWAY_DELEGATION_ENABLED=true` remains a separate, default-off setting, but it does not currently make new delegated execution available. HAI fails closed before opening a write connection or sending `sessions.create`: the current shared-secret Gateway handshake does not prove an authenticated durable creator identity, a `sandbox: "required"` named operator role, or the effective policy bound to the exact run. OpenClaw documents that named roles attach to authenticated durable profiles and that required sandboxing is immutable session-creation provenance; a shared token alone cannot establish that identity ([operator scopes](https://docs.openclaw.ai/gateway/operator-scopes)). A local permission mode, read-only discovery, a high-risk acknowledgement, or a `runStarted` response cannot substitute for server-enforced policy evidence.

HAI retains stop and reconciliation support for existing owner-bound Gateway receipts. It resolves stored receipt identity server-side and uses only `sessions.abort { key, runId }` or exact-run `agent.wait`; it never falls back to session-wide cancellation. Abort acknowledgement is not terminal evidence. Gateway replies, tool calls, and raw errors are not imported. New delegated work may be enabled only after HAI can authenticate a durable identity, verify its sandbox-required role and effective policy for the exact run, persist admission provenance, and pass pinned-Gateway acceptance tests. The local WebSocket fixtures are not live acceptance.

`OPENCLAW_GATEWAY_ARTIFACT_IMPORT_ENABLED=true` is a separate, default-off
post-terminal metadata handoff. It requires a configured `OPENCLAW_GATEWAY_TOKEN`
and the source-backed completed run receipt, then asks for exactly
`operator.read` and calls only `artifacts.list { runId }`. HAI verifies the
returned run provenance and retains at most 20 descriptors as a one-way digest,
type, MIME type, and byte count in a private receipt ledger. Artifact IDs,
titles, source strings, URLs, downloaded bytes, transcripts, and raw Gateway
responses are discarded. It never calls `artifacts.get` or `artifacts.download`.
If the initial `operator.read` handshake explicitly returns retryable
`UNAVAILABLE` with a bounded delay, HAI opens one fresh read-only socket before
it sends `artifacts.list`; the metadata request itself is never replayed by
connection recovery. An unavailable metadata handoff never changes the terminal
run outcome.

An owner can separately request stored artifact metadata through
`GET /api/v1/openclaw-artifacts/:eventId` and file content through
`GET /api/v1/openclaw-artifacts/:eventId/:digest/download`. The Command Center's
automation diagnostics expose these through an initially collapsed **OpenClaw
files** section for each OpenClaw execution. Metadata loads only on expansion;
content requires a separate download click. The browser checks the returned
content SHA-256 and size before requesting a binary download, and cancels obsolete
requests when the execution changes, the section closes, or the view is destroyed.
These authenticated, read-permission routes retain the same owner/task/run
binding; fresh runtime retrieval additionally requires the artifact-import switch.
The content route uses native `artifacts.download`, limits content to 8 MiB and
two concurrent requests, records a content checksum and audit, and returns a
binary attachment. Native URL mode requires an exact origin in
`OPENCLAW_ARTIFACT_DOWNLOAD_ORIGINS` (empty by default). Downloads do not forward
Gateway credentials, follow redirects, or use proxy settings; DNS addresses,
expiry, response size and TLS are checked. Use dedicated artifact-serving
origins. With `OPENCLAW_ARTIFACT_RETENTION_KEY` configured, **Retain in HAI**
explicitly saves an encrypted copy through an owner-scoped, write-permission
POST. Retention is capped at 64 MiB or 128 files per owner (8 MiB per file),
is idempotent, never overwrites a different retained version, and commits its
audit atomically. Downloads prefer retained bytes and can work without the
upstream Gateway. The expanded file details show database-backed owner storage
usage and offer a confirmed **Remove HAI copy** action. It uses the reviewed
content checksum as a strong precondition, removes only HAI's encrypted copy, and
records the removal atomically; source files and history remain. There is no
automatic eviction. Back up the dedicated key with the database; replacing it
does not rotate existing ciphertext. New retention writes require both the
dedicated key and `OPENCLAW_ARTIFACT_RETENTION_KEY_CONFIRMED=true`. Generate a
dedicated value with a cryptographically secure random source (for example,
`openssl rand -hex 32`), keep it private, and set the confirmation only after
checking its provenance. HAI rejects obvious low-diversity placeholders, but
cannot prove how a key was generated. Confirmation is separate from the key
bytes, so existing ciphertext remains decryptable. Older `random-v1:`-prefixed
configurations remain readable through a compatibility path; do not add or
remove prefixes as a key-rotation method. Retained files remain unverified;
verified-deliverable linkage, key rotation tooling and live provider acceptance
are still outstanding. See [native content download boundaries](docs/external-runtime-feature-parity.md#owner-scoped-native-content-downloads-2026-09-06).

The ordinary metadata-import boundary rejects any receipt that is not already
recorded as a completed terminal receipt. During durable reconciliation, the
worker can pass only its just-observed completed Gateway result through a
narrower internal handoff; pending, failed, malformed, or task-mismatched
observations cannot reach `artifacts.list`.

The scoped `operator.write` client retains bounded handshake recovery for
owner-bound stop and reconciliation of previously persisted runs. It never
automatically repeats `sessions.abort` or `agent.wait`. New session creation is
blocked before a write socket is opened until identity-bound sandbox-policy
attestation is implemented; local protocol fixtures do not change that gate.

DeepSeek Harness (DSH) is integrated as an HAI architecture/runtime adapter:
the runtime registry, capability metadata, approval-aware dispatch contract,
host-job model, and Windows bridge design are present. This is not production
task-execution support. Production execution is hard-disabled in the DSH
adapter, host-runtime service, and native bridge; no environment setting,
approval, version pin, or bridge token overrides that gate.

Two security prerequisites remain unmet. First, the configured host bridge uses
plain HTTP on loopback. Its bearer token can authenticate a worker to the
gateway, but the worker cannot authenticate the gateway's identity; a local
process impersonating the loopback listener remains an unresolved threat. A
loopback-only bind limits remote exposure but does not establish server
identity. TLS with verified peer identity/pinning and a reviewed credential
lifecycle is required before this transport can be trusted. Second, Windows
Job Objects provide process-lifecycle containment (including bounded process
tree termination), not least-privilege sandboxing: they do not restrict file,
network, registry, or credential access, and a DSH plugin or wrapper can launch
WSL work outside that Windows process boundary. A real OS-enforced sandbox and
native Windows denial tests are required before dispatch can be enabled.

The bridge's production configuration, polling, execution, and version-check
entry points currently fail closed before contacting the gateway or launching
`dsh`. Backend enqueue, lease, and launch-confirmation boundaries are also
unavailable by default. Do not set `HAI_HOST_RUNTIME_BRIDGE_ENABLED`,
`DEEPSEEK_HARNESS_ENABLED`, or `DEEPSEEK_HARNESS_EXECUTION_ENABLED` to `true`,
start the `local-host-runtime` profile, or supply a bridge token as a workaround.
Those values cannot make this path safe or executable. Keep the example values
disabled; see [Optional Runtime Profiles](docs/optional-runtime-profiles.md#deepseek-harness-host-runtime-hard-disabled).

Process status is not task verification. A zero exit code is recorded as
`process_succeeded_unverified`; it does not update the automation's last-success
state or establish that the requested deliverable is correct. Independent,
source-aware verification must establish task completion. Safe future activation
therefore requires, at minimum, authenticated bridge transport, a real
OS-enforced least-privilege sandbox with native Windows acceptance evidence,
and an independent task-outcome verification path. Until then, HAI may expose
DSH architecture/readiness information, but must not dispatch production DSH
tasks. The status is an HAI integration boundary, not a claim about the current
availability or security of the upstream DSH release.

API, script, and Docker adapters have the same default posture: disabled until
explicitly allowlisted and configured. The emergency stop blocks runtime
registry execution even when an adapter is invoked directly. Mutating API,
script, Docker-start, and agent-runtime actions also require the internal
action-bound approval proof described above. The proof is issued only from an
approved task review and is validated before network, process/filesystem,
Docker-socket, or agent-runtime access. Direct mutating launch requests
therefore block; read-only API `GET`/`HEAD` probes remain available within the
normal access and allowlist policy.

### Local provider fixture

For a controlled HTTP compatibility check without downloading a model or
contacting a provider, the optional `provider-fixture` Compose profile serves
both Ollama discovery (`/api/tags`) and OpenAI-compatible discovery
(`GET /v1/models`) plus deterministic generation-shaped responses. It is not
an LLM and is never started by the normal local stack. It has no host port,
read-only storage, no Linux capabilities, and a 32 MB / 0.10 CPU / 32 PID
limit.

Use it only in an isolated test configuration where a test-only provider is
explicitly pointed at `http://provider-fixture:11434`:

```powershell
docker compose --env-file .env.example --profile provider-fixture -f docker-compose.local.yml up --build provider-fixture
```

This validates only HAI's network compatibility with a deterministic local
service. It is not evidence that Ollama, LM Studio, a cloud provider, or a
model has been installed or accepted for real work.

## Developer Checks

```powershell
# Backend (use Docker when Go is not installed locally)
docker run --rm -v hai-go-module-cache:/go/pkg/mod -v "${PWD}/backend:/workspace" -w /workspace golang:1.27.2 go test ./...
docker run --rm -v hai-go-module-cache:/go/pkg/mod -v "${PWD}/backend:/workspace" -w /workspace golang:1.27.2 go vet ./...
docker run --rm -v hai-go-module-cache:/go/pkg/mod -v "${PWD}/backend:/workspace" -w /workspace golang:1.27.2 go build ./...

# Identity service (Go 1.27.2)
Set-Location idp
go vet ./...
go test ./...
go build ./...

# Nginx configuration service (Go 1.27.2)
Set-Location ..\nginx-config-manager
go vet ./...
go test ./...
go build ./...

# Frontend
Set-Location ..\frontend
npm.cmd ci
npm.cmd run build
npx ng test --watch=false --browsers=ChromeHeadlessNoSandbox

# Compose contract
Set-Location ..
docker compose --env-file .env.example -f docker-compose.local.yml config --quiet
```

With the matching local Go toolchains installed, run the backend commands from
`backend/`, and the IDP and nginx-config-manager commands from their respective
directories. These are the same build-and-test surfaces required by CI. The
critical-path smoke is `scripts/smoke-critical-path.sh` from a Bash-capable
shell with its prerequisites. The CI definition includes a temporary production-mode
Compose browser gate. The
[CI isolation report](output/ci-isolation-production-20260930.md) now records
guarded-harness wiring and 76 passing local contracts; hosted CI remains
unverified. The browser suite covers
login, owner-scoped source intake, governed workflow approval, and one verified
read-only backend-health execution. It uses CI-only values and does not
authorize external providers or mutable external actions. The suite creates
temporary local records and requires an explicit `E2E_ALLOW_MUTATION=true` flag;
use it only against a disposable acceptance stack prepared with the reviewed
manual-local mode in the [isolated acceptance guide](docs/isolated-acceptance.md).
Successful queue submission is not source completion: the operator test must
poll its exact owner-scoped job to `completed` and verify the same source's
synthetic extraction before continuing.

The repository's verification evidence is in:

- [completion matrix](docs/codex-goal/completion-matrix.md)
- [final verification report](docs/codex-goal/final-verification-report.md)
- [fresh-clone dry run](docs/fresh-clone-dryrun.md)
- [external provider reality review](docs/external-provider-reality-review.md)
- [technical debt](docs/technical-debt.md)

These reports distinguish exercised local behavior from unproven real-world
integrations. Treat target-machine and provider-specific checks as release
gates, not paperwork.

## Repository Layout

```text
backend/                 Go API and HAI engines
frontend/                Angular dashboard
idp/                     Identity provider service
nginx-config/            Gateway configuration used by local Compose
nginx-config-manager/    Generated route-config manager; Docker socket disabled by default
automation-scripts/      Read-only allowlisted script mount
connected-sources/       Read-only local/export ingestion root
phase2-feeds/            Read-only Phase 2 JSON feed intake root
agent-workspaces/        Bounded local safe-worker output root
browser-extension/       Explicit user-authorized conversation capture
scripts/                 Smoke and operational verification scripts
docs/                    Architecture, runbooks, evidence, audits, and roadmap
.github/workflows/       CI pipeline
docker-compose.local.yml Windows/local-first Compose topology
.env.example             Environment template; copy to untracked .env.local
generic-auto/            Legacy service, not the canonical HAI engine
gate/                    Legacy gateway/config area; local Compose uses nginx-config/
```

## Further Documentation

- [Operator runbook](docs/operator-runbook.md)
- [Scheduled model maintenance scope and limits](docs/model-maintenance.md)
- [Runtime hardening checks and remaining acceptance (2026-09-30)](docs/verification-2026-09-30-runtime-hardening.md)
- [Isolated acceptance and manual-local opt-in](docs/isolated-acceptance.md)
- [Framework Registry and task approval contract](docs/framework-registry.md)
- [User guide](docs/user-guide.md)
- [HAI Personal AI Operating System blueprint](docs/hai-personal-ai-operating-system.md)
- [Universal task success engine](docs/universal-task-success-engine.md)
- [Connected-source ingestion](docs/connected-source-ingestion-extraction.md)
- [Verification and anti-hallucination policy](docs/anti-hallucination-verification.md)
- [Source-grounded answer engine](docs/source-grounded-answer-engine.md)
- [Automation Control Center](docs/automation-control-center-blueprint.md)
- [Release process](docs/release-process.md)
- [Privacy impact assessment](docs/privacy-impact-assessment.md)

## License

See [LICENSE](LICENSE).
