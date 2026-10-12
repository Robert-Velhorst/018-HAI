[CmdletBinding(DefaultParameterSetName = 'Start')]
param(
    [Parameter(ParameterSetName = 'Start')]
    [switch]$ValidateOnly,

    [Parameter(ParameterSetName = 'Stop', Mandatory = $true)]
    [switch]$Stop
)

$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'Hai-InstallerSupport.ps1')

Assert-HaiDockerReady
Assert-HaiSingleInstallation
Assert-HaiLocalEnvironment

$cloudAccessScript = Join-Path (Get-HaiInstallRoot) 'scripts\start-ngrok.ps1'
if (-not (Test-Path -LiteralPath $cloudAccessScript -PathType Leaf)) {
    throw "HAI cloud access is unavailable because the installed ngrok script is missing: $cloudAccessScript"
}

$arguments = @(
    '-EnvFile', (Get-HaiEnvironmentFile),
    '-ComposeFile', (Get-HaiComposeFile)
)
if ($ValidateOnly) {
    $arguments += '-ValidateOnly'
}
if ($Stop) {
    $arguments += '-Stop'
}

& $cloudAccessScript @arguments
if ($LASTEXITCODE -ne 0) {
    throw "Governed HAI cloud access failed with exit code $LASTEXITCODE. No public access was enabled."
}
