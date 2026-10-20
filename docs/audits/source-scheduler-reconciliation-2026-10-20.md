# Source and scheduler production reconciliation

## Scope

Review the complete diffs of fifteen selected tracked backend file versions
against public main commit `847ce573a65659abca85bb8897a42e386ab8fe2c`.
Cross-check the current extraction history service, sync-error formatting and
workflow intake routing where the changes move responsibilities between layers.
Verify original source integrity against the private frozen inventory.

The selected differences do not identify missing retained functionality that
needs restoration. Keep the newer implementation on main. This is a per-file
source review, not whole-worktree equivalence, archive completion, a live
integration test or deletion eligibility.

## Source connector decisions

Paths below are relative to `backend/internal/`.

| File | Decision |
| --- | --- |
| `source/durable_scheduler.go` | Keep actual newly enqueued counts, review routing for terminal sync failures, retirement of obsolete paused/revoked intents and lease-contention deferral. Keep the bounded newer one-minute poll default and explicit global legacy-fallback setting. |
| `source/scheduler.go` | Keep redacted scheduler readiness records and fail-closed startup unless degraded volatile fallback is explicitly enabled. |
| `source/handler.go` | Keep the compact owner-scoped overview/health endpoints and repository-backed extraction/history pagination. Preserve legacy array responses when offset is absent. Do not restore unbounded reads followed by trimming in the HTTP handler. |
| `source/oauth.go` | Keep digest-only OAuth state persistence and owner-bound one-time consumption before token exchange. Other selected changes are formatting. |
| `source/repository.go` | Keep distinct optimistic source-update conflicts and deterministic extraction ordering. Keep atomic OAuth state save/consume/revoke, source locking, expiry, ownership, digest and active-source validation. |
| `source/service.go` | Keep owner-scoped contracts, explicit unavailable Autofever status, deliberate clearing of rejected LARO cursors, terminal-failure review routing and context-aware semantic retrieval. Keep extraction ownership selection in the bounded service layer. |
| `source/trello.go` | Keep quiet-board action checks before fetching every card/comment. Unknown, saturated or malformed activity falls back to bounded reconciliation. Activity API failures remain failures, not evidence of an unchanged board. |

The old extraction handler filtered to owned sources after retrieving records.
The new bounded service selects those owned source IDs before paging. This
responsibility moved; restoring the removed helper alone would not improve
isolation and restoring the old read path would undo bounded access.

OAuth repository changes retain only a digest of the signed state, not the
bearer value. Save and consume serialize with revocation by locking the source.
Reviewing these branches does not prove a live Google authorization round trip
or production database concurrency behavior.

The terminal-failure workflow uses the existing workflow intake boundary.
Non-routing pursuit linkers are rejected rather than bypassing the required
lifecycle router. No new execution authority is derived from an ingestion error.

## Scheduling, ambient and worker decisions

| File | Decision |
| --- | --- |
| `workflow/durable_scheduler.go` | Keep the bounded newer poll default and explicit global legacy-fallback configuration. No retained durable handler is removed in this selected diff. |
| `ambient/durable_scheduler.go` | Keep the bounded newer poll default and explicit global legacy-fallback configuration. No retained durable handler is removed in this selected diff. |
| `ambient/scheduler.go` | Keep redacted readiness/lifecycle state. Durable startup failure remains stopped. Legacy mode requires both durable opt-out and global legacy opt-in. |
| `workflow/scheduler.go` | Keep redacted readiness/lifecycle state and explicit opt-in for non-crash-recoverable fallback instead of automatic fallback after durable failure. |
| `workflow/reminder_delivery.go` | Keep nil results before any checked outcome and partial summaries after settled progress when cancellation/failure occurs. Do not treat an empty interrupted summary as completed work. |
| `ambient/repository.go` | Keep optional batch fingerprint lookup, including empty-input handling; single-record lookup remains available. |
| `ambient/service.go` | Keep compact overview, validated owner-bound pursuit snapshot reuse, checkpointed batch candidate storage, content-sensitive writes/daily freshness, owner-filtered visibility and read-side defaults. Keep explicit failed/unconfirmed scan handling rather than restoring older counters or write-heavy reads. |
| `background/worker.go` | Keep paused-mode safe deferral, emergency-mode processing gates and bounded public error summaries with separate redacted local diagnostics. Retained ingestion and claim flows remain. |

These scheduling policies are not interchangeable: ambient startup failure
does not fall back merely because the global legacy flag is set. Workflow and
source scheduling have their own explicit fallback contracts. Do not collapse
them into one weaker generic fallback during consolidation.

The older poll default was five minutes; the reviewed main default is one
minute with existing bounds and configuration overrides. This report preserves
that source choice without changing local scheduler configuration or starting
background work. It does not claim an overall resource-reduction percentage.

Ambient batch lookup and unchanged-content write suppression are already
implemented on main. Daily freshness persistence is intentionally different
from writing every rediscovery. This review does not measure database load or
prove full end-to-end ambient execution.

## Verification and retention

Full selected diffs were read and original source hashes were checked against
the private inventory. No product implementation changed in this batch and no
local runtime, database, provider, migration, scheduled task or container was
started for it. Hosted exact-head CI remains a merge gate, not evidence of live
account acceptance.

Original sources and all remaining archives, runtime state, configuration and
backups remain intact. The other tracked variants and excluded operational
material still require their own reconciliation. No broad cleanup follows from
this report alone.
