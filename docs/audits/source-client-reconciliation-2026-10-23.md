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

## System status follow-up

Read the retained system-status component, template and stylesheet and their
complete current differences. Keep the current visibility-aware adaptive polling,
request cancellation and deadlines, supplemental build identity, and owner-only
Advanced delivery inspection with deliberate, bounded retry confirmation. The
older unbounded subscription and fixed 15-second healthy polling must not return.
Keep the existing responsive theme styles; no stylesheet is replaced in this
follow-up.

Ten new controls failed against the prior implementation: an empty completed
readiness stream left the view loading without scheduling recovery, malformed
reports replaced the last valid snapshot, and a failed refresh could still label
the earlier green snapshot as currently ready. A null report also produced an
unhandled callback error. Validate each new report before changing the snapshot
or freshness timestamp, including nonnegative integer counts, check shapes, and
the aggregate status/count contract of the authenticated backend handler. Empty
responses now enter the existing error/recovery path.

Keep the last valid records after failure but label current status as unverified,
using the already registered outline question icon rather than a green success
icon. All 31 focused Chrome Headless component/delivery tests pass, including a
rendered icon check, stale-data explanation, narrow-container layout, and existing
owner permission, disclosure, uncertain retry, cancellation and HTTP-route
fixtures. These are local tests with synthetic reports, not deployed health,
real delivery, or full visual/operator acceptance. Original worktrees remain
retained; semantic review is not a main-preservation or deletion gate.

## Automation editor and retained coverage follow-up

Read the retained automation form component, home-page tests, quick-capture
template and system-status tests, then their complete current differences.
Preserve explicit change detection and the stronger current home-page navigation,
responsive-content and recovery tests. Preserve quick capture's labelled section
instead of restoring a second main landmark inside the application's main shell.
Keep its existing validation, Basic save/error feedback and Advanced disclosure.
Keep all earlier status coverage alongside the current polling, provenance and
unverified-snapshot checks; none of the retained tests is discarded.

Three new editor controls failed before correction: reopening after cancellation
lost the creation defaults; opening a different record could inherit prior
identity, model, image and title state; an opening during an unconfirmed save
could replace the active editor record. Reset the form to creation defaults
before applying the selected record, clear prior image state, derive the title
from the requested mode and ignore openings while saving. The existing
parent-confirmed completion and failed-save retry contract remains unchanged.
All six focused editor tests pass. These are local component fixtures, not real
automation creation or provider/runtime execution. Source copies remain retained
until the separate main-preservation gate is satisfied.
The combined nine-suite frontend run passes all 131 tests with this change set.

## Skills observations follow-up

Read the retained skills component, template, stylesheet and service specification
in full and reviewed their complete current differences. Preserve current
catalog-fingerprint reconciliation after legacy selection replies, invalidation
of previews when the catalog changes, pinned source links in both views,
accessible announcements and visible focus fallbacks. Keep the service's strict
inventory schema, duplicate-ID rejection and reviewed-hash mutation requirement;
do not restore older shortened/hash-placeholder inventory fixtures as valid data.
No template or stylesheet replacement is needed for this review.

Eight new controls failed before correction: empty or stalled inventory/session
reads, empty or stalled guidance reads, empty or stalled selection responses,
an empty legacy audit refresh and reads surviving page destruction. Observe one
response with a ten-second request deadline, route empty completion through the
existing error handling, and cancel observations on destruction. A selection
timeout remains an uncertain outcome requiring a fresh inventory, never an
automatic retry or a claim that the server rejected the mutation.

Destroying the component also invalidates in-progress guidance verification and
suppresses scheduled focus changes. Two additional controls cover detaching a
pending mutation without claiming rejection and ignoring an asynchronous guidance
verification that finishes after destruction. Existing permission, exact text
hash, reapproval, concurrency and disclosure checks remain unchanged.

All 190 tests in the combined twelve-suite Chrome Headless run pass, including
the ten new observation controls and existing skills component/service tests.
Progressive-section, split-theme and static-icon registry checks also pass.
These are local synthetic observations and rendered component fixtures, not a
live owner-account approval or deployed runtime acceptance. The retained skills
component test file and backend catalog are not declared fully reconciled by
this narrower file review. Original worktrees and transcript archives remain
retained pending main-preservation verification.

## LLM policy presentation follow-up

Read the retained LLM policy component, template, stylesheet, component tests
and accessibility tests in full and reviewed all current differences. Preserve
the seven-tier catalog, per-module disclosure, budget/token context, audit/error
distinctions and cancelled/in-progress maintenance warnings. Keep the newer
cloud-catalog versus health-only maintenance wording, explicit change detection,
shared main landmark, responsive status grid and keyboard-focusable budget
information group instead of the old no-op budget button. No stylesheet is
replaced and no provider settings are changed.

Four negative controls failed before correction. Failed accounting was still
formatted as numeric spend/token totals in provider and policy tooltips, and an
invalid or future live-probe timestamp could pass the displayed strict freshness
check. Unavailable accounting now remains explicitly unavailable in budget,
token and accessible-detail text while retaining the configured budget ceiling.
Known durable zero remains zero. The provider token counter also reflects the
unavailable state; the existing unknown-usage meter suppression remains intact.

The displayed strict-readiness classification now treats invalid or future
timestamps as unverified relative to this device clock. A valid probe at exactly
the configured age boundary remains accepted by the display. This does not
change the backend routing/admission policy or initiate a probe.

All 21 focused LLM component/accessibility tests pass, including rendered
provider counters, accessible budget details, tier selection, positive zero and
age-boundary controls. These are local report fixtures, not actual billing,
live provider freshness, full-screen-reader or deployed runtime acceptance.
The separate LLM service and backend implementation still need their own
reconciliation; this review does not declare the whole module operationally
accepted. Original recovery files remain retained until main preservation is
verified.
The combined fourteen-suite frontend run passes all 211 tests; progressive
section, split-theme and icon-registry checks also pass with this change set.

## Shared global stylesheet reconciliation

Read the retained global stylesheet and its complete current diff. Preserve
the newer light/dark purple tokens, dark native heading contrast, descendant
notification-pane matching and notification hit-testing fallback. Do not restore
the older direct-child-only overlay selector: a nested notification container
must not prevent an underlying operational action from receiving input.
Keep the explicit notification close control interactive and unrelated dialog
panes unaffected. Diagnostics modal surfaces continue to use the active theme.
No production stylesheet replacement is needed.

Seven Chrome Headless regressions exercise computed theme/heading/modal styles,
actual browser element hit-testing through direct and nested notification panes,
the close control and an unrelated interactive overlay. All seven pass against
the application global stylesheet loaded by the Angular test builder. These are
synthetic DOM fixtures, not a whole-app visual review, real notification-service
integration, keyboard/screen-reader acceptance or deployed browser acceptance.
The original stylesheet remains retained pending main-preservation verification.
The combined fifteen-suite reconciliation run passes all 218 tests; the
progressive-section, split-theme and icon-registry contracts also pass.

## Shared shell template reconciliation

Read the full retained shell template, current template and complete difference.
Preserve the single authenticated main landmark, grouped navigation, current-page
labels, responsive navigation dialog/inert workspace, visible runtime safety
state, module-local disclosure/reset controls and theme control. The current
skip link deliberately supersedes the older bare fragment: it uses the current
path and query, while ordinary activation focuses main without navigating away
from the module under a root base URL. Its handler leaves modified/non-primary
clicks alone and refuses to focus an inert workspace.

No template replacement is needed. All 52 existing shell Chrome tests pass,
including rendered skip-link route/focus behavior, mobile dialog isolation,
navigation, view controls, freshness and safety states. Eighteen Node landmark
and delayed-focus controls also pass. This template-specific review does not
claim full reconciliation of the retained shell TypeScript, stylesheet or large
test file. Those files and all originals remain retained. These local fixtures
are not a full deployed navigation, accessibility or visual acceptance pass.

## Shared shell behavior reconciliation

Read the retained shell TypeScript and current implementation in full and
reviewed the complete difference. Preserve current document titles, module mode
subscriptions, route-aware skip links, immediate dialog rendering before focus,
current-group entry focus and focus non-interference. Keep bounded delayed
deep-link restoration, generation checks, visibility recovery, explicit reset
of Basic fragments and cancellation on route changes, destruction or trusted
interaction. Do not restore the older one-shot lazy-content lookup.

Four new negative controls reproduced stale navigation focus: a successful
promise resolving after destruction, a focus frame already queued at destruction,
a rejected mobile navigation resolving after destruction, and an older canceled
navigation overriding a newer completed one. All four failed before correction;
a live successful navigation control already passed.

Navigation focus now has its own generation marker, invalidated on destruction
and subsequent shell-initiated navigation. Both promise settlement and its
animation-frame callback check that marker. Rejection recovery also requires a
connected, non-inert menu target. This only suppresses obsolete focus work; it
does not cancel router operations, change URLs, grant permissions or alter
runtime safety policy. Existing safety freshness and emergency-stop semantics
remain unchanged.

All 57 shell/component lifecycle Chrome controls pass, and the combined
seventeen-suite frontend reconciliation run passes all 275 tests. Eighteen Node
focus/landmark controls and the progressive-section/theme/icon contracts pass.
This is local fixture acceptance, not deployed accessibility or runtime acceptance.
The separate retained shell stylesheet and large shell test source remain
unreconciled by this TypeScript review. Originals remain retained until verified
main preservation and the broader cleanup gate are satisfied.
