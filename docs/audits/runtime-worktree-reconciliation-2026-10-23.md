# Runtime worktree reconciliation

## Scope and outcome

Compare the complete meaningful diffs of seventeen present changed/untracked
file versions from the retained runtime-safety worktree against main commit
`30781d4fb393386aecab0c449d100d158f4eabf5`. Ignore CRLF-only presentation
differences while reviewing; verify original raw content separately against the
private inventory. Also account for one locally absent tracked safety file.

No missing retained implementation was identified in this selection. Keep the
newer main implementations and their expanded tests. This batch records a
reconciliation, not a newly implemented runtime integration or a claim of live
provider acceptance.

## Per-file decisions

All paths below are relative to `backend/internal/`.

| File | Decision |
| --- | --- |
| `agentruntime/deepseek_harness.go` | Keep environment-value redaction, credential-free version probes, live-context stop admission, exact runtime/owner/task/job receipt checks, indeterminate uncertain cancellation, bounded enqueue recovery and link/reparse-safe workspaces. |
| `agentruntime/ecosystem_authorization.go` | Keep approval-bound rollback with both current and target archive identities; rollback does not bypass authorization. |
| `agentruntime/handler.go` | Keep two-slot upload admission before body parsing, cancelled-request rejection, durable content-addressed archive selection, retained prior archives, approval-gated rollback and overflow-safe compression-ratio comparison. Do not restore deletion of the prior uploaded archive. |
| `agentruntime/handler_test.go` | Keep expanded overload, cancellation, oversize, archive-retention/restart, rollback-route and integer-overflow coverage. |
| `agentruntime/runtime.go` | Keep checked stop-response identities, exact configured gateway host/port, pinned validated DNS targets, TLS verification, narrow explicit Docker-host opt-in, owner-bound durable terminal artifact receipts, final commit admission and persistent archive selection. Keep the current capability map; an older static provider identifier alone does not prove a working integration. |
| `agentruntime/runtime_test.go` | Keep expanded terminal-receipt, archive-retention, gateway policy, DNS rebinding, IP pinning, redirect and untrusted-TLS tests. |
| `agentruntime/openclaw_artifact_download_test.go` | Keep rejection of production loopback artifact URLs, including the otherwise valid URL mode; do not restore the old expected successful local HTTP download. |
| `agentruntime/openclaw_artifact_url.go` | Keep HTTPS/origin checks, production loopback denial, explicit isolated-test-only loopback policy, NAT64-aware address validation, DNS-answer pinning and bounded credential-free transport. |
| `agentruntime/openclaw_artifact_url_test.go` | Keep production-versus-test policy separation, mapped IPv4/IPv6 and NAT64 cases, rebinding prevention and same-/cross-origin redirect rejection. |
| `hostruntime/handler.go` | Keep bounded authenticated start-intent, start-acknowledgment and unknown-outcome routes, with distinct conflict and unavailable states. |
| `hostruntime/handler_test.go` | Keep the full confirmation/start-intent/acknowledgment path and explicit live cancellation context instead of the older confirm-only fixture. |
| `hostruntime/migration_contract_test.go` | Keep start-intent migration checks for legacy-lease quarantine, immutable protocol evidence and refused destructive rollback. Text contracts are not database execution proof. |
| `hostruntime/repository.go` | Keep immutable approval digests, row-locked one-shot start intents, exact binding and freshness checks, uncertain-start quarantine, context-bound cancellation, actual-start acknowledgment requirements and explicit GORM update models. |
| `hostruntime/repository_memory_test.go` | Keep the expanded in-memory fixture's start-intent/acknowledgment state machine, quarantine paths and pre-intent versus started cancellation distinction. This fixture is not PostgreSQL proof. |
| `hostruntime/repository_postgres_integration_test.go` | Keep the isolated temporary-table schema aligned with start-intent columns/constraints, distinct fixture runtimes, approval digests and actual-start acknowledgment. It remains explicitly integration-tagged and operator-gated. |
| `hostruntime/service.go` | Keep microsecond-stable approval binding, bounded start protocol, live-context cancellation, emergency-stop fencing and the hard execution-isolation gate. A bridge acknowledgment is a worker claim, not independent OS proof. |
| `hostruntime/service_test.go` | Keep explicit start intent/acknowledgment fixtures, freshness, idempotent/conflicting replay, stop ordering, uncertain completion and cancellation race coverage. |
| `agentruntime/final_effect_authorization.go` (locally absent) | Keep the main file. The worktree's missing file must not become a product deletion. Preserve exact request/proof binding and fail-closed verifier behavior. |

## Safety and evidence boundaries

The host execution isolation gate remains closed without verified isolation and
appropriate process-start ordering. A durable start intent does not make a
later Windows Resume atomic with server emergency-stop state. An authenticated
worker acknowledgment is not independent verification of the operating system.

The gateway Docker-host exception requires explicit enablement, the exact
allowlisted host and port, and configured private IP pins. It is not a blanket
private-network allowance. Artifact downloads have a separate policy and do
not inherit loopback permission from gateway development access.

The test-only artifact functions are not production download entry points.
Tests using local HTTP servers prove their isolated transport contracts, not
acceptance of real OpenClaw, Hermes, DeepSeek or external accounts.

## Validation and retention

Both local package runs passed on Windows with Go 1.27.3:

```text
go test -p 1 -timeout 180s ./internal/hostruntime ./internal/agentruntime -count=1
```

Original integrity checks matched all seventeen recorded raw file hashes.
Hosted exact-head CI remains separate. The PostgreSQL integration-tagged suite
was not executed in this review. No
database, credentials, installed runtime or external account was modified.

The seventeen original file versions remain intact and are not byte-identical
to main. Recording these decisions does not preserve every historical byte in
GitHub and does not make the worktree deletable. Ignored files and unchanged
worktree files are outside this selected-difference review; they still require
their own preservation assessment before whole-worktree cleanup.

A supplementary GitHub path-history metadata check, bounded to twelve commits
per present file at the pinned main revision, found no raw or CRLF-normalized
original-content match for any of the seventeen versions. No history page
reached the bound. This is a negative result for the inspected path history,
not proof that no equivalent version exists under another path or reference.
It does not authorize deletion or replace a recoverable source-preservation
record.
