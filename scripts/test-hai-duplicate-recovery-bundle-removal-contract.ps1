$ErrorActionPreference = 'Stop'
$removerPath = Join-Path $PSScriptRoot 'remove-hai-duplicate-recovery-bundles.ps1'
$readinessPath = Join-Path $PSScriptRoot 'test-hai-volume-cleanup-readiness.ps1'
$remover = [IO.File]::ReadAllText($removerPath)
$readiness = [IO.File]::ReadAllText($readinessPath)
$required = @(
    'SupportsShouldProcess = $true',
    'ConfirmationPhrase',
    'REMOVE HAI EXACT DUPLICATE RECOVERY BUNDLES',
    'source_removed_recovery_archives',
    'archive_integrity_verified -eq $true',
    'Get-FileHash',
    'canonical_bundle_id',
    'Assert-HaiPrivateEnvironmentAcl $bundle.FullName -Directory',
    'Assert-HaiEffectivePrivateFileAcl',
    'Remove-Item -LiteralPath $entry.FullName',
    'Remove-Item -LiteralPath $directory',
    'postflight_verified'
)
foreach ($token in $required) {
    if (-not $remover.Contains($token)) { throw "Duplicate recovery cleanup is missing a required safety check: $token" }
}
if ($remover -match 'Remove-Item[^\r\n]*-Recurse|Remove-Item[^\r\n]*\*|docker\s+(volume|image|system)\s+(rm|prune)') {
    throw 'Duplicate recovery cleanup may not use recursive/wildcard filesystem removal or Docker removal.'
}
if ($remover -match '018-hai-postgres|018-hai-phase2-control-state|hai-completed-agent-sessions') {
    throw 'Duplicate recovery cleanup may not target databases, control state, or session transcripts.'
}
if (-not $readiness.Contains('source_removed_recovery_archives = @($sourceRemovedRecoveryArchives)') -or
    -not $readiness.Contains('archive_integrity_verified = $true')) {
    throw 'Volume readiness does not provide verified canonical recovery archives.'
}
$tokens = $null
$errors = $null
[System.Management.Automation.Language.Parser]::ParseFile($removerPath, [ref]$tokens, [ref]$errors) | Out-Null
if ($errors.Count -gt 0) { throw 'HAI duplicate recovery cleanup script has PowerShell syntax errors.' }
Write-Output 'HAI exact duplicate recovery bundle removal contract: PASS'
