# OpenClaw maintenance worker reconciliation

## Reviewed source variants

Compared four retained variants with current code: `capability.go`,
`process_status.go`, `types.go` and `worker.go` in `internal/openclawmaintenance`.
This scope does not cover the entire maintenance service, launcher ecosystem,
Windows containment implementation or remaining HAI archives.

- Retain the complete short-lived authenticated Windows capability handshake;
  it is an observation, not installation authorization.
- Retain the newer cancellation-aware suspended-thread resume guard and
  explicit unknown process-tree termination reporting.
- Retain receipt-relative provider freshness validation, calendar-valid numeric
  release revisions and normalized reports that do not expose raw local paths.
- Retain bounded stdout/stderr consumption and cancellation on excess output,
  strict provider Date/Age validation, independent package signing-key pinning,
  approved publisher verification, no-replacement bounded installer caching and
  locked-file hash/signature verification before execution.
- Retain approved pre-install evidence across post-install discovery and the
  proxy-free, no-redirect loopback health client. Do not restore the older
  unconditional post-install publisher-success assignment.

## Integrated correction

The locked installer admission helper previously delegated cancellation handling
to later callbacks. It could open and hash an installer, invoke publisher
verification and dispatch the run callback despite an already canceled context.

The helper now requires a non-nil execution context and checks cancellation at
entry and between lock acquisition, hashing, publisher verification and launch.
Cancellation observed after the run callback also cannot report success.
Acquired locks still close exactly once, including canceled paths.

These are stage-boundary checks, not a claim that an in-progress filesystem read
can be preempted or that an installer already launched can be rolled back. The
existing contained process implementation remains responsible for process-tree
termination, and execution uncertainty remains review-required.

## Verification and preservation

- Five canceled-stage regressions and the missing-context regression failed
  before the fix.
- The full maintenance package passes locally after the correction, including
  existing successful admission and lock-cleanup controls.
- No actual Companion update, external provider acceptance or production
  installation was performed. CI and operator acceptance remain separate gates.
- Original worktrees and archives remain intact pending main-preservation checks.
