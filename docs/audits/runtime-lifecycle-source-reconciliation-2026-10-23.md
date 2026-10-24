# Windows runtime lifecycle test reconciliation

Read the complete retained lifecycle test source and its complete integration
delta, including fixture setup, mocked collaborators, native-child tests and
cleanup. The retained source contains 131 assertion call occurrences; the
current source has 166. Of the original calls, 117 remain text-identical after
newline normalization. The fourteen nonidentical original occurrences require
semantic comparison; counts alone are not proof of behavior preservation.

Keep the cleanup-error and credential-redaction checks, A2A opt-in/token/settings
preservation, URL credential refusal, configured Agent Card matching, process
path/command/creation-time checks, PID reuse refusal, orphan recovery/status,
unknown-process refusal, native process-handle checks and hard-disabled DSH
execution path. Keep the no-destructive-cleanup behavior after failed/concurrent
Compose startup. Their fixtures do not authorize an actual application action.

The adapted assertions follow the current local-only A2A contract and removal
of its old public setting, narrower Agent Card request counting, and host-runtime
refusal before any process/PID mutation. Do not restore remote A2A acceptance or
stop a pre-existing process as a prerequisite to refusing an unsupported option.
The separate historical public-tunnel feature remains explicitly unresolved.

The current source adds local-port/project-name validation, protected-env/data
recovery checks, dashboard/Open readiness, stricter Compose ownership fixtures,
network-range preservation and forced-initialization ACL checks. Its A2A target
fixture now uses the canonical 8080 port from the preceding installer repair.

## Test-mode correction and cleanup

Both focused switches could previously be supplied together. The orphan branch
returned success without exercising the settings branch. An independent CLI
regression observed exit zero before repair. Reject that contradictory request
before resolving source paths or creating a fixture, rather than reporting a
partial run as both requested checks.

Fixture cleanup retains its exact generated basename/temp-root boundary, now
fails explicitly for an invalid root and refuses reparse points before recursive
removal. No retained source, user data or unrelated temporary directory is a
cleanup target. This source guard does not establish resistance to every
concurrent filesystem substitution; that remains a separate security concern.

## Verification boundary

- The full lifecycle script passes on PowerShell 7 and Windows PowerShell 5.1.
  This includes the real Windows ACL/file operations on generated fixtures and
  native process identity/stop checks against child processes created by the
  test itself. It does not terminate an installed runtime or another app.
- Docker, HTTP readiness and browser-opening boundaries are mocked. Progress
  messages saying the stack is starting/ready come from fixture-driven production
  scripts, not evidence that Docker built, started or served a real dashboard.
- All 131 current CI contract tests pass locally without skips. The new conflicting
  focus-mode test fails before repair and passes after it.
- No new installer, container, provider, public tunnel, account or private
  environment was used. No clean-machine or deployed-operator acceptance is
  claimed. Original files remain intact pending integration on main and separate
  preservation/deletion verification.
