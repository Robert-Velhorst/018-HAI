# Linked HAI worktree audit (2026-10-16)

## Purpose and scope

This report records a read-only comparison of the linked HAI Git worktrees and selected adjacent local HAI material against the live canonical branch. It follows the primary-checkout reconciliation in `local-worktree-candidate-census-2026-10-16-addendum.md`.

Repository: `Robert-Velhorst/018-HAI`  
Live `origin/main`: `df42ab2475b0814bacfc0f5989af85f855a64e14`  
Primary checkout: `codex/hai-runtime-release` at `e07b9daeb3ba2630ecbe12a64d948f791203c241`

The audit compared Git/worktree status, refs, changed paths, selected implementation behavior, and exact path/blob overlap where feasible. It was static and read-only. It did not run the product test suites or inspect secret values. “No port candidate” means none of the reviewed deltas was safe to copy as-is; it does not prove every assertion or generated artifact is semantically redundant.

## Worktree inventory

The 14 linked worktrees occupy approximately 1,805 MiB in aggregate file bytes, including duplicate checkouts and generated build data. This is not a unique-data estimate. Ten worktrees were dirty and four clean at inventory time. No worktree was removed or archived.

| Worktree | Approx. size | Status / review disposition |
|---|---:|---|
| Primary checkout | 979.4 MiB | Dirty: 448 tracked deletions, 244 tracked modifications, 1,582 untracked entries. Separately reconciled in the companion addendum. |
| `hai-pr36-resolution` | 490.1 MiB | Dirty; 264 tracked entries. Contains older/different code and a connector assuming a contract current main explicitly disables. |
| `hai-pr36-integration` | 41.1 MiB | Dirty; 521 tracked entries. Merge-conflicted snapshot. No safe code port identified. |
| `hai-resolve-pr35` | 37.1 MiB | Dirty; 516 tracked entries and two untracked entries. Repeats an older conflicted snapshot; no separate safe port identified. |
| `outcome-monitor-metrics` | 36.5 MiB | Clean; PR #157 is merged. |
| `hai-redact-ledger-fixture` | 36.4 MiB | Clean; PR #147 is merged. |
| `D:/hai-source-reconciliation` | 34.9 MiB | Dirty; 310 staged changes and 34 unstaged deletions. Local-only blobs/history remain unclassified; do not remove. |
| `hai-pr36-local-delta` | 34.9 MiB | Dirty; 59 conflicted entries, a narrower view of the same PR36 snapshot. |
| `hai-model-maintenance-audit` | 21.0 MiB | Dirty; 425 tracked changes and 95 untracked files. No safe port identified. |
| `hai-openclaw-artifact-audit` | 19.9 MiB | Dirty; 150 tracked changes and 97 untracked files. Artifact/retention/recovery paths already exist on main in different versions; no safe port identified. |
| `hai-pr34-conflict-fix` | 18.7 MiB | Dirty; 528 tracked entries and 96 unresolved index paths. No safe port identified. |
| `hai-resolve-pr34` | 18.4 MiB | Dirty; 516 tracked entries and two untracked entries. Its queue/lease index is already on main; an added IDP sender silently drops events when Kafka is absent. |
| `D:/codex-temp/hai-pr153-repair-20261015` | 18.3 MiB | Clean but detached/divergent; not PR #153's remote branch. Preserve its unique history until separately reconciled. |
| `hai-runtime-safety-audit` | 18.2 MiB | Dirty; 14 tracked entries and 4 untracked entries. Local changes weaken effect proof, cancellation, workspace-link validation, and redaction. |

The aggregate count and sizes are point-in-time inventory values and may change as files/build outputs change.

## Audit findings by area

### Runtime, backend, and source workflows

Reviewed local changes weakened or removed behaviors present on main, including request cancellation and owner scoping, endpoint/config validation, bounded rate limiting, durable approval/confirmation for emergency-stop and runtime-resume, atomic automation-event publication, readiness response privacy, pause short-circuiting and error redaction in background work, source-sync cursor handling, and backup/recovery protections. Current main contains later or stricter implementations in the sampled paths. No backend/source implementation was recommended for direct port.

### Frontend and identity

Reviewed changes include an outdated runtime-resume UI contract, faster health polling than current adaptive behavior, an acceptance test that performs actions despite a read-only label and lacks the expected isolation setup, and weakened identity validation/ownership behavior. Current main has stricter or equivalent protections. No frontend or IDP code was recommended for direct port.

### CI, deployment, installers, and service runners

Reviewed workflow and service snapshots downgrade toolchain/runtime inputs or remove CI checks, weaken Windows backup/recovery safeguards, alter runner dependencies/images, eagerly initialize optional services, and weaken configuration handling. Current main has newer or stronger contracts. No operations/service/installer changes were recommended for direct port.

### Tests and untracked candidates

- A prior name-level scan extracted 162 untracked test names already present on main. Name overlap is not assertion-equivalence proof.
- Linked-worktree reviews found no untracked implementation/test that was both unique and verified safe to port.
- One primary-checkout untracked file is an exact main blob match: `backend/internal/runtimelab/owner_gate_handler_test.go`, 12,495 bytes. It is a small, exact-match candidate, but it has not been deleted.
- In `hai-model-maintenance-audit`, 89 of 95 untracked paths exist on main with different content; the six absent paths are four agent-state JSON files and two browser-acceptance logs.
- In `hai-openclaw-artifact-audit`, all 97 untracked paths exist on main with different content.
- In `hai-runtime-safety-audit`, all four untracked paths exist on main with different content.
- Four local service manifests/lockfiles reviewed separately are older/different than main.

### Divergent snapshots and local-only data

The D: source-reconciliation worktree has staged local-only blobs and omitted reports relative to main; those blobs were not semantically reconciled and must be preserved. The detached `hai-pr153-repair-20261015` checkout is not the PR branch: it has 226 commits unique to its local history, while main has 86 commits absent from it, and their trees differ across 2,126 paths. Its distinct runtime/DSH work is already on main in more hardened forms, but the unique commit history has not been independently preserved or classified.

PR #153 is a separate open draft on `codex/account-feed-cursor-20261015` at `8ac42197`. It remains `UNSTABLE`; its persisted HTTP-feed pagination cursor proposal needs its own review and CI. Do not treat the detached checkout as that PR.

## Credential exposure warning

Three PR34/36-related worktrees contain tracked `.env-backend` and `.env-idp` files with non-placeholder-looking values in fields named `DB_PASSWORD` and `JWT_SECRET`. Their values were not printed or copied. Commit `e07b9da` is reachable from remote refs including `codex/hai-runtime-release`, `pr-36`, and `pr/36`. Treat these credentials as potentially exposed and rotate them through the appropriate secret-management process. Do not copy these files into a PR or another worktree.

Some local Nginx configs contain a placeholder backend key and are not equivalent to main's environment-rendered template. Do not port or deploy them.

## Disposition and cleanup gate

No implementation or documentation from the reviewed linked worktrees was copied into a source PR because the reviewed changes were already represented by newer main behavior, duplicated, conflicted, weaker, run-specific, or not sufficiently validated. PRs #41 and #42 are merged; their merge commits are ancestors of current main. PRs #147 and #157 are also merged.

No local files, worktrees, environment files, databases, logs, agent state, or generated output were deleted, moved, archived, or rewritten by this audit. This inventory does not prove that all HAI-related files elsewhere on the device were found, nor does it prove semantic integration of transcript archives. Preserve all unresolved worktree history and local-only data. Cleanup remains limited to separately verified exact-main candidates under the existing cleanup gate; this report itself is not deletion authorization.

The current audit PR is documentation-only. Its checks must be rerun for this added commit; passing CI is not evidence that local files are safe to delete.


## Transcript archive verification recheck (2026-10-16)

Read-only verifiers were run from a temporary clean checkout of current `main` at `df42ab2475b0814bacfc0f5989af85f855a64e14`. No removal script was invoked.

- Main transcript-root readiness verification stopped because the manifest-listed source file `rollout-2026-07-30T10-02-49-019fb20c-2b95-7550-8304-1117de872148.jsonl` is missing under `D:\\codex-temp\\hai-completed-agent-sessions`. The exact filename was searched under `D:\\codex-temp`, `C:\\Users\\NO\\.codex`, `C:\\Users\\NO\\Documents\\Codex`, and then `C:\\Users\\NO`; no matching file was found. This search establishes only that the filename was not found in those enumerated trees, not that no backup exists elsewhere.
- The supplemental read-only verifier completed successfully: 9 files, 14,013,737,298 logical bytes, 2 candidate hashes verified, 7 retained files, 19 report-source links verified. It returned `cleanup_authorized=false` and `deletion_performed=false`.
- Since the main-root source verifier failed, the combined archive cleanup gate remains unsatisfied. All transcript roots and files remain untouched. Recovering or formally reconciling the missing source record and rerunning the full gate are required before candidate deletion can be considered.

PR #37 was freshly verified as merged into `main` with successful checks, but that does not override the missing main-root source file or the supplemental verifier's explicit no-authorization result. The current audit PR #156 has a new head commit from this report update; its checks must complete before it can be considered merge-ready.
