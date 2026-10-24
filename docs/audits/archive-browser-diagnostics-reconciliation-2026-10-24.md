# Archived browser diagnostics reconciliation: 24 October 2026

## Verified source scope

The 25 successful-patch variants outside the historical product-worktree roots
were resolved to ten temporary JavaScript browser-audit files: ten additions,
five updates and ten final deletions. Each indexed event was read at its bounded
offset, its operation and success flag checked, and its content/diff fingerprint
recomputed against the frozen index. The index still matches its frozen checksum.

All ten final source texts parsed through the installed TypeScript JavaScript
parser without syntax errors. Only source structure was inspected; no archived
script was executed, no browser was controlled and no installation API was called.
All ten original temporary file paths are currently absent. They were not removed
by this review and do not represent newly reclaimed disk space.

| Temporary basename | Variants | Intent |
| --- | ---: | --- |
| `hai-dashboard-nav-audit.js` | 3 | Dashboard navigation and section visibility. |
| `hai-dashboard-action-audit.js` | 5 | Dashboard action/dialog routing and viewport checks. |
| `hai-scroll-audit.js` | 3 | Section position and document scroll diagnostics. |
| `hai-local-preview-login.js` | 2 | Existing preview-login interaction. |
| `hai-dashboard-utility-audit.js` | 2 | Utility actions and connected-source navigation. |
| `hai-utility-smoke.js` | 2 | Utility destinations and viewport position. |
| `hai-background-smoke.js` | 2 | Background-operations controls. |
| `hai-route-audit.js` | 2 | Route and rendered-heading inventory. |
| `hai-control-inventory.js` | 2 | Rendered-control inspection. |
| `hai-quick-capture-e2e.js` | 2 | Capture smoke path with API DELETE cleanup. |

The five updates change selectors, viewport bounds, fixed wait durations and a
login fallback. These are historical browser diagnostics, not missing backend
service or agent-engine implementations. Their completed-event flags do not
establish current browser acceptance or justify deleting whole transcript files.

## Canonical coverage and preserved test intent

Current `control-room.spec.ts`, `owned-browser-matrix.spec.ts` and their guarded
fixtures provide maintained navigation, focus, disclosure, theme, viewport,
blocker-routing and unavailable-backend checks. Existing Quick Capture component
tests cover successful memory creation and retention after failure. These tests
were read, not newly executed as browser acceptance by this audit; exact parity
with every old selector or utility action is not claimed.

One useful capture scenario is prepared in `quick-capture.spec.ts` for the current
implementation, which creates a memory note rather than an exception:

- Require the same isolated loopback/synthetic-operator target guard and explicit
  mutation opt-in as the existing operator acceptance suite.
- Fill a unique synthetic note, wait for autosave and verify draft reload.
- Save through the actual UI, require the real memory creation response and read
  back that exact owner-scoped note through the API.
- Verify the saved draft remains cleared after reload.
- Do not replay the historical DELETE against exception records. The existing
  owned-stack launcher owns disposal of its whole synthetic dataset.

The new test contains no fabricated API responses, personal data, fixed waiting
delays, model/provider calls or installation-record cleanup.

## Validation limits

The new TypeScript source parses without syntax errors. Full E2E type checking is
NOT a pass: the existing local baseline already lacks `@playwright/test` and its
type definitions. With the installed compiler the baseline reports 62 diagnostics;
the added test reports four further diagnostics derived from that missing module.
No dependency installation or additional Docker environment was started to hide
this gate. Hosted pinned-dependency type checking and actual owned-stack execution
remain required before this test can be called operationally verified.

## Wider archive boundary

The previously recorded product-root cohort contains 5,503 fingerprint-verified
variants and the historical scratch cohort contains 214. These 25 temporary
variants account for the remaining successful-patch lookup cohort, totaling 5,742.
This is structural fingerprint coverage, not semantic integration of 5,742 changes.
Thousands of code differences, ordering/supersession decisions and non-patch
archive records remain separate work. Original archives and source copies stay
protected; this receipt is not deletion authority.
