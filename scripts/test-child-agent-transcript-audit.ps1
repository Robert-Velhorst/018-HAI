$ErrorActionPreference = 'Stop'

$tempRoot = [IO.Path]::GetFullPath([IO.Path]::GetTempPath()).TrimEnd('\') + '\'
$scratch = Join-Path $tempRoot ('hai-transcript-audit-' + [Guid]::NewGuid().ToString('N'))
$sessions = Join-Path $scratch 'source\sessions\2026\08'
$output = Join-Path $scratch 'output'
$rootSessionId = '019e7acc-44f2-7c90-a04e-253f6d43df28'
$childId = '019fb1d1-53a5-78a0-9387-b23f05eb6aef'
$transcript = Join-Path $sessions "rollout-2026-08-01T00-00-00-$childId.jsonl"
$audit = Join-Path $PSScriptRoot 'audit-child-agent-transcripts.ps1'

[IO.Directory]::CreateDirectory($sessions) | Out-Null
try {
    $records = @(
        (@{ type = 'session_meta'; payload = @{ id = $childId; agent_nickname = 'synthetic'; root_session_id = $rootSessionId } } | ConvertTo-Json -Compress -Depth 4),
        (@{ type = 'event_msg'; payload = @{ type = 'task_complete'; last_agent_message = 'Synthetic completed report.' } } | ConvertTo-Json -Compress -Depth 4)
    )
    [IO.File]::WriteAllLines($transcript, [string[]]$records, [Text.UTF8Encoding]::new($false))

    & $audit -RootSessionId $rootSessionId -SessionsRoot $sessions -OutputDirectory $output -TailBytes 1048576 | Out-Null

    $summaryPath = Join-Path $output 'child-agent-transcript-summary.json'
    $manifestPath = Join-Path $output 'child-agent-transcript-manifest.csv'
    $reportsPath = Join-Path $output 'child-agent-final-reports.md'
    $summary = Get-Content -LiteralPath $summaryPath -Raw | ConvertFrom-Json
    $rows = @(Import-Csv -LiteralPath $manifestPath)
    if ($summary.audited_transcripts -ne 1 -or $summary.completed_transcripts -ne 1 -or
        $summary.cleanup_candidate_transcripts -ne 1 -or $summary.deletion_performed -ne $false) {
        throw "Synthetic audit summary mismatch: $($summary | ConvertTo-Json -Compress)"
    }
    if ($rows.Count -ne 1 -or $rows[0].terminal_status -cne 'completed' -or
        $rows[0].disposition -cne 'candidate_after_ledger_commit' -or
        -not (Get-Content -LiteralPath $reportsPath -Raw).Contains('Synthetic completed report.')) {
        throw 'Synthetic transcript was not classified and reported as expected.'
    }
    if (-not (Test-Path -LiteralPath $transcript -PathType Leaf)) {
        throw 'Transcript audit unexpectedly removed its source file.'
    }

    Write-Host 'Synthetic transcript audit classification, reports, and non-deletion contract passed.'
} finally {
    $resolvedScratch = [IO.Path]::GetFullPath($scratch)
    if (-not $resolvedScratch.StartsWith($tempRoot, [StringComparison]::OrdinalIgnoreCase)) {
        throw 'Refusing to remove a test directory outside the temporary root.'
    }
    if (Test-Path -LiteralPath $resolvedScratch -PathType Container) {
        Remove-Item -LiteralPath $resolvedScratch -Recurse -Force
    }
}
