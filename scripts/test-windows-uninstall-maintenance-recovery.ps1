[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$repositoryRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..')).Path
$installerPath = Join-Path $repositoryRoot 'installer\windows\HAI.iss'
$managerPath = Join-Path $repositoryRoot 'installer\windows\Manage-HAI-OpenClawMaintenanceTask.ps1'
$maintenancePath = Join-Path $repositoryRoot 'installer\windows\Hai-OpenClawMaintenance.ps1'
$managerSource = [IO.File]::ReadAllText($managerPath)
$installerSource = [IO.File]::ReadAllText($installerPath)

$branchMatch = [regex]::Match($installerSource, "(?s)if not AcquireHaiMaintenanceMutex then\s*begin(?<body>.*?)\r?\n  ExitCode := -1;\r?\n  if not RunHaiMaintenanceTaskManager\('AssertAbsent'")
if (-not $branchMatch.Success -or
    $branchMatch.Groups['body'].Value -notmatch 'RestoreHaiMaintenanceTaskAfterCancelledUninstall\(ExitCode\)' -or
    $branchMatch.Groups['body'].Value -notmatch 'if MaintenanceRestored then' -or
    $branchMatch.Groups['body'].Value -notmatch 'uninstall was cancelled but the scheduled task could not be restored') {
    throw 'A canceled uninstall must attempt deferred task restoration and report restoration failure.'
}
if ($installerSource -notmatch '(?s)function RestoreHaiMaintenanceTaskAfterCancelledUninstall\(.*?-Action Register -SkipImmediateRun -RestoreAfterCancelledUninstall') {
    throw 'The installer recovery adapter must request registration without starting maintenance.'
}
if ($managerSource -notmatch '(?s)if \(\$RestoreAfterCancelledUninstall -and \(\$Action -ne ''Register'' -or -not \$SkipImmediateRun\)\).*?throw') {
    throw 'The manager must restrict canceled-uninstall recovery to deferred task registration.'
}

$temporaryRoot = Join-Path ([IO.Path]::GetTempPath()) ('hai-uninstall-task-recovery-' + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $temporaryRoot -Force | Out-Null
$fixtureEnvironment = Join-Path $temporaryRoot 'hai.env'
$fixtureLauncher = Join-Path $temporaryRoot 'Run-HAI-OpenClawMaintenance.ps1'
$fixtureWorker = Join-Path $temporaryRoot 'hai-openclaw-maintenance.exe'
$fixturePowerShell = Join-Path $temporaryRoot 'powershell.exe'
$fixtureLockName = 'Local\HAI.UninstallRecovery.' + [Guid]::NewGuid().ToString('N')
$lock = $null
$ownsLock = $false

function Assert-HaiUninstallRecoveryTest([bool]$Condition, [string]$Message) {
    if (-not $Condition) { throw $Message }
}

try {
    foreach ($path in @($fixtureLauncher, $fixtureWorker, $fixturePowerShell)) {
        [IO.File]::WriteAllText($path, 'isolated test fixture')
    }
    [IO.File]::WriteAllText($fixtureEnvironment, @'
HAI_OPENCLAW_MAINTENANCE_ENABLED=true
HAI_OPENCLAW_MAINTENANCE_TOKEN=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
BACKEND_API_SHARED_KEY=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
BACKEND_PORT=17070
'@)

    . $maintenancePath
    $script:HaiOpenClawMaintenanceMutexName = $fixtureLockName
    $script:HaiOpenClawMaintenanceSupportRoot = $temporaryRoot
    $script:registeredTask = $null
    $script:maintenanceStarted = $false
    function Get-HaiOpenClawMaintenanceTask { return $null }
    function Get-HaiOpenClawWorkerEnvironment { return @{ HAI_OPENCLAW_MAINTENANCE_URL = 'http://127.0.0.1:17070' } }
    function Write-HaiOpenClawMaintenanceLog { }
    function New-ScheduledTaskAction { param($Execute, $Argument, $WorkingDirectory) return [pscustomobject]@{ Execute = $Execute; Arguments = $Argument; WorkingDirectory = $WorkingDirectory } }
    function New-ScheduledTaskTrigger {
        param([switch]$Daily, [DateTime]$At, [switch]$AtLogOn, [string]$User)
        $startBoundary = if ($Daily) { $At.ToString('o') } else { [DateTime]::Now.ToString('o') }
        return [pscustomobject]@{ Repetition = $null; UserId = $User; StartBoundary = $startBoundary; DaysInterval = 1 }
    }
    function New-CimInstance { param($ClassName, $Namespace, [switch]$ClientOnly, $Property) return [pscustomobject]$Property }
    function New-ScheduledTaskPrincipal { param($UserId, $LogonType, $RunLevel) return [pscustomobject]@{ UserId = $UserId } }
    function New-ScheduledTaskSettingsSet { param($MultipleInstances, $ExecutionTimeLimit, $RestartCount, $RestartInterval, [switch]$StartWhenAvailable, [switch]$AllowStartIfOnBatteries, [switch]$DontStopIfGoingOnBatteries) return [pscustomobject]@{} }
    function Register-ScheduledTask { param($TaskName, $TaskPath, $Action, $Trigger, $Principal, $Settings, $Description, [switch]$Force) $script:registeredTask = [pscustomobject]@{ TaskName = $TaskName; Action = $Action } }
    function Start-HaiOpenClawMaintenanceTask { $script:maintenanceStarted = $true }

    $lock = New-Object Threading.Mutex($false, $fixtureLockName)
    $ownsLock = $lock.WaitOne(0)
    Assert-HaiUninstallRecoveryTest $ownsLock 'Could not hold the isolated maintenance lock fixture.'

    $unsafeRejected = $false
    try {
        Register-HaiOpenClawMaintenanceTask -EnvFile $fixtureEnvironment -LauncherPath $fixtureLauncher `
            -WorkerPath $fixtureWorker -PowerShellPath $fixturePowerShell -UserId 'OFFLINE\TestUser' `
            -SkipSynchronizationLock | Out-Null
    } catch { $unsafeRejected = $_.Exception.Message -match 'only be skipped when an immediate run is also suppressed' }
    Assert-HaiUninstallRecoveryTest $unsafeRejected 'Synchronization bypass without deferred execution was not rejected.'
    Assert-HaiUninstallRecoveryTest ($null -eq $script:registeredTask) 'Rejected bypass modified the scheduled task.'

    $registered = Register-HaiOpenClawMaintenanceTask -EnvFile $fixtureEnvironment -LauncherPath $fixtureLauncher `
        -WorkerPath $fixtureWorker -PowerShellPath $fixturePowerShell -UserId 'OFFLINE\TestUser' `
        -SkipImmediateRun -SkipSynchronizationLock
    Assert-HaiUninstallRecoveryTest $registered 'Deferred recovery did not re-register the owned maintenance task while the active run held the mutex.'
    Assert-HaiUninstallRecoveryTest ($null -ne $script:registeredTask) 'Deferred recovery did not create the task registration.'
    Assert-HaiUninstallRecoveryTest (-not $script:maintenanceStarted) 'Canceled-uninstall recovery started a maintenance run.'

    Write-Output 'Windows canceled-uninstall maintenance recovery contract passed (isolated held mutex, deferred registration only, unsafe bypass rejected).'
} finally {
    if ($ownsLock) { $lock.ReleaseMutex() }
    if ($null -ne $lock) { $lock.Dispose() }
    $resolvedRoot = [IO.Path]::GetFullPath($temporaryRoot)
    $tempRoot = [IO.Path]::GetFullPath([IO.Path]::GetTempPath()).TrimEnd([IO.Path]::DirectorySeparatorChar) + [IO.Path]::DirectorySeparatorChar
    if ($resolvedRoot.StartsWith($tempRoot, [StringComparison]::OrdinalIgnoreCase) -and
        [IO.Path]::GetFileName($resolvedRoot) -match '\Ahai-uninstall-task-recovery-[a-f0-9]{32}\z' -and
        (Test-Path -LiteralPath $resolvedRoot -PathType Container)) {
        Remove-Item -LiteralPath $resolvedRoot -Recurse -Force
    }
}
