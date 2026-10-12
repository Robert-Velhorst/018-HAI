[CmdletBinding()]
param(
    [string]$EnvFile = ".env.local"
)

$ErrorActionPreference = "Stop"

$repositoryRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot "..")).Path
$envPath = if ([IO.Path]::IsPathRooted($EnvFile)) { $EnvFile } else { Join-Path $repositoryRoot $EnvFile }
if (-not (Test-Path -LiteralPath $envPath -PathType Leaf)) {
    throw "Environment file not found: $envPath"
}
if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
    throw "Docker Desktop is required to run the isolated Trello acceptance test."
}

function Get-DotEnvValue {
    param([string]$Name, [string[]]$Lines)

    $pattern = '^\s*' + [Regex]::Escape($Name) + '\s*=\s*(.*)\s*$'
    foreach ($line in $Lines) {
        if ($line -match $pattern) {
            $value = $Matches[1].Trim()
            if ($value.Length -ge 2 -and (($value.StartsWith('"') -and $value.EndsWith('"')) -or ($value.StartsWith("'") -and $value.EndsWith("'")))) {
                return $value.Substring(1, $value.Length - 2)
            }
            return $value
        }
    }
    return ""
}

$lines = [IO.File]::ReadAllLines($envPath)
$required = @("TRELLO_API_KEY", "TRELLO_READ_TOKEN", "TRELLO_LIVE_BOARD")
$optional = @("TRELLO_API_BASE_URL", "CONNECTED_SOURCE_HTTP_ALLOWED_HOSTS", "CONNECTED_SOURCE_HTTP_ALLOW_LINK_LOCAL")
$values = @{}
foreach ($name in $required + $optional) {
    $values[$name] = Get-DotEnvValue -Name $name -Lines $lines
}
foreach ($name in $required) {
    $value = $values[$name]
    if ([string]::IsNullOrWhiteSpace($value) -or $value -match '(?i)change-this|replace|example|placeholder') {
        throw "$name must be set to a non-placeholder value in $envPath."
    }
}

$backendPath = Join-Path $repositoryRoot "backend"
$previous = @{}
foreach ($name in $values.Keys) {
    $previous[$name] = [Environment]::GetEnvironmentVariable($name, "Process")
    if ([string]::IsNullOrWhiteSpace($values[$name])) {
        Remove-Item "Env:$name" -ErrorAction SilentlyContinue
    } else {
        Set-Item "Env:$name" $values[$name]
    }
}

try {
    # The test image can reach Trello, but the repository is mounted read-only
    # and only the three read-only live-test functions are eligible to run.
    $output = & docker run --rm `
        -e TRELLO_API_KEY `
        -e TRELLO_READ_TOKEN `
        -e TRELLO_LIVE_BOARD `
        -e TRELLO_API_BASE_URL `
        -e CONNECTED_SOURCE_HTTP_ALLOWED_HOSTS `
        -e CONNECTED_SOURCE_HTTP_ALLOW_LINK_LOCAL `
        -v "${backendPath}:/workspace:ro" `
        -w /workspace `
        golang:1.25.13 `
        go test -count=1 -tags live ./internal/source -run '^TestLiveTrello' -v 2>&1
    $exitCode = $LASTEXITCODE
} finally {
    foreach ($name in $previous.Keys) {
        if ($null -eq $previous[$name]) {
            Remove-Item "Env:$name" -ErrorAction SilentlyContinue
        } else {
            Set-Item "Env:$name" $previous[$name]
        }
    }
}

$output | ForEach-Object { Write-Host $_ }
if ($exitCode -ne 0) {
    throw "Trello live acceptance tests failed. No Trello data was changed."
}
foreach ($testName in @(
    "TestLiveTrelloSyncAgainstRealBoard",
    "TestLiveTrelloIncrementalSyncSkipsUnchanged",
    "TestLiveTrelloTokenIsReadOnly"
)) {
    if (-not ($output -match "--- PASS: $testName")) {
        throw "$testName did not pass. A skipped or incomplete run is not live-connector evidence."
    }
}

Write-Host "Trello live acceptance passed: board sync, incremental cursor, and read-only token scope verified."
