# Authentication test reconciliation

## Scope and result

The original checkout contains ten untracked authentication test files whose
paths already exist on main. Their complete diffs were read against main
`847ce573a65659abca85bb8897a42e386ab8fe2c`, rather than inferring integration
from filenames or test names. None of the retained assertions in this selected
batch is absent from the current files. Do not restore the older API calls.

This result covers these ten specific file versions, not other worktrees,
production authentication changes, or the remaining untracked inventory.

All paths below are relative to `idp/internal/app/authentication/`.

| File | Reviewed difference and preservation decision |
| --- | --- |
| `password_reset_log_privacy_test.go` | Adds the context import and a background context to reset requests. Store, clear and confirmation failure privacy assertions are retained. |
| `refresh_rotation_test.go` | Adds explicit contexts to authentication, refresh and logout calls. Revocation-race, consumed-child, swapped/malformed cache, store-failure, expiry and malformed-claim assertions are retained. |
| `durable_session_authority_test.go` | Migrates calls to context-aware APIs. Stale role, missing dependency, unknown family and unavailable durable-authority checks are retained. |
| `session_authority_test.go` | Adds the context import and context arguments. Loss-of-positive-authority and substituted-descendant replay assertions are retained. |
| `login_interleaving_integration_test.go` | Adds context to the final authentication check. The interleaved successful-login/lockout assertion is unchanged. |
| `auth_password_reset_integration_test.go` | Adds contexts to service calls and the reset-sender stub. The database fixture sets the driver's explicit `search_path` query parameter instead of appending it to server `options`. Digest-at-rest, legacy reset, session revocation and account reactivation assertions remain. |
| `durable_session_postgres_redis_test.go` | Adds contexts, and expands one concurrent refresh closure for formatting. Snapshot restore, replay revocation, canonical-pair recovery, SQL faults, concurrency, legacy-family rejection, migration and closed-pool assertions are retained. |
| `http_session_redis_test.go` | Adds context to the direct authentication check. The HTTP-issued JWT expiry and cookie-chain assertions remain. |
| `refresh_rotation_redis_test.go` | Adds contexts to refresh/authentication/logout calls. Concurrent cookies, expired grace, replay and logout-race assertions are retained. |
| `session_store_loss_redis_test.go` | Adds contexts to service calls. Missing-authority, old/new session separation, selective key loss and malformed Redis authority assertions remain. |

The fixture's `search_path` update does not remove other existing URL query
parameters: it updates one entry in the parsed query map. No older server-option
construction is reintroduced here.

## Evidence boundary

This is a source-difference review. No production implementation or test
assertion is changed by this report. No new local Go test run, live IDP, Redis,
PostgreSQL instance, email delivery or OAuth acceptance is claimed. CI must run
on the exact documentation head before this PR is merged under the standing
consolidation rule.

The reviewed main files are not byte-identical to their retained copies. This
report records behavioral test preservation, not raw-file identity or automatic
deletion eligibility. Private inventories retain the original identities without
publishing machine paths or private hashes.

## Retention and remaining work

Keep the original files for now. No source, runtime state, transcript or backup
is removed by this review. The broader inventory still includes unreviewed
production changes, other test variants and operational evidence. Do not infer
whole-checkout or whole-archive reconciliation from this ten-file batch.
