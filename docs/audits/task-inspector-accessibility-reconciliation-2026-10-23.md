# Task inspector accessibility reconciliation

## Retained-source decision

Read the complete retained Task Blueprint accessibility specification and the
complete difference against the current specification. This is one retained
test-file review, not a review of the entire Task Blueprint implementation,
other worktree contents or transcript archives.

Keep all four original scenarios: selected-state inspector buttons instead of
incomplete tab semantics; expanding the full Basic approval queue without
resolving a decision; showing returned plan/source context and opening evidence;
and exposing recovery for approved work outside the pending queue.

Keep the newer cases rather than replacing them with the older specification.
They cover module-local Basic/Advanced persistence and shell synchronization,
reviewing the current request before Run, persisted uncertain execution in Basic,
non-runnable uncertain receipts, restricted retry versus closure, missing
participants, risk-policy blocks and confirmed metadata preflight failures.
Recovery remains visible when switching inspector views.

## Additional browser control

Added a rendered inspector control check for Overview, Plan, Evidence and Logs.
Each control is a non-submit native button, accepts focus, preserves that focus
when selecting its view, and leaves exactly one `aria-pressed` button selected.
Inspecting alone must not dispatch an assistant command, resolve a decision or
reconcile approved reviews.

This uses actual Chrome DOM focus and rendered Angular controls. It does not
simulate an operating-system keyboard, prove screen-reader announcements,
certify contrast/reflow or establish whole-application WCAG compliance.

## Verification and preservation

The current Task Blueprint component and accessibility suites both ran in Chrome
Headless: 78 cases passed. Services were test doubles; this does not prove live
provider, runtime, database or deployed-browser behavior. No production behavior
was changed by this reconciliation.

The retained original remains intact and is bound to its frozen source hash in
the private review ledger. Publication and a passing test run are not evidence
that all bytes of its worktree or archives can be deleted. Main preservation and
the remaining full-source scope still require separate verification.
