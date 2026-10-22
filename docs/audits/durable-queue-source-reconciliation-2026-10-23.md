# Durable queue source reconciliation

## Reviewed versions

Compared six retained source versions against the current queue implementation:

- Model-maintenance worktree: `backend/internal/durablejob/repository.go`,
  `runner.go`, `runner_test.go`, and `integration_test.go`.
- Artifact worktree: `backend/internal/durablejob/runner.go` and
  `runner_test.go`.

The current code preserves the older capabilities with stronger recovery and
review boundaries. Do not copy the older files over it. This comparison does
not cover every queue source, model, migration, or transcript.

## Decisions

| Boundary | Retain |
| --- | --- |
| Active work | Include review-held and settling occurrences in singleton checks. |
| Recovery | Apply queue-specific lease durations; refuse a global-only adapter in the runner. |
| Unknown outcomes | Persist replay policy and hold review-sensitive or unknown policies rather than automatically repeating uncertain work. |
| Registration | Persist review scheduling before publishing its handler/policy and prevent downgrades through ordinary registration. |
| Claims | Claim one job immediately before execution, rather than letting unstarted batch leases expire. |
| Same-pass retries | Preserve the original failure while deferring a duplicate same-pass claim. |
| Attempts | Count expired deliveries and successful attempts; dead-letter exhausted replay-safe work without another execution. |
| Recurrences | Commit terminal state and any successor atomically, under singleton locks, without releasing review-held uncertainty. |
| Ownership | Check worker identity and lease generation on settlement; surface unconfirmed writes. |
| Shutdown | Preserve in-flight leases until recovery; release unstarted work without consuming an attempt. |
| Lifecycle | Use lifecycle-owned handler admission and observe cancellation even with sub-tick heartbeat intervals. |
| Retry cycles | Retain the lookup for recently exhausted stable payloads, allowing scanners to respect cooldown. |
| Privacy | Retain stored error redaction and repair the remaining unredacted poll log. |
| Database tests | Keep explicit dedicated database authorization and database-name verification before destructive fixture setup. |

The comparison also identified three repairs recorded in
[the focused audit](durable-review-sentinel-2026-10-23.md): explicit review
sentinel classification, preservation of a review result across a later claim
failure, and secret redaction in the poll log.

## Evidence and limitations

The full local durable-queue and ambient packages passed after the repairs.
The existing CI workflow selects the dedicated PostgreSQL queue integration
suite; database execution for this PR remains a separate CI gate. Local fake
repositories are not evidence of SQL lock ordering or crash recovery behavior.

All six retained original versions were freshly checked against their frozen
inventory fingerprints. They remain on disk. Semantic review is not exact-byte
preservation on main and does not authorize whole-worktree/archive deletion.
