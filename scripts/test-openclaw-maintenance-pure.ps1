[CmdletBinding()]
param(
    [string]$SupportPath = ''
)

$ErrorActionPreference = 'Stop'
if ([string]::IsNullOrWhiteSpace($SupportPath)) {
    $SupportPath = Join-Path $PSScriptRoot '../installer/windows/Hai-OpenClawMaintenance.ps1'
}
. $SupportPath
$script:pureChecks = 0

function Assert-HaiMaintenancePure([bool]$Condition, [string]$Case) {
    if (-not $Condition) { throw "Maintenance pure contract failed: $Case" }
    $script:pureChecks++
}

function New-HaiPureSettings {
    return @{
        HAI_OPENCLAW_MAINTENANCE_ENABLED = 'true'
        HAI_OPENCLAW_MAINTENANCE_TOKEN = ('a' * 64)
        BACKEND_API_SHARED_KEY = ('b' * 64)
        BACKEND_PORT = '17070'
        HAI_OPENCLAW_PUBLISHER_THUMBPRINT = ''
    }
}

$settings = New-HaiPureSettings
$worker = Get-HaiOpenClawWorkerEnvironment -Values $settings
Assert-HaiMaintenancePure ($worker.Count -eq 4 -and $worker.HAI_OPENCLAW_MAINTENANCE_URL -ceq 'http://127.0.0.1:17070') 'valid allowlisted environment'
$settings.BACKEND_PORT = '65535'
$settings.HAI_OPENCLAW_PUBLISHER_THUMBPRINT = 'c' * 40
$worker = Get-HaiOpenClawWorkerEnvironment -Values $settings
Assert-HaiMaintenancePure ($worker.HAI_OPENCLAW_MAINTENANCE_URL -ceq 'http://127.0.0.1:65535' -and $worker.HAI_OPENCLAW_PUBLISHER_THUMBPRINT -ceq ('c' * 40)) 'valid port boundary and publisher pin'

foreach ($change in @(
    { param($v) $v.HAI_OPENCLAW_MAINTENANCE_ENABLED = 'false' },
    { param($v) $v.HAI_OPENCLAW_MAINTENANCE_ENABLED = 'yes' },
    { param($v) $v.HAI_OPENCLAW_MAINTENANCE_TOKEN = 'short' },
    { param($v) $v.HAI_OPENCLAW_MAINTENANCE_TOKEN = ('a' * 64) + '$VALUE' },
    { param($v) $v.HAI_OPENCLAW_MAINTENANCE_TOKEN = ('a' * 64) + "`n" },
    { param($v) $v.BACKEND_API_SHARED_KEY = 'short' },
    { param($v) $v.BACKEND_API_SHARED_KEY = $v.HAI_OPENCLAW_MAINTENANCE_TOKEN },
    { param($v) $v.HAI_HOST_RUNTIME_BRIDGE_TOKEN = $v.HAI_OPENCLAW_MAINTENANCE_TOKEN },
    { param($v) $v.OPENCLAW_GATEWAY_TOKEN = $v.HAI_OPENCLAW_MAINTENANCE_TOKEN },
    { param($v) $v.OPENCLAW_GATEWAY_DELEGATION_TOKEN = $v.HAI_OPENCLAW_MAINTENANCE_TOKEN },
    { param($v) $v.BACKEND_PORT = '0' },
    { param($v) $v.BACKEND_PORT = '65536' },
    { param($v) $v.BACKEND_PORT = 'not-a-port' },
    { param($v) $v.HAI_OPENCLAW_MAINTENANCE_URL = 'http://localhost:17070' },
    { param($v) $v.HAI_OPENCLAW_MAINTENANCE_URL = 'http://127.0.0.1:17070/api' },
    { param($v) $v.HAI_OPENCLAW_MAINTENANCE_URL = 'http://user@127.0.0.1:17070' },
    { param($v) $v.HAI_OPENCLAW_MAINTENANCE_URL = 'http://127.0.0.1:17070/?secret=synthetic' },
    { param($v) $v.HAI_OPENCLAW_PUBLISHER_THUMBPRINT = 'not-a-pin' }
)) {
    $values = New-HaiPureSettings
    & $change $values
    $rejected = $false
    try { Get-HaiOpenClawWorkerEnvironment -Values $values | Out-Null } catch { $rejected = $true }
    Assert-HaiMaintenancePure $rejected 'invalid environment rejected'
}

$parsed = ConvertFrom-HaiMaintenanceEnvironment -Lines @('# ignored', 'BACKEND_PORT=17070', 'HAI_OPENCLAW_MAINTENANCE_ENABLED="true"')
Assert-HaiMaintenancePure ($parsed.Count -eq 2 -and $parsed.HAI_OPENCLAW_MAINTENANCE_ENABLED -ceq 'true') 'quoted dotenv value'
$rejected = $false
try { ConvertFrom-HaiMaintenanceEnvironment -Lines @('BACKEND_PORT=17070', 'BACKEND_PORT=17071') | Out-Null } catch { $rejected = $true }
Assert-HaiMaintenancePure $rejected 'duplicate dotenv settings rejected'

# Only the ownership predicate runs: these paths are strings, not files to create.
$powerShellPath = [IO.Path]::GetFullPath((Join-Path $PSHOME 'powershell.exe'))
$launcherPath = [IO.Path]::GetFullPath((Join-Path $script:HaiOpenClawMaintenanceSupportRoot 'Run-HAI-OpenClawMaintenance.ps1'))
$envFile = [IO.Path]::GetFullPath((Join-Path $script:HaiOpenClawMaintenanceSupportRoot 'synthetic-not-created.env'))
$userId = 'OFFLINE\TestUser'
$expectedArguments = '-NoProfile -WindowStyle Hidden -ExecutionPolicy Bypass -File "{0}" -EnvFile "{1}" -Once -MaxJobs 3' -f $launcherPath, $envFile
$ownershipArguments = @{ PowerShellPath = $powerShellPath; LauncherPath = $launcherPath; EnvFile = $envFile; UserId = $userId }

function New-HaiPureTask {
    return [pscustomobject]@{
        TaskName = $script:HaiOpenClawMaintenanceTaskName
        TaskPath = '\'
        Description = $script:HaiOpenClawMaintenanceTaskDescription
        Principal = [pscustomobject]@{ UserId = $userId; GroupId = ''; LogonType = 'Interactive'; RunLevel = 'Limited' }
        Triggers = @(
            [pscustomobject]@{ CimClass = [pscustomobject]@{ CimClassName = 'MSFT_TaskLogonTrigger' }; UserId = $userId },
            [pscustomobject]@{
                CimClass = [pscustomobject]@{ CimClassName = 'MSFT_TaskDailyTrigger' }
                StartBoundary = [DateTime]::Today.AddHours(3).ToString('o')
                DaysInterval = 1
                Repetition = [pscustomobject]@{ Interval = 'PT1H'; Duration = 'P1D'; StopAtDurationEnd = $false }
            }
        )
        Settings = [pscustomobject]@{
            MultipleInstances = 'IgnoreNew'; ExecutionTimeLimit = 'PT1H'; RestartCount = 3; RestartInterval = 'PT5M'
            StartWhenAvailable = $true; DisallowStartIfOnBatteries = $false; StopIfGoingOnBatteries = $false
        }
        Actions = @([pscustomobject]@{ Execute = $powerShellPath; Arguments = $expectedArguments; WorkingDirectory = $script:HaiOpenClawMaintenanceSupportRoot })
    }
}

Assert-HaiMaintenancePure (Test-HaiOpenClawMaintenanceTaskOwnership -Task (New-HaiPureTask) @ownershipArguments) 'exact owned task'
foreach ($change in @(
    { param($t) $t.TaskName = 'Another task' },
    { param($t) $t.TaskPath = '\foreign\' },
    { param($t) $t.Description = 'Unrelated task' },
    { param($t) $t.Principal.UserId = 'OFFLINE\OtherUser' },
    { param($t) $t.Principal.GroupId = 'OFFLINE\Group' },
    { param($t) $t.Principal.LogonType = 'ServiceAccount' },
    { param($t) $t.Principal.RunLevel = 'Highest' },
    { param($t) $t.Triggers[0].UserId = 'OFFLINE\OtherUser' },
    { param($t) $t.Triggers[1].DaysInterval = 2 },
    { param($t) $t.Triggers[1].Repetition.Interval = 'PT2H' },
    { param($t) $t.Triggers[1].Repetition.Duration = 'P2D' },
    { param($t) $t.Triggers[1].Repetition.StopAtDurationEnd = $true },
    { param($t) $t.Settings.MultipleInstances = 'Parallel' },
    { param($t) $t.Settings.RestartCount = 0 },
    { param($t) $t.Settings.ExecutionTimeLimit = 'PT2H' },
    { param($t) $t.Actions[0].Arguments += ' -Unexpected' },
    { param($t) $t.Actions[0].WorkingDirectory = $PSHOME },
    { param($t) $t.Actions += $t.Actions[0] }
)) {
    $task = New-HaiPureTask
    & $change $task
    Assert-HaiMaintenancePure (-not (Test-HaiOpenClawMaintenanceTaskOwnership -Task $task @ownershipArguments)) 'modified task rejected'
}

Write-Output "OpenClaw pure maintenance contracts passed: $script:pureChecks checks; no files, tasks, logs, locks, workers or network changed."
