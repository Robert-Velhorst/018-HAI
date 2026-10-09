[CmdletBinding()]
param(
    [ValidateRange(1, 65535)]
    [int]$GatewayPort = 8088,
    [ValidateRange(30, 900)]
    [int]$HealthTimeoutSeconds = 600,
    [switch]$EnableA2ABridge,
    [switch]$EnableHostRuntime,
    [switch]$NoBrowser,
    [switch]$PauseOnError
)

$ErrorActionPreference = "Stop"
. (Join-Path $PSScriptRoot "Hai-InstallerSupport.ps1")

try {
if ($EnableHostRuntime) {
    Assert-HaiHostRuntimeConfigured
}

$environmentAvailable = Test-HaiEnvironmentFileAvailable
if ($environmentAvailable) {
    Assert-HaiDockerReady
} else {
    try {
        Assert-HaiDockerReady
    } catch {
        throw "HAI's protected hai.env is missing, and Docker could not perform the read-only check for existing data volumes ($($_.Exception.Message)). Starting Docker Desktop and retrying only performs that check; it cannot recover credentials. If HAI volumes exist, restore the original protected hai.env from a matching backup. No credentials, containers, or volumes were changed."
    }
    Assert-HaiExistingDataRequiresEnvironment
}
Assert-HaiSingleInstallation

if ($PSBoundParameters.ContainsKey('EnableA2ABridge')) {
    Initialize-HaiLocalEnvironment -GatewayPort $GatewayPort -EnableA2ABridge:$EnableA2ABridge
} else {
    Initialize-HaiLocalEnvironment -GatewayPort $GatewayPort
}
Assert-HaiRequiredEnvironment

# Do not touch host workers until ownership and environment checks pass.
if ($EnableHostRuntime) {
    Start-HaiHostRuntimeWorker
}
Stop-HaiHostRuntimeWorkerIfPresent

# Initialization may migrate the project setting. Check the identity that the
# following Compose commands will actually use before changing containers.
Assert-HaiSingleInstallation
$composeArguments = Get-HaiComposeArguments
Write-Host "Starting the local HAI stack. The first run downloads and builds its containers." -ForegroundColor Cyan
Start-HaiComposeStack -ComposeArguments $composeArguments -HealthTimeoutSeconds $HealthTimeoutSeconds
try {
    . (Join-Path $PSScriptRoot "Hai-OpenClawMaintenance.ps1")
    if (Register-HaiOpenClawMaintenanceTask -EnvFile (Join-Path $env:LOCALAPPDATA "HAI\hai.env")) {
        Write-Host "HAI's verified OpenClaw maintenance check is scheduled for logon and daily operation." -ForegroundColor Green
    }
} catch {
    $failure = $_
    $maintenanceSecrets = @()
    if (Get-Command Write-HaiOpenClawMaintenanceLog -ErrorAction SilentlyContinue) {
        try {
            $maintenanceValues = ConvertFrom-HaiMaintenanceEnvironment -Lines ([IO.File]::ReadAllLines((Join-Path $env:LOCALAPPDATA "HAI\hai.env")))
            $maintenanceSecrets = @($maintenanceValues.HAI_OPENCLAW_MAINTENANCE_TOKEN, $maintenanceValues.BACKEND_API_SHARED_KEY)
        } catch { }
        Write-HaiOpenClawMaintenanceLog -Level error -Message ("Task setup or immediate start failed: " + $failure.Exception.Message) -Secrets $maintenanceSecrets
    } else {
        Write-Warning "OpenClaw maintenance support could not be loaded; a local maintenance log is unavailable."
    }
    Write-Warning "HAI is ready, but OpenClaw maintenance setup or its immediate start did not complete."
}
$url = "$(Get-HaiUrl)/control-center"
Write-Host "HAI is ready at $url" -ForegroundColor Green
if (-not $NoBrowser) {
    Start-Process -FilePath $url
}
} catch {
    if (-not $PauseOnError) { throw }
    Write-Host "Start HAI failed: $($_.Exception.Message)" -ForegroundColor Red
    [void](Read-Host "Press Enter to close this window")
    exit 1
}
