# Dependency Vulnerabilities — Status & Next Action

Records the actual `govulncheck` result, the CI scanning posture, and the exact
remediation, so nothing is hand-waved.

## 2026-08-14 Go security patch

GitHub's blocking scans detected new standard-library advisories in Go 1.25.12.
All four Go modules now recommend Go 1.25.13; the backend, IDP, and nginx
configuration manager builders use the digest-pinned official 1.25.13 image,
and every matching CI job uses 1.25.13. Fresh vet, full-test, build, and
`govulncheck` v1.6.0 runs report zero code-affecting vulnerabilities in all
three networked services. The scanner still reports unreachable advisories in
imported or required packages; they remain visible but do not call vulnerable
symbols. Go's official download feed lists 1.25.13 as a stable release:
<https://go.dev/dl/?mode=json>.

## Backend remediation completed (40 to 0)

The 2026-07-30 scan first found 40 code-affecting vulnerabilities under Go
1.21.13. The backend was then moved as one coordinated change to Go 1.25.12 and
the affected module family was upgraded:

- Go toolchain and Docker/CI builder: **1.25.12**
- `golang.org/x/net`: **v0.56.0**
- `golang.org/x/text`: **v0.39.0**
- `github.com/jackc/pgx/v5`: **v5.9.2**
- `github.com/redis/go-redis/v9`: **v9.6.3**

Go 1.25 vet identified one dynamic `fmt.Errorf` call; it was changed to
`errors.New`. Full vet, tests, and build then passed.

## Backend (`govulncheck ./...`)

`govulncheck` v1.6.0 using the 2026-07-27 vulnerability database and Go
1.25.12 reports:

- **0** vulnerabilities affecting code
- **0** vulnerabilities in imported packages
- 3 advisories in required modules whose vulnerable symbols are not called

The backend scan is pinned and blocking in CI.

## IDP (`govulncheck ./...`)

The same scanner, database snapshot, and Go 1.25.12 toolchain report 0
vulnerabilities affecting IDP code and 0 in imported packages. Two advisories
remain in required modules whose vulnerable symbols are not called. The IDP
scan is also pinned and blocking in CI.

## Nginx configuration manager (`govulncheck ./...`)

The first 2026-07-30 scan found 31 code-affecting vulnerabilities under Go
1.21.13, including findings in the obsolete Docker SDK reload path. HAI removed
the Docker SDK and all Compose Docker-socket mounts, upgraded Sarama, and moved
the service to Go 1.25.12. Enabling automatic reload now fails closed and tells
the operator to use an approved deployment operation.

Tests, vet, and build pass. The refreshed scan reports 0 vulnerabilities
affecting code and 0 in imported packages. One advisory remains in a required
module whose vulnerable symbol is not called. The scan is pinned and blocking
in CI.

## Frontend (`npm audit --audit-level=high`)

The coordinated 2026-08-09 migration replaced Angular 16 and the legacy webpack
builder with Angular 22.1.1, `@angular/build`/CLI 22.1.3, ng-zorro 22.0.1,
TypeScript 6.0.3, Zone.js 0.16.2, and the supported esbuild/Vite application
builder. The unused drag/drop package and duplicate pnpm lock/workspace files
were removed; npm 10.9.8 and `package-lock.json` are now authoritative.

The migration reduced the audit from **91 findings (4 critical, 59 high, 19
moderate, 9 low)** to **3 moderate, 0 high, and 0 critical**. The complete
418-test frontend suite and production build pass. The initial production bundle
is approximately 626 kB, down from approximately 1.40 MB under the old builder.

The three remaining records describe one development-tool chain:

`@angular/cli@22.1.3` -> `@modelcontextprotocol/sdk@1.29.0` ->
`@hono/node-server<2.0.5` ([GHSA-frvp-7c67-39w9](https://github.com/advisories/GHSA-frvp-7c67-39w9)).

This code is used by the local Angular CLI and is not copied into the nginx
runtime image. Angular CLI 22.1.3 pins MCP SDK 1.29.0; npm's proposed automatic
fix is a breaking downgrade to Angular CLI 21.0.4. Forcing Hono 2.0.5 would also
violate MCP SDK 1.29.0's declared dependency range, so neither workaround is an
accepted production remediation without upstream compatibility evidence.

The repository maintainer accepts this **moderate, development-only** exposure
until the earlier of an upstream Angular CLI release that adopts MCP SDK 1.30+
or **2026-09-09**, when it must be reviewed again. The CLI development server
must remain loopback-only and must not be used as the deployed application
server. The production nginx image does not contain Angular CLI, MCP SDK, or
Hono.

## Frontend CI gate

The frontend audit is now a blocking high/critical gate. CI installs from the
lockfile, builds, runs all headless-browser unit tests, and then executes `npm
audit --audit-level=high` without `continue-on-error`. A high or critical
finding fails the job. Moderate findings remain visible and are governed by the
time-bounded exception above.

## Current CI posture

- Backend: vet, build, test, race checks, and pinned `govulncheck` are **hard
  gates**.
- IDP: vet, build, test, and pinned `govulncheck` are **hard gates**.
- Nginx configuration manager: vet, build, test, and pinned `govulncheck` are
  **hard gates**; Docker-socket control is forbidden by an executable contract.
- Frontend: lockfile install, production build, **unit tests (headless Chrome)**,
  and `npm audit --audit-level=high` are hard gates.
