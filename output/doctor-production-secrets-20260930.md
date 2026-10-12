# F3: Production Doctor Secret Minimum

Date: 2026-09-30; verified around 23:42 Europe/Amsterdam (21:42 UTC).
Checkout HEAD: e07b9daeb3ba2630ecbe12a64d948f791203c241.
Status: scoped fix stable for inclusion in the parent's backend image build.
The parent was notified after focused regressions passed, before remaining verification.

## Exact Owned Changes

- `backend/internal/doctor/doctor.go`: trim each of the three configured secrets; retain empty/placeholder diagnostics; fail production values shorter than 32 bytes.
- `backend/internal/doctor/doctor_test.go`: replace the three short healthy-fixture values with distinct 32-byte synthetic values. Default production mode is preserved.
- `backend/internal/doctor/secrets_test.go`: add production, run-mode, and rendering regressions.
- `output/doctor-production-secrets-20260930.md`: this new handoff with commands and output evidence.

Owned paths had no changes at initial inspection. The shared worktree was already heavily dirty. No other files were edited, staged, committed, pushed, or reverted by this task.

## Behavior And Inspected Call Sites

F3 was reproduced through Diagnose: each of JWT_SECRET, BACKEND_API_SHARED_KEY, and HAI_MEMORY_ENCRYPTION_KEY accepted a one-byte non-placeholder value in production.

The new minimum is `len(strings.TrimSpace(value)) >= 32`, in Go string bytes (UTF-8 for multibyte fixtures), not character count. Leading/trailing whitespace cannot pad a short key. Hex strings are not decoded by this check; length is not an entropy assessment.

Empty and shipped-placeholder values still fail production with existing specific diagnostics. Placeholder checks precede length checks, even for long placeholders. Explicit demo/test modes still accept short non-placeholder values and warn, not fail, for empty/placeholder values. Empty or unknown modes still resolve to production. No approval-proof, database-password, or mode-parser policy changed.

Call sites inspected, not edited or tested as running integrations:

- `backend/cmd/main.go:24`: doctor CLI diagnosis/render/exit.
- `backend/internal/router/router.go:39`: startup guard refuses failures.
- `backend/internal/router/routes.go:136`: /readyz combines diagnosis and dependency probes.
- `backend/internal/router/routes.go:937`: system diagnostics.
- `backend/internal/router/routes.go:953` and `backend/internal/memoryengine/service.go:137`: memory-secret selection and trimmed-secret SHA-256 derivation; neither changed.

Direct local source inspection and the disposable Docker runner were used. The diagnosing-bugs skill informed the red/green regression loop; broader hypotheses/instrumentation were unnecessary for this reproduced missing guard. The two-axis code-review skill was inspected but not applicable; no branch review or descendants were requested. No browser, remote account, or deployment access was needed.

## Disposable Runner

Existing image: golang:1.25.13, Linux/amd64.
Image ID: sha256:14e75143c833c7398ea3a5e4c673aeaae35f40e781e6b060e2f97b72c475c975.
Observed output: `go version go1.25.13 linux/amd64`.

Every Go invocation used this exact common PowerShell prefix, followed by a command listed below:

```powershell
docker run --rm --network none --read-only --cap-drop ALL --security-opt no-new-privileges --cpus 2 --tmpfs /tmp:rw,exec,nosuid,size=512m --mount "type=bind,source=$((Resolve-Path backend\go.mod).Path),target=/src/go.mod,readonly" --mount "type=bind,source=$((Resolve-Path backend\go.sum).Path),target=/src/go.sum,readonly" --mount "type=bind,source=$((Resolve-Path backend\internal\doctor).Path),target=/src/internal/doctor,readonly" --mount "type=bind,source=$((Resolve-Path backend\internal\config).Path),target=/src/internal/config,readonly" --mount "type=bind,source=$((Resolve-Path backend\internal\demomode).Path),target=/src/internal/demomode,readonly" --mount type=volume,source=hai-go-modcache,target=/go/pkg/mod --mount type=volume,source=hai-go-buildcache,target=/root/.cache/go-build -e GOMAXPROCS=2 -e GOTOOLCHAIN=local -e GOFLAGS=-mod=readonly -e GOMODCACHE=/go/pkg/mod -e GOCACHE=/root/.cache/go-build -w /src golang:1.25.13
```

Only doctor/config/demomode sources and go.mod/go.sum were mounted, readonly, excluding credential files. Container root was readonly; /tmp was executable writable tmpfs (512 MiB). Existing hai-go-modcache and hai-go-buildcache volumes were writable. Concurrency: GOMAXPROCS=2, -p 2, -parallel 2; vet used -p 2. Containers were disposable (--rm), network-disabled, privilege-restricted, and two-CPU-limited. No host Go was used; no services were installed/restarted.

## Exact Commands And Outcomes

All executions were on 2026-09-30. Test-result caching was disabled with -count=1 or -count=10. Below are exact command suffixes after the common prefix.

Pre-fix red run, exit 1; all 12 selected leaf cases failed, package 0.013s:

```text
go test -p 2 -parallel 2 -count=1 -v -run '^TestDiagnoseProductionSecretMinimumBytes/(JWT_SECRET|BACKEND_API_SHARED_KEY|HAI_MEMORY_ENCRYPTION_KEY)/(one_byte|ascii_31_bytes|padded_short|multibyte_31_bytes)$' ./internal/doctor
```

Post-fix focused run, exit 0; PASS, package 0.023s:

```text
go test -p 2 -parallel 2 -count=1 -v -run '^(TestDiagnoseProductionSecretMinimumBytes|TestDiagnoseSecretRunModeSemantics|TestRenderProductionSecretFailureDoesNotExposeValue)$' ./internal/doctor
```

Full doctor package, exit 0; canonical summarized run 0.147s:

```text
go test -p 2 -parallel 2 -count=1 -json ./internal/doctor
```

Race detector, exit 0; output `ok automation-hub-backend/internal/doctor 1.188s`:

```text
go test -race -p 2 -parallel 2 -count=1 ./internal/doctor
```

Ten repeated focused runs, exit 0; output `ok automation-hub-backend/internal/doctor 0.050s`:

```text
go test -p 2 -parallel 2 -count=10 -run '^(TestDiagnoseProductionSecretMinimumBytes|TestDiagnoseSecretRunModeSemantics|TestRenderProductionSecretFailureDoesNotExposeValue)$' ./internal/doctor
```

Additional container commands, all exit 0; vet and gofmt produced no output:

```text
go version
go vet -p 2 ./internal/doctor
gofmt -l internal/doctor/doctor.go internal/doctor/doctor_test.go internal/doctor/secrets_test.go
```

Host `git diff --check -- backend/internal/doctor` exited 0 with only Git LF-to-CRLF working-copy warnings, no whitespace errors. gofmt also checked the new untracked test file.

## Captured Log Evidence

Pre-fix excerpts (the same failures also occurred for ASCII-31, padded-short, and multibyte-31 cases):

```text
=== RUN   TestDiagnoseProductionSecretMinimumBytes/JWT_SECRET/one_byte
    secrets_test.go:61: security.jwtSecret severity = ok (found=true), want fail
=== RUN   TestDiagnoseProductionSecretMinimumBytes/BACKEND_API_SHARED_KEY/one_byte
    secrets_test.go:61: security.backendApiKey severity = ok (found=true), want fail
=== RUN   TestDiagnoseProductionSecretMinimumBytes/HAI_MEMORY_ENCRYPTION_KEY/one_byte
    secrets_test.go:61: security.memoryEncryptionKey severity = ok (found=true), want fail
FAIL
FAIL automation-hub-backend/internal/doctor 0.013s
```

The canonical full-package JSON output was captured in memory by PowerShell and parsed with ConvertFrom-Json; no separate log files were written. Complete top-level pass listing and counts:

```text
PASS TestDiagnoseHealthyConfigHasNoFailures
PASS TestDiagnoseFlagsMissingDatabaseAsFailure
PASS TestDiagnoseInvalidPortFails
PASS TestDiagnoseEmptySecretsWarnButDoNotFail
PASS TestDiagnoseEmptySecuritySecretsFailInProduction
PASS TestDiagnoseFailsPlaceholderDatabasePasswordInProduction
PASS TestDiagnoseMissingOrWeakApprovalProofKeyFails
PASS TestRenderReportsReadinessAndExitCode
PASS TestRunProbesReportsReachableDependencyAsOK
PASS TestRunProbesCriticalFailureIsFail
PASS TestRunProbesNonCriticalFailureIsWarn
PASS TestRunProbesTimesOutInsteadOfBlocking
PASS TestRunProbesTimesOutEvenWhenProbeIgnoresContext
PASS TestRunProbesPreservesOrderAcrossConcurrentProbes
PASS TestMergeCombinesConfigAndLiveChecks
PASS TestValidConfigWithDeadDatabaseIsNotReady
PASS TestIsPlaceholderSecret
PASS TestDiagnoseFlagsPlaceholderSecrets
PASS TestDiagnosePlaceholderSecretIsWarnOutsideProduction
PASS TestDiagnoseProductionSecretMinimumBytes
PASS TestDiagnoseSecretRunModeSemantics
PASS TestRenderProductionSecretFailureDoesNotExposeValue
TOP_LEVEL_PASSED=22 SECRET_MATRIX_LEAF_PASSED=153 ALL_TEST_PASS_EVENTS=217
{"Time":"2026-09-30T21:41:47.183670776Z","Action":"output","Package":"automation-hub-backend/internal/doctor","Output":"ok  \tautomation-hub-backend/internal/doctor\t0.147s\n"}
```

SECRET_MATRIX_LEAF_PASSED comprises 45 production-secret cases and 108 run-mode cases. There are also three rendering subtests, each evaluating two values. ALL_TEST_PASS_EVENTS includes grouping events and is not an independent-case count.

Coverage: generated random hex keys at 32/64 encoded bytes; ASCII 1/31/32/33-byte boundaries; empty and ASCII/Unicode whitespace; padding; multibyte strings at 31/32 bytes; short/long/case-insensitive placeholders; default/unknown/production and explicit demo/test variants; report failure count/exit behavior; rendering NOT READY without exposing tested secrets. Generated values were confined to test memory, never printed or installed.

## Verified Source Fingerprints

SHA-256 of verified source bytes, not Git-normalized bytes:

```text
65336FA649627553DD6E3D067094046B535CA91E5177F85ED44DF762023015C0  backend/internal/doctor/doctor.go
14546FB18D81C2AD8CAB330E0AF3249933348B7DFEFCFBA42EE90ED5BF4B1A2F  backend/internal/doctor/doctor_test.go
0F0C76726BB2DE069D558A71AE67AF39F1289BDE90EA6F10D86D07A82F31E766  backend/internal/doctor/secrets_test.go
```

## Limitations And Operational Handoff

- This verifies the doctor package, not the entire backend suite, router/HTTP integration, installed configuration, image build, ownedstack, or live provider/deployment acceptance. Call-site propagation was inspected in source, not exercised against running services. Parent F2/manual-control results are parent-reported context, not verification performed here.
- No personal .env credential files were opened or mounted; installed key strengths were not measured. No installed keys, encryption data, accounts, services, permissions, or deployment state were changed.
- Existing short production keys now fail startup/readiness diagnosis. Image inclusion does not prove installed configuration acceptance.
- Do not silently rotate HAI_MEMORY_ENCRYPTION_KEY: the secret determines the existing data-encryption key. Replacement requires an explicitly approved migration/re-encryption plan to preserve access to retained memory. Rotation remains outside this task.
- Parent next step: include these stable source bytes in its backend image build, then perform its separately authorized ownedstack/configuration acceptance. No further scoped production-code edits are planned.

