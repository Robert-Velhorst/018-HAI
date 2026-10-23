# Model maintenance scheduler reconciliation

## Scope

Reviewed both retained worktree variants of `maintenance_scheduler.go` in full
against current code. Their behavior is already represented or superseded by
the current scheduler. Keep explicit background permission rather than the
older nil-callback authorization, lifecycle ownership, single scheduler per
service, provider-probe startup and startup-signal coalescing. Preserve durable
deadline scheduling, pause polling, emergency-stop monitoring and the existing
rate-limited retry path after cancelled maintenance.

This review does not mark the larger maintenance implementation or scheduler
test files as fully reconciled.

## Integrated correction

The run wrapper previously relied on the provider callback to report its own
cancellation. A callback that observed a cancelled context but returned a result
without that flag could make a paused run look complete and select the ordinary
deadline instead of the cancellation recovery path.

The wrapper now returns cancellation for invalid or already cancelled admission
and records context cancellation after the callback, before its own normal
monitor cleanup. Completed observation counters remain intact. Successful runs
are not marked cancelled merely because cleanup cancels their monitor.

## Verification and limits

Three negative cases reproduce the earlier misclassification: already cancelled
entry, parent cancellation during work and the real permission monitor cancelling
during work. The fourth case is a successful-run control. No external provider is
contacted by these fixtures. This is cooperative cancellation accounting, not
preemption or rollback of a callback that ignores cancellation.

Original worktrees and archives remain intact. Publication and semantic review
are separate from main-preservation verification and local deletion.
