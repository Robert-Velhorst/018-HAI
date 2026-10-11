# Browser-level end-to-end tests

This self-contained Playwright suite drives the real HAI UI against a running
stack. Its dependencies do not affect the Angular application build.

## Covered operator chain

`tests/acceptance.spec.ts` proves:

```text
password login
-> owner-scoped read-only local source registration
-> bounded source sync
-> explicit pursuit creation
-> project-matched high-risk workflow intake
-> exact runtime selection
-> durable human approval
-> one exact selected read-only execution
-> terminal completion with deterministic verification evidence
```

The source path is the dedicated, empty `connected-sources/e2e-fixture/`
subfolder in the read-only mount. The suite does not
authorize an external provider or request an irreversible operation. Its runtime
target is the backend's real `GET /readyz` endpoint. Missing controls, failed
proposal resolution, an unexpected execution route, non-terminal workflows, or
absent verification evidence fail the test; no acceptance step is optional. All
temporary workflow and pursuit records are archived, the temporary source is
paused, and the disposable readiness automation is deleted by the `afterEach`
cleanup hook on both successful and failed runs.

`tests/authenticated-routes.spec.ts` separately opens a loopback-only local
preview session and navigates to every module through the shared shell. It
verifies the URL, title, active shell identity, visible module outlet, and
absence of uncaught page errors. The route audit is read-only and does not need
operator credentials or the mutation acknowledgement.

## Run it

```powershell
# Start HAI from the repository root.
docker compose --env-file .env.local -f docker-compose.local.yml up -d --build

# Install and run from frontend/e2e.
npm install
npx playwright install chromium
$env:E2E_BASE_URL = 'http://localhost'
$env:E2E_OPERATOR_EMAIL = 'your-seeded-operator@example.com'
$env:E2E_OPERATOR_PASSWORD = 'your-local-password'
$env:E2E_ALLOW_MUTATION = 'true'
npm test
```

Credentials come from the environment and are never committed. The test is
skipped unless both `E2E_OPERATOR_PASSWORD` and the explicit
`E2E_ALLOW_MUTATION=true` acknowledgement are present. `E2E_BASE_URL` has no
usable default, preventing accidental writes to an unintended stack.

## Retire artifacts from older acceptance runs

Older revisions of this suite did not clean up. Inventory the exact strict-prefix
records first, then apply the authenticated, API-level retirement only after
reviewing the counts:

```powershell
.\scripts\cleanup-e2e-artifacts.ps1 -EnvFile .\.env.local
.\scripts\cleanup-e2e-artifacts.ps1 -EnvFile .\.env.local -Apply -Confirm:$false
```

The command archives workflows and pursuits, pauses sources so their audit
history remains available, and deletes only disposable automations whose names
start with `E2E backend readiness `. It does not run raw database mutations.

## Current evidence

The suite passed against the rebuilt local Windows Compose stack on 2026-08-14
in 9.1 seconds. A direct read-only database inventory after the run confirmed
zero active E2E automations, sources, pursuits, and workflows. Paused source
records remain as audit history by design. See `docs/completion-matrix.md` for
the exact acceptance scope and remaining external gates.
