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
