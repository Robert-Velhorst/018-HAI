# Verification evidence — honest health, readiness, and boot

This document is a historical verification record for the health-readiness work
that was later merged into `main`. It records exactly what was run and what
happened on the dated host below. It is not a claim that the current branch has
the same package count, connector catalog, or external-service configuration.
Current release evidence comes from the CI jobs and operator checks documented
in `docs/operator-runbook.md`.

- Date: 2026-07-14
- Host: Linux 6.8, Docker 29.1.3, Docker Compose 2.40.3
- Stack file: `docker-compose.local.yml` with `--env-file .env.local`
- Go build/test run in `golang:1.23` container (no Go toolchain on host)

> Current note on Compose authority: `docker-compose.local.yml` defines the one
> source-built topology. The root and backend `docker-compose.yml` files now
> delegate to it; the retired `jacksonbarreto/*` images and three-broker
> ZooKeeper topology are no longer Compose entrypoints. This supersedes the
> historical warning below while preserving the dated verification record.

---

## 1. The boot blocker (before the fix)

Command:

```
docker compose -f docker-compose.local.yml --env-file .env.local up -d --build
```

Result: **FAILED to start.**

```
Error response from daemon: failed to set up container networking:
Bind for 0.0.0.0:6379 failed: port is already allocated
```

Root cause: the Redis service published host port `6379`, which was already in
use on the machine by an unrelated container. Redis failed to bind, its
dependents never started, and **zero** application containers came up.

## 2. After the fix — full boot

Change: infrastructure services (Postgres, Redis) now bind to `127.0.0.1` on
configurable host ports, defaulting Redis to `6380` to avoid the common
already-running-Redis clash. Only the gateway is published on all interfaces.

```
docker compose -f docker-compose.local.yml --env-file .env.local up -d --build
docker compose -f docker-compose.local.yml --env-file .env.local ps
```

Result at the time of this historical capture: **11/11 services up, 10 healthy.**
The current local topology has since replaced the Kafka plus ZooKeeper pair
with one bounded Kafka KRaft process; the readiness method below is unchanged.

| Service | Status |
| --- | --- |
| backend | Up (healthy) |
| frontend | Up (healthy) |
| nginx (gateway) | Up (healthy) |
| idp | Up (healthy) |
| postgres-automation | Up (healthy) |
| postgres-idp | Up (healthy) |
| redis | Up (healthy) |
| kafka | Up (healthy) |
| generic-auto | Up (healthy) |
| nginxconfigmanager | Up |

### Historical KRaft cutover evidence (2026-08-09)

The retained-data Windows stack was cut over to a fresh
`018-hai-kafka-kraft-data` volume without deleting the three legacy Kafka and
ZooKeeper volumes. The active topology contained ten services and no ZooKeeper
container. Kafka topic `automation-events` was recreated with one partition,
leader `1`, and in-sync replica `1`.

An authenticated automation transaction exercised the real backend, Postgres,
Kafka publisher, and nginx-config-manager consumer: create returned `201`, read
returned `200`, delete returned `204`, and the deleted record returned `404`.
The topic end offset advanced from `2` to `4`, and the temporary generated
route was removed. `/readyz` reported live Postgres, Redis, and Kafka probes as
reachable with `fail: 0`; it remained truthfully `degraded` only because no LLM
provider was configured.

Three immediate idle samples placed KRaft at a median 303.4 MiB, 69 processes,
and 1.13% CPU. The previous Kafka-plus-ZooKeeper pair used about 413.5 MiB and
98 processes in the pre-cutover sample. The full active HAI stack dropped from
about 574.2 MiB to 473.2 MiB in the comparable point-in-time measurements.

That first KRaft envelope was subsequently proven unsafe: its 160 MiB heap
exhausted while the snapshot emitter serialized retained metadata, even though
the TCP health probe stayed green. The corrected 2026-08-09 validation used a
256 MiB maximum heap inside a 512 MiB container, retained the same named KRaft
volume, and produced a new snapshot at offset `25531` without an OOM or snapshot
error. Three stabilized samples measured a median 352.8 MiB, 69 processes, and
1.67% CPU for Kafka, while the current nine-service stack measured a median
498.1 MiB. These later values supersede the lower unsafe memory figure as the
supported local operating envelope.

### Kafka-protocol Redpanda cutover evidence (2026-08-13)

The replacement broker was first tested side by side on the same Docker
network using the exact digest pinned in Compose. It reached cluster-ready
state, created a one-partition topic, and idled at approximately 58.7 MiB with
3 processes under a 256 MiB/0.5 CPU/96-process envelope while retaining fsync.
The active Java KRaft
broker measured approximately 364 MiB and 64 processes immediately beforehand.
Three stabilized connected-live samples measured a median 153 MiB and 4
processes after the backend, IDP, config manager, and application transaction.
Development mode and unsafe fsync bypass are disabled, and the cluster's
`write_caching_default` must remain false.

The authenticated live application smoke then created a disposable automation
(`201`), read it (`200`), consumed its create event into a generated nginx
route, deleted it (`204`), consumed the delete event and removed the route, and
returned `404` for the deleted record. The topic high watermark advanced by
exactly two. `/readyz` reported reachable database, Redis, Kafka, and outbox;
its only warning was the truthful absence of a configured LLM provider.

The new broker writes only to `018-hai-redpanda-data`. All prior Kafka and
ZooKeeper volumes remain detached and untouched. Their presence is rollback
evidence, not permission to delete them.

### Fractional-CPU scheduler evidence (2026-08-14)

The local Redpanda process is capped at half a CPU and runs beside the other
HAI services on Docker Desktop. A controlled run with production-style busy
polling used 151 MiB and about 1.3% CPU after the authenticated application
round-trip. The equivalent overprovisioned scheduler run used about 98 MiB and
0.38% CPU across six samples. Both runs created, read, and deleted a disposable
automation; created and removed its generated route; returned `404` after
deletion; and advanced the topic high watermark by exactly two. The final run
resumed the persisted topic at offset `36` and advanced it to `38`.

Only the scheduler mode changed. Fsync bypass remains disabled, write caching
remains disabled, the Redpanda development profile is not enabled, and
`/readyz` returned HTTP 200 after the final run.

## 3. The readiness lie (before the fix)

`/readyz` was backed by `doctor.Diagnose`, which performs no I/O — it only checks
that configuration strings are non-empty. To demonstrate, all three data
dependencies were stopped and the endpoint was queried:

```
docker stop 018-hai-postgres-automation 018-hai-redis 018-hai-kafka
curl -s http://127.0.0.1:17070/readyz -w '%{http_code}'
```

Result: **HTTP 200, `"status":"ready"`, 14 ok / 0 fail** — with the database,
cache and event bus all stopped. Docker also reported the backend container
`healthy` throughout, because its healthcheck was a plain TCP socket probe.

## 4. Honest readiness (after the fix)

`/readyz` now runs live probes (authenticated Postgres ping, Redis PING over
RESP, Kafka client connect, LLM provider HTTP) and reports three states.

**All dependencies up:**

```
curl -s http://127.0.0.1:17070/readyz -w '%{http_code}'
```

```
HTTP 200  status=degraded  {ok:18, warn:1, fail:0}
  [OK  ] database.connection   reachable in 14ms
  [OK  ] redis.connection      reachable in 2ms
  [OK  ] kafka.connection      reachable in 3ms
  [WARN] llm.provider          no provider configured (OLLAMA_BASE_URL / FREE_CLOUD_OPENAI_BASE_URL unset)
```

`degraded`, not `ready`, is correct: no LLM provider is configured by default, so
generation genuinely is unavailable. Nothing is being hidden.

**Database stopped:**

```
docker stop 018-hai-postgres-automation
curl -s http://127.0.0.1:17070/readyz -w '%{http_code}'
```

```
HTTP 503  status=not_ready  {ok:17, warn:1, fail:1}
  [FAIL] database.connection   unreachable after 2ms: connect postgres-automation:5432/automation ...
```

The endpoint now fails, with HTTP 503, for the exact reason readiness exists.

### Local model acceptance update (2026-08-14)

The opt-in private `local-model` profile was started on the retained Windows
workspace with `qwen2.5:0.5b`. The authenticated acceptance path recorded:

- Ollama live probe: HTTP 200, one model reported;
- route: `ollama/qwen2.5:0.5b`, local tier, EUR 0, no paid approval;
- bounded generation: completed with provider-reported 53 input and 5 output
  tokens;
- persisted generation history: matching audit and telemetry identifiers,
  with prompt and output content absent;
- readiness: HTTP 200, `ready`, 21 ok / 0 warn / 0 fail.

The fixed-output marker was an operational smoke only. The generation record
correctly remains `unvalidated`; this does not establish factual model quality.

## 5. Honest container healthcheck

The backend healthcheck now polls `/readyz` with `curl -f`, so it fails on the
503. Observed transitions:

- All dependencies up: backend reaches `healthy` ~32s after start.
- Database stopped: backend flips to `unhealthy` within ~40s (`retries: 3`).
- Database restarted: backend returns to `healthy` on the next probe (~8s).

Health probe log line while the DB was down:

```
exit=22 out=curl: (22) The requested URL returned error: 503
```

## 6. Placeholder-secret detection

`.env.example` ships `RUN_MODE=production` with `change-this-*` placeholder
secrets. The doctor report now flags these instead of passing them:

- In production mode: `security.backendApiKey` / `jwtSecret` / `memoryEncryptionKey`
  report **FAIL** when the value is a known placeholder, so `/readyz` is
  `not_ready` until real secrets are set.
- Outside production: the same values report **WARN** (visible, non-blocking).

`scripts/generate-secrets.sh` writes real `openssl rand -hex 32` values that can
be appended to `.env.local`.

## 7. Gateway routing drift

The nginx `/api/v1/*` allowlist had drifted from the backend's actual route
groups. Before the fix, three groups fell through to the IDP and returned 404
via the gateway: `agent-cycle`, `flags`, `system`.

```
# before: GET http://localhost:8088/api/v1/flags -> HTTP 404
# after:  GET http://localhost:8088/api/v1/flags -> HTTP 401 (reaches backend, gated by gateway login)
```

The 401 is correct for protected `/api/v1/*` engine routes: the route now maps
to the backend, which the gateway guards with its `auth_request` login check.
`/healthz` and `/readyz` are separate, intentionally public gateway probes.
Neither requires an IDP session. `/healthz` reports liveness; `/readyz` returns
the aggregate readiness state and counts with HTTP 200 or 503. Individual
dependency checks are available only from authenticated
`/api/v1/system/readiness`.

## 8. Two bugs the new probe surfaced immediately

Running the live probes for the first time exposed two latent
misconfigurations, both now fixed:

1. **Redis was unreachable from the backend.** Redis was attached only to
   `idp-network` while the backend runs on `automation-hub-network`; the probe
   reported `dial redis:6379: lookup redis ... no such host`. Redis is now also
   on `automation-hub-network`. (The backend does not yet *use* Redis — quota
   and rate-limit state is still in-process — but it can now reach it.)
2. **`JWT_SECRET` was never passed to the backend container.** It was defined for
   the IDP only, so the backend's identity middleware silently ran as a no-op.
   The secret (and `RUN_MODE`) are now passed to the backend.

## 9. Go build and tests

```
go build ./...      # clean
go vet ./...        # clean
go test ./...       # clean at the time of this dated verification
```

New tests: `internal/doctor/probe_test.go` (probe severity/criticality, timeout
bounding, order preservation, placeholder detection, the exact "valid config +
dead database" regression) and updated `internal/router/readiness_test.go` for
the tri-state semantics.

## 10. Frontend — System Status page

A new authenticated page at `/system-status` (nav: Control Center → System →
System Status) consumes authenticated `/api/v1/system/readiness` and renders it:
an overall banner
(ready/degraded/not ready), a recommended-next-actions list built from the
non-healthy checks, and per-subsystem cards that sort failures to the top. It
polls adaptively (15 seconds while not ready, 60 seconds after errors or while
degraded, 120 seconds while ready), pauses while the page is hidden, and treats
a 503 body as data rather than an error, so a not-ready backend is shown rather
than swallowed.

Build: the Angular image built cleanly; the page compiled into its own lazy
chunk (`pages-system-status-system-status-module`, 10.53 kB).

Verified in a real browser (Playwright, logged in as a locally-registered test
user, through the gateway on :8088):

- **All dependencies up** — banner "Degraded — serving with warnings", 18 ok / 1
  warn / 0 fail; the one warning is the absent LLM provider, with live probe
  timings shown (database 14 ms, redis 2 ms, kafka 11 ms).
  Screenshot: `docs/evidence/system-status-healthy.png`.
- **Database stopped** — the page (after its poll / a Refresh click) flips to a
  red "Not ready — a critical dependency is down", 17 ok / 1 warn / 1 fail; the
  Database card sorts to the top and shows the real driver error. Everything
  else stays green. Screenshot: `docs/evidence/system-status-db-down.png`.
- **Unauthenticated readiness probe** - `/readyz` through the gateway is
  intentionally public and returns only aggregate readiness plus 200/503
  semantics without an IDP session. Protected `/api/v1/*` engine routes,
  including `/api/v1/system/readiness`, return 401 when the session is absent.

The public probe omits subsystem names, hosts, users, paths, and configuration
details. Authentication must not be added to `/readyz` without also updating
container health checks and monitoring clients that rely on a public probe.

Note: the browser check registered a throwaway local account
(`verify@local.test`) via the open `/api/v1/auth/register` endpoint. It exists
only in the local Postgres volume.

---

## 11. Redis-backed rate limiting (quota across restarts)

The per-IP rate limiter kept its counters in a per-process map, so the limit
reset on every restart and could not hold across multiple backend instances.
Counters now live in Redis when `REDIS_ADDR` is set, with an in-process
fallback when it is absent at startup or becomes unavailable at runtime. The
runtime fallback remains bounded; a Redis outage no longer turns an enabled
limiter into an unlimited pass-through. Its per-key map is capped at 4,096
entries, pruning expired windows before evicting the oldest active key, so
rotating client identifiers cannot grow fallback memory indefinitely.

Fixing this surfaced another plumbing gap first: `RATE_LIMIT_PER_MINUTE` was
defined in `.env` but never passed to the backend container, so the limiter
could not be enabled at all regardless of configuration. Now passed through.

Verified with `RATE_LIMIT_PER_MINUTE=5`:

```
# backend log on startup:
ratelimit: using shared Redis store at redis:6379

# six requests in one window:
req 1..5 -> 200
req 6    -> 429   (Retry-After set)

# the counter is visibly in Redis:
redis-cli --scan --pattern 'ratelimit:*'
  ratelimit:172.23.0.1:29733334
```

Durability across a backend restart (the actual ask):

```
# before restart
ratelimit:172.23.0.1:29733334 = 100
docker restart 018-hai-backend
# after restart — same key, same value, untouched by the restart
ratelimit:172.23.0.1:29733334 = 100
```

And that enforcement reads the shared state rather than process memory: seeding
the counter in Redis to 99 caused the next request to be rejected immediately,
without the backend having counted it locally.

Default is unchanged (`RATE_LIMIT_PER_MINUTE=0`, disabled): 12 rapid requests
all return 200, so normal use is unaffected. Unit tests cover the limit
boundary, per-key isolation, bounded local failover, and fail-closed behaviour
when no fallback exists, using a deterministic fake so they need no running
Redis.

## 12. Honest connector status

The connector catalog reported all 9 connectors as `operational`, and the
dashboard showed "9 connectors operational". But only two make a live network
connection (GitHub REST, and the JSON feed); four are named after cloud services
(email, calendar, cloud-documents, project-board) while actually reading export
files from a local folder; one (`odoo-herp`) generates built-in domain models
with no live connection; the rest read local folders/exports. There is no OAuth
anywhere in the backend.

`AdapterStatus` was overloaded: it was both the UI label and the gate deciding
whether a source can be created (`service.go`). So the fix separates the two:

- Honest status values: `operational` (live remote), `local_only` (reads local
  files/folders), `modeled` (built-in models), `not_implemented` (contract only).
- A new `adapterIsUsable` helper gates source creation: everything except
  `not_implemented` can still be created, so the local-folder connectors that
  genuinely work keep working.

Verified:

```
# catalog now reports honestly:
github, json-feed          -> operational   (2)
email, calendar, cloud-documents,
project-board, local-folder,
whatsapp-export            -> local_only    (6)
odoo-herp                  -> modeled       (1)

# creating a local_only source still succeeds (function preserved):
POST /api/v1/sources {connectorKey: "email"} -> HTTP 201
```

The dashboard now reads **"2 live · 6 local · 1 modeled of 9 connectors"** instead
of "9 operational", and each connector in the picker is labelled honestly
("… — local files only", "… — live", "… — built-in model"). Screenshot:
`docs/evidence/connectors-honest-status.png`. The backend test suite passed at
the time of this dated verification, and the catalog test asserted the honest
statuses.

This is deliberately a downward change in the headline number — it removes an
overstatement, which is the point. No connector lost any real capability.

## 13. Authenticated and role-aware API boundary

The `rbac` package (roles owner/operator/viewer; permissions read/write/approve/
admin) was fully implemented and unit-tested but attached to **zero** routes —
`requirePermission` appeared 0 times in `routes.go`. And no role reached the
backend: the gateway forwarded only the cookie and shared key, so every caller
defaulted to `viewer`, which meant wiring write-gates naively would have 403'd
the entire UI.

The current contract does not trust a caller-supplied role header:

- Protected engine routes require the backend shared key and a verified identity
  token. The shared key alone is not user identity and receives HTTP 401.
- The gateway obtains a verified token from the IDP auth subrequest and forwards
  that token to the backend. The backend derives the subject and role from the
  signed token.
- Route-specific permission middleware then applies read, write, approve, or
  admin requirements. Sensitive owner-only routes retain explicit owner gates.
- A client-provided `X-HAI-Role` value is not an authentication mechanism.

The authenticated smoke tests exercise representative boundaries:

- shared key without identity token: HTTP 401 on protected routes;
- signed viewer identity: HTTP 403 on an admin route;
- signed owner identity: HTTP 200 on the same admin route.

These tests prove the local authentication and authorization contract. They do
not prove an external identity provider, OAuth consent, or production secret
rotation until those environment-specific steps are completed.

## 14. First real OAuth connector: Gmail (read-only)

The first connector that talks to a live external account instead of reading
local files. Built against the developer's own Google OAuth app.

Architecture:
- `internal/googleoauth`: the authorization-code flow (consent URL with
  `access_type=offline` for a refresh token, code exchange, refresh), an
  AES-256-GCM codec for tokens at rest, and a read-only Gmail REST client with
  bounded message bodies, attachment metadata, and textual attachment content.
  All are unit-tested against mock servers.
- `internal/source/oauth.go`: HMAC-signed CSRF state with only an owner-bound,
  single-use SHA-256 digest retained in PostgreSQL; encrypted token storage
  (`SourceOAuthToken`); transparent token refresh; and a Gmail fetch wired into
  the sync dispatch.
- Routes: `sources/oauth/google/start` and `.../callback` both require a
  verified HAI role. The callback additionally consumes the exact initiating
  owner's state before code exchange, so restart, replay, denial, expiry, and
  revocation fail closed.

Verified on the running stack:

```
# Not configured (GOOGLE_OAUTH_* empty) — honest, does not pretend to work:
gmail connector adapterStatus            -> not_implemented
GET sources/oauth/google/start           -> HTTP 400 "google oauth is not configured"

# Configured — the flow produces a real Google consent URL:
gmail connector adapterStatus            -> operational
GET sources/oauth/google/start           -> authorizeUrl =
    https://accounts.google.com/o/oauth2/v2/auth?...
      client_id=<configured>
      scope=https://www.googleapis.com/auth/gmail.readonly
      access_type=offline
      redirect_uri=<gateway>/api/v1/sources/oauth/google/callback
      state=<hmac-signed>
```

This is verified up to the consent boundary. The final step — a real Google
account clicking "Allow", the callback storing tokens, and a sync pulling real
messages — requires the OAuth app's real credentials and a human consent, and is
performed with the operator during the live/remote session. It is deliberately
not claimed as done here.

## 15. Framework Registry and durable task state (2026-07-30 addendum)

This section describes the current branch implementation boundary. It is
separate from the dated 2026-07-14 host evidence above.

### Present in the repository

- A code-owned Framework Registry catalog with exactly 55 version `1.0.0`
  records: 50 `active`, five `experimental`, and zero `deprecated`.
- A deterministic `selector-v5` contract with required overlays, risk-ceiling
  enforcement, conflict handling, a 16-framework limit, authority ceilings, and evidence/completion
  requirements.
- A durable operating contract containing multi-domain classification,
  needs-state and capacity constraints, fresh verified agent cards, explicit
  unassigned roles, complete identity/capability/access/health/revocation
  metadata, zero-spend delegation contracts, typed replay-resistant
  communication, coordination, exact 0-10 per-action autonomy, stop
  conditions, outcome monitoring, and eight Chief-of-Staff answers.
- Owner-scoped preferences. `disabled` is an effective preference, not a catalog
  lifecycle status; protected overlays reject disable attempts.
- Owner-scoped Constitution drafts and activation. Activation requires the exact
  case- and whitespace-sensitive phrase `ACTIVATE CONSTITUTION` and a redacted
  approval note of at least 10 characters.
- Restrictive typed Constitution rules for deny-capability, require-approval,
  and authority-ceiling. Ordinary prose is versioned context, not executable
  authority, and no typed rule can grant capability.
- Reproducibility metadata containing catalog, selector, effective-preference,
  and Constitution versions/digests plus selected framework versions and exact
  Constitution source.
- Authenticated registry API routes and Angular `/framework-registry` UI.
  Viewers read, operators read and request selections, and owners administer
  preferences and Constitution lifecycle.
- Pre-phase migrations `0003_framework_registry`,
  `0004_task_state_storage`, and `0005_framework_operating_contract`.
- Owner-scoped task completion logs, review items, and immutable decisions.
  Approved execution is bound to the stored owner, request digest, review
  revision, task/project/automation context, and `task-review:<id>` source.
- HTTP task-history/review reads return an error when the durable repository
  fails instead of showing an empty ledger.

These are implementation statements, not live-provider or production-readiness
statements.

### Verification boundary for this addendum

The repository contains focused unit, route, frontend, in-memory repository, and
PostgreSQL integration tests for these contracts. CI is configured to run
Framework Registry and task-state database tests in isolated PostgreSQL
databases and to exercise signed-session and Windows shell contracts.

On 2026-07-30, a fresh local verification pass completed the backend unit
suite, vet, and build; the IDP unit suite, vet, and build; the frontend's 418
headless unit tests and production build; the Python CI, authentication, and
gateway contract suites; Compose configuration validation; and Bash syntax
checks for the smoke scripts. All three Go modules, container builders, and CI
runners are pinned to the same Go 1.25.13 toolchain by an executable CI
contract test. Refreshed `govulncheck` v1.6.0 scans report 0 vulnerabilities
affecting backend, IDP, or nginx configuration manager code; all three pinned
scans are blocking CI gates. The nginx manager no longer imports the Docker SDK
or receives a Docker-socket mount.

This is source and local-build evidence, not a production deployment receipt.
PostgreSQL integration jobs, race tests, authenticated stack smoke, real
connected accounts, and target-machine acceptance remain release gates. Treat
the next completed CI run and the target-machine acceptance run as the
authoritative evidence for those environments.

That dated pass also ran the production-dependency frontend audit and found 12
high and 1 moderate advisory in the Angular 16 dependency family. The finding
was subsequently superseded by the coordinated 2026-08-09 Angular 22 migration:
the full audit now reports 3 moderate development-tool findings and 0
high/critical, and `npm audit --audit-level=high` is blocking. See
`docs/dependency-vulnerabilities.md` for the accepted scope and review deadline.

### 2026-08-09 live Windows regression acceptance

The current Angular 22 frontend, IDP, and task-review backend were rebuilt and
deployed into the retained local Windows Compose stack. The full backend and
IDP Go suites passed, all 379 frontend tests passed under Node 22.22.3, the
production frontend build passed, and all 29 executable CI contract tests
passed. An authenticated browser run against `http://localhost` verified login,
the shared shell, Basic-to-Advanced disclosure, mobile overflow, and meaningful
content on Control Center, Pursuits, Workflow Engine, Task Blueprint, Connected
Sources, Framework Registry, Runtime Control, and System Status. The browser
reported no console or HTTP failures.

This pass also repaired two live defects rather than masking them: the pending
task review queue no longer fails because of unreadable resolved history, and
the signed-out session probe now returns an explicit unauthenticated status
without producing a normal 401 console error. Historical task digests are
accepted only when they exactly match the legacy v1 algorithm and carry none of
the newer mandate or coordination provenance, preserving the fail-closed trust
boundary.

The repository now also contains a disabled-by-default, digest-pinned ngrok
profile and a Windows public-exposure preflight. Static CI contracts, Compose
validation, ngrok v3 configuration validation, shell syntax, and positive and
negative fail-closed gate cases pass. No live ngrok token or public endpoint was
used in this verification, so external tunnel and callback behavior remains a
target-environment acceptance gate rather than a readiness claim.

The A2A planning subset now has exact unauthenticated gateway routes for its
public Agent Card and token-protected `SendMessage` endpoint, plus a dedicated
nginx throttle and 16 KiB request limit. Its backend accepts only one bounded
standalone text message and returns a non-executable HAI planning draft. A local
end-to-end gateway smoke passed, including anonymous denial and authenticated
success. Synthetic positive and negative ngrok preflight cases also passed.
No real ngrok credential/domain was available for this verification, so public
transport remains unproven and does not imply external-agent execution,
distributed coordination, or full A2A task-lifecycle support.

The backend and IDP now share signal-aware HTTP lifecycles with the backend's
in-process schedulers and workers. A live Compose SIGTERM cycle drained both
containers in 3.2 seconds with exit code 0, recovered both to healthy, retained
all 59 pre-phase and three post-phase migrations, and reconciled all nine
stored memories without findings. The gateway now serves a strict script CSP,
COOP `same-origin`, CORP `same-origin`, clickjacking, MIME-sniffing, referrer,
permissions, and server-token controls. Static contracts and live response
headers verify the policy, including cookie-refresh locations where nginx
header inheritance would otherwise be lost.

### 2026-08-14 request and history resource hardening

The shared backend router now rejects known oversized non-multipart request
bodies before route handlers run and wraps streamed request bodies with a 2 MiB
hard ceiling. Multipart routes remain explicit: automation image forms are
bounded by the configured image limit plus form overhead, while the reviewed
OpenClaw archive route retains its separate 750 MiB archive budget and now
enforces that budget before multipart parsing. The production HTTP server also
has bounded header size, request-read time, and idle connection lifetime while
leaving the response write timeout unset for intentionally long, independently
bounded provider operations.

Verification detail lookup no longer loads and scans an owner's complete run
history. It performs one owner-scoped, ID-scoped database query, while the
history endpoint returns the latest 200 records; an older run remains directly
addressable by its ID. This keeps normal inspection cost stable as audit
history grows.

Acceptance evidence: all backend tests and `go vet` passed, execution-boundary
race tests passed, all 418 frontend tests and the production build passed, all
55 CI/auth/gateway contracts passed, and both default and local-model Compose
profiles validated. The rebuilt Windows stack reported 21 ready checks, zero
warnings, zero failures, and no recent backend error lines. A live oversized
JSON request returned HTTP 413 before authentication, while a normal write
still returned HTTP 401. A post-rebuild Ollama generation completed with model
`qwen2.5:0.5b`, EUR 0 cost, provider-reported 53 input and 5 output tokens,
recorded audit evidence, and ready system status.

### Known recovery and deployment gaps

- Automation approval-proof replay state is owner-scoped and durable in the
  append-only PostgreSQL consumption ledger. Its signing key is deployment
  configuration and must remain consistent across backend instances.
- A durable task review can remain `approved` if the backend stops after the
  decision is committed but before the execution outcome is committed.
- There is no automatic review-reconciliation worker and no public recovery
  endpoint for that indeterminate state. Operators must inspect external
  effects and audit evidence before any retry.
- PostgreSQL persistence and immutable constraints do not prove exactly-once
  external side effects.
- Migration files exist, but a clean-clone migration/rollback/restore exercise
  on the target Windows host remains required.
- A two-real-account owner-isolation browser exercise remains required.
- Named implementation candidates in the 55-record catalog are not installed
  or trusted merely because the catalog references them.
- Agent cards prove only the freshness and provenance supplied by a trusted
  runtime health source. Missing specialists remain `required_unassigned`;
  HAI does not invent live workers from catalog role names.
- Typed communication validates idempotency, expiry, confidentiality,
  provenance, payload digest, and optional signature-digest shape, but a
  distributed A2A transport, cryptographic signature authority, live consensus
  service, and durable standing-mandate workflow are not claimed.
- Real LLM providers, connected accounts, and external agent runtimes require
  their own configuration, authorization, bounded probe, approved task, audit,
  and verification evidence.

## What is NOT claimed here

- A real Gmail OAuth connector now exists and is verified to the consent
  boundary (section 14); the live pull of real mail awaits real credentials and
  consent. This historical record does not assert the current readiness of
  other connectors; use the current connector catalog and its live health
  checks for that decision.
- This verification exercised bounded local Ollama generation, but not paid or
  external cloud-provider billing and quota accounting. Local-provider evidence
  must not be presented as proof of those external behaviors.
- The signed-JWT smoke fixtures prove route enforcement, not production identity
  provisioning. Production readiness still requires configured IDP secrets,
  real sessions, and operator-owned external authorization.
- The Framework Registry does not prove that all named agent frameworks,
  workflow platforms, memory stores, policy engines, or evaluation products
  power HAI.
- Durable approval records do not prove crash-safe or exactly-once external
  execution.

These are called out honestly rather than presented as done.
