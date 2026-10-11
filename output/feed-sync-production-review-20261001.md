# HAI Accountfeed: Source-Only Production Semantics Review

Parent integration amendment, 2026-10-01: Findings 1 and 2 below are fixed in the
current checkout. Missing/null/non-array items are rejected; overlapping calls for
the same in-memory feed are rejected before observation/intake, while independent
feeds remain usable. A deterministic blocked-intake regression passes. The storage
failure test now also removes the injected rejection and verifies real synthetic
recovery. Final accountfeed native suite: 20 top-level / 36 test-subtest passes,
zero skips/failures, terminal receipt in
`production-low-resource-feed-observation-reviewed-final-20261001.receipt.json`.
The original source-only findings/fingerprints below remain historical evidence.
Finding 3 is not resolved by widening operation uniqueness: archived/dismissed
re-intake remains intentional. Durable feed-revision receipts and cross-process
observation fencing remain open; successful timestamps indicate local ingestion,
not verified live-provider freshness or completeness.

Date: 2026-10-01. Scope: current working-tree `registry_service.go` and `sync_audit_failure_test.go`, with narrowly related parser, fetcher, operation-ingest/repository, migration, and HTTP-handler source reads. Two correctness findings and one contract-dependent lifecycle risk follow. No full production-readiness claim.

## Findings

### 1. [P2] Missing/null item collections can publish a cursor and successful freshness

**Source verified:** [backend/internal/accountfeed/registry_service.go:268](../backend/internal/accountfeed/registry_service.go):268 (lines 268-275) accepts a successful parser return and copies its cursor. Lines 301-310 classify an error-free, zero-item result as `synced`; [backend/internal/accountfeed/registry_service.go:362](../backend/internal/accountfeed/registry_service.go):362 (lines 362-369) then advances `LastSyncedAt`. In [backend/internal/accountfeed/generic_feed.go:105](../backend/internal/accountfeed/generic_feed.go):105 (lines 105-115), envelope decoding does not require an `items` member or distinguish JSON `null` from an explicit empty array. The item-validation loop at lines 96-100 does nothing for either absent or null items.

**Concrete source-derived case:** `{"cursor":"next-42","error":"upstream unavailable"}` or `{"cursor":"next-42","items":null}` is decoded into no items, no error, and a nonempty cursor. The registry therefore reports successful ingestion of zero items and advances successful freshness despite not receiving the required collection. `{}` also advertises successful freshness. This is not a runtime reproduction.

**Impact/scope:** Both local and enabled HTTP generic-envelope inputs. The freshness error is internal and unconditional for these inputs. Actual skipped upstream records additionally require an external consumer to act on the returned cursor; no such consumer was verified. A legitimate `{"items":[]}` should remain successful.

**Action:** Require a present, non-null JSON array for envelope `items` before returning a parsed feed. Reject malformed envelopes without publishing a cursor or changing successful freshness. Add missing/null/error-envelope cases alongside a valid-empty-array case.

### 2. [P2] Overlapping syncs can overwrite newer evidence and stamp older observations as fresh

**Source verified:** [backend/internal/accountfeed/registry_service.go:223](../backend/internal/accountfeed/registry_service.go):223 (lines 223-230) releases the registry lock before fetching/ingesting; [backend/internal/accountfeed/registry_service.go:243](../backend/internal/accountfeed/registry_service.go):243 (lines 243-255) does the same for due-feed snapshots. There is no per-feed in-flight exclusion or observation-generation fence. HTTP handlers expose both entry points at [backend/internal/accountfeed/handler.go:154](../backend/internal/accountfeed/handler.go):154 (lines 154-179).

[backend/internal/accountfeed/feed.go:105](../backend/internal/accountfeed/feed.go):105 (lines 105-109) hashes body and metadata, not all raw evidence. [backend/internal/operations/service.go:50](../backend/internal/operations/service.go):50 (lines 50-53, 85-94) looks up the current operation and replaces different raw evidence without checking which fetch observed the newer source state. [backend/internal/accountfeed/registry_service.go:365](../backend/internal/accountfeed/registry_service.go):365 (lines 365-369) stamps successful freshness at completion, with no association to fetch order.

**Concrete source-derived sequence:** Sync A fetches an older item and stalls before ingest. Sync B fetches a title/source-URI-only change with the same body/metadata, ingests its newer evidence, and completes. A then performs its dedupe lookup, obtains B's current operation version, and successfully replaces that evidence with the older raw item. A also becomes the most recent successful freshness timestamp. Optimistic operation-version checks do not prevent this sequence because A reads the current version after B commits.

**Impact/scope:** Concurrent single-feed/due-feed calls for the same registry feed. Evidence can regress; the latest timestamp can describe an older snapshot. This requires overlapping calls plus changing source content and was not reproduced. It is not a Go map data-race allegation. Both reports can still be classified `synced` under the current implementation.

**Action:** Serialize each feed across snapshot selection, fetch, ingest, and completion recording; alternatively enforce an observation-generation fence before both writes and health updates. If cross-process writers are supported, use a durable fence rather than only an in-process mutex. Add a deterministically blocked-old-fetch/new-fetch regression. Do not solve only the timestamp assignment while leaving stale evidence writes possible.

## Contract-Dependent Lifecycle Risk

### 3. [P2, conditional] Whole-feed retry is idempotent only while prior operations remain active

**Source verified:** [backend/internal/accountfeed/registry_service.go:261](../backend/internal/accountfeed/registry_service.go):261 (lines 261-300) fetches and ingests the entire response again. [backend/internal/accountfeed/fetcher.go:27](../backend/internal/accountfeed/fetcher.go):27 (lines 27-45) rereads the same file or GETs the same configured URL; no prior cursor is supplied. [backend/internal/operations/repository.go:267](../backend/internal/operations/repository.go):267 (lines 267-278) excludes archived/dismissed operations from dedupe lookup; [backend/internal/operations/memory_repository.go:56](../backend/internal/operations/memory_repository.go):56 (lines 56-60, 141-151) has the same active-only behavior.

Importantly, [backend/migrations/post/0004_operations_owner_workspace_dedupe_active.up.sql:1](../backend/migrations/post/0004_operations_owner_workspace_dedupe_active.up.sql):1 (lines 1-6) explicitly permits later re-intake after archival/dismissal. This is an intentional ledger policy, not a defect in that index.

**Concrete source-derived sequence:** A partial sync commits item A and rejects B. A is dismissed or archived before retry. Retrying the same feed does not find A as an active duplicate and creates a new operation plus creation event for unchanged A, even if B still fails. The new test's zero-duplicate expectation at [backend/internal/accountfeed/sync_audit_failure_test.go:70](../backend/internal/accountfeed/sync_audit_failure_test.go):70 (lines 70-83) covers only an unchanged active operation.

**Impact/assumption:** This violates feed-level replay-once semantics only if dismissal/archival is supposed to survive automatic retries of the same source revision. If deliberate automatic re-intake is the product contract, retain it and state this limitation instead of calling every retry idempotent. No evidence here proves that policy choice.

**Action:** If replay-once is required, track durable feed/revision intake receipts independently of active-operation dedupe and reserve re-intake for an explicit action or genuinely new revision. Do not broaden the shared ledger's uniqueness constraint without checking its intentional re-intake contract. Add partial-retry-after-dismissal/archive coverage for the selected policy.

## Current Fix and Test Boundaries

- Verified in source: any item conversion/ingest error clears the report cursor; mixed accepted/rejected items yield `sync_partial`, all rejected items yield `sync_failed`; neither advances `lastSuccess`. Fetch/parse errors similarly leave the previous successful timestamp intact. These changes address the original sequential error-path freshness issue.
- [backend/internal/accountfeed/sync_audit_failure_test.go:22](../backend/internal/accountfeed/sync_audit_failure_test.go):22 (lines 22-26, 40, 70-83) injects only creation failure. Rejection remains enabled on retry, despite the comment saying retry records failed items. The test checks repeated failure with active-item dedupe, not recovery. A source-level coverage addition should disable rejection, retry, then assert the previously rejected item is created exactly once, cursor returns, audit becomes `synced`, and successful freshness advances.
- [backend/internal/accountfeed/sync_audit_failure_test.go:89](../backend/internal/accountfeed/sync_audit_failure_test.go):89 (lines 89-119) checks success followed by malformed JSON. It does not check success followed by partial/all-item ingest failure, duplicate evidence-refresh failure, overlapping calls, or malformed-but-decodable envelopes. These are missing scenarios, not failed executed tests.
- Cursor clearing is response safety, not a durable replay guarantee: the registry has no cursor state/request argument, and the fetcher has no acknowledgment/replay protocol. A producer must retain failed records for retries. Whether any deployed producer does so is unverified. No claim is made that this registry incorrectly advances a stored cursor, because it does not store one.
- `LastSyncedAt` currently means successful local ingestion completion, not provider-observation time or full provider pagination completion. Reimporting an unchanged export updates it. The generic format supplies no envelope observation time/completeness proof; do not infer live-provider freshness from this timestamp.
- No defect is asserted merely because parser item validation rejects a whole envelope before ingestion: its current declared contract validates every item. Partial semantics here begin after that successful parse.

## Evidence and Execution Scope

Only local, read-only source inspection and this report creation were performed. Checked the code-review skill; its fixed-point/subagent workflow was not applicable to the explicit narrow, no-subagent request. Local files were sufficient; no external services were accessed.

No builds, tests, lint, Angular runner work, Docker, downloads, subagents, commits, pushes, or production/provider calls. Existing dirty source work was not edited. Suggested regressions above are not executed verification.

Working-tree evidence at review time:
- `registry_service.go`: modified; SHA-256 `35FAE9DACCC42227D64693186AFA36ACFBF760C64DBEEDA24B8EF2EDADEBD861`.
- `sync_audit_failure_test.go`: untracked; SHA-256 `D805DA52D14F6E46B9A588F24D1210C383B291E37259B3E23BAD65E7754AEFFF`.

Line references describe the files read during this audit, not a committed revision. Shared-workspace changes after these reads can invalidate references. Findings are source-derived reasoning, not runtime, PostgreSQL, deployment, or live-provider acceptance evidence.
