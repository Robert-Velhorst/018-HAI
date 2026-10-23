# Google OAuth source binding reconciliation

## Reviewed source decisions

Reviewed the connected-source model and OAuth implementation in both audit
worktrees: four source versions. The model variants match after line-ending
normalization; the artifact OAuth variant is older than the model-audit variant.
The large OAuth test-file variants remain a separate, unfinished review batch.

Retain main's durable single-use OAuth state model storing only the state
digest, owner/source/connector binding, PKCE and encrypted token storage. Keep
atomic token/source persistence, refresh snapshot comparison and token-bound
reconnect-required updates. Do not restore separate unguarded token writes or
source-only stateless callbacks.

Retain exact least-privilege scope checks, verified refresh-token preservation,
inactive/revoked source rejection and provider/source grant identity checks.
The oldest variant omits several of these protections.

Retain text-sized source/sync cursors rather than restoring the old 512-character
columns. Keep decimal/monotonic Gmail history boundaries, stalled-page detection,
bounded content-review records, attachment coverage warnings, unavailable-message
records and deletion/label-change history events. Do not silently treat partial
message content as a complete import.

## Additional normal-token reuse defect

The forced-refresh reuse path already rechecked source eligibility and request
cancellation after repository reads. The normal valid-access-token path did
not. It could return a token after cancellation, pause, disablement, revocation,
reconnect-required state or owner/connector replacement during token lookup.

The normal path now rejects missing/ended contexts before storage, observes
cancellation after reads, snapshots the original source binding and rechecks
the current source before reusing or refreshing a grant. The binding snapshot
also avoids treating a changed repository pointer as the original authorization.
Repository errors retain their causes. Existing refresh/write transaction
guards remain unchanged.

This is a recheck at a defined boundary, not a transaction held across provider
network access or a promise that revocation cannot occur after the recheck.

## Verification and preservation

Ten normal-route regressions failed before the repair. The existing eleven-case
forced-refresh matrix and the new eleven-case normal-route matrix pass afterward,
including the unchanged valid-grant positive controls. Three additional invalid
context cases verify that storage is not read at all. All use synthetic encrypted
grants and repository hooks; no real Google account or token exchange is involved.

The full source, Google OAuth and model Go packages pass locally. SQL-dependent
cases without a dedicated DSN remain skipped; this is not live OAuth acceptance.

Original source fingerprints are preserved in the private review ledger. No
original worktree, transcript, archive, credential or database was deleted.
