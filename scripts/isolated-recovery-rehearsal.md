# Isolated Windows Recovery Handoff

## Status and Ownership

Source-only changes, not executable proof or HAI production acceptance. Used
local source reads/diff inspection and apply_patch only. No parser, tests, builds,
Docker, services, providers, installs, Git mutations, or agents were run.
Preserved the preexisting integrity work and historical evidence. The report
`output/restore-production-integrity-20261001.md` remains unchanged; its
separate-daemon/fixed-installation-name proposal is not needed by this new path.

Exact worker edit ownership:
- `scripts/backup-windows.ps1`: explicit opt-in manifest route and optional
  guarded execution in existing database evidence helpers.
- `scripts/test-restore-windows.ps1`: explicit opt-in manifest route; normal
  callers still need BackupDirectory. Isolated callers cannot supply env/bundle paths.
- `scripts/windows-recovery-contract.ps1`: isolated owner/project, container,
  resource, database receipt and backup ownership validators.
- `scripts/isolated-recovery-rehearsal.ps1`: new parent-run fixture support.
- `scripts/test-isolated-recovery-contract.ps1`: new pure refusal fixtures.
- `scripts/isolated-recovery-rehearsal.md`: this handoff.

No other files, including the existing recovery tests/docs, were edited here.
Normal-installation defaults remain on their original installation path; they
are NOT the isolated rehearsal and must not be used for this parent check.

## Designed Safety Boundary

Prepare generates a fresh GUID owner/project, non-secret manifest and tiny media
fixtures only under output/recovery-rehearsal-OWNER. Later actions require that
exact path, reject reparse trees and changed manifests, and expire after 12 hours.
Cached Linux PostgreSQL 17 image ID only; no tags/pulls/builds or backend image.
Existing local engine only, engine ID rechecked; no context/daemon changes.
Two exact labelled containers, each 256 MiB/no extra swap/0.5 CPU/64 PIDs, 128 MiB PG tmpfs,
16 MiB control tmpfs and 16 MiB shared memory; no network, published ports, bind
mounts, Docker volumes, personal env/files or running HAI app services.
Full container IDs/name/image/labels/config and every recorded fixture database
name/OID/role/owner marker are reread before dump, restore, exec, copy and cleanup.
Source initialization checks identities before recording its ownership marker.
Cleanup removes only recorded container IDs after fresh ownership/database checks;
it never guesses suffix targets, prunes, removes volumes or deletes host evidence.
Failure keeps fixtures and bundle evidence; no implicit finally cleanup.

## Parent Serial Gates

First parse all changed PowerShell sources, then run both focused contract suites
serially in Windows PowerShell and PowerShell 7. Neither suite proves real restore.
The new pure suite must not invoke Docker. Parent owns every executable check.

After parent grants the owned-resource lifecycle authority and confirms headroom,
use the exact approved, already-cached PG17 image ID (never a tag or download):

```powershell
$script = '.\scripts\isolated-recovery-rehearsal.ps1'
$m = & $script -Action Prepare -PostgresImageId $approvedCachedPg17ImageId
& $script -Action Start -RecoveryResourceManifest $m
& $script -Action Seed -RecoveryResourceManifest $m
& .\scripts\backup-windows.ps1 -RecoveryResourceManifest $m
& .\scripts\test-restore-windows.ps1 -RecoveryResourceManifest $m
& $script -Action Verify -RecoveryResourceManifest $m
# Only after inspecting the successful receipt, or authorizing failed-fixture cleanup:
& $script -Action Cleanup -RecoveryResourceManifest $m
```

Retain resource-manifest.json, bundle and receipt.json. Success checks actual
pg_dump/pg_restore plus complete sorted row hashes, column/default metadata,
called/uncalled sequence states, custom enum values, relation/row owners,
Unicode/newline data, EUR0/default-deny fields, two media files/empty directory,
UID10001-readable exact restrictive control bytes, and unchanged sources.
The schema is deliberately synthetic transport data, not production model data.
Repeated verification/cleanup receipts have unique names; removed full container
IDs and database identity receipts remain in the resource manifest. Cleanup also
rechecks exact ID absence after removal, retaining all host evidence.

Parent also needs negative real cases (fresh owned runs, preserve good originals):
changed row/sequence/type/safety evidence must refuse restore without a success
receipt; changed container labels/IDs, database OIDs/markers, foreign mounts,
expired/changed manifests and wrong paths must refuse mutation/cleanup.

## Unverified and Remaining Gates

No execution or syntax-check result is claimed. Parent must validate script binding,
Docker inspect/tmpfs behavior, cached image utilities, resource headroom, actual
PostgreSQL restoration and independent receipts before accepting this increment.
Tmpfs survives only while the fixture container remains running; stopped/OOM
fixtures can lose their data. Uninitialized/expired/stopped or identity-mismatched
fixtures are preserved and cleanup fails closed, requiring reviewed parent action.
Checks are adjacent to operations, not atomic against a hostile Docker-admin actor;
parent must keep this uniquely owned project serial/exclusive. No personal/live
restore is authorized or needed for this transport check. Production schema/RLS,
grants/triggers/ledger authenticity, real pgvector compatibility, application
sessions/revocations and runtime safety acceptance remain separate gates.
