$ErrorActionPreference = 'Stop'
$scriptPath = Join-Path $PSScriptRoot 'remove-hai-unreferenced-image.ps1'
$scriptText = Get-Content -LiteralPath $scriptPath -Raw
$readinessText = Get-Content -LiteralPath (Join-Path $PSScriptRoot 'get-hai-local-cleanup-readiness.ps1') -Raw
foreach ($required in @(
    "backup-windows.ps1') -LibraryOnly",
    'Invoke-HaiBoundedDockerCommand $Arguments -TimeoutSeconds 30',
    'outcome may be unknown',
    'deletion_outcome_unknown = $outcomeUnknown',
    'remove_response_confirmed = $false',
    "'018-hai-backend:latest'",
    "'018-hai-backend-migrate:latest'",
    "'018-hai-idp:latest'",
    "'018-hai-frontend:latest'",
    "'018-hai-nginxconfigmanager:latest'",
    "`$expectedRoot = 'D:\codex-temp\hai-pr-update-018-20261009'",
    "`$serviceByReference = @{",
    "'018-hai-backend-migrate:latest' = 'backend-migrate'",
    "'018-hai-idp:latest' = 'idp'",
    "'018-hai-frontend:latest' = 'frontend'",
    'PR #36 must be merged into main at this exact local HEAD.',
    'Every PR #36 check must succeed before local images are removed.',
    'docker compose --profile event-bus --env-file $environmentFile -f $composeFile config --format json',
    "`$environmentFile = Join-Path `$repoRoot '.env.example'",
    'com.docker.compose.project',
    'com.docker.compose.service',
    "'ps', '-a', '--filter'",
    'ancestor=$id',
    'REMOVE HAI UNREFERENCED IMAGES',
    "'image', 'rm', [string]`$candidate.reference",
    'Get-HaiImageCandidates @([string]$candidate.reference) $contextName',
    "mode = if (`$failure) { 'partial_failure' } else { 'completed' }",
    'image_size_bytes_estimate_not_reclaimable',
    'docker_volumes_and_containers_touched = $false'
)) {
    if (-not $scriptText.Contains($required)) { throw "Image cleanup is missing required safety control: $required" }
}
foreach ($reference in @(
    '018-hai-backend:latest',
    '018-hai-backend-migrate:latest',
    '018-hai-idp:latest',
    '018-hai-frontend:latest',
    '018-hai-nginxconfigmanager:latest'
)) {
    if (-not $readinessText.Contains("'$reference'")) {
        throw "Unified HAI cleanup inventory omits removable image '$reference'."
    }
}
if ($readinessText.Contains("'018-hai-backend:local'")) {
    throw 'Unified HAI cleanup inventory must preserve the backup and restore image.'
}
if ($scriptText -match '(?i)docker\s+system\s+prune|docker\s+image\s+prune|docker\s+volume\s+rm|docker\s+container\s+rm') {
    throw 'Image cleanup must never use broad prune or remove containers/volumes.'
}
$tokens = $null
$parseErrors = $null
[void][System.Management.Automation.Language.Parser]::ParseFile($scriptPath, [ref]$tokens, [ref]$parseErrors)
if ($parseErrors.Count -gt 0) { throw "Image remover has PowerShell syntax errors: $($parseErrors[0].Message)" }
Write-Output 'HAI unreferenced-image removal safety contract passed.'
