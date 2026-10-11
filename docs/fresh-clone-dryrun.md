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
powershell -ExecutionPolicy Bypass -File .\scripts\initialize-windows.ps1
docker compose config --quiet
docker compose up --build -d
docker compose ps
curl.exe -i http://localhost/
curl.exe -i http://localhost/healthz
curl.exe -i http://localhost/readyz
curl.exe -i http://localhost/api/v1/llm/policy
```

The initializer prompts for the first-run owner email and a password of at
least 12 characters. It generates independent random backend, memory, JWT,
approval-proof, database-owner, and database-runtime secrets, keeps the gateway
on loopback, disables local-login bypass, and enables the planning-only local
A2A connector with a separate token bound to that owner. Public connector and
ngrok exposure remain disabled. The initializer does not print credentials.
Copying `.env.example` without running the initializer is expected to fail
closed in production because its shipped values are placeholders.

The root `docker-compose.yml` contains no second service topology. It includes
`docker-compose.local.yml` with `.env.local`, so the standard Compose command
and the explicit local-file command resolve to the same services and local
source builds.

Expected result:

- Backend, frontend, gateway, Postgres, Redis, Kafka, and their required
  dependencies are running; services that define health checks report healthy.
- `GET /` returns the Angular dashboard shell.
- `GET /healthz` returns the backend health JSON, and `GET /readyz` returns a
  ready response through the nginx gateway.
- An unauthenticated protected engine route, such as `/api/v1/llm/policy`, is
  rejected with `401` rather than being proxied as an anonymous backend call.

The nginx gateway resolves Docker service names per request. Recreating the
frontend or backend must not leave nginx pinned to a prior container IP.
The legacy `generic-auto` endpoint is not part of the default boot; it is
available only through the explicit `compatibility` profile.

## Status Of This Evidence

On 2026-08-09, this sequence was exercised from a separate clean checkout on
the current Windows host using the Windows initializer. All 11 Compose services
became healthy. The gateway served the dashboard, health and readiness passed,
the first-run owner could sign in, protected routes required that session, and
a pursuit candidate could be accepted into a workflow. A bounded read-only API
runtime regression also proves that deterministic local execution can complete
without an LLM or approval, while destructive execution remains blocked before
the executor. The run uncovered and fixed an ambiguity where infrastructure
"health" was incorrectly classified as personal health; genuine personal or
clinical requests still retain the stricter care-evidence contract.

An authenticated live task run then selected an existing read-only API
automation for the backend readiness endpoint. It skipped connected-source
refresh and search, classified the work as `automation`, found resource
feasibility without consuming owner capacity, verified all 24 applicable
pre-authorization assertions, executed the configured API target, produced a
durable launch record, returned `test_passed`, and persisted the task as
`validated` without selecting a model or allowing paid usage. A complementary
personal-medication request selected the health domain, required
`health_admin_assistant`, recorded that specialist as `requires_assignment`,
and remained `review_required` with no tool execution.

This evidence does not prove Robert's separate target Windows installation,
event delivery outside the local stack, or any third-party provider/account
connector. Those checks remain provider- and environment-specific release
gates before HAI is trusted for real operational work.

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
