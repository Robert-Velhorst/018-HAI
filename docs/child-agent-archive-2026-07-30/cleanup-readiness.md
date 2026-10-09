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
  rollback refuses to discard non-empty criteria. These changes remain
  uncommitted. `go test ./...` passed in the backend. Frontend Angular checks
  could not run because this worktree has no complete `node_modules` tree
  (required packages including TypeScript and Angular compiler are absent).
  The GitHub connector required reauthentication, so current PR state could
  not be verified. This is local implementation evidence, not release or live
  database acceptance.
- Completed-report cross-check: exact action-bound approval proof, value and
  negation-aware criterion evidence matching, approved-review reconciliation,
  Constitution history with `baseVersion`, selection-history error states,
  and framework selection detail fields are present in current source with
  focused tests or route/template coverage. The previously blank registry
  template was replaced by the implemented inspector/recommendation views.
  These are repository checks, not production acceptance. Preference audit
  history and explicit workflow success criteria are the newly integrated
  items described above and remain uncommitted; their frontend behavior is not
  yet compiled or browser-tested in this worktree.

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

**Transcript cleanup gate is not yet satisfied.** These integration changes are
uncommitted and current PR/CI state could not be refreshed. Do not remove any of
the eight candidate transcripts until the source changes, crosswalk, reports,
and manifest are committed in the intended HAI repository, the commit is
independently confirmed, and backend, IDP, frontend, and relevant deployment
checks are recorded against that exact commit. The manifest/hash is a candidate
allowlist only, not a deletion command.

- The eight unique completed transcripts are candidates for archive cleanup
  only after the integration changes, ledger, and report/manifest are committed
  and independently verified in the intended repository history. Preserve all
  ten retained files.
  Do not delete duplicate-ID files, aborted work, or nonterminal work on the
  basis of file size or an empty final message.

## Other local HAI data: refreshed snapshot (2026-10-10)

This is a point-in-time inventory; re-inventory before any future cleanup.
No local data was removed during this review. The attempt to remove clearly
synthetic temporary fixtures was blocked by the execution platform; that
restriction was not bypassed.

- Six `%TEMP%` folders matching the generated
  `hai-acceptance-<32 hex>` pattern occupy 392,892 bytes at this snapshot.
  Four (351,030 bytes total) contain exactly `manifest.json`, `compose.json`,
  and `synthetic.env`. Their manifest owner matches the folder suffix, the
  project matches the generated owner prefix, and the email is the test-only
  `e2e-owner@example.test`; no containers, networks, or volumes with their
  project prefix were found. These are strongly identified generated
  acceptance fixtures, but remain present because deletion was blocked.
  Two other folders contain only `synthetic.env` (20,931 bytes each) and have
  no manifest or Compose file. Their provenance is unresolved; retain them.
  The previous marker/seven-fixture description did not match the current
  filesystem and is superseded by this inventory.
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
  decision.
- Six HAI-tagged images are present (`018-hai-backend:latest`,
  `018-hai-backend-migrate:latest`, `018-hai-idp:latest`,
  `018-hai-frontend:latest`, `018-hai-backend:local`, and
  `018-hai-nginxconfigmanager:latest`). The backend-migrate, IDP, frontend,
  and backend-local images are referenced by created/exited HAI containers;
  backend-latest and nginxconfigmanager currently have no container references.
  `018-hai-backend:local` is required by the Windows backup and restore
  tooling. Keep all six while the PR is unresolved and local rebuild/startup
  acceptance remains incomplete.
- The previous audit snapshot recorded PR #36 at head
  `1758e3c18cae06b7846167f2ff94756f6cb96c73` and its CI run as terminal. This
  session's local branch HEAD is `fd6286f7f04385ad7857bebbc846ca0eb35b52da`,
  with the integration changes above still uncommitted. The GitHub connection
  required reauthentication, so current PR status and checks were not
  independently refreshed. The previous run had failures in the repository
  secret scan, Windows installer/signing guards,
  two-account isolation, Promptfoo safety image, browser acceptance, native
  Windows runtime regressions, backend build/tests, authenticated control-plane
  smoke, and migration integration. Passing component checks do not cancel
  those failures. Keep the worktree and its untracked CI logs/download, scanner,
  evidence fixtures, and local patch as review evidence until the current PR
  state and acceptance evidence are refreshed.
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

1. Resolve the open PR checks and land the recovery-contract changes and this
   ledger through the normal review path; confirm the candidate transcript
   hashes in the merged tree before considering any archive candidate.
2. Implement and rehearse export/restore for every remaining HAI persistent
   volume. Until then the backup script must continue refusing to call the
   installation fully backed up when an unsupported HAI volume exists.
3. For the four complete acceptance fixtures, use only a platform-authorized
   deletion path after preserving any required evidence. Do not infer ownership
   of the two incomplete env-only folders; retain them until their provenance
   is established. Recheck project resources immediately before any cleanup.
4. Delete nothing if the platform blocks deletion. Do not bypass that control
   with another shell, runtime, or API.
