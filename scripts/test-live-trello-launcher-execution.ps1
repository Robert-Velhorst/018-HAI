[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$root = Join-Path ([IO.Path]::GetTempPath()) ('hai-trello-launcher-' + [Guid]::NewGuid().ToString('N'))
$owned = $false
$names = @('TRELLO_API_KEY', 'TRELLO_READ_TOKEN', 'TRELLO_ACCOUNT_OWNER_IDENTITY', 'TRELLO_ACCOUNT_MEMBER_ID', 'TRELLO_LIVE_BOARD', 'TRELLO_API_BASE_URL', 'CONNECTED_SOURCE_HTTP_ALLOWED_HOSTS', 'CONNECTED_SOURCE_HTTP_ALLOW_LINK_LOCAL')
$saved = @{}
$savedExitCode = $global:LASTEXITCODE
$mockState = [pscustomobject]@{ Calls = 0; Scenario = '' }
foreach ($name in $names) { $saved[$name] = [Environment]::GetEnvironmentVariable($name, 'Process') }

function docker {
    $mockState.Calls++
    $arguments = @($args)
    foreach ($required in @('TRELLO_ACCOUNT_OWNER_IDENTITY', 'TRELLO_ACCOUNT_MEMBER_ID')) {
        if ($arguments -notcontains $required -or [string]::IsNullOrWhiteSpace([Environment]::GetEnvironmentVariable($required, 'Process'))) {
            throw 'Mock Docker did not receive account binding'
        }
    }
    if ($arguments -notcontains 'golang:1.27.3') { throw 'Live launcher uses the wrong Go toolchain' }
    if ($env:TRELLO_API_BASE_URL -ne 'https://api.trello.com' -or $env:CONNECTED_SOURCE_HTTP_ALLOWED_HOSTS -ne 'api.trello.com' -or $env:CONNECTED_SOURCE_HTTP_ALLOW_LINK_LOCAL -ne 'false') {
        throw 'Live evidence must use the real provider boundary'
    }
    $global:LASTEXITCODE = if ($mockState.Scenario -eq 'process failure') { 1 } else { 0 }
    foreach ($test in @('TestLiveTrelloSyncAgainstRealBoard', 'TestLiveTrelloIncrementalSyncSkipsUnchanged', 'TestLiveTrelloTokenIsReadOnly')) {
        if ($mockState.Scenario -eq 'skipped test' -and $test -eq 'TestLiveTrelloTokenIsReadOnly') { continue }
        Write-Output "--- PASS: $test (0.01s)"
    }
}

try {
    $savedTemp = [Environment]::GetEnvironmentVariable('TEMP', 'Process')
    try {
        [Environment]::SetEnvironmentVariable('TEMP', $null, 'Process')
        & (Join-Path $PSScriptRoot 'test-live-trello-launcher.ps1')
        if ($mockState.Calls -ne 0) { throw 'Missing-environment contract reached Docker' }
        Write-Host 'PASS: Trello launcher contract uses platform temp path without TEMP'
    } finally {
        [Environment]::SetEnvironmentVariable('TEMP', $savedTemp, 'Process')
    }
    if (Test-Path -LiteralPath $root) { throw 'Fixture path already exists' }
    [IO.Directory]::CreateDirectory($root) | Out-Null
    $owned = $true
    [IO.Directory]::CreateDirectory((Join-Path $root 'scripts')) | Out-Null
    [IO.Directory]::CreateDirectory((Join-Path $root 'backend')) | Out-Null
    $launcher = Join-Path $root 'scripts/test-live-trello.ps1'
    Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'test-live-trello.ps1') -Destination $launcher
    $envPath = Join-Path $root 'fixture.env'
    foreach ($case in @('valid', 'missing owner', 'invalid member', 'simulated endpoint', 'skipped test', 'process failure')) {
        $mockState.Scenario = $case
        $mockState.Calls = 0
        $values = [ordered]@{
            TRELLO_API_KEY = 'synthetic-key'
            TRELLO_READ_TOKEN = 'synthetic-read-token'
            TRELLO_ACCOUNT_OWNER_IDENTITY = 'synthetic-owner'
            TRELLO_ACCOUNT_MEMBER_ID = 'aaaaaaaaaaaaaaaaaaaaaaaa'
            TRELLO_LIVE_BOARD = 'ABCDEFGH'
        }
        if ($case -eq 'missing owner') { $values.Remove('TRELLO_ACCOUNT_OWNER_IDENTITY') }
        if ($case -eq 'invalid member') { $values.TRELLO_ACCOUNT_MEMBER_ID = 'invalid' }
        if ($case -eq 'simulated endpoint') { $values['TRELLO_API_BASE_URL'] = 'http://127.0.0.1:1234' }
        [IO.File]::WriteAllLines($envPath, @($values.Keys | ForEach-Object { "$_=$($values[$_])" }))
        $failure = $null
        try { & $launcher -EnvFile $envPath 6>$null | Out-Null } catch { $failure = $_ }
        if ($case -eq 'valid') {
            if ($failure -or $mockState.Calls -ne 1) { throw "Valid mock launcher failed: $failure" }
        } else {
            if (-not $failure) { throw "Launcher accepted invalid scenario: $case" }
            $expectedCalls = if ($case -in @('skipped test', 'process failure')) { 1 } else { 0 }
            if ($mockState.Calls -ne $expectedCalls) { throw "Unexpected Docker boundary in scenario: $case" }
        }
        foreach ($name in $names) {
            if ([Environment]::GetEnvironmentVariable($name, 'Process') -cne $saved[$name]) { throw 'Launcher did not restore its process environment' }
        }
        Write-Host "PASS: isolated Trello launcher $case"
    }
} finally {
    $global:LASTEXITCODE = $savedExitCode
    foreach ($name in $names) { [Environment]::SetEnvironmentVariable($name, $saved[$name], 'Process') }
    if ($owned) {
        $resolved = (Resolve-Path -LiteralPath $root).Path
        $parent = [IO.Path]::GetFullPath([IO.Path]::GetTempPath()).TrimEnd('\', '/') + [IO.Path]::DirectorySeparatorChar
        if (-not $resolved.StartsWith($parent, [StringComparison]::OrdinalIgnoreCase) -or [IO.Path]::GetFileName($resolved) -notmatch '^hai-trello-launcher-[0-9a-f]{32}$') {
            throw 'Refusing cleanup outside the owned fixture directory'
        }
        Remove-Item -LiteralPath $resolved -Recurse -Force
    }
}
