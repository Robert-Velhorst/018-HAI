# OpenClaw harness maintenance

HAI owns the workflow, approvals, evidence, memory and completion decision.
OpenClaw is an optional execution dependency, not a second HAI application.
Maintenance does not grant OpenClaw permission to run HAI tasks.

## What is implemented

- Separate installed/available versions and policies for Windows Companion and
  the Gateway/core package in the `OpenClawGateway` WSL distribution.
- Persisted 24-hour check scheduling, deduplicated jobs, 20-minute leases and
  exact-version installation authorization in PostgreSQL migration `0072`.
- Stale pending checks are failed and rescheduled; stale or expired installer
  jobs become `needs_review` and can only trigger a read-only recovery check.
  Recovery retains the active-job uniqueness constraint and transaction lock.
- Durable consecutive check-failure accounting in migration `0083`. Failed
  checks retry hourly for the first two failures; from the third consecutive
  failure onward, HAI continues read-only checks once per day instead of
  permanently stopping version discovery. A successful check resets the
  failure streak. Updates remain blocked unless the latest completed check
  receipt is fresh, timestamp-consistent, target-bound and publisher-verified.
- A Windows pull worker with no HTTP listener. It accepts only fixed target
  profiles, not arbitrary commands, download URLs, file paths or scripts.
- Runtime Lab and System Status controls for checking, update policy and
  explicitly acknowledging recovery after an uncertain installation.
- Shared task-admission/update locking. New HAI OpenClaw tasks wait while an
  update is pending, leased or unresolved. Durable `admitting`, `admitted`, and
  `needs_review` Gateway receipts block installation until exact-run terminal
  evidence is persisted. Maintenance does not cancel them.
- Companion and Gateway/core installers cannot be leased concurrently.
- Worker contact is reported from authenticated API requests observed since the
  current backend process started. Recent contact is not proof of current
  process health; after a backend restart the state is unknown until the worker
  contacts HAI again.
- Exact receipt retries are idempotent, including when the independent backend
  health check rejects a worker's claimed success. A timed-out installation is
  not automatically replayed. An owner can resolve it only after a new successful
  check, independent runtime health verification and explicit acknowledgement.
  Recovery leaves automatic updating switched off.

## Defaults and authentication

Fresh Windows installations enable read-only version checks by default. The
installation policy starts in `observe` and requires an explicit owner choice.
The Windows first-run initializer generates a dedicated random maintenance
token; after HAI is ready, the installer registers a least-privilege scheduled
task only when the worker executable, launcher, account, credentials, and
loopback configuration all validate. The task polls at logon and hourly while
the user is signed in. The backend schedules release checks every 24 hours.
Compose also defaults the feature flag to enabled when it is omitted, but an
empty, malformed, or reused worker token keeps both the handler and scheduler
disabled. Missing worker prerequisites cannot lease an install job.

To explicitly opt out on Windows, set the following in
`%LOCALAPPDATA%\HAI\hai.env`, then restart HAI with **Start HAI** so the backend
re-reads the setting and removes its owned scheduled task:

```dotenv
HAI_OPENCLAW_MAINTENANCE_ENABLED=false
```

When HAI migrates an existing `hai.env`, an explicit `false` remains an opt-out
and is preserved. If the setting is absent, the migration adds `true` and
creates a separate random worker token; malformed or duplicate settings stop
the migration rather than being guessed. Non-Windows deployments must provide
their own dedicated token and worker; the blank token in `.env.example` is
intentionally not a usable credential.

Gateway/core installation is blocked for every policy: Windows Job Objects do
not contain Linux descendants in WSL, and no Linux-side containment path has
been implemented and verified. Read-only Gateway/core checks remain available.
Companion installation uses a separate Windows-only path. The worker creates
the installer suspended, assigns it to a kill-on-close Job Object before
resuming it, waits for the whole job to become empty, and reports termination as
`unknown` when emptiness cannot be verified. It has no unsuspended fallback.
The backend accepts apply requests only while an authenticated worker has
reported this complete Companion capability within the prior 90 seconds;
authenticated polling and permit requests renew that bounded observation.
Missing, unsupported or stale capability blocks new apply jobs. An owner must
also explicitly select `auto_install_verified`; exact-version, publisher,
runtime-health and emergency-stop checks remain in force. Ambiguous or
unhealthy attempts remain durably recorded as `needs_review` and are never
replayed. Choosing `observe` disables new automatic applies without disabling
read-only checks. Setting `HAI_OPENCLAW_MAINTENANCE_ENABLED=false` disables the
maintenance handler, worker polling and scheduler entirely.

Companion version discovery does not require a publisher thumbprint. When a
newer Companion release exists, a missing/invalid pin or certificate mismatch is
reported as a blocked update, never as publisher-verified or install-ready.
Configure `HAI_OPENCLAW_PUBLISHER_THUMBPRINT` only after independently verifying
the expected certificate identity; the pin is read by the Windows worker, not
the backend. A valid pin alone is insufficient: the fresh worker capability,
explicit owner-selected policy and all runtime gates must also pass. Updates are
blocked while a delegated session, prior review, or emergency stop is
unresolved. Failed read-only checks
retry without authorizing an install; after repeated failures, checks continue
daily but the last known release is never used as install evidence. Legacy
automatic policies without a recorded owner are moved to `observe`; explicit
owner-selected policies are preserved across restarts. Ambiguous or unhealthy update attempts stop task
admission and require owner review.

## Execution boundaries and status semantics

- The worker runs as the configured interactive Windows user, not as a Windows
  service, Administrator or SYSTEM. It reads that account's `%LOCALAPPDATA%`
  Companion files and invokes the configured WSL distribution under that user's
  WSL registration. A scheduled task requiring interactive sign-in cannot run
  while the account is logged out; backend catch-up does not make the worker
  available.
- The worker is not a security sandbox. The fixed command allowlist and
  loopback-only API constrain this integration's inputs, but child commands
  inherit the Windows user's OS permissions. Job Object containment applies
  only to the Windows Companion installer process tree; it does not contain
  WSL/Linux descendants or provide a restricted Windows token.
- Companion installers are created with `CREATE_SUSPENDED`, assigned to a
  kill-on-close Job Object before their primary thread resumes, and never use an
  unsuspended fallback. HAI waits for the process and all job members to exit;
  if the job cannot be verified empty, the receipt reports `unknown` and stays
  `needs_review`. Gateway/core WSL updater execution is rejected before it
  starts. Older or externally started installers are outside this guarantee and
  must be inspected before owner review.
- A pending job older than 20 minutes is treated as stale. Stale checks become
  failed with a bounded retry; stale apply jobs become `needs_review` and are
  never replayed. An expired leased apply is also `needs_review`, followed only
  by a deduplicated read-only recovery check.
- Worker contact is an authenticated request timestamp held in backend process
  memory. `recent_contact` means a request arrived within three minutes;
  `stale_contact` means it did not. Neither state proves the worker process is
  running or stopped. After a backend restart the state is `unknown` until a
  new authenticated request arrives.
Dashboard routes require the authenticated owner and admin permission.
The worker uses a dedicated bearer token, at least 32 characters, plus the
backend's existing internal API key. Reusing a Gateway, delegation, host-runtime
or backend API credential as the maintenance token disables the handler.

Backend environment:

```dotenv
HAI_OPENCLAW_MAINTENANCE_ENABLED=true
HAI_OPENCLAW_MAINTENANCE_TOKEN=<dedicated-random-secret>
```

Set the flag to `false` to opt out. Do not use a token shared with another HAI
runtime or service.

Windows worker environment:

```dotenv
HAI_OPENCLAW_MAINTENANCE_URL=http://127.0.0.1:17070
HAI_OPENCLAW_MAINTENANCE_TOKEN=<same-dedicated-random-secret>
BACKEND_API_SHARED_KEY=<existing-internal-backend-key>
HAI_OPENCLAW_PUBLISHER_THUMBPRINT=<independently-approved-Companion-certificate>
```

Place secrets in a protected local configuration, not command-line arguments,
Git, screenshots or logs. The URL must be an HTTP loopback IP, with no userinfo,
query or path. Do not expose the backend worker route publicly or run the worker
as Administrator or SYSTEM. It uses the current Windows user's Companion and
WSL installation. Deployment must arrange its continued operation under that
user; a built executable alone is not an installed background service.

Windows release builds now bundle the executable and an **OpenClaw maintenance**
start-menu shortcut. Its launcher reads the installed `hai.env`, validates the
configuration and starts only one launcher per Windows session. Set `BACKEND_PORT`
to the actual port; an explicit maintenance URL must match
`http://127.0.0.1:<BACKEND_PORT>`. Empty URL overrides are rejected. Use literal
random secrets (letters, digits, `_+/=.-`); dotenv expansion is not supported by
this launcher. `-ValidateOnly` checks settings without connecting or executing.
The start-menu shortcut is an explicit manual launch path. Separately,
`Start-HAI.ps1` registers the least-privileged, current-user scheduled task only
when `HAI_OPENCLAW_MAINTENANCE_ENABLED=true` and all worker prerequisites pass.
The task starts an initial bounded poll, runs at user logon, and repeats hourly
within its daily 03:00 schedule while that user is signed in. When maintenance
is disabled, registration removes only a task that passes HAI ownership checks.
The launcher waits up to five minutes for the shared worker/installer mutex
before failing, so a short active poll or installer handoff can finish without
being interrupted. If the lock remains held, the launcher exits with an error
and Task Scheduler's bounded restart policy retries; it never kills the current
owner. Task registration and removal use a non-waiting lock attempt so startup
and uninstall do not hang behind a worker. Task Scheduler ignores a second
instance of its own task while one is running. These local controls only
serialize the Windows worker and installer; backend leases and OpenClaw session
checks remain authoritative for whether an update may run alongside Gateway
work.
Task setup errors do not prevent an otherwise healthy HAI stack from starting.
This describes the installer code path; it does not prove that a task is
registered or persisted on a particular computer.

From `backend`, using the Go version pinned in `go.mod`, build the Windows worker:

```powershell
go build -o "$env:LOCALAPPDATA\HAI\hai-openclaw-maintenance.exe" ./cmd/hai-openclaw-maintenance
```

Create the output directory first if it does not exist. With the environment
configured, launch that executable without arguments to poll HAI once a minute.
The installed Windows scheduled task is different: it runs at user logon, at
03:00 local time, and hourly while the interactive account is signed in, so a
one-hour backend retry can actually be picked up. It does not run while the user
is logged out. Idle worker polls contact only HAI on loopback; only a due check
contacts upstream. Deploy the matching backend and frontend and apply migrations
`0072` and `0083` using the repository's normal migration process before
connecting the worker.

Launcher diagnostics are stored in
`%LOCALAPPDATA%\HAI\logs\openclaw-maintenance-launcher.log`. Each file is capped
at 1 MiB and four files total (the current log plus three numbered rotations)
are retained under a cross-process mutex. Rollover removes only the oldest HAI
launcher log. These local diagnostic files are not the audit ledger. Durable
maintenance receipts remain in HAI's database.

`hai-openclaw-maintenance.exe --once` processes at most one queued job and exits.
It uses the same authenticated pull client as continuous polling. Read-only
checks may proceed; a Companion `apply` is considered only with fresh worker
capability, explicit `auto_install_verified` policy, exact-version and publisher
verification, a fresh server permit, and the contained Windows installer path.
Gateway/core `apply` remains blocked before runtime or network inspection because
WSL process containment is not verified. Checks are not dry runs: they contact
the installed runtime and release service. Use `check` below for direct read-only
runtime inspection. Unknown command-line options are rejected instead of
accidentally starting an indefinite worker.

The pull client rejects expired or malformed leases, confirms admission before
execution, and rechecks installation permission after upstream preflight. While
an update is running, it rechecks the permit every two seconds; a denied or
unavailable check requests cancellation and records `needs_review`, then stops
the worker after its receipt attempt. Cancellation still cannot prove
termination of Windows/WSL descendants, which is why this build blocks apply
before launch.
It never forwards credentials across HTTP redirects or through an environment
proxy. Only exact receipt delivery is retried (up to three attempts), never the
installer. Failure and unconfirmed receipt delivery produce a nonzero exit in
`--once` mode; continuous polling logs a normalized diagnostic without upstream
error bodies.

Read-only verification does not require a backend connection:

```powershell
& "$env:LOCALAPPDATA\HAI\hai-openclaw-maintenance.exe" check companion
& "$env:LOCALAPPDATA\HAI\hai-openclaw-maintenance.exe" check gateway_core
```

These print normalized version metadata. `healthOk: false` in a check-only
receipt means no post-install health probe was performed; it is not a failed
health diagnosis. Companion publisher verification is needed when a newer
installer is available, not when merely reporting the current version.

## Verification boundaries

Companion downloads must come from the fixed official Windows repository and
match its release-asset SHA-256. Authenticode must validate against the configured
certificate thumbprint. Missing digests or publisher rotations require review.
Only bounded redirects to GitHub's release-asset host are permitted.

Gateway/core read-only checks use the installed OpenClaw updater's stable npm
channel. HAI verifies npm's ECDSA signature over the exact package version and
integrity metadata. This is registry signature verification, not Windows
Authenticode and not a source-code audit. HAI does not invoke the Gateway/core
updater: installation remains blocked until a separately designed and tested
Linux-side containment boundary exists. The evidence digest stays bound to the
checked release even if upstream publishes another version in the meantime.

The upstream core updater can also synchronize OpenClaw plugins, refresh shell
completion and restart its Gateway/doctor checks. HAI does not sandbox those
upstream actions or guarantee that vendor code leaves all local configuration
unchanged. The update policy is therefore a trust decision about the official
updater. HAI never changes its own delegation flags or imports upstream plugin
permissions as HAI authority. Health/version checks do not prove full protocol,
plugin or delegated-task compatibility with every new release.

The admission lock covers HAI's OpenClaw adapter and recorded HAI sessions, not
independent tasks started directly through the Companion or another client.
Check those external clients before enabling unattended updates. This feature
does not provide OS-level exclusion or claim updates are safe under every
circumstance. Emergency stop blocks update admission; it cannot undo an installer
that already changed files. Once a Companion update starts, HAI rechecks its
permit every two seconds. Revocation cancels the contained installer job, and
the worker verifies the whole Job Object is empty before reporting termination
as verified. If it cannot prove the job is empty, the receipt reports
`unknown`, remains `needs_review`, and is never replayed. After cancellation,
the worker attempts to record the review receipt and exits instead of leasing
more work. Gateway/core WSL updater execution remains blocked; Windows Job
Objects are not evidence about Linux descendants. For a previously persisted HAI
Gateway run, local context cancellation does not prove remote cancellation. New
delegated session creation is blocked until HAI can verify an identity-bound
sandbox-required role and run-bound effective policy. HAI permits only owner-bound
`sessions.abort { key, runId }` and exact-run `agent.wait`; an abort acknowledgment
does not prove terminal state. Ambiguous effects remain blocked for owner review.

## API

- `GET /api/v1/openclaw-maintenance`: stored target status, pending job state
  and start, install capability, and last authenticated worker contact.
- `POST /api/v1/openclaw-maintenance/:target`: `check`, `apply`, `policy` or `review`.
- `POST /api/v1/openclaw-maintenance-worker/leases`: lease one authorized job.
- `POST .../leases/:id/confirm`: start a lease once.
- `POST .../leases/:id/permit`: recheck authority after preflight and periodically during updates.
- `POST .../leases/:id/complete`: submit normalized receipt; never raw command output.
- `POST /api/v1/openclaw-sessions/:eventId/reconcile`: owner-authorized exact-run terminal observation; available during emergency stop and never session-wide.

Policy names are `observe` and `auto_install_verified`. Job receipts contain
version/digest evidence, not transcripts, prompts or credentials. Policy changes
and recovery acknowledgements are retained in the job ledger.

## Testing and rollout evidence

`internal/openclawmaintenance` includes unit tests for versions, signatures,
request framing, credential separation, authenticated capability expiry and
target-specific blocking. Windows-only tests exercise Job Object descendant
termination and whole-job completion. Windows cross-compilation verifies that
the containment implementation builds; it does not replace running these tests
on Windows. PostgreSQL integration
tests. Set `TEST_OPENCLAW_MAINTENANCE_DSN` to a disposable PostgreSQL database
to execute the latter. Each test creates and removes only its own generated
schema. Without that variable, database tests explicitly skip.

`TestPullClientPostgresLifecycleAndLostAcknowledgement` connects the same client
used by the Windows executable to real maintenance handlers and PostgreSQL. It
checks check/apply/permit/receipt persistence and a lost acknowledgement after
database commit. Its installer and health verifier are test substitutes, so it
does not prove an actual OpenClaw installation.

On Windows, additionally set `TEST_OPENCLAW_MAINTENANCE_LIVE_CHECK=true` and run
`go test ./internal/openclawmaintenance -run TestPullClientLiveGatewayCheck -v`
with the disposable DSN to test real WSL Gateway version/registry inspection
through that HTTP and persistence chain. This opt-in test permits only
`gateway_core` checks, keeps policy at `observe`, and writes receipts only to
its generated test schema. It does not install, run an AI task or alter live HAI
records. It requires the installed `OpenClawGateway` WSL distribution and network
access to the official release registry.

On 2026-09-04, the Windows executable's real read-only checks found Companion
`2026.7.1-4` current and Gateway/core `2026.6.10` with signed registry metadata
for `2026.9.1`. The official updater dry run confirmed plugin synchronization
and Gateway restart. No installation was performed by those checks.

A subsequent supervised update on the same date installed core `2026.9.1`.
Its first attempt rejected the bundled Node `22.22.0`. Node `22.23.2` was then
installed from the official archive after verifying SHA-256
`d60acfe00a2932254bb0ad20e01b0d74397a0875595de719654b214f4b03f307`
against [Node's checksum manifest](https://nodejs.org/dist/v22.23.2/SHASUMS256.txt).
The configuration and original Node executable were backed up. The legacy
versioned Node path used by the service/updater was pointed at the verified
runtime while preserving the existing OpenClaw package location. This dependency
repair was supervised, not an automatic capability of the HAI worker.

The updater installed core but its doctor step initially still used the old
Node path. After repairing that path, the official doctor completed and the
existing service was started. A fresh worker probe reported installed and
available `2026.9.1`; Windows loopback `/health` returned `ok: true, status: live`.
The Windows Companion was started to manage the local Gateway. This is local
runtime proof, not a successful HAI delegated-task acceptance test.

The new release created heartbeat, dreaming and skill-review automations with
no recorded executions. The newly created dreaming job was disabled through
the official CLI. The system-owned monitors cannot be edited as ordinary cron
jobs, so the documented `cron.enabled=false` setting was applied to prevent
new scheduled OpenClaw agent activity outside HAI's coordination. Manual and
event-driven work remains governed separately. This setting is not a substitute
for tool policy or a sandbox; future adoption of upstream scheduling must be
deliberately wired to HAI budgets and approvals.

PostgreSQL maintenance integration tests passed earlier in the session. A later
repeat timed out while connecting to the disposable database as Docker stopped
responding reliably; the live HAI backend readiness request also timed out.
No Docker/WSL-wide restart was performed because other projects share it.

Docker later recovered. The final maintenance test run with the disposable
PostgreSQL DSN passed again (4.046 seconds). The HAI backend container reached
the Gateway health endpoint successfully, and backend readiness returned
21 OK checks, zero warnings and zero failures. The disposable test container
was stopped afterwards. These point-in-time checks do not erase the earlier
intermittent Docker failures or prove unattended reliability.

Focused maintenance frontend tests passed (4 tests), and the production build
passed with the existing unrelated pursuits stylesheet budget warning. Go
maintenance, agent-runtime, router and migration checks also passed. Rendered
browser acceptance of the new controls remains outstanding.

On 2026-09-05, Windows installer source contracts and the new launcher tests
passed, including execution of validation-only mode under Windows PowerShell
5.1. Tests cover credential separation, loopback port matching, duplicate dotenv
entries, opt-in enforcement and unchanged parent environment during validation.
They do not prove an installed start-menu shortcut, Windows startup registration,
or a live queue-to-worker installation. No release installer was produced from
the dirty development worktree during these checks.

The subsequent 2026-09-05 pull-client integration pass verified the complete
check and apply receipt lifecycle against real handlers and PostgreSQL, including
an acknowledgement lost after commit without repeating installation (the apply
executor remained a test substitute). A separate opt-in read-only run used the
real Windows/WSL OpenClaw installation and official registry through the same
client, handlers and disposable database: installed and available versions were
both `2026.9.1`, with publisher verification true. The two integration tests
passed in 7.699 seconds. This proves the real version-check chain, not a live
installation, delegated AI task, startup service or production HAI deployment.

During this pass, Windows connections to the test database intermittently timed
out while in-container SQL remained available. Only the identified disposable
test container was restarted; the final checks then passed and that container
was stopped. No Docker-wide restart or live HAI data changes were made. Keep this
environment reliability issue separate from protocol-test success.

This source addition is not itself evidence that the live HAI deployment has
been rebuilt, migration `0072` applied, the Windows worker registered at startup,
or unattended installation accepted. Complete those deployment and controlled
installation gates before claiming always-on update coverage.

Upstream references: [OpenClaw update CLI](https://docs.openclaw.ai/cli/update),
[Windows releases](https://github.com/openclaw/openclaw-windows-node/releases),
[npm registry signatures](https://docs.npmjs.com/about-registry-signatures).
