# Roadmap & Blocked Items

Honest forward view. Nothing here is claimed done; the completion matrix is the
source of truth for current state.

## Near-term hardening

- **Fresh-clone Windows acceptance (phase 032/TD-8):** a separate clean checkout
  on the current Windows host generated new secrets, built empty volumes, became
  healthy, signed in, and completed the bounded operator chain. Repeat that
  release gate on every distinct target machine; it is no longer an
  implementation gap in this repository.
- **RBAC — implemented (phase 008/TD-9):** the IDP persists and signs
  owner/operator/viewer roles, the backend trusts only verified JWT claims, and
  read/write/approve/execute/admin guards cover the protected route groups.
  Continue adding route-specific permission regression cases with new APIs.
- **Frontend dependency hardening (TD-6/BH-7) completed:** Angular 22.1.1,
  ng-zorro 22.0.1, TypeScript 6.0.3, and the supported esbuild/Vite builder are
  in place; the 379-test suite and production build pass; high/critical audit
  findings are zero and blocking. Recheck the documented moderate CLI-only
  exception by 2026-09-09 or when Angular CLI adopts MCP SDK 1.30+.
- Adopt the `apierror` envelope across handlers in step with the frontend (TD-1).
- **Advisory outcome monitor release acceptance:** retain a disposable-PostgreSQL
  and signed-browser run for all three fixed collectors, exact replay after a
  transient composition failure, two-owner isolation, disabled-target behavior,
  active/expired lease fencing, and the Governance Control lifecycle. Assert
  that the path creates only observations, runs, outcome evaluations,
  proactivity decisions, and inbox records, with zero execution, delivery,
  Calendar, workflow, mandate, provider, or learning effects.
  The three fixed collectors plus repository/composition lifecycles are now
  mandatory in CI through a dedicated disposable database. The integrated
  release test covers a due run, one exact transient composition retry without
  recollection, pause/re-enable, active and expired lease recovery, two-owner
  isolation, exact no-op replay, and a whole-schema assertion that only the
  advisory monitor, outcome-evaluation, and proactivity ledgers changed. A
  retained signed-browser not-due pass separately proves truthful HTTP/UI no-op
  reporting and no duplicate ledgers. The signed due-run, pause/re-enable, and
  expired-lease recovery lifecycle also passed through the Windows gateway and
  Governance Control on 2026-08-14. Its isolated database reconciliation found
  one observation/run/handoff chain and zero workflow, automation, provider, or
  execution-authority records. Repeat both signed checks on each distinct
  release target; see
  `docs/evidence/ambient-monitor-windows-acceptance-2026-08-14.md`.

## Frontend follow-up

- Extend feature-flag/i18n surfaces across the dashboard (remaining TD-7 scope).
- Deeper accessibility + cross-browser visual passes on the existing pages.

## Larger initiatives

- Retain production-scale `EXPLAIN (ANALYZE, BUFFERS)`, latency percentiles, and
  resource measurements for the owner-scoped PostgreSQL memory query against a
  representative release-target dataset (see `docs/performance-baseline.md`).
- Retained live acceptance runs for each implemented, read-only provider connector.
- Retain local scrape acceptance for the implemented tenant-free durable
  outcome-monitor metrics. Configure collector-side retention and alert rules
  per deployment; HAI deliberately does not embed a second monitoring service
  or add effect authority to solve an observability gap.

## Blocked items

| Item | Blocker | Next action |
| --- | --- | --- |
| Release-target Windows 11 acceptance | Clean-clone acceptance is retained for the current host, but hardware, Docker Desktop, ports, and local policy vary by target | Run the documented clean-clone operator chain on each distinct release target and retain the result |
| Google Drive/Contacts/Calendar live acceptance | Live sandbox credentials and retained evidence; adapters remain unconfigured by default | Run bounded consent, backfill, incremental-change, revoke, and source-link acceptance for each account |
| Paid LLM routing / grounded LLM verification | Paid-budget approval (currently €0); no LLM provider configured | Approve budget / configure a local LLM provider |

Blocked items are blocked by external credentials/approvals, not by engineering
difficulty, and are documented rather than faked.
