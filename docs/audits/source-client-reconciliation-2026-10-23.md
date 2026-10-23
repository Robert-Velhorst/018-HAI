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

## Environment example reconciliation

Read the complete retained example with assignment values suppressed, and the
complete current delta. Parse both complete declaration sets privately: the
retained example has 578 unique keys and the current example has 609. All 575
shared values remain identical, with 34 newer declarations retained. Neither
example has duplicate keys or unsupported single-line declarations. No installed
environment files were read or modified.

Three removed ngrok declarations belong to the still-unintegrated tunnel path
tracked in `legacy-ngrok-source-reconciliation-2026-10-23.md`. Their absence is
not a retirement decision or proof that those source files can be deleted.
Keep the original until that feature is integrated or explicitly retired.
Preserve the newer database deadlines, owner-bound connectors, signed Trello
webhook configuration, fail-closed queue fallback switches, explicit Docker-host
pins, read-only history context and bounded operational-graph settings.

Correct the reminder-delivery comment that appeared immediately above the
numeric workflow lease setting: `false` applies to reminder delivery, not the
lease. No example assignment value changes. Add a standard-library contract for
the canonical single-line template, duplicate/malformed declarations with
value-free errors, critical opt-in defaults and loopback gateway binding. This
is deliberately not a replacement dotenv parser. Run it in the existing Linux
and native Windows CI jobs, without removing their previous checks.

Seven local environment-template tests and all 122 existing CI-contract tests
pass. These static checks do not establish runtime configuration parity for
every declaration, installed secrets, authenticated providers, SMTP, tunnels,
Docker startup or deployment acceptance. No dependencies were installed, no
runtime was started and no retained original was deleted.

## Workflow component-test reconciliation

Read the complete retained workflow component spec and its complete integration
delta. All 33 original literal test titles remain. Seven original callback bodies
are identical after newline normalization; 26 are adapted to the newer strict
UUID/response contracts, explicit loaded state, outcome criteria, evidence-linked
interruption recovery and cautious server-acknowledgement wording. Preserve
manual intake, independent reminder failures, cancellation, exact selected-run
confirmation, framework provenance, dedicated approval, proposal routing, Basic
actions, persisted Advanced sections, keyboard rows, filters and retry states.
The current integration already adds 14 literal tests covering stale decisions,
worker review, explicit criteria, disclosure focus, loading and checklist writes.

The original interrupted-completion rejection test omits both reconciliation and
evidence, so it cannot independently protect either requirement. Add four cases
covering retry and completion with each requirement missing separately. Verify
the form is otherwise valid, the endpoint is not called, selection stays intact,
saving never starts and the explicit reconciliation error is emitted.

All 51 focused workflow component tests pass in headless Chrome, and all 190
native frontend contract tests pass. This spec uses
service spies and partially shallow templates with `NO_ERRORS_SCHEMA`; it does
not prove a complete real-NG-Zorro action chain, backend authorization, an actual
external outcome, deployed browser acceptance or a full workflow-controller
audit. No production component behavior changes in this reconciliation. The
retained original is not eligible for deletion yet.

## Task Planning controller reconciliation

Read the complete retained controller and its complete current integration delta.
All 95 retained method/getter names remain; 74 method bodies are unchanged and
21 have reviewed adaptations. Preserve the newer plan-first/reviewed-request
fingerprints, persistent uncertain-outcome restrictions, history-dependent run
gates, participant prerequisites, explicit Basic/Advanced view, copied-plan
context reset, MCP inspection and cautious outcome presentation. Source-member
presence is not a claim that every runtime path is correct.

The MCP result lookup could return the last same-tool context item even when
the supplied call did not belong to the current plan. Reproduce this with a
foreign clone and a stale call from another plan. Also reproduce ambiguous
repeated references, malformed call collections, non-string content and a
missing call. Five of the initial six new cases fail before the fix; the
malformed-context positive guard already behaves safely.

Require exactly one reference to the requested call in the current trace, array
collections, matching counts of successful calls and context records, matching
tool names and string content. Two additional cases reject a missing or extra
context item, particularly important when consecutive calls use the same tool.
Keep valid mixed success/failure ordering and bounded text previews. The backend
tool loop appends a context item for each completed trace and carries retry
traces in order; this supports the local pairing invariant, not independent
proof of the upstream result's authenticity or semantic correctness.

After repair, all 1,649 browser tests in the 55-spec reconciliation group and
all 190 native frontend contract tests pass. The initial six regression cases
pass in a separate 64-test focused run; the final combined group also includes
the two context-count cases. No deployed visual or live-provider acceptance is
claimed from these fixture tests.

Lifecycle cancellation remains a known follow-up: this controller still has no
destruction hook for its route/service subscriptions or delayed focus callback.
This review does not certify comprehensive response-schema validation, stale
planning-draft handling or approval-dialog context binding. Keep the original
and the broader task-service sources until those paths and main preservation
are resolved. No execution permission, provider configuration or deployment is
enabled by this change.

## Brain catalog template reconciliation

Read the complete retained template and its integration delta. Preserve Basic
review guidance, Advanced capability planes and roadmap, profile readiness and
activation boundaries, bridge restrictions, explicit aggregate exports, transient
migration planning, source links, metadata maintenance and discovery, skill
provenance, loading/error states and per-module disclosure. The current template
already fixes the OSS Insight open-change callback name; retain that correction.

Add a named capability input rather than relying on its placeholder. Four tests
first compile and render the real BrainCatalogModule with NG-Zorro, its real
services and mocked HTTP; the accessible-name case fails before the fix. A fifth
case covers stale refresh recovery. The final five rendered cases verify a calm
Basic view with no Advanced reads, history loading through the actual disclosure,
visible failed-history messages without replacement scans, keyboard-focusable
named search, bounded pending search interaction, and retained-data refresh retry.
No schema suppression or replacement template is used.

The combined suite passes 1,627 Chrome cases across 53 specification files;
69 native controls and 122 CI contracts pass separately. The expected offline
console message belongs to the existing Home negative-path test. This is local
component rendering and HTTP-boundary evidence, not a deployed-app screenshot,
full-app visual acceptance or live provider authorization. The original template
is retained until preservation on main is verified.

## Windows installer contract reconciliation

Read the complete retained installer contract and the current integration delta.
Keep release-source selection, payload rollback, process identity, A2A opt-in and
credential handling, protected environment writes, worker mutex/promotion and
uninstall boundaries. Current source checks additionally cover readiness,
existing-data recovery, guarded uninstall, validated executable payloads,
digest-pinned builder inputs and the shared least-privilege role provisioner.
The retained contract referenced a cloud-tunnel script absent at the current
path; this pass does not restore that launcher or claim its historical behavior
has been preserved. That dependency remains part of the broader reconciliation.

Add an explicit StaticOnly mode that performs source checks but excludes all
behavioral fixture and payload invocations. Reject combinations with the three
existing mode switches rather than giving an ambiguous partial result. Leave
the default full behavior and existing fixture modes unchanged when StaticOnly
is absent. Document that source assertions are not runtime acceptance.

The companion AST contract accounts for ten non-read command boundaries: each
must be inside a negative StaticOnly guard or after its unconditional top-level
exit. New script/native/mutation commands are not silently classified as safe
read commands. All three conflicting switch combinations are rejected. These
checks and the actual static-only source pass succeed under PowerShell 7 and
Windows PowerShell 5.1. Both are wired into the existing Windows CI step without
replacing its full installer command. The 122 CI contracts pass locally.

No behavioral fixture suite, payload preparation, installer compiler, Docker
operation, scheduled-task mutation, live database action or original deletion
was run for this pass. These source checks do not establish signed installer,
clean-machine, real uninstall or deployment acceptance. The retained contract
and original worktrees remain intact pending main preservation verification.

## Historical tunnel follow-up

Locate and completely read the four retained tunnel files referenced by the old
installer contract. Their bytes remain available outside the selected changed-
file ledger; they are not integrated in the current source tree. The current
Compose document has no ngrok service or cloud-tunnel profile. Record source
hashes, incomplete ownership checks, unbound Compose project arguments, shell
all-zero rate-limit handling and the mismatched recovery fixture in the
[dedicated review](legacy-ngrok-source-reconciliation-2026-10-23.md).

Correct the installer overview so it does not imply a currently available
cloud-tunnel feature. Keep the old sources intact and their integration gate
open; neither source location nor this audit makes them safe to delete. No
legacy launcher, fixture, Docker operation or public endpoint was run.

## Control Center template reconciliation

Read the complete retained Control Center template, the differences between both
retained variants, and the current integration delta. Keep the later variant's
accessible action names and descriptions, noninteractive help marker and hidden
hover duplication. Preserve the action catalogue, approval and workflow detail,
blockers, priorities, activity, memory, runtime diagnostics, launch history,
OpenClaw evidence components and inspector close/continue controls. Current
integration adds honest authentication/retry and unavailable states, bound
open-loop navigation, per-item approval disabling, memory errors and archive
pending state; this pass does not claim a full controller review.

An actual template defect remained: after an earlier empty memory result, the
empty-state message stayed visible during a new read and alongside its failure
warning. Six new tests render the real ControlCenterModule and NG-Zorro through
real services with mocked HTTP, without schema suppression. Two cases fail
before repair. Show a polite pending status and permit the empty message only
after a completed successful read with no current error. Retain previously
loaded records on failure and the rendered retry path.

All six cases pass in the combined 1,633-case Chrome run over 54 specification
files. The 69 native controls and 122 CI contracts pass. Tests cover Basic lazy
loading, confirmed empty data, pending and failed refresh, successful retry and
retained records. The existing Home offline-path console message is expected.
This is component/HTTP-boundary evidence, not live memory-provider, database,
signed-session or deployed visual acceptance. Both originals remain retained
until their work is verified preserved on main.

## Frontend lockfile reconciliation

Parse and compare all retained/current lock metadata rather than restoring old
versions. The current graph accounts for every historical record as unchanged,
changed or removed, retains all direct dependency names and adds current build
requirements. Add four manifest/registry/integrity/install-hook contract tests
to the normal frontend command. The full native run exposes one obsolete
selection-history source assertion; repair it without restoring hidden stale
rows. All 190 native cases pass afterward.

The logical dependency tree passes npm's lockfile-only validation. Separate live
registry audits report zero production-only findings but two high and one low
full-toolchain findings. Keep dependency remediation open pending patch approval;
do not downgrade Angular automatically or claim npm 12 installation acceptance
from the available npm 11 audit CLI. Details and primary advisory links are in
the [dependency review](frontend-dependency-reconciliation-2026-10-23.md).
No dependency version, lockfile bytes, installed dependency or original source
was changed or deleted by the metadata review; the frontend test command now
includes the new contract tests.

## Command Dashboard template reconciliation

Read the complete retained template and its current delta. Preserve snapshot
freshness and unavailable states, assistant actions, pursuit queues and brief,
runtime readiness, OpenClaw ecosystem/skills/upload/path controls, owner-approved
rollback, memory search, source links, corrections, decisions, projects, archive
controls, command history and runtime-surface detail. Retain the current removal
of the nested main landmark and existing explicit rollback control. This is a
template review, not a complete controller or execution-path audit.

The retained/current template placed the empty/omitted open-loop messages inside
the Contradictions card instead of Open Loops. Four new cases render the real
CommandDashboardModule, NG-Zorro and HTTP services with mocked responses. Three
cases fail before repair. Move those messages into Open Loops, retain the
owner-scoped omission distinction, and give contradictions a separate
snapshot-scoped empty state. Populated queues retain their own rows and no
misplaced empty claim. The tests deliberately make the secondary pursuit reads
unavailable; they do not claim a healthy live pursuit integration.

All four cases pass within 1,637 Chrome cases over 55 specification files;
190 native tests and 122 CI contracts pass. The expected existing Home offline
console case remains a negative fixture. No runtime, archive, approval, provider
or package mutation was performed. These checks do not prove real-account,
database, signed-session, deployment or full-app visual acceptance. The retained
template remains intact pending verified preservation on main.
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

## Shared shell stylesheet reconciliation

Read the retained shell stylesheet completely and reviewed its full current
difference. Preserve the newer equivalent selector consolidation, scoped source/
memory subtitle selector, bounded safety text, stable icon sizing, mobile utility
menu and compact status width. Keep explicit focus outlines, forced-color rules,
reduced-motion transition suppression, module depth visibility and themed legacy
surfaces. Do not replace these with the older larger stylesheet or restore the
broad header-span rule that also restyled button labels.

Six new Chrome checks use isolated iframes containing a clone of the rendered
shell and the actual compiled stylesheet rules. Four assert real iframe widths
of 375, 768, 1024 and 1440 pixels, document/header containment and non-overlapping
visible toolbar controls. Two cover unchanged subtitle-versus-button styling
and a focused navigation link's outline. No production CSS change is needed.
The iframe clones are non-interactive layout fixtures: these checks do not
establish every module's responsive behavior, a visual screenshot review,
production theme-bundle loading or deployed accessibility acceptance.

The combined eighteen-suite frontend reconciliation run passes all 281 Chrome
tests. Progressive-section, split-theme and icon-registry contracts also pass.
This completes the retained stylesheet review only, not the separate large
shell test source. Originals remain retained until preservation and cleanup
requirements have been demonstrated for their complete scope.

## Retained shell test reconciliation

Read all 1,441 lines of the retained shell test file and its complete current
diff. Preserve tests for grouped navigation, independent module preferences,
runtime-status uncertainty and emergency stops, accessible controls, responsive
focus transfer, inert mobile navigation, reset persistence and nested disclosures.
Keep the newer rendered visibility/dialog timing, current-group initial focus,
route-preserving skip-link, canceled-navigation recovery, Basic-fragment reset,
delayed-content focus and user-interaction cancellation coverage.

The changed older expectations are intentional: opening navigation now focuses
the active destination rather than the brand, and activating the skip link must
not change the current route/hash. The nested native-detail fixture now includes
its real summary/focus target. Do not restore the older weaker fixture.

Some helper-created shells were not destroyed by their individual tests and
could leave safety refresh/retry timers scheduled during a long suite. Register
helper instances for central afterEach teardown, including cases that exit early.
Existing explicit destruction remains safe; no production behavior changes.
The complete eighteen-suite reconciliation run still passes all 281 Chrome
tests. These fixtures are not a claim of full deployed acceptance. Original
worktree copies and unrelated archive content remain retained.

## Knowledge claim review reconciliation

Read the retained claim component, template and test file completely and
reviewed all current differences. Preserve immutable successor corrections,
exact source/digest detail, per-module disclosure and the distinction between
human approval and external source verification. Keep current authenticated
subject/permission checks, authority revocation on denial, draft preservation,
superseded request cancellation, destruction cleanup and effective-date validation.
The backend correction service/handler and frontend service/model were inspected
to check the successor receipt contract; no backend code changes are claimed.

Ten new negative controls failed before repair: empty/stalled queue and session
reads, empty/stalled lifecycle reads, and empty/stalled/offline/wrong-workspace
correction results. Observe one response with a ten-second deadline and route
empty completion through existing failure handling. A correction receipt must
identify the new human-approved successor for the captured owner/workspace,
subject/predicate/value and superseded claim before success is displayed.

Non-authority errors, empty completion, timeout and invalid receipts remain
unknown outcomes, not proof that nothing was stored. Require a fresh claim read
before another correction, remove the old inspector selection for fresh review
and clear confirmation. A failed refresh keeps the gate closed. No automatic
mutation retry is introduced, and this is not durable exactly-once execution
across browser reloads or concurrent clients. Permission denials still revoke
authority without losing the draft. Workspace changes and overlapping loads/
inspections are blocked while the correction request is pending.

An eleventh rendered negative control found an incomplete bounded queue reporting
no claim exceptions or no records. Its Basic summary now warns that review is
incomplete instead of displaying an all-clear state. A matching-successor positive
control and failed-reconciliation control also pass.

All 19 claim component checks pass within the combined nineteen-suite,
300-test Chrome run. Progressive-section/theme/icon contracts pass. These are
local synthetic API and rendered UI fixtures, not real owner-account corrections,
database concurrency, source truth, durable delivery or deployed acceptance.
Original recovery copies remain retained pending main-preservation verification.

## Home and login stylesheet reconciliation

Read the complete retained home and login stylesheets and their full current
differences. Preserve current shell-owned navigation, wrapping toolbar, bounded
automation grid, long-title wrapping and narrow management layout. Do not restore
the obsolete fixed header or duplicate navigation drawer. Keep the login's
current flex layout, width constraints, service-retry focus treatment and
capability explanations. The login production stylesheet remains unchanged.

A duplicate home password-toggle declaration overrode the visible focus outline
with a transparent one. A scoped, browser-computed focus regression failed before
repair. Remove only the duplicate override, preserving the earlier token-based
hover and visible keyboard-focus styles. The test deliberately isolates the
component stylesheet; it is not a full profile-dialog interaction test.

Add actual-width iframe checks at 375, 768, 1024 and 1440 pixels for login,
operator registration and reset confirmation in both theme modes. The initial
lightweight Karma run omitted the public NG-Zorro box-model reset and reported
four fixture failures. Loading the actual public theme resolves those failures
without a production login change. The isolated fixture now explicitly mirrors
that reset for default lightweight runs rather than misclassifying the missing
test theme as a product defect.

The two-suite run with the real public theme passes all 65 checks. After the
fixture correction, the combined twenty-suite reconciliation run passes all
355 checks, including existing auth and rendered form behavior. Section,
split-theme and icon contracts pass. Cloned iframe content is non-interactive;
these results do not establish real sign-in, email delivery, all-page visual
acceptance, production deployment or full WCAG conformance. No source recovery
copy or transcript archive is deleted by this review or PR publication.

## Model intelligence observation reconciliation

Read the complete retained component and its complete current difference.
Preserve completion-first calibration, the distinction between declared and
attested inference/billing claims, token-usage provenance and lazy Advanced
profile/runtime loading. Keep current eager change detection compatibility.

Eight new controls failed before repair: empty and stalled reads for the
overview, profiles and runtime bundle; raw error disclosure; and a late overview
after destruction. Non-mutating reads now observe one response, reject empty
completion and have a six-second deadline. Finalization clears loading flags,
while a failed refresh keeps prior data alongside a visible error. Runtime
bundle members are bounded individually so one empty or stalled response cannot
silently strand the whole section.

Error previews now use the relevant fixed explanation and a bounded HTTP status,
never raw response bodies, exception messages or request URLs. Update the old
test that expected a private marker in Advanced: sensitive error content must
not become visible merely by opening a deeper view. Existing retry, provenance
and rendered disclosure tests remain enabled.

Destroying the component stops observation subscriptions, including benchmark
and hardware-result notifications, and prevents later dispatch. Pending section
navigation cannot scroll after destruction; rejected navigation has a safe
warning instead of an unhandled rejection. This does not assert that canceling
a browser subscription undoes an already dispatched server operation.

Four additional controls cover first-response completion and retained data,
late benchmark/hardware receipts, and navigation rejection. The final combined
twenty-two-suite Chrome run passes all 375 tests. Section, split-theme and icon
contracts pass. Tests do not call an actual model provider or detect host
hardware. Full runtime response-schema validation, mutation deadlines and
durable benchmark reconciliation are not established by this read-boundary
repair and remain separate work. Original copies remain retained; review and
PR publication are not deletion permission without main preservation.

## Ambient template reconciliation

Read the complete retained Ambient Brain template and its complete current
difference. Preserve current mutation conflict/uncertain-outcome guards, the
single shell-owned main landmark, percentage confidence, dated telemetry and
policy configuration disclosure. A configuration snapshot is not authorization;
deterministic guard checks are not evidence of external-provider correctness.
The retained component logic and separate test source are not declared fully
reconciled by this template-only review.

Two new rendered controls reproduced an enabled but ineffective Tune priorities
button when there were no enabled ranking targets. Disable that action and show
the empty-priority state, while preserving the individual priority editors below
for enabling targets. A positive control opens the editor for an enabled target;
all three controls assert that merely opening it does not save a profile.
The focused Ambient Brain suite passes all 12 checks in Chrome Headless;
the two empty/disabled-target controls failed before the template correction.

Original recovery copies remain retained. Local fixture checks do not establish
live scheduler, model-provider, persisted profile or deployed visual acceptance.

## Ambient component and retained test reconciliation

The separate retained component and test source have now also been read in
full, including their complete current differences. Preserve current teardown,
first-response deadlines, validated telemetry ratios and guard case totals,
scan identity/count/time validation, conflict guards, source-bound proposal
receipts, profile draft preservation and shared safe external-link handling.
Keep all nine retained test scenarios with their stronger current expectations.
In particular, do not restore incomplete scan fixtures, raw error-body display
or a success notification that implies a proposal is executed work.

Seven additional controls reproduced synchronous service-construction exceptions
escaping before subscription and leaving operation flags set: overview,
telemetry, guard suite, scan, profile update and both proposal decisions. Wrap
the existing factories in RxJS defer so these failures reach existing static
error handling, refresh gates and finalizers. No retry, new endpoint, policy
change or server-side execution authority is introduced.

All 19 focused Chrome Headless checks pass after correction, including all
seven controls that failed beforehand. Profile drafts remain available;
uncertain decisions still require refresh. These are service-factory fault
injections, not proof of a reproduced failure against a live provider. The
profile error path still permits deliberate review/retry of its idempotent
field update; cross-client durability and complete nested payload validation
are not established by this review. Canceling a subscription is not server
rollback. Originals and archives remain retained pending verified main
preservation and the wider cleanup requirements.

## Retained Home component and template

Read the complete retained Home component and template and both complete current
differences. The current component preserves registration, editing, deliberate
deletion confirmation, ordering failure recovery, profile controls and empty/
loading/error states. Preserve shell-owned navigation instead of restoring the
older duplicate navigation drawer and hardcoded menu. Account actions remain
in the page header; automation links now have accessible names and long titles
use the wrapping title template. No older implementation needs to be copied
over the current files.

The existing rendered Home checks cover page/account action dispatch, Basic and
Advanced management disclosure, long-content containment, load errors, failed
submission and ordering recovery. These do not establish live profile changes,
deletion durability, runtime launch or a complete concurrency/security audit.
The retained component's broader unbounded request lifecycle and mutation receipt
limitations are not claimed repaired by this source-preservation review.

## HAI OS template and stylesheet reconciliation

Read both retained files in full and both complete current differences. Preserve
the current explicit unavailable pursuit state, identity-checked record links,
registry-bound plane navigation, concise status labels and calm queue lists.
Keep shared theme tokens, visible focus, narrow-layout wrapping and separate
Advanced source/readiness/governance sections. Do not restore the older
clickable queue summaries as if they opened distinct filtered queues.

Three rendered controls reproduced nonfunctional pagination: the reference,
operating-plane and readiness tables iterated the entire backend array even
after selecting a different page. Render each table's existing NG-Zorro page
data instead. The checks click the real next-page control and verify the first
page limit, second-page records and absence of another overview request. All
records remain loaded; this changes client-side presentation, not API paging.

The combined 23-suite Chrome Headless run passes all 397 tests with the complete
frontend reconciliation changes applied. The three new pagination cases failed
before repair. These are rendered component fixtures, not deployed visual
acceptance, a fresh production build, live backend/provider acceptance or full
accessibility conformance. No retained source or transcript is deleted.

## Background operation logic and retained tests

Read the complete retained component and its test source and both complete
current differences. Preserve owner/source revision previews, explicit fresh
digest confirmation, retained uncertain execution receipts, per-operation retry
fences, interrupted work visibility, inline failure recovery and the distinction
between approval, execution, worker verification and recorded outcomes. Keep
all retained scenarios with newer uncertainty and diagnostic expectations;
do not restore claims that a failed request proves nothing changed.

Four new controls reproduced gaps in the source-approval observation chain:
the approval write could remain pending beyond the preview timeout, repeated
fresh-preview emissions could dispatch repeated approvals, empty previews
silently ended the review, and a retained review could dispatch after component
destruction. Observe the first result of each preview/write, reject empty
completion and apply a separate deadline to the write. Construct requests inside
the observable error boundary. Block new approval/review dispatch after teardown.
No automatic retry, new permission or backend authorization change is introduced.

All 65 focused component/UI checks pass after repair; all four new controls
failed beforehand. The combined 25-suite Chrome Headless run passes all 462
checks. These use synthetic services and rendered fixtures, not a live owner
approval or proof of durable exactly-once behavior across reloads/clients.
Unsubscribing or timing out does not undo a server write. Complete approval
response-schema validation, the legacy approval observation path, general
refresh observation/schema handling and full diagnostic redaction are not
established by this targeted correction. The separate retained template and
stylesheet remain outside this two-file review. Originals remain retained.

## Current main synchronization and retained gateway tests

Merged current main normally without conflicts. The existing frontend source
and tests are preserved; the shared credential checklist also retains main's
Trello owner/member bindings and live-acceptance limitations. Main's already
integrated backend, authentication, installer and gateway changes are retained,
not replaced with older branch snapshots. Fresh exact-head CI is still required.

Read the complete retained gateway contract test and its complete current
difference. Preserve the authenticated routing, upload, identity forwarding,
loopback bridge and local-preview contracts. Keep newer frontend/host listener
ports, dual rotated-cookie checks, suppressed internal headers, bounded
readiness probes, restrictive generated-config permissions and exact Git-ignore
verification. The current test deliberately reads the public template instead
of requiring generated credential-bearing nginx.conf. No older test replacement
is needed.

All 462 frontend checks in 25 suites pass after synchronization, as do all 22
gateway source/Git contracts. Karma reported delayed Chrome termination but the
runner subsequently exited successfully; no frontend process was force-terminated.
The separate synthetic Docker build-context test stalled in a Buildx descendant
beyond its subprocess deadline. Its identified test processes were stopped;
that test is not a local pass. Removal of its temporary synthetic fixture was
blocked by the local security policy and was not retried through another route.
These checks do not establish a deployed gateway, live cookie rotation, external
OAuth, account access, or runtime execution. The original recovery files and
transcripts remain retained.

## Background operation template and stylesheet review

Read the complete retained template and stylesheet and the current versions.
Preserve the newer calm primary action, per-module Advanced ledger, enabled-feed
filtering, paged table rows, narrow-layout wrapping, shared theme tokens and
visible keyboard focus. Keep inline recovery, explicit uncertain outcomes,
source-derived approval previews and revision-bound confirmation. Restoring the
older presentation would lose these controls; no older stylesheet needs copying.

Two rendered regression cases reproduced a misleading green success icon for
reports containing feed errors or failed operations. A failure count without an
error list also produced the heading "Pass complete". Derive report issue state
from both the error list and failed-operation count. Show the existing warning
surface and a warning icon, retain the issue heading and show the failed count.
This does not change execution, permissions, approval or retry behavior. A later
request failure takes precedence over a previous report's warning.

All 68 focused component/UI tests pass, including the two regression cases,
the clean-success control and the later permission-failure control. The combined
25-suite frontend run passes all 465 checks. These are
Chrome-rendered component fixtures with synthetic services, not deployed visual
acceptance or live feed execution. Full report-response schema validation,
diagnostic redaction and end-to-end provider acceptance remain separate work.
Retained source copies and transcript archives are not deleted by this review.

## Memory component, tests and stylesheet reconciliation

Read the retained component and tests in full and compare the current versions.
Keep all older disclosure, selection, unavailable-state and obsolete-request
scenarios with the current server-page query contract. Preserve actual server
pagination and applied project/filter scope rather than restoring a twelve-row
client cap. Keep the manual-versus-review-managed write boundary, drafts on
failure, record mutation overlap guards, retrieval invalidation and partial
reindex warnings. Read the retained stylesheet and its entire current delta;
keep newer filter, page range, form fieldset, wrapping and header control rules.
The separate retained Memory HTML file is not included in this three-file review.

Ten new negative controls reproduced synchronous request-construction exceptions
escaping the observable error boundary: query, retrieve, create, update, archive,
restore, delete, export, semantic reindex and conversation import. Construct each
request inside RxJS defer so existing error handlers clear its busy state,
preserve drafts and report failure without claiming success. Record mutations
now acquire their existing pending marker before request construction. No new
endpoint, permission, automatic retry or backend mutation behavior is added.

All 45 focused Memory checks pass after correction; all ten new exception cases
failed before it. The combined 26-suite frontend run passes all 510 checks.
This includes rendered HTTP action chains using the real
frontend service with a synthetic HTTP backend, not a live persistence/provider
test. Empty/multiple-emission handling, full response-schema validation,
post-destruction direct dispatch prevention, full diagnostic redaction and
durable mutation reconciliation are not established by this targeted repair.
No source copy or archive is eligible for deletion merely from these checks.

## Retained Memory template and explicit retry recovery

Read the entire retained template and its complete current difference. Keep the
Basic retrieved-record detail, source provenance, review-managed edit gates,
server-page filters/ranges, loading and unavailable distinctions, export scope,
pending-record controls and nested content/danger disclosures. Do not restore
the older generic editing shortcut or show an unloaded inventory as zero.

A rendered HTTP regression reproduced a dead recovery button after switching
from a failed Advanced library load to Basic. The visible Retry called refresh,
which correctly refused a Basic inventory read. Offer "Open library to retry"
in Basic and explicitly open this module's Advanced library before refreshing.
The existing Basic read restriction is preserved; the transition requires the
user's recovery action. The test verifies no automatic Basic query, exactly one
recovery request and the recovered record rendered in the opened library.
All 46 focused Memory checks pass; the new recovery case failed beforehand.
The combined 26-suite frontend run passes all 511 checks.
This is synthetic HTTP/component evidence, not deployed browser acceptance.

## Retained installer entry point and build script

Read the complete retained Inno entry point and PowerShell build script and
their complete current differences. Both current files already match main
exactly. Keep explicit production signing with pinned certificate and timestamp
verification, distinct developer previews, signed-uninstaller evidence,
source-file path validation, build provenance and atomic output promotion.
Keep visible launch errors, maintenance synchronization, fail-closed uninstall
ownership checks and scheduled-task recovery after a refused uninstall. No
older installer code needs porting over these controls.

Both retained and current build scripts parse without PowerShell syntax errors.
This review did not compile or install an EXE, invoke signing tools, access
certificates, register tasks, start Docker, or prove installed lifecycle behavior.
Source comparison and parsing are not release or runtime acceptance. The
retained recovery copies remain untouched pending the broader preservation gate.

## Runtime Lab reconciliation and observation limits

Read the complete retained runtime component, tests and template and their
complete current differences. Preserve on-demand parity/capability loading,
the DeepSeek hard block, redacted legacy diagnostics, explicit discovery versus
execution evidence, retained uncertain self-test receipts and repeat-test
fences. Do not restore success claims based on process or status alone.

Ten new negative controls reproduced escaping request-factory exceptions,
unbounded status reads and post-teardown callbacks/dispatch. Construct all
eight request paths inside defer; bound the five status reads at six seconds;
observe one result and reject empty read/handshake results. Cancel observation
and reject direct runtime actions after component teardown. Preserve uncertain
self-test outcomes rather than authorizing retry when a timeout or empty result
cannot establish what happened. Unsubscribing does not undo a server operation.
Additional controls cover an empty MCP handshake and repeated self-test results.

The current template also contained duplicate discovery-persistence warnings.
A rendered control reproduced both copies and the blanket assertion that all
runtime execution remained blocked. Show one warning that discovery alone does
not authorize execution; retain the actual backend/runtime policy separately.

The final combined run passes all 556 frontend checks in 27 suites, including
45 Runtime Lab checks. The ten new observation failures and duplicate-warning
assertion failed before their corresponding repairs.

These are frontend observation and presentation changes, not new execution
authority, an OS sandbox, durable reconciliation across reloads or live runtime
acceptance. Full response-schema validation, cross-refresh ordering and complete
diagnostic redaction remain separate work.

## Optional runtime profile documentation

Read the complete retained profile guide and its complete current difference.
Compare the active Compose document through a YAML parser and its existing
source contracts. It defines 44 services: ten default definitions, including
three one-shot jobs, and 34 profile-gated services over 25 profiles. Correct the
older nine-service/two-job description and aggregate limits to 3,328 MiB and
5.2 CPU including all setup jobs; these are configured ceilings, not measured
resource consumption or simultaneous peaks.

The documented local-model profile and ollama-local service do not exist in
this Compose file. A new regression assertion rejected the stale profile
commands. Replace them with the separately managed, reviewed provider boundary
and describe the authenticated generation smoke script without running it or
claiming prior local evidence proves a current deployment. Preserve opt-in
helpers, local-network cautions and the hard-disabled host-runtime policy.
All 118 CI source contracts pass; these are not GitHub CI or live Docker tests.
No model, account, container, certificate or retained source was changed by
executing this review's tests, and no recovery original is deleted.

## Plan coordination component and template

Read the complete retained component and template and both complete current
differences. The current implementation already retains their immutable plan
preview, criteria/constraint deduplication, owner bindings, revision/digest
acceptance, append-only replan, inspector and per-module disclosure behavior.
Keep the shared shell's single-main landmark instead of restoring a nested main.

Ten new controls reproduced direct request-construction exceptions, obsolete
list/detail responses replacing newer selections, overlapping preview submission,
unbounded reads and callbacks/dispatch after teardown. Construct requests inside
defer, observe one result, fail empty responses, bound status reads at six seconds
and writes at thirty seconds. Cancel obsolete reads and pending observation on
teardown. Block list/selection changes and duplicate preview submission during
a write; reflect that lock on the visible controls. Keep the exact reviewed
revision/digest request and existing backend permission boundary unchanged.

Do not claim a failed preview/replan proves nothing was created. Preserve the
draft and direct the user to refresh before retrying when completion cannot be
confirmed. This repair does not establish durable idempotency across reloads,
independent write verification, complete response-schema validation, source
provenance validation or full diagnostic redaction. Stopping observation does
not undo a server-side write. The existing HTTP action-chain tests use the real
frontend service with synthetic HTTP responses, not a live plan database.

All 26 focused checks pass after repair; all ten new cases failed beforehand.
The combined frontend run covering 28 selected spec files also exits zero.
Its final output was unavailable, so no combined assertion count is claimed.
No runtime execution permission, provider setting, account or source original
was modified. The recovery worktrees and transcript archives remain retained.

## Grounded answers request and observation boundary

Read the complete retained component and its complete current delta. Preserve
lazy provider/history reads, per-module disclosure, pursuit linking, audit
warnings and the explicit distinction between discovered candidate evidence and
verified claims. The retained implementation is already present apart from the
current Angular change-detection declaration; do not restore older UI plumbing.

Eighteen new negative controls fail before repair: eleven synchronous service
factory exceptions, duplicate answer submission, two negative endpoint probes
reported as success, obsolete detail replacement, silent status reads, empty
history completion and observation/dispatch after teardown. All 32 focused tests
pass after repair. Centralize the repeated deferred, one-response, nonempty,
deadline-bound observation contract in this component. Status/history/detail
reads and endpoint probes use six seconds; answer and retrieval requests use
thirty seconds. Every API entry point refuses dispatch after teardown. Cancel
an older detail read before selecting another detail or starting a new answer.

A negative or missing reachable flag cannot produce a successful health toast.
An answer timeout or transport error does not prove that a verification run or
an explicitly permitted memory update was not saved. Retain the question and
tell the operator to inspect history before retrying. Unsubscribing does not undo
backend work. Existing evidence request fields and backend authorization remain
unchanged; this is not independent verification of provider output.

Complete response-schema validation, binding editable evidence to its original
candidate, durable idempotency across reloads, live source acceptance, deployed
visual review and a full production build remain outside this repair's proof.
Four additional controls cover uncertain answer timeout, empty answer completion,
one-result observation and detail cancellation before a new answer. All 618
tests across 29 selected frontend spec files pass together in ChromeHeadless,
including the resulting 36 grounded-answer tests. These test counts are not
counts of recovered source files or live-provider acceptance evidence.
No original worktree file or transcript archive has been removed.

## Agent teams component and template

Read both complete retained files and their full current deltas. Their advisory
charter creation, governed membership, revision checks, canonical message and
acknowledgment requests, consensus outcomes, Basic attention queue and Advanced
records already exist in the current application. Preserve the current Angular
change-detection declaration and the shared shell's single-main landmark rather
than restoring older plumbing. Advisory teams still grant no execution authority.

Nineteen new tests fail before repair. They cover thirteen request construction
paths, both initial request factories, an obsolete detail response after leaving
and returning to the same team, team switching during a write, empty/silent
attention reads and dispatch/callbacks after teardown. All 25 focused tests pass
after repair. Standardize deferred, nonempty, one-response observation with six-
second read/probe deadlines and thirty-second write deadlines; preserve backend
permissions and exact revision/request fields. Cancel team-scoped observations
when the selected team changes or a write starts. Deduplicate pending sections
and derive their shared busy state from outstanding reads. Registry refresh and
selection are blocked during writes, including their visible controls.

Retry initial session loading when it failed rather than refreshing only the
team list and leaving permission recovery unreachable. An unconfirmed write
retains the draft and directs inspection before retrying; the queue's generic
error no longer claims a decision succeeded. Section failures remain distinct
from a successful empty result. Current-location markup includes team version.

Six additional controls cover permission-read recovery, pending-section
deduplication/concurrent busy state, pre-write read cancellation, uncertain write
timeout, empty write completion and rendered disabled navigation. All 649 tests
in 30 selected frontend spec files pass together, including 31 team tests.
These are synthetic browser component/request tests, not live multi-agent
operation, independent consensus correctness or production acceptance.

Complete response-schema/identity validation, redaction of all server-provided
diagnostics, durable retry idempotency and preservation of edits made to an
in-flight form are not established by these controls. A stopped observation
does not undo server work. No authority ceiling, provider, account, source
original or transcript archive was changed by executing the tests.

## Framework registry style preservation

Read the complete retained stylesheet and its complete current delta. All old
theme-token surfaces, keyboard focus rules, bounded inspector widths, responsive
grid breakpoints, minimum-width protections and reduced-motion rules remain.
The current file adds explicit help-button alignment and bounded, wrapping
preference-history presentation. It already exactly matches the freshly fetched
main snapshot; no obsolete stylesheet needs restoring. Both retained and current
files compile successfully with the installed Sass compiler. This establishes
style source preservation and syntax validity, not rendered viewport, contrast
or accessibility acceptance. The retained source remains intact.

After the exact-head 41-check feed-read PR merged, its three files were verified
against the merge commit and fetched main before deleting only its remote branch.
A normal conflict-free main merge preserves the tested frontend tree unchanged.
The complete accountfeed package passes again locally, uncached, in 2.922 seconds.
That package run uses local fixtures, not live account or provider acceptance.

## Windows OpenClaw maintenance preservation

Read the complete retained maintenance support and launcher test suite, plus
both full current differences. Preserve credential separation and header-safe
literal validation, exact loopback backend binding, publisher-pin validation,
bounded redacted logging, task identity/principal/action/schedule validation,
least-privileged scheduling, overlap control and uninstall ownership checks.
The current support already exactly matches fetched main. Keep its bounded
mutex wait and deferred canceled-uninstall recovery instead of restoring the
older immediate-only lock behavior. The manager restricts that recovery path
to task registration with its immediate maintenance run suppressed.

The current launcher suite retains the old scheduling, quoting, redaction,
rotation, contention, failure and uninstall controls, and adds bounded lock-wait
tests plus a real-launcher malformed-worker rejection fixture. Preserve those
newer checks. This review did not execute that complete suite: its generated
processes, mutex/log fixtures and simulated lifecycle are distinct from pure
configuration/ownership validation and from an installed-product acceptance run.

Add a separately runnable, side-effect-free contract script for the actual
environment parser, worker-environment allowlist and task-ownership predicate.
Its 41 checks include positive controls and rejection of altered credentials,
URLs, port/pin data, task principals, scheduling, actions and working directory.
All 41 pass against retained and current support in both PowerShell 7 and Windows
PowerShell 5.1. The 5.1 run initially exposed a new-test default-parameter path
issue; computing that default in the script body repairs it. No production
maintenance behavior was changed. The existing launcher suite invokes the new
script, which is reached by the Windows CI installer-contract step.

Five current/retained PowerShell files parse without errors and all 118 CI source
contracts pass locally. These checks do not start a worker, register a real task,
read installed credentials, write logs, acquire maintenance locks, contact a
provider or perform an installation/update. They do not prove current upstream
release availability, signing, all synchronization behavior, log-path security,
cancelled-uninstall recovery or deployed lifecycle correctness. Originals remain
intact; publication is not permission to discard the broader recovery scope.

## HAI OS specification preservation

Read the complete retained HAI OS component specification and its full current
difference. Keep current operational wording and routing rather than restoring
older queue shortcuts or assertions that hide a clear status. Preserve Basic
attention limits, module-local disclosure persistence, real empty/unavailable
states, emergency-stop separation, timestamp handling, named scroll regions and
last-known data on refresh failure. Keep newer internal-route allowlisting,
missing-record-ID refusal, unavailable-pursuit handling and rendered pagination
checks for all three Advanced tables. All 26 current cases pass again in Chrome
Headless after integrating the reviewed main changes. These are rendered local
component/router doubles, not deployed navigation, real provider or whole-app
accessibility acceptance. No older test needs to replace the current coverage.

## Historical orchestration metadata

Read the complete retained terminal store, agent registry and model-router state.
These are local development records, not product implementations or authoritative
runtime observations. The terminal store contains a closed session with failed
commands and no returned verification output. Agent entries contain registration,
configuration and counters, not source patches or review results. The router's
retained history contains failed attempts and truncated task descriptions; its
counters and labels cannot establish successful reviews or current model health.

Do not replay those commands, restart registered agents, restore stale routing
priors, import private workspace locations or present old idle/healthy labels as
current HAI state. No unique implementation or completed-work result was found
in these three files. Record the classification and exact source integrity
privately; do not publish raw local identifiers or paths. Their original files
remain intact. The separate policy state, other worktree data and transcript
archives are not covered by this classification or approved for removal.

After the reviewed OAuth, account-bridge and reminder PRs were merged into main,
both main updates integrated without conflicts. Local uncached source and
workflow package tests pass in 3.314 and 2.879 seconds respectively; all 119 CI
source contracts pass. The local database DSN was empty, so dedicated PostgreSQL
cases skip locally; the explicit OAuth database PASS evidence is the separately
verified exact-head hosted job linked in its source-repository audit.

The separately authorized clean source-header PR also passed all 41 exact-head
checks and was squash-merged. All three head/merge/fetched-main file blobs match;
only its remote branch was removed. Its privately retained pre-rewrite history
and all local recovery originals remain intact. That merge changes source API
header binding, not provider permission or installed credentials.
The complete source package passes again locally after this final integration,
uncached, in 3.668 seconds. The database DSN remains empty for that local run.

## Task and runtime-control main integration

PRs #325 and #331 each passed all 41 checks at their reviewed exact heads and
were squash-merged into main. A fresh fetch verified all six task PR files and
all three runtime-control PR files against their head and merge blobs. Only the
two matching remote branches were removed; no local source or archive was
deleted. Their combined main changes merged into this integration branch without
conflicts. The complete task package passes uncached in 12.294 seconds and both
Runtime Control suites pass all 40 cases in Chrome Headless after integration.
The local database DSN was empty: this is not a new PostgreSQL or live runtime
acceptance claim. Existing backend authority and explicit owner review remain
authoritative; frontend observation cancellation is not server rollback proof.

## Historical policy-state classification

Decode the complete retained policy JSON and validate every nested field in all
99 receipts against the observed schema. The file contains legacy default-allow
decisions for ten tool names, request/identity references and input digests.
Rules, budgets, usage and approvals are empty. There are no tool arguments,
source patches or returned execution results in these records. A historical
allowance proves neither successful execution nor current authorization.

Do not replay calls, restore the permissive legacy mode, infer completed agent
work from a receipt or treat an input digest as recoverable source code. Publish
this classification only, with exact raw-file integrity recorded privately.
The raw state remains local and is not main-preserved or deletion eligible.
The structural check does not claim cryptographic receipt authenticity. This
completes one additional selected-file review, not the entire recovery scope.

## Login source and specification reconciliation

Read the complete retained login implementation and component specification,
then every difference against current code. Preserve operator-only registration,
explicit local preview, remember-me bindings, masked confirmation, inline linked
validation and guarded internal return destinations. Keep the current Unicode
code-point minimum and UTF-8 byte maximum, callback-error explanations, recovery
rate-limit handling and the distinction between enabled email recovery and actual
delivery. The older wording that implied a code had been sent must not return.
Retain the newer rendered form, viewport and disabled-provider coverage rather
than replacing it with the smaller historical suite.

Twenty-six new regression cases failed before repair. All seven authentication
request paths now defer setup, take one response, reject empty completion and
unsubscribe when the login component is destroyed. Capabilities retain a
five-second deadline, sign-in/session/registration/preview fifteen seconds, and
the two password-recovery stages forty-five seconds. Strict boolean capability
and session checks reject truthy strings rather than enabling optional flows.
The local busy flags clear on teardown; this is not proof that a dispatched
server write was cancelled or rolled back.

Session retry cannot overlap sign-in, registration or local preview. A cancelled
retry navigation receives an actionable failure message; late navigation
feedback cannot mutate a destroyed login view. No HTTP action is automatically
replayed after uncertainty. This does not change server permissions, bootstrap
roles, OAuth configuration, SMTP settings or installed credentials. Neither
mocked request acknowledgements nor local component tests establish real sign-in,
mail delivery, complete response-schema validation or deployed acceptance.

The initial 26 regression cases pass after repair. Additional positive/lifecycle
controls and the combined existing frontend suites are verified separately
below. Both retained originals remain intact, with exact hashes recorded
privately; no archive or whole-worktree deletion follows from this scoped review.

The combined selection now includes 33 frontend specification files: all 719
cases pass in Chrome Headless, including all 30 new login observation cases and
both Runtime Control suites. The logged `offline` error is the existing Home
negative-path fixture, not a live service observation. This is not the entire
frontend suite, a production build or a deployed browser acceptance run.

## Control Center and Connected Sources stylesheet reconciliation

Read both retained module stylesheets completely and all current differences.
Preserve shared theme tokens, visible focus, responsive grid breakpoints,
wrapping provenance and failure messages, reduced-motion handling, and status
colors. Keep current selector consolidation, corrected heading colors,
labelled approval-input layout, connector-readiness feedback, progress state and
the header-button span isolation. Do not restore removed Basic action styles
whose old class is no longer used by the current template, or make static metric
containers look like actionable buttons again.

Two layout defects remained in both source generations. Connected Sources put
outer row actions in column two despite defining only one explicit column;
that forced an implicit second track beside the source selector. Keep badges in
column two of their nested selector grid, but place the sibling actions in
column one of the outer row. Control Center declared a mobile one-column metric
inspector before a later equal-specificity two-column rule, which overrode the
mobile intent. Move that breakpoint after its base declaration.

Eight Chrome layout regressions use the actual component stylesheets with
representative static markup in isolated 375, 768, 1024 and 1440 pixel frames.
Before repair, all four source-action layouts and the 375-pixel inspector case
fail. All eight pass after repair, checking explicit column count, vertical
action placement, control containment and the inspector breakpoint. Both
retained and current variants compile with the installed Sass implementation.
The current compiled CSS is 23,953 and 23,794 bytes respectively; no stylesheet
bundle or generated image was added to the product.

These focused style fixtures do not establish full live-page layout, a visual
design sign-off, complete keyboard/contrast acceptance or real connector
execution. Original sources remain intact; their review is distinct from
main-preservation and whole-worktree deletion eligibility.

After adding both full module component suites and the new layout fixture to the
combined selection, all 888 cases across 36 specification files pass in Chrome
Headless. The expected Home offline-fixture console output remains unchanged.
This broader local check still does not substitute for deployed visual review.

## CI workflow reconciliation and exact PASS evidence

Read the complete retained workflow and the complete current workflow in
contiguous ranges. Keep current read-only repository permissions, pinned action
references, non-persisted checkout credentials, hash-locked runner matrix,
Windows containment/recovery/signing guards, dedicated PostgreSQL/Redis suites,
owner-scoped disposable browser-stack lifecycle, redacted evidence collection,
resource checks and full frontend test discovery. Do not restore the older
fixed browser project, unguarded Compose cleanup, smaller database-test set or
unrestricted installer artifact upload.

The current browser matrix deliberately selects its documented first scope;
broader uncertainty fixtures remain incomplete. A provider fixture, synthetic
credential or capability check is not live external-account acceptance. The
workflow's presence is not proof that every hosted job ran or passed. Keep these
limitations separate from local source-contract success and main preservation.

Twenty-one migration-job PASS assertions matched only a test-name prefix. A
longer test name could satisfy those checks if the required case were removed
from a broader test selection. Require the literal space/elapsed-time opening
parenthesis after the exact name, consistently with newer assertions already
in this job. Do not weaken test selectors, required environment flags, failure
handling, skip checks or the secret scanner.

Two new contract tests fail before repair: the structural test identifies all
21 prefix-only patterns, and executing the actual grep expression confirms a
wrong-name false positive. After repair, all 121 CI contract tests pass. The
shell regression exercises all 51 migration-job `grep -q` PASS patterns with 204
synthetic exact, longer-name, SKIP and FAIL probes, using Git Bash on Windows.
It neither opens a database nor runs a provider or cleanup operation.

The current YAML parses into 17 jobs (36 expanded matrix jobs); all 62 Linux Bash
run blocks pass Bash syntax validation after substituting inert text for GitHub
expressions. Heredoc contents are not independently proven by that shell parse.
The full CI-contract test source remains a separate retained-file review; this
change does not claim its complete historical reconciliation. Originals and
archives remain intact. Hosted CI and post-merge preservation are still required
before any associated cleanup eligibility can be established.

## Capability catalog and task-planning specification reconciliation

Read the complete retained catalog and Task Blueprint specifications, then the
complete current catalog delta and Task Blueprint specification in contiguous
ranges. Preserve deferred Advanced profile/skill/history loading, explicit manual
adapter/discovery reviews, held-candidate refusal, metadata-only upstream checks,
separate probe/export actions and the distinction between a catalog profile and
real activation. Keep the newer persisted-history disclosure regression.

For task planning, preserve module-local preferences, query-scoped pursuit
context, plan-only shortcuts, explicit mandates without inferred approval,
structured validation and its absent/partial/not-applicable states, confirmation
before uncertain retries and recovery preview before apply. Retain the newer
cold-history replay restrictions, exact project/pursuit scoping, malformed
receipt refusal, bounded/redacted partial-effect inspection, backend retry-policy
refusal and confirmation-time rechecks. A frontend advisory model route or
selected framework is not proof of execution readiness or additional authority.

The current Task Blueprint specification contained five exact duplicate MCP
scenario bodies. An AST comparison verifies identical copies and, after removing
only the second copies, preserves every one of its 58 unique callback bodies
(line-ending normalized source comparison). Tool argument/result labels, rejected
call alignment, wrong-tool refusal and bounded result truncation remain covered.
Add a unique-title/discoverability guard to the existing Node control-room test
file already executed by CI. It fails before deduplication and passes after;
all 66 tests in that Node file pass. No application behavior or test assertion
was removed or weakened to reduce a failure count.

Both current Angular specifications pass all 84 cases after deduplication. These
are service-double/component-logic tests, not live OSS Insight availability,
package installation, real LLM evaluation, external source truth or runtime
execution acceptance. The two production component sources remain separate
retained-file reviews. Original specifications remain intact; publishing this
decision does not authorize transcript or whole-worktree deletion.

After adding both reviewed specifications to the combined selection, all 972
cases across 38 frontend specification files pass in Chrome Headless. The five
removed duplicate callbacks are not counted as unique coverage. This run keeps
the earlier 888-case selection and adds the 84 catalog/task cases; it is still a
bounded selection rather than a full frontend or deployed acceptance claim.

## Capability catalog implementation reconciliation

Read the complete retained component implementation and its full current delta.
Keep current eager change detection and the corrected collection-section event
handler, deferred profile/skill/history reads, explicit probe/export commands,
transient migration mapping and manual pursuit review instead of installation.
Retain candidate/held distinctions, capped discovery display, scoped metadata
revalidation and review history; none establishes live upstream capability.

Five new regression cases fail before repair. The component announced Langfuse
readiness even for false health/readiness flags, accepted malformed/undated
probe evidence, announced export on incomplete trace acknowledgements and threw
on a valid JSON `null` migration sample. Require dated boolean probe evidence;
a well-shaped negative result remains inspectable but is not called ready.
Require nonzero hexadecimal trace/span identities, a recorded timestamp and a
nonempty scope before showing an export receipt. Langfuse and OpenLIT share
that same narrow acknowledgement check.

A transport error cannot prove that an export was rejected. Both export error
paths now state that the snapshot may have been accepted and direct the
operator to inspect trace records before another export. No automatic retry is
introduced. Reject null, primitive/array roots and blank workload IDs before
dispatching a transient migration request. Existing valid Langfuse proof stays
covered; added controls retain valid OpenLIT acknowledgements, one-shot lost
export response handling and valid transient migration mapping.

The initial 31 catalog cases pass after repair. The later combined run includes
three additional positive/transport controls. These are component/service-double
observations, not actual provider receipts or proof of remote acceptance.
Known follow-up remains: this catalog does not yet uniformly bound/cancel every
request or fully validate every nested response; obsolete profile observations
also need a separate lifecycle review. This source reconciliation must not be
presented as complete catalog safety hardening. The retained source is intact
and no cleanup eligibility follows until the broader preservation gates pass.

The final combined selection passes all 980 cases across the same 38 frontend
specification files, including all 34 catalog cases. No live telemetry endpoint,
external account, package installation or deployed dashboard was exercised.

## Catalog request lifecycle follow-up

Bound all 32 catalog request observations: 14 profile status reads have a
six-second deadline, public metadata and ordinary reads have a fifteen-second
deadline, and migration planning, aggregate exports, pursuit creation and
maintenance have a thirty-second deadline. Deferred factories contain synchronous
setup failures. Empty completion follows the existing error path, only the first
response is accepted, and finalizers release loading flags on every terminal or
unsubscribe path. Destruction cancels observations and blocks further dispatch.

Profile changes cancel profile status, probe and selected upstream metadata
observations, including selection away and back to the same profile. Deliberate
status retries clear the previous unavailable marker. Selection cannot clear a
pending export or migration view. Writes are not retried automatically; timeout
and unsubscribe do not establish rollback. Pursuit creation failures now explain
that a manual review may already exist and direct the operator to inspect it
before another attempt. Existing export acknowledgement and approval boundaries
are retained. No runtime is installed or activated by these changes.

The initial 98 lifecycle regression cases failed before the request-wrapper
repair. Additional controls cover post-destruction dispatch, all-path silent
deadlines, one-response profile reads and preservation of pending export/migration
selection. These are local component observations, not provider acceptance or a
deployed-page visual review. Full nested response validation remains separate.
The original worktree files and transcript archives remain retained; this patch
alone does not make them deletion-eligible.

All 179 lifecycle cases pass. The combined selection passes all 1,159 cases
across 39 frontend specification files, retaining the existing 34 catalog cases.
The 66 native control-room logic checks also pass. One existing failure-message
expectation was updated to require the honest uncertain-outcome wording and
assert a single pursuit dispatch, rather than advising an immediate blind retry.

## Retained browser-acceptance build logs

Read both retained stdout/stderr logs in full. They contain build progress, not
source patches or browser interactions. Four image exports completed, primarily
using cached build layers. The subsequent Compose startup failed while creating
an internal network because Docker's predefined address pools were exhausted.
Therefore these logs do not establish a running application, successful database
migration, healthy authentication, or browser acceptance. The recorded image
versions describe that historical attempt, not the current product toolchain.

Preserve that failed-attempt classification here rather than adding raw local
build logs to the public repository. No container/network/volume removal, Docker
configuration change, stack restart or rerun was performed during this review.
Both original logs remain intact pending main-preservation and cleanup gates.

## Windows initializer reconciliation

Read the complete retained initializer and the complete current implementation,
including their delta. Preserve cryptographic independent secrets, additive
maintenance migration with explicit opt-out, strict duplicate-setting checks,
atomic environment writes and protected temporary-file handling. Keep the current
local-only A2A host/path restriction, removed public-tunnel configuration, saved
network subnet handling, existing-data-volume refusal and DACL/rollback recovery
checks; do not replace them with the older permissive behavior. Reading this code
does not establish a successful native installer, ACL recovery or live deployment.

The first-run password preflight did not match the current IDP policy. It counted
UTF-16 units instead of Unicode scalars, omitted bcrypt's 72-byte limit and could
accept whitespace that the IDP trims, NUL/invalid Unicode or markers that the IDP
rejects. Validate strict Unicode, at least 12 scalars and at most 72 UTF-8 bytes;
reject boundary whitespace rather than silently changing the supplied secret.
Retain the dotenv single-quote/line-break restriction and reject NUL and the same
known placeholder markers. Existing-install migration bypasses first-run input
validation and continues to preserve saved credentials.

The new test executes the actual preflight AST block and its validator, without
executing the rest of the initializer. Before the repair, 12 of its 20 cases
failed. All 20 now pass in both PowerShell 7 and Windows PowerShell 5.1, including
valid ASCII, multibyte and astral boundaries, plus literal interpolation symbols.
The test also rejects disclosure or modification of supplied values. No test
environment file, credential, Docker resource, account or ACL was created or
changed. Wire both interpreter checks into the existing Windows installer CI
step without adding a new job or weakening payload checks. All 121 existing CI
contract tests pass locally. Full installer/payload execution and real IDP login
remain separate acceptance gates. The retained initializer is not deleted.

## Workflow and task stylesheet reconciliation

Read the complete retained workflow and task-planning stylesheets and their
complete deltas against the current versions. Preserve existing shared theme
tokens, Basic/Advanced layouts, responsive inspectors, approval/error states,
keyboard focus, reminder provenance, reduced-motion task rules and MCP trace
inspection. Do not restore obsolete hard-coded light surfaces or duplicate
declarations merely to reproduce an older file.

The workflow component explicitly uses Angular emulated encapsulation. Twenty-three
selectors incorrectly targeted a descendant `app-workflow-engine` instead of the
component host. That left its empty/quiet states, compact toolbar disclosure and
idle layout without their intended component styles. Use `:host` and retain the
narrowly anchored child-disclosure styling. Keep absolute positioning on the
disclosure content only; the projected menu body remains in normal flow rather
than forming a second overlapping positioned panel. The task MCP explanatory
paragraph is not interactive, so remove its hand cursor and the duplicated
summary selector while retaining the real result-disclosure cursor.

Twelve new Chrome style tests fail before repair and pass afterwards at 375,
768, 1024 and 1440 pixels. They compile the actual two SCSS files under Angular
encapsulation and check scoped empty-state styles, idle columns, one bounded
toolbar popover, task inspector layout, dark surface tokens and document width.
These are isolated rendered style harnesses with representative markup and a
child disclosure harness, not a live application or screenshot acceptance pass.
No backend API, stored view preference, approval policy or execution handler was
changed. Original worktree files remain retained pending preservation on main.

An additional action-strip assertion catches three-column crowding at 375 and
768 pixels after the previously inactive host rules are enabled. Both breakpoint
cases fail before the follow-up fix. Keep three columns on desktop, two through
980 pixels and one through 680 pixels. Existing component tests are included in
the broader regression selection rather than relying only on style harnesses.

The final combined run passes all 1,218 cases across 41 specification files,
including the 12 style cases and 47 existing workflow component cases. The 66
native control-room logic checks and progressive-section, theme and icon
contracts also pass locally. This is not a full frontend-suite, production-build
or deployed-page visual acceptance claim.

## Migration guide and checksum exception reconciliation

Read the complete retained migration guide and its delta against the current
guide. Preserve its evidence-retention, least-privilege, explicit legacy-adoption
and backup requirements, while separating source/unit coverage from actual
PostgreSQL upgrade or recovery acceptance. The illustrative tree was not the
full catalog: add a checked snapshot of all 133 pre-phase and seven post-phase
migration pairs, require every phase-qualified guide reference to resolve to
both embedded SQL directions, and run these documentation checks uncached in CI.
Replace the obsolete `0082` coverage boundary and unavailable-toolchain note.
Clarify that individual down snippets are guarded targets, not standalone
downgrade plans: post-phase rollback must finish before pre-phase rollback, and
nonempty evidence or checksum refusals must not be bypassed.

Following the guide into the actual runner found a source-integrity defect.
The special historical checksum for `0128_account_feed_sync_cursor` was accepted
by version alone, even if its supplied up/down scripts had changed or were
empty. Three mutation cases fail before repair. Pin that compatibility bridge
to the exact reviewed repaired up/down/version digest, with separately pinned
LF and CRLF checkout variants. Reject unknown scripts, empty scripts, other
versions and mixed encodings; retain exact-current-checksum acceptance and valid
reviewed LF/CRLF controls. No SQL migration file or database ledger was edited.

Both local Go suites (`internal/infra` and `migrations`) pass with 208 top-level
PASS cases and 42 explicitly skipped PostgreSQL fixtures. The new guide-head
test fails before the documentation update and passes afterwards. Scoped
`go vet` also passes. No live PostgreSQL database was used; tagged runner
integration tests were not compiled by the ordinary run, and skipped fixtures
are not evidence of successful apply, adoption, rollback or restore. The
retained guide and source copies remain intact pending main-preservation gates.

The CI contract run also exposed one stale static stylesheet assertion from the
prior layout round: it required the incorrect descendant component selector.
Update it to require `:host` and explicitly reject the old selector, retaining
the independent Emulated/None encapsulation checks and the passing browser
geometry regressions. Do not reinstate broken CSS to satisfy a stale string
expectation.

All 121 CI contract tests pass after that expectation update. This does not
replace exact-head remote CI or the outstanding database acceptance gates.

## Life Ops component reconciliation

Read the complete retained Life Ops implementation and its full current delta.
Preserve eager change detection, owner-context loading, dated source-backed
need/capacity records, explicit numeric inputs, allowed goal levels and required
success/stop conditions, manual domain links, goal hierarchy, and advisory
priority assessment. The upcoming-goals filter is deliberately future-facing;
do not silently relabel it as overdue work or invent health/priority values.

All eight service paths across seven subscription sites now use deferred,
single-response observation with empty-response errors, finalization and
destruction teardown. Reads have six-second deadlines; writes have thirty-second
deadlines. A new entity query supersedes older observations, including an
away-and-back selection. Closing/switching the editor cancels obsolete entity
reads; mutation dispatch invalidates pending overview/entity reads so late data
cannot replace a subsequent write. Editor replacement and duplicate pending
writes are blocked. Cancellation and timeout do not prove server-side rollback.

Do not display raw transport or server error payloads. Provide a stable access
message for 401/403 and operation-specific recovery guidance otherwise. A failed,
empty or unusable write acknowledgement may still follow a committed record;
retain the form and direct the operator to inspect recorded state before a
deliberate retry. No automatic retry is introduced. Require nonempty record and
owner identities before announcing a write acknowledgement. The actual Go DTOs
and handler return paths were inspected for these fields; this narrow shape
check is not authorization, cryptographic proof or full nested-schema validation.
Only strictly boolean fresh/review metadata and a known non-unknown capacity
status permit capacity to influence a priority assessment. The result remains
advisory and grants no execution authority.

The initial 52 lifecycle regressions fail before repair and pass afterwards.
Additional controls exercise unusable acknowledgements, duplicate pending writes
and valid one-shot acknowledgements. The final combined selection passes all
1,297 cases across 43 specification files, including 70 new Life Ops cases and
its nine existing cases. All 121 CI contracts and 66 native control-room checks
pass. These are local component/service-double tests, not live owner API,
PostgreSQL, provider, browser screenshot or deployed acceptance. Deeper response
validation remains open. The retained source is intact and not deletion-eligible.

## Framework registry specification reconciliation

Read the complete retained framework-registry specification and its complete
current delta. Preserve its 33 original cases: source-backed overview counters
and partial failures, independent module preferences, lazy taxonomy disclosure,
deterministic records, non-authoritative selection hints, owner-and-permission
gates, immutable Constitution drafts, exact activation confirmation, conflict
recovery, restrictive rule recognition, authorization downgrade, safe error
messages, superseded reads, inspector focus restoration and navigation.

The current suite already adds persisted taxonomy-toggle and preference-history
retry coverage. Give the toggle case a distinct name rather than leaving two
identically named cases. Add three controls proving preference-history requests
are unsubscribed on framework replacement, inspector closure and component
destruction. Closed or obsolete reads cannot inject a later error or record into
the current inspector. Extend the existing AST-based scenario-identity check to
the framework suite without removing the task-blueprint check.

All 38 current framework cases pass as part of 1,335 Chrome cases across 44
specification files; all 67 native control-room checks pass. An AST statement
comparison separately checks that every retained case and its original ordered
statements remain, allowing the existing additional history-call assertion.
These tests use service doubles: they are not live backend authorization,
Constitution activation, deployed UI or provider acceptance. The entire
framework controller has not been reviewed in this step. Its retained
specification remains intact pending verified preservation on main.

## Command Center specification reconciliation

Read the complete retained Command Center specification and its full current
delta. Keep the original host-review summary boundary, pending approval lock,
operating-brief timeout, automation-health duplicate lock, unloaded/failed source
distinctions, freshness state, obsolete-refresh cancellation, inspector identity
and close behavior, bounded recent-memory query, local disclosure preferences,
accessible action cards, workflow routes and explicit operating-brief retry.

Current adaptations use the bounded pursuit dashboard-summary endpoint and
complete source fixtures. They retain approval locking while adding uncertain
outcome reconciliation, unavailable-queue retry, current-record admission and
signed-out recovery. The renamed refresh case retains all three source-checking
assertions and additionally excludes stale primary actions. Review the existing
asynchronous DOM, memory-archive row isolation, truthful action labels,
pursuit/ambient completeness and blocker-disclosure cases as well. Remove one
exact duplicated action-label case without removing its surviving assertions;
include Command Center in the native scenario-identity check.

This review found a real lifecycle defect: continuing an inspector scheduled a
360ms callback to wait for drawer closure, but leaving the page did not cancel
it. Three added cases fail before repair: destruction, inspector replacement and
explicit dismissal still allowed the old callback to execute or navigate. Store
and clear the pending timer on all three paths, and reject action dispatch after
destruction. Retain the ordinary deferred operation and route behavior. This
cancels pending local callbacks only; it is not server-side cancellation or
rollback of an operation that already began.

The focused Command Center suite passes all 51 cases after the repair. The
combined frontend selection passes all 1,337 cases across 44 specification files.
All 68 native control-room checks and 121 CI contracts pass. Review of controller and
template code here is limited to the relevant lifecycle and action paths, not
the complete controller. No live provider, backend or deployed acceptance is
claimed, and the retained original remains in place.

## OpenClaw maintenance migration fixture reconciliation

Read the complete retained Windows maintenance-migration test and its complete
current delta. Preserve opt-out and valid-token idempotency, BOM/encoding and
unrelated-text preservation, invalid/reused-token repair, ambiguous-setting
refusal, restrictive ACL-before-secret-write, concurrent path locking, isolated
worker-promotion exclusion, mocked legacy-process refusal/retry, silent-upgrade
ordering, fresh-install deferral and no application launch.

The current integration deliberately compares the environment-file DACL rather
than claiming to preserve every security-descriptor field. It also checks
removal of the retired public-tunnel setting during an explicit maintenance
opt-out. Promotion fixtures now use two distinct, valid Windows executables and
SHA-256 comparisons instead of text files masquerading as executables; neither
copied executable is launched as a maintenance worker.

Bound the test helper's direct PowerShell child to a configurable 1-120 second
deadline (60 seconds by default). Keep the exact process handle so Windows
PowerShell 5.1 retains the exit code. On timeout, stop only that child and await
its exit for at most five seconds, then dispose the handle. Do not kill processes
by name or make a general descendant-tree containment claim. A focused contract
loads the real helper through its PowerShell AST and checks success output,
nonzero exit/stderr and a sleeping child's timeout on both supported engines.
It fails before the deadline support is added. Temporary contract fixtures are
bounded to their generated directory; original source and retained archives are
not cleanup targets.

Correct the concurrent log-redaction assertion to inspect that migration's
actual token, not a different fixture's earlier token. Both the focused process
contract and complete migration fixtures pass on PowerShell 7 and Windows
PowerShell 5.1. All 122 CI contract tests pass. Add both-engine process checks and
the Windows PowerShell migration fixtures to the existing installer CI step;
the payload contract already runs the migration fixtures on PowerShell 7.
These are offline fixtures, not a signed installer upgrade, scheduled-task
registration, running-worker replacement, provider update or live owner install.
No real account configuration or application deployment was changed.

## Command Dashboard specification and OpenClaw chain reconciliation

Read the complete retained Command Dashboard specification and its complete
current delta. Preserve all 31 original cases: explicit candidate/approval and
completion endpoints, retained history and snapshots, stale/unavailable rather
than fabricated zero states, duplicate command prevention, partial and queued
outcome labels, incomplete-result failure, search retention, safe source links,
archive pending controls, unsaved source-path retention, runtime discovery versus
execution, owner-bound preparation, local disclosure and contextual accessibility.
Thirty original case bodies remain identical. The remaining unsaved-path test
now supplies complete approval references and explicitly verifies that the apply
endpoint was reached. Keep the five already-integrated rollback logic/rendering
cases; exercise execution failure with valid approval and safe recovery copy.

The four OpenClaw mutation paths (set path, refresh, upload and rollback) share a
bounded observation helper. Defer service calls so synchronous failures release
pending state. Consume exactly one preparation and acknowledgement, treat empty
completion as unconfirmed, cancel observation on destruction, and prevent new
calls after destruction. Require usable reference fields and a digest shape
before apply; require a matching OpenClaw acknowledgement and a usable path when
the operation selects an archive. These checks are not cryptographic approval
verification or authority: the backend still enforces its owner, immutable
effect, idempotency and approval gates. Require an explicit boolean rollback
availability flag. Keep error copy static and direct the operator to refresh
inventory before deliberate retry; transport failure or cancellation does not
prove rollback of a server-side write. Other Command Dashboard mutation paths
and the complete controller remain outside this review step.

Inspection of the actual backend handler, adapter and JSON shape shows that a
refresh can legitimately return inventory without a selected archive. Preserve
that response and avoid falsely announcing that an archive was re-scanned.
Ordinary preparation/application phases remain bounded to 30 seconds; both
full-archive upload phases receive the existing 15-minute upload limit. Resolve
these exact normalized POST paths through the shared timeout policy, replacing
the interceptor substring match that also granted long deadlines to unrelated
query strings, prefixes and path suffixes. Four interceptor regressions fail
before this repair. The gateway also lacked the upload envelope for approval:
add an exact authenticated approval-upload location with the same 752 MiB
multipart envelope, buffering policy, timeouts, refreshed identity/cookies and
security headers as the existing apply-upload location. A new parity contract
fails before the route exists. No unrelated route or body limit is expanded.

The initial component regression run observed 60 failures and unhandled malformed
acknowledgements, then disconnected; it is not a clean completed baseline run.
The final combined selection passes 1,478 Chrome cases across 48 specification
files, including 88 new OpenClaw boundary cases, 36 existing Command Dashboard
cases, 13 interceptor cases and four real runtime-service/mock-HTTP cases.
All 69 native control-room checks, 122 CI contracts and 23 static gateway
contracts pass. The native loader resolves the real shared timeout policy rather
than weakening its import allowlist. There is no local native Nginx command, so
these static contracts are not `nginx -t`, a live large-upload transfer, deployed
gateway or real owner/provider acceptance. No Docker service was started.
Original source and archive copies remain intact until main preservation.

## Framework registry template reconciliation

Read the entire retained framework template and the complete current delta.
Preserve selection context and capability hints, explicit recommendation versus
execution boundaries, source-backed counters/freshness, lazy taxonomy and
catalogue filters, recorded selections, immutable Constitution history/drafts,
explicit activation, inspector contracts and owner-scoped preference history.
Retain the shared-shell section landmark and the existing labelled help notes
instead of reinstating a nested main landmark or non-functional help buttons.

Upgrade the rendered accessibility suite to import the actual registry module,
including real NG-Zorro controls, tables, drawers, recommendation/inspector and
progressive disclosure. Register the app's actual icons. Do not suppress unknown
component errors or replace the checkbox controls with placeholders.

Two rendered regressions fail before repair. A disabled native fieldset did not
stop the custom checkbox hosts from changing all six read-only capability hints.
Bind `nzDisabled` on each real checkbox to the same permission/pending condition;
retain the fieldset boundary as well. Check both signed-out/read-only and pending
selection states, and verify that authorized idle users can still edit through
the same controls. This fixes frontend interaction state, not server authority.

The error branch also hid previously loaded selection rows despite retaining
them in memory. Keep known rows visible with an explicit stale-check warning,
without presenting a first failed read as empty history. Exercise the rendered
retry button through the existing refresh service path and verify that a
successful empty response clears the warning. Owner amendment preparation also
has a rendered disabled/enabled control check; preparing an editor does not
create or activate a Constitution version.

All eight rendered registry cases pass, including the original help-note case
and seven added controls. The combined run passes 1,486 Chrome cases across 49
specification files, plus 69 native controls and 122 CI contracts. No full
framework-controller review, live authorization/provider mutation, deployed
browser screenshot or full-app visual acceptance is claimed. The retained
template remains intact pending verified preservation on main.

## Framework registry controller reconciliation

Read the complete retained controller and its complete current delta, including
the existing preference-history plumbing, eager detection and local disclosure
helpers. Preserve independent refresh failures and previous records, deterministic
catalogue/history ordering, non-authoritative selection hints, protected-framework
limits, explicit owner gating, immutable Constitution drafts, exact activation
phrase, conflict recovery, restrictive typed-rule recognition, stable inspector
identity, reduced-motion focus behavior and per-module view reset.

All 14 service call sites now defer invocation and observe one response with
explicit empty-completion failure. Read deadlines use the shared eight-second
policy; writes use thirty seconds. Existing destruction teardown is retained,
and destroyed controllers cannot start new service calls. This catches service
construction errors inside the same error/finalization path instead of leaving
controls permanently pending. A focused 102-case matrix initially reports 74
failures; it then passes after repair. The final suite adds denial, stale
inspector, response-identity, raw-error and deferred-focus regressions.

Require boolean authentication and permission fields plus a nonempty subject in
the component. This is defense in depth: the existing real session service
already normalizes those fields strictly, and the backend remains the authority.
A denied write clears component authority and cancels an older pending refresh
so its late owner snapshot cannot re-enable another mutation. Bind framework
detail and preference responses to their requested record. A preference receipt
can update its catalogue entry, but cannot replace a newer inspector or its
edits, even after navigating away and back to the same ID. Delayed focus callbacks
also stop after destruction or superseding inspection/selection.

Do not echo raw backend validation strings through a keyword-denylist. Keep
explicit session/permission and Constitution-conflict guidance, with static
operation-specific copy otherwise. Transport failure, empty response or timeout
does not establish that a write was rolled back: selection, preference, draft
and activation failures now direct the operator to inspect current records before
deliberate retry rather than asserting that nothing changed. No automatic retry
or authority escalation is introduced.

All 112 new controller cases pass within 1,622 Chrome cases across 52 specification
files. That run also includes the 24 existing framework-registry and session
service cases through mocked HTTP and the previously rendered registry controls.
Separately, 69 native control-room checks and 122 CI contracts pass. These prove local client
behavior and contract consistency, not signed-session verification, real database
commit/rollback, complete cross-endpoint semantic validation, live framework
selection or provider acceptance. The retained controller remains intact until
its preservation on main is verified.

## OSS Insight evidence and runner dependency reconciliation

Read the retained OSS Insight screening ledger and its integration delta. Keep
its July collection and upstream-review notes explicitly historical: this pass
did not repeat the remote screening or advance upstream verification dates.
Replace stale current-version claims with links to the actual runner build
inputs and hash locks. The three runner Dockerfiles install requirements.txt;
the local Compose file defines the pgvector image and PostgreSQL target. Neither
an integration profile nor a build pin proves installation, compatibility,
provider evaluation or completed database migration. Legacy database volumes
remain protected.

The Python runner policy previously skipped exact dependency pins containing
extras. Compare canonical package, extras and version identities for the simple
pin syntax used by these manifests, without claiming a general dependency
resolver. Two new regression cases fail before repair and all eight runner
policy tests pass afterward. Preserve the existing lock/build-input equality
and hash requirements.

The complete braincatalog package passes 84 top-level tests and ten subtests;
the focused go vet check also passes. New evidence tests cover the historical
snapshot boundary and the three runner manifest links. The 122 CI contract
tests pass. These are local source and fixture checks, not live upstream,
container, provider or database acceptance. No dependency installation, runtime
upgrade or original-file deletion was performed.

## Trello resumable test reconciliation

Read the complete retained resumable test file and its current integration
delta. All 19 original test-function bodies remain identical after newline
normalization. Preserve generation reset and overlap, independent card/action
cursors, supported ID pagination, monotonic transitions, observed/verified
identity contracts, authorized terminal-job resume, resource caps, unchanged
extraction behavior, resumable inventory checks and progress projections.
The existing integration adds webhook repository stubs, explicit loopback
fixture binding, board identity checks and short-link/member provenance cases.

The convenient card-page fixture fills absent board IDs automatically. Add an
independent raw HTTP fixture that does not repair provider records: missing,
empty, whitespace-only, malformed and foreign board identities must return no
importable slice. Canonical and normalized board IDs are positive controls.
Assert that validation reaches the card-page response, uses authenticated reads
without query credentials and does not persist raw items or extractions before
the durable commit. All seven new subcases pass without production-code changes.

The focused Trello/progress run passes 101 top-level tests and 150 subtests;
seven database-dependent tests skip because no test database is configured.
These checks use local HTTP fixtures and in-memory repositories, not a live
Trello account, signed production webhook, transactional database acceptance or
complete connector readiness. The original remains retained pending main
preservation verification.

## Router smoke-test reconciliation

Read the full retained router smoke-test source and its current integration
delta. The two retained variants are identical after newline normalization;
their raw hashes remain tracked separately. Fourteen original test bodies are
unchanged, and the route-tree test retains every original line in order with
additional registrations and dispatch cases. Keep the real MCP, Serena and
planning-optimizer initializer permission tests, API-key configuration failure
checks, dedicated production memory-secret boundary and local-origin CORS cases.

The large marker-handler route tree is representative, not a registration-parity
test against every production initializer. Correct its overly broad comment.
Add dispatch assertions for the already-registered source and background overview
paths. Also cover an unreadable emergency-stop provider in all six known modes:
background work must remain blocked even if the provider returns false alongside
its error. The existing clear/engaged stop checks remain intact.

All 16 focused router tests and six new subtests pass without skips. No production
route, middleware or autonomy-policy behavior changed. Marker dispatch and
in-process initializer checks do not prove complete production route parity,
signed-session authentication, real provider execution, deployment or database
acceptance. Both retained originals remain intact until main preservation is
verified.
