# UI Action Audit

Reviews the dashboard's user-facing actions to confirm each is wired to a real
backend endpoint (no dead buttons). Based on `frontend/src/app/pages` +
`services` mapped to `internal/router/routes.go`.

## Pages → backing endpoints

| Page | Primary actions | Backing endpoint(s) |
| --- | --- | --- |
| command-dashboard | overview, next actions | `/os/overview`, `/workflow/*` |
| memory | list/search/create/edit/archive/export | `/memory`, **`/memory/query`**, `/memory/export` |
| connected-sources | list/create/sync/pause/revoke | `/sources/*` |
| workflow-engine | list/approve/transition | `/workflow/*` |
| task-blueprint | plan/run | `/task/*` |
| llm-policy | policy/probes | `/llm/policy`, `/llm/probes` |
| grounded-answers | answer/verify | `/verification/*` |
| pursuits | list/brief/decisions | `/pursuits/*` |
| ambient-brain | scan/needs | `/ambient/*` |
| control-center | diagnostics/emergency stop | `/os/overview`, `/autonomy/*` |
| memory (engine) | import/search/insights | `/memory-engine/*` |
| login | authenticate | `idp` / API key |

## Findings

- Each audited page maps to real, existing endpoints — no purely-decorative
  actions found in the reviewed set.
- **Not yet surfaced in the UI (backend exists, UI wiring pending):**
  the new `/memory/query` search parameters, `/flags`, `/system/info` readiness,
  and i18n EN/NL strings. Tracked in `docs/technical-debt.md` (TD-7).
- **Implemented:** the authenticated System Status page consumes
  `/api/v1/system/readiness`; the public `/readyz` endpoint remains limited to
  aggregate orchestrator status.

## Method note

This is a static route-mapping audit. A live click-through audit against a running
stack is the complementary manual step (pending the compose smoke automation).
