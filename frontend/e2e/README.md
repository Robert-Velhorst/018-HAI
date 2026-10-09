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
-> project-matched low-risk workflow intake
-> exact runtime selection
-> one exact selected read-only execution
-> terminal completion with deterministic verification evidence
```

The source path is the read-only `connected-sources/` mount. The suite does not
authorize an external provider, request an irreversible operation, or present
the health probe as legal work. Its runtime target is the backend's real
`GET /readyz` endpoint. High-risk approval and legal-evidence boundaries are
covered by backend policy tests; this browser suite proves the separate
read-only operator path. Missing controls, failed proposal resolution, an
unexpected execution route, non-terminal workflows, or absent verification
evidence fail the test; no acceptance step is optional.

## Run it

The read-only login and route-guard smoke test is safe to run against a normal
local HAI instance and does not require credentials or create records:

```powershell
$env:E2E_BASE_URL = 'http://localhost'
npm run test:smoke
```

Authenticated tests must use a disposable acceptance stack, including the
read-only control-room checks. Do not start the normal local Compose file as a
second test installation: its fixed container names and named volumes can
reuse a personal database. The CI browser-acceptance job runs on a clean runner.
For local acceptance, use separate container names, a separate Docker network,
temporary database storage, and a unique port bound to literal loopback. Seed
only a synthetic owner account, disable providers and scheduled connectors,
and never reuse the personal `.env.local` or persistent volumes.

With that isolated stack already running, execute from `frontend/e2e`:

```powershell
npm install
npm run typecheck
npx playwright install chromium
$env:E2E_BASE_URL = 'http://127.0.0.1:18080'
$env:E2E_ISOLATED_STACK = 'true'
$env:E2E_OPERATOR_EMAIL = 'e2e-owner@example.test'
$env:E2E_OPERATOR_PASSWORD = '<the disposable owner password>'

# Real login, route, responsive layout, disclosure, and error rendering.
npx playwright test tests/control-room.spec.ts

# Additional operator flow that creates records in the disposable database.
$env:E2E_ALLOW_MUTATION = 'true'
npm test
```

Credentials come from the environment and are never committed. Authenticated
tests reject a missing isolation declaration, a non-loopback URL, default ports,
and a non-synthetic account. `E2E_ALLOW_MUTATION=true` separately authorizes only
the disposable operator flow; it does not make a personal installation safe.
Control-room tests block post-login mutation requests. Pure target-validation
and rendering-contract tests need no stack or account:

```powershell
npx playwright test tests/isolated-stack.spec.ts tests/render-contract.spec.ts
```

`control-room.spec.ts` covers the registry's operational routes at 375, 768,
1024, and 1440 pixels, per-module preferences, deep sections, reset behavior,
keyboard skip navigation, and truthful rendering when the workflow API fails.
Remove only the disposable test containers and network after acceptance; leave
all personal source files, volumes, and running services intact.

## Current evidence

Historical evidence: the operator suite passed against a rebuilt local Windows
Compose stack on 2026-08-04 in 10.7 seconds. That result does not verify the
current source or the subsequently added control-room tests. See
`docs/completion-matrix.md` and the dated verification reports for actual
acceptance scope and remaining external gates.
