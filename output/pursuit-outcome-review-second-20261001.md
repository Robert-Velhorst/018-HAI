# Pursuit Outcome Second Review

Date: 2026-10-01. Read-only source review for parent integration. Three remaining findings; no executable verification or production certification.

## Findings

### 1. [P1] Explicit runtime completion blockers still disappear when there are more than 50 linked launches

Evidence: `backend/internal/pursuit/service.go:5191-5197`, `backend/internal/pursuit/repository.go:645-662`; display aggregation at `service.go:1585-1589`.

The completion guard passes the number of explicitly linked launch IDs as the repository limit. `FindLinkedAutomationLaunches` resets every limit greater than 50 to 20, then orders by newest `started_at`. With 51 explicitly linked launches, an older unknown/indeterminate receipt outside the newest 20 is never inspected. An independently accepted verification run or completed workflow can therefore permit `UpdateForOwner(... CompletionVerified ...)` despite that unresolved linked runtime outcome. A direct task-attempt blocker can catch some task-owned launches, but it does not cover runtime links that have no direct task-attempt projection. The detail/summary path always uses 20 and can advertise a completion candidate even earlier.

The fake masks the production threshold: `backend/internal/pursuit/service_test.go:5997-6014` applies the supplied limit without the production `>50 -> 20` reset, and does not reproduce production timestamp ordering. The new pending-intent/receipt test exercises the predicate and blocker with two in-memory rows, not this repository path.

Minimal fix: separate the bounded recent-runtime display query from an uncapped or fully paginated query over exact linked launch IDs for completion authority. Ensure unresolved explicitly linked runtime receipts also contribute to detail blockers and completion-candidate calculation. Do not make an unbounded automation-history display query or reinterpret ordinary pending intents as failures. Align the fake with the actual display contract.

Focused parent regressions: 51 explicitly linked receipts with the oldest receipt indeterminate and a separate verified run; assert completion is rejected and detail is not a completion candidate. Include the unresolved receipt only as an explicit runtime link, with no direct task attempt, so another guard cannot mask the defect. Check the real PostgreSQL query as well as the fake.

### 2. [P2] Historical terminal direct runs with missing or unknown verification are neither review blockers nor selected by the new uncapped query

Evidence: `backend/internal/pursuit/service.go:1411-1427`, `:1430-1434`, `:5163-5168`; `backend/internal/pursuit/repository.go:418-423`.

For an existing same-owner row with `Mode=run`, `Status=validated`, non-null `CompletedAt`, no blocker, and `VerificationStatus="unknown"` or empty, `pursuitTaskAttemptNeedsReview` returns false. The SQL also excludes it. With an independent verified completed workflow, the row produces no direct-task blocker, the summary can be a completion candidate, and verified pursuit completion is permitted. This differs from the newly hardened projection boundary: the equivalent new final run is downgraded by `backend/internal/task/service.go:1041-1047`. The activity message is now appropriately non-affirmative, but that does not fix completion authority or timeline review state for already persisted contradictory rows.

This is a legacy/input-contract gap, not a claim that the normal current Run path produces those contradictory successful rows. The new projection test does not exercise an existing persisted unknown/empty-verification terminal run through pursuit aggregation or SQL.

Minimal fix: for terminal successful direct **run** rows, require a task-specific accepted verification status in the review predicate and matching SQL selection. Preserve task's accepted `schema_validated` status and do not turn plan-only validated snapshots or ordinary nonterminal snapshots into failed runs. Handle nullable legacy verification fields explicitly.

Focused parent regressions: old same-owner terminal run rows with empty, NULL, unknown, and provider-new verification outside the recent 20, plus independent accepted completion evidence; assert blocker, timeline NeedsReview, and rejected completion. Positive controls: terminal run/schema_validated, run/verified, and plan-only validated records.

### 3. [P2] SQL trimming is not equivalent to the fake/Go review predicate, so tab/newline-wrapped uncertainty bypasses completion blocking

Evidence: `backend/internal/pursuit/repository.go:421-422` versus `backend/internal/pursuit/service.go:1422-1424`; fake delegation at `backend/internal/pursuit/service_test.go:5233-5237`. Existing database regression at `backend/internal/pursuit/completion_outcome_postgres_test.go:33-34` uses ordinary spaces only.

A persisted row with `Status=validated`, `VerificationStatus="\tuncertain\n"`, and empty blocker is review-required under Go's `strings.TrimSpace`, but PostgreSQL's default `TRIM(verification_status)` removes ordinary spaces, not tabs/newlines. None of the SQL OR branches selects that row. Consequently even a recent row can appear as a blocker in Detail (loaded by the recent query and checked in Go), while the completion guard consults only the SQL review query and permits completion using separate accepted evidence. Outside the recent 20, both authority and display lose it. The fake always uses the Go predicate and therefore does not reproduce this disagreement. `UpsertTaskAttempt` normalizes several other fields but does not normalize `VerificationStatus` (`service.go:1299-1305`), so this is not prevented at that write boundary.

Minimal fix: make the SQL normalization match the supported Go whitespace contract, and normalize verification statuses on new writes while retaining correct selection of existing rows. Keep NULL handling explicit when implementing finding 2. Do not merely add another status literal with whitespace.

Focused parent regression: real PostgreSQL rows with tabs/newlines around each review verification status, no other blocker/status match, and independent accepted evidence. Assert the uncapped query returns them and owner-scoped completion rejects them. Preserve ordinary-space and same/foreign-owner controls.

## Reviewed Contracts And Limits

- Read the earlier appendix, current task projection, pursuit service/repository, all three requested new tests, current receipt classifier, task uncertainty/validation helpers, workflow completion/recovery contracts, schema, and repository fakes/call sites. Local file reads, targeted searches, and Git diff/status inspection only.
- No remaining finding in the inspected LaunchType-specific completed-code predicate: API uses persisted completed status with an HTTP receipt code, Docker-service uses 204/304, and process-like receipts require zero. Ordinary pending intents remain nonblocking and do not count as completed evidence.
- Current workflow blockers reject required approvals unless approved, including rejected/revoked/expired/unknown approvals despite independent accepted evidence. Completed workflows need accepted verification and no worker/recovery/blocker contradiction. Non-UUID or missing-run internal verification provenance no longer independently authorizes completion.
- Current new direct-task projection preserves explicit uncertainty dominance, source IDs and attempt-end time, and accepts schema_validated run verification. Direct activity/timeline now share the review predicate; findings 2 and 3 concern its coverage and SQL parity.
- Recovery suppression requires an accepted, unblocked, approval-resolved completed workflow with the exact launch SourceURI; recovery labels alone do not pass. No new claim is made that a universal attestation foreign key should be required for accepted historical workflows. Recovery review was source-only, not operator acceptance.
- Reviewed fake interface coverage: base `fakeRepo` supplies the new method; portfolio acceptance/dispatch fakes inherit it; the resource-ledger wrapper promotes the updated Repository; source intake's repository explicitly supplies an empty implementation. No missing-method finding from inspected source. These checks do not establish compilation. Fake differences relevant to defects are identified above.
- Same-owner review rows are retained and foreign-owned legacy task rows remain excluded from owner detail and completion blockers by `taskAttemptsVisibleToOwner`; ownerless wrappers remain distinct internal calls. No cross-owner exploit or adoption of ownerless records was demonstrated.
- No tests, builds, formatting, Docker, network, services, installations, descendants, commits, or deletions. Only this report was created. Parent owns serial executable verification. No live database, migration execution, provider acceptance, deployment, or whole-product readiness was checked.

## Source Snapshots

SHA-256 snapshots at final source inspection; concurrent parent edits may supersede these findings and line references:

- `backend/internal/task/service.go`: `4FE3167DB8EA59E591C0B0C024CFDB46D59AFB549C472DFE191A81E7C75F6821`
- `backend/internal/pursuit/service.go`: `7AF65F6A65D6A422D1AD7917FAEA8FAC3BB41F35FE1C8FFFE7A8EE7A1AED7269`
- `backend/internal/pursuit/repository.go`: `1ECAE632AB9CBD7D0197203080BFD4CFA2CB5C8815FC684A43AD588B3C585184`
- `backend/internal/pursuit/completion_outcome_integrity_test.go`: `6DCF35140ADA342C45B25CEBC80BEA16618595C2EFF5760424D536EA5AA4C914`
- `backend/internal/pursuit/completion_outcome_postgres_test.go`: `D04A94433B17538D3267157554A4DED03BA1371D197BE27A6DF70FAD1739A90D`
- `backend/internal/task/pursuit_outcome_projection_test.go`: `F6EE018DC3ACD20BDE02CD0491A6C32D10FD6C8C96408C9234BD6A2578893D34`
