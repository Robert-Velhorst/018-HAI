# Technical Debt Register

Tracked debt with severity and a concrete completion condition. It complements
the engineering action register and bug-hunt log.

| ID | Severity | Debt | Done when |
| --- | --- | --- | --- |
| TD-1 | Medium | Partial utility adoption: `apierror` is used for RBAC 403 responses and file safety is enforced in the live upload path, but not every handler uses the shared error envelope or filesystem helper. | Handlers migrate with their frontend consumers and file I/O uses the shared safety helpers. |
| TD-2 | Medium | List and search operations are memory-backed and are not suitable for very large datasets. | SQL-backed search uses appropriate composite and trigram indexes. |
| TD-3 | Low | Go toolchain pinning. | Resolved: CI pins Go `1.21.13`. |
| TD-4 | Low | Historical binary artifact in the repository. | Resolved: the unused `hai-engine-control.zip` was removed. |
| TD-5 | Low | Agent runtime CLI test flakiness under parallel load. | Resolved: timeouts were raised to 30 seconds and repeated runs pass. |
| TD-6 | High | Prior frontend dependency advisories in the Angular 16 family. | Resolved: Angular 20/ng-zorro 20 now reports 0 production vulnerabilities on `npm audit --omit=dev --audit-level=high`; the scan is a blocking CI gate. |
| TD-7 | Info | i18n catalog and feature flags are backend-only. | The Angular dashboard consumes `/flags` and translated messages. |
| TD-8 | Medium | The local Compose topology and gateway contract have been exercised, but a clean-machine Windows 11 fresh-clone run, signed-in browser journey, and Kafka event-publishing proof are still outstanding. | A fresh clone reaches `/readyz` through nginx, the intended signed-in workflow succeeds, and the required event path is observed on the target machine. |
| TD-9 | Low | RBAC is not fully driven by per-user role issuance. The backend verifies an IDP JWT and maps a role claim, but the IDP still needs to issue role claims and sensitive routes need broader permission enforcement. | Role claims are issued by the IDP and permission checks cover the remaining ownership-sensitive routes. |

## Rules

- Every new debt entry names a concrete completion condition.
- Debt is paid down or explicitly reprioritized during maintenance cycles.
