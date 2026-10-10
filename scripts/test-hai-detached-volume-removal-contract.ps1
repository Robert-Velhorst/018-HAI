$ErrorActionPreference = 'Stop'
$remover = [IO.File]::ReadAllText((Join-Path $PSScriptRoot 'remove-hai-detached-volume.ps1'))
$readiness = [IO.File]::ReadAllText((Join-Path $PSScriptRoot 'test-hai-volume-cleanup-readiness.ps1'))
$required = @(
    "ValidateSet('018-hai-kafka-kraft-data', '018-hai-ollama-local-data', '018-hai-redis-data', '018-hai-redpanda-data')",
    'SupportsShouldProcess = $true',
    'ConfirmationPhrase',
    'cleanup_requires_apply = $true',
    'verified_recovery_archives',
    'source_matches = $true',
    'Get-HaiVerifiedSummary',
    'archive_bytes = [long]$metadata.archive_bytes',
    '$report.safe_to_remove_any -ne $false',
    '$metadata.safe_to_remove -ne $false',
    'verify-hai-detached-volume-archive.ps1',
    'current_source_matches_verified_recovery_archive',
    'sourceDetached -ne $true',
    'archive_sha256',
    'Assert-HaiLocalDockerEngine',
    'Docker context changed during volume cleanup',
    "'volume', 'rm', `$name",
    'remaining_selected_volumes',
    'deletion_performed = ($removed.Count -gt 0)'
)
foreach ($token in $required) {
    if (-not $remover.Contains($token)) { throw "HAI detached-volume removal is missing a required guard: $token" }
}
if ($remover -match 'docker\s+volume\s+(prune|rm\s+-f)|docker\s+system\s+prune|018-hai-postgres|018-hai-phase2-control-state') {
    throw 'HAI detached-volume cleanup may not target persistent application databases/state or use broad Docker removal.'
}
if ($readiness -match 'docker\s+volume\s+(rm|prune)|Remove-Item|\[IO\.Directory\]::Delete') {
    throw 'HAI volume readiness must remain read-only.'
}
$tokens = $null
$errors = $null
[System.Management.Automation.Language.Parser]::ParseFile((Join-Path $PSScriptRoot 'remove-hai-detached-volume.ps1'), [ref]$tokens, [ref]$errors) | Out-Null
if ($errors.Count -gt 0) { throw 'HAI detached-volume removal script has PowerShell syntax errors.' }
Write-Output 'HAI detached-volume removal safety contract: PASS'
