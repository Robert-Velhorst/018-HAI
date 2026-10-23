# Account feed read-boundary reconciliation

## Retained source and current protections

Reviewed the complete retained feed-fetcher implementation from an additional
recovery worktree, the complete current implementation and their full delta.
The old named transport and IP helpers are not simply missing: their protections
are superseded by current direct dialing, complete DNS answer validation,
special-address restrictions, explicit loopback URL opt-in and redirect refusal.
Keep those stronger controls rather than restoring the older permissive loopback
behavior. Preserve opaque cursor handling and the governed Autoposter reader.

Keep secure-root file opening and post-read file identity verification instead
of restoring the older path-join/stat/open sequence. A declaration-name comparison
alone cannot establish whether a capability has been lost or safely replaced.

## Useful behavior carried forward

The retained bounded-reader helper checked cancellation both before and after
reading. The current HTTP path only checked before dispatch and relied on the
transport for later cancellation. Restore the small standard-library helper and
share it between HTTP and local reads, while keeping secure-root verification
after the local read. A reader canceled at its final data/EOF boundary cannot
return successful source bytes. The five-MiB cap, including one-byte overflow
detection, remains unchanged. This does not make arbitrary blocking readers
interruptible or undo operations already performed elsewhere.

Reject a missing fetch context without panicking. Remove the configured URL
from the disabled-HTTP error: a rejection before URL validation must not echo
private configuration, user information or query data.

## Verification and archive scope

Two regressions failed before repair: a missing-context panic and private URL
disclosure. Added bounded-reader controls cover cancellation at EOF, nil and
pre-canceled contexts without reads, and below/exact/above-cap payloads. The
complete accountfeed Go package passes uncached after integration, retaining
existing HTTP cancellation, redirect, cursor and loopback-policy tests.

A separate private triage refresh verifies all 5,503 indexed known-worktree
patch fingerprints against a pinned main snapshot. Literal full-hunk matches
increase from 1,107 to 1,108; this is not semantic or whole-archive completion.
It does not reverify every complete archive hash, other event content or damaged
records. That result is not authorization to delete transcripts.

No live account, provider or production database was used. No recovery source,
environment file or transcript archive was deleted. Publishing this PR does not
establish exact-main preservation or complete the broader HAI recovery audit.
