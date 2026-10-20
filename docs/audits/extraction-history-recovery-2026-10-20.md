# Extraction history recovery - 2026-10-20

Reviewed from main `7f93a2229b6cfeb42a9ec1f18f1ae50a3b804017`.

## Retained work and integration gap

Retained frontend variants include `pageExtractions`, `pageSyncJobs`, and
`pageAuditLogs`, including versions inside unresolved merge conflicts. Main
already contains the corresponding history service/repository readers, but its
extraction HTTP handler only exposed a recent bounded array. The Advanced records
view loaded 100 records and displayed eight, without a path to older records.

This recovery wires extraction paging through the existing endpoint and client
to Previous/Next controls. Requests explicitly supplying `offset` receive the
paged envelope; requests without it retain the existing array/header contract.
Offsets must be nonnegative integers no greater than 1,000,000; repeated/invalid
offsets are rejected. The existing paged service caps pages at 250 records; the
UI requests eight. Live offset browsing is not an immutable import snapshot.

Both extraction readers now share the existing strict owner-source selection.
This avoids reviving the older paged path's inclusion of unowned legacy sources.
Current idempotent correction requests, revision checks, and destructive-action
approval headers are preserved rather than copied from the older frontend.

Failed page navigation retains the current records and offset. Navigation is
blocked while a read is active; changing archive scope returns to the first page.
Filtering is visibly scoped to the loaded page. Per-source badges describe the
loaded page rather than claiming global source totals.

## Inventory and conflict triage

The earlier 9,742 raw non-Go differences reduce to 2,704 distinct path/hash
variants. Rechecking one recorded copy of each variant found 990 CRLF-only
variants (6,095 recorded physical instances), leaving 1,714 content variants
(3,647 instances) for review. These are not missing-feature counts or deletion
approvals; not every physical copy was reread in the normalization pass.

TypeScript AST triage covered 710 frontend variants. Of those, 191 failed parsing;
hash-verified rereads found unresolved conflict markers in all 191, with no
changed/missing sources. The remaining variants produced 147 declaration-name
candidates and 131 literal test-title candidates. Parsing both sides of the 191
conflicts in memory produced 382 views, of which 372 parsed; ten still require
manual syntax review. Both-side name counts contain duplicates and do not prove
whether behavior has been integrated.

Direct comparisons also found renamed framework-registry array normalization,
an existing dedicated agent-team listing service, and replacement of blind
extraction edits with governed correction submission. These older declarations
must not overwrite current safety behavior merely because names differ.

## Remaining scope

Sync-job and audit-log paging, full semantic review of the other TypeScript
candidates, templates/styles/configuration variants, ignored/unregistered copies,
and retained transcript content remain separate unfinished work. No local source
file, original conflict, archive, or credential is removed by this recovery.
Private inventories and environment data are not published.

## Verification

- The HTTP paging regressions failed before wiring the handler to the paged
  service. The full native source package suite passed after the recovery,
  including the owner-scope handoff assertion.
- All 118 focused frontend tests passed in headless Chrome through Karma. The
  rendered Advanced section was exercised by clicking Next and Previous; tests
  also cover duplicate clicks, failed-read retention, stale-cache invalidation,
  scope reset, and an older backend ignoring an offset.
- The production build passed with a 781.25 kB initial bundle and the existing
  single-worker build policy. Existing unrelated component-style warning budgets
  remain; no budget was raised. The pager reuses existing toolbar styling.
- Local validation used verified Node 26.12.0 and the existing installed
  dependency tree (Angular CLI 22.2.2). Fresh lockfile installation of the declared
  22.2.3 packages remains a hosted CI gate, not a local result.
- The existing PostgreSQL source-history fixture now asserts offset ordering and
  empty-owner scope. It compiles locally, but execution requires the existing CI
  database job; no local database or Docker environment was started.
- Live-route, mobile, screenshot, external-provider, and deployment acceptance
  were not performed by these component tests. They are not completion evidence
  for the whole HAI product or the retained source corpus.
