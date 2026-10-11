# Supply Chain & Dependency Review

## Backend (Go)

Module `automation-hub-backend` declares Go 1.25 and recommends the exact
Go 1.25.12 toolchain used by its Docker builder and CI jobs.
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

Angular 20 + ng-zorro-antd 20, pinned via `package-lock.json`.

## Gaps & actions

| Item | Action |
| --- | --- |
| Vulnerability scanning | Backend, IDP, and nginx manager `govulncheck` v1.6.0 scans are pinned, clean, and blocking after 40-to-0 and 31-to-0 remediations. Frontend `npm audit --omit=dev --audit-level=high` is also a blocking CI gate and reported 0 production vulnerabilities on 2026-08-23; see `docs/dependency-vulnerabilities.md`. |
| Go version alignment | All three Go modules, Docker builders, and CI jobs are pinned to Go 1.25.12 and checked by `scripts/test_ci_contract.py`. |
| Committed binary `hai-engine-control.zip` | Remove from VCS; move to release assets. |
| Dependency freshness | Adopt a scheduled `go list -m -u all` / `npm outdated` review (register #95). |

## Policy

- No new direct dependency without a stated purpose and a license check
  (see `docs/third-party-licenses.md`).
- Prefer the standard library; several internal utilities (rate limiting,
  idempotency, path safety, feature flags) are dependency-free by design.
