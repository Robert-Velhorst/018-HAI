# Safe Outcome Consumer Integration Handoff

Date: 2026-10-01. Source-only integration work; tests and production acceptance
remain pending the parent's serial verification.

## Scope And Preservation

Read `output/background-outcome-production-20261001.md`, the current
`background.SafeOutcome`/receipt/error contract, safe-worker output and
verification types, claim transition/recovery behavior, the three assigned
consumers, existing fixtures, error/redaction helpers and frontend response
callers. Used the installed Go-pattern/testing guidance already reviewed in
this chat. No provider/account/browser connector was applicable.

Incremental edits were confined to the assigned production files and narrowly
named new tests. Earlier model edits and the shared dirty checkout were left
untouched. Existing handler/runtime-lab changes were preserved; their complete
Git diffs must not be attributed to this continuation. No worker, operations,
source, task, pursuit or background production file was edited. No descendants,
tests, builds, installations, Docker/runtime process operations, private
localhost operations, live account calls, commits, pushes or deletes occurred.
No README was edited and no credentials were printed.

## Established Findings And Fixes

1. **Broker discarded supplied worker output on `Run` error.** The completion
   adapter now returns that exact `SafeWorkerOutput` with `OK=false`, zero
   verification and the unchanged original error. It does not call verification
   or dispatch again on error. The private finish helper makes this previously
   unreachable partial-output branch testable without modifying the concrete
   worker or adding an execution override. Authorization/error-chain identity
   remains intact: retained effect evidence still defeats background's
   pre-effect refusal classification.

2. **Phase 2 discarded `SafeOutcome.Operation`/`Receipt` on error and echoed raw
   joined errors.** Run responses now preserve a redacted receipt and operation
   snapshot, real operation correlation, `interrupted`,
   `reconciliationRequired` and `outcomeRecorded`. An execution error cannot
   promote `verified`, even if receipt-level verification or a contradictory
   flag says true. Errors use stable public messages; operation snapshots do
   not echo raw LastError. Structured redaction preserves existing operation
   shape and exact numbers. If oversize redaction discards the full snapshot,
   the real ID/status/verification-status fields remain available. Original
   operation/receipt evidence is not mutated by display redaction.

3. **Runtime Lab discarded receipt snapshots and collapsed post-effect errors
   into `failed`.** Attempts now retain redacted receipt snapshots, operation
   status, interruption and explicit reconciliation/recording fields. Unknown,
   interrupted and result-plus-error outcomes are `inconclusive`, not verified
   success or known retryable failure. Pre-effect claim/storage diagnostics use
   stable messages; the known claim-conflict sentinel remains distinguishable.
   Record/list methods copy receipt snapshots to prevent caller mutation of the
   retained in-memory attempt.

4. **Both consumers released claims after post-effect persistence failures.**
   They now leave nonterminal post-execution claims fenced when a receipt or
   uncertainty/completion evidence is returned. Existing lease-expiry recovery
   remains available instead of creating unleased running/verifying records.
   Background's successful terminal transition already releases its claim.
   Ordinary pre-execution refusals retain the existing bounded cleanup path.
   No claim, retry, approval, recovery or execution authorization rule was
   relaxed or upgraded.

5. **Recovered Runtime Lab attempts lost interrupted classification and could
   advertise verification from a passed flag on a noncompleted operation.**
   Interrupted records project `inconclusive` with reconciliation required.
   Recovered receipt snapshots are decoded from the existing execution-state
   slot; raw LastError is not displayed. Explicit uncertain, malformed/null,
   incomplete, or contradictory receipt evidence overrides a completed label.
   Receipt-bearing awaiting-approval recovery remains review-required. A
   historical completed/passed record without the new receipt slot retains its
   existing legacy interpretation; no missing receipt is invented or backfilled.

## Meaning Of Returned Fields

- `receipt` is observed artifact evidence, with display-only secret redaction
  and bounded output. Its worker-level flags are not operation completion or
  permission to execute/retry.
- `verified` / `verificationPassed` require the successful operation-completion
  contract and no conflicting returned error/interruption.
- `interrupted` reflects the background transition flag or a recovered
  interrupted operation, not a guessed successful write after persistence error.
- `outcomeRecorded` acknowledges a reported terminal repository transition or
  recovered terminal label. It does not certify PostgreSQL durability, artifact
  receipt durability, valid completion, or storage surviving a restart.
- `operationSnapshot` identifies returned snapshot material, not an independent
  read confirming that every field in the snapshot was persisted.

## Changed Paths

- `backend/internal/executionbroker/broker.go`
- `backend/internal/executionbroker/broker_partial_outcome_test.go` (new)
- `backend/internal/phase2/handler.go`
- `backend/internal/phase2/safe_outcome_response_test.go` (new)
- `backend/internal/runtimelab/service.go`
- `backend/internal/runtimelab/safe_outcome_attempt_test.go` (new)
- `output/safe-outcome-consumers-production-20261001.md` (new)

## Regressions Added, Not Run

- Broker partial-output retention, no fabricated verification, unchanged error
  identity including joined authorization/post-effect errors, bounded redaction
  and nonmutation of source evidence. These pure tests supply output doubles;
  they do not claim that the unchanged worker currently returns real partial
  output on its error paths.
- Handler receipt/state preservation, failed-versus-interrupted distinction,
  contradictory verified flag plus error, secret/raw-error suppression,
  exact-number retention and correlation preservation after oversize redaction.
- Runtime Lab partial receipt/state retention, inconclusive classification,
  contradictory/missing outcome handling, interrupted/contradictory/malformed
  recovered evidence, and immutable returned receipt snapshots.
- Consumer integration fixtures inject errors at both verifying and completed
  ledger writes after the existing authorized safe-worker path. Assertions
  retain the returned artifact hash, refuse verification/recorded-interruption
  claims, retain the live recovery fence and refuse ordinary retry. These tests
  will create bounded temporary artifacts only when the parent runs them; they
  were not executed in this continuation.

## Actual Verification

- Source review of types, consumers, read-only claim/lease/recovery rules,
  production wiring and the existing test fixtures.
- `git diff --check -- backend/internal/executionbroker/broker.go
  backend/internal/phase2/handler.go backend/internal/runtimelab/service.go`:
  exit 0; only LF-to-CRLF notices.
- Trailing-whitespace scan of the three new regression files: no matches,
  exit 0.

No Go formatter, compiler, test runner, race runner, database integration or
browser was run. Source/whitespace review is not executable verification.

## Parent Commands

Run serially from `backend`, using the parent's existing approved cached
toolchain/test environment and established memory limits. Do not install tools,
download modules/toolchains, or target an existing user database.

```text
gofmt -d internal/executionbroker/broker.go internal/executionbroker/broker_partial_outcome_test.go internal/phase2/handler.go internal/phase2/safe_outcome_response_test.go internal/runtimelab/service.go internal/runtimelab/safe_outcome_attempt_test.go
go test -p 1 -parallel 1 -count=1 -timeout 90s ./internal/executionbroker -run '^TestBroker(PartialOutcome|PublicReceipt)'
go test -p 1 -parallel 1 -count=1 -timeout 120s ./internal/phase2 -run 'TestSafeOutcomeResponse|TestRunOperationFinalizationFailure'
go test -p 1 -parallel 1 -count=1 -timeout 120s ./internal/runtimelab -run 'TestSafeOutcomeAttempt|TestSafeWorkerSelfTestFinalizationFailure'
go test -p 1 -parallel 1 -count=1 -timeout 180s ./internal/executionbroker ./internal/background ./internal/phase2 ./internal/runtimelab
go vet -p 1 ./internal/executionbroker ./internal/phase2 ./internal/runtimelab
```

Review and apply formatting only within the six assigned source/test files,
preserving any concurrent edits. Example low-memory/no-download settings are
`GOMAXPROCS=1`, `GOMEMLIMIT=384MiB`, `GOTOOLCHAIN=local`, `GOPROXY=off`,
`GOSUMDB=off`. Race/integration verification remains the parent's choice within
its approved isolated environment. These are commands to run, not passed results.

## Remaining Limits

- The unchanged concrete worker currently returns zero output on its own error
  paths, including possible post-effect failures. The broker preserves only
  what it actually receives; it does not reconstruct or fabricate artifacts.
- Receipt retention in an HTTP response or in-memory Runtime Lab attempt does
  not make a failed first post-effect ledger write crash-durable. A restart can
  lose an unpersisted receipt. Ledger state, claim expiry and operator artifact
  reconciliation remain separate evidence/recovery mechanisms.
- Redaction follows the existing shared helper's recognized secret formats.
  New run/attempt projections do not establish a privacy audit of every other
  operation-list/detail endpoint or diagnostic surface.
- Frontend receipt rendering/types, background-run reports and other consumers
  remain unchanged/read-only. Additive API fields are present in source; no
  browser acceptance or user-visible reconciliation workflow was verified.
- No PostgreSQL claim/recovery behavior, live provider/account acceptance,
  deployment, restart survival or full production readiness was verified.

Next step: parent formatting review and serial focused/full package checks,
then evaluate these consumer results together with the background worker's
report and actual test evidence before making integration/readiness claims.
