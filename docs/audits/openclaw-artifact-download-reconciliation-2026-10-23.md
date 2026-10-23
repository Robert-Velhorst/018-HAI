# OpenClaw artifact download reconciliation

## Source review

Reviewed the retained `openclaw_artifact_download.go` and
`openclaw_artifact_download_test.go` variants in full against current code.
Keep the newer case-folded duplicate JSON rejection, exact optional-size
metadata matching and explicit unsupported classification above the content
limit. Keep current approved-origin transport rules rather than restoring the
older fixture's assumption that any configured loopback URL can be fetched.
Existing byte, run/session/owner scope, metadata-change, secret-free transport,
invalid encoding and size-boundary assertions remain in the current tests.

## Cancellation boundary

Three added regressions reproduce a nil-parent panic and lost cancellation
identity before or during the stored receipt lookup. Downloads now require a
non-nil context, reject an already cancelled context before touching the receipt
store, and recheck after the synchronous lookup before opening a connection.
Cancellation is returned as the context error, not misreported as a connection
failure. Owner/run/session/terminal receipt binding, read-only capabilities,
bounded frames and approved-origin restrictions are unchanged.

The synchronous receipt-store interface is not made cancellable by this patch.
An in-progress lookup can finish, but cancellation observed on return blocks the
next network stage. Tests count store reads and local HTTP requests and require
zero requests for every rejected context case.

## Evidence and preservation

The three regression cases fail before the change and pass after it. Local
package tests are fixture evidence, not live Gateway/provider acceptance.
Original source worktrees remain intact. Semantic reconciliation and PR
publication do not establish byte-for-byte main preservation or authorize
deleting those original copies.
