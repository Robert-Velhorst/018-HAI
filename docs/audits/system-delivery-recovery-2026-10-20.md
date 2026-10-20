# System delivery and connector recovery

## Verified integration gap

The retained System Status UI exposed event-delivery records and planning-bridge
configuration. Current main retained the typed clients but not the page wiring.
The delivery client also used `/api/v1/event-delivery`, which does not match
`initializeAutomationEventRoutes`: the real backend route is
`/api/v1/automation/events/delivery`, without a trailing slash.

The backend requires authenticated owner administration for delivery reads and
retries. A2A status requires authenticated owner read access. Client-side role
checks are presentation only; the existing backend enforcement is unchanged.

## Recovered behavior

- Register a persisted, deep-linkable Advanced section. Opening it reads the
  authenticated session before loading only the details its permissions allow.
  Basic does not eagerly load delivery or connector records.
- Read real delivery counts and recent failed records through the corrected
  route. Do not expose raw provider errors, credentials or bridge endpoints.
- Label bridge configuration separately from live connectivity. Published
  events do not establish downstream completion.
- Require an explicit review and confirmation for a current dead-lettered row.
  Prevent duplicate submissions and accept only a matching queued receipt.
- Treat timeout, failed transport, empty or mismatched receipt as an unverified
  mutation outcome, not proof that nothing changed. Require a status refresh
  before another retry. No automatic mutation retry is added.
- Cancel closed-section reads and destroyed-page subscriptions. Existing
  readiness polling remains independent; navigation now also cancels its read.
- Validate count/record snapshots before enabling retry. Keep unavailable and
  stale state visible rather than inventing healthy or empty results.

## Evidence and boundaries

Regression coverage includes Basic laziness, verified-role boundaries, explicit
confirmation, duplicate prevention, mismatched/empty receipts, timeouts,
cancellation, malformed snapshots, and a rendered Angular HTTP action chain.
The native router permission regression passed, including authorized-owner
requests reaching both real handlers. The canonical production build passed
after correcting an optional-session Observable typing error. An initial full
test run was not accepted: Chrome disconnected before reporting results and its
failed owned process tree was stopped. The subsequent standalone canonical npm
test completed with exit zero: all 180 Node and 1,428 Angular tests passed.
Local checks used Node 26.12.0, npm 11.6.2 and the existing dependency tree;
fresh locked installation and exact-head hosted CI remain merge gates. Existing
style-budget warnings remain unchanged. Synthetic HTTP fixtures do not establish
real IDP-token or Kafka delivery acceptance.

The retained source still matches its inventoried hash. Separately, explicit
approval authorized removal of exactly two damaged transcript copies after fresh
full-byte prefix/NUL comparison and final write-locked hash verification against
their intact counterparts. Both intact copies and the other nine archive files
remain. The private cleanup ledger records the exact identities and byte counts;
none are published here. No source worktree or database was deleted, repaired or
moved. This exception does not establish main integration or authorize removal
of the remaining archives.
