# HAI Patch Archive Reconciliation - 2026-10-17

## Scope and snapshot

Repository: `Robert-Velhorst/018-HAI`  
Remote `main` observed: `80fdeabd16efda6d9328fa0c89a4ab813f087908`  
Archives reviewed in place under `D:\\codex-temp`:

- `hai-c-worktree-clean-20261010.patch` (6,442,729 bytes; SHA-256 `13F4629FA4E4AD16B9F2AEF8578646DC1BB47A8AAF61E2DE42FEED9E1F1A9D44`)
- `hai-c-worktree-20261010.patch` (14,169,378 bytes; SHA-256 `1660BA97FD58406C683628C896B5F2639A0A523DD9422DBBD61EE6260941FDA8`)

Both archives remain on the local device. Their Git result blobs are present in the local object database, so source content is recoverable without expanding either patch into the checkout.

## File-level comparison

The 493-file clean patch represents 98,461 additions and 10,054 deletions relative to its historical base. Compared with current `main`:

- 324 result blobs match current `main` exactly.
- 168 paths exist on current `main` but have different content.
- One additional path, `nginx-config/nginx.conf`, is absent from current `main`.
- No file deletions are present in this patch.

The larger 682-file patch represents 199,137 additions and 74,020 deletions. A prior path/blob comparison found 324 exact-current matches and 358 paths that differ or are absent. Neither archive is a safe bulk overlay.

## Concrete regressions found in the stale snapshot

Direct blob-to-blob comparisons against current `main` found:

- `backend/internal/source/service.go`: the archived version removes the explicit Autofever sync-disabled guard that current `main` retains.
- `backend/internal/task/service.go`: the archived version removes ChatGPT-log context and MCP tool-call traces present in current `main`.
- `backend/internal/accountfeed/registry_service.go`: the archived version omits current cursor-presence handling and the HTTP-feed-only persistence/clearing rules.
- `backend/internal/source/trello.go`: the archived version removes the quiet-board cursor optimization, which avoids re-fetching a quiet board's full card/comment history.
- `backend/internal/opscontrol/service.go`: the archived version removes the durable control-review repository hook used by the current safety-control flow.
- `idp/internal/app/authentication/auth_handler.go`: the archived version drops request-context propagation for logout, authentication checks, token refresh, and password-reset requests.
- `nginx-config/nginx.conf`: the archived standalone file embeds the fixed value `change-this-local-backend-key`; current Compose instead renders `nginx-config/nginx.conf.template` with an environment-provided secret. Compose does not mount or reference the archived standalone file. Do not port this file.

These are examples of why an old result blob differing from `main` is not evidence that it is a missing improvement. The remaining changed paths still require semantic review.

## Reconciliation attempt and safety

A three-way apply was attempted using an isolated temporary Git index initialized from `origin/main`; the patch had conflicts, including account-feed, agent-runtime, automation, and IDP files, and also contained audit/output paths outside the product-code subset. The attempt exited non-zero. The real checkout index was checked afterward: no staged changes were present. The primary checkout's tracked working-tree state remained 244 modified and 448 deleted files at that check.

No source patch was committed or pushed by this review, and no local source, archive, worktree, database, log, or transcript was removed. No test pass is claimed for these archives.

## Next decision

Continue semantic, module-by-module reconciliation from the archived blobs against current `main`. Only port a change when its intended behavior is still absent, it preserves current safeguards, and its tests pass on a clean branch based on the refreshed `main`. Keep both patch archives until every unique delta is either integrated and remotely verified or explicitly rejected with evidence.
