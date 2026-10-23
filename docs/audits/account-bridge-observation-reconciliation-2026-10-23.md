# Account bridge observation reconciliation

## Retained-source review

Read all four retained Account Bridges component files (implementation, template,
styles and component specification) and their complete differences against the
current code. Keep the newer implementation instead of restoring older code.

Preserve independent feed/contract error states, last-known evidence without
false freshness, Basic recovery actions, per-module Advanced disclosure,
accessible feed controls and distinct attempt/success timestamps. Active or
interrupted sync claims remain non-runnable. Import completion requires its
reported audit state; more pages and reported errors remain review states.
Enabling a supported source never starts its import automatically.

Preserve explicit local-feed identity inspection, selection/configuration
invalidation, bounded first-response observation, stable safe backend-code
messages, count/item consistency validation and the limited 100-item sample.
That preview is not global historical coverage, a migration or reconciliation.
Keep current responsive wrapping, minimum control dimensions, focus styling,
UTC instants, semantic lists and explicit unknown/loading/error presentation.

## Correction

Ordinary feed/contract refreshes and mutations did not consistently use the
identity inspector's existing one-response observation discipline. Empty streams
could finish silently; an emitted response without stream completion could
leave refresh loading; synchronous setup failures escaped error handling.
Refresh/sync entry points could also issue requests after page destruction.

Use the existing RxJS operators directly: defer setup, retain deadlines, take
one response and treat empty completion as an error. Feed and contract failures
remain independent and retain their previous data. Mutation errors preserve
unconfirmed-outcome wording and refresh status without replaying the mutation.
Block refresh, primary-action and sync dispatch after destruction. Existing
teardown subscriptions and identity inspection behavior are retained.

## Verification and limits

Seven new regression cases failed against the prior implementation; the
already-bounded stalled-read control passed before repair. After repair, all
88 cases across the component, rendered HTTP controls, sync outcomes and new
observation suites passed in Chrome Headless. Additional controls cover an empty
bridge stream with valid feed data, first-only bulk/state acknowledgements and
all three mutation deadlines without automatic write retries.

These are local Angular/HTTP doubles and DOM checks, not live provider, deployed
browser, full responsive screenshot, database or server cancellation acceptance.
Unsubscribing from a request does not prove that a server mutation had no effect.
Full nested feed/report schema validation, report-to-feed binding and arbitrary
sync-report diagnostic redaction are separate review work, not guarantees added
by this observation fix. No provider permission or execution authority changes.

Original source copies remain intact, with exact source hashes in the private
review ledger. This review does not establish whole-worktree/archive deletion
eligibility. Main preservation and remaining source scope must be checked before
local cleanup.
