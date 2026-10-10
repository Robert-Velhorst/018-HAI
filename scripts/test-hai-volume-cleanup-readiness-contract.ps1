$ErrorActionPreference = 'Stop'
$source = [IO.File]::ReadAllText((Join-Path $PSScriptRoot 'test-hai-volume-cleanup-readiness.ps1'))
$processRunner = [IO.File]::ReadAllText((Join-Path $PSScriptRoot 'backup-windows.ps1'))
foreach ($token in @(
    'Invoke-HaiBoundedDockerCommand $Arguments -TimeoutSeconds 15',
    '$result.timed_out',
    "'system', 'df', '--verbose', '--format', 'json'",
    'reported_size = if ($volumeSizes.ContainsKey([string]$name)) { [string]$volumeSizes[[string]$name] } else { $null }',
    "reported_volume_size_status = `$volumeSizeStatus",
    "'image', 'ls', '--all', '--no-trunc'",
    'ancestor=$($parts[1])',
    'container_reference_count = $references.Count',
    "'hold_container_reference'",
    "'hold_retention_review'",
    "'verified_detached_archive'",
    "'offline_archive_and_restore_drill'",
    "'018-hai-postgres-automation-data'",
    "'018-hai-postgres-idp-data'",
    "'018-hai-phase2-control-state'",
    "'hold_retention_decision'",
    "'retain_unverified'",
    "'manifest_missing'",
    "'manifest_acl_unverified'",
    "'artifact_acl_unverified'",
    "'archive_verification_failed'",
    'exact_duplicate_of_verified_archive = $false',
    "'review_exact_duplicate_of_verified_archive'",
    'artifact_sha256 = $artifactSha256',
    'bundle_bytes = $bundleBytes',
    'verify-hai-detached-volume-archive.ps1',
    "'verified_recovery_archive_source_removed'",
    'source_removed_recovery_archives = @($sourceRemovedRecoveryArchives)',
    'ArchiveOnly = $true',
    'verified_recovery_archives = @($verifiedRecoveryArchives)',
    'unverified_recovery_bundles = @($unverifiedRecoveryBundles)',
    'unverified_recovery_bundle_bytes = $unverifiedRecoveryBytes',
    'recovery_status = $recoveryStatus',
    'recovery_verified_this_run = $archiveVerified',
    'volumes_without_supported_recovery_method = $unsupportedRecoveryMethods',
    'volumes_without_current_verified_recovery_evidence = $unverifiedRecoveryCount',
    'safe_to_remove = $false',
    'image_cleanup_authorized = $false',
    'safe_to_remove_any = $false',
    'deletion_performed = $false'
)) {
    if (-not $source.Contains($token)) { throw "HAI image readiness is missing its reference or retention gate: $token" }
}
foreach ($token in @(
    'function Invoke-HaiBoundedProcess',
    '$process.WaitForExit($TimeoutSeconds * 1000)',
    '$process.Kill($true)',
    'timed_out = $true'
)) {
    if (-not $processRunner.Contains($token)) { throw "Bounded Docker process runner is missing timeout safety: $token" }
}
if ($source.Contains('uncovered_volume_count')) {
    throw 'Volume readiness must distinguish a supported recovery workflow from a verified current recovery artifact.'
}
if ($source -match 'docker\s+image\s+(rm|prune)|docker\s+volume\s+(rm|prune)|docker\s+system\s+prune') {
    throw 'HAI resource readiness must remain read-only.'
}
if ($source -match '(?i)Remove-Item|\[IO\.Directory\]::Delete|\[IO\.File\]::Delete') {
    throw 'HAI resource readiness must not delete recovery bundles or their artifacts.'
}
Write-Output 'HAI volume and image cleanup readiness contract: PASS'
& (Join-Path $PSScriptRoot 'test-hai-bounded-process.ps1')
