param()

$ErrorActionPreference = 'Stop'
$scriptPath = Join-Path $PSScriptRoot 'get-hai-local-cleanup-readiness.ps1'
$scriptText = Get-Content -LiteralPath $scriptPath -Raw
$required = @(
    'test-hai-temp-fixture-cleanup-readiness.ps1',
    'remove-hai-completed-session-transcripts.ps1',
    'test-hai-volume-cleanup-readiness.ps1',
    'remove-hai-pr-diagnostic-artifacts.ps1',
    'remove-hai-unreferenced-image.ps1',
    'deletion_performed -ne $false',
    'mode = ''read_only_inventory''',
    'cleanup_authorized = $false',
    'cleanup_targets = @($targets)',
    'preserve_active_pr_worktree = $true'
)
foreach ($token in $required) {
    if (-not $scriptText.Contains($token)) { throw "Unified HAI cleanup inventory is missing contract token: $token" }
}
if ($scriptText -match '(?im)^\s*(?:Remove-Item|Move-Item|docker\s+(?:image|volume|container)\s+rm|docker\s+(?:image|volume|system)\s+prune|git\s+clean)') {
    throw 'Unified HAI cleanup inventory must never mutate local data.'
}
if ($scriptText -match '(?i)\s-(?:Apply|ConfirmationPhrase|AllowPersistentDataRemoval)(?:\s|$)') {
    throw 'Unified HAI cleanup inventory must not invoke destructive authorization switches.'
}
$tokens = $null
$errors = $null
[void][System.Management.Automation.Language.Parser]::ParseFile($scriptPath, [ref]$tokens, [ref]$errors)
if ($errors.Count -gt 0) { throw "Unified HAI cleanup inventory has PowerShell syntax errors: $($errors[0].Message)" }
Write-Output 'HAI unified local-cleanup readiness contract: PASS'
