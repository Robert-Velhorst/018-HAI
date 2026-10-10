[CmdletBinding(SupportsShouldProcess = $true, ConfirmImpact = 'High')]
param(
    [string]$TranscriptRoot = 'D:\codex-temp\hai-completed-agent-sessions',
    [string[]]$ChildIds = @(),
    [switch]$Apply,
    [string]$ConfirmationPhrase = ''
)

$ErrorActionPreference = 'Stop'
$expectedRoot = 'D:\codex-temp\hai-completed-agent-sessions'
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$archiveRoot = Join-Path $repoRoot 'docs/child-agent-archive-2026-07-30'
$manifestPath = Join-Path $archiveRoot 'child-agent-transcript-manifest.csv'
$readinessScript = Join-Path $PSScriptRoot 'test-hai-transcript-cleanup-readiness.ps1'

function Get-HaiTranscriptReadiness {
    try {
        $output = @(& $readinessScript -TranscriptRoot $TranscriptRoot -RequireSourceArchive -RequireCommittedLedger -RequireMergedPullRequest -PullRequestNumber 36)
        if ($output.Count -eq 0) {
            return [pscustomobject]@{ ready = $false; reason = 'readiness verifier returned an unsuccessful or ambiguous result'; report = $null }
        }
        $report = ($output -join [Environment]::NewLine) | ConvertFrom-Json -ErrorAction Stop
        if ($report.cleanup_gate_ready -ne $true -or $report.cleanup_authorized -ne $false -or $report.deletion_performed -ne $false) {
            return [pscustomobject]@{ ready = $false; reason = 'source, committed-ledger, merged-PR, or no-deletion contract did not pass'; report = $report }
        }
        return [pscustomobject]@{ ready = $true; reason = $null; report = $report }
    } catch {
        return [pscustomobject]@{ ready = $false; reason = 'source/ledger/PR verification failed; no transcript was removed'; report = $null }
    }
}

function Test-HaiTranscriptRemovalPreflight {
    $git = Get-Command git -ErrorAction SilentlyContinue
    if ($null -eq $git) { return [pscustomobject]@{ ready = $false; reason = 'git is unavailable' } }
    $gitPrefix = @('-c', "safe.directory=$repoRoot", '-C', $repoRoot)
    $branch = (& $git.Source @gitPrefix branch --show-current 2>$null | Out-String).Trim()
    if ($LASTEXITCODE -ne 0 -or $branch -cne 'main') {
        return [pscustomobject]@{ ready = $false; reason = 'cleanup must run from canonical main after the PR is merged' }
    }
    $origin = (& $git.Source @gitPrefix remote get-url origin 2>$null | Out-String).Trim()
    if ($LASTEXITCODE -ne 0 -or $origin -notmatch '(?i)(github\.com[:/]Robert-Velhorst/018-HAI(?:\.git)?$)') {
        return [pscustomobject]@{ ready = $false; reason = 'origin does not identify the expected HAI repository' }
    }
    $gh = Get-Command gh -ErrorAction SilentlyContinue
    if ($null -eq $gh) { return [pscustomobject]@{ ready = $false; reason = 'GitHub CLI is unavailable' } }
    $prOutput = @(& $gh.Source pr view 36 --repo Robert-Velhorst/018-HAI --json state,mergedAt,baseRefName,mergeCommit 2>$null)
    if ($LASTEXITCODE -ne 0 -or $prOutput.Count -eq 0) {
        return [pscustomobject]@{ ready = $false; reason = 'GitHub could not verify PR #36' }
    }
    try { $pr = ($prOutput -join [Environment]::NewLine) | ConvertFrom-Json -ErrorAction Stop }
    catch { return [pscustomobject]@{ ready = $false; reason = 'GitHub returned invalid PR metadata' } }
    $mergeCommit = [string]$pr.mergeCommit.oid
    if ([string]$pr.state -cne 'CLOSED' -or [string]::IsNullOrWhiteSpace([string]$pr.mergedAt) -or
        [string]$pr.baseRefName -cne 'main' -or $mergeCommit -notmatch '^[0-9a-f]{40}$') {
        return [pscustomobject]@{ ready = $false; reason = 'PR #36 is not verified as merged into main' }
    }
    $checkOutput = @(& $gh.Source pr checks 36 --repo Robert-Velhorst/018-HAI --json name,state 2>$null)
    if ($LASTEXITCODE -ne 0 -or $checkOutput.Count -eq 0) {
        return [pscustomobject]@{ ready = $false; reason = 'PR #36 checks are unavailable or empty' }
    }
    try { $checks = @(($checkOutput -join [Environment]::NewLine) | ConvertFrom-Json -ErrorAction Stop) }
    catch { return [pscustomobject]@{ ready = $false; reason = 'GitHub returned invalid PR check metadata' } }
    if ($checks.Count -eq 0 -or @($checks | Where-Object { [string]$_.state -cne 'SUCCESS' }).Count -gt 0) {
        return [pscustomobject]@{ ready = $false; reason = 'one or more PR #36 checks are not successful' }
    }
    & $git.Source @gitPrefix merge-base --is-ancestor $mergeCommit HEAD 2>$null
    if ($LASTEXITCODE -ne 0) { return [pscustomobject]@{ ready = $false; reason = 'the verified merge commit is not in canonical main history' } }
    return [pscustomobject]@{ ready = $true; reason = $null }
}

function Get-HaiTranscriptCandidatePath($Entry) {
    $archiveLeaf = Split-Path -Leaf $TranscriptRoot
    $relativeKey = ([string]$Entry.session_path).Replace('\', '/')
    $prefix = $archiveLeaf + '/'
    if (-not $relativeKey.StartsWith($prefix, [StringComparison]::Ordinal) -or
        [IO.Path]::IsPathRooted($relativeKey) -or
        @($relativeKey -split '/' | Where-Object { $_ -ceq '.' -or $_ -ceq '..' }).Count -gt 0) {
        throw 'Manifest entry is outside the exact completed-session archive root.'
    }
    $relative = $relativeKey.Substring($prefix.Length).Replace('/', [IO.Path]::DirectorySeparatorChar)
    $path = [IO.Path]::GetFullPath((Join-Path $TranscriptRoot $relative))
    if (-not $path.StartsWith($TranscriptRoot.TrimEnd('\') + '\', [StringComparison]::OrdinalIgnoreCase)) {
        throw 'Resolved transcript path is outside the exact completed-session archive root.'
    }
    $item = Get-Item -LiteralPath $path -Force -ErrorAction Stop
    if ($item.PSIsContainer -or ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
        throw 'Transcript candidate is not a regular file.'
    }
    $cursor = $item.Directory
    while ($null -ne $cursor -and $cursor.FullName -ne $TranscriptRoot) {
        if (($cursor.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw 'Transcript path traverses a reparse point.'
        }
        $cursor = $cursor.Parent
    }
    if ($null -eq $cursor) { throw 'Transcript path does not resolve beneath the exact archive root.' }
    return $path
}

function Assert-HaiTranscriptCandidate($Entry) {
    if ([string]$Entry.disposition -cne 'candidate_after_ledger_commit' -or
        [string]$Entry.terminal_status -cne 'completed' -or
        [string]$Entry.final_report_preserved -cne 'True' -or
        [int]$Entry.duplicate_id_file_count -ne 1 -or
        [string]$Entry.transcript_sha256 -cnotmatch '^[0-9a-f]{64}$' -or
        [long]$Entry.logical_bytes -le 0) {
        throw 'Manifest candidate does not satisfy the completed, unique, reported, hash-ledgered contract.'
    }
    $path = Get-HaiTranscriptCandidatePath $Entry
    $item = Get-Item -LiteralPath $path -Force
    if ([long]$item.Length -ne [long]$Entry.logical_bytes -or
        (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash.ToLowerInvariant() -cne [string]$Entry.transcript_sha256) {
        throw 'Transcript size or SHA-256 differs from the committed cleanup ledger.'
    }
    return $path
}

$resolvedRoot = (Resolve-Path -LiteralPath $TranscriptRoot -ErrorAction Stop).Path.TrimEnd('\')
if ($resolvedRoot -cne $expectedRoot -or $resolvedRoot -cne [IO.Path]::GetFullPath($expectedRoot).TrimEnd('\')) {
    throw 'Transcript cleanup is restricted to the exact inventoried D:\codex-temp\hai-completed-agent-sessions path.'
}
$rootItem = Get-Item -LiteralPath $resolvedRoot -Force
if (-not $rootItem.PSIsContainer -or ($rootItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
    throw 'Transcript archive root is not a regular directory; cleanup is blocked.'
}
if (-not (Test-Path -LiteralPath $manifestPath -PathType Leaf)) { throw 'Committed transcript manifest is missing.' }
$manifest = @(Import-Csv -LiteralPath $manifestPath)
$candidateEntries = @($manifest | Where-Object disposition -CEQ 'candidate_after_ledger_commit')
$requestedIds = @($ChildIds | ForEach-Object { ([string]$_).ToLowerInvariant() } | Sort-Object -Unique)
$selected = if ($requestedIds.Count -eq 0) {
    $candidateEntries
} else {
    @($candidateEntries | Where-Object { $requestedIds -ccontains ([string]$_.child_id).ToLowerInvariant() })
}
if ($requestedIds.Count -gt 0 -and $selected.Count -ne $requestedIds.Count) {
    throw 'Every requested child ID must match exactly one ledger-approved transcript candidate.'
}
if (@($selected | Group-Object child_id | Where-Object Count -ne 1).Count -gt 0) {
    throw 'Selected transcript IDs are not unique.'
}
$selectedBytes = [long](($selected | Measure-Object -Property logical_bytes -Sum).Sum)
if (-not $Apply) {
    $preflight = Test-HaiTranscriptRemovalPreflight
    if (-not $preflight.ready) {
        [pscustomobject][ordered]@{
            mode = 'blocked'
            candidate_files = $selected.Count
            candidate_bytes = $selectedBytes
            readiness_gate_ready = $false
            blocker = $preflight.reason
            source_hash_audit_started = $false
            cleanup_requires_apply = $true
            deletion_performed = $false
        } | ConvertTo-Json -Depth 4
        return
    }
    $dryRunReadiness = Get-HaiTranscriptReadiness
    if (-not $dryRunReadiness.ready) {
        [pscustomobject][ordered]@{
            mode = 'blocked'
            candidate_files = $selected.Count
            candidate_bytes = $selectedBytes
            readiness_gate_ready = $false
            blocker = $dryRunReadiness.reason
            cleanup_requires_apply = $true
            deletion_performed = $false
        } | ConvertTo-Json -Depth 4
        return
    }
    foreach ($entry in $selected) { $null = Assert-HaiTranscriptCandidate $entry }
    [pscustomobject][ordered]@{
        mode = 'dry_run'
        candidate_files = $selected.Count
        candidate_bytes = $selectedBytes
        readiness_gate_ready = $true
        child_ids = @($selected | ForEach-Object child_id)
        cleanup_requires_apply = $true
        deletion_performed = $false
    } | ConvertTo-Json -Depth 4
    return
}

if ($selected.Count -eq 0) { throw 'No ledger-approved transcript candidates were selected.' }
$requiredPhrase = "REMOVE HAI COMPLETED SESSIONS $($selected.Count)"
if ($ConfirmationPhrase -cne $requiredPhrase) {
    throw "Confirmation phrase mismatch. Required phrase: $requiredPhrase"
}

# Check all repository, merge, source-archive, and candidate-hash gates once immediately before deletion.
$applyPreflight = Test-HaiTranscriptRemovalPreflight
if (-not $applyPreflight.ready) { throw 'Cleanup preflight failed; no transcript was removed.' }
$applyReadiness = Get-HaiTranscriptReadiness
if (-not $applyReadiness.ready) {
    throw 'Repository, merged-PR, or source-archive evidence changed; no transcript was removed.'
}
$paths = [Collections.Generic.List[string]]::new()
foreach ($entry in $selected) { $paths.Add((Assert-HaiTranscriptCandidate $entry)) }

$processes = Get-CimInstance Win32_Process -ErrorAction Stop
$archiveReferences = @($processes | Where-Object {
    -not [string]::IsNullOrWhiteSpace([string]$_.CommandLine) -and
    ([string]$_.CommandLine).IndexOf($resolvedRoot, [StringComparison]::OrdinalIgnoreCase) -ge 0
})
if ($archiveReferences.Count -gt 0) { throw 'A running process references the transcript archive; cleanup is blocked.' }

if (-not $PSCmdlet.ShouldProcess("$($paths.Count) manifest-listed files ($selectedBytes bytes) in $resolvedRoot", 'Remove verified completed HAI session transcripts')) {
    [pscustomobject][ordered]@{
        mode = 'not_applied'
        candidate_files = $paths.Count
        candidate_bytes = $selectedBytes
        readiness_gate_ready = $true
        deletion_performed = $false
    } | ConvertTo-Json -Depth 4
    return
}

$removed = [Collections.Generic.List[object]]::new()
foreach ($index in 0..($selected.Count - 1)) {
    $entry = $selected[$index]
    $path = $paths[$index]
    $null = Assert-HaiTranscriptCandidate $entry
    Remove-Item -LiteralPath $path -Force -ErrorAction Stop
    if (Test-Path -LiteralPath $path) { throw 'Transcript file removal could not be verified.' }
    $removed.Add([pscustomobject]@{ child_id = [string]$entry.child_id; bytes = [long]$entry.logical_bytes })
}

$selectedChildIds = @($selected | ForEach-Object { [string]$_.child_id })
$retainedEntries = @($manifest | Where-Object { $selectedChildIds -cnotcontains ([string]$_.child_id) })
$expectedRetained = @{}
foreach ($entry in $retainedEntries) {
    $relativeKey = ([string]$entry.session_path).Replace('\', '/')
    $prefix = (Split-Path -Leaf $TranscriptRoot) + '/'
    if (-not $relativeKey.StartsWith($prefix, [StringComparison]::Ordinal)) { throw 'Retained manifest entry is outside the archive root.' }
    $expectedRetained[$relativeKey.Substring($prefix.Length)] = [long]$entry.logical_bytes
}
$remainingFiles = @(Get-ChildItem -LiteralPath $resolvedRoot -File -Force -Recurse -ErrorAction Stop)
$actualRetained = @{}
foreach ($file in $remainingFiles) {
    if (($file.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) { throw 'Remaining archive contains a reparse point after cleanup.' }
    $key = $file.FullName.Substring($resolvedRoot.Length + 1).Replace('\', '/')
    if (-not $expectedRetained.ContainsKey($key) -or $actualRetained.ContainsKey($key) -or
        [long]$file.Length -ne [long]$expectedRetained[$key]) {
        throw 'Post-cleanup archive contents differ from the exact retained manifest set.'
    }
    $actualRetained[$key] = $true
}
if ($actualRetained.Count -ne $expectedRetained.Count) {
    throw 'Post-cleanup archive is missing one or more manifest-retained transcripts.'
}

[pscustomobject][ordered]@{
    mode = 'apply'
    removed_files = $removed.Count
    removed_bytes = [long](($removed | Measure-Object -Property bytes -Sum).Sum)
    retained_files_verified = $actualRetained.Count
    removed = @($removed)
    deletion_performed = ($removed.Count -gt 0)
    post_cleanup_retained_set_verified = $true
} | ConvertTo-Json -Depth 5
