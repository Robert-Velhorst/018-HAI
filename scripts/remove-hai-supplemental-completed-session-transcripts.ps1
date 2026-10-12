[CmdletBinding(SupportsShouldProcess = $true, ConfirmImpact = 'High')]
param(
    [string[]]$TranscriptRoots = @(
        'D:\codex-temp\completed-agent-sessions',
        'D:\codex-temp\session-log-relief'
    ),
    [string[]]$ChildIds = @(),
    [switch]$Apply,
    [string]$ConfirmationPhrase = ''
)

$ErrorActionPreference = 'Stop'
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$manifestPath = Join-Path $repoRoot 'docs/child-agent-archive-2026-07-30/supplemental-child-transcript-manifest.csv'
$sourceCheck = Join-Path $PSScriptRoot 'test-hai-supplemental-transcript-cleanup-readiness.ps1'
$mainCheck = Join-Path $PSScriptRoot 'test-hai-transcript-cleanup-readiness.ps1'
$expectedRoots = @('D:\codex-temp\completed-agent-sessions', 'D:\codex-temp\session-log-relief')

if ($TranscriptRoots.Count -ne 2 -or -not (Test-Path -LiteralPath $manifestPath -PathType Leaf)) {
    throw 'The exact supplemental roots and committed manifest are required.'
}
$resolvedRoots = @{}
foreach ($root in $TranscriptRoots) {
    $resolved = (Resolve-Path -LiteralPath $root -ErrorAction Stop).Path.TrimEnd('\', '/')
    $leaf = Split-Path -Leaf $resolved
    $expected = $expectedRoots | Where-Object { (Split-Path -Leaf $_) -ceq $leaf }
    if (@($expected).Count -ne 1 -or $resolved -cne $expected.TrimEnd('\', '/') -or $resolvedRoots.ContainsKey($leaf)) {
        throw 'Supplemental cleanup is restricted to the two exact inventoried source roots.'
    }
    if ((Get-Item -LiteralPath $resolved -Force).Attributes -band [IO.FileAttributes]::ReparsePoint) {
        throw 'A supplemental source root is a reparse point.'
    }
    $resolvedRoots[$leaf] = $resolved
}
if (@($expectedRoots | Where-Object { -not $resolvedRoots.ContainsKey((Split-Path -Leaf $_)) }).Count -gt 0) {
    throw 'Both supplemental source roots must be supplied.'
}

$manifest = @(Import-Csv -LiteralPath $manifestPath)
$candidates = @($manifest | Where-Object disposition -CEQ 'candidate_after_ledger_commit')
$ids = @($ChildIds | ForEach-Object { ([string]$_).ToLowerInvariant() } | Sort-Object -Unique)
$selected = if ($ids.Count -eq 0) { $candidates } else { @($candidates | Where-Object { $ids -ccontains ([string]$_.child_id).ToLowerInvariant() }) }
if ($selected.Count -eq 0 -or ($ids.Count -gt 0 -and $selected.Count -ne $ids.Count) -or
    @($selected | Group-Object child_id | Where-Object Count -ne 1).Count -gt 0) {
    throw 'Each requested child ID must identify exactly one supplemental cleanup candidate.'
}

function Resolve-SupplementalCandidate($Entry) {
    $id = [string]$Entry.child_id
    $turnId = [string]$Entry.child_turn_id
    if ([string]$Entry.disposition -cne 'candidate_after_ledger_commit' -or
        [string]$Entry.terminal_status -cne 'completed' -or
        [string]$Entry.final_report_preserved -cne 'True' -or
        [string]$Entry.turn_attribution -cne 'unique_prefix_match' -or
        $id.Length -lt 8 -or -not $turnId.StartsWith($id.Substring(0, 8), [StringComparison]::OrdinalIgnoreCase) -or
        [int]$Entry.duplicate_id_file_count -ne 1 -or [int]$Entry.scan_oversized_records -ne 0 -or
        [int]$Entry.scan_malformed_candidate_records -ne 0 -or [string]$Entry.transcript_sha256 -cnotmatch '^[0-9a-f]{64}$') {
        throw 'Supplemental candidate does not satisfy the completed, unique, reported, hashed contract.'
    }
    $relativeKey = ([string]$Entry.session_path).Replace('\', '/')
    $separator = $relativeKey.IndexOf('/')
    if ($separator -le 0) { throw 'Supplemental candidate has no source-root prefix.' }
    $rootName = $relativeKey.Substring(0, $separator)
    if (-not $resolvedRoots.ContainsKey($rootName)) { throw 'Supplemental candidate resolves outside the supplied roots.' }
    $relative = $relativeKey.Substring($separator + 1)
    if ([IO.Path]::IsPathRooted($relative) -or @($relative -split '/' | Where-Object { $_ -ceq '.' -or $_ -ceq '..' }).Count -gt 0) {
        throw 'Supplemental candidate path escapes its source root.'
    }
    $root = [string]$resolvedRoots[$rootName]
    $path = [IO.Path]::GetFullPath((Join-Path $root ($relative.Replace('/', [IO.Path]::DirectorySeparatorChar))) )
    if (-not $path.StartsWith($root.TrimEnd('\') + '\', [StringComparison]::OrdinalIgnoreCase)) { throw 'Resolved candidate is outside its source root.' }
    $item = Get-Item -LiteralPath $path -Force -ErrorAction Stop
    if ($item.PSIsContainer -or ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 -or
        [long]$item.Length -ne [long]$Entry.logical_bytes -or
        (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash.ToLowerInvariant() -cne [string]$Entry.transcript_sha256) {
        throw 'Supplemental candidate size, type, or SHA-256 differs from the committed manifest.'
    }
    $cursor = $item.Directory
    while ($null -ne $cursor -and $cursor.FullName -ne $root) {
        if (($cursor.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) { throw 'Candidate path traverses a reparse point.' }
        $cursor = $cursor.Parent
    }
    if ($null -eq $cursor) { throw 'Candidate path is not beneath its exact source root.' }
    return $path
}

function Test-SupplementalCleanupGates {
    $gitPrefix = @('-c', "safe.directory=$repoRoot", '-C', $repoRoot)
    $git = Get-Command git -ErrorAction Stop
    $branch = (& $git.Source @gitPrefix branch --show-current 2>$null | Out-String).Trim()
    $head = (& $git.Source @gitPrefix rev-parse --verify HEAD 2>$null | Out-String).Trim()
    $origin = (& $git.Source @gitPrefix remote get-url origin 2>$null | Out-String).Trim()
    $remoteMain = @(& $git.Source @gitPrefix ls-remote origin refs/heads/main 2>$null)
    $remoteMatch = if ($remoteMain.Count -eq 1) { [regex]::Match([string]$remoteMain[0], '^([0-9a-f]{40})\s+refs/heads/main$') } else { [regex]::Match('', 'a^') }
    $status = @(& $git.Source @gitPrefix status --porcelain=v1 --untracked-files=all 2>$null)
    $statusExitCode = $LASTEXITCODE
    if ($branch -cne 'main' -or $head -notmatch '^[0-9a-f]{40}$' -or
        $origin -notmatch '(?i)(github\.com[:/]Robert-Velhorst/018-HAI(?:\.git)?$)' -or
        -not $remoteMatch.Success -or $remoteMatch.Groups[1].Value -cne $head -or $statusExitCode -ne 0 -or $status.Count -ne 0) {
        throw 'Supplemental cleanup requires a clean canonical main worktree at the current remote main commit.'
    }
    $main = @(& $mainCheck -TranscriptRoot 'D:\codex-temp\hai-completed-agent-sessions' -RequireSourceArchive -RequireCommittedLedger -RequireMergedPullRequest -PullRequestNumber 37 -GitHubRepository 'Robert-Velhorst/018-HAI')
    if ($LASTEXITCODE -ne 0 -or $main.Count -ne 1) { throw 'Canonical main cleanup gate did not return a verified report.' }
    $mainReport = ($main -join [Environment]::NewLine) | ConvertFrom-Json -ErrorAction Stop
    if ($mainReport.cleanup_gate_ready -ne $true -or $mainReport.deletion_performed -ne $false -or $mainReport.cleanup_authorized -ne $false) {
        throw 'Canonical main cleanup gate failed or reported a mutation.'
    }
    $supplemental = @(& $sourceCheck -TranscriptRoots $TranscriptRoots)
    if ($LASTEXITCODE -ne 0 -or $supplemental.Count -ne 1) { throw 'Supplemental source integrity gate did not return a verified report.' }
    $supplementalReport = ($supplemental -join [Environment]::NewLine) | ConvertFrom-Json -ErrorAction Stop
    if ($supplementalReport.result -cne 'supplemental_source_archive_verified' -or
        $supplementalReport.deletion_performed -ne $false -or $supplementalReport.cleanup_authorized -ne $false) {
        throw 'Supplemental source integrity gate failed or reported a mutation.'
    }
}

$selectedBytes = [long](($selected | Measure-Object -Property logical_bytes -Sum).Sum)
if (-not $Apply) {
    Test-SupplementalCleanupGates
    foreach ($entry in $selected) { $null = Resolve-SupplementalCandidate $entry }
    [pscustomobject][ordered]@{
        mode = 'dry_run'
        candidate_files = $selected.Count
        candidate_bytes = $selectedBytes
        child_ids = @($selected | ForEach-Object child_id)
        readiness_gate_ready = $true
        deletion_performed = $false
    } | ConvertTo-Json -Depth 4
    return
}

$requiredPhrase = "REMOVE HAI SUPPLEMENTAL SESSIONS $($selected.Count)"
if ($ConfirmationPhrase -cne $requiredPhrase) { throw "Confirmation phrase mismatch. Required phrase: $requiredPhrase" }
Test-SupplementalCleanupGates
$paths = @($selected | ForEach-Object { Resolve-SupplementalCandidate $_ })
$processes = Get-CimInstance Win32_Process -ErrorAction Stop
foreach ($root in $resolvedRoots.Values) {
    if (@($processes | Where-Object { [int]$_.ProcessId -ne $PID -and -not [string]::IsNullOrWhiteSpace([string]$_.CommandLine) -and
        ([string]$_.CommandLine).IndexOf([string]$root, [StringComparison]::OrdinalIgnoreCase) -ge 0 }).Count -gt 0) {
        throw 'A running process references a supplemental transcript root; cleanup is blocked.'
    }
}
if (-not $PSCmdlet.ShouldProcess("$($paths.Count) manifest-listed files ($selectedBytes bytes) in the two supplemental roots", 'Remove verified completed HAI supplemental transcripts')) { return }

foreach ($path in $paths) {
    Remove-Item -LiteralPath $path -Force -ErrorAction Stop
    if (Test-Path -LiteralPath $path) { throw 'Supplemental transcript removal could not be verified.' }
}

$expected = @{}
foreach ($entry in $manifest | Where-Object { $selected.child_id -cnotcontains $_.child_id }) {
    $key = ([string]$entry.session_path).Replace('\', '/')
    $expected[$key] = [long]$entry.logical_bytes
}
$actual = @{}
foreach ($rootName in $resolvedRoots.Keys) {
    $root = [string]$resolvedRoots[$rootName]
    foreach ($file in Get-ChildItem -LiteralPath $root -File -Force -Recurse -ErrorAction Stop) {
        if (($file.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) { throw 'Remaining supplemental archive contains a reparse point.' }
        $relative = $file.FullName.Substring($root.Length + 1).Replace('\', '/')
        $key = "$rootName/$relative"
        if (-not $expected.ContainsKey($key) -or $actual.ContainsKey($key) -or [long]$file.Length -ne [long]$expected[$key]) {
            throw 'Post-cleanup supplemental archive differs from the exact retained manifest set.'
        }
        $actual[$key] = $true
    }
}
if ($actual.Count -ne $expected.Count) { throw 'Post-cleanup supplemental archive is missing retained manifest entries.' }
[pscustomobject][ordered]@{
    mode = 'apply'
    removed_files = $paths.Count
    removed_bytes = $selectedBytes
    retained_files_verified = $actual.Count
    removed_child_ids = @($selected | ForEach-Object child_id)
    deletion_performed = ($paths.Count -gt 0)
    post_cleanup_retained_set_verified = $true
} | ConvertTo-Json -Depth 4
