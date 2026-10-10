$ErrorActionPreference = 'Stop'
$source = [IO.File]::ReadAllText((Join-Path $PSScriptRoot 'test-hai-volume-cleanup-readiness.ps1'))
foreach ($token in @(
    "'image', 'ls', '--all', '--no-trunc'",
    'ancestor=$($parts[1])',
    'container_reference_count = $references.Count',
    "'hold_container_reference'",
    "'hold_retention_review'",
    "'verified_detached_archive'",
    "'hold_retention_decision'",
    "'retain_unverified'",
    'verify-hai-detached-volume-archive.ps1',
    'verified_recovery_archives = @($verifiedRecoveryArchives)',
    'unverified_recovery_bundles = @($unverifiedRecoveryBundles)',
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
if ($source.Contains('uncovered_volume_count')) {
    throw 'Volume readiness must distinguish a supported recovery workflow from a verified current recovery artifact.'
}
if ($source -match 'docker\s+image\s+(rm|prune)|docker\s+volume\s+(rm|prune)|docker\s+system\s+prune') {
    throw 'HAI resource readiness must remain read-only.'
}
Write-Output 'HAI volume and image cleanup readiness contract: PASS'
