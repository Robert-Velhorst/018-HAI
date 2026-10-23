# Runtime execution reconciliation

## Retained work reviewed

Compared four retained versions of `backend/internal/agentruntime/runtime.go`
and `runtime_test.go` from the model-maintenance and artifact worktrees with
main `4684546907900d4958f85e7b71ba8848376b0b93`.

| Area | Integration decision |
| --- | --- |
| Provider inventory | Restore the older `vercel-ai-gateway` classification when an inspected archive actually contains that extension. Do not create providers in an empty inventory or enable execution. |
| Authorization scope | Retain the current canonical task scope shared by proof binding and adapter execution. |
| Stop acknowledgements | Retain identity-bound runtime/task/reference validation and indeterminate results for mismatched acknowledgements. Add the request-context guard described below. |
| Gateway transport | Retain exact host/port allowlisting, vetted DNS-to-IP dialing, TLS validation, no redirect following, blocked address-space checks, and the explicitly pinned Docker-host exception. |
| Delegated artifacts | Retain owner-bound durable completed receipts, matching terminal timestamps, and the emergency-stop admission boundary before metadata persistence. |
| Managed archives | Retain durable selection, integrity failure reporting, governed rollback, previous archives, and shared emergency-stop commit fencing instead of automatic deletion. |
| DeepSeek fixtures | Retain context-aware dispatch fixtures and explicit production isolation/start-ordering blocks. |

The older artifact version's unfenced refresh and direct mutation paths are
superseded, not ported back. This review covers these four source versions, not
every file or runtime dependency in either worktree.

## Stop request repair

Registry stop operations previously could cancel a running local task and call
an adapter even if the incoming request context was already cancelled, expired,
or missing. The registry now rejects such requests before those effects on the
owner, execution-reference and exact-receipt entry points. This does not promise
rollback if cancellation occurs after a valid stop has already begun, and does
not restrict the separate operator emergency-stop control.

## Verification

- Twenty stop cases cover live/missing/cancelled/expired contexts, owner and
  reference stops with/without tracked local tasks, and exact receipt stops.
  All fifteen invalid-context cases failed before the guard was added; five
  live-context controls retain normal dispatch and local cancellation.
- The archive-provider classification regression failed before restoration.
  It also checks that an empty inventory does not invent provider availability.
- Full `agentruntime` package tests passed locally with Windows Go 1.27.3.
- These fixtures do not establish live provider, Gateway or external execution
  acceptance. No runtime is enabled and no account credentials are changed.
- Source fingerprints remain private and original copies remain intact. Review
  and an open PR are not evidence permitting local source deletion.
