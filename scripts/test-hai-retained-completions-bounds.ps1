$ErrorActionPreference = 'Stop'

$audit = Join-Path $PSScriptRoot 'audit-hai-retained-completions.ps1'
$fixtureRoot = Join-Path ([IO.Path]::GetTempPath()) ("hai-retained-completions-" + [guid]::NewGuid().ToString('N'))
$transcripts = Join-Path $fixtureRoot 'transcripts'
[void](New-Item -ItemType Directory -Path $transcripts -Force)
$relativePath = 'hai-completed-agent-sessions/fixture.jsonl'
$transcript = Join-Path $transcripts 'fixture.jsonl'
$oversizedRecord = [ordered]@{
    type = 'event_msg'
    payload = [ordered]@{ type = 'task_complete'; last_agent_message = ('x' * 700) }
} | ConvertTo-Json -Depth 10 -Compress
$validRecord = [ordered]@{
    type = 'event_msg'
    payload = [ordered]@{ type = 'task_complete'; last_agent_message = 'Retained completion fixture.' }
} | ConvertTo-Json -Depth 10 -Compress
[IO.File]::WriteAllText($transcript, $oversizedRecord + "`n" + $validRecord, [Text.UTF8Encoding]::new($false))

$manifestPath = Join-Path $fixtureRoot 'manifest.csv'
$manifestRow = [pscustomobject]@{
    session_path = $relativePath
    child_id = 'fixture-child'
    disposition = 'retain_test_fixture'
    terminal_status = 'aborted'
    logical_bytes = (Get-Item -LiteralPath $transcript).Length
}
$manifestRow | Export-Csv -LiteralPath $manifestPath -NoTypeInformation

$report = & $audit -TranscriptRoot $transcripts -SessionPath $relativePath -ManifestPath $manifestPath -MaxRecordCharacters 512 -AsJson | ConvertFrom-Json
if ($report.scanned_files -ne 1 -or
    $report.task_complete_records -ne 1 -or
    $report.nonempty_final_messages -ne 1 -or
    $report.oversized_records_skipped -ne 1 -or
    $report.files_with_oversized_records -ne 1 -or
    $report.scan_complete -ne $false -or
    $report.transcript_modified -ne $false) {
    throw "Bounded retained-completion scan mismatch: $($report | ConvertTo-Json -Compress)"
}

if (-not (Test-Path -LiteralPath $transcript -PathType Leaf)) {
    throw 'Retained-completion audit unexpectedly removed its transcript fixture.'
}

Write-Host 'Bounded retained-completion scan passed; oversized record was reported and the following valid final record was counted.'
Write-Host "Preserved test artifacts: $fixtureRoot"
