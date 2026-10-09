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
- The eight unique completed transcripts are candidates for archive cleanup
  only after this ledger and its report/manifest are committed and independently
  verified in the intended repository history. Preserve all ten retained files.
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
- Seven HAI volumes are present. Current container references include
  `018-hai-phase2-control-state` from the created backend and failed permission
  helper, `018-hai-postgres-automation-data` from the exited automation
  Postgres container, and `018-hai-postgres-idp-data` from the exited IDP
  Postgres container. The Kafka, Ollama, Redis, and Redpanda volumes have no
  container references in the current `docker ps -a` inventory. None of these
  facts establishes that the underlying data is disposable:
  `018-hai-postgres-automation-data` (858.4 MB),
  `018-hai-redpanda-data` (251.7 MB),
  `018-hai-kafka-kraft-data` (53.5 MB),
  `018-hai-postgres-idp-data` (64.69 MB),
  `018-hai-ollama-local-data` (397.8 MB),
  `018-hai-redis-data` (908 B), and
  `018-hai-phase2-control-state` (164 B). The current backup contract covers
  only the two Postgres volumes and safety-control volume. Redpanda, Kafka,
  Ollama, and Redis remain outside verified export/restore coverage, so the
  backup script correctly refuses to certify a complete backup. Zero attached
  containers is not proof that volume data is disposable. Keep all seven.
- Six HAI-tagged images are present (`018-hai-backend:latest`,
  `018-hai-backend-migrate:latest`, `018-hai-idp:latest`,
  `018-hai-frontend:latest`, `018-hai-backend:local`, and
  `018-hai-nginxconfigmanager:latest`). The backend-migrate, IDP, frontend,
  and backend-local images are referenced by created/exited HAI containers;
  backend-latest and nginxconfigmanager currently have no container references.
  `018-hai-backend:local` is required by the Windows backup and restore
  tooling. Keep all six while the PR is unresolved and local rebuild/startup
  acceptance remains incomplete.
- PR #36 is still open at head `1758e3c18cae06b7846167f2ff94756f6cb96c73`;
  it is not merged. Its associated CI run is terminal, not active. The run has
  failures in the repository secret scan, Windows installer/signing guards,
  two-account isolation, Promptfoo safety image, browser acceptance, native
  Windows runtime regressions, backend build/tests, authenticated control-plane
  smoke, and migration integration. Passing component checks do not cancel
  those failures. Keep the worktree and its untracked CI logs/download, scanner,
  evidence fixtures, and local patch as review evidence; do not treat them as
  cleanup targets while the PR remains open and unresolved.
- The Go toolchain ZIP occupied 67,590,465 bytes in the prior snapshot; the
  installed toolchain can serve other repositories. Do not remove the shared
  toolchain as HAI-only data.
- No HAI containers are running; the `018-hai` Compose project currently has
  created and exited containers, including the migration and permission-helper
  failures. This review did not change container or volume state. Joyce,
  ShareT, and LARO services remain running and were not modified.

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
