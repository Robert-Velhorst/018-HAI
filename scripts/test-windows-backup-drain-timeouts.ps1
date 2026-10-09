$ErrorActionPreference = 'Stop'

$repoRoot = Split-Path -Parent $PSScriptRoot
$backupScript = Join-Path $repoRoot 'scripts/backup-windows.ps1'
$composeFile = Join-Path $repoRoot 'docker-compose.local.yml'
$source = Get-Content -LiteralPath $backupScript -Raw
$compose = Get-Content -LiteralPath $composeFile -Raw

if ($source -notmatch '(?s)\$shutdownTimeoutSeconds\s*=\s*@\{[^}]*backend\s*=\s*60[^}]*idp\s*=\s*120') {
    throw 'Backup stop timeouts must match the backend (60s) and IDP (120s) shutdown windows.'
}
if ($source -notmatch '(?s)foreach\s*\(\$service\s+in\s+@\("backend",\s*"idp"\)\)\s*\{\s*if\s*\(\$running\[\$service\]\)\s*\{\s*\$timeout\s*=\s*\$shutdownTimeoutSeconds\[\$service\]') {
    throw 'Backup must apply per-service timeouts only to the HAI backend and IDP services that were running.'
}
if ($source -notmatch 'docker\s+compose\s+--env-file\s+\$envPath\s+-f\s+\$compose\s+stop\s+--timeout\s+\$timeout\s+\$service') {
    throw 'Backup stop command does not use the validated per-service timeout.'
}
if ($compose -notmatch '(?s)backend:.*?stop_grace_period:\s*60s' -or $compose -notmatch '(?s)idp:.*?stop_grace_period:\s*120s') {
    throw 'Compose shutdown grace periods changed; review and update this contract test.'
}

Write-Output 'Windows backup drain-timeout contract passed.'
