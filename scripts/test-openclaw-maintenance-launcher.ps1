[CmdletBinding()]
param()
$ErrorActionPreference = 'Stop'
Import-Module ScheduledTasks -ErrorAction Stop
$root = Split-Path -Parent $PSScriptRoot
$support = Join-Path $root 'installer/windows/Hai-OpenClawMaintenance.ps1'
$payloadValidator = Join-Path $root 'installer/windows/Hai-WindowsExecutable.ps1'
if (-not (Test-Path -LiteralPath $support)) { throw 'OpenClaw maintenance launcher support is missing.' }
. $support
$script:HaiOpenClawMaintenanceMutexName = 'Local\HAI.OpenClawMaintenance.Test.' + [Guid]::NewGuid().ToString('N')
if ($script:HaiOpenClawMaintenanceLogMutexName -cne 'Global\HAI.OpenClawMaintenance.Log') {
    throw 'The shared local log must use a cross-session maintenance lock.'
}

function New-TestSettings {
    return @{
        HAI_OPENCLAW_MAINTENANCE_ENABLED = 'true'
        HAI_OPENCLAW_MAINTENANCE_TOKEN = ('a' * 64)
        BACKEND_API_SHARED_KEY = ('b' * 64)
        BACKEND_PORT = '17070'
    }
}
function New-TestScheduledTask {
    param(
        [Parameter(Mandatory = $true)]$Action,
        [string]$UserId = 'OFFLINE\TestUser',
        [string]$Description = 'Runs HAI-authorized OpenClaw maintenance checks for the signed-in Windows user.',
        [string]$TaskPath = '\',
        $Triggers = $null,
        $Settings = $null
    )
    if ($null -eq $Triggers) {
        $Triggers = @(
            [pscustomobject]@{ CimClass = [pscustomobject]@{ CimClassName = 'MSFT_TaskLogonTrigger' }; UserId = $UserId },
            [pscustomobject]@{
                CimClass = [pscustomobject]@{ CimClassName = 'MSFT_TaskDailyTrigger' }
                StartBoundary = [DateTime]::Today.AddHours(3).ToString('o')
                DaysInterval = 1
                Repetition = [pscustomobject]@{ Interval = 'PT1H'; Duration = 'P1D'; StopAtDurationEnd = $false }
            }
        )
    }
    if ($null -eq $Settings) {
        $Settings = [pscustomobject]@{
            MultipleInstances = 'IgnoreNew'; ExecutionTimeLimit = 'PT1H'; RestartCount = 3; RestartInterval = 'PT5M'
            StartWhenAvailable = $true; DisallowStartIfOnBatteries = $false; StopIfGoingOnBatteries = $false
        }
    }
    return [pscustomobject]@{
        TaskName = 'HAI OpenClaw Maintenance'
        TaskPath = $TaskPath
        Description = $Description
        Actions = @($Action)
        Principal = [pscustomobject]@{ UserId = $UserId; LogonType = 'Interactive'; RunLevel = 'Limited' }
        Triggers = @($Triggers)
        Settings = $Settings
    }
}
function Invoke-OfflinePowerShell {
    param([string]$Executable, [string[]]$Arguments, [string]$OutputRoot, [switch]$CaptureOutput)
    $stdoutPath = Join-Path $OutputRoot ('stdout-' + [Guid]::NewGuid().ToString('N') + '.txt')
    $stderrPath = Join-Path $OutputRoot ('stderr-' + [Guid]::NewGuid().ToString('N') + '.txt')
    $argumentLine = ($Arguments | ForEach-Object { '"' + $_.Replace('"', '\"') + '"' }) -join ' '
    try {
        $process = Start-Process -FilePath $Executable -ArgumentList $argumentLine -WindowStyle Hidden `
            -Wait -PassThru -RedirectStandardOutput $stdoutPath -RedirectStandardError $stderrPath
        if ($CaptureOutput) {
            $output = [IO.File]::ReadAllText($stdoutPath) + [IO.File]::ReadAllText($stderrPath)
            return [pscustomobject]@{ ExitCode = [int]$process.ExitCode; Output = $output }
        }
        return [int]$process.ExitCode
    } finally {
        foreach ($path in @($stdoutPath, $stderrPath)) {
            if (Test-Path -LiteralPath $path -PathType Leaf) { Remove-Item -LiteralPath $path -Force }
        }
    }
}
function Assert-Rejected {
    param([hashtable]$Settings, [string]$Case)
    $rejected = $false
    try { Get-HaiOpenClawWorkerEnvironment -Values $Settings | Out-Null } catch { $rejected = $true }
    if (-not $rejected) { throw "Unsafe configuration accepted: $Case" }
}
$script:scheduledTaskMocks = @{ Existing = $null; Registration = $null; Unregistered = $false; Started = $false }
function Get-ScheduledTask {
    [CmdletBinding()]
    param([string]$TaskName, [string]$TaskPath)
    if ($script:scheduledTaskMocks.GetFailure) { throw 'Simulated Task Scheduler lookup failure.' }
    return $script:scheduledTaskMocks.Existing
}
function Register-ScheduledTask {
    [CmdletBinding()]
    param($TaskName, $TaskPath, $Action, $Trigger, $Principal, $Settings, $Description, [switch]$Force)
    $script:scheduledTaskMocks.Registration = [pscustomobject]@{
        TaskName = $TaskName; TaskPath = $TaskPath; Action = $Action; Trigger = @($Trigger)
        Principal = $Principal; Settings = $Settings; Description = $Description; Force = $Force.IsPresent
    }
    $script:scheduledTaskMocks.Existing = New-TestScheduledTask -Action $Action -UserId $Principal.UserId `
        -Description $Description -TaskPath $TaskPath -Triggers $Trigger -Settings $Settings
}
function Start-ScheduledTask {
    [CmdletBinding()]
    param([string]$TaskName, [string]$TaskPath)
    $script:scheduledTaskMocks.Started = $true
}
function Unregister-ScheduledTask {
    [CmdletBinding()]
    param([string]$TaskName, [string]$TaskPath, [switch]$Confirm)
    $script:scheduledTaskMocks.Unregistered = $true
    $script:scheduledTaskMocks.Existing = $null
}
$settings = New-TestSettings
$result = Get-HaiOpenClawWorkerEnvironment -Values $settings
if ($result.HAI_OPENCLAW_MAINTENANCE_URL -ne 'http://127.0.0.1:17070') { throw 'Backend port was not mapped to loopback.' }
if ($result.Count -ne 4 -or $result.ContainsKey('BACKEND_PORT')) { throw 'Worker environment is not allowlisted.' }
$settings.BACKEND_PORT = '17071'
if ((Get-HaiOpenClawWorkerEnvironment $settings).HAI_OPENCLAW_MAINTENANCE_URL -ne 'http://127.0.0.1:17071') { throw 'Custom backend port was ignored.' }
foreach ($url in @('http://example.com:17070', 'http://localhost:17070', 'http://127.0.0.1:17070/api', 'http://user@127.0.0.1:17070', 'http://127.0.0.1:17070/?key=secret', 'http://127.0.0.1:17070/#fragment', 'https://127.0.0.1:17070', 'http://127.0.0.1:17070')) {
    $settings = New-TestSettings
    $settings.BACKEND_PORT = '17071'
    $settings.HAI_OPENCLAW_MAINTENANCE_URL = $url
    Assert-Rejected $settings $url
}
foreach ($key in @('BACKEND_API_SHARED_KEY', 'HAI_HOST_RUNTIME_BRIDGE_TOKEN', 'OPENCLAW_GATEWAY_TOKEN', 'OPENCLAW_GATEWAY_DELEGATION_TOKEN')) {
    $settings = New-TestSettings
    $settings[$key] = $settings.HAI_OPENCLAW_MAINTENANCE_TOKEN
    Assert-Rejected $settings "credential reuse: $key"
}
foreach ($disabled in @('', 'false', 'yes')) {
    $settings = New-TestSettings
    $settings.HAI_OPENCLAW_MAINTENANCE_ENABLED = $disabled
    Assert-Rejected $settings 'disabled worker'
}
foreach ($port in @('0', '65536', 'x', '')) {
    $settings = New-TestSettings
    $settings.BACKEND_PORT = $port
    Assert-Rejected $settings 'invalid port'
}
foreach ($secret in @('', 'too-short', ('x' * 31), (('x' * 32) + '$VALUE'), (('x' * 32) + "`n"))) {
    $settings = New-TestSettings
    $settings.HAI_OPENCLAW_MAINTENANCE_TOKEN = $secret
    Assert-Rejected $settings 'invalid maintenance token'
}
$settings = New-TestSettings
$settings.HAI_OPENCLAW_PUBLISHER_THUMBPRINT = 'invalid'
Assert-Rejected $settings 'invalid publisher pin'
$settings.HAI_OPENCLAW_PUBLISHER_THUMBPRINT = 'c' * 40
$settings.UNRELATED_SECRET = 'must-not-be-exported'
$result = Get-HaiOpenClawWorkerEnvironment $settings
if ($result.ContainsKey('UNRELATED_SECRET') -or $result.HAI_OPENCLAW_PUBLISHER_THUMBPRINT -ne ('c' * 40)) { throw 'Publisher pin or allowlist failure.' }

$values = ConvertFrom-HaiMaintenanceEnvironment -Lines @('# comment', 'BACKEND_PORT=17070', 'HAI_OPENCLAW_MAINTENANCE_ENABLED="true"')
if ($values.BACKEND_PORT -ne '17070' -or $values.HAI_OPENCLAW_MAINTENANCE_ENABLED -ne 'true') { throw 'Environment parsing failed.' }
$rejected = $false
try { ConvertFrom-HaiMaintenanceEnvironment -Lines @('BACKEND_PORT=17070', 'BACKEND_PORT=17071') | Out-Null } catch { $rejected = $true }
if (-not $rejected) { throw 'Duplicate configuration must not choose a different value than Compose.' }

$build = [IO.File]::ReadAllText((Join-Path $root 'scripts/build-windows-installer.ps1'))
foreach ($text in @('hai-openclaw-maintenance.exe', './cmd/hai-openclaw-maintenance')) {
    if (-not $build.Contains($text)) { throw "Installer does not bundle $text" }
}
$launcher = Join-Path $root 'installer/windows/Run-HAI-OpenClawMaintenance.ps1'
$taskManager = Join-Path $root 'installer/windows/Manage-HAI-OpenClawMaintenanceTask.ps1'
$startHai = Join-Path $root 'installer/windows/Start-HAI.ps1'
foreach ($path in @($support, $launcher, $taskManager, $startHai)) {
    $tokens = $null; $errors = $null
    [Management.Automation.Language.Parser]::ParseFile($path, [ref]$tokens, [ref]$errors) | Out-Null
    if ($errors.Count) { throw "PowerShell syntax error in $path" }
}
$launcherAst = [Management.Automation.Language.Parser]::ParseFile($launcher, [ref]$tokens, [ref]$errors)
$lockWaitParameter = @($launcherAst.ParamBlock.Parameters | Where-Object { $_.Name.VariablePath.UserPath -eq 'LockWaitSeconds' })
if ($lockWaitParameter.Count -ne 1 -or $lockWaitParameter[0].DefaultValue.SafeGetValue() -ne 300) {
    throw 'The maintenance launcher must default to a bounded five-minute lock wait.'
}
$fixture = Join-Path ([IO.Path]::GetTempPath()) ('hai-maintenance-launcher-' + [Guid]::NewGuid().ToString('N') + '.env')
$taskTestRoot = Join-Path ([IO.Path]::GetTempPath()) ('HAI maintenance & offline test ' + [Guid]::NewGuid().ToString('N'))
$before = [Environment]::GetEnvironmentVariable('HAI_OPENCLAW_MAINTENANCE_TOKEN', 'Process')
try {
    New-Item -ItemType Directory -Path $taskTestRoot -Force | Out-Null
    $lines = @('UNRELATED_SECRET=not-for-worker')
    $settings = New-TestSettings
    foreach ($key in $settings.Keys) { $lines += "$key=$($settings[$key])" }
    [IO.File]::WriteAllLines($fixture, $lines)
    & $launcher -EnvFile $fixture -ValidateOnly
    if ([Environment]::GetEnvironmentVariable('HAI_OPENCLAW_MAINTENANCE_TOKEN', 'Process') -cne $before) {
        throw 'Validation modified the parent environment.'
    }
    [IO.File]::WriteAllLines($fixture, @('HAI_OPENCLAW_MAINTENANCE_ENABLED=false'))

    $taskFixture = Join-Path $taskTestRoot 'hai config & tokens.env'
    $testLauncher = Join-Path $taskTestRoot 'Run HAI & OpenClaw.ps1'
    $testWorker = Join-Path $taskTestRoot 'hai-openclaw-maintenance.exe'
    $testPowerShell = (Get-Process -Id $PID).Path
    Copy-Item -LiteralPath $launcher -Destination $testLauncher
    [IO.File]::WriteAllText($testWorker, 'offline placeholder; never executed')
    $settings = New-TestSettings
    $lines = @()
    foreach ($key in $settings.Keys) { $lines += "$key=$($settings[$key])" }
    [IO.File]::WriteAllLines($taskFixture, $lines)

    $script:scheduledTaskMocks = @{ Existing = $null; Registration = $null; Unregistered = $false; Started = $false }
    $registered = Register-HaiOpenClawMaintenanceTask -EnvFile $taskFixture -LauncherPath $testLauncher `
        -WorkerPath $testWorker -PowerShellPath $testPowerShell -UserId 'OFFLINE\TestUser'
    if (-not $registered -or -not $script:scheduledTaskMocks.Started) { throw 'Enabled maintenance did not register and start its scheduled worker.' }
    $registration = $script:scheduledTaskMocks.Registration
    if ($registration.TaskName -ne 'HAI OpenClaw Maintenance' -or $registration.TaskPath -ne '\' -or $registration.Trigger.Count -ne 2) {
        throw 'The scheduled worker must have both logon and daily triggers.'
    }
    if ($registration.Force) { throw 'Initial task creation must not force-replace a task that appeared after lookup.' }
    $logonTriggers = @($registration.Trigger | Where-Object { $_.CimClass.CimClassName -eq 'MSFT_TaskLogonTrigger' })
    $dailyTriggers = @($registration.Trigger | Where-Object { $_.CimClass.CimClassName -eq 'MSFT_TaskDailyTrigger' })
    if ($logonTriggers.Count -ne 1 -or $dailyTriggers.Count -ne 1 -or $logonTriggers[0].UserId -ne 'OFFLINE\TestUser') {
        throw 'Scheduled triggers have incorrect types or the logon trigger targets a different user.'
    }
    $dailyTrigger = $dailyTriggers[0]
    $dailyStart = [DateTimeOffset]::Parse([string]$dailyTrigger.StartBoundary).ToLocalTime()
    if ($dailyStart.TimeOfDay -ne (New-TimeSpan -Hours 3)) { throw 'Daily maintenance must run at the configured local 03:00 check time.' }
    if ($dailyTrigger.DaysInterval -ne 1 -or $dailyTrigger.Repetition.Interval -ne 'PT1H' -or
        $dailyTrigger.Repetition.Duration -ne 'P1D' -or $dailyTrigger.Repetition.StopAtDurationEnd) {
        throw 'The worker must wake hourly so durable one-hour backend retries are actually executed.'
    }
    if ($registration.Principal.UserId -ne 'OFFLINE\TestUser' -or $registration.Principal.LogonType -ne 'Interactive' -or $registration.Principal.RunLevel -ne 'Limited') {
        throw 'The scheduled worker must use the signed-in user with limited privileges.'
    }
    $expectedArguments = '-NoProfile -WindowStyle Hidden -ExecutionPolicy Bypass -File "{0}" -EnvFile "{1}" -Once -MaxJobs 3' -f `
        [IO.Path]::GetFullPath($testLauncher), [IO.Path]::GetFullPath($taskFixture)
    if (-not [string]::Equals($registration.Action.Arguments, $expectedArguments, [StringComparison]::Ordinal) -or
        $registration.Action.Arguments.Contains($settings.HAI_OPENCLAW_MAINTENANCE_TOKEN) -or
        $registration.Action.Arguments.Contains($settings.BACKEND_API_SHARED_KEY)) {
        throw 'Scheduled action argument quoting failed or credentials were embedded.'
    }
    if ($registration.Action.WorkingDirectory -ne (Join-Path $root 'installer/windows') -or
        $registration.Description -ne 'Runs HAI-authorized OpenClaw maintenance checks for the signed-in Windows user.') {
        throw 'Scheduled task install paths or the HAI ownership marker are incorrect.'
    }
    if ($registration.Settings.MultipleInstances -ne 'IgnoreNew' -or $registration.Settings.ExecutionTimeLimit -ne 'PT1H' -or
        $registration.Settings.RestartCount -ne 3 -or $registration.Settings.RestartInterval -ne 'PT5M' -or
        -not $registration.Settings.StartWhenAvailable -or
        $registration.Settings.DisallowStartIfOnBatteries -or $registration.Settings.StopIfGoingOnBatteries) {
        throw 'Scheduled task retry, overlap, or execution bounds are incorrect.'
    }
    $script:scheduledTaskMocks.Started = $false
    if (-not (Register-HaiOpenClawMaintenanceTask -EnvFile $taskFixture -LauncherPath $testLauncher -WorkerPath $testWorker `
            -PowerShellPath $testPowerShell -UserId 'OFFLINE\TestUser') -or -not $script:scheduledTaskMocks.Started) {
        throw 'An owned task could not be updated and restarted idempotently.'
    }
    if (-not $script:scheduledTaskMocks.Registration.Force) { throw 'Updating a verified HAI-owned task must replace its definition.' }
    $script:scheduledTaskMocks.Started = $false
    if (-not (Register-HaiOpenClawMaintenanceTask -EnvFile $taskFixture -LauncherPath $testLauncher -WorkerPath $testWorker `
            -PowerShellPath $testPowerShell -UserId 'OFFLINE\TestUser' -SkipImmediateRun) -or $script:scheduledTaskMocks.Started) {
        throw 'Installer task registration must be able to defer its immediate worker run.'
    }
    $script:scheduledTaskMocks.Started = $false
    $ownsFixtureLock = $false
    $fixtureLock = New-Object Threading.Mutex($false, $script:HaiOpenClawMaintenanceMutexName)
    $waitProcess = $null
    $previousRegistration = $script:scheduledTaskMocks.Registration
    $lockProbePath = Join-Path $taskTestRoot 'maintenance-lock-probe.ps1'
    [IO.File]::WriteAllText($lockProbePath, @'
param([string]$Support, [string]$MutexName, [string]$EnvFile, [string]$Launcher, [string]$Worker, [string]$PowerShell)
$ErrorActionPreference = 'Stop'
. $Support
$script:HaiOpenClawMaintenanceMutexName = $MutexName
$registerBlocked = $false
try {
    Register-HaiOpenClawMaintenanceTask -EnvFile $EnvFile -LauncherPath $Launcher -WorkerPath $Worker `
        -PowerShellPath $PowerShell -UserId 'OFFLINE\TestUser' | Out-Null
} catch { $registerBlocked = $_.Exception.Message -match 'currently owns the synchronization lock' }
$unregisterBlocked = $false
try {
    Unregister-HaiOpenClawMaintenanceTask -EnvFile $EnvFile -LauncherPath $Launcher `
        -PowerShellPath $PowerShell -UserId 'OFFLINE\TestUser' | Out-Null
} catch { $unregisterBlocked = $_.Exception.Message -match 'currently owns the synchronization lock' }
if (-not $registerBlocked -or -not $unregisterBlocked) { exit 10 }
exit 0
'@)
    try {
        $ownsFixtureLock = $fixtureLock.WaitOne(0)
        if (-not $ownsFixtureLock) { throw 'Could not acquire isolated maintenance lock fixture.' }
        $lockProbeExitCode = Invoke-OfflinePowerShell -Executable $testPowerShell -OutputRoot $taskTestRoot `
            -Arguments @('-NoLogo', '-NoProfile', '-File', $lockProbePath, $support, $script:HaiOpenClawMaintenanceMutexName, `
                $taskFixture, $testLauncher, $testWorker, $testPowerShell)
        if ($lockProbeExitCode -ne 0 -or $script:scheduledTaskMocks.Unregistered -or
            $script:scheduledTaskMocks.Started -or
            -not [object]::ReferenceEquals($previousRegistration, $script:scheduledTaskMocks.Registration)) {
            throw 'A separate process bypassed the held maintenance lock or changed task fixture state.'
        }

        $waitProbePath = Join-Path $taskTestRoot 'maintenance-lock-wait-probe.ps1'
        [IO.File]::WriteAllText($waitProbePath, @'
param([string]$Support, [string]$MutexName, [string]$ReadyPath, [string]$ResultPath, [int]$WaitSeconds)
$ErrorActionPreference = 'Stop'
. $Support
$script:HaiOpenClawMaintenanceMutexName = $MutexName
[IO.File]::WriteAllText($ReadyPath, 'waiting')
$watch = [Diagnostics.Stopwatch]::StartNew()
try {
    $mutex = Enter-HaiOpenClawMaintenanceLock -WaitSeconds $WaitSeconds
    $watch.Stop()
    try { [IO.File]::WriteAllText($ResultPath, "acquired:$($watch.ElapsedMilliseconds)") }
    finally { Exit-HaiOpenClawMaintenanceLock -Mutex $mutex }
} catch {
    $watch.Stop()
    [IO.File]::WriteAllText($ResultPath, "timed-out:$($watch.ElapsedMilliseconds):$($_.Exception.Message)")
}
'@)
        $startLockWaitProbe = {
            param([string]$ReadyPath, [string]$ResultPath, [int]$WaitSeconds)
            foreach ($path in @($ReadyPath, $ResultPath)) {
                if (Test-Path -LiteralPath $path) { Remove-Item -LiteralPath $path -Force }
            }
            $arguments = @('-NoLogo', '-NoProfile', '-File', $waitProbePath, $support,
                $script:HaiOpenClawMaintenanceMutexName, $ReadyPath, $ResultPath, [string]$WaitSeconds)
            $argumentLine = ($arguments | ForEach-Object { '"' + $_.Replace('"', '\"') + '"' }) -join ' '
            return Start-Process -FilePath $testPowerShell -ArgumentList $argumentLine -WindowStyle Hidden -PassThru
        }

        $readyPath = Join-Path $taskTestRoot 'lock-wait-ready.txt'
        $resultPath = Join-Path $taskTestRoot 'lock-wait-result.txt'
        $waitProcess = & $startLockWaitProbe $readyPath $resultPath 10
        $readyDeadline = [DateTimeOffset]::UtcNow.AddSeconds(10)
        while (-not (Test-Path -LiteralPath $readyPath) -and [DateTimeOffset]::UtcNow -lt $readyDeadline -and -not $waitProcess.HasExited) {
            Start-Sleep -Milliseconds 50
        }
        if (-not (Test-Path -LiteralPath $readyPath) -or $waitProcess.HasExited) { throw 'The lock-wait child did not start while the maintenance mutex was held.' }
        Start-Sleep -Milliseconds 350
        $fixtureLock.ReleaseMutex()
        $ownsFixtureLock = $false
        if (-not $waitProcess.WaitForExit(10000)) { throw 'The lock waiter did not finish after the active owner released the mutex.' }
        $waitResult = [IO.File]::ReadAllText($resultPath)
        if ($waitProcess.ExitCode -ne 0 -or $waitResult -notmatch '^acquired:(?<elapsed>\d+)$' -or [int]$Matches.elapsed -lt 200) {
            throw "The launcher lock wait did not acquire the released mutex after bounded contention: $waitResult"
        }

        $ownsFixtureLock = $fixtureLock.WaitOne(0)
        if (-not $ownsFixtureLock) { throw 'Could not reacquire the isolated mutex for the timeout test.' }
        $readyPath = Join-Path $taskTestRoot 'lock-timeout-ready.txt'
        $resultPath = Join-Path $taskTestRoot 'lock-timeout-result.txt'
        $waitProcess = & $startLockWaitProbe $readyPath $resultPath 1
        if (-not $waitProcess.WaitForExit(7000)) { throw 'The configured lock wait did not time out within its bound.' }
        $timeoutResult = [IO.File]::ReadAllText($resultPath)
        if ($waitProcess.ExitCode -ne 0 -or $timeoutResult -notmatch '^timed-out:(?<elapsed>\d+):' -or
            [int]$Matches.elapsed -lt 800 -or [int]$Matches.elapsed -gt 5000 -or -not $ownsFixtureLock) {
            throw "The lock waiter did not fail after its bounded timeout while preserving the active owner: $timeoutResult"
        }
    } finally {
        if ($null -ne $waitProcess -and -not $waitProcess.HasExited) {
            $waitProcess.Kill()
            [void]$waitProcess.WaitForExit(5000)
        }
        if ($ownsFixtureLock) { $fixtureLock.ReleaseMutex() }
        $fixtureLock.Dispose()
    }
    if (-not (Unregister-HaiOpenClawMaintenanceTask -EnvFile $taskFixture -LauncherPath $testLauncher `
            -PowerShellPath $testPowerShell -UserId 'OFFLINE\TestUser') -or
        -not $script:scheduledTaskMocks.Unregistered) { throw 'Uninstall did not remove the matching HAI task.' }

    $script:scheduledTaskMocks.Existing = $null
    if (-not (Assert-HaiOpenClawMaintenanceTaskAbsent)) { throw 'Uninstall task absence assertion did not pass for an empty fixture.' }
    $script:scheduledTaskMocks.Existing = New-TestScheduledTask -Action $registration.Action
    $absenceRejected = $false
    try { Assert-HaiOpenClawMaintenanceTaskAbsent | Out-Null } catch { $absenceRejected = $true }
    if (-not $absenceRejected) { throw 'Uninstall proceeded when a fixture task had reappeared.' }

    $script:scheduledTaskMocks = @{ Existing = [pscustomobject]@{ Actions = @([pscustomobject]@{ Execute = $testPowerShell; Arguments = 'unrelated action' }) }; Registration = $null; Unregistered = $false; Started = $false }
    $rejected = $false
    try { Unregister-HaiOpenClawMaintenanceTask -EnvFile $taskFixture -LauncherPath $testLauncher `
            -PowerShellPath $testPowerShell -UserId 'OFFLINE\TestUser' | Out-Null } catch { $rejected = $true }
    if (-not $rejected -or $script:scheduledTaskMocks.Unregistered) { throw 'Uninstall removed a same-name task that was not owned by HAI.' }

    $elevatedTask = New-TestScheduledTask -Action $registration.Action
    $elevatedTask.Principal.RunLevel = 'Highest'
    foreach ($foreignTask in @(
        (New-TestScheduledTask -Action $registration.Action -UserId 'OFFLINE\OtherUser'),
        (New-TestScheduledTask -Action $registration.Action -Description 'User-owned task'),
        (New-TestScheduledTask -Action $registration.Action -TaskPath '\Other'),
        $elevatedTask
    )) {
        $script:scheduledTaskMocks = @{ Existing = $foreignTask; Registration = $null; Unregistered = $false; Started = $false }
        $rejected = $false
        try { Unregister-HaiOpenClawMaintenanceTask -EnvFile $taskFixture -LauncherPath $testLauncher `
                -PowerShellPath $testPowerShell -UserId 'OFFLINE\TestUser' | Out-Null } catch { $rejected = $true }
        if (-not $rejected -or $script:scheduledTaskMocks.Unregistered) {
            throw 'Uninstall removed a task with mismatched HAI ownership metadata or principal.'
        }
        $rejected = $false
        try { Register-HaiOpenClawMaintenanceTask -EnvFile $taskFixture -LauncherPath $testLauncher -WorkerPath $testWorker `
                -PowerShellPath $testPowerShell -UserId 'OFFLINE\TestUser' | Out-Null } catch { $rejected = $true }
        if (-not $rejected -or $null -ne $script:scheduledTaskMocks.Registration) {
            throw 'Update replaced a task with mismatched HAI ownership metadata or principal.'
        }
    }
    $wrongWorkingDirectoryAction = [pscustomobject]@{
        Execute = $registration.Action.Execute
        Arguments = $registration.Action.Arguments
        WorkingDirectory = Join-Path $taskTestRoot 'other-working-directory'
    }
    $script:scheduledTaskMocks = @{
        Existing = New-TestScheduledTask -Action $wrongWorkingDirectoryAction
        Registration = $null; Unregistered = $false; Started = $false
    }
    $rejected = $false
    try { Unregister-HaiOpenClawMaintenanceTask -EnvFile $taskFixture -LauncherPath $testLauncher `
            -PowerShellPath $testPowerShell -UserId 'OFFLINE\TestUser' | Out-Null } catch { $rejected = $true }
    if (-not $rejected -or $script:scheduledTaskMocks.Unregistered) {
        throw 'Uninstall removed a task whose action used a different working directory.'
    }
    $rejected = $false
    try { Register-HaiOpenClawMaintenanceTask -EnvFile $taskFixture -LauncherPath $testLauncher -WorkerPath $testWorker `
            -PowerShellPath $testPowerShell -UserId 'OFFLINE\TestUser' | Out-Null } catch { $rejected = $true }
    if (-not $rejected -or $null -ne $script:scheduledTaskMocks.Registration) {
        throw 'Update replaced a task whose action used a different working directory.'
    }

    $changedScheduleTask = New-TestScheduledTask -Action $registration.Action
    $changedScheduleTask.Triggers[1].Repetition.Interval = 'PT30M'
    $script:scheduledTaskMocks = @{
        Existing = $changedScheduleTask; Registration = $null; Unregistered = $false; Started = $false
    }
    $rejected = $false
    try { Unregister-HaiOpenClawMaintenanceTask -EnvFile $taskFixture -LauncherPath $testLauncher `
            -PowerShellPath $testPowerShell -UserId 'OFFLINE\TestUser' | Out-Null } catch { $rejected = $true }
    if (-not $rejected -or $script:scheduledTaskMocks.Unregistered) {
        throw 'Uninstall removed a same-name task whose hourly cadence had been changed.'
    }
    $rejected = $false
    try { Register-HaiOpenClawMaintenanceTask -EnvFile $taskFixture -LauncherPath $testLauncher -WorkerPath $testWorker `
            -PowerShellPath $testPowerShell -UserId 'OFFLINE\TestUser' | Out-Null } catch { $rejected = $true }
    if (-not $rejected -or $null -ne $script:scheduledTaskMocks.Registration) {
        throw 'Update replaced a same-name task whose schedule was not the HAI hourly/daily definition.'
    }

    $script:scheduledTaskMocks = @{ Existing = $null; GetFailure = $true; Registration = $null; Unregistered = $false; Started = $false }
    $rejected = $false
    try { Register-HaiOpenClawMaintenanceTask -EnvFile $taskFixture -LauncherPath $testLauncher -WorkerPath $testWorker `
            -PowerShellPath $testPowerShell -UserId 'OFFLINE\TestUser' | Out-Null } catch { $rejected = $true }
    if (-not $rejected -or $null -ne $script:scheduledTaskMocks.Registration) {
        throw 'A Task Scheduler lookup failure was treated as an absent task during install.'
    }
    $rejected = $false
    try { Unregister-HaiOpenClawMaintenanceTask -EnvFile $taskFixture -LauncherPath $testLauncher `
            -PowerShellPath $testPowerShell -UserId 'OFFLINE\TestUser' | Out-Null } catch { $rejected = $true }
    if (-not $rejected -or $script:scheduledTaskMocks.Unregistered) {
        throw 'A Task Scheduler lookup failure was treated as an absent task during uninstall.'
    }

    $script:scheduledTaskMocks = @{ Existing = $null; Registration = $null; Unregistered = $false; Started = $false }
    $invalidSettings = New-TestSettings
    $invalidSettings.HAI_OPENCLAW_MAINTENANCE_TOKEN = 'short'
    $lines = @()
    foreach ($key in $invalidSettings.Keys) { $lines += "$key=$($invalidSettings[$key])" }
    [IO.File]::WriteAllLines($taskFixture, $lines)
    $rejected = $false
    try { Register-HaiOpenClawMaintenanceTask -EnvFile $taskFixture -LauncherPath $testLauncher -WorkerPath $testWorker `
            -PowerShellPath $testPowerShell -UserId 'OFFLINE\TestUser' | Out-Null } catch { $rejected = $true }
    if (-not $rejected -or $null -ne $script:scheduledTaskMocks.Registration) { throw 'Invalid credentials reached the Task Scheduler.' }

    $script:scheduledTaskMocks = @{ Existing = New-TestScheduledTask -Action $registration.Action; Registration = $null; Unregistered = $false; Started = $false }
    [IO.File]::WriteAllText($taskFixture, 'HAI_OPENCLAW_MAINTENANCE_ENABLED=false')
    if (Register-HaiOpenClawMaintenanceTask -EnvFile $taskFixture -LauncherPath $testLauncher -WorkerPath $testWorker `
            -PowerShellPath $testPowerShell -UserId 'OFFLINE\TestUser' -ne $false -or -not $script:scheduledTaskMocks.Unregistered) {
        throw 'Disabling maintenance did not remove only the existing HAI task.'
    }

    $oldLocalAppData = $env:LOCALAPPDATA
    try {
        $env:LOCALAPPDATA = $taskTestRoot
        $runFixtureRoot = Join-Path $taskTestRoot 'actual launcher lock wait'
        $runFixtureWindows = Join-Path $runFixtureRoot 'installer\windows'
        New-Item -ItemType Directory -Path $runFixtureWindows -Force | Out-Null
        $runSupport = Join-Path $runFixtureWindows 'Hai-OpenClawMaintenance.ps1'
        $runLauncher = Join-Path $runFixtureWindows 'Run-HAI-OpenClawMaintenance.ps1'
        $runWorker = Join-Path $runFixtureWindows 'hai-openclaw-maintenance.exe'
        $runConfig = Join-Path $runFixtureRoot 'hai.env'
        $runLog = Join-Path $env:LOCALAPPDATA 'HAI\logs\openclaw-maintenance-launcher.log'
        $runMarker = Join-Path $runFixtureRoot 'launcher-entered-lock.txt'
        $runMutexName = $script:HaiOpenClawMaintenanceMutexName
        $runSupportText = [IO.File]::ReadAllText($support).Replace(
            "`$script:HaiOpenClawMaintenanceMutexName = 'Global\HAI.OpenClawMaintenance'",
            "`$script:HaiOpenClawMaintenanceMutexName = '$runMutexName'")
        $lockCreationLine = '    $mutex = New-Object Threading.Mutex($false, $script:HaiOpenClawMaintenanceMutexName)'
        if (-not $runSupportText.Contains($lockCreationLine)) { throw 'Could not instrument the isolated launcher lock fixture.' }
        $markerInstrumentation = @'
    if ($env:HAI_MAINTENANCE_LOCK_TEST_MARKER) { [IO.File]::WriteAllText($env:HAI_MAINTENANCE_LOCK_TEST_MARKER, 'entered') }
'@
        $runSupportText = $runSupportText.Replace($lockCreationLine, $lockCreationLine + "`n" + $markerInstrumentation.TrimEnd())
        [IO.File]::WriteAllText($runSupport, $runSupportText, (New-Object Text.UTF8Encoding($false)))
        Copy-Item -LiteralPath $launcher -Destination $runLauncher
        Copy-Item -LiteralPath $payloadValidator -Destination (Join-Path $runFixtureWindows 'Hai-WindowsExecutable.ps1')
        [IO.File]::WriteAllText($runWorker, 'Invalid test executable; lock contention must prevent invocation.')
        $runSettings = New-TestSettings
        $runConfigLines = @()
        foreach ($key in $runSettings.Keys) { $runConfigLines += "$key=$($runSettings[$key])" }
        [IO.File]::WriteAllLines($runConfig, $runConfigLines)
        if (Test-Path -LiteralPath $runLog) { Remove-Item -LiteralPath $runLog -Force }

        $runMutex = New-Object Threading.Mutex($false, $runMutexName)
        $ownsRunMutex = $false
        $runProcess = $null
        $enginePath = (Get-Process -Id $PID).Path
        $previousLockMarker = $env:HAI_MAINTENANCE_LOCK_TEST_MARKER
        try {
            $ownsRunMutex = $runMutex.WaitOne(0)
            if (-not $ownsRunMutex) { throw 'Could not acquire the actual-launcher contention fixture mutex.' }
            $env:HAI_MAINTENANCE_LOCK_TEST_MARKER = $runMarker
            $runArguments = @('-NoLogo', '-NoProfile', '-File', $runLauncher, '-EnvFile', $runConfig, '-Once', '-LockWaitSeconds', '5')
            $runArgumentLine = ($runArguments | ForEach-Object { '"' + $_.Replace('"', '\"') + '"' }) -join ' '
            $runProcess = Start-Process -FilePath $enginePath -ArgumentList $runArgumentLine -WindowStyle Hidden -PassThru
            $runDeadline = [DateTimeOffset]::UtcNow.AddSeconds(10)
            while (-not (Test-Path -LiteralPath $runMarker) -and [DateTimeOffset]::UtcNow -lt $runDeadline -and -not $runProcess.HasExited) {
                Start-Sleep -Milliseconds 50
            }
            if (-not (Test-Path -LiteralPath $runMarker) -or $runProcess.HasExited) {
                throw 'The real launcher did not enter the lock wait while the fixture owner held the mutex.'
            }
            Start-Sleep -Milliseconds 300
            $runMutex.ReleaseMutex()
            $ownsRunMutex = $false
            if (-not $runProcess.WaitForExit(15000)) { throw 'The actual launcher did not continue after the mutex was released.' }
            $runLogText = [IO.File]::ReadAllText($runLog)
            if ($runProcess.ExitCode -eq 0 -or
                -not $runLogText.Contains('could not be verified as a valid 64-bit Windows executable') -or
                $runLogText.Contains('Scheduled OpenClaw maintenance batch started') -or
                $runLogText.Contains('currently owns the synchronization lock')) {
                throw "The actual launcher did not wait for the owner and reject the malformed worker before invocation: $runLogText"
            }
        } finally {
            if ($null -ne $runProcess -and -not $runProcess.HasExited) {
                $runProcess.Kill()
                [void]$runProcess.WaitForExit(5000)
            }
            $env:HAI_MAINTENANCE_LOCK_TEST_MARKER = $previousLockMarker
            if ($ownsRunMutex) { $runMutex.ReleaseMutex() }
            $runMutex.Dispose()
        }

        $failureFixture = Join-Path $taskTestRoot 'disabled.env'
        [IO.File]::WriteAllText($failureFixture, 'HAI_OPENCLAW_MAINTENANCE_ENABLED=false')
        $enginePath = (Get-Process -Id $PID).Path
        $launcherExitCode = Invoke-OfflinePowerShell -Executable $enginePath -OutputRoot $taskTestRoot `
            -Arguments @('-NoLogo', '-NoProfile', '-File', $launcher, '-EnvFile', $failureFixture, '-Once')
        if ($launcherExitCode -eq 0) { throw 'Launcher accepted disabled maintenance or returned a success exit code.' }
        $logPath = Join-Path $taskTestRoot 'HAI\logs\openclaw-maintenance-launcher.log'
        if (-not (Test-Path -LiteralPath $logPath -PathType Leaf)) { throw 'The hidden-worker failure was not logged locally.' }
        Write-HaiOpenClawMaintenanceLog -Level error -Message 'secret-marker-for-test' -Secrets @('secret-marker-for-test')
        $logContent = [IO.File]::ReadAllText($logPath)
        if ($logContent.Contains('secret-marker-for-test')) { throw 'Maintenance logging retained a supplied secret.' }

        $oldLogMaxBytes = $script:HaiOpenClawMaintenanceLogMaxBytes
        $oldLogBackupCount = $script:HaiOpenClawMaintenanceLogBackupCount
        $oldLocalAppDataForRotation = $env:LOCALAPPDATA
        try {
            $env:LOCALAPPDATA = Join-Path $taskTestRoot 'rotation test'
            $script:HaiOpenClawMaintenanceLogMaxBytes = 2048
            $script:HaiOpenClawMaintenanceLogBackupCount = 3
            $rotationLogDirectory = Join-Path $env:LOCALAPPDATA 'HAI\logs'
            New-Item -ItemType Directory -Path $rotationLogDirectory -Force | Out-Null
            $unrelatedLog = Join-Path $rotationLogDirectory 'unrelated.txt'
            [IO.File]::WriteAllText($unrelatedLog, 'preserve this file')
            for ($index = 1; $index -le 5; $index++) {
                Write-HaiOpenClawMaintenanceLog -Level info -Message (("rotation-$index ") + ('x' * 1500))
            }
            $rotationLogs = @(Get-ChildItem -LiteralPath $rotationLogDirectory -Filter 'openclaw-maintenance-launcher.log*' -File)
            if ($rotationLogs.Count -ne 4) { throw 'The maintenance log exceeded its configured retained-file count.' }
            foreach ($rotationLog in $rotationLogs) {
                if ($rotationLog.Length -gt $script:HaiOpenClawMaintenanceLogMaxBytes) {
                    throw 'A rotated maintenance log exceeded its configured size limit.'
                }
            }
            $retainedLogText = ($rotationLogs | ForEach-Object { [IO.File]::ReadAllText($_.FullName) }) -join "`n"
            foreach ($index in 2..5) {
                if (-not $retainedLogText.Contains("rotation-$index")) { throw "Rotated log history lost rotation-$index unexpectedly." }
            }
            if ($retainedLogText.Contains('rotation-1')) { throw 'The bounded log retention policy did not discard the oldest generated test entry.' }
            if ([IO.File]::ReadAllText($unrelatedLog) -cne 'preserve this file') { throw 'Log rotation changed a non-HAI file.' }

            $concurrentRoot = Join-Path $taskTestRoot 'concurrent rotation'
            $writerScript = Join-Path $taskTestRoot 'write-maintenance-log.ps1'
            $writerScriptText = @'
param([Parameter(Mandatory = $true)][string]$RunId)
. __SUPPORT_SCRIPT__
$env:LOCALAPPDATA = __LOG_ROOT__
$script:HaiOpenClawMaintenanceLogMaxBytes = 4096
$script:HaiOpenClawMaintenanceLogBackupCount = 3
for ($index = 1; $index -le 10; $index++) {
    Write-HaiOpenClawMaintenanceLog -Level info -Message (("writer-$RunId-$index ") + ('x' * 1500))
}
'@
            $writerScriptText = $writerScriptText.Replace('__SUPPORT_SCRIPT__', "'$support'").Replace('__LOG_ROOT__', "'$concurrentRoot'")
            [IO.File]::WriteAllText($writerScript, $writerScriptText, (New-Object Text.UTF8Encoding($false)))
            $writerJobs = @()
            foreach ($runId in @('one', 'two')) {
                $writerJobs += Start-Job -ScriptBlock {
                    param($supportPath, $localDataRoot, $writerId)
                    . $supportPath
                    $env:LOCALAPPDATA = $localDataRoot
                    $script:HaiOpenClawMaintenanceLogMaxBytes = 4096
                    $script:HaiOpenClawMaintenanceLogBackupCount = 3
                    for ($index = 1; $index -le 10; $index++) {
                        Write-HaiOpenClawMaintenanceLog -Level info -Message (("writer-$writerId-$index ") + ('x' * 1500))
                    }
                } -ArgumentList $support, $concurrentRoot, $runId
            }
            $completedJobs = @(Wait-Job -Job $writerJobs -Timeout 30)
            if ($completedJobs.Count -ne 2) { throw 'Concurrent maintenance log writers did not finish within 30 seconds.' }
            foreach ($writerJob in $writerJobs) {
                if ($writerJob.State -ne 'Completed') {
                    $workerError = Receive-Job -Job $writerJob 2>&1 | Out-String
                    throw "Concurrent maintenance writer $($writerJob.Id) failed: $workerError"
                }
                Receive-Job -Job $writerJob -ErrorAction Stop | Out-Null
            }
            Remove-Job -Job $writerJobs -Force
            $writerJobs = @()
            $concurrentDirectory = Join-Path $concurrentRoot 'HAI\logs'
            $concurrentLogs = @(Get-ChildItem -LiteralPath $concurrentDirectory -Filter 'openclaw-maintenance-launcher.log*' -File)
            if ($concurrentLogs.Count -ne 4) { throw 'Concurrent writes exceeded the configured retained-file count.' }
            $concurrentLines = @()
            foreach ($concurrentLog in $concurrentLogs) {
                if ($concurrentLog.Length -gt 4096) { throw 'Concurrent rotation exceeded the configured per-file size.' }
                $concurrentLines += [IO.File]::ReadAllLines($concurrentLog.FullName)
            }
            if ($concurrentLines.Count -ne 8) { throw 'Concurrent rotation lost or corrupted a retained log record.' }
            $concurrentIds = @()
            foreach ($line in $concurrentLines) {
                if ($line -notmatch '^\d{4}-.* \[INFO\] writer-(one|two)-(\d+) x+$') {
                    throw 'Concurrent log output contains a partial or interleaved record.'
                }
                $concurrentIds += "writer-$($Matches[1])-$($Matches[2])"
            }
            if (@($concurrentIds | Select-Object -Unique).Count -ne 8) { throw 'Concurrent log rotation duplicated a record.' }
        } finally {
            if ($writerJobs.Count -gt 0) {
                Stop-Job -Job $writerJobs -ErrorAction SilentlyContinue
                Remove-Job -Job $writerJobs -Force -ErrorAction SilentlyContinue
            }
            $env:LOCALAPPDATA = $oldLocalAppDataForRotation
            $script:HaiOpenClawMaintenanceLogMaxBytes = $oldLogMaxBytes
            $script:HaiOpenClawMaintenanceLogBackupCount = $oldLogBackupCount
        }

        $missingEnv = Join-Path $taskTestRoot 'missing.env'
        $managerExitCode = Invoke-OfflinePowerShell -Executable $enginePath -OutputRoot $taskTestRoot `
            -Arguments @('-NoLogo', '-NoProfile', '-File', $taskManager, '-Action', 'Register', '-EnvFile', $missingEnv)
        if ($managerExitCode -eq 0) { throw 'Task registration failure did not return a failure exit code.' }

        $startupRoot = Join-Path $taskTestRoot 'startup without maintenance support'
        New-Item -ItemType Directory -Path $startupRoot -Force | Out-Null
        Copy-Item -LiteralPath $startHai -Destination (Join-Path $startupRoot 'Start-HAI.ps1')
        $installerSupportStub = @'
function Assert-HaiDockerReady {}
function Assert-HaiSingleInstallation {}
function Assert-HaiExistingDataRequiresEnvironment {}
function Test-HaiEnvironmentFileAvailable { return $true }
function Assert-HaiRequiredEnvironment {}
function Stop-HaiHostRuntimeWorker {}
function Stop-HaiHostRuntimeWorkerIfPresent {}
function Initialize-HaiLocalEnvironment { param([int]$GatewayPort) }
function Get-HaiComposeArguments { return @() }
function Wait-HaiReady { param([int]$TimeoutSeconds) }
function Get-HaiUrl { return 'http://127.0.0.1:17070' }
function Start-HaiComposeStack { param([string[]]$ComposeArguments, [int]$HealthTimeoutSeconds); & docker @ComposeArguments up -d --build; if ($LASTEXITCODE -ne 0) { throw 'Offline Compose stub failed.' }; Wait-HaiReady -TimeoutSeconds $HealthTimeoutSeconds }
function docker { $global:LASTEXITCODE = 0; Write-Output 'offline-docker-stub' }
'@
        [IO.File]::WriteAllText((Join-Path $startupRoot 'Hai-InstallerSupport.ps1'), $installerSupportStub)
        $startupScript = Join-Path $startupRoot 'Start-HAI.ps1'
        $startupResult = Invoke-OfflinePowerShell -Executable $enginePath -OutputRoot $taskTestRoot -CaptureOutput `
            -Arguments @('-NoLogo', '-NoProfile', '-File', $startupScript, '-NoBrowser')
        if ($startupResult.ExitCode -ne 0 -or -not $startupResult.Output.Contains('offline-docker-stub') -or
            -not $startupResult.Output.Contains('OpenClaw maintenance support could not be loaded') -or
            -not $startupResult.Output.Contains('HAI is ready at http://127.0.0.1:17070')) {
            throw 'A missing optional maintenance support file prevented HAI startup from reaching ready state.'
        }
    } finally {
        $env:LOCALAPPDATA = $oldLocalAppData
    }
} finally {
    # Delete only the exact generated fixture, never an installation or data directory.
    if (Test-Path -LiteralPath $fixture -PathType Leaf) { Remove-Item -LiteralPath $fixture }
    $temporaryRoot = [IO.Path]::GetFullPath([IO.Path]::GetTempPath()).TrimEnd([IO.Path]::DirectorySeparatorChar) + [IO.Path]::DirectorySeparatorChar
    $resolvedTaskTestRoot = [IO.Path]::GetFullPath($taskTestRoot)
    if ($resolvedTaskTestRoot.StartsWith($temporaryRoot, [StringComparison]::OrdinalIgnoreCase) -and
        (Split-Path -Leaf $resolvedTaskTestRoot).StartsWith('HAI maintenance & offline test ', [StringComparison]::Ordinal) -and
        (Test-Path -LiteralPath $resolvedTaskTestRoot -PathType Container)) {
        Remove-Item -LiteralPath $resolvedTaskTestRoot -Recurse -Force
    }
}
$menu = [IO.File]::ReadAllText((Join-Path $root 'installer/windows/HAI.iss'))
$supportText = [IO.File]::ReadAllText($support)
$taskManagerText = [IO.File]::ReadAllText($taskManager)
$postInstallStart = $menu.IndexOf('procedure CurStepChanged', [StringComparison]::Ordinal)
$postInstallLockRelease = $menu.IndexOf('if not ReleaseHaiMaintenanceMutex then', $postInstallStart, [StringComparison]::Ordinal)
$postInstallPromotion = $menu.IndexOf("RunHaiInstallerSupport('-PromoteMaintenanceWorker'", [StringComparison]::Ordinal)
$postInstallMigration = $menu.IndexOf('-ConfigureSilentUpgrade', $postInstallStart, [StringComparison]::Ordinal)
$postInstallTaskStart = $menu.IndexOf('-StartMaintenanceTask', $postInstallStart, [StringComparison]::Ordinal)
$uninstallStart = $menu.IndexOf('function InitializeUninstall(): Boolean;', [StringComparison]::Ordinal)
$uninstallTaskRemove = $menu.IndexOf("RunHaiMaintenanceTaskManager('Unregister'", $uninstallStart, [StringComparison]::Ordinal)
$uninstallLock = $menu.IndexOf('AcquireHaiMaintenanceMutex', $uninstallStart, [StringComparison]::Ordinal)
$uninstallAbsenceCheck = $menu.IndexOf("RunHaiMaintenanceTaskManager('AssertAbsent'", $uninstallStart, [StringComparison]::Ordinal)
if ($supportText -match 'InstallerOwnsMaintenanceLock|InstallerOwnsLock' -or
    $taskManagerText -match 'InstallerOwnsMaintenanceLock' -or
    $postInstallLockRelease -lt $postInstallStart -or $postInstallPromotion -lt $postInstallLockRelease -or
    $postInstallMigration -lt $postInstallPromotion -or $postInstallTaskStart -lt $postInstallMigration -or
    $uninstallTaskRemove -lt $uninstallStart -or $uninstallLock -lt $uninstallTaskRemove -or
    $uninstallAbsenceCheck -lt $uninstallLock -or $menu.Contains('[UninstallRun]') -or $menu.Contains('[UninstallDelete]')) {
    throw 'Installer lock ownership, silent-upgrade ordering, or uninstall task-preservation contract regressed.'
}
if (-not $menu.Contains('Run-HAI-OpenClawMaintenance.ps1')) { throw 'Maintenance launcher is not exposed by the installer.' }
if (-not $menu.Contains('Manage-HAI-OpenClawMaintenanceTask.ps1') -or
    -not (Get-Content $startHai -Raw).Contains('Register-HaiOpenClawMaintenanceTask') -or
    -not $menu.Contains('InitializeUninstall') -or
    -not $menu.Contains("RunHaiMaintenanceTaskManager('Unregister'") -or
    -not $menu.Contains("RunHaiMaintenanceTaskManager('AssertAbsent'") -or
    $menu.Contains('[UninstallRun]') -or $menu.Contains('[UninstallDelete]')) {
    throw 'Uninstall must remove only the verified HAI task through the normal lock, verify it stayed absent, and preserve user data.'
}
Write-Host 'OpenClaw Windows maintenance configuration, scheduling, quoting, failure reporting, and uninstall contracts passed.'
