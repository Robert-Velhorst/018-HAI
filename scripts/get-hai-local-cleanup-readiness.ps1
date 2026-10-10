[CmdletBinding()]
param(
    [string]$TranscriptRoot = 'D:\codex-temp\hai-completed-agent-sessions',
    [string]$RecoveryArchiveRoot = (Join-Path $env:LOCALAPPDATA 'HAI\volume-recovery'),
    [ValidateRange(1, 8760)]
    [int]$MinimumFixtureAgeHours = 24,
    [switch]$VerifyTranscriptArchive
)

$ErrorActionPreference = 'Stop'
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$expectedRoot = 'D:\codex-temp\hai-pr-update-018-20261009'
if ([IO.Path]::GetFullPath($repoRoot).TrimEnd('\') -cne $expectedRoot) {
    throw 'Unified HAI cleanup inventory is restricted to the inventoried PR worktree.'
}

function Get-ReadOnlyReport([string]$Name, [string]$ScriptName, [hashtable]$Arguments = @{}) {
    $path = Join-Path $PSScriptRoot $ScriptName
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
        return [pscustomobject]@{ name = $Name; status = 'blocked'; blocker = 'readiness script is missing'; report = $null }
    }
    try {
        $global:LASTEXITCODE = 0
        $lines = @(& $path @Arguments)
        if ($lines.Count -eq 0) {
            return [pscustomobject]@{ name = $Name; status = 'blocked'; blocker = 'readiness command returned no successful report'; report = $null }
        }
        $report = ($lines -join [Environment]::NewLine) | ConvertFrom-Json -ErrorAction Stop
        if ($report.PSObject.Properties.Name -contains 'deletion_performed' -and $report.deletion_performed -ne $false) {
            return [pscustomobject]@{ name = $Name; status = 'blocked'; blocker = 'readiness command violated the no-deletion contract'; report = $null }
        }
        return [pscustomobject]@{ name = $Name; status = 'reported'; blocker = $null; report = $report }
    } catch {
        return [pscustomobject]@{ name = $Name; status = 'blocked'; blocker = [string]$_.Exception.Message; report = $null }
    }
}

$temp = Get-ReadOnlyReport 'synthetic_temp_fixtures' 'test-hai-temp-fixture-cleanup-readiness.ps1' @{ MinimumAgeHours = $MinimumFixtureAgeHours }
$transcriptIntegrity = if ($VerifyTranscriptArchive) {
    Get-ReadOnlyReport 'transcript_source_integrity' 'test-hai-transcript-cleanup-readiness.ps1' @{
        TranscriptRoot = $TranscriptRoot
        RequireSourceArchive = $true
    }
} else {
    [pscustomobject]@{
        name = 'transcript_source_integrity'
        status = 'not_requested'
        blocker = 'Full transcript enumeration and candidate SHA-256 verification were skipped. Pass -VerifyTranscriptArchive for this I/O-intensive check.'
        report = $null
    }
}
$transcripts = Get-ReadOnlyReport 'completed_session_transcripts' 'remove-hai-completed-session-transcripts.ps1' @{ TranscriptRoot = $TranscriptRoot }
$integrity = if ($transcriptIntegrity.status -eq 'reported') { $transcriptIntegrity.report } else { $null }
$integrityStatus = if ($null -eq $integrity) {
    [string]$transcriptIntegrity.status
} elseif ($integrity.source_archive_verified -eq $true) {
    'verified'
} else {
    'not_verified'
}
$integritySummary = [pscustomobject][ordered]@{
    status = $integrityStatus
    result = if ($null -ne $integrity) { [string]$integrity.result } else { [string]$transcriptIntegrity.status }
    source_archive_verified = ($null -ne $integrity -and $integrity.source_archive_verified -eq $true)
    source_archive_files = if ($null -ne $integrity) { $integrity.source_archive_files } else { $null }
    source_archive_logical_bytes = if ($null -ne $integrity) { $integrity.source_archive_logical_bytes } else { $null }
    blocker = if ($null -ne $integrity -and $integrity.source_archive_verified -eq $true) { $null } else { [string]$transcriptIntegrity.blocker }
}
$volumes = Get-ReadOnlyReport 'docker_volumes_and_recovery_archives' 'test-hai-volume-cleanup-readiness.ps1' @{ RecoveryArchiveRoot = $RecoveryArchiveRoot }
$diagnostics = Get-ReadOnlyReport 'pr_diagnostics_and_tool_downloads' 'remove-hai-pr-diagnostic-artifacts.ps1'
$images = Get-ReadOnlyReport 'unreferenced_local_images' 'remove-hai-unreferenced-image.ps1' @{
    ImageReferences = @(
        '018-hai-backend:latest',
        '018-hai-backend-migrate:latest',
        '018-hai-idp:latest',
        '018-hai-frontend:latest',
        '018-hai-nginxconfigmanager:latest'
    )
}

$git = Get-Command git -ErrorAction SilentlyContinue
$branch = $null
$head = $null
$trackedChangeCount = $null
$untrackedFileCount = $null
$pullRequestState = $null
$pullRequestMergedAt = $null
$pullRequestHead = $null
$workspaceBlocker = $null
if ($null -eq $git) {
    $workspaceBlocker = 'git is unavailable; active worktree state could not be verified'
} else {
    $gitPrefix = @('-c', "safe.directory=$repoRoot", '-C', $repoRoot)
    $branchLines = @(& $git.Source @gitPrefix branch --show-current 2>$null)
    if ($LASTEXITCODE -eq 0 -and $branchLines.Count -eq 1) { $branch = [string]$branchLines[0] }
    $headLines = @(& $git.Source @gitPrefix rev-parse HEAD 2>$null)
    if ($LASTEXITCODE -eq 0 -and $headLines.Count -eq 1) { $head = ([string]$headLines[0]).Trim().ToLowerInvariant() }
    $statusLines = @(& $git.Source @gitPrefix status --porcelain=v1 --untracked-files=all 2>$null)
    if ($LASTEXITCODE -eq 0) {
        $trackedChangeCount = @($statusLines | Where-Object { -not ([string]$_).StartsWith('??', [StringComparison]::Ordinal) }).Count
        $untrackedFileCount = @($statusLines | Where-Object { ([string]$_).StartsWith('??', [StringComparison]::Ordinal) }).Count
    } else {
        $workspaceBlocker = 'git could not inventory the active worktree'
    }
}
$gh = Get-Command gh -ErrorAction SilentlyContinue
if ($null -eq $gh) {
    $workspaceBlocker = 'GitHub CLI is unavailable; pull request state could not be verified'
} else {
    $prLines = @(& $gh.Source pr view 36 --repo Robert-Velhorst/018-HAI --json state,mergedAt,headRefOid 2>$null)
    if ($LASTEXITCODE -eq 0 -and $prLines.Count -gt 0) {
        try {
            $pr = ($prLines -join [Environment]::NewLine) | ConvertFrom-Json -ErrorAction Stop
            $pullRequestState = [string]$pr.state
            $pullRequestMergedAt = [string]$pr.mergedAt
            $pullRequestHead = [string]$pr.headRefOid
        } catch {
            $workspaceBlocker = 'GitHub returned invalid PR metadata; worktree retention state could not be verified'
        }
    } else {
        $workspaceBlocker = 'GitHub could not verify PR #36; worktree retention state could not be verified'
    }
}
$worktreeReason = if ($workspaceBlocker) {
    $workspaceBlocker
} elseif ($pullRequestState -ceq 'OPEN') {
    'PR #36 is open; preserve source, environment files, backups, and verification artifacts.'
} elseif (-not [string]::IsNullOrWhiteSpace($pullRequestMergedAt)) {
    'PR #36 is merged, but retain this checkout until changed and untracked files are reviewed and any needed local state is preserved.'
} else {
    'PR #36 is not verified as merged; preserve this checkout and its local state.'
}
$worktreeTarget = [pscustomobject][ordered]@{
    id = 'active_pr_worktree'
    source_path = $repoRoot
    status = 'retain'
    branch = $branch
    head = $head
    pull_request_state = $pullRequestState
    pull_request_merged_at = $pullRequestMergedAt
    pull_request_head = $pullRequestHead
    tracked_change_count = $trackedChangeCount
    untracked_file_count = $untrackedFileCount
    blocker = $worktreeReason
    cleanup_requires_manual_review = $true
    cleanup_authorized = $false
    deletion_performed = $false
}
$sharedToolchainPath = Join-Path (Split-Path $repoRoot -Parent) 'go1.25.12'
$toolchainTarget = [pscustomobject][ordered]@{
    id = 'shared_go_toolchain'
    source_path = $sharedToolchainPath
    status = if (Test-Path -LiteralPath $sharedToolchainPath -PathType Container) { 'retain_shared' } else { 'not_present' }
    blocker = if (Test-Path -LiteralPath $sharedToolchainPath -PathType Container) { 'Shared development toolchain; it is not HAI-exclusive and is not a cleanup candidate.' } else { $null }
    cleanup_authorized = $false
    deletion_performed = $false
}
$secondaryWorktreePath = Join-Path $env:USERPROFILE 'Documents\Codex\2026-05-30\github-plugin-github-openai-curated-noodzakelijk'
$secondaryWorktreeTarget = [pscustomobject][ordered]@{
    id = 'secondary_hai_checkout'
    source_path = $secondaryWorktreePath
    status = 'not_present'
    repository = $null
    branch = $null
    head = $null
    tracked_change_count = $null
    untracked_file_count = $null
    blocker = $null
    cleanup_authorized = $false
    deletion_performed = $false
}
if (Test-Path -LiteralPath $secondaryWorktreePath -PathType Container) {
    if ($null -eq $git) {
        $secondaryWorktreeTarget.status = 'retain_unverified'
        $secondaryWorktreeTarget.blocker = 'A known secondary checkout exists, but git is unavailable; preserve it.'
    } else {
        $secondaryGitPrefix = @('-c', "safe.directory=$secondaryWorktreePath", '-C', $secondaryWorktreePath)
        $secondaryOriginLines = @(& $git.Source @secondaryGitPrefix remote get-url origin 2>$null)
        if ($LASTEXITCODE -ne 0 -or $secondaryOriginLines.Count -ne 1 -or
            [string]$secondaryOriginLines[0] -notmatch '(?i)(github\.com[:/]Robert-Velhorst/018-HAI(?:\.git)?$)') {
            $secondaryWorktreeTarget.status = 'retain_unverified'
            $secondaryWorktreeTarget.blocker = 'A known secondary checkout exists, but its repository identity could not be verified.'
        } else {
            $secondaryBranchLines = @(& $git.Source @secondaryGitPrefix branch --show-current 2>$null)
            if ($LASTEXITCODE -eq 0 -and $secondaryBranchLines.Count -eq 1) { $secondaryWorktreeTarget.branch = [string]$secondaryBranchLines[0] }
            $secondaryHeadLines = @(& $git.Source @secondaryGitPrefix rev-parse HEAD 2>$null)
            if ($LASTEXITCODE -eq 0 -and $secondaryHeadLines.Count -eq 1) { $secondaryWorktreeTarget.head = ([string]$secondaryHeadLines[0]).Trim().ToLowerInvariant() }
            $secondaryStatusLines = @(& $git.Source @secondaryGitPrefix status --porcelain=v1 --untracked-files=all 2>$null)
            if ($LASTEXITCODE -eq 0) {
                $secondaryWorktreeTarget.repository = 'Robert-Velhorst/018-HAI'
                $secondaryWorktreeTarget.tracked_change_count = @($secondaryStatusLines | Where-Object { -not ([string]$_).StartsWith('??', [StringComparison]::Ordinal) }).Count
                $secondaryWorktreeTarget.untracked_file_count = @($secondaryStatusLines | Where-Object { ([string]$_).StartsWith('??', [StringComparison]::Ordinal) }).Count
                $secondaryWorktreeTarget.status = 'retain'
                $secondaryWorktreeTarget.blocker = 'Separate local HAI checkout; preserve it until its branch, changes, and relationship to the PR checkout are reconciled.'
            } else {
                $secondaryWorktreeTarget.status = 'retain_unverified'
                $secondaryWorktreeTarget.blocker = 'Secondary checkout status could not be inventoried; preserve it.'
            }
        }
    }
}

$targets = [Collections.Generic.List[object]]::new()
if ($temp.status -eq 'reported') {
    $candidateDirectories = @($temp.report.directories | Where-Object disposition -CEQ 'candidate_manual_cleanup')
    $targets.Add([pscustomobject][ordered]@{
        id = 'synthetic_temp_fixtures'
        source_path = [string]$temp.report.temp_root
        status = if ($candidateDirectories.Count -gt 0) { 'candidate_requires_explicit_confirmation' } else { 'retain_or_wait' }
        candidate_count = $candidateDirectories.Count
        candidate_bytes = [long](($candidateDirectories | Measure-Object -Property bytes -Sum).Sum)
        retained = @($temp.report.directories | Where-Object disposition -CNE 'candidate_manual_cleanup' | ForEach-Object {
            [pscustomobject]@{
                owner = [string]$_.owner
                disposition = [string]$_.disposition
                age_hours = $_.age_hours
                bytes = [long]$_.bytes
                reason = [string]$_.reason
            }
        })
    })
} else {
    $targets.Add([pscustomobject]@{ id = 'synthetic_temp_fixtures'; status = 'blocked'; blocker = $temp.blocker })
}

if ($transcripts.status -eq 'reported') {
    $targets.Add([pscustomobject][ordered]@{
        id = 'completed_session_transcripts'
        source_path = $TranscriptRoot
        status = [string]$transcripts.report.mode
        candidate_count = [int]$transcripts.report.candidate_files
        candidate_bytes = [long]$transcripts.report.candidate_bytes
        blocker = [string]$transcripts.report.blocker
        source_archive_integrity = $integritySummary
        source_hash_audit_requested = [bool]$VerifyTranscriptArchive
    })
} else {
    $targets.Add([pscustomobject][ordered]@{
        id = 'completed_session_transcripts'
        status = 'blocked'
        blocker = $transcripts.blocker
        source_archive_integrity = $integritySummary
        source_hash_audit_requested = [bool]$VerifyTranscriptArchive
    })
}

if ($volumes.status -eq 'reported') {
    $targets.Add([pscustomobject][ordered]@{
        id = 'docker_volumes_and_recovery_archives'
        status = if ($volumes.report.safe_to_remove_any -eq $true) { 'candidate_requires_explicit_confirmation' } else { 'retain' }
        named_volumes = @($volumes.report.volumes | ForEach-Object {
            [pscustomobject]@{
                name = [string]$_.volume
                recovery_method = [string]$_.recovery_method
                recovery_status = [string]$_.recovery_status
                container_reference_count = [int]$_.container_reference_count
                container_references = @($_.container_references)
                disposition = [string]$_.disposition
                safe_to_remove = [bool]$_.safe_to_remove
            }
        })
        unverified_recovery_bundles = @($volumes.report.unverified_recovery_bundles | ForEach-Object {
            [pscustomobject]@{
                bundle_id = [string]$_.bundle_id
                failure_code = [string]$_.failure_code
                bundle_bytes = [long]$_.bundle_bytes
                disposition = [string]$_.disposition
            }
        })
        unverified_recovery_bundle_bytes = [long]$volumes.report.unverified_recovery_bundle_bytes
        potential_unreferenced_images = @($volumes.report.hai_images | Where-Object container_reference_count -eq 0 | ForEach-Object {
            [pscustomobject]@{
                reference = [string]$_.reference
                image_id = [string]$_.image_id
                size = [string]$_.size
                disposition = [string]$_.disposition
                safe_to_remove = [bool]$_.safe_to_remove
            }
        })
        image_cleanup_authorized = [bool]$volumes.report.image_cleanup_authorized
        blocker = 'Attached or unverified state remains protected; persistent-volume removal requires an independently verified archive and restore drill.'
    })
} else {
    $targets.Add([pscustomobject]@{ id = 'docker_volumes_and_recovery_archives'; status = 'blocked'; blocker = $volumes.blocker })
}

foreach ($check in @($diagnostics, $images)) {
    if ($check.status -eq 'reported') {
        $report = $check.report
        $targets.Add([pscustomobject][ordered]@{
            id = $check.name
            status = [string]$report.mode
            candidate_count = if ($report.PSObject.Properties.Name -contains 'candidate_files') { [int]$report.candidate_files } elseif ($report.PSObject.Properties.Name -contains 'eligible_count') { [int]$report.eligible_count } else { 0 }
            candidate_bytes = if ($report.PSObject.Properties.Name -contains 'candidate_bytes') { [long]$report.candidate_bytes } elseif ($report.PSObject.Properties.Name -contains 'image_size_bytes_estimate_not_reclaimable') { [long]$report.image_size_bytes_estimate_not_reclaimable } else { 0L }
            blocker = [string]$report.blocker
        })
    } else {
        $targets.Add([pscustomobject]@{ id = $check.name; status = 'blocked'; blocker = $check.blocker })
    }
}

$report = [pscustomobject][ordered]@{
    schema_version = 1
    repository = 'Robert-Velhorst/018-HAI'
    pull_request = 36
    worktree = $repoRoot
    generated_utc = [DateTimeOffset]::UtcNow.ToString('o')
    mode = 'read_only_inventory'
    transcript_source_hash_audit_requested = [bool]$VerifyTranscriptArchive
    cleanup_authorized = $false
    deletion_performed = $false
    preserve_active_pr_worktree = $true
    preserve_source_reports_ledgers_and_recovery_archives = $true
    preservation_requirements = @(
        'Do not stop or remove Joyce Work Schedule, ShareT, or LARO containers.',
        'Do not remove attached HAI database/control-state volumes or containers.',
        'Do not remove the active PR worktree or toolchain before PR #36 is merged and its checks pass.',
        'Do not remove retained, aborted, duplicate-ID, or nonterminal transcripts.'
    )
    cleanup_targets = @($targets) + @($worktreeTarget, $secondaryWorktreeTarget, $toolchainTarget)
}
$report | ConvertTo-Json -Depth 10
