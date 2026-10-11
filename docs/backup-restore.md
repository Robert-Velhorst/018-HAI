# Backup & Restore Procedures

018-HAI's recoverable state spans two Postgres databases, uploaded media, and
the persisted safety-control volume. A recovery set is incomplete unless it
contains all four. In particular, a database-only recovery must not silently
reset the emergency stop or autonomy mode.

## What to back up

| Asset | Source | Method |
| --- | --- | --- |
| Automation ledger | Postgres (`AUTOMATION_DB_NAME`) | `pg_dump` logical backup |
| Accounts and authentication | Postgres (`IDP_DB_NAME`) | `pg_dump` logical backup |
| Uploaded media | `IMAGE_SAVE_DIR` | file archive (tar) |
| Emergency-stop and autonomy controls | `018-hai-phase2-control-state` | checksummed `phase2-control-state.tar.gz` |
| Configuration | `.env*` (secrets — store securely, never in git) | secure secret store |

## Database backup

On Windows 11, use the repository command. It validates Compose, briefly stops
the backend and IDP to prevent cross-database changes, dumps both databases,
archives `./images` and the safety-control volume, writes SHA-256 hashes and a
non-secret version-2 manifest, removes temporary container files, and restarts
only services that were running:

```powershell
.\scripts\backup-windows.ps1 -ValidateOnly
.\scripts\backup-windows.ps1
```

Generated bundles are ignored under `./backups`. Move the completed bundle to
encrypted off-host storage. The script deliberately does not copy `.env.local`;
store that secret material separately in an approved credential vault.

Prove the bundle can restore without touching either live database:

```powershell
.\scripts\test-restore-windows.ps1 -BackupDirectory .\backups\hai-backup-YYYYMMDDTHHMMSSZ -ValidateOnly
.\scripts\test-restore-windows.ps1 -BackupDirectory .\backups\hai-backup-YYYYMMDDTHHMMSSZ
```

The drill verifies all manifest hashes, the media ZIP, and the safety-control
archive. It restores both dumps into randomly suffixed scratch databases,
extracts the control archive into a disposable scratch volume, requires real
public tables in each database, and removes only those scratch targets in
`finally`. Version-1 bundles are rejected because they do not contain the
emergency-stop and autonomy state.

For non-Windows operators, dump **both** databases:

```bash
# Logical, compressed, restorable dump.
pg_dump --host "$AUTOMATION_DB_HOST" --port "$DB_PORT" --username "$DB_USER" \
        --format=custom --file "hai-automation-$(date +%F).dump" "$AUTOMATION_DB_NAME"
pg_dump --host "$IDP_DB_HOST" --port "$DB_PORT" --username "$DB_USER" \
        --format=custom --file "hai-identity-$(date +%F).dump" "$IDP_DB_NAME"
```

Store dumps off-host. Keep a rolling window aligned with the retention policy
(`internal/retention`).

## Media backup

```bash
tar -czf "hai-media-$(date +%F).tgz" -C "$IMAGE_SAVE_DIR" .
```

## Safety-control backup on non-Windows hosts

Use the same pinned local backend image as the Windows procedure. The helper
has no network, a read-only root filesystem, and only read access to the live
control volume. Mount a dedicated output directory rather than the repository
root:

```bash
mkdir -p ./hai-safety-backup
docker run --rm --network none --read-only --cap-drop ALL \
  --cap-add DAC_READ_SEARCH --user 0:0 \
  -v 018-hai-phase2-control-state:/source:ro \
  -v "$PWD/hai-safety-backup:/backup" \
  --entrypoint /bin/tar 018-hai-backend:local \
  -czf /backup/phase2-control-state.tar.gz -C /source .
sha256sum ./hai-safety-backup/phase2-control-state.tar.gz \
  > ./hai-safety-backup/phase2-control-state.tar.gz.sha256
tar -tzf ./hai-safety-backup/phase2-control-state.tar.gz | \
  grep -E '^\./(background_mode|emergency_stop)\.json$'
```

Test extraction in a disposable volume before accepting the bundle. Inspect
both JSON records and validate the mode, emergency-stop boolean, timestamp, and
positive revision before removing the scratch volume:

```bash
scratch="018-hai-phase2-restore-drill-$(date +%s)"
docker volume create "$scratch"
docker run --rm --network none --read-only --cap-drop ALL --cap-add CHOWN --user 0:0 \
  -v "$scratch:/restore" -v "$PWD/hai-safety-backup:/backup:ro" \
  --entrypoint /bin/sh 018-hai-backend:local -c \
  'tar -oxzf /backup/phase2-control-state.tar.gz -C /restore && chmod 0750 /restore && chmod 0600 /restore/background_mode.json /restore/emergency_stop.json && chown -R 10001:10001 /restore'
docker run --rm --network none --read-only --cap-drop ALL --user 10001:10001 \
  -v "$scratch:/state:ro" --entrypoint /bin/cat 018-hai-backend:local \
  /state/background_mode.json | jq -e \
  '.mode | IN("paused","read_only","draft_only","approval_required","autonomous_safe","emergency_stopped")'
docker run --rm --network none --read-only --cap-drop ALL --user 10001:10001 \
  -v "$scratch:/state:ro" --entrypoint /bin/cat 018-hai-backend:local \
  /state/emergency_stop.json | jq -e \
  '(.engaged | type == "boolean") and (.updatedAt | type == "string" and test("^[0-9]{4}-[0-9]{2}-[0-9]{2}T.+Z$")) and (.revision >= 1)'
docker volume rm "$scratch"
```

For an actual recovery, verify the saved checksum, restore the archive into
`018-hai-phase2-control-state`, and only then start the backend. Never extract
into or delete that live volume during a restore drill.

## Restore (into a clean database)

```bash
createdb --host "$AUTOMATION_DB_HOST" --port "$DB_PORT" --username "$DB_USER" "$AUTOMATION_DB_NAME"
pg_restore --host "$AUTOMATION_DB_HOST" --port "$DB_PORT" --username "$DB_USER" \
           --dbname "$AUTOMATION_DB_NAME" --clean --if-exists "hai-automation-YYYY-MM-DD.dump"
createdb --host "$IDP_DB_HOST" --port "$DB_PORT" --username "$DB_USER" "$IDP_DB_NAME"
pg_restore --host "$IDP_DB_HOST" --port "$DB_PORT" --username "$DB_USER" \
           --dbname "$IDP_DB_NAME" --clean --if-exists "hai-identity-YYYY-MM-DD.dump"
tar -xzf "hai-media-YYYY-MM-DD.tgz" -C "$IMAGE_SAVE_DIR"
# Restore phase2-control-state.tar.gz into 018-hai-phase2-control-state before
# starting the backend. Do not start execution from a bundle missing this file.
```

## Verify a restore

1. Start the backend against the restored database.
2. `GET /readyz` must return ready (200).
3. Run `backend reconcile` — it scans memories for broken invariants and reports
   any records needing attention after the restore.
4. Spot-check a known memory and a workflow item.

## Cadence

- Automate a daily complete recovery set: both databases, media on its required
  retention cadence, and the safety-control archive on every run.
- Test a restore into a scratch database at least monthly — an untested backup
  is not a backup.
