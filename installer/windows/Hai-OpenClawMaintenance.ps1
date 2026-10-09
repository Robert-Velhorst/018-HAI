$script:HaiOpenClawMaintenanceSupportRoot = $PSScriptRoot
$script:HaiOpenClawMaintenanceMutexName = 'Global\HAI.OpenClawMaintenance'
$script:HaiOpenClawMaintenanceLogMutexName = 'Global\HAI.OpenClawMaintenance.Log'
$script:HaiOpenClawMaintenanceTaskName = 'HAI OpenClaw Maintenance'
$script:HaiOpenClawMaintenanceTaskDescription = 'Runs HAI-authorized OpenClaw maintenance checks for the signed-in Windows user.'
$script:HaiOpenClawMaintenanceLogMaxBytes = 1048576
$script:HaiOpenClawMaintenanceLogBackupCount = 3

function ConvertFrom-HaiMaintenanceEnvironment {
    param([string[]]$Lines)
    $values = @{}
    foreach ($line in $Lines) {
        if ($line -notmatch '^(?<name>[A-Z][A-Z0-9_]*)=(?<value>.*)$') { continue }
        $name = $Matches.name
        $value = $Matches.value.Trim()
        if ($values.ContainsKey($name)) { throw "Duplicate environment setting: $name" }
        if (($value.StartsWith('"') -and $value.EndsWith('"')) -or
            ($value.StartsWith("'") -and $value.EndsWith("'"))) {
            if ($value.Length -lt 2) { throw "Invalid environment setting: $name" }
            $value = $value.Substring(1, $value.Length - 2)
        }
        $values[$name] = $value
    }
    return $values
}

function Get-HaiOpenClawWorkerEnvironment {
    param([Parameter(Mandatory = $true)][hashtable]$Values)
    if ($Values.HAI_OPENCLAW_MAINTENANCE_ENABLED -ine 'true') {
        throw 'OpenClaw maintenance is disabled. Configure and deploy the matching HAI backend before starting this worker.'
    }
    $token = [string]$Values.HAI_OPENCLAW_MAINTENANCE_TOKEN
    $key = [string]$Values.BACKEND_API_SHARED_KEY
    foreach ($secret in @($token, $key)) {
        # Literal header-safe secrets avoid dotenv interpolation mismatches.
        if ($secret.Length -lt 32 -or $secret -notmatch '\A[A-Za-z0-9_+/=.-]+\z') {
            throw 'Maintenance and backend credentials must be separate literal random values of at least 32 characters.'
        }
    }
    foreach ($name in @('BACKEND_API_SHARED_KEY', 'HAI_HOST_RUNTIME_BRIDGE_TOKEN', 'OPENCLAW_GATEWAY_TOKEN', 'OPENCLAW_GATEWAY_DELEGATION_TOKEN')) {
        if ([string]::Equals($token, [string]$Values[$name], [StringComparison]::Ordinal)) {
            throw "The maintenance token must not reuse $name."
        }
    }
    $port = 0
    if (-not [int]::TryParse([string]$Values.BACKEND_PORT, [ref]$port) -or $port -lt 1 -or $port -gt 65535) {
        throw 'BACKEND_PORT must identify the loopback backend port (1-65535).'
    }
    $expectedUrl = "http://127.0.0.1:$port"
    if ($Values.ContainsKey('HAI_OPENCLAW_MAINTENANCE_URL') -and
        ([string]$Values.HAI_OPENCLAW_MAINTENANCE_URL).TrimEnd('/') -cne $expectedUrl) {
        throw 'The maintenance URL must match the installed backend: http://127.0.0.1:<BACKEND_PORT>.'
    }
    $pin = [string]$Values.HAI_OPENCLAW_PUBLISHER_THUMBPRINT
    if ($pin -and $pin -notmatch '\A[A-Fa-f0-9]{40}\z') {
        throw 'The Companion publisher thumbprint must be a verified 40-character certificate thumbprint.'
    }
    return @{
        HAI_OPENCLAW_MAINTENANCE_URL = $expectedUrl
        HAI_OPENCLAW_MAINTENANCE_TOKEN = $token
        BACKEND_API_SHARED_KEY = $key
        HAI_OPENCLAW_PUBLISHER_THUMBPRINT = $pin
    }
}

function Write-HaiOpenClawMaintenanceLog {
    param(
        [Parameter(Mandatory = $true)][ValidateSet('info', 'warning', 'error')][string]$Level,
        [Parameter(Mandatory = $true)][string]$Message,
        [string[]]$Secrets = @()
    )
    foreach ($secret in $Secrets) {
        if (-not [string]::IsNullOrEmpty($secret)) {
            $Message = $Message.Replace($secret, '[redacted]')
        }
    }
    $Message = ($Message -replace '[\r\n\t]+', ' ').Trim()
    if ($Message.Length -gt 1500) { $Message = $Message.Substring(0, 1500) }
    try {
        $directory = Join-Path $env:LOCALAPPDATA 'HAI\logs'
        if (-not (Test-Path -LiteralPath $directory -PathType Container)) {
            New-Item -ItemType Directory -Path $directory -Force | Out-Null
        }
        $path = Join-Path $directory 'openclaw-maintenance-launcher.log'
        $line = "{0:o} [{1}] {2}{3}" -f [DateTimeOffset]::Now, $Level.ToUpperInvariant(), $Message, [Environment]::NewLine
        $encoding = New-Object Text.UTF8Encoding($false)
        $mutex = New-Object Threading.Mutex($false, $script:HaiOpenClawMaintenanceLogMutexName)
        $locked = $false
        try {
            try { $locked = $mutex.WaitOne([TimeSpan]::FromSeconds(5)) }
            catch [Threading.AbandonedMutexException] { $locked = $true }
            if (-not $locked) { throw 'Timed out waiting for the local maintenance log lock.' }

            $lineBytes = $encoding.GetByteCount($line)
            $currentBytes = if (Test-Path -LiteralPath $path -PathType Leaf) { (Get-Item -LiteralPath $path).Length } else { 0 }
            if ($currentBytes -gt 0 -and ($currentBytes + $lineBytes) -gt $script:HaiOpenClawMaintenanceLogMaxBytes) {
                $oldestPath = "$path.$script:HaiOpenClawMaintenanceLogBackupCount"
                if (Test-Path -LiteralPath $oldestPath -PathType Leaf) { Remove-Item -LiteralPath $oldestPath -Force }
                for ($index = $script:HaiOpenClawMaintenanceLogBackupCount - 1; $index -ge 1; $index--) {
                    $sourcePath = "$path.$index"
                    if (Test-Path -LiteralPath $sourcePath -PathType Leaf) {
                        Move-Item -LiteralPath $sourcePath -Destination "$path.$($index + 1)" -Force
                    }
                }
                Move-Item -LiteralPath $path -Destination "$path.1" -Force
            }
            [IO.File]::AppendAllText($path, $line, $encoding)
        } finally {
            if ($locked) { $mutex.ReleaseMutex() }
            $mutex.Dispose()
        }
    } catch {
        Write-Warning 'Could not write the local OpenClaw maintenance log.'
    }
}

function Test-HaiOpenClawMaintenanceTaskOwnership {
    param(
        [Parameter(Mandatory = $true)]$Task,
        [Parameter(Mandatory = $true)][string]$PowerShellPath,
        [Parameter(Mandatory = $true)][string]$LauncherPath,
        [Parameter(Mandatory = $true)][string]$EnvFile,
        [Parameter(Mandatory = $true)][string]$UserId
    )
    if ([string]$Task.TaskName -cne $script:HaiOpenClawMaintenanceTaskName -or [string]$Task.TaskPath -cne '\') { return $false }
    if ([string]$Task.Description -cne $script:HaiOpenClawMaintenanceTaskDescription) { return $false }
    if ($null -eq $Task.Principal -or
        -not [string]::Equals([string]$Task.Principal.UserId, $UserId, [StringComparison]::OrdinalIgnoreCase) -or
        -not [string]::IsNullOrWhiteSpace([string]$Task.Principal.GroupId) -or
        [string]$Task.Principal.LogonType -ine 'Interactive' -or
        [string]$Task.Principal.RunLevel -ine 'Limited') { return $false }
    $triggers = @($Task.Triggers)
    if ($triggers.Count -ne 2) { return $false }
    $logonTriggers = @($triggers | Where-Object { [string]$_.CimClass.CimClassName -eq 'MSFT_TaskLogonTrigger' })
    $dailyTriggers = @($triggers | Where-Object { [string]$_.CimClass.CimClassName -eq 'MSFT_TaskDailyTrigger' })
    if ($logonTriggers.Count -ne 1 -or $dailyTriggers.Count -ne 1 -or
        -not [string]::Equals([string]$logonTriggers[0].UserId, $UserId, [StringComparison]::OrdinalIgnoreCase)) { return $false }
    try {
        $dailyStart = [DateTimeOffset]::Parse([string]$dailyTriggers[0].StartBoundary).ToLocalTime()
    } catch { return $false }
    if ($dailyTriggers[0].DaysInterval -ne 1 -or $dailyStart.TimeOfDay -ne [TimeSpan]::FromHours(3) -or
        [string]$dailyTriggers[0].Repetition.Interval -cne 'PT1H' -or
        [string]$dailyTriggers[0].Repetition.Duration -cne 'P1D' -or
        [bool]$dailyTriggers[0].Repetition.StopAtDurationEnd) { return $false }
    $settings = $Task.Settings
    if ($null -eq $settings -or [string]$settings.MultipleInstances -ine 'IgnoreNew' -or
        [string]$settings.ExecutionTimeLimit -cne 'PT1H' -or [int]$settings.RestartCount -ne 3 -or
        [string]$settings.RestartInterval -cne 'PT5M' -or -not [bool]$settings.StartWhenAvailable -or
        [bool]$settings.DisallowStartIfOnBatteries -or [bool]$settings.StopIfGoingOnBatteries) { return $false }
    $actions = @($Task.Actions)
    if ($actions.Count -ne 1) { return $false }
    if ([string]::IsNullOrWhiteSpace([string]$actions[0].WorkingDirectory)) { return $false }
    try {
        $actualWorkingDirectory = [IO.Path]::GetFullPath([string]$actions[0].WorkingDirectory)
    } catch { return $false }
    if (-not [string]::Equals($actualWorkingDirectory, [IO.Path]::GetFullPath($script:HaiOpenClawMaintenanceSupportRoot), [StringComparison]::OrdinalIgnoreCase)) {
        return $false
    }
    $expectedArguments = '-NoProfile -WindowStyle Hidden -ExecutionPolicy Bypass -File "{0}" -EnvFile "{1}" -Once -MaxJobs 3' -f `
        [IO.Path]::GetFullPath($LauncherPath), [IO.Path]::GetFullPath($EnvFile)
    return [string]::Equals([IO.Path]::GetFullPath([string]$actions[0].Execute), [IO.Path]::GetFullPath($PowerShellPath), [StringComparison]::OrdinalIgnoreCase) -and
        [string]::Equals([string]$actions[0].Arguments, $expectedArguments, [StringComparison]::Ordinal)
}

function Get-HaiOpenClawMaintenanceTask {
    try {
        Get-ScheduledTask -TaskName $script:HaiOpenClawMaintenanceTaskName -TaskPath '\' -ErrorAction Stop
    } catch {
        if ([string]$_.FullyQualifiedErrorId -like 'CmdletizationQuery_NotFound,Get-ScheduledTask*') { return $null }
        throw
    }
}

function Enter-HaiOpenClawMaintenanceLock {
    param([ValidateRange(0, 300)][int]$WaitSeconds = 0)
    $mutex = New-Object Threading.Mutex($false, $script:HaiOpenClawMaintenanceMutexName)
    try {
        $locked = $false
        try { $locked = $mutex.WaitOne([TimeSpan]::FromSeconds($WaitSeconds)) } catch [Threading.AbandonedMutexException] { $locked = $true }
        if (-not $locked) {
            throw "OpenClaw maintenance or an HAI installer currently owns the synchronization lock after waiting $WaitSeconds seconds; retry after it completes."
        }
        return $mutex
    } catch {
        $mutex.Dispose()
        throw
    }
}

function Exit-HaiOpenClawMaintenanceLock {
    param($Mutex)
    if ($null -eq $Mutex) { return }
    try { $Mutex.ReleaseMutex() } finally { $Mutex.Dispose() }
}

function Start-HaiOpenClawMaintenanceTask {
    param(
        [string]$EnvFile = (Join-Path $env:LOCALAPPDATA 'HAI\hai.env'),
        [string]$LauncherPath = (Join-Path $script:HaiOpenClawMaintenanceSupportRoot 'Run-HAI-OpenClawMaintenance.ps1'),
        [string]$PowerShellPath = (Join-Path $env:WINDIR 'System32\WindowsPowerShell\v1.0\powershell.exe'),
        [string]$UserId = ([Security.Principal.WindowsIdentity]::GetCurrent().User.Value)
    )
    $task = Get-HaiOpenClawMaintenanceTask
    if ($null -eq $task) { return $false }
    if (-not (Test-HaiOpenClawMaintenanceTaskOwnership -Task $task -PowerShellPath $PowerShellPath `
            -LauncherPath $LauncherPath -EnvFile $EnvFile -UserId $UserId)) {
        throw 'A task with HAI OpenClaw maintenance name exists but does not match this installation; it was left untouched.'
    }
    Start-ScheduledTask -TaskName $script:HaiOpenClawMaintenanceTaskName -TaskPath '\' -ErrorAction Stop
    return $true
}

function Unregister-HaiOpenClawMaintenanceTask {
    param(
        [string]$EnvFile = (Join-Path $env:LOCALAPPDATA 'HAI\hai.env'),
        [string]$LauncherPath = (Join-Path $script:HaiOpenClawMaintenanceSupportRoot 'Run-HAI-OpenClawMaintenance.ps1'),
        [string]$PowerShellPath = (Join-Path $env:WINDIR 'System32\WindowsPowerShell\v1.0\powershell.exe'),
        [string]$UserId = ([Security.Principal.WindowsIdentity]::GetCurrent().User.Value),
        [switch]$SkipSynchronizationLock
    )
    $mutex = $null
    if (-not $SkipSynchronizationLock) { $mutex = Enter-HaiOpenClawMaintenanceLock }
    try {
        $task = Get-HaiOpenClawMaintenanceTask
        if ($null -eq $task) { return $false }
        if (-not (Test-HaiOpenClawMaintenanceTaskOwnership -Task $task -PowerShellPath $PowerShellPath `
                -LauncherPath $LauncherPath -EnvFile $EnvFile -UserId $UserId)) {
            throw 'A task with HAI OpenClaw maintenance name exists but does not match this installation; it was left untouched.'
        }
        Unregister-ScheduledTask -TaskName $script:HaiOpenClawMaintenanceTaskName -TaskPath '\' -Confirm:$false -ErrorAction Stop
        Write-HaiOpenClawMaintenanceLog -Level info -Message 'Removed the HAI-owned scheduled maintenance task; OpenClaw and HAI user data were preserved.'
        return $true
    } finally {
        if ($null -ne $mutex) { Exit-HaiOpenClawMaintenanceLock -Mutex $mutex }
    }
}

function Assert-HaiOpenClawMaintenanceTaskAbsent {
    $task = Get-HaiOpenClawMaintenanceTask
    if ($null -ne $task) {
        throw 'An OpenClaw maintenance task appeared while uninstall was starting; no application files should be removed. Retry uninstall.'
    }
    return $true
}

function Register-HaiOpenClawMaintenanceTask {
    param(
        [string]$EnvFile = (Join-Path $env:LOCALAPPDATA 'HAI\hai.env'),
        [string]$LauncherPath = (Join-Path $script:HaiOpenClawMaintenanceSupportRoot 'Run-HAI-OpenClawMaintenance.ps1'),
        [string]$WorkerPath = (Join-Path $script:HaiOpenClawMaintenanceSupportRoot 'hai-openclaw-maintenance.exe'),
        [string]$PowerShellPath = (Join-Path $env:WINDIR 'System32\WindowsPowerShell\v1.0\powershell.exe'),
        [string]$UserId = ([Security.Principal.WindowsIdentity]::GetCurrent().User.Value),
        [switch]$SkipImmediateRun,
        [switch]$SkipSynchronizationLock
    )
    if ($SkipSynchronizationLock -and -not $SkipImmediateRun) {
        throw 'The maintenance synchronization lock may only be skipped when an immediate run is also suppressed.'
    }
    if (-not (Test-Path -LiteralPath $EnvFile -PathType Leaf)) {
        throw 'HAI environment file is missing; scheduled OpenClaw maintenance was not registered.'
    }
    $values = ConvertFrom-HaiMaintenanceEnvironment -Lines ([IO.File]::ReadAllLines($EnvFile))
    if ($values.HAI_OPENCLAW_MAINTENANCE_ENABLED -ine 'true') {
        [void](Unregister-HaiOpenClawMaintenanceTask -EnvFile $EnvFile -LauncherPath $LauncherPath `
            -PowerShellPath $PowerShellPath -UserId $UserId -SkipSynchronizationLock:$SkipSynchronizationLock)
        Write-HaiOpenClawMaintenanceLog -Level info -Message 'Scheduled maintenance remains disabled because HAI_OPENCLAW_MAINTENANCE_ENABLED is not true.'
        return $false
    }
    [void](Get-HaiOpenClawWorkerEnvironment -Values $values)
    foreach ($requiredPath in @($LauncherPath, $WorkerPath, $PowerShellPath)) {
        if (-not (Test-Path -LiteralPath $requiredPath -PathType Leaf)) {
            throw "Scheduled maintenance prerequisite is missing: $requiredPath"
        }
    }
    if ([string]::IsNullOrWhiteSpace($UserId)) { throw 'Could not identify the interactive Windows account for scheduled maintenance.' }

    $mutex = $null
    if (-not $SkipSynchronizationLock) { $mutex = Enter-HaiOpenClawMaintenanceLock }
    try {
        $existing = Get-HaiOpenClawMaintenanceTask
        if ($null -ne $existing -and -not (Test-HaiOpenClawMaintenanceTaskOwnership -Task $existing `
                -PowerShellPath $PowerShellPath -LauncherPath $LauncherPath -EnvFile $EnvFile -UserId $UserId)) {
            throw 'A task with HAI OpenClaw maintenance name exists but does not match this installation; it was left untouched.'
        }
        $arguments = '-NoProfile -WindowStyle Hidden -ExecutionPolicy Bypass -File "{0}" -EnvFile "{1}" -Once -MaxJobs 3' -f `
            [IO.Path]::GetFullPath($LauncherPath), [IO.Path]::GetFullPath($EnvFile)
        $action = New-ScheduledTaskAction -Execute ([IO.Path]::GetFullPath($PowerShellPath)) -Argument $arguments -WorkingDirectory $script:HaiOpenClawMaintenanceSupportRoot
        $dailyTrigger = New-ScheduledTaskTrigger -Daily -At ([DateTime]::Today.AddHours(3))
        $dailyTrigger.Repetition = New-CimInstance -ClassName MSFT_TaskRepetitionPattern `
            -Namespace 'root/Microsoft/Windows/TaskScheduler' -ClientOnly `
            -Property @{ Interval = 'PT1H'; Duration = 'P1D'; StopAtDurationEnd = $false } -ErrorAction Stop
        $triggers = @((New-ScheduledTaskTrigger -AtLogOn -User $UserId), $dailyTrigger)
        $principal = New-ScheduledTaskPrincipal -UserId $UserId -LogonType Interactive -RunLevel Limited
        $settings = New-ScheduledTaskSettingsSet -MultipleInstances IgnoreNew -ExecutionTimeLimit (New-TimeSpan -Minutes 60) `
            -RestartCount 3 -RestartInterval (New-TimeSpan -Minutes 5) -StartWhenAvailable -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries
        $registrationParameters = @{
            TaskName = $script:HaiOpenClawMaintenanceTaskName
            TaskPath = '\'
            Action = $action
            Trigger = $triggers
            Principal = $principal
            Settings = $settings
            Description = $script:HaiOpenClawMaintenanceTaskDescription
        }
        if ($null -ne $existing) { $registrationParameters.Force = $true }
        Register-ScheduledTask @registrationParameters -ErrorAction Stop | Out-Null
    } finally {
        if ($null -ne $mutex) { Exit-HaiOpenClawMaintenanceLock -Mutex $mutex }
    }
    if (-not $SkipImmediateRun) {
        [void](Start-HaiOpenClawMaintenanceTask -EnvFile $EnvFile -LauncherPath $LauncherPath `
            -PowerShellPath $PowerShellPath -UserId $UserId)
        Write-HaiOpenClawMaintenanceLog -Level info -Message 'Registered and started the least-privileged scheduled OpenClaw maintenance task.'
    } else {
        Write-HaiOpenClawMaintenanceLog -Level info -Message 'Registered the least-privileged scheduled OpenClaw maintenance task; its immediate run was deferred by the installer.'
    }
    return $true
}
