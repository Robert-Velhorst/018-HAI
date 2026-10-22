# Host start service reconciliation

## Reviewed source versions

Compared six retained source versions with main
`4684546907900d4958f85e7b71ba8848376b0b93`:

| Files | Versions | Decision |
| --- | ---: | --- |
| `backend/internal/hostruntime/handler.go` and `handler_test.go` | 2, model-maintenance worktree | Retain authenticated, body-bounded start-intent, actual-start acknowledgement and unknown-outcome endpoints with explicit safe error classifications. |
| `backend/internal/hostruntime/service.go` and `service_test.go` | 4, model-maintenance and artifact worktrees | Retain context-aware admission, approval digests, stop revisions, one-shot start bindings, freshness/replay quarantine and cancellation truthfulness. Add post-repository context checks below. |

Older confirm-only execution must not replace the newer start protocol. The
protocol records an authenticated worker report, not independent Windows proof.
Production execution remains blocked without verified OS isolation and atomic
ordering of remote Resume against emergency stop. This review does not cover
the remaining repository and migration source versions.

## Uncertain-response repair

Begin-start, actual-start acknowledgement and unknown-outcome reporting could
return success after their repository operation cancelled the request context.
The service now checks its bounded context after each operation and preserves
both repository errors and cancellation errors. It withholds a successful start
intent or acknowledgement when the request has ended.

This is not rollback: a repository operation may already have committed. The
existing intent/acknowledgement remains available for reconciliation. The repair
does not retry, erase a durable record, issue a replacement intent, or claim that
an OS process never started. Existing exact replay and operator-review rules
remain in force.

## Verification

- Three deterministic regressions failed before the fix, one per protocol
  operation. The fixture first applies the memory-repository operation and then
  cancels the request before returning.
- Tests require cancellation errors, no successful response payload, one
  repository call, and preservation of the already stored intent/acknowledgement.
- Full `hostruntime` and `agentruntime` package tests passed locally on Windows
  with Go 1.27.3 after the fix.
- Memory-repository and handler tests do not establish real PostgreSQL commit
  behavior, Windows process isolation, or live bridge acceptance.
- Original worktree files and private integrity receipts remain intact. Nothing
  in this review or an open PR permits deletion of unpreserved source data.
