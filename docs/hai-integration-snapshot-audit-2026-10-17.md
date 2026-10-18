# HAI Integration Snapshot Audit - 2026-10-17

> Historical audit record. Counts and repository references describe the
> dated observations below, not current device or release status. Local
> paths and transcript identifiers use public aliases; private source
> evidence remains retained. This export does not authorize deletion.

## Scope

Compared the standalone snapshot at `<local-path-003>` with repository `Robert-Velhorst/018-HAI` at `main` commit `80fdeabd16efda6d9328fa0c89a4ab813f087908`.

The snapshot contains 41,167 files totaling 320,725,853 bytes. The source comparison covered 2,802 code, configuration, dependency-manifest, and documentation files. It excluded local `.env*` files, `node_modules`, `.git`, Playwright state, build outputs, coverage, model directories, source maps, and logs. No environment-file contents were read or copied.

## Comparison result

- 35 files are exact path-and-content matches with `main`.
- 2,764 files exist at paths on `main` but have different content.
- Three source paths are absent from `main`.
- This is a structural comparison, not a semantic review of every changed blob. Different content in an older snapshot is not evidence of a missing improvement and must not be overlaid wholesale.

The three absent paths are:

- `backend/migrations/post/0004_operations_owner_workspace_dedupe_active.up.sql`
- `backend/migrations/post/0004_operations_owner_workspace_dedupe_active.down.sql`
- `nginx-config/nginx.conf`

The two migration files define an active-only unique index on owner, workspace, and dedupe key. Current `main` already carries the same index behavior as `backend/migrations/post/0006_operations_owner_workspace_dedupe_active.{up,down}.sql`; the snapshot's lower migration number reflects its older migration sequence. No duplicate migration should be added.

The standalone `nginx-config/nginx.conf` hardcodes `change-this-local-backend-key` for authenticated proxy routes. It is not present in current Compose wiring. Current `main` uses `nginx-config/nginx.conf.template` and environment-provided secrets, so the snapshot file must not be ported.

## Disposition and remaining work

No product source change from this snapshot was established as missing and safe to port in this pass. The 2,764 changed existing paths remain unclassified semantically; this report does not claim that all their behavior has been reviewed.

The snapshot's `frontend/node_modules` accounts for about 291,495,463 bytes and is generated dependency state, not source to commit. It has not been removed; first confirm no process or workflow still consumes it and finish reviewing the snapshot's source deltas. The three local environment files were excluded and remain preserved.

No snapshot files were deleted. No tests or live integration checks were run for the snapshot comparison.
