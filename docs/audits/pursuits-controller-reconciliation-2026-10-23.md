# Pursuits controller reconciliation

## Scope and method

The retained model-maintenance variant of
`frontend/src/app/pages/pursuits/pursuits.component.ts` was compared with the
integration branch. The complete retained delta (980 added, 303 removed lines
before the loader repair below) was read in consecutive bounded chunks through
EOF. Additional local repair changes were read separately.

TypeScript AST/token accounting covers imports, class header, properties,
constructor, methods, and accessors: 336 retained units, 382 current; 297
token-identical, 38 changed, one deliberately replaced helper, 47 additions.
Comments and literals are retained in token comparison; whitespace is ignored.
Unchanged bodies were accounted for, not all manually reread. These unit counts
are not feature counts or evidence of live execution.

## Retained operational contracts

- Basic does not automatically select the first pursuit or prefill real intake
  with sample text. Explicit create intent is consumed without losing unrelated
  route parameters; direct selection and evidence routes remain available.
- Current detail and decision reads bind the requested pursuit. Mutations check
  the selected pending decision, returned identity/state/audit evidence, and
  current selection before replacing content. Empty responses do not establish
  success. Candidate acceptance, archive/reopen, proposal, approval, next-action,
  runtime review, and completion review remain distinct paths.
- Context changes compare the submitted goals, criteria, stop conditions,
  dependencies, dates, and resource ceilings against returned values. Changed
  drafts and newer editors/selections are not overwritten by older responses.
- Resource recording binds the immutable receipt to its quantity, kind, pursuit,
  and recording key. Ledger reads reject missing, duplicate, or mismatched
  entries. Reservation release confirms the selected hold and explanation, does
  not adopt an unscoped budget object as proof of settlement, and reloads detail.
- Intake and first-plan handling distinguish new linked work from existing work,
  require approval context for high-risk/approval-first pursuits, retain uncertain
  drafts, and do not equate planning/routing with completed execution.
- Delegation checks ready/not-ready state, instructions, boundaries, and matching
  pursuit identity. Link creation binds returned association fields; detachment
  confirms the current association without claiming deletion of its source.
- Runtime stop distinguishes requested, stopped, blocked, failed, and unconfirmed
  states. Candidate stop evidence remains unconfirmed until an exact matching
  stored attempt can be inspected.
- Owned observations are released on teardown. Superseded detail/evidence reads
  cannot reopen a different pursuit's inspector.

The old private `isBrowserNavigableUri` helper is intentionally not restored.
`browserSourceHref` uses URL parsing, explicit schemes, credential rejection, and
control-character checks. Local/internal evidence is resolved in the selected
pursuit rather than opening arbitrary `file:` or executable URLs.

## Loader defects found and repaired

The new project-dossier loader still accepted multiple emissions, remained busy
after empty completion or a stalled read, and allowed matching-key responses
without the containers/counts required by its template. Synchronous dispatch
failure also escaped observable recovery. The preceding reconciliation had
already protected its raw error text.

The loader now defers dispatch, observes one response, rejects empty completion,
and bounds the read at six seconds, matching the existing connected-source read
deadline. A generation-bound finalizer releases busy state without settling a
newer request. It does not automatically retry or claim server-side cancellation.

Before assigning a dossier to the template, it checks required containers,
finite timestamps and numeric values, boolean flags, nested record arrays, and
nonnegative integral counts. Returned workflow/memory totals and nested returned
counts must match their arrays; workflow truncation must agree with matching and
returned totals. These count relationships were checked against the current Go
project-dossier projection rather than invented independently. Valid empty and
populated dossiers remain accepted.

This is a renderability/consistency boundary, not proof of source truth, ownership,
or completion. The backend remains responsible for authorization, source
provenance, policy, and grounded output. Optional textual metadata and every
possible domain value are not comprehensively validated by this UI guard.

## Verification and remaining scope

Four new native regression groups failed before the loader repair. The five
dossier groups then passed, including the prior privacy case. A further group
passes complete populated data through the actual service and component with a
stubbed HTTP response, and rejects malformed nested records. Virtual time proves
the six-second observation bound without a six-second test sleep.

The native pursuit/control-room/landmark run passed 198 tests, and a focused strict
Angular compiler pass with no output emitted passed. These do not establish live
API/provider, account, browser-deployment, or runtime acceptance. Most other
Pursuits request paths are not newly certified to have deadlines or safely handle
synchronous dispatch exceptions merely because the dossier path now does.

The affected Pursuits Angular suite also passed 71 tests in Chrome Headless,
including a rendered incomplete-response error state and the existing valid
empty/populated views. Its truncation fixture was corrected to make the matching
count greater than the returned count when it claims workflow truncation.

Reviewing the final selected variant completes only the selected-source review
set, not the original dirty checkout, ignored/baseline worktree contents, recovery
clones, or transcript archives. Private content receipts and PR publication do
not establish preservation on `main` or authorize deleting those originals.
