# idp-automations-hub

The IDP exposes two container-local probes:

- `GET /healthz` is liveness only. It confirms the HTTP process is responding.
- `GET /readyz` checks the identity database and Redis session-revocation store
  with a bounded request context. It returns `200` only when both dependencies
  respond, otherwise `503` with no internal connection details. Readiness also
  checks the durable session-authority table/column contract, not just database
  reachability.

The local Compose healthcheck uses `/readyz`; a dependency outage therefore
marks the IDP unready without changing the liveness result or restarting it.
This is distinct from the gateway's `/readyz`, which reports backend readiness.

## Durable Session Authority

PostgreSQL owns device-session families, their current refresh generation,
revocation and encrypted canonical refresh-rotation receipts. Redis remains a
required, atomically updated revocation/session projection; it is not allowed to
recreate missing PostgreSQL authority from old cache entries or a signed JWT.
Both stores must be available. Role checks use the current account role and
reject stale privileged claims.

Refresh rotation commits one canonical pair before updating Redis. An eligible
concurrent replay may recover that same encrypted pair within the absolute
five-second database deadline, not create another child or extend that deadline.
Replays after eligibility expires revoke the device family before returning an
application error. Password/session-version changes invalidate all older user
sessions; ordinary logout revokes one device family.

### Upgrade and Recovery Boundaries

- Startup applies the additive session-authority schema with bounded locks and
  rejects detected incompatible columns/required constraints. It does not adopt
  old Redis-only families. Existing sessions therefore require signing in again
  after this change is deliberately deployed. No existing installation is
  migrated merely by editing this checkout.
- Encrypted rotation responses are credentials. Do not export them, emit them
  in SQL logs, or show them in dashboards or diagnostics.
- Restoring an older Redis snapshot cannot override current PostgreSQL authority.
  Restoring an older **PostgreSQL** snapshot can itself restore revoked authority.
  Production recovery still requires a separately reviewed session-invalidation
  fence outside the restored snapshot. Do not describe this as complete rollback
  or process-crash/fsync protection.
- `durable_session_postgres_redis_test.go` requires explicitly opted-in disposable
  PostgreSQL (`hai_idp_auth_test`) and marked Redis fixtures. Skipped fixture
  tests are not real-store acceptance. See the current verification ledger for
  actual executed results, not the mere presence of these tests.
