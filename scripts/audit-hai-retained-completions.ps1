[CmdletBinding()]
param(
    [Parameter(Mandatory)][string]$TranscriptRoot,
    [Parameter(Mandatory)][string[]]$SessionPath,
    [string]$ManifestPath = '',
    [ValidateRange(1, 10485760)][int]$MaxRecordCharacters = 10485760,
    [switch]$AsJson
)

$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'lib\hai-transcript-records.ps1')
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
if ([string]::IsNullOrWhiteSpace($ManifestPath)) {
    $ManifestPath = Join-Path $repoRoot 'docs/child-agent-archive-2026-07-30/child-agent-transcript-manifest.csv'
}
if (-not (Test-Path -LiteralPath $ManifestPath -PathType Leaf)) {
    throw "Transcript manifest does not exist: $ManifestPath"
}
$root = (Resolve-Path -LiteralPath $TranscriptRoot).Path.TrimEnd('\', '/')
$manifest = @(Import-Csv -LiteralPath $ManifestPath)
$rows = foreach ($requestedPath in $SessionPath) {
    $matches = @($manifest | Where-Object { $_.session_path -ceq $requestedPath })
    if ($matches.Count -ne 1) {
        throw "Expected exactly one manifest row for session path: $requestedPath"
    }
    $row = $matches[0]
    if ($row.disposition -notlike 'retain*') {
        throw "Refusing to scan a non-retained transcript: $requestedPath"
    }
    $prefix = 'hai-completed-agent-sessions/'
    if (-not $requestedPath.StartsWith($prefix, [StringComparison]::Ordinal)) {
        throw "Manifest path is outside the July 30 archive: $requestedPath"
    }
    $relative = $requestedPath.Substring($prefix.Length).Replace('/', [IO.Path]::DirectorySeparatorChar)
    if ([IO.Path]::IsPathRooted($relative) -or
        @($relative -split '[\\/]+' | Where-Object { $_ -ceq '.' -or $_ -ceq '..' }).Count -gt 0) {
        throw "Unsafe relative transcript path: $requestedPath"
    }
    $fullPath = Join-Path $root $relative
    if (-not (Test-Path -LiteralPath $fullPath -PathType Leaf)) {
        throw "Transcript is missing: $requestedPath"
    }
    $item = Get-Item -LiteralPath $fullPath -Force
    if ($item.Length -ne [long]$row.logical_bytes) {
        throw "Transcript size differs from manifest: $requestedPath"
    }
    [pscustomobject]@{ Row = $row; Path = $fullPath }
}

$allMessageHashes = [Collections.Generic.HashSet[string]]::new([StringComparer]::Ordinal)
$fileResults = foreach ($entry in $rows) {
    $perFileHashes = [Collections.Generic.HashSet[string]]::new([StringComparer]::Ordinal)
    $recordCount = 0
    $nonemptyMessageCount = 0
    $parseErrorCount = 0
    $oversizedRecordCount = 0
    $reader = [IO.StreamReader]::new($entry.Path, [Text.Encoding]::UTF8, $true, 1048576)
    try {
        Read-HaiBoundedTranscriptRecords -Reader $reader -MaximumCharacters $MaxRecordCharacters | ForEach-Object {
            $scannedRecord = $_
            if ($scannedRecord.Oversized) {
                $oversizedRecordCount++
            } else {
                $line = $scannedRecord.Text
                if ($line.Contains('task_complete')) {
                    $event = $null
                    try {
                        $event = ConvertFrom-Json -InputObject $line -Depth 100 -ErrorAction Stop
                    } catch {
                        $parseErrorCount++
                    }
                    if ($null -ne $event -and $null -ne $event.payload -and $event.payload.type -ceq 'task_complete') {
                        $recordCount++
                        $message = ([string]$event.payload.last_agent_message).Trim()
                        if ($message) {
                            $nonemptyMessageCount++
                            $bytes = [Text.Encoding]::UTF8.GetBytes($message)
                            $hash = [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData($bytes)).ToLowerInvariant()
                            [void]$perFileHashes.Add($hash)
                            [void]$allMessageHashes.Add($hash)
                        }
                    }
                }
            }
        }
    } finally {
        $reader.Dispose()
    }
    [pscustomobject]@{
        child_id = $entry.Row.child_id
        file = [IO.Path]::GetFileName($entry.Path)
        logical_bytes = [long]$entry.Row.logical_bytes
        terminal_status = $entry.Row.terminal_status
        task_complete_records = $recordCount
        nonempty_final_messages = $nonemptyMessageCount
        unique_trimmed_final_message_hashes = $perFileHashes.Count
        parse_errors_on_matching_lines = $parseErrorCount
        oversized_records_skipped = $oversizedRecordCount
    }
}

$summary = [pscustomobject]@{
    scanned_files = $fileResults.Count
    task_complete_records = [long](($fileResults | Measure-Object task_complete_records -Sum).Sum)
    nonempty_final_messages = [long](($fileResults | Measure-Object nonempty_final_messages -Sum).Sum)
    unique_trimmed_final_messages_across_scanned_files = $allMessageHashes.Count
    parse_errors_on_matching_lines = [long](($fileResults | Measure-Object parse_errors_on_matching_lines -Sum).Sum)
    oversized_records_skipped = [long](($fileResults | Measure-Object oversized_records_skipped -Sum).Sum)
    files_with_oversized_records = @($fileResults | Where-Object { $_.oversized_records_skipped -gt 0 }).Count
    max_record_characters = $MaxRecordCharacters
    scan_complete = (($fileResults | Where-Object { $_.oversized_records_skipped -gt 0 -or $_.parse_errors_on_matching_lines -gt 0 }).Count -eq 0)
    semantics_verified = $false
    transcript_modified = $false
}
if ($AsJson) {
    $summary | ConvertTo-Json -Depth 5
} else {
    $fileResults | Format-Table -AutoSize
    $summary | Format-List
}
