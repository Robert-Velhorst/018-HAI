[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"
. (Join-Path $PSScriptRoot "Hai-InstallerSupport.ps1")

Assert-HaiDockerReady
$environmentFile = Get-HaiEnvironmentFile
if (-not (Test-Path -LiteralPath $environmentFile -PathType Leaf)) {
    Write-Host "HAI has not been started from this installation yet."
    exit 0
}

$composeArguments = Get-HaiComposeArguments
& docker @composeArguments stop
if ($LASTEXITCODE -ne 0) {
    throw "HAI could not be stopped cleanly. Open Docker Desktop and inspect the 018-hai containers."
}
Write-Host "HAI is stopped. Its Docker volumes and local settings were preserved."
