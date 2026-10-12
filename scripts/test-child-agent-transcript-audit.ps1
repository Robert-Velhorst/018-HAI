param([switch]$KeepArtifacts)

$ErrorActionPreference = 'Stop'

$tempRoot = [IO.Path]::GetFullPath([IO.Path]::GetTempPath()).TrimEnd('\') + '\'
$scratch = Join-Path $tempRoot ('hai-transcript-audit-' + [Guid]::NewGuid().ToString('N'))
$sessions = Join-Path $scratch 'source\sessions\2026\08'
$output = Join-Path $scratch 'output'
$rootSessionId = '019e7acc-44f2-7c90-a04e-253f6d43df28'
$childId = '019fb1d1-53a5-78a0-9387-b23f05eb6aef'
$completedChildTurn = '019fb1d1-6d0b-7fd0-af97-9e88a3022f11'
$abortedChildId = '019fb2aa-53a5-78a0-9387-b23f05eb6aef'
$abortedChildTurn = '019fb2aa-6d0b-7fd0-af97-9e88a3022f11'
$ambiguousChildId = '019fb2bb-53a5-78a0-9387-b23f05eb6aef'
$oversizedChildId = '019fb2cc-53a5-78a0-9387-b23f05eb6aef'
$oversizedChildTurn = '019fb2cc-6d0b-7fd0-af97-9e88a3022f11'
$transcript = Join-Path $sessions "rollout-2026-08-01T00-00-00-$childId.jsonl"
$abortedTranscript = Join-Path $sessions "rollout-2026-08-01T00-01-00-$abortedChildId.jsonl"
$ambiguousTranscript = Join-Path $sessions "rollout-2026-08-01T00-02-00-$ambiguousChildId.jsonl"
$oversizedTranscript = Join-Path $sessions "rollout-2026-08-01T00-03-00-$oversizedChildId.jsonl"
$audit = Join-Path $PSScriptRoot 'audit-child-agent-transcripts.ps1'

function New-SyntheticTranscriptLine([string]$Type, $Payload) {
    return [ordered]@{
        timestamp = '2026-08-01T00:00:00.000Z'
        type = $Type
        payload = $Payload
    } | ConvertTo-Json -Compress -Depth 8
}

[IO.Directory]::CreateDirectory($sessions) | Out-Null
try {
    $records = @(
        (New-SyntheticTranscriptLine 'session_meta' @{ id = $childId; agent_nickname = 'synthetic'; root_session_id = $rootSessionId }),
        (New-SyntheticTranscriptLine 'event_msg' @{ type = 'task_started'; turn_id = $completedChildTurn }),
        (New-SyntheticTranscriptLine 'event_msg' @{ type = 'task_complete'; turn_id = $completedChildTurn; last_agent_message = 'Synthetic completed report. Source: C:/Users/NO/Documents/Codex/2026-05-30/work/018-hai-port-engine-control/backend/example.go and C:\Users\NO\Documents\Codex\2026-05-30\github-plugin-github-openai-curated-noodzakelijk\docs\example.md' }),
        (New-SyntheticTranscriptLine 'response_item' @{ type = 'custom_tool_call'; name = 'apply_patch'; input = '*** Begin Patch'; internal_chat_message_metadata_passthrough = @{ turn_id = $completedChildTurn } }),
        (New-SyntheticTranscriptLine 'event_msg' @{ type = 'task_started'; turn_id = '019fb1ee-6d0b-7fd0-af97-9e88a3022f11' }),
        (New-SyntheticTranscriptLine 'event_msg' @{ type = 'task_complete'; turn_id = '019fb1ee-6d0b-7fd0-af97-9e88a3022f11'; last_agent_message = "Unrelated parent completion mentions child $childId." }),
        (New-SyntheticTranscriptLine 'response_item' @{ type = 'custom_tool_call'; name = 'apply_patch'; input = '*** Begin Patch'; internal_chat_message_metadata_passthrough = @{ turn_id = '019fb1ee-6d0b-7fd0-af97-9e88a3022f11' } })
    )
    [IO.File]::WriteAllLines($transcript, [string[]]$records, [Text.UTF8Encoding]::new($false))
    $abortedRecords = @(
        (New-SyntheticTranscriptLine 'session_meta' @{ id = $abortedChildId; agent_nickname = 'aborted'; root_session_id = $rootSessionId }),
        (New-SyntheticTranscriptLine 'event_msg' @{ type = 'turn_aborted'; turn_id = $abortedChildTurn; reason = 'interrupted' }),
        (New-SyntheticTranscriptLine 'event_msg' @{ type = 'task_complete'; turn_id = '019fb2cc-6d0b-7fd0-af97-9e88a3022f11'; last_agent_message = "Unrelated parent completion mentions child $abortedChildId." })
    )
    [IO.File]::WriteAllLines($abortedTranscript, [string[]]$abortedRecords, [Text.UTF8Encoding]::new($false))
    $ambiguousRecords = @(
        (New-SyntheticTranscriptLine 'session_meta' @{ id = $ambiguousChildId; agent_nickname = 'ambiguous'; root_session_id = $rootSessionId }),
        (New-SyntheticTranscriptLine 'event_msg' @{ type = 'task_complete'; turn_id = '019fb2dd-6d0b-7fd0-af97-9e88a3022f11'; last_agent_message = 'Prefix does not match child.' })
    )
    [IO.File]::WriteAllLines($ambiguousTranscript, [string[]]$ambiguousRecords, [Text.UTF8Encoding]::new($false))
    $oversizedMetadata = New-SyntheticTranscriptLine 'session_meta' @{ id = $oversizedChildId; agent_nickname = 'oversized'; root_session_id = $rootSessionId }
    $oversizedRecord = '{"timestamp":"2026-08-01T00:00:00.000Z","type":"event_msg","payload":{"type":"task_started","turn_id":"' + $oversizedChildTurn + '","content":"' + ('x' * (9MB)) + '"}}'
    $laterCompletion = New-SyntheticTranscriptLine 'event_msg' @{ type = 'task_complete'; turn_id = $oversizedChildTurn; last_agent_message = 'Must stay retained because an earlier event was too large to parse.' }
    [IO.File]::WriteAllLines($oversizedTranscript, [string[]]@($oversizedMetadata, $oversizedRecord, $laterCompletion), [Text.UTF8Encoding]::new($false))

    & $audit -RootSessionId $rootSessionId -SessionsRoot $sessions -OutputDirectory $output | Out-Null

    $summaryPath = Join-Path $output 'child-agent-transcript-summary.json'
    $manifestPath = Join-Path $output 'child-agent-transcript-manifest.csv'
    $reportsPath = Join-Path $output 'child-agent-final-reports.md'
    $summary = Get-Content -LiteralPath $summaryPath -Raw | ConvertFrom-Json
    $rows = @(Import-Csv -LiteralPath $manifestPath)
    if ($summary.audited_transcripts -ne 4 -or $summary.completed_transcripts -ne 1 -or
        $summary.cleanup_candidate_transcripts -ne 1 -or $summary.retained_transcripts -ne 3 -or
        $summary.deletion_performed -ne $false) {
        throw "Synthetic audit summary mismatch: $($summary | ConvertTo-Json -Compress)"
    }
    $completedRow = @($rows | Where-Object child_id -CEQ $childId)[0]
    $abortedRow = @($rows | Where-Object child_id -CEQ $abortedChildId)[0]
    $ambiguousRow = @($rows | Where-Object child_id -CEQ $ambiguousChildId)[0]
    $oversizedRow = @($rows | Where-Object child_id -CEQ $oversizedChildId)[0]
    if ($rows.Count -ne 4 -or $completedRow.terminal_status -cne 'completed' -or
        $completedRow.child_turn_id -cne $completedChildTurn -or
        $completedRow.turn_attribution -cne 'unique_prefix_match' -or
        [int]$completedRow.patch_call_count -ne 1 -or
        $completedRow.disposition -cne 'candidate_after_ledger_commit' -or
        $abortedRow.terminal_status -cne 'aborted' -or
        $abortedRow.disposition -cne 'retain' -or
        $ambiguousRow.terminal_status -cne 'unattributed' -or
        $ambiguousRow.disposition -cne 'retain' -or
        $oversizedRow.terminal_status -cne 'unattributed' -or
        $oversizedRow.observed_terminal_status -cne 'completed' -or
        $oversizedRow.turn_attribution -cne 'oversized_record_unparsed' -or
        [long]$oversizedRow.scan_oversized_records -ne 1 -or
        $oversizedRow.disposition -cne 'retain_unparsed_candidate_record' -or
        -not (Get-Content -LiteralPath $reportsPath -Raw).Contains('Synthetic completed report.') -or
        -not (Get-Content -LiteralPath $reportsPath -Raw).Contains('observed completion only') -or
        -not (Get-Content -LiteralPath $reportsPath -Raw).Contains('../../backend/example.go') -or
        -not (Get-Content -LiteralPath $reportsPath -Raw).Contains('../../docs/example.md') -or
        (Get-Content -LiteralPath $reportsPath -Raw) -match '(?i)[A-Z]:[\\/]+Users[\\/]+[^\\/\s]+[\\/]+Documents[\\/]+Codex') {
        throw 'Synthetic transcript was not classified and reported as expected.'
    }
    $sourceTranscripts = @($transcript, $abortedTranscript, $ambiguousTranscript, $oversizedTranscript)
    if (@($sourceTranscripts | Where-Object { -not (Test-Path -LiteralPath $_ -PathType Leaf) }).Count -gt 0) {
        throw 'Transcript audit unexpectedly removed its source file.'
    }

    Write-Host 'Synthetic transcript attribution, patch counting, reports, and non-deletion contract passed.'
    if ($KeepArtifacts) {
        Write-Host "Preserved test artifacts: $scratch"
    }
} finally {
    $resolvedScratch = [IO.Path]::GetFullPath($scratch)
    if (-not $resolvedScratch.StartsWith($tempRoot, [StringComparison]::OrdinalIgnoreCase)) {
        throw 'Refusing to remove a test directory outside the temporary root.'
    }
    if (-not $KeepArtifacts -and (Test-Path -LiteralPath $resolvedScratch -PathType Container)) {
        Remove-Item -LiteralPath $resolvedScratch -Recurse -Force
    }
}
