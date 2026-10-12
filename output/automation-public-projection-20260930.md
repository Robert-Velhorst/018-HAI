# Automation Public Projection - 2026-09-30

## Outcome

F2 is remediated in the automation HTTP responses: list, detail, diagnostics, Create, and Update use sanitized copies instead of serializing credential-bearing execution configuration. The current Home editor can round-trip the exact visible configuration and change non-secret fields without overwriting stored credentials with masks. Modified masked values fail explicitly with HTTP 400 before writes.

Verification completed on 2026-10-01 shortly after midnight Europe/Amsterdam. This report retains the requested 20260930 filename. The automation patch was announced stable for a possible backend-only isolated rebuild after its focused regressions passed. Full automation race/repeated tests and vet subsequently passed. This is local synthetic verification, not a deployment or production-readiness claim.

## Authored Scope

- Modified only `backend/internal/automation/automation_handle.go`, `automation_health_handler.go`, and `automation_service.go`.
- Added only `backend/internal/automation/public_projection.go`, `public_projection_test.go`, and this report.
- The checkout was already heavily dirty. Existing automation work was preserved; the complete Git diff includes earlier work and must not be attributed to this fix.
- Read the F2 report, relevant automation/model/approval/redaction code, the Home form submission path, and local tool/test conventions. Used the available file/patch tools and existing Docker Go toolchain. No connected source or live provider was relevant. No descendants were created; the multi-agent review workflow was not applicable to the ownership boundary.
- Did not edit `backend/internal/safety/redaction*.go`. Its unchanged `RedactSecrets`/`RedactURL` APIs are consumed here; the separately owned JSON fix was present during successful verification.
- No frontend, CI, launcher, other documentation, approval/auth/RBAC model, secret-store architecture, personal environment, shared service, live provider, commit, merge, or deployment changes.

## Projection and Edit Contract

`public_projection.go:16` creates an independent automation copy. `launchTarget` uses the existing method-aware `redactLaunchTarget`; `publicUrl`, `localUrl`, and `healthCheckUrl` use `safety.RedactURL`. Known secrets in dependency notes and failure text use `RedactSecrets`. Public copies omit the upload-only file pointer. IDs, HTTP method, URL host/path/non-secret query parameters, host/port, ordering, route/service/runtime metadata, status, timings, counters, and timestamps remain available.

Handlers project list/detail and both write responses. Service list/detail records stay raw for internal use; launch, approval, and execution logic continue reading raw repository configuration. Diagnostics sanitize copied health/launch history, output/messages/audit events, and runtime route traces at the service boundary and again at the HTTP boundary. Applying diagnostic defaults cannot mutate a repository-owned pointer. Historical records are not rewritten or deleted.

`public_projection.go:78` defines compatibility without introducing frontend fields:

1. An incoming field exactly equal to the current raw field's public projection preserves the entire current raw field. This handles URL userinfo removal, which does not leave a mask, as well as encoded query masks.
2. A different complete unmasked field replaces that field. A caller changing a masked URL's non-secret path/query must submit the complete replacement, not a partially masked URL.
3. Any different field containing a literal or URL-encoded redaction marker is rejected. Arbitrary placeholders and altered masked URLs do not silently preserve credentials or become real credentials.
4. New automations reject masked execution/configuration fields. HTTP Create/Update errors explain the need for an unchanged current display value or a complete unmasked replacement.

Preservation runs inside Update immediately after loading the current raw record and before image work, slug changes, persistence, or event publication. The existing editor still posts all visible fields; a notes edit with unchanged projected URLs succeeds. Position and existing operational state remain server-owned as before. Approval digests are computed from original executable configuration, never from the projection. Repository update errors now return before dereferencing/publishing a missing updated record.

## Output and Persistence

- `automation_service.go:2958`: `trimOutput` calls `RedactSecrets` on the complete supplied body before enforcing the display byte limit. Nested and escaped JSON therefore reaches the structured redactor intact.
- API and Docker response readers capture up to 1 MiB plus a sentinel byte. Complete bounded bodies are sanitized and then limited to 4096 display bytes. Oversize or incomplete transport capture returns an explicit omission marker, never an unsafe raw prefix.
- Script capture is separately bounded at 1 MiB; its existing 1024-65536 byte display setting applies after redaction. Capture overflow withholds output and records that omission. Normal status/exit/duration metadata remains available.
- Existing launch outcome, returned launch result, replay result, failure-state, audit, and route-trace sanitization continues using the shared helper before persistence/return. No approval inputs were redacted.
- Health failure text is sanitized before state/history persistence and result return; health event targets use `RedactURL`. New HTTP request failures render the URL through `RedactURL`, so userinfo is not retained in an error message. API failure messages use the same request-error renderer.
- Fresh runtime-stop messages and audit events are sanitized before the persisted stop outcome and returned result, matching replay behavior.

## Regression Evidence

Before wiring the projections, the new fake-handler viewer GET regression failed on list, detail, and diagnostics, directly reproducing raw synthetic URL-userinfo/query disclosure.

After the fix, all 10 new top-level regression tests passed (focused run: 0.121 s):

- Viewer list/detail/diagnostics contain no distinct synthetic credentials while retaining method, path, non-secret query, host/port, status, and latency. Raw repository configuration, historical records, and approval digest remain unchanged.
- A repository returning the actual stored pointer still cannot be mutated by diagnostic defaults.
- Create/Update handlers sanitize even an injected service's raw response without mutating that response object.
- Editor round-trips preserve all four raw credential-bearing URL fields, including userinfo-only URLs, while accepting non-secret notes edits and preserving server-owned position.
- Altered literal/encoded masks are rejected with HTTP 400 and zero repository updates. Complete unmasked replacements persist verbatim; masked Create is rejected.
- Approved API execution still receives the original query credential after a public GET; the original approval action digest remains valid. Nested/escaped access token, refresh token, password, and client secret are absent from both returned and saved output for ordinary, >4096-byte, and >1-MiB response fixtures. Non-secret JSON metadata survives bounded normal responses.
- Direct trimming sanitizes escaped nested JSON before cutting its prefix. Failed health checks return/persist sanitized errors and targets while keeping the raw executable health URL and non-secret query metadata.

Viewer identity/role is injected through the pure handler harness; this is not a fresh JWT/router/browser authorization acceptance test. Network fixtures are loopback-only inside a disposable container with no external network.

## Reproducible Verification

Used locally cached `golang:1.25.13`, image ID `sha256:14e75143c833c7398ea3a5e4c673aeaae35f40e781e6b060e2f97b72c475c975`; runtime reported `go version go1.25.13 linux/amd64`.

The verification container used `--rm --pull never --network none --read-only --cap-drop ALL --security-opt no-new-privileges`; backend source was mounted at `/src` read-only. Existing `hai-go-modcache` and `hai-go-buildcache` volumes were mounted read-only. The warm build cache was copied into a private 6-GiB `/tmp` tmpfs; no shared cache writes were made. Environment: `GOTOOLCHAIN=local`, `GOPROXY=off`, `GOSUMDB=off`, `GOCACHE=/tmp/go-build`, `GOMAXPROCS=2`, `CGO_ENABLED=1`. Working directory: `/src`.

The fail-fast container script ran:

```sh
set -eu
cp -a /warm /tmp/go-build
go version
go test -mod=readonly -p 2 ./internal/automation -run 'Test(PublicAutomation|PublicDiagnostics|TrimOutput|LaunchJSONOutput|HealthCheckSanitizes)' -count=1 -v
go test -mod=readonly -p 2 -race ./internal/automation -count=3
go vet -mod=readonly -p 2 ./internal/automation
```

Results: focused regressions PASS; entire automation package race/count=3 PASS in 15.659 s; vet PASS with no diagnostics. Final script exit code: 0. Source was formatted with Go 1.25.13 gofmt, restricted to the five authored automation files. `git diff --check` for the three modified existing files passed; Git emitted only its LF/CRLF notices.

## Stable Snapshot

SHA-256 values were unchanged between focused verification and the post-verification check:

```text
ED1D13345E7632226154C0CAC5B42CCDD9AE9536AC53F4D581D5551C3115502D  backend/internal/automation/automation_handle.go
4A92F21D729E2CCC5CCFF4FD1CBD593EC74B10B8EDE0003089C3C1FF2AA0948C  backend/internal/automation/automation_health_handler.go
D7FDA4B982FEFF8E5987CDA6C0C8821E09508EFB69A091CE34110AA20DE713A4  backend/internal/automation/automation_service.go
DCA02085B491C707963D5EF29CF0B3C9E3921C5AFDD8B1C1332C959A451A5760  backend/internal/automation/public_projection.go
48A6E078BF4D59517DA4066FADCCC3CCE0837A8D16A4C203FDB4BFABA146A9EC  backend/internal/automation/public_projection_test.go
0FBC17F3B1F7256C249ADD895BF2BF6399B2F33BE9069DCBAEDE4BC4C02E4984  backend/internal/safety/redaction.go (separately owned; not edited here)
```

## Remaining Acceptance

The parent reported earlier browser sync-to-exact-job-completion/extraction evidence, action-bound workflow approval/negative-run checks, and 3815 passing backend top-level tests across 147 packages with 202 optional skips. Those are parent-provided results for an earlier snapshot, not this patch's verification.

Next action is the parent's optional isolated backend-only rebuild and final backend/browser rerun against this source snapshot plus the separately owned shared-redactor fix. No real database migration, historical secret cleanup, provider execution, credential rotation, deployment, or production acceptance was performed. Redaction is bounded to recognized secret patterns and configured URL fields; it is not a claim to identify every arbitrary secret in arbitrary text.
