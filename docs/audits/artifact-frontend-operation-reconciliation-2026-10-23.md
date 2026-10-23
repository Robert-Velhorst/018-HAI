# Artifact frontend operation reconciliation

## Source review

Reviewed the retained `openclaw-artifact-operations.service.ts` and its complete
specification against current code. Keep bounded terminal replay, watcher cleanup,
concurrent-operation accounting, in-flight coalescing, size/digest validation,
version-bound removal and requests that finish independently of a closing view.
Keep the newer allow-listed retained-copy integrity error code without replaying
raw backend error details into shared operation status.

The artifact component's retain/remove subscription handlers were inspected for
compatibility; this is not a full reconciliation of that larger component file.

## Integrated corrections

Six new Chrome Headless regressions reproduced these faults:

- Null retention responses could leave shared state at running.
- Invalid/mismatched or oversized retention evidence could publish failed state
  while still reaching a subscriber's success handler.
- A removal response other than the required 204 could similarly reach success.
- Resubscribing to an errored mutation observable could send its HTTP request
  again because the sharing operator reset on error.
- A synchronous watcher callback could reenter before in-flight registration,
  issuing a duplicate mutation request.

Validate mutation evidence before success delivery, route validation errors
through the shared failure publisher and preserve the terminal outcome for later
subscribers without restarting HTTP. Register the shared operation before
notifying watchers. Existing timeout, error-code allowlisting, bounded retained
state and view-independent request ownership remain in place.

A failed confirmation is not proof that the backend mutation had no effect.
Existing component error handling refreshes the current retained-copy list;
there is no automatic retry or claim of rollback.

## Verification and preservation

The six new cases fail before the repair. After it, all 46 focused operation
service and artifact component tests pass in real Chrome Headless using Angular
HTTP test fixtures. This covers the changed service plus its existing component
consumers, not a live Gateway/provider/account or deployed-app acceptance test.
Positive controls cover confirmed 204 removal with its If-Match version, cached
success replay and a deliberately new request after an earlier terminal failure.

The production build and existing runtime/progressive-section/theme/icon checks
pass. Four existing component-SCSS budget warnings remain in the shell, workflow,
pursuits and task-blueprint styles; this service correction does not change them.

Original source worktrees and archives remain intact. Review, local tests and PR
publication do not establish main preservation or local deletion eligibility.
