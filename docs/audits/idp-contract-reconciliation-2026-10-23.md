# Retained identity contract reconciliation

## Scope

Ten retained files were read in full and compared with their complete current
diffs. Their useful contracts are already present; the older versions must not
replace current safety behavior.

| Retained file | Current decision |
| --- | --- |
| `idp/internal/app/authentication/auth_service_interface.go` | Keep all authentication operations and newer context parameters for logout, refresh, authentication checks, and reset requests. |
| `idp/internal/app/users/user_service_interface.go` | Keep all user, reset-token, and account-update contracts, including expected session versions; retain the newer successful-login completion contract. |
| `idp/internal/app/repositories/irepository/user_repository_interface.go` | Keep repository operations and typed errors; retain successful-login completion with expected user snapshot and timestamp. |
| `idp/internal/app/services/service_mock/user_service_mock.go` | Keep retained mock methods and newer login/reset helpers, including nil-result handling for login completion. |
| `idp/internal/app/services/mocks_test.go` | Keep retained repository/logger/email/hasher fixtures and newer successful-login completion mock. |
| `idp/internal/app/users/mocks_test.go` | Keep retained user-repository and hasher fixtures and newer successful-login completion mock. |
| `idp/internal/app/config/local_preview_config.go` | Keep explicit opt-in, numeric loopback bind, strong gateway secret, owner requirement, and issuance-boundary revalidation; retain malformed-boolean configuration rejection. |
| `idp/internal/app/router/router.go` | Keep exact resolved-gateway trust, bounded DNS cache, and redacted reset paths. Retain context-aware lifecycle/resource ownership, HTTP timeouts, private panic recovery, and stricter forwarded-identity handling. |
| `idp/internal/app/router/routes.go` | Keep retained account/auth routes except the deliberately superseded GET logout. Retain POST logout with origin checks, owned database/service dependencies, and separate readiness. |
| `idp/internal/app/router/router_test.go` | Keep retained forwarding and reset-log tests, newer readiness controls, and duplicate/missing-header cases. Ambiguous gateway headers now yield no attributable client identity, not the gateway address. |

The current route differences are intentional: health indicates the process is
alive, while readiness reports dependency availability without exposing errors.
Logout remains a state-changing POST rather than restoring the older GET route.
The local-preview configuration remains an explicit local-only option; this
review does not enable it or change an account's permissions.

## Verification

```text
go test -C idp -p 1 -timeout 180s ./...
```

The complete default IDP package suite passed locally, including config,
repositories, router, authentication, services, users, command lifecycle, and
infrastructure packages. Some package results were cached from the preceding
successful checks. This validates local compilation and covered fixtures, not
live Redis/PostgreSQL configuration, real OAuth/email delivery, or deployment.

This is a contract-and-wiring reconciliation, not a claim to have completed the
remaining authentication handler/service, repository implementation, Google
OAuth, password-reset integration, or generated API-documentation reviews.
No runtime code was replaced by older interfaces. Retained source copies remain
intact, with private exact-content review records kept separately from public
documentation and from any deletion gate.
