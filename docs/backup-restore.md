# Backup & Restore Procedures

018-HAI's recoverable state spans two Postgres databases, uploaded media, the
persisted safety-control volume, and optional Temporal/OpenClaw state. A
recovery set is incomplete unless it contains every stateful asset that exists
or explicitly records that an optional asset is absent/empty. In particular,
a database-only recovery must not silently reset the emergency stop, autonomy
mode, durable workflow history, or selected OpenClaw archive/rollback state.

## What to back up

| Asset | Source | Method |
| --- | --- | --- |
| Automation ledger | Postgres (`AUTOMATION_DB_NAME`) | `pg_dump` logical backup |
| Accounts and authentication | Postgres (`IDP_DB_NAME`) | `pg_dump` logical backup |
| Uploaded media | `IMAGE_SAVE_DIR` | ZIP on Windows; tar elsewhere |
| Emergency-stop and autonomy controls | `018-hai-phase2-control-state` | checksummed `phase2-control-state.tar.gz` |
| Other HAI-named Docker volumes | Docker volume inventory | **Fail-closed:** the current format refuses if an `018-hai-*` volume is outside its explicit coverage list. |
| Optional Temporal workflow persistence | `018-hai-temporal-postgres-data` | **Fail-closed:** current Windows format cannot archive/restore this volume. Backup refuses if it exists. |
| OpenClaw managed archive selections and rollback archives | `agent-workspaces/.hai-openclaw-ecosystem` | **Fail-closed:** current Windows format cannot archive/restore this store. Backup refuses if it is non-empty. |
| Windows runtime configuration | selected `-EnvFile` (contains secrets) | version-3 bundle includes a DPAPI CurrentUser-encrypted `environment.dpapi` |
| Other configuration | `.env*` (secrets — store securely, never in git) | approved secret store |

## Database backup

On Windows 11, use the repository command. It validates Compose, briefly stops
the backend and IDP to prevent cross-database changes, dumps both databases,
archives `./images` and the safety-control volume, writes SHA-256 hashes and a
version-3 manifest with `hai-recovery-integrity.v1` evidence, encrypts the selected
Windows environment file for the current Windows user, removes only
exclusively reserved temporary container files, and restarts only services that
were running. Keep the manifest private: it contains schema/count/digest
metadata, although it does not contain credentials or raw records:

```powershell
.\scripts\backup-windows.ps1 -EnvFile "$env:LOCALAPPDATA\HAI\hai.env" -ValidateOnly
.\scripts\backup-windows.ps1 -EnvFile "$env:LOCALAPPDATA\HAI\hai.env"
```

Before either preflight or backup, the script inventories Docker volumes and
the exact OpenClaw managed-store path. The current `hai-extended-recovery.v2`
contract explicitly records the automation database, IDP database, and safety
control volume, including whether each named volume exists. It refuses to
create a complete bundle if any other `018-hai-*` volume exists, if the
Temporal volume exists, or if the OpenClaw store contains any entry. It does not
stop, archive, or modify unsupported assets. This is deliberate: their current
archive/restore contract has not been implemented or rehearsed. The v2
manifest records Temporal absent, OpenClaw store absent/empty, and exactly the
three supported HAI volume records; the safety-control volume must be present.
Restore validates this declaration before environment recovery or Docker
operations. Existing v1 bundles remain readable under the legacy contract, but
cannot attest the explicit HAI volume inventory introduced by v2. Do not remove
or disable an unsupported asset to get a backup; preserve it and implement and
rehearse a separate recovery method first.

The encrypted environment artifact uses Windows DPAPI `CurrentUser` scope and
bundle-specific entropy. It can be decrypted only in the same Windows user
security context/profile that created it; copying the bundle to another account,
machine, service account, or a reset Windows profile does not transfer the
decryption key. Keep an independent recovery path for a lost Windows profile.
The `environment.dpapi` artifact is ciphertext, not a plaintext `.env` copy.
Version-3 manifests bind it to the bundle ID and record the encrypting Windows
SID; they do not contain environment values.

When the restore target is missing, the version-3 restore flow verifies the
manifest and encrypted artifact, requires the same Windows SID, decrypts in
memory, checks database/media identity against the manifest, and stages the
plaintext only in a same-directory temporary folder with protected ACLs for the
current user and SYSTEM. It installs via a no-overwrite move and removes the
staging folder on success or failure. If the target already exists, it is left
unchanged and used only after its database identities match the bundle. A
version-2 bundle remains usable with an existing matching environment file, but
cannot recover a missing one. Do not manually extract or decrypt the artifact.
An abrupt power loss or forced process termination can bypass `finally`; if a
`.hai-env-recovery-<guid>` directory remains beside the target, it is created
with current-user/SYSTEM-only ACLs. Do not copy its contents; remove only that
exact stale staging directory after verifying no restore process is still active.

If only `%LOCALAPPDATA%\HAI\hai.env` is missing and the existing Docker volumes
must be preserved, restore just the protected environment from a completed
version-3 bundle. This command validates the manifest/checksums and current-user
DPAPI binding, refuses to overwrite any target, and exits before invoking Docker;
it does not restore databases or start/stop containers:

```powershell
.\scripts\test-restore-windows.ps1 `
  -BackupDirectory .\backups\hai-backup-YYYYMMDDTHHMMSSZ-GUID `
  -EnvFile "$env:LOCALAPPDATA\HAI\hai.env" `
  -RestoreEnvironmentOnly
```

Run it as the same Windows user/profile that created the backup. Once it reports
success, use **Start HAI**; startup performs its read-only volume/installation
ownership checks before it initializes or changes any containers. If no matching
version-3 backup is available, do not generate replacement credentials or
remove volumes; recover the original protected environment from the documented
secure backup instead. This environment-only path is separate from the full
restore drill below, which requires Docker and creates scratch databases.

Windows backup requires `IMAGE_SAVE_DIR=/root/images`, its existing `./images`
bind source, and the standard mounted safety-control directory. Missing media
must not be replaced by a newly created empty folder. Reparse points are refused.
Other writers must be quiesced by the operator; stopping backend/IDP is not a
global distributed snapshot. Per-table evidence is read before and after the
backup and must agree, but cannot prove absence of every concurrent external write.

Generated bundle names include a timestamp and a GUID. Failed bundles are
retained, not recursively deleted. Only a bundle with a completed manifest is
eligible for restore. Generated bundles are ignored under `./backups`. Move the
completed bundle to encrypted off-host storage. The user-bound environment
artifact is not a substitute for a portable secret vault.

Prove the bundle can restore without touching either live database:

```powershell
.\scripts\test-restore-windows.ps1 -BackupDirectory .\backups\hai-backup-YYYYMMDDTHHMMSSZ-GUID -EnvFile "$env:LOCALAPPDATA\HAI\hai.env" -ValidateOnly
.\scripts\test-restore-windows.ps1 -BackupDirectory .\backups\hai-backup-YYYYMMDDTHHMMSSZ-GUID -EnvFile "$env:LOCALAPPDATA\HAI\hai.env"
```

The drill verifies all manifest hashes, the media ZIP, the safety-control
archive, and the explicit optional-state coverage declaration. `-ValidateOnly` checks bundle/paths/types/configuration, not restored
records; it still runs disposable read-only archive-inspection helpers. A full
drill writes scratch databases inside the selected Postgres containers, so do
not run it on the personal installation merely to test the script.

The full drill uses GUID-named scratch databases created from `template0`,
a labeled owned scratch safety volume, and an exclusively created temporary
media directory. It compares counts and streaming SHA-256 fingerprints of every
public table's canonical complete rows (including owner fields), column metadata,
and sequence `last_value`/`is_called` against evidence from the backup. The
canonical tables `context_memories`, `workflow_items`, `workflow_events`,
`workflow_decisions`, both `life_ledger_*` tables, and identity `users` must exist;
unrelated public tables are not sufficient. Zero-row tables remain valid: data
preservation is distinct from populated owner/ledger acceptance.

Restored tables/sequences must be owned by the selected restore role. This is
not production role/ACL proof: `--no-owner --no-privileges` deliberately does not
rehearse production grants. Media is extracted and compared byte-for-byte by
length/SHA-256. Safety archives may contain only their root directory and the
two regular JSON documents; links, duplicates, extras, ZIP traversal, Windows
device paths, and case-colliding media paths are refused. Restored safety record
hashes must match the saved hashes, preserving mode, emergency-stop state,
revision, and attribution without printing their content.

Cleanup only removes databases whose `createdb` succeeded, exclusively reserved
staging files, a volume whose ownership label still matches, and the exact owned
temporary directory with no reparse points. A cleanup failure suppresses the
success receipt. Version-1 bundles and older version-2 bundles lacking integrity
evidence are refused; retain them for manual review rather than rewriting them.
Complete version-2 bundles remain usable with an existing matching environment,
but cannot recover a missing environment file. New backups use version 3 and
include the user-bound protected environment artifact.

### Source contract checks

The parent/operator can run this serially in a fresh PowerShell process:

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\scripts\test-windows-recovery-contract.ps1
```

It uses a fake Docker function, synthetic credentials, and tiny owned temporary
files, not actual containers or the operator's environment. It checks evidence
mismatches, DPAPI encryption/decryption, current-user identity and version
rejection, ciphertext tampering, restrictive temporary ACLs, no-overwrite
behavior, missing-target recovery, archive safety, database-creation
collisions, preexisting-volume preservation, and unsuccessful cleanup. These
checks do not constitute an actual PostgreSQL backup/restore rehearsal.

### Disposable actual rehearsal

Use a separately provisioned, operator-approved Docker engine/context and an
isolated copy of this checkout, never the personal Docker engine or its volumes.
The scripts intentionally address fixed Compose container/volume names. A
separate Compose project name alone therefore does not provide isolation.
Before any run, the operator must verify the selected daemon identity, inventory,
absence of those names, available memory/disk, and the fixture checkout path.
Do not automatically create an engine, pull/build images, or start containers.

On that approved isolated engine only, prepare two bounded Postgres 17 fixture
containers named `018-hai-postgres-automation` and `018-hai-postgres-idp`, using
the locally available matching images and throwaway storage. Prepare the
standard-named safety fixture volume and an isolated `images` folder. Seed all
six canonical automation tables plus `users` with at least two owners, stable
known row IDs, a commitment history, a cost entry, a decision/event, a sequence,
and distinct media bytes. Use both an engaged emergency stop with actor/reason/
revision evidence and a restrictive background mode. Do not configure providers.
Keep fixture credentials in the fixture `.env.local`, not in logs or reports.

Run the backup, then the full restore drill serially on that fixture checkout.
Repeat with a deliberately incorrect manifest table digest, changed sequence
evidence, and changed safety-record digest; each must fail without a success
receipt, while the fixture source rows/media/control hashes remain unchanged.
Retain the good bundle and sanitized receipts. Cleanup requires a separately
checked exact fixture inventory; never use Compose `down -v` on the personal
engine. Parent run instructions are a proposal until explicit rehearsal authority
and sufficient resources are available.

For non-Windows operators, dump **both** databases. These commands alone do
not produce a complete recovery set. If the Temporal volume exists or the
OpenClaw managed archive store is non-empty, this repository currently has no
documented complete archive/restore path for them; do not label the resulting
database/media/control files complete or use them as a full recovery bundle.

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

Run this only against an isolated restore target, never against the live HAI
databases. The script must stop if `createdb` fails; do not continue when a
target database already exists. `pg_restore` intentionally omits `--clean` so
it cannot drop existing objects if the target check is bypassed or misapplied.

```bash
set -eu
createdb --host "$AUTOMATION_DB_HOST" --port "$DB_PORT" --username "$DB_USER" "$AUTOMATION_DB_NAME"
pg_restore --host "$AUTOMATION_DB_HOST" --port "$DB_PORT" --username "$DB_USER" \
           --exit-on-error --dbname "$AUTOMATION_DB_NAME" "hai-automation-YYYY-MM-DD.dump"
createdb --host "$IDP_DB_HOST" --port "$DB_PORT" --username "$DB_USER" "$IDP_DB_NAME"
pg_restore --host "$IDP_DB_HOST" --port "$DB_PORT" --username "$DB_USER" \
           --exit-on-error --dbname "$IDP_DB_NAME" "hai-identity-YYYY-MM-DD.dump"
tar -xzf "hai-media-YYYY-MM-DD.tgz" -C "$IMAGE_SAVE_DIR"
# Restore phase2-control-state.tar.gz into 018-hai-phase2-control-state before
# starting the backend. Do not start execution from a bundle missing this file.
```

## Verify a restore

1. Finish the integrity drill above before accepting a bundle. Verify known owner
   UUID/role/active status, known memories/workflows, and exact commitment
   revisions/cost/decision/event rows against private fixture/source receipts.
2. Separately validate production grants, RLS/owner isolation, ledger immutability,
   login/session behavior, and app-compatible migrations using approved fixtures.
3. Start the backend against an approved isolated restored database, restored
   media and restored safety state, with provider execution disabled.
4. `GET /readyz` must return ready (200).
5. Run `backend reconcile` — it scans memories for broken invariants and reports
   any records needing attention after the restore.
6. Confirm the saved restrictive autonomy mode and engaged emergency stop remain
   effective before considering any separately approved live recovery.

## Cadence

- Automate a daily complete recovery set: both databases, media on its required
  retention cadence, and the safety-control archive on every run.
- Test a restore into a scratch database at least monthly — an untested backup
  is not a backup.
