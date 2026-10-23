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

## Account and migration implementation follow-up

Six additional retained files were read completely, including both halves of the
retained user-service test file, and compared with their entire current diffs:

- `idp/internal/app/users/user_service.go`: preserve account reauthentication,
  canonical email, session-version/reset-token concurrency checks, password
  policy, and one-time reset handling. Keep newer borrowed-database construction,
  owned-resource cleanup, successful-login completion, legacy email normalization,
  and reset-error log redaction.
- `idp/internal/app/users/user_service_test.go`: every retained test remains;
  current code additionally tests legacy email normalization and invalid-token
  error classification. No retained assertion needs replacement.
- `idp/internal/app/users/user_handler.go`: keep bounded single-value JSON,
  authenticated account binding, and safe error mapping. Retain current nil/type
  checks instead of restoring panic-prone principal assertions or nil responses.
- `idp/internal/app/repositories/user_repository.go`: preserve failed-login row
  locking, conditional account/password updates, reset-token digests, and
  one-time consumption. Keep newer successful-login snapshot validation under a
  row lock, deactivation that invalidates sessions/resets, and strict digest
  validation instead of length-only checks.
- `idp/internal/infra/migrations.go`: preserve conditional legacy token upgrades
  and operator-by-default role handling. Keep the newer durable-session migration
  call; this review does not claim complete review of its separate SQL schema.
- `idp/internal/infra/migrations_test.go`: all retained first-owner password
  boundary/redaction assertions remain. Keep the newer synthetic passphrase
  fixture rather than restoring the old literal.

No older implementation is copied over the stronger current code. Reading the
conditional SQL paths and passing fixture tests does not establish that real
PostgreSQL contention, upgrades, rollback, or deployment acceptance has passed.

## Google login follow-up

Both retained `google_oauth.go` and `google_oauth_test.go` were read completely and
compared with their current diffs. Keep signed state, state-bound PKCE, verifier
non-disclosure, verified-email enforcement, bounded HTTP timeouts, and redirect
refusal. Do not restore the older userinfo endpoint/claim name or weaker callback
configuration checks. Preserve the current `email_verified` claim, validated
callback URL, and expiration-at-equality rejection; current tests cover those
intentional differences.

The response reader had a separate defect: limiting reads to exactly 1 MiB could
turn an oversized invalid provider response into valid JSON by discarding the
extra bytes. Two local HTTP regressions confirmed that both the token and
userinfo paths accepted a valid JSON prefix followed by padding and extra junk
beyond the limit. Both failed before repair. Positive controls for valid JSON
padded to exactly the limit passed.

Both paths now use a bounded limit-plus-one reader and reject oversized content
before parsing it. An oversized token response never advances to userinfo. The
existing response-body cleanup and timeout behavior remains; the limit increases
by only one sentinel byte, not an unbounded read. No provider configuration,
credentials, scopes, or account permissions change.

## Cookies, handler, and reset-limit follow-up

Five further retained files were read fully and compared with their complete
current diffs: `cookies.go`, `auth_handler.go`, `auth_middleware_test.go`,
`password_reset_rate_limit.go`, and `password_reset_rate_limit_test.go` in
`idp/internal/app/authentication`.

Preserve HttpOnly/Secure/SameSite settings, remember-me/session behavior, OAuth
state/return cookies, bounded reset windows and bucket capacity, purpose-scoped
HMAC keys, normalization, concurrent memory limits, and fail-closed Redis outage
behavior. Keep newer owned Redis-client cleanup, missing-address denial, context
parameters in service fixtures, bounded request parsing, public authentication
limits, origin-checked POST logout, and internal refresh-cookie propagation.
The newer reset response explicitly avoids claiming account existence or email
delivery; do not restore the older wording.

Session resolution still chose the first cookie when duplicate `access_token`
or `refresh_token` values were supplied. Four actual handler fixture regressions
returned HTTP 200 or invoked refresh before repair. Session-cookie ambiguity now
fails before any token-validation or refresh call. The nine-case handler suite
also preserves single access, single refresh, expired-access refresh, unrelated
duplicate cookies, and no-credential behavior. OAuth state cookies and other
cookie preferences are not reclassified as session credentials by this change.

The complete retained middleware fixture file is reviewed here, not the remaining
large authentication-handler test file or the full durable auth-service source.
Redis unit fixtures and Lua source review do not establish live Redis failover or
cross-process abuse-limit acceptance.

## Verification

```text
go test -C idp -p 1 -timeout 180s -count=1 ./...
```

The complete default IDP package suite passed locally, including config,
repositories, router, authentication, services, users, command lifecycle, and
infrastructure packages. This final follow-up run disabled the Go test-result
cache. This validates local compilation and covered fixtures, not
live Redis/PostgreSQL configuration, real OAuth/email delivery, or deployment.

This is a contract-and-wiring reconciliation, not a claim to have completed the
remaining authentication-handler tests and service, repository acceptance tests,
password-reset integration, or generated API-documentation reviews.
No runtime code was replaced by older interfaces. Retained source copies remain
intact, with private exact-content review records kept separately from public
documentation and from any deletion gate.
