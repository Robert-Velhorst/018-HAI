# Runtime mutation reconciliation

## Reviewed sources

Six retained source versions were compared with main
`4684546907900d4958f85e7b71ba8848376b0b93`:

| Files | Retained versions | Decision |
| --- | ---: | --- |
| `backend/internal/agentruntime/deepseek_harness.go` | 2 | Retain current bounded admission, context-aware dispatch and cancellation receipts, environment-secret redaction, link/reparse rejection, and truthful unavailable health without a current authenticated worker heartbeat. |
| `backend/internal/agentruntime/final_effect_authorization.go` | 2 | Retain canonical task scope shared by adapter inputs and the final-effect proof request. |
| `backend/internal/agentruntime/handler.go` and `handler_test.go` | 2 | Retain durable managed archives, previous-archive retention and governed rollback, bounded concurrent uploads, overflow-safe ratio checks, and cancellation-aware upload inspection. Add the shared commit-boundary cancellation check below. |

DeepSeek execution remains blocked pending the required peer authentication,
OS-enforced isolation and acknowledged start/stop ordering. Reviewing its adapter
is not permission to enable it or evidence of live harness acceptance.

## Cancellation repair

A path-selection request cancelled by its authorizer could still apply the
selection. Upload inspection already had cancellation checks, but the shared
ecosystem commit helper did not. All HTTP mutation callers now reject missing or
cancelled request context, and recheck cancellation inside the adapter-state and
emergency-stop commit fences immediately before invoking the mutation.

The repair covers the shared boundary used by set-path, refresh, upload and
rollback. Browser disconnection or cancellation cannot roll back an operation
that already began; callback errors remain visible rather than being relabelled
as proof that no effect occurred. An approval already consumed before cancellation
is not refunded or automatically reused.

## Evidence and limitations

- The HTTP path-selection regression and inside-fence cancellation regression
  both failed before the repair.
- A third regression preserves errors from callbacks that already started.
- Full `agentruntime` package tests passed locally on Windows with Go 1.27.3.
  Existing emergency-stop interleaving fixtures now supply real request context.
- These are local handler/adapter checks, not live external runtime acceptance.
- Original worktree source copies remain intact. Private source fingerprints
  identify the six reviewed versions; no whole-worktree deletion is authorized
  by this review or by an open PR.
