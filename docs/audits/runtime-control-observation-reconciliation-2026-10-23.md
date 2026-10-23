# Runtime control observation reconciliation

## Retained-source review

Read the retained Runtime Control implementation, template and specification in
full, then every difference against current code. Keep the newer durable owner
review for resume: preparing a review must not clear the stop; confirmation is a
separate explicit action bound to the returned review identifier and digest.
Do not restore the older immediate preparation-to-resume chain.

Preserve signed session permissions, operator versus owner boundaries,
mode-approval proof checks, inconsistent-stop refusal, stop fan-out uncertainty,
verification/restoration checks, bounded recovery reporting and Basic readiness
remediation. Keep module-local Advanced disclosure and labelled review input.
These are frontend controls; backend authorization remains authoritative.

## Request observation and lifecycle correction

Fourteen new regressions failed before repair. They cover silent empty session
and control observations, refresh waiting for stream completion, synchronous
request-setup errors, controls dispatched after destruction, a late mode
preparation dispatching its successor, repeated preparation responses, unbounded
preparation, and arbitrary resume/mode error diagnostics reaching notifications.

Use the existing RxJS tools to defer setup, take one response, reject empty
completion and retain a deadline for each stage. Session/status/readiness reads
use six seconds; each control stage uses thirty seconds. Destroying the view
unsubscribes control observations and prevents new protected dispatch, including
a delayed mode-change successor. The busy flag stays set across preparation
until the governed write responds or fails. No uncertain write is auto-retried.

Resume/mode error messages use static fallbacks or a narrow exact list of known
non-sensitive policy messages rather than arbitrary exception or response text.
Existing approval-proof and changed-state explanations are retained.

## Verification and limits

All 40 cases across the current rendered role-boundary suite and the new
observation suite pass in Chrome Headless. Additional controls cover destruction
during all six action paths, a stalled final mode write and synchronous stop
request setup. No runtime-control API was called against a live server.

Unsubscribing an HTTP observation does not prove that an already-dispatched
server action stopped, rolled back or never persisted. Re-entering the module
requires a fresh backend snapshot; a local busy flag is not a durable receipt.
This run does not establish installed Windows behavior, external process
termination, real owner review consumption, production deployment, complete
nested status/readiness schema validation or redaction of every successful
report field. Existing fan-out/detail report text remains separate from the
resume/mode error-message boundary changed here.

The three original source variants remain intact and are tied to frozen hashes
in the private review ledger. This PR is not whole-worktree or archive deletion
evidence. Publication, main preservation and local deletion remain separate
gates across the full retained HAI scope.
