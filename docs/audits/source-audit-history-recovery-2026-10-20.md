# Source activity history recovery - 2026-10-20

Canonical baseline: main `41840861bed0deaa206752dc8b19b98223943711`.

## Recovered operational path

Retained frontend variants contain `pageAuditLogs` and `pageSyncJobs`, but the
canonical UI read bounded recent arrays and displayed only eight audit events
and six sync jobs without older-page controls. The repository and service already
contained offset readers. This recovery wires them through HTTP, the typed client,
and Advanced activity while replacing the unsafe unowned sync-job offset reader.

Requests without `offset` keep the existing recent-array contract. An explicit
offset requests a page envelope with items, total, limit, offset, and hasMore.
Offsets must be single nonnegative integers no greater than 1,000,000; limits
default to eight and must be single integers between one and 250. Unsupported
paged services fail closed instead of returning an array that looks like a page.

The authenticated subject determines ownership; query parameters cannot override
it. The paged reader excludes foreign-owned and unowned legacy sources before
counting or fetching records. An existing source-specific access check still
applies. Backend errors retain public redaction behavior.

The client validates the page shape, offset, limit, total, continuation flag,
and row fields before displaying it. In particular, an older server ignoring
offset cannot masquerade as working pagination. The UI loads eight audit rows
and six sync rows per request; filters explicitly apply to the loaded records.
Previous/Next controls have accessible names, tooltips, and loading guards. Failed reads keep
the previous page and expose a retry path rather than claiming an empty history.

History browsing never replaces the latest sync-job array or its operational
indexes. The sync reader applies manual-job ownership filtering in SQL before
counting or paging, including exclusion of missing manual-job owners. Returned
manual-job statuses use the existing durable-worker projection instead of stale
history-only status. Nonmanual jobs remain scoped to the authenticated owner's
sources. Legacy no-offset endpoints remain unchanged.

The UI stores history rows separately from live jobs. Polling may update an
already-loaded history row with the same job ID; it does not invent new history
totals. Refresh cancels obsolete reads and invalidates closed history caches
without eagerly loading those sections in Basic mode.

Audit, sync, and extraction pagers permit backward navigation when concurrent
deletions shrink the total below an older offset. Offset browsing is live
history, not an immutable snapshot.

## Verification

- Focused audit and sync HTTP/service tests cover authenticated ownership,
  invalid/duplicate bounds, missing permission, old array compatibility,
  unavailable capabilities, redaction, and exclusion of foreign/legacy sources.
- The full native source package suite passed.
- All 129 final focused Angular tests passed, including actual rendered
  Previous/Next clicks, duplicate-read prevention, failed-read retention, and
  preservation of latest sync-job state, plus concurrent-deletion, obsolete-read,
  closed-cache invalidation, Basic laziness, and zero/invalid timestamp regressions.
- The final production build including both history readers passed using Node
  26.12.0 and the existing installed dependency tree. Existing unrelated style
  warnings remain; no budget was raised. Fresh lockfile installation remains a
  hosted CI gate.
- The existing real-PostgreSQL fixture now seeds audit and sync rows and asserts
  ordering, offset, totals, foreign-source exclusion, manual-owner filtering
  before paging, durable-status projection, and an empty source scope. It also
  exercises the actual HTTP handler, service, and repository together for Alice
  and Bob using injected test subjects. Real IDP token verification remains a
  separate gate. Local execution without a database skips that fixture; hosted database CI remains
  the execution gate. No local database or Docker stack was started.
- An initial focused Angular invocation incorrectly combined a narrowed
  TypeScript program with default all-spec discovery. It failed before tests;
  selecting the same two spec files explicitly corrected the harness scope.

These checks do not establish deployed-route, mobile, external-account, or full
product acceptance. No archive or worktree deletion is authorized by this patch.

## Additional retained-content evidence

A second bounded full-file scan revalidated all 13 archive hashes and retained
the original 58,960 patch-entry count. All 59 decoding failures are invalid JSON
ending unexpectedly, not merely incompatible typed payloads. Re-reading those
exact byte ranges and decoding only complete leading fields identifies all 59
as `compacted` records blocked inside `payload.replacement_history`. No truncated
value was reconstructed or treated as a completed patch event. Incomplete
compaction decisions still require reconciliation; the original records remain.

The separate HAI port root accounts for 214 of the 239 patch variants outside
the previously inventoried worktrees. Literal comparison found 43 variants with
all new-side hunks present and two exact added-file matches. The rest still need
content review, including changes superseded by stronger current safeguards.
These are triage counts, not integration or deletion counts. Existing accessible
image-removal buttons and focus styling were also confirmed directly in current
frontend source rather than blindly restored from an older patch.

Private paths, transcript text, range hashes, and raw inventories are not included
here. No original archive was repaired, truncated, moved, or removed.
