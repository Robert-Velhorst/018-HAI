# HAI local cleanup readiness: 2026-07-30 child-session archive

This is a dated inventory and preservation ledger for the July 30 HAI
completed-agent-session archive. It is separate from the August
cohort documented in `docs/child-agent-integration-ledger.md`. Hashing and
ledgering do not themselves authorize or perform deletion.

## Transcript inventory

The audited source contained 18 JSONL files occupying 20,739,169,122 bytes
(about 19.315 GiB). They represent 16 unique child IDs because two IDs each
have duplicate transcript files. The audit preserved all nine completed final
messages, including the completed transcript whose ID is duplicated.

| Disposition | Files | Bytes | Meaning |
| --- | ---: | ---: | --- |
| Hashed cleanup candidates | 8 | 7,939,888,699 | Completed, unique-ID transcripts; exact SHA-256 is in the manifest. Candidate only, not deleted. |
| Retain | 10 | 12,799,280,423 | Aborted/nonterminal transcripts and every transcript for duplicated child IDs. |

The generated manifest records per-file size, status, disposition, final-message
hash, and (for each candidate) full transcript SHA-256. The audit reported no
source length or last-write-time change during candidate hashing. The preserved
completed reports are in `child-agent-final-reports.md`; machine-readable counts
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

### Completed-report integration crosswalk (2026-10-10)

This maps all eight manifest rows marked `candidate_after_ledger_commit` to the
preserved report and current repository evidence. It records source integration,
not permission to remove the source transcript. The two duplicate-ID transcripts
are outside this candidate set and remain retained as shown in the manifest.

| Child | Report kind | Integrated finding / disposition | Current repository evidence |
| --- | --- | --- | --- |
| `019fb1d1-53a5-78a0-9387-b23f05eb6aef` | Partial IDP report | Auth configuration, token/session checks, logout/OAuth failure handling, DB error distinction, Kafka nil handling, and non-root runtime are present; the report explicitly said latest edits still needed validation. | `idp/internal/app/config/`, `idp/internal/authentication/`, `idp/Dockerfile`; IDP package tests remain a separate gate. |
| `019fb1d3-1ebf-7b63-897a-a4fa0fbf1f10` | Advisory | Approval binding, criterion-specific evidence, workflow success-criteria propagation, task recovery, Constitution history, registry rendering, and preference audit findings were cross-checked; missing paths have since been implemented. | `backend/internal/automation/approval_proof.go`; `backend/internal/task/validation.go`, `review_reconciliation.go`; `backend/internal/workflow/service.go`; `backend/internal/workflowtask/runner.go`; `backend/internal/frameworkregistry/`; Framework Registry and Workflow Engine pages. |
| `019fb20c-2b95-7550-8304-1117de872148` | Advisory | Exact historical workflow snapshot could not be restored; no incorrect replacement was made. Its report is preserved, and current workflow code is verified independently rather than represented as that snapshot. | `backend/internal/workflow/` and its tests; see the preserved report in `child-agent-final-reports.md`. |
| `019fb220-c797-77d1-8060-0f220cd733e1` | Implementation | Constitution base-version provenance, stale-draft protections, invariant enforcement, and catalog contract checks are represented in current registry implementation and tests. | `backend/internal/frameworkregistry/constitution_rules_test.go`, `repository.go`, `catalog_test.go`; registry migrations. |
| `019fb221-54e7-7921-8c65-8a74da2e6d93` | Implementation | The Framework Registry template and accessible automation-form button styling are present; latest UI integration adds preference audit details. | `frontend/src/app/pages/framework-registry/framework-registry.component.html`; `frontend/src/app/pages/home/modals/automations-form/automations-form.component.scss`; registry component specs. |
| `019fb287-b2e4-7251-8f1b-bc8fa9febc07` | Advisory | Action-bound approval, value/negation-aware criterion evidence, approved-review reconciliation, and workflow-owned success criteria are wired and covered by focused tests. | `backend/internal/task/validation_test.go`, `review_reconciliation_test.go`; `backend/internal/workflow/service_test.go`; `backend/internal/workflowtask/runner_test.go`; workflow intake spec. |
| `019fb287-d1ed-75f0-99a8-8cd09c1ef9f2` | Advisory | Owner-scoped Constitution history and append-only framework-preference history are exposed; preference events are digest-checked, and frontend contracts preserve `baseVersion`. | `backend/internal/frameworkregistry/history_test.go`, `handler.go`, `repository.go`; routes `/frameworks/:id/preference-history` and `/constitution/history`; frontend registry model, service, component, and specs. |
| `019fb287-f3be-7860-ba1f-e6b05088db73` | Advisory | Selection-history fetch failures render as unavailable/stale rather than empty history; workflow framework inspector includes conflicts, context requirements, and learning plan. | `frontend/src/app/pages/framework-registry/framework-registry.component.html`; `frontend/src/app/models/workflow.model.interface.ts`; Workflow Engine template and spec. |

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
| Remaining six retained rows | A full streaming pass over these files reviewed user-message events and all shell-call records. No patch-tool calls were present. User/task context and command history include: (1) Meitner and Volta nonterminal histories repeat the HAI OSS-agent/RAGFlow work; (2) Carson contains isolated Go workflow-test performance diagnostics; (3) Parfit contains generated dependency-tree compression and attempted cache quarantine; (4) Hume contains storage/cache inventory and copy attempts; (5) Arendt contains a read-only disk-exhaustion integrity audit. | OSS/RAGFlow behavior is already represented in current source and documentation; task/workflow/governance changes are covered above. Performance and integrity work is diagnostic, not a product patch. The full shell-call scan found 6 mutation-like cache/temp commands in Parfit and 4 in Hume; recorded outcomes include successful moves, failed moves, timeouts, and an interrupted copy. These concern workstation state, not HAI product files. Do not replay or clean those paths from transcript evidence. |

This is a source-presence and CI cross-check, not proof that each historical
patch was applied byte-for-byte or that the product is deployed. The full
transcripts remain the authoritative record for unresolved/partial work. The
eight candidate hashes are verified; the ten retained transcripts are not
eligible for deletion under the current ledger policy. The six retained files
were streamed for user-message events and shell-call records, with mutation-like
calls matched to their recorded outputs where available. Their full
assistant/tool-output histories were not semantically reviewed line by line;
the six nonterminal/aborted records with no patch calls are preserved for that
reason. No claim of complete transcript integration is made while that
limitation holds.

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

The ledger-only check passed with 18 manifest rows, 8 candidates, 10 retained
transcripts, and 8 matching candidate crosswalk rows. The source check also
passed against `D:\codex-temp\hai-completed-agent-sessions`: all 18 files
(20,739,169,122 logical bytes) were present and the eight candidate files
(7,939,888,699 bytes total) matched their recorded size and SHA-256. This
verifies the archive inventory and candidate bytes against the ledger; it does
not establish that every event in the six retained histories has been
integrated, close the separate PR/CI and repository-history gates, or authorize
deletion.

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
