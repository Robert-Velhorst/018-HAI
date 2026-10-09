[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][ValidateSet('Register', 'Unregister', 'AssertAbsent')][string]$Action,
    [string]$EnvFile = (Join-Path $env:LOCALAPPDATA 'HAI\hai.env'),
    [switch]$SkipImmediateRun,
    [switch]$RestoreAfterCancelledUninstall
)

$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'Hai-OpenClawMaintenance.ps1')

try {
    if ($RestoreAfterCancelledUninstall -and ($Action -ne 'Register' -or -not $SkipImmediateRun)) {
        throw 'Cancelled-uninstall recovery may only register the task without starting a maintenance run.'
    }
    if ($Action -eq 'Register') {
        [void](Register-HaiOpenClawMaintenanceTask -EnvFile $EnvFile -SkipImmediateRun:$SkipImmediateRun `
            -SkipSynchronizationLock:$RestoreAfterCancelledUninstall)
    } elseif ($Action -eq 'Unregister') {
        [void](Unregister-HaiOpenClawMaintenanceTask -EnvFile $EnvFile)
    } else {
        [void](Assert-HaiOpenClawMaintenanceTaskAbsent)
    }
} catch {
    $failure = $_
    $secrets = @()
    if (Test-Path -LiteralPath $EnvFile -PathType Leaf) {
        try {
            $values = ConvertFrom-HaiMaintenanceEnvironment -Lines ([IO.File]::ReadAllLines($EnvFile))
            $secrets = @($values.HAI_OPENCLAW_MAINTENANCE_TOKEN, $values.BACKEND_API_SHARED_KEY)
        } catch { }
    }
    Write-HaiOpenClawMaintenanceLog -Level error -Message ("$Action scheduled task operation failed: " + $failure.Exception.Message) -Secrets $secrets
    throw
}
