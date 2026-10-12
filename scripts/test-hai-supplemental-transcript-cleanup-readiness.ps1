[CmdletBinding()]
param(
    [string[]]$TranscriptRoots = @(
        'D:\codex-temp\completed-agent-sessions',
        'D:\codex-temp\session-log-relief'
    )
)

$ErrorActionPreference = 'Stop'
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$archiveDir = Join-Path $repoRoot 'docs/child-agent-archive-2026-07-30'
$manifestPath = Join-Path $archiveDir 'supplemental-child-transcript-manifest.csv'
$summaryPath = Join-Path $archiveDir 'supplemental-child-transcript-summary.json'
$reportsPath = Join-Path $archiveDir 'supplemental-child-final-reports.md'
$expectedRootNames = @('completed-agent-sessions', 'session-log-relief')

if ($TranscriptRoots.Count -ne $expectedRootNames.Count) {
    throw 'Exactly the two inventoried supplemental transcript roots are required.'
}

$rootsByName = @{}
foreach ($transcriptRoot in $TranscriptRoots) {
    if (-not (Test-Path -LiteralPath $transcriptRoot -PathType Container)) {
        throw 'A supplemental transcript root is unavailable.'
    }
    $fullPath = (Resolve-Path -LiteralPath $transcriptRoot).Path.TrimEnd('\', '/')
    $leaf = Split-Path -Leaf $fullPath
    if ($expectedRootNames -cnotcontains $leaf -or $rootsByName.ContainsKey($leaf)) {
        throw 'Supplemental transcript roots are duplicated or outside the inventoried root set.'
    }
    $rootItem = Get-Item -LiteralPath $fullPath -Force
    $cursor = $rootItem
    while ($null -ne $cursor) {
        if (($cursor.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw 'A supplemental transcript root or ancestor is a reparse point.'
        }
        $cursor = $cursor.Parent
    }
    $rootsByName[$leaf] = $fullPath
}
if ((@($rootsByName.Keys | Sort-Object) -join ',') -cne (@($expectedRootNames | Sort-Object) -join ',')) {
    throw 'The supplemental root set is incomplete.'
}

$rows = @(Import-Csv -LiteralPath $manifestPath)
$candidates = @($rows | Where-Object disposition -CEQ 'candidate_after_ledger_commit')
$retained = @($rows | Where-Object disposition -CLike 'retain*')
if (@($rows | Group-Object child_id | Where-Object Count -ne 1).Count -gt 0) {
    throw 'Supplemental manifest child IDs are duplicated.'
}
$summary = Get-Content -LiteralPath $summaryPath -Raw | ConvertFrom-Json
$candidateBytes = [long](($candidates | Measure-Object -Property logical_bytes -Sum).Sum)
$retainedBytes = [long](($retained | Measure-Object -Property logical_bytes -Sum).Sum)
if ([int]$summary.audited_transcripts -ne $rows.Count -or
    [long]$summary.logical_bytes_scanned -ne [long](($rows | Measure-Object -Property logical_bytes -Sum).Sum) -or
    [int]$summary.cleanup_candidate_transcripts -ne $candidates.Count -or
    [long]$summary.cleanup_candidate_logical_bytes -ne $candidateBytes -or
    [int]$summary.retained_transcripts -ne $retained.Count -or
    [long]$summary.retained_logical_bytes -ne $retainedBytes -or
    [int]$summary.unattributed_transcripts -ne @($rows | Where-Object terminal_status -CEQ 'unattributed').Count -or
    [int]$summary.transcripts_with_oversized_records -ne @($rows | Where-Object { [int]$_.scan_oversized_records -gt 0 }).Count -or
    [int]$summary.oversized_records_skipped -ne [int](($rows | Measure-Object -Property scan_oversized_records -Sum).Sum) -or
    $summary.deletion_performed -ne $false) {
    throw 'Supplemental generated summary does not match the manifest.'
}

$sourcePaths = @{}
[long]$sourceBytes = 0
foreach ($rootName in $expectedRootNames) {
    $rootPath = [string]$rootsByName[$rootName]
    try {
        $entries = @(Get-ChildItem -LiteralPath $rootPath -Force -Recurse -ErrorAction Stop)
    } catch {
        throw 'A supplemental transcript root could not be completely enumerated.'
    }
    if (@($entries | Where-Object { ($_.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 }).Count -gt 0) {
        throw 'A supplemental transcript root contains a reparse point.'
    }
    foreach ($file in @($entries | Where-Object { -not $_.PSIsContainer })) {
        $relative = $file.FullName.Substring($rootPath.Length + 1).Replace('\', '/')
        $key = "$rootName/$relative"
        if ($sourcePaths.ContainsKey($key)) { throw 'Supplemental roots contain a duplicate relative path.' }
        $sourcePaths[$key] = [long]$file.Length
        $sourceBytes += [long]$file.Length
    }
}

$seenPaths = @{}
foreach ($row in $rows) {
    $relativeKey = ([string]$row.session_path).Replace('\', '/')
    $separator = $relativeKey.IndexOf('/')
    if ($separator -le 0) { throw 'Supplemental manifest path has no root prefix.' }
    $rootName = $relativeKey.Substring(0, $separator)
    if (-not $rootsByName.ContainsKey($rootName) -or $seenPaths.ContainsKey($relativeKey)) {
        throw 'Supplemental manifest path is duplicated or outside the inventoried roots.'
    }
    $relative = $relativeKey.Substring($separator + 1)
    if ([IO.Path]::IsPathRooted($relative) -or @($relative -split '/' | Where-Object { $_ -ceq '.' -or $_ -ceq '..' }).Count -gt 0) {
        throw 'Supplemental manifest path escapes its source root.'
    }
    $rootPath = [string]$rootsByName[$rootName]
    $sourcePath = Join-Path $rootPath ($relative.Replace('/', [IO.Path]::DirectorySeparatorChar))
    if (-not (Test-Path -LiteralPath $sourcePath -PathType Leaf)) {
        throw 'A supplemental transcript listed in the manifest is missing.'
    }
    $item = Get-Item -LiteralPath $sourcePath -Force
    if ([long]$item.Length -ne [long]$row.logical_bytes -or
        -not $sourcePaths.ContainsKey($relativeKey) -or [long]$sourcePaths[$relativeKey] -ne [long]$row.logical_bytes) {
        throw 'Supplemental transcript size or exact archive inventory differs from the manifest.'
    }
    if ([string]$row.disposition -ceq 'candidate_after_ledger_commit') {
        $childId = [string]$row.child_id
        $childTurnId = [string]$row.child_turn_id
        if ([string]$row.terminal_status -cne 'completed' -or
            [string]$row.final_report_preserved -cne 'True' -or
            [string]$row.turn_attribution -cne 'unique_prefix_match' -or
            $childId.Length -lt 8 -or
            -not $childTurnId.StartsWith($childId.Substring(0, 8), [StringComparison]::OrdinalIgnoreCase) -or
            [int]$row.duplicate_id_file_count -ne 1 -or
            [string]$row.transcript_sha256 -cnotmatch '^[0-9a-f]{64}$') {
            throw 'A supplemental candidate lacks completed, unique, reported, hash-ledgered metadata.'
        }
        if ((Get-FileHash -LiteralPath $sourcePath -Algorithm SHA256).Hash.ToLowerInvariant() -cne [string]$row.transcript_sha256) {
            throw 'A supplemental candidate transcript hash differs from the manifest.'
        }
    }
    $seenPaths[$relativeKey] = $true
}
if ($seenPaths.Count -ne $rows.Count -or $sourcePaths.Count -ne $rows.Count -or
    $sourceBytes -ne [long](($rows | Measure-Object -Property logical_bytes -Sum).Sum)) {
    throw 'Supplemental source roots do not exactly match the manifest path and byte inventory.'
}

$reportText = Get-Content -LiteralPath $reportsPath -Raw
foreach ($candidate in $candidates) {
    if (-not $reportText.Contains([string]$candidate.child_id)) {
        throw 'A supplemental cleanup candidate has no preserved completed report.'
    }
}

$git = Get-Command git -ErrorAction Stop
$gitPrefix = @('-c', "safe.directory=$repoRoot", '-C', $repoRoot)
$ledgerPaths = @(
    'docs/child-agent-archive-2026-07-30/supplemental-child-transcript-manifest.csv',
    'docs/child-agent-archive-2026-07-30/supplemental-child-transcript-summary.json',
    'docs/child-agent-archive-2026-07-30/supplemental-child-batch-audit.md',
    'docs/child-agent-archive-2026-07-30/supplemental-child-final-reports.md'
)
foreach ($ledgerPath in $ledgerPaths) {
    $trackedLedger = @(& $git.Source @gitPrefix ls-files --error-unmatch -- $ledgerPath 2>$null)
    if ($LASTEXITCODE -ne 0 -or $trackedLedger.Count -ne 1 -or [string]$trackedLedger[0] -cne $ledgerPath) {
        throw 'Supplemental transcript evidence is not tracked by the repository.'
    }
    & $git.Source @gitPrefix diff --quiet HEAD -- $ledgerPath 2>$null
    if ($LASTEXITCODE -ne 0) { throw 'Supplemental transcript evidence has uncommitted worktree changes.' }
    & $git.Source @gitPrefix diff --cached --quiet HEAD -- $ledgerPath 2>$null
    if ($LASTEXITCODE -ne 0) { throw 'Supplemental transcript evidence has staged changes.' }
}
$trackedPaths = [Collections.Generic.HashSet[string]]::new([StringComparer]::OrdinalIgnoreCase)
foreach ($trackedPath in @(& $git.Source @gitPrefix ls-files)) { [void]$trackedPaths.Add(([string]$trackedPath).Replace('\', '/')) }
if ($LASTEXITCODE -ne 0) { throw 'Git could not enumerate tracked source files for report verification.' }
$sourceLinks = [regex]::Matches($reportText, '\]\(\.\./\.\./([^)]+)\)')
if ($sourceLinks.Count -eq 0) { throw 'Supplemental completed reports contain no repository source links to verify.' }
$verifiedLinks = [Collections.Generic.HashSet[string]]::new([StringComparer]::OrdinalIgnoreCase)
foreach ($match in $sourceLinks) {
    $sourceRelative = $match.Groups[1].Value.Replace('\\', '/')
    if ([IO.Path]::IsPathRooted($sourceRelative) -or @($sourceRelative -split '/' | Where-Object { $_ -ceq '.' -or $_ -ceq '..' }).Count -gt 0) {
        throw 'A supplemental report source link escapes the repository.'
    }
    if (-not $trackedPaths.Contains($sourceRelative) -or -not (Test-Path -LiteralPath (Join-Path $repoRoot ($sourceRelative.Replace('/', [IO.Path]::DirectorySeparatorChar))) -PathType Leaf)) {
        throw 'A supplemental report source link is not present and tracked in the repository.'
    }
    [void]$verifiedLinks.Add($sourceRelative)
}

[pscustomobject][ordered]@{
    result = 'supplemental_source_archive_verified'
    supplemental_manifest_rows = $rows.Count
    candidate_files = $candidates.Count
    candidate_bytes = [long](($candidates | Measure-Object -Property logical_bytes -Sum).Sum)
    candidate_hashes_verified = $candidates.Count
    retained_files = $retained.Count
    source_archive_files = $sourcePaths.Count
    source_archive_logical_bytes = $sourceBytes
    report_source_paths_verified = $verifiedLinks.Count
    deletion_performed = $false
    cleanup_authorized = $false
} | ConvertTo-Json -Depth 4
