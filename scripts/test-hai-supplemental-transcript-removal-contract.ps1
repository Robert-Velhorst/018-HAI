$ErrorActionPreference = 'Stop'
$scriptPath = Join-Path $PSScriptRoot 'remove-hai-supplemental-completed-session-transcripts.ps1'
$source = [IO.File]::ReadAllText($scriptPath)
$required = @(
    "'D:\codex-temp\completed-agent-sessions'",
    "'D:\codex-temp\session-log-relief'",
    'candidate_after_ledger_commit',
    'unique_prefix_match',
    'Get-FileHash',
    'test-hai-supplemental-transcript-cleanup-readiness.ps1',
    'clean canonical main worktree at the current remote main commit',
    '-PullRequestNumber 37',
    'REMOVE HAI SUPPLEMENTAL SESSIONS',
    'Get-CimInstance Win32_Process',
    '[int]$_.ProcessId -ne $PID',
    '$PSCmdlet.ShouldProcess',
    'Post-cleanup supplemental archive differs from the exact retained manifest set.',
    'Remove-Item -LiteralPath $path -Force -ErrorAction Stop'
)
foreach ($token in $required) {
    if (-not $source.Contains($token)) { throw "Supplemental transcript removal is missing a required guard: $token" }
}
if ($source -match 'Remove-Item[^\r\n]*-Recurse|docker\s+(container|network|volume|image)\s+(rm|prune)|git\s+clean') {
    throw 'Supplemental transcript cleanup may remove only individual manifest-listed files.'
}
$tokens = $null
$errors = $null
[System.Management.Automation.Language.Parser]::ParseFile($scriptPath, [ref]$tokens, [ref]$errors) | Out-Null
if ($errors.Count -gt 0) { throw 'Supplemental transcript removal script has PowerShell syntax errors.' }
Write-Output 'HAI supplemental transcript removal safety contract: PASS'
