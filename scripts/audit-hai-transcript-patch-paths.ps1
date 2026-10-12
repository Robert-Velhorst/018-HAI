param(
    [Parameter(Mandatory)][string]$TranscriptRoot,
    [Parameter(Mandatory)][string]$SourceCheckoutRoot,
    [Parameter(Mandatory)][string]$RepositoryRoot,
    [int]$MaxMissingPaths = 100,
    [ValidateRange(1, 10485760)][int]$MaxRecordCharacters = 10485760
)

$ErrorActionPreference = 'Stop'

if ($PSVersionTable.PSVersion.Major -lt 7) {
    throw 'PowerShell 7 or later is required for ConvertFrom-Json -AsHashtable.'
}

if ($MaxMissingPaths -lt 0) {
    throw 'MaxMissingPaths must be zero or greater.'
}

function Read-BoundedTranscriptRecords {
    param(
        [Parameter(Mandatory)][IO.StreamReader]$Reader,
        [Parameter(Mandatory)][int]$MaximumCharacters
    )

    $buffer = [char[]]::new(65536)
    $builder = [Text.StringBuilder]::new()
    $skippingOversized = $false
    while (($readCharacters = $Reader.Read($buffer, 0, $buffer.Length)) -gt 0) {
        $offset = 0
        while ($offset -lt $readCharacters) {
            $newline = [Array]::IndexOf($buffer, [char]10, $offset, $readCharacters - $offset)
            $segmentLength = if ($newline -ge 0) { $newline - $offset } else { $readCharacters - $offset }
            if (-not $skippingOversized) {
                if ($builder.Length + $segmentLength -gt $MaximumCharacters) {
                    [void]$builder.Clear()
                    $skippingOversized = $true
                } elseif ($segmentLength -gt 0) {
                    [void]$builder.Append($buffer, $offset, $segmentLength)
                }
            }

            if ($newline -ge 0) {
                if ($skippingOversized) {
                    [pscustomobject]@{ Text = ''; Oversized = $true }
                } else {
                    [pscustomobject]@{ Text = $builder.ToString().TrimEnd("`r"); Oversized = $false }
                }
                [void]$builder.Clear()
                $skippingOversized = $false
                $offset = $newline + 1
            } else {
                $offset += $segmentLength
            }
        }
    }

    if ($skippingOversized) {
        [pscustomobject]@{ Text = ''; Oversized = $true }
    } elseif ($builder.Length -gt 0) {
        [pscustomobject]@{ Text = $builder.ToString().TrimEnd("`r"); Oversized = $false }
    }
}

$transcriptPath = (Resolve-Path -LiteralPath $TranscriptRoot -ErrorAction Stop).Path.TrimEnd('\', '/')
$sourceCheckoutPath = (Resolve-Path -LiteralPath $SourceCheckoutRoot -ErrorAction Stop).Path.TrimEnd('\', '/')
$repositoryPath = (Resolve-Path -LiteralPath $RepositoryRoot -ErrorAction Stop).Path.TrimEnd('\', '/')
$primaryPrefix = $sourceCheckoutPath + '\'
$scratchMarker = '\work\018-hai-port-engine-control\'
$temporaryMarker = '\AppData\Local\Temp\'
$paths = [Collections.Generic.HashSet[string]]::new([StringComparer]::OrdinalIgnoreCase)
$primaryReferences = 0
$scratchReferences = 0
$temporaryReferences = 0
$otherReferences = 0
$eventCount = 0
$successfulEvents = 0
$failedEvents = 0
$markerRecords = 0
$nonEventMarkers = 0
$parseErrors = 0
$oversizedRecordsSkipped = 0
$filesWithOversizedRecords = 0

$trackedPaths = [Collections.Generic.HashSet[string]]::new([StringComparer]::OrdinalIgnoreCase)
$trackedFileList = @(git -C $repositoryPath ls-files)
if ($LASTEXITCODE -ne 0) {
    throw 'git ls-files failed; repository comparison is incomplete.'
}
foreach ($trackedPath in $trackedFileList) {
    [void]$trackedPaths.Add($trackedPath.Replace('/', '\'))
}

$files = @(Get-ChildItem -LiteralPath $transcriptPath -File -Filter '*.jsonl' | Sort-Object Name)
foreach ($file in $files) {
    $stream = [IO.FileStream]::new($file.FullName, [IO.FileMode]::Open, [IO.FileAccess]::Read, [IO.FileShare]::ReadWrite, 1048576, [IO.FileOptions]::SequentialScan)
    $reader = [IO.StreamReader]::new($stream, [Text.Encoding]::UTF8, $true, 1048576)
    $fileHadOversizedRecord = $false
    try {
        Read-BoundedTranscriptRecords -Reader $reader -MaximumCharacters $MaxRecordCharacters | ForEach-Object {
            $scannedRecord = $_
            if ($scannedRecord.Oversized) {
                $oversizedRecordsSkipped++
                $fileHadOversizedRecord = $true
            } else {
                $line = $scannedRecord.Text
                if ($line.Contains('patch_apply_end')) {
                    $markerRecords++
                    $record = $null
                    try {
                        $record = ConvertFrom-Json -InputObject $line -AsHashtable -Depth 100
                    }
                    catch {
                        $parseErrors++
                    }
                    if ($null -ne $record) {
                        if ($record.type -ne 'event_msg' -or $record.payload.type -ne 'patch_apply_end') {
                            $nonEventMarkers++
                        } else {
                            $eventCount++
                            if ($record.payload.success -eq $true) { $successfulEvents++ } else { $failedEvents++ }

                            foreach ($path in @($record.payload.changes.Keys)) {
                                if ([string]::IsNullOrWhiteSpace($path)) { continue }
                                [void]$paths.Add($path)

                                if ($path.StartsWith($primaryPrefix, [StringComparison]::OrdinalIgnoreCase)) {
                                    $primaryReferences++
                                }
                                elseif ($path -match [regex]::Escape($scratchMarker)) {
                                    $scratchReferences++
                                }
                                elseif ($path -match [regex]::Escape($temporaryMarker)) {
                                    $temporaryReferences++
                                }
                                else {
                                    $otherReferences++
                                }
                            }
                        }
                    }
                }
            }
        }
    }
    finally {
        if ($fileHadOversizedRecord) { $filesWithOversizedRecords++ }
        $reader.Dispose()
        $stream.Dispose()
    }
}

$primaryPaths = @($paths | Where-Object { $_.StartsWith($primaryPrefix, [StringComparison]::OrdinalIgnoreCase) } | Sort-Object)
$missingPaths = [Collections.Generic.List[string]]::new()
$trackedPrimaryPaths = 0
$presentUntrackedPaths = 0
$aliasedPaths = 0
foreach ($path in $primaryPaths) {
    $relativePath = $path.Substring($primaryPrefix.Length).Replace('/', '\')
    $repositoryRelativePath = $relativePath -replace '^hai-repo-link\\', ''
    if ($repositoryRelativePath -ne $relativePath) { $aliasedPaths++ }
    if ($trackedPaths.Contains($repositoryRelativePath)) {
        $trackedPrimaryPaths++
        continue
    }

    $candidate = Join-Path $repositoryPath $repositoryRelativePath
    if (Test-Path -LiteralPath $candidate -PathType Leaf) {
        $presentUntrackedPaths++
    }
    else {
        $missingPaths.Add($path)
    }
}

$summary = [ordered]@{
    transcript_files = $files.Count
    transcript_bytes = (($files | Measure-Object -Property Length -Sum).Sum)
    marker_records = $markerRecords
    oversized_records_skipped = $oversizedRecordsSkipped
    files_with_oversized_records = $filesWithOversizedRecords
    max_record_characters = $MaxRecordCharacters
    patch_event_scan_complete = ($oversizedRecordsSkipped -eq 0 -and $parseErrors -eq 0)
    patch_events = $eventCount
    non_event_marker_records = $nonEventMarkers
    successful_events = $successfulEvents
    failed_events = $failedEvents
    parse_errors = $parseErrors
    unique_target_paths = $paths.Count
    path_references = $primaryReferences + $scratchReferences + $temporaryReferences + $otherReferences
    primary_path_references = $primaryReferences
    scratch_path_references = $scratchReferences
    temporary_path_references = $temporaryReferences
    other_path_references = $otherReferences
    unique_primary_paths = $primaryPaths.Count
    primary_paths_resolved_through_hai_repo_link_alias = $aliasedPaths
    primary_paths_tracked_in_repository = $trackedPrimaryPaths
    primary_paths_present_but_untracked = $presentUntrackedPaths
    primary_paths_missing_from_repository = $missingPaths.Count
    missing_paths = @($missingPaths | Select-Object -First $MaxMissingPaths | ForEach-Object { $_.Substring($primaryPrefix.Length) })
    missing_paths_truncated = ($missingPaths.Count -gt $MaxMissingPaths)
}

$summary | ConvertTo-Json -Depth 5
