# Agent coordination, planning and DSH source reconciliation

## Scope

Reviewed ten retained versions against the current implementation:

- Model-maintenance worktree: `backend/internal/agentcoordination/timeout.go`
  and `timeout_test.go`.
- Both model-maintenance and artifact worktrees:
  `backend/internal/agentframework/service.go` and `service_test.go`.
- Both worktrees: `backend/cmd/hai-dsh-bridge/main.go` and `main_test.go`.

The older versions do not supply a missing capability that warrants replacing
the current code. The current implementation retains their behavior while
strengthening cancellation, timing and execution-safety boundaries.

## Reconciliation decisions

| Area | Keep on main |
| --- | --- |
| Deferred acknowledgments | Evaluate stored deferred acknowledgments after their retry time passes, while keeping stricter future-time validation at ingress. |
| Reminder pacing | Do not issue the next reminder until its own interval is due. |
| Planning deadline | Apply the configured timeout to injected HTTP clients as well as the default client; maintenance and planning share the caller context. |
| Maintenance cancellation | Use the context-aware model-maintenance gate instead of the older synchronous contract. |
| Planning redirects | Reject redirects even for injected clients. |
| Advisory skills | Describe consented catalog guidance as non-authoritative text, not executable upstream skills or side-effect authority. |
| Bridge lease context | Keep worker identity, approval digest and stop revision fields in the lease envelope. |
| Bridge credentials | Do not follow gateway redirects or forward its bearer token; construct a fresh HTTP client from exported settings rather than copying internal synchronization state. |
| Termination truthfulness | Ambiguous process-start failures cannot authorize a positive process-tree termination acknowledgment. |
| Error classification | A cancelled deadline alone must not relabel an unrelated supervisor failure as a verified timeout stop. |
| Production execution | Keep DSH execution disabled until TLS peer trust, OS-enforced least privilege and an acknowledged process-start/emergency-stop ordering protocol are implemented and verified. |

## Verification and limits

The meaningful source and regression-test differences were inspected. Local
verification passed for all three packages with Go 1.27.3 on Windows:

```text
go test -p 1 -timeout 180s ./internal/agentcoordination ./internal/agentframework ./cmd/hai-dsh-bridge -count=1
```

This does not establish real model-provider acceptance, production
DSH isolation, or permission to enable a disabled execution path.

Original versions remain retained and have private fingerprint evidence.
Semantic reconciliation is not exact-byte preservation, and this audit does not
make an unfinished worktree or transcript archive safe to remove.
