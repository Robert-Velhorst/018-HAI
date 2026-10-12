# Model Production Health Handoff

Date: 2026-10-01. Branch observed: `codex/hai-runtime-release`.
Status: bounded source fixes and focused regressions saved; executable validation
is pending the parent. This report is a handoff, not production certification.

## Scope And Preservation

Inspected Model Intelligence provider registration, probing, inference outcome
classification, local maintenance gates, registry routing eligibility, telemetry
and their existing tests. Read neighboring LLM policy/maintenance, deterministic
providers, DSpark, the existing redaction helper and the preceding
`output/runtime-outcome-integration-20261001.md` as source evidence only.

Used installed `golang-patterns` and `golang-testing` guidance and the historical
local-first/approval constraints, checked against current source. No provider,
account, browser or unrelated connector access was necessary. No descendants
were created. No executable tests, installs, builds, containers, application
process operations, private-localhost operations, live prompts, downloads,
commits, pushes, deletes or README edits were performed. Pre-existing changes
were retained, including the already-dirty owned files; this report does not
attribute their entire Git diff to this worker.

## Established Findings And Repairs

1. **P2: Nil Go errors were treated as inference success without checking the
   adapter's success flag, error field or selected identity.** The service's
   existing usage validator did not reject `OK=false`, contradictory error
   metadata, empty output or a different provider/model/lane. This could promote
   a benchmark and record successful telemetry without a successful inference.
   A shared service-boundary validator now rejects those cases and invalid
   duration/throughput metrics before output acceptance, validation or caching.
   Failed telemetry is attributed to the selected route rather than missing or
   spoofed result identities. Adapter-reported usage is still retained under
   the existing usage classification; rejected output is not returned.

2. **P2: Probe success stayed active indefinitely, and returned check provenance
   was replaced by request time.** Registry status now carries its actual check
   time and expires cached active evidence after 15 minutes, matching the
   canonical LLM policy's default readiness window. Expired evidence projects
   `configured`, not a fabricated failed live probe. LastProbedAt retains the
   actual accepted check time. Stale active checks, future times and negative
   model counts/durations are rejected. Built-in deterministic providers remain
   exempt from artifact/readiness expiry. Failed live status does not become
   active or merely configured through age expiry.

3. **P2: Failed inference, including an adapter's failed pre-generation
   re-probe, left prior active catalog health intact.** Rejected inference now
   invalidates the selected model's health as `failed`. It does not invent a
   probe timestamp or erase historical observed probe provenance. Probes and
   inference failures share the existing ordering counter, so an older failed
   attempt cannot overwrite a newer committed probe. A subsequent successful
   explicit probe can recover the model. A failed attempt is not automatically
   retried and cannot promote claims or populate the output cache.

4. **P2: Raw adapter/maintenance errors could reach public probe or benchmark
   details and RunLane errors.** Those boundaries now use the existing
   `safety.RedactSecrets` helper. A redacted error wrapper preserves `errors.Is`
   identity while sanitizing its displayed message. The owned remote adapter
   also redacts configuration, probe and completion diagnostics directly.

5. **P2: Partial/nil dependencies could panic instead of failing closed.** A
   nil registry becomes an empty registry, not an invented configured provider.
   Nil and typed-nil adapters are excluded from registry reads, typed-nil local
   maintenance gates are refused, and incomplete owned remote adapters without
   a client or model ID report `not_configured` before transport access.

## Changed Paths

- `backend/internal/modelintelligence/provider.go`: dependency nil detection
  and display-safe error wrapper.
- `backend/internal/modelintelligence/provider_registry.go`: timestamped health
  expiry, nil adapter handling, partial configuration checks and diagnostic
  redaction on the owned remote adapter.
- `backend/internal/modelintelligence/maintenance.go`: typed-nil checks and
  redacted wrapped maintenance failures.
- `backend/internal/modelintelligence/service.go`: result validation, route-bound
  failed telemetry, failed-inference health ordering and probe timestamp checks.
- `backend/internal/modelintelligence/production_health_test.go`: new focused
  table-driven and lifecycle regressions. Tests use existing doubles and a fake
  HTTP transport, not live providers or newly started servers.
- `output/model-production-health-20261001.md`: this handoff.

The existing `maintenance_test.go` and `service_test.go` were inspected but not
edited by this worker. Other Model Intelligence, LLM and agent-runtime source
remained read-only.

## Actual Verification

- `git diff --check -- backend/internal/modelintelligence/provider.go
  backend/internal/modelintelligence/provider_registry.go
  backend/internal/modelintelligence/maintenance.go
  backend/internal/modelintelligence/service.go`: exit 0; only existing
  LF-to-CRLF notices. This is a whitespace check, not Go compilation.
- A trailing-whitespace source scan of the newly added regression file returned
  no matches, exit 0.
- Read source call sites for the changed registry status-recording signature and
  health-ordering helpers. Reviewed existing adapter fixtures for client/model
  configuration compatibility. These are source checks only.

## Parent Verification Commands

Run serially using the parent's approved bounded environment, from `backend`.
Keep network module/toolchain downloads disabled; use the existing cached
toolchain or approved existing test image rather than installing anything.

```text
gofmt -d internal/modelintelligence/provider.go internal/modelintelligence/provider_registry.go internal/modelintelligence/maintenance.go internal/modelintelligence/service.go internal/modelintelligence/production_health_test.go
go test -p 1 -parallel 1 -count=1 -timeout 120s ./internal/modelintelligence -run '^TestProductionHealth'
go test -p 1 -parallel 1 -count=1 -timeout 180s ./internal/modelintelligence
go vet -p 1 ./internal/modelintelligence
go test -race -p 1 -parallel 1 -count=1 -timeout 300s ./internal/modelintelligence -run 'TestProductionHealth|TestOlderProbeCompletion|TestCancelledProviderProbe|TestConcurrentBenchmark|TestMaintenanceGate'
```

None of these formatting/Go commands were run by this worker. Apply formatting
only to the listed owned files if the parent confirms differences; do not
overwrite concurrent unrelated changes. Parent execution should use the
established low-memory settings, for example `GOMAXPROCS=1`, `GOMEMLIMIT=384MiB`,
`GOTOOLCHAIN=local`, `GOPROXY=off` and `GOSUMDB=off`. The full package contains
existing local test servers; executing them is reserved to the parent.

## Limits And Acceptance Boundaries

- Exact local-inference attestation, unknown/paid-billing refusal, token bounds,
  durable telemetry prerequisites, canonical daily-maintenance ownership and
  context cancellation remain in force. No specialist/provider prerequisite
  was relaxed and no new framework was introduced.
- The auxiliary registry's 15-minute expiry is an explicit fixed default, not
  a claim to dynamically inherit a differently configured LLM policy max age.
  Historical probed/benchmarked claim levels remain historical evidence; current
  status determines routing eligibility.
- Zero probe timestamps retain the pre-existing service-time fallback for
  adapter compatibility. Production remote and DSpark probes explicitly return
  the supplied check time; arbitrary adapters' timestamp provenance is not
  independently authenticated by this interface.
- The owned service sanitizes DSpark diagnostics crossing that boundary, but
  raw direct helper/DSpark behavior in read-only files was not changed. Redaction
  tests cover synthetic secret shapes recognized by the existing shared helper;
  they are not proof that every possible secret format can be recognized.
- A canceled generation remains withheld under the existing cancellation
  contract. This work does not add an inference outcome/reconciliation ledger
  for calls that may have reached a provider before cancellation.
- Successful auxiliary inference does not auto-recover previously failed
  catalog health; explicit live probing remains the recovery path.
- No real model availability, model download/update, canonical durable
  maintenance acceptance, PostgreSQL persistence, UI behavior, live provider
  acceptance, deployment or overall HAI production readiness was verified.
  The earlier outcome ledger is preceding evidence, not proof of these edits.

Next step: parent formatting review, focused tests, complete package validation
and scoped race checks before integrating these changes into a wider readiness
assessment. Preserve the source-only distinction until that evidence exists.
