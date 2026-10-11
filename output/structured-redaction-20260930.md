# Structured Secret Redaction - F1

## Outcome and Ownership

The shared-redactor defect described in Finding 1 of `output/production-safety-followup-20260930.md` is fixed. The synthetic access/refresh-token JSON now returns:

```json
{"access_token":"[REDACTED]","refresh_token":"[REDACTED]"}
```

Authored files are limited to `backend/internal/safety/redaction.go`, `backend/internal/safety/redaction_test.go`, and this report. The original finding report, automation service/tests, projections, and callers were not edited. Existing dirty changes were preserved. Work began on 2026-09-30 and continued after midnight on 2026-10-01 Europe/Amsterdam.

## Implementation and Caller Contract

- Exported signatures remain `RedactSecrets(string) string` and `RedactURL(string) string`; no caller changes or dependencies are required.
- Complete JSON uses standard `encoding/json`, recursively sanitizing objects and arrays. A sensitive field's entire value is replaced, including strings, objects, arrays, numeric values, booleans, and null. Case-insensitive sensitive names cover password/passwd/pwd, secret, token, API-key variants, key, and authorization, including access/refresh tokens and client secrets. Unicode-escaped field names are decoded before classification.
- `Decoder.UseNumber` preserves large integers and numeric lexemes instead of passing through floating point. Ordinary fields retain their JSON values and types; quotes, backslashes, and escaped strings are decoded and encoded correctly. Formatting/key order are canonicalized; duplicate JSON keys follow the standard decoder's last-value semantics, not byte-for-byte preservation.
- Ordinary string fields still receive existing bearer/provider/private-key and assignment redaction, including JSON embedded in a string. These three credential-pattern expressions are unchanged. URL userinfo removal and sensitive-query filtering remain, with additional pwd/hyphenated API-key coverage.
- Mixed, invalid, truncated, and newline-delimited logs use quoted-key-aware matching. Standard JSON decoding classifies escaped quoted keys; a bounded escape-aware scan consumes the complete quoted secret or secret container. Single-quoted log assignments are also supported. Unterminated quoted/container secrets, mismatched containers, and excessive fallback container depth discard the uncertain tail instead of retaining secret suffixes.
- Resource contract: input and final output are bounded at 1,048,576 bytes. Oversize input or expanded output returns `[REDACTED_SIZE_LIMIT]`, with no retained source prefix. Recursive sanitization replaces a subtree reached at depth 64 with `[REDACTED_DEPTH_LIMIT]`; fallback container scanning has the same 64-level stack bound. Standard JSON parsing has its own depth guard; rejected input goes through the fallback. An unexpected validated-JSON decode/encode failure returns `[REDACTED_JSON_ERROR]`. Size/error markers are log text, not a promise of valid JSON.
- Traversal is bounded; fallback scans do not repeatedly decode every input suffix. No custom serialization framework, mutable global state, goroutines, configuration, or external I/O was added to the redactor.

Shared-file handoff for the separately owned automation work: redact the complete captured output before display truncation, using the unchanged API. Accept fail-closed markers; do not restore raw output on a marker or parse error. Keep projection, returned/saved launch-event regressions, and any upstream response-read bounds in that separate ownership. This report does not claim those caller changes or tests were completed here.

## Verification Evidence

Current source and synthetic tests are the evidence, not historical passing records. Used direct repository reads, the installed Go-testing skill, PowerShell, the existing cached Go image, and offline module/build caches. Memory supplied only the historical disposable-container verification hint. No connector, browser, provider, private environment file, or internet source was needed.

| Phase | Exact Go command | Observed result |
| --- | --- | --- |
| Red, new regression tests against original implementation | `go test -p=2 -count=1 -v ./internal/safety` | Exit 1; `FAIL automation-hub-backend/internal/safety 0.312s`. Six structured-JSON and fourteen mixed/malformed subcases failed (20 total), including exact F1. Existing assignment/bearer/provider/URL fixtures passed. |
| First green | `go test -p=2 -count=1 -v ./internal/safety` | Exit 0; `ok automation-hub-backend/internal/safety 0.281s`; all initially added cases passed. |
| Added truncation/resource/fuzz-seed cases | `go test -p=2 -count=1 -v ./internal/safety` | Exit 0; `ok automation-hub-backend/internal/safety 0.581s`. |
| Final expanded suite, race and three repetitions | `go test -p=2 -race -count=3 -v ./internal/safety` | Exit 0; `ok automation-hub-backend/internal/safety 5.785s`; no race report. Includes 20 top-level tests, 11 structured subcases, 15 mixed/malformed subcases, 5 resource subcases, every byte cut of an escaped synthetic secret, and 5 fuzz seeds. |
| Explicit static analysis | `go vet -p=2 ./internal/safety` | Exit 0; no diagnostics. |
| Bounded generated-input fuzz run | `go test -p=2 -run=^$ -fuzz=^FuzzRedactSecretsQuotedValues$ -fuzztime=10s -parallel=2 ./internal/safety` | Exit 0; 16,503 executions, 31 new interesting inputs, `PASS`, `ok automation-hub-backend/internal/safety 11.064s`. |
| Repeat on final formatted, hashed Go files | `go test -p=2 -race -count=3 ./internal/safety` | Exit 0; `ok automation-hub-backend/internal/safety 6.071s`; no race report. |

Structured and mixed tables assert absence of synthetic secrets, preserved nonsecret values, deterministic repeated calls, and idempotent sanitization. Fuzz properties require exactly the sanitized password/name document for both complete JSON and an INFO-prefixed fragment, even for arbitrary quoted/escaped input values. Existing safety/emergency-stop fixtures also ran, without being edited.

Test containers used this PowerShell launch envelope, followed by each command above:

```powershell
docker run --rm --pull never --network none --read-only `
  --cap-drop ALL --security-opt no-new-privileges --cpus 2 --memory 1g `
  --tmpfs /tmp:rw,exec,nosuid,size=512m `
  --mount "type=bind,source=$((Resolve-Path backend).Path),target=/src,readonly" `
  --mount type=volume,source=hai-go-modcache,target=/go/pkg/mod,readonly `
  --mount type=volume,source=hai-go-buildcache,target=/root/.cache/go-build `
  -e GOMAXPROCS=2 -e GOTOOLCHAIN=local -e GOPROXY=off -e GOSUMDB=off `
  -e GOFLAGS=-mod=readonly -e GOMODCACHE=/go/pkg/mod `
  -e GOCACHE=/root/.cache/go-build -w /src golang:1.25.13 <command>
```

Cached image ID: `sha256:14e75143c833c7398ea3a5e4c673aeaae35f40e781e6b060e2f97b72c475c975`. `go version` exited 0 with `go version go1.25.13 linux/amd64`. Only scratch/build-cache writes are permitted during tests; module/source/root mounts are read-only. No image pull, install, published port, live account, database, installed-service mutation, descendant agent, commit, or push occurred.

Formatting note: an initial `gofmt -w` attempt with only the two owned files writable exited 1 because its sibling temporary file required a writable parent directory. No additional host file was created; the safety directory still contains its original five files. A subsequent read-only `gofmt -d` identified only declaration alignment, applied manually within the owned source file. Final read-only `gofmt -l /src/redaction.go /src/redaction_test.go` exited 0 with no filenames, confirming both files are formatted. `git diff --check` passed with only existing Windows LF/CRLF notices.

## Boundaries and Snapshot

This proves the shared helper's synthetic behavior, not end-to-end automation persistence, historical-record cleanup, production deployment, arbitrary unlabeled-secret detection, or provider acceptance. Caller/projection fixes and regressions remain with their separate owner. Fail-closed size/depth/tail policies intentionally discard uncertain or excessive nonsecret context rather than leak credentials. The heavily dirty concurrent workspace is not represented as a committed revision.

SHA-256 after the formatting-only alignment change:

```text
0FBC17F3B1F7256C249ADD895BF2BF6399B2F33BE9069DCBAEDE4BC4C02E4984  backend/internal/safety/redaction.go
0ED64760F437BAE2BE1625971D9FAA18094E32423EBD8A618C5C970F106008D3  backend/internal/safety/redaction_test.go
```
