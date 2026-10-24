# Archived helper successor review

## Scope and preservation boundary

Reviewed against remote main `3b5afa697ebb0e72b01d59e3a04648a04317cd45`, freshly checked on 2026-10-24.
This receipt reviews five absent helper identities, not the complete archived files or the entire transcript corpus. No transcript, source, database, backup, or credentials were deleted. No external provider or deployment was changed.

The indexed archived additions were reread at their frozen offsets, with successful add-event type, SHA-256 content identity, archive size and before/after metadata verified by the existing archive-input helper. References below are abbreviated content fingerprints, not credentials.

## Authentication cookies

- Archived path: `idp/internal/app/authentication/cookies.go`; reference `1703aa672fae`.
- Historical identity: `setAuthCookie`.
- Current successors: `setAuthCookieWithPersistence` and `authCookie` in the same file.
- The historical HttpOnly, configured Secure, SameSite Strict, root path and persistent expiration behavior remain. New session-only behavior deliberately omits expiration when persistence is disabled; the original persistent wrappers still pass true.
- Retain the current implementation. Restoring the old unconditional-expiration helper would discard explicit session-only semantics.
- The source and `cookies_test.go` match main after repository Git filters. Raw working bytes differ through line endings; this is not a raw-byte disposal receipt.

## Criterion-specific execution evidence

- Archived path: `backend/internal/task/validation.go`; reference `3aad37d301e9`.
- Historical identity: `validationEvidence`.
- Current successor: `criterionValidationEvidence`, with explicit system checks in `validatePlan` and the retained `toolExecutionValidationEvidence` helper.
- Historical behavior collected execution status, aggregate counts, tool evidence and every claim source into one undifferentiated list.
- Current code selects evidence by criterion, skips review-needed or unverified claims, distinguishes generic provenance, and records verification/count checks separately. Therefore an absent old helper is not evidence of lost grounding functionality.
- Retain current criterion-specific validation. Do not restore the aggregate helper as a shortcut for unrelated criteria.
- The source and `validation_test.go` match main after repository Git filters. Raw working bytes differ through line endings.

## MCP preflight endpoint validation

- Archived path: `backend/internal/mcppreflight/service.go`; reference `c3d997fc7bd4`.
- Historical identity: `validateLocalURL`; current successor: `validateEndpointURL`.
- Scheme, credential, missing-host, query and fragment checks remain. Default admission remains localhost, loopback IPs and host.docker.internal.
- The successor separately supports explicitly configured remote endpoints only when the remote flag is true, HTTPS is used and a safe bearer token exists. This is operator configuration, not a default network expansion or authorization to invoke tools.
- Constructor code disables proxy inheritance and redirect following; preflight remains initialize/tools-list inspection, not permission to execute tools.
- Source and service tests match main after Git filtering. This review does not enable remote endpoints or claim a live endpoint is reachable.

## A2A planning bridge endpoint validation

- Archived path: `backend/internal/a2abridge/service.go`; reference `972bac9ba4b0`.
- Historical identity: `validateLocalURL`; current successor: `validateBridgeURL` plus `isLocalHost`.
- The historical code admitted private literal IPs and did not require the current exact API path. Current code restricts admission to loopback or explicitly named local gateway hosts, rejects encoded path variants and requires `/api/v1/a2a`.
- Keep the narrower current boundary. Reintroducing the archived validator would broaden access rather than recover a missing capability.
- Current service, service tests and private-network boundary regression tests match main after Git filtering. Configuration and planning remain token/owner-bound and non-executing.

## A2A message text extraction

- Archived path: `backend/internal/a2abridge/handler.go`; reference `e0b265985caf`.
- Historical identity: `taskText`; current successor: `taskInput`.
- Both normalize one to four nonempty text parts and enforce a 4096-rune limit. Current input uses the explicit protocol role, requires a valid message identity, rejects unsupported context/task IDs, metadata, extensions and non-text payload fields, and feeds the replay-protected Send path.
- Retain current protocol parsing and replay control, not the historical permissive JSON unmarshalling or obsolete message shape. Current handler tests explicitly reject obsolete shapes and ambiguous JSON-RPC requests.

## Verification

Both native Go 1.27.3 commands completed successfully with vet enabled: five cookie tests and seven task-validation tests passed, with no skipped cases. The commands used offline module resolution, readonly module files and bounded parallelism; no new dependencies were downloaded.

Cookie coverage: secure defaults, explicit insecure-local-HTTP setting, clearing access/refresh cookies, session-only security flags and persistent expiration.

Task coverage: initial checks are not premarked, verified execution records criterion evidence, unevidenced framework requirements fail, missing approval identity fails closed, unrelated aggregate evidence is rejected, claims map only to related criteria, and generic provenance cannot satisfy unrelated retention.

These are local unit-test results, not live authentication, browser, provider, database or release acceptance. No external services were contacted by these tests.

The complete MCP preflight and A2A bridge unit suites also completed successfully on native Go 1.27.3 with vet enabled: 15 MCP tests and seven A2A tests passed, plus ten nested A2A subtests; no skips. These tests use in-process handlers and ephemeral loopback fixtures, not live external agents. Coverage includes protocol handshakes, read-only tool inventories, token redaction, explicit remote opt-in/TLS/token checks, local-only bridge boundaries, non-executing planning, replay deduplication and malformed request rejection.

## Remaining work

Other declarations in these archived files still need their own review where the AST comparison reports changes. The `sortedIDs` historical planning helper was inspected but is not resolved by this receipt. No whole-file integration or archive deletion eligibility is asserted.
