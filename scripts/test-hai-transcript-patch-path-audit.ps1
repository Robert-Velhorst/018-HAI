$ErrorActionPreference = 'Stop'

$audit = Join-Path $PSScriptRoot 'audit-hai-transcript-patch-paths.ps1'
$repo = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$fixtureRoot = Join-Path ([IO.Path]::GetTempPath()) ("hai-patch-path-audit-" + [guid]::NewGuid().ToString('N'))
$transcripts = Join-Path $fixtureRoot 'transcripts'
$source = Join-Path $fixtureRoot 'source'
$output = Join-Path $fixtureRoot 'output'
foreach ($path in @($transcripts, $source, $output)) {
    [void](New-Item -ItemType Directory -Path $path -Force)
}

$sourceFile = Join-Path $source 'README.md'
[IO.File]::WriteAllText($sourceFile, 'fixture')

$validRecord = [ordered]@{
    type = 'event_msg'
    payload = [ordered]@{
        type = 'patch_apply_end'
        success = $true
        changes = [ordered]@{ $sourceFile = @{} }
    }
} | ConvertTo-Json -Depth 10 -Compress
$oversizedRecord = '{"type":"event_msg","payload":{"type":"patch_apply_end","noise":"' + ('x' * 700) + '"}}'
$transcript = Join-Path $transcripts 'fixture.jsonl'
[IO.File]::WriteAllText($transcript, $oversizedRecord + "`n" + $validRecord, [Text.UTF8Encoding]::new($false))
$hiddenTranscript = Join-Path $transcripts 'hidden.jsonl'
[IO.File]::WriteAllText($hiddenTranscript, $validRecord, [Text.UTF8Encoding]::new($false))
[IO.File]::SetAttributes($hiddenTranscript, [IO.FileAttributes]::Hidden)
$nestedTranscripts = Join-Path $transcripts 'nested'
[void](New-Item -ItemType Directory -Path $nestedTranscripts -Force)
$nestedTranscript = Join-Path $nestedTranscripts 'nested.jsonl'
[IO.File]::WriteAllText($nestedTranscript, $validRecord, [Text.UTF8Encoding]::new($false))

$report = & $audit -TranscriptRoot $transcripts -SourceCheckoutRoot $source -RepositoryRoot $repo -MaxRecordCharacters 512 -Recurse | ConvertFrom-Json
if ($report.transcript_files -ne 3 -or
    $report.oversized_records_skipped -ne 1 -or
    $report.files_with_oversized_records -ne 1 -or
    $report.patch_events -ne 3 -or
    $report.successful_events -ne 3 -or
    $report.primary_path_references -ne 3 -or
    $report.primary_paths_tracked_in_repository -ne 1 -or
    $report.patch_event_scan_complete -ne $false) {
    throw "Bounded patch-path scan mismatch: $($report | ConvertTo-Json -Compress)"
}

if (-not (Test-Path -LiteralPath $transcript -PathType Leaf) -or
    -not (Test-Path -LiteralPath $hiddenTranscript -PathType Leaf) -or
    -not (Test-Path -LiteralPath $nestedTranscript -PathType Leaf)) {
    throw 'Patch-path audit unexpectedly removed its transcript fixture.'
}

Write-Host 'Bounded patch-path scan passed for hidden and nested files; oversized record was reported and later valid records were processed.'
Write-Host "Preserved test artifacts: $fixtureRoot"
