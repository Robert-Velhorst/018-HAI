# Completed child-agent final reports

This generated archive preserves the terminal report from each completed
HAI child transcript in the audited cohort. Credential-shaped values and
local repository roots are redacted; the manifest retains the SHA-256 of
the original terminal message. Aborted and nonterminal transcripts are not
represented as completed work and must be retained. Every transcript sharing
a child ID is also retained until its duplicate history is reconciled.

## 019fb087-5a29-7983-bc56-3f8956867155

- Date: 2026-07-30
- Agent: Hubble
- Work kind: implementation
- Original report SHA-256: `3e92dc21b2f993737e85467b3f762c8f78b0d43cde358e1eaab2813e5aaecc10`
- Transcript: `completed-agent-sessions/rollout-2026-07-30T02-58-08-019fb087-5a29-7983-bc56-3f8956867155.jsonl`

**Audit Complete**
Fixed these Framework Registry defects:

- Empty validation now focuses the correct request field.
- Owner mutations require both the signed `owner` role and `canAdminister`.
- Viewers and unverified sessions cannot submit framework selections.
- Loading, unavailable, filtered-empty, and genuinely empty states are distinct.
- Credential-like inbound values and error details are redacted.
- Recommendation rows, history actions, loading states, and the pin control now have proper keyboard/accessibility behavior.
- Duplicate submissions are blocked while requests are running.
- Losing owner authority clears in-progress Constitution approval data.

**Files Changed**
- Main registry: [component.ts](../../frontend/src/app/pages/framework-registry/framework-registry.component.ts), [component.html](../../frontend/src/app/pages/framework-registry/framework-registry.component.html), [component.scss](../../frontend/src/app/pages/framework-registry/framework-registry.component.scss), [component.spec.ts](../../frontend/src/app/pages/framework-registry/framework-registry.component.spec.ts)
- Inspector: [inspector.html](../../frontend/src/app/pages/framework-registry/framework-registry-inspector.component.html), [inspector.scss](../../frontend/src/app/pages/framework-registry/framework-registry-inspector.component.scss), [inspector.spec.ts](../../frontend/src/app/pages/framework-registry/framework-registry-inspector.component.spec.ts)
- Recommendation: [recommendation.ts](../../frontend/src/app/pages/framework-registry/framework-registry-recommendation.component.ts), [recommendation.html](../../frontend/src/app/pages/framework-registry/framework-registry-recommendation.component.html), [recommendation.scss](../../frontend/src/app/pages/framework-registry/framework-registry-recommendation.component.scss), [recommendation.spec.ts](../../frontend/src/app/pages/framework-registry/framework-registry-recommendation.component.spec.ts)
- Wiring/API: [module.ts](../../frontend/src/app/pages/framework-registry/framework-registry.module.ts), [service.ts](../../frontend/src/app/services/framework-registry.service.ts), [service.spec.ts](../../frontend/src/app/services/framework-registry.service.spec.ts)

**Validation**
- Focused Angular suite: **36/36 passed** in Chrome Headless.
- Click-through coverage includes Command Center navigation, framework inspection, recommendation inspection, and API-backed preference/selection operations.
- No workflow-engine or `.playwright-cli` files were touched.
- Nothing was staged, committed, or pushed.

A supplemental full production build exceeded the 180-second limit and was stopped. The focused requested test gate passed; Node 25 also reports its expected non-LTS warning.

## 019fb090-112e-7741-8c8b-1f78cafa14d6

- Date: 2026-07-30
- Agent: Popper
- Work kind: implementation
- Original report SHA-256: `80cb0c9bb4b12ef684fe6da81a4a6ace8328415639100b9bc1b215a14819ffbe`
- Transcript: `completed-agent-sessions/rollout-2026-07-30T03-07-39-019fb090-112e-7741-8c8b-1f78cafa14d6.jsonl`

**Fixed**
- Replaced unsupported `moon` usage with the shipped `StarOutline`.
- Registered `BulbOutline` and `StarOutline` globally through `NZ_ICONS`.
- Removed the duplicate icon logic from Model Routing.

Changed:
- [app.module.ts](../../frontend/src/app/app.module.ts)
- [theme.service.ts](../../frontend/src/app/services/theme.service.ts)
- [theme.service.spec.ts](../../frontend/src/app/services/theme.service.spec.ts)
- [llm-policy.component.ts](../../frontend/src/app/pages/llm-policy/llm-policy.component.ts)
- [llm-policy.component.spec.ts](../../frontend/src/app/pages/llm-policy/llm-policy.component.spec.ts)

**Verification**
- Focused tests: `2 SUCCESS`
- Production build: passed
- Runtime source scan: no remaining `moon` references outside tests
- Browser: local `/control-center` route in an ephemeral dev server (not a deployed URL).
- Light-to-dark interaction rendered `anticon-star` with `nzType="star"`
- Dark mode remained correct after reload
- Browser console warnings/errors: `0`
- `git diff --check`: passed

The build still reports unrelated existing size warnings for the Framework Registry stylesheet and initial bundle. No files were staged, committed, or pushed, and the existing workflow-engine SCSS change was untouched.

## 019fb09c-e5bb-7c31-b6c4-59479eaca1e6

- Date: 2026-07-30
- Agent: Anscombe
- Work kind: implementation
- Original report SHA-256: `52478a75db344592832394bcd6197ac6cf7d6c5f34c65eb2c1a2572061fba352`
- Transcript: `completed-agent-sessions/rollout-2026-07-30T03-21-40-019fb09c-e5bb-7c31-b6c4-59479eaca1e6.jsonl`

**Findings Fixed**
- Smoke aggregation could pass without per-suite result evidence. CI now verifies all five suites report `0 failed`.
- Isolation testing did not explicitly fail when the backend never started. It now checks process survival and liveness.
- Migration filters could match no integration tests and still exit successfully. CI now verifies exact test names passed.
- Race tests could use cached results. Added `-count=1`.
- Added explicit timeouts to long-running security and integration jobs.
- Added a regression contract covering these guarantees.

**Files Changed**
- [.github/workflows/ci.yml](../../.github/workflows/ci.yml)
- [scripts/test_ci_contract.py](../../scripts/test_ci_contract.py)

**Verification**
- Actionlint `v1.7.7`: passed
- YAML parsing: passed
- Shell syntax for all 25 workflow run blocks: passed
- CI/auth/gateway contracts: 14 tests passed
- Dependency and cache paths: valid
- `git diff --check`: passed

No production source, migrations, documentation, smoke scripts, or `.playwright-cli` files were changed. Nothing was staged, committed, or pushed. Docker-dependent jobs were not run locally.
