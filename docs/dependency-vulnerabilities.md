# Dependency Vulnerabilities — Status & Next Action

Records the actual `govulncheck` result, the CI scanning posture, and the exact
remediation, so nothing is hand-waved.

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

## Frontend (`npm audit --omit=dev --audit-level=high`)

The Angular 20/ng-zorro 20 production dependency graph reported **0
vulnerabilities** when scanned on 2026-08-23. The prior Angular 16 advisory
state is retained in the bug-hunt log as historical context, not as the current
release posture.

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
