# Execution and identity boundary reconciliation

## Retained source review

Reviewed these five retained files in full against the current implementation:

| File | Decision |
| --- | --- |
| `backend/internal/task/automation_executor.go` | Keep newer bounded context-aware approval registration/inspection/issuance, captured review configuration, pre-dispatch error classification, retained uncertain outcomes, correlation evidence and sanitized metadata. Existing task/owner/automation idempotency and exact decision binding remain represented. |
| `backend/internal/router/automation_permissions_test.go` | Keep signed role and verified owner propagation assertions, context-aware user-scoped stop evidence and newer admin-only event-delivery inspection/retry tests. |
| `backend/internal/router/openclaw_artifact_permissions_test.go` | Keep owner-scoped reads/retention and stronger admin-only forget permission; do not restore the older operator-forget expectation. |
| `backend/internal/router/identity_test.go` | Keep access-token type, issuer/audience/time validation, stable principal requirement, production secret fail-closed behavior, deliberate demo compatibility and cookie-bound IDP subject. |
| `backend/internal/router/legacy_control_plane_permissions_test.go` | Keep stronger configured-installation identity and signed scope checks, global pause restrictions, Runtime Lab route isolation, no-store identity preview and context-aware doctor callback. |

All retained behavior is represented or superseded; no older weaker control is
restored. Reviewing these files does not mark the large task service, complete
route registration or remaining archives as reconciled.

## Credential collection ambiguity

The router previously selected the first Authorization value or access-token
cookie. Seven regression cases reproduce accepted ambiguous collections,
including identical duplicates, conflicting values, an empty extra header and
two valid cookie identities. This does not demonstrate access without a valid
signed token; it demonstrates unsafe credential selection in an ambiguous request.

With identity verification configured, reject multiple Authorization values.
When cookie fallback is selected, reject multiple access-token cookies rather
than selecting an arbitrary first value. Reject with a generic 401 response
that does not echo credentials. Preserve existing explicit Bearer precedence,
single-cookie authentication, unrelated cookies and demo-mode behavior when
identity verification is deliberately disabled. Production signing-secret
configuration failure still takes precedence.

## Verification and preservation

- Full router, task and automation package tests pass before this additional
  credential correction; seven new negative cases fail against that baseline.
- The credential regression also retains single-header, single-cookie and
  explicit-Bearer-over-cookie positive controls, plus invalid-first rejection.
- All credential cases and the full router, task and automation packages pass
  after the correction. `git diff --check` also passes.
- These are handler/service fixtures, not live browser, IDP or provider acceptance.
- Original worktree files and archives remain intact. Semantic reconciliation
  and PR publication are not exact main-preservation or local-deletion proof.
