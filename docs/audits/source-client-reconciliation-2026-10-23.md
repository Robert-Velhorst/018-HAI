# Source client reconciliation

## Retained implementation

Reviewed the saved model-audit source service interface, HTTP client and complete
client test suite, then all differences against current code. Preserve existing
manual-sync queue/status calls, stable idempotency keys, exact correction revision
headers, no-cache status reads, source controls and Google consent startup.

Keep newer durable approval references for destructive operations, encoded
destructive record/source identifiers and independently bounded audit/sync history.
Do not restore the older unapproved revoke/delete signatures or lose extraction
offset support. HTTP requests still depend on server-owned authorization; client
approval references do not grant authority.

## Extraction page consistency

The extraction client previously checked only the presence of an items array and
matching offset for explicit page requests. It could accept inconsistent limits,
negative/fractional totals, contradictory total fields, impossible record counts,
null records, incorrect continuation metadata or contradictory response headers.
That could make displayed counts and navigation misleading rather than presenting
a failed load.

Explicit extraction pages now use the existing shared history validator. Require
safe integer totals, requested limit/offset, valid record identifiers, bounded
item counts and consistent continuation. Both server total fields must agree;
optional total/limit/offset headers must agree with the validated body. Preserve
the validated offset in the returned view.

This matches the current backend `ExtractionPage` / `ExtractionsForOwnerPage`
response contract, inspected directly. Requests without an explicit offset retain
their legacy array response and missing-header count fallback. Server approval,
ownership and destructive-operation policies are unchanged.

## Verification and preservation

Ten negative HTTP-fixture cases failed against the prior client, then pass after
the correction. The added positive envelope case also verifies returned offset
and operation without optional response headers. All 28 focused client tests
pass in actual Chrome Headless, including the pre-existing correction, manual
queue, approval header, emergency-stop and legacy-array controls.
The strict source-navigation Angular template/TypeScript compiler group also
passes without emitting build files.

These are Angular HTTP fixtures and a code-level backend contract comparison,
not an authenticated deployed browser/API or live account acceptance run. The
large connected-sources page and backend service are not thereby declared fully
reconciled. Original recovery copies and archives remain retained pending the
separate main-preservation and deletion gates.
