# Fresh-Clone Dry Run

Run this from a clean checkout before calling the deployment operational on a
target Windows 11 machine. It distinguishes repeatable build checks from
environment-specific live proof.

## Backend

```powershell
git clone https://github.com/Noodzakelijk-Online/018-HAI.git
cd 018-HAI/backend
go build ./...
go vet ./...
go test ./...
go run ./cmd doctor
```

Expected result: build, vet, tests, and `doctor` succeed on a sane local
configuration. The repository's critical-path smoke also exercises the backend
against a real local Postgres instance.

## Full Local Stack

```powershell
cd 018-HAI
./scripts/initialize-windows.ps1
docker compose --env-file .env.local config --quiet
docker compose --env-file .env.local up --build -d
docker compose --env-file .env.local ps
curl.exe -i http://localhost:8088/
curl.exe -i http://localhost:8088/healthz
curl.exe -i http://localhost:8088/readyz
curl.exe -i http://localhost:8088/api/v1/llm/policy
```

Expected result:

- Backend, frontend, gateway, Postgres, Redis, and their required dependencies
  are running; services that define health checks report healthy. The optional
  Kafka-compatible event bus is not part of this ordinary local run.
- `GET /` returns the Angular dashboard shell.
- `GET /healthz` returns the backend health JSON, and `GET /readyz` returns a
  ready response through the nginx gateway.
- An unauthenticated protected engine route, such as `/api/v1/llm/policy`, is
  rejected with `401` rather than being proxied as an anonymous backend call.

The default Compose file is a compatibility wrapper around
`docker-compose.local.yml`; both commands resolve to this source-built local
stack. The old prebuilt-image stack is no longer a runnable repository entry
point.

The nginx gateway resolves Docker service names per request. Recreating the
frontend or backend must not leave nginx pinned to a prior container IP.

## Status Of This Evidence

The Compose configuration, local topology, dashboard shell, gateway health and
readiness routes, and protected-route rejection were exercised in the current
repository on 2026-07-14. This does not prove a fresh clone on Robert's target
Windows 11 machine, a signed-in browser journey, optional event publishing, or any
third-party provider or account connector. Those checks remain required before
the system is trusted for real operational work.

## Definition Of Pass

1. The backend commands complete successfully from the clean clone.
2. `docker compose ... config --quiet` succeeds and required services become
   healthy after `up --build -d`.
3. The gateway serves the dashboard and proxies `/healthz` and `/readyz` to the
   backend.
4. A user can sign in, create a bounded low-risk workflow, approve it where
   required, and inspect its audit and verification records.
5. No third-party account, paid provider, or runtime is enabled until its own
   scoped readiness and approval evidence has been recorded.
