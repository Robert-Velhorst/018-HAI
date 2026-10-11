[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$EnvFile
)

$ErrorActionPreference = "Stop"

if (-not (Test-Path -LiteralPath $EnvFile -PathType Leaf)) {
    throw "HAI host runtime environment file is missing."
}

$values = @{}
foreach ($line in [IO.File]::ReadAllLines($EnvFile)) {
    if ($line -notmatch '^(?<name>[A-Z][A-Z0-9_]*)=(?<value>.*)$') {
        continue
    }
    $values[$Matches.name] = $Matches.value.Trim().Trim("'").Trim('"')
}

$required = @(
    "HAI_HOST_RUNTIME_BRIDGE_URL",
    "HAI_HOST_RUNTIME_BRIDGE_TOKEN",
    "DEEPSEEK_HARNESS_EXECUTABLE",
    "DEEPSEEK_HARNESS_VERSION",
    "DEEPSEEK_HARNESS_WORKSPACE",
    "DEEPSEEK_HARNESS_STATE_DIR",
    "DEEPSEEK_HARNESS_WORKSPACE_KEY",
    "DEEPSEEK_HARNESS_TIMEOUT_SECONDS"
)

$allowlist = @()
if ($values.ContainsKey("DEEPSEEK_HARNESS_ENV_ALLOWLIST")) {
    $allowlist = $values["DEEPSEEK_HARNESS_ENV_ALLOWLIST"].Split(",") |
        ForEach-Object { $_.Trim() } |
        Where-Object { $_ -match '^[A-Za-z][A-Za-z0-9_]*$' }
}

foreach ($name in @($required + $allowlist | Select-Object -Unique)) {
    if (-not $values.ContainsKey($name)) {
        throw "HAI host runtime environment is missing $name."
    }
    Set-Item -Path "Env:$name" -Value $values[$name]
}

$bridge = Join-Path $PSScriptRoot "hai-dsh-bridge.exe"
if (-not (Test-Path -LiteralPath $bridge -PathType Leaf)) {
    throw "HAI host runtime worker is not installed. Reinstall HAI from a complete installer payload."
}

& $bridge
exit $LASTEXITCODE
