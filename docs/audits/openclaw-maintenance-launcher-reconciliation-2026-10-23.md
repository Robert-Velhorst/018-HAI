# OpenClaw maintenance launcher reconciliation

## Reviewed scope

Compared five retained Windows launcher and regression-test variants with current code:

- `installer/windows/Hai-OpenClawMaintenance.ps1`
- `installer/windows/Manage-HAI-OpenClawMaintenanceTask.ps1`
- `installer/windows/Run-HAI-OpenClawMaintenance.ps1`
- `scripts/test-openclaw-maintenance-launcher.ps1`
- `scripts/test-openclaw-maintenance-migration.ps1`

Keep the stronger current implementations rather than restoring older launchers
wholesale. One useful native-process scenario from the older migration test was
adapted as described below. NSIS integration and remaining archive work are not
marked reviewed by this comparison.

## Retained current behavior

- Global synchronization for worker/install operations and cross-session log
  rotation; the older logger mutex was session-local.
- Stronger scheduled-task ownership checks cover the principal, group absence,
  trigger types/user, daily/hourly timing, restart policy, execution limit,
  battery behavior and exact action/working directory.
- Registration and unregistration acquire the maintenance lock. Task absence
  can be explicitly verified before uninstall removes application files.
- Cancelled-uninstall recovery can restore registration without starting a
  maintenance run. Skipping synchronization is disallowed when immediate launch
  is requested.
- The worker launcher waits within a bounded interval for the shared lock and
  validates the bundled executable payload before starting it.
- Existing credential separation, loopback URL/port binding, publisher-pin
  validation, bounded log rotation, secret redaction and environment restoration
  remain intact.

## Integrated regression work

The launcher regression now isolates LOCALAPPDATA before loading support or
performing mock registration, restores the prior environment in `finally`, and
only deletes its exact generated temporary directory. Its lock-probe child also
blocks native Task Scheduler lookup/registration/removal/start even if the lock
regresses. Previously some early test log writes reached the real profile.

The migration regression now binds the parent promotion helper to the same
isolated mutex as its child fixture and restores the previous mutex name. It
also checks concurrent worker output against the newly generated concurrent
token, not just an unrelated earlier fixture token.

The earlier real legacy-worker test was missing from the current mocked process
coverage. Restore that useful evidence using a copied Windows command processor
and bounded loopback-only child in the generated temporary installation. Verify
its exact PID/executable path through native CIM, require the actual promotion
timeout while it still runs, prove installed/staged hashes remain unchanged,
wait for natural exit, then verify successful atomic promotion. Keep current
payload validation and self-acquired isolated locking; do not restore the old
installer-lock bypass. Other legacy-process cases remain explicitly mocked.

## Verification and cleanup boundary

Local Windows PowerShell validation covered all three files' syntax, the valid
four-setting worker projection, ten rejected configurations, duplicate settings
and quoted-value parsing. It did not register/unregister tasks, write maintenance
logs, access the network or launch an installer.

After the isolation fixes, both complete Windows regression scripts pass locally.
These runs exercise real temporary-file ACL/encoding/idempotency and migration
mutex operations, bounded launcher lock contention, malformed executable rejection,
temporary log rotation and the restored native process/path replacement guard.
Task Scheduler mutation is mocked/blocked. The native child uses loopback only;
no real HAI installation, production environment file or installer is executed.
Portable CI contracts protect the fixture-isolation and native-path checks.

A separate byte comparison covered all 618 frozen selected worktree entries
against the pinned current main revision. None matched exactly or through the
tested uniform LF-to-CRLF roundtrip: 611 differed and seven were absent on main.
This rules out bulk deletion based merely on the assumption that the differences
are line endings. It is not a claim that each differing file contains useful new
work, nor a substitute for per-source semantic reconciliation.

Original source copies remain intact. Existing implementations already present
on main and semantic review receipts are not by themselves proof that every
original byte, archive item or unfinished change has been preserved for deletion.
