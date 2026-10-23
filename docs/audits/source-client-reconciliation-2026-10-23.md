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

## Automation client follow-up

The retained `automations.service.ts` and complete service tests were also read
against their full current differences. Preserve multipart model/runtime options,
PATCH configuration ordering, persisted secure launch keys, fail-closed storage
admission, transport-error recovery and compare-before-remove key cleanup.

Keep the newer shared confirmed-receipt predicate instead of the older rule that
cleared a request key for every response except `indeterminate`. It requires the
exact automation, nonzero UUID event receipt, `ready`/`completed` status and no
approval requirement. The actual control-center consumer uses the same predicate
and warns rather than reporting success for an unconfirmed response.

Ten added tests cover eight unconfirmed receipt shapes, a newer request key that
must survive an older response, and a corrupt stored key that must block HTTP
without silently replacing recovery state. These pass against the current service;
they are additional coverage, not newly reproduced defects or live runtime proof.
Together, the automation and source-client suites pass all 45 Chrome Headless
tests. All 67 existing launch-safety/control-room logic tests also pass locally.
No launch, automation registration, real local storage profile or external runtime
is modified by these fixtures.

## Onboarding and overview follow-up

Reviewed the retained onboarding component, HAI OS component and provider
credential checklist in full, then their complete current differences. Keep safe
internal onboarding return URLs and local completion state. Keep current HAI OS
single-result/deadline behavior, response-shape validation, preserved prior data
on failed refresh, cancellation on replacement/destroy, and registry-bound
internal navigation instead of the older unrestricted link dispatch. Preserve
the newer Trello resumable/incremental and webhook limitations documentation;
the older claim that those cursors are unimplemented must not return.

The onboarding copy previously made unconditional claims about on-device data
and individual approvals. It now discloses that enabled external services may
receive data and describes configured permissions/approval policy, without
changing that policy. The provider checklist distinguishes configuration/probe
sign-off from operational acceptance through an authorized account/resource,
persisted result and verification/recovery chain.

Two new wording controls failed before correction and pass afterward. All 32
onboarding/HAI OS Chrome Headless tests and both existing HAI OS observation
tests pass. These checks do not establish deployment, live provider or full
visual/operator acceptance. No provider, permission or credential is enabled or
changed, and no new source/transcript deletion is authorized by the review.
The combined four-suite browser run passes all 77 tests with the complete frontend
client/onboarding change set applied.
