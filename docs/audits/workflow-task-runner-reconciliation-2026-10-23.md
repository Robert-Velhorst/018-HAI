# Workflow task runner reconciliation

## Retained source review

Read both complete retained runner and specification versions and reviewed the
complete differences against the current implementation. Each pair differs only
in line endings. Preserve deferred delegation, conservative automation ranking,
side-effect-free framework preflight, risk ceilings, immutable approval
provenance, standing mandates, idempotency and partial launch evidence.

Keep current contextual execution and cancellation, copied success criteria,
redacted bounded failure reasons, approval-only classification constraints and
explicit uncertain-result handling. A post-dispatch error must not be labelled
safe to retry. A non-validated plan must not inherit a passing validation flag
as workflow success. No older safeguards or assertions are removed.

## Contract continuity defect

The framework selection records contained an operating-contract digest, but
the selector-v5 preview/result comparison omitted that field. A changed or
removed digest could therefore survive comparison and return a successful
workflow result. Comparing the catalog, risk and constitution fields alone
does not bind the operating contract.

The comparison now includes that digest. Invalid post-run result evidence also
sets outcome uncertainty before the existing reconciliation projection: the
result cannot retain `Passed`, become an approval-only action, or invite an
automatic replay. Partial plan and launch evidence remain available alongside
the error for reconciliation. This detects post-run inconsistency; it is not a
claim that this adapter can undo effects already performed by a runtime.

Legacy non-selector-v5 compatibility is unchanged. Optional historical contract
digests are not invented; a transition from a present digest to a missing one
is rejected when the selector-v5 contract comparison applies.

## Verification and preservation

- The comparison regression failed before the fix.
- The adapter-level changed/missing-digest cases failed before the fix; an
  unchanged digest passed as a positive control.
- The complete workflow-task and workflow Go packages passed uncached after
  the fix. Database-dependent workflow cases skip without a dedicated DSN.
- Tests preserve one preview, one execution attempt, partial plan identity and
  an unmarked post-dispatch error, with uncertain/review-required result flags.

These are local fixture results, not live provider, real installer or production
database acceptance. No original audit file, worktree or archive was deleted.
