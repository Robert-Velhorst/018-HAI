# Owned Browser Matrix Handoff

2026-10-01. Source preparation only, not a passing browser or production-readiness report.

## Exact Edit Ownership

Only these files were edited/added by this task:

- `frontend/e2e/tests/support/isolated-stack.ts`: literal-input loopback guard; reject URL normalization aliases.
- `frontend/e2e/tests/isolated-stack.spec.ts`: focused negative guard cases.
- `frontend/e2e/tests/support/owned-browser-matrix.ts`: fresh-session, evidence-bound real-stack fixture.
- `frontend/e2e/tests/owned-browser-matrix.spec.ts`: deterministic route/state/keyboard/uncertainty matrix.
- `frontend/e2e/tests/support/owned-browser-matrix-handoff.md`: this handoff.

Production frontend, backend, configuration, packages/locks, existing browser suites and all prior evidence are untouched. No Git mutation, executable check, browser, service, container, installer, download, provider/account access, deletion or descendant was performed. Local read-only shell/source/diff inspection and apply_patch were used. Frontend-testing guidance and checkout memory were consulted for scope; current findings below come from checked-out source/reports. Available browser tools were deliberately not invoked. Parent alone owns serialized executable verification.

## Source Findings, Not New Proof

- Existing `control-room.spec.ts` covers four widths, Basic routes and selected disclosure/focus paths, but not both themes/modes per module or console errors. Its post-login route guard aborts writes; it never manufactures API responses. Existing pure isolation/render contracts do not establish integration acceptance.
- `playwright.config.ts` defaults to `http://localhost` and enables failure recordings. It is unchanged. The new fixture rejects that fallback, requires an explicit matching baseURL, and creates its own fresh, unrecorded context rather than inheriting private sessions or HARs.
- Saved reports retain 11 PASS/1 FAIL operator results and eight passing inspection tests on an earlier rebuilt UI. `output/production-integration-20261001.md` explicitly leaves current-source browser acceptance open. Historical Angular counts/builds are not proof for this matrix or the newest uncertainty changes.
- Known operator blocker: the selected workflow stopped before approval with no capable model and missing framework/required-participant evidence (`docs/verification-2026-09-30-runtime-hardening.md`, final rebuilt UI section). No automation/participant exemption was added. Route inspection cannot close this terminal execution gate.
- Additional source gap: task history `copyPlanToComposer()` restores composer fields only, not the persisted receipt or its restriction. `rememberUncertainHistory()` only runs its restriction matching when an in-session uncertain command exists. The full-scope persisted-task regression expects the real selected historical receipt to remain visible in Basic and prevent another run/cycle. On current source this is expected to fail, not an observed browser failure. Production task owner must review/fix that path; this task does not modify it or inject component state.

## Safety And Coverage

Before login, fixture requires `E2E_ISOLATED_STACK=true`, literal `127.0.0.1` or `[::1]` with a non-default explicit port, synthetic `e2e-*@example.test`, and `E2E_ALLOW_MUTATION=true`. The latter permits only one exact credential-matched synthetic login POST. Every other write and external-origin request is aborted and fails health assertions. Service workers/download acceptance are off. Live same-origin reads continue unchanged; there is no fulfill/mock/fake JSON or backend-effect execution. API fixture reads have 10-second timeouts and no redirects.

The parent-owned `HAI_ACCEPTANCE_EVIDENCE` must exist with the launcher's private manifest. Version, owner/project relationship, email/password and port must match the caller; port must be 1025-65535. Results and HTML report destinations must resolve beneath it, including symlink-aware existing ancestors. This is a local ownership binding, not proof of actual Docker labels, compiled-image identity, egress isolation or database ownership; parent validates those before running.

`first`: three routes (Command Center, Workflows, Sources), widths 375/1440, both themes and Basic/Advanced, independent module preferences, route/reload persistence and reset persistence; plus drawer tests at 375/768. Eight cases, at most 26 explicit viewport PNGs. No uncertainty fixtures; not final acceptance.

`full`: current operational module registry (currently 28 routes) at 375/768/1024/1440, both themes/modes and persistence, plus two keyboard/drawer cases and eight real persisted uncertainty cases. Currently 122 cases; at most 458 viewport PNGs. Counts are source-derived, not an executed test discovery or PASS count. Optional data-dependent disclosures are opened when rendered; this is not all populated/empty/error/dynamic-section coverage. Onboarding/login are used, not a complete authentication matrix.

All route cases require a real main/H1 and meaningful content, true CSS viewport width, no document/body horizontal overflow, no framework overlay, no uncaught/console errors, no failed requests or HTTP >=400, and no attempted writes/external requests. Sources/Memory keep label-containment checks; Command Center/Workflows/HAI OS have explicit loaded-state checks. The sole expected HTTP exception is the actual signed-out guard's exact same-origin auth-check 401 before login, counted separately; post-login 401 remains a failure. Drawer keyboard traversal stays inside the dialog, Escape/close restore menu focus, route navigation and skip link focus main. Each case is bounded to 90 seconds, navigations 15 seconds, assertions 10 seconds, settling 15 seconds. One worker and zero retries are enforced; one Chromium process and one fresh context at a time through the existing Playwright browser fixture.

Diagnostics attach counts only, never console text, requests, bodies, headers or manifest credentials. Trace/video/automatic failure screenshots are disabled. Explicit post-login screenshots mask password/token inputs and marked secrets; captures/error reports still remain private synthetic evidence requiring parent review/redaction before sharing. No universal screenshot/HTML redaction guarantee is claimed.

## Available Dependencies: Read-Only Inventory

Installed package JSON and lock entries: `@playwright/test`/`playwright-core` 1.62.1; TypeScript 5.9.3; `@types/node` 22.0.0. Local Playwright/tsc shims, `@playwright/test/cli.js`, `playwright/lib/program.js`, and `typescript/bin/tsc` exist. Package engines require Node `^24.15.0`; package-manager pin is npm 10.9.8. Found Node/npm at `C:\Program Files\nodejs`; their runtime versions were NOT executed or established here.

`playwright-core/browsers.json` requires Chromium revision 1234 / 151.0.7922.34. Read-only file inspection found both `C:\Users\NO\AppData\Local\ms-playwright\chromium-1234\chrome-win64\chrome.exe` and `C:\Users\NO\AppData\Local\ms-playwright\chromium_headless_shell-1234\chrome-headless-shell-win64\chrome-headless-shell.exe`. Presence does not prove launchability or availability in another parent environment/custom cache. No dependency install/update is needed or authorized by this task. Parent must confirm its actual Node version, configured browser cache and launchability, not silently download replacements.

## Parent-Run Commands

Run serially from the checkout root, only after the parent has validated and started its current-source owned stack. `$evidence` is the parent's existing absolute launcher evidence directory, never a personal environment. Do not print/attach its manifest. Use fresh child destinations: Playwright can clear reused output/report directories BEFORE test fixtures run, so the parent must enforce this preflight. Fixture validation cannot prevent runner-level cleanup before a test starts. Preserve failed runs and use new names for every rerun.

```powershell
# Parent executable dependency/compile gates; not run by this task.
node --version
npm.cmd --version
npm.cmd --prefix .\frontend\e2e run typecheck
if ($LASTEXITCODE -ne 0) { throw 'E2E typecheck failed; do not start matrix.' }

$privateManifest = Get-Content -LiteralPath (Join-Path $evidence 'manifest.json') -Raw | ConvertFrom-Json
$env:HAI_ACCEPTANCE_EVIDENCE = $evidence
$env:E2E_BASE_URL = "http://127.0.0.1:$($privateManifest.port)"
$env:E2E_ISOLATED_STACK = 'true'
$env:E2E_OPERATOR_EMAIL = $privateManifest.email
$env:E2E_OPERATOR_PASSWORD = $privateManifest.password
$env:E2E_ALLOW_MUTATION = 'true' # Only synthetic login in this matrix.
$env:E2E_MATRIX_SCOPE = 'first'
$label = 'matrix-' + [Guid]::NewGuid().ToString('N')
$results = Join-Path $evidence "$label-results"
$env:PLAYWRIGHT_HTML_OUTPUT_DIR = Join-Path $evidence "$label-report"
$log = Join-Path $evidence "$label.log"
foreach ($destination in @($results, $env:PLAYWRIGHT_HTML_OUTPUT_DIR, $log)) {
    if (Test-Path -LiteralPath $destination) { throw 'Evidence destination already exists; choose fresh names.' }
}
npm.cmd --prefix .\frontend\e2e test -- tests/owned-browser-matrix.spec.ts --project=chromium --workers=1 --retries=0 --trace=off --output $results 2>&1 | Tee-Object -FilePath $log
if ($LASTEXITCODE -ne 0) { throw 'Matrix failed; preserve evidence and report exact failing cases.' }
```

After the first scope, use NEW child destinations and `E2E_MATRIX_SCOPE=full` for the complete matrix. Supply both fixture IDs before full scope:

- `E2E_MATRIX_UNCERTAIN_WORKFLOW_ID`: existing UUID owned by the same synthetic operator, `currentState=blocked`, `recoveryStatus=needs_review`, visible through the real workflow API. No recovery decision is submitted.
- `E2E_MATRIX_UNCERTAIN_TASK_ID`: existing UUID present in that owner's real task logs, unique request matching `E2E matrix [letters/digits/spaces/hyphens]`, `executionResult.outcomeUncertain=true`, `retryPolicy.retryAvailable=false`. Loading history is inspection, not a run. Missing/invalid data fails; no skip or UI/API fabrication fallback. Parent owns any separately authorized backend fixture preparation; no seeding endpoint or SQL mutation was introduced here.

To stage full route/keyboard coverage before these data gates, run full scope with `--grep 'routes |keyboard drawer'` and a fresh destination. Record this exclusion explicitly; it is 114 source-derived cases, NOT complete matrix acceptance. Pure source guard checks may run separately with the existing `tests/isolated-stack.spec.ts tests/render-contract.spec.ts`; do not label them integration proof. Restore parent environment values/clear the password variable after the session, preserving private evidence; this task does not change the parent environment or stop any stack.

## Final Acceptance Gates

Parent must retain actual source/build/image identities, owned stack/manifest/label validation, CSS viewports, final exit status, discovered/executed PASS/FAIL/SKIP counts and named artifacts. First scope, source typecheck and historical reports do not establish current full acceptance. Current production task-history restriction behavior, real uncertainty fixtures, broader populated/error states, cross-browser accessibility, persistent privacy/redaction, EUR0/default-deny policy, required participants/model/evidence, action-bound approval and verified terminal operation remain distinct gates. No provider/installer/restore/deployment/production-ready claim is made by this browser task.
