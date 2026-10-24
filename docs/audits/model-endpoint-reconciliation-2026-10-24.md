# Original model-client and calibration reconciliation

Reviewed main: `3b5afa697ebb0e72b01d59e3a04648a04317cd45`.
Prepared baseline: `e2a2728fd5161040f30e9fd89cf3b8c3b4744681`.

## Historical source disposition

The original modified `modelintelligence/calibration.go` lacks the current
explicit zero-success Wilson-bound return. Main already preserves the intended
acceptance-rate calculation while returning a stable, nonnegative plain zero
for zero accepted outcomes. Its existing regression covers totals 1 through 40
and a nonzero-success bound. No older formula should replace that fix.

The original modified `modelintelligence/http_openai.go` lacks main's rejection
of URL credentials, queries and fragments and its provider-specific allowance
for the configured internal Ollama service name. Those changes are already on
main and preserve the local endpoint boundary. No old client implementation
needs to be ported over them.

Both original source hashes still match the frozen inventory. All 26 Go source
and test files in the retained modelintelligence package matched the prepared
baseline and reviewed main before this repair; module files matched prepared.

## Reproduced endpoint defect and repair

Review of the newer endpoint gate exposed a separate boundary case: an empty
query delimiter (`?`) produces `ForceQuery` without a nonempty `RawQuery`, and
an empty fragment delimiter (`#`) does not produce a nonempty `Fragment`.
Consequently the prior guard accepted base URLs whose appended API paths became
query or fragment content instead of request paths.

Three regression methods were added to the existing service test file:

- Reject empty `?`, `#` and `?#` suffixes.
- Preserve ordinary endpoint paths and encoded question/hash path characters.
- Require invalid LocalAI configuration to remain not-configured and make no
  HTTP call to an owned loopback test server.

The red run reproduced all six invalid-suffix subcase failures across the
validator and registry tests. The ordinary/encoded-path cases passed. The
targeted repair rejects `ForceQuery` and a trailing literal `#` while retaining
the existing credential/query/fragment checks. It adds no dependency and does
not change valid endpoint profiles or provider execution policy.

## Verification and limits

The complete modelintelligence package passed with the production/test repair
supplied through a native Go overlay: 86 root tests and 156 test cases including
subtests; no skips or failures. The runner checked that all three new root tests
actually executed and that the expected package passed, so an empty test
selection cannot count as success. Go's default test vet checks were retained.

Go 1.27.3 ran offline with read-only module metadata, CGO disabled, one build
worker, GOMAXPROCS=2 and a 512 MiB GOMEMLIMIT soft target. Fixtures use in-process
state and owned loopback HTTP servers, not external model accounts. No container,
real database or paid provider was used. The red build cache was reused for green
and then cleaned natively: 230,609,798 logical bytes became 228 bytes. This is
cleanup of verification overhead, not an equivalent net reclamation claim.

Overlay hashes stayed stable during execution. All 28 retained package/module
inputs and both original inputs were unchanged afterward. Neither original
checkout nor retained module source was edited by the test overlay.

The fix is prepared locally; it is not claimed published, merged, deployed or
accepted against a real provider. This review does not cover other original
source variants, transitive-source equivalence, the full backend/frontend,
browser behavior, PostgreSQL, race-detector acceptance or whole-archive cleanup.
No original source file became deletion-eligible solely from this result.
