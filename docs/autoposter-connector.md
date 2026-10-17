# Secondhand Autoposter reference connector

## Status and meaning

This branch adds an **on-demand, authenticated, read-only pull** from Secondhand
Autoposter to HAI's existing Operation Ledger. It is not marketplace
publishing, unattended scheduling, a semantic-memory/RAG connector, or
permission to execute imported text. The connector is wired into backend
startup when explicitly configured, but this work has not deployed or
configured the installed HAI service or contacted a live Autoposter instance.

Each listing is stored as a `read_only_reference` operation with terminal
`archived` status and `observe_only` decision. "Archived" here deliberately
means an inactive reference, not a completed task or proof that the source
listing was deleted. Inspect `evidence.deleted` to distinguish a tombstone.
These rows do not enter the background execution queue. A source URL remains
available on live records; opening Autoposter still requires its normal login.

The existing generic JSON file/manual import remains available and unchanged.
Do not configure the protected Autoposter API as a generic HTTP JSON feed.

## Prerequisites and consent

- Review and deploy both the consumer source and an Autoposter version whose
  manifest advertises `ordered_change_ids` and whose records contain decimal
  string `change_id` values. Older producer versions are rejected explicitly.
- Obtain approval for the particular Autoposter owner, HAI owner/workspace,
  network route, data retention, and installation changes. Source-review
  permission is not installation or real-data registration permission.
- Use a current, expiring Autoposter `hai:read` token created in its Settings.
  The normal Autoposter login token is not a connector credential.
- HAI's normal PostgreSQL migrations must be applied, including
  `pre/0131_account_feed_reference_checkpoints`,
  `pre/0132_account_feed_reference_controls`, and
  `pre/0133_account_feed_autoposter_source_type`. References use the existing
  operations table; these additive migrations store owner/workspace/feed-scoped
  progress and source stop state. Missing or invalid control/checkpoint storage
  fails closed instead of reverting to an in-memory fallback.

## Operator configuration

Set these on the **HAI backend**, not the frontend. The local Compose file
passes them through, with the connector disabled when the base URL is empty.
Never commit the real token or put it in URLs, screenshots, logs, or examples.

| Setting | Meaning |
| --- | --- |
| `HAI_AUTOPOSTER_BASE_URL` | Exact Autoposter application base, optionally including a mount path; no query, fragment, or URL credentials |
| `HAI_AUTOPOSTER_REMOTE_OWNER` | Exact `owner` returned by Autoposter's authenticated `/api/hai/status` |
| `HAI_AUTOPOSTER_TOKEN` | Secret `hai_...` read-only token, retained only in server configuration/memory |
| `HAI_PHASE2_OWNER` | HAI authenticated owner subject that is allowed to view and sync the feed |
| `HAI_PHASE2_WORKSPACE_ID` | HAI workspace, default `local` |

HTTPS with normal certificate verification is required except for literal
loopback HTTP (`http://127.0.0.1:8000`, for example). `localhost` HTTP and
non-loopback HTTP are rejected. A container's loopback is not the Windows host:
use an explicitly reviewed HTTPS route when the services are on different
hosts or in separate containers. No ngrok tunnel is opened by this connector.

The token is bound to this exact endpoint, remote owner, HAI owner, and workspace.
API callers cannot override its destination or choose a different credential.
Redirects are not followed, inherited HTTP proxies are not used, and HTTP error
bodies are not copied into sync reports. The generic HTTP-feeds enable flag does
not enable or configure this separate credential-bound provider.

An explicitly configured but invalid connector prevents backend initialization
with a sanitized error. Leaving the base URL empty disables it. Changing any
identity binding creates a different feed identity; it does not repurpose old
references or delete them. Plan retention/migration of the old identity first.

## Use and inspect

1. Sign in to HAI as the configured owner and open **Account Bridges**.
2. Inspect the `secondhand_autoposter` feed, then select **Sync now**. This is a real
   network read, so do this only after the operator approvals above.
3. Inspect the result. Failures are errors, not a successful sync; remaining
   pages produce a warning to run another bounded batch.
4. Inspect archived `read_only_reference` rows in the Operation Ledger, including
   `sourceUri`, current evidence, and `worldModel.source_change_id`.
5. Change and delete a synthetic listing first. Verify the actual persisted
   content, not only the HTTP status or item count.
6. Select **Disable** when imports should stop. Wait for the confirmed result.
   The disabled state survives HAI restarts and applies to all HAI processes
   sharing this database. Select **Enable** explicitly to allow a new manual
   Sync; enabling alone does not fetch source records.

The corresponding authenticated HAI API endpoints are
`GET /api/v1/account-feeds`, `PATCH /api/v1/account-feeds/{id}` with
`{"enabled":false}` or `{"enabled":true}`, `POST /api/v1/account-feeds/{id}/sync`,
`POST /api/v1/account-feeds/sync-due`, and
`GET /api/v1/account-feeds/{id}/audit`. Existing read/write/admin permissions
still apply. Feed listing, individual reads, patches, sync, sync-due, and audit
are owner/workspace scoped. `sync-due` checks enabled feeds for the caller; it
is not a newly installed periodic scheduler.

### What Disable guarantees

The API acknowledges the change only after it is stored. A storage failure
returns an error, and the UI does not optimistically claim it was saved. A lost
HTTP response is ambiguous: refresh the feed to confirm its actual state.

Both direct calls and the HTTP PATCH path apply a five-second database-operation
context, preserving any earlier caller deadline or cancellation. A blocked
source lock therefore does not leave the HTTP control request waiting without
an application deadline. A timeout returns HTTP 503 without claiming success;
refresh before retrying because a failure around commit can still have an
ambiguous outcome.

Disabled sources cannot start direct or sync-due reads. Queued requests retain
the control revision under which they started; disabling and re-enabling does
not revive them. A local active request is canceled, other processes re-check
the control before each page, and the database rejects uncommitted pages whose
control revision is stale or disabled. The same source-scoped transaction lock
orders page commits and control changes. Pages committed before Disable takes
effect remain imported; requests already sent over the network cannot be
recalled. No existing reference or checkpoint is deleted by Disable.

The current UI offers this persistent control for Autoposter only. Other
registered feed types retain their existing configuration lifecycle. The Sync
button is disabled when a feed is off, but server-side checks remain the
authority even for callers bypassing the UI.

## Data, replay, and deletion contract

- Up to 100 records per page, ten pages per sync. `hasMore=true` means more work
  remains. Each HTTP response is capped at 5 MiB; content is capped at 200,000
  bytes and metadata at 16,000 bytes. Oversized/malformed pages fail explicitly.
- HTTP calls have ten-second timeouts inside a thirty-second sync context,
  including time spent waiting for the per-feed lane. The
  PostgreSQL writes use that context. There is no unbounded retry loop; retry a
  failed batch only after addressing credentials, limits, network, or storage.
- The entire page is validated before storage. PostgreSQL commits all of its
  reference changes and the opaque cursor/last change ID in one transaction.
  A failed record or checkpoint write rolls back that whole page. Previously
  completed pages remain committed, and reports count only committed records.
  If the connection fails while a commit is being acknowledged, its outcome
  can be unknown. A new sync reloads durable state instead of assuming rollback
  or blindly advancing a local cursor.
- A fresh HAI service/registry reads the durable checkpoint before requesting
  records, avoiding full-history replay on ordinary restarts. An empty response
  does not rewrite the database checkpoint. No bearer token is stored there.
- Cross-process sync uses a source-scoped advisory lock and compares the exact
  cursor/change ID read before the HTTP request. If another process advanced it,
  this page is rejected without writes; the report asks the user to retry from
  the stored progress. Network requests never hold a database transaction open.
- Stable endpoint/owner/feed/listing identities prevent duplicates. Every
  distinct producer `change_id` is an immutable terminal reference revision;
  replaying the same revision cannot overwrite its source evidence. Each
  PostgreSQL reference is additionally serialized for its identity with an
  advisory lock, including across independent service instances. Page locks
  use deterministic record ordering to avoid cross-page lock-order conflicts.
- A durable decimal change sequence rejects non-advancing pages. Timestamps are
  descriptive, not the ordering authority. A listing update creates a new
  immutable reference revision rather than mutating a previous observation.
- A tombstone creates a minimal deletion revision with no prior description,
  URL, or source metadata. Earlier imported revisions remain in the ledger;
  this connector does not purge historical references, database backups, prior
  manual exports, or other copies an operator made.
- Revoking a token, deleting an Autoposter account, or disabling the feed stops
  future access; none of those actions remotely purges existing HAI references.
  Account deletion can revoke access before pending deletion events are read.
  Retention and purge decisions require an explicit operator workflow.
- Source metadata never grants execution authority. Imports are reference-only;
  no model/provider call, marketplace write, or HAI executor is invoked.

## Checkpoint operations and recovery

The production store is `public.account_feed_reference_checkpoints`. Its
composite primary key is `(owner_user_id, workspace_id, feed_id)`; the data is
only an opaque cursor, last numeric sequence, and update time. The configured
feed identity includes the endpoint, remote owner, HAI owner, and workspace.
Token rotation alone does not change that identity. The companion
`public.account_feed_reference_controls` table stores the enabled flag and
monotonic control revision under the same key. Keep both tables and reference
rows in the **same PostgreSQL backup/restore boundary**.

For source review/deployment, use the normal `backend migrate status` and
`backend migrate up` workflow in [Database migrations](migrations.md). No
installed database was migrated by preparing this change. The migration is
additive and does not rewrite or remove existing operation rows. An existing
feed with records but no checkpoint replays once, safely using per-record
sequence protection, then resumes incrementally on later restarts.

On a page-storage or competing-sync error, retry after fixing the reported
problem. Do not manually invent a cursor, skip a failed page, or erase progress
to make an error disappear. A returned non-advancing change sequence is rejected
before storage. An invalid or obsolete producer cursor requires operator review;
there is no automatic fallback to a full replay.

Prefer deploying compatible application code while retaining the additive tables
if a rollback is necessary. Stop all connector processes, remove the connector
configuration, and retain a backup before any schema rollback. Removing
`pre/0132_account_feed_reference_controls` loses the saved disabled state, so
do not restart a configured connector assuming that stop state still exists.
If an explicitly approved rollback also needs to remove pagination progress,
`backend migrate down pre/0133_account_feed_autoposter_source_type` must precede
`backend migrate down pre/0132_account_feed_reference_controls`, which must
precede `backend migrate down pre/0131_account_feed_reference_checkpoints`. Migrations
cannot be rolled back out of order. References are retained, but progress is lost;
reapplying the up migration requires a full safe replay of the same source
history. This is not a source-reset or deletion procedure. This port has not
executed a migration rollback against a live or disposable PostgreSQL database.

## Explicit remaining limitations

The registry's name/operation-type overrides, last-run health, and feed audit
remain in memory. Autoposter's enabled flag and sync checkpoint are persisted
in PostgreSQL and restored on startup. Generic feed configurations are still
in memory. The DB-free memory repository
survives only service/registry recreation in the same process, not process exit.
A reviewed unattended polling lifecycle remains follow-up work. Source sequence continuity
after an Autoposter database restore/replacement must be reviewed: do not reset
the source history behind an existing feed identity and assume imports update.
Deleting only the checkpoint cannot reset retained per-record sequence guards.
Restoring/replacing a producer can also yield an empty response at an old cursor;
an empty sync alone is not evidence that a replacement source was reconciled.

No signed-in human walkthrough, screen-reader/keyboard/zoom acceptance, target
network/TLS drill, production deployment, or installed HAI acceptance is implied
by the local automated tests below. The consumer source remains a review change.

## Current port verification (2026-10-16)

- The current `main` migration tail already includes generic account-feed
  cursors and automation approval-proof changes through `0130`. This connector
  uses `0131` through `0133`; apply the migrations in that order. Its migration-chain test
  recognizes the optional `0128` predecessor both before and after that merge.

- Focused account-feed, operations, and router tests passed in Docker using the
  repository's Go 1.27.2 toolchain. The complete focused command, including
  migration-package tests, passed:
  `go test ./internal/accountfeed ./internal/operations ./internal/router ./migrations`.
- The integration-tagged account-feed test compiled with
  `go test -tags=integration ./internal/accountfeed -run '^$'`; it was not
  executed because no disposable Autoposter instance or test database was
  configured.
- The broader full Go suite was attempted before this verification pass and
  failed in unrelated packages including `internal/background`,
  `internal/executionauth`, and `internal/runtimelab`. Failures included
  timeouts and contract assertions. It is not a passing full-suite result.
- The rendered Angular test for the Enable/Disable/Sync flow was added but could
  not be run: this worktree has no `frontend/node_modules`, and the available
  Node runtime (25.2.1) does not satisfy the frontend's Node 24.15 requirement.
- The live Autoposter API, PostgreSQL integration tests, production migrations,
  frontend build/browser acceptance, and GitHub Actions have not been exercised
  for this port.
- These changes are local and uncommitted. They are not deployed or accepted as
  a working production integration.

## Historical source-branch verification notes (2026-09-13)

The following results were reported for an earlier source branch and are kept
only as provenance. They have **not** been reproduced against this port. That
branch used mutable reference rows; this port now uses immutable per-change
revisions, so its database/UI results do not establish current behavior.

### Consumer stall investigation and HTTP timeout correction

An earlier packaged integration run failed after fetching a tombstone; its
wrapper did not preserve the Go failure output. A successful rerun was not a
diagnosis. A fresh, caller-owned PostgreSQL 16 container and the Windows EXE
subsequently completed the original real-producer test **20 consecutive times**
in 45.493 seconds. The original intermittent failure was not reproduced, and
its cause remains unconfirmed. No speculative database/session fix was made.

That investigation separately reproduced a missing timeout on the HTTP source
control path: the five-second deadline existed only in the direct-call wrapper,
which the HTTP handler bypassed. The deadline now lives in `PatchContext`.
The actual-handler regression first failed without a caller deadline and passed
after the correction; a shorter caller deadline remains unchanged. The
integration test additionally holds the real PostgreSQL source advisory lock,
requires a bounded HTTP 503, checks that persisted/cached state was not changed,
and then exercises a successful disable and database-pool restart. Phase markers
now distinguish tombstone completion, disable acknowledgment, pool closure, and
reopening without logging credentials or source content.

After the correction, the updated real-producer test passed another **20
consecutive runs**, including **20 held-lock timeout checks**, in 152.044 seconds
(`go test ... -count=20 -v -timeout=5m`). The Go process exited successfully,
with no failed or skipped tests. The temporary outer wrapper initially counted
the nested PASS lines twice and failed its final count assertion; that parser
was corrected and the retained Go output was independently checked for exactly
20 top-level passes and 20 nested lock checks. This wrapper bookkeeping error
is distinct from the earlier unexplained consumer failure. All owned test
processes and the disposable database container were stopped. The full backend
Go test suite and build, focused vet checks, and 20 repetitions of the HTTP
deadline unit test passed as well; unchanged Go packages used cached results.

These HAI changes remain local, uncommitted, unpublished, and uninstalled. They
do not establish production availability or explain the original failed run.

### Other recorded verification

- The full Go backend suite (`go test ./...`) and backend build (`go build ./...`)
  passed. Accountfeed, operations, and router `go vet` checks passed as well.
- Consumer tests exercise authenticated GETs, owner/destination binding, page
  validation, updates, tombstones, replay/restart, serialized concurrent sync,
  401/403/429/500 errors, redirect rejection, response bounds, and API ownership.
- A fresh disposable PostgreSQL 16 container passed the real HAI pre-migrations,
  reference replacement/replay, owner isolation, and concurrent newest-sequence
  retention through the actual Gorm repository. Checkpoint tests also verified
  atomic page rollback, a forced checkpoint database-constraint failure,
  cancellation, concurrent checkpoint conflicts, and migration down/up while
  retaining references. The PostgreSQL suite passed twice on the same database.
- Stop-control checks reproduced and fixed direct-sync bypass, queued reads
  after disabling, and restart re-enabling. Tests cover local request cancellation,
  cross-registry stop changes, queued/in-flight disable/re-enable fencing,
  owner/workspace isolation, and failed control writes without false acknowledgment.
- The rebuilt Windows Autoposter EXE served 101 synthetic listings to this
  consumer and PostgreSQL. The test verified two-page ingestion, empty sync,
  update, deletion, the real owner-scoped HTTP handler saving Disable, and a
  fresh database connection/service/registry retaining that disabled state.
  Explicit Enable then resumed with **zero records replayed**. The test also
  checked token revocation, private-note exclusion, and terminal/non-executable
  reference rows. The test actually ran
  and passed; a skipped integration test is not counted as evidence.
- Five Angular notification regressions first failed on false success,
  unreported backlog, and misleading reference counts; they passed after the UI
  correction. These are browser-hosted component-method tests, not manual UX QA.
- All 390 frontend tests passed in Chrome Headless, including rendered native
  Disable/Enable button interactions through the actual Angular HTTP service
  with a test HTTP backend. Component checks cover failed/mismatched saves,
  duplicate clicks, and stale refreshes. These are not human accessibility or
  real-user acceptance tests. The Angular production build
  passed on the bundled Node 24.19.0 runtime, with five stylesheet-size warnings
  in untouched pages. Local Compose configuration validation passed without
  starting or modifying the installed Compose stack.
- The review branch's CI configuration now includes accountfeed/operations race
  checks and the disposable PostgreSQL checkpoint suite, requiring an explicit
  test PASS marker rather than accepting skipped tests. Those CI changes have
  not been pushed or run on GitHub. The local Windows environment has no C
  compiler on PATH, so no local race-detector pass is claimed.
- All 24 local CI/smoke-auth contract tests passed. These inspect workflow and
  authentication-script contracts; they do not substitute for running GitHub CI
  or a signed-in installed-service acceptance test.

From `backend`, ordinary checks are:

```sh
go test ./internal/accountfeed ./internal/operations ./internal/router
go vet ./internal/accountfeed ./internal/operations ./internal/router
```

The integration tests require `AUTOPOSTER_TEST_DISPOSABLE=1` and an explicit
`HAI_TEST_DATABASE_DSN` for an isolated test database. The cross-repository test
also requires `AUTOPOSTER_TEST_URL` pointing to a caller-owned, disposable
literal-loopback Autoposter instance. It creates synthetic accounts/listings;
**never use a personal or production instance**.

```sh
go test -tags integration ./internal/operations -run TestAutoposterReferencePostgres -count=1 -v
go test -tags integration ./internal/accountfeed -run TestAutoposterRealProducerToPostgres -count=1 -v
```
