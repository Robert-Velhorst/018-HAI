# Supply Chain & Dependency Review

## Backend (Go)

Module `automation-hub-backend` declares Go 1.25 and recommends the exact
Go 1.25.13 toolchain used by its digest-pinned Docker builder and CI jobs.
Direct dependencies of note:

| Dependency | Purpose | Notes |
| --- | --- | --- |
| `gin-gonic/gin` | HTTP framework | widely used, active |
| `gorm.io/gorm` + `driver/postgres` | ORM / Postgres | active |
| `IBM/sarama` | Kafka client | active |
| `google/uuid` | IDs | stable |
| `swaggo/*` | Swagger docs | dev/doc only |

All third-party code is pinned via `go.sum` (checksums enforced by the Go
toolchain).

## Frontend (Angular)

Angular 22.1.1 + ng-zorro-antd 22.0.1 on Node 22.22.3 and npm 10.9.8,
pinned via the sole authoritative `package-lock.json`. The application uses the
supported `@angular/build` esbuild/Vite builder; pnpm metadata and the unused
drag/drop dependency were removed.

## Gaps & actions

| Item | Action |
| --- | --- |
| Vulnerability scanning | Backend, IDP, and nginx manager `govulncheck` v1.6.0 scans are pinned, clean, and blocking after 40-to-0 and 31-to-0 remediations. Frontend `npm audit --audit-level=high` is also blocking after the Angular 22 migration reduced 91 findings to 3 moderate development-tool findings and 0 high/critical; see `docs/dependency-vulnerabilities.md`. |
| Go version alignment | All three Go modules, digest-pinned Docker builders, and CI jobs are pinned to Go 1.25.13 and checked by `scripts/test_ci_contract.py`. |
| Committed binary `hai-engine-control.zip` | Remove from VCS; move to release assets. |
| Dependency freshness | Adopt a scheduled `go list -m -u all` / `npm outdated` review (register #95). |

## Policy

- No new direct dependency without a stated purpose and a license check
  (see `docs/third-party-licenses.md`).
- Prefer the standard library; several internal utilities (rate limiting,
  idempotency, path safety, feature flags) are dependency-free by design.
