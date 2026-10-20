# Runtime control read recovery

## Scope and retained-work reconciliation

Review of retained runtime-control changes identified an unbounded read path in
the current page. A stalled session, status, or readiness request could leave
the loading state active indefinitely. Pending reads also survived navigation.

Older control-flow variants were not restored wholesale. They coupled routine
operator controls to approval permissions and automatically consumed generic
control proofs. The current stop-revision-bound resume review, server-side
preparation, permission boundaries, and response checks remain authoritative.

## Change

- Bound session lookup to six seconds and consume only its first response.
- Bound the combined status/readiness read to a separate six-second window.
  Sequential stages can therefore take approximately twelve seconds in total.
- Reuse existing unavailable/error states; a timeout never implies a successful
  action or silently submits a mutation. Recovery is an explicit refresh.
- Unsubscribe pending reads on destruction and reject late refresh calls after
  navigation, including those triggered by a delayed mutation callback.
- Preserve duplicate-read and busy-action guards. No mutation retry, mutation
  timeout, role change, backend API change, or approval bypass is introduced.

## Verification

The 23 focused runtime component/service tests passed, including stalled-session
recovery, stalled-status recovery, explicit retry, no mutation submission, and
subscription cancellation on destruction. The canonical production build and
full npm test passed, including preflight guards, 180 Node tests and 1,424
Angular tests. Local verification used Node 26.12.0, npm 11.6.2 and the existing
installed dependency tree, not a fresh lockfile installation. Existing style
budget warnings remain unchanged. Hosted exact-head CI is the merge gate, not
a substitute for live runtime/provider acceptance.

## Remaining boundaries

This patch does not solve mutation unknown-outcome reconciliation or establish
end-to-end provider execution safety. Source archives and worktrees remain
retained until their complete work is demonstrably preserved on main. Neither
these tests nor this reconciliation note authorize archive deletion.
