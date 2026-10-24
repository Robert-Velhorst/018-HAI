# Task Blueprint template source reconciliation

The complete retained 62,477-byte model-maintenance template and its complete
diff against the published template were read. The published difference has
136 added and 58 removed lines. Keep the newer uncertainty receipts, safer
action/status labels, per-module view controls, approval-specific gates, MCP
trace inspection, and shared approved-work recovery template. Do not replace
these with the older template.

The historical last-activity click expression is intentionally replaced by
`openLastActivity()`, which also handles missing history. Recovery controls were
moved into a shared template used in both Overview and Logs, not removed.
Navigation, draft criteria, source/evidence inspection, project/pursuit context,
mandate entry, advisory coordination, life graph, scheduling, approval, rejection,
planning and execution handlers remain present. Binding preservation is not
proof that each connected service or destination is live and operational.

## Non-submitting command buttons

Angular's template parser identified three buttons inside the task form without
explicit button types: open the command's pursuit, use typed-draft criteria, and
use reviewed-draft criteria. They now declare `type="button"`. This prevents
browser-default submit behavior in addition to their intended click handler.
The existing form-submit prevention remains intact; this repair does not claim
that these buttons previously executed an unauthorized task.

The initial manual estimate was four controls; the complete parser inspection
identified exactly three omissions. No fourth omission is claimed.

## Verification and limits

Five new native tests use the installed Angular template parser. Two tests failed
before the three type attributes were added; all five pass afterward. The parser
walks structural directives and explicit templates, checks every current button,
rejects implicit/dynamic type fixtures, and verifies both approval/rejection
surfaces and distinct planning, run, cycle, uncertain retry and recovery gates.
The new tests run within the existing control-room suite, which passes all 114
tests with no skips.

The initial five parser checks are not rendered DOM interaction or visual
verification. A subsequent full frontend run passed 235 native checks and all
2,276 Angular tests in Chrome Headless. The production build also passed, with
four existing component-stylesheet warning-budget overruns and no raised budgets.
This does not establish manual keyboard/visual acceptance or live provider
behavior. No server/container rebuild or live provider action was run.
Publication, exact-head CI, preservation on `main`, and local
deletion remain separate gates. Other retained sources and archives remain open.
