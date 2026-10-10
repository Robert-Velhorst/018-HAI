$ErrorActionPreference = 'Stop'
$remover = [IO.File]::ReadAllText((Join-Path $PSScriptRoot 'remove-hai-completed-session-transcripts.ps1'))
$readiness = [IO.File]::ReadAllText((Join-Path $PSScriptRoot 'test-hai-transcript-cleanup-readiness.ps1'))

foreach ($token in @(
    "'D:\codex-temp\hai-completed-agent-sessions'",
    '-RequireSourceArchive -RequireCommittedLedger -RequireMergedPullRequest',
    'function Test-HaiTranscriptRemovalPreflight',
    'source_hash_audit_started = $false',
    'cleanup_gate_ready -ne $true',
    'REMOVE HAI COMPLETED SESSIONS',
    'SupportsShouldProcess = $true',
    '$PSCmdlet.ShouldProcess',
    'candidate_after_ledger_commit',
    'final_report_preserved',
    'duplicate_id_file_count',
    'Get-FileHash',
    'Get-CimInstance Win32_Process',
    'Post-cleanup archive contents differ from the exact retained manifest set.',
    'post_cleanup_retained_set_verified = $true',
    'Remove-Item -LiteralPath $path -Force -ErrorAction Stop'
)) {
    if (-not $remover.Contains($token)) { throw "HAI transcript removal is missing a required guard: $token" }
}
if ($remover -match 'Remove-Item[^\r\n]*-Recurse|docker\s+(container|network|volume|image)\s+(rm|prune)|git\s+clean') {
    throw 'HAI transcript removal must delete only individual manifest-listed files.'
}
if ($readiness -match 'Remove-Item|Move-Item|git\s+clean') {
    throw 'HAI transcript readiness must remain read-only.'
}

$tokens = $null
$errors = $null
[System.Management.Automation.Language.Parser]::ParseFile((Join-Path $PSScriptRoot 'remove-hai-completed-session-transcripts.ps1'), [ref]$tokens, [ref]$errors) | Out-Null
if ($errors.Count -gt 0) { throw 'HAI transcript removal script has PowerShell syntax errors.' }
Write-Output 'HAI transcript removal safety contract: PASS'
