# Runtime hardening verification - 2026-09-30

## Scope

This is a bounded engineering verification record for a continuation of HAI's
runtime, connector, worker, and release-gate work. It is not certification of
the entire product, live provider correctness, or a production deployment.
Changes were integrated into the existing `codex/hai-runtime-release` worktree.
Other pre-existing changes were preserved; no commit or push was performed.

## Behavior corrected

- Background operations recheck execution policy, renew ownership, and surface
  claim failures. A policy-deferred item gets a review cooldown rather than
  starving unrelated work. Claim loss cancels further processing.
- Durable recurring jobs atomically dead-letter an exhausted expired delivery
  and retain exactly one next occurrence. Recovery uses the same singleton
  locking as registration/completion. Failed successor insertion rolls back
  the recovery transaction; stale workers remain fenced.
- Google read clients coalesce simultaneous 401s into one bounded refresh.
  Individual request cancellation does not poison the shared refresh. Grant
  reuse rechecks source ownership/status and cancellation, and unusable grants
  no longer report ready. Token-store failures are not disguised as a missing
  connection. Trusted Google media redirects may proceed without forwarding
  bearer credentials or cookies across origins; unrelated hosts remain blocked.
- OpenClaw dials vetted addresses. `localhost` must resolve only to loopback;
  `host.docker.internal` requires explicit host/port opt-in and reviewed private
  IP pins. HTTP redirects and untrusted TLS certificates are rejected.
- Linux script execution stages reviewed bytes privately, verifies SHA-256,
  bounds files/copying to 16 MiB, propagates the deadline to authorization, and
  limits output-pipe draining to 250 ms. Uncertain outcomes are indeterminate.
  Non-Linux safe-open support fails closed. This is not a capability sandbox,
  process-tree containment, or a guarantee against stalled kernel/filesystem
  operations. See the operator runbook before enabling it.
- Trello migration 0109 locks sources before receipts, matching webhook lock
  order. Preflight/backfill are protected from concurrent writes.
- Destructive package tests require opt-in, dedicated database identities, and
  loopback-only connection configuration. Migration helpers validate fallback
  hosts too. The agent-register DDL replay test preserves the ordered migration
  ledger instead of deleting a historical row in its middle.
- CI provisions a migrated brain-skill database, a separate durable-job
  database, and requires actual PASS markers for the new recovery/migration
  integration tests. Background workers are included in the race-check gate.

## Completed checks

Go checks used disposable Go 1.25.13 Linux containers with a read-only repository
mount. Normal unit-suite runs did not receive database/provider credentials.
The PostgreSQL checks used a separately created PostgreSQL 17 container, no
published ports, a temporary memory-backed data directory, and synthetic test
data. Existing HAI databases/accounts/services were not used or restarted.

| Check | Result and evidence boundary |
| --- | --- |
| Backend `go test -count=1 ./...` | Passed. Optional database tests skip without their explicit DSNs; the separate database results below are not inferred from this run. |
| Backend `go vet ./...` and `go build ./...` | Passed in Linux. |
| Race checks | Passed for automation, task, source, LLM, agent runtime, background workers, durable jobs, and PostgreSQL test guards. |
| Identity-provider `go test -count=1 ./...` | Passed; no live OAuth or email delivery tested. |
| Nginx config-manager `go test -count=1 ./...` | Passed using test fixtures, not shared gateway mutations. |
| Entire migrations suite with `-tags integration` | Passed against disposable PostgreSQL 17. |
| Entire infra suite with `-tags integration` | Passed against disposable PostgreSQL 17, including concurrent runners, ordered-ledger rejection, apply idempotence, and post-migration rollback. |
| Migration command on a fresh test database | Passed: 109 pre migrations and 5 post migrations recorded, zero pending. Not evidence of a safe upgrade of existing operator data. |
| Dedicated brain-skill selection, evaluation, resilience suites | Passed against separate disposable PostgreSQL databases. |
| Agent-registry PostgreSQL tests | Passed after correcting the obsolete ledger-hole fixture and rerunning on a fresh disposable test database. |
| Entire durable-job suite with `-tags integration` | Passed against its dedicated disposable database, including concurrent reapers/startup, one successor, injected rollback, restart, and stale-worker fencing. |
| CI contracts | 60 tests passed. YAML parsing and migration-job Bash syntax checks passed; not proof of a hosted GitHub Actions run. |
| Frontend checks earlier in this work batch | 1,063 tests and production build passed; 29 module templates/146 static disclosure IDs checked. Existing stylesheet budget warnings remain. No new browser acceptance or screenshot pass is claimed here. |
| Linux first-run secret generator | Fixture tests and focused contract passed earlier in this batch; actual credentials were not generated into the operator's environment. |

## Remaining acceptance

- Real Google-account revocation/refresh and multi-hop media downloads, and
  live Trello intake/reconciliation against approved accounts.
- A bounded configured LLM task with source-backed output/postcondition
  verification; unit tests do not establish semantic real-world correctness.
- Operator-reviewed Docker host IP configuration and a real Windows
  Companion/Gateway handshake. New delegated OpenClaw execution remains
  blocked without the required authenticated identity and policy attestation.
- Native Windows execution/containment and clean-clone Windows installer,
  migration/restore, login, browser, and restart acceptance. Cross-compilation
  is not Windows runtime proof.
- Hosted CI execution and fresh responsive/accessibility browser acceptance.
- Production-data upgrade and restore rehearsal with verified backups.

Do not convert these remaining gates into success claims or enable
consequential execution solely because the local checks are green.

## Additional integrated hardening pass

The following is a later pass on the same date, not a replacement for the
earlier evidence above. Specialist agents worked in disjoint areas; their
changes were reviewed and integrated into the shared worktree. No commit,
push, live deployment, personal account mutation, or existing-service restart
was performed.

### Corrected behavior

- Refresh-token replay after the concurrent-refresh grace period revokes the
  family atomically. Cached replacements must match the family and remain
  unexpired, unconsumed, and unrevoked. Revocation TTLs cannot be shortened;
  missing Redis and Redis errors fail closed. Expired access tokens cannot
  refresh merely because verification allows clock leeway.
- Reminder revocation remains available after a preparation deadline expires
  or its source closes. Exact duplicate revocations are idempotent; stale
  bindings are rejected. Delivery checks expiry immediately before dispatch
  and records the time at which the receipt is processed.
- Windows installer staging preserves existing CRLF configuration/ports and
  rejects malformed or duplicate port settings. Every worker retry revalidates
  the staged payload. Executable validation rejects zero-filled entry points
  and checks the hash against the same opened file handle.
- Module view preferences preserve newer writes during synchronous subscriber
  callbacks. Previously saved open sections emit their initial opening state
  so lazy records actually load. Collapsing a section restores focus before
  hiding it; resetting one module clears its deep section without changing
  other modules. Skip navigation focuses content without losing the route.
- Workflow and open-loop renewal acquire their database row lock before
  testing wall-clock expiry through a materialized CTE. A timestamp captured
  before a blocked update, or an ordinary wall-clock WHERE predicate evaluated
  before the lock wait, could otherwise revive a now-expired claim. The
  dedicated PostgreSQL regression covers that unchanged-row lock-wait case.
- Angular 22 defaults were incompatible with legacy subscription-backed page
  fields: loading states could remain visible after requests completed.
  NgModule bootstrap now explicitly enables coalesced zone change detection;
  35 previously implicit production components explicitly select Eager
  rendering. Existing explicitly OnPush components retain their strategy.
  Command Center success and error paths also notify change detection, tested
  with asynchronous responses and no manual post-response render trigger.
- Task planning's interrupted-work recovery uses one shared template, available
  from both overview and history. The regression now switches between the
  actual rendered inspector modes rather than depending on a stale view.
- Workflows now retains a semantic, visible page title and its refresh control
  in Basic view; the isolated browser route audit identified the missing title.
- Thirteen operational templates no longer introduce a second main landmark
  inside AppShell. Classes, controls, and named regions are preserved; login
  retains its own main outside the authenticated shell. An Angular-parser
  contract checks all operational templates and is included in `npm test`.
- The model page's fixed seven-column status strip now uses available-width
  auto-fit tracks at every breakpoint, including desktop layouts with the
  shared navigation rail. The content grid can shrink and status tiles wrap
  without removing budget, token, or provider information.
- The shell's safety label uses a constrained two-line layout instead of
  overlapping text in compact header space.
- Mobile navigation renders its dialog and workspace inert state before
  scheduling focus. Closing renders the released inert state before returning
  focus. The regression clicks real controls without a manual post-click
  change-detection update, covering a timing defect found in the test browser.
- Authenticated browser tests require explicit disposable-stack intent, a
  literal loopback address with a non-default port, and a synthetic
  `e2e-*` account at `example.test`. Local test instructions no longer start
  the normal Compose installation with its fixed names and persistent volumes.

### Completed local checks

| Check | Result and evidence boundary |
| --- | --- |
| Backend full unit suite | Passed in disposable Go 1.25.13; optional DSN-dependent suites are not inferred to have run. |
| Identity-provider full unit suite | Passed in disposable Go 1.25.13. |
| Real Redis refresh/logout races | Passed for three repeated race-enabled runs, including concurrent refresh and logout. Disposable Redis only. |
| Reminder PostgreSQL revocation regression | Passed without skipping on disposable PostgreSQL 17.11 after all 114 canonical migrations. |
| Workflow claim-expiry tests | Focused unit, real PostgreSQL, and race-enabled checks passed; no personal database used. |
| Windows installer validation fixtures | Payload validation and focused update-integrity checks passed, in addition to the specialist's lifecycle/rollback fixtures. Not a native installed-product acceptance run. |
| Angular full suite | 1,076 tests passed after the async-rendering, inspector-placement, workflow-heading, and mobile-focus fixes. |
| Frontend production build | Passed, initial raw bundle 703.22 KB / estimated transfer 115.37 KB. Five existing component stylesheet budget warnings remain; limits were not raised to conceal them. |
| Browser suite type check | Passed. |
| Pure browser-test contracts | Two tests passed: isolated-target validation and explicit Angular render strategy/bootstrap. No UI acceptance is inferred from these contracts. |
| Proxy and template-landmark contracts | Ten Node tests passed, including seven Angular-parser landmark checks. These are structural checks, not screen-reader or live-provider certification. |
| CI contracts | 64 tests passed; the specialist also parsed YAML and checked 55 shell blocks. Redis/reminder/claim-expiry checks require actual non-skipped PASS markers. Hosted CI was not run. |

### Isolated browser evidence

The final production frontend (`main-YJYZ4IO5.js`) was served through a
separate loopback gateway with real disposable backend, IDP, PostgreSQL, and
Redis services. Synthetic account only; no operator account, database, or
existing container was used. External providers, schedulers, and execution
were disabled and background mode was paused.

- All 28 operational routes passed DOM/accessibility-tree checks at actual
  CSS viewport widths 480, 768, 1024, and 1440 pixels: 112 combinations.
  Each retained a visible page title, one main landmark, Basic mode, dark
  theme, and no document-level horizontal overflow. The captured browser
  error/warning log was empty. This is not a complete visual/accessibility
  certification or populated-data action-chain test.
- Command Center reached loaded states for workflow, ambient, and pursuits
  API data without a second user action. Its refresh control became enabled
  and its primary action stopped displaying the loading state.
- Workflow Advanced mode persisted across navigation and reload while HAI OS
  remained Basic. Module reset, direct `#product-stack` disclosure, and
  keyboard skip-to-content worked without losing the current route.
- Mobile navigation focused the current module inside its rendered dialog,
  made the workspace inert, and restored focus to the menu button after
  Escape while releasing inert state.
- Task planning exposed exactly one visible recovery-preview control in both
  overview and logs. No recovery, worker, provider probe, sync, or external
  operation was executed through the browser.

The browser tooling's Windows scaling/minimum viewport produced 480 CSS
pixels when a narrower viewport was requested. Exact 375-pixel acceptance
remains unverified; the automated suite asserts actual viewport width to
prevent falsely reporting it as tested. Screenshot capture was unavailable,
so this pass does not claim screenshot-based visual approval. The new full
Playwright suite was type-checked but not executed; the manual browser checks
above are separate evidence, not a claim that the automated suite passed.
The agent-created browser tab was closed and its temporary viewport reset.
All 13 Compose-owned test containers and both owned networks were removed;
the separate stopped static-server fault-test container was then verified by
ID and removed without volume deletion. The owned container-name filter is
empty and test port 57815 is closed. Existing HAI services were left running;
volumes, pre-existing networks, caches, repository data, and scratch evidence
were retained. No broad prune or file cleanup was performed.

### Still not established by this pass

- At the end of this earlier pass, Redis key loss could remove revocation-only
  evidence. The positive-session authority fix and its remaining restore-risk
  boundary are documented in the subsequent pass below.
- Follow-up projection and reminder sink/receipt atomicity were still open at
  this earlier checkpoint; the subsequent pass addresses both below.
- Application-wide Windows migration rollback, clean installed-product
  acceptance, signing, production-data upgrade/restore, and live providers
  remain external acceptance gates.
- The newly added automated Playwright suite, exact 375-pixel viewport,
  screenshot-based visual review, full keyboard/screen-reader acceptance,
  populated-data action chains, and hosted CI remain unverified.

## Session authority and atomic follow-up continuation

Five specialist agents contributed implementation, database regressions,
native Windows checks, and an independent authentication review. The parent
reviewed integration and ran the full backend/IDP suites. All changes remain in
the existing worktree; this is not a commit, hosted CI run, or live deployment.

### Corrected behavior

- Session creation now requires expiring positive Redis authority for both
  refresh token and family. Access and refresh checks require both records;
  key loss, eviction, malformed values, permanent keys, and Redis outages fail
  closed. Existing JWTs without this authority must log in again on deployment.
- Refresh rotation consumes predecessor authority before its later writes and
  registers its child atomically. Family and predecessor lifetimes bound the
  replacement lifetime. Logout removes positive family authority first. Real
  Redis ACL fault injection checks failures after earlier Lua writes: Lua
  execution is not treated as a rollback transaction.
- Cached refresh responses use authenticated encryption bound to the exact
  predecessor ID. Moving an encrypted grandchild response into an earlier
  predecessor's cache is rejected; the legitimate grandchild remains usable.
  There is no permissive legacy-cache or legacy-session fallback.
- Follow-up checklist/proposal, workflow state, open-loop status, transition,
  decision, and event writes share one PostgreSQL transaction. Deterministic
  per-loop receipts prevent duplicate projection or state reset after replay.
  Workflow/open-loop locks precede database-clock lease checks; an additional
  post-write check rolls back expired workers. Ambiguous legacy adoption fails
  closed while already adopted records are preserved.
- Internal reminder workers serialize on authorization, revalidate locked
  approval/source records, and commit the proactivity signal and delivery
  receipt together. A sink's partial writes are rolled back to a savepoint
  before committing a retry receipt; expiration during the sink rolls back its
  signal. Three committed failed attempts exhaust delivery. Canonical startup
  wiring was inspected: both repositories use the cached `infra.GetDefaultDB`
  pool, satisfying the shared-transaction binding. Unsupported or already
  transaction-bound repositories fail closed rather than using independent
  production writes. No external delivery permission is granted.
- Native Windows testing found a workspace junction reaching the guarded
  launch boundary. DeepSeek workspace validation now rejects link/reparse
  components before probing or starting. Disabled defaults and production
  isolation gates remain unchanged; the test harness did not enable production
  execution or use personal credentials.

### Completed checks and evidence

| Check | Result and evidence boundary |
| --- | --- |
| Full backend suite | Passed for all packages using disposable Go 1.25.13, `-mod=readonly -p 2 -count=1`, no provider credentials. DSN-dependent skips are not counted as database proof. An initial 512 MiB compiler-temp failure was corrected by giving the owned test container 2 GiB and limiting build parallelism; no disk pruning was used. |
| Full backend vet and build | Passed with the same isolated read-only source setup. |
| Final focused backend race suite | Workflow, proactivity, and agent-runtime packages passed after the original reminder fixture adaptation. Optional PostgreSQL cases without DSNs remain separate from the actual database proof below. |
| Full IDP suite and vet | Passed with the explicitly owned disposable Redis instance; no live OAuth or email delivery. |
| Real Redis session regressions | Final authentication/service Redis run passed all 28 top-level groups three times with race detection: 84 group passes, zero skips/failures. Includes both partial-write ACL cases; no result is inferred from mocks. |
| Follow-up PostgreSQL regressions | All 11 groups passed three times with race detection: 33 group passes, zero skips/failures. Includes rollback at every write, replay, concurrent workers/schedulers, lease expiry after lock wait and during writes, approval/retraction serialization, legacy adoption, distinct identical-text loops, emergency stop, and nested-transaction rejection. |
| Reminder PostgreSQL regressions | Original historical revocation/source-closure and new atomic replay groups each passed three race-enabled runs, zero skips. Includes concurrent workers, receipt-write failure, panic, connection loss, partial sink writes, retry exhaustion, lock waits, expiry, approval revocation, and source change. The original fixture now commits synthetic setup before exercising the root-owned delivery transaction; original assertions were retained. |
| Native Windows adapter regression | All 14 top-level checks passed on Windows build 26200 with actual synthetic child execution. Includes junction/path rejection, bounded output, timeout, cancellation, and emergency-stop denial. Not OS sandbox, descendant containment, installed-product, or live-provider acceptance. |
| Linux workspace links | Four focused symlink cases passed. |
| CI contracts and syntax | 67 contract tests passed; workflow YAML parsed with 14 jobs. New Windows and PostgreSQL gates require actual PASS markers, not a successful command that skipped the requested tests. Hosted CI has not run. |

Retained evidence, including logs, ownership and cleanup records:

- Redis: `C:\Users\NO\AppData\Local\Temp\hai-session-evidence-0ff5f304345c4774a367874c2f6b8c8a\redis-tests.jsonl`
  (84 top-level group passes, zero skips/failures).
- Reminder: `output/reminder-revocation-replay-20260930-34f71c8b.log`
  (three PASS markers for each original/atomic group, zero skips;
  SHA-256 `D69B9B4EA90C1ED836E33B905C4CB882F9DCDDD4542686ADB24DADBE2C84DF1D`).
- Follow-up: `C:\Users\NO\AppData\Local\Temp\hai-followup-evidence-20260930-160131-8c21\test-summary.json`
  and `postgres-tests.jsonl` (33 PostgreSQL group passes, zero skips/failures).
- Windows: `C:\Users\NO\AppData\Local\Temp\hai-windows-native-acceptance-a8e374c8ab464f2783f89fab7fc54075\acceptance-evidence.json`
  and `native-tests.log`. The parent independently compared source hashes for
  the production adapter, native test, and runner with the recorded hashes.
- Linux: `C:\Users\NO\AppData\Local\Temp\hai-linux-workspace-acceptance-575069db36ee44cebdde7e00747ff36a\linux-tests.log`.

A final Redis rerun initially failed because the owned test server's `/data`
tmpfs was not writable by its non-root Redis user; scheduled RDB saving then
disabled writes. Its failure evidence is retained in
`C:\Users\NO\AppData\Local\Temp\hai-session-evidence-89c58a40df094083be52ace5efbbbee8`.
Only that exactly identified disposable container was replaced with a writable
tmpfs. Background-save status was verified as `ok` before the final passing
rerun. Persistence and write-failure checks were not disabled, and production
Redis configuration was not changed.

The replacement Redis instance also completed a background save after the
tests. Its exact ID, ownership label, lack of mounts/host networking, and
removal were checked; `cleanup-verification.log` and `redis-persistence.log`
are retained alongside its final test log. Follow-up and reminder agents also
recorded cleanup of their own temporary PostgreSQL instances. No persistent
volumes, existing installations, source/worktree files, or diagnostic evidence
were deleted. Stopped native-build containers and caches were retained.

### Remaining acceptance and limitations

- Restoring an older Redis snapshot can restore earlier positive authority.
  Invalidate restored sessions or rotate signing material before exposure;
  snapshot anti-rollback is not established by eviction/fault-injection tests.
  The low-level access-token-only blacklist is not independently resistant to
  loss; the production logout path revokes the entire positive family.
- Clean installed-product Windows acceptance, OS-enforced isolation, descendant
  containment, signing, production-data migration/backup/restore, real
  Companion/Gateway handshakes, and approved live providers remain open.
- Prior browser/frontend results above were not rerun in this backend-focused
  continuation. Exact 375-pixel, screenshot-based review, full keyboard/screen
  reader, populated-data action chains, and full automated Playwright acceptance
  remain unverified. Hosted CI and deployment are not implied by local success.

## 2026-09-30: Historical evaluation, HTTP sessions, and isolated acceptance continuation

This dated checkpoint adds evidence; it does not rewrite the earlier passes
or make their source snapshots current. Production readiness remains the goal,
not an outcome established by this documentation pass. Only this subsection
and [the isolated acceptance guide](isolated-acceptance.md) were authored here.
No descendant agents, commits, pushes, deployment, provider calls, container
startup, or cleanup were performed in this documentation pass.

### Checked evidence and its limits

| Area | Evidence checked on 2026-09-30 | Exact boundary |
| --- | --- | --- |
| Outcome evaluation on PostgreSQL | `backend/internal/outcomeevaluation/test-evidence/20260930-pg-snapshot/pass-summary.json` and `go-test-race-count3-passing.jsonl`: five top-level groups, each passing three race-enabled repetitions, **15 PASS, zero skips/failures**. The JSONL SHA-256 matches the summary. Ownership and cleanup records describe a separate tmpfs PostgreSQL fixture with canonical migration 0023, not the parent acceptance stack. | Independently exercises historical evaluation-selector binding: exact revision/digest survives bounded history, later definitions change semantics, reopening the repository replays the persisted evaluation, and forged/cross-owner selectors are rejected. This is not the entire proactivity/composer snapshot chain or an all-migrations/full-product database acceptance run. The earlier failed race log is retained, not counted as passing. |
| IDP HTTP session chain with real Redis | `output/hai-idp-http-session-6afa39fa70b74273811751540c73c756-evidence.json` and its final logs: **31 top-level Redis groups once with race detection**, plus **three new HTTP groups three times with race detection (nine PASS)** and nine non-race repeated passes. Final log files exist, match their recorded hashes, and contain zero skips/failures. | Real Gin handlers/middleware and Redis authority through `httptest`, but a synthetic user repository and sender. Not full PostgreSQL, nginx, browser, live OAuth, SMTP, or Kafka proof. The unavailable-store case closes the real client; it is not a network-outage test. Earlier failed/pre-poll attempts remain recorded. |
| Native Windows process trees | `C:\Users\NO\AppData\Local\Temp\hai-windows-process-containment-a8230c8427ec401da09133d01e9154b7\execution-ledger.json`, `containment-evidence.json`, and retained stress logs: **90 named PASS markers, nine names repeated ten times**, zero recorded skips; actual native synthetic process-tree execution. | Three names are standalone fixture helpers, not three independent behavioral regressions. Process-tree supervision is not an OS security sandbox, installed-product, signing, live-provider, or native race-detector proof. The ledger's source hashes identify the tested snapshot; `.github/workflows/ci.yml` has since changed, so this is not blanket current-worktree/CI proof. Earlier unsuccessful attempts remain unsuccessful. |
| Integrated Angular suite | `C:\Users\NO\AppData\Local\Temp\hai-frontend-integrated-20260930.log` ends with **TOTAL: 1127 SUCCESS**. | A completed historical integrated run, not a post-repair rerun or browser/production-readiness claim. |
| Connected Sources focused UI/service suite | `frontend/src/app/pages/connected-sources/ACTION_CHAIN_AUDIT.md` and `C:\Users\NO\AppData\Local\Temp\hai-connected-sources-action-chain-20260930-final.log`: **111 SUCCESS**. | Focused synthetic Angular component/service contracts, not live-account ingestion or approval issuance end to end. |
| Memory focused suite | Parent handoff reports **40 focused passes** and locally retained logs/audit. | The separate focused evidence file was not located during this documentation pass. This count is parent-reported, not independently reverified here; it is not additional browser or live-data proof. |
| CI contracts and workflow structure | This documentation pass reran `python -B scripts/test_ci_contract.py`: **69 tests, OK**. The current workflow YAML was separately parsed: **14 jobs**. | Local Python/source contracts and YAML parsing only. No hosted CI job, deployment, or release was run or inferred. |

### Default isolated stack and unresolved browser checkpoint

Source inspected: `scripts/isolated-acceptance-stack.ps1` and
`scripts/test-isolated-acceptance-stack.ps1`. Read-only `Validate` succeeded
against the retained prepared configuration; the isolation test script itself
was inspected, not executed in this documentation pass.

The retained evidence directory is
`C:\Users\NO\AppData\Local\Temp\hai-acceptance-1f9eca3e7ceb48e5a039e2f5cba22142`.
Its historical configuration has nine core services, two uniquely owned
networks, only nginx attached to ingress, and the backend attached solely to
the internal network. Its gateway was `127.0.0.1:58143`. There are no
persistent volume mounts in that configuration; read-only evidence-directory
binds are distinct from Docker volumes. PostgreSQL and Redis use tmpfs, and
the copied initializer enables `uuid-ossp`. All 140 service environment flags
ending in `_ENABLED` are false; provider accounts are absent, OAuth/SMTP are
blank, and phase 2 is paused. Synthetic credential files are private: do not
publish their contents, the bootstrap identity, or passwords.

`browser-tests.log` retains **nine passed and three failed of twelve**. The
failed cases are the bounded operator execution chain, the 375-pixel module
layout, and dashboard error recovery. Keep that log/report as failed evidence;
do not replace its outcome with later fixes. The parent reports that dashboard
retry and Home navigation/layout repairs are persisted and a new build
finished, but no final browser PASS evidence is available here. The latest
`C:\Users\NO\AppData\Local\Temp\hai-frontend-integrated-run3-20260930.log`
contains only `Building...`; the interrupted run is **not passing proof**.

At this documentation recheck, `docker ps` found no running containers with
this acceptance owner. `docker ps -a` still showed nine exited owned records,
including successful one-shot migration/role setup containers. Accordingly,
the old stack is not running, but complete container removal is not confirmed.
No cleanup or restart was attempted here.

With `SOURCE_SCHEDULER_ENABLED=false`, startup does not launch the durable
source runner; paused background policy additionally defers its work. Manual
async source completion is therefore blocked in this default stack. A queue
acknowledgement or persisted intent is not successful sync, extraction, or
verified execution. Any future manual-local opt-in requires a separately
reviewed mode and actual completion evidence; none is claimed in this guide.

### Pending parent evidence: explicitly unverified

| Follow-up | Current status | Parent to append only after actual results |
| --- | --- | --- |
| Post-repair integrated Angular rerun | Unverified; run 3 interrupted at building. | Exact command/browser binary, source snapshot, final log, exit status, PASS/failure/skip counts. |
| Post-repair isolated browser rerun | Unverified; retained checkpoint is 9/12 PASS, 3 FAIL. | Fresh owner/project, built source/assets, actual viewport widths, named results, logs/report, and cleanup observations. |
| Manual-local source worker acceptance, if introduced | No accepted mode or completed run documented. Default remains disabled/paused. | Reviewed opt-in boundaries, synthetic-only inputs, durable job terminal state, extraction/persistence and postcondition evidence. |

The parent may append the eventual dated result without changing these
historical outcomes. OS sandboxing, clean installed Windows acceptance,
production-data migration/restore, approved live integrations, full
accessibility acceptance, hosted CI, and deployment remain separate gates.

## 2026-09-30: Production-readiness documentation reconciliation

This appended checkpoint supersedes continuation status, not the outcomes of
the historical runs above. Earlier failed/interrupted evidence is unchanged.
Only `README.md`, `docs/isolated-acceptance.md`, and this new end subsection
were edited by this documentation assignment. Existing dirty content was
preserved. Relevant capabilities were local file/log inspection, scoped patching
and documentation consistency checks; no external connector was needed.
No current personal environment was read, no descendants were created, and no
tests, builds, container startup/cleanup, commits, pushes, deployment or provider
actions were performed by this documentation assignment.

### Latest proof and provenance

| Surface | Evidence available at this reconciliation | Exact boundary |
| --- | --- | --- |
| Final integrated Angular rerun | Directly read the end of `C:\Users\NO\AppData\Local\Temp\hai-frontend-integrated-run5-20260930.log`: Chrome Headless **151.0.7922.34**, all **1131** executed, ending **TOTAL: 1131 SUCCESS**. | Completed unit-suite log, not final Playwright, screenshot, populated-data, accessibility, hosted CI or release proof. Run 3 remains interrupted; the older 1127-success run retains its own snapshot. No fresh run was launched here. |
| Proxy, landmarks and progressive-section contracts | Parent latest handoff reports **10 Node proxy tests plus landmark checks PASS**, and progressive contracts **29 templates, 146 static and two dynamic sections PASS**. | Parent-reported source-contract checks, not independently rerun here and not browser-rendering proof. |
| Isolated launcher guards | Source-read `scripts/isolated-acceptance-stack.ps1` and `scripts/test-isolated-acceptance-stack.ps1`; parent reports **34 paused negative cases**, including the nine external-configuration cases, plus those **nine in manual-local** passed. Each of the nine in both modes invokes actual `Start` and refuses before Docker; wrong-owner cleanup and failure/caller-environment restoration PASS. | The Docker command is mocked for refusal/cleanup controls. No full stack starts in those negative tests. Test source inspected here; PASS outcomes are parent-reported, not new executions by this assignment. |
| Manual source worker | Direct-read [worker handoff](../output/manual-source-worker-20260930.md): 51 broader top-level source tests, zero skips; final 34 distinct focused tests repeated ten times under race detection (**340 passes**, zero skips); 31 existing durable-runner tests under race detection. | Independent/default-off startup, HTTP 503 with no writes when disabled, policy/stop deferral, owner/source rejection, idempotent retry and local extraction covered by focused tests. Local completion uses temporary files and in-memory repositories; SQL restrictions use GORM DryRun. **Not real PostgreSQL, concurrency/restart or browser acceptance.** Initial red build stays failed evidence. |
| Ambient-monitor PostgreSQL | Direct-read [monitor report](../output/ambient-monitor-production-20260930.md), evidence `output/ambient-20260930-4654f19eed64/`: PostgreSQL **17.11**, all **109 embedded pre migrations through 0109** applied; race-enabled `-count=3` run of ambientmonitor/pgtestguard reports **186 top-level passes**, no failed tests/races. Three outer subprocess-probe skips are intentional; actual PostgreSQL cases were not skipped. | Bounded lifecycle/composition/guard and real fixed-collector SQL acceptance. Fixture-derived counts are 262 open loops, 260 verified completions, 260 overdue commitments, with 256-row digest snapshots; read-only repeatable-read was confirmed. Lifecycle collectors are mocks; real collector proof is separate. Concurrent source writes/count-snapshot consistency and fully pinned policy/composer snapshots remain unproven. This assignment read the report, not independently replayed or rehashed every artifact. |
| Windows installer | Direct-read [installer audit](../output/installer-production-audit-20260930.md): malformed PE32+ fixed headers and oversized directory counts were accepted before the fix and rejected afterward; three accepted header shapes and 12 rejected states. Source selection, payload validation and promotion passed on Windows PowerShell 5.1 and PowerShell 7; six build-transaction cases passed on each with filesystem doubles. Recovery/static checks were bounded; the full installer contract suite was not run. | Structural PE validation is not loadability, publisher/signature or clean-install proof. No new installer was built/executed. Retained `HAI-Setup-541b870.exe` is reported **NotSigned**, manifest commit `541b87042b2b5a4af7d6d0ff09c7976a46a82b44`; it is stale and contains no demonstrated audit fix. Release dirty-worktree refusal preserved existing artifact hashes. No real backup/restore, power-loss/upgrade/uninstall or signing-chain acceptance. |
| CI continuation | Parent reports the CI owner is replacing ordinary volume-deleting Compose cleanup with the guarded harness. | In progress at handoff; no current completed CI reconciliation or hosted CI PASS inferred. Earlier local CI-contract counts/YAML checks remain historical, not proof for the latest workflow edits. |

### Paused default and bounded manual-local opt-in

The launcher now supports `-ExecutionMode manual-local` at **Prepare**; the
default remains `paused`. Its persisted manifest controls later Validate/Start.
The sole enabled feature flag in manual-local is backend
`SOURCE_MANUAL_WORKER_ENABLED=true`, with `SOURCE_WORKER_POLL_SECONDS=15`.
**All automatic schedulers remain false**, including source, workflow,
open-loop, ambient, outcome monitor, model maintenance and catalog revalidation.
Backend remains solely on the internal network, source mounts are read-only
synthetic fixtures, and accounts/providers/feeds/OAuth/SMTP remain absent.

`HAI_PHASE2_MODE=autonomous_safe` allows the exact manual, owned synthetic
acceptance work and its governed read-only health probe. It is **not a broad
autonomy grant** or authorization for personal/live source ingestion. The
manual runner claims/recovers only queue `source`, kind `source.manual_sync`,
one job per poll; it registers no automatic sync/scan, webhook or
extraction-correction handlers. It is not a connector allowlist: existing
manual jobs can resume on a populated installation. Global pause/emergency
stop, ownership, source eligibility and source-sync leases remain enforced;
an accepted/deferred job is not success, nor proof of in-flight rollback or
at-most-once effects.

The [guide](isolated-acceptance.md#explicit-manual-local-extension) now requires
the separate `E2E_ALLOW_MUTATION=true` consent for synthetic operator records.
Its documented proof follows the current operator test: HTTP 202 with exact
`sourceId`, `mode=manual_async_sync` and job `id`; polling that same owner-scoped
job until persisted `completed`; and reading the same source's
`/acceptance.txt` extraction with the exact synthetic content. Queue acceptance,
a toast or worker startup does not satisfy these conditions. Environment
restoration removes `Env:<key>` with `Remove-Item` when its original value was
null and otherwise uses `Set-Item`; it does not rely on .NET null assignment.

### Cleanup and fresh browser handoff

The parent reports the old daemon exited **255**, then removed **exactly nine
old-owner containers and two owned networks**, without deleting files, caches
or volumes. This updates cleanup status only: the historical browser report
remains **9/12 PASS, 3 FAIL**, including its operator-chain, 375-pixel-layout
and dashboard-error-recovery failures. No result is converted to PASS by a fix
or by cleanup. This documentation assignment did not inspect live Docker or
perform cleanup itself.

The new prepared stack owner is `6cfa099c027a455bb3cba587f5b1f0ff`, evidence
directory
`C:\Users\NO\AppData\Local\Temp\hai-acceptance-6cfa099c027a455bb3cba587f5b1f0ff`.
It was **not started at the assignment handoff**; the parent owns its build and
browser run. Final build, actual job/extraction completion, exact 375-pixel
rendering and **final Playwright PASS remain unverified here**. Append actual
owner/source/build identity, named pass/fail/skip counts, report/logs, viewport
measurements and cleanup observations after execution; do not infer them from
Angular success or prepared configuration.

On the subsequent retry handoff, the parent reports build/browser work ongoing
and **two additional confirmed redaction/projection issues delegated for repair**.
No detailed finding or completed-fix report was supplied to this documentation
assignment. Both remain unresolved in this checkpoint; no fix, passing
regression or final browser result is claimed until actual reports arrive.

### Remaining release gates

- Approved current-revision installer packaging and enforced signing/trust;
  the existing Setup is stale and unsigned.
- Clean Windows installed-product install/upgrade/uninstall, interrupted
  upgrade/rollback and real production-data migration/backup/restore, including
  restored-session invalidation or signing rotation before exposure.
- Approved live providers, local-model tasks, real OAuth/SMTP and
  Companion/Gateway handshakes with retained audit and postcondition evidence.
- OS-enforced sandboxing; process-tree supervision is not a security sandbox.
- Final isolated Playwright acceptance, full keyboard/screen-reader and visual
  accessibility, and populated Advanced-view action chains.
- Completed guarded CI integration, hosted CI and target release/deployment
  acceptance. Local tests and documentation do not establish any of these.

The bounded PostgreSQL monitor proof narrows its earlier acceptance gap; it
does not establish concurrent-source consistency or fully pinned
policy/composer snapshots. The manual worker's focused proof does not close
its real database/concurrency/restart/browser gates. Production readiness is
still a goal, not the result of this reconciliation.

## 2026-10-01: Staged production-readiness partial checkpoint

Local date is now **2026-10-01, Europe/Amsterdam**. Production readiness is
**staged and incomplete**. This appended subsection updates current status
without altering any preceding historical evidence. The September statements
that the new stack was only prepared and that the two privacy repairs were
pending describe those earlier handoffs, not the observations below.

This assignment owns only `README.md`, `docs/isolated-acceptance.md` and this
append-only record. Capabilities used were direct local report/log reads,
structured JSON evidence counting, scoped document patches and consistency
checks. No descendant agents, source edits, current environment reads, commits,
service/container operations or provider calls were performed. Parent-owned
browser and final rebuild work was not duplicated or awaited indefinitely.

### Current stack and failed browser runs

The parent reports owner `6cfa099c027a455bb3cba587f5b1f0ff` **started and fully
healthy** in the synthetic `manual-local` mode. Automatic schedulers remain
disabled; only the manual source worker is enabled, the backend stays on its
internal network, and no provider/account/OAuth/SMTP configuration is enabled.
The bounded `autonomous_safe` setting is not a broad autonomy grant. Startup
health is not final operator, privacy or release acceptance. No live Docker
inspection was performed by this documentation assignment.

| Browser checkpoint | Parent-reported actual result | Interpretation and remaining proof |
| --- | --- | --- |
| Historical earlier owner | **9/12 PASS, 3 FAIL**, retained unchanged. | Not superseded with a fabricated PASS; its artifacts and failed outcomes remain historical. |
| New owner's run 1 | Terminal **11/12 PASS, 1 FAIL**. | The action-bound approval proof was correctly mandatory. The operator test had not yet exercised that approval path; this was not authorization to bypass the guard. |
| New owner's run 2 | Terminal **11/12 PASS, 1 FAIL**. | The loaded test asserted API `needs_approval` against displayed human text `needs approval`. The parent corrected that assertion; the failed run remains failed. |
| Focused operator run 3 | **Ongoing**, with UI approval followed by retry. | The current test source distinguishes API state from UI text, requires a blocked/zero-attempt response before approval, then approves the exact workflow and retries. Inspected assertions are not a completed browser result. No final PASS is claimed. |

The running image **predates both stable privacy fixes below**. The fresh final
full-stack rebuild and its privacy-inclusive browser acceptance remain pending
with the parent, even if a focused operator run on the earlier image succeeds.
Parent source/report review of both fixes is reported complete and the two
specialists closed; that is not deployed-image or full-stack acceptance.

### Newly available evidence and precise limits

| Surface | Evidence read or supplied | Exact boundary |
| --- | --- | --- |
| Full backend snapshot | Directly counted `output/backend-integrated-production-20260930.jsonl`: **147 passing packages, 3815 top-level test passes, zero test/package fail events**. There are **202 optional test-skip events**: 173 top-level and 29 subtest skips; two additional package-skip events are not included in the 202. | This identifies the retained run's source snapshot, not a fresh full suite after final privacy edits. Skips do not establish optional database acceptance. Parent reports vet terminal exit **0**; its terminal output was not independently captured here. Running-image inclusion and final rebuilt-stack proof are separate. |
| Final Angular | Directly re-read the end of `C:\Users\NO\AppData\Local\Temp\hai-frontend-integrated-run5-20260930.log`: **TOTAL: 1131 SUCCESS**, Chrome Headless **151.0.7922.34**. | Completed integrated unit-suite log, not final Playwright, populated Advanced actions, full accessibility or release proof. No suite was rerun here. |
| Manual worker on real PostgreSQL | Direct-read [Faraday report](../output/manual-worker-postgres-20260930.md) and `output/manual-worker-pg-20260930-e6905598c8c5/named-postgres-proof.json`: six named PostgreSQL tests each passed three race-enabled repetitions, **18 PASS, zero skips/failures**. Report records PostgreSQL **17.11**, dedicated `hai_source_manual_worker_test`, uniquely owned schemas and tmpfs fixtures; final owned-container log is empty. | Actual production claim SQL, twelve concurrent claimers, skip-locked behavior, bounded reaping/retry and persisted owner/local-file extraction reload are covered. Pools/services are reopened, not a full process/server restart. Seven production models use fixture AutoMigrate; migrations 0087/0105 and the full canonical constraint/index set are not applied. No live provider, full startup wiring or parent-stack browser proof. Exact two owned containers/schemas were cleaned; no volumes/networks/cache deletion claimed. |
| CI isolation | Direct-read [Wegener report](../output/ci-isolation-production-20260930.md): **76 local contract tests PASS**, no skips, guarded Prepare/manual-local/Start and separate `always()` Stop wired to the exact evidence directory. CI disables traces and restricts/redacts retained diagnostics/artifacts. | Local mocked process/OS boundaries and parsed workflow, not hosted Actions, actual Ubuntu startup/cleanup, browser artifacts or deployment acceptance. The report's **14 jobs** is a YAML job count, not a gateway-test count. Parent separately reports **14 gateway checks PASS**; those results were not rerun here. |
| Launcher guard | Parent reports **34 paused negatives, nine manual-local negatives, 18 actual Start refusals before any Docker call** (the nine external-configuration cases in each mode), plus caller/failure environment restoration and wrong-owner cleanup refusal PASS. | Actual launcher Start entry point with mocked Docker; not 18 real container launches or an independent new documentation-run test. Existing paused/manual isolation boundaries remain intact. |
| Doctor production secrets | Direct-read [doctor report](../output/doctor-production-secrets-20260930.md): **22 top-level tests and 153 secret-matrix leaf cases PASS**, plus race, ten focused repetitions and vet. Production diagnosis enforces at least 32 trimmed bytes for the three configured security secrets while retaining placeholder/mode semantics. | Synthetic package proof and inspected call sites, not running router/readiness, installed-key entropy or final image acceptance. 217 aggregate pass events include groups and are not 217 independent cases. No installed secrets were read or rotated; memory-key rotation still needs an approved migration/re-encryption plan. |
| F1 structured secret redaction | Direct-read [Gauss report](../output/structured-redaction-20260930.md): defect fixed in the shared helper; **20 previously failing regression cases now pass**, final **race/count=3**, vet and **16,503 fuzz executions** passed. Complete JSON is recursively sanitized; input/output are bounded to **1 MiB**, recursion/fallback container depth to **64**, with fail-closed size/depth/error/uncertain-tail handling. | Focused synthetic helper verification, not arbitrary unlabeled-secret detection, historical-record cleanup or final-image privacy. Ordinary fields/types survive within bounds, but serialization/key order need not match original bytes. Caller/projection acceptance is a separate report. |
| F2 automation public projection | Direct-read [Poincare report](../output/automation-public-projection-20260930.md): list/detail/diagnostics/Create/Update project sanitized copies; unchanged exact projected values preserve raw execution credentials, altered literal/encoded masks reject with **HTTP 400 before writes**. Complete bounded output capture precedes redaction, then the display limit. **Ten new top-level regressions**, full automation **race/count=3** and vet PASS. | Raw executable configuration and action-bound approval digests are retained internally. Synthetic handler/loopback fixtures, not fresh JWT/router/browser authorization or deployed privacy proof. No historical records were rewritten/deleted. Oversize/incomplete captures omit output rather than return a raw secret-bearing prefix. |

### Operator guide privacy and approval controls

The [isolated acceptance guide](isolated-acceptance.md) now explicitly passes
`--trace=off` in its operator CLI command. Traces can retain the login request
body; disabling them does not sanitize screenshots, videos, reports or logs,
nor remediate any historical artifact. Credentials remain private process
values, and no credential-bearing manifest/config/environment file should be
published. Original-null environment values are still restored by `Remove-Item`
on the specific `Env:<key>`; present originals use `Set-Item`.

`E2E_ALLOW_MUTATION=true` remains a separate explicit opt-in for this owned
synthetic stack. Queue submission alone is insufficient: verify the exact
returned job/source, persisted terminal completion and same-source synthetic
extraction. The selected workflow must first show the mandatory action-bound
approval refusal with zero attempts, receive its exact UI approval, then be
retried and terminally verified. The fixed assertion compares API
`needs_approval` separately from UI `needs approval`; neither the assertion
change nor the local unit/database suites imply completed operator acceptance.

### Pending parent completion and release gates

The parent will append the actual focused-run result, fresh final build/image
identity and privacy-inclusive browser acceptance after stable completion.
**No final Playwright PASS or final build completion is established here.**
Hosted CI remains unverified. Earlier unsigned/stale installer, clean Windows
install/upgrade/uninstall, production-data migration/backup/restore, approved
live providers/OAuth/SMTP, OS sandboxing, full accessibility and populated
Advanced-view action chains remain release gates. Bounded manual-worker
PostgreSQL acceptance does not close canonical constraints or full process
restart; bounded ambient-monitor proof does not close concurrent-source
consistency or fully pinned policy/composer snapshots.

## 2026-10-01: Final integrated backend and UI continuation

This parent-authored addendum supersedes pending status above, not historical
results. Final stable backend run 3, `output/backend-final-run3-20261001.jsonl`,
completed with exit 0: 147 passing packages, 3854 top-level passes, no failures
and 208 test/subtest skip events (package skips excluded). Full backend vet
passed; the final safety/frameworkregistry/pursuit/source privacy suite passed
three race-enabled repetitions. Source job/audit regression fixtures used
refused local URLs, not real accounts.

The final full Angular rerun, `output/frontend-final-suite-20261001.log`, ends
with 1132 SUCCESS on Chrome Headless 151.0.7922.34. The source disclosure
contract, ten proxy/landmark tests, five build-resource tests and browser
TypeScript checking passed. Build workers default to two; no measured
percentage resource saving is established.

Synthetic owner `7428197162ac47be996715b3b261c634` was rebuilt and started
healthy. Its browser report records 11 PASS/1 FAIL. Local manual source job
completion and the exact fixture extraction were observed; pursuit intake
and exact runtime selection succeeded. The first selected run returned
blocked/blocked, not needs_approval, with no capable model, required approval
and missing framework precondition evidence. The operator test failed before
approval. Attempts=1 does not establish actual tool execution. It cannot be
combined with earlier-snapshot UI approval into terminal completion evidence.
That owner was removed through guarded Stop, with private evidence, all files,
images, caches and volumes retained.

Visual review found verbose Basic diagnostics and toolbar labels spilling
outside Sources/Memory buttons. Basic now summarizes without changing stored
diagnostics or Advanced detail; navigational inspection is not disabled by an
unrelated scan. The shared subtitle selector now excludes generated button
label spans and both toolbars preserve whole-button wrapping. New read-only
browser assertions check label containment at four widths and the loaded HAI
OS summary. Rebuilt rendered outcomes and exact cleanup follow in the
[integration safety ledger](../output/integrated-safety-checkpoint-20261001.md).

No commits, push, personal deployment, real provider/account changes or new
authority grants occurred. Required participants, evidence, capability and
approval remain mandatory. All outstanding release gates above remain unless
specifically verified by a later retained result.

### Final rebuilt UI result and cleanup

Owner `8b65752c7f0845779ca3c4c77614af25` started healthy from the rebuilt
current UI. The production build passed with a 703.22 KB initial bundle,
115.34 KB estimated transfer and six remaining SCSS budget warnings.
`ui-final-browser.log` retains 11 PASS/1 FAIL: the operator still blocks before
approval, while the updated read-only route tests passed. The last separate
`inspection-final-browser.log` completed with eight passes and zero skips,
covering all 28 operational modules at 375/768/1024/1440px, label containment,
loaded HAI OS, disclosure, focus and error recovery. The actual persisted
blocked workflow opened from Inspect blocker with its exact owner-scoped ID
and blocked state; non-read requests were prevented and none were observed.
This proves inspection, not approved execution. Source/image identities and
the four directly viewed captures are recorded in the integration ledger.

Guarded Stop completed and an exact ownership-label recheck returned zero
owned containers and networks. Logs/private evidence remain. No files, data
volumes, images or caches were deleted; the personal installation was not
changed. These terminal results resolve earlier pending UI verification and
cleanup entries only, not the incomplete operator/release gates.
