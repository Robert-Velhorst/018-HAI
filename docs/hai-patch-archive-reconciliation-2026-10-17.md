# HAI Patch Archive Reconciliation - 2026-10-17

## Scope and snapshot

Repository: `Robert-Velhorst/018-HAI`  
Remote `main` observed: `80fdeabd16efda6d9328fa0c89a4ab813f087908`  
Archives reviewed in place under `D:\\codex-temp`:

- `hai-c-worktree-clean-20261010.patch` (6,442,729 bytes; SHA-256 `13F4629FA4E4AD16B9F2AEF8578646DC1BB47A8AAF61E2DE42FEED9E1F1A9D44`)
- `hai-c-worktree-20261010.patch` (14,169,378 bytes; SHA-256 `1660BA97FD58406C683628C896B5F2639A0A523DD9422DBBD61EE6260941FDA8`)

Both archives remain on the local device. Their Git result blobs are present in the local object database, so source content is recoverable without expanding either patch into the checkout.

## Merged work and local-copy check

PR #153 (`feat(accountfeed): persist HTTP feed pagination cursors`) is closed and merged as `ad8a72ea4c744a3bd8adf83907be87d92ae7351a`. A live GitHub ancestry comparison confirmed current `main` contains that merge and is four commits ahead of it.

I checked the 17 files changed by PR #153 across all 11 registered HAI worktrees. No existing copy of those files in those worktrees was an exact blob match for current `main`; therefore, this scan found no safe direct-match file deletion from those registered worktrees. It did not search every directory on the device.

## File-level comparison

The 493-file clean patch represents 98,461 additions and 10,054 deletions relative to its historical base. Compared with current `main`:

- 324 result blobs match current `main` exactly.
- 168 paths exist on current `main` but have different content.
- One additional path, `nginx-config/nginx.conf`, is absent from current `main`.
- No file deletions are present in this patch.

The larger 682-file patch represents 199,137 additions and 74,020 deletions. A prior path/blob comparison found 324 exact-current matches and 358 paths that differ or are absent. Neither archive is a safe bulk overlay.

A line-level comparison of the 168 non-matching clean-patch files against current `main` found 5,245 archived additions and 7,649 archived deletions (net 2,404 fewer lines in the archive); 139 files are net smaller, four are net larger, and 25 have equal line counts. This is evidence of a reduced/older snapshot, not proof that every remaining delta is obsolete.

## Concrete regressions found in the stale snapshot

Direct blob-to-blob comparisons against current `main` found:

- `backend/internal/source/service.go`: the archived version removes the explicit Autofever sync-disabled guard that current `main` retains.
- `backend/internal/task/service.go`: the archived version removes ChatGPT-log context and MCP tool-call traces present in current `main`.
- `backend/internal/accountfeed/registry_service.go`: the archived version omits current cursor-presence handling and the HTTP-feed-only persistence/clearing rules.
- `backend/internal/source/trello.go`: the archived version removes the quiet-board cursor optimization, which avoids re-fetching a quiet board's full card/comment history.
- `backend/internal/opscontrol/service.go`: the archived version removes the durable control-review repository hook used by the current safety-control flow.
- `backend/internal/frameworkregistry/repository.go`: the archived version removes transactional preference-change audit persistence.
- `backend/internal/task/state_repository.go`: the archived version removes context-aware task-operation claim/heartbeat/completion contracts and the canceled operation state.
- `backend/internal/task/handler.go`: the archived version removes bounded task-history limits and the compact/full history projection.
- `backend/internal/ambient/service.go`: the archived version removes the compact owner-scoped overview and its smaller scan projection.
- `frontend/src/app/pages/governance-control/governance-control.component.html`: the archived version removes the expired-monitor recovery action and lazy/full-evidence loading controls.
- `idp/internal/app/authentication/auth_handler.go`: the archived version drops request-context propagation for logout, authentication checks, token refresh, and password-reset requests.
- `nginx-config/nginx.conf`: the archived standalone file embeds the fixed value `change-this-local-backend-key`; current Compose instead renders `nginx-config/nginx.conf.template` with an environment-provided secret. Compose does not mount or reference the archived standalone file. Do not port this file.

These are examples of why an old result blob differing from `main` is not evidence that it is a missing improvement. The remaining changed paths still require semantic review.

## Reconciliation attempt and safety

A three-way apply was attempted using an isolated temporary Git index initialized from `origin/main`; the patch had conflicts, including account-feed, agent-runtime, automation, and IDP files, and also contained audit/output paths outside the product-code subset. The attempt exited non-zero. The real checkout index was checked afterward: no staged changes were present. The primary checkout's tracked working-tree state remained 244 modified and 448 deleted files at that check.

No source patch was committed or pushed by this review, and no local source, archive, worktree, database, log, or transcript was removed. No test pass is claimed for these archives.

## Next decision

Continue semantic, module-by-module reconciliation from the archived blobs against current `main`. Only port a change when its intended behavior is still absent, it preserves current safeguards, and its tests pass on a clean branch based on the refreshed `main`. Keep both patch archives until every unique delta is either integrated and remotely verified or explicitly rejected with evidence.

## Current-state follow-up (2026-10-17)

Live GitHub API recheck confirms `main` is still `80fdeabd16efda6d9328fa0c89a4ab813f087908`. PR #153 is already merged as `ad8a72ea4c744a3bd8adf83907be87d92ae7351a`; no further merge action is needed. The fetch failure in the local checkout was a `.git/FETCH_HEAD` permission error, not evidence of a remote change.

The primary checkout is on `codex/hai-runtime-release` at `e07b9da`. A fresh porcelain inventory reports 244 modified tracked files, 448 tracked deletions, and 1,581 untracked paths. The remote branch tip is `e62b568`, while PR #36 is merged to `main` as `c31cec1`. The local checkout compared with the remote branch tip differs at 2,071 paths (3,905 insertions and 412,782 deletions); it is not a safe wholesale push or deletion candidate.

All 11 registered worktrees were rechecked. Status counts below are raw porcelain XY states, preserving staged/unstaged distinctions; `UU` and `UD` entries are unresolved merge conflicts and must remain untouched.

| Worktree | Current changes (XY: count) |
| --- | --- |
| Primary checkout | ` D:448, M :244, ??:1,581` |
| Model-maintenance audit | ` D:124, M :301, ??:95` |
| OpenClaw-artifact audit | ` D:42, M :108, ??:97` |
| Runtime-safety audit | ` D:1, M :13, ??:4` |
| PR #34 conflict-fix | ` A :122, AA:13, D :1, M :309, UD:1, UU:82` |
| PR #36 integration | ` A :99, AA:27, M :165, MD:1, UD:2, UU:227` |
| PR #36 local-delta | ` UU:59` |
| PR #36 resolution | ` M :264` |
| PR #34 resolution | ` D :4, M :60, ??:2, A :122, D :1, M :291, MM:38` |
| PR #35 resolution | ` M :17, UU:199` |
| D-drive source reconciliation | ` D :34, A :1, M :309` |

The D-drive index contains 3,178 paths versus 3,200 in `main`, with 424 differing paths. Its staged delta is 310 files (+7,138/-12,453); a separate unstaged layer deletes 34 `output/` audit/evidence files (-9,490). The previously identified clean patch has 493 file deltas, of which 476 result blobs match that staged index and 324 match current `main`. This confirms the patch is not an independent clean change set; the 152 staged non-main blobs need semantic review and overlap checks against open PRs.

No files or worktrees were removed. No source changes were pushed from these local snapshots. Keep the archives, unresolved worktrees, and output evidence until each item is either integrated and verified remotely or explicitly classified as reproducible and unnecessary.

### Staged-tree comparison to current main

A follow-up compared the complete D-drive staged index directly with the live `main` tree, not only the historical-base patch: 3,178 indexed paths versus 3,200 main paths; 3,177 shared paths, 400 differing shared blobs, 23 main-only paths, and one index-only path. The 424-path diff is net -8,909 lines relative to current main. The sole index-only path is `nginx-config/nginx.conf`, which embeds a fixed placeholder backend key and is not used by Compose. The largest positive per-file line deltas are `services/promptfoo-runner/package-lock.json`, `services/crewai-runner/requirements.lock`, and `services/crewai-runner/requirements.txt`; these are generated dependency-state changes, not standalone application features. No code from this staged snapshot is suitable for a bulk port. The 400 changed shared blobs still require targeted review for any behavior that might be a genuine improvement; size and path statistics alone are not acceptance evidence.
