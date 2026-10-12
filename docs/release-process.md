# Release Process: Canary & Rollback

018-HAI runs local-first via Docker Compose. This defines how to ship a change
safely and roll back cleanly.

## Pre-release gates

1. Build, test and vet both the backend and IDP at the exact release revision.
2. `backend doctor` reports no failures for the target environment.
3. Frontend `ng build` succeeds.
4. The `browser-acceptance` CI job is green. It boots a temporary,
   production-mode Compose stack, waits for `/readyz`, and runs the signed-in,
   local read-only Playwright path.
5. Update the worklog and dated integration ledger for anything user-facing.
   The July completion matrix is historical scope inventory, not current
   whole-product acceptance. Record failing, skipped and unperformed gates.
6. Restore a real backup into a separate owner-labelled environment and verify
   both databases, media, control state and schema compatibility. Mocked Docker
   contracts alone do not pass this gate. Never repoint the rehearsal at the
   user's live installation or delete its volumes.
7. For a Windows production distribution, build from a clean committed tree
   with `build-windows-installer.ps1 -Production` and operator-provisioned
   signing inputs. Verify the pinned publisher, timestamps and hashes of Setup,
   workers and the uninstaller. An unsigned CI preview is not a release.
8. Test those exact installer bytes on clean Windows 11: install, first run,
   authenticated dashboard, restart, upgrade, interrupted maintenance, recovery
   and uninstall while preserving user settings/data. Bind this retained
   evidence to the source revision and release manifest. Signing mocks and
   source contract tests do not establish native installation behavior.

Only a passing, retained run establishes the browser gate for that revision;
the existence of a CI job or test script does not. The browser gate proves only
HAI's local stack. It does not prove a newly
configured OAuth account, LLM provider, browser-control path, mutable runtime,
or external delivery. Each of those needs its own approved target-environment
dry run, retained audit evidence, and postcondition verification before use.

## Versioning

Semantic tags (`vMAJOR.MINOR.PATCH`). Tag only from a green build. The current
historical baseline recorded here is `v1.0.0`; inspect remote release refs before
choosing or claiming a current published release.

## Canary (single-host)

1. Bring up the new images alongside the current stack on a non-default port /
   compose project.
2. Probe the canary: `GET /healthz` (liveness) then `GET /readyz` (readiness).
   Do not proceed while `/readyz` returns 503.
3. Repeat the signed-in local read-only browser acceptance path against the
   canary, including source intake, an approval-gated workflow, and a verified
   audit record.
4. For every newly enabled provider or mutable runtime, perform its separately
   approved bounded dry run and retain its audit and postcondition evidence.
5. Watch logs for redaction failures or unexpected 5xx for a soak window.

## Promote

Once the canary is healthy, switch the gateway/compose to the new images and
retire the old containers.

## Rollback

1. Pause inbound writes, scheduled workers, and outbound effects. Capture and
   verify backups of every affected database before changing images or schema.
2. Re-deploy the previous compatible image tag while keeping normal traffic and
   background work paused.
3. If a migration shipped, review its down-migration and the data written since
   release. Apply a down-migration only when it is explicitly safe and preserves
   that data; otherwise restore the verified backup or use a forward-compatible
   repair. Do not automatically roll back evidence-bearing or additive data.
4. Check migration status and application compatibility, then confirm
   `/readyz` is green.
5. Exercise one authenticated, read-only operator flow before reopening writes
   and workers.

Take and verify backups before cleanup or schema rollback; see
`docs/backup-restore.md` and `docs/migrations.md` for the detailed procedures.

## Migration safety

- Ship migration files (not only Gorm `AutoMigrate`) so changes are reviewable
  and reversible.
- Additive migrations first; destructive changes only after the new code is
  proven in canary.
