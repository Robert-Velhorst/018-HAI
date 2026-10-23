# Durable source scheduler reconciliation

## Reviewed source scope

Reviewed the durable source scheduler and its test file in both retained audit
worktrees: four source versions. The artifact versions precede the improved
busy-source recovery behavior in the model-maintenance versions. Neither
variant should replace the stronger current implementation wholesale.

Retain the current implementation and tests for:

- A recurring singleton scan and stable source-ID payload deduplication,
  including source renames and repeated registration after restart.
- Durable manual-sync and extraction-correction recovery; keep the newer
  Trello webhook handler registration as well.
- Busy-source deferral without consuming provider retries, rechecking whether
  the source remains due before resuming, and preserving that check across a
  temporary due-source read failure.
- Per-source retry backoff and lease cleanup, expired-lease recovery, queue
  isolation, and lease generation checks. Do not restore a fake reaper that
  always reports zero or treats busy work as already completed.
- Operator pause/revoke retirement of queued work without provider retries,
  dead-letter cooldown and review workflow creation for terminal failures.
- Lifecycle-owned worker admission and readiness cleanup, disabled-by-default
  legacy fallback, and the explicitly tested one-minute idle poll default.

These are source and synthetic queue comparisons, not proof of live provider
availability or a production database restart test. Other connector service,
repository and handler files remain separate review work.

## Poll interval parsing repair

The old integer scanner accepted a numeric prefix without requiring the whole
setting to be a number: `15junk`, `15.5`, `15 30` and a two-line `15` / `30`
setting all selected a 15-second poll interval. That could silently increase
database wakeups instead of using the configured safe fallback.

Use the standard-library whole-string decimal parser before applying the
existing 15-second to one-hour bounds. Invalid syntax and integer overflow
retain the one-minute default. Surrounding whitespace, signed positive decimal
input and decimal leading zeros remain compatible; no dependency is added.
The outdated five-minute comment now matches the current tested default.

Four regression cases failed before the change. Tests also cover malformed
separators, hexadecimal syntax, overflow and valid boundary/whitespace/sign
controls. The full source and durable-job packages passed locally with uncached
tests on Windows and Go 1.27.3;
database-only cases without a dedicated DSN are not claimed as executed.

## Preservation boundary

Private receipts bind the four reviewed source versions to exact fingerprints.
The originals remain intact. Semantic reconciliation and publishing this PR
do not prove that an entire worktree or transcript archive is safe to delete.
