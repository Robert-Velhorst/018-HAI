# Isolated HAI acceptance

Source-reviewed 2026-09-30; evidence reconciled 2026-10-01 Europe/Amsterdam.
Production readiness is staged and incomplete. This guide describes the default paused path and
explicit manual-local extension in
`scripts/isolated-acceptance-stack.ps1`, with guard coverage defined by
`scripts/test-isolated-acceptance-stack.ps1`. Commands below are instructions
for an authorized disposable local run, not evidence that this documentation
pass executed them. Current results and open gates live in
[the October partial checkpoint](verification-2026-09-30-runtime-hardening.md#2026-10-01-staged-production-readiness-partial-checkpoint).

## Safety boundary

- Use a fresh generated project, never the normal installation's Compose
  names, personal `.env*` files, databases, source folders, or volumes.
- Prepare derives configuration from `.env.example`, disables `_ENABLED`
  flags (only the backend manual worker is excepted in explicit manual-local
  mode), generates synthetic secrets, and temporarily clears/restores caller
  Compose interpolation variables. It does not edit the personal environment.
- The nine services are `idp`, `backend`, `backend-migrate`,
  `backend-runtime-role`, `frontend`, `nginx`, `postgres-idp`,
  `postgres-automation`, and `redis`. Migration and runtime-role provisioning
  are one-shot jobs; nine configured containers does not mean nine daemons.
- Only nginx joins both `ingress` and internal `isolated` networks. All other
  services join only `isolated`. Only nginx port 80 is published, on literal
  `127.0.0.1` at the selected non-default host port. Ingress is not internal;
  this is not an OS sandbox or a claim that image builds cannot access registries.
- Persistent/anonymous Docker volumes are forbidden. PostgreSQL data and
  Redis `/data` are tmpfs; backend scratch directories are also tmpfs. The
  copied `init.sql` enables `uuid-ossp`. Allowed binds are read-only and inside
  the fresh evidence directory: source fixture, initializer, nginx files, and
  role-provisioning script. No personal source mount is needed.
- OAuth, SMTP, provider credentials and LLM accounts are absent. All automatic
  schedulers remain disabled in both modes; default phase 2 is paused. Only the
  reviewed manual-local extension below changes its bounded processing gate.
  Local HTTP uses
  non-secure synthetic cookies only; do not expose this gateway externally.
- `manifest.json`, `synthetic.env`, and resolved `compose.json` contain private
  synthetic credentials. Do not print, commit, attach, publish, or quote them.
  Redact diagnostics and browser artifacts before sharing them.

## Prerequisites and port/subnet checks

Use PowerShell and a running Docker engine with Compose v2 supporting JSON
configuration and `up --wait`. Browser work additionally needs the runtime
required by `frontend/e2e/package.json`, its locked dependencies, and Chromium.
Review dependency/image downloads and available resources before authorizing
Start. Never repair resource pressure with a broad prune or volume deletion.

Run from the repository root. Default port is **18080**; choose another unused
port if necessary, without stopping its current listener. The script also
tests loopback binding immediately before Start.

```powershell
$port = 18080
Get-NetTCPConnection -LocalPort $port -State Listen -ErrorAction SilentlyContinue
docker ps --format '{{.Names}} {{.Ports}}'
```

Without subnet parameters Docker allocates the two networks. If explicit
subnets are necessary, supply **both**, distinct and independently verified
unused. The accepted form is `10.254.N.0/24`, with N from 1 through 254.
The script checks shape/pairing, not all host/VPN/LAN routing conflicts.
Inspect existing Docker IPAM and host routes, including VPN/WSL networks:

```powershell
$networkIds = @(docker network ls -q)
if ($LASTEXITCODE -ne 0) { throw 'Cannot inventory Docker networks.' }
foreach ($id in $networkIds) {
    $n = (docker network inspect $id | ConvertFrom-Json)[0]
    if ($LASTEXITCODE -ne 0) { throw 'Cannot inspect Docker network.' }
    [pscustomobject]@{ Name = $n.Name; Subnets = ($n.IPAM.Config.Subnet -join ',') }
}
Get-NetRoute -AddressFamily IPv4 | Select-Object DestinationPrefix, InterfaceAlias
```

Check for overlapping ranges, not just matching strings. Do not reuse the
historical evidence run's subnets as assumed-free defaults. Recheck just
before Start; retain the chosen ranges privately with the run evidence.

## Prepare

```powershell
$launcher = Join-Path (Get-Location) 'scripts/isolated-acceptance-stack.ps1'
$prepareArgs = @{ Action = 'Prepare'; Port = $port }
# Only after verifying both ranges are unused, add:
# $prepareArgs.Subnet = $verifiedInternalSubnet
# $prepareArgs.IngressSubnet = $verifiedIngressSubnet
$prepared = & $launcher @prepareArgs
$prepared
$line = @($prepared | Where-Object { $_ -like 'Prepared: *' })
if ($line.Count -ne 1) { throw 'No unique prepared evidence directory.' }
$evidence = $line[0].Substring('Prepared: '.Length)
```

Prepare always creates a fresh `%TEMP%/hai-acceptance-<owner>` directory;
do not pass `-EvidenceDirectory` to Prepare. The project name is generated as
`hai-acceptance-<first 12 owner characters>` and each resource has an owner
label. Preserve the printed path. Preparation renders configuration but starts
no containers. A failed preparation leaves its evidence directory for review.

## Validate

```powershell
& $launcher -Action Validate -EvidenceDirectory $evidence
```

Validate starts no containers. It checks the exact service/network set,
ownership, disabled settings, synthetic database bindings, loopback publication,
absence of privileged/host-namespace access, and evidence-only read-only binds.
PostgreSQL image-declared storage, including the role helper, must be covered
by tmpfs to avoid anonymous volumes. Stop on a validation failure; do not
bypass it or substitute the normal installation's configuration.

The separate isolation test script prepares disposable configuration and checks
34 paused-mode negative cases plus nine external-configuration negative cases
in manual-local mode. Each of those nine cases in both modes invokes the actual
launcher `Start` path and requires refusal before any Docker call (Docker is
mocked). It also covers poisoned inherited variables, caller restoration,
failure restoration, wrong-owner cleanup refusal before mutation, and rejection
of automatic scheduling in manual-local mode. The parent reports these checks
passed: 34 paused negatives, nine manual-local negatives and 18 actual `Start`
refusals before any Docker call. This documentation pass did not rerun them.
The [CI report](../output/ci-isolation-production-20260930.md) separately records
76 passing local contracts, guarded startup/cleanup and trace-disabled browser
wiring; hosted execution remains unverified.
Running it is optional local guard verification, not browser/full-stack proof;
it retains temporary evidence and starts no acceptance services:

```powershell
& .\scripts\test-isolated-acceptance-stack.ps1
```

## Explicit manual-local extension

For an authorized operator mutation test, choose the mode **before preparing
a fresh stack**, not by editing an existing paused manifest/configuration:

In the Prepare block above, insert this line after creating `$prepareArgs`
and before invoking `$launcher`:

```powershell
$prepareArgs.ExecutionMode = 'manual-local'
```

Then complete Prepare, Validate and Start against that new `$evidence`.

The launcher persists the mode in the manifest. Validate/Start derive the
mode from that manifest; a later `-ExecutionMode` argument does not convert an
existing run. Omitting the mode at Prepare retains `paused`.

The exact extension is `SOURCE_MANUAL_WORKER_ENABLED=true`, a 15-second source
worker poll, and `HAI_PHASE2_MODE=autonomous_safe`. Every automatic scheduler
and every other `_ENABLED` flag remains false. Backend stays exclusively on the
internal network; the fixture is a read-only `acceptance.txt` inside this run's
evidence directory. No providers, accounts, feeds, OAuth or SMTP are configured.
The phase-2 setting permits only the exact owned synthetic acceptance work;
it grants no broad autonomy, personal-installation access, or external effect.
The governed read-only backend-health probe still needs its exact approval
and verification path.

The worker claims/recoveries are restricted to queue `source`, kind
`source.manual_sync`, one job per poll. It does not register automatic scan,
sync, webhook or extraction-correction handlers. The flag is **not a local-only
connector allowlist**: pre-existing manual jobs could resume on a populated
installation. Never use a personal stack for this exercise. Owner/source
eligibility, readable clear emergency-stop state, source leases and global
background policy still apply. A queued/deferred job is not completion;
pause/stop checks do not establish instantaneous in-flight rollback or
at-most-once external effects. See the
[manual worker handoff](../output/manual-source-worker-20260930.md) for focused
test evidence and the later
[real PostgreSQL proof](../output/manual-worker-postgres-20260930.md): six
named tests repeated three times under race detection, 18 PASS with zero
skips/failures, dedicated PostgreSQL 17.11 tmpfs fixtures and exact owned
cleanup. This adds concurrent claim, retry, lease and persisted reload/local
extraction coverage. New pools/services are not a full process/server restart;
fixture AutoMigrate is not the canonical migration/constraint/index set.
Final rebuilt-stack browser and privacy acceptance remain separate gates.

## Start

Only run this once the disposable startup is authorized:

```powershell
& $launcher -Action Start -EvidenceDirectory $evidence
```

Start revalidates configuration, refuses any existing container with that
owner (including exited records), checks the loopback port, builds/starts
with `--wait --wait-timeout 300`, and verifies `/login` returns HTTP 200 with
`<app-root`. It retains `start.log` and prints the actual acceptance URL.
That readiness check is not login, ingestion, worker completion, or browser
acceptance. Failure may leave partial owned resources; Inspect them, retain
evidence, and use the guarded Stop rather than retrying over the old owner.
After Stop, prepare a fresh run; stopping tmpfs services discards fixture data.

## Inspect and collect bounded evidence

```powershell
& $launcher -Action Inspect -EvidenceDirectory $evidence
```

Inspect displays the owned project's `compose ps -a`; it is not a full runtime
isolation audit. Privately verify actual resource labels, networks, published
ports, tmpfs and mount types against the prepared configuration before any
browser actions. Project/owner/container names must agree; verify **zero
mounts of Type `volume`**, not zero binds. Never dump container environment
or resolved Compose configuration into a shareable log.

The authenticated browser guard requires `E2E_ISOLATED_STACK=true`, a literal
loopback `E2E_BASE_URL` with an explicit non-default port and no path/query,
and a synthetic owner from this manifest. Pass credentials privately through
process environment, not command-line values or report prose. Preserve/restore
the caller environment. With dependencies and Chromium already installed,
the following scopes browser artifacts to this evidence directory. The
mutation flag is a **separate explicit opt-in**, required for the operator
test's local source/pursuit/workflow/approval records. Set it to `true` only
for the authorized manual-local synthetic run; leaving it unset can skip the
operator test and must not be reported as acceptance. Keep `--trace=off` in
the command: traces can retain the login request body and synthetic credentials.
Disabling tracing does not sanitize reports, screenshots, videos or logs;
retain the existing artifact review/redaction boundary:

```powershell
$keys = @('E2E_BASE_URL', 'E2E_ISOLATED_STACK', 'E2E_OPERATOR_EMAIL',
          'E2E_ALLOW_MUTATION', 'PLAYWRIGHT_HTML_OUTPUT_DIR')
$keys += 'E2E_OPERATOR_' + 'PASSWORD'
$saved = @{}
foreach ($key in $keys) {
    $saved[$key] = [Environment]::GetEnvironmentVariable($key, 'Process')
}
try {
    $privateManifest = Get-Content -LiteralPath (Join-Path $evidence 'manifest.json') -Raw | ConvertFrom-Json
    if ($privateManifest.executionMode -ne 'manual-local') {
        throw 'Operator mutation acceptance requires a fresh authorized manual-local stack.'
    }
    $env:E2E_BASE_URL = "http://127.0.0.1:$($privateManifest.port)"
    $env:E2E_ISOLATED_STACK = 'true'
    $env:E2E_OPERATOR_EMAIL = $privateManifest.email
    $env:E2E_OPERATOR_PASSWORD = $privateManifest.password
    $env:E2E_ALLOW_MUTATION = 'true'
    $env:PLAYWRIGHT_HTML_OUTPUT_DIR = Join-Path $evidence 'browser-report'
    npm.cmd --prefix .\frontend\e2e test -- --trace=off --output (Join-Path $evidence 'browser-results') 2>&1 |
        Tee-Object -FilePath (Join-Path $evidence 'browser-tests.log')
    if ($LASTEXITCODE -ne 0) { throw 'Browser run failed; retain its report.' }
} finally {
    foreach ($key in $keys) {
        if ($null -eq $saved[$key]) {
            Remove-Item -LiteralPath "Env:$key" -ErrorAction SilentlyContinue
        } else {
            Set-Item -LiteralPath "Env:$key" -Value $saved[$key]
        }
    }
    $privateManifest = $null
}
```

Use a fresh run for each final report; do not overwrite failed historical
artifacts. Missing credentials can skip authenticated tests, so an exit code
alone is insufficient: record named PASS/failure/skip counts and the command,
source hashes/built assets, project owner, runtime, actual viewport widths,
and screenshot/trace availability. Redact private artifacts before sharing.

For actual manual-source acceptance, retain evidence from the authenticated
synthetic owner showing **all** of the following, as asserted by
`frontend/e2e/tests/acceptance.spec.ts`:

1. `POST /api/v1/sources/<sourceId>/sync-jobs` returns HTTP 202,
   `mode=manual_async_sync`, the exact created `sourceId`, and a UUID job `id`.
2. Poll `GET /api/v1/sources/sync-jobs/<id>` for that exact returned job
   (the test allows 120 seconds). Every response must retain that `id` and
   `sourceId`; persisted `status` must reach `completed`.
3. Read owner-scoped `GET /api/v1/sources/extractions?limit=100` and verify
   the same `sourceId`, a `sourceUri` ending `/acceptance.txt`, and text
   `Synthetic HAI acceptance source. No personal records.`

A toast, HTTP 202, worker startup message, unrelated completed job, unit test,
or extraction for another source is insufficient. This source proof alone
does not prove later workflow approval/execution, full-stack restart/concurrency safety,
live connectors, or a full browser-suite PASS.

The governed operator path must also exercise the mandatory action-bound
approval, not bypass it for the read-only probe. Before approval, assert the
selected run is `blocked`, API state `needs_approval`, zero execution attempts,
and the approval-proof-required message. The UI displays `needs approval`.
Approve that exact workflow through its UI control, retain its matching
approval response, then retry the selected run and verify terminal completion
and verification evidence. Current test assertions encode this sequence;
they are not evidence that the pending focused operator run passed.

**Default-mode limitation:** `SOURCE_SCHEDULER_ENABLED=false` prevents the
durable source runner from starting; phase-2 background policy is paused.
With the manual flag also false, submission must return HTTP 503 without new
history/queue/extraction writes. Manual async source work is blocked in this
default mode. Queue acceptance is not
completed ingestion/extraction, and no LLM account exists for a real bounded
task. The full operator execution test can therefore fail at these intentional
gates. Do not enable flags, providers, or personal mounts to force it green.
Use only the explicit manual-local extension above for separately authorized
synthetic mutation acceptance, with terminal-job/postcondition proof.

## Stop without touching personal data

Capture required diagnostics first: tmpfs database/Redis contents are ephemeral.

```powershell
& $launcher -Action Stop -EvidenceDirectory $evidence
```

Stop reads the manifest and inspects actual owner-labeled container IDs. It
requires matching project/owner/name and refuses unexpected Docker volumes
before container deletion. It stops/removes only those exact IDs, then removes
only correctly named/owned networks with no attached containers. It intentionally
does not require the old startup configuration to validate, allowing guarded
cleanup after configuration repair. On a refusal, retain evidence for review;
do not work around it with broad deletion.

Independently verify both `docker ps` and `docker ps -a` have no containers for
that exact owner, both owned networks are absent, and the chosen loopback port
is closed. No-running-containers is weaker than complete removal. Record any
remaining resources without deleting unrelated ones. Keep evidence files,
source/worktree data, images, caches, and every pre-existing volume/network.
Never use volume deletion, broad Compose cleanup, prune, or personal-folder
cleanup as part of this acceptance guide.

## Evidence status

As of 2026-10-01 Europe/Amsterdam, the historical isolated browser report
remains **9/12 PASS, 3 FAIL**, not completion. The final integrated Angular
run-5 log ends **1131 SUCCESS** on Chrome Headless 151; the earlier interrupted
run-3 log remains non-passing historical evidence. The parent reports removal
of exactly the old owner's nine containers and two networks after daemon
exit 255, without deleting files/caches/volumes. The parent now reports owner
`6cfa099c027a455bb3cba587f5b1f0ff` started and fully healthy in synthetic
manual-local mode. Run 1 ended **11/12 PASS, 1 FAIL** because the mandatory
action-bound approval path was not yet exercised by the operator test. Run 2
also ended **11/12 PASS, 1 FAIL**: its assertion expected API `needs_approval`
instead of UI `needs approval`. The parent corrected the assertion; focused
operator run 3 with UI approval/retry remains ongoing. Neither failed run is
converted to PASS by changing its test.

Direct-read [structured-redaction](../output/structured-redaction-20260930.md)
and [automation-projection](../output/automation-public-projection-20260930.md)
reports now establish scoped locally verified fixes. The parent reports review
completed, but the running image predates these fixes. The final full-stack
rebuild, privacy-inclusive browser acceptance and final Playwright result are
**unverified here**, and will be appended by the parent after execution.
Local contracts, readiness, and synthetic acceptance do not imply
hosted CI, deployment, installed-product Windows approval, OS sandboxing,
production-data restore, accessibility certification, or live-provider success.
