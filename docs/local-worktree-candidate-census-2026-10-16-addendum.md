# HAI local worktree reconciliation addendum (2026-10-16)

## Snapshot

Repository: `Robert-Velhorst/018-HAI`  
Canonical `main` at the refreshed snapshot: `efd486b85ca02907416d5a0e0f162a6c49ef935a` (PR #156 base at refresh: `86fb1d9c53a69e1edb2fb098832aa6cb35d38eee`)  
Primary local checkout: branch `codex/hai-runtime-release`, HEAD `e07b9daeb3ba2630ecbe12a64d948f791203c241`

This is a read-only follow-up to the bounded census in this PR. The path inventory and representative comparisons below do not constitute a semantic review of every local delta.

## Current checkout evidence

The primary checkout reports 448 tracked deletions, 244 tracked modifications, and 1,582 untracked paths. All 448 deleted tracked paths are present by path in the current `main` tree. They are absent from the local worktree, so there is no local file content from those paths to port or remove.

The screened non-generated, non-agent-state, non-log, non-environment untracked source/support set contains 65 paths. All 65 paths exist in current `main`; one blob is byte-identical (`backend/internal/runtimelab/owner_gate_handler_test.go`, 12,495 bytes), while the other 64 differ. A same-path entry alone is not evidence that a differing local copy is newer or safe to replace canonical code.

Representative local comparisons found current `main` already has later implementations or stricter behavior in ambient scanning, OpenClaw ecosystem mutation handling, runtime validation, frontend routing, and Windows build inputs. For example, current `main` includes additional cancelled-upload guards, newer Go installer tooling, stricter port validation, and more recent Angular/dependency versions than the corresponding local snapshots. These samples do not prove every local modification is obsolete; they do show why the divergent working tree cannot be pushed wholesale.

## Generated evidence and reports

A read-only inventory found 1,507 files under local `output/`, totaling 126,491,186 bytes. This is generated run evidence, not product source. The raw logs and JSON/JSONL artifacts were not semantically reconciled and are not approved for deletion by this addendum.

Eight local report paths also exist in `main`. Six compare byte-for-byte after substituting the local workspace and temporary-directory paths with the documented placeholders. The frontend safe-outcome report has the same line count and findings but uses machine-specific absolute links locally; the main copy uses repository-relative links. The local production-durability report is an older, much shorter snapshot than the current main copy. These are not grounds to delete the raw test logs or other output files.

## Disposition

- PRs #41 and #42 are merged into `main`.
- No code from the divergent local checkout was copied into a new PR in this pass; no reviewed local-only feature was identified that was safe to port without a broader semantic and test review.
- Draft PR #158 was closed unmerged after its diff was found to replace newer Trello regression coverage on `main` with an older fixture.
- No local files, environment files, databases, logs, agent state, or generated output were deleted or modified.
- The 244 tracked modifications, 64 differing untracked source/support files, and raw output artifacts remain unresolved. This addendum is not a completion claim or authorization to clean them up.

## Materialized gateway configuration candidate

The local checkout also contains a modified `nginx-config/nginx.conf`, a path absent from current `main`. It is not mounted by current Compose. Current `docker-compose.local.yml` mounts `nginx.conf.template` and renders it with `envsubst` from environment-provided backend and preview secrets. Current `nginx.conf.template` includes the local file's reviewed changes (server-token suppression, refresh-cookie forwarding, and forwarded-header handling) plus additional routes and protections. The local materialized file instead contains a hard-coded placeholder backend key, so it is not safe to port or use as the canonical runtime config.

Deleting this obsolete local file was attempted after confirming the replacement path and behavior, but Windows denied the operation for insufficient access rights. No alternate deletion method was used; the local file remains unchanged. Its prior committed version remains recoverable from the local Git object database.

## Refreshed audit (2026-10-16)

The live `main` ref now resolves to `efd486b85ca02907416d5a0e0f162a6c49ef935a`; PR #156 is based on `86fb1d9c53a69e1edb2fb098832aa6cb35d38eee`. Current worktree porcelain still reports 448 tracked deletions, 244 tracked modifications, and 1,582 untracked files. The all-path GitHub tree was read at the live `main` commit and was not truncated.

The 65 screened untracked source/support paths still all exist in `main`: one is an exact blob match (`backend/internal/runtimelab/owner_gate_handler_test.go`), and 64 differ. A declaration-level comparison across 139 existing modified Go files and 49 modified TypeScript files found no confirmed local-only implementation after reviewing flagged symbols: the local pursuit dashboard helper is represented in the newer dashboard builder; route setup is now lifecycle-aware; source extraction filtering has been replaced with owner-scoped bounded pagination; and verification helpers have expanded equivalents. Two local-only test names assert superseded contracts: main has stricter provider endpoint/TLS tests, and emergency-stop authorization now derives its scope from the authenticated actor. This is a symbol/contract triage, not a line-by-line semantic review of every modified file.

Across 40 untracked Go tests and four untracked frontend script-test suites, no local-only test names were found; current `main` has equal or broader named coverage. No modified tracked file was an exact blob match against current `main`. The local `frontend/package-lock.json` has no package entry absent from main, while main has one additional entry. Local generated CrewAI `requirements.txt` is substantially larger than main and is not a candidate to port.

The hidden `output/` tree still contains 1,507 files (126,491,186 bytes); eight paths exist in main and none of the local output blobs match main. The dated PostgreSQL/race-test evidence, `.claude-flow`/`.swarm` state, all non-identical outputs, and the materialized `nginx-config/nginx.conf` remain untouched. Windows denied deletion of the exact-match owner-gate test and the obsolete Nginx file in prior attempts; this audit did not retry either deletion or use an alternate method.

No source code was ported in this refresh because the candidates examined were already represented by newer `main` behavior or were run-specific evidence/state. PR checks remain pending; this addendum does not claim a complete semantic audit or authorize deletion of divergent files.

## Complete current-worktree reconciliation (2026-10-16)

### Authoritative refs and status

A fresh fetch updated local `origin/main` to `df42ab2475b0814bacfc0f5989af85f855a64e14`. The checkout remains at `e07b9daeb3ba2630ecbe12a64d948f791203c241` on `codex/hai-runtime-release`. PR #41 merge commit `71102fd40a960b59b70e56f902cb4b3dc91f1390` and PR #42 merge commit `b1caa0b074793e794ea8bc340cba9ac562924ff7` are both ancestors of current `main`.

A fresh `git status --porcelain=v1 --untracked-files=all` contains 448 tracked deletions, 244 tracked modifications, and 1,582 untracked files. The counts reconcile to 2,274 status entries.

### Blob-level reconciliation against current main

The current main tree was loaded locally from the refreshed ref and compared against working-file Git blobs:

- All 448 locally deleted tracked paths exist on current `main`. Their local file content is already absent, so this checkout has no remaining bytes at those paths to reclaim.
- Of 244 modified tracked files, none is an exact blob match. 243 paths exist on main but contain different content; `nginx-config/nginx.conf` is absent from main.
- Of 1,582 untracked files, 73 paths also exist on main. One is byte-identical: `backend/internal/runtimelab/owner_gate_handler_test.go` (12,495 bytes); 72 same-path files differ. The other 1,509 untracked paths are absent from main.
- The earlier 65-path untracked source/support screen was narrower; this full status-path comparison supersedes that count. Path overlap alone does not establish that a differing local file is newer or safe to port.

### Subsystem review disposition

Independent read-only reviews covered backend; frontend and IDP; operations, CI, documentation, installer and gateway configuration; and service runners plus untracked test/source candidates. No reviewed local implementation or test was recommended for port as-is.

The specific differences reviewed are predominantly regressions relative to current main: removal of request cancellation and owner scope in authentication, weaker endpoint/config validation and rate-limit bounds, loss of durable approval/confirmation behavior for emergency-stop and runtime-resume actions, non-atomic automation-event publication, exposure of detailed readiness checks, weaker pause/error-redaction behavior in background work, reduced CI/backup/recovery safeguards, and older runtime/dependency inputs. Current main also contains later owner-scoped source retrieval, workflow and dashboard implementations, runner files, and test coverage that are absent from this checkout.

The local CrewAI Dockerfile additionally sets `CREWAI_DISABLE_TELEMETRY=true`. CrewAI's current documentation says either that variable or `OTEL_SDK_DISABLED=true` disables CrewAI telemetry; current main already sets the latter. The local variable therefore does not change the present behavior and was not ported as a standalone change. [CrewAI telemetry documentation](https://github.com/crewAIInc/crewAI/blob/main/docs/v1.12.2/en/telemetry.mdx)

The test-name scan found 162 extracted untracked test names already present on main. This is a coverage/name comparison, not proof that every local assertion is equivalent; no local-only test was identified as a safe port candidate in the reviewed files.

### Local storage inventory and preservation

A fresh read-only measurement found:

- `output/`: 1,507 files, 126,491,186 bytes (120.6 MiB).
- `frontend/dist/`: 17,549 files, 387,064,530 bytes (369.1 MiB).
- `installer/release/`: 1,975 files, 37,126,164 bytes (35.4 MiB).
- `.git/`: approximately 147.1 MiB; `db_data_automation/` and `db_data_idp/`: approximately 46.0 MiB and 45.9 MiB.

Generated outputs, installer artifacts, databases, Git history, agent state, logs, and all divergent working files remain untouched. No HAI-named container appeared in `docker ps`. No listener was found on ports 4200, 4201, 8088, 5173, or 8080; this is a scoped check, not a complete host-process audit. No runtime was stopped.

### Outcome and limits

No source code was copied into a PR in this audit because the reviewed local versions were superseded or weaker than current main, and the one exact-match untracked test had already been identified as a deletion candidate in the earlier audit. No local file was deleted, moved, compressed, or rewritten. The local source audit is substantially broader, but raw generated evidence was not semantically reviewed and is preserved. This addendum is not a claim that every HAI-related file on the device has been examined, nor does it authorize deletion of divergent content.