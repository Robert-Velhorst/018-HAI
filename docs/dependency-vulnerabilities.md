# Dependency Vulnerabilities — Historical Status & Next Action

This document records historical scan results and the intended CI posture. The
dated results below are not current release clearance; use checks from the
candidate commit being reviewed as the authoritative result.

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
1.25.12 reported at that time:

- **0** vulnerabilities affecting code
- **0** vulnerabilities in imported packages
- 3 advisories in required modules whose vulnerable symbols are not called

The backend scan is pinned and blocking in CI.

## IDP (`govulncheck ./...`)

The same scanner, database snapshot, and Go 1.25.12 toolchain reported at that
time zero vulnerabilities affecting IDP code and zero in imported packages.
Two advisories remained in required modules whose vulnerable symbols were not
called. The IDP scan is also pinned and blocking in CI.

## Nginx configuration manager (`govulncheck ./...`)

The first 2026-07-30 scan found 31 code-affecting vulnerabilities under Go
1.21.13, including findings in the obsolete Docker SDK reload path. HAI removed
the Docker SDK and all Compose Docker-socket mounts, upgraded Sarama, and moved
the service to Go 1.25.12. Enabling automatic reload now fails closed and tells
the operator to use an approved deployment operation.

Tests, vet, and build passed at that time. The refreshed scan reported zero
vulnerabilities affecting code and zero in imported packages. One advisory
remained in a required module whose vulnerable symbol was not called. The scan
is pinned and blocking in CI.

## Current verification snapshot (2026-10-11)

- Backend, IDP, generic-auto, and Nginx configuration manager Go tests passed
  under Go 1.27.2. This is test evidence, not a fresh `govulncheck` result.
- The frontend production dependency audit passed with zero vulnerabilities.
- The full frontend dependency audit reported six high-severity findings, all
  in the development/test dependency graph (`@angular/build`, Karma, and related
  packages). npm's suggested fixes downgrade Angular/Karma across major
  versions; no such downgrade was applied. Reassess these findings with a
  compatible upstream fix before release.
- The production frontend build passed and all 176 Node-based checks passed.
  Angular browser specs compiled but could not execute because the verification
  container has no Chrome binary.

The 2026-08-23 Angular 20/ng-zorro 20 production audit and earlier Go scans
below are historical results, not current release clearance.

The production-only audit is a blocking CI gate. A future high-severity runtime
advisory therefore blocks release until it is remediated or accepted through a
time-bounded security decision with an owner and scope.

## Current CI posture

- Backend: vet, build, test, race checks, and pinned `govulncheck` are **hard
  gates**.
- IDP: vet, build, test, and pinned `govulncheck` are **hard gates**.
- Nginx configuration manager: vet, build, test, and pinned `govulncheck` are
  **hard gates**; Docker-socket control is forbidden by an executable contract.
- Frontend: build + **unit tests (headless Chrome)** +
  `npm audit --omit=dev --audit-level=high` are hard gates.
