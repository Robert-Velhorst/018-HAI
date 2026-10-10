$ErrorActionPreference = 'Stop'
$source = [IO.File]::ReadAllText((Join-Path $PSScriptRoot 'test-hai-temp-fixture-cleanup-readiness.ps1'))
foreach ($token in @(
    'Temp scan root is a reparse point',
    'fixture root is a reparse point',
    'generated manifest or compose definition is missing',
    'fixture file/directory inventory differs from the generated acceptance layout',
    'MinimumAgeHours = 24',
    'retain_recent',
    'generated fixture has not reached the minimum retention age',
    "'hai.acceptance.owner'",
    'docker_resources_checked = $dockerAvailable',
    "disposition = 'candidate_manual_cleanup'",
    'cleanup_authorized = $false',
    'deletion_performed = $false'
)) {
    if (-not $source.Contains($token)) { throw "HAI Temp fixture readiness is missing a cleanup safety guard: $token" }
}
if ($source -match 'Remove-Item|Move-Item|docker\s+(container|network|volume)\s+(rm|prune)') {
    throw 'HAI Temp fixture readiness must remain read-only.'
}
Write-Output 'HAI Temp fixture cleanup readiness contract: PASS'
& (Join-Path $PSScriptRoot 'test-hai-temp-fixture-removal-contract.ps1')
