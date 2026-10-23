# Workflow engine and test reconciliation

## Retained sources

The retained workflow engine variants are 240,651 and 235,238 bytes. They are
identical after CRLF-to-LF normalization, while their original hashes were
checked separately. Both have 216 declarations: 189 token-identical and 27
changed against the repaired working file, which has 225 declarations including
nine additions. All retained functions remain present.

The retained workflow test variants are 229,781 and 223,771 bytes, each with 228
declarations. The current test file has 232. The model variant has 225
token-identical and three changed declarations; the artifact variant has 224
token-identical and four changed declarations. All old functions remain present.

Go AST/token accounting retains comments, literal text and statement order.
Token-identical bodies were not all manually reread. The complete published
production delta (367 added and 196 removed lines), test delta (151 added and
eight removed lines), artifact-to-model test delta, and local compatibility
repair were read through their ends. Selected current follow-up transaction
and revision-boundary context was also inspected.

## Preserve current integration

- Explicit success criteria survive intake and reach the task runner.
- Caller contexts and execution uncertainty propagate through worker paths;
  uncertainty overrides reported success, approval and safe-error markers.
- Manual transitions and checklist updates retain atomic state/audit repository
  operations. Failed memory-context/provenance persistence is not silently
  treated as complete intake.
- Open-loop projection is delegated to the owner/lease-fenced transaction in
  `followup_transaction.go`, not discarded with the older independent-write
  body. It preserves approval requirements and durable replay receipts without
  executing a task or provider action in that transaction.
- Review-required results stay visible to clients. Task fixtures explicitly
  declare external execution where their test scenario needs that evidence.
- Framework provenance tests retain normalization, conflicting idempotent write
  rejection and legacy payload compatibility. Capability wording remains partial
  for connector-specific webhooks; tests do not certify live delivery.
- Revision encoding retains separator and literal-prefix escaping. Existing
  current-format revision fixtures remain unchanged.

## Pre-criteria replay repair

Adding the success-criteria field changed source revision hashes even when the
field was empty. Replaying an unchanged older intake could replace an existing
workflow or conflict with work already in progress. A new fixture derived from
the retained pre-criteria field layout reproduced this in three workflow states.

The local repair accepts that older layout only when neither requested nor stored
criteria exist and the fields are unambiguous. It preserves the existing workflow,
state and historical revision instead of rewriting them. Current-format hashes
remain unchanged. Changed criteria, stored criteria and a different owner cannot
reuse that legacy workflow. Raw boundary separators and literal encoding prefixes
are excluded from automatic legacy matching; no ambiguous historical hash is
silently trusted.

The repair does not migrate database contents, grant execution authority, reuse
an approval for a newly created workflow or bypass backend safety checks.

## Verification and limits

Targeted revision and existing separator-boundary cases pass after the regression
failed before repair. The uncached `workflow`, `workflowtask`, `source`, `task`
and `pursuit` packages all pass on the repaired local code. No dependency install,
Docker environment, real provider mutation or database migration was performed.

The source-engine review and this compatibility repair are not yet published at
review time. The published PR head remains
`b23388ddd53e9d3e9ffd7c030ef3996a2a98168a` while its CI runs. CI for that head
does not validate the later local repair.

Local tests do not prove live providers, database concurrency or all claimed
execution lifetimes. Claim expiry after task execution remains a distinct
repository/concurrency review issue, not fixed by this compatibility change.

The selected ledger covers 610 of 618 files. A complete selected-file review of
one worktree is not proof of integration of all its baseline/ignored data, nor
proof of preservation on `main`. Other checkouts and transcript archives remain
open. No original sources or transcripts were deleted.
