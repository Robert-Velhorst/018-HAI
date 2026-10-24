# Original test declaration ambiguities

## Scope and cause

The original Go inventory left three comparisons unresolved because the
declaration analyzer uses declaration names as keys. All three files contain
multiple legitimate `var _ Interface = implementation` compile-time checks.
The repeated blank identifier is ambiguous to that analyzer, not a Go parse
failure. The analyzer has not been weakened to silently drop those assertions.

The complete original-to-current text delta of each file was read directly.
This review covers those deltas and interface checks; it does not certify every
unchanged test body, shared dependency, external service or user-facing path.

## Disposition

| File | Original behavior and current disposition |
| --- | --- |
| `idp/internal/app/authentication/auth_service_test.go` | Existing registration, refresh, revocation, password reset and authentication assertions remain. Calls now pass explicit contexts. Current tests additionally bind operator scope to the configured first-run owner and reject canceled callers. Fixture setup explicitly declares test mode and database fixture values. The signing fixture is generated rather than kept as a credential-like literal. |
| `idp/internal/app/authentication/auth_password_reset_integration_test.go` | Original reset digest/migration, single-consumption races, login lockout, stale-account updates and session invalidation tests remain. Password-reset sender and authentication calls use contexts. The isolated database URL now sets `search_path` directly rather than extending PostgreSQL `options`. This change requires real PostgreSQL execution, not just text comparison. |
| `backend/internal/operations/ingest_atomicity_regression_test.go` | Original creation, duplicate, isolation, collision and mutation assertions remain. Fixtures and expected refreshed records now include `SourceEvidenceRawSHA256` computed from their evidence bytes. This aligns assertions with the current evidence-integrity contract rather than restoring digest-free fixtures. |

No removed original test function or lost interface assertion was found in these
three deltas: the authentication unit file retains all 50 original test names
and adds two, the password-reset integration file retains all eleven, and the
atomic-ingest file retains all fourteen. All seven interface assertions remain.
That finding is not proof that every current assertion is adequate.

The fourteen atomic-ingest tests passed locally with an uncached test execution
(`go test -p 1 -count=1 -run '^TestAtomicIngest' ./internal/operations`). This
does not assert execution of the complete operations package or live persistence.

All 52 current test functions from `auth_service_test.go` were selected by exact
name and passed locally: 81 named test/subtest PASS events, zero failures and
zero skips. Dependencies remained offline, and build/temp paths stayed on D:.
This is unit-test execution, not live OAuth, Redis, PostgreSQL or SMTP acceptance.

## Recovered execution coverage

The IDP CI job already supplies a disposable PostgreSQL service. Its explicit
database-enabled step previously selected only `TestDurableSession...`; the
normal `go test ./...` step does not inherit that step's database environment.
Consequently, the eleven tests in the original password-reset integration file
could skip instead of exercising PostgreSQL.

A separate step now selects all eleven names, enables only the CI-owned test
database, runs serial race tests, rejects any SKIP/FAIL, and requires a top-level
PASS line for every selected test. Selection was checked against the actual test
function declarations. YAML parsing and Bash syntax checks passed locally.
The actual Bash log guard also passed four synthetic-log checks: all tests pass,
one selected test is missing, a test skips, and a test fails. Only the first case
is accepted. No provider or database is invoked by those guard checks.

The newly selected database tests have not yet run in CI at this checkpoint.
No local database service was created or mutated. Synthetic fixtures and CI
configuration do not prove live email delivery, OAuth or production readiness.

## Preservation boundary

Keep the current architecture and tests; do not copy the old files over them.
The three manual comparison ambiguities now have a bounded disposition. The
remaining 173 structural comparisons still require their own semantic review
where not already covered by an individual reconciliation. Original source
integrity and preservation on `main` remain separate checks. No source checkout,
transcript archive, database or worktree is eligible for deletion from this
document alone.

## First database-enabled execution

At commit `b3d2b00957aaa573bf99e25ee48b9abeb6b6a915`, CI run `40018740909`
actually executed the eleven newly selected PostgreSQL cases: nine passed and
two failed. The retained tests therefore exposed issues that skipped execution
had hidden; the new step is not treated as green merely because most cases pass.

The account-deletion/reactivation fixture supplied a just-created in-memory
user to session issuance, rather than reloading the persisted login snapshot.
Its reset-expiry timestamp can differ from PostgreSQL's stored precision and
trigger the strict session-creation comparison. CI reported the bounded issuance
error, not its underlying cause, so this diagnosis remains an inference pending
a fresh database run. The fixture now reloads the user before issuing the
session. Production comparison and revocation rules remain unchanged.

The account-update fixture reused a result struct still holding the first user's
primary key when querying the duplicate account. GORM added that nonzero key to
the new lookup, producing `record not found`. A separate empty result struct now
reads the duplicate account. Its unchanged-email/password/version assertions and
the duplicate-user rejection assertion are retained.

These fixture corrections are not a claim that the next PostgreSQL run passes.
The exact-head eleven-case step must execute again without skips. No production
database, identity permissions, session authority or password rules were changed.
