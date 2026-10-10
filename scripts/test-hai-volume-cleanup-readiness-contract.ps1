$ErrorActionPreference = 'Stop'
$source = [IO.File]::ReadAllText((Join-Path $PSScriptRoot 'test-hai-volume-cleanup-readiness.ps1'))
foreach ($token in @(
    "'image', 'ls', '--all', '--no-trunc'",
    'ancestor=$($parts[1])',
    'container_reference_count = $references.Count',
    "'hold_container_reference'",
    "'hold_retention_review'",
    'image_cleanup_authorized = $false',
    'safe_to_remove_any = $false',
    'deletion_performed = $false'
)) {
    if (-not $source.Contains($token)) { throw "HAI image readiness is missing its reference or retention gate: $token" }
}
if ($source -match 'docker\s+image\s+(rm|prune)|docker\s+volume\s+(rm|prune)|docker\s+system\s+prune') {
    throw 'HAI resource readiness must remain read-only.'
}
Write-Output 'HAI volume and image cleanup readiness contract: PASS'
