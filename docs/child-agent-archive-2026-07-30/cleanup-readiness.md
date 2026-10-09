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

**Transcript cleanup gate is not yet satisfied.** The source changes,
crosswalk, reports, and manifest are committed and the remote branch head is
confirmed at `f788d48`. The read-only verifier below confirms that all eight
candidate source files match their manifest hashes. The PR is still open and
its CI run has failures and pending jobs; these do not satisfy the cleanup gate.
Do not remove any candidate transcript until those gates are satisfied. The
manifest/hash is a candidate allowlist only, not a deletion command.

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

The ledger-only check passed with 18 manifest rows, 8 candidates, 10 retained
transcripts, and 8 matching crosswalk rows. The source check also passed against
`D:\codex-temp\hai-completed-agent-sessions`: all eight candidate files
(7,939,888,699 bytes total) matched their recorded size and SHA-256. This
verifies candidate bytes against the ledger; it does not close the separate
PR/CI and repository-history gates or authorize deletion.

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
