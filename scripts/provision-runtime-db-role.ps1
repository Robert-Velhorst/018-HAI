[CmdletBinding()]
param(
    [string]$EnvFile = ".env.local"
)

$ErrorActionPreference = "Stop"

$repositoryRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot "..")).Path
if (-not [IO.Path]::IsPathRooted($EnvFile)) {
    $EnvFile = Join-Path $repositoryRoot $EnvFile
}
$EnvFile = [IO.Path]::GetFullPath($EnvFile)
if (-not (Test-Path -LiteralPath $EnvFile -PathType Leaf)) {
    throw "Environment file not found: $EnvFile"
}

function Get-DotEnvValue {
    param([string]$Content, [string]$Name)
    $match = [Regex]::Match($Content, "(?m)^" + [Regex]::Escape($Name) + "=(.*)$")
    if (-not $match.Success) { throw "Missing $Name in $EnvFile" }
    return $match.Groups[1].Value.Trim().Trim("'").Trim('"')
}

$content = [IO.File]::ReadAllText($EnvFile)
$owner = Get-DotEnvValue $content "DB_USER"
$ownerPassword = Get-DotEnvValue $content "DB_PASSWORD"
$role = Get-DotEnvValue $content "BACKEND_DB_USER"
$password = Get-DotEnvValue $content "BACKEND_DB_PASSWORD"
$database = Get-DotEnvValue $content "AUTOMATION_DB_NAME"

if ($owner -notmatch '^[A-Za-z_][A-Za-z0-9_]{0,62}$' -or
    $role -notmatch '^[A-Za-z_][A-Za-z0-9_]{0,62}$' -or
    $database -notmatch '^[A-Za-z_][A-Za-z0-9_]{0,62}$') {
    throw "DB_USER, BACKEND_DB_USER, and AUTOMATION_DB_NAME must be safe PostgreSQL identifiers."
}
if ([string]::IsNullOrWhiteSpace($ownerPassword) -or [string]::IsNullOrWhiteSpace($password) -or
    $ownerPassword -like 'change-this-*' -or $password -like 'change-this-*' -or
    $password -ceq $ownerPassword) {
    throw "DB_PASSWORD and BACKEND_DB_PASSWORD must be real secrets, and BACKEND_DB_PASSWORD must be distinct."
}
if ($role -eq $owner) {
    throw "BACKEND_DB_USER must differ from DB_USER before provisioning a least-privilege runtime role."
}

Push-Location $repositoryRoot
try {
    & docker compose --env-file $EnvFile -f docker-compose.local.yml up -d --wait postgres-automation | Out-Host
    if ($LASTEXITCODE -ne 0) { throw "Could not start postgres-automation." }
    & docker compose --env-file $EnvFile -f docker-compose.local.yml run --rm --no-deps backend-runtime-role | Out-Host
    if ($LASTEXITCODE -ne 0) { throw "Could not provision the runtime database role." }
} finally {
    Pop-Location
}

Write-Host "Provisioned DML-only runtime role '$role'. Application objects in schema public must be created with DB_USER; unexpected PUBLIC access or relevant defaults from another creator stop provisioning."
Write-Host "Set DB_MIGRATIONS_ENABLED=false before restarting the backend."
