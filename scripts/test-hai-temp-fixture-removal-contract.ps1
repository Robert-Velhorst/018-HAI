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
    'Remove-Item -LiteralPath $verifiedPath -Recurse -Force -ErrorAction Stop',
    'Test-Path -LiteralPath $verifiedPath'
)) {
    if (-not $remover.Contains($token)) { throw "Temp fixture removal is missing a fail-closed guard: $token" }
}
$confirmationPosition = $remover.IndexOf('if ($PSCmdlet.ShouldProcess($path')
$finalAuditPosition = $remover.IndexOf('$finalReadiness = Get-HaiFixtureReadiness')
$removePosition = $remover.IndexOf('Remove-Item -LiteralPath $verifiedPath -Recurse -Force')
if ($confirmationPosition -lt 0 -or $finalAuditPosition -le $confirmationPosition -or $removePosition -le $finalAuditPosition) {
    throw 'Temp fixture removal must repeat its full readiness audit after confirmation and immediately before deletion.'
}
foreach ($token in @(
    'finalReadiness.deletion_performed -ne $false',
    'finalReadiness.cleanup_authorized -ne $false',
    'source_hashes',
    'provenance = [string]$finalEntry[0].provenance',
    'Get-CimInstance Win32_Process -ErrorAction Stop',
    'verifiedEntries = @(Get-ChildItem -LiteralPath $verifiedPath -Force -Recurse'
)) {
    if (-not $remover.Contains($token)) { throw "Temp final deletion audit is missing required guard: $token" }
}
if ($remover -match 'docker\s+(container|network|volume)\s+(rm|prune)|git\s+clean') {
    throw 'Temp fixture removal may not remove Docker resources or repository data.'
}

Write-Output 'HAI Temp fixture removal safety contract: PASS'
