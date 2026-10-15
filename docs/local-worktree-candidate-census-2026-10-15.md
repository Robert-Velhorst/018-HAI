# HAI local worktree candidate census (2026-10-15)

## Purpose and scope

This note records a bounded comparison of two existing local HAI audit worktrees against the GitHub `main` commit `86fb1d9c53a69e1edb2fb098832aa6cb35d38eee`. The canonical SHA was read from GitHub. The local worktrees are based on `e07b9daeb3ba2630ecbe12a64d948f791203c241`, an older HAI snapshot.

This is not a complete audit of every local HAI-related file, every changed file in either worktree, or the entire device. It does not authorize cleanup.

## Model-maintenance audit worktree

Worktree: `hai-model-maintenance-audit`

At inspection, `git status --short --untracked-files=all` reported 498 entries: 425 tracked changes and 73 untracked paths, with no unmerged index entries.

Thirteen representative model-maintenance and OpenClaw files were checked for a same-path entry in canonical `main`; all thirteen paths already exist there. Their local contents differ from the canonical blobs. Six measured local files are shorter than their canonical counterparts:

| Path | Local lines | `main` lines |
| --- | ---: | ---: |
| `backend/internal/llm/maintenance_cooldown.go` | 190 | 191 |
| `backend/internal/openclawmaintenance/service.go` | 929 | 956 |
| `backend/internal/openclawmaintenance/worker.go` | 907 | 992 |
| `backend/internal/openclawmaintenance/pull_client.go` | 299 | 304 |
| `backend/internal/openclawreconcile/scheduler.go` | 94 | 97 |
| `backend/internal/runtimelab/openclaw_discovery_ledger.go` | 333 | 349 |

This representative path/size check does not establish semantic equivalence. It found no evidence that these local copies should replace or be copied over the canonical files.

## OpenClaw-artifact audit worktree

Worktree: `hai-openclaw-artifact-audit`

At inspection, `git status --short --untracked-files=all` reported 247 entries: 150 tracked changes and 97 untracked paths, with no unmerged index entries.

Among its untracked paths matching the OpenClaw, artifact, runtime, or maintenance feature scope, 46 paths were checked. All 46 already exist at the same path in canonical `main`; none was a missing canonical-path addition. Seven representative local files were measured and all were shorter than their canonical counterparts:

| Path | Local lines | `main` lines |
| --- | ---: | ---: |
| `backend/internal/agentruntime/openclaw_artifact_download.go` | 258 | 297 |
| `backend/internal/agentruntime/openclaw_artifact_url.go` | 316 | 381 |
| `backend/internal/openclawreconcile/artifact_archive.go` | 523 | 528 |
| `backend/internal/openclawreconcile/artifact_recovery.go` | 177 | 207 |
| `backend/internal/openclawreconcile/artifact_view.go` | 315 | 383 |
| `backend/internal/openclawmaintenance/service.go` | 913 | 956 |
| `backend/internal/openclawmaintenance/worker.go` | 894 | 992 |

This does not prove that every tracked local change is obsolete. The 150 tracked changes and remaining untracked content were not semantically reviewed by this bounded check.

## Disposition

- No source files were copied into `main` from these two snapshots: the sampled implementation paths are already present in canonical `main`, while the inspected local versions differ and are shorter.
- No local files, worktrees, logs, or generated `output/` artifacts were modified, moved, or deleted.
- The worktrees remain preserved for further path-level and semantic review.
- A cleanup candidate requires a separate exact-content/provenance check and the repository's canonical cleanup gate. This census alone is not sufficient evidence for deletion.

## Runtime-safety audit worktree

Worktree: `hai-runtime-safety-audit`

At inspection, `git status --short --untracked-files=all` reported 18 entries: 14 tracked changes and 4 untracked paths, with no unmerged index entries. All four untracked paths already exist at the same path in canonical `main`.

The local versions of the 13 changed runtime/host-runtime files measured in this pass were shorter than their canonical counterparts. One local artifact-download test file was three lines longer than `main`; both versions contain the same seven named test modes. The local test permits the allowlisted `url` mode, whereas canonical `main` rejects URL-mode artifacts along with denied, expired, metadata-changed, and size-changed cases. The canonical policy is stricter; the local variation is not a safe change to port.

This is a focused safety comparison, not a semantic audit of all 14 tracked changes or a complete verification of runtime safety.


## Source-reconciliation worktree

Worktree: `D:/hai-source-reconciliation`

Git inspection used a command-scoped `safe.directory` exception because the D: volume does not preserve ownership metadata; no global Git configuration was changed. The worktree reported 344 changed paths. Comparing those paths with the canonical `main` tree found one path absent from `main`: `nginx-config/nginx.conf`. The other changed paths have same-path entries in `main`; this path check does not establish that their local contents are equivalent or safe to port.

The 127-line Nginx candidate was inspected without printing credential-like values. It contains a hard-coded placeholder `X-HAI-Backend-Key change-this-local-backend-key` directive. This file was not ported; it needs a separately designed, secret-safe configuration if a current deployment requirement exists.

No local source files or worktree data were modified or deleted. The 343 same-path changes remain outside this bounded semantic review.


## PR-resolution worktrees

The PR-resolution worktrees for PRs #34 and #36 are divergent, dirty snapshots. At inspection, the #34 conflict-fix worktree had 528 status entries and 274 unmerged index-stage entries; the #36 integration worktree had 521 status entries and 739 unmerged index-stage entries. The #36 local-delta worktree retains unresolved file conflicts. These snapshots are not safe to push or copy wholesale.

A path-level comparison of six PR-resolution worktrees found 22 distinct changed paths absent by name from canonical `main`. The SQL paths are historical migration renumberings, not missing schema work:

- The 16 local `pre/0060` through `pre/0067` up/down files correspond to canonical `pre/0119` through `pre/0126`. After whitespace normalization, 15 of 16 local SQL files match canonical content. The old task-completion history up migration does not: it omits `NULLIF(..., 'null'::jsonb)` before `COALESCE` for `successCriteria`. Canonical `pre/0123` includes that JSON-null handling; the older SQL is not safe to port.
- The old durable-job queue lease index migration corresponds to canonical `post/0007_durable_jobs_queue_lease_index`; both up/down SQL files match after whitespace normalization.
- `backend/internal/llm/maintenance_context_test.go` uses older, incompatible method names. Canonical `main` has its own context-aware maintenance, cancellation, and lease tests under the current API; this file was not copied.
- The only other absent product path found was the Nginx config already described above, which contains a placeholder backend key and remains excluded.

This path and migration comparison does not semantically review the many same-path source changes in those dirty worktrees. No conflict was resolved and no local data was changed or deleted.


## Primary checkout untracked source screen

A fresh status in the primary checkout reported 692 tracked changes and 1,582 untracked paths; 1,501 untracked paths are under generated `output/`. These counts are a point-in-time inventory, not a complete classification of every HAI-related local artifact.

Among the non-generated, non-agent-state, non-log, non-environment untracked source/support candidates, 65 paths exactly match a path in canonical `main`. Git blob IDs were compared for those 65: one file is byte-identical and 64 differ in content. The exact match is `backend/internal/runtimelab/owner_gate_handler_test.go` (12,495 bytes, measured from the identical canonical blob). The other 64 files cannot be treated as safe duplicates based on path alone.

No local files were deleted. The exact match is only a cleanup candidate; the repository cleanup gate and the existing local cleanup restriction still apply.

## Primary-checkout reconciliation

A fresh status of the primary checkout reports 692 tracked changes (448 deletions and 244 modifications) and 1,582 untracked paths. Its branch, `codex/hai-runtime-release`, was 440 commits behind its configured upstream `origin/codex/hai-runtime-release` at inspection; that is not a count of commits behind `main`. The local HEAD is `e07b9daeb3ba2630ecbe12a64d948f791203c241`. The canonical `main` ref was independently confirmed at `86fb1d9c53a69e1edb2fb098832aa6cb35d38eee`.

Comparing the current working tree to canonical `main` yields 2,100 path entries, with roughly 421,294 removed lines versus 4,400 added lines in the text diff. This is dominated by branch divergence and must not be treated as a ready-to-port change set. The local `.env-backend`, `.env-gateway`, and `.env-idp` are intentionally excluded from any port; their values were not inspected or exposed.

The non-generated, non-agent-state, non-log, non-environment untracked source/support screen covers 65 paths. All 65 paths exist in `main`; blob IDs show one exact match and 64 content differences. The identical file is `backend/internal/runtimelab/owner_gate_handler_test.go` (12,495 bytes). Reviewed differences in local test copies are not safe upgrades: for example, the local worker reliability test drops the approval-gate assertion, and the local source-approval test drops revision-digest and reminder-override cases. The local Promptfoo manifest is also older (`0.123.1`) than canonical `main` (`0.124.1`); its larger lockfile does not indicate a newer implementation.

A read-only disk inventory found 1,507 files under local `output/` totaling 126,491,186 bytes. The directory contains mostly logs, JSON/JSONL reports, exit markers, and a small number of archives/dumps. It appears to be generated run evidence rather than product source, but its contents have not been semantically reviewed or matched to canonical evidence; none is approved for port or cleanup by this census. Agent-state files under `.claude-flow/` and `.swarm/` were also preserved and not inspected for content.

No local files, environment files, logs, agent state, or generated outputs were changed, moved, or deleted.

### Additional security and API spot checks

A targeted comparison of the primary checkout's modified `backend/internal/llm/policy.go` against canonical `main` found no safe upgrade to port. Both versions bound provider response reads, but `main` uses the named `providerProbeResponseLimitBytes` and rejects provider endpoints containing credentials, query parameters, or fragments. The local variant uses a literal response limit and permits query/fragment forms; canonical policy is stricter.

The local verification, pursuit, and ambient service files also expose older/different helper and owner-scoped API surfaces than canonical `main`. Their local diffs are not drop-in replacements; no whole-file port was made from these snapshots.


## Primary-checkout deployment, workflow, and build-artifact review

A focused review compared the primary checkout's current changes in `.github/workflows/ci.yml`, `backend/Dockerfile`, `docker-compose.local.yml`, and `README.md` with canonical `main`. This is a scoped review of these files, not a complete semantic audit of all 692 tracked changes.

The reviewed deltas are not safe to port as a batch. The local CI workflow changes Go setup from 1.27.2 to 1.25.13, removes the HAI cleanup-readiness contract job and several contract/race tests, and drops other build-context, runner, and integration checks present on `main`. The local Compose diff removes configured variables and the optional SearXNG research-discovery service, changes the backend state-permission helper to a PostgreSQL image with a different shell command and capabilities, and changes a gateway health check. The local README removes current routes and documentation references and describes the older Go version. These observations indicate divergence/regression risk; they do not establish that each individual edit is harmful in every environment. No changes from this sample were ported.

A read-only filesystem inventory measured generated/build-oriented directories: `output/` contains 1,507 files (120.6 MiB), `frontend/dist/` contains 17,549 files (369.1 MiB), and `installer/release/` contains 1,975 files (35.4 MiB). The latter includes an installer and payload. Contents and rebuildability were not fully validated, and this census does not authorize removal; all remain preserved. Other large state such as PostgreSQL WAL and the backend deployment artifact was also left untouched.



## IDP authentication-test snapshot review

Ten untracked files under `idp/internal/app/authentication/` were screened against the same-path files in canonical `main`. Their named test functions match the canonical test set in the sampled files, but the local snapshots remove `context.Context` parameters from calls such as `RefreshToken`, `Logout`, `IsUserAuthenticated`, and password-reset methods. Canonical tests call the context-aware APIs, so the local test copies are incompatible with the current canonical API and are not safe to port. This comparison is limited to those ten files; it is not a full behavioral review of every assertion.



## Backend untracked-test inventory

A test-declaration comparison covered the 26 untracked Go test files under `backend/` that also have a path in canonical `main`. It found no test functions unique to the local snapshots. Canonical `main` contains additional test functions absent from the local copies in three files: `ambient/scheduler_test.go` (scheduler policy-disabled state), `operations/source_approval_test.go` (explicit approval overriding a future review reminder), and `pursuit/completion_outcome_integrity_test.go` (three receipt/timeline cases). The local pursuit and source-approval variants are shorter and omit coverage present in `main`; the already-reviewed worker reliability variant also omits an approval-gate assertion. This inventory compares test declarations and selected assertions; it does not prove every assertion in all 26 files equivalent. No local test was ported.



## Frontend and runner-script test snapshot review

A test-name comparison of local untracked frontend/script tests against their same-path canonical files found no additional safe behavior to port. The local `frontend/scripts/control-room-logic.test.mjs` lacks the canonical uncapped-pursuit-count metric case; `release-cache.test.mjs` lacks the restrictive-script-policy same-origin theme case; and `scripts/test_python_runner_container_smoke.py` lacks the canonical Grype liveness smoke case. The local `scripts/test-frontend-build-resource-limit.cjs` has a differently named case that documents invalid worker values passing through the installed parser; canonical `main` instead tests that invalid custom values fall back to host concurrency. These differences are consistent with an older test snapshot, not a safe additive patch. This review compares the identified test declarations and the relevant resource-limit case only.



## Commit-history and PR #36 reconciliation

The earlier census wording conflated the branch's upstream lag with its relationship to `main`; this correction supersedes that interpretation. Current Git evidence shows `origin/codex/hai-runtime-release` is 440 commits ahead of local `HEAD` (zero commits behind it), while the graph comparison `origin/main...HEAD` is 42 main-only commits and 292 local-only commits. Commit counts alone do not establish that code is missing from `main` because the historical PR was squash-merged.

GitHub reports PR #36 as merged, with branch head `e62b56843eb8bbbf9a566aff5e883f9adf375429` and squash-merge commit `c31cec1ba2a25fcfd280b1f2f499297e54d96aa4`. The local `HEAD` is an ancestor of that PR head; the PR head tree and merge-commit tree have zero changed paths; and the merge commit is an ancestor of current `main`. PR #36 recorded 727 commits and 1,976 changed files. Therefore, the committed tree through PR #36 is already represented in `main`; those commits must not be replayed as new work.

The remaining review target is the dirty working tree, not the merged commit history. At this check it still had 692 tracked status entries and 1,582 untracked entries. Among 243 present modified tracked files hash-checked (excluding `.env.example`), four match the PR #36 merged tree, none match current `main`, and 239 differ from both. A separate review is required before any of those differences can be ported. The four matches are historical copies, not current-main duplicates. No local files were changed or deleted.



## Tracked source-delta inventory

A fresh path comparison confirms all 448 tracked paths currently marked deleted in the primary checkout still exist in canonical `main`; none is a missing upstream file to recreate. The local working tree has no copy at those paths, so there is nothing at those paths to reclaim locally.

A function-declaration inventory compared 141 present modified Go files with same-path files in `main`: 89 files have function names present only in `main`, while six have at least one name present only in the local snapshot. This is structural evidence, not a behavior-equivalence proof. The previously reviewed local-only names include old helper/API variants in source, pursuit, router, and verification code; test-name coverage and policy comparisons show canonical `main` contains stricter or additional behavior in the sampled areas. No local implementation was accepted for port from name differences alone.

The measured source delta remains open for semantic review: 243 present modified tracked files hash-checked (excluding `.env.example`) yielded four blobs equal to the PR #36 merge tree, zero equal to current `main`, and 239 equal to neither. This supports neither wholesale copying nor deletion; files differing from both remain preserved until individually reviewed.

## E2E matrix and cleanup-script follow-up

A focused review of the primary checkout's untracked browser-matrix artifacts found that canonical `main` already contains `frontend/e2e/tests/owned-browser-matrix.spec.ts`, its support fixture, and its handoff document. The local untracked fixture is an older variation: it permits one synthetic login POST and lacks the canonical fixture's authenticated-session refresh and structured, redacted console diagnostics. Canonical `main` rejects every browser-origin write and retains the fuller route/state/keyboard matrix. The local handoff describes a matrix spec, but that spec is not present in the old primary checkout; the current canonical spec is present. No browser-matrix code needs porting from this local snapshot.

Two generated `output/` scripts were reviewed. `cleanup-unused-hai-test-containers-20261001.ps1` targets exited HAI Go test/build runners, but saves raw container logs to a temporary evidence directory and is not connected to canonical cleanup contracts. It was not ported as a destructive operator utility. `cleanup-aged-successful-test-containers-20261002.ps1` targets CoFounder containers and is outside HAI scope. Neither file was executed.

A fresh extension/size inventory of `output/` found 1,507 files: 884 logs, 420 JSON, 82 JSONL, 60 exit markers, 37 Markdown, 11 PowerShell, and a small number of other files. JSONL accounts for approximately 115.25 MiB. This classifies the directory as run evidence and scripts, not a bulk of product source; individual evidence and provenance have not been reconciled, and no deletion is authorized by this review.

PR #157's new hosted CI run `38786365450` completed all 30 jobs successfully after the stale router-call contract was corrected. This verifies that PR head's hosted test suite only; it does not complete the local-file reconciliation or authorize deletion.
