$ErrorActionPreference = 'Stop'
$remover = [IO.File]::ReadAllText((Join-Path $PSScriptRoot 'remove-hai-completed-session-transcripts.ps1'))
$readiness = [IO.File]::ReadAllText((Join-Path $PSScriptRoot 'test-hai-transcript-cleanup-readiness.ps1'))

foreach ($token in @(
    "'D:\codex-temp\hai-completed-agent-sessions'",
    '-RequireSourceArchive -RequireCommittedLedger -RequireMergedPullRequest',
    'function Test-HaiTranscriptRemovalPreflight',
    'pr view 37 --repo Robert-Velhorst/018-HAI',
    "state -cne 'MERGED'",
    'pr checks 37 --repo Robert-Velhorst/018-HAI',
    'ls-remote origin refs/heads/main',
    'the checked-out main is not the current remote main commit',
    'canonical main worktree is not completely clean',
    'source_hash_audit_started = $false',
    'cleanup_gate_ready -ne $true',
    'REMOVE HAI COMPLETED SESSIONS',
    'SupportsShouldProcess = $true',
    'test-hai-supplemental-transcript-cleanup-readiness.ps1',
    'supplemental_source_archive_verified',
    '$PSCmdlet.ShouldProcess',
    'candidate_after_ledger_commit',
    'final_report_preserved',
    'duplicate_id_file_count',
    'Get-FileHash',
    'Get-CimInstance Win32_Process',
    '[int]$_.ProcessId -ne $PID',
    'Post-cleanup archive contents differ from the exact retained manifest set.',
    'post_cleanup_retained_set_verified = $true',
    'Remove-Item -LiteralPath $path -Force -ErrorAction Stop'
)) {
    if (-not $remover.Contains($token)) { throw "HAI transcript removal is missing a required guard: $token" }
}
if ($remover.Contains("pr view 36 --repo Robert-Velhorst/018-HAI") -or $remover.Contains("state -cne 'CLOSED'")) {
    throw 'HAI transcript removal must verify the corrected merged-PR contract.'
}
if ($remover -match 'Remove-Item[^\r\n]*-Recurse|docker\s+(container|network|volume|image)\s+(rm|prune)|git\s+clean') {
    throw 'HAI transcript removal must delete only individual manifest-listed files.'
}
if ($readiness -match 'Remove-Item|Move-Item|git\s+clean') {
    throw 'HAI transcript readiness must remain read-only.'
}
$supplementalRemoval = [IO.File]::ReadAllText((Join-Path $PSScriptRoot 'remove-hai-supplemental-completed-session-transcripts.ps1'))
if (-not (Test-Path -LiteralPath (Join-Path $PSScriptRoot 'test-hai-supplemental-transcript-removal-contract.ps1') -PathType Leaf) -or
    -not $supplementalRemoval.Contains('REMOVE HAI SUPPLEMENTAL SESSIONS') -or
    -not $supplementalRemoval.Contains('test-hai-supplemental-transcript-cleanup-readiness.ps1')) {
    throw 'Supplemental transcript cleanup must have a separate guarded removal path and contract test.'
}

$tokens = $null
$errors = $null
[System.Management.Automation.Language.Parser]::ParseFile((Join-Path $PSScriptRoot 'remove-hai-completed-session-transcripts.ps1'), [ref]$tokens, [ref]$errors) | Out-Null
if ($errors.Count -gt 0) { throw 'HAI transcript removal script has PowerShell syntax errors.' }
Write-Output 'HAI transcript removal safety contract: PASS'
