# Bug Hunt Log

A running log of defects and risks found while working the repository, with
status. Kept honest: unresolved items stay open rather than being silently
closed.

| # | Severity | Area | Finding | Status |
| --- | --- | --- | --- | --- |
| BH-1 | Low (flaky) | `internal/agentruntime` | `TestHermesAdapterInvokesControlledCli` (and the OpenClaw equivalent) timed out under parallel `go test ./...` load — shells to a controlled CLI with a 5s wall-clock timeout and starves. | **Resolved** — CLI-invocation test timeouts raised 5s → 30s; passes repeatedly under full-suite load. |
| BH-2 | Low (repo hygiene) | repo root | `hai-engine-control.zip` (2.2 MB) committed as a binary artifact; opaque to review, bloats clones. | **Resolved** — removed from the repo (`git rm`); no code/config referenced it. |
| BH-3 | Low (CI) | `.github/workflows/ci.yml` | Go version unpinned (floating `1.21.x`) while local toolchain is newer. | **Resolved** — backend, IDP, and nginx-config-manager are aligned to Go 1.25.12 across module, Docker, and CI contracts. |
| BH-6 | High (supply chain) | `backend` deps | The refreshed `govulncheck` database found 40 code-affecting vulnerabilities in Go 1.21.13 plus stale `x/net`, `x/text`, `pgx`, and `go-redis` modules. | **Resolved** — coordinated Go 1.25.12 and module upgrade; vet, full tests, and build pass; `govulncheck` v1.6.0 reports 0 code-affecting findings and is a pinned blocking CI gate. |
| BH-7 | High (supply chain) | `frontend` deps | The prior Angular 16 dependency graph reported 12 high and 1 moderate production advisories. | **Resolved** — the frontend is on Angular 20/ng-zorro 20 and `npm audit --omit=dev --audit-level=high` reported 0 production vulnerabilities on 2026-08-23. The same scan is now a blocking CI gate. |
| BH-8 | High (supply chain / privilege) | `nginx-config-manager` | Go 1.21.13 plus the Docker SDK produced 31 code-affecting findings, and the legacy Compose file mounted the Docker socket for a reload operation disabled in the canonical stack. | **Resolved** — removed Docker SDK and socket mounts, made reload fail closed, aligned Go 1.25.12, added tests/contracts, and reduced the scan to 0 code-affecting findings. |
| BH-4 | Info | `internal/router` | Before this work the API had no global security headers and no rate limiting. | Resolved — `securityHeadersMiddleware` + config-gated `rateLimitMiddleware` added (phases 029/018). |
| BH-5 | Medium (tests) | `frontend` | 7 pre-existing specs failed — stale scaffold assertions (`AppComponent` expected "app is running") and missing test providers (`No provider for NzNotificationService`, no `HttpClientTestingModule`, unresolved DI tokens). | **Resolved** — added `HttpClientTestingModule` to service specs, mocked providers + `NO_ERRORS_SCHEMA` for component specs, corrected the AppComponent assertion. The expanded full suite is now 126/126 green in headless Chrome. |

## How to add an entry

Record: severity, area, a concrete reproduction or observation, and a status
(Open / Resolved / Won't-fix with reason). Never mark Resolved without evidence.
