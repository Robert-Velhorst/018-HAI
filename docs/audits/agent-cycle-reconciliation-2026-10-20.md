# Agent-cycle reconciliation and cancellation repair

## Comparison scope

This review follows the retained `Service.runForOwner` declaration candidate
against main `e08d4bc029d7631b740f857051b6363324b29b5a`. A retained version
deliberately skips source sync, recovery, workflow execution, follow-ups,
ambient scanning and dashboard refresh in the authenticated owner path. It
only refreshes owner context and pursuit state, reserving execution for a
separate system worker. Another retained version already contains the newer
owner-scoped `runForOwnerContext` path.

Current main implements owner-scoped contracts for these phases and refuses
global fallbacks. Existing regressions cover operating order, missing owner
contracts, skip controls, owner-only learning and shared pursuit snapshots.
Restoring the older read-only helper would remove integrated functionality,
not recover missing work. Keep the current orchestrator.

This is a review of the selected declaration and its phase boundaries, not a
claim that all retained agent-cycle files or all archived work are reconciled.

## Reproduced current gap

The optimized owner path retrieves a pursuit snapshot and immediately starts
an ambient scan, without checking whether the caller cancelled during that
snapshot. It also treats wrapped cancellation and deadline errors from the
snapshot or snapshot-based scan as ordinary phase failures and may continue
to later phases.

Before the fix, five synthetic regression cases failed:

1. Caller cancellation after a successful snapshot still started a scan.
2. Snapshot cancellation was not returned as an interruption.
3. Snapshot deadline expiry was not returned as an interruption.
4. Scan cancellation allowed later dashboard and brief phases to run.
5. Scan deadline expiry allowed later dashboard and brief phases to run.

These are local reproductions, not evidence of a deployed account incident.

## Repair and acceptance boundary

Use the existing `requestContextError` and `interruptCycle` helpers at the
snapshot/scan boundary. Check caller cancellation before starting the next
phase. Keep settled source/workflow summaries and any returned scan summary
in the partial interrupted result. Do not retry, broaden ownership, change
approval rules, introduce another execution path, or cancel an atomic work
unit halfway through its persistence.

The full native agent-cycle suite passed after this repair, including the five
previously failing cases. `git diff --check` passed. Hosted exact-head CI is a
separate merge gate. These tests do not prove real-provider,
IDP, database, external account or every possible cancellation boundary.

## Retention

No retained source, worktree or transcript is removed by this patch. Remaining
variants need further review; this report is not deletion authorization or
whole-archive equivalence evidence.
