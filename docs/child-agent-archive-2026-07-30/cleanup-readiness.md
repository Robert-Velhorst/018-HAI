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

## Other local HAI data: last recorded inventory

The following is a dated snapshot from the cleanup audit, not a live inventory
for future runs:

- Six synthetic acceptance fixture files occupied 393,104 bytes in `%TEMP%`.
  Their content was marked synthetic; retain until their exact ownership and
  the cleanup mechanism are independently checked.
- `hai-go-toolchain-check-20261009-1.zip` occupied 67,590,465 bytes in `%TEMP%`.
  The Go toolchain itself is installed and may serve other repositories; do not
  remove it as HAI-only data.
- The active PR worktree, CI logs/download, security scanner binary/archive,
  evidence fixtures, and local patch were intentionally left untouched. Some
  are shared/review evidence rather than disposable HAI runtime data.
- Docker volumes reported at that time included
  `018-hai-postgres-automation-data` (858.4 MB),
  `018-hai-redpanda-data` (251.7 MB),
  `018-hai-kafka-kraft-data` (53.5 MB),
  `018-hai-postgres-idp-data` (64.69 MB),
  `018-hai-ollama-local-data` (397.8 MB),
  `018-hai-redis-data` (908 B), and
  `018-hai-phase2-control-state` (164 B). Current backup coverage handles the
  two database volumes and safety-control volume; the other named volumes are
  not yet covered by a verified export/restore contract. Keep them intact.
- No claim is made here that HAI containers, images, volumes, or temporary
  artifacts have since been removed. Re-inventory immediately before any
  cleanup, and preserve Joyce, ShareT, and LARO data and running services.

## Safe next steps

1. Merge/land the recovery-contract changes and this ledger through the normal
   review path; confirm the candidate transcript hashes in the merged tree.
2. Implement and rehearse export/restore for every remaining HAI persistent
   volume. Until then the backup script must continue refusing to call the
   installation fully backed up when an unsupported HAI volume exists.
3. Obtain a fresh local inventory and separately confirm ownership of each
   synthetic temp item, stopped/unused image, or other proposed cleanup target.
4. Delete nothing if the platform blocks deletion. Do not bypass that control
   with another shell, runtime, or API.
