[CmdletBinding()]
param(
    [string]$RootSessionId = '019e7acc-44f2-7c90-a04e-253f6d43df28',
    [string]$SessionsRoot = 'C:\Users\NO\.codex\sessions\2026\08',
    [string]$OutputDirectory = '',
    [switch]$HashCleanupCandidates,
    [ValidateRange(1, 10485760)][int]$MaxRecordCharacters = 10485760
)

$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'lib\hai-transcript-records.ps1')

if ([string]::IsNullOrWhiteSpace($OutputDirectory)) {
    $OutputDirectory = Join-Path $PSScriptRoot '..\docs'
}

if (-not (Test-Path -LiteralPath $SessionsRoot -PathType Container)) {
    throw "Sessions root does not exist: $SessionsRoot"
}

$existingManifestPath = Join-Path $OutputDirectory 'child-agent-transcript-manifest.csv'
if (Test-Path -LiteralPath $existingManifestPath -PathType Leaf) {
    $existingRows = @(Import-Csv -LiteralPath $existingManifestPath)
    $existingBase = (Split-Path -Parent $SessionsRoot).TrimEnd('\')
    $missingAuditedFiles = @(
        $existingRows | Where-Object {
            $existingPath = Join-Path $existingBase ($_.session_path -replace '/', '\')
            -not (Test-Path -LiteralPath $existingPath -PathType Leaf)
        }
    )
    if ($missingAuditedFiles.Count -gt 0) {
        throw "Existing audit artifacts refer to $($missingAuditedFiles.Count) removed transcript(s). Preserve the committed manifest and final reports; do not regenerate them from the reduced session tree."
    }
}

if (-not ('HaiTranscriptAllocation' -as [type])) {
    Add-Type -TypeDefinition @'
using System;
using System.ComponentModel;
using System.Runtime.InteropServices;

public static class HaiTranscriptAllocation
{
    [DllImport("kernel32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
    private static extern uint GetCompressedFileSizeW(string fileName, out uint high);

    public static ulong GetAllocatedBytes(string path)
    {
        uint high;
        uint low = GetCompressedFileSizeW(path, out high);
        if (low == 0xffffffff && Marshal.GetLastWin32Error() != 0)
        {
            throw new Win32Exception();
        }
        return ((ulong)high << 32) | low;
    }
}
'@
}

function Get-Sha256 {
    param([AllowEmptyString()][string]$Text)

    $algorithm = [System.Security.Cryptography.SHA256]::Create()
    try {
        $bytes = [System.Text.Encoding]::UTF8.GetBytes($Text)
        return ([System.BitConverter]::ToString($algorithm.ComputeHash($bytes))).Replace('-', '').ToLowerInvariant()
    } finally {
        $algorithm.Dispose()
    }
}

function Protect-ReportText {
    param([AllowEmptyString()][string]$Text)

    $protected = $Text
    $protected = $protected -replace '(?i)[A-Z]:[\\/]+Users[\\/]+[^\\/\s`]+[\\/]+Documents[\\/]+Codex[\\/]+(?:[^\\/\s`]+[\\/]+)*?(?:work[\\/]+018-hai-port-engine-control|github-plugin-github-openai-curated-noodzakelijk)[\\/]+', '../../'
    $protected = [regex]::Replace($protected, '(?<=\.\./\.\./)[^\s`]+', { param($match) $match.Value.Replace('\', '/') })
    $protected = $protected -replace '(?i)(Authorization:\s*Bearer\s+)[A-Za-z0-9._~+/=-]+', '$1[REDACTED]'
    $protected = $protected -replace '(?i)sk-[A-Za-z0-9_-]{12,}', 'sk-[REDACTED]'
    $protected = $protected -replace '(?i)ghp_[A-Za-z0-9]{12,}', 'ghp_[REDACTED]'
    $protected = $protected -replace '(?is)-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----.*?-----END (?:RSA |EC |OPENSSH )?PRIVATE KEY-----', '[REDACTED PRIVATE KEY]'
    $protectedLines = @($protected -split "`r?`n" | ForEach-Object { $_.TrimEnd() })
    return ($protectedLines -join "`n").Trim()
}

function ConvertTo-CsvField {
    param([AllowEmptyString()][string]$Value)
    return '"' + ($Value -replace '"', '""') + '"'
}

$records = [System.Collections.Generic.List[object]]::new()
$relativeBase = (Split-Path -Parent $SessionsRoot).TrimEnd('\')
$sessionFiles = Get-ChildItem -LiteralPath $SessionsRoot -Recurse -File -Filter '*.jsonl'
[long]$bytesScanned = 0
$maxRecordCharacters = $MaxRecordCharacters
$oversizedMetadataRecordsSkipped = 0

foreach ($file in $sessionFiles) {
    $metadataStream = [IO.File]::Open($file.FullName, [IO.FileMode]::Open, [IO.FileAccess]::Read, [IO.FileShare]::ReadWrite)
    $metadataReader = [IO.StreamReader]::new($metadataStream, [Text.Encoding]::UTF8, $true, 1048576)
    try {
        $firstRecord = Read-HaiBoundedTranscriptRecords -Reader $metadataReader -MaximumCharacters $maxRecordCharacters | Select-Object -First 1
    } finally {
        $metadataReader.Dispose()
        $metadataStream.Dispose()
    }
    if ($null -eq $firstRecord) { continue }
    if ($firstRecord.Oversized) {
        $oversizedMetadataRecordsSkipped++
        continue
    }
    $firstLine = $firstRecord.Text
    if ($firstLine -notmatch [regex]::Escape($RootSessionId)) {
        continue
    }

    $metadata = $firstLine | ConvertFrom-Json
    $childId = [string]$metadata.payload.id
    $childPrefix = if ($childId.Length -ge 8) { $childId.Substring(0, 8) } else { '' }
    $lifecycleEvents = [System.Collections.Generic.List[object]]::new()
    $patchCountsByTurn = @{}
    $turnIdMarker = if ($childPrefix) {
        [regex]::new('"turn_id"\s*:\s*"' + [regex]::Escape($childPrefix), [System.Text.RegularExpressions.RegexOptions]::IgnoreCase)
    } else {
        $null
    }
    $topLevelEventMsg = [regex]::new('^\s*\{\s*(?:"timestamp"\s*:\s*"[^"]+"\s*,\s*)?"type"\s*:\s*"event_msg"', [System.Text.RegularExpressions.RegexOptions]::IgnoreCase)
    $topLevelResponseItem = [regex]::new('^\s*\{\s*(?:"timestamp"\s*:\s*"[^"]+"\s*,\s*)?"type"\s*:\s*"response_item"', [System.Text.RegularExpressions.RegexOptions]::IgnoreCase)
    $patchToolTypeMarker = [regex]::new('"type"\s*:\s*"custom_tool_call"', [System.Text.RegularExpressions.RegexOptions]::IgnoreCase)
    $patchToolNameMarker = [regex]::new('"name"\s*:\s*"apply_patch"', [System.Text.RegularExpressions.RegexOptions]::IgnoreCase)
    $beforeScan = Get-Item -LiteralPath $file.FullName
    [long]$oversizedRecordCount = 0
    $script:malformedCandidateCount = 0

    function Process-TranscriptRecord([string]$RecordLine) {
        if ($null -eq $turnIdMarker -or -not $turnIdMarker.IsMatch($RecordLine)) {
            return
        }
        $isLifecycleCandidate = $topLevelEventMsg.IsMatch($RecordLine)
        $isPatchCandidate = $topLevelResponseItem.IsMatch($RecordLine) -and
            $patchToolTypeMarker.IsMatch($RecordLine) -and $patchToolNameMarker.IsMatch($RecordLine)
        if (-not $isLifecycleCandidate -and -not $isPatchCandidate) {
            return
        }
        try {
            $event = $RecordLine | ConvertFrom-Json -ErrorAction Stop
            if ($isLifecycleCandidate -and $event.type -eq 'event_msg' -and
                [string]$event.payload.type -in @('task_started', 'task_complete', 'turn_aborted', 'task_aborted', 'task_cancelled')) {
                $turnId = [string]$event.payload.turn_id
                if ($turnId.StartsWith($childPrefix, [StringComparison]::OrdinalIgnoreCase)) {
                    $lifecycleEvents.Add([pscustomobject]@{
                        turn_id = $turnId
                        type = [string]$event.payload.type
                        reason = [string]$event.payload.reason
                        last_agent_message = [string]$event.payload.last_agent_message
                    })
                }
            }
            if ($isPatchCandidate -and $event.type -eq 'response_item' -and
                $event.payload.type -eq 'custom_tool_call' -and
                [string]$event.payload.name -eq 'apply_patch') {
                $turnId = [string]$event.payload.internal_chat_message_metadata_passthrough.turn_id
                if ($turnId.StartsWith($childPrefix, [StringComparison]::OrdinalIgnoreCase)) {
                    if (-not $patchCountsByTurn.ContainsKey($turnId)) {
                        $patchCountsByTurn[$turnId] = 0
                    }
                    $patchCountsByTurn[$turnId]++
                }
            }
        } catch {
            $script:malformedCandidateCount++
        }
    }

    $stream = [System.IO.File]::Open(
        $file.FullName,
        [System.IO.FileMode]::Open,
        [System.IO.FileAccess]::Read,
        [System.IO.FileShare]::ReadWrite
    )
    $reader = [System.IO.StreamReader]::new($stream, [System.Text.Encoding]::UTF8, $true, 1048576)
    $buffer = [char[]]::new(65536)
    $lineBuilder = [System.Text.StringBuilder]::new()
    $skippingOversizedRecord = $false
    try {
        [void]$reader.ReadLine()
        while (($readCharacters = $reader.Read($buffer, 0, $buffer.Length)) -gt 0) {
            $offset = 0
            while ($offset -lt $readCharacters) {
                $newline = [Array]::IndexOf($buffer, [char]10, $offset, $readCharacters - $offset)
                $segmentLength = if ($newline -ge 0) { $newline - $offset } else { $readCharacters - $offset }
                if (-not $skippingOversizedRecord) {
                    if ($lineBuilder.Length + $segmentLength -gt $maxRecordCharacters) {
                        $lineBuilder.Clear() | Out-Null
                        $skippingOversizedRecord = $true
                    } elseif ($segmentLength -gt 0) {
                        [void]$lineBuilder.Append($buffer, $offset, $segmentLength)
                    }
                }
                if ($newline -ge 0) {
                    if ($skippingOversizedRecord) {
                        $oversizedRecordCount++
                    } else {
                        $recordLine = $lineBuilder.ToString().TrimEnd("`r")
                        Process-TranscriptRecord $recordLine
                    }
                    $lineBuilder.Clear() | Out-Null
                    $skippingOversizedRecord = $false
                    $offset = $newline + 1
                } else {
                    $offset += $segmentLength
                }
            }
        }
        if ($skippingOversizedRecord) {
            $oversizedRecordCount++
        } elseif ($lineBuilder.Length -gt 0) {
            Process-TranscriptRecord $lineBuilder.ToString().TrimEnd("`r")
        }
    } finally {
        $reader.Dispose()
        $stream.Dispose()
    }
    $afterScan = Get-Item -LiteralPath $file.FullName
    $bytesScanned += [long]$beforeScan.Length
    $changedDuringScan = $beforeScan.Length -ne $afterScan.Length -or
        $beforeScan.LastWriteTimeUtc -ne $afterScan.LastWriteTimeUtc

    $childTurnIds = @($lifecycleEvents | Select-Object -ExpandProperty turn_id -Unique)
    $observedChildTurnId = if ($childTurnIds.Count -eq 1) { [string]$childTurnIds[0] } else { '' }
    $observedTerminalEvents = @($lifecycleEvents | Where-Object {
        $_.turn_id -ceq $observedChildTurnId -and $_.type -in @('task_complete', 'turn_aborted', 'task_aborted', 'task_cancelled')
    })
    $observedLastTerminalEvent = if ($observedTerminalEvents.Count -gt 0) { $observedTerminalEvents[-1] } else { $null }
    $observedTerminalStatus = if ($null -eq $observedLastTerminalEvent) {
        'nonterminal'
    } elseif ($observedLastTerminalEvent.type -eq 'task_complete') {
        'completed'
    } else {
        'aborted'
    }
    $turnAttribution = if ($oversizedRecordCount -gt 0) {
        'oversized_record_unparsed'
    } elseif ($script:malformedCandidateCount -gt 0) {
        'malformed_candidate_record'
    } elseif ($childTurnIds.Count -eq 1) {
        'unique_prefix_match'
    } else {
        'no_unique_match'
    }
    $childTurnId = $observedChildTurnId
    $terminalStatus = if ($turnAttribution -eq 'unique_prefix_match') { 'nonterminal' } else { 'unattributed' }
    $terminalReason = ''
    $finalMessage = ''
    $patchCallCount = 0
    if ($null -ne $observedLastTerminalEvent) {
        if ($observedTerminalStatus -eq 'completed') {
            $finalMessage = [string]$observedLastTerminalEvent.last_agent_message
        } else {
            $terminalReason = [string]$observedLastTerminalEvent.reason
        }
    }
    if ($turnAttribution -eq 'unique_prefix_match') {
        $patchCallCount = [int]$patchCountsByTurn[$childTurnId]
        if ($null -ne $observedLastTerminalEvent) {
            if ($observedTerminalStatus -eq 'completed') {
                $terminalStatus = 'completed'
            } else {
                $terminalStatus = 'aborted'
            }
        }
    }

    $workKind = if ($terminalStatus -ne 'completed') {
        $terminalStatus
    } elseif ($finalMessage -match '(?i)^Stopped\.|preserved and uncommitted|nothing committed or pushed') {
        'partial-report'
    } elseif ($patchCallCount -gt 0) {
        'implementation'
    } else {
        'advisory'
    }

    $relativePath = $file.FullName.Substring($relativeBase.Length).TrimStart('\').Replace('\', '/')
    $allocatedBytes = [HaiTranscriptAllocation]::GetAllocatedBytes($file.FullName)
    $disposition = if ($changedDuringScan) {
        'retain_changed_during_audit'
    } elseif ($oversizedRecordCount -gt 0 -or $script:malformedCandidateCount -gt 0) {
        'retain_unparsed_candidate_record'
    } elseif ($terminalStatus -eq 'completed') {
        'candidate_after_ledger_commit'
    } else {
        'retain'
    }

    $dateMatch = [regex]::Match($file.Name, '^rollout-(?<date>\d{4}-\d{2}-\d{2})T')
    $sessionDate = if ($dateMatch.Success) {
        $dateMatch.Groups['date'].Value
    } else {
        $file.LastWriteTimeUtc.ToString('yyyy-MM-dd')
    }

    $records.Add([pscustomobject]@{
        child_id = $childId
        session_date = $sessionDate
        nickname = [string]$metadata.payload.agent_nickname
        terminal_status = $terminalStatus
        observed_terminal_status = $observedTerminalStatus
        terminal_reason = $terminalReason
        child_turn_id = $childTurnId
        turn_attribution = $turnAttribution
        work_kind = $workKind
        patch_call_count = $patchCallCount
        logical_bytes = [int64]$file.Length
        allocated_bytes = [uint64]$allocatedBytes
        final_message_sha256 = Get-Sha256 -Text $finalMessage
        final_report_preserved = ($terminalStatus -eq 'completed')
        disposition = $disposition
        duplicate_id_file_count = 1
        transcript_sha256 = ''
        session_path = $relativePath
        scan_scope = 'full_file'
        scan_changed_during_audit = $changedDuringScan
        scan_oversized_records = $oversizedRecordCount
        scan_malformed_candidate_records = $script:malformedCandidateCount
        protected_final_message = Protect-ReportText -Text $finalMessage
    })
}

$records = @($records | Sort-Object session_date, child_id)
$duplicateGroups = @($records | Group-Object child_id | Where-Object Count -gt 1)
foreach ($group in $duplicateGroups) {
    foreach ($record in $group.Group) {
        $record.disposition = 'retain_duplicate_id'
        $record.duplicate_id_file_count = $group.Count
    }
}
if ($HashCleanupCandidates) {
    foreach ($record in @($records | Where-Object disposition -eq 'candidate_after_ledger_commit')) {
        $sourcePath = Join-Path $relativeBase ($record.session_path -replace '/', '\\')
        $before = Get-Item -LiteralPath $sourcePath
        $digest = (Get-FileHash -LiteralPath $sourcePath -Algorithm SHA256).Hash.ToLowerInvariant()
        $after = Get-Item -LiteralPath $sourcePath
        if ($before.Length -ne $after.Length -or $before.LastWriteTimeUtc -ne $after.LastWriteTimeUtc) {
            $record.disposition = 'retain_changed_during_audit'
        } else {
            $record.transcript_sha256 = $digest
        }
    }
}
$completed = @($records | Where-Object terminal_status -eq 'completed')
$candidates = @($records | Where-Object disposition -eq 'candidate_after_ledger_commit')
$retained = @($records | Where-Object disposition -ne 'candidate_after_ledger_commit')

$csvHeader = @(
    'child_id', 'session_date', 'nickname', 'terminal_status', 'observed_terminal_status', 'terminal_reason', 'child_turn_id', 'turn_attribution',
    'work_kind', 'patch_call_count', 'logical_bytes', 'allocated_bytes',
    'final_message_sha256', 'final_report_preserved', 'disposition', 'scan_scope', 'scan_changed_during_audit', 'scan_oversized_records', 'scan_malformed_candidate_records',
    'duplicate_id_file_count', 'transcript_sha256', 'session_path'
)
$csvLines = [System.Collections.Generic.List[string]]::new()
$csvLines.Add(($csvHeader | ForEach-Object { ConvertTo-CsvField $_ }) -join ',')
foreach ($record in $records) {
    $values = foreach ($column in $csvHeader) {
        ConvertTo-CsvField ([string]$record.$column)
    }
    $csvLines.Add($values -join ',')
}

$reportLines = [System.Collections.Generic.List[string]]::new()
$reportLines.Add('# Completed child-agent final reports')
$reportLines.Add('')
$reportLines.Add('This generated archive preserves terminal reports observed in the audited')
$reportLines.Add('transcripts. A report marked observed is not proof that the whole file')
$reportLines.Add('was attributable or complete; such transcripts remain retained.')
$reportLines.Add('Credential-shaped values and')
$reportLines.Add('local repository roots are redacted; the manifest retains the SHA-256 of')
$reportLines.Add('the original terminal message. Aborted and nonterminal transcripts are not')
$reportLines.Add('represented as completed work and must be retained. Every transcript sharing')
$reportLines.Add('a child ID is also retained until its duplicate history is reconciled.')
$reportLines.Add('')
foreach ($record in @($records | Where-Object { $_.terminal_status -eq 'completed' -or $_.observed_terminal_status -eq 'completed' })) {
    $agentLabel = if ([string]::IsNullOrWhiteSpace($record.nickname)) {
        '(not recorded)'
    } else {
        $record.nickname
    }
    $reportLines.Add("## $($record.child_id)")
    $reportLines.Add('')
    $reportLines.Add("- Date: $($record.session_date)")
    $reportLines.Add("- Agent: $agentLabel")
    $reportLines.Add("- Work kind: $($record.work_kind)")
    if ($record.terminal_status -ne 'completed') {
        $reportLines.Add('- Status: observed completion only; transcript remains unattributed and retained.')
    }
    $reportLines.Add('- Original report SHA-256: `' + $record.final_message_sha256 + '`')
    $reportLines.Add('- Transcript: `' + $record.session_path + '`')
    $reportLines.Add('')
    if ([string]::IsNullOrWhiteSpace($record.protected_final_message)) {
        $reportLines.Add('_The child completed without a terminal text report._')
    } else {
        $reportLines.Add($record.protected_final_message)
    }
    $reportLines.Add('')
}

$normalizedReportLines = @($reportLines | ForEach-Object { $_.TrimEnd() })
$lastReportLine = $normalizedReportLines.Count - 1
while ($lastReportLine -ge 0 -and $normalizedReportLines[$lastReportLine] -eq '') {
    $lastReportLine--
}
if ($lastReportLine -ge 0) {
    $normalizedReportLines = @($normalizedReportLines[0..$lastReportLine])
}

$summary = [ordered]@{
    generated_at_utc = [DateTime]::UtcNow.ToString('o')
    root_session_id = $RootSessionId
    audited_transcripts = $records.Count
    full_file_scan = ($oversizedMetadataRecordsSkipped -eq 0)
    oversized_metadata_records_skipped = $oversizedMetadataRecordsSkipped
    logical_bytes_scanned = $bytesScanned
    max_jsonl_record_characters = $maxRecordCharacters
    oversized_or_malformed_candidate_records_retained = $true
    unique_child_ids = @($records | Select-Object -ExpandProperty child_id -Unique).Count
    duplicate_child_ids = $duplicateGroups.Count
    completed_transcripts = $completed.Count
    observed_completed_transcripts = @($records | Where-Object observed_terminal_status -eq 'completed').Count
    cleanup_candidate_transcripts = $candidates.Count
    candidate_transcripts_hashed = @($candidates | Where-Object { $_.transcript_sha256 -match '^[0-9a-f]{64}$' }).Count
    candidate_hashing_requested = [bool]$HashCleanupCandidates
    retained_transcripts = $retained.Count
    unattributed_transcripts = @($records | Where-Object terminal_status -eq 'unattributed').Count
    transcripts_with_oversized_records = @($records | Where-Object { [long]$_.scan_oversized_records -gt 0 }).Count
    transcripts_with_malformed_candidate_records = @($records | Where-Object { [long]$_.scan_malformed_candidate_records -gt 0 }).Count
    oversized_records_skipped = [long](($records | Measure-Object -Property scan_oversized_records -Sum).Sum)
    malformed_candidate_records_skipped = [long](($records | Measure-Object -Property scan_malformed_candidate_records -Sum).Sum)
    cleanup_candidate_logical_bytes = [int64](($candidates | Measure-Object logical_bytes -Sum).Sum)
    cleanup_candidate_allocated_bytes = [uint64](($candidates | Measure-Object allocated_bytes -Sum).Sum)
    completed_logical_bytes = [int64](($completed | Measure-Object logical_bytes -Sum).Sum)
    completed_allocated_bytes = [uint64](($completed | Measure-Object allocated_bytes -Sum).Sum)
    retained_logical_bytes = [int64](($retained | Measure-Object logical_bytes -Sum).Sum)
    retained_allocated_bytes = [uint64](($retained | Measure-Object allocated_bytes -Sum).Sum)
    completed_reports_file = 'child-agent-final-reports.md'
    manifest_file = 'child-agent-transcript-manifest.csv'
    deletion_performed = $false
}

[System.IO.Directory]::CreateDirectory($OutputDirectory) | Out-Null
$utf8 = [System.Text.UTF8Encoding]::new($false)
[System.IO.File]::WriteAllLines(
    (Join-Path $OutputDirectory 'child-agent-transcript-manifest.csv'),
    $csvLines,
    $utf8
)
[System.IO.File]::WriteAllLines(
    (Join-Path $OutputDirectory 'child-agent-final-reports.md'),
    $normalizedReportLines,
    $utf8
)
[System.IO.File]::WriteAllText(
    (Join-Path $OutputDirectory 'child-agent-transcript-summary.json'),
    ($summary | ConvertTo-Json -Depth 3),
    $utf8
)

$summary | ConvertTo-Json -Depth 3
