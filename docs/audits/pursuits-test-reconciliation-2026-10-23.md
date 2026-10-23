# Pursuits test reconciliation

## Scope

The retained model-maintenance variant of
`frontend/src/app/pages/pursuits/pursuits.component.spec.ts` was compared with
the current branch. The complete retained delta (232 added, nine removed lines),
including imports, fixture helpers, setup, assertions, and DOM interactions, was
read. Subsequent local changes are described below.

TypeScript test-identity/token accounting preserves all 62 retained literal test
titles. The original comparison had 59 token-identical and three changed cases;
the three changes are explicit candidate acceptance, routed-candidate wording,
and selected-detail failure/retry. No unchanged case body is represented as fully
manually reread merely because its token stream matches.

## Preserved behavior

- Candidate responses use the real detail-normalization contract, distinguish
  active accepted work from a waiting candidate, and do not imply that execution
  happened. Global intake carries its explicit dashboard trigger.
- The Basic list remains compact until selection. Selection changes cancel an
  obsolete project-dossier observation and clear its data before rendering the
  next project. Detail failure/retry does not leak the supplied raw error.
- A create-intent URL opens the form once and removes the consumed query intent
  without replacing unrelated parameters.
- Dossier tests cover Basic summary, Advanced returned evidence and truncation,
  missing project key, failed read/retry, and rejection of a mismatched project
  key. The dossier fixture remains read-only evidence, not execution authority.

## Corrections found during review

Two newer test names claimed empty-dossier coverage that their bodies did not
provide: one only checked missing project key; another only rejected a wrong key.
Their names now describe those actual cases. A separate rendered case supplies
zero counts and empty workflow/memory arrays and asserts the confirmed-empty
state is distinct from missing-key, loading, and failure states. The current file
has 70 literal cases rather than the previous 69.

The dossier error callback displayed raw backend error text, unlike the protected
selected-detail callback. A native regression reproduced the disclosure with a
synthetic credential/path message. The callback now uses a generic retry message;
the rendered failure/retry case also checks that the synthetic private value is
not present. This localized repair does not complete the separate full retained
Pursuits-controller reconciliation.

## Verification boundary

The native dossier-error regression failed before the repair and passes after it.
The native pursuit, control-room, and template-landmark run passes 193 tests. It
loads actual component logic but is not a rendered browser acceptance test.

The combined source/Pursuits Angular run subsequently passed 182 tests in Chrome
Headless, including all 70 current Pursuits cases and the new confirmed-empty
rendered case. Temporary browser files were redirected to D: because C: remained
low on free space. This is not a full frontend suite or production build.

No live project API, account, provider, runtime, deployment, complete repository,
or transcript-archive acceptance is asserted. Exact-content review receipts are
not `main` preservation or permission to delete the original source.
