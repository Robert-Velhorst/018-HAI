[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$InstallerPath,
    [Parameter(Mandatory = $true)][ValidatePattern('^[0-9A-Za-z][0-9A-Za-z._-]{0,63}$')][string]$ExpectedVersion,
    [switch]$AllowUnavailableDockerOnHostedRunner,
    [switch]$ContractOnly
)

$ErrorActionPreference = 'Stop'

function Assert-HaiSmokeContract {
    param([Parameter(Mandatory = $true)][string]$RepositoryRoot)

    $installerScript = Join-Path $RepositoryRoot 'installer\windows\HAI.iss'
    $supportScript = Join-Path $RepositoryRoot 'installer\windows\Hai-InstallerSupport.ps1'
    $maintenanceScript = Join-Path $RepositoryRoot 'installer\windows\Hai-OpenClawMaintenance.ps1'
    $taskManagerScript = Join-Path $RepositoryRoot 'installer\windows\Manage-HAI-OpenClawMaintenanceTask.ps1'
    $installer = [IO.File]::ReadAllText($installerScript)
    $support = [IO.File]::ReadAllText($supportScript)
    $maintenance = [IO.File]::ReadAllText($maintenanceScript)
    $taskManager = [IO.File]::ReadAllText($taskManagerScript)
    $smoke = [IO.File]::ReadAllText($PSCommandPath)

    $tokens = $null
    $errors = $null
    [Management.Automation.Language.Parser]::ParseFile($PSCommandPath, [ref]$tokens, [ref]$errors) | Out-Null
    if ($errors.Count -gt 0) {
        throw "PowerShell parser rejected '$PSCommandPath': $($errors[0].Message)"
    }

    $runSectionMatch = [regex]::Match($installer, '(?ms)^\[Run\]\s*(?<body>.*?)(?=^\[|\z)')
    $runStartEntries = @([regex]::Matches($runSectionMatch.Groups['body'].Value, '(?m)^Filename: .*Start-HAI\.ps1.*$'))
    $uninstallMatch = [regex]::Match($installer, '(?s)function InitializeUninstall\(\): Boolean;(?<body>.*?)\r?\nprocedure DeinitializeUninstall\(\);')
    $uninstallBody = $uninstallMatch.Groups['body'].Value
    $unregisterIndex = $uninstallBody.IndexOf("RunHaiMaintenanceTaskManager('Unregister', False, ExitCode)", [StringComparison]::Ordinal)
    $stopVerificationIndex = $uninstallBody.IndexOf('if not StopHaiRuntimeForUninstall(ExitCode) or (ExitCode <> 0) then', [StringComparison]::Ordinal)
    $restoreTaskIndex = $uninstallBody.IndexOf("RunHaiMaintenanceTaskManager('Register', True, ExitCode)", [StringComparison]::Ordinal)
    $successfulUninstallIndex = $uninstallBody.LastIndexOf('Result := True;', [StringComparison]::Ordinal)

    if (-not $runSectionMatch.Success -or $runStartEntries.Count -ne 1 -or
        $runStartEntries[0].Value -notmatch '-PauseOnError' -or
        $runStartEntries[0].Value -notmatch 'Flags: postinstall nowait skipifsilent\r?$') {
        throw 'Post-install startup must retain -PauseOnError while Inno Setup skips it for silent installs.'
    }
    if (-not $uninstallMatch.Success -or $unregisterIndex -lt 0 -or $stopVerificationIndex -lt 0 -or
        $restoreTaskIndex -lt 0 -or -not ($unregisterIndex -lt $stopVerificationIndex -and
            $stopVerificationIndex -lt $restoreTaskIndex -and $restoreTaskIndex -lt $successfulUninstallIndex)) {
        throw 'Uninstall must unregister maintenance, verify runtime stop, and restore maintenance on failure before any successful uninstall result.'
    }
    if ($uninstallBody -notmatch '(?s)Result := False;.*?if not StopHaiRuntimeForUninstall\(ExitCode\) or \(ExitCode <> 0\) then.*?MaintenanceRestored := RunHaiMaintenanceTaskManager\(''Register'', True, ExitCode\).*?Exit;.*?Result := True;') {
        throw 'Runtime-stop verification failure must keep uninstall cancelled and attempt task restoration with immediate execution suppressed.'
    }
    if ($installer -notmatch '(?s)function RunHaiMaintenanceTaskManager\(.*?if SkipImmediateRun then\s+Parameters := Parameters \+ '' -SkipImmediateRun''') {
        throw 'The installer task-manager adapter must forward the no-immediate-run option.'
    }
    if ($taskManager -notmatch '\[switch\]\$SkipImmediateRun' -or
        $taskManager -notmatch 'Register-HaiOpenClawMaintenanceTask -EnvFile \$EnvFile -SkipImmediateRun:\$SkipImmediateRun' -or
        $maintenance -notmatch '(?s)if \(-not \$SkipImmediateRun\) \{.*?Start-HaiOpenClawMaintenanceTask.*?\} else \{.*?immediate run was deferred') {
        throw 'Task registration must carry SkipImmediateRun through to the maintenance implementation, which must not launch an immediate run.'
    }

    if ($installer -notmatch '(?m)^PrivilegesRequired=lowest\r?$' -or
        $installer -notmatch '(?m)^Filename: .*Parameters: .*Start-HAI\.ps1.*-PauseOnError.*Flags: postinstall nowait skipifsilent\r?$' -or
        $installer -notmatch '(?m)^Name: "\{autoprograms\}\\HAI Local\\Start HAI"; Filename: .*Start-HAI\.ps1.*-PauseOnError' -or
        $installer -notmatch "if WizardSilent then\s+RunHaiInstallerSupport\('-ConfigureSilentUpgrade'" -or
        $installer -notmatch "if WizardSilent then\s+RunHaiInstallerSupport\('-StartMaintenanceTask'" -or
        $support -notmatch 'No existing HAI environment was found\. First-run setup remains deferred' -or
        $support -notmatch 'Register-HaiOpenClawMaintenanceTask -EnvFile \$environmentFile `\s*-SkipImmediateRun' -or
        $support -notmatch 'Silent-upgrade configuration completed without starting HAI or the maintenance worker\.' -or
        $installer -notmatch 'the HAI Compose runtime could not be verified and stopped safely; uninstall was cancelled' -or
        $installer -notmatch "RunHaiMaintenanceTaskManager\('Register', True, ExitCode\)" -or
        $maintenance -notmatch "HaiOpenClawMaintenanceTaskName = 'HAI OpenClaw Maintenance'" -or
        $smoke -notmatch "RUNNER_ENVIRONMENT.*github-hosted" -or
        $smoke -notmatch 'AllowUnavailableDockerOnHostedRunner' -or
        $smoke -notmatch 'Disposable GitHub-hosted runner has no reachable Docker engine' -or
        $smoke -notmatch 'RUNNER_TEMP is a distinct, existing runner-owned temporary directory' -or
        $smoke -notmatch 'RUNNER_TEMP is a reparse point' -or
        $smoke -notmatch 'Keep installer/runtime profile writes inside this uniquely owned smoke' -or
        $smoke -notmatch 'SignatureStatus\]::NotSigned' -or
        $smoke -notmatch 'HAI has existing project containers or volumes' -or
        $smoke -notmatch 'hai\.env was created by installer setup' -or
        $smoke -notmatch 'Uninstall was safely cancelled' -or
        $smoke -notmatch 'Remove-HaiSmokeOwnedArtifacts') {
        throw 'Installer smoke contract is missing hosted-runner isolation, silent setup, first-run environment, task, or fail-closed uninstall assertions.'
    }
}

function Get-HaiTask {
    try {
        return Get-ScheduledTask -TaskName 'HAI OpenClaw Maintenance' -TaskPath '\' -ErrorAction Stop
    } catch {
        if ([string]$_.FullyQualifiedErrorId -like 'CmdletizationQuery_NotFound,Get-ScheduledTask*') { return $null }
        throw
    }
}

$script:HaiDockerUnavailableWarningWritten = $false

function Assert-HaiDockerStateIsEmpty {
    $dockerCommand = Get-Command docker -ErrorAction SilentlyContinue
    $dockerUnavailableReason = $null
    if ($null -eq $dockerCommand) {
        $dockerUnavailableReason = 'Docker CLI is unavailable; existing HAI project/volume state cannot be ruled out. Refusing to run installer smoke test.'
    } else {
        $info = @(& $dockerCommand.Source info --format '{{.ServerVersion}}' 2>&1)
        if ($LASTEXITCODE -ne 0 -or $info.Count -eq 0 -or [string]::IsNullOrWhiteSpace([string]$info[0])) {
            $dockerUnavailableReason = 'Docker engine state could not be verified; refusing to run installer smoke test.'
        }
    }
    if ($null -ne $dockerUnavailableReason) {
        $allowHostedSkip = $AllowUnavailableDockerOnHostedRunner -and
            $env:GITHUB_ACTIONS -ceq 'true' -and $env:RUNNER_ENVIRONMENT -ceq 'github-hosted' -and
            [Environment]::OSVersion.Platform -eq [PlatformID]::Win32NT
        if (-not $allowHostedSkip) { throw $dockerUnavailableReason }
        if (-not $script:HaiDockerUnavailableWarningWritten) {
            Write-Warning 'Disposable GitHub-hosted runner has no reachable Docker engine; skipping Docker inventory assertions.'
            $script:HaiDockerUnavailableWarningWritten = $true
        }
        return
    }

    $containers = @(& $dockerCommand.Source ps --all --quiet --filter 'label=com.docker.compose.project=018-hai' 2>&1)
    if ($LASTEXITCODE -ne 0) { throw 'Could not inspect Docker containers; refusing to run installer smoke test.' }
    $volumes = @(& $dockerCommand.Source volume ls --quiet 2>&1)
    if ($LASTEXITCODE -ne 0) { throw 'Could not inspect Docker volumes; refusing to run installer smoke test.' }
    $haiVolumes = @($volumes | Where-Object { [string]$_ -match '^018-hai-' })
    $projectVolumes = @(& $dockerCommand.Source volume ls --quiet --filter 'label=com.docker.compose.project=018-hai' 2>&1)
    if ($LASTEXITCODE -ne 0) { throw 'Could not inspect HAI-labelled Docker volumes; refusing to run installer smoke test.' }
    $projects = @(& $dockerCommand.Source compose ls --all --format json 2>&1)
    if ($LASTEXITCODE -ne 0) { throw 'Could not inspect Docker Compose projects; refusing to run installer smoke test.' }
    $matchingProjects = @()
    if ($projects.Count -gt 0) {
        try {
            $parsedProjects = ($projects -join "`n") | ConvertFrom-Json -ErrorAction Stop
            $matchingProjects = @($parsedProjects | Where-Object { [string]$_.Name -ieq '018-hai' })
        } catch {
            throw 'Docker Compose project inventory was not parseable; refusing to assume HAI is absent.'
        }
    }
    if (@($containers | Where-Object { -not [string]::IsNullOrWhiteSpace([string]$_) }).Count -gt 0 -or
        $haiVolumes.Count -gt 0 -or $projectVolumes.Count -gt 0 -or $matchingProjects.Count -gt 0) {
        throw 'HAI has existing project containers or volumes; installer smoke test will not inspect, stop, or alter them.'
    }
}

function Assert-HaiNoApplicationProcesses {
    param([Parameter(Mandatory = $true)][string]$InstallRoot)

    $canonicalRoot = [IO.Path]::GetFullPath($InstallRoot).TrimEnd('\')
    $matches = @(Get-CimInstance Win32_Process -ErrorAction Stop | Where-Object {
        $executable = [string]$_.ExecutablePath
        $commandLine = [string]$_.CommandLine
        ($executable -and $executable.StartsWith($canonicalRoot, [StringComparison]::OrdinalIgnoreCase)) -or
        ($commandLine -and $commandLine.IndexOf((Join-Path $canonicalRoot 'app\installer\windows\Start-HAI.ps1'), [StringComparison]::OrdinalIgnoreCase) -ge 0)
    })
    if ($matches.Count -gt 0) {
        throw 'An HAI process was launched from the smoke-test installation; silent setup must not start the application.'
    }
}

function Assert-HaiRunnerSafety {
    if ($env:GITHUB_ACTIONS -cne 'true' -or $env:RUNNER_ENVIRONMENT -cne 'github-hosted') {
        throw 'This install smoke test runs only on a GitHub-hosted disposable runner; self-hosted and local execution are refused.'
    }
    if ([Environment]::OSVersion.Platform -ne [PlatformID]::Win32NT) {
        throw 'The install smoke test requires a Windows GitHub-hosted runner.'
    }
    foreach ($name in @('RUNNER_TEMP', 'RUNNER_WORKSPACE', 'USERPROFILE', 'LOCALAPPDATA', 'APPDATA')) {
        if ([string]::IsNullOrWhiteSpace([Environment]::GetEnvironmentVariable($name))) {
            throw "Required hosted-runner isolation path '$name' is unavailable."
        }
    }
    $runnerTemp = [IO.Path]::GetFullPath($env:RUNNER_TEMP).TrimEnd('\')
    $runnerWorkspace = [IO.Path]::GetFullPath($env:RUNNER_WORKSPACE).TrimEnd('\')
    if ($runnerTemp -ieq $runnerWorkspace -or
        [IO.Path]::GetPathRoot($runnerTemp).TrimEnd('\') -ieq $runnerTemp -or
        -not (Test-Path -LiteralPath $runnerTemp -PathType Container)) {
        throw 'RUNNER_TEMP is not a distinct, existing runner-owned temporary directory; refusing installer execution.'
    }
    $tempDirectory = Get-Item -LiteralPath $runnerTemp -Force -ErrorAction Stop
    if (($tempDirectory.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
        throw 'RUNNER_TEMP is a reparse point; refusing installer execution.'
    }
    $expectedLocalAppData = [IO.Path]::GetFullPath((Join-Path $env:USERPROFILE 'AppData\Local')).TrimEnd('\')
    if ([IO.Path]::GetFullPath($env:LOCALAPPDATA).TrimEnd('\') -ine $expectedLocalAppData) {
        throw 'LOCALAPPDATA is redirected outside the disposable GitHub runner profile; refusing installer execution.'
    }
    $expectedAppData = [IO.Path]::GetFullPath((Join-Path $env:USERPROFILE 'AppData\Roaming')).TrimEnd('\')
    if ([IO.Path]::GetFullPath($env:APPDATA).TrimEnd('\') -ine $expectedAppData) {
        throw 'APPDATA is redirected outside the disposable GitHub runner profile; refusing installer execution.'
    }
    $programsPath = [IO.Path]::GetFullPath([Environment]::GetFolderPath('Programs')).TrimEnd('\')
    if (-not $programsPath.StartsWith(($expectedAppData + '\'), [StringComparison]::OrdinalIgnoreCase)) {
        throw 'The current user Start Menu resolves outside the disposable GitHub runner profile; refusing installer execution.'
    }
}

function Remove-HaiSmokeOwnedArtifacts {
    param(
        [Parameter(Mandatory = $true)][string]$SmokeRoot,
        [Parameter(Mandatory = $true)][string]$StartMenuGroup,
        [Parameter(Mandatory = $true)][string]$InstallRoot
    )

    $runnerTemp = [IO.Path]::GetFullPath($env:RUNNER_TEMP).TrimEnd('\') + '\'
    $canonicalSmokeRoot = [IO.Path]::GetFullPath($SmokeRoot).TrimEnd('\')
    if (-not $canonicalSmokeRoot.StartsWith($runnerTemp, [StringComparison]::OrdinalIgnoreCase) -or
        [IO.Path]::GetFileName($canonicalSmokeRoot) -notmatch '^hai-installer-smoke-[0-9a-f]{32}$') {
        throw 'Smoke cleanup refused a path outside its uniquely owned runner-temp directory.'
    }
    $startMenuRoot = [IO.Path]::GetFullPath([Environment]::GetFolderPath('Programs')).TrimEnd('\') + '\'
    $canonicalGroup = [IO.Path]::GetFullPath($StartMenuGroup).TrimEnd('\')
    if (-not $canonicalGroup.StartsWith($startMenuRoot, [StringComparison]::OrdinalIgnoreCase) -or
        [IO.Path]::GetFileName($canonicalGroup) -cne 'HAI Local') {
        throw 'Smoke cleanup refused a Start Menu path it does not own.'
    }
    if (Test-Path -LiteralPath $canonicalGroup) {
        $allowedShortcutNames = @(
            'Start HAI.lnk', 'Open local dashboard.lnk', 'HAI status.lnk',
            'OpenClaw maintenance.lnk', 'Test local agent connector.lnk',
            'Stop HAI.lnk', 'Uninstall HAI.lnk'
        )
        $shortcutFiles = @(Get-ChildItem -LiteralPath $canonicalGroup -Force -ErrorAction Stop)
        if (@($shortcutFiles | Where-Object { $_.PSIsContainer -or $_.Name -notin $allowedShortcutNames }).Count -gt 0) {
            throw 'Smoke cleanup refused a Start Menu group containing unexpected files.'
        }
        $canonicalInstallRoot = [IO.Path]::GetFullPath($InstallRoot).TrimEnd('\') + '\'
        $shell = New-Object -ComObject WScript.Shell
        try {
            foreach ($shortcutFile in $shortcutFiles) {
                $shortcut = $shell.CreateShortcut($shortcutFile.FullName)
                $target = [IO.Path]::GetFullPath([string]$shortcut.TargetPath)
                $arguments = [string]$shortcut.Arguments
                $ownedTarget = $target.StartsWith($canonicalInstallRoot, [StringComparison]::OrdinalIgnoreCase)
                $ownedScript = $arguments.IndexOf($canonicalInstallRoot, [StringComparison]::OrdinalIgnoreCase) -ge 0
                if (-not ($ownedTarget -or $ownedScript)) {
                    throw 'Smoke cleanup refused a shortcut that does not point into its isolated install.'
                }
            }
        } finally {
            [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($shell)
        }
        Remove-Item -LiteralPath $canonicalGroup -Recurse -Force -ErrorAction Stop
    }
    $uninstallKey = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\{2F1FA2B5-68B6-4EAF-A4B4-7E44F456B889}_is1'
    if (Test-Path -LiteralPath $uninstallKey) {
        $registeredLocation = [string](Get-ItemProperty -LiteralPath $uninstallKey -Name InstallLocation -ErrorAction Stop).InstallLocation
        if ([IO.Path]::GetFullPath($registeredLocation).TrimEnd('\') -ine (Join-Path $canonicalSmokeRoot 'installed')) {
            throw 'Smoke cleanup refused an uninstall registry entry pointing outside its owned install root.'
        }
        Remove-Item -LiteralPath $uninstallKey -Recurse -Force -ErrorAction Stop
    }
    if (Test-Path -LiteralPath $canonicalSmokeRoot) {
        Remove-Item -LiteralPath $canonicalSmokeRoot -Recurse -Force -ErrorAction Stop
    }
}

$repositoryRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..')).Path
Assert-HaiSmokeContract -RepositoryRoot $repositoryRoot
if ($ContractOnly) {
    Write-Host 'Windows installer install-smoke static safety contract passed.'
    exit 0
}

Assert-HaiRunnerSafety
Assert-HaiDockerStateIsEmpty

$resolvedInstaller = (Resolve-Path -LiteralPath $InstallerPath -ErrorAction Stop).Path
if ([IO.Path]::GetExtension($resolvedInstaller) -ine '.exe' -or
    [IO.Path]::GetFileName($resolvedInstaller) -cne "HAI-Setup-$ExpectedVersion.exe") {
    throw 'Installer path/name does not match the expected unsigned preview version.'
}
$signature = Get-AuthenticodeSignature -LiteralPath $resolvedInstaller -ErrorAction Stop
if ($signature.Status -ne [Management.Automation.SignatureStatus]::NotSigned) {
    throw "CI install smoke accepts only the explicitly unsigned preview; signature status was '$($signature.Status)'."
}
$runnerTemp = [IO.Path]::GetFullPath($env:RUNNER_TEMP).TrimEnd('\')
$smokeRoot = Join-Path $runnerTemp ("hai-installer-smoke-{0}" -f [Guid]::NewGuid().ToString('N'))
$installRoot = Join-Path $smokeRoot 'installed'
$setupLog = Join-Path $smokeRoot 'setup.log'
$uninstallLog = Join-Path $smokeRoot 'uninstall.log'
$originalLocalAppData = $env:LOCALAPPDATA
$startMenuGroup = Join-Path ([Environment]::GetFolderPath('Programs')) 'HAI Local'
$uninstallKey = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\{2F1FA2B5-68B6-4EAF-A4B4-7E44F456B889}_is1'
if (Get-HaiTask) { throw 'A HAI maintenance scheduled task already exists; refusing to run the installer.' }
if (Test-Path -LiteralPath $uninstallKey) { throw 'A HAI installer registration already exists in this runner profile.' }
if (Test-Path -LiteralPath $startMenuGroup) { throw 'A HAI Start Menu group already exists in this runner profile.' }
if (Test-Path -LiteralPath $smokeRoot) { throw 'Generated smoke directory unexpectedly already exists.' }
New-Item -ItemType Directory -Path $smokeRoot -ErrorAction Stop | Out-Null

try {
    # Keep installer/runtime profile writes inside this uniquely owned smoke
    # root. Any existing runner-profile HAI data remains outside the test.
    $env:LOCALAPPDATA = Join-Path $smokeRoot 'isolated-profile\AppData\Local'
    $localHaiRoot = Join-Path ([IO.Path]::GetFullPath($env:LOCALAPPDATA)) 'HAI'
    $environmentFile = Join-Path $localHaiRoot 'hai.env'
    if (Test-Path -LiteralPath $localHaiRoot) { throw 'Isolated smoke profile unexpectedly contains HAI data.' }

    $arguments = @(
        '/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART', '/SP-',
        "/DIR=`"$installRoot`"", "/LOG=`"$setupLog`""
    )
    $setup = Start-Process -FilePath $resolvedInstaller -ArgumentList $arguments -Wait -PassThru -NoNewWindow
    if ($setup.ExitCode -ne 0) { throw "Unsigned HAI preview setup failed with exit code $($setup.ExitCode). Review the runner-local setup log." }

    $uninstall = Get-ItemProperty -LiteralPath $uninstallKey -ErrorAction Stop
    if ([string]$uninstall.DisplayVersion -cne $ExpectedVersion -or
        [IO.Path]::GetFullPath([string]$uninstall.InstallLocation).TrimEnd('\') -ine [IO.Path]::GetFullPath($installRoot).TrimEnd('\')) {
        throw 'Installed registry version or location does not match the expected smoke-test installation.'
    }
    foreach ($relative in @(
        'app\installer\windows\Start-HAI.ps1',
        'app\installer\windows\Open-HAI.ps1',
        'app\installer\windows\HAI-Status.ps1',
        'app\installer\windows\Stop-HAI.ps1',
        'app\installer\windows\Hai-InstallerSupport.ps1',
        'app\installer\windows\Manage-HAI-OpenClawMaintenanceTask.ps1',
        'app\installer\windows\Hai-OpenClawMaintenance.ps1',
        'app\installer\windows\hai-openclaw-maintenance.exe'
    )) {
        $payloadPath = Join-Path $installRoot $relative
        if (-not (Test-Path -LiteralPath $payloadPath -PathType Leaf)) { throw "Expected installed payload is missing: $relative" }
    }
    if (-not (Test-Path -LiteralPath $setupLog -PathType Leaf)) { throw 'Inno Setup did not produce the requested setup log.' }

    $statusScript = Join-Path $installRoot 'app\installer\windows\HAI-Status.ps1'
    if ([string]::IsNullOrWhiteSpace([IO.File]::ReadAllText($statusScript))) {
        throw 'The current runner user cannot read the installed HAI payload.'
    }
    $accessProbe = Join-Path $installRoot ('.hai-install-access-' + [Guid]::NewGuid().ToString('N'))
    $accessProbeContent = [Guid]::NewGuid().ToString('N')
    try {
        [IO.File]::WriteAllText($accessProbe, $accessProbeContent)
        if ([IO.File]::ReadAllText($accessProbe) -cne $accessProbeContent) {
            throw 'The installed-payload access probe could not be read back.'
        }
    } catch {
        throw 'The current runner user cannot create and read files in its isolated installed payload.'
    } finally {
        if (Test-Path -LiteralPath $accessProbe -PathType Leaf) {
            Remove-Item -LiteralPath $accessProbe -Force -ErrorAction Stop
        }
    }

    $shortcutTargets = [ordered]@{
        'Start HAI.lnk' = 'app\installer\windows\Start-HAI.ps1'
        'Open local dashboard.lnk' = 'app\installer\windows\Open-HAI.ps1'
        'HAI status.lnk' = 'app\installer\windows\HAI-Status.ps1'
        'OpenClaw maintenance.lnk' = 'app\installer\windows\Run-HAI-OpenClawMaintenance.ps1'
        'Test local agent connector.lnk' = 'app\installer\windows\Test-HAI-LocalConnector.ps1'
        'Stop HAI.lnk' = 'app\installer\windows\Stop-HAI.ps1'
    }
    $shell = New-Object -ComObject WScript.Shell
    try {
        foreach ($entry in $shortcutTargets.GetEnumerator()) {
            $shortcutPath = Join-Path $startMenuGroup $entry.Key
            if (-not (Test-Path -LiteralPath $shortcutPath -PathType Leaf)) { throw "Expected Start Menu shortcut is missing: $($entry.Key)" }
            $shortcut = $shell.CreateShortcut($shortcutPath)
            $expectedScript = Join-Path $installRoot $entry.Value
            if ([IO.Path]::GetFileName($shortcut.TargetPath) -ine 'powershell.exe' -or
                $shortcut.Arguments.IndexOf($expectedScript, [StringComparison]::OrdinalIgnoreCase) -lt 0) {
                throw "Start Menu shortcut points to an unexpected target: $($entry.Key)"
            }
        }
        $uninstallShortcut = Join-Path $startMenuGroup 'Uninstall HAI.lnk'
        if (-not (Test-Path -LiteralPath $uninstallShortcut -PathType Leaf) -or
            [IO.Path]::GetFullPath($shell.CreateShortcut($uninstallShortcut).TargetPath) -ine (Join-Path $installRoot 'unins000.exe')) {
            throw 'The HAI uninstall shortcut is missing or points outside the isolated install.'
        }
    } finally {
        [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($shell)
    }

    if (Test-Path -LiteralPath $environmentFile) { throw 'hai.env was created by installer setup; first-run environment creation must remain deferred.' }
    if (Test-Path -LiteralPath $localHaiRoot) { throw 'Installer setup created the HAI user-data directory unexpectedly.' }
    if (Get-HaiTask) { throw 'Silent setup unexpectedly registered a scheduled maintenance task without a protected environment.' }
    Assert-HaiDockerStateIsEmpty
    Assert-HaiNoApplicationProcesses -InstallRoot $installRoot

    $supportPath = Join-Path $installRoot 'app\installer\windows\Hai-InstallerSupport.ps1'
    $supportStdout = Join-Path $smokeRoot 'uninstall-preflight.stdout.log'
    $supportStderr = Join-Path $smokeRoot 'uninstall-preflight.stderr.log'
    $powershellPath = Join-Path $env:WINDIR 'System32\WindowsPowerShell\v1.0\powershell.exe'
    $preflight = Start-Process -FilePath $powershellPath -ArgumentList @(
        '-NoProfile', '-NonInteractive', '-ExecutionPolicy', 'Bypass', '-File',
        ('"{0}"' -f $supportPath), '-StopRuntimeForUninstall'
    ) -RedirectStandardOutput $supportStdout -RedirectStandardError $supportStderr -Wait -PassThru -NoNewWindow
    if ($preflight.ExitCode -eq 0) { throw 'Uninstall runtime preflight unexpectedly succeeded without a protected environment file.' }
    $preflightOutput = [IO.File]::ReadAllText($supportStdout) + [IO.File]::ReadAllText($supportStderr)
    if ($preflightOutput -notmatch '(?i)existing HAI environment file is missing') {
        throw 'Uninstall runtime preflight failed for an unexpected reason; expected missing-environment protection was not observed.'
    }
    if (Test-Path -LiteralPath $environmentFile) { throw 'Uninstall runtime preflight created hai.env unexpectedly.' }
    Assert-HaiDockerStateIsEmpty

    $uninstallerPath = Join-Path $installRoot 'unins000.exe'
    if (-not (Test-Path -LiteralPath $uninstallerPath -PathType Leaf)) { throw 'Installed uninstaller executable is missing.' }
    $uninstallArgs = @('/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART', "/LOG=`"$uninstallLog`"")
    $uninstallProcess = Start-Process -FilePath $uninstallerPath -ArgumentList $uninstallArgs -Wait -PassThru -NoNewWindow
    if (-not (Test-Path -LiteralPath (Join-Path $installRoot 'app\installer\windows\Start-HAI.ps1') -PathType Leaf) -or
        -not (Test-Path -LiteralPath $uninstallKey) -or -not (Test-Path -LiteralPath $startMenuGroup)) {
        throw 'Uninstall did not fail closed: absent environment/runtime verification must preserve the installed files and registration.'
    }
    if (Test-Path -LiteralPath $environmentFile) { throw 'Uninstall created or modified the protected environment file.' }
    if (Get-HaiTask) { throw 'Uninstall left a HAI maintenance task behind or registered one unexpectedly.' }
    Assert-HaiDockerStateIsEmpty
    Assert-HaiNoApplicationProcesses -InstallRoot $installRoot
    if (-not (Test-Path -LiteralPath $uninstallLog -PathType Leaf)) { throw 'Inno uninstall did not produce the requested uninstall log.' }
    $uninstallLogText = [IO.File]::ReadAllText($uninstallLog)
    if ($uninstallLogText -notmatch '(?i)HAI uninstaller: the HAI Compose runtime could not be verified and stopped safely; uninstall was cancelled') {
        throw 'Uninstall did not cancel specifically at the expected runtime-ownership safety gate.'
    }
    Write-Host "PASS: installed HAI $ExpectedVersion payload and shortcuts on a GitHub-hosted disposable runner."
    Write-Host "PASS: silent setup did not create hai.env, a scheduled task, HAI containers, or an HAI process."
    Write-Host "PASS: uninstall was safely cancelled because no protected first-run environment existed; installed files were preserved. (exit $($uninstallProcess.ExitCode))"
} finally {
        $env:LOCALAPPDATA = $originalLocalAppData
        Remove-HaiSmokeOwnedArtifacts -SmokeRoot $smokeRoot -StartMenuGroup $startMenuGroup -InstallRoot $installRoot
}
