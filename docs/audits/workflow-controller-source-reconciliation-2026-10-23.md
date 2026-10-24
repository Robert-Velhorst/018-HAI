# Workflow controller source reconciliation

## Review scope

The retained 59,115-byte workflow controller was compared with published commit
`13933b0a744b37f671d39ac2651767b10845e3d1` and the subsequent local checklist
repair. TypeScript AST accounting covers imports, the decorated class header,
constructor, properties, methods and accessors. Length-delimited lexical token
hashes retain comments and literal text while ignoring whitespace trivia.

The retained file has 141 entries. Against the repaired working file, 99 are
token-identical, 41 changed and one removed; 49 entries are added, making 189
current entries. Token-identical bodies were not all manually reread. The complete
885-added/288-removed-line published delta and subsequent checklist repair diff
were read. AST identity does not prove dependency or runtime behavior.

The removed `selectNewestPursuitWorkflow` method guessed the result of an intake
by sorting existing records. Preserve the newer exact intake references rather
than restore that heuristic.

## Preserve newer safeguards

- Request subscriptions, obsolete record reads and input matching have explicit
  lifetimes; request cancellation is not proof that the server stopped a write.
- Empty, malformed, foreign and uncertain action responses do not become verified
  completion notices. Selected records and confirmation snapshots are rechecked.
- Workflow approval, interruption recovery and reminder decisions bind their
  returned state and evidence to the requested identities and digests.
- Local outcome-review pauses require refreshed records and explicit review;
  backend authorization remains separate and cannot be granted by that UI flag.
- Worker, recovery and follow-up summaries validate bounded counts and record
  identities. Provider messages are not passed through as authoritative status.
- Intake preserves bounded input, optional success criteria and exact returned
  workflow links. Filter focus opens its module-local Advanced section.

## Checklist repair

The review found that `markChecklist` still applied an arbitrary returned record,
did not release a busy state on empty completion, and left its observation active
after component destruction. The local repair:

- Validates the current workflow, one matching checklist item, its parent and a
  supported requested status before dispatch.
- Accepts only one acknowledgement matching the workflow, item and status.
- Requires affected-workflow readback after an error, empty response, malformed
  acknowledgement or observation timeout; it does not automatically repeat a
  mutation or claim that the prior mutation did not happen.
- Preserves a newer user selection and unsubscribes on component destruction.
- Clears checklist errors after successful affected-workflow readback.

Three new regression groups failed before the repair. Four new groups, including
a stalled-observation case, now pass within the 118-test native control-room suite.
The existing Angular interaction test was strengthened to require readback before
retry, rather than re-enable the mutation immediately after an error.

The final repaired working tree passed all 2,276 Angular ChromeHeadless tests and
the production build. The initial bundle remains 781.14 kB (estimated transfer
180.90 kB). Existing SCSS warning-budget overruns remain in App Shell, Task
Blueprint, Workflow Engine and Pursuits; no budget was raised. Build/runtime
checks for progressive sections, split themes and icon registration also passed.
The local npm runner is 11.6.2 rather than the manifest's 12.2.1; no dependency
installation, upgrade or deployment was performed.

## Publication and cleanup boundaries

At review time this repair is local, not part of the cited published commit.
Exact-head CI for that commit must finish before another head is pushed, avoiding
automatic cancellation of its long-running job. These local tests do not prove
real backend execution, external outcomes or a deployed browser flow.

The selected-source ledger now covers 602 of 618 files. This is not the complete
HAI inventory: other checkouts, divergent recovery sources and transcript archives
remain open. No original source or transcript was deleted. Review, publication,
verified preservation on `main` and local deletion remain separate gates.
