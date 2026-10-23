# Identity and approval boundary source reconciliation

## Selected source scope

Reviewed twelve retained versions:

- Both model-maintenance and artifact worktrees: `jwt.go` and `jwt_test.go`
  under `backend/internal/identity`.
- Model-maintenance worktree:
  `backend/internal/executionapproval/composite_resolver.go`.
- Both worktrees: `errors.go`, `repository.go`, and `transactional.go` under
  `backend/internal/executionauth`.
- Artifact worktree: `backend/internal/executionauth/transactional_test.go`.

The retained identity versions have identical text after newline normalization.
Other meaningful version differences were inspected explicitly. This is not a
review of all authorization service, final-effect bridge, PostgreSQL repository,
or integration-test source versions.

## Retained boundaries

| Area | Keep rather than restoring older code |
| --- | --- |
| JWT validation | Bounded verification key ring, stable principal, canonical base64url, valid UTF-8, unique JSON keys, bounded nesting, unsupported critical-header rejection, time ordering and not-before checks. |
| Operator context | Retain the explicit operator-scope claim/context field, not an inferred expansion of permissions. |
| Approval routing | Preserve canonical namespaces, reject overlapping registrations, copy registration storage, and never fall back after the selected resolver rejects evidence. |
| Control changes | Keep the dedicated ops-control review resolver; absence fails closed. |
| In-memory authorization | Keep cancellation-aware lock acquisition, owner-scoped receipts, single-use approval claims, approval expiry and final-effect freshness checks. |
| Transaction binding | Require the same PostgreSQL pool and active transaction, propagate execution deadlines/context and bind mutable task-review approval resolution to that transaction. |
| Advisory projection | Keep post-commit projection separate from the authorization transaction and retain its original default context. |
| Isolation errors | Keep explicit errors for already-claimed approvals and unsupported approval-claim isolation. |

## Unicode duplicate-key repair

The duplicate-key guard lowercased names, while the Go JSON decoder matches
field names using Unicode simple folding. A signed token containing both `sub`
and its long-s variant was accepted even though those names address the same
decoded claim. Equivalent issuer and nested-extension cases also reproduced.

Use `unicode.SimpleFold` to choose a stable representative for each rune's fold
set, matching the standard library's field-name equivalence. Three signed-token
regressions failed before the repair. Pairwise checks compare the canonical key
equality against `strings.EqualFold`, including ASCII, long s, Kelvin sign and
Greek sigma forms. No dependency or signing-policy change is introduced.

This is a parser-ambiguity fix, not evidence that an unsigned or incorrectly
signed token could bypass authentication.

## Verification and preservation boundary

The complete identity, execution-authorization and execution-approval packages
passed locally with Go 1.27.3 on Windows:

```text
go test -p 1 -timeout 180s ./internal/identity ./internal/executionauth ./internal/executionapproval -count=1
```

Local package tests and real PostgreSQL execution are separate gates. Database
cases requiring a test DSN are not claimed as executed merely because ordinary
package tests pass. Original versions remain retained with private fingerprint
evidence. Semantic comparison is not byte-preservation proof on main and does
not make an unfinished worktree or transcript archive safe to delete.
