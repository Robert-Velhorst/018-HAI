# Command dashboard approval reconciliation

Current local gate: 102 native control-room tests, including 33 targeted
dashboard decision scenarios, and zero no-emit TypeScript diagnostics. Earlier
counts below describe successive checks, not additional unique tests.

## Source scope

The complete retained Command Dashboard controller and its complete pre-edit
integration delta were read. The retained controller has 81 methods; the
pre-edit integration controller retains all 81, with 75 whitespace-normalized
definitions unchanged, six adapted, and two additional OpenClaw methods.
Current approval preparation, rollback, URL safety and OpenClaw observation
guards remain intact. This is not certification of the full application.

## Approval repair

The controller previously treated any emitted approval response, including
`{}`, as an audited success. An empty observable left its action busy, and a
late response after component destruction still notified and refreshed the UI.
The actual WorkflowService forwards HTTP responses without validating their
approval evidence, so a service-method stub was not the only affected boundary.

The repaired workflow approval path now:

- Rejects invalid workflow references, mismatched workflow decision references
  and non-boolean decisions before service dispatch.
- Defers service invocation so synchronous failures use the same recovery path.
- Consumes one response, rejects empty completion, uses the shared operation
  timeout and ends UI observation on component destruction.
- Releases its pending flag through finalization, including cancellation.
- Requires the requested workflow ID, approval-required state, matching
  approved/ready or rejected/blocked state, and a matching dated approval
  decision with a non-nil UUID.
- Reports a matching server response, not task execution or independently
  proven audit persistence. Errors expose no raw backend/provider detail.
- Refreshes the pursuit decision queue for uncertain outcomes without
  automatically repeating a consequential mutation.

This preserves the backend's verified-actor, ownership, permission and atomic
approval transition/decision/event controls. Unsubscribing from UI observation
does not prove that a server-side operation was cancelled. Concurrent state
changes can leave a response unconfirmed even if approval was saved; that is a
reconciliation case, not permission to retry blindly.

## Verification

Eight new native regression scenarios failed against the pre-fix controller
and pass after the repair. They exercise the actual controller and RxJS;
one uses the actual WorkflowService with a mocked HTTP response. Cases include
both decisions, incomplete/foreign/contradictory receipts, empty/error/throw,
virtual-time timeout, duplicate dispatch, duplicate emissions and destruction.

The initial approval-only control-room native run, including the imported new
scenarios, passed 77 tests without skips. A no-emit TypeScript program for the controller,
its Jasmine spec and 330 imported source/declaration files reports zero
diagnostics. The Jasmine happy-path fixture now supplies real-shaped evidence
instead of expecting success from `{}`.

No production build, rendered browser check, full Karma suite or live backend
acceptance was run locally in this disk-constrained round. Hosted CI remains a
separate publication gate. No dependency installation or deployed app change
is part of this repair.

## Remaining scope and retention

The subsequent follow-ups below cover the false completion confirmation and
the remaining decision paths. Non-decision reads/mutations, server-side gaps,
rendered acceptance and broader source reconciliation remain separate work.

The additional checkout inventory distinguishes raw working-byte matches from
line-ending-only matches; neither a normalized comparison nor an open PR is
permission to discard differing work. Original source copies and transcript
archives remain retained. Main preservation must be verified separately before
any relevant local deletion.

## Completion follow-up

Source checks confirmed the backend's canonical completion-review ID,
completed/verified state and pursuit.decision_resolved activity contract. The
controller now captures the requested identity and requires the returned
pursuit, resolution activity, decision outcome and queue state to agree.
Approved completion requires non-archived completed/verified state; rejection
requires a still-open active/waiting/blocked pursuit. A same-ID pending queue
entry, including differently cased pending text, makes the response unconfirmed.

This path now defers the service call, observes one nonempty response, applies
the shared operation timeout, finalizes the pending flag and unsubscribes on
destruction. It does not automatically retry or expose raw provider errors.
Matching responses are described as server reports, not independent verification.
The backend's ownership, approval permission and evidence guards remain intact.

Eight completion scenarios failed on the pre-fix controller while the eight
approval scenarios continued to pass. All 16 targeted native scenarios pass
after the repair, including real PursuitService normalization with mocked HTTP,
contradictory active/unverified state, mismatched activity, pending queue,
rejection of closed pursuits, empty/error/throw, timeout, duplicate emissions,
destruction and invalid identities. The updated Jasmine fixture also supplies
actual-shaped resolution evidence rather than expecting success from `{}`.

The combined control-room native suite now passes 85 tests without skips. The
no-emit TypeScript check was rerun after this follow-up: two roots, 330 imported
source/declaration files, zero diagnostics, with host output writes forbidden.

Neither a live server mutation nor independent outcome verification is proven
by these tests. No source archive becomes deletion-eligible from this repair.

## Remaining decision paths

All six decision handlers, including the separate candidate-archive branch,
now use the same deferred, one-response, nonempty, timeout-bounded observation.
Its finalizer releases the matching pending action; destruction cancels UI
observation, not proof of backend execution. Unknown results refresh the queue
without automatic mutation retries or raw provider errors.

The additional response checks are specific to the backend contracts:

- Proposal: exact workflow/proposal IDs, requested outcome/option, resolution
  timestamp, and matching dated proposal decision. Both frontend service and
  backend router use the existing `/proposals/:id/resolve` path.
- Candidate acceptance: accepted intake origin, still-open pursuit state,
  matching dated acceptance activity and removal of its candidate decision.
  Acceptance alone is not described as creation or execution of a workflow.
- Candidate rejection: exact pursuit ID plus archived flag and archived state.
  The endpoint returns a pursuit row, not a separate audit receipt.
- Next action: exact pursuit decision, resolution activity and non-pending
  queue state. Approval additionally requires a matching source-bound workflow
  and approved-next-action link with approval required.
- Runtime review: exact canonical attempt/decision/evidence references and
  resolution activity. Approval additionally requires the specific attempt,
  matching source-bound approval-gated workflow and recovery link. This is not
  described as a direct retry or independently proven runtime state.

The final 32 scenarios were replayed against the exact controller from commit
`d49226f` using an in-memory override of that one source read: 16 earlier
approval/completion scenarios passed and all 16 new scenarios failed. This
comparison used corrected proposal route fixtures, verified against both the
actual service and router; two earlier fixture URLs had omitted `/resolve`.
No checkout or source file was modified for the baseline replay. With the
selection-routing scenario below, all 33 targeted scenarios now pass, and the
containing control-room suite passes 102
tests without skips. The TypeScript controller/spec program reports zero
diagnostics over 330 source/declaration files, with no output writes allowed.
Real frontend services are exercised against mocked HTTP responses; no live
provider, browser, container or database mutation is claimed.

Exact automation-selection proposals use a reserved recommended-action value
which the backend itself matches exactly. A generic `Accept` option cannot
satisfy its candidate-list validation. Approval of that proposal now opens the
existing workflow review using its `workflowId` query parameter without sending
a decision. A native test verifies the route and absence of a mutation request;
rendered navigation and real owner selection remain separate acceptance gates.

## Server follow-ups still open

Reading the actual pursuit service identified boundaries that the frontend
cannot certify or repair by checking responses:

- Candidate acceptance updates the pursuit and then ignores an acceptance
  activity write error. The UI now remains unconfirmed if that activity is
  absent; atomic server persistence still needs separate repair and tests.
- `runtimeAttemptForDecision` considers evidence and decision references
  independently and falls back to the sole attempt. The dashboard now refuses
  conflicting/noncanonical references, but this does not fix direct API callers.
  Server-side exact binding and legacy compatibility need separate review.

These server gaps, other controller operations, live acceptance and the broader
file/archive scope remain open. Reviewed source is not equivalent to complete
product acceptance, main preservation or permission to delete original copies.
