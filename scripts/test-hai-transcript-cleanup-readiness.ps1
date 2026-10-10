param(
    [string]$TranscriptRoot,
    [switch]$RequireSourceArchive,
    [switch]$RequireCommittedLedger,
    [switch]$RequireMergedPullRequest,
    [int]$PullRequestNumber = 36,
    [string]$GitHubRepository = 'Robert-Velhorst/018-HAI'
)

$ErrorActionPreference = 'Stop'
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$archiveDir = Join-Path $repoRoot 'docs/child-agent-archive-2026-07-30'
$manifestPath = Join-Path $archiveDir 'child-agent-transcript-manifest.csv'
$summaryPath = Join-Path $archiveDir 'child-agent-transcript-summary.json'
$reportsPath = Join-Path $archiveDir 'child-agent-final-reports.md'
$readinessPath = Join-Path $archiveDir 'cleanup-readiness.md'

function Stop-ReadinessCheck([string]$Message) {
    throw "Transcript cleanup readiness failed: $Message"
}

if (-not (Test-Path -LiteralPath $manifestPath -PathType Leaf) -or
    -not (Test-Path -LiteralPath $summaryPath -PathType Leaf) -or
    -not (Test-Path -LiteralPath $reportsPath -PathType Leaf) -or
    -not (Test-Path -LiteralPath $readinessPath -PathType Leaf)) {
    Stop-ReadinessCheck 'one or more committed ledger artifacts are missing.'
}

$summary = Get-Content -LiteralPath $summaryPath -Raw | ConvertFrom-Json
$rows = @(Import-Csv -LiteralPath $manifestPath)
$candidates = @($rows | Where-Object disposition -CEQ 'candidate_after_ledger_commit')
$retained = @($rows | Where-Object disposition -CLike 'retain*')

if ($summary.deletion_performed -ne $false) {
    Stop-ReadinessCheck 'the summary must state deletion_performed=false.'
}
if ($rows.Count -ne [int]$summary.audited_transcripts -or
    $candidates.Count -ne [int]$summary.cleanup_candidate_transcripts -or
    $retained.Count -ne [int]$summary.retained_transcripts) {
    Stop-ReadinessCheck 'manifest rows do not match the recorded summary counts.'
}
$candidateBytes = [long](($candidates | Measure-Object -Property logical_bytes -Sum).Sum)
$retainedBytes = [long](($retained | Measure-Object -Property logical_bytes -Sum).Sum)
if ($candidateBytes -ne [long]$summary.cleanup_candidate_logical_bytes -or
    $retainedBytes -ne [long]$summary.retained_logical_bytes) {
    Stop-ReadinessCheck 'manifest byte totals do not match the recorded summary.'
}
if ($candidates.Count -eq 0 -or
    @($candidates | Group-Object child_id | Where-Object Count -ne 1).Count -gt 0) {
    Stop-ReadinessCheck 'cleanup candidate child IDs are empty or duplicated.'
}

foreach ($candidate in $candidates) {
    if ($candidate.terminal_status -cne 'completed' -or
        $candidate.final_report_preserved -cne 'True' -or
        [int]$candidate.duplicate_id_file_count -ne 1 -or
        $candidate.transcript_sha256 -notmatch '^[0-9a-f]{64}$' -or
        [long]$candidate.logical_bytes -le 0) {
        Stop-ReadinessCheck "candidate metadata is incomplete or unsafe for child $($candidate.child_id)."
    }
}

$reportText = Get-Content -LiteralPath $reportsPath -Raw
$crosswalkText = Get-Content -LiteralPath $readinessPath -Raw
$crosswalkStart = $crosswalkText.IndexOf('### Completed-report integration crosswalk', [StringComparison]::Ordinal)
$crosswalkEnd = $crosswalkText.IndexOf('**Transcript cleanup gate', [StringComparison]::Ordinal)
if ($crosswalkStart -lt 0 -or $crosswalkEnd -le $crosswalkStart) {
    Stop-ReadinessCheck 'the bounded completed-report crosswalk section is missing.'
}
$crosswalkBody = $crosswalkText.Substring($crosswalkStart, $crosswalkEnd - $crosswalkStart)
$crosswalkIds = @([regex]::Matches($crosswalkBody, '(?m)^\| `([0-9a-f-]{36})` \|') | ForEach-Object { $_.Groups[1].Value })
if (@($crosswalkIds | Group-Object | Where-Object Count -ne 1).Count -gt 0) {
    Stop-ReadinessCheck 'the completed-report crosswalk contains duplicate child IDs.'
}
if ($crosswalkIds.Count -ne $candidates.Count) {
    Stop-ReadinessCheck 'crosswalk rows do not exactly match the cleanup candidate count.'
}

$crosswalkSourcePaths = @(
    [regex]::Matches($crosswalkBody, '`((?:backend|frontend|idp|scripts)/[^`]+)`') |
        ForEach-Object { $_.Groups[1].Value } |
        Sort-Object -Unique
)
foreach ($sourcePath in $crosswalkSourcePaths) {
    $relativePath = $sourcePath.TrimEnd('/')
    $localPath = Join-Path $repoRoot ($relativePath -replace '/', [IO.Path]::DirectorySeparatorChar)
    if (-not (Test-Path -LiteralPath $localPath)) {
        Stop-ReadinessCheck "crosswalk source path does not exist: $relativePath"
    }
}

foreach ($candidate in $candidates) {
    $id = [string]$candidate.child_id
    if (@($crosswalkIds | Where-Object { $_ -ceq $id }).Count -ne 1) {
        Stop-ReadinessCheck "candidate $id does not have exactly one integration crosswalk row."
    }
    if (-not $reportText.Contains($id)) {
        Stop-ReadinessCheck "candidate $id has no preserved report in the committed report file."
    }
}

$sourceVerified = $false
$committedLedgerVerified = $false
$mergedPullRequestVerified = $false
$repositoryHead = $null
$pullRequestMergeCommit = $null
$cleanupGateReady = $false
if ($RequireSourceArchive -and [string]::IsNullOrWhiteSpace($TranscriptRoot)) {
    Stop-ReadinessCheck '-RequireSourceArchive requires -TranscriptRoot.'
}
if (-not [string]::IsNullOrWhiteSpace($TranscriptRoot)) {
    if (-not (Test-Path -LiteralPath $TranscriptRoot -PathType Container)) {
        Stop-ReadinessCheck 'the source transcript archive path is unavailable.'
    }
    $archiveFullPath = (Resolve-Path -LiteralPath $TranscriptRoot).Path.TrimEnd([IO.Path]::DirectorySeparatorChar, [IO.Path]::AltDirectorySeparatorChar)
    $archiveLeaf = Split-Path -Leaf $archiveFullPath
    $archiveRoot = Get-Item -LiteralPath $archiveFullPath -Force
    if (($archiveRoot.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
        Stop-ReadinessCheck 'the source archive root is a reparse point.'
    }
    try {
        $sourceEntries = @(Get-ChildItem -LiteralPath $archiveFullPath -Force -Recurse -ErrorAction Stop)
    }
    catch {
        Stop-ReadinessCheck 'the source archive could not be completely enumerated.'
    }
    if (@($sourceEntries | Where-Object { ($_.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 }).Count -gt 0) {
        Stop-ReadinessCheck 'the source archive contains a reparse point.'
    }
    $sourceFiles = @($sourceEntries | Where-Object { -not $_.PSIsContainer })
    $sourcePaths = @{}
    [long]$sourceArchiveBytes = 0
    foreach ($sourceFile in $sourceFiles) {
        $sourceRelative = $sourceFile.FullName.Substring($archiveFullPath.Length + 1).Replace('\', '/')
        if ($sourcePaths.ContainsKey($sourceRelative)) {
            Stop-ReadinessCheck "the source archive contains a duplicate path: $sourceRelative."
        }
        $sourcePaths[$sourceRelative] = $true
        $sourceArchiveBytes += [long]$sourceFile.Length
    }
    $seenPaths = @{}
    foreach ($entry in $rows) {
        $relativeKey = ([string]$entry.session_path).Replace('\', '/')
        $prefix = $archiveLeaf + '/'
        if (-not $relativeKey.StartsWith($prefix, [StringComparison]::Ordinal) -or
            [IO.Path]::IsPathRooted($relativeKey) -or
            @($relativeKey -split '/' | Where-Object { $_ -ceq '.' -or $_ -ceq '..' }).Count -gt 0) {
            Stop-ReadinessCheck "transcript $($entry.child_id) has a path outside the declared archive root."
        }
        $sourceRelativeKey = $relativeKey.Substring($prefix.Length)
        $relative = $sourceRelativeKey.Replace('/', [IO.Path]::DirectorySeparatorChar)
        if ([IO.Path]::IsPathRooted($relative)) {
            Stop-ReadinessCheck "transcript $($entry.child_id) has a path outside the declared archive root."
        }
        if ($seenPaths.ContainsKey($sourceRelativeKey)) {
            Stop-ReadinessCheck "manifest contains a duplicate transcript path: $relativeKey."
        }
        $seenPaths[$sourceRelativeKey] = $true
        $sourcePath = Join-Path $archiveFullPath $relative
        if (-not (Test-Path -LiteralPath $sourcePath -PathType Leaf)) {
            Stop-ReadinessCheck "transcript source file is missing: $($entry.session_path)."
        }
        $sourceItem = Get-Item -LiteralPath $sourcePath -Force
        if ($sourceItem.Length -ne [long]$entry.logical_bytes) {
            Stop-ReadinessCheck "transcript source file has changed size: $($entry.session_path)."
        }
        if ([string]$entry.disposition -ceq 'candidate_after_ledger_commit') {
            $actualHash = (Get-FileHash -LiteralPath $sourcePath -Algorithm SHA256).Hash.ToLowerInvariant()
            if ($actualHash -cne [string]$entry.transcript_sha256) {
                Stop-ReadinessCheck "candidate source hash does not match the manifest: $($entry.session_path)."
            }
        }
    }
    if ($seenPaths.Count -ne $rows.Count -or $sourcePaths.Count -ne $rows.Count) {
        Stop-ReadinessCheck 'source archive file count does not exactly match the manifest.'
    }
    foreach ($relative in $seenPaths.Keys) {
        if (-not $sourcePaths.ContainsKey($relative)) {
            Stop-ReadinessCheck "source archive contains a missing or unlisted manifest path: $relative."
        }
    }
    $manifestBytes = [long](($rows | Measure-Object -Property logical_bytes -Sum).Sum)
    $ledgerBytes = [long]$summary.cleanup_candidate_logical_bytes + [long]$summary.retained_logical_bytes
    if ($sourceArchiveBytes -ne $manifestBytes -or $sourceArchiveBytes -ne $ledgerBytes) {
        Stop-ReadinessCheck 'source archive bytes do not exactly match the manifest and ledger totals.'
    }
    $sourceVerified = $true
}

if ($RequireMergedPullRequest) { $RequireCommittedLedger = $true }
if ($RequireCommittedLedger) {
    $git = Get-Command git -ErrorAction SilentlyContinue
    if ($null -eq $git) { Stop-ReadinessCheck 'git is unavailable for committed-ledger verification.' }
    $gitPrefix = @('-c', "safe.directory=$repoRoot", '-C', $repoRoot)
    $repositoryHead = (& $git.Source @gitPrefix rev-parse --verify HEAD 2>$null | Out-String).Trim()
    if ($LASTEXITCODE -ne 0 -or $repositoryHead -notmatch '^[0-9a-f]{40}$') {
        Stop-ReadinessCheck 'the repository HEAD could not be verified.'
    }
    $branch = (& $git.Source @gitPrefix branch --show-current 2>$null | Out-String).Trim()
    if ($LASTEXITCODE -ne 0 -or $branch -cne 'main') {
        Stop-ReadinessCheck 'committed-ledger cleanup checks must run from the canonical main branch.'
    }
    $origin = (& $git.Source @gitPrefix remote get-url origin 2>$null | Out-String).Trim()
    if ($LASTEXITCODE -ne 0 -or $origin -notmatch '(?i)(github\.com[:/]Robert-Velhorst/018-HAI(?:\.git)?$)') {
        Stop-ReadinessCheck 'origin does not identify the expected HAI repository.'
    }
    $ledgerFiles = @(
        'docs/child-agent-archive-2026-07-30/child-agent-transcript-manifest.csv',
        'docs/child-agent-archive-2026-07-30/child-agent-transcript-summary.json',
        'docs/child-agent-archive-2026-07-30/child-agent-final-reports.md',
        'docs/child-agent-archive-2026-07-30/cleanup-readiness.md'
    )
    foreach ($relativePath in $ledgerFiles) {
        $tracked = @(& $git.Source @gitPrefix ls-files --error-unmatch -- $relativePath 2>$null)
        if ($LASTEXITCODE -ne 0 -or $tracked.Count -ne 1 -or [string]$tracked[0] -cne $relativePath) {
            Stop-ReadinessCheck "ledger file is not tracked: $relativePath."
        }
        & $git.Source @gitPrefix diff --quiet HEAD -- $relativePath 2>$null
        if ($LASTEXITCODE -ne 0) { Stop-ReadinessCheck "ledger file has uncommitted worktree changes: $relativePath." }
        & $git.Source @gitPrefix diff --cached --quiet HEAD -- $relativePath 2>$null
        if ($LASTEXITCODE -ne 0) { Stop-ReadinessCheck "ledger file has staged changes: $relativePath." }
        & $git.Source @gitPrefix cat-file -e "HEAD:$relativePath" 2>$null
        if ($LASTEXITCODE -ne 0) { Stop-ReadinessCheck "ledger file is absent from committed HEAD: $relativePath." }
    }
    $committedLedgerVerified = $true
}

if ($RequireMergedPullRequest) {
    $gh = Get-Command gh -ErrorAction SilentlyContinue
    if ($null -eq $gh) { Stop-ReadinessCheck 'GitHub CLI is unavailable for merged-PR verification.' }
    if ($PullRequestNumber -le 0 -or $GitHubRepository -cne 'Robert-Velhorst/018-HAI') {
        Stop-ReadinessCheck 'pull request identity is invalid or does not match the expected HAI repository.'
    }
    $prJson = @(& $gh.Source pr view $PullRequestNumber --repo $GitHubRepository --json state,mergedAt,baseRefName,mergeCommit 2>$null)
    if ($LASTEXITCODE -ne 0 -or $prJson.Count -ne 1) {
        Stop-ReadinessCheck 'GitHub could not verify the pull request; reauthentication or network access may be required.'
    }
    try { $pr = [string]$prJson[0] | ConvertFrom-Json -ErrorAction Stop }
    catch { Stop-ReadinessCheck 'GitHub returned invalid pull request metadata.' }
    $pullRequestMergeCommit = [string]$pr.mergeCommit.oid
    if ([string]$pr.state -cne 'CLOSED' -or [string]::IsNullOrWhiteSpace([string]$pr.mergedAt) -or
        [string]$pr.baseRefName -cne 'main' -or $pullRequestMergeCommit -notmatch '^[0-9a-f]{40}$') {
        Stop-ReadinessCheck 'the integration pull request is not verified as merged into main.'
    }
    $checkOutput = @(& $gh.Source pr checks $PullRequestNumber --repo $GitHubRepository --json name,state 2>$null)
    if ($LASTEXITCODE -ne 0 -or $checkOutput.Count -eq 0) {
        Stop-ReadinessCheck 'GitHub pull-request check results are unavailable or empty.'
    }
    try { $pullRequestChecks = ($checkOutput -join [Environment]::NewLine) | ConvertFrom-Json -ErrorAction Stop }
    catch { Stop-ReadinessCheck 'GitHub returned invalid pull-request check metadata.' }
    $pullRequestChecks = @($pullRequestChecks)
    if ($pullRequestChecks.Count -eq 0 -or @($pullRequestChecks | Where-Object { [string]$_.state -cne 'SUCCESS' }).Count -gt 0) {
        Stop-ReadinessCheck 'one or more pull-request checks are not successful.'
    }
    & $git.Source @gitPrefix merge-base --is-ancestor $pullRequestMergeCommit $repositoryHead 2>$null
    if ($LASTEXITCODE -ne 0) {
        Stop-ReadinessCheck 'the verified pull-request merge commit is not an ancestor of the checked-out main history.'
    }
    $mergedPullRequestVerified = $true
}

$cleanupGateReady = $sourceVerified -and $committedLedgerVerified -and $mergedPullRequestVerified

[pscustomobject][ordered]@{
    result = if ($sourceVerified) { 'source_archive_verified' } else { 'ledger_verified_source_not_checked' }
    repository_root = $repoRoot
    manifest_rows = $rows.Count
    candidate_files = $candidates.Count
    candidate_bytes = [long]$summary.cleanup_candidate_logical_bytes
    retained_files = $retained.Count
    source_archive_files = if ($sourceVerified) { $sourcePaths.Count } else { $null }
    source_archive_logical_bytes = if ($sourceVerified) { $sourceArchiveBytes } else { $null }
    crosswalk_candidate_ids = $crosswalkIds.Count
    source_archive_verified = $sourceVerified
    committed_ledger_verified = $committedLedgerVerified
    merged_pull_request_verified = $mergedPullRequestVerified
    repository_head = $repositoryHead
    pull_request_merge_commit = $pullRequestMergeCommit
    cleanup_gate_ready = $cleanupGateReady
    deletion_performed = $false
    cleanup_authorized = $false
} | ConvertTo-Json -Depth 4
