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

A fresh status of the primary checkout reports 692 tracked changes (448 deletions and 244 modifications) and 1,582 untracked paths. Its branch, `codex/hai-runtime-release`, is 440 commits behind `main`; the local HEAD is `e07b9daeb3ba2630b3e3a9f92cf875ecbc3560b6d`. The canonical `main` ref was independently confirmed at `86fb1d9c53a69e1edb2fb098832aa6cb35d38eee`.

Comparing the current working tree to canonical `main` yields 2,100 path entries, with roughly 421,294 removed lines versus 4,400 added lines in the text diff. This is dominated by branch divergence and must not be treated as a ready-to-port change set. The local `.env-backend`, `.env-gateway`, and `.env-idp` are intentionally excluded from any port; their values were not inspected or exposed.

The non-generated, non-agent-state, non-log, non-environment untracked source/support screen covers 65 paths. All 65 paths exist in `main`; blob IDs show one exact match and 64 content differences. The identical file is `backend/internal/runtimelab/owner_gate_handler_test.go` (12,495 bytes). Reviewed differences in local test copies are not safe upgrades: for example, the local worker reliability test drops the approval-gate assertion, and the local source-approval test drops revision-digest and reminder-override cases. The local Promptfoo manifest is also older (`0.123.1`) than canonical `main` (`0.124.1`); its larger lockfile does not indicate a newer implementation.

A read-only disk inventory found 1,507 files under local `output/` totaling 126,491,186 bytes. The directory contains mostly logs, JSON/JSONL reports, exit markers, and a small number of archives/dumps. It appears to be generated run evidence rather than product source, but its contents have not been semantically reviewed or matched to canonical evidence; none is approved for port or cleanup by this census. Agent-state files under `.claude-flow/` and `.swarm/` were also preserved and not inspected for content.

No local files, environment files, logs, agent state, or generated outputs were changed, moved, or deleted.

### Additional security and API spot checks

A targeted comparison of the primary checkout's modified `backend/internal/llm/policy.go` against canonical `main` found no safe upgrade to port. Both versions bound provider response reads, but `main` uses the named `providerProbeResponseLimitBytes` and rejects provider endpoints containing credentials, query parameters, or fragments. The local variant uses a literal response limit and permits query/fragment forms; canonical policy is stricter.

The local verification, pursuit, and ambient service files also expose older/different helper and owner-scoped API surfaces than canonical `main`. Their local diffs are not drop-in replacements; no whole-file port was made from these snapshots.
