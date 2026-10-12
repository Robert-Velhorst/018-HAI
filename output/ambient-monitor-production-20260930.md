# Ambient Monitor PostgreSQL Acceptance - 2026-09-30

## Outcome and Scope

PASS for the bounded ambient-monitor PostgreSQL acceptance below, not full HAI production readiness. No descendants, commits, external providers, personal.env reads, application database/service changes, new volumes, or shared infrastructure changes. Existing caches were used as explicitly requested. Global instructions and the installed Go testing skill were reviewed; local source and Docker were the relevant capabilities.

Only these four package files were changed by this run; existing dirty scheduler files were preserved:

| File under backend/internal/ambientmonitor/ | SHA-256 |
| --- | --- |
| repository_postgres_test.go | 49ddad8b9670b34ebf4756f1bdb775d3f1eb2b6ffe2ffce109f771c5978b93b6 |
| composition_repository_postgres_test.go | 643b81e48fcf229db388cff06e479f891fa57f45b21c61b9bad38cc0802adb4e |
| postgres_fixture_test.go (new) | 745c0a2684c7f52700ac3283f1a6aa03059af993c0231e815dc504351a3e989c |
| collectors_postgres_test.go (new) | e3118e104ed064bf401ab50ba1caaeec24dc224abc9bdb9e3f29f1d9dfc05d08 |

Both lifecycle tests now use one fixture helper calling existing pgtestguard before connection construction or migrations. Required DSN: HAI_AMBIENT_MONITOR_POSTGRES_TEST_DSN; exact database: hai_ambient_monitor_test; opt-in: HAI_ALLOW_DESTRUCTIVE_DATABASE_TESTS=true. No shared database allowlist extension was needed. Lifecycle assertions were unchanged; SQL connections now close in cleanup.

Guard subprocess tests cover missing DSN, missing/false/non-explicit opt-in, wrong/suffixed database, hostname, remote host, socket, remote fallback, and malformed DSN. They require skip/refusal before open/migration diagnostics. Explicit opt-in with exact database and literal IPv4/IPv6 loopback is accepted without connecting.

## Real PostgreSQL Proof

Retained evidence directory: output/ambient-20260930-4654f19eed64/.

Command: `go test -json -race -count=3 -p=2 -timeout=12m ./internal/ambientmonitor ./internal/pgtestguard`.

UTC test interval: 2026-09-30T21:35:35.440563792Z through 2026-09-30T21:35:56.143315144Z. Both packages passed; 186 top-level pass events; no failed tests or reported races. Each required lifecycle, fixed-collector, fixture-guard, and loopback acceptance test passed exactly three times. The subprocess-only probe was intentionally skipped in the outer process three times; the actual PostgreSQL tests were not skipped. gofmt emitted no diffs; scoped git diff --check passed.

Real server: PostgreSQL 17.11, database hai_ambient_monitor_test. All 109 embedded pre migrations through pre/0109_trello_webhook_reconciliation_generations applied. Migration ledger and version output are retained.

NewGormCollector and its actual SQL reader matched complete fixture-derived row snapshots, counts, and digests across three runs: open loops = 262; verified completions = 260; overdue commitments = 260; each digest snapshot = 256 rows. Tests cover owner isolation, due/null/future boundaries, archived/completed/closed exclusions, verified/test_passed attestations with matching workflow trigger requirements, latest commitment revision, all five qualifying commitment states, terminal/no-due exclusions, workspace digest binding, empty owners, and unchanged-source replay. PostgreSQL confirmed transaction_read_only=on; collection used a read-only repeatable-read transaction. Digests vary between runs because synthetic owner/row IDs are unique; every digest matched that run's fixture.

## Isolation and Cleanup

Unique owner label: com.hai.ambient.acceptance.owner=ambient-20260930-4654f19eed64. Only hai-ambient-20260930-4654f19eed64-pg and hai-ambient-20260930-4654f19eed64-go were created/removed. Actual inspections were retained at creation, immediately before cleanup, and after stopping. Postgres used network=none, literal-loopback listen address, 2 GiB data tmpfs, no volumes/ports; Go shared only this isolated network namespace, had a read-only root and backend source mount, 2 GiB executable /tmp tmpfs, GOMAXPROCS=2, -p=2, GOPROXY=off, GOTOOLCHAIN=local. Existing hai-go-modcache and hai-go-buildcache were mounted and not removed. No image pulls or external networks. Final owner-filtered container listing is empty.

Postgres image: sha256:d74eeac9a635390a49bc21bd49fccd973de707e2a53a76ac49b552b8712ec46f. Go 1.25.13 image: sha256:14e75143c833c7398ea3a5e4c673aeaae35f40e781e6b060e2f97b72c475c975.

Earlier attempt output/ambient-20260930-326afac65b1b/ is retained: tests could not execute on default non-executable tmpfs; no tests/migrations ran. Its exact owned fixtures were inspected and removed. Only the second disposable fixture's /tmp received exec permission; shared infrastructure was untouched.

## Source and Evidence Hashes

Before/after manifests cover ambientmonitor, pgtestguard, outcomeevaluation, proactivity, infra, pre SQL migrations, go.mod/go.sum, and migrations/embed.go. Zero hashed source changes occurred during the successful run (source-changes.json = []). This is source identification, not a claim that the entire dependency/source tree or policy/composer snapshots are fully pinned.

| Source/evidence | SHA-256 |
| --- | --- |
| ambientmonitor/collectors.go | f34c25c10b096a75b0a7cbd75a9649f6bb16e046f46c384bb3ce6c521ae66f78 |
| ambientmonitor/repository_postgres.go | e954cbbb5e84a6eb27553e1aa4eef7beebf5db23099ab35f207428d610de4d91 |
| ambientmonitor/composition_repository_postgres.go | 55b6c517b4bb8fd0d4cbd784c3e245d52240dfa11642a57662f8e301afe802cc |
| ambientmonitor/composer.go | 76b14c48c1ee8b2a68f1946295fa15653052161ed894d3eaaaadd061c5728a03 |
| pgtestguard/postgres.go | 4f8bc04491410d5657da8f7a1078ec245b6f23133613441a9ba9e8dcaba24c16 |
| successful go-tests.log | 817aab276219966375565fd8160ae5515453f3d301e91d499e6acda1a0603177 |
| successful source-hashes-before.json | 571c144c0bf8b27810d93ef04d68725c49ecffcc83b9c0c0b41a90d4004e5c95 |
| output/ambient-monitor-20260930-run.ps1 (retained runner) | 795772dc2cf2c01a9f6fbe5bcc91177dca99006e83a16179c60843aba1d32d88 |

## Limitations and Parent Handoff

The lifecycle tests' postgresMonitorCollector is a mock collector, NOT real collector proof. The new source-row test provides that bounded proof separately. Its expected digest uses the production snapshot serializer over independently constructed fixtures, not an independent cryptographic implementation. Concurrent production source writes/count-snapshot consistency were not exercised. No concrete production repository/collector defect was reproduced, so their implementation was not edited.

Fully pinned policy/composer snapshots are NOT established. Parent-owned outcomeevaluation changes were exercised through the composition test but not edited. Source worker, UI, CI, acceptance scripts, docs, and installer remain with their designated owners. Parent can reference this evidence for the open outcome-monitor acceptance item; wiring the guarded DSN/database into shared CI or infrastructure is a parent-only follow-up.
