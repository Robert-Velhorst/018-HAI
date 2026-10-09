# External Runtime Feature Parity

## Purpose

HAI treats OpenClaw, Hermes Agent, and Odysseus as subordinate capability
providers. They do not own pursuits, plans, policy, approval, canonical state,
verification, audit, or completion. PostgreSQL and the HAI Operation Ledger
remain authoritative.

## Integration Scope Clarification (2026-09-05)

Robert's requested end state is reuse of OpenClaw infrastructure throughout
HAI, not merely discovery or a parallel set of HAI-native equivalents. Native
HAI planning, memory, scheduling, and model routing do not by themselves prove
OpenClaw integration. Their upstream wiring remains backlog until the actual
call, owner/authority binding, persisted result, and recovery path are verified.
Host tools remain in scope through upstream tool policy and sandbox controls;
unrestricted host execution is not enabled. Installation, configuration,
updates, and operational controls also remain in scope beneath HAI's interface.

Prefer supported upstream APIs/libraries, then a thin adapter, then a selective
licensed port. Keep HAI's goals, approvals, budgets, canonical records, and final
verification authoritative without rebuilding upstream tool implementations.
This reuse-first rule applies equally to Hermes Agent: upstream infrastructure
is an implementation foundation and blueprint, not merely inspiration for a
second implementation. Before adding custom infrastructure, record why a
supported upstream capability or existing adapter cannot meet the requirement.
HAI's distinct product purpose remains personal operations across goals and
projects. Claims of improvement require evidence such as less operator work,
reliable execution, recoverable failures, and effective user control; a larger
codebase or more listed integrations is not evidence of a better product.
Backlog entries neither grant execution authority nor turn on any runtime,
provider, channel, scheduler, or host access.

The authenticated `GET /api/v1/runtime-lab/feature-parity` endpoint is the
machine-readable inventory. It accounts for every required analysis area for
each runtime and fails validation when an area is missing, an exclusion lacks a
reason, or a deferred/externally blocked feature lacks a priority,
requirements, and recommended implementation path.

Reading the inventory performs no network request, installation, configuration,
probe, self-test, or execution.

`GET /api/v1/runtime-lab/capabilities` projects the viable integration points
into HAI-native capability cards. Each card declares its input/output schema,
authentication state, availability, runtime location, required authority, risk,
EUR cost ceiling, context cost, timeout, retry behavior, reversibility,
approval requirements, verification method, and evidence. All external cards
start with `canInvoke:false`, `canExecuteExternalEffect:false`, and
`authority:contract_only`. After an owner explicitly configures an allowlisted
endpoint and runs a schema-valid probe, only the corresponding read-only
discovery card may return `canInvoke:true`. External effects remain false.

## Reviewed Upstreams

### DeepSeek Harness: Architecture Adapter, Execution Disabled

HAI contains a DeepSeek Harness runtime adapter and architecture/capability
metadata, together with approval-bound host-dispatch and reconciliation
contracts. This is an adapter integration, not parity with an executable DSH
runtime and not evidence of a live task run. The production adapter reports the
runtime blocked; the host-runtime service blocks enqueue, lease, and launch
confirmation; and the native Windows bridge's production entry points return
a hard-disabled result before polling the gateway or launching DSH. Approval,
version pinning, feature flags, and a bearer token cannot override these gates.

Production execution must stay disabled until both security boundaries are
implemented and accepted. The bridge currently targets plain HTTP loopback: the
bearer token does not authenticate the gateway to the Windows worker, so local
loopback listener impersonation is unresolved. Require verified TLS peer
identity/pinning and a reviewed token lifecycle. The Windows Job Object is
process-lifecycle containment only; it does not enforce file, network, registry,
or credential restrictions, and it does not contain WSL launched through a DSH
plugin/wrapper. Require a real OS-enforced least-privilege sandbox, with native
Windows negative acceptance tests for those boundaries. See [Optional Runtime
Profiles](optional-runtime-profiles.md#deepseek-harness-host-runtime-hard-disabled).

A process exit is not an independently verified task outcome. HAI records exit
code zero as `process_succeeded_unverified`; it does not mark the automation's
last-success state or prove that a requested artifact is correct. An
independent, source-aware verification path is a separate completion gate even
after transport and sandbox work is accepted. Do not treat local fixtures,
cross-compilation, configuration, or a queued host-job record as live DSH
acceptance.

### Native Artifact Metadata Contract And Storage (2026-09-05)

The current upstream [artifact schema](https://github.com/openclaw/openclaw/blob/main/packages/gateway-protocol/src/schema/artifacts.ts)
and [handler](https://github.com/openclaw/openclaw/blob/main/src/gateway/server-methods/artifacts.ts)
were inspected directly. The list response requires an artifacts array; a title
is Unicode text, and sizeBytes is optional. HAI's earlier adapter incorrectly
rejected Unicode titles, treated absent sizes as measured zero, and treated
missing/null arrays as successful empty results. It also counted duplicate IDs.

The parser now accepts bounded Unicode titles without storing them, preserves
new unknown sizes as null, distinguishes explicit zero, rejects invalid arrays,
duplicates, wrong-run descriptors and invalid sizes, and bounds payload size.
Original IDs, titles, URLs and contents still remain outside this metadata path.

The artifact ORM model now names the canonical migrated table explicitly.
Migration `0078_openclaw_artifact_alignment` retains and copies any legacy
`open_claw_gateway_artifact_receipts` records, refuses conflicting histories,
retains the session foreign key, and permits null for new unknown sizes.
Historical numeric values are preserved exactly: a legacy zero does not prove
that the upstream measured an empty file. No historical measurement is invented.
Stop old workers before migrating; automatic rollback refuses to hide canonical
history behind the old table name.

The isolated PostgreSQL integration test passed for legacy retention, repeated
migration, ORM writes to the canonical table, nullable size versus measured zero,
duplicate import, orphan rejection, conflict refusal and rollback refusal.
This is source-contract and local-storage proof, not live artifact download or
end-user acceptance. Full artifact content retrieval and attachment to verified
HAI deliverables remain in the integration backlog. Bounded inline downloads
are wired separately below. Durable metadata recovery is
implemented as described below; it does not download artifact content.

### Durable Artifact Metadata Recovery (2026-09-05)

The existing OpenClaw reconciliation worker now collects metadata after storing
the completed terminal receipt. The production repository uses a separate
`openclaw_gateway_artifact_collections` record (migration `0079`) to retain retry
state across restarts. Legacy synchronous importer callers remain compatible;
the durable path does not run both import mechanisms for the same settlement.

Each pass claims at most ten due receipts and has a two-minute collection budget.
Parent-row locks with `SKIP LOCKED` prevent competing claims. Each receipt gets
eight attempts, with delays from 15 minutes up to 24 hours. A disabled artifact
import switch, missing configuration or an already-active emergency stop prevents
claiming. Exhausted collections retain their history and require operator review;
there is no automatic reset of the attempt ceiling.

Migration `0098` adds a three-minute durable lease and a unique fencing token to
each claim. Active metadata reads report `collecting`, including the eighth
attempt, rather than prematurely reporting exhaustion. Read failures release
only the matching token and preserve the scheduled backoff; crashed workers can
be reclaimed after lease expiry, and a late result from a released or replaced
claim cannot commit. The read budget remains two minutes, shorter than the lease.

The registry reuses the existing native `artifacts.list` reader with
`operator.read` and the exact stored run ID. It does not invoke an agent, download
contents, or write descriptors itself. The repository validates the owner, task,
run, session and matching automation event, then commits descriptors, collection
completion and one `observed` audit event in one transaction. A stale attempt
cannot write either metadata or an audit event. A valid empty list is captured
as zero descriptors; a failed/malformed read is not treated as an empty result.

The isolated PostgreSQL recovery test passed for captured metadata, an empty
result, and the eight-attempt ceiling. It exercises four concurrent claimants,
stale claims, owner/automation mismatches, forced audit failure with transaction
rollback, and duplicate completion. This is database and protocol-fixture
evidence, not live OpenClaw/provider or downloaded-deliverable acceptance.

This does not change HAI deliverable verification, billing or execution status.
It is not live deployment acceptance and does not grant new runtime authority.

### Owner-Scoped Native Content Downloads (2026-09-06)

HAI now exposes stored metadata at `GET /api/v1/openclaw-artifacts/:eventId`
and an explicit content request at
`GET /api/v1/openclaw-artifacts/:eventId/:digest/download`. Both require an
authenticated owner and read permission. Receipt, launch event, task and owner
must agree; the download also requires the persisted artifact descriptor.

The adapter resolves the private native ID through `artifacts.list`, checks
unchanged metadata and the stored run/session, then uses native
`artifacts.download` with that run and ID. The upstream
[download schema](https://github.com/openclaw/openclaw/blob/main/packages/gateway-protocol/src/schema/artifacts.ts)
supports inline base64 or a URL. HAI retrieves either mode up to 8 MiB.
URL mode requires an exact origin in `OPENCLAW_ARTIFACT_DOWNLOAD_ORIGINS`;
the empty default denies every URL. A denied origin returns
`download_origin_not_authorized`, not its private URL. Unsupported/oversized
content also returns an explicit error, never an empty successful file.

Origins include scheme, hostname and port, not a path, query or wildcard. Use
dedicated artifact-serving origins: approving an origin permits artifact GETs
at any path on it. HTTPS is required except for explicitly configured loopback
HTTP origins. Domain names must resolve only to public unicast addresses;
`localhost` must resolve only to loopback. Internal services require an explicit
literal-IP origin. Metadata, link-local and reserved address ranges remain
blocked even when listed. The dialer validates every resolved address and
connects directly to a validated IP, preserving normal TLS certificate checking.
The standard NAT64 prefixes (`64:ff9b::/96`, `64:ff9b:1::/48`) and 6to4
(`2002::/16`) are also denied, including when explicitly listed. This is a
conservative transport restriction, not support for every translation topology;
use a directly reachable origin instead. Prefix assignments were checked against
the [IANA IPv6 registry](https://www.iana.org/assignments/iana-ipv6-special-registry/).
No proxy, cookies, Gateway token, Authorization header, redirects, transparent
compression, or request replay is used. Signed query strings stay inside the
request, outside persisted receipts, audit messages and error responses.
The smaller of the runtime timeout, URL expiry (when supplied), and 30 seconds
bounds retrieval. Declared and observed size limits are enforced.

Two content requests may run concurrently per handler. Metadata connections
retain their 64 KiB limit; only the dedicated download connection admits the
bounded larger frame. Cancellation closes that connection. Emergency-stop and
artifact-import configuration are rechecked before the content request.

Returned bytes have a separate content SHA-256 and an observed audit event.
They are served as a non-cacheable, `nosniff`, binary attachment with a synthetic
filename, not rendered, executed or stored in general runtime JSON. Audit
failure prevents the HTTP success response. This does not prove that a browser
received the file or that its contents satisfy a HAI deliverable.

The API and Command Center automation-diagnostics download control are wired.
Each OpenClaw execution has an initially collapsed files section, independent
of usage. Metadata loads on expansion; bytes only on explicit download. The
browser checks content size and SHA-256, uses a synthetic binary filename,
and cancels transfer or suppresses pending checksum results on record change,
collapse, or destruction. Unknown sizes, incomplete collection, exhausted
retries, download policy refusals and temporary failures have distinct feedback.
Runtime content is not rendered as HTML or executed.

Verified-deliverable linkage and live-provider acceptance remain open.
Browser fixture tests do not establish those gates. Operators must configure
real download origins before URL-mode acceptance can be exercised live.

Validation for this UI increment: 19 focused Angular tests passed across files,
usage, and Control Center, including checksum refusal, obsolete transfers,
collapse cancellation, invalid metadata, collection failure and unknown sizes.
A Chromium run against the development preview at 1440x1000 and 375x812 used
synthetic API fixtures to click through the actual dashboard, download and
compare bytes, verify the synthetic filename, redact an unsupported-download
error, and exercise keyboard disclosure. These checks are not a live OpenClaw
provider test or full-application accessibility certification.

URL transport validation (2026-09-06): `go test -p 2 ./internal/agentruntime
./internal/openclawreconcile ./internal/router -count=1` passed. Local native
WebSocket/HTTP fixtures exercised bytes and URL downloads, wrong-owner refusal,
expired/disallowed URLs, and metadata changes between listing and download.
Regression tests first demonstrated acceptance of three IPv6 translation
addresses and changed inline MIME metadata; both were fixed and retested.
Transport cases cover redirects, declared and streamed oversize bodies,
truncation, size mismatch, encoding refusal, and cancellation during body read.
HTTP handler cases verify checksum/audit failures prevent attachment success.
These tests use controlled local servers and do not establish remote-provider
compatibility or exhaustive network-policy coverage.

The dashboard fixture smoke check was rerun at both widths above and passed.
The production frontend build passed (the existing Pursuits stylesheet budget
warning remains), as did Compose validation with `.env.example`. No live
download origins were enabled, no deployment was changed, and no database
retention or verified-deliverable gate was completed by this increment.

### Explicit Encrypted Artifact Retention (2026-09-06)

Migration `0080_openclaw_artifact_retention` adds owner-bound encrypted copies
without changing the metadata collector. `POST
/api/v1/openclaw-artifacts/:eventId/:digest/retain` requires authenticated owner
and write permission. The Command Center files section offers **Retain in HAI**
only when the dedicated `OPENCLAW_ARTIFACT_RETENTION_KEY` is configured. Viewing
or downloading alone never retains content. An interrupted POST may have already
committed: refresh its stored state before retrying.

Content is AES-256-GCM encrypted with a domain-separated derived key and a random
nonce. Owner, execution, artifact fingerprint and content checksum are authenticated
along with the bytes. Raw native URLs, IDs, names and plaintext content remain
outside the archive table and public JSON. A retained copy is explicitly
`unverified`, never a verified memory, deliverable, or completed workflow.

The storage transaction rechecks the owner/event/artifact binding, serializes
per-owner quota decisions with a database advisory lock, and commits the content
and observed audit together. Quotas are 64 MiB of plaintext content or 128 files
per owner, with 8 MiB per file. Repeated identical writes return the same record;
different content for an already retained reference is refused, not overwritten.
Download requests prefer the retained copy; an unreadable/corrupt copy fails
instead of silently replacing it from the runtime.

`GET /api/v1/openclaw-artifacts/:eventId` reports account-wide retained bytes and
file counts from the database. In the expanded file details, **Remove HAI copy**
requires confirmation and sends the reviewed content SHA-256 as a strong `If-Match`
validator. A stale version is rejected. The transaction removes only the HAI
ciphertext and appends its audit event atomically; OpenClaw files, source receipts,
and prior audit history remain. The refreshed response reports the new totals.
There is no automatic eviction. Keep the key with the database. Key
rotation/re-encryption and verified-deliverable linkage remain unfinished. The key
is empty by default; no production database or deployment was changed for this increment.

Validation: real isolated PostgreSQL tests exercised encryption, restart reads,
owner and execution binding, concurrent idempotence, the byte/count quotas,
wrong-key refusal, version preservation, audit rollback and HTTP retention/read
without a Gateway downloader. The cross-execution regression failed before the
binding fix and passed afterward. A second isolated database applied all 80 pre
and 4 post migrations, reapplied with zero changes and refused destructive
rollback. Ten focused Angular tests and a desktop/mobile Chromium fixture run
passed, including explicit retention and refreshed persisted status. The frontend
production build and Compose configuration validation passed; the existing
Pursuits stylesheet size warning remains. These are local tests, not acceptance
against an external OpenClaw deployment or a claim of verified deliverables.
For the subsequent removal/usage change, the backend package build and Go package
suite passed, and the Angular production build passed. The updated Angular tests
compiled but Karma's Chrome process did not connect to the test server. A separate
Chromium click-through with synthetic API responses exercised retaining, cancelling
the confirmation, deleting with the reviewed `If-Match` value, refreshing owner
storage totals, and preserving the source row at 1440px and 375px. The isolated
PostgreSQL deletion/usage integration test passed, including audit-failure rollback.
These browser fixtures do not establish live OpenClaw provider acceptance.

### Explicit OpenClaw Start Model (2026-09-05)

An automation may store `runtimeModel` as an exact `provider/model` reference.
The automation editor includes this optional field for controlled OpenClaw
execution. Registration, updates and reads preserve it; migration
`0073_automation_runtime_model` adds its database column.

The selected reference is included in both the action approval digest and the
consumed final-effect authorization binding, then passed as the native
`sessions.create.model` parameter. Changing the stored model invalidates the
previous action approval. `OPENCLAW_GATEWAY_ALLOWED_MODELS` is a comma-separated,
case-sensitive allowlist of exact references. A nonempty selection requires
Gateway delegation; the CLI route must not silently ignore it. Missing model
selection preserves the Gateway default for existing automations.

This reuses the [upstream session API](https://docs.openclaw.ai/gateway/protocol),
not a new provider implementation. It selects the **start model**, not a promise
that every subsequent call uses that model: Gateway fallback configuration
still applies. Per-run actual model/fallback receipts, token/cost reconciliation
and HAI budget enforcement remain integration work. No provider call, new
allowlist entry, live deployment or paid execution is enabled by this change.

Verification for this increment: Go package tests for agentruntime, automation,
models, executionauth, router and migrations passed; six targeted Angular
tests and the production build passed. A separate browser session at
`http://127.0.0.1:14208/home` used intercepted test API responses to verify
opening the automation editor, selecting OpenClaw and controlled execution,
entering the model and including it in the registration request. The field was
inspected at 1440px and 390px. This is not live database or provider acceptance.

### Session Instance Provenance And Receipt Storage (2026-09-05)

HAI now retains the native `sessions.create.sessionId` and requested start model
in the existing private execution receipt, in addition to the session key and
run ID. The database representation omits both new fields from JSON output.
Migration `0074_openclaw_session_instance` adds the columns. Legacy receipts
remain readable with blank instance/model values; no historical attribution is
fabricated. Missing instance IDs from older Gateway responses remain compatible,
but cannot support instance-level usage attribution.

An ORM/schema audit identified a pre-existing naming mismatch: GORM used
`open_claw_gateway_session_receipts`, while SQL migrations created
`openclaw_gateway_session_receipts`. The model now explicitly uses the latter.
Migration `0075_openclaw_receipt_table_alignment` copies legacy receipts into
that canonical table, retains the original table, and aborts on conflicting
histories. Identical execution references are not duplicated. Automatic rollback
is deliberately refused because reverting the model without reconciling new
receipts would hide execution history.

Deployment requires stopping old backend workers before applying these migrations
and starting the new backend, so an old process cannot keep writing to the retained
legacy table. This checkout change has not been deployed to the live HAI database.

The stored requested model is **not** an actual-model measurement. OpenClaw's
[usage documentation](https://docs.openclaw.ai/concepts/usage-tracking) distinguishes
session-derived tokens/estimated costs from provider-reported billing. A native
session usage snapshot is now captured as described below. Fallback attribution,
per-call model attribution and budget-ledger reconciliation remain outstanding.
The identity fields themselves are not token or cost accounting.

Verification: regression suites for agentruntime, openclawreconcile, automation,
models, executionauth, router, and migrations passed. A separate opt-in PostgreSQL
test used an isolated database to verify legacy-copy preservation, repeatable
alignment, new receipt write/read with session/model fields, and conflict refusal
without overwriting canonical history. The disposable PostgreSQL container was
stopped afterwards. This is storage/protocol-contract evidence, not live provider
usage acceptance or a deployed release.

### Native Session Usage Snapshot (2026-09-05)

After `agent.wait` verifies a completed or failed run, the adapter reuses the
existing read-only Gateway connection boundary to request `sessions.usage` for
the stored session key. The request specifies instance grouping, no historical
family aggregation, one result, no context weights, and UTC calendar dates from
admission through terminal completion. It requires the existing read token and
the Gateway-advertised method; no new credentials or inference calls are created.

The result must match both the stored key and session instance ID (including the
nested usage ID). A cached snapshot older than terminal completion, future-dated
snapshot, wrong date range, duplicate/mismatched instance, missing counter, or
invalid numeric value is rejected. Input, output, cache-read, cache-write and
reported total tokens are preserved as distinct counters. An estimate is labelled
USD and not billing; missing prices are unavailable, never a fabricated zero.
This follows the installed OpenClaw usage handler and its
[pricing-unit documentation](https://docs.openclaw.ai/reference/token-use).

The bounded numeric summary, source method, observation timestamp, and date scope
flow through the existing terminal event audit persistence. Session IDs, transcript
paths, origin labels, messages, tools and raw payloads are not imported. This is a
**session-instance date-window snapshot**, not a per-run bill or budget debit.
Usage failure does not change a verified terminal outcome. If usage is unavailable
at settlement, the audit records that fact; the recovery worker below can later
capture an available snapshot. There is no new budget-counter UI in this increment.

Verification: a local WebSocket contract server exercised terminal verification,
read-only usage request, matching/reused session identities, and private-data
exclusion. Parser tests covered missing, negative, fractional, stale, future,
duplicate and wrong-scope data, plus known zero versus missing pricing. The
reconciliation service test verified audit propagation and single-event settlement.
All six package suites (agentruntime, openclawreconcile, automation, router, models,
migrations) passed. No live provider request, database deployment, commit or push
was performed for this increment.

### Durable Usage Recovery (2026-09-05)

Migration `0076_openclaw_usage_recovery` adds capture state, retry count and next
attempt time to the existing private Gateway receipt table. The existing terminal
reconciliation scheduler also visits eligible completed/failed receipts, up to
ten usage claims per pass. This is not a second execution scheduler and never
restarts a model task. Missing session-instance IDs remain ineligible rather than
being guessed from mutable session keys.

PostgreSQL row locking with `SKIP LOCKED` and a persisted next-attempt time prevent
concurrent claims. Attempts use increasing delays from 15 minutes to 24 hours,
with a maximum of eight attempts including interrupted claims. The retry counter
survives process restarts. A paused emergency stop or missing read-only Gateway
configuration prevents claims instead of consuming attempts in the background.
After eight unsuccessful attempts, automatic recovery stops; operator remediation
and a user-facing retry/reset control are not yet implemented.

The worker checks the original owner-bound automation link before reading native
session usage. A successful summary and its `agent_runtime_openclaw_usage` event
commit in one transaction. A stale attempt cannot replace a newer claim, and a
repeat completion cannot insert another usage event. The event has status
`observed`, not a new execution success. It records a session snapshot, not a
per-run bill or budget debit. Private receipt state is excluded from JSON output.

This capture is an auditable session snapshot, not a budget ledger. Migration
`0077_openclaw_usage_snapshot` adds typed numeric storage as described below.
It can supplement an earlier terminal audit snapshot; neither audit
entry should be summed as a separate charge. Provider billing reconciliation,
per-call fallback accounting, and budget-counter UI remain separate unfinished
integration work. Deployment still requires applying the migrations with old
workers stopped; the live HAI database has not been changed by this implementation.

Verification for recovery: seven package suites passed (agentruntime,
openclawreconcile, automation, models, executionauth, router, migrations).
Two isolated PostgreSQL integration tests passed for receipt alignment and usage
recovery, including exclusivity, one-event completion, restart-persistent attempt
counts and the eight-attempt ceiling. Service tests covered background wiring and
paused recovery without consuming a claim. The disposable database container was
stopped afterwards. This does not establish live-provider usage acceptance.

### Typed Session Usage And Reported Models (2026-09-05)

HAI now preserves the native `sessions.usage` counters in a bounded JSONB snapshot
on the private execution receipt. It stores input, output, cache-read,
cache-write, total tokens, optional estimated USD cost, missing-price count,
source method, UTC date window and observation time. `modelUsage` groups retain
their reported provider, model, count and totals, with at most 32 groups.
Groups are observations within the session date window, not per-call records,
fallback order, an invoice, or an additional budget charge.

The parser rejects missing/fractional/negative counters, duplicate model groups,
invalid model labels, ambiguous session instances and oversized payloads. Unknown
provider/model identities remain empty instead of being replaced by the requested
start model. Unknown cost remains null; a source-backed zero remains zero.
Raw transcripts, file paths, credentials and private session identifiers are not
included in the stored JSON projection.

Migration `0077` changes recovery eligibility from missing summary text to missing
typed JSON. Earlier summary-only captures can therefore be supplemented while
retry attempts remain; no text is parsed into fabricated numeric data and no
attempt counter is reset. Existing audit events remain intact. The typed capture
uses a versioned `openclaw-gateway-usage-v2:` event key and commits with the JSON
in one transaction. Repeating the same capture does not add another v2 event.
Old workers must be stopped before migration and restart.

Verification: parser tests cover invalid/duplicate model groups, missing model
identity and unknown-versus-zero prices. Both opt-in PostgreSQL tests passed on
an isolated disposable database: canonical receipt alignment and typed usage
recovery, including JSONB round-trip, private-identity exclusion, stale-claim
rejection, summary-only backfill, preservation of the original audit event,
duplicate completion and the durable retry ceiling. An initial connection
timeout during container startup required rerunning the database pair; the
completed rerun passed and the disposable container was stopped.
The full package suites for agentruntime, openclawreconcile, automation, models,
executionauth, router, runtimelab and migrations also passed after these changes
(database tests were run separately with the explicit opt-in above).

### Owner-scoped stored usage view

`GET /api/v1/openclaw-usage/:eventId` exposes a validated stored snapshot only to
the authenticated owner. The receipt and launch event must agree on owner,
execution reference and runtime task. The endpoint does not call OpenClaw or
start an agent. Private session identities, transcripts and raw storage errors
are not returned; responses use `Cache-Control: no-store`.

Control Center execution diagnostics now include a lazy OpenClaw usage disclosure.
It separates input, output, cache read/write and total tokens from estimated USD,
preserves unknown prices as unavailable, and exposes reported model groups and
source dates separately. Pending, unavailable and exhausted collection states
are distinct. Refresh reloads stored data, not the provider. Session snapshots
are explicitly not per-run bills or budget debits.

The owner/exact-task joins were exercised against isolated PostgreSQL storage,
along with typed recovery, retry limits and legacy audit preservation. This
increment's separate receipt-alignment test timed out; it is not counted as a
fresh pass despite passing in the earlier migration verification above.

Eleven focused frontend tests passed, including lazy loading, cancellation,
unavailable costs, collection failures and the deferred inspector update.
Browser fixtures exercised the diagnostics-to-usage disclosure at 1440px and
375px; the latter kept the usage view within the viewport without horizontal
page overflow. These are synthetic API fixtures, not live provider billing.
The execution diagnostics modal now uses the existing theme tokens and stacks
each execution's usage below its summary rather than squeezing it into a flex row.

Per-call fallback attribution, provider billing reconciliation and enforced
budget debits remain unfinished. These changes have not been deployed to the
live HAI database and do not establish live-provider acceptance.

The existing mixed-theme modal styling and Pursuits stylesheet size warning
remain separate presentation issues. The editor's imperative open/close path
now marks the view for update, fixing the observed invisible-modal defect.

| Project | Repository | Branch | Reviewed revision | Release | License | HAI readiness ceiling |
| --- | --- | --- | --- | --- | --- | --- |
| OpenClaw | `openclaw/openclaw` | `main` | `fa9626c4e1002d83e6c06a09ad670c8f41f8b24e` | `v2026.7.1-2` | MIT | `declared` |
| Hermes Agent | `NousResearch/hermes-agent` | `main` | `b3aa561faffd64f05436e429a6415d175e534ec9` | `v2026.8.3` | MIT | `declared` |
| Odysseus | `odysseus-dev/odysseus` | `dev` | `e4fa4ae5dd1d709ce4168397bd1d200fec1b2494` | no formal release recorded | AGPL-3.0-or-later | `declared` |

Reviewed on 2026-08-08 from the upstream GitHub repositories and their own
README, protocol, security, threat-model, feature, and roadmap documentation.
The pinned revision is evidence of what was examined; it is not a dependency
lock or proof that the project is configured locally.

## Implemented Discovery Contracts

Runtime Lab now validates reviewed, non-mutating protocol responses instead of
treating an arbitrary HTTP 200 as health:

| Runtime | Requests | Validation | Highest discovery level | Identity limitation |
| --- | --- | --- | --- | --- |
| OpenClaw | `GET /health`; optionally a bounded Gateway `connect` handshake | Health: `ok=true`, `status=live`. Auth discovery: challenge, protocol `4`, `hello-ok`, server identity, non-null features/snapshot, positive policy limits, and exactly `operator.read`. | `available` | Health is unauthenticated. Auth discovery proves only that the configured Gateway accepted one read-only socket; it does not prove execution readiness or a final effect. |
| Hermes | `GET /health`; optionally authenticated `GET /v1/capabilities` | `platform=hermes-agent`, version, capability object/platform | `health_checked` | Capability discovery requires `HERMES_API_SERVER_KEY`; liveness does not. |
| Odysseus | `GET /api/health`, `GET /api/version` | healthy status, RFC3339 timestamp, non-empty version | `available` | The public responses do not carry a cryptographic product identity. |

The production Runtime Lab OpenClaw entry is a read-only projection of the same
canonical `internal/agentruntime` registry used by governed automation. It does
not consult `OPENCLAW_BASE_URL`, create a second Gateway client policy, own
task cancellation, or enable task execution. Its manual probe maps only bounded
canonical readiness to the Runtime Lab contract and retains no raw Gateway
payload or synthetic evidence digest. The generic `OPENCLAW_BASE_URL` adapter
remains only for isolated Runtime Lab tooling and compatibility tests.

Generic Runtime Lab responses are limited to 64 KiB, redirects are refused,
hosts are allowlisted, JSON shape is checked, raw bodies are not returned, and
only a SHA-256 evidence digest plus bounded metadata reaches the dashboard. The
canonical OpenClaw adapter now also performs the stricter Companion-specific `GET /health` handshake: its
response body is capped at 4 KiB, it accepts only `ok=true` with `status=live`,
and it refuses URL credentials and redirects. Query and fragment data are
never forwarded to the fixed `/health` request. A live Companion reports
`available` for read-only discovery; `ready` remains reserved for a genuinely
executable adapter. No gateway token is sent during this health probe. Discovery
evidence from health-only liveness is intentionally in-process only: it does
not establish protocol proof and is never restored after a restart.

With `OPENCLAW_GATEWAY_PROTOCOL_DISCOVERY_ENABLED=true`, HAI performs a second,
still unauthenticated and read-only validation step. It opens the configured
WebSocket, limits the first inbound frame to 64 KiB, requires an `event` frame
named `connect.challenge` with a non-empty nonce and non-negative integer
timestamp, then closes the socket. It sends no token, Authorization header,
`connect` frame, RPC, task, or node command. A malformed or unavailable
challenge changes the result to `unavailable`; it can never be treated as an
execution-ready state.

With `OPENCLAW_GATEWAY_AUTH_DISCOVERY_ENABLED=true` and a configured
`OPENCLAW_GATEWAY_TOKEN`, HAI may perform one further read-only identity check.
After the same bounded challenge it sends only `connect`, requests exactly
`operator.read`, validates a matching `hello-ok` response and closes the socket.
The returned role and scope must be exactly `operator` and `operator.read`;
over-scoped, incomplete, malformed, or unavailable responses are `unavailable`.
When the initial `connect` response is explicitly retryable `UNAVAILABLE` with
a bounded `retryAfterMs`, HAI waits once inside the operation context and opens
one new read-only socket. It does not replay any Gateway RPC, task, or mutation
as part of this recovery path.

`OPENCLAW_GATEWAY_TASK_LEDGER_DISCOVERY_ENABLED=true` is an independent second
opt-in that only operates after the authenticated identity check succeeds. HAI
requires the `tasks.list` feature to be advertised, sends one `tasks.list`
request with `limit: 50`, accepts only documented task-status values, and
returns a non-persistent aggregate of status counts. It rejects malformed,
unexpected, over-limit, or unknown-status responses. Task identifiers, titles,
prompts, session keys, owner information, result summaries, and error text are
discarded before the HAI health response is created. No task cancellation,
creation, execution, tool invocation, channel action, browser action, node
action, or configuration change is available through this discovery path.
Only this authenticated path may expose the bounded printable Gateway server
version in HAI health evidence; a health-only probe never infers a version.
With task-ledger discovery disabled, no Gateway RPC, task, tool, browser, node,
message, pairing, or channel command is sent. This remains an opt-in credential
boundary and discovery-only even when the bounded task-ledger summary is
enabled.

`OPENCLAW_GATEWAY_CAPABILITY_DISCOVERY_ENABLED=true` is a separate
authenticated `operator.read` discovery option. HAI requires `skills.status`,
`tools.catalog`, and `commands.list` to appear in the authenticated `hello-ok`
feature list. It sends the first two parameter-free read requests in sequence
and `commands.list` with `includeArgs=false`, accepting only a bounded skills
array, grouped tools with the documented `core` or `plugin` provenance, and a
bounded command array. HAI reduces the response immediately to total/eligible
skill counts, tool counts by provenance, and a total command count. It rejects
absent groups, unexpected provenance,
or over-limit responses. Skill text, names, descriptions, tool names, plugin
IDs, arguments, raw frames, session context, and credentials never enter health
responses or durable evidence. This path cannot install, update, invoke,
configure, or enable upstream capabilities.

`OPENCLAW_GATEWAY_MODEL_CATALOG_DISCOVERY_ENABLED=true` is a separately gated,
authenticated `operator.read` signal for prepared model availability. HAI
requires `models.list` to be advertised and sends only
`{ "view": "configured", "preparedOnly": true }`. `preparedOnly` prevents a
fresh provider-discovery attempt. The bounded response becomes only sampled,
available, unavailable, and unknown availability counts; model IDs, provider
names, endpoints, prices, route metadata, capabilities, credentials, and raw
payloads are discarded. This discovery does not change HAI model selection,
its 24-hour maintenance cycle, provider configuration, or update policy.

`OPENCLAW_GATEWAY_AGENT_ROSTER_DISCOVERY_ENABLED=true` is a separately gated,
authenticated `operator.read` agent-availability signal. HAI requests only the
`agent-kind` client capability, requires `agents.list` in the authenticated
feature list, and sends a parameter-free request. It accepts at most 500 rows
and retains only aggregate counts for `agent`, `system`, and omitted legacy
`kind`; unexpected kinds, malformed payloads, and over-limit responses fail
closed. Agent IDs, labels, model/runtime metadata, workspace paths, creation
provenance, raw frames, and credentials are discarded. The result is a
read-only availability card, not agent creation, mutation, binding, routing,
or delegation authority.

`OPENCLAW_GATEWAY_DELEGATION_ENABLED=true` remains separate from discovery, but it does not currently make new delegated execution available. HAI fails closed before opening a write connection or sending `sessions.create`: the current shared-secret Gateway handshake does not prove an authenticated durable creator identity, a `sandbox: "required"` named operator role, or the effective policy bound to the exact run. OpenClaw documents that named roles attach to authenticated durable profiles and required sandboxing becomes immutable session-creation provenance ([operator scopes](https://docs.openclaw.ai/gateway/operator-scopes)). A separate write token, local permission mode, read-only discovery, high-risk acknowledgement, or `runStarted` must not be treated as proof of that server-enforced policy.

The exact-run receipt, stop, and reconciliation contracts remain for existing persisted/legacy owner-bound runs. HAI resolves those receipts server-side and uses only `sessions.abort { key, runId }` or `agent.wait { runId, timeoutMs: 0 }`; it never falls back to session-wide cancellation. Abort acknowledgement is not terminal evidence. Replies, tool calls, raw error details, and transcripts are not imported. New delegated execution may be enabled only after HAI can authenticate a durable identity, verify its sandbox-required role and effective policy on the exact created run, persist the admission provenance, and pass pinned-Gateway acceptance tests. Local WebSocket fixtures do not constitute live acceptance.

### Interleaved Gateway Events

The same read/write request path accepts asynchronous Gateway events between
sending an RPC and receiving its matching response. This covers all ten
currently allowlisted methods, including discovery, admission, cancellation,
terminal observation, and artifact metadata listing. It reuses OpenClaw's
[documented frame contract](https://docs.openclaw.ai/gateway/protocol) within
the existing Go adapter; there is no additional runtime, transport dependency,
or second task engine.

Each connection still has one outstanding RPC. Event payloads are discarded,
not logged, stored, interpreted as authority, or used as completion evidence.
A response must match the request ID. Unknown well-formed event names are
tolerated for upstream compatibility; malformed envelopes, invalid sequences,
unrelated responses, and server request frames fail the operation. A maximum
of 256 events and 1 MiB of aggregate event data can precede a response, inside
the existing connection deadline and 64 KiB per-frame cap. Reaching a limit or
losing the connection does not replay admission or cancellation.

The pre-authentication challenge and handshake remain strict; this event
handling applies only to post-handshake RPCs. This is transport compatibility,
not yet a durable progress subscription or a full observability integration.
Local WebSocket regression tests exercise interleaving, denial preservation,
request correlation, event-only disconnects, and resource bounds. Real
delegated-task acceptance remains a separate gate.

`OPENCLAW_GATEWAY_ARTIFACT_IMPORT_ENABLED=true` is a separate, default-off
post-terminal metadata handoff. It needs the distinct read-only Gateway token,
the private v2 execution receipt, and a source-backed completed `agent.wait`
outcome. HAI requests only `operator.read`, requires `artifacts.list`, and
sends only `artifacts.list { runId }`. It accepts no more than 20 summaries and
persists only a SHA-256 digest of each raw artifact identifier, type, MIME type,
and size under the opaque execution reference. It discards artifact identifiers,
titles, sources, URLs, payloads, downloads, raw responses, and transcripts; it
never calls `artifacts.get` or `artifacts.download`. Import failure is recorded
as unavailable metadata and cannot change a terminal success or failure. A
retryable, bounded `UNAVAILABLE` response to the initial `operator.read`
handshake can open one fresh read-only socket before `artifacts.list` is sent;
HAI never repeats the metadata request as part of that connection recovery.

The ordinary metadata-import boundary rejects any receipt that is not already
recorded as completed. The durable reconciliation worker can pass only the
specific completed terminal observation it just verified through a narrower
internal handoff; pending, failed, malformed, and task-mismatched results are
rejected before an `operator.read` connection can open.

Gateway credentials stay inside their respective HAI protocol clients. Neither
the discovery token nor the separately configured delegation token is ever
forwarded to an OpenClaw CLI child process, task envelope, audit record, or
Runtime Lab capability card. Task-state availability is surfaced through its
own aggregate-only card and never grants task control.

Gateway RPC errors are reduced before they leave the protocol client. HAI keeps
only a stable category (`unavailable`, access denied, invalid request, or
rejected) and a bounded retry advisory. An eligible `UNAVAILABLE` response to
an initial scoped `connect` handshake may open one fresh socket within the
operation context, before any Gateway method is sent. A read-only metadata
request may separately make one context-bound retry after an eligible
`UNAVAILABLE` response. Gateway error messages, details, paths, provider
diagnostics, and scope lists are discarded. An `UNAVAILABLE` response never
retries a session mutation or a terminal observation automatically, even when
the upstream response marks it retryable.

HAI also enforces a fixed Gateway method-to-scope map in the protocol client.
Read-only connections may call only the reviewed inventory methods and
`artifacts.list`; write-scoped connections may call only `sessions.create`,
`sessions.abort`, and the terminal observation method `agent.wait`. A new
Gateway method requires a code review, a matching scoped handshake, and a
contract test before HAI can send it.

After a protocol challenge or authenticated `operator.read` discovery succeeds,
Runtime Lab stores one owner- and workspace-scoped Operation Ledger record. It
contains the reviewed schema revision, a SHA-256 digest of the normalized
endpoint, protocol/authentication facts, a printable authenticated version when
available, bounded task-status, capability, model-availability, and
agent-roster aggregates, a digest of the retained evidence,
and a 24-hour expiry. It never stores the endpoint URL, credentials, raw
Gateway frames, task IDs, prompts, owners, result text, or errors. On restart,
HAI restores only an unexpired, internally digest-validated record; it never
infers readiness from configuration or health-only liveness. The recovered
record remains read-only and grants no task, tool, message, browser, channel,
node, configuration, or execution authority.

## Disposition Rules

Every reviewed feature group has exactly one disposition:

- `integrated_directly`
- `adapted_for_hai`
- `hai_native_reimplementation`
- `already_present`
- `consolidated_existing`
- `constrained_unsafe`
- `excluded_irrelevant`
- `excluded_incompatible_license`
- `deferred`
- `blocked_external`

The current review intentionally reports zero `integrated_directly` items. The
existing generic health adapters are not feature integration. A feature may
move to a stronger disposition only after its HAI contract, authority boundary,
protocol implementation, test evidence, and operator documentation exist.

## Project Decisions

### OpenClaw

The relevant integration surface is the Gateway protocol: scoped WebSocket
clients, isolated agent workspaces/sessions, capability discovery, lifecycle
events, and bounded delegation. HAI will retain its own Plan Graph, memory,
scheduler, LLM budget, approval, and verification systems. OpenClaw channel
delivery and host tools stay blocked or constrained until channel-specific
credentials, approval receipts, sandboxing, and independent effect verification
exist. The OpenClaw Control UI, updater, and product shell are not imported.

Primary sources:

- <https://github.com/openclaw/openclaw/blob/main/README.md>
- <https://github.com/openclaw/openclaw/blob/main/docs/gateway/protocol.md>
- <https://github.com/openclaw/openclaw/blob/main/docs/concepts/multi-agent.md>
- <https://github.com/openclaw/openclaw/blob/main/docs/tools/skills.md>
- <https://github.com/openclaw/openclaw/blob/main/docs/gateway/sandboxing.md>

### Hermes Agent

The relevant integration surfaces are `hermes serve` and the documented
JSON-RPC/WebSocket and OpenAI-compatible APIs. HAI may adapt capability
discovery, bounded delegated tasks, tool-progress telemetry, and reviewed skill
metadata. Hermes planning, memory, cron, model routing, and product UI do not
replace HAI's corresponding canonical systems. Learned skills and
self-improvement must enter HAI as evidence-backed controlled-learning
proposals; they cannot mutate authority or policy.

Primary sources:

- <https://github.com/NousResearch/hermes-agent/blob/main/README.md>
- <https://github.com/NousResearch/hermes-agent/blob/main/website/docs/user-guide/features/api-server.md>
- <https://github.com/NousResearch/hermes-agent/blob/main/website/docs/user-guide/features/skills.md>
- <https://github.com/NousResearch/hermes-agent/blob/main/website/docs/user-guide/features/memory.md>
- <https://github.com/NousResearch/hermes-agent/blob/main/apps/desktop/README.md>

### Odysseus

Odysseus is an AGPL-3.0-or-later self-hosted workspace, so its server, UI,
tools, and product shell are not copied or linked into the canonical HAI code.
Useful behavior may be reproduced through HAI-native interfaces, or HAI may
later interoperate with a separately deployed service after legal and security
review.

The upstream threat model explicitly says that a logged-in admin can execute
shell commands, read/write files, send email, and control model serving. It also
documents that shell/filesystem tools lack confinement and network-egress
filtering. Those tools are therefore not exposed through the generic runtime
adapter. Odysseus email, Calendar, and model-serving behavior remains separate
and externally blocked unless a least-privilege, source-linked, approval-gated
protocol is proven.

Primary sources:

- <https://github.com/odysseus-dev/odysseus/blob/dev/README.md>
- <https://github.com/odysseus-dev/odysseus/blob/dev/THREAT_MODEL.md>
- <https://github.com/odysseus-dev/odysseus/blob/dev/SECURITY.md>
- <https://github.com/odysseus-dev/odysseus/blob/dev/ROADMAP.md>
- <https://github.com/odysseus-dev/odysseus/blob/dev/LICENSE>

## Readiness Levels

The delivery specification distinguishes `declared`, `configured`, `available`,
`health-checked`, `self-tested`, `integration-tested`, `demonstrated`, and
`production-ready`. This source review establishes only `declared` parity.
Runtime Lab may separately report a configured endpoint or a successful health
probe, but neither authorizes a task. A runtime can advance only with retained
evidence for the exact level; no level is inferred from a lower one.

## Next Integration Gates

1. Map every discovered method/tool to a HAI capability card with input/output
   schema, authority, risk, cost, timeout, retry, reversibility, approval, and
   verification requirements.
2. Add request identity, cancellation, redacted events, result artifacts, and
   ambiguous-outcome reconciliation.
3. Run a loopback protocol self-test that cannot invoke a tool and retain its
   evidence independently from runtime configuration.
4. Add one bounded local task behind exact execution authorization and
   independent verification.
5. Keep Odysseus remote-only and read-only until its legal and security gates
   are accepted.

No external runtime is production-ready at this point.
