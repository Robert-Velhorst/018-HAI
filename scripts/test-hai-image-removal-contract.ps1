$ErrorActionPreference = 'Stop'
$scriptPath = Join-Path $PSScriptRoot 'remove-hai-unreferenced-image.ps1'
$scriptText = Get-Content -LiteralPath $scriptPath -Raw
foreach ($required in @(
    "backup-windows.ps1') -LibraryOnly",
    "[ValidateSet('018-hai-backend:latest', '018-hai-nginxconfigmanager:latest')]",
    "`$expectedRoot = 'D:\codex-temp\hai-pr-update-018-20261009'",
    "`$serviceByReference = @{",
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
    'image_size_bytes_estimate_not_reclaimable',
    'docker_volumes_and_containers_touched = $false'
)) {
    if (-not $scriptText.Contains($required)) { throw "Image cleanup is missing required safety control: $required" }
}
if ($scriptText -match '(?i)docker\s+system\s+prune|docker\s+image\s+prune|docker\s+volume\s+rm|docker\s+container\s+rm') {
    throw 'Image cleanup must never use broad prune or remove containers/volumes.'
}
$tokens = $null
$parseErrors = $null
[void][System.Management.Automation.Language.Parser]::ParseFile($scriptPath, [ref]$tokens, [ref]$parseErrors)
if ($parseErrors.Count -gt 0) { throw "Image remover has PowerShell syntax errors: $($parseErrors[0].Message)" }
Write-Output 'HAI unreferenced-image removal safety contract passed.'
