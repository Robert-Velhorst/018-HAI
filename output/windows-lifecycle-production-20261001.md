# Windows lifecycle production hardening: source-only handoff

Date: 2026-10-01.
Workspace: `C:\Users\NO\Documents\Codex\2026-05-30\github-plugin-github-openai-curated-noodzakelijk`.
Observed branch: `codex/hai-runtime-release`.
Status: scoped source fixes delivered; behavioral and production acceptance remain unverified.

## Scope and preservation

The checkout was already extremely dirty. Changes below were applied to the current working files, not reconstructed from HEAD. Existing unrelated work was preserved. No descendants, installations, containers, application/helper processes, credential access, live-account access, deletes, commits, or pushes were performed by this worker. PowerShell was used only for source reads, source-only parsing/guard checks, and read-only Git checks.

Relevant capabilities used: local filesystem/Git reads, PowerShell parser, scoped patches, and the diagnosing-bugs skill. Its behavioral reproduction phases were deferred because this assignment explicitly forbids executing lifecycle/process/container fixtures. No applicable on-disk AGENTS.md or CONTEXT.md was found in the checked ancestor/assigned paths; the user-provided instructions govern. Connected services, browsers, live providers, signing facilities, and additional workers were not needed or used. Prior HAI memory was consulted for scope/acceptance boundaries, not treated as current test evidence.

Production files changed:

- `installer/windows/Hai-InstallerSupport.ps1`.
- `installer/windows/Start-HAI.ps1`.
- `installer/windows/Stop-HAI.ps1`.

`installer/windows/Open-HAI.ps1` was audited and left unchanged: it opens the configured loopback dashboard without starting or changing Compose. Signing scripts, `HAI.iss`, backup/restore scripts, authentication, frontend, and backend files were not changed by this worker.

Narrow regression files changed/added:

- `scripts/test-windows-runtime-lifecycle.ps1`: added project-name and Compose-collision cases and corrected its Docker label-filter mocks.
- `scripts/test-windows-installer-update-promotion.ps1`: added mocked unsafe-destination rejection cases.
- `scripts/test-windows-lifecycle-production-source.ps1`: new read-only syntax/source checks, with optional isolated mocked ownership and Stop modes for parent execution.

## Concrete findings and exact changes

### Compose project-name ambiguity

Previously `Get-HaiComposeProjectName` counted only assignments whose values already matched the accepted project-name pattern. A valid `COMPOSE_PROJECT_NAME=018-hai` followed by an invalid, empty, quoted, or whitespace-prefixed assignment could escape duplicate rejection.

The helper now counts every recognized assignment before validating the single trimmed value with a case-sensitive, anchored lowercase project-name pattern. Duplicate assignments fail closed. Missing assignments still use the existing `018-hai` default. Invalid single assignments do not silently fall back. New fixtures cover CRLF/LF, malformed duplicates, invalid single assignments, valid custom names, and the missing-setting default without rewriting settings.

### Compose ownership bypass through project-name collision

Previously `Assert-HaiSingleInstallation` enumerated only containers carrying the Compose config-file label and then recognized HAI using the config basename plus service/container markers. A container in the configured HAI project using another compose filename or unknown service name could be ignored. A configured-project container lacking the config-file label could be omitted entirely.

The helper now unions the existing config-file enumeration with a second enumeration for the exact configured project, checks both enumeration exit codes, and deduplicates IDs. Containers selected by that project cannot bypass checks through missing config labels, another compose filename, or an unknown service name. The existing exact install-root, exact compose-file, and project-name checks still apply. Unrelated stacks in other projects remain ignored unless they meet the existing HAI markers.

The runtime regression Docker mock now actually honors both label filters. Added cases reject same-project foreign config, absent config metadata, and another owner root without Docker mutations. The new optional isolated mocked-ownership mode also checks deduplication, missing owner metadata, an unrelated project, and enumeration failure. Those behavioral modes were not run here.

### Startup identity after initialization

`Start-HAI.ps1` now repeats `Assert-HaiSingleInstallation` after local initialization/migration and before selecting Compose arguments. The initial check remains, protecting the existing pre-initialization path. This closes the stale pre-initialization identity check; it is not a claim that independent concurrent Docker activity is atomically excluded.

### Stop and inherited optional settings

Start already suppresses caller `HAI_A2A_*` and `COMPOSE_PROFILES` values while managing Compose. Stop previously did not, allowing inherited profile/interpolation values to affect stopping the pinned stack or prevent configuration validation.

`Stop-HAI.ps1` now uses the same suspension helper around its existing `--profile local-a2a stop` call and restores caller state in `finally`, including a terminating Docker error. It snapshots Docker's exit code before restoration. No `down`, volume deletion, container deletion, or worker-force-stop behavior was added. Optional mocked Stop cases check success, nonzero exit, terminating error, and restoration exactly once; unrun here.

### Worker promotion destination

Promotion already validates and hashes staged PE payloads, waits on the shared maintenance mutex, revalidates changed staged bytes, and retries sharing violations within one deadline. Those existing safeguards were preserved.

Immediately before replacement, promotion now explicitly rejects an existing destination that is a directory or a reparse point instead of treating it as an ordinary installed worker or relying on a generic move/replacement failure. Unsafe-destination fixtures mock destination metadata and assert unchanged installed/pending hashes, without creating real links. They were added but not executed by this worker. The guard is a point-in-time check, not handle-bound protection against every filesystem race.

## Actual verification

Executed serially in PowerShell 7.6.5 Core:

```powershell
& .\scripts\test-windows-lifecycle-production-source.ps1
git diff --check -- installer/windows/Hai-InstallerSupport.ps1 installer/windows/Start-HAI.ps1 installer/windows/Stop-HAI.ps1 installer/windows/Open-HAI.ps1 scripts/test-windows-runtime-lifecycle.ps1 scripts/test-windows-installer-update-promotion.ps1
```

The source script exited 0 and reported: `Windows lifecycle production source checks passed (syntax and source guards only; no runtime acceptance).` It parsed the four lifecycle files and two modified regression scripts and checked project parsing, enumeration/ownership guards, post-initialization ordering, Stop restoration/exit-code capture, unchanged dashboard-only Open behavior, and promotion guard ordering. It did not invoke any lifecycle function against the host.

The scoped Git whitespace check exited 0 with no whitespace errors. Git printed its existing LF-to-CRLF conversion warnings for the lifecycle files. This is not a clean-worktree claim and does not prove Windows runtime behavior. The two existing regression scripts and the new source-test/report files are untracked, so Git's tracked diff check does not cover them. A separate read-only trailing-whitespace scan of those four files passed. The new source-test is additionally validated by its own successful execution.

Not executed: optional mocked modes, environment-file fixtures, promotion fixtures, native process termination/PID-reuse checks, Docker commands, installation/upgrade/uninstallation, signing verification, browser launch, provider traffic, or backend tests. No earlier test result is presented as evidence for these edits.

## Parent serial checks

Run these separately and serially when the parent permits mocked function execution. They need no container, install, real process enumeration/termination, real browser, credential file, or live account:

```powershell
& .\scripts\test-windows-lifecycle-production-source.ps1
& .\scripts\test-windows-lifecycle-production-source.ps1 -MockedOwnership
& .\scripts\test-windows-lifecycle-production-source.ps1 -MockedStop
```

On a separately authorized Windows fixture host, also run serially:

```powershell
& .\scripts\test-windows-runtime-lifecycle.ps1 -FocusedInstallerSettings
& .\scripts\test-windows-installer-update-promotion.ps1 -FocusedIntegrity
& .\scripts\test-windows-runtime-lifecycle.ps1
& .\scripts\test-windows-installer-update-promotion.ps1
```

The focused settings test writes/deletes isolated temporary fixture files and changes its temporary process environment. The focused promotion test performs real fixture file replacement and file-lock checks but skips its child-process timeout probe. The full lifecycle and promotion tests start controlled native children. Do not run them under this worker's no-process/no-delete constraints. The existing broad `test-windows-installer-contract.ps1 -SkipPayload` is NOT purely static: current source invokes maintenance, promotion, and runtime fixture scripts before its payload-skip path. Do not substitute it for the read-only source check.

## Remaining installer/signing acceptance gaps

- Clean supported Windows installation and signed-in-user Start/Stop/Open behavior have not been tested for these edits, including first run, repeated runs, custom ports/projects, Docker-unavailable errors, and real foreign-project collisions.
- Native process identity/creation-time verification, PID-reuse refusal, orphan cleanup, and genuine stop completion need the existing native lifecycle acceptance tests. No changes or success claims were made for those mechanisms here.
- Worker update contention, file-lock timeout, destination-link rejection, rollback/recovery, interrupted promotion, and maintenance scheduling need Windows fixture acceptance. The candidate is validated before path-based replacement; this pass does not prove race-free substitution resistance against a concurrently writable installation directory.
- Ownership is inspected before Compose operations, not under a lock shared with every independent Docker client. The new post-initialization recheck narrows a stale-check window but does not prove atomic exclusion of a concurrent external stack change.
- Signature/publisher validation, certificate/timestamp trust, signed setup/uninstaller acceptance, build provenance, and silent-upgrade outcomes are outside this worker's ownership and remain unverified here. Structural PE validation and a SHA256 comparison are not signing acceptance.
- Real clean install, in-place/silent upgrade, downgrade/recovery, maintenance-lock interaction, and uninstall data preservation require a separately approved fixture machine and the signing/installer owners' evidence. Backup/restore acceptance belongs to its assigned worker.
- Windows DSH host execution remains hard-disabled by the current source. Nothing in this patch enables it or establishes sandbox, peer authentication, durable start/cancellation, or production readiness.

The useful next step is the parent's serial read-only/mocked checks, followed by separately authorized Windows installer acceptance. Do not mark HAI or the Windows installer production-complete from this handoff.
