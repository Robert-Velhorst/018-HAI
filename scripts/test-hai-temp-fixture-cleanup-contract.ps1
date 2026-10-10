$ErrorActionPreference = 'Stop'
$source = [IO.File]::ReadAllText((Join-Path $PSScriptRoot 'test-hai-temp-fixture-cleanup-readiness.ps1'))
$processRunner = [IO.File]::ReadAllText((Join-Path $PSScriptRoot 'backup-windows.ps1'))
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
    'Get-HaiExampleEnvironmentValues',
    'tracked environment example is not a bounded regular file',
    'environment example has uncommitted changes',
    'credential-like value outside generated markers and the unchanged tracked example',
    'Assert-HaiSyntheticEnvironment $environmentPath $expectedProject',
    "kind -cne 'hai-acceptance-synthetic-fixture'",
    'syntheticEnvSha256',
    'actualFiles.Count -in @(1, 2)',
    'AllowEnvironmentMissing',
    'empty interrupted-preparation marker',
    '$isPreparingFixture',
    "disposition = 'candidate_manual_cleanup'",
    'cleanup_authorized = $false',
    'deletion_performed = $false'
)) {
    if (-not $source.Contains($token)) { throw "HAI Temp fixture readiness is missing a cleanup safety guard: $token" }
}
if (-not $source.Contains('Invoke-HaiBoundedDockerCommand $Arguments -TimeoutSeconds 10') -or
    -not $processRunner.Contains('function Invoke-HaiBoundedDockerCommand')) {
    throw 'HAI Temp readiness must use a bounded local Docker query and fail closed on timeout.'
}
foreach ($token in @(
    "'(?:^|_)(?:PASSWORD|PASS|TOKEN|SECRET|API_KEY|CLIENT_ID|PRIVATE_KEY|SIGNING_KEY|WORKSPACE_KEY|ENCRYPTION_KEY|ACCESS_KEY|SHARED_KEY|CREDENTIALS?)$'",
    "state = 'preparing'",
    "kind = 'hai-acceptance-synthetic-fixture'",
    'syntheticEnvSha256',
    'syntheticEnvBytes = 0',
    'Write-JsonFile $cleanupManifest $cleanupManifestPath',
    'Remove-Item -LiteralPath $cleanupManifestPath -Force -ErrorAction Stop'
)) {
    if (-not $runner.Contains($token)) { throw "Acceptance fixture generation is missing cleanup provenance or secret isolation: $token" }
}
if ($runner.IndexOf('Write-JsonFile $cleanupManifest $cleanupManifestPath', [StringComparison]::Ordinal) -gt
    $runner.IndexOf("'config', '--format', 'json'", [StringComparison]::Ordinal)) {
    throw 'The cleanup manifest must be written before Docker configuration can fail.'
}
if ($runner.IndexOf('Write-JsonFile $cleanupManifest $cleanupManifestPath', [StringComparison]::Ordinal) -gt
    $runner.IndexOf('[IO.File]::WriteAllLines($envFile', [StringComparison]::Ordinal)) {
    throw 'The cleanup provenance marker must be written before the synthetic environment payload.'
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
$fileCountAssignment = $source.IndexOf('$record.file_count = $files.Count', [StringComparison]::Ordinal)
$bytesAssignment = $source.IndexOf('$record.bytes = [long](($files | Measure-Object -Property Length -Sum).Sum)', [StringComparison]::Ordinal)
$inventoryRejection = $source.IndexOf("throw 'fixture file/directory inventory differs from the generated acceptance layout'", [StringComparison]::Ordinal)
if ($fileCountAssignment -lt 0 -or $bytesAssignment -le $fileCountAssignment -or $inventoryRejection -le $bytesAssignment) {
    throw 'HAI Temp readiness must report observed file counts and bytes before rejecting an incomplete fixture layout.'
}
Write-Output 'HAI Temp fixture cleanup readiness contract: PASS'
& (Join-Path $PSScriptRoot 'test-hai-temp-fixture-removal-contract.ps1')
