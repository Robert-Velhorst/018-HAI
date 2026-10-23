# Connected-source controller and tests reconciliation

## Scope and evidence

The retained model-maintenance variants of these files were compared with the
integration branch:

- `frontend/src/app/pages/connected-sources/connected-sources.component.ts`
- `frontend/src/app/pages/connected-sources/connected-sources.component.spec.ts`

The complete controller delta (345 added, 57 removed lines before this repair),
the complete test delta (716 added, 14 removed), and the repair diff were read.
The TypeScript parser accounts for all old controller declarations: 309 retained,
355 current; 268 token-identical, 41 changed, 46 added, none removed. Token
comparison retains comments and literals but ignores whitespace; it does not
prove dependency behavior or substitute for reading changed context.

Literal test-title accounting finds 81 retained and 112 current cases: 72
token-identical, eight changed, and one explicitly expanded/renamed status case.
No old test title is silently discarded. Imports, factories, hooks, pagination
fixtures, and modified assertions were checked in the complete textual diff.
Unchanged bodies were token-accounted, not all manually reread.

## Preserved and deliberately strengthened contracts

- Connector capability no longer defaults to operational when missing. Modeled,
  local-only, configured, disconnected, revoked, failed, stale, and verified
  connection states remain distinct. Enabled-source counts exclude revocations
  confirmed by either the source or its health record.
- Google consent is blocked for paused, revoked, mutating, or checking sources;
  pending create/authorization operations do not start duplicate creations.
- Manual sync accepts only matching source/job identities, supported states,
  valid counters, and dates. Newer authoritative job state can supersede stale
  failed or running cache records without turning old history into live state.
- Paged extraction, audit, and sync-history reads remain independent of latest
  job indexes. Existing page data survives failed reads, with warning states;
  changing archive scope resets extraction offset. Previous-page navigation
  remains available after concurrent deletion reduces the total.
- Basic refresh does not eagerly fetch closed Advanced history. Loading and
  unavailable states are not rendered as confirmed empty records. Odoo is
  modeling, WhatsApp export is local import, and neither is promoted to verified
  live account access by a configured health record alone.
- The old browser-confirm/naked-delete path is intentionally replaced, not
  restored: destructive actions require existing server-approved references,
  matching resource identity, and the backend's approval/idempotency checks.

The changed tests intentionally expect local-only rather than connected for
local imports, disconnected rather than generic unavailable, and paged APIs
rather than eager full history. These are changed contracts, not unchanged old
assertions. Existing provider, correction, uncertainty, and pursuit-handoff cases
remain present.

## Defect found and repaired

An empty destructive observable completed without a confirmed outcome but allowed
another submit. A multi-emission observable could announce success and refresh
more than once. Destructive observation was not owned by component teardown, and
a synchronous service-dispatch exception escaped its recovery handler.

The request is now deferred into its error-handled pipeline, consumes at most
one acknowledgement, rejects empty completion as uncertain, and is unsubscribed
when the component is destroyed. Destroyed components cannot initiate this
destructive flow. A valid emitted void deletion response remains valid; an empty
stream is not the same as that response. Revocation still requires the exact
source and revoked state. No retry, approval bypass, or server-cancellation claim
is introduced. Uncertain UI state remains local to this component; backend
authorization and idempotency remain authoritative across navigation/reload.

## Verification and limits

- Four new native regression groups visibly failed before the repair; all five
  groups pass after it, including the existing exact-source/revoked-state rule.
- The combined native control-room and template-landmark run passes 133 tests.
  These tests load the actual component/RxJS/form logic but do not render its
  template.
- A focused Angular compiler pass checks the connected-sources module and its
  dependencies with `noEmit` and strict inherited application settings. It passed
  without generating browser/build output.
- The earlier browser-bundled connected-sources suite passed 112 tests before
  this final observation repair. That earlier result is not presented as a fresh
  browser run of the repaired code. A new full browser suite/production build was
  not run here because C: fell below 1 GiB free.

There is no new live provider, account, destructive-server-action, deployment,
full-repository, or transcript-archive acceptance claim. Private exact-content
receipts do not authorize deletion: integration into `main` remains a separate
gate. Original sources and archives are retained.
