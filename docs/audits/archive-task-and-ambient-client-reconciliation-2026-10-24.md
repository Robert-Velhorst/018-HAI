# Archived task and ambient-client reconciliation

Baseline: `3b5afa697ebb0e72b01d59e3a04648a04317cd45`.
This review covers three overlapping historical variants, relative to
`frontend/src/app/services/`: `task-plan.service.interface.ts` (`8ba4356a12db`),
`task-plan/task-plan.service.ts` (`de2c4a6afc53`) and `ambient.service.ts`
(`9d6bfe1f5e9f`). They belong to the already indexed TypeScript cohort.

## Task planning source disposition

The plan endpoint remains. Plan and the added run operation now copy requests
with a trimmed existing idempotency key or a generated operation identity.
The caller's request is not mutated. Separate method calls without an existing
key generate separate identities: this is not proof that arbitrary caller
retries deduplicate, nor that the server persists or enforces the key.

History still uses the logs endpoint, but its interface changes from complete
plans to compact history items with a bounded limit. Current backend
`task/handler.go` verifies owner identity, validates the log limit and returns
owner-scoped compact history by default; it retains a separate full-view branch.
The compact shape is a deliberate change, not exact historical response parity.
No full-view client method is claimed by this review.

Review queue, decision and approved-review reconciliation transports are added.
The last two forward their typed request; these tests do not certify arbitrary
inputs, review-ID validation, authority or server approval enforcement. Nothing
here authorizes replacing current safety-aware execution with an old plan-only
implementation or accepting a client-side boolean as execution permission.

## Ambient source disposition

Overview retains the full default and adds an explicit summary view parameter.
Scan retains its POST. Need updates now validate key format, integer 0-100
levels/weight, boolean enabled and optional string notes, then construct a body
containing only permitted fields. Accept/dismiss reject malformed and nil UUID
references before contacting HTTP. These are client checks; ownership and policy
must still be enforced by the server.

## Forty-one focused checks

All 41 current-main local checks pass. They use installed real RxJS, generated
synthetic UUIDs, an immutable HttpParams test double and an HTTP double returning
local observables. The Angular decorator is stubbed for isolated construction;
there is no Angular TestBed, browser, live request or archived-code execution.

Seventeen task checks cover missing, empty, whitespace and existing operation
identities for both plan/run; immutable caller input; six log-limit cases;
queue retrieval; review-decision transport; and approved-review reconciliation
transport. Assertions check HTTP verb, resolved endpoint and relevant request
values. Forwarding a synthetic decision is not a real approval operation.

Twenty-four ambient checks cover full/summary overview, scan transport, valid
need-update allowlisting, four invalid key formats, seven invalid payload cases,
omitted optional notes, and valid/invalid accept and dismiss references. Invalid
cases produce an observable error and record zero HTTP calls.

A separate negative control changes only the in-memory current-source plan URL.
The transport assertion detects the wrong endpoint. Product files remain
unchanged. This is harness-sensitivity evidence, not a repaired product defect.

## Remaining boundaries

Backend idempotency persistence, cross-call retries, authorization, task execution,
compact-history rendering, summary payload size, actual integrations and browser
error handling are not established by these tests. Full type checking and
application build were not run for this receipt.

Publication, exact-head CI, main integration and complete archived-requirement
reconciliation remain necessary before source cleanup. Neither whole-file
integration nor archive deletion eligibility is established here.
