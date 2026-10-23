# Maintenance health response reconciliation

## Reviewed source

Reviewed the entire retained model-maintenance worker and its complete current
diff. Keep trusted absolute system command paths, independently pinned package
signatures, official stable release/asset identity, fresh provider metadata,
bounded non-replacing installer cache and locked-file hash/publisher validation.
Retain credential filtering, fresh server authorization before apply,
post-install binding to the approved evidence and proxy-free loopback health.
Do not restore an output buffer whose promoted ReadFrom bypasses its limit,
ambiguous Date/Age acceptance or classification of canceled verification as
a confirmed publisher mismatch. Existing guarded installer cancellation work
remains a separate change; no runtime permission is enabled here.

## Integrated correction

The post-install health decoder consumed only the first JSON value from a
limited reader. Consequently a valid healthy prefix could be accepted despite
trailing junk, a second JSON document, more than the permitted response bytes,
or a body read failure after the valid prefix. This is an evidence-validation
gap, not proof that an arbitrary installer can bypass publisher or approval
checks.

Extract the small response boundary into a focused parser. Read at most 4097
bytes, reject read errors and bodies larger than 4096 bytes, then use the
standard whole-document JSON parser. Keep HTTP 200, ok=true and status=live as
required conditions. Valid extra metadata and bounded trailing whitespace stay
compatible. The worker uses the helper before setting its final HealthOK flag;
an invalid health response keeps the existing needs-review failure path.

No network endpoint, update policy, publisher trust, installer approval or
database schema changes. The response body remains closed by the worker.

## Verification and preservation

Five regression cases failed against the original decoding expression: trailing
junk, second document, oversized whitespace, oversized suffix and a read error
after a healthy prefix. Positive controls cover live status, additional metadata,
whitespace and the exact byte limit; negative controls include unhealthy status,
failed HTTP status, null/truncated JSON and missing response/body. A counting
reader verifies the bounded read.

Focused standard-library Go tests are run locally without compiling the full
backend dependency graph. Full package/build validation remains a CI gate.
These synthetic HTTP-body tests are not a real Companion installation, external
provider check or operator acceptance. Source fingerprints stay private;
original worktrees and transcript archives remain intact pending preservation.
