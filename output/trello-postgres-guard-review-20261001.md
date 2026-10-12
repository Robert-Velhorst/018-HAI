# Trello PostgreSQL Guard Follow-up

Date: 2026-10-01. Read-only child: Chandrasekhar,
`01a0f731-45a3-79f2-ac3b-6882b111a2e3`; completed and closed. Parent owns all
implementation and executable verification below; child ran no commands that
mutated files, connected providers, tested code or provisioned services.

## Confirmed Source Findings

- The repository-test helper connected before checking only an `_test` suffix;
  opted-in remote or unrelated test targets were not rejected before connection.
- Explicit connection/schema failures became skips and could conceal an
  incomplete selected integration run. Underlying connection errors were printed.
- Connection-pool cleanup was registered after successful GORM initialization.
- CI already migrates `hai_migration_runner_test`, but did not select the four
  Trello repository tests or require their PASS evidence.

## Parent Integration

- Reused the existing `pgtestguard` exact-name/literal-loopback/fallback policy
  before parsing or constructing a connection pool. Retained
  `HAI_TEST_DATABASE_DSN` and the existing explicit destructive-test opt-in.
- Opened a bounded pgx standard-library pool with immediate cleanup, bounded
  connection/statement timeout and ping/actual-name verification before GORM.
- Opted-in unavailable connections, mismatched actual identity and missing
  schema fail with stage-specific messages, without emitting supplied DSNs or
  underlying connection errors. Central mismatch errors no longer echo names.
- Added the narrow no-network target matrix and CI source contract. CI runs
  `^TestTrelloPostgres` with race detection and explicitly requires all four
  repository PASS lines. Existing CI migrations and other packages' DSNs remain.
- Updated the completion-matrix recipe and the parent-owned fixture runner's
  additive `-IncludeTrello` switch. No private HAI service was changed or restarted.

## Evidence And Limits

- Local expanded CI contract: 78 passing tests, terminal exit 0,
  `production-ci-contract-expanded-final-20261001.log`. This is not hosted CI.
- Real fixture results, current full Go tests and repeated race checks are
  authoritative only where listed in `production-integration-20261001.md` with
  terminal logs. A runnable script and a completed review are not passing tests.
- Pure target validation and source-order contracts do not exercise every
  connection failure/identity-spoof/pool-cleanup branch dynamically. No full
  fault-injection coverage or live Trello API/provider acceptance is claimed.
- Other PostgreSQL test helpers and provider paths were outside this bounded
  review. This is not a repository-wide safety certification.
