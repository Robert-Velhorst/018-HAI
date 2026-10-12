[CmdletBinding()]
param(
    [string]$EnvFile = (Join-Path $env:LOCALAPPDATA 'HAI\hai.env'),
    [switch]$ValidateOnly,
    [switch]$Once,
    [ValidateRange(0, 300)][int]$LockWaitSeconds = 300,
    [ValidateRange(1, 8)][int]$MaxJobs = 1
)
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'Hai-OpenClawMaintenance.ps1')
$mutex = $null
$held = $false
$previous = @{}
$settings = @{}
$secrets = @()
$exitCode = 0
try {
    if (-not (Test-Path -LiteralPath $EnvFile -PathType Leaf)) {
        throw 'HAI environment file is missing. Initialize HAI before configuring maintenance.'
    }
    $settings = ConvertFrom-HaiMaintenanceEnvironment -Lines ([IO.File]::ReadAllLines($EnvFile))
    $workerEnvironment = Get-HaiOpenClawWorkerEnvironment -Values $settings
    if ($ValidateOnly) {
        Write-Host 'OpenClaw maintenance configuration is valid. No connection or installation was attempted.'
        return
    }
    $binary = Join-Path $PSScriptRoot 'hai-openclaw-maintenance.exe'
    if (-not (Test-Path -LiteralPath $binary -PathType Leaf)) {
        throw 'OpenClaw maintenance worker is not bundled. Install HAI from a complete release payload.'
    }
    $payloadValidationScript = Join-Path $PSScriptRoot 'Hai-WindowsExecutable.ps1'
    if (-not (Test-Path -LiteralPath $payloadValidationScript -PathType Leaf)) {
        throw 'OpenClaw maintenance worker validation support is missing; the worker was not started.'
    }
    . $payloadValidationScript
    # Wait briefly for a worker or installer already in its protected critical section.
    $mutex = Enter-HaiOpenClawMaintenanceLock -WaitSeconds $LockWaitSeconds
    $held = $true
    foreach ($name in $workerEnvironment.Keys) {
        $previous[$name] = [Environment]::GetEnvironmentVariable($name, 'Process')
        [Environment]::SetEnvironmentVariable($name, $workerEnvironment[$name], 'Process')
    }
    $secrets = @($workerEnvironment.HAI_OPENCLAW_MAINTENANCE_TOKEN, $workerEnvironment.BACKEND_API_SHARED_KEY)
    Test-HaiWindowsExecutablePayload -Path $binary | Out-Null
    if ($Once) {
        Write-HaiOpenClawMaintenanceLog -Level info -Message "Scheduled OpenClaw maintenance batch started (maximum $MaxJobs jobs)."
        for ($index = 0; $index -lt $MaxJobs; $index++) {
            & $binary '--once'
            $workerExitCode = $LASTEXITCODE
            if ($workerExitCode -ne 0) {
                throw "OpenClaw maintenance worker exited with code $workerExitCode. Check HAI Background Operations and the local log."
            }
        }
        Write-HaiOpenClawMaintenanceLog -Level info -Message "Scheduled OpenClaw maintenance batch completed ($MaxJobs bounded poll(s))."
    } else {
        Write-Host 'OpenClaw maintenance is polling HAI. Installation remains governed by HAI policy.'
        Write-HaiOpenClawMaintenanceLog -Level info -Message 'Interactive OpenClaw maintenance worker started.'
        & $binary
        $workerExitCode = $LASTEXITCODE
        if ($workerExitCode -ne 0) {
            throw "OpenClaw maintenance worker exited with code $workerExitCode. Check HAI Background Operations and the local log."
        }
    }
} catch {
    $exitCode = 1
    Write-HaiOpenClawMaintenanceLog -Level error -Message ("Launcher failed: " + $_.Exception.Message) -Secrets $secrets
    [Console]::Error.WriteLine('OpenClaw maintenance failed. Check HAI Background Operations and the local maintenance log.')
} finally {
    try {
        foreach ($name in $previous.Keys) {
            [Environment]::SetEnvironmentVariable($name, $previous[$name], 'Process')
        }
    } finally {
        if ($held -and $null -ne $mutex) { $mutex.ReleaseMutex() }
        if ($null -ne $mutex) { $mutex.Dispose() }
    }
}
if ($exitCode -ne 0) { exit $exitCode }
