# Source and governance progressive-view reconciliation

## Scope

This review compares the retained model-maintenance variants of:

- `frontend/src/app/pages/connected-sources/connected-sources.component.html`
- `frontend/src/app/pages/governance-control/governance-control.component.html`
- `frontend/src/app/pages/governance-control/governance-control.component.spec.ts`

The complete textual differences were read. Angular parsing traverses structural
templates as well as ordinary elements and accounts for event-expression and
reactive-form-control identities. This identity comparison does not prove
multiplicity, layout, accessibility, server behavior, or every unchanged body.

This is not a review of every retained connected-sources controller/test variant,
every repository file, the transcript archives, or complete worktree preservation.

## Connected sources

The retained template has 49 explicitly typed buttons; the current template has
64. All old reactive form-control names remain. The old audit retry expression is
replaced by separate audit/sync retry controls. Toggling archived extraction scope
now resets its page instead of reusing the previous offset.

Preserved behavior includes connector setup, Google read-only authorization,
WhatsApp local import, Odoo modeling, source access checks, sync admission and
uncertain-request recovery, correction status, search and provenance inspection,
pause/resume, and existing-approval-only revocation/deletion. Added history
controls expose bounded extraction, sync, and audit pages. Loading, unavailable,
and empty states remain distinct. Trello polling is not presented as proof of
webhook registration or comment delivery.

The reconciliation found a stale metric tooltip claiming that 100 recent records
were loaded. It now binds to the existing extraction scope summary, so its page
range, total, loading state, and unavailable state agree with the actual view.
The rendered pagination test checks the tooltip before navigation, on the next
page, and after returning. Native Angular-parser tests enforce explicit button
types and retain the approval form's non-click-driven submit contract.

## Governance

The retained/current templates have 72/73 explicitly typed buttons. All retained
event-expression identities remain. Additions are expired-monitor recovery and
lazy execution/learning section-open handlers. Domain counts use the summary
API's method count rather than expecting omitted full playbooks. The service
normalizes this summary field; the backend derives it from the full playbook.

The retained test delta preserves review, mandate, outcome, and monitor scenarios
while replacing eager-history expectations with Basic/Advanced request contracts.
The synthetic policy-run fixture is renamed consistently, not removed. Monitor
recovery tests distinguish actual recovered leases from no-op recovery.

Review of the new lazy handlers exposed overlapping-read problems: opening full
execution evidence cancelled its pending summary without settling the shared
surface; opening learning history cancelled proposal loading. Refresh could also
replace full execution evidence with a summary while its loaded marker prevented
another full fetch. Execution now uses one summary/full request path that settles
the shared surface state and retains the requested depth on refresh. Learning
history has its own lifecycle-managed subscription, independent of proposals.
Saved open execution/learning sections restore their detailed reads.

Five asynchronous regression cases failed before the repair. After the repair,
both affected Angular suites passed together: 171 tests in Chrome Headless.
The source metric DOM test also checked its changing tooltip across page
navigation. The native control-room and template-landmark checks passed 128
tests, including three new source-template cases. These are scoped test results,
not a fresh full frontend suite or production-build result.

## Verification boundaries

Local Angular tests exercise compiled templates, mocked services, and browser DOM
interactions. They do not establish deployed viewport quality, actual accounts,
database acceptance, provider availability, or live lease recovery. No approval
authority or execution gate is relaxed by this work.

Private integrity receipts bind the reviewed originals and current files to their
exact content. A PR or successful local test is not proof that these originals are
preserved on `main`. No reviewed source or transcript archive is deleted by this
batch.
