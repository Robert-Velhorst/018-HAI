# HAI local cleanup readiness: 2026-07-30 child-session archive

This is a dated inventory and preservation ledger for the July 30 HAI
completed-agent-session archive. It is separate from the August
cohort documented in `docs/child-agent-integration-ledger.md`. Hashing and
ledgering do not themselves authorize or perform deletion.

## Transcript inventory

The audited source contains 18 JSONL files occupying 20,739,169,122 bytes
(about 19.315 GiB). They represent 16 unique child IDs because two IDs each
have duplicate transcript files. A bounded full-file rescan on 2026-10-12 with
a 10 MiB record limit found two oversized records in two files. Those files
remain unattributed even where an earlier full parse observed a terminal
message. The previous terminal messages remain observations, not proof that the
bounded scan independently classified the whole file.

| Disposition | Files | Bytes | Meaning |
| --- | ---: | ---: | --- |
| Hashed cleanup candidates | 5 | 3,179,439,595 | Completed, uniquely attributed transcripts with exact SHA-256; candidate only, not deleted. |
| Retain | 13 | 17,559,729,527 | Oversized/unattributed, duplicate-ID, aborted, or nonterminal transcripts. |

The generated manifest records per-file size, verified and observed status,
turn attribution, skipped oversized/malformed-record counts, disposition,
final-message hash, and (for each candidate) full transcript SHA-256. The five
candidate files were hashed after the bounded scan; no source length or
last-write-time change was observed while hashing. Preserved reports and their
qualification are in `child-agent-final-reports.md`; machine-readable counts
are in `child-agent-transcript-summary.json`. `deletion_performed` is `false`.

## Integration assessment and limits

- The source checkout contains implementations for Framework Registry, task,
  workflow-task, and IDP areas referenced by completed reports. Focused current
  backend tests passed for `frameworkregistry`, `task`, `workflowtask`, and
  `automation`; IDP package tests also passed after dependency metadata was
  tidied. This is code/test evidence, not proof of production deployment or
  live integration acceptance.
- The completed child reports have been retained verbatim after credential and
  local-repository-root redaction. Advisory findings in those reports are
  historical observations, not automatically resolved findings. In
  particular, the archive alone cannot establish the current status of every
  approval-binding, criterion-level evidence, task recovery, registry history,
  or mutation-audit concern. Recheck the current completion matrices and live
  acceptance gates before claiming those closed.
- Implementation follow-up (2026-10-10): the active `codex/hai-runtime-release`
  worktree now carries the workflow success-criteria path end to end (intake
  form, pursuit routing, workflow persistence, and task-runner handoff), plus
  owner-scoped append-only framework-preference history with digest validation
  on reads and chain checks between returned events. The Framework Registry
  frontend now fetches and displays before/after preference snapshots and
  digests, with explicit retryable load errors. New migrations are
  sequential after the checked-in `0115` tail (`0116` and `0117`); workflow
  rollback refuses to discard non-empty criteria. The changes were committed
  as `29e974323b081f339d248e2e25e223fb52752cc2` on
  `codex/hai-runtime-release`; `git ls-remote` confirmed that exact commit at
  the remote branch. `go test ./...` passed in the backend before the checkpoint,
  but Go is unavailable in the current shell for a rerun. Frontend Angular
  checks could not run because this worktree has no complete `node_modules`
  tree. The GitHub connector required reauthentication, so PR status and checks
  were not verified. This is source/checkpoint evidence, not release or live DB
  acceptance.
- Completed-report cross-check: exact action-bound approval proof, value and
  negation-aware criterion evidence matching, approved-review reconciliation,
  Constitution history with `baseVersion`, selection-history error states,
  and framework selection detail fields are present in current source with
  focused tests or route/template coverage. The previously blank registry
  template was replaced by the implemented inspector/recommendation views.
  These are repository checks, not production acceptance. Preference audit
  history and explicit workflow success criteria were included in checkpoint
  `29e9743`; their frontend behavior is not yet compiled or browser-tested.

### Completed-report integration crosswalk (2026-10-12)

This maps the five manifest rows marked `candidate_after_ledger_commit` to the
preserved report and current repository evidence. It records source integration,
not permission to remove the source transcript. Duplicate-ID and oversized
transcripts remain retained as shown in the manifest. The 10 MiB rescan now
attributes two completed reports (Copernicus and Aristotle) that the earlier
8 MiB scan retained only as observations.

| Child | Report kind | Integrated finding / disposition | Current repository evidence |
| --- | --- | --- | --- |
| `019fb287-b2e4-7251-8f1b-bc8fa9febc07` | Advisory | Action-bound approval, value/negation-aware criterion evidence, approved-review reconciliation, and workflow-owned success criteria are wired and covered by focused tests. | `backend/internal/task/validation_test.go`, `review_reconciliation_test.go`; `backend/internal/workflow/service_test.go`; `backend/internal/workflowtask/runner_test.go`; workflow intake spec. |
| `019fb287-d1ed-75f0-99a8-8cd09c1ef9f2` | Advisory | Owner-scoped Constitution history and append-only framework-preference history are exposed; preference events are digest-checked, and frontend contracts preserve `baseVersion`. | `backend/internal/frameworkregistry/history_test.go`, `handler.go`, `repository.go`; routes `/frameworks/:id/preference-history` and `/constitution/history`; frontend registry model, service, component, and specs. |
| `019fb287-f3be-7860-ba1f-e6b05088db73` | Advisory | Selection-history fetch failures render as unavailable/stale rather than empty history; workflow framework inspector includes conflicts, context requirements, and learning plan. | `frontend/src/app/pages/framework-registry/framework-registry.component.html`; `frontend/src/app/models/workflow.model.interface.ts`; Workflow Engine template and spec. |
| `019fb20c-2b95-7550-8304-1117de872148` | Implementation | Pursuit approval tests use the owner role for approval-only paths while preserving non-approver coverage. | `backend/internal/pursuit/handler_test.go`. |
| `019fb220-c797-77d1-8060-0f220cd733e1` | Implementation | Constitution base-version provenance and stale activation protection, first-owner draft selection, deferred active-Constitution invariant, and deterministic catalog digest are present. | `backend/internal/frameworkregistry/catalog_test.go`, `constitution_rules_test.go`, `migration_contract_test.go`, `repository.go`, `repository_integration_test.go`, `repository_postgres_test.go`, `repository_test.go`, `selector_test.go`, `service.go`, `service_test.go`, `types.go`; `backend/internal/models/framework_registry.go`; `backend/migrations/pre/0003_framework_registry.up.sql`, `0003_framework_registry.down.sql`; `docs/framework-registry.md`. |

### Retained-transcript work assessment (2026-10-10)

The ten manifest rows marked `retain*` are outside the eight-row candidate
crosswalk above. Their successful patch outputs were checked against the
current tracked source tree so the retention decision does not hide completed
work:

| Retained child/file | Archived patch evidence | Current source disposition |
| --- | --- | --- |
| `019fb20b-b4b1-7250-9d7c-9d1e591ab852` (completed duplicate-ID transcript) | The completed report says the approval-boundary security test was restored; the transcript tail records successful approval/runtime edits and a later failed patch. | The approval security test, automation service, and agent-runtime files are tracked and updated in commit `d3ad560`. Backend CI passed. The transcript/report discrepancy and duplicate ID are why both files remain retained. |
| `019fb277-0ddf-7500-9088-52cb4655be1a` (aborted duplicate-ID transcript) | Successful patch output touched workflow criteria and task evidence validation; later patch attempts also failed. | Workflow and task paths are tracked in commits `29e9743` and `d3ad560`; backend CI passed. The duplicate-ID companion is nonterminal, so both transcripts remain retained. |
| `019fb296-75ae-7943-913c-32d41f5de2d6` (aborted) | One successful patch changed task evidence validation and tests. | `backend/internal/task/validation.go` and its tests are tracked in `d3ad560`; backend CI passed. |
| `019fb296-9d1a-7301-901c-c35d99ccf3da` (aborted) | One successful patch changed Constitution history types, service, repository, handler, routes, and tests. | These paths are tracked in `29e9743`/`d3ad560`; backend CI passed. |
| Remaining six retained rows | The initial shallow audit reviewed user-message events and shell-call records and found no child-local patch calls. A corrective stream found thousands of `patch_apply_end` events in each transcript file. | Sampled event metadata ties those patch records to the pinned parent session: the transcript `session_id`, `parent_thread_id`, and sampled patch `turn_id` all identify root `019e7acc-44f2-7c90-a04e-253f6d43df28`. These are repeated/shared parent-history events, not proof that each child independently changed hundreds of files. Child-specific attribution and normalized path reconciliation remain open. Do not replay, discard, or clean these records until the crosswalk is complete. |

This is a source-presence and CI cross-check, not proof that each historical
patch was applied byte-for-byte or that the product is deployed. The full
transcripts remain the authoritative record for unresolved/partial work. The
eight candidate hashes are verified; the ten retained transcripts are not
eligible for deletion under the current ledger policy. A corrective bounded
stream over retained transcripts observed roughly 2,783-3,610 parent-history
patch events and hundreds of paths in individual files. Sampled metadata
identifies these events as belonging to the pinned parent session, so repeated
counts must not be treated as unique child work. The initial path comparison
also failed to normalize a second HAI checkout root; its missing-path count is
not valid evidence of unintegrated files. Child-local patch attribution and a
deduplicated source-to-PR crosswalk remain incomplete. Some large compacted
records exceeded the parser's configured bound and were skipped. No claim of
complete transcript integration is made, and no transcript is eligible for
deletion while this remains open.

**Transcript cleanup gate is not yet satisfied.** The source changes,
crosswalk, reports, and manifest are committed on the PR branch. A fresh
read-only source audit confirms all 18 archive files are present and all eight
candidate files match their manifest hashes. At this earlier 2026-10-10
checkpoint, PR #36 was open at head
`aa110fae0a9ad541815e6bce4405f47724f5d36b`; run `38036603737` completed with
seven failed checks. This is a historical snapshot, not the current PR status.
See `docs/child-agent-integration-ledger.md` for later publication checkpoints
and the latest verified cleanup-gate state. The CI and canonical-main/merged-PR
gates did not pass at this checkpoint. Do not remove any candidate transcript
under the current cleanup procedure. The manifest/hash is a candidate allowlist
only, not a deletion command.

- The eight unique completed transcripts are candidates for archive cleanup
  only after the integration changes, ledger, and report/manifest are committed
  and independently verified in the intended repository history. Preserve all
  ten retained files.
  Do not delete duplicate-ID files, aborted work, or nonterminal work on the
  basis of file size or an empty final message.

### Read-only cleanup verifier (2026-10-10)

`scripts/test-hai-transcript-cleanup-readiness.ps1` checks summary counts,
manifest disposition and hashes, unique candidate IDs, exact crosswalk coverage,
and preserved report presence. When given `-TranscriptRoot`, it verifies every
manifest path, file type, and byte count (including all ten retained files),
then checks the full SHA-256 of each candidate. It has no deletion behavior and
always reports `cleanup_authorized=false`.

Before any later retention review, rerun it with `-RequireSourceArchive
-RequireCommittedLedger -RequireMergedPullRequest`. The repository-history gate
requires the four ledger artifacts to be tracked and unchanged on canonical
`main` at the expected repository origin. The PR gate requires PR #36 to be
merged into `main`, all reported checks to succeed, and the merge commit to be
in the checked-out history. This checks every reported result because the
repository currently reports no branch-protection-required checks. Missing
GitHub authentication, missing check results, any non-success check, a dirty
ledger, or an unmerged PR fails closed. Even when these gates pass, the command
remains read-only and does not authorize or perform transcript deletion.
`scripts/test-hai-transcript-cleanup-gate-contract.ps1` protects these
boundaries in the Windows recovery CI suite.

The unified inventory defaults to a low-I/O pass. It reports manifest-ledger
candidates and marks source integrity `not_requested`; it does not enumerate or
hash the 20+ GB transcript archive. To run the full source enumeration and
candidate SHA-256 audit, opt in explicitly:

```powershell
./scripts/get-hai-local-cleanup-readiness.ps1 -VerifyTranscriptArchive
```

That report is read-only. The full future transcript-removal gate invocation is:

```powershell
./scripts/test-hai-transcript-cleanup-readiness.ps1 `
  -TranscriptRoot 'D:\codex-temp\hai-completed-agent-sessions' `
  -RequireSourceArchive -RequireCommittedLedger -RequireMergedPullRequest
```

It must run from a clean checkout of `main` after PR #36 and all required checks
are complete. A normal source-hash verification intentionally reports
`cleanup_gate_ready=false` when the repository/PR gates were not requested.

At the earlier 8 MiB scan checkpoint, the ledger-only and source checks passed
with 18 rows, 8 candidate files (7,939,888,699 bytes), and 10 retained files.
That scan is superseded by the 10 MiB rescan below, which newly attributes two
completed reports and changes the current candidate/retained disposition. The
older hash check proves only that the archive matched that earlier manifest;
it does not validate the current candidate hashes or prove every event in the
retained histories is integrated, close the PR/CI and repository-history gates,
or authorize deletion.

The Windows backup preflight now inventories mounted HAI volumes by Compose
project and HAI container name, checks Docker's
`com.docker.volume.anonymous` ownership label, and refuses to certify a
complete bundle when a mounted volume is absent from the recovery allowlist.
A live read-only check on 2026-10-10 found three uncovered anonymous mounts:
Redis `/data` and the PostgreSQL data mounts on the runtime-role and
state-permissions helper containers. The shared recovery contract rejected
that exact inventory before any backup or cleanup operation. The focused
contract test covers detection and refusal; these mounts still need a defined
recovery or explicit non-persistent service contract before they can be
removed.

Run the current read-only checks from the repository root in PowerShell:

```powershell
.\scripts\test-hai-transcript-cleanup-readiness.ps1 `
  -TranscriptRoot 'D:\codex-temp\hai-completed-agent-sessions' `
  -RequireSourceArchive
.\scripts\test-hai-temp-fixture-cleanup-readiness.ps1
.\scripts\test-hai-volume-cleanup-readiness.ps1
```

All three commands report status only. They do not delete files, stop
containers, or remove volumes.

## Other local HAI data: refreshed snapshot (2026-10-10)

This is a point-in-time inventory; re-inventory before any future cleanup.
No local data was removed during this review. The attempt to remove clearly
synthetic temporary fixtures was blocked by the execution platform; that
restriction was not bypassed.

- Six `%TEMP%` folders matching the generated
  `hai-acceptance-<32 hex>` pattern occupy 474,396 bytes at the 2026-10-10
  refreshed snapshot. Four (432,534 bytes total) have a matching version-1
  manifest, generated Compose project, test-only `e2e-owner@example.test`
  identity, and fixture-contained bind sources. They contain generated
  compose/init/nginx/runtime-role/source/env files; the env and Compose files
  include synthetic credentials, and `sources/acceptance.txt` explicitly
  identifies itself as synthetic with no personal records. The verifier
  requires the exact generated file/directory inventory, rejects reparse
  points and out-of-root bind sources, and checks resource owner markers.
  `scripts/test-hai-temp-fixture-cleanup-readiness.ps1` hashes each file and
  confirms no related Docker container, network, or volume by project/owner
  labels or resource-name prefix. It classified all four as
  `candidate_manual_cleanup` with zero Docker resources. No folder was deleted.
  Two folders (41,862 bytes total) contain only `synthetic.env` and have no
  manifest or Compose definition; their provenance remains unresolved, so the
  verifier classifies them `retain_unverified`. The verifier's output is a
  read-only report; it does not authorize or perform deletion.
- A guarded operator path is available at
  `scripts/remove-hai-temp-fixtures.ps1`. It is dry-run by default and scans
  only the current Windows Temp root. Readiness requires at least 24 hours
  since fixture creation and the most recent fixture-file change. Removal
  requires exact owner IDs, `-Apply`, the count-bound phrase
  `REMOVE HAI TEMP FIXTURES <count>`, and PowerShell's high-impact
  `ShouldProcess` confirmation. It reruns the exact file-hash and Docker
  inventory check, rejects changed files/references and process command lines
  that mention a selected path, and removes only selected generated fixture
  directories. It cannot remove Docker resources, repository files, or the two
  manifest-less synthetic environment folders. This operator path does not
  change the PR, transcript, volume, image, worktree, or toolchain retention
  gates.
- Seven HAI volumes are present. A refreshed read-only Docker inventory on
  2026-10-10 found `018-hai-postgres-automation` and `018-hai-postgres-idp`
  running and mounting their respective Postgres data volumes. The backend
  container is `created` and references `018-hai-phase2-control-state`; the
  exited permission helper also referenced that state volume. The HAI Redis
  container is running but uses an anonymous volume, not the named
  `018-hai-redis-data` volume. Kafka, Ollama, named Redis, and Redpanda volumes
  had no HAI container references in this refreshed inventory. None of these
  attachment facts establishes that the underlying data is disposable:
  `018-hai-postgres-automation-data` (858.4 MB),
  `018-hai-redpanda-data` (251.7 MB),
  `018-hai-kafka-kraft-data` (53.5 MB),
  `018-hai-postgres-idp-data` (64.69 MB),
  `018-hai-ollama-local-data` (397.8 MB),
  `018-hai-redis-data` (908 B), and
  `018-hai-phase2-control-state` (164 B). The recovery contract covers only
  the two Postgres volumes and safety-control volume. Redpanda, Kafka, Ollama,
  and the named Redis volume remain outside verified export/restore coverage,
  so the backup script correctly refuses to certify a complete backup. The
  two Postgres volumes are actively mounted now; do not stop or remove them as
  part of cleanup. Unattached volume status is not proof that data is
  disposable. Keep all seven pending export/restore coverage and a retention
  decision. `scripts/test-hai-volume-cleanup-readiness.ps1` now performs a
  read-only local-engine inventory of all seven names, their attached
  containers, backup-method coverage, and HAI images. On this run it found four
  uncovered detached volumes, two active Postgres volume attachments, and the
  phase2 volume referenced by a created backend and failed helper. It also
  found three anonymous volumes mounted by the Redis container and two helper
  containers; these are not among the seven named volumes and must be included
  in any complete recovery assessment. The script reports
  `safe_to_remove=false` for every named volume and has no stop, export,
  restore, or deletion action.
- Six HAI-tagged images are present (`018-hai-backend:latest`,
  `018-hai-backend-migrate:latest`, `018-hai-idp:latest`,
  `018-hai-frontend:latest`, `018-hai-backend:local`, and
  `018-hai-nginxconfigmanager:latest`). The backend-migrate, IDP, frontend,
  and backend-local images are referenced by created/exited HAI containers;
  backend-latest and nginxconfigmanager currently have no container references.
  `018-hai-backend:local` is required by the Windows backup and restore
  tooling. Keep all six while the PR is unresolved and local rebuild/startup
  acceptance remains incomplete.
- Before the cleanup-readiness update, PR #36 was open at
  `f788d489f2c6843328cdf16e60f268e97eeb97b9` on `codex/hai-runtime-release`.
  CI run `38002967480` was terminal: backend tests, authenticated smoke, browser
  acceptance, frontend tests, migration integration, Promptfoo image,
  repository secret scan, two-account isolation, and Windows installer/signing
  guards failed. Gateway/Compose validation, native Windows runtime, IDP,
  provider fixture, nginx manager, runner contracts, and Windows smoke-path
  checks passed. The secret scan reported 27 redacted candidate findings in
  historical commits; they require triage, not blind allowlisting. This PR is
  not merge-ready on that run and its failure evidence remains needed. The
  cleanup-readiness update was committed as `9830786` and pushed to the same PR;
  the new CI run `38004875276` was pending when checked.
- The cleanup ledger and three read-only verifiers are now committed and pushed.
  Other untracked
  acceptance evidence, CI logs, frontend-job ZIP, scanner binary/ZIP, local
  patch, and isolated-acceptance note remain untouched. Do not remove them until
  the failed CI evidence has been reviewed and the PR status is resolved.
- The Go toolchain ZIP occupied 67,590,465 bytes in the prior snapshot; the
  installed toolchain can serve other repositories. Do not remove the shared
  toolchain as HAI-only data.
- The refreshed `018-hai` inventory has IDP, frontend, both Postgres services,
  and Redis running; the backend and runtime-role helper are created, while
  migration and permission-helper containers exited unsuccessfully. The stack
  is therefore live, unlike the previous inventory snapshot. This review did
  not change container or volume state. Joyce, ShareT, and LARO services remain
  running and were not modified.

## Current PR status (2026-10-10)

PR #36 remains open on `codex/hai-runtime-release`. CI run `38004949959` for
the prior head `d13ba7c` completed with failures in frontend and backend tests,
Windows installer/signing guards, guarded Windows runtime, browser acceptance,
two-account isolation, migration integration, Promptfoo safety image,
authenticated control-plane smoke, and repository secret scan. Gateway/Compose,
provider fixture, nginx manager, Windows path contract, and several runner
contracts passed. The failure details have not been cleared, so this run is not
a cleanup gate. The anonymous/uncovered-volume backup guard was then committed
as `d845534` and pushed; the PR points to that commit, and its new CI checks are
still running. No merge, transcript deletion, volume removal, or image removal
is authorized by these checks.

## Safe next steps

1. Resolve the open PR checks and land the recovery-contract changes, all three
   read-only cleanup verifiers, and this ledger through normal review; rerun the
   source verifier against the merged manifest before considering any
   transcript candidate.
2. The 8 transcript candidates remain gated on successful PR/CI and merged
   history. Retain all 10 other transcript files, including duplicate-ID,
   aborted, and nonterminal sessions.
3. Implement and rehearse export/restore for every remaining HAI persistent
   volume and anonymous mount. Until then the backup script must continue
   refusing to certify a complete installation backup or volume removal.
4. Re-run `scripts/test-hai-temp-fixture-cleanup-readiness.ps1` immediately
   before any future fixture cleanup. Only the four complete, hash-verified
   fixture folders with zero matching Docker resources are candidates; retain
   the two env-only folders unless their provenance is established.
5. Delete nothing if the platform blocks deletion. Do not bypass that control
   with another shell, runtime, or API.

## Detached volume recovery addition (2026-10-10)

`scripts/archive-hai-detached-volume.ps1` now provides an explicit export and
restore-drill path for the four known detached legacy volumes only:
Kafka KRaft, local Ollama cache, named Redis, and Redpanda. It requires the
local Docker engine, the already cached `018-hai-backend:local` image, and a
private archive directory. It refuses a source volume with any container
reference, uses a read-only mount for the source, disables network access, and
does not pull or substitute an image. It restores the archive into a bounded
1 GiB tmpfs, compares the restored contents and unchanged source against the
archive, then lets the temporary container dispose of the tmpfs. It creates no
persistent scratch volume. The source volume is never removed. A manifest is
written only after the restore comparison and private-file ACL checks pass; the
manifest records archive size and SHA-256.

`scripts/test-hai-detached-volume-archive-contract.ps1` covers refusal of
attached/changed sources, redacted failure diagnostics, absence of persistent
scratch-volume operations, source-read-only behavior, bounded tmpfs restore,
restore comparison, source non-removal, and the read-only verifier contract.
The verifier `scripts/verify-hai-detached-volume-archive.ps1` checks a generated
bundle's manifest and SHA-256, confirms the cached image and detached source
volume identities, and compares the current source against the archive without
writing to either. It always reports `safeToRemove=false` and
`cleanupAuthorized=false`; it is evidence for a later human review, not deletion
authority. Both scripts are covered by the protected Windows recovery CI step.
These contract tests do not exercise an actual Docker volume archive or restore.
Run the archive command only for one verified detached volume at a time and
retain its output bundle. The existing volume readiness verifier still reports
`safe_to_remove_any=false`; an archive/restore drill is evidence for recovery,
not automatic permission to remove a volume. Active Postgres,
phase2, Redis anonymous, and helper-container anonymous volumes remain outside
this new detached-volume path and continue to block complete installation
backup/removal certification.

The earlier inventory paragraphs above are historical snapshots. A later
refresh below records the actual local archive-and-restore results and
supersedes the earlier statement that no detached volume archive had yet been
made.

## Fresh local cleanup gate (2026-10-10)

This snapshot was refreshed from the Windows host and local Docker Desktop; it
is evidence for this check only and must be refreshed before any later cleanup.

- **Completed-agent sessions:** the source directory contained 18 files totaling
  20,739,169,122 bytes. The ledger contains 8 candidates totaling 7,939,888,699
  bytes and 10 retained entries. The full source-size inventory passed, and
  candidate SHA-256 values matched. No source file was removed. Candidate status
  is not equivalent to cleanup authorization.
- **Synthetic Temp fixtures:** 6 generated-name directories were inspected.
  Four exact-layout fixtures totaling 432,534 bytes had zero related containers,
  networks, or volumes and remain `candidate_manual_cleanup`; two empty or
  layout-mismatched directories remain `retain_unverified`. No directory was
  removed.
- **Docker volumes:** all 7 known named volumes were found. The automation and
  IDP Postgres volumes each remain attached to a healthy container; the phase2
  state volume is referenced by a created backend and an exited helper. Kafka,
  Ollama, named Redis, and Redpanda are detached but still lack a real archive
  and restore drill. Three anonymous HAI volume mounts remain attached to
  runtime/helper containers. `safe_to_remove_any=false`.
- **Docker images and runtime:** all 6 named HAI images remain installed. Four
  have container references, including a failed migration container; two
  (`018-hai-backend:latest` and `018-hai-nginxconfigmanager:latest`) currently
  have no container references and are held for retention review. Five HAI
  containers were running and healthy; backend/helper/migration containers
  also include `Created` and `Exited` states. No image is marked safe to remove.
  The images and volumes remain retained because the stack is still present and
  no full recovery/acceptance gate has passed.
- **PR worktree:** the isolated checkout is on `codex/hai-runtime-release` at
  `0853e73d69e9ec35daa6becc80ad460c6280ff87` and has 28 tracked/untracked status
  entries, including diagnostic and acceptance artifacts. It was left intact.
  A read-only GitHub CLI check confirmed PR #36 is open. Its 35 reported checks
  currently include 10 failures and 25 successes; the failing checks include
  backend/frontend builds, Postgres migration, secret scan, browser acceptance,
  and Windows runtime/installer gates. No reported checks are marked required,
  so the transcript deletion gate correctly remains closed.

The detached-volume archive and read-only source verifier are implemented, but
have only passed contract checks and a synthetic tar/restore comparison. No
actual HAI volume has been exported or removed. The only change in this pass is
this ledger update and local verification; no local HAI data, containers,
volumes, images, worktrees, or diagnostic artifacts were deleted.

## Detached archive and cleanup refresh (2026-10-10)

This later refresh supersedes the detached-volume and archive-completion claims
in the earlier snapshot. All commands below were read-only with respect to the
source volumes and transcript/fixture folders. No files, volumes, images,
containers, worktrees, or toolchains were deleted.

- The archive tool now uses a private manifest/archive and a bounded 1 GiB
  tmpfs restore target; it creates no Docker scratch volume. The source volume
  is mounted read-only for both archival and verification. The manifest is
  emitted only after the restored copy and live source match the archive.
- The live verifier rechecked all four intended detached volumes, their source
  identities, archive hashes, and source contents. Results remain
  `safeToRemove=false` and `cleanupAuthorized=false`:
  - Kafka KRaft: bundle `hai-volume-recovery-a1489c6f199e4b8c93bdfe864d8275d5`,
    10,201,979 archive bytes, SHA-256
    `acffd54c11a877a80eb948bdf47a52141aa2fc2745a88b61c68ad9a11ebab32c`.
  - Ollama cache: bundle `hai-volume-recovery-8520099a46d940f39c72c5fcc520a2c5`,
    384,800,505 archive bytes, SHA-256
    `4bce3222e106f8d082035f4a30f232d508bfd9e859653a6f28dff910f8f7dab2`.
  - Named Redis: bundle `hai-volume-recovery-32738cc8c1cd46228bb7e9533a02925a`,
    663 archive bytes, SHA-256
    `4ae6bea1884d69a2c34e1d434ccdf86dd7c5a32b67883e3248d4916404f7a9fd`.
  - Redpanda: bundle `hai-volume-recovery-a428f75bb9e6442a9a44d4c3ce87fb82`,
    274,228 archive bytes, SHA-256
    `44fa5c75b3db1e2faf980e9ce6a81623c963539bcdae49fb236805068c73e704`.
- Five other attempt bundles are not recovery copies and remain untouched:
  four have no completion manifest; the fifth
  (`hai-volume-recovery-39d10dd71be04f40a91676fc080d3356`) fails the private
  ACL requirement. The readiness report now surfaces these as
  `retain_unverified` instead of treating their tar files as valid backups.
- `scripts/test-hai-volume-cleanup-readiness.ps1` now calls the independent
  verifier for generated bundles, reports verified and unverified bundle IDs,
  and changes the four covered volumes from `not_covered` only after the live
  source comparison passes. Even then it holds them for a retention decision;
  it never authorizes removal. Its latest live integrated run found seven
  named volumes, four current verified detached-volume archives, five
  unverified bundles, zero named volumes without a supported recovery method,
  and three named volumes without current verified recovery evidence (the two
  Postgres data volumes and phase2 control state). The Postgres/phase2 paths
  are supported by `backup-windows.ps1`, but that workflow was not run as part
  of this read-only check, so this is not evidence of a current backup or
  restore. All seven volumes remain `safe_to_remove=false`; the three active
  Postgres/phase2 volumes, three anonymous mounts, and all six HAI images remain
  held. `deletion_performed=false`.
- The current read-only Temp scan found four exact synthetic fixtures
  (432,534 bytes total) with zero related Docker resources, classified as
  `candidate_manual_cleanup`; two directories (one empty and one layout
  mismatch) remain `retain_unverified`. `cleanup_authorized=false` and
  `deletion_performed=false` for every directory.
- The transcript source check again verified the eight candidate hashes and
  full source archive inventory, but `cleanup_gate_ready=false`: PR #36 is
  still open. The verifier now enumerates the entire source tree, rejects
  reparse points, requires an exact manifest-to-disk path/count match, and
  compares aggregate source bytes with both manifest and ledger totals before
  reporting source verification. Public GitHub API evidence for head
  `14cd192c80e5fa2e05293a8fa83da63a7a7afa06` showed 36 checks, five failures,
  and no pending checks. The PR worktree and all untracked diagnostic evidence
  remain preserved.
- Dashboard diagnostics found the gateway unhealthy and backend container in
  `Created` state. The migration job refuses the database's eight applied
  `pre/0060`-`pre/0067` IDs because they are from a different migration
  lineage; a no-migration smoke test also failed runtime-role password
  authentication. No migration, credential change, or permission change was
  attempted. This is an additional reason to preserve the active database,
  images, and worktree until the stack is reconciled.

## Source and runtime recheck (2026-10-10 01:45 UTC)

This snapshot supersedes the earlier runtime and source-inventory observations
above. Checks are read-only; no archive, fixture, Docker resource, worktree, or
toolchain was deleted.

- **Transcript archive:** `scripts/test-hai-transcript-cleanup-readiness.ps1`
  passed `-RequireSourceArchive` against the local 20,739,169,122-byte source.
  The full tree contains exactly 18 files matching the ledger; reparse-point
  checks passed, all eight candidate hashes matched, and all 10 retained
  entries matched their recorded sizes. The 7,939,888,699 candidate bytes
  remain ineligible for cleanup until the ledger is committed on `main` and
  PR #36 is merged with successful checks.
- **Temp fixtures:** the live scanner found six generated-name directories:
  four exact synthetic fixtures (432,534 bytes) with no related Docker
  resources, and two empty/layout-mismatched directories retained as
  unverified. `cleanup_authorized=false`; `deletion_performed=false`.
- **Docker volumes:** the live inventory found seven named HAI volumes, no
  unknown HAI volume names, and three attached anonymous mounts. The two
  Postgres volumes are attached to healthy containers; phase2 state remains
  attached to the created backend and exited helper. Four detached volumes
  have current verified archives, but all seven volumes remain
  `safe_to_remove=false`; three volumes still lack current verified recovery
  evidence. Five recovery bundles remain unverified.
- **Docker images:** all six HAI images remain installed. Four have container
  references; the two unreferenced images remain `hold_retention_review`.
  `image_cleanup_authorized=false` and `deletion_performed=false`.
- **PR and verification:** PR #36 remains open at `17539e3efd1b9e13b73624dcb8d572173ff8fbcc`.
  CI run `38014306769` was queued at this check; its HAI cleanup-readiness
  safety-contract job had passed, but the complete run had not finished.
  The worktree and its 23 untracked diagnostics were preserved.

## Runtime and cleanup recheck (2026-10-10 01:59 UTC)

Read-only checks were repeated against the live Windows Docker engine and the
local transcript source. No transcript, Temp fixture, Docker volume, image,
worktree, or diagnostic artifact was removed or changed.

- The complete transcript source still contains exactly 18 files and
  20,739,169,122 logical bytes. All eight candidate hashes match the ledger;
  the ten retained entries match their recorded sizes. The gate remains
  `cleanup_gate_ready=false`, with `deletion_performed=false` and
  `cleanup_authorized=false`.
- The Temp scan still finds six generated-name directories: four exact
  synthetic candidates totaling 432,534 bytes and two unverified directories.
  All candidates have zero related Docker resources. The verifier continues to
  report `cleanup_authorized=false` and `deletion_performed=false`.
- The volume verifier still reports `safe_to_remove_any=false` for all seven
  named HAI volumes and `image_cleanup_authorized=false`. The two PostgreSQL
  volumes remain attached to healthy database containers; the phase2 state
  volume remains referenced by the created backend and exited helper. The
  active `automation_hub` database is 716 MB, so its volume is not disposable.
- The dashboard shell responds at `/control-center`, but API data is
  unavailable: the gateway is unhealthy, the backend remains in `Created`,
  and the migration job exited with code 1. Its fail-closed migration check
  reports eight applied `pre/0060` through `pre/0067` IDs absent from the
  current migration bundle. No migration, credential change, or database
  repair was attempted. This runtime/schema mismatch is an additional reason
  to preserve the database, attached state, and backend image until a verified
  recovery and migration-lineage repair are completed.
- At the 01:59 UTC check, PR #36 was open at
  `65fbf1f3be8640a78b492d4d68299f9b580db760`. CI run
  `38014491605` completed with failures in backend vulnerability scanning,
  repository secret scanning, Windows installer guards, real-Postgres
  migration integration, two-account isolation, authenticated control-plane
  smoke, the Promptfoo safety runner image, and browser acceptance. The
  dedicated HAI cleanup-readiness safety-contract job passed. The PR gate is
  still unmet, and the worktree's 23 untracked diagnostic artifacts remain
  untouched.
- This 01:59 UTC recheck was subsequently committed as
  `296366d741f2e5f4d3ed3e19a2073ada67437f50` and pushed to PR #36. New CI run
  `38015664226` was queued at 02:06 UTC; no result was available at this
  ledger update.

## Recovery-bundle diagnostics update (2026-10-10)

`scripts/test-hai-volume-cleanup-readiness.ps1` now includes stable failure
codes, manifest presence, last-modified time, and bundle byte counts for
unverified recovery bundles. It only computes an artifact hash after the file
passes the private-ACL check. When that check passes, it can identify an
unmanifested artifact as an exact byte duplicate of a current-source-verified
archive; the readiness script remains read-only and never authorizes deletion.

The live check found five unverified bundles totaling 50,997,691 bytes. One
bundle's manifest and four artifacts fail the required private-file-ACL check;
the other files remain unverified. All five are retained. The distinct older
Kafka archive and the valid-manifest bundle are not treated as disposable.
Four detached-volume archives remain verified against their current source;
three named volumes still lack current verified recovery evidence, including
the attached application and control-state volumes.

The generated Temp fixture scan found no eligible cleanup candidates: four
verified synthetic fixtures are about 11.7 hours old (below the 24-hour
retention minimum), and two folders remain unverified. No local file, Docker
container, volume, image, or service was deleted or changed during this
recheck.

## Local PR diagnostic artifact cleanup (2026-10-10)

The isolated PR worktree contains 15 inventoried local diagnostics/tool files
totalling 31,505,884 bytes: 12 downloaded CI logs, one frontend-job ZIP, and
the Gitleaks 8.30.1 Windows executable and ZIP. Exact relative paths, byte
sizes, and SHA-256 digests are recorded in
`scripts/local-hai-pr-artifact-cleanup-manifest.json`. The manifest is an
allowlist, not deletion authorization.

`scripts/remove-hai-pr-diagnostic-artifacts.ps1` is restricted to the exact
`D:\codex-temp\hai-pr-update-018-20261009` worktree and those 15 regular files.
It checks the expected repository and branch, requires PR #36 to be merged into
`main` at the exact local HEAD, requires every PR check to succeed, verifies
each manifest size and digest, refuses tracked files and running Gitleaks
processes, and then requires `-Apply`, a count-bound confirmation phrase, and
PowerShell `ShouldProcess`. It does not recursively remove directories or
touch Docker resources. The working-tree patch, isolated acceptance document,
outcome-evaluation test evidence, transcript archive, all runtime data, and
recovery bundles are explicitly retained.

### Unreferenced HAI image path

`scripts/remove-hai-unreferenced-image.ps1` provides a separate, opt-in path for
the five removable first-party tags in the audited inventory:
`018-hai-backend:latest`, `018-hai-backend-migrate:latest`,
`018-hai-idp:latest`, `018-hai-frontend:latest`, and
`018-hai-nginxconfigmanager:latest`. `018-hai-backend:local` is deliberately
excluded because the Windows backup and restore tooling requires it. Before
removal the script requires PR #36 to be merged into `main` at the exact local
HEAD and all PR checks to succeed; it validates each tag against its Compose
service, build definition and Dockerfile, HAI Compose ownership labels, sole
image tag, and absence of any container reference. It rechecks those
conditions after confirmation and uses `docker image rm` on each exact tag
only, never force-removes or prunes. It does not touch containers or volumes.
Reported image sizes are explicitly estimates, not guaranteed reclaimed disk
bytes because layers may be shared.
The live dry run found two unreferenced tags but blocked at the open-PR gate;
`deletion_performed=false`.

The image-removal contract is included in both Windows cleanup CI paths. It
checks the fixed image allowlist and guards against broad image/volume/container
prune operations. It is a source-level contract plus a live blocked dry run,
not a build or deletion acceptance test.

The safety contract is wired into both existing Windows cleanup-contract CI
paths. Local verification passed the complete cleanup-contract set and the
PowerShell parser. The live dry run verified all 15 artifact hashes and
reported 31,505,884 candidate bytes, but correctly blocked because PR #36 is
still open; `deletion_performed=false`. At this audit, PR #36 head remains
`48c5f6e3665abf9495cc6ecb77d13baa96a4fcd2`, with 21 successful checks, 7
failed checks, and 8 pending checks. No local diagnostic, archive, Temp
fixture, Docker volume, image, worktree, or service was deleted.

The transcript archive now has a separate removal command,
`scripts/remove-hai-completed-session-transcripts.ps1`. It is fail-closed and
targets only the eight manifest-listed completed transcripts whose reports and
integration crosswalks are preserved. It requires the exact inventoried source
path, canonical `main`, PR #36 merged into `main`, successful PR checks, a full
source/archive/hash audit, explicit child IDs or the complete manifest-approved
set, `-Apply`, a count-bound confirmation phrase, and PowerShell's high-impact
`ShouldProcess` confirmation. Before deleting it also checks for running
processes referencing the archive and rehashes every selected transcript. Its
postflight verifies that only manifest entries not selected for deletion
remain; it never removes the archive directory or unselected transcripts. A lightweight
branch/PR preflight prevents a multi-gigabyte source hash pass while the PR is
still open or checks are incomplete. This command has not been run against the
source archive; current PR state keeps it blocked.

The four legacy detached Docker volumes with current-source-verified restore
archives also have a narrowly scoped operator path at
`scripts/remove-hai-detached-volume.ps1`. It accepts only the exact Kafka,
Ollama, named Redis, and Redpanda volume names; verifies the local Docker
context, current source/restore archive, and detached status; requires `-Apply`,
a count-bound confirmation phrase, and `ShouldProcess`; and re-verifies each
volume immediately before removing only that exact named volume. It excludes
the PostgreSQL and phase2 control-state volumes, never uses `prune`, retains
the recovery archives, and reports partial outcomes. Its contract runs in CI.
No volume removal was performed.

## Temp fixture environment validation update (2026-10-10 05:43 UTC)

The Temp readiness audit now validates `synthetic.env` for complete fixtures
even when an older successful fixture has no `cleanup-manifest.json`. It
requires the checked-in `.env.example` to be tracked and unchanged in both the
staged and unstaged worktree, rejects malformed or duplicate keys, and accepts
nonempty credential-like settings only when they are generated markers or
exactly match that trusted template. New acceptance environments also clear
the expanded credential-key suffix set before applying the isolated test
overrides. This prevents a matching directory name or generic fixture manifest
from serving as the only proof that environment data is synthetic.

The current read-only Temp audit inspected nine matching directories: zero
were deletion candidates; seven were too recent (618,126 bytes total), and two
older markerless environment files remain unverified (41,862 bytes). The
command reported `deletion_performed=false`. The credential values were not
printed, changed, or removed.

## Transcript and Docker state recheck (2026-10-10 05:52 UTC)

The full read-only transcript source audit was run with
`scripts/test-hai-transcript-cleanup-readiness.ps1 -TranscriptRoot
D:\codex-temp\hai-completed-agent-sessions -RequireSourceArchive`. It verified
all 18 source files and 20,739,169,122 logical bytes against the manifest and
summary, matched all eight candidate transcript hashes, and found exactly
eight candidate integration-crosswalk rows. The eight candidates total
7,939,888,699 bytes; the other ten manifest entries remain retained. The
source archive is verified, but the cleanup gate remains false: this source-only
run did not request committed-ledger or merged-PR verification, and a separate
live check confirms PR #36 is still open. No transcript was changed or removed.
A later removal must still run the full gate from canonical `main` after PR #36
is merged and all its checks pass.

A fresh live volume check on Docker context `desktop-linux` found only three
HAI named volumes, and all are attached: `018-hai-phase2-control-state` has
two container references, while each Postgres volume has one. All three lack
current-source-verified recovery evidence and remain ineligible for removal.
The earlier Ollama local-model item is no longer a Docker volume; its
384,800,505-byte archive remains in the recovery directory. It is preserved as
recovery data, not treated as disposable volume data. A separate 10,189,079-
byte recovery bundle still fails private-ACL verification and remains
unverified. The readiness report returned `safe_to_remove_any=false` and
`deletion_performed=false`. No container, volume, image, or recovery archive
was changed.

## Secondary checkout reconciliation (2026-10-10 12:47 UTC)

The additional checkout
`C:\Users\NO\Documents\Codex\2026-05-30\github-plugin-github-openai-curated-noodzakelijk`
was inspected read-only and verified as the same `Robert-Velhorst/018-HAI`
repository. It is on `codex/hai-runtime-release` at
`e07b9daeb3ba2630ecbe12a64d948f791203c241` (2026-09-04). GitHub's compare API
confirms that commit is an ancestor of the current PR head, with 80 later
commits and no commits unique to the secondary checkout.

The checkout itself has 692 tracked files modified and 2,475 untracked files.
Its working data totals about 152.7 MB; `output/` accounts for about 127.0 MB.
The non-output path comparison against the current PR checkout found:

- All 692 tracked paths exist in the current PR checkout; one has identical
  SHA-256 content and 691 differ.
- Of 899 non-output untracked files, 858 have identical SHA-256 content to a
  corresponding file in the current PR checkout, 37 differ, and four are
  local test-evidence logs with no counterpart.
- No source file was copied, staged, committed, or removed during this audit.

The differing working-tree versions are not safe to import wholesale. Direct
comparison found examples that weaken current protections: an approved source
operation no longer binds approval to the reviewed owner type or review time;
an execution claim omits the current Robert-owner and review-time conditions;
the pinned OpenClaw signing public key is changed to a scanner-triggering
literal; and the Promptfoo runner is downgraded from the PR's `0.124.1` to
`0.123.1`. These local variants were not integrated. The remaining divergent
paths still require content-level review before this checkout can be removed.

The current PR #36 head is `75b71fc7646ef2f753eaaa414a46a40b6be37702`, open
and mergeable. Its current GitHub Actions run `38052358151` is still in
progress; the secret scan, authenticated control-plane smoke, Promptfoo audit,
Postgres migration integration, and Windows installer checks have failed in
the latest observed run, while browser acceptance is still running. The prior
run's logs identify additional concrete failure details, so PR acceptance and
cleanup remain blocked. The transcript archive, secondary checkout, 2,475
untracked files, output evidence, diagnostics, containers, images, volumes, and
recovery bundles remain untouched. `deletion_performed=false`.

## Live PR and cleanup gate update (2026-10-10 13:04 UTC)

The current PR #36 head is `e7ca8c85d6169e91d5c1e62eb344b8aca9e1887c` on
`codex/hai-runtime-release`, targeting `main`. GitHub reports the PR open and
mergeable. Run `38053311971` is not terminal: backend, IDP, frontend, gateway,
local provider fixture, two-account isolation, Windows CI contract, and the
Python runner contract jobs completed successfully. Five jobs completed with
failure: Windows installer preview/signing guards, repository secret scan,
authenticated control-plane smoke, Postgres migration integration, and the
Promptfoo safety runner image. The owned browser acceptance job remains in
progress. Failure logs for this run are not yet available while the workflow
remains active. These results are not a passing PR gate.

The live transcript source audit remains limited to the previously recorded
source/hash and manifest checks. A separate streaming pass found 71 distinct
commit references in parseable retained-transcript final answers; all 71
resolve to commits in the PR branch ancestry. This is useful corroboration, not
proof that every transcript item is integrated: 47 malformed or oversized
JSONL records were skipped by that parser, and commit references cannot prove
that uncommitted outputs or non-code decisions were incorporated. The retained
transcripts and secondary checkout therefore remain preserved and ineligible
for cleanup. No local source or recovery data was changed or removed.

## Shared execution-claim defect and PR gate recheck (2026-10-10 13:22 UTC)

The failed migration and authenticated-smoke logs for run `38054839348` exposed
a shared production defect in `backend/internal/operations/execution_claim.go`:
the claim query referenced `operation_events.payload`, while the schema stores
that JSON in `payload_json`. This made Postgres claim selection fail with
`SQLSTATE 42703`; background smoke failures cascaded from the same claim path,
and the model-intelligence lane had no operation to stamp or telemetry from
which to derive a lane winner. The query now uses the schema's `payload_json`
column. The change is pushed to PR #36 as `c6df3cd31a9cb45e9cf3c2bc2e95e43a74cf3ea1`.

Run `38054839348` tests the prior head and is still non-terminal while browser
acceptance runs; its migration, authenticated-smoke, Promptfoo audit, and secret
scan jobs failed. The current PR head is `c6df3cd31a9cb45e9cf3c2bc2e95e43a74cf3ea1`.
Its new run `38055436363` has already failed the Promptfoo production dependency
audit and repository secret scan; migration and authenticated smoke have not
finished, so the SQL repair is not yet verified by CI. The Promptfoo audit
reports ten high advisories for the pinned `0.124.1` dependency tree; its only
suggested package-level remediation is a downgrade to `0.116.7`, which has not
been adopted without compatibility and audit verification. The secret-scan
summary identifies historical candidates concentrated in synthetic fixtures,
tests, and workflow literals; no blanket suppression has been added and the
local and CI scans have not yet been reconciled.

The new run is not passing evidence and PR #36 remains open. Transcript
integration remains incomplete; the 20.7 GB archive, secondary checkout, and
all unrelated untracked diagnostics remain untouched. No local source or
recovery data was deleted; `deletion_performed=false`.

## Correction: post-fix verification and rollback guard (2026-10-10 13:28 UTC)

The `payload_json` correction is valid, but the earlier statement that the
authenticated background-smoke failures cascaded from that same query was not
proved. Run `38055436363` continued to report authenticated smoke failures
after the SQL fix was present, so the smoke root cause remains open. The model-
intelligence failures on that run also remain unverified against the corrected
head.

The current head at the time of this update is `d63f115`, pushed to PR #36.
Its only code change after `bf0d051` separates the phase-order rollback
assertion from a new isolated Postgres test that creates a registry-managed
source and exercises the migration's own data guard. `git diff --cached
--check` passed before commit. This Windows environment has no Go executable,
so the test has not been run locally. Run `38055567389` for `bf0d051` already
reported failures in the repository secret scan, Promptfoo dependency audit,
and authenticated control-plane smoke; its Postgres integration was still
running at the time of this entry. Run `38055436363` was also not terminal.
The new head's checks have not yet completed and must not be inferred from the
prior runs.

The transcript archive and secondary checkout remain preserved. Transcript
content-level integration is incomplete, so neither source is eligible for
deletion. Existing untracked evidence, logs, scanner binaries, and diagnostic
patches were not staged or removed. No archive, checkout, recovery data,
container, or volume cleanup has been performed.

## Rollback-fixture diagnosis (2026-10-10 13:33 UTC)

Directly retrieved the completed migration-job log for run `38055839177`.
The new rollback-guard fixture failed before reaching the migration guard:
its feed item used provider `local`, which `accountfeed.ParseProvider` rejects;
the sync report therefore had zero items and zero created operations. The
fixture now uses the supported `generic_json_feed` provider, matching the
existing accepted feed contract. This is a test-fixture correction only; the
managed-data rollback refusal is still unverified until the next real Postgres
integration run passes. The failed test is not evidence of a production
migration defect. No source archive, checkout, log, or recovery data was
removed.

## Candidate transcript parse integrity supplement (2026-10-10 18:58 UTC)

A streaming JSONL parse of the Aristotle candidate
(`019fb220-c797-77d1-8060-0f220cd733e1`) found four malformed records; the
Descartes candidate (`019fb221-54e7-7921-8c65-8a74da2e6d93`) found fourteen.
Each error is an unterminated JSON string at a record whose byte length is an
exact 2, 4, 6, or 8 MiB boundary. The records cannot be decoded as JSON and
their contents have not been certified against the PR. This supplements, and
does not replace, the eight-candidate path crosswalk. The archive hashes match
the manifest, but that proves only that the archived bytes are stable; it does
not prove these records are semantically complete. Keep both files and treat
their affected content as unresolved until a trustworthy source or export can
be reconciled.

The synced parent Codex conversation also contains the final child reports for
these two IDs (conversation `019fd0df-32c2-79b0-aae3-a9d3c566575b`, messages
21646, 21648, and 21652). Their report-named files were checked individually
against current PR `HEAD`: all 14 Framework Registry files named by Aristotle,
plus the Framework Registry template and the automation form HTML/SCSS named
by Descartes, are present. This recovers the final reports and proves source
presence for those 17 paths; it does not recover or verify the malformed raw
patch records. Aristotle's report says integration-tag tests compiled but
Postgres runtime assertions were skipped because `HAI_TEST_DATABASE_DSN` was
unset in that child environment. Do not treat the report as live database
runtime evidence.

The same checkpoint rechecked PR #36 at head
`c4d2aa752203df0ed17373cf1da496f29c95d989`, open and mergeable. Exact-head run
`38076417111` has passing backend, IDP, frontend, Postgres 17 migration,
authenticated smoke, secret scan, and cleanup-safety checks. The Promptfoo
production dependency audit and Windows installer preview/signing guards have
failed; browser acceptance remains in progress. A live npm audit of the locked
Promptfoo `0.124.1` production tree independently reports ten high and zero
critical advisories. npm offers only a major downgrade to `0.116.7`; no
downgrade or audit suppression was applied. The exact protected-recovery test
set passed locally under PowerShell 7.6.5, but that does not override the
failed GitHub check; its completed job log is still required to diagnose the
CI/local discrepancy. The PR is not green and no cleanup gate passes.

## Retained completion-event audit (2026-10-11)

The six retained files behind the previously unattributed completion-text
figure are now identified by exact `session_path` values in the manifest. The
read-only `scripts/audit-hai-retained-completions.ps1` validates each path and
byte length against that manifest, then scans the six files with a bounded
record reader and prints only counts derived from hashes, not message bodies
or hash values. Results: four
large aborted files each contain 580 completion
records and 542 non-empty final messages; two small aborted files contain no
completion records. The cross-file totals are 2,320 completion records, 2,168
non-empty messages, 542 unique trimmed-message hashes, and zero JSON parse
errors on matching records.

These counts establish provenance and exact-text deduplication, not unique
tasks or semantic integration. The 793-path event crosswalk and child-local
attribution remain incomplete. The audit changed no transcript and does not
alter the existing retention or cleanup decision.

## Live archive and PR gate recheck (2026-10-11)

The read-only cleanup-readiness verifier was run against
`D:\codex-temp\hai-completed-agent-sessions`. It verified all 18 manifest
paths, the complete logical byte total of `20,739,169,122`, and the recorded
SHA-256 values for all eight unique completed candidate files. The verifier
reported `source_archive_verified=true`, but
`committed_ledger_verified=false`, `merged_pull_request_verified=false`, and
`cleanup_gate_ready=false`. It performed no deletion.

At the source-verification checkpoint, PR #36 was open at head
`d6cb0903dd63e15ed9da685c8f42d831eda8c535`; exact-head run `38108110220` was
queued overall. This ledger update was subsequently pushed as
`b720ea1c1092b3b40ed0344fcf1264048a97d245`. At publication, the PR remains
open and its new exact-head run `38108719169` is queued. Neither status is
final acceptance. The 793-path event crosswalk and semantic attribution of
retained/aborted transcript work remain incomplete, so even hash-verified
candidate files are not eligible for removal. Preserve all 18 source
transcripts until those gaps are closed and the repository cleanup gate passes
after merge to `main`.

## Supplemental archive discovery (2026-10-11)

Two additional HAI-related folders were found outside the 18-file source root
checked above: `D:\codex-temp\completed-agent-sessions` (3 files,
4,651,926,405 bytes) and `D:\codex-temp\session-log-relief` (6 files,
9,361,810,893 bytes). Their metadata links all nine files to the same HAI
parent session; the three completed transcripts are unique and hash-verified,
while all six aborted/nonterminal transcripts remain protected.

The original verifier does not cover these roots. A separate read-only verifier
now checks their exact two-root inventory, all nine sizes, hashes for the three
completed candidates, candidate report IDs, and all 21 report-linked source
paths against tracked repository files. It also refuses reparse-point roots or
ancestors and uncommitted changes to its manifest/report evidence. The
transcript-removal preflight now requires that supplemental verifier to pass in
addition to the original archive, merged-PR, and CI gates. A unified
multi-root removal operation is not implemented: the existing remover can only
remove candidates from the original 18-file root, so the three supplemental
candidates remain protected. The six aborted/nonterminal supplemental files
remain protected regardless. The overall known archive is 27 files and
34,752,906,420 bytes. Supplemental audit artifacts remain in
`supplemental-child-batch-audit.md`,
`supplemental-child-transcript-manifest.csv`, and
`supplemental-child-final-reports.md`.

## Additional HAI source snapshots (2026-10-11)

The read-only cleanup inventory also found two HAI Git snapshots outside the
primary checkout and PR worktree:

| Snapshot | Git state at inspection | Decision |
| --- | --- | --- |
| `D:\codex-temp\hai-integration-snapshot` | Branch `codex/hai-integration-snapshot`, HEAD `91c8620c557229f1da4ed15fcbb7088c6a6947a7`; 1,794 tracked changed paths, 1,790 staged, 19 unstaged, 0 untracked, 0 unmerged | Retain; compare its changes against the canonical PR before any cleanup. |
| `D:\codex-temp\hai-merge-scratch-20261010` | Detached HEAD `e3843448a3e2af8a11556251af6a3861dd1a8577`; 206 tracked changed paths, 17 staged, 0 unstaged, 0 untracked, 189 unmerged | Retain; conflicted work is not reconciled. |

Both snapshots contain `.env-backend`, `.env-gateway`, and `.env-idp`. The
inventory recorded filenames only; it did not read, copy, or publish their
contents. No source code from either snapshot has been copied into the PR.
These counts are a point-in-time observation, not a cleanup authorization.
The readiness script now reports both snapshots as retain-only cleanup targets
and always sets `cleanup_authorized=false` and `deletion_performed=false` for
them. Their individual file changes still require reconciliation before they
can be considered redundant.

### Integration-snapshot code review (2026-10-11)

The staged snapshot diff was reviewed by subsystem before any source import.
It is not a safe branch to merge or copy wholesale. The following observed
differences are behaviorally significant and require isolated review against
tests and history:

| Area | Observed snapshot difference | Current disposition |
| --- | --- | --- |
| Background worker | Removes paused-mode safe-work deferral and the emergency-stop processing gate in `RunOnce`. | Reject as-is; this weakens policy behavior. |
| Browser verification | Removes workflow-linking request/result fields and relocates route initialization away from the workflow service. | Reject as-is; it drops a review signal already implemented in the PR. |
| Source evidence | Removes the raw-evidence digest field, digest calculation, and its migration. | Reject as-is; provenance/integrity coverage would regress. |
| Operation execution | Changes claim eligibility/CAS timestamps. More critically, replaces the hex-encoded PostgreSQL approval-lock identity with `owner + "\x00" + sourceID + "\x00" + decisionID` and passes it as a `text` query parameter. PostgreSQL disallows NUL in character strings ([official PostgreSQL docs](https://www.postgresql.org/docs/17/datatype-character.html)); the snapshot also removes `TestApprovalClaimLockIdentityIsTextSafeAndUnambiguous`. It also queries `operation_events.payload` instead of the canonical `payload_json` column (`backend/migrations/pre/0002_baseline.up.sql`, `backend/internal/models/operation.go`). | Reject as-is; source-approval claim transactions can fail on the lock parameter and the query references a nonexistent column. Preserve the text-safe encoding, canonical schema name, and integration coverage. |
| Pursuit intake | Re-reads scoped detail after intake instead of returning the response-only intake workflow reference. | Reject as-is; response data can be lost. |
| Test/CI wiring | Removes browser-verification test coverage and Playwright global setup while changing acceptance configuration. | Do not import until exact-head browser acceptance is shown to run. |
| Local credentials | The staged index adds `.env-backend`, `.env-gateway`, and `.env-idp`. | Never import or publish these files; their contents were not opened. |

This is a representative safety review, not a complete review of all 1,794
changed paths. No code was copied from the integration snapshot. Continue
reviewing the remaining changed paths in bounded subsystem batches, and retain
the snapshot until the full reconciliation and post-merge cleanup gate pass.

Additional configuration and release-path differences were then inspected:

| Area | Observed snapshot difference | Current disposition |
| --- | --- | --- |
| Secret-file hygiene | Removes `.env-backend`, `.env-gateway`, and `.env-idp` ignore rules while adding those files to the snapshot index. | Reject; do not publish the files or the ignore-rule removal. |
| SearXNG discovery | Deletes the optional local-only Compose profile, network definitions, settings, and related documentation/tests. | Do not import; this removes an existing capability and its isolation configuration. |
| Docker network setup | Removes configurable private subnets and Windows initialization code that preserves the operator's selected subnet values. | Do not import without overlap and upgrade-path evidence. |
| Windows environment migration | Removes post-replacement ACL/content verification and the verified rollback path for the existing environment file. | Reject as-is; failure recovery and permission-preservation guarantees regress. |
| Isolated acceptance | Removes service-dependency validation and changes backend tmpfs ownership setup. | Do not import without disposable-stack acceptance and permission tests. |
| CI and secret scanning | Removes transcript-integrity execution from CI, drops approval-claim/Postgres test selection, and removes the explicit pull-request base revision from secret scanning. | Reject as-is; coverage and scan scope are reduced. |

These findings cover additional high-impact paths, not every remaining test,
documentation, frontend, or service diff in the snapshot. The complete
path-by-path reconciliation is still open; counts and this representative
review are not evidence that remaining files are redundant.

### Frontend and browser-acceptance review (2026-10-11)

| Area | Observed snapshot difference | Current disposition |
| --- | --- | --- |
| Browser matrix authentication | Replaces the run-scoped synthetic storage-state setup and refresh-aware session reuse with credentials in the per-test fixture and a fresh login from `signIn`. | Do not import as-is; verify login-rate-limit behavior, secret-safe artifacts, and test isolation first. |
| Workflow review response | Replaces the explicit `reviewRequired` path that pauses the worker, reloads persisted workflow state, and refreshes the page with a generic uncertain result. | Reject as-is; it can hide the server's approval state from the operator. |
| Workflow match control | Removes the page-loading action guard from the button and deletes the test asserting matching does not run while loading. | Do not import without an equivalent readiness guard and focused regression coverage. |
| Authentication response contract | Changes `login` from `Observable<void>` to an inferred generic object response and deletes the test for the successful empty response. | Reject as-is; it weakens the declared contract without evidence of a backend response change. |
| End-to-end workflow proposal | Adds an isolated browser path for approval followed by a bounded read-only run and verification. | Potentially useful, but not accepted until the preceding auth, approval-state, fixture-isolation, and exact-runtime contracts are verified on the PR branch. |

No frontend or browser code from this batch has been copied into the PR. The
end-to-end execution proposal remains a candidate for a separately tested,
fail-closed port; the snapshot's broader changes are not required to adopt it.

### Windows packaging and runner-isolation review (2026-10-11)

The snapshot also removes the installer-payload exception for versioned
`backend/migrations/{pre,post}/NNNN_*.sql` files. Those files then match the
general `.sql` exclusion rule. Do not adopt this change until the installer
payload manifest, first-install path, and upgrade path prove that migrations
remain available from the packaged backend image or another verified source.
The same snapshot removes the install smoke test's temporary `LOCALAPPDATA`
profile override and several `RUNNER_TEMP` ownership/reparse-point checks.
Although the smoke script still requires a hosted disposable Windows runner and
checks the application profile is initially empty, retain the stronger isolated
profile and path-ownership checks until a replacement is independently tested.

### Backend and audit-evidence review (2026-10-11)

The remaining backend and migration test diffs include additional deletion
risks:

| Area | Observed snapshot difference | Current disposition |
| --- | --- | --- |
| Outcome evaluation | Removes defensive slice copies before normalization and deletes tests proving caller-owned nested data is unchanged and concurrent shared input does not race. | Reject as-is; normalization mutates caller-owned buffers and the regression coverage is removed. |
| Migration integrity | Removes 0118 source-evidence digest migration expectations and deletes isolated rollback/data-preservation cases from source-configuration/head tests. | Reject as-is; these tests prove upgrade and data-loss refusal contracts. |
| Transcript audit evidence | Deletes the supplemental manifest, final reports, batch audit, and `audit-hai-retained-completions.ps1` from the PR comparison. | Reject; these are current provenance and reconciliation evidence, not disposable generated output. |
| Browser-verification tests | Deletes the dedicated handler test while also removing the workflow-link response contract. | Reject as-is; retain explicit API and workflow-link coverage. |

This is still a risk-based review of the remaining high-impact diffs; no claim
is made that all changed paths have been individually adjudicated. The source
snapshot remains unreconciled and must not be deleted or merged wholesale.

### Canonical checkout-to-PR content comparison (2026-10-11)

The primary checkout at `e07b9dae` was compared with the canonical PR tree at
`332f9c3ac4aa179d63e26c9c03b1205b58592d72`. The PR commit descends from the
primary checkout commit. In the inspected source/config/test/doc candidate set
(excluding `output/`, `.claude-flow/`, and `.swarm/`), 1,449 paths have content
identical to the PR tree, 122 paths differ, and 12 paths are absent from the PR
tree. The absent paths are test-evidence/log outputs, not implementation source.
The equality count establishes duplication for those bytes only; it does not
resolve findings in untracked review reports or establish that the distinct
paths are redundant.

Representative differences already reviewed show why the 122-path remainder
cannot be copied or discarded as a unit: some local candidates downgrade Go
versions and remove CI/cleanup contracts; remove SearXNG isolation; regress the
PostgreSQL-safe approval-lock identity; drop evidence digests or workflow
success criteria; or weaken secret-scan reporting. Other paths still need
individual disposition. No local candidate was imported on the basis of this
comparison, and no differing path is authorized for deletion.

The primary checkout's `output/` tree contained 1,576 files totaling
126,970,802 bytes at inventory time. It includes source-review reports, test
receipts, raw test logs, JSON/JSONL output, scripts, binaries, archives, and
database snapshots. The report set includes both findings and later
parent-resolution notes; the reports have not all been mapped to exact PR source,
regression tests, and current CI evidence. Therefore the output tree is retained
pending a path-level evidence disposition. Exact duplicate generated artifacts
may be considered later only after their hash, retained copy, and lack of unique
audit value are recorded.

The PR remains open against `main` at the recorded head. The exact-head GitHub
check-runs snapshot returned 36 checks: 5 successful, 10 in progress, and 15
queued. This is not a green or complete CI result. No transcript archive,
integration snapshot, conflicted merge scratch, local environment file, source
candidate, test-evidence output, or diagnostic artifact was deleted, moved,
truncated, compressed, or archived as part of this comparison. Cleanup remains
unauthorized until the source and evidence reconciliation is complete and each
specific deletion has a verified retained counterpart or is proven reproducible
and non-unique.

### Path-aware checkout comparison refresh (2026-10-11)

A follow-up comparison used `git hash-object --stdin-paths` so the checkout's
normal Git clean filters, including Windows line-ending normalization, were
applied before comparing blobs. The primary checkout currently reports 692
tracked modifications and 2,475 untracked paths. Of 1,593 source/config/test/doc
candidate files (still excluding `output/`, `.claude-flow/`, and `.swarm/`),
1,459 match the PR tree, 122 differ, and 12 local test-evidence/log files are
absent from that tree. This corrects the earlier count of 1,449 identical files:
ten additional exact matches are present in the refreshed candidate inventory;
the 122 distinct paths and 12 absent evidence files are unchanged. The
`output/` inventory was also rechecked: 1,576 files, 126,970,802 bytes.

The following additional exact diffs were inspected from the primary checkout
against the PR worktree. The local variants are not suitable imports:

| Paths / area | Local difference | Disposition |
| --- | --- | --- |
| `backend/internal/executionauth/repository_postgres.go`, `repository_postgres_test.go` | Replaces the hex-encoded advisory-lock identity with NUL-delimited text, removes the text-safe identity regression, and scans `pg_advisory_xact_lock(...) IS NULL` into a boolean. | Reject; preserve the PR's PostgreSQL text-safe lock argument and regression. |
| `backend/internal/operations/domain.go`, `service.go` | Removes the raw evidence SHA-256 field, calculation, and change-detection path. | Reject; loses source-integrity provenance. |
| `backend/internal/workflow/service.go`, `backend/internal/pursuit/service.go`, `backend/internal/workflowtask/runner.go`, `frontend/src/app/pages/workflow-engine/workflow-engine.component.ts` | Removes `successCriteria` from intake, persistence, launch, and UI; treats review-required results as generic uncertainty rather than pausing and refreshing persisted review state. | Reject; loses workflow intent and obscures the operator's approval state. |
| `backend/internal/background/worker.go` | Removes paused-mode deferral for already approved safe work and the emergency-stop processing gate. | Reject; weakens autonomy and emergency-stop policy. |
| `backend/internal/router/routes.go` | Initializes browser verification without the workflow linker and removes the framework preference-history route. | Reject; drops integration and audit history. |
| `frontend/src/app/control-room/module-registry.ts`, `module-registry.spec.ts`, framework registry page/service/tests | Removes the newly registered success-criteria and framework-preference-history sections and their UI/service/tests. | Reject; feature and test coverage regress. |
| `.github/workflows/ci.yml`, backend/IDP Dockerfiles, `README.md` | Downgrades the Go toolchain from 1.27.2 to 1.25.13; removes the cleanup-readiness safety-contract job, secret-scan base-SHA wiring, and other cleanup gates. | Reject; do not reduce the pinned toolchain or hosted safety coverage. |
| `docker-compose.local.yml` | Removes the isolated optional SearXNG profile and changes the state-permission helper from the backend image's `chown` to a PostgreSQL shell command with additional capability. | Reject pending separate isolation and permissions evidence; preserve the verified PR setup. |
| `scripts/run-secret-scan.py` | Removes redacted finding summaries and requires neither a valid base revision nor CI-provided comparison SHA. | Reject; loses actionable safe reporting and weakens scan scope. |
| `frontend/src/app/services/auth/auth.service.ts` | Drops the explicit `Observable<void>` login response contract. | Reject; no corresponding API contract change supports the weaker typing. |

These targeted reviews do not adjudicate the remaining differing paths or the
unmapped local review artifacts. No source, test, configuration, evidence, or
diagnostic file was deleted or copied during this refresh. The PR and its checks
remain governed by the separate exact-head status recorded at the time of each
verification; passing local comparison/contract checks do not substitute for
hosted CI completion.

### Framework preference lock correction (2026-10-11)

The canonical PR source review found a separate PostgreSQL text-parameter defect
in `backend/internal/frameworkregistry/repository.go`: preference updates built
the advisory-lock key as `owner + NUL + frameworkID` and passed it as a text
parameter. PostgreSQL documents that code-zero/NUL cannot be stored in its
character types ([PostgreSQL character types](https://www.postgresql.org/docs/17/datatype-character.html)).
The PR source now uses the existing `encoding/hex` import to hex-encode each
component and join them with a delimiter that cannot occur in hexadecimal
output. `repository_test.go` adds a regression proving the generated key
contains no NUL and distinguishes inputs that collide under NUL-delimited
concatenation.

This is a source fix and authored unit test only. The Go toolchain is not
available in the current local environment, so the focused Go test and a live
PostgreSQL reproduction have not been run here. The exact PR-head CI result must
verify the test before this fix is considered validated; no production or live
database acceptance is claimed.

### Execution authorization and Windows recovery comparison (2026-10-11)

Further exact diffs in the same primary checkout show additional local changes
that must not replace the canonical PR versions:

| Paths / area | Local difference | Disposition |
| --- | --- | --- |
| `backend/internal/operations/execution_claim.go` | Queries `operation_events.payload` rather than the canonical `payload_json`; changes approved/self-held claim eligibility and timestamp updates. | Reject; the query does not match the event schema and the altered claim/update semantics can invalidate approval or audit chronology. |
| `backend/internal/operations/execution_claim_memory.go` | Reorders review-time checks relative to approved-claim receipt handling. | Reject until memory/Postgres eligibility parity is proven. |
| `backend/internal/operations/source_approval.go` | Removes reviewed-owner binding and the evidence digest from approval revision checks, and collapses specific stale-receipt diagnostics. | Reject; approval is no longer bound to the complete source revision and ownership context. |
| `scripts/backup-windows.ps1` | Removes bounded Docker process handling and local-engine result validation, plus detection of uncovered volume mounts and portions of the recovery-asset coverage contract. | Reject; it weakens bounded execution and risks claiming incomplete backups are complete. |
| `scripts/initialize-windows.ps1` | Removes ACL/content post-replacement verification and verified rollback, and stops preserving configured private Docker subnets when regenerating the environment file. | Reject; it weakens recovery/permission integrity and can silently change network configuration. |
| `scripts/test-restore-windows.ps1` | Removes isolated contract-test mode and substitutes direct Docker-dependent execution. | Reject; retain the deterministic isolated recovery contract coverage. |

The schema comparison was checked against `backend/internal/models/operation.go`
and `backend/migrations/pre/0002_baseline.up.sql`; the safety dispositions are
source-derived. No Windows installer/restore run or PostgreSQL integration run
was performed during this comparison. These paths remain part of the unresolved
122-path divergence set; no files were imported or removed.

### Runner dependency and contract fixture comparison (2026-10-11)

The PR's Promptfoo runner package and lockfile pin `promptfoo` 0.124.1, pin the
transitive `get-uri` `basic-ftp` override to 6.2.1, and explicitly install the
Linux libsql optional package. Its Docker build omits other optional packages.
The primary checkout's alternate package downgrades Promptfoo to 0.123.1 and
removes those dependency safeguards; `app.mjs` still reports 0.124.1, making
that alternative internally inconsistent. The package and lockfile variants
were not copied into the PR.

The audit also found the PR's backend Promptfoo HTTP fixtures still advertised
0.123.1 even though the shipped runner reports 0.124.1. Updated
`backend/internal/promptfoo/service_test.go` fixtures to the pinned runner
version so the contract tests model the actual image. Go tests could not be run
locally because the Go toolchain is unavailable; the PR's hosted backend test
job remains the verification gate. This fixture correction does not imply a
live Promptfoo/model-provider execution or safety certification.

The primary checkout's DeepTeam lock still contains a pinned Sentry SDK, while
its alternate input/lock files differ. The runner code currently has no direct
Sentry import, so this audit did not infer whether that dependency is required
transitively or remove it; that dependency difference remains open pending
lock-generation and runtime evidence.

### Superseded 8 MiB transcript disposition (2026-10-12)

This scan was superseded by the 10 MiB rescan below. It recorded 3 uniquely
attributed, hashed candidate transcripts (1,644,177 bytes) and 15 retained
transcripts (20,737,524,945 bytes). Thirteen retained files contained
oversized JSONL records (1,630 skipped records) and were unattributed in that
bounded scan; prior terminal observations did not override that disposition.

The two supplemental roots add 9 retained transcripts (14,013,737,298 bytes);
all nine contain oversized records (783 skipped records), leaving zero
supplemental cleanup candidates. The combined known inventory is 27 files and
34,752,906,420 bytes: 3 candidates and 24 retained. The supplemental summary
and manifest are now part of this ledger, but neither source root is yet wired
into the destructive cleanup command. The 3 main-root files remain candidates
only; no source transcript was deleted. Full semantic reconciliation of
oversized transcript content, external integration, canonical merge, and
post-merge source verification are still outstanding, so cleanup is not ready.

### Latest 10 MiB rescan crosswalk (2026-10-12)

This supersedes the preceding 8 MiB cleanup totals. It also confirms the two
newly attributable completion reports against the current PR tree. Source
presence and hosted CI do not authorize transcript removal.

| Child | Work integrated | Current source evidence |
| --- | --- | --- |
| `019fb20c-2b95-7550-8304-1117de872148` (Copernicus) | Approval-only pursuit tests use an owner role; non-approver checks are preserved. | `backend/internal/pursuit/handler_test.go`; current PR tree contains the path and the exact prior PR head's backend CI passed. |
| `019fb220-c797-77d1-8060-0f220cd733e1` (Aristotle) | Constitution base-version provenance/stale-activation protection, first-owner draft selection, deferred active-Constitution invariant, and deterministic catalog digest are present. | Framework Registry service/repository/types and focused tests; model contract, `0003_framework_registry` up/down migrations, and `docs/framework-registry.md`; all named paths exist in current PR tree. Exact prior PR head CI passed; live PostgreSQL assertions remain environment-dependent. |

Current main-root inventory: 18 files / 20,739,169,122 bytes; 5 hashed
candidates / 3,179,439,595 bytes; 13 retained / 17,559,729,527 bytes; two
records remain oversized. Supplemental inventory: 9 files / 14,013,737,298
bytes; 2 hashed candidates / 3,113,294,358 bytes; 7 retained /
10,900,442,940 bytes; five records remain oversized. Combined: 27 files /
34,752,906,420 bytes; 7 candidates / 6,292,733,953 bytes and 20 retained /
28,460,172,467 bytes. Candidate files remain local. Deletion requires merged
canonical-main evidence, updated cleanup tooling for both supplemental roots,
and passing exact-hash cleanup gates; none of those conditions is asserted by
this scan.

The read-only supplemental archive verifier was rerun on 2026-10-12 at
14:35 UTC. It verified all 9 manifest paths and byte totals, both candidate
SHA-256 values, and 19 tracked report-source links. It reported
`cleanup_authorized=false` and `deletion_performed=false`. This verifies
inventory/hash integrity for the two supplemental roots, not semantic
integration of the oversized transcript contents or eligibility for removal.

The read-only main archive verifier was rerun on 2026-10-12 at 14:37 UTC with
`-RequireSourceArchive`. It verified all 18 manifest paths and 20,739,169,122
bytes, all five candidate hashes (3,179,439,595 bytes), and all five candidate
crosswalk IDs. It reported `source_archive_verified=true`, but
`committed_ledger_verified=false`, `merged_pull_request_verified=false`,
`cleanup_gate_ready=false`, `cleanup_authorized=false`, and
`deletion_performed=false`. The combined main and supplemental inventories
therefore remain preserved: 27 files, 34,752,906,420 bytes, and seven hashed
candidate transcripts totaling 6,292,733,953 bytes; none has been deleted.

### 2026-10-17: main archive source discrepancy

The canonical-main ledger at `80fdeabd16efda6d9328fa0c89a4ab813f087908`
still records 18 main-archive files totaling 20,739,169,122 bytes: five
candidate transcripts totaling 3,179,439,595 bytes and 13 retained
transcripts totaling 17,559,729,527 bytes. A fresh read-only inventory of
`D:\codex-temp\hai-completed-agent-sessions` found only 13 files at the archive
root, totaling 17,559,729,527 bytes. Their sizes match the 13 retained ledger
rows. The five paths marked `candidate_after_ledger_commit` are absent, so
their recorded hashes could not be checked against source files.

The five missing basenames were searched under `D:\codex-temp` and
`C:\Users\NO\.codex`; no copies were found in those searched roots. This was
not a full-device search and does not establish whether the files were moved,
removed, or are available elsewhere. No transcript was deleted during this
audit, and no cause or date for the missing files is known.

The canonical `scripts/test-hai-transcript-cleanup-readiness.ps1` was run from
a clean worktree at current `main` with `-TranscriptRoot` and
`-RequireSourceArchive`. It failed closed on the first missing candidate path,
before reporting source verification or reaching the repository/PR gates. This
is expected behavior and confirms that the archive is not cleanup-ready. Do
not interpret the earlier candidate classification, preserved reports, or
code crosswalk as proof that these missing source transcripts were safely
removed or fully integrated. Preserve the 13 remaining files and reconcile
the missing five from a verified backup or other authoritative source before
any archive cleanup decision.

### Public reconciliation export (2026-10-18)

The historical additions below retain their original audit dates and
bounded scope. They supplement the preceding source-discrepancy warning;
they do not supersede its missing-source finding. Transcript identifiers
and absolute machine paths in this export use aliases. The private source
documents remain retained locally. This publication does not establish
complete reconciliation or authorize any further transcript deletion.

### Current local retained-transcript scan (2026-10-17)

A read-only rerun of `scripts/audit-child-agent-transcripts.ps1` streamed the
three currently inventoried source folders. It read 20 files totaling
28,460,172,467 logical bytes and confirmed the pinned HAI parent session ID
in each file's session metadata. No source file was modified by the audit.

| Source folder | Files | Bytes scanned | Completed / aborted / unattributed | Oversized records skipped | Cleanup candidates |
| --- | ---: | ---: | ---: | ---: | ---: |
| `hai-completed-agent-sessions` | 13 | 17,559,729,527 | 0 / 8 / 5 | 2 | 0 |
| `completed-agent-sessions` | 1 | 1,538,632,047 | 0 / 0 / 1 | 0 | 0 |
| `session-log-relief` | 6 | 9,361,810,893 | 0 / 1 / 5 | 5 | 0 |
| **Total** | **20** | **28,460,172,467** | **0 / 9 / 11** | **7** | **0** |

The parser used its configured 10 Mi-character record bound. Seven oversized
records were skipped from semantic attribution; consequently the two folders
with oversized records report `full_file_scan=false`. The one-file folder had
no oversized or malformed candidate records, but its child turn could not be
uniquely attributed. These results do not establish that all child changes are
integrated, and they do not authorize cleanup. All 20 files remain required
for the unresolved attribution and code crosswalk; no transcript was deleted.


### Stale committed-checkout tree comparison (2026-10-17)

Compared the primary local checkout's committed `HEAD` tree (commit
`e07b9dae`, dated 2026-09-04) with the live `origin/main` ref
(`80fdeabd`, dated 2026-10-17). The local tree has 1,992 paths versus 3,200
on `main`: 1,088 blob-identical paths, 898 same-path but different-blob paths,
6 local-only paths, and 1,214 paths present only on `main`.

The six local-only paths are three local environment files, an environment-
specific `nginx-config/nginx.conf`, and two older operation-index migration
files. The operation-index migration is already present in current `main` as
`backend/migrations/post/0006_operations_owner_workspace_dedupe_active.*`; the
local `0004` files are duplicate filenames/content and are not candidates to
port. Environment files and host-specific Nginx configuration are excluded
from publication. The 292 local-only commit count is not an integration gap by
itself: PR #36 was squash-merged, so its original commit ancestry is not
preserved on `main`.

This comparison does **not** prove the 898 divergent same-path files are
redundant. They remain an explicit source-review gap, and the dirty checkout
also contains 244 modified paths, 448 deleted paths, and 1,581 untracked paths
(1,501 under generated `output/`). No stale branch snapshot or mass deletion
was pushed. Continue reviewing any potentially newer same-path behavior against
current `main` before porting or cleaning it.

### Reconciliation worktree and open-PR audit (2026-10-17)

A read-only inventory found ten Git worktrees registered to this HAI repository.
The primary checkout and all nine secondary worktrees have non-default or
uncommitted state; none was established as a clean full-tree copy of current
`main`. The separate D: reconciliation checkout is staged/modified and has
`HEAD` `3b15fe4a08b10bb2e15afe15640bd6488cebff5d`; it has 458 paths different
from the current `main` tree (400 modified, 57 deleted, 1 added) and no
untracked files. Do not remove or wholesale-merge any of these worktrees.

The primary checkout's current status is 244 modified tracked paths, 448
deleted tracked paths, and 1,581 untracked paths (1,501 under `output/`). In
a bounded untracked source/evidence comparison, 64 files shared a path with
`main` but had different content; 12 evidence/log files were absent from
`main`; no exact-copy candidate was found. The changed tracked-file check
also found no exact current-`main` copy. Generated output and state folders
were excluded from source-port eligibility. No deletion or source-tree write
was performed.

Targeted comparison of the D: worktree against current `main` rejected at
least these changes as regressions:
- `backend/internal/background/worker.go` removes paused-mode and emergency
  processing gates and reports raw internal errors rather than bounded,
  redacted operator messages.
- `backend/internal/operations/execution_claim.go` queries the wrong event
  JSON column (`payload` instead of `payload_json`), removes exact-revision
  approval/review-time safeguards, and changes timestamp-preserving CAS updates.
- `backend/internal/operations/source_approval.go` removes reviewed-owner and
  raw-evidence-digest binding from approval receipts.
- `backend/internal/executionauth/repository_postgres.go` restores a NUL-
  delimited PostgreSQL text lock key and drops newer receipt-reference columns.
- The D: variant deletes the merged PR #153 feed-cursor rollback migration and
  its refusal to drop live cursor/claim state.

These are sampled high-impact findings, not a full review of the 458 paths.
They demonstrate why the local snapshot cannot be ported or discarded as one
unit. The remaining paths still need a current-main semantic comparison and
targeted tests before any code is imported.

Current open PR snapshots checked directly: #160 is open/draft at head
`835a06d` based on current `main` `80fdeab`; #163 (`774ecb1`), #164
(`e288dc7`), and #168 (`b68a037`) are open/draft and were based on older
`main` `1e18f90`. Their remote branches preserve their proposed changes, but
they are not merged-main evidence and do not qualify local copies for removal.
PR #151 remains a draft audit ledger. No local worktree, transcript, source
file, test evidence, or generated artifact was deleted.


## 2026-10-17 local reconciliation follow-up

This update supersedes the earlier statement that no local worktree had been
removed; it describes a later, separately verified cleanup pass.

- PR #153 was already merged to `main` as `ad8a72e`; its cursor migration and
  rollback protections are present in the current `main` ref.
- Two clean duplicate worktrees were removed after confirming their contents
  were represented by remote PRs: the Autoposter checkout exactly matched PR
  #160 head `835a06d`, and the reconciliation checkout was an ancestor of PR
  #151 head `3c93f61`. Their working directories totaled 76,023,663 bytes.
  Shared Git history, remote branches, PRs, and the archived Autoposter
  worktree were retained.
- PR #160 exact-head Actions run `38981873284` completed with 30/30 jobs
  successful, including backend/frontend tests, browser acceptance, real
  PostgreSQL migration integration, two-account isolation, and secret scan.
  This is CI evidence, not live Autoposter account acceptance.
- The primary checkout remains on stale branch `codex/hai-runtime-release`
  at `e07b9da`, while `origin/main` is `80fdeab`. Its current working tree
  has 244 tracked modifications, 448 tracked deletions, 1,581 untracked paths,
  and 34,613 ignored entries. The 1,501 untracked `output/` files total
  126,491,186 bytes.
- No exact-content matches were found between untracked non-output files and
  `origin/main`. Most have a path counterpart on `main` but different
  contents, so they are alternate/stale copies, not safe deletion candidates.
- Focused review found the local background-worker variant removes pause and
  emergency-stop processing gates and secret-redacted bounded error reporting;
  the local operations source-approval implementation is deleted. These
  changes were not published. The local tree is not safe to push wholesale.
- The separate older Autoposter draft contains unsafe defaults and omits
  owner/source bindings that are present in the reviewed PR #160 version. No
  code from that older copy was added to the PR.
- A separate stale checkout contains about 299 MB of ignored frontend and
  Python build/dependency caches. Recursive cache removal was rejected by the
  local tool policy; a follow-up inspection confirmed the directories remain.
  No source, environment files, test evidence, or cache directories were
  removed in that attempt.

The remaining primary and D: snapshots have uncommitted or divergent content.
They require per-file comparison and targeted validation before any source
port or local deletion. This entry is an inventory checkpoint, not a claim that
the full HAI local-device audit is complete.


## 2026-10-17 execution authorization diff check

A focused comparison of the primary local checkout with current `main` found
that its modified `backend/internal/executionauth/service.go` calls the life-
graph projector from `Get` and `List`, while current `main` has an explicit
`TestAuthorizationReadsDoNotRunLifeGraphProjection` asserting reads do not
trigger graph writes. The same local diff removes
`CloneWithEmergencyStopEvaluator`, the isolated evaluator composition used
to keep emergency-stop recovery from mutating the shared authorizer. Neither
change is a safe port as-is. A focused Go test run could not start because the
Go executable is unavailable in this session; no local test result is claimed.



## 2026-10-17 PR #153 merge and local cursor-copy audit

GitHub reports PR #153 ("feat(accountfeed): persist HTTP feed pagination cursors") merged on 2026-10-16 as merge commit `ad8a72ea4c744a3bd8adf83907be87d92ae7351a`. Live `main` is `80fdeabd16efda6d9328fa0c89a4ab813f087908`, and the merge commit is an ancestor of that ref.

Compared the PR's 17 changed paths against current `main` using Git blob hashes in every one of the 11 registered local HAI worktrees. No existing local file examined was an exact current-`main` blob match. The copies found are older or otherwise divergent; the primary checkout lacks several of the PR paths and has two dirty migration-test paths. No cursor-related local files were deleted.

The primary checkout is at `e07b9daeb3ba2630ecbe12a64d948f791203c241`, with 292 commits not reachable from current `main` and 84 commits of current `main` not reachable from that local HEAD. Its uncommitted tracked delta is 690 files (72,730 insertions, 108,946 deletions); the separate status inventory reports 244 modified tracked paths, 448 deleted tracked paths, 1,581 untracked paths, and 34,613 ignored entries. This is not a safe wholesale PR or deletion candidate. All 11 registered worktrees have tracked changes, untracked files, ignored entries, or a combination; none qualifies as a clean disposable copy based on the current inventory.

This is a bounded comparison of the registered Git worktrees and the PR's changed paths, not a full-device inventory. Local files outside those worktrees and imported archives remain to be reviewed. No source or worktree data was removed.


## 2026-10-17 broader snapshot and legacy-worktree audit

Three read-only audits examined the primary checkout, `<local-path-001>`, and the six registered worktrees under `<local-path-002>`. No source, index, PR, or worktree state was changed by those audits.

The primary checkout remains unsafe to publish wholesale. It contains 244 modified tracked paths, 448 tracked deletions, and 1,581 untracked paths; local `worker.go` removes pause/autonomy processing gates and bounded secret-redacted error handling; `executionauth/service.go` projects life-graph records during reads; and `opscontrol/service.go` removes control-review wiring. A plausible candidate set (isolated acceptance harness and guide) exists locally and differs from `main`, but requires a focused diff review and non-destructive contract tests before any port. Its diagnostic logs, `output/`, and agent-state files remain excluded.

The D: snapshot is on `codex/hai-local-source-reconciliation`, 75 commits behind `main`, with 310 staged paths (309 modified, one added), no reported untracked non-ignored files, and 34 unstaged `output/` deletions. Those 34 report contents are already present in `main`; the files are already absent from that working tree. Only one staged file examined, `backend/internal/runtimelab/owner_gate_handler_test.go`, exactly matches current `main`. The broad staged patch is not safe to push: it removes pause checks, redacted error bounds, source-evidence-hash binding in approval revisions, and OpsControl approval routing; it adds a hard-coded placeholder backend key to `nginx-config/nginx.conf`. Potentially useful verification changes are mixed into this patch and require isolated review and tests.

The six legacy 2026-10-11 worktrees contain 2,104 tracked status entries, two untracked files, and 46,157 ignored files totaling 482,267,937 bytes. Their associated PRs #34, #35, and #36 are merged, but every worktree has local tracked changes; one also has untracked files and two contain ignored runtime/build data. Therefore the PR history does not preserve those worktree deltas, and no whole worktree is currently a safe deletion target.

Do not push the primary or D: snapshot wholesale. The safe next step is to select a narrow test/acceptance improvement, compare it to current `main`, run its focused checks in an isolated clean-main worktree, and publish only after review. Local material remains preserved pending that work; the audit scope still excludes unregistered device locations and imported archives.


## Oversized compaction-row follow-up (2026-10-18)

A read-only, fixed-buffer scan examined only the two JSONL rows previously skipped for exceeding the 16 MiB parser limit. The rows are 1,016,062,444 bytes and 904,740,623 bytes; both identify their top-level event type as `compacted`. A memory-mapped marker scan found 2,783 and 2,857 literal `patch_apply_end` occurrences, respectively, matching the patch-event counts already indexed from parseable records for those same transcripts.

This strongly suggests the oversized rows carry compacted copies of the already-indexed conversation history rather than additional independent patch events. The marker counts are not a one-to-one comparison of embedded event payloads, so they do not prove semantic integration of every patch. The 41 previously failed patch results, five unattributed transcript entries, duplicate-named archive pairs, and remaining worktree differences are still unresolved. No source code was imported, and no archive or local checkout was deleted. These two files remain ineligible for cleanup until the full content-level reconciliation is complete.


## Historical embedded-runtime review reconciliation (2026-10-18)

The local report `output/approved-embedded-runtime-review-20261001.md` raised a P1 concern that an embedded-runtime exemption could skip required independent specialists. A source check against current `main` at `d85cb1a657763027b21b0e59dc41aff8864b260e` found no `usesAdmittedEmbeddedRuntime` or embedded-coverage exemption. In `backend/internal/task/service.go`, `applyFrameworkRisk` checks each selected `RequiredAgents` entry and every unready required delegation before allowing execution. `backend/internal/task/required_participant_admission_test.go` contains `TestFrameworkRequiredParticipantCannotBeBypassedByAutomaticOrApprovedRuntime` for automatic Level 8 and approved Level 6, and `TestApprovedReadinessProbeDoesNotFabricateSpecialistEvidence` verifies a real task Run blocks dispatch without required specialist evidence.

This establishes that the report's proposed whole-loop bypass is not present in current `main`; no code from that proposal needs to be ported. The source and focused regressions were inspected but not run in this audit. This does not resolve unrelated archive records, and it does not authorize local cleanup.


## Historical automation approval snapshot reconciliation (2026-10-18)

The local report `output/automation-launch-binding-20261001.md` raised a concern that approval review did not bind the reviewed automation configuration. A source check against current `main` at `d85cb1a657763027b21b0e59dc41aff8864b260e` found this behavior present and covered: `ReviewConfigurationSnapshot` is captured through `InspectReviewConfiguration`, kept as internal review evidence, included in the durable review-request representation, and validated against the current automation scope/configuration digest before an approval decision is recorded. The validation rejects configuration or policy changes since review.

Focused regression coverage is present in `backend/internal/task/historical_automation_review_test.go`, `backend/internal/task/review_configuration_context_test.go`, `backend/internal/task/state_repository_postgres_test.go`, and `backend/internal/automation/historical_review_binding_test.go`; the tests cover snapshot persistence/round-trip, rejection of client-supplied or unavailable snapshots, legacy approvals not acquiring a later snapshot, and changed review targets/configuration. The report's snapshot-binding gap is therefore resolved in current `main`; no duplicate implementation should be ported from a local snapshot. Source and test names were inspected from GitHub, but these tests were not run in this audit. This finding does not close the broader transcript/source reconciliation or authorize local deletion.


## Historical atomic mutation review reconciliation (2026-10-18)

The local report `output/atomic-mutation-source-review-20261001.md` describes pre-fix concerns about forged `BeforeStatus` and mutable `CreatedAt`. Current `main` at `d85cb1a657763027b21b0e59dc41aff8864b260e` now checks a status-change event's `BeforeStatus` against the locked current row and applies the transition from that authoritative status in `backend/internal/operations/repository.go`. The memory repository also preserves `CreatedAt` from the stored row; the SQL path does the same. The exact regression `TestAtomicMutationTransitionRejectsForgedBeforeStatus` is present in `backend/internal/operations/mutation_atomicity_regression_test.go`.

The report itself records the parent fix and a parent-run native suite result of 26 top-level / 65 subtests, while explicitly noting that this was not real PostgreSQL acceptance. This audit independently confirmed the current source and regression-test presence through GitHub, but did not rerun the suite. These two reported defects are resolved in current `main`; no duplicate patch is needed. This does not establish full transcript attribution or authorize local deletion.


## Historical executor-outcome and redaction compatibility reconciliation (2026-10-18)

Two more local reports were checked against current `main` at `d85cb1a657763027b21b0e59dc41aff8864b260e`:

- `output/automation-executor-outcome-20261001.md`: the executor maps non-nil launch receipts even when an error is returned, preserves pre-dispatch versus uncertain-after-dispatch distinctions, and treats missing/malformed receipts as uncertain. The dedicated `backend/internal/task/automation_executor_outcome_test.go` is present with regression cases for result-plus-error, missing result, uncertain status, malformed completion, pre-dispatch failures, sanitized/bounded metadata, receipt mismatch, and unavailable executor. This report's proposed implementation is present in current `main`; no separate source patch is indicated by this comparison.
- `output/structured-redaction-compatibility-20261001.md`: current `main` retains the shared conservative credential redactor, while fixing the reported metadata collision at typed validation boundaries. Agent-team contracts are scanned through `containsAgentTeamSecret` so schema labels such as `key` and authority booleans do not cause false credential findings, while their values are still inspected. Portfolio allocation validation creates a typed scan projection that renames only numeric token-budget fields to inbound/outbound unit labels; its digest remains bound to the original encoded request. Regression sources exist in `backend/internal/frameworkregistry/agent_team_service_test.go` and `backend/internal/pursuit/portfolio_secret_metadata_test.go`. The broad redaction rule was not weakened.

These are source and test-presence findings. A GitHub Actions backend test job on a descendant PR head passed, but this audit did not rerun these suites or independently establish production acceptance. The old report's failing local test snapshot is historical and should not be presented as the current result. No additional source change is proposed from these two reports; transcript attribution and local cleanup remain open.


## Historical task/workflow uncertainty and fallback review reconciliation (2026-10-18)

Current `main` at `d85cb1a657763027b21b0e59dc41aff8864b260e` was checked against four local reports: `output/task-ui-outcome-uncertainty-20261001.md`, `output/workflow-outcome-recovery-20261001.md`, `output/workflow-preflight-independent-review-20261001.md`, and `output/workflow-preflight-followup-review-20261001.md`.

The backend path now maps task execution uncertainty into `workflow.TaskRunResult.ExecutionOutcomeUncertain`; workflow uncertainty persistence and review gating are present in `backend/internal/workflow/service.go`, and runner coverage is present in `backend/internal/workflowtask/tool_outcome_uncertainty_test.go`. The former fallback findings have follow-up implementation/tests in current `main`: `backend/internal/task/fallback_review_evidence_test.go` covers retaining a prior completed receipt after reservation denial, requiring review without automatic retry for an empty fallback model, and letting fallback settlement blockers override verified output. `backend/internal/workflowtask/approval_preflight_test.go` includes `TestRunnerPreservesNamedMixedReadinessPrerequisites`; current resource/domain admission checks remain separate from approval-only classification. The reports' proposed workflow uncertainty propagation and bounded preflight fixes therefore have corresponding current-main source and regression coverage. The frontend's Basic uncertainty warning/disabled retry behavior and persisted-history protections are present in `frontend/src/app/pages/task-blueprint/task-blueprint.accessibility.spec.ts`; the owned browser-matrix handoff explicitly says its full-scope persisted-history scenario has not been executed.

This is source and test-presence reconciliation, not execution or end-user acceptance. The historical worker reports did not run all relevant tests; this audit did not rerun backend/frontend tests or the named browser scenario. Existing workflow review remains approval-gated; no retry, participant, model, source, or authority requirement is waived. No code import is indicated by these reports. Their findings are not a substitute for unresolved transcript attribution, live integration acceptance, or the cleanup gates.


## Historical intent, worker partial-outcome, and final-consumer reconciliation (2026-10-18)

Current `main` at `d85cb1a657763027b21b0e59dc41aff8864b260e` was checked against `output/safe-intent-source-review-20261001.md`, `output/local-safe-worker-partial-outcome-20261001.md`, `output/runtime-outcome-independent-review-20261001.md`, and `output/runtime-outcome-final-consumer-review-20261001.md`.

- The claimed-intent cancellation edge regression is present as `TestSafeExecutionCancellationAfterIntentCommitRetainsReplayBarrier` in `backend/internal/background/execute_intent_test.go`. It protects the in-memory cancellation/replay contract; it does not establish PostgreSQL crash durability or process-restart acceptance.
- Safe-worker stage progress is present in `backend/internal/executionbroker/local_safe_worker.go`. The background outcome classifier consumes contradictory/partial progress as uncertainty; `backend/internal/background/execute_outcome_test.go` includes consumed/unknown authorization, effect-started and partial-progress-versus-success contradictions. This addresses the historical concern that a bare authorization error could be misclassified as a safe pre-effect refusal. It does not prove cross-process filesystem identity recovery or live operator reconciliation.
- The runtime outcome consumer report says its three final P2s were source-resolved: uncertain task outcomes remain review-required in the life-ontology projection; assistant public review/summary/next-action text uses the shared uncertainty helper; and missing prior-plan evidence maps to reconciliation-required before approval mutation. This is consistent with the current consumer code represented in the report and the shared `task.ExecutionOutcomeUncertain` contract. It is a source reconciliation, not independent runtime verification.

The historic worker reports did not execute all proposed regressions, and this audit did not rerun them. The HAI cleanup verifier remains read-only; no source artifact, report, worktree, transcript, or cache was removed. These findings do not close real PostgreSQL, process-restart, filesystem crash, or complete archive-attribution gates.
