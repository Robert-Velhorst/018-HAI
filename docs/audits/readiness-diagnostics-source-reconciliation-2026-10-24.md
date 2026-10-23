# Readiness diagnostics source reconciliation

## Retained behavior

Compare the original health and doctor sources with their canonical deltas.
Keep the shared serving-pool database probe rather than restoring an independently
opened and closed pool for every readiness request. Preserve cancellation,
scheduler status and the noncritical outbox-delivery probe. Broker reachability
alone does not establish event delivery. The existing provider root probe is
reachability evidence, not successful generation or credential acceptance.

Keep the canonical warning for a shipped database password outside production
and its failure in production. Do not downgrade these diagnostics to the older
source's optimistic default-password classification. The separately documented
legacy migration-switch precedence is unchanged by this review or repair.

## Credential projection repair

The original and canonical provider error format can embed an unquoted URL in
surrounding diagnostic text. The central secret filter previously handled whole
URLs and quoted URLs but could leave credentials in an embedded URL's authority.
Live probe failures also reached the detailed readiness checks without applying
that shared filter. Authentication of the detailed endpoint is not permission
to expose configuration credentials.

The shared filter now finds embedded URL spans and applies the existing parsed
URL policy. Malformed credential URLs fail closed. Already-redacted text remains
idempotent: an empty sensitive value at the end of a scanned span adds no new
credential when the existing text marker follows a space. The existing text
assignment policy may discard adjacent query context; it is not weakened merely
to preserve query fields in diagnostics.

The doctor probe projection applies this same filter before constructing its
public detail. Failure severity, check names, ordinary failure causes and the
provider configuration label remain available. Provider endpoints, credentials,
authentication requirements, runtime settings and live deployments are unchanged.

## Verification and preservation boundary

Three focused regression tests fail before the correction with synthetic
credentials. After correction, both complete Go packages `internal/safety` and
`internal/doctor` pass uncached tests and `go vet` under Go 1.27.3. Tests cover
plain and JSON diagnostics, malformed embedded URLs, benign-text preservation,
idempotence and unchanged critical/noncritical failure classification. Only
synthetic error strings are used; no real provider is contacted.

An earlier bounded native verification passed `internal/doctor`; its remaining
`internal/health` compilation was deliberately stopped to reduce local resource
use. That stopped build is not a health-package pass or failure. The repair does
not claim full backend, live database, browser or hosted new-head acceptance.

All 244 present tracked files in the original frozen dirty-status inventory
still match fresh source hashes. A separate crosswalk locates content-matched
review references for 83 of them; reference linkage does not prove semantic
completion or make the remaining baseline, missing, ignored or untracked files
disposable. Original sources and transcript archives remain retained pending
the independent main-preservation and deletion gates.
