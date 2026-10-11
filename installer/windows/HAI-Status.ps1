[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"
. (Join-Path $PSScriptRoot "Hai-InstallerSupport.ps1")

Assert-HaiDockerReady
$environmentFile = Get-HaiEnvironmentFile
if (-not (Test-Path -LiteralPath $environmentFile -PathType Leaf)) {
    Write-Host "HAI is not initialized. Use Start HAI to create the local owner account and start the stack."
    exit 0
}

$composeArguments = Get-HaiComposeArguments
& docker @composeArguments ps
if ($LASTEXITCODE -ne 0) {
    throw "Could not read HAI container status."
}

$url = "$(Get-HaiUrl)/readyz"
try {
    $response = Invoke-WebRequest -UseBasicParsing -Uri $url -TimeoutSec 5
    Write-Host "Gateway readiness: HTTP $($response.StatusCode) at $url" -ForegroundColor Green
} catch {
    Write-Host "Gateway readiness: unavailable at $url" -ForegroundColor Yellow
}
