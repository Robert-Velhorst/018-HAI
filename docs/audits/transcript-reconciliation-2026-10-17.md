# HAI Transcript Reconciliation Checkpoint (2026-10-17)

## Scope and limits

This checkpoint records a targeted review of retained child-agent transcripts under `D:\\codex-temp\\hai-completed-agent-sessions`. It is not a complete archive review. The local audit manifest reports 13 transcript files, 17,559,729,527 logical bytes, 11 unique child IDs, 2 duplicate child IDs, 5 unattributed transcript entries, and 2 oversized JSONL records skipped by its parser. It records `full_file_scan: false`, zero cleanup candidates, and no deletion. Those transcripts remain preserved.

## Work reconciled to main

Two small aborted transcripts were inspected for patch calls:

- Bohr, child ID `019fb296-75ae-7943-913c-32d41f5de2d6`, SHA-256 `a7dac5f8cb5f784b0614a5252864a8ea932bb25503a333dbd772b50a46d2647e` — one patch to `backend/internal/task/validation.go` and `backend/internal/task/validation_test.go`, covering contradictory values, negation polarity, and generic provenance matching. The child transcript ended before its own focused tests ran.
- Darwin, child ID `019fb296-9d1a-7301-901c-c35d99ccf3da`, SHA-256 `f8eaceb3d453530295489f73e9a04484012e2ef5fc1cdb8fdbb174e9eddf687f` — one patch across eight `backend/internal/frameworkregistry/` and router files for bounded Constitution version history. The child transcript ended before its own focused tests ran.

The current `main` at `e768e7fb75eb8167fc6b6bcc5f43fbb38f7757b6` contains the corresponding validation functions/tests and Framework Registry history implementation/tests. The changes therefore do not need to be re-submitted as source-code changes. This is source-presence evidence; it does not claim those child runs passed.

At commit `a97ff6e1930c16d9b04fc2f4bb12e4c470a3b2e6`, GitHub reports 9 of 9 checks completed successfully, including backend, frontend, IDP, PostgreSQL migration, Windows smoke, and two-account isolation. Current `main` descends from that commit. This later CI evidence is not a substitute for the missing child-run test results.

## Duplicate-ID files retained

Two archive pairs have names indicating duplicate copies. Full-file SHA-256 checks show that neither pair is byte-identical. They may contain distinct or divergent history and must not be treated as removable duplicate files:

| Child ID | File | Bytes | SHA-256 |
|---|---|---:|---|
| `019fb20b-b4b1-7250-9d7c-9d1e591ab852` | `rollout-2026-07-30T10-02-19-019fb20b-b4b1-7250-9d7c-9d1e591ab852.jsonl` | 1,603,851,727 | `84e07b24a3acaed044a1f52db04de1cbc69c635f4d111d6f86fbb243cdcbc89c` |
| same | `rollout-2026-07-30T10-02-19-019fb20b-b4b1-7250-9d7c-9d1e591ab852.duplicate-5d868d4ecc874ff3a34f508517ace927.jsonl` | 1,603,851,727 | `a7b0bb14fbffa7571f436b023ed5316c9ab07fed6142874eb1fc6e4d53228ce9` |
| `019fb277-0ddf-7500-9088-52cb4655be1a` | `rollout-2026-07-30T11-59-34-019fb277-0ddf-7500-9088-52cb4655be1a.jsonl` | 1,603,399,243 | `85da9f27d10a60112b327f9c4e478ecdf78dcd6db5ac8a0b8c96e24415bc3a65` |
| same | `rollout-2026-07-30T11-59-34-019fb277-0ddf-7500-9088-52cb4655be1a.duplicate-5c6ea92b1f1c491ea0af46a1937e6b08.jsonl` | 1,603,399,243 | `961fb8ff7619f95ef70a7b32c1737605fc61d14d0b3ffe79f5e410a5b6e94500` |

## Disposition

- No transcript or archive file was deleted.
- The two small code changes are already present in `main`; their source can be removed from a local child checkout only after the exact local copy and its backups are individually matched.
- The two duplicate-ID pairs, all oversized/unattributed records, and the remaining archive stay retained until their distinct content and relevant work are reconciled.
- This checkpoint is a partial reconciliation record, not authorization or evidence for deleting the 17.5 GB archive.


## Bounded patch-path index (2026-10-17)

All 13 archived JSONL files were streamed without modification and compared to the local origin/main tree at e768e7fb75eb8167fc6b6bcc5f43fbb38f7757b6 (3,200 tracked paths). The parser considered patch_apply_end events, capped individual records at 16 MiB, and recorded path presence only. It saw 37,824 such events, 41 unsuccessful results, and skipped two oversized records. The two small Bohr/Darwin transcripts each exposed one patch event in a different record shape; their source changes are assessed separately above.

| Transcript identifier | Patch events | Unsuccessful | Oversized records | Distinct paths in parsed changes |
|---|---:|---:|---:|---:|
| 019fb1d1-53a5-78a0-9387-b23f05eb6aef | 3,577 | 3 | 0 | 674 |
| 019fb1d3-1ebf-7b63-897a-a4fa0fbf1f10 | 3,549 | 2 | 0 | 674 |
| 019fb20b-b4b1-7250-9d7c-9d1e591ab852 duplicate-named copy | 3,610 | 8 | 0 | 674 |
| 019fb20b-b4b1-7250-9d7c-9d1e591ab852 original-named copy | 2,783 | 0 | 1 | 448 |
| 019fb221-54e7-7921-8c65-8a74da2e6d93 | 3,571 | 4 | 0 | 674 |
| 019fb250-eabe-7c82-a488-19e4541375f0 | 3,574 | 4 | 0 | 674 |
| 019fb274-e722-7332-affe-a739fe68e5e9 | 3,575 | 5 | 0 | 674 |
| 019fb276-99d1-7b71-9fa3-041e9331fa87 | 3,575 | 5 | 0 | 674 |
| 019fb277-0ddf-7500-9088-52cb4655be1a duplicate-named copy | 3,576 | 5 | 0 | 674 |
| 019fb277-0ddf-7500-9088-52cb4655be1a original-named copy | 2,857 | 0 | 1 | 491 |
| 019fb277-f977-7db1-8417-67f225aef4f3 | 3,575 | 5 | 0 | 674 |
| 019fb296-75ae-7943-913c-32d41f5de2d6 (Bohr) | 1 | 0 | 0 | 0* |
| 019fb296-9d1a-7301-901c-c35d99ccf3da (Darwin) | 1 | 0 | 0 | 0* |

* The generic change-map parser did not extract paths from these two event shapes; their specific patches and corresponding current-main implementations/tests were inspected separately.

Across the parsed large transcripts, the paths absent from the current tree were limited to: nginx-config/nginx.conf, frontend/proxy.preview.json, frontend/mock-preview-server.js, .codex-preview-server.mjs, .git/info/exclude, .tmp-ai-code-review.json, and .env.local. These do not establish that the associated work is integrated: the old gateway configuration needs semantic comparison against the maintained gateway templates; preview/mock tools need final-state and data-quality review; local Git metadata, review scratch data, and environment files must not be published. No local copy was deleted.

This is an archive-wide path index, not a content-level reconciliation. A path existing on main does not prove that every transcript patch is present there. The 41 unsuccessful patch results and two oversized records also remain unresolved at content level. Therefore this section does not authorize deleting any transcript or source/worktree copy.


## Failed-patch content reconciliation (2026-10-17)

A separate read-only byte-stream pass examined the 41 unsuccessful `patch_apply_end` results, with a 32 MiB per-record cap. It found 17 unique failed diff payloads across 18 path-target groups. Two records exceeded the cap and remain unexamined. The pass did not modify the archives.

The reviewed diffs do not justify replaying the old patches wholesale:

- IDP Kafka client-ID validation and its missing-client-ID test are present in current `main`.
- Google OAuth token-endpoint redirect rejection is present with the corresponding regression test.
- Task Blueprint validation fields and normalization are present in an expanded schema that also includes framework assurance criteria and typed criterion results.
- Authenticated access to ownerless legacy workflows is denied in current `main`; the current service and API tests cover that boundary.
- OpenClaw allowed-root resolution canonicalizes the accepted path before storing it. Current tests reject caller-selected outside-root paths and symlink escapes. One transcript-added positive-path test asserting that an allowed symlink is stored as its canonical target is not present by name; the source path resolution implements that behavior, but this audit did not run the Go suite.
- Several transcript test names are superseded by current, stricter behavior: Hermes and Odysseus execution is deliberately blocked before process/network activity until HAI can mediate individual tool calls. The older proposed tests that expected controlled CLI/agent execution must not be copied over that safety gate.
- Approval freshness, execution-policy binding, immutable launch-intent persistence, and indeterminate outcome handling are present in current `main` under current test names. Framework-selection provenance is normalized and conflicting persisted decisions/events are rejected; this is stricter than the earlier patch versions.
- The automation-form keyboard focus rule and the listed ignore rules are also present.

Evidence was based on the exact failed diff additions/deletions and source/test searches at `e768e7fb75eb8167fc6b6bcc5f43fbb38f7757b6`. This is not a full semantic review of every successful patch event, and the two oversized records plus five unattributed transcripts remain unresolved. No source patch was necessary from the reviewed failures, and no local copy is approved for deletion by this result.


## Primary checkout comparison (2026-10-17)

A fresh, read-only comparison refreshed `origin/main` to `498dcf7800f86be8c2e799f44d27ff649413881a`. The primary local checkout is branch `codex/hai-runtime-release` at `e07b9daeb3ba2630ecbe12a64d948f791203c241`; it is a stale, heavily changed worktree, not a clean checkout of current `main`.

- The worktree reported 244 modified tracked files, 448 deleted tracked files, and 1,578 untracked files.
- Content hashes were compared for 243 modified files whose paths exist on current `main` and 69 untracked files whose paths exist on current `main`. None were byte-for-byte matches.
- Thirty-five untracked Go test files share paths with current `main`. Every local test function name was present in the corresponding `main` file, but 33 files had differing test bodies. This name comparison is not proof of equivalent assertions or behavior.
- The scan did not inspect all contents in other worktrees, source archives, transcript archives, or output/evidence directories. It does not establish that local changes are integrated and does not authorize deleting them.

No local file was deleted. The preceding transcript findings remain pinned to their explicitly stated earlier `main` snapshot and were not re-characterized as a full current-main audit by this comparison.
