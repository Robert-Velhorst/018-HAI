[CmdletBinding()]
param(
    [switch]$PauseOnError
)

$ErrorActionPreference = "Stop"
. (Join-Path $PSScriptRoot "Hai-InstallerSupport.ps1")

try {
    if (-not (Test-HaiEnvironmentFileAvailable)) {
        $guidance = Get-HaiMissingEnvironmentGuidance
        throw "$($guidance.Summary) If existing HAI data must be preserved, restore the original protected environment file from a completed version-3 backup as the same Windows user/profile. Do not generate replacement credentials or remove volumes. Command: $($guidance.Command). See $($guidance.Documentation). No Docker commands ran; no containers or data were changed."
    }

    $result = Get-HaiStackReadinessResult
    if (-not $result.Ready) {
        throw "HAI cannot open the dashboard: $($result.Component) readiness failed ($($result.Reason)). Use Start HAI and follow any recovery message before opening the dashboard. No containers or data were changed."
    }

    $gatewayUrl = Get-HaiUrl
    $dashboardUrl = "$gatewayUrl/control-center"
    Start-Process -FilePath $dashboardUrl -ErrorAction Stop
} catch {
    if (-not $PauseOnError) { throw }

    Write-Host "Open local dashboard failed: $($_.Exception.Message)" -ForegroundColor Red
    [void](Read-Host "Press Enter to close this window")
    exit 1
}
