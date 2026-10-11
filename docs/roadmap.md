# Roadmap & Blocked Items

Honest forward view. Nothing here is claimed done; the completion matrix is the
source of truth for current state.

## Near-term hardening

- **Fresh-clone Windows acceptance (phase 032, TD-8):** the maintained Windows
  Compose installation runs the core Postgres, Redis, nginx, IDP, backend, and
  frontend services by default. Kafka-compatible delivery is an explicit
  `event-bus` profile. Repeat the same acceptance from a clean clone and empty
  volumes before calling installation reproducibility complete.
- **RBAC — done on the backend (phase 008/TD-9):** IDP-JWT identity→role is wired + runtime-proven. Remaining: IDP emits a `role` claim; broaden `requirePermission` onto more routes.
- **Frontend dependency posture (TD-6/BH-7):** the checked-in dependency manifest
  now pins Angular 22.1.1 and ng-zorro 22.0.1. Production dependency auditing is
  blocking in CI; rerun the advisory check for each release rather than treating
  a previous clean report as a current security guarantee.
- Adopt the `apierror` envelope across handlers in step with the frontend (TD-1).
- **Advisory outcome monitor release acceptance:** retain a disposable-PostgreSQL
  and signed-browser run for all three fixed collectors, exact replay after a
  transient composition failure, two-owner isolation, disabled-target behavior,
  active/expired lease fencing, and the Governance Control lifecycle. Assert
  that the path creates only observations, runs, outcome evaluations,
  proactivity decisions, and inbox records, with zero execution, delivery,
  Calendar, workflow, mandate, provider, or learning effects.

## Frontend-dependent (need Angular work)

- Memory search is wired to `/memory/query` with kind/tag filters, sort and
  paging; focused UI/service tests and the integrated suite cover its local
  contracts. Populated-data browser acceptance, feature-flag/i18n surfaces,
  and large-data user-experience validation remain required (TD-7).
- Deeper accessibility + cross-browser visual passes on the existing pages.

## Larger initiatives

- Move list/search from in-memory to SQL with composite/trigram indexes at scale
  (see `docs/performance-baseline.md`).
- Retained live acceptance runs for each implemented, read-only provider connector.
- Add deployed metrics and alerts for durable outcome-monitor sweep latency,
  due backlog, lease recovery, redacted failures, and composition retries only
  after the local acceptance contract is retained. Do not add effect authority
  to solve an observability gap.

## Blocked items

| Item | Blocker | Next action |
| --- | --- | --- |
| DeepSeek Harness native execution | The production bridge entry points remain hard-disabled. The reviewed process-supervision tests do not establish a secure server-to-Windows start protocol, authenticated peer pinning, or an OS-enforced least-privilege sandbox. | Keep execution blocked. Implement and independently review each boundary, then run native Windows integration and operator acceptance before enabling any bridge path. |
| Fresh-clone Windows 11 acceptance | The maintained local stack is proven, but it contains retained volumes and configured local state | Clone into a clean directory, create a new `.env.local`, build empty volumes, and run the documented operator chain |
| Google Drive/Contacts/Calendar live acceptance | Live sandbox credentials and retained evidence; adapters remain unconfigured by default | Run bounded consent, backfill, incremental-change, revoke, and source-link acceptance for each account |
| Paid LLM routing / grounded LLM verification | Paid-budget approval (currently €0); no LLM provider configured | Approve budget / configure a local LLM provider |

Some remaining items need engineering before acceptance can begin; others need
external credentials, operator approval, or a clean environment. Those blocker
types are kept explicit rather than treated as interchangeable or faked.
