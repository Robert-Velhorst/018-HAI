[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"
. (Join-Path $PSScriptRoot "Hai-InstallerSupport.ps1")

$hostRuntimeStopFailure = $null
try {
    Stop-HaiHostRuntimeWorker
} catch {
    $hostRuntimeStopFailure = $_.Exception.Message
}
$dockerPreflightFailure = $null
try {
    Assert-HaiDockerReady
    Assert-HaiSingleInstallation
} catch {
    $dockerPreflightFailure = $_.Exception.Message
}
if ($null -ne $dockerPreflightFailure) {
    if ($null -ne $hostRuntimeStopFailure) {
        throw "Docker could not be verified, so the Compose stack was not changed: $dockerPreflightFailure The optional host runtime also could not be confirmed stopped: $hostRuntimeStopFailure"
    }
    throw $dockerPreflightFailure
}
$environmentFile = Get-HaiEnvironmentFile
if (-not (Test-Path -LiteralPath $environmentFile -PathType Leaf)) {
    if ($null -ne $hostRuntimeStopFailure) {
        throw "HAI has not been initialized, and its optional host runtime could not be confirmed stopped: $hostRuntimeStopFailure"
    }
    Write-Host "HAI has not been started from this installation yet."
    exit 0
}

$composeArguments = Get-HaiComposeArguments
$environmentOverrides = Suspend-HaiComposeA2AOverrides
try {
    & docker @composeArguments --profile local-a2a stop
    $composeStopExitCode = $LASTEXITCODE
} finally {
    Restore-HaiComposeA2AOverrides -Overrides $environmentOverrides
}
if ($composeStopExitCode -ne 0) {
    if ($null -ne $hostRuntimeStopFailure) {
        throw "HAI could not be stopped cleanly, and the optional host runtime also needs attention. Docker error: $composeStopExitCode. Host runtime: $hostRuntimeStopFailure"
    }
    throw "HAI could not be stopped cleanly. Open Docker Desktop and inspect the 018-hai containers."
}
if ($null -ne $hostRuntimeStopFailure) {
    throw "The HAI Docker stack is stopped, but the optional host runtime could not be confirmed stopped: $hostRuntimeStopFailure"
}
Write-Host "HAI is stopped. Its Docker volumes and local settings were preserved."
