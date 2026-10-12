# Portfolio PostgreSQL Integrity Review

## Conclusion

The inspected path has a production serialization defect, not a proven fixture defect. An absent `CoordinationPlanDigest` is hashed as the Go empty string before insertion, but PostgreSQL stores its `CHAR(64)` representation padded with spaces. The installed pgx driver preserves those spaces on reload, and `digestPortfolioAllocation` hashes the raw reloaded string. The resulting parent digest differs even though the allocation is semantically unchanged.

This is a source-backed diagnosis consistent with the supplied real PostgreSQL failure. No fresh database query or test was run, so the failing row's actual field bytes have not been captured here.

## Evidence

- `output/production-pursuit-postgres-race-second-20261001.jsonl:25` records the failure at test line 338: `validate accepted allocation evidence: portfolio allocation history parent digest verification failed`.
- `backend/internal/pursuit/portfolio_acceptance_repository_postgres_test.go:453` already uses `time.Now().UTC().Truncate(time.Second)`. The fixture uses this same value for both horizon endpoints, acceptance time, item schedule, reservation time, and activity time. Nanosecond precision loss cannot explain this fixture's parent failure.
- Test lines 318-327 compute the actual production parent and item digests before `SavePortfolioAllocation`. The fixture has no coordination binding, so its coordination digest is initially `""`.
- `backend/internal/pursuit/portfolio_acceptance.go:407-425` hashes `CoordinationPlanDigest` verbatim and the UTC horizon endpoints. `AcceptedAt` is not part of this digest.
- `backend/internal/models/pursuit.go:256` declares `CoordinationPlanDigest` as `char(64)` with an empty default. `backend/migrations/pre/0054_pursuit_portfolio_coordination_plan_binding.up.sql:4` adds `coordination_plan_digest character(64) NOT NULL DEFAULT ''`. Empty values consequently have a space-padded database representation; a full 64-character digest has no such difference.
- The installed `github.com/jackc/pgx/v5@v5.9.2/pgtype/pgtype_default.go:54` maps `bpchar` to `TextCodec`. In `pgtype/text.go:184-190`, its string scan copies `string(src)` without trimming. The default database/sql conversion in `stdlib/sql.go:850-856` uses that string scan path. These files were read directly from the local module cache; no dependencies were fetched.
- `backend/internal/pursuit/plan_binding.go:108-112` treats whitespace-only optional binding fields as absent during shape validation. This is why the padded absent binding passes that validation before failing the raw parent digest comparison at `portfolio_acceptance.go:121-123`.
- `backend/internal/pursuit/portfolio_acceptance_repository.go:123` returns a copy of the inserted in-memory allocation rather than reloading it. `portfolio_execution_proposal_repository.go:31` subsequently reloads the parent with `SELECT *` semantics, without canonicalizing the coordination digest. `portfolio_execution_proposal.go:173` then validates the reloaded evidence.
- Real production acceptance also leaves these optional fields empty when no coordination plan is supplied (`portfolio_acceptance.go:250` and `plan_binding.go:149-156`), so this is not limited to the handcrafted fixture. Allocation history uses the same evidence verifier.

## Required Production Repair, Not Applied

Restore one canonical representation for an absent optional coordination digest across insertion, repository reloads, and all digest consumers. Preserve digest verification, owner isolation, binding validation, immutable records, and exact verification of present 64-character digests. Do not pre-pad the fixture, hash database-mutated values after saving, bypass evidence validation, or remove this test.

A narrowly scoped canonicalization of PostgreSQL's ASCII-space padding can preserve existing digests originally computed from the empty string without changing stored immutable evidence. Its placement must cover every relevant read/digest path. In particular, `backend/internal/pursuit/portfolio_dispatch.go:308` also copies the raw allocation coordination digest into a request digest; fixing only one reader may leave another inconsistent path. An alternative storage-type migration requires separate migration and historical-record compatibility review; no migration is proposed as an unreviewed shortcut.

## Parent Verification Still Required

1. Capture the failing allocation's loaded optional digest with an escaped representation and length; compare the canonical parent hash before insertion and after reload. Also compare the other hashed fields to exclude an additional mutation in the actual row.
2. Apply the reviewed production repair under the parent's ownership, then rerun the existing failing integration test serially without excluding it.
3. Verify both absent and exact accepted coordination bindings through actual PostgreSQL save/reload, history inspection, proposal preparation, and relevant replay paths. Keep tampering, owner-isolation, decision replay, and append-only checks intact.
4. The supplied log also contains an independent failure in `TestPostgresPursuitUnresolvedAttemptQueryIgnoresDisplayLimit` at `completion_outcome_postgres_test.go:59`. This review does not fix or verify that separate failure.

## Changes And Limits

- Only this report was created. `portfolio_acceptance_repository_postgres_test.go` was intentionally left unchanged because a fixture-only repair is not justified.
- Existing dirty test changes, including the dedicated database guard and connection-pool cleanup, and all other dirty files were preserved.
- Used the local debugging skill, supplied JSONL records, checked-out production/test/migration source, existing diffs, and installed driver source. Prior HAI memory was consulted only for workspace/safety context; the diagnosis relies on current local evidence.
- No descendants, tests, builds, Docker, network, browser, credentials, installation, deletion, commit, or push actions were performed. Runtime confirmation and a passing repaired serial run remain outstanding.
