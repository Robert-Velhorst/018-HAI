[CmdletBinding()]
param(
    [Parameter(Mandatory)][string]$TranscriptRoot,
    [Parameter(Mandatory)][string[]]$SessionPath
)

$ErrorActionPreference = 'Stop'
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$manifestPath = Join-Path $repoRoot 'docs/child-agent-archive-2026-07-30/child-agent-transcript-manifest.csv'
$root = (Resolve-Path -LiteralPath $TranscriptRoot).Path.TrimEnd('\', '/')
$manifest = @(Import-Csv -LiteralPath $manifestPath)
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
    $reader = [IO.StreamReader]::new($entry.Path, [Text.Encoding]::UTF8, $true, 1048576)
    try {
        while ($null -ne ($line = $reader.ReadLine())) {
            if (-not $line.Contains('task_complete')) { continue }
            try {
                $event = $line | ConvertFrom-Json -Depth 100
            } catch {
                $parseErrorCount++
                continue
            }
            if ($null -eq $event.payload -or $event.payload.type -cne 'task_complete') { continue }
            $recordCount++
            $message = ([string]$event.payload.last_agent_message).Trim()
            if (-not $message) { continue }
            $nonemptyMessageCount++
            $bytes = [Text.Encoding]::UTF8.GetBytes($message)
            $hash = [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData($bytes)).ToLowerInvariant()
            [void]$perFileHashes.Add($hash)
            [void]$allMessageHashes.Add($hash)
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
    }
}

$fileResults | Format-Table -AutoSize
[pscustomobject]@{
    scanned_files = $fileResults.Count
    task_complete_records = [long](($fileResults | Measure-Object task_complete_records -Sum).Sum)
    nonempty_final_messages = [long](($fileResults | Measure-Object nonempty_final_messages -Sum).Sum)
    unique_trimmed_final_messages_across_scanned_files = $allMessageHashes.Count
    parse_errors_on_matching_lines = [long](($fileResults | Measure-Object parse_errors_on_matching_lines -Sum).Sum)
    semantics_verified = $false
    transcript_modified = $false
} | Format-List
