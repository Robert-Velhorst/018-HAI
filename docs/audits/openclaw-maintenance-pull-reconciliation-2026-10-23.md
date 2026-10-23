# OpenClaw maintenance pull reconciliation

## Scope and retained behavior

Reviewed the retained `pull_client.go`, `pull_integration_test.go`,
`contained_process_windows.go`, `handler.go` and `handler_test.go` variants
against current code. Also inspected and hardened the shared PostgreSQL fixture
setup in `service_test.go`; this does not mark that entire larger test file
or remaining archives as reconciled. The retained `service.go` implementation
has now also been reviewed in full against the current implementation.

- Keep current lifecycle ownership of installer goroutines: unconfirmed
  cancellation must not make a still-running callback disappear from shutdown.
- Keep strict loopback-only, proxy-free, no-redirect authenticated requests,
  validated leases, confirmation before execution and live permit monitoring.
- Keep bounded final-receipt delivery after cancellation and retry only the
  exact receipt, never the installation or authorization.
- Keep the newer explicit publisher-pin verification in successful synthetic
  integration reports. The older reports omitted evidence now required by the
  current report validator.
- Keep the newer pre-create/pre-resume cancellation checks in Windows process
  containment and the newer owned scheduler goroutines, paused-background gate
  and retry recovery when no persisted deadline can be loaded.

## Unambiguous worker headers

The handler previously read only the first Authorization and capability header
value. Duplicate credentials could pass authentication, and duplicate capability
reports could refresh installation readiness from just the first value. This
does not establish an authentication bypass without a valid credential.

Require exactly one Authorization value before recording contact. A duplicate
capability header is treated as unsupported, clearing previous install readiness;
the authenticated worker can still perform read-only/status work. Six HTTP
cases cover single valid headers, both credential orders, identical duplicates
and conflicting capability reports. Four cases failed before the correction.
An additional HTTP/service/PostgreSQL regression checks the durable chain:
duplicate credentials leave the pending install unleased, while duplicate
capability reports cancel it with the recorded capability-unavailable reason.
Neither path may dispatch the installer callback.

## Integrated acceptance and safety work

CI previously did not configure `TEST_OPENCLAW_MAINTENANCE_DSN`. The two HTTP /
service / PostgreSQL integration tests therefore skipped in ordinary package
runs. CI now creates a dedicated maintenance database in its existing disposable
PostgreSQL service and explicitly runs:

1. Independent backend-health rejection, preserving the submitted worker report
   while switching the target to owner review and queuing read-only recovery.
2. Lost receipt acknowledgement, proving that retry records one completed update
   without executing the installer callback twice.

Those two tests, the ambiguous-header admission regression, the persisted
review-evidence rejection regression and the existing successful explicit-review
lifecycle require
top-level PASS markers; a skip is insufficient. The live Windows /
WSL opt-in test remains excluded: no real update or live Gateway access is
authorized by this fixture work.

The shared fixture now uses the existing dedicated PostgreSQL test guard:
explicit destructive-test opt-in, exact `hai_openclaw_maintenance_test` database,
literal loopback address and no remote failover destinations. pgx configuration
copying replaces raw DSN concatenation so URI and keyword forms share the same
schema binding. Administration and scoped pools are bounded, early failures
retain cleanup hooks, initialization is time-bounded, and schema cleanup verifies
the created namespace OID before dropping it. Connection errors do not echo DSNs.

## Review evidence and recovery chronology

Keep the current paused-background gates, metadata freshness measured at receipt
completion, successful current-update admission and read-only recovery when a
release changes during installation. The older service does not supersede them.

Owner review previously accepted completed check evidence without fully binding
its target, receipt identity, worker lease, observation time and stored versions.
Ten pure regressions reproduced those invalid acceptances before the correction.
Review now requires a matching completed check, authenticated worker/lease
provenance, matching observed versions and monotonic creation/start/completion
times. Existing freshness, publisher verification and independent health gates
remain required.

Recovery checks now record their actual creation time instead of a fabricated
timestamp one second in the future. Creation may share the interrupted update's
completion tick, but the new observation must start strictly after that update.
The PostgreSQL regression covers four persisted-evidence mutations, rejection
without resolving the interrupted job or writing a success audit, restoration
of valid evidence and successful explicit review without automatic installation.
It also checks the actual recovery creation time. These database assertions are
mandatory in CI, not claimed as executed by local package runs without a DSN.

## Verification and preservation boundary

- Configuration regressions first reproduced acceptance of wrong database,
  remote host, hostname and remote failover destinations before adding the guard.
- Positive keyword, URI and IPv6-loopback configurations and malformed-input
  rejection are covered without opening database connections.
- The CI contract failed before the dedicated database and test selection were
  added; the full 113-contract suite passes after wiring and fixture-isolation coverage.
- Local maintenance, pgtestguard and maintenance-command package tests pass.
  SQL-backed service tests skip without the dedicated DSN and explicit opt-in;
  actual PostgreSQL lifecycle and cleanup execution remain CI acceptance gates.
- Original worktrees and transcripts remain intact. These source reviews and
  publication do not by themselves establish main preservation or deletion safety.
