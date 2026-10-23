# OpenClaw maintenance launcher reconciliation

## Reviewed scope

Compared three retained Windows launcher variants with current code:

- `installer/windows/Hai-OpenClawMaintenance.ps1`
- `installer/windows/Manage-HAI-OpenClawMaintenanceTask.ps1`
- `installer/windows/Run-HAI-OpenClawMaintenance.ps1`

The older variants add no missing behavior. Keep the current implementations
rather than restoring them wholesale. The larger launcher/migration test-script
variants, NSIS integration and remaining archive work are not marked reviewed by
this comparison.

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

## Verification and cleanup boundary

Local Windows PowerShell validation covered all three files' syntax, the valid
four-setting worker projection, ten rejected configurations, duplicate settings
and quoted-value parsing. It did not register/unregister tasks, write maintenance
logs, access the network or launch an installer.

A separate byte comparison covered all 618 frozen selected worktree entries
against the pinned current main revision. None matched exactly or through the
tested uniform LF-to-CRLF roundtrip: 611 differed and seven were absent on main.
This rules out bulk deletion based merely on the assumption that the differences
are line endings. It is not a claim that each differing file contains useful new
work, nor a substitute for per-source semantic reconciliation.

Original source copies remain intact. Existing implementations already present
on main and semantic review receipts are not by themselves proof that every
original byte, archive item or unfinished change has been preserved for deletion.
