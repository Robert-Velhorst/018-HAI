$ErrorActionPreference = 'Stop'
$readiness = [IO.File]::ReadAllText((Join-Path $PSScriptRoot 'test-hai-temp-fixture-cleanup-readiness.ps1'))
$remover = [IO.File]::ReadAllText((Join-Path $PSScriptRoot 'remove-hai-temp-fixtures.ps1'))

foreach ($token in @(
    'MinimumAgeHours = 24',
    'retain_recent',
    'generated fixture has not reached the minimum retention age',
    'Get-FileHash',
    'related_containers',
    'related_networks',
    'related_volumes'
)) {
    if (-not $readiness.Contains($token)) { throw "Temp readiness is missing stale-fixture protection: $token" }
}

foreach ($token in @(
    'SupportsShouldProcess = $true',
    'if (-not $Apply)',
    'ConfirmationPhrase',
    'REMOVE HAI TEMP FIXTURES',
    'Re-run the full file-hash and Docker-resource audit',
    'Get-CimInstance Win32_Process -ErrorAction Stop',
    'Resolved fixture path is outside the exact Temp-owned fixture directory.',
    '$PSCmdlet.ShouldProcess',
    'Remove-Item -LiteralPath $path -Recurse -Force -ErrorAction Stop',
    'Test-Path -LiteralPath $path'
)) {
    if (-not $remover.Contains($token)) { throw "Temp fixture removal is missing a fail-closed guard: $token" }
}
if ($remover -match 'docker\s+(container|network|volume)\s+(rm|prune)|git\s+clean') {
    throw 'Temp fixture removal may not remove Docker resources or repository data.'
}

Write-Output 'HAI Temp fixture removal safety contract: PASS'
