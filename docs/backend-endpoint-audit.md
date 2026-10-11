# Backend Endpoint Usage Audit

Enumerates the real HTTP surface (from `internal/router/routes.go`) and confirms
each route maps to a handler. Unauthenticated probes are intentional; everything
under `/api/v1` requires `X-HAI-Backend-Key` when configured.

## Unauthenticated

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/healthz` | liveness |
| GET | `/readyz` | aggregate readiness only; 200/503 without internal check detail |
| GET | `/swagger/*` | API docs |

## Authenticated (`/api/v1`)

| Group | Representative routes |
| --- | --- |
| `/automation` | verified-operator shared local registry: GET `/`, GET `/:id`, POST `/`, PATCH `/`, DELETE `/:id`, POST `/:id/launch`, health/diagnostics, `/images/:imageName` |
| `/agent-runtimes` | owner-gated registry/health/skills, runtime stop, and OpenClaw ecosystem set/refresh/upload |
| `/llm` | owner-gated GET `/policy`, `/probes`, `/probes/history`, `/logs`; POST `/route`, `/generate` |
| `/memory` | owner-gated GET `/`, **GET `/query`**, GET `/:id`, `/export`; POST `/`, `/retrieve`, `/:id/archive|restore`; PATCH/DELETE `/:id` |
| `/memory-engine` | owner-gated import, dashboard, search, conversations, insights |
| `/sources` | owner-gated connectors, list/create, search, sync-due, sync-jobs, extractions, audit-logs, per-source sync/pause/resume/revoke |
| `/workflow` | overview, approvals, dashboard, list, intake, owner-scoped exact `/:id/run`, run-due/recovery/follow-up controls, transitions, approval/interruption/proposal resolve, checklist |
| `/pursuits` | list/create, dashboard, brief, decisions, per-pursuit evidence/activity/next-actions/blockers/approvals, read-only VA delegation package, governed portfolio workflow authorization/creation/verified settlement |
| `/verification` | owner-gated POST `/answer`, GET `/runs`, `/runs/:id` |
| `/task` | owner-gated plan, run, success, logs, review-queue, review resolution |
| `/assistant`, `/agent-cycle`, `/autonomy`, `/ambient`, `/os` | owner-gated command/logs, run, overview/stress, scan/needs/proposal resolution, overview |
| **`/flags`** | GET — feature flags (added this goal run) |
| **`/system`** | **GET `/info`**, **GET `/readiness`** (live detailed checks), **GET `/support-bundle`** |

## Findings

- Every registered route resolves to a handler (verified by the router smoke test
  and `go build`).
- New reachable surfaces added this run: `/memory/query`, `/flags`, `/system/info`,
  `/system/support-bundle`, plus `/readyz`.
- No orphaned/dead routes found in `routes.go`.
- Workflow browser/API routes require a verified owner. Their worker controls
  operate only on that owner's work; global workflow scheduling remains an
  in-process system-worker operation rather than a dashboard capability.
- `POST /workflow/:id/run` atomically claims only the named owner-scoped
  workflow. It runs only when the item is due, ready, and approved where
  required; emergency stop, task/runtime authorization, verification, audit,
  retry, and review handling remain enforced by the existing worker path.
- `POST /pursuits/portfolio-execution-proposal-items/:itemId/settle-workflow`
  is a separate owner/execute-capability command. It revalidates the immutable
  item, original approval, receipt and exact consumption, single pursuit link,
  completed workflow projection, and immutable `verified`/`test_passed`
  completion attestation before atomically appending measured usage and its
  portfolio settlement proof. It cannot rerun work or grant authority.
- A manual source `sync-due` request requires a verified owner and refreshes only that owner's explicitly owned sources. The global source scheduler remains in-process.
- Task HTTP requests require a verified owner for planning, controlled execution,
  history, review visibility, and review resolution. The task service's
  ownerless/system methods remain in-process worker APIs and are not HTTP fallbacks.
- Ambient proposal acceptance and dismissal require a verified owner before the
  service may create workflow work. Ownerless ambient resolution remains an
  in-process system-worker API only.
- Runtime task-stop and OpenClaw ecosystem mutation endpoints require a verified
  owner before reaching a runtime adapter or filesystem operation. Read-only
  runtime inventory, health, and skill discovery remain available to the
  authenticated gateway surface.
- `GET /pursuits/:id/delegation` is owner-scoped through pursuit detail access.
  It compiles only already VA-ready workflow context, checklists, source links,
  delivery expectations, and escalation rules. It creates no assignment, does
  not grant external authority, and cannot execute or send anything.
- Pursuit intake, planning, candidate acceptance, decision resolution, and summary refresh execute
  through owner-scoped service paths. Those paths filter legacy cross-owner
  links before deriving evidence, creating workflow follow-ups, or changing a
  pursuit's summary or completion state.
- Auto-created pursuit candidates cannot use generic planning or intake. Only
  `POST /pursuits/:id/candidate/accept` may activate one, and that route plus
  its handler require approval capability before it can create or unlock
  governed work.
- Authenticated Pursuit, Source, Memory, conversation-archive, semantic-search,
  and Verification reads expose ownerless pre-identity data only to the single
  `HAI_LEGACY_DATA_OWNER_IDENTITY`. Unset configuration fails closed and every
  other operator remains exact-owner isolated. Authenticated mutation routes
  reject ownerless legacy records; empty-owner in-process workers retain the
  only supported path for controlled legacy maintenance.
- **Follow-up:** adopt the `apierror` envelope uniformly across handlers (error
  shapes currently vary; frontend depends on the existing shapes — migrate both
  together).

**Authorization correction (2026-07-14):** HAI engine APIs are gateway-session
protected and use the verified token's read/write/approve/admin permission.
Direct backend clients also require the configured backend key. Request headers
never grant a role.
