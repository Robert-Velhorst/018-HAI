$ErrorActionPreference = 'Stop'
$source = [IO.File]::ReadAllText((Join-Path $PSScriptRoot 'test-hai-temp-fixture-cleanup-readiness.ps1'))
$runner = [IO.File]::ReadAllText((Join-Path $PSScriptRoot 'isolated-acceptance-stack.ps1'))
$behaviorTest = [IO.File]::ReadAllText((Join-Path $PSScriptRoot 'test-isolated-acceptance-stack.ps1'))
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
    'Assert-HaiCleanupManifest',
    "kind -cne 'hai-acceptance-synthetic-fixture'",
    'syntheticEnvSha256',
    '$isPreparingFixture',
    "disposition = 'candidate_manual_cleanup'",
    'cleanup_authorized = $false',
    'deletion_performed = $false'
)) {
    if (-not $source.Contains($token)) { throw "HAI Temp fixture readiness is missing a cleanup safety guard: $token" }
}
foreach ($token in @(
    "'(?:^|_)(?:PASSWORD|TOKEN|SECRET|API_KEY|CLIENT_SECRET|CLIENT_ID|PRIVATE_KEY|SIGNING_KEY|WORKSPACE_KEY)$'",
    "state = 'preparing'",
    "kind = 'hai-acceptance-synthetic-fixture'",
    'syntheticEnvSha256',
    'Write-JsonFile $cleanupManifest $cleanupManifestPath',
    'Remove-Item -LiteralPath $cleanupManifestPath -Force -ErrorAction Stop'
)) {
    if (-not $runner.Contains($token)) { throw "Acceptance fixture generation is missing cleanup provenance or secret isolation: $token" }
}
if ($runner.IndexOf('Write-JsonFile $cleanupManifest $cleanupManifestPath', [StringComparison]::Ordinal) -gt
    $runner.IndexOf("'config', '--format', 'json'", [StringComparison]::Ordinal)) {
    throw 'The cleanup manifest must be written before Docker configuration can fail.'
}
foreach ($token in @(
    'Successful preparation retained its interrupted-cleanup marker.',
    'credential-like setting was inherited into the synthetic environment',
    'interrupted preparation retains hash-bound synthetic cleanup provenance',
    'cleanupMarker.syntheticEnvSha256'
)) {
    if (-not $behaviorTest.Contains($token)) { throw "Acceptance behavior tests are missing a cleanup safety assertion: $token" }
}
if ($source -match 'Remove-Item|Move-Item|docker\s+(container|network|volume)\s+(rm|prune)') {
    throw 'HAI Temp fixture readiness must remain read-only.'
}
Write-Output 'HAI Temp fixture cleanup readiness contract: PASS'
& (Join-Path $PSScriptRoot 'test-hai-temp-fixture-removal-contract.ps1')
