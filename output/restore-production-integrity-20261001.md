# Windows Recovery Integrity Handoff - 2026-10-01

## Status and authority

Source-only worker on `codex/hai-runtime-release`. Implemented bounded changes
in the four assigned files. This is a handoff, not a runtime verification or HAI
production-readiness claim. No descendants, commits, pushes, deletes, installs,
container/service/process lifecycle operations, localhost mutations, provider
calls, personal-install backups/restores, or volume changes were performed.
Existing shared-checkout work was preserved; no README was edited.

Used local source, PowerShell static parsing, Git read-only checks, and the
installed diagnosis/database-safety guidance. Runtime reproduction phases were
deferred explicitly because this worker is source-only and host RAM is scarce.
Cloud/browser/provider capabilities were not applicable. Memory provided only
historical scope/acceptance boundaries; current conclusions below come from
the inspected checkout. No additional agents were launched because descendants
are prohibited for this assigned worker.

## Changed Paths

- `scripts/backup-windows.ps1`
- `scripts/test-restore-windows.ps1`
- `scripts/test-windows-recovery-contract.ps1`
- `docs/backup-restore.md`
- `output/restore-production-integrity-20261001.md` (this handoff)

`scripts/windows-recovery-contract.ps1` and all Go/Angular/migration/Compose
sources were inspected read-only; their architecture and existing work are intact.

## Demonstrated Source Gaps and Fixes

1. **Unowned destructive cleanup.** The previous restore `finally` always ran
   `dropdb --if-exists` for both generated names and removed the generated volume,
   even if `createdb` failed or an idempotent `docker volume create` reused an
   existing volume. It now uses exact GUID-derived target guards, successful
   creation flags, a separately generated ownership label checked before mount
   and again before removal, and exclusively reserved staging files. The live
   database list is checked against both scratch names, not only corresponding
   database pairs. Media deletion checks the exact owned path, ancestors and
   descendant reparse points. Failed cleanup cannot emit a success receipt.

2. **Table count was not data integrity evidence.** Previously one unrelated
   public table per restored database was enough. Backup now records every public
   table's column metadata, row count and streamed SHA-256 fingerprint of sorted
   canonical complete JSONB rows, plus every public sequence's `last_value` and
   `is_called`. Raw rows are hex-encoded UTF-8 before transport and never printed.
   Sorting has a 4MB work-memory setting and 120-second per-table statement limit;
   hashing is streamed rather than buffering a table in PowerShell. Required
   canonical tables are memories, workflow items/events/decisions, commitment
   revisions, cost entries, and identity users. Full restore compares all saved
   evidence, including owner fields, and checks restored relation ownership.
   Pre/post source evidence must agree before backup completion.

3. **Safety extraction checked names but not entry types or preservation.**
   The existing archive-name helper accepts extra members and does not identify
   links. The assigned scripts now additionally require exactly the archive root
   and two regular JSON files, without duplicate/extra members or links.
   Restored emergency-stop/autonomy document hashes must match saved hashes;
   existing JSON validity and UID 10001 readability checks remain in place.

4. **Media was only ZIP-opened.** The drill now validates Windows-safe entry
   paths/types and case uniqueness, extracts into an owned temporary directory,
   and compares file lengths/SHA-256 with each archived stream. Backup verifies
   archived content against the existing media source; missing media no longer
   silently becomes a newly created empty directory. The configured media/safety
   paths must match the fixed Windows Compose mounts.

5. **Failed bundle deletion could remove prior material.** The previous
   timestamp-only directory was created with `-Force` and recursively deleted
   on failure. Bundle names now include a GUID, creation is exclusive, and failed
   partial bundles remain available for diagnosis. No prior bundle is deleted.

6. **Restart/receipt scope.** Previous backup recovery used Compose `up`,
   which could recreate containers or start dependencies, and reported success
   before cleanup/restart. It now uses `start` for the previously running
   services, attempts recovery of both, and emits success only after cleanup and
   health checks. Preflight wording now distinguishes configuration/path checks
   from database/document verification.

## Verification Actually Performed

- Read the assigned scripts/docs, recovery contract, canonical models and ledger
  migration, and relevant local Compose mount/container definitions.
- Confirmed current branch with `git branch --show-current`:
  `codex/hai-runtime-release`.
- PowerShell `System.Management.Automation.Language.Parser.ParseFile` reported
  no syntax errors in the three assigned scripts after edits. This is static
  parsing only, not execution of their operations or tests.
- Scoped `git diff --check` returned exit 0; only Git's normal LF/CRLF warnings.

## Parent: Serial Source Contract Execution

Run in a fresh process with no other tests running:

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\scripts\test-windows-recovery-contract.ps1
```

The focused regressions use a fake `docker` function and tiny owned temp files.
They cover streamed fingerprints/counts, column/sequence changes, private query
failure, unsafe ZIP paths/case collisions/links, media byte changes, unsafe TAR
types, target collisions, preservation after each refused `createdb`, preexisting
volume preservation, cleanup failure, and a successful mocked full-drill path.
The fake function deliberately does not call a Docker executable. A global mock
state is restored at test exit. Existing manifest/JSON contracts are retained.
These tests have NOT been executed by this worker; parent must record their real
result and repair any behavioral incompatibility before acceptance.

## Parent: Exact Disposable Actual Rehearsal Proposal

Do not run now on the personal installation or shared Docker engine. First obtain
explicit lifecycle/rehearsal authority and resource headroom. Use an already
provisioned, approved separate Docker engine/context and a separate fixture copy
of this checkout. A Compose project suffix alone is unsafe because the scripts
use fixed container and volume names. Do not implicitly create/pull/build an
engine/image. Verify the isolated daemon ID against an operator-provided expected
ID before any write, and remove any ambient `DOCKER_HOST` override in that
fixture-only process.

Required exact fixture target inventory on that separate engine:

- `018-hai-postgres-automation`: approved locally available matching pgvector
  Postgres 17 image, throwaway storage, at most 256MB/0.5 CPU, no published port.
- `018-hai-postgres-idp`: approved locally available Postgres 17 image,
  throwaway storage, at most 256MB/0.5 CPU, no published port.
- `018-hai-phase2-control-state`: fixture-only volume; root and the two regular
  JSON documents owned/readable by UID/GID 10001, root 0750, documents 0600.
- `018-hai-backend:local`: approved compatible local archive-helper image on the
  isolated engine; no real backend/IDP service need run for a transport rehearsal.
- Isolated checkout `images` directory containing at least two known files and
  an empty directory; fixture-only `.env.local` whose database names/user match
  the two fixture containers and `IMAGE_SAVE_DIR=/root/images`.

Seed canonical fixture tables before backup with two distinct known owner UUIDs,
known user roles/active states, memories/workflows, workflow decisions/events,
at least two commitment revisions, a cost entry, Unicode/newline-bearing text,
and a sequence with known noninitial `last_value`/`is_called`. Use an engaged
emergency-stop document with valid actor/reason/timestamps/revision and a
restrictive background mode. Preserve private expected-record receipts without
printing passwords/session material. For production-schema acceptance, seed
valid application-generated ledger digests, not invented digest placeholders.
Minimal transport fixtures do not prove application ledger validation or RLS.

After target identity/inventory/quiescence approval, execute these exact script
calls serially FROM THE ISOLATED FIXTURE CHECKOUT ONLY:

```powershell
$run = [Guid]::NewGuid().ToString('N')
$out = Join-Path (Get-Location) ("output\recovery-rehearsal-$run")
& .\scripts\backup-windows.ps1 -EnvFile .env.local -OutputDirectory $out
$bundles = @(Get-ChildItem -LiteralPath $out -Directory)
if ($bundles.Count -ne 1) { throw 'Expected exactly one newly owned fixture bundle.' }
$bundle = $bundles[0].FullName
& .\scripts\test-restore-windows.ps1 -EnvFile .env.local -BackupDirectory $bundle
```

No execution of those calls occurred here. Capture sanitized success/failure
receipts and source-vs-restored evidence. Parent should additionally retain a
private independent source receipt for the known owner, ledger revisions, cost,
decision/event, sequence and media/control hashes rather than relying solely on
the script's own manifest. Confirm source fixtures are unchanged and all owned
scratch resources are removed. Run the drill against a COPY of the good bundle
with a changed table fingerprint, changed sequence evidence, and changed safety
digest in separate serial cases; each must fail and emit no success receipt.
Do not change the original good bundle. Container-name collision scenarios use
the focused mocked regression, not manipulation of personal volumes/databases.

Keep the accepted bundle/receipts. Any final fixture-container/volume cleanup
must use separately checked exact owned inventory on the isolated engine;
never use blanket deletion, global pruning, or personal Compose `down -v`.

## Remaining Limits / Acceptance Gates

- No executable test, Docker command, PostgreSQL backup/restore, runtime/UI test,
  owner login/session test, production grant/RLS test, ledger semantic validation,
  or actual recovery rehearsal was run. Parent tests are required.
- `formatVersion=2` is retained with a mandatory integrity-contract extension.
  Older version-2 bundles remain on disk but are refused by this automated drill;
  they require reviewed manual recovery rather than silent evidence invention.
- Fingerprints prove preservation relative to captured source evidence, not
  validity/authenticity of every original row, populated-production readiness,
  cross-database owner referential validity, or resistance to a malicious actor
  replacing both manifest and bundle. Use trusted, immutable, secured bundles.
- Evidence covers public tables/columns/sequences; production functions,
  extension compatibility, constraints/triggers/immutability, RLS/policies,
  grants/roles and non-public schema need separate acceptance. Restore deliberately
  uses `--no-owner --no-privileges`; production ACL equivalence is not claimed.
- Backend/IDP stops do not quiesce all possible external writers. Operators must
  establish quiescence; matching pre/post evidence is not a universal distributed
  snapshot proof. Large datasets can hit the per-table timeout and fail closed.
- Media/TAR helpers and database sort disk use need approved resource/disk limits
  in the isolated rehearsal. No resource-heavy work should run on this <1GB-free
  host. A lower-memory mocked contract run is the first parent gate.
- Restore still uses the fixed database containers and writes scratch databases
  inside them. Engine isolation is therefore mandatory for the proposed worker
  rehearsal; no claim of zero writes to production containers is made.
- Separate secret-store recovery and Redis/session/revocation behavior are not
  covered by the four-file bundle; app/runtime safety acceptance must address
  those before live use. Providers remain disabled; the EUR 0 ceiling and
  explicit live-action approvals are unchanged.

Next action: parent runs the focused mocked contract serially and reviews the
results, then schedules the approved isolated actual rehearsal. This worker's
source improvements alone must not mark HAI production-ready.
