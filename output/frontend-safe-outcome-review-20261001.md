# Frontend Safe Outcome Integration Review

Date: 2026-10-01. Shared checkout: `%HAI_WORKSPACE%`.

## Outcome And Authority

The initial read-only review established the gaps below. Robert subsequently authorized minimal additive frontend fixes and focused regression source. Those fixes are now integrated in source; no executable tests, builds, Docker, browser, descendants, installs, downloads, provider calls, credential access, commits, pushes, deletions or service changes were performed. Existing dirty edits, NG-Zorro controls and progressive-disclosure layout were preserved. The whole Git diff includes substantial pre-existing work and must not be attributed to this continuation.

Used local source inspection, the supplied backend consumer handoff, existing Angular test conventions and checkout-specific memory for scope/safety boundaries. The available code-review skill was inspected, but its branch-diff/subagent workflow was not applicable to this restricted integration audit. No external connector, account, browser or online documentation was needed for these local contracts. Parent-reported backend/IDP passing results are not independent verification by this reviewer.

## Source-Grounded Findings

### P1: Structured Run Conflicts Were Discarded; A Stale Ready Snapshot Permitted Another Click

The backend returns HTTP 409 together with operation correlation, a redacted receipt and explicit interruption/reconciliation/recording flags after post-effect failures: [handler.go](../backend/internal/phase2/handler.go):382. The completion predicate is error-sensitive, and the HTTP response retains receipt evidence even when recorded completion cannot be confirmed: [response projection](../backend/internal/phase2/handler.go):402.

Before this integration, the frontend error callback extracted only `error.error`, stored a transient action issue keyed to the old status, and refreshed. `canRun` considered only `currentDecision` and the classified/ready/failed status. Thus a stale ready overview/detail after the refresh re-enabled another request once pending/loading cleared; a status change could also hide the warning. This is a frontend repeat-request gap, not proof that the same backend operation executes twice: the handler deliberately retains its claim fence, and the existing backend regression source asserts ordinary retry refusal after verifying/completed persistence failures: [backend regression](../backend/internal/phase2/safe_outcome_response_test.go):116.

Integrated fix: typed HTTP result contract; narrow projections for both success and HTTP-error bodies; per-operation uncertainty latch independent of read snapshots; retained bounded evidence and correlation; Run withheld through stale, omitted and changed-status reads. A receipt-free 401/403 or known policy/claim 409 refusal is not converted into uncertain effects. A known failed result remains distinct and retryable under the existing decision rule when its evidence does not contradict a pre-effect failure.

Current implementation: [run consumer](../frontend/src/app/pages/background-operations/background-operations.component.ts):343, [typed result](../frontend/src/app/models/background-operations.model.interface.ts):3.

### P1: Runtime Lab Could Start A New Self-Test Before Resolving Its Previous Outcome

Runtime Lab returns uncertain executions as HTTP 200 inconclusive attempts, with receipt, operation status and reconciliation fields: [attempt projection](../backend/internal/runtimelab/service.go):259, [HTTP handler](../backend/internal/runtimelab/handler.go):67. The old frontend used the returned attempt only for a toast and refreshed; refresh failure retained an older displayed attempt. Error/timeout became "Self-test failed", and only the in-flight busy flag blocked another click.

A second self-test is not merely a retry of the fenced operation: the backend increments its sequence and creates a new operation/dedupe key for each self-test: [self-test operation creation](../backend/internal/runtimelab/service.go):186. The old UI therefore offered new work while the earlier effects remained unresolved.

Integrated fix: retain the returned narrow attempt immediately, latch uncertainty by runtime, preserve it across failed/stale/omitted overview reads, and block Self-test in both the method and disclosed button. Transport unknowns and an empty response become explicitly unconfirmed outcomes, not known execution failures. Refresh, read-only Probe/Check runtime and Review operation ledger remain available. Recovered running/verifying/interrupted attempts and effect-correlated pending/unknown/indeterminate/future statuses fail closed. Receipt-free authorization refusals and documented external health-only inconclusive records do not manufacture uncertainty.

Current implementation: [self-test consumer](../frontend/src/app/pages/runtime-lab/runtime-lab.component.ts):373, [attempt predicates](../frontend/src/app/models/runtime-lab.model.interface.ts):48.

### P2: Verified Display Used Labels Instead Of The Completion Contract

The old operation success branch trusted `verified`; the drawer printed a passed verification field even on a noncompleted snapshot. Runtime Lab success notifications and green lastAttempt badges trusted `status === succeeded`, ignoring `verificationPassed` and contradictory state. These are consumer fail-closed gaps; the reviewed new backend normal projections already reject error/interruption conflicts, so this review does not claim a current normal backend response incorrectly certifies completion.

There is also a real, documented non-execution succeeded record: recovered OpenClaw read-only discovery uses `succeeded`, `discoveryRecovered=true` and no execution verification: [discovery recovery](../backend/internal/runtimelab/service.go):322. It must not be relabeled as verified execution or as uncertain side effects.

Integrated fix: require correlated completed/passed operation state without error, interruption, reconciliation or explicitly unrecorded outcome. Runtime verified completion additionally requires `verificationPassed` and rejects discovery-only records. Supplied receipt `ok=false`, `verification.passed=false`, incomplete/invalid receipt shape, or conflicting effect evidence cannot produce verified UI or ordinary replay eligibility. Non-null malformed receipt input latches reconciliation instead of silently becoming absence. Historical receipt absence/null remains compatible when the other completion evidence is consistent; no receipt verification is invented. Read-only recovered discovery gets its own non-execution label and blue badge.

Current implementation: [completion predicate](../frontend/src/app/models/background-operations.model.interface.ts):33, [receipt validation/projection](../frontend/src/app/models/safe-outcome.model.interface.ts):20.

### P2: Interrupted Work Was Not Discoverable As Review-Required In Basic

Interrupted was absent from the original filter labels, priority order and status color mapping. Locally uncertain ready records could look routine; the three-item Basic cap could bury review-required work.

Integrated fix: explicit Reconciliation required filter/badge, warning color, retained uncertainty independent of the last-read status, review-first ordering and visibility of all interrupted/review-required items in Basic. The normal three-item cap still applies when there is no review-required work; unresolved work is not silently dropped to Advanced. One action warning carries recovery guidance; receipt evidence is a bounded textual summary, never a raw JSON dump. Known failed work retains its separate failure/retry treatment.

Current implementation: [Basic selection](../frontend/src/app/pages/background-operations/background-operations.component.ts):613. Runtime uncertainty is likewise visible in Basic even while inventory disclosure is closed.

## Integrated Paths

All paths below are relative to the shared checkout; only these frontend files plus this report were edited/created by this continuation.

- `frontend/src/app/models/safe-outcome.model.interface.ts` (new): shared narrow receipt/state projection, recognized-secret redaction, bounded text and completion consistency checks.
- `frontend/src/app/models/safe-outcome.model.spec.ts` (new): compact parser/helper regressions.
- `frontend/src/app/models/background-operations.model.interface.ts`: additive run DTO and verified predicate; existing operation contract retained.
- `frontend/src/app/models/runtime-lab.model.interface.ts`: additive attempt fields, narrow projection and status/verification predicates; existing discovery/catalog additions preserved.
- `frontend/src/app/services/background-operations.service.ts`: additive typed run response; errors stay on the HTTP error channel, with no replay or automatic retry.
- `frontend/src/app/services/background-operations.service.spec.ts`: success/error envelope retention regressions.
- `frontend/src/app/services/runtime-lab.service.spec.ts` (new): inconclusive self-test, overview and history field retention regressions. Runtime Lab production service already forwards the typed attempt unchanged and required no edit.
- `frontend/src/app/pages/background-operations/background-operations.component.ts` and `.html`: action latch, bounded summaries, truthful verification, review-required Basic visibility.
- `frontend/src/app/pages/background-operations/background-operations.component.spec.ts` and `.ui.spec.ts`: focused state and rendered-template regressions.
- `frontend/src/app/pages/runtime-lab/runtime-lab.component.ts` and `.html`: retained attempt, runtime latch, truthful lastAttempt display and Basic review controls.
- `frontend/src/app/pages/runtime-lab/runtime-lab.component.spec.ts`: focused state and disclosed-control regressions.

No backend, stylesheet, module, configuration, provider or service process was changed.

## Exact Regression Scenarios Added, Not Run

1. Structured operation HTTP 409 after a synthetic post-effect persistence failure: retain operation ID/status, artifact hash and redacted bounded output; keep `outcomeRecorded=false`; a second Run call issues no POST after successful stale-ready overview/detail, filter change or drawer close/reopen. An unrelated operation remains independently eligible.
2. Run timeout, HTTP 0 and HTTP 503: stale successful reads cannot clear the latch. Receipt-free 401/403/policy 409 remain explicit refusals, without a fabricated effects warning.
3. Operation verified flag combined with interrupted, reconciliation required, unrecorded outcome, verifying/passed, null snapshot or wrong operation correlation: no success notification and no ordinary Run. A consistent correlated completed/passed result is accepted.
4. Warning survives ready-to-interrupted status changes; reported recorded state is labeled without a durability promise. Existing known-failed retry tests remain intact.
5. Interrupted/local uncertainty is prioritized and labeled in Basic, with Inspect/Refresh retained; four interrupted items are not hidden by the normal three-item cap. The rendered Basic test shows a receipt hash, no synthetic secret and no Run/Retry button after stale refresh.
6. Runtime HTTP 200 inconclusive receipt: retain it immediately even if overview fails; a later stale succeeded overview or omitted runtime cannot authorize another self-test. Basic readiness excludes the unresolved attempt.
7. Runtime succeeded combined with false verification, interrupted, reconciliation, unrecorded outcome, verifying or missing operation status: no verified notification/green badge and no ordinary Self-test. Consistent completed/passed evidence is accepted.
8. Runtime timeout/HTTP 0/HTTP 503: unknown outcome and repeat test blocked; receipt-free 403 and setup-required remain non-effect refusals. Recovered interrupted/running/verifying and correlated future/unknown/indeterminate statuses remain fenced.
9. Read-only recovered discovery (`succeeded`, `discoveryRecovered=true`, no execution correlation) and external health-only inconclusive attempts: no invented execution verification or effect uncertainty. The discovery label explicitly says execution is not verified.
10. Rendered Runtime Basic shows unresolved receipt evidence with inventory closed; disclosing inventory shows a disabled Self-test control and nonverified label, while ledger review/read-only readiness controls remain.
11. Compact helper matrix: supplied `ok=false`, `verification.passed=false`, string/array/empty-object receipt and null output reject verified predicates and imply review rather than retry. Absent/null historical receipts do not fabricate receipt flags. Malformed safety booleans fail closed; repeated projection preserves legitimately absent optional flags. Oversize output is bounded; credential-shaped test values and arbitrary payload/path fields are not displayed.
12. HTTP service tests preserve additive fields on operation success and 409 error, issue no replay, and preserve Runtime Lab inconclusive receipts through self-test/overview/attempt-history responses.

Fixtures are source-only synthetic values. None of these scenarios was executed in this continuation.

## Actual Verification And Limits

- Inspected the requested backend handoff, phase2 handler, Runtime Lab service/handler, background safe execution transitions, relevant backend fixture source, frontend models/services/components/templates and existing test conventions. Inspected the request timeout interceptor/wiring; it adds a timeout, not receipt recovery or an automatic retry.
- Static source/diff review only. Final scoped `git diff --check` passed (exit 0, LF-to-CRLF notices only). A trailing-whitespace scan of the three new frontend files and this report found no matches (exit 1 is the search tool's no-match result). A clean whitespace check is not compiler/test/browser evidence.
- No claim of TypeScript compilation, Angular template compilation, passing frontend tests, browser rendering, live PostgreSQL/recovery behavior, deployed API compatibility, restart survival or production readiness. Parent owns serial frontend tests/build and any acceptance decisions.
- UI receipts and latches live only in the component instance. Navigation, reload, a new tab or a crash can lose them. These changes are not crash-durable receipt storage, an exactly-once mechanism or a replacement for backend claim/evidence/reconciliation authority.
- There is deliberately no frontend clear-and-retry bypass. Refresh/Inspect/ledger review expose current evidence but do not authorize replay. A durable, authenticated reconciliation result and retry authorization workflow would require a separate backend/UI contract; it was not invented here.
- Safe summaries whitelist correlation, status, recording acknowledgment, artifact hash and bounded output. They omit raw JSON, artifact paths and unrelated receipt fields. Redaction handles recognized secret-shaped text, not every possible personal/secret value; no full privacy audit is claimed.
- The legacy background-pass report/counter contract and its existing refresh-clears-pass-warning behavior were not changed. The new per-operation and per-runtime latches are separate; this work does not establish run-wide durable reconciliation or validate dashboard aggregate completion claims.

Next step: parent serially verifies the focused new/existing frontend specs, full unit suite and frontend build, preserving the shared dirty checkout. Reconcile any compiler/template/regression failures before claiming frontend integration acceptance.
