# Original health and safety source reconciliation

Reviewed main: `3b5afa697ebb0e72b01d59e3a04648a04317cd45`.
Tested prepared source checkpoint: `d1a26fd2eb00bac7ee82026a60a833b3daaef8e5`.

## Original source deltas

Three present modified production files in the original dirty checkout were
read and compared with the prepared source. Their raw SHA-256 values still
match the frozen original-source inventory. The corresponding current
production file bodies are already on reviewed main; no old implementation
needs to be copied over them.

### Doctor diagnostics

`backend/internal/doctor/doctor.go` retains the original database-password
validation intent. Main additionally distinguishes the shipped PostgreSQL
default in both development and production diagnostics, rather than allowing
the local default to look healthy. Production placeholder and strength checks
remain. The Kafka warning now describes durable outbox queuing rather than
claiming that unconfigured publishing drops events.

### Readiness

`backend/internal/health/health.go` supersedes the older per-probe PostgreSQL
pool creation/close path with acquisition and ping of the shared serving pool.
It checks cancellation before acquisition. Existing tests cover ping failure
and caller cancellation without contacting a database.

Main also wires the event-outbox and source/workflow/ambient scheduler probes
as noncritical readiness diagnostics. The optional Kafka and Redis text
reflects outbox queuing and per-process fallback rather than claiming unused
or dropped capabilities. This source comparison is not verification of a live
broker, Redis deployment or event-delivery outcome.

### Emergency stop

`backend/internal/safety/emergency_stop.go` retains the original stop handling
and strengthens cancellation around execution-fence admission. It checks the
context on each wait iteration and after obtaining the read lock, releasing
the lock if admission was cancelled. The dedicated regression case exercises
that cancellation boundary deterministically.

Environment hard-stop evaluation is factored into
`EvaluateImmutableEmergencyStop`; the ordinary evaluator still consults it
before the persisted provider. This supports narrowly scoped operator-stop
recovery without allowing application approval to override an environment stop.
It does not certify the whole recovery authorization chain or installation-owner
policy, which require their separate route/persistence checks.

## Executed verification

The retained module was checked against the prepared revision before execution:
all 19 Go source/test files in the three packages and both module files match.
The complete package sets then ran with Go 1.27.3, read-only module metadata,
offline dependency resolution, one build worker, two logical Go processors,
a 512 MiB Go memory limit and CGO disabled.

Result: all three packages passed; 82 root tests and 329 test cases including
subtests passed, with zero skips and zero failures. The tests use in-process
fixtures and loopback HTTP test servers where applicable; no application stack,
container, real database or external provider was started or contacted.

All 21 selected package/module input hashes and all three original source hashes
were stable after execution. The isolated compiler temporary directory was
empty. Native `go clean -cache` reduced the verification-owned build cache from
259,771,707 logical bytes to 228 bytes. This removes this check's overhead; it
is not a claim of equivalent net cleanup of pre-existing user data. No new
module directories were observed under the retained module cache during the
bounded post-run creation-time check.

## Limits and preservation

Four of the 19 package files differ from main: prepared doctor-probe/redaction
implementation and regression files contain previously prepared privacy fixes.
The execution therefore validates the prepared package cohort, not an exact
main-tree/full-dependency closure. It does not establish PostgreSQL, browser,
provider, runtime, race-detector, full CI or production acceptance.

This disposition covers the observed differences in three original files, not
all original Go files, the 692-path tracked delta, the untracked cohort, other
worktrees or conversation archives. No global reconciliation count is increased
without a separate overlap check. No original source file was changed or removed,
and no whole checkout became deletion-eligible through this review.
