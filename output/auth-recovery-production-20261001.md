# Authentication Recovery Production Handoff - 2026-10-01

## Scope And Status

Source-only worker on `codex/hai-runtime-release`. Existing dirty and untracked work was inspected and preserved. No descendants, commits, pushes, installs, service/container lifecycle actions, executable tests/builds, private localhost writes, or provider/account calls were performed. No credentials were requested or printed. This is a handoff, not production verification or acceptance.

Capabilities used: local Go source, existing tests, scoped Git inspection, `apply_patch`, installed ECC Go patterns/testing instructions, and a lightweight prior-memory check followed by current-source inspection. External connectors and browser access were unnecessary; descendant agents and executable verification were excluded by assignment.

## Changed Paths In This Worker

- `idp/internal/app/authentication/auth_service.go`: retained fail-closed persistence handling; replaced stale full-user login update with atomic successful-login completion and current-account issuance.
- `idp/internal/app/repositories/user_repository.go`: added the row-locked narrow successful-login reset and snapshot guards.
- `idp/internal/app/repositories/irepository/user_repository_interface.go`: added the explicit atomic operation.
- `idp/internal/app/users/user_service.go` and `user_service_interface.go`: delegate the verified snapshot to the repository and reject missing returned accounts.
- `idp/internal/app/authentication/auth_recovery_production_test.go`: fail-closed account-completion regressions, including a missing completed account.
- `idp/internal/app/authentication/auth_service_test.go` and `http_session_redis_test.go`: narrowly updated existing test doubles to implement atomic completion.
- `idp/internal/app/authentication/login_interleaving_integration_test.go`: new deterministic PostgreSQL interleaving regressions.
- `idp/internal/app/authentication/auth_password_reset_integration_test.go`: gates the existing fixture before any database connection.
- `idp/internal/app/authentication/auth_database_guard_test.go`: dedicated-database/opt-in/effective-host validation plus connection-free guard regressions.
- `idp/internal/app/users/successful_login_test.go`: narrow delegation, current-account return, and error/nil-account regressions.
- `idp/internal/app/users/mocks_test.go`, `idp/internal/app/services/mocks_test.go`, and `idp/internal/app/services/service_mock/user_service_mock.go`: directly necessary interface mock additions.
- `output/auth-recovery-production-20261001.md`: this report.

The user explicitly extended ownership to these shared IDP repository/users/interfaces for this continuation. All prior edits in these files were preserved. No model/schema/migration, block-list, deployment, or README changes were made.

## Findings

### Fixed: Login Continues After Account Persistence Errors

Before this worker's changes, successful password comparison was followed by `UpdateUser`; only `users.ErrConcurrentUserUpdate` stopped the login. Other failures were logged and execution continued into refresh-session registration and JWT issuance. Persistence failures could therefore still produce a successful login despite the required account-state update failing.

Login now denies all atomic-completion errors and nil completed accounts before registration. The existing concurrency message remains unchanged. The login handler already rejects service errors without issuing login cookies. This fixes issuance after failed persistence; it does not assert that the prior behavior bypassed all later account/session checks.

Regressions cover generic write/account-read errors, direct and wrapped concurrency conflicts, nil completed accounts, zero registrations/no returned tokens on failure, and normal issuance after successful completion. Password comparison is synthetic in these narrow tests. They are not real-password/provider acceptance tests; none were executed here.

### Implemented: Atomic Successful Login Preserves Concurrent Lockout

Previously established interleaving (historical source reasoning, not an executed reproduction): a successful login held an unblocked snapshot, concurrent failed attempts committed a lockout without changing session/reset fields, and the stale full-user update then cleared that lockout.

`GormUserRepository.CompleteSuccessfulLogin` now takes `SELECT FOR UPDATE` on the same user row as `RecordLoginFailure`, validates the locked account, performs a narrow reset, and commits before returning current account state. It rejects inactive users, password/email/session-version/reset-digest/reset-expiry changes, and indefinite or still-active lockout. Only failed-attempt count, last-attempt time, expired-block state, and update timestamp are written. Role/password/email/active/session/reset/first-access fields are not overwritten. The current locked role is returned and used for token issuance.

Added deterministic regressions:

- A password-comparison hook commits an indefinite block or a real threshold lockout after the DB snapshot but before success completion; login must fail without registration or any lockout/account overwrite.
- Successful login commits first, then a threshold failure blocks the account; the previously issued access token must be denied by current-account checks.
- Real PostgreSQL row-lock barriers stage both operation orders using separate connections, GORM callbacks, channels, and bounded contexts. No sleep or scheduler-dependent winner determines the intended order.
- Account changes during comparison: current role demotion is preserved and issued as viewer; password/email/session/reset/active changes deny login and preserve the competing row exactly.
- An expired lock is reset without changing account/security state; unit tests ensure the users service delegates without a full-user update or unguarded reread.

All test sources remain uncompiled and unexecuted. Password comparison and session-registration storage are synthetic in the new lockout tests; the integration fixture uses real GORM/PostgreSQL operations when the parent opts in. The row transaction is the success-reset boundary, not an atomic transaction spanning PostgreSQL and Redis/JWT issuance. A block committed after success completion can still race token issuance, but current account checks deny its use. No broader redesign of generic `UpdateUser` or auth/provider gates was made.

### Implemented: Dedicated PostgreSQL Integration Guard

Before any fixture connection, `openPasswordResetTestDB` requires exact `HAI_ALLOW_DESTRUCTIVE_DATABASE_TESTS=true` plus `HAI_IDP_TEST_DATABASE_URL`. Missing opt-in/URL skips; configured unsafe targets fail. Only a PostgreSQL URL naming exactly `hai_idp_auth_test` with a literal loopback IP is accepted. Validation also parses pgx's effective database/host and every fallback; query overrides and alternate/remote/socket/hostname targets cannot bypass the URL check. Errors are generic and never include DSNs or credentials.

Connection-free table tests cover IPv4/IPv6, absent/nonliteral opt-in, incorrect/missing database, hostname/remote/socket/missing hosts, invalid URL/port, host/database query overrides, and local/remote alternate fallbacks. The existing fixture still creates an extension and an isolated schema and later drops only that schema; these are destructive test operations and require separately authorized disposable infrastructure. The guard is not proof that a chosen local server is otherwise disposable or appropriate.

### Durability Remains An Acceptance Requirement

Current Redis Lua rotation consumes positive parent authority before fallible writes and atomically supplies one cached winner for the five-second grace window. Positive family/token records deny lost-key sessions; logout deletes family authority and preserves longer revocation TTLs. Existing tests cover concurrency, replay, selective record loss, malformed authority, and partial Lua writes.

Those checks do not establish safety after restoring an older snapshot that contains both formerly active positive records. `docker-compose.local.yml:1766` supplies only a Redis port command, with no explicit acknowledged-write persistence barrier in the session service. No crash/restart/snapshot-rollback test was run or found in the reviewed session tests. Parent/deployment coordination must establish a durable session epoch/authority strategy or equivalent guarantees before claiming durable consumption under rollback. No deployment settings or shared persistence models were changed here.

## Current Source Audit Results

- Reset consumption: `GormUserRepository.ConsumePasswordResetToken` conditionally matches user ID, one-way token digest, expiry, and active status; one update changes password, consumes reset fields, and increments `session_version`. The users service hashes the new password once before that update. Auth maps already-consumed/invalid tokens separately from unavailable persistence.
- Reset issuance: expected session version is checked on the narrow digest write; failed delivery clears only the matching token digest. Concurrent newer tokens are not blindly erased.
- Refresh/reuse: `services/block_list_service.go:51` serializes rotation; grace calls reuse one encrypted response. Replay after grace removes family authority. `refresh_rotation.go` binds ciphertext to its exact predecessor and validates the child token pair, original family/user/version/expiry, and current child authority before return.
- Cookie path: `authentication/auth_handler.go:374` emits access and refresh replacements, preserving session-only preference. Internal auth subrequests emit separate cookie headers rather than public `Set-Cookie`; existing Redis HTTP tests cover this path. Gateway deployment behavior was not executed here.
- Logout: family, refresh, and access revocations are all attempted; errors are returned and browser cookies cleared. Family revocation denies previously rotated children.
- Owner authorization: `GetSessionFromToken` derives permissions from the current stored role after live account/session checks, not from client identity headers or the token's old role. Unknown roles downgrade to viewer; only owner receives approve/administer permissions. Existing demotion tests were read, not run.
- No additional concrete atomic-consumption or cookie-rotation defect was established in the current source. Existing changes must not be reimplemented from older memory notes.

## Verification Performed

- Read-only inspection of the cited source and existing regression/integration tests.
- `git branch --show-current`: observed `codex/hai-runtime-release`.
- `git diff --check -- idp/internal/app/authentication/auth_service.go idp/internal/app/authentication/auth_recovery_production_test.go`: exited 0; Git reported only its LF/CRLF warning. The untracked new test is not covered by this tracked-diff check and was reviewed directly.
- Final tracked-source whitespace check exited 0. A separate `git diff --no-index --check -- NUL idp/internal/app/authentication/auth_recovery_production_test.go` inspected the new file, returned 1 for the differing files, and emitted only the LF/CRLF warning with no whitespace-error diagnostics. Scoped status showed the existing modified service plus the new test/report as untracked.
- Continuation: inspected the production repository/users/interfaces and all located direct mocks; read the backend's existing pgtestguard design without editing or importing that separate Go module. Scoped `git diff --check` across IDP auth/users/repositories and necessary mocks exited 0, with only LF/CRLF warnings. New untracked files were read directly. This checks neither compilation nor database behavior.
- Separate no-index whitespace checks for the new login-interleaving, database-guard, and users-success tests returned 1 for differing files and emitted only LF/CRLF warnings, with no whitespace-error diagnostics. Rechecked source call sites for the added operation and observed the original Redis port-only command unchanged. No test results are implied by these inspections.
- No Go compilation, formatting command, executable unit/integration test, race detector, service probe, or production verification. Resource and authority restrictions were retained.

## Parent Verification Commands

Run serially from `idp`, only when the parent approves available resources and the local Go 1.25.13 toolchain/dependencies are already present. These commands were NOT run by this worker:

```powershell
$env:GOMAXPROCS = '1'
$env:GOMEMLIMIT = '256MiB'
$env:GOTOOLCHAIN = 'local'
$env:GOPROXY = 'off'
$env:GOSUMDB = 'off'
$env:HAI_IDP_TEST_DATABASE_URL = ''
$env:HAI_TEST_REDIS_ADDR = ''
gofmt -l internal/app/authentication/auth_service.go internal/app/authentication/auth_service_test.go internal/app/authentication/auth_recovery_production_test.go internal/app/authentication/auth_password_reset_integration_test.go internal/app/authentication/http_session_redis_test.go internal/app/authentication/login_interleaving_integration_test.go internal/app/authentication/auth_database_guard_test.go internal/app/repositories/user_repository.go internal/app/repositories/irepository/user_repository_interface.go internal/app/users/user_service.go internal/app/users/user_service_interface.go internal/app/users/mocks_test.go internal/app/users/successful_login_test.go internal/app/services/mocks_test.go internal/app/services/service_mock/user_service_mock.go
go test -p 1 -parallel 1 -count=1 -timeout 60s ./internal/app/authentication -run '^(TestPasswordLogin(NeverIssuesSessionAfterAccountUpdateFailure|IssuesSessionAfterAccountUpdateSucceeds|RejectsMissingCompletedAccount)|TestIDPAuthDatabaseGuardRejectsUnsafeTargetsWithoutConnecting|TestSuccessfulLoginDoesNotThrottleNextValidSession)$'
go test -p 1 -parallel 1 -count=1 -timeout 60s ./internal/app/users -run '^TestCompleteSuccessfulLogin'
go test -p 1 -parallel 1 -count=1 -timeout 90s ./internal/app/authentication -run '^(TestConcurrentRefreshReusesOneRotatedTokenPair|TestRefreshRejectsLogoutOrAccountChangeDuringRotation|TestRefreshRejectsCachedConsumedChild|TestRefreshRejectsSwappedOrMalformedCachedPair|TestAccessAndRefreshFailClosedWhenPositiveSessionAuthorityIsLost|TestCachedDescendantCannotBeSubstitutedForAncestorReplay|TestSuccessfulPasswordResetRevokesExistingAccessAndRefreshTokens|TestLogoutDuringRefreshInvalidatesAlreadyRotatedChildSession|TestSessionUsesCurrentStoredRoleInsteadOfTokenRole)$'
```

Stop on unavailable tooling/dependencies or resource exhaustion; do not install or start services to satisfy these commands. Restore environment settings in the parent runner afterward.

After separately approving/provisioning a disposable PostgreSQL instance, set `HAI_IDP_TEST_DATABASE_URL` to its literal-loopback PostgreSQL URL with database name exactly `hai_idp_auth_test`, and set `HAI_ALLOW_DESTRUCTIVE_DATABASE_TESTS=true`. Then run serially (not executed here):

```powershell
go test -p 1 -parallel 1 -count=1 -timeout 120s ./internal/app/authentication -run '^(TestSuccessfulLoginPreservesLockoutCommittedDuringPasswordComparison|TestSuccessfulLoginBeforeThresholdFailureDoesNotClearLaterLockout|TestSuccessfulLoginPreservesConcurrentAccountAndResetChanges|TestAtomicSuccessfulLoginAndFailureSerializeOnSameRow|TestAtomicSuccessfulLoginClearsExpiredBlockOnly)$'
go test -p 1 -parallel 1 -count=1 -timeout 120s ./internal/app/authentication -run '^(TestPasswordResetTokenCanBeConsumedOnlyOnceUnderConcurrency|TestConcurrentFailedLoginsAtomicallyIncrementAndLockAccount|TestStaleUserUpdateCannotRestoreConsumedResetTokenOrSessionVersion|TestPasswordChangeWithStaleValidatedSessionCannotOverwriteReset|TestSetPasswordResetTokenRejectsStaleAccountVersion)$'
```

Real Redis tests still require an explicitly authorized disposable literal-loopback instance and matching `HAI_TEST_REDIS_INSTANCE` marker. Skips, whitespace checks, and source reasoning are not runtime verification or durable production readiness. Full suite, Go interface/build compatibility, formatting, race detection, actual row-lock scheduling/callback behavior, pgx guard cases, and real password/provider/deployment acceptance remain unverified in this worker.

## Next Action

Parent: review the atomic reset and interface/mock changes, perform the bounded serial unit/guard checks, then separately authorize and run dedicated PostgreSQL interleavings/reset regressions. Establish Redis crash/rollback durability acceptance independently. Production readiness remains unclaimed.
