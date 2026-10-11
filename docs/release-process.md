# Release Process: Canary & Rollback

018-HAI runs local-first via Docker Compose. This defines how to ship a change
safely and roll back cleanly.

## Pre-release gates

1. `go build ./...` and `go test ./...` are green (backend).
2. `backend doctor` reports no failures for the target environment.
3. Frontend `ng build` succeeds.
4. Completion matrix and worklog updated for anything user-facing.

## Versioning

Semantic tags (`vMAJOR.MINOR.PATCH`). Tag only from a green build. The current
baseline tag is `v1.0.0`.

## Canary

The canonical Compose file intentionally reserves fixed container, network, and
volume names so the Windows installer can identify one authoritative local
stack. It therefore cannot run a second independent HAI stack beside the live
one on the same Docker Desktop engine. Do not attempt a same-host
`docker compose -p canary` launch: it can collide with the running stack or its
data volumes.

Run a canary in an **isolated Docker context or host** with its own copied
configuration and empty disposable volumes:

1. Generate a separate ignored environment file with
   `scripts/initialize-windows.ps1` in the canary checkout. Never copy a live
   environment file or data volume into the canary.
2. Bring up the canary using the canonical Compose command on that isolated
   context or host.
3. Probe the canary: `GET /healthz` (liveness) then `GET /readyz` (readiness).
   Do not proceed while `/readyz` returns 503.
4. Smoke the critical path: create a memory, run a workflow item through an
   approval gate, and confirm an audit event.
5. Watch logs for redaction failures or unexpected 5xx for a soak window.

## Single-host update

For the normal one-stack Windows installation, take a verified backup, run the
pre-release checks against the candidate source, then update the existing stack
in place. This is a rolling update, not a canary:

1. From the approved checkout, stop the stack with
   `docker compose --env-file .env.local -f docker-compose.local.yml down`.
2. Start the candidate from the same checkout with
   `docker compose --env-file .env.local -f docker-compose.local.yml up -d --build`.
   The installed Windows application instead uses its Start Menu **Stop HAI**
   and **Start HAI** entries; see `docs/windows-installer.md`.
3. Confirm `/healthz`, `/readyz`, login, and the bounded governed-workflow
   smoke described above before resuming operational work.
4. Roll back immediately if any check fails.

## Promote

Once the isolated canary is healthy, update the production host using the
single-host update procedure. Retire the isolated canary after preserving its
test evidence.

## Rollback

1. Re-deploy the previous image tag (compose `up -d` with the prior tag).
2. If a database migration shipped, apply its down-migration or restore from the
   most recent backup (see `docs/backup-restore.md`). Never leave the schema
   ahead of the running binary.
3. Confirm `/readyz` is green post-rollback.

## Migration safety

- Ship migration files (not only Gorm `AutoMigrate`) so changes are reviewable
  and reversible.
- Additive migrations first; destructive changes only after the new code is
  proven in canary.
