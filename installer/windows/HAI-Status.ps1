[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"
. (Join-Path $PSScriptRoot "Hai-InstallerSupport.ps1")

if (-not (Test-HaiEnvironmentFileAvailable)) {
    $guidance = Get-HaiMissingEnvironmentGuidance
    Write-Host $guidance.Summary -ForegroundColor Yellow
    try {
        Assert-HaiExistingDataRequiresEnvironment
        Write-Host "Installation diagnosis: first-run candidate. The read-only inventory found no HAI data volumes covered by the known project and volume-name checks." -ForegroundColor Green
        Write-Host $guidance.FirstRun
    } catch {
        $inventoryFailure = $_.Exception.Message
        if ($inventoryFailure -match '^HAI has existing data volumes') {
            Write-Host "Installation diagnosis: recovery required. Existing HAI data volumes were detected by read-only inventory." -ForegroundColor Red
            Write-Host $guidance.Recovery -ForegroundColor Yellow
        } else {
            Write-Host "Installation diagnosis: unknown. The read-only HAI data inventory could not be completed ($inventoryFailure). Treat this as recovery, not first run, until the data state is verified." -ForegroundColor Yellow
            Write-Host $guidance.Recovery -ForegroundColor Yellow
        }
    }
    Write-Host "Environment-only recovery command (run in PowerShell; replace the backup placeholder with your verified v3 backup folder):" -ForegroundColor Cyan
    Write-Host $guidance.Command
    Write-Host "Details: $($guidance.Documentation)"
    Write-Host "Only read-only Docker volume inventory was attempted; no Docker services or containers were started, and no credentials or volumes were changed." -ForegroundColor Green
    return
}

Assert-HaiDockerReady
$composeArguments = Get-HaiComposeArguments
& docker @composeArguments ps
if ($LASTEXITCODE -ne 0) {
    throw "Could not read HAI container status."
}

$report = Get-HaiStackHealthReport -ProbeTimeoutSeconds 5
$checks = @(
    $report.Backend,
    $report.IdentityProvider,
    $report.FrontendLogin,
    $report.FrontendDashboard,
    $report.ProtectedApiRoute
)
foreach ($check in $checks) {
    $color = if ($check.State -eq 'healthy') { 'Green' } elseif ($check.State -eq 'sign_in_required') { 'Yellow' } else { 'Red' }
    Write-Host "$($check.Name): $($check.State) ($($check.Detail)) [$($check.Uri)]" -ForegroundColor $color
}

Write-Host "Authenticated API session: $($report.AuthenticatedApi.State) - $($report.AuthenticatedApi.Detail)" -ForegroundColor Yellow
if (-not $report.Ready) {
    Write-Host "Overall local runtime: not ready ($($report.Component): $($report.Reason))." -ForegroundColor Red
} else {
    Write-Host "Overall local runtime: core services and frontend shells are ready; authenticated data access still requires sign-in verification." -ForegroundColor Green
}
Write-Host "Optional host runtime worker: $(Get-HaiHostRuntimeWorkerStatus)"
