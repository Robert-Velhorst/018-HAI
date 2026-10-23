# OpenClaw discovery summary reconciliation

## Retained behavior

Reviewed the complete discovery-ledger source in both audit worktrees and the
complete difference against the current implementation. The retained versions
are byte-identical. Keep current explicit persistence status reporting and use
the operation returned by the durable save before advancing its state.

Preserve owner/workspace-scoped lookup, protocol and endpoint binding, bounded
freshness, evidence hashing, authenticated read-only aggregate discovery and
cloned summaries. Discovery evidence does not grant execution authority.

## Correctness fix

Nonnegative aggregate counters were added before their individual upper bounds
were checked. Machine-integer overflow could turn impossible task/model/agent
counts into the declared sample size, or turn tool counts into a negative total
that passed the maximum-size comparison. A valid evidence hash did not prevent
this semantic error.

Each counter now has an upper bound before addition: tasks cannot exceed the
sample size, model and agent categories cannot exceed their respective sample
sizes, and each tool-source count cannot exceed the existing 500-tool limit.
Combined totals still must satisfy the existing consistency checks. Constructed
evidence must pass the same validation used during restoration before it can be
reported as ready for persistence.

## Verification

- Four overflow regressions failed before the production fix. Each covers both
  construction and restoration of an otherwise correctly hashed payload.
- Valid zero, one and maximum-size summaries remain accepted; a combined tool
  count above 500 remains rejected.
- The complete runtime-lab Go package passed uncached after the fix.

These are local code and fixture results, not acceptance against a live OpenClaw
Gateway or an external account. No runtime was enabled and no credentials,
providers or installed applications were changed. Original audit files remain
intact; this review alone does not authorize deleting a worktree or archive.
