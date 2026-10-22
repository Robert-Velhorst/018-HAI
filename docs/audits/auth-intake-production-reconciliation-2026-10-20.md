# Authentication and intake production reconciliation

## Scope and decision

Compare the full diffs of 17 selected tracked IDP file versions and eleven selected
backend production versions against public main commit
`847ce573a65659abca85bb8897a42e386ab8fe2c`.
The IDP batch comprises ten production files, six test files and one mock file.
This is separate from the selected untracked authentication-test inventory.

Keep the newer implementations on main rather than restoring older API
signatures or weaker validation. No missing retained implementation was found
in the selected IDP differences. The account-feed comparison exposed a current
Autoposter readiness defect, described separately below.

Original source integrity was checked against the private frozen inventory.
No original source, worktree, archive, database, configuration or backup was
deleted. Semantic review is not byte identity or blanket cleanup permission.

## IDP file decisions

Paths below are relative to `idp/`.

| File | Reconciliation decision |
| --- | --- |
| `Dockerfile` | Keep the coordinated digest-pinned Go/runtime update, bounded build, CA certificates and non-root runtime. |
| `internal/app/authentication/auth_handler.go` | Keep caller context propagation for logout, token checks and password reset; retained flows remain. |
| `internal/app/authentication/auth_handler_test.go` | The selected difference is formatting, not lost assertions. |
| `internal/app/authentication/auth_middleware_test.go` | Keep the retained assertions with context-aware mock signatures. |
| `internal/app/authentication/auth_service.go` | Keep nil/canceled caller guards, caller-bound reset delivery and signed operator scope restricted to the configured owner. Do not restore context-free methods. |
| `internal/app/authentication/auth_service_interface.go` | Keep the context-bearing service contract. |
| `internal/app/authentication/auth_service_test.go` | Keep context migration, synthetic fixtures, operator-scope binding and pre-canceled caller coverage. |
| `internal/app/config/config.go` | Keep the optional IDP event-bus configuration entry point. |
| `internal/app/config/kafka_config.go` | Keep per-IDP disabling in addition to global event-bus opt-in. This review does not enable a broker. |
| `internal/app/repositories/user_repository.go` | Keep lowercase hexadecimal reset-digest validation instead of length-only acceptance. Existing guard tests cover malformed digests before database access. |
| `internal/app/services/service_mock/user_service_mock.go` | Keep reset completion, storage and conditional-clear mocks matching the current contract. |
| `internal/app/services/smtp_password_reset_sender.go` | Keep caller deadlines, cancellation socket closure/error identity and recipient-free message headers. |
| `internal/app/services/smtp_password_reset_sender_test.go` | Retain TLS/recipient checks with context-bearing calls and pre-network cancellation coverage. |
| `internal/app/users/user_handler.go` | Keep UUID, nil identity and nil result guards instead of older unsafe assertions/dereferences. |
| `internal/app/users/user_service_test.go` | Keep invalid reset-token classification coverage added on main. |
| `internal/infra/database.go` | Keep validated pool settings and percent-encoded query spaces, initial ping and failure cleanup. |
| `internal/infra/migrations_test.go` | Keep the stronger synthetic password construction; validation assertions remain. |

The context migration does not prove cancellation reaches every SQL/Redis
operation. This source review does not establish email delivery, OAuth, database
migration or live authentication acceptance. Those require their own execution
evidence; no IDP runtime was started for this comparison.

## Backend file decisions

Paths below are relative to `backend/internal/`.

| File | Reconciliation decision |
| --- | --- |
| `accountfeed/bridge.go` | Keep source setup metadata and the read-only Autoposter provider. Fix its false credential-presence status independently rather than restoring the older missing provider. |
| `accountfeed/permission_registry.go` | Keep the distinction between Google OAuth client bootstrap and an owner grant. Credential presence is not a live connection or write authority. |
| `executionapproval/composite_resolver.go` | Keep server-owned resolver registrations, rejection of reserved/overlapping prefixes, dedicated safety-control review dispatch and fail-closed behavior when that resolver is absent. |
| `executionauth/service.go` | Keep isolated recovery evaluator cloning and side-effect-free receipt reads. Graph projections still occur on settlement/transaction paths; do not restore write side effects during `Get`/`List`. |
| `executionauth/repository_postgres.go` | Keep safety-control reference columns and strict matching against evidence. Keep delimiter-safe hexadecimal advisory-lock identity and error-based lock acquisition instead of NUL-containing PostgreSQL text or boolean inference from a void result. |
| `identity/jwt.go` | Keep the optional signed operator-scope claim and request context key. Signature/time validation is unchanged in this selected diff. |
| `router/identity.go` | Keep canonical bounded operator-scope propagation only for a verified owner role. Do not restore role-only provenance. |
| `executionbroker/local_safe_worker.go` | Keep the tighter artifact limit of twice the 4,096-byte output bound rather than restoring the older 64 KiB allowance. No executor logic differs in this selected version. |
| `safety/emergency_stop.go` | Keep separate immutable deployment-stop evaluation, still checked first in ordinary evaluation. Approved application-level recovery cannot override environment hard stops. |
| `operations/domain.go` | Keep the reference-sync sequence input and raw source-evidence digest creation. Initial observe-only state and retained fields are unchanged. |
| `models/operation.go` | Keep the internal raw-evidence digest excluded from JSON output. Other selected changes are alignment only. |

## Autoposter readiness defect

The registered Autoposter bridge unconditionally returned
`credentials_present_unverified`. As a result, the permission registry reported
`Granted: true` even when required configuration was absent. This is a status
reporting defect, not evidence that unconfigured requests bypassed the existing
owner/workspace, token or endpoint validation in the actual read path.

Report `credentials_required` when the URL, token or remote owner is empty after
trimming whitespace. A complete configuration remains explicitly unverified;
this does not validate the endpoint/token, perform a network read, establish
owner/workspace authorization or grant write authority.

The regression checks each missing field, a whitespace-only token, complete but
unverified configuration, derived permission status and read-only authority.
Before the fix, all four incomplete/blank configuration cases failed. After the
fix, all five scenarios passed. The full accountfeed package produced 369 passing
test/subtest events, zero failures and two explicit skips: symbolic link creation
requires an unavailable privilege, and junction creation requires NTFS on the
test host volume. These skipped boundaries are not claimed as locally verified.
No live integration-tagged database/provider tests were run or disposable stack
enabled. `git diff --check` passed; hosted exact-head CI remains a merge gate.

## Remaining boundary

This batch does not cover all tracked production variants, excluded operational
files, whole worktrees or transcript archives. It is not a claim that all HAI
integrations work in production, nor authorization to remove retained originals.
