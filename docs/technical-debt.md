# Technical Debt Register

Tracked debt with severity and a concrete completion condition. It complements
the engineering action register and bug-hunt log.

| ID | Severity | Debt | Done when |
| --- | --- | --- | --- |
| TD-1 | Medium | Partial utility adoption: `apierror` is used for RBAC 403 responses and file safety is enforced in the live upload path, but not every handler uses the shared error envelope or filesystem helper. | Handlers migrate with their frontend consumers and file I/O uses the shared safety helpers. |
| TD-2 | Medium | List and search operations are memory-backed and are not suitable for very large datasets. | SQL-backed search uses appropriate composite and trigram indexes. |
| TD-3 | Low | Go toolchain pinning. | Resolved: modules, digest-pinned Docker builders, and CI pin Go `1.25.13`. |
| TD-4 | Low | Historical binary artifact in the repository. | Resolved: the unused `hai-engine-control.zip` was removed. |
| TD-5 | Low | Agent runtime CLI test flakiness under parallel load. | Resolved: timeouts were raised to 30 seconds and repeated runs pass. |
| TD-6 | Moderate | Resolved at high/critical severity: the Angular 22 migration, 418-test regression pass, production build, and blocking audit gate leave 0 high/critical findings. Three moderate findings remain in the Angular CLI-only MCP/Hono development chain. | Remove the time-bounded exception when upstream Angular CLI adopts MCP SDK 1.30+; review no later than 2026-09-09. Keep the CLI server loopback-only and out of the nginx runtime image. |
| TD-7 | Info | i18n catalog and feature flags are backend-only. | The Angular dashboard consumes `/flags` and translated messages. |
| TD-8 | Low | Resolved for the current host: a separate clean checkout generated fresh secrets, built empty volumes, reached `/readyz`, signed in, and completed a bounded governed workflow. Release-target variation remains an operator gate rather than repository debt. | Re-run and retain the same chain on every distinct release target. |
| TD-9 | Low | Resolved: the IDP persists and signs owner/operator/viewer roles; the backend trusts verified claims and applies read/write/approve/execute/admin permission guards across protected route groups. | Keep role-matrix and route-specific permission regression tests mandatory for new APIs. |

## Rules

- Every new debt entry names a concrete completion condition.
- Debt is paid down or explicitly reprioritized during maintenance cycles.
