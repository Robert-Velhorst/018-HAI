# Trello Production Sync Handoff

Date: 2026-10-01. Workspace: `%HAI_WORKSPACE%`. Branch observed: `codex/hai-runtime-release`.

Status: source-only audit and bounded implementation handoff. This is NOT an executable-test, live-provider, deployment, or production-readiness verification.

## Scope And Boundaries

- Read the real Trello snapshot, resumable, repository, webhook, and comment-refresh paths and their existing fixtures. Shared source service, HTTP transport, provider error classification, models, and migrations were inspected read-only where relevant.
- Inspected installed diagnosis/review skill instructions and checkout-relevant memory. The explicit no-descendants/no-executable-tests assignment overrides delegation and runtime reproduction steps. No external connector, browser, account, or network capability was used.
- Preserved the pre-existing dirty/untracked Trello implementation. Only the assigned Trello source files, one new focused test file, and this report were edited. No README changes, commits, pushes, deletes, installs, process/container lifecycle actions, credentials access, or runtime data writes were performed.
- All outbound provider operations remain GET-only. Owner/member binding, read-only-token checks, redirect refusal, shared network policy, existing caps, signed callbacks, durable receipt replay checks, source leases, and owner-review intake remain in place. No paid-provider or LLM path was added.

## Changed Paths

- `backend/internal/source/trello.go`: distinguish request-attempt budget exhaustion without changing its safe public error or non-retryable classification for non-resumable callers.
- `backend/internal/source/trello_durable.go`: canonicalize accepted card/action-card identities; checkpoint only verified inventory progress when retries consume the request cap; count lists in the aggregate record cap.
- `backend/internal/source/trello_sync_repository.go`: replay comment actions from the preceding cycle's start rather than only its completion timestamp.
- `backend/internal/source/trello_webhook.go`: align creation-callback action metadata with provider-scan identity; retain the existing `actionId` field and record `actionType` explicitly.
- `backend/internal/source/trello_webhook_reconcile.go`: accept a provider-verified configured board short link; recognize the exact legacy verified creation-metadata shape without rewriting it; count the fetched list in the record budget.
- `backend/internal/source/trello_production_sync_test.go`: seven focused regression functions, unexecuted.
- `output/trello-production-sync-20261001.md`: this handoff.

## Demonstrated Findings And Fixes

### P1: Long Resumable Cycles Could Skip Comment Actions

`beginTrelloSyncGeneration` previously used `LastSuccessfulAt - 24h`. Descending action pages can miss actions created above the first page while subsequent pages/card scans/reconciliation continue. If a cycle starts September 25, finishes September 28, and an unseen action arrives September 26, the previous next-cycle cutoff was September 27, excluding that action.

The next replay cutoff now uses the earlier of the previous nonzero `CycleStartedAt` and `LastSuccessfulAt`, minus the existing overlap. Completion evidence remains the actual completion time. Legacy state without a cycle-start timestamp retains the prior completion-based fallback. Stable external action identities and existing receipts still govern replay/upsert idempotency.

Regression: `TestTrelloProductionSyncActionWatermarkCoversLongRunningCycle`.

### P1: Short-Link Boards Could Not Refresh Edited/Deleted Comments

Sources accept eight-character board short links. Webhook receipts store the connected source's target. Comment refresh resolved that short link to a canonical Mongo ID, then compared the receipt's short link only against that Mongo ID, rejecting a valid connected board.

After `canonicalTrelloBoardIDForTarget` verifies the provider metadata, refresh now accepts the receipt matching either the immutable configured target or its verified canonical ID. Card-to-board validation is unchanged. Wrong aliases and case-changed short links remain rejected.

Regression: `TestTrelloProductionSyncWebhookRefreshBindsVerifiedShortLink`.

### P1: Webhook-First Comment Creation Blocked Deletion Reconciliation

Creation callbacks emitted `trello:action:<id>` records with `action=<type>;actionId=<id>`. Deletion reconciliation requires the canonical action ID in `action`, so a callback-first comment not yet rewritten by a later provider scan caused the affected-card refresh to fail before archive.

New callback metadata now carries the canonical action ID in `action`, like provider scans. Existing records are accepted only when their type is `trello_comment_created`, `webhookVerified=true`, the old `action` value is a supported creation type, and `actionId` agrees with the external identity. Conflicting IDs and unverified legacy shapes remain rejected. Original raw content and metadata are not rewritten or removed during archive.

Regression: `TestTrelloProductionSyncWebhookCreatedCommentCanBeArchived` covers new metadata, verified legacy metadata, and both negative cases.

### P2: Validated Identities Could Still Fail Durable Checkpointing

The durable card validator accepted normalized Mongo IDs but retained mixed-case/whitespace provider values in the observed inventory and final cursor. Action pages similarly validated the card ID without using its normalized value in receipts. The repository requires canonical lowercase identities, so a page accepted by the reader could later fail its checkpoint contract.

Accepted cards now carry the canonical card/board IDs into import, observed inventory, fingerprint payload, and cursor. Action-card IDs are normalized before receipt and import construction. Invalid IDs and cross-board cards still fail closed.

Regressions: `TestTrelloProductionSyncCanonicalCardCheckpoint` and `TestTrelloProductionSyncCanonicalActionCardReceipt` also validate the prepared page against the real repository cursor contract.

### P2: Inventory Retries Discarded Already Verified Progress

Inventory sized its batch from the remaining request count assuming one attempt per card. Bounded 429 retries consume extra attempts. Hitting the 90-attempt cap on a later lookup previously returned a terminal error, discarding the verified prefix and leaving the same durable inventory cursor to repeat on resume.

Request-cap exhaustion now has a specific internal error identity. Inventory alone handles that error by returning an unfinished slice with the last successfully verified cursor. The unverified card is not marked fetched, tombstoned, or complete. Authentication failures, transport failures, ordinary 429 exhaustion, malformed responses, and byte/record caps continue propagating their existing errors.

Regression: `TestTrelloProductionSyncInventoryCheckpointsBeforeRetryRequestCap` exercises successful reads requiring retries, the exact attempt cap, the repository cursor contract, and the next-slice boundary.

### P2: Resumable Reads Omitted Lists From The Shared Record Cap

The snapshot reader counts board lists toward the shared lists/cards/actions cap; the durable reader did not. Per-card comment refresh also omitted its fetched list. Both now count these records without changing progress counters, which continue to describe the page's primary records.

Regression: `TestTrelloProductionSyncListRecordsRespectAggregateCap` asserts oversized list inventories fail before card reads or a returned slice.

## Verification Actually Performed

- Confirmed the current branch with `git branch --show-current`.
- Read current working-tree source and existing fixtures; did not substitute committed HEAD for the dirty implementation.
- `git diff --check` on the edited tracked Trello path returned exit 0. Git emitted only its LF/CRLF conversion warning for `trello.go`.
- A trailing-whitespace source scan covering the new test and edited untracked Trello files found no matches.
- Reviewed the new tests against existing fixture helpers, provider error unwrapping, repository cursor contracts, and source/extraction fence interfaces.
- No formatter, compiler, test runner, server, database, browser, container, or provider request was executed. No passing regression results are claimed.

## Parent Verification Commands

Run serially only when the parent has a resource-approved executable environment and the already provisioned Go toolchain/dependency cache. These commands are proposals, NOT results. Keep live tests and database-dependent tests out of this first pass.

From the workspace root, formatting inspection only:

```powershell
gofmt -l backend/internal/source/trello.go backend/internal/source/trello_durable.go backend/internal/source/trello_sync_repository.go backend/internal/source/trello_webhook.go backend/internal/source/trello_webhook_reconcile.go backend/internal/source/trello_production_sync_test.go
```

From `backend`, with resource/network-download bounds scoped to the parent's test process:

```powershell
$env:GOTOOLCHAIN = 'local'
$env:GOPROXY = 'off'
$env:GOSUMDB = 'off'
$env:GOMAXPROCS = '1'
$env:GOGC = '40'
$env:GOMEMLIMIT = '384MiB'
go test -p 1 -vet=off -parallel 1 -count=1 -timeout 120s ./internal/source -run '^TestTrelloProductionSync'
go test -p 1 -vet=off -parallel 1 -count=1 -timeout 120s ./internal/source -run '^Test(BeginTrelloSyncGeneration|ValidateTrelloPageCursorProgress|TrelloInventoryReconciliation|TrelloRequestBudgetStops|TrelloRateLimit|TrelloCommentEdit|TrelloCommentDelete|TrelloWebhookCommentCreation|TrelloDurableSlicesKeep|TrelloDurableRejects|TrelloPageResponseRead)'
```

The HTTP regressions use in-process loopback fixtures and synthetic credentials when run by the parent; they do not establish real Trello behavior. The memory setting limits the Go runtime target, not total host RSS; the parent must enforce its actual resource envelope and stop if prerequisites/resources are unavailable.

## Remaining Limits And Parent Requests

- No generic helper outside ownership requires modification for the landed fixes.
- Compile/format validation and all executable regressions remain pending. Actual Postgres checkpoint transactions, worker leases, concurrent receipt replay, and crash recovery need separately approved isolated-database verification.
- Do not run the existing Trello Postgres suites against an operator-provided DSN without independently confirming the isolation guard. The inspected `openTrelloRepositoryPostgresTestDB` connects before checking a `_test` suffix and does not itself enforce a loopback/dedicated-name policy. Parent request: route that test helper through the existing central `pgtestguard` contract before authorizing those suites; do not use real runtime data.
- Pagination assumes the provider ordering/page semantics already used by the connector. No live board, current provider documentation, rate-limit headers, webhook registration, signatures from Trello, or account permission was verified in this no-network assignment.
- Full scans remain eventually consistent, not transactional provider snapshots. Existing attachment-content omissions and signed-webhook/operator-registration requirements remain explicit. Comment edit/delete handling still requires an actually configured and registered webhook; those operational prerequisites are not supplied by these fixes.
- No claim of full HAI production readiness. Parent should review these source edits together with other workers' changes, run the narrow serial checks, and record separate runtime/provider acceptance evidence.
