$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$scriptPath = Join-Path $PSScriptRoot 'remove-hai-pr-diagnostic-artifacts.ps1'
$manifestPath = Join-Path $PSScriptRoot 'local-hai-pr-artifact-cleanup-manifest.json'
$scriptText = Get-Content -LiteralPath $scriptPath -Raw
$manifest = Get-Content -LiteralPath $manifestPath -Raw | ConvertFrom-Json -ErrorAction Stop

foreach ($required in @(
    '[CmdletBinding(SupportsShouldProcess = $true, ConfirmImpact = ''High'')]',
    "`$expectedRoot = 'D:\codex-temp\hai-pr-update-018-20261009'",
    'every PR #36 check must succeed',
    'PR #36 must be merged into main at this exact local HEAD',
    'Get-FileHash',
    'Get-CimInstance Win32_Process',
    'REMOVE HAI LOCAL DIAGNOSTICS',
    'Remove-Item -LiteralPath $path -Force',
    'local-working-tree.patch',
    'all Docker containers, images, volumes, and recovery bundles'
)) {
    if (-not $scriptText.Contains($required)) { throw "PR diagnostic cleanup is missing required safety control: $required" }
}

if ([int]$manifest.version -ne 1 -or [string]$manifest.repository -cne 'Robert-Velhorst/018-HAI' -or
    [int]$manifest.pullRequest -ne 36 -or [string]$manifest.worktreeRoot -cne 'D:\codex-temp\hai-pr-update-018-20261009' -or
    @($manifest.artifacts).Count -ne 15) {
    throw 'Diagnostic artifact manifest identity/count is not exact.'
}
$seen = @{}
foreach ($entry in @($manifest.artifacts)) {
    if ([string]$entry.path -notmatch '^(ci-113991681(379|677|724|733|798|876|884|896)\.log|ci-113991682(018|031|127|189)\.log|ci-frontend-job\.zip|gitleaks\.exe|gitleaks_8\.30\.1_windows_x64\.zip)$' -or
        [long]$entry.bytes -le 0 -or [string]$entry.sha256 -notmatch '^[0-9a-f]{64}$' -or $seen.ContainsKey([string]$entry.path)) {
        throw 'Manifest contains an unapproved, duplicate, empty, or unhashed artifact.'
    }
    $seen[[string]$entry.path] = $true
}

$tokens = $null
$parseErrors = $null
[void][System.Management.Automation.Language.Parser]::ParseFile($scriptPath, [ref]$tokens, [ref]$parseErrors)
if ($parseErrors.Count -gt 0) { throw "PR diagnostic remover has PowerShell syntax errors: $($parseErrors[0].Message)" }
Write-Output 'HAI PR local diagnostic removal safety contract passed.'
