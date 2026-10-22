# Durable queue explicit-review classification

## Finding

`RequiresManualReview` recognized errors implementing the review-classification
interface but not the package's own `ErrManualReviewRequired` sentinel.
A handler returning the sentinel could therefore be treated as an ordinary
retryable failure. Joined with a policy deferral, it could be treated as a
normal pause. Both paths returned a successful poll rather than the requested
review outcome.

This was found while comparing retained durable-queue implementations. The
broader retained repository/runner/test reconciliation is not complete and is
not claimed by this focused repair.

## Repair and regression evidence

- Check `errors.Is(err, ErrManualReviewRequired)` before custom classification.
- Preserve the sentinel through ordinary wrapping and joined false classifiers
  or policy deferrals.
- Keep nil, ordinary errors and ordinary policy deferrals outside manual review.
- Exercise recurring handlers returning direct or joined review sentinels:
  the occurrence becomes `needs_review`, no retry/successor is created, and a
  later polling pass does not invoke the handler again.

Four classifier cases and both runner scenarios failed before the production
repair. The tests use the existing controlled review repository; they are not
proof of production PostgreSQL behavior.

After the repair, the complete `internal/durablejob` and `internal/ambient`
packages passed locally with Go 1.27.3 on Windows:

```text
go test -p 1 -timeout 180s ./internal/durablejob ./internal/ambient -count=1
```

## Boundary

The change does not alter persisted replay policy, database lease recovery,
review-write failure handling, or operator approval authority. Review-sensitive
work still requires the existing durable review-registration and persistence
capabilities for crash/unknown-outcome safety. No external action is enabled.

Original worktree and transcript copies remain retained. This repair does not
establish that any original is fully integrated or safe to delete.
