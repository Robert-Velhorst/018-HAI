# Command dashboard approval reconciliation

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

The existing control-room native suite, including the imported new scenarios,
passes 77 tests without skips. A no-emit TypeScript program for the controller,
its Jasmine spec and 330 imported source/declaration files reports zero
diagnostics. The Jasmine happy-path fixture now supplies real-shaped evidence
instead of expecting success from `{}`.

No production build, rendered browser check, full Karma suite or live backend
acceptance was run locally in this disk-constrained round. Hosted CI remains a
separate publication gate. No dependency installation or deployed app change
is part of this repair.

## Remaining scope and retention

Other decision paths, especially pursuit completion, still need equivalent
state/evidence validation and lifecycle coverage. In-memory source review also
reproduced a completion confirmation from active/unverified pursuit detail;
this approval-only repair does not claim to fix it. The full controller is not
yet a completed reconciliation ledger entry for that reason.

The additional checkout inventory distinguishes raw working-byte matches from
line-ending-only matches; neither a normalized comparison nor an open PR is
permission to discard differing work. Original source copies and transcript
archives remain retained. Main preservation must be verified separately before
any relevant local deletion.
