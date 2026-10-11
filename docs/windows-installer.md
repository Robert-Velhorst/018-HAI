# Windows 11 Installer

HAI ships as a local Windows installer around the canonical Docker Compose
stack. It installs product files under `Program Files`, stores the local
configuration in `%LOCALAPPDATA%\HAI`, and keeps all services bound to
`127.0.0.1` on port `8088` by default. The optional local A2A planning
connector is separately bound to `127.0.0.1:8091`; it cannot be reached from
the cloud tunnel and only serves the Agent Card plus its bearer-authenticated
planning endpoint.

## Prerequisite

Install and start current **Docker Desktop** with its Linux engine enabled.
The installer does not silently install Docker Desktop, enable public access,
or turn on the local-login bypass.

## Build a developer preview

From a Git checkout on Windows 11:

```powershell
winget install --id JRSoftware.InnoSetup -e
.\scripts\build-windows-installer.ps1
```

The default command produces an **unsigned developer preview**, not a production
release. A successful compile is not clean-machine acceptance or signing proof.
The generated executable is `installer\release\HAI-Setup-<commit>.exe`. The
build copies tracked product source files plus the installer source files being
built. It excludes arbitrary local files, personal source exports, credentials,
Docker data, diagnostics, and build output.

The build prepares the selected files in a separate staging directory and
checks the required installer inputs before replacing the existing payload. If
promotion fails, it attempts to restore the prior payload and manifest. This is
a local payload-generation safeguard; it is not a substitute for validating a
compiled installer on a clean Windows machine.

Release builds require a clean Git worktree. This prevents an installer from
claiming a commit while silently omitting newer local source changes. Commit or
stash work before building a distributable installer. `-AllowDirtyWorktree` is
available only for deliberate non-release developer payload experiments and
must not be used for a client or production installer.

## Build a signed production candidate

Use a clean committed checkout, an explicitly provisioned current-user
code-signing certificate, the Windows SDK SignTool executable, and an approved
HTTPS RFC 3161 timestamp service. The script does not obtain a certificate,
import a private key, or change Windows trust. Supply your real approved values:

```powershell
.\scripts\build-windows-installer.ps1 -Production `
  -SignToolPath '<absolute path to signtool.exe>' `
  -SigningCertificateThumbprint '<40-hex certificate thumbprint>' `
  -TimestampServer '<approved HTTPS timestamp URL>'
```

Production mode rejects dirty-worktree and skip-compile options. Retain the
source revision, artifact hashes, pinned-publisher Authenticode verification
for Setup, bundled workers and the uninstaller, and the release manifest.
Neither signing-test doubles nor an old installer establish a signed release
for current source. Before distribution, perform the clean Windows 11 install,
first-run, restart, upgrade, interrupted-maintenance and uninstall acceptance
gates in [the release process](release-process.md). Preserve settings and data
through recovery and uninstall. A signed candidate is not yet an accepted
production release.

## First run

Run the installer, then select **Start HAI** from the Start menu. The first run
asks for the local owner email and password, creates independent secrets in
`%LOCALAPPDATA%\HAI\hai.env`, builds the real local containers, waits for
readiness, and opens `http://127.0.0.1:8088`.

Use the Start menu entries to open the dashboard, inspect HAI status, or stop
the stack. **Test local agent connector** fetches and validates the local A2A
Agent Card, then sends one bearer-authenticated, non-executable planning probe.
It verifies the bounded planning response without exposing the connector
publicly or creating work. Stop HAI preserves the Docker volumes and local
settings.

## One installation at a time

HAI uses one canonical Compose project and named Docker volumes. If it detects
an existing HAI stack started from another directory, Start HAI stops and names
the existing directory. Stop or migrate that installation before using the
installer. This prevents two HAI instances from competing for the same data.
Ownership checks fail closed: if Docker cannot report the Compose project or
working-directory labels needed to establish which installation owns a
container, Start HAI and Stop HAI stop before acting on that stack.

If `%LOCALAPPDATA%\HAI\hai.env` is missing, Start HAI does not generate new
database or signing credentials until it has used Docker's read-only inventory
to establish that no existing HAI data volumes need the original credentials.
If Docker is unavailable, starting Docker and retrying permits only that
read-only check; it does not itself recover credentials. If existing volumes
are found, restore the original environment from the same-user protected backup
using the [environment-only recovery procedure](backup-restore.md).
Do not delete volumes or create replacement credentials to get past the guard.

The **HAI status** shortcut reports separate checks for backend dependency
readiness, identity-provider readiness, the login/dashboard frontend shells,
and the protected API route. A healthy frontend shell only proves the pages can
be served; it does not prove authenticated dashboard data works. The status
check sends no browser credentials, so it reports authenticated API-session
readiness as **not verified** and asks you to sign in and confirm that data
loads. A protected API response that requires sign-in is reported separately
from a failed backend/API route. These checks are read-only and do not log in,
create an account, or change runtime state.

If `hai.env` is missing, **HAI status** does not invoke Docker. It prints both
the clean first-run path and the safe recovery path. When existing data must be
kept, restore only the protected environment from a completed version-3 backup
under the same Windows user/profile; the procedure validates the bundle,
refuses to overwrite an existing target, and does not run Docker. Do not
generate replacement credentials or remove volumes to bypass the guard. The
status shortcut prints a PowerShell command targeting the installed recovery
script; replace its backup-folder placeholder with the verified backup path.

## Shutdown, upgrade, and recovery

Stop HAI first checks for and safely cleans up any previously recorded local
DeepSeek Harness bridge process by verifying its process identity and installed
executable path. This is legacy-process cleanup, not evidence that DSH execution
is available; see the hard-disabled status below. It then attempts to stop this
installation's Compose stack even if bridge shutdown could not be confirmed. A
remaining or unverifiable bridge is reported as an error and its process record
is retained for recovery; HAI does not claim a clean stop in that case. Docker
volumes and local settings are preserved.

Setup and uninstall share a maintenance lock with the scheduled maintenance
worker. If the lock is held or cannot be checked, setup does not replace
program files and uninstall does not begin removing them. Wait for maintenance
to finish and retry; HAI does not forcibly terminate the worker. If startup
fails after local initialization, inspect the HAI status and container logs,
then run Stop HAI before retrying. Do not remove `%LOCALAPPDATA%\HAI` or Docker
volumes as a first recovery step; they contain local credentials and user data.

## OpenClaw maintenance worker

Release builds bundle `hai-openclaw-maintenance.exe` alongside the DeepSeek
worker. The **HAI Local > OpenClaw maintenance** shortcut starts the pull worker
under the signed-in Windows user, using `%LOCALAPPDATA%\HAI\hai.env`. On a clean
installation, the initializer enables verified maintenance and creates its
dedicated worker token; **Start HAI** registers the least-privilege scheduled
task only when all worker prerequisites validate. See the [maintenance setup](openclaw-maintenance.md)
for the policy and explicit opt-out.

The launcher checks explicit enablement, separate credentials, the matching
loopback backend port and the optional Companion publisher pin. It refuses
duplicate settings and a second launcher in the same Windows session. Secrets
are passed through the child environment, never command-line arguments. Its
validation-only mode makes no backend or upstream requests:

```powershell
& '<HAI install directory>\app\installer\windows\Run-HAI-OpenClawMaintenance.ps1' -ValidateOnly
```

The normal Windows setup uses the scheduled worker; the shortcut remains an
explicit manual recovery/diagnostic path. Leave the worker running while
maintenance is enabled. Before closing it or stopping HAI, let any running
update finish; interruption can leave a job requiring owner review. No forced
stop/restart is wired into the installer. Scheduled task registration is
skipped when prerequisites are missing. Installer bundling is not proof that
an existing HAI installation has been upgraded.

## DeepSeek Harness execution status

DeepSeek Harness execution is **hard-disabled** in this release. Do not set
host-runtime or DSH execution flags, create a bridge token, configure a DSH
workspace, or run `Start-HAI.ps1 -EnableHostRuntime`. The installer helper
rejects that option regardless of environment settings. The bridge's internal
process-supervision code and tests do not mean the Windows worker is available
or safe to use.

Execution cannot be enabled until HAI has all of the following and they have
been verified on a supported Windows host: an authenticated, peer-verified
bridge channel; an OS-enforced least-privilege sandbox (a Windows Job Object
only contains process lifetime); and a durable start/cancellation protocol
that orders process resume against emergency stop and confirms the exact
process tree has exited before reporting stop as complete. The current system
does not provide an authenticated worker heartbeat, so queued work cannot be
reported as host-ready. Keep this runtime disabled until these release gates
and operator acceptance tests are complete.

## Uninstall and data

Uninstall waits for the same maintenance lock used by HAI upgrades and workers.
If maintenance is active or its state cannot be verified, uninstall is cancelled
before files are removed; wait for the worker to finish and retry. Uninstall
removes the installed program files only. It **does not delete**
`%LOCALAPPDATA%\HAI` or Docker volumes. Use the documented backup and restore
procedure before manually removing data.
